package service

import (
	"bubble/src/db/models"
	"bubble/src/logger"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// ──────────────────────────────────────────────
// Magic Login — 邮箱登录即注册（验证码登录）
// ──────────────────────────────────────────────
//
// 设计目标：
//   - 用户只需输入邮箱 → 收到验证码 → 输入验证码 → 完成登录/注册
//   - 无需密码、无需区分登录和注册
//   - 新用户自动创建账号（无密码），后续可选设置密码
//   - 完全兼容现有认证体系（密码登录、V2 设备登录等均不受影响）
//
// 接口：
//   POST /api/v2/magic/request  — 步骤1：输入邮箱，发送验证码
//   POST /api/v2/magic/verify   — 步骤2：验证码确认，完成登录/注册
//
// 安全特性：
//   - 防枚举：无论邮箱是否存在，统一返回 sent=true
//   - 频率限制：60秒内只能发送一次验证码
//   - 验证码有效期：5分钟
//   - 验证码哈希存储，单次使用后立即失效
//   - 账户锁定检测（与现有体系共享）
//   - 自动标记邮箱已验证（能收到验证码即证明邮箱所有权）

// MagicLoginRequestResult 步骤1的结果
type MagicLoginRequestResult struct {
	Sent      bool `json:"sent"`
	ExpiresIn int  `json:"expiresIn"` // 验证码有效期（秒）
	IsNewUser bool `json:"isNewUser"` // 是否为新用户（仅 debug 模式下返回前端）
}

// MagicLoginRequest 邮箱登录即注册 - 步骤1：发送验证码
//
// 逻辑：
//  1. 邮箱已存在 → 发送登录验证码
//  2. 邮箱不存在 → 自动静默注册（随机密码，name 从邮箱前缀推导）+ 发送验证码
//
// 安全：无论哪种情况，对外统一返回 sent=true，不暴露邮箱注册状态
func (s *Service) MagicLoginRequest(email, ip, userAgent string) (*MagicLoginRequestResult, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if !isValidEmail(email) {
		return nil, ErrBadRequest
	}

	isNewUser := false
	u, _ := s.Repo.GetUserByEmail(email)

	if u == nil || u.ID == 0 {
		// ════════════════════════════════════
		// 新用户：自动静默注册
		// ════════════════════════════════════
		name := ""
		if i := strings.Index(email, "@"); i > 0 {
			name = email[:i]
		}
		if name == "" {
			name = "用户"
		}

		// 使用随机密码（用户后续可通过"设置密码"功能补充）
		randomPwd := randToken(16) // 32 hex chars，足够安全
		hash, err := hashPasswordNoCheck(randomPwd)
		if err != nil {
			logger.Errorf("[MagicLogin] Failed to hash random password: %v", err)
			return nil, &Err{Code: 500, Msg: "服务器内部错误"}
		}

		u = &models.User{
			Name:         name,
			Email:        email,
			Token:        "legacy_" + randToken(8),
			PasswordHash: hash,
			Status:       "offline",
		}
		if err := s.createUserWithUniqueToken(u); err != nil {
			// 并发注册导致的 duplicate key：尝试再次获取已存在的用户
			if strings.Contains(err.Error(), "Duplicate entry") && strings.Contains(err.Error(), "email") {
				u, _ = s.Repo.GetUserByEmail(email)
				if u == nil || u.ID == 0 {
					return nil, &Err{Code: 500, Msg: "服务器内部错误"}
				}
				// 用户已存在，当作老用户继续发验证码
			} else {
				logger.Errorf("[MagicLogin] Failed to create user for %s: %v", email, err)
				return nil, &Err{Code: 500, Msg: "服务器内部错误"}
			}
		} else {
			isNewUser = true
			_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
				UserID: &u.ID, Email: email, Type: "magic_login_auto_register",
				IP: ip, UserAgent: userAgent, CreatedAt: time.Now(),
			})
		}
	}

	// ════════════════════════════════════
	// 检查账户锁定
	// ════════════════════════════════════
	if u.AccountLockedUntil != nil && time.Now().Before(*u.AccountLockedUntil) {
		remaining := int(time.Until(*u.AccountLockedUntil).Minutes())
		return nil, &Err{Code: 403, Msg: fmt.Sprintf("账户已被锁定，请在 %d 分钟后重试", remaining)}
	}

	// ════════════════════════════════════
	// 发送验证码（复用内部方法）
	// ════════════════════════════════════
	expiresIn, err := s.sendLoginVerifyCode(u, email, ip)
	if err != nil {
		return nil, err
	}

	_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
		UserID: &u.ID, Email: email, Type: "magic_login_code_sent",
		IP: ip, UserAgent: userAgent, Meta: fmt.Sprintf("isNewUser=%v", isNewUser), CreatedAt: time.Now(),
	})

	return &MagicLoginRequestResult{
		Sent:      true,
		ExpiresIn: expiresIn,
		IsNewUser: isNewUser,
	}, nil
}

