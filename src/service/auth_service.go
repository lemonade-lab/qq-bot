package service

import (
	"bubble/src/db/models"
	"bubble/src/logger"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWT claims & helpers
type AccessClaims struct {
	UserID uint `json:"uid"`
	jwt.RegisteredClaims
}

type RefreshClaims struct {
	UserID uint `json:"uid"`
	jwt.RegisteredClaims
}

func (s *Service) IssueAccessToken(u *models.User) (string, error) {
	if s.Cfg == nil || s.Cfg.JWTSecret == "" {
		return "", &Err{Code: 500, Msg: "JWT密钥未配置"}
	}
	exp := time.Now().Add(time.Duration(s.Cfg.JWTAccessMinutes) * time.Minute)
	claims := AccessClaims{
		UserID: u.ID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   strconv.FormatUint(uint64(u.ID), 10),
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString([]byte(s.Cfg.JWTSecret))
}

func (s *Service) IssueRefreshToken(u *models.User) (string, error) {
	if s.Cfg == nil || s.Cfg.JWTSecret == "" {
		return "", &Err{Code: 500, Msg: "JWT密钥未配置"}
	}
	exp := time.Now().Add(time.Duration(s.Cfg.JWTRefreshDays) * 24 * time.Hour)
	claims := RefreshClaims{
		UserID: u.ID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   strconv.FormatUint(uint64(u.ID), 10),
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString([]byte(s.Cfg.JWTSecret))
}

func (s *Service) ParseAccessToken(tok string) (uint, error) {
	if s.Cfg == nil || s.Cfg.JWTSecret == "" {
		return 0, &Err{Code: 500, Msg: "JWT密钥未配置"}
	}
	parsed, err := jwt.ParseWithClaims(tok, &AccessClaims{}, func(t *jwt.Token) (interface{}, error) {
		return []byte(s.Cfg.JWTSecret), nil
	})
	if err != nil || !parsed.Valid {
		return 0, ErrUnauthorized
	}
	claims, ok := parsed.Claims.(*AccessClaims)
	if !ok {
		return 0, ErrUnauthorized
	}
	return claims.UserID, nil
}

func (s *Service) ParseRefreshToken(tok string) (uint, error) {
	if s.Cfg == nil || s.Cfg.JWTSecret == "" {
		return 0, &Err{Code: 500, Msg: "JWT密钥未配置"}
	}
	parsed, err := jwt.ParseWithClaims(tok, &RefreshClaims{}, func(t *jwt.Token) (interface{}, error) {
		return []byte(s.Cfg.JWTSecret), nil
	})
	if err != nil || !parsed.Valid {
		return 0, ErrUnauthorized
	}
	claims, ok := parsed.Claims.(*RefreshClaims)
	if !ok {
		return 0, ErrUnauthorized
	}
	return claims.UserID, nil
}

// Register and password login
func (s *Service) Register(name, password string) (*models.User, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrBadRequest
	}
	hash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	u := &models.User{Name: name, Token: "legacy_" + randToken(8), PasswordHash: hash, Status: "offline"}
	if err := s.createUserWithUniqueToken(u); err != nil {
		return nil, err
	}
	return u, nil
}

// RegisterEmail registers a user with email + password (+ optional name).
// If name empty, derive from email local-part.
func (s *Service) RegisterEmail(email, name, password string) (*models.User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if !isValidEmail(email) {
		return nil, ErrBadRequest
	}
	if existing, _ := s.Repo.GetUserByEmail(email); existing != nil && existing.ID != 0 {
		return nil, &Err{Code: 400, Msg: "邮箱已被占用"}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		if i := strings.Index(email, "@"); i > 0 {
			name = email[:i]
		}
	}
	if name == "" {
		return nil, ErrBadRequest
	}
	hash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	u := &models.User{Name: name, Email: email, Token: "legacy_" + randToken(8), PasswordHash: hash, Status: "offline"}
	if err := s.createUserWithUniqueToken(u); err != nil {
		return nil, err
	}
	return u, nil
}

// LoginByPassword performs password login via name + password (legacy).
func (s *Service) LoginByPassword(name, password string) (*models.User, error) {
	u, err := s.Repo.GetUserByName(name)
	if err != nil || u == nil {
		return nil, ErrUnauthorized
	}
	if !verifyPassword(u.PasswordHash, password) {
		return nil, ErrUnauthorized
	}
	return u, nil
}

// LoginByEmail performs password login via email.
func (s *Service) LoginByEmail(email, password string) (*models.User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if !isValidEmail(email) {
		return nil, ErrBadRequest
	}
	u, err := s.Repo.GetUserByEmail(email)
	if err != nil || u == nil {
		// 即使用户不存在，也记录安全事件（防止枚举攻击时，避免响应时间差异）
		_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
			Email:     email,
			Type:      "login_failed_user_not_found",
			CreatedAt: time.Now(),
		})
		return nil, ErrUnauthorized
	}

	// ✅ 检查账户是否被锁定
	if u.AccountLockedUntil != nil && time.Now().Before(*u.AccountLockedUntil) {
		remainingMinutes := int(time.Until(*u.AccountLockedUntil).Minutes())
		_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
			UserID:    &u.ID,
			Email:     email,
			Type:      "login_failed_account_locked",
			CreatedAt: time.Now(),
		})
		return nil, &Err{
			Code: 403,
			Msg:  fmt.Sprintf("账户已被锁定，请在 %d 分钟后重试", remainingMinutes),
		}
	}

	// 验证密码
	if !verifyPassword(u.PasswordHash, password) {
		// ✅ 登录失败：增加失败计数
		failCount := u.LoginFailedCount + 1
		now := time.Now()
		updates := map[string]any{
			"login_failed_count": failCount,
			"login_failed_at":    now,
		}

		// ✅ 连续失败 5 次，锁定账户 15 分钟
		const maxFailedAttempts = 5
		const lockDuration = 15 * time.Minute

		if failCount >= maxFailedAttempts {
			lockUntil := now.Add(lockDuration)
			updates["account_locked_until"] = lockUntil
			_ = s.Repo.UpdateUser(u, updates)
			_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
				UserID:    &u.ID,
				Email:     email,
				Type:      "account_locked_too_many_failures",
				Meta:      fmt.Sprintf("Failed attempts: %d", failCount),
				CreatedAt: now,
			})
			return nil, &Err{
				Code: 403,
				Msg:  fmt.Sprintf("登录失败次数过多，账户已被锁定 %d 分钟", int(lockDuration.Minutes())),
			}
		}

		_ = s.Repo.UpdateUser(u, updates)
		_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
			UserID:    &u.ID,
			Email:     email,
			Type:      "login_failed_wrong_password",
			Meta:      fmt.Sprintf("Attempt %d/%d", failCount, maxFailedAttempts),
			CreatedAt: now,
		})
		return nil, ErrUnauthorized
	}

	// ✅ 登录成功：重置失败计数，解锁账户，更新登录信息
	now := time.Now()
	updates := map[string]any{
		"login_failed_count":   0,
		"login_failed_at":      nil,
		"account_locked_until": nil,
		"last_login_at":        now,
	}
	_ = s.Repo.UpdateUser(u, updates)
	_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
		UserID:    &u.ID,
		Email:     email,
		Type:      "login_success",
		CreatedAt: now,
	})

	return u, nil
}

