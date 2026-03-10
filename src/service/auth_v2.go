package service

import (
	"bubble/src/db/models"
	"bubble/src/logger"
	"fmt"
	"strings"
	"time"
)

// ──────────────────────────────────────────────
// V2 Login - 智能设备感知登录
// ──────────────────────────────────────────────
//
// 设计目标：
//   - 已信任设备 + 密码 → 跳过验证码，直接登录
//   - 设备令牌 (DeviceToken) → 免密码自动登录（类似"记住我"）
//   - 新设备 + 密码 → 需验证码，与 v1 流程一致
//   - 每次使用设备令牌后自动轮转（单次有效、90天过期）
//
// 安全特性：
//   - DeviceToken 仅下发一次，哈希存储，客户端保管原文
//   - 每次使用后轮转 → 即使被窃取，攻击窗口极小
//   - 设备可随时撤销 → 所有关联令牌立即失效
//   - 账户锁定、失败计数与 v1 共享，不可绕过

const (
	// DeviceTokenExpireDays 设备令牌有效期（天）
	DeviceTokenExpireDays = 90
)

// RandTokenForDevice 导出 randToken 供 handler 层使用（设备管理场景）
func RandTokenForDevice(n int) string {
	return randToken(n)
}

// HashTokenForDevice 导出 hashToken 供 handler 层使用
func HashTokenForDevice(token string) string {
	return hashToken(token)
}

// DeviceTokenExpireTime 返回设备令牌过期时间
func DeviceTokenExpireTime() time.Time {
	return time.Now().Add(DeviceTokenExpireDays * 24 * time.Hour)
}

// TrustDeviceV2 为已登录用户信任新设备并签发设备令牌
func (s *Service) TrustDeviceV2(userID uint, deviceName, platform, ip, userAgent string) (*models.TrustedDevice, string, error) {
	deviceID, err := generateDeviceID()
	if err != nil {
		return nil, "", err
	}
	token := randToken(32)
	tokenHash := hashToken(token)
	tokenExpire := DeviceTokenExpireTime()

	td, err := s.Repo.UpsertTrustedDeviceV2(userID, deviceID, deviceName, platform, tokenHash, ip, userAgent, &tokenExpire)
	if err != nil {
		return nil, "", err
	}
	return td, token, nil
}

// LoginV2Result 统一 v2 登录结果
type LoginV2Result struct {
	User                 *models.User `json:"-"`
	RequireVerification  bool         `json:"requireVerification,omitempty"`  // true = 需要验证码
	VerificationExpireIn int          `json:"verificationExpireIn,omitempty"` // 验证码有效期（秒）
	DeviceID             string       `json:"deviceId,omitempty"`             // 设备ID（信任设备时回传）
	DeviceToken          string       `json:"deviceToken,omitempty"`          // 设备令牌（仅下发一次）
}

// LoginV2 v2 统一登录入口
//
// 优先级：
//  1. DeviceToken 免密登录 → 令牌有效则直接登录 + 轮转令牌
//  2. DeviceID 已信任 + 密码 → 跳过验证码
//  3. 新设备 + 密码 → 发验证码，要求二次验证
func (s *Service) LoginV2(email, password, deviceID, deviceToken, deviceName, platform, ip, userAgent string) (*LoginV2Result, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if !isValidEmail(email) {
		return nil, ErrBadRequest
	}

	u, err := s.Repo.GetUserByEmail(email)
	if err != nil || u == nil {
		_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
			Email: email, Type: "v2_login_user_not_found", IP: ip, UserAgent: userAgent, CreatedAt: time.Now(),
		})
		return nil, ErrUnauthorized
	}

	// ── 检查账户锁定 ──
	if u.AccountLockedUntil != nil && time.Now().Before(*u.AccountLockedUntil) {
		remaining := int(time.Until(*u.AccountLockedUntil).Minutes())
		return nil, &Err{Code: 403, Msg: fmt.Sprintf("账户已被锁定，请在 %d 分钟后重试", remaining)}
	}

	deviceID = strings.TrimSpace(deviceID)
	deviceToken = strings.TrimSpace(deviceToken)
	platform = strings.TrimSpace(platform)
	if platform == "" {
		platform = "unknown"
	}

	// ═══════════════════════════════════════════
	// 路径 1: DeviceToken 免密登录
	// ═══════════════════════════════════════════
	if deviceToken != "" && deviceID != "" {
		result, err := s.loginByDeviceTokenV2(u, deviceID, deviceToken, deviceName, platform, ip, userAgent)
		if err == nil {
			return result, nil
		}
		// 设备令牌无效 → 回退到密码验证
		logger.Infof("[AuthV2] Device token invalid for user %d device %s, falling back to password", u.ID, deviceID)
	}

	// ═══════════════════════════════════════════
	// 路径 2 & 3: 需要密码
	// ═══════════════════════════════════════════
	if strings.TrimSpace(password) == "" {
		return nil, &Err{Code: 400, Msg: "需要提供密码"}
	}

	if !verifyPassword(u.PasswordHash, password) {
		return nil, s.handleLoginFailure(u, email, ip, userAgent, "v2_login_wrong_password")
	}

	// 密码正确 → 重置失败计数
	s.resetLoginFailCount(u, ip)

	// ── 路径 2: 已信任设备 → 直接登录 ──
	if deviceID != "" {
		trusted, _ := s.Repo.IsTrustedDevice(u.ID, deviceID)
		if trusted {
			result, err := s.completeLoginV2(u, deviceID, deviceName, platform, ip, userAgent, true)
			if err != nil {
				return nil, err
			}
			_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
				UserID: &u.ID, Email: email, Type: "v2_login_trusted_device",
				IP: ip, UserAgent: userAgent, Meta: fmt.Sprintf("deviceId=%s", deviceID), CreatedAt: time.Now(),
			})
			return result, nil
		}
	}

	// ── 路径 3: 新设备 → 发送验证码 ──
	expiresIn, err := s.sendLoginVerifyCode(u, email, ip)
	if err != nil {
		return nil, err
	}

	_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
		UserID: &u.ID, Email: email, Type: "v2_login_require_verification",
		IP: ip, UserAgent: userAgent, CreatedAt: time.Now(),
	})

	return &LoginV2Result{
		User:                 u,
		RequireVerification:  true,
		VerificationExpireIn: expiresIn,
	}, nil
}