// MagicLoginVerify 邮箱登录即注册 - 步骤2：验证码验证 + 登录
//
// 验证码通过后：
//   - 自动标记邮箱为已验证（EmailVerified = true）
//   - 重置登录失败计数
//   - 完成登录（复用 V2 completeLoginV2 逻辑）
//   - 可选信任设备
func (s *Service) MagicLoginVerify(
	email, code string,
	deviceID, deviceName, platform string,
	ip, userAgent string,
	trustDevice bool,
) (*LoginV2Result, bool, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if !isValidEmail(email) {
		return nil, false, ErrBadRequest
	}

	u, err := s.Repo.GetUserByEmail(email)
	if err != nil || u == nil {
		return nil, false, ErrUnauthorized
	}

	// ── 验证码校验 ──
	if u.LoginVerifyCodeHash == "" {
		_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
			UserID: &u.ID, Email: email, Type: "magic_login_verify_code_not_requested",
			IP: ip, UserAgent: userAgent, CreatedAt: time.Now(),
		})
		return nil, false, ErrUnauthorized
	}
	if u.LoginVerifyCodeExpires == nil || time.Now().After(*u.LoginVerifyCodeExpires) {
		_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
			UserID: &u.ID, Email: email, Type: "magic_login_verify_code_expired",
			IP: ip, UserAgent: userAgent, CreatedAt: time.Now(),
		})
		return nil, false, &Err{Code: 401, Msg: "验证码已过期，请重新获取"}
	}
	if !verifyToken(u.LoginVerifyCodeHash, code) {
		_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
			UserID: &u.ID, Email: email, Type: "magic_login_verify_code_wrong",
			IP: ip, UserAgent: userAgent, CreatedAt: time.Now(),
		})
		return nil, false, &Err{Code: 401, Msg: "验证码错误"}
	}

	// ── 验证码正确 → 完成登录 ──
	now := time.Now()
	isNewUser := !u.EmailVerified && u.LastLoginAt == nil

	updates := map[string]any{
		"login_verify_code_hash":    "", // 清空验证码（单次使用）
		"login_verify_code_sent_at": nil,
		"login_verify_code_expires": nil,
		"login_failed_count":        0, // 重置失败计数
		"login_failed_at":           nil,
		"account_locked_until":      nil,
		"last_login_at":             now,
		"last_login_ip":             ip,
		"email_verified":            true, // ✅ 自动标记邮箱已验证
	}
	_ = s.Repo.UpdateUser(u, updates)

	_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
		UserID: &u.ID, Email: email, Type: "magic_login_success",
		IP: ip, UserAgent: userAgent, Meta: fmt.Sprintf("isNewUser=%v", isNewUser), CreatedAt: now,
	})

	// 更新内存中的用户对象以返回正确的状态
	u.EmailVerified = true
	u.LastLoginAt = &now

	// ── 完成登录 + 可选信任设备（复用 V2 逻辑）──
	platform = strings.TrimSpace(platform)
	if platform == "" {
		platform = "unknown"
	}

	result, err := s.completeLoginV2(u, strings.TrimSpace(deviceID), strings.TrimSpace(deviceName), platform, ip, userAgent, trustDevice)
	if err != nil {
		return nil, false, err
	}

	return result, isNewUser, nil
}

// hashPasswordNoCheck 哈希密码但不检查强度（用于内部自动生成的随机密码）
func hashPasswordNoCheck(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}