// RequestLoginVerificationCode 邮箱二次验证登录 - 步骤1：验证账号密码，发送登录验证码
// 返回验证码有效期（秒）
func (s *Service) RequestLoginVerificationCode(email, password, ip string) (int, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if !isValidEmail(email) {
		return 0, ErrBadRequest
	}

	u, err := s.Repo.GetUserByEmail(email)
	if err != nil || u == nil {
		_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
			Email:     email,
			Type:      "login_verify_failed_user_not_found",
			IP:        ip,
			CreatedAt: time.Now(),
		})
		return 0, ErrUnauthorized
	}

	// 检查账户是否被锁定
	if u.AccountLockedUntil != nil && time.Now().Before(*u.AccountLockedUntil) {
		remainingMinutes := int(time.Until(*u.AccountLockedUntil).Minutes())
		return 0, &Err{
			Code: 403,
			Msg:  fmt.Sprintf("账户已被锁定，请在 %d 分钟后重试", remainingMinutes),
		}
	}

	// 验证密码
	if !verifyPassword(u.PasswordHash, password) {
		// 登录失败：增加失败计数（与普通登录共用计数）
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
				Email:     email,
				Type:      "account_locked_login_verify_failures",
				IP:        ip,
				Meta:      fmt.Sprintf("Failed attempts: %d", failCount),
				CreatedAt: now,
			})
			return 0, &Err{
				Code: 403,
				Msg:  fmt.Sprintf("登录失败次数过多，账户已被锁定 %d 分钟", int(lockDuration.Minutes())),
			}
		}

		_ = s.Repo.UpdateUser(u, updates)
		_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
			UserID:    &u.ID,
			Email:     email,
			Type:      "login_verify_failed_wrong_password",
			IP:        ip,
			Meta:      fmt.Sprintf("Attempt %d/%d", failCount, maxFailedAttempts),
			CreatedAt: now,
		})
		return 0, ErrUnauthorized
	}

	// ✅ 密码验证通过，检查发送频率限制（1分钟内只能发送一次）
	now := time.Now()
	if u.LoginVerifyCodeSentAt != nil && now.Sub(*u.LoginVerifyCodeSentAt) < 60*time.Second {
		remaining := 60 - int(now.Sub(*u.LoginVerifyCodeSentAt).Seconds())
		return 0, &Err{
			Code: 429,
			Msg:  fmt.Sprintf("发送过于频繁，请 %d 秒后再试", remaining),
		}
	}

	// 生成6位数字验证码
	code := randNumericCode(6)
	codeHash := hashToken(code)

	// 验证码5分钟有效
	const codeExpireMinutes = 5
	expiresAt := now.Add(codeExpireMinutes * time.Minute)

	// 更新用户的登录验证码信息
	if err := s.Repo.UpdateUser(u, map[string]any{
		"login_verify_code_hash":    codeHash,
		"login_verify_code_sent_at": now,
		"login_verify_code_expires": expiresAt,
	}); err != nil {
		return 0, err
	}

	_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
		UserID:    &u.ID,
		Email:     email,
		Type:      "login_verify_code_sent",
		IP:        ip,
		CreatedAt: now,
	})

	// 发送登录验证码邮件
	if err := s.Email.SendLoginVerificationEmail(email, code); err != nil {
		logger.Errorf("Failed to send login verification email to %s: %v", email, err)
		// 不阻止流程，验证码已生成
	}

	return codeExpireMinutes * 60, nil // 返回有效期（秒）
}

