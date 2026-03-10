package service

import (
	"bubble/src/db/models"
	"bubble/src/logger"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ──────────────────────────────────────────────
// Phone Auth — 手机号注册/登录（短信验证码）
// ──────────────────────────────────────────────
//
// 设计目标：
//   - 用户输入手机号 → 收到短信验证码 → 输入验证码 → 完成登录/注册
//   - 新用户自动创建账号（无密码），后续可选设置密码/绑定邮箱
//   - 完全兼容现有认证体系（邮箱登录、V2 设备登录等均不受影响）
//
// 接口：
//   POST /api/phone/send-code   — 步骤1：输入手机号，发送短信验证码
//   POST /api/phone/verify      — 步骤2：验证码确认，完成登录/注册
//
// 安全特性：
//   - 防枚举：无论手机号是否存在，统一返回 sent=true
//   - 频率限制：60秒内只能发送一次验证码
//   - 验证码由阿里云号码认证服务自动生成、存储、校验（服务端无需本地管理）
//   - 账户锁定检测（与现有体系共享）

// 中国大陆手机号正则（支持带 +86 前缀或不带前缀）
var phoneRegex = regexp.MustCompile(`^(\+?86)?1[3-9]\d{9}$`)

// isValidPhone 验证手机号格式
func isValidPhone(phone string) bool {
	phone = strings.TrimSpace(phone)
	if phone == "" || len(phone) < 11 || len(phone) > 15 {
		return false
	}
	return phoneRegex.MatchString(phone)
}

// normalizePhone 标准化手机号（去除 +86 前缀，仅保留11位手机号）
func normalizePhone(phone string) string {
	phone = strings.TrimSpace(phone)
	phone = strings.TrimPrefix(phone, "+86")
	phone = strings.TrimPrefix(phone, "86")
	return phone
}

// PhoneSendCodeResult 步骤1结果
type PhoneSendCodeResult struct {
	Sent      bool `json:"sent"`
	ExpiresIn int  `json:"expiresIn"` // 验证码有效期（秒）
}

// PhoneSendCode 手机号登录/注册 - 步骤1：发送短信验证码
//
// 逻辑：
//  1. 手机号已存在 → 发送登录验证码
//  2. 手机号不存在 → 自动静默注册（随机密码，name 从手机号后四位生成）+ 发送验证码
//
// 安全：无论哪种情况，对外统一返回 sent=true，不暴露手机号注册状态
func (s *Service) PhoneSendCode(phone, ip, userAgent string) (*PhoneSendCodeResult, error) {
	phone = normalizePhone(phone)
	if !isValidPhone(phone) {
		return nil, ErrBadRequest
	}

	const codeExpireSeconds = 300 // 5分钟
	now := time.Now()

	// 查找手机号对应的用户
	u, _ := s.Repo.GetUserByPhone(phone)

	if u == nil || u.ID == 0 {
		// 新用户：自动静默注册
		name := "用户" + phone[len(phone)-4:]

		// 生成随机密码（用户后续可设置密码）
		randomPw := randToken(16)
		hash, err := hashPasswordNoCheck(randomPw)
		if err != nil {
			logger.Errorf("[PhoneAuth] Failed to hash password for new phone user %s: %v", phone, err)
			return &PhoneSendCodeResult{Sent: true, ExpiresIn: codeExpireSeconds}, nil
		}

		u = &models.User{
			Name:         name,
			Phone:        &phone,
			Token:        "legacy_" + randToken(8),
			PasswordHash: hash,
			Status:       "offline",
		}
		if err := s.createUserWithUniqueToken(u); err != nil {
			logger.Errorf("[PhoneAuth] Failed to create phone user %s: %v", phone, err)
			// 隐私：不暴露注册状态
			return &PhoneSendCodeResult{Sent: true, ExpiresIn: codeExpireSeconds}, nil
		}
		logger.Infof("[PhoneAuth] New user created via phone: %s (id=%d)", phone, u.ID)
	}

	// 检查账户是否被锁定
	if u.AccountLockedUntil != nil && time.Now().Before(*u.AccountLockedUntil) {
		// 隐私：即使锁定也返回 sent=true
		return &PhoneSendCodeResult{Sent: true, ExpiresIn: codeExpireSeconds}, nil
	}

	// 频率限制：60秒内只能发送一次
	if u.PhoneVerifyCodeSentAt != nil && now.Sub(*u.PhoneVerifyCodeSentAt) < 60*time.Second {
		remaining := 60 - int(now.Sub(*u.PhoneVerifyCodeSentAt).Seconds())
		return nil, &Err{
			Code: 429,
			Msg:  fmt.Sprintf("发送过于频繁，请 %d 秒后再试", remaining),
		}
	}

	// 记录发送时间（用于频率限制）
	if err := s.Repo.UpdateUser(u, map[string]any{
		"phone_verify_code_sent_at": now,
	}); err != nil {
		logger.Errorf("[PhoneAuth] Failed to update send time for %s: %v", phone, err)
		return &PhoneSendCodeResult{Sent: true, ExpiresIn: codeExpireSeconds}, nil
	}

	// 记录安全事件
	_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
		UserID:    &u.ID,
		Type:      "phone_verify_code_sent",
		IP:        ip,
		UserAgent: userAgent,
		CreatedAt: now,
		Meta:      fmt.Sprintf("phone=%s", phone),
	})

	// 调用阿里云号码认证服务发送验证码（由阿里云自动生成验证码）
	if err := s.SMS.SendVerificationCode(phone, SMSTemplateLogin); err != nil {
		logger.Errorf("[PhoneAuth] Failed to send SMS to %s: %v", phone, err)
		// 不阻止流程返回，避免暴露系统状态
	}

	return &PhoneSendCodeResult{Sent: true, ExpiresIn: codeExpireSeconds}, nil
}