// VerifyLoginV2 v2 登录验证码确认（路径 3 的第二步）
func (s *Service) VerifyLoginV2(email, code, deviceID, deviceName, platform, ip, userAgent string, trustDevice bool) (*LoginV2Result, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if !isValidEmail(email) {
		return nil, ErrBadRequest
	}

	u, err := s.Repo.GetUserByEmail(email)
	if err != nil || u == nil {
		return nil, ErrUnauthorized
	}

	// 验证码校验
	if u.LoginVerifyCodeHash == "" {
		_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
			UserID: &u.ID, Email: email, Type: "v2_verify_code_not_requested", IP: ip, CreatedAt: time.Now(),
		})
		return nil, ErrUnauthorized
	}
	if u.LoginVerifyCodeExpires == nil || time.Now().After(*u.LoginVerifyCodeExpires) {
		_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
			UserID: &u.ID, Email: email, Type: "v2_verify_code_expired", IP: ip, CreatedAt: time.Now(),
		})
		return nil, &Err{Code: 401, Msg: "验证码已过期，请重新获取"}
	}
	if !verifyToken(u.LoginVerifyCodeHash, code) {
		_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
			UserID: &u.ID, Email: email, Type: "v2_verify_code_wrong", IP: ip, CreatedAt: time.Now(),
		})
		return nil, &Err{Code: 401, Msg: "验证码错误"}
	}

	// 验证码正确 → 清除验证码 + 重置失败计数
	now := time.Now()
	_ = s.Repo.UpdateUser(u, map[string]any{
		"login_verify_code_hash":    "",
		"login_verify_code_sent_at": nil,
		"login_verify_code_expires": nil,
		"login_failed_count":        0,
		"login_failed_at":           nil,
		"account_locked_until":      nil,
		"last_login_at":             now,
		"last_login_ip":             ip,
	})

	_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
		UserID: &u.ID, Email: email, Type: "v2_login_verify_success", IP: ip, UserAgent: userAgent, CreatedAt: now,
	})

	platform = strings.TrimSpace(platform)
	if platform == "" {
		platform = "unknown"
	}

	// 完成登录 + 可选信任设备
	return s.completeLoginV2(u, strings.TrimSpace(deviceID), strings.TrimSpace(deviceName), platform, ip, userAgent, trustDevice)
}

// ──────────────────────────────────────────────
// 内部方法
// ──────────────────────────────────────────────

// loginByDeviceTokenV2 设备令牌免密登录
func (s *Service) loginByDeviceTokenV2(u *models.User, deviceID, deviceToken, deviceName, platform, ip, userAgent string) (*LoginV2Result, error) {
	td, err := s.Repo.GetTrustedDevice(u.ID, deviceID)
	if err != nil {
		return nil, ErrUnauthorized
	}
	if !td.Trusted || td.RevokedAt != nil {
		return nil, ErrUnauthorized
	}
	if td.DeviceTokenHash == "" {
		return nil, ErrUnauthorized
	}
	// 检查令牌是否过期
	if td.DeviceTokenExpire != nil && time.Now().After(*td.DeviceTokenExpire) {
		return nil, &Err{Code: 401, Msg: "设备令牌已过期，请重新登录"}
	}
	// 验证令牌
	if !verifyToken(td.DeviceTokenHash, deviceToken) {
		_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
			UserID: &u.ID, Email: u.Email, Type: "v2_device_token_mismatch",
			IP: ip, UserAgent: userAgent, Meta: fmt.Sprintf("deviceId=%s", deviceID), CreatedAt: time.Now(),
		})
		return nil, ErrUnauthorized
	}

	// 令牌有效 → 轮转令牌 + 登录成功
	newToken := randToken(32) // 64 hex chars
	newHash := hashToken(newToken)
	newExpire := time.Now().Add(DeviceTokenExpireDays * 24 * time.Hour)

	_ = s.Repo.RotateDeviceToken(u.ID, deviceID, newHash, &newExpire, ip, userAgent)

	// 更新最后登录时间
	now := time.Now()
	_ = s.Repo.UpdateUser(u, map[string]any{
		"last_login_at": now,
		"last_login_ip": ip,
	})

	_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
		UserID: &u.ID, Email: u.Email, Type: "v2_login_device_token",
		IP: ip, UserAgent: userAgent, Meta: fmt.Sprintf("deviceId=%s", deviceID), CreatedAt: now,
	})

	return &LoginV2Result{
		User:        u,
		DeviceID:    deviceID,
		DeviceToken: newToken, // 下发新令牌，客户端应替换存储
	}, nil
}