// VerifyLoginCode 邮箱二次验证登录 - 步骤2：验证登录验证码，完成登录
func (s *Service) VerifyLoginCode(email, code, ip string) (*models.User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if !isValidEmail(email) {
		return nil, ErrBadRequest
	}

	u, err := s.Repo.GetUserByEmail(email)
	if err != nil || u == nil {
		return nil, ErrUnauthorized
	}

	// 检查验证码是否存在
	if u.LoginVerifyCodeHash == "" {
		_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
			UserID:    &u.ID,
			Email:     email,
			Type:      "login_verify_code_not_requested",
			IP:        ip,
			CreatedAt: time.Now(),
		})
		return nil, ErrUnauthorized
	}

	// 检查验证码是否过期
	if u.LoginVerifyCodeExpires == nil || time.Now().After(*u.LoginVerifyCodeExpires) {
		_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
			UserID:    &u.ID,
			Email:     email,
			Type:      "login_verify_code_expired",
			IP:        ip,
			CreatedAt: time.Now(),
		})
		return nil, &Err{Code: 401, Msg: "验证码已过期，请重新获取"}
	}

	// 验证验证码
	if !verifyToken(u.LoginVerifyCodeHash, code) {
		_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
			UserID:    &u.ID,
			Email:     email,
			Type:      "login_verify_code_wrong",
			IP:        ip,
			CreatedAt: time.Now(),
		})
		return nil, &Err{Code: 401, Msg: "验证码错误"}
	}

	// ✅ 验证码正确，完成登录
	now := time.Now()
	updates := map[string]any{
		"login_verify_code_hash":    "", // 清空验证码（单次使用）
		"login_verify_code_sent_at": nil,
		"login_verify_code_expires": nil,
		"login_failed_count":        0, // 重置失败计数
		"login_failed_at":           nil,
		"account_locked_until":      nil,
		"last_login_at":             now,
		"last_login_ip":             ip,
	}
	_ = s.Repo.UpdateUser(u, updates)
	_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
		UserID:    &u.ID,
		Email:     email,
		Type:      "login_verify_success",
		IP:        ip,
		CreatedAt: now,
	})

	return u, nil
}