// PhoneVerifyCode 手机号登录/注册 - 步骤2：验证短信验证码，完成登录/注册
func (s *Service) PhoneVerifyCode(phone, code, ip, userAgent string) (*models.User, error) {
	phone = normalizePhone(phone)
	if !isValidPhone(phone) {
		return nil, ErrBadRequest
	}

	u, err := s.Repo.GetUserByPhone(phone)
	if err != nil || u == nil {
		return nil, ErrUnauthorized
	}

	// 检查账户是否被锁定
	if u.AccountLockedUntil != nil && time.Now().Before(*u.AccountLockedUntil) {
		remainingMinutes := int(time.Until(*u.AccountLockedUntil).Minutes())
		return nil, &Err{
			Code: 403,
			Msg:  fmt.Sprintf("账户已被锁定，请在 %d 分钟后重试", remainingMinutes),
		}
	}

	// 调用阿里云 CheckSmsVerifyCode 校验验证码
	passed, checkErr := s.SMS.CheckVerificationCode(phone, code)
	if checkErr != nil {
		logger.Errorf("[PhoneAuth] SMS check error for %s: %v", phone, checkErr)
		return nil, &Err{Code: 500, Msg: "验证码校验服务异常，请稍后重试"}
	}

	if !passed {
		// 验证码错误：增加失败计数
		failCount := u.LoginFailedCount + 1
		now := time.Now()
		updates := map[string]any{
			"login_failed_count": failCount,
			"login_failed_at":    now,
		}

		const maxFailedAttempts = 5
		const lockDuration = 15 * time.Minute

		if failCount >= maxFailedAttempts {
			lockUntil := now.Add(lockDuration)
			updates["account_locked_until"] = lockUntil
			_ = s.Repo.UpdateUser(u, updates)
			_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
				UserID:    &u.ID,
				Type:      "account_locked_phone_verify_failures",
				IP:        ip,
				UserAgent: userAgent,
				Meta:      fmt.Sprintf("phone=%s failed_attempts=%d", phone, failCount),
				CreatedAt: now,
			})
			return nil, &Err{
				Code: 403,
				Msg:  fmt.Sprintf("验证失败次数过多，账户已被锁定 %d 分钟", int(lockDuration.Minutes())),
			}
		}

		_ = s.Repo.UpdateUser(u, updates)
		_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
			UserID:    &u.ID,
			Type:      "phone_verify_code_wrong",
			IP:        ip,
			UserAgent: userAgent,
			Meta:      fmt.Sprintf("phone=%s attempt=%d/%d", phone, failCount, maxFailedAttempts),
			CreatedAt: now,
		})
		return nil, &Err{Code: 401, Msg: "验证码错误"}
	}

	// ✅ 验证码正确，完成登录
	now := time.Now()
	updates := map[string]any{
		"phone_verify_code_sent_at": nil,
		"phone_verified":            true, // 标记手机号已验证
		"login_failed_count":        0,    // 重置失败计数
		"login_failed_at":           nil,
		"account_locked_until":      nil,
		"last_login_at":             now,
		"last_login_ip":             ip,
	}
	_ = s.Repo.UpdateUser(u, updates)
	_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
		UserID:    &u.ID,
		Type:      "phone_verify_login_success",
		IP:        ip,
		UserAgent: userAgent,
		Meta:      fmt.Sprintf("phone=%s", phone),
		CreatedAt: now,
	})

	return u, nil
}