// completeLoginV2 完成登录并可选信任设备
func (s *Service) completeLoginV2(u *models.User, deviceID, deviceName, platform, ip, userAgent string, trustDevice bool) (*LoginV2Result, error) {
	result := &LoginV2Result{User: u}

	if !trustDevice || deviceID == "" {
		// 不信任设备 → 无设备令牌
		return result, nil
	}

	// 确保 deviceID 足够安全
	if len(deviceID) < 32 {
		issued, err := generateDeviceID()
		if err != nil {
			return result, nil // 生成失败不阻断登录
		}
		deviceID = issued
	}

	// 生成设备令牌
	token := randToken(32)
	tokenHash := hashToken(token)
	tokenExpire := time.Now().Add(DeviceTokenExpireDays * 24 * time.Hour)

	_, err := s.Repo.UpsertTrustedDeviceV2(u.ID, deviceID, deviceName, platform, tokenHash, ip, userAgent, &tokenExpire)
	if err != nil {
		logger.Warnf("[AuthV2] Failed to upsert trusted device for user %d: %v", u.ID, err)
		return result, nil // 设备信任失败不阻断登录
	}

	result.DeviceID = deviceID
	result.DeviceToken = token
	return result, nil
}

// CompleteLoginWithDeviceTrust 完成登录后信任设备（用于扫码登录等场景）
func (s *Service) CompleteLoginWithDeviceTrust(u *models.User, deviceID, deviceName, platform, ip, userAgent string) (*LoginV2Result, error) {
	return s.completeLoginV2(u, deviceID, deviceName, platform, ip, userAgent, true)
}

// sendLoginVerifyCode 发送登录验证码（提取公共逻辑）
func (s *Service) sendLoginVerifyCode(u *models.User, email, ip string) (int, error) {
	now := time.Now()

	// 频率限制：60秒内只能发送一次
	if u.LoginVerifyCodeSentAt != nil && now.Sub(*u.LoginVerifyCodeSentAt) < 60*time.Second {
		remaining := 60 - int(now.Sub(*u.LoginVerifyCodeSentAt).Seconds())
		return 0, &Err{Code: 429, Msg: fmt.Sprintf("发送过于频繁，请 %d 秒后再试", remaining)}
	}

	code := randNumericCode(6)
	codeHash := hashToken(code)
	const codeExpireMinutes = 5
	expiresAt := now.Add(codeExpireMinutes * time.Minute)

	if err := s.Repo.UpdateUser(u, map[string]any{
		"login_verify_code_hash":    codeHash,
		"login_verify_code_sent_at": now,
		"login_verify_code_expires": expiresAt,
	}); err != nil {
		return 0, err
	}

	// 发送验证码邮件
	if err := s.Email.SendLoginVerificationEmail(email, code); err != nil {
		logger.Errorf("Failed to send login verification email to %s: %v", email, err)
	}

	return codeExpireMinutes * 60, nil
}

// handleLoginFailure 处理登录失败（密码错误）— 增加失败计数 + 锁定逻辑
func (s *Service) handleLoginFailure(u *models.User, email, ip, userAgent, eventType string) error {
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
			UserID: &u.ID, Email: email, Type: "account_locked_" + eventType, IP: ip, UserAgent: userAgent,
			Meta: fmt.Sprintf("Failed attempts: %d", failCount), CreatedAt: now,
		})
		return &Err{Code: 403, Msg: fmt.Sprintf("登录失败次数过多，账户已被锁定 %d 分钟", int(lockDuration.Minutes()))}
	}

	_ = s.Repo.UpdateUser(u, updates)
	_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
		UserID: &u.ID, Email: email, Type: eventType, IP: ip, UserAgent: userAgent,
		Meta: fmt.Sprintf("Attempt %d/%d", failCount, maxFailedAttempts), CreatedAt: now,
	})
	return ErrUnauthorized
}

// resetLoginFailCount 重置登录失败计数
func (s *Service) resetLoginFailCount(u *models.User, ip string) {
	now := time.Now()
	_ = s.Repo.UpdateUser(u, map[string]any{
		"login_failed_count":   0,
		"login_failed_at":      nil,
		"account_locked_until": nil,
		"last_login_at":        now,
		"last_login_ip":        ip,
	})
}