// Email verification flow
func (s *Service) RequestEmailVerification(email string) (string, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if !isValidEmail(email) {
		return "", ErrBadRequest
	}
	u, err := s.Repo.GetUserByEmail(email)
	if err != nil || u == nil {
		return "", ErrNotFound
	}
	if u.EmailVerified {
		return "", &Err{Code: 400, Msg: "已验证"}
	}
	now := time.Now()
	// 注册验证码频率限制：1分钟/次
	if u.EmailVerifyRequestedAt != nil && now.Sub(*u.EmailVerifyRequestedAt) < 60*time.Second {
		return "", ErrTooManyRequests
	}
	code := randNumericCode(6)
	codeHash := hashToken(code)
	if err := s.Repo.UpdateUser(u, map[string]any{
		"email_verify_token_hash":   codeHash,
		"email_verify_requested_at": now,
	}); err != nil {
		return "", err
	}
	_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{UserID: &u.ID, Email: email, Type: "email_verify_requested", CreatedAt: now})

	// Send verification email
	if err := s.Email.SendVerificationEmail(email, code); err != nil {
		_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
			UserID:    &u.ID,
			Email:     email,
			Type:      "email_send_failed",
			CreatedAt: time.Now(),
			Meta:      err.Error(),
		})
	}

	return code, nil
}

func (s *Service) VerifyEmail(email, token string) error {
	email = strings.TrimSpace(strings.ToLower(email))
	if !isValidEmail(email) || strings.TrimSpace(token) == "" {
		return ErrBadRequest
	}
	u, err := s.Repo.GetUserByEmail(email)
	if err != nil || u == nil {
		return ErrNotFound
	}
	if u.EmailVerified {
		return &Err{Code: 400, Msg: "已验证"}
	}
	if u.EmailVerifyTokenHash == "" || u.EmailVerifyTokenHash != hashToken(token) {
		_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{UserID: &u.ID, Email: email, Type: "email_verify_failed", CreatedAt: time.Now(), Meta: "token mismatch"})
		return ErrUnauthorized
	}
	// 检查验证码是否过期（5分钟有效期）
	if u.EmailVerifyRequestedAt != nil && time.Now().Sub(*u.EmailVerifyRequestedAt) > 5*time.Minute {
		_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{UserID: &u.ID, Email: email, Type: "email_verify_failed", CreatedAt: time.Now(), Meta: "token expired"})
		return &Err{Code: 400, Msg: "验证码已过期"}
	}
	if err := s.Repo.UpdateUser(u, map[string]any{"email_verified": true, "email_verify_token_hash": ""}); err != nil {
		return err
	}
	_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{UserID: &u.ID, Email: email, Type: "email_verify_success", CreatedAt: time.Now()})
	return nil
}

// Password reset flow
func (s *Service) RequestPasswordReset(email string) (string, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if !isValidEmail(email) {
		return "", ErrBadRequest
	}
	u, err := s.Repo.GetUserByEmail(email)
	if err != nil || u == nil {
		return "", ErrNotFound
	}
	now := time.Now()
	if u.ResetRequestedAt != nil && now.Sub(*u.ResetRequestedAt) < 60*time.Second {
		return "", ErrTooManyRequests
	}
	if u.ResetRequestedAt != nil && now.Sub(*u.ResetRequestedAt) < time.Hour && u.ResetRequestCount >= 5 {
		return "", ErrTooManyRequests
	}
	count := u.ResetRequestCount
	if u.ResetRequestedAt == nil || now.Sub(*u.ResetRequestedAt) >= time.Hour {
		count = 0
	}
	// 生成6位数字验证码（与登录、注册保持一致）
	code := randNumericCode(6)
	codeHash := hashToken(code)
	// 验证码5分钟有效
	exp := now.Add(5 * time.Minute)
	if err := s.Repo.UpdateUser(u, map[string]any{"reset_token_hash": codeHash, "reset_requested_at": now, "reset_request_count": count + 1, "reset_token_expires": exp}); err != nil {
		return "", err
	}
	_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{UserID: &u.ID, Email: email, Type: "password_reset_requested", CreatedAt: now})

	// Send password reset email
	if err := s.Email.SendPasswordResetEmail(email, code); err != nil {
		_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{
			UserID:    &u.ID,
			Email:     email,
			Type:      "email_send_failed",
			CreatedAt: time.Now(),
			Meta:      err.Error(),
		})
	}

	return code, nil
}