// PhoneBindSendCode 绑定手机号 - 步骤1：已登录用户绑定手机号，发送验证码
func (s *Service) PhoneBindSendCode(userID uint, phone, ip, userAgent string) (*PhoneSendCodeResult, error) {
	phone = normalizePhone(phone)
	if !isValidPhone(phone) {
		return nil, ErrBadRequest
	}

	const codeExpireSeconds = 300
	now := time.Now()

	// 检查手机号是否已被其他用户使用
	existingUser, _ := s.Repo.GetUserByPhone(phone)
	if existingUser != nil && existingUser.ID != 0 && existingUser.ID != userID {
		return nil, &Err{Code: 400, Msg: "该手机号已被其他账号绑定"}
	}

	// 获取当前用户
	u, err := s.Repo.GetUserByID(userID)
	if err != nil || u == nil {
		return nil, ErrUnauthorized
	}

	// 频率限制：60秒内只能发送一次
	if u.PhoneVerifyCodeSentAt != nil && now.Sub(*u.PhoneVerifyCodeSentAt) < 60*time.Second {
		remaining := 60 - int(now.Sub(*u.PhoneVerifyCodeSentAt).Seconds())
		return nil, &Err{
			Code: 429,
			Msg:  fmt.Sprintf("发送过于频繁，请 %d 秒后再试", remaining),
		}
	}

	// 仅记录发送时间和待绑定手机号到临时字段，不修改正式 phone
	if err := s.Repo.UpdateUser(u, map[string]any{
		"pending_phone":             phone,
		"phone_verify_code_sent_at": now,
	}); err != nil {
		return nil, err
	}

	_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
		UserID:    &u.ID,
		Type:      "phone_bind_code_sent",
		IP:        ip,
		UserAgent: userAgent,
		CreatedAt: now,
		Meta:      fmt.Sprintf("phone=%s", phone),
	})

	// 调用阿里云发送验证码（绑定新手机号模板）
	if err := s.SMS.SendVerificationCode(phone, SMSTemplateBindPhone); err != nil {
		logger.Errorf("[PhoneAuth] Failed to send bind SMS to %s: %v", phone, err)
	}

	return &PhoneSendCodeResult{Sent: true, ExpiresIn: codeExpireSeconds}, nil
}

// PhoneBindVerify 绑定手机号 - 步骤2：验证码确认，完成绑定
func (s *Service) PhoneBindVerify(userID uint, phone, code, ip, userAgent string) error {
	phone = normalizePhone(phone)
	if !isValidPhone(phone) {
		return ErrBadRequest
	}

	u, err := s.Repo.GetUserByID(userID)
	if err != nil || u == nil {
		return ErrUnauthorized
	}

	// 检查手机号是否匹配待绑定手机号
	if u.PendingPhone == nil || *u.PendingPhone != phone {
		return &Err{Code: 400, Msg: "手机号不匹配"}
	}

	// 调用阿里云校验验证码
	passed, checkErr := s.SMS.CheckVerificationCode(phone, code)
	if checkErr != nil {
		logger.Errorf("[PhoneAuth] SMS check error for bind %s: %v", phone, checkErr)
		return &Err{Code: 500, Msg: "验证码校验服务异常，请稍后重试"}
	}

	if !passed {
		_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
			UserID:    &u.ID,
			Type:      "phone_bind_verify_failed",
			IP:        ip,
			UserAgent: userAgent,
			CreatedAt: time.Now(),
			Meta:      fmt.Sprintf("phone=%s", phone),
		})
		return &Err{Code: 401, Msg: "验证码错误"}
	}

	// 绑定成功：正式写入 phone 字段
	now := time.Now()
	if err := s.Repo.UpdateUser(u, map[string]any{
		"phone":                     phone,
		"phone_verified":            true,
		"pending_phone":             nil,
		"phone_verify_code_sent_at": nil,
	}); err != nil {
		return err
	}

	_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
		UserID:    &u.ID,
		Type:      "phone_bind_success",
		IP:        ip,
		UserAgent: userAgent,
		CreatedAt: now,
		Meta:      fmt.Sprintf("phone=%s", phone),
	})

	return nil
}