func (s *Service) ResetPassword(email, token, newPassword string) error {
	email = strings.TrimSpace(strings.ToLower(email))
	if !isValidEmail(email) || strings.TrimSpace(token) == "" {
		return ErrBadRequest
	}
	u, err := s.Repo.GetUserByEmail(email)
	if err != nil || u == nil {
		return ErrNotFound
	}
	if u.ResetTokenHash == "" || u.ResetTokenExpires == nil || time.Now().After(*u.ResetTokenExpires) || u.ResetTokenHash != hashToken(token) {
		_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{UserID: &u.ID, Email: email, Type: "password_reset_failed", CreatedAt: time.Now(), Meta: "token invalid"})
		return ErrUnauthorized
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.Repo.UpdateUser(u, map[string]any{"password_hash": hash, "reset_token_hash": "", "reset_token_expires": nil}); err != nil {
		return err
	}
	_ = s.Repo.CreateSecurityEvent(&models.SecurityEvent{UserID: &u.ID, Email: email, Type: "password_reset_success", CreatedAt: time.Now()})
	return nil
}

// Session Management - "Ultimate Authority" for Authentication
// CreateSession creates a new session for the user with 30 days expiry.
func (s *Service) CreateSession(userID uint, ip, userAgent, deviceFingerprint string) (*models.Session, error) {
	sessionToken := randToken(32)                    // 64 hex chars
	expiresAt := time.Now().Add(30 * 24 * time.Hour) // 30 days

	session := &models.Session{
		UserID:            userID,
		SessionToken:      sessionToken,
		IP:                ip,
		UserAgent:         userAgent,
		DeviceFingerprint: deviceFingerprint,
		ExpiresAt:         expiresAt,
		LastUsedAt:        time.Now(),
	}

	if err := s.Repo.CreateSession(session); err != nil {
		return nil, err
	}
	return session, nil
}

// ValidateSession validates session token and returns the session if valid.
// This is the "ultimate authority" check - session is the source of truth.
func (s *Service) ValidateSession(sessionToken string) (*models.Session, error) {
	session, err := s.Repo.GetSessionByToken(sessionToken)
	if err != nil {
		return nil, ErrUnauthorized
	}

	// Check if session is expired
	if time.Now().After(session.ExpiresAt) {
		return nil, &Err{Code: 401, Msg: "会话已过期"}
	}

	// Update last used time asynchronously (best effort)
	go s.Repo.UpdateSessionLastUsed(session.ID)

	return session, nil
}

// RefreshAccessToken issues a new 15-minute access token based on valid session.
// This allows token refresh without re-authentication as long as session is valid.
func (s *Service) RefreshAccessToken(sessionToken string) (string, error) {
	session, err := s.ValidateSession(sessionToken)
	if err != nil {
		return "", err
	}

	user, err := s.Repo.GetUserByID(session.UserID)
	if err != nil {
		return "", ErrUnauthorized
	}

	return s.IssueAccessToken(user)
}

// RevokeSession revokes a specific session (for logout).
func (s *Service) RevokeSession(sessionToken string) error {
	return s.Repo.RevokeSessionByToken(sessionToken)
}

// RevokeAllUserSessions revokes all sessions for a user (for security actions like password change).
func (s *Service) RevokeAllUserSessions(userID uint) error {
	return s.Repo.RevokeAllUserSessions(userID)
}
