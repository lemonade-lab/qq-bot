package handlers

import (
	"net/http"
	"strings"

	"bubble/src/middleware"
	"bubble/src/service"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// ==================== Authentication & Authorization ====================

// @Summary      Register by email
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      map[string]string  true  "{email,password,name(optional)}"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Router       /api/register/email [post]
// registerByEmail 通过邮箱+密码注册账号，支持可选 name 字段。
// Method/Path: POST /api/register/email
// 请求体: {"email": "邮箱", "password": "密码", "name": "昵称(可选)"}
// 响应: 200 用户对象（通过 HttpOnly Cookie 设置 token）。
// 错误: 400 邮箱格式不合法 / 已被占用 / 其他服务层错误。
// 后续: 可发送验证邮件（当前逻辑通过 Service 处理）。
func (h *HTTP) registerByEmail(c *gin.Context) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误：需要邮箱和密码"})
		return
	}
	u, err := h.Svc.RegisterEmail(body.Email, body.Name, body.Password)
	if err != nil {
		logger.Errorf("[Auth] Failed to register email %s: %v", body.Email, err)
		c.JSON(400, gin.H{"error": "注册失败"})
		return
	}

	// 分离处理 Web 端和移动端：
	// - Web端：仅使用 Session Cookie，不使用 JWT
	// - 移动端：使用双 Token：Access Token (15分钟) + Refresh Token (30天)
	if strings.EqualFold(c.GetHeader("X-Client-Type"), "mobile") {
		// 移动端：签发双 Token (Access + Refresh)
		accessToken, err := h.Svc.IssueAccessToken(u)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "签发访问令牌失败"})
			return
		}

		refreshToken, err := h.Svc.IssueRefreshToken(u)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "签发刷新令牌失败"})
			return
		}

		c.JSON(200, gin.H{
			"id":               u.ID,
			"name":             u.Name,
			"email":            u.Email,
			"accessToken":      accessToken,
			"refreshToken":     refreshToken,
			"expiresIn":        h.Cfg.JWTAccessMinutes * 60,         // Access Token 有效期（秒）
			"refreshExpiresIn": h.Cfg.JWTRefreshDays * 24 * 60 * 60, // Refresh Token 有效期（秒）
		})
		return
	}

	// Web端：仅使用 Session Cookie
	deviceFingerprint := getDeviceFingerprint(c)
	session, err := h.Svc.CreateSession(u.ID, c.ClientIP(), c.Request.UserAgent(), deviceFingerprint)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建会话失败"})
		return
	}

	setSecureSessionCookie(c, session.SessionToken, h.Cfg.SessionCookieName)

	c.JSON(200, gin.H{
		"id":            u.ID,
		"name":          u.Name,
		"email":         u.Email,
		"emailVerified": u.EmailVerified,
		"status":        u.Status,
		"createdAt":     u.CreatedAt,
	})
}

// @Summary      Register by email (requires email verification)
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      map[string]string  true  "{email,password,name(optional)}"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Router       /api/register/email/verify/request [post]
// requestRegisterEmailVerification 新注册流程 - 步骤1：创建账号并发送邮箱验证码。
// - 若邮箱已存在且未验证：要求密码匹配后允许重新发送验证码。
// - 若邮箱已存在且已验证：返回邮箱已被占用。
// 响应: 200 {"sent": true}
func (h *HTTP) requestRegisterEmailVerification(c *gin.Context) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误：需要邮箱和密码"})
		return
	}
	email := strings.TrimSpace(strings.ToLower(body.Email))
	if email == "" || strings.TrimSpace(body.Password) == "" {
		c.JSON(400, gin.H{"error": "请求体格式错误：需要邮箱和密码"})
		return
	}

	// 隐私：避免枚举邮箱是否存在。
	// - 邮箱已存在且已验证：不发送验证码，但返回 sent=true
	// - 邮箱已存在但未验证：仅当密码匹配才发送验证码，否则不发送但仍返回 sent=true
	if h.Svc != nil && h.Svc.Repo != nil {
		if existing, _ := h.Svc.Repo.GetUserByEmail(email); existing != nil && existing.ID != 0 {
			if !existing.EmailVerified {
				if _, err := h.Svc.LoginByEmail(email, body.Password); err == nil {
					_, _ = h.Svc.RequestEmailVerification(email)
				}
			}
			c.JSON(200, gin.H{"sent": true})
			return
		}
	}

	// 新用户：先创建账号（未验证），再发送邮箱验证码
	if _, err := h.Svc.RegisterEmail(email, body.Name, body.Password); err != nil {
		// 隐私：即使邮箱并发注册导致“已被占用”，也统一返回 sent=true
		if strings.Contains(err.Error(), "邮箱已被占用") {
			c.JSON(200, gin.H{"sent": true})
			return
		}
		logger.Errorf("[Auth] Failed to register email verification for %s: %v", email, err)
		c.JSON(400, gin.H{"error": "注册失败"})
		return
	}
	_, _ = h.Svc.RequestEmailVerification(email)
	c.JSON(200, gin.H{"sent": true})
}

// @Summary      Verify register email code (complete registration)
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      map[string]string  true  "{email,token}"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Router       /api/register/email/verify [post]
// verifyRegisterEmail 新注册流程 - 步骤2：验证邮箱验证码并完成注册。
// - Web端：创建 Session Cookie
// - 移动端：返回 Access/Refresh Token
func (h *HTTP) verifyRegisterEmail(c *gin.Context) {
	var body struct {
		Email string `json:"email"`
		Token string `json:"token"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误：需要邮箱和验证码"})
		return
	}
	email := strings.TrimSpace(strings.ToLower(body.Email))
	if email == "" || strings.TrimSpace(body.Token) == "" {
		c.JSON(400, gin.H{"error": "请求体格式错误：需要邮箱和验证码"})
		return
	}

	if err := h.Svc.VerifyEmail(email, body.Token); err != nil {
		// 隐私：不暴露邮箱是否存在/是否已验证等状态
		if err == service.ErrBadRequest {
			c.JSON(400, gin.H{"error": "参数错误"})
			return
		}
		c.JSON(400, gin.H{"error": "验证码错误或已过期"})
		return
	}
	// 获取用户
	u, err := h.Svc.Repo.GetUserByEmail(email)
	if err != nil || u == nil {
		c.JSON(400, gin.H{"error": "验证码错误或已过期"})
		return
	}

	// Web / Mobile 分流
	if strings.EqualFold(c.GetHeader("X-Client-Type"), "mobile") {
		accessToken, err := h.Svc.IssueAccessToken(u)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "签发访问令牌失败"})
			return
		}
		refreshToken, err := h.Svc.IssueRefreshToken(u)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "签发刷新令牌失败"})
			return
		}
		c.JSON(200, gin.H{
			"id":               u.ID,
			"name":             u.Name,
			"email":            u.Email,
			"emailVerified":    u.EmailVerified,
			"accessToken":      accessToken,
			"refreshToken":     refreshToken,
			"expiresIn":        h.Cfg.JWTAccessMinutes * 60,
			"refreshExpiresIn": h.Cfg.JWTRefreshDays * 24 * 60 * 60,
		})
		return
	}

	deviceFingerprint := getDeviceFingerprint(c)
	session, err := h.Svc.CreateSession(u.ID, c.ClientIP(), c.Request.UserAgent(), deviceFingerprint)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建会话失败"})
		return
	}
	setSecureSessionCookie(c, session.SessionToken, h.Cfg.SessionCookieName)
	// 注册完成后返回用户信息
	c.JSON(200, gin.H{
		"id":            u.ID,
		"name":          u.Name,
		"email":         u.Email,
		"emailVerified": u.EmailVerified,
		"status":        u.Status,
		"createdAt":     u.CreatedAt,
	})
}

// @Summary      Login by email/password
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      map[string]string  true  "{email,password}"
// @Success      200   {object}  map[string]any
// @Failure      401   {object}  map[string]string
// @Router       /api/login [post]
// loginByEmail 邮箱+密码登录，并在配置要求下检查邮箱是否已验证。
// Method/Path: POST /api/login
// 请求体: {"email":"","password":""}
// 响应: 200 用户对象（通过 HttpOnly Cookie 设置双层认证）
//   - Session Cookie: 30天有效期，认证的终极权威
//   - Token Cookie: 5分钟有效期，过期后自动刷新（前端无需处理）
//
// 错误: 400 body 解析失败；401 认证失败；403 若 RequireEmailVerified 为 true 且未验证邮箱。
// 安全: 可增加失败次数限制与 MFA。
func (h *HTTP) loginByEmail(c *gin.Context) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误：需要邮箱和密码"})
		return
	}
	u, err := h.Svc.LoginByEmail(body.Email, body.Password)
	if err != nil {
		// 区分邮箱格式错误和认证失败，但保持安全性
		if err == service.ErrBadRequest {
			c.JSON(400, gin.H{"error": "邮箱格式不正确"})
		} else {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "邮箱或密码错误"})
		}
		return
	}
	if h.Cfg != nil && h.Cfg.RequireEmailVerified && !u.EmailVerified {
		c.JSON(http.StatusForbidden, gin.H{"error": "邮箱未验证，请检查邮箱"})
		return
	}

	// 分离处理 Web 端和移动端
	if strings.EqualFold(c.GetHeader("X-Client-Type"), "mobile") {
		// 移动端：签发双 Token (Access + Refresh)
		accessToken, err := h.Svc.IssueAccessToken(u)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "签发访问令牌失败"})
			return
		}

		refreshToken, err := h.Svc.IssueRefreshToken(u)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "签发刷新令牌失败"})
			return
		}

		c.JSON(200, gin.H{
			"id":               u.ID,
			"name":             u.Name,
			"email":            u.Email,
			"accessToken":      accessToken,
			"refreshToken":     refreshToken,
			"expiresIn":        h.Cfg.JWTAccessMinutes * 60,         // Access Token 有效期（秒）
			"refreshExpiresIn": h.Cfg.JWTRefreshDays * 24 * 60 * 60, // Refresh Token 有效期（秒）
		})
		return
	}

	// Web端：仅使用 Session Cookie
	deviceFingerprint := getDeviceFingerprint(c)
	session, err := h.Svc.CreateSession(u.ID, c.ClientIP(), c.Request.UserAgent(), deviceFingerprint)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建会话失败"})
		return
	}

	setSecureSessionCookie(c, session.SessionToken, h.Cfg.SessionCookieName)

	c.JSON(200, gin.H{
		"id":            u.ID,
		"name":          u.Name,
		"email":         u.Email,
		"emailVerified": u.EmailVerified,
		"status":        u.Status,
		"createdAt":     u.CreatedAt,
	})
}

// @Summary Refresh access token (mobile)
// @Tags auth
// @Accept json
// @Produce json
// @Param body body map[string]string true "{refreshToken}"
// @Success 200 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /api/mobile/refresh [post]
// mobileRefresh 使用 Refresh Token（JWT，30天有效期）刷新 Access Token（15分钟有效期）
// 注意：Refresh Token 仅用于此接口，其他所有接口都使用 Access Token
func (h *HTTP) mobileRefresh(c *gin.Context) {
	var body struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := c.BindJSON(&body); err != nil || body.RefreshToken == "" {
		// 也支持从 header 中读取
		body.RefreshToken = c.GetHeader("X-Refresh-Token")
		if body.RefreshToken == "" {
			c.JSON(401, gin.H{"error": "缺少刷新令牌"})
			return
		}
	}

	// 解析 Refresh Token (JWT)
	userID, err := h.Svc.ParseRefreshToken(body.RefreshToken)
	if err != nil {
		c.JSON(401, gin.H{"error": "刷新令牌无效或已过期"})
		return
	}

	// 获取用户信息
	user, err := h.Svc.GetUserByID(userID)
	if err != nil || user == nil {
		c.JSON(401, gin.H{"error": "用户不存在"})
		return
	}

	// 签发新的 Access Token
	newAccessToken, err := h.Svc.IssueAccessToken(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "签发访问令牌失败"})
		return
	}

	c.JSON(200, gin.H{"accessToken": newAccessToken, "expiresIn": h.Cfg.JWTAccessMinutes * 60})
}

// @Summary Request login verification code (Step 1: Email Two-Factor Login)
// @Tags auth
// @Accept json
// @Produce json
// @Param body body map[string]string true "{email,password}"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 429 {object} map[string]string
// @Router /api/login/email [post]
// requestLoginVerification 邮箱二次验证登录 - 步骤1：验证账号密码，发送登录验证码到邮箱
// 请求体: {"email":"","password":""}
// 响应: 200 {"sent": true, "expiresIn": 300} (验证码5分钟内有效)
// 错误:
//   - 400 参数错误
//   - 401 账号密码错误
//   - 429 发送过于频繁（1分钟内只能发送一次）
//
// 安全特性:
//   - 验证账号密码（但不直接登录）
//   - 1分钟内只能发送一次验证码
//   - 验证码5分钟有效期
//   - 登录失败计数
func (h *HTTP) requestLoginVerification(c *gin.Context) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误：需要邮箱和密码"})
		return
	}

	// 调用 Service 层验证账号密码并发送登录验证码
	expiresIn, err := h.Svc.RequestLoginVerificationCode(body.Email, body.Password, c.ClientIP())
	if err != nil {
		// 根据错误类型返回不同的 HTTP 状态码
		if err == service.ErrBadRequest {
			c.JSON(400, gin.H{"error": "邮箱格式不正确"})
		} else if err == service.ErrTooManyRequests {
			c.JSON(429, gin.H{"error": "发送过于频繁，请1分钟后再试"})
		} else if err == service.ErrUnauthorized {
			c.JSON(401, gin.H{"error": "邮箱或密码错误"})
		} else {
			logger.Errorf("[Auth] Failed to send login code to %s: %v", body.Email, err)
			c.JSON(400, gin.H{"error": "发送验证码失败"})
		}
		return
	}

	c.JSON(200, gin.H{
		"sent":      true,
		"expiresIn": expiresIn, // 验证码有效期（秒）
		"message":   "登录验证码已发送到您的邮箱，请查收",
	})
}

// @Summary Verify login code and complete login (Step 2: Email Two-Factor Login)
// @Tags auth
// @Accept json
// @Produce json
// @Param body body map[string]string true "{email,code}"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /api/login/email/verify [post]
// verifyLoginCode 邮箱二次验证登录 - 步骤2：验证登录验证码，完成登录
// 请求体: {"email":"","code":""}
// 响应: 200 用户对象 + token/session
// 错误:
//   - 400 参数错误
//   - 401 验证码错误或已过期
//
// 安全特性:
//   - 验证码必须在5分钟内使用
//   - 验证码使用后立即失效
//   - 重置登录失败计数
func (h *HTTP) verifyLoginCode(c *gin.Context) {
	var body struct {
		Email       string `json:"email"`
		Code        string `json:"code"`
		DeviceID    string `json:"deviceId"`
		DeviceName  string `json:"deviceName"`
		TrustDevice bool   `json:"trustDevice"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误：需要邮箱和验证码"})
		return
	}

	// 调用 Service 层验证登录验证码
	u, err := h.Svc.VerifyLoginCode(body.Email, body.Code, c.ClientIP())
	if err != nil {
		if err == service.ErrBadRequest {
			c.JSON(400, gin.H{"error": "邮箱格式不正确"})
		} else if err == service.ErrUnauthorized {
			c.JSON(401, gin.H{"error": "验证码错误或已过期"})
		} else {
			logger.Errorf("[Auth] Failed to verify login code for %s: %v", body.Email, err)
			c.JSON(400, gin.H{"error": "验证失败"})
		}
		return
	}

	// 分离处理 Web 端和移动端
	if strings.EqualFold(c.GetHeader("X-Client-Type"), "mobile") {
		// 可选：绑定可信设备（仅移动端）
		var trustedDeviceID string
		if body.TrustDevice {
			// 升级：deviceId 可由服务端签发（客户端可不传或传短 id），最终生效的 deviceId 会回传。
			if td, err := h.Svc.TrustDevice(u.ID, body.DeviceID, body.DeviceName, c.ClientIP(), c.Request.UserAgent()); err == nil && td != nil {
				trustedDeviceID = td.DeviceID
			}
		}

		// 移动端：签发双 Token (Access + Refresh)
		accessToken, err := h.Svc.IssueAccessToken(u)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "签发访问令牌失败"})
			return
		}

		refreshToken, err := h.Svc.IssueRefreshToken(u)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "签发刷新令牌失败"})
			return
		}

		c.JSON(200, gin.H{
			"id":               u.ID,
			"name":             u.Name,
			"email":            u.Email,
			"emailVerified":    u.EmailVerified,
			"accessToken":      accessToken,
			"refreshToken":     refreshToken,
			"expiresIn":        h.Cfg.JWTAccessMinutes * 60,         // Access Token 有效期（秒）
			"refreshExpiresIn": h.Cfg.JWTRefreshDays * 24 * 60 * 60, // Refresh Token 有效期（秒）
			"trustedDeviceId":  trustedDeviceID,
		})
		return
	}

	// Web端：仅使用 Session Cookie
	deviceFingerprint := getDeviceFingerprint(c)
	session, err := h.Svc.CreateSession(u.ID, c.ClientIP(), c.Request.UserAgent(), deviceFingerprint)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建会话失败"})
		return
	}

	setSecureSessionCookie(c, session.SessionToken, h.Cfg.SessionCookieName)

	c.JSON(200, gin.H{
		"id":            u.ID,
		"name":          u.Name,
		"email":         u.Email,
		"emailVerified": u.EmailVerified,
		"status":        u.Status,
		"createdAt":     u.CreatedAt,
	})
}

// @Summary Mobile login by trusted device
// @Tags auth
// @Accept json
// @Produce json
// @Param body body map[string]string true "{email,deviceId}"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Router /api/mobile/login/device [post]
// mobileLoginByDevice 移动端可信设备登录：通过 (email + deviceId) 换取双 Token。
// 注意：该接口仅对设置 X-Client-Type: mobile 的客户端开放。
func (h *HTTP) mobileLoginByDevice(c *gin.Context) {
	if !strings.EqualFold(c.GetHeader("X-Client-Type"), "mobile") {
		c.JSON(400, gin.H{"error": "该接口仅供移动端使用"})
		return
	}
	var body struct {
		Email    string `json:"email"`
		DeviceID string `json:"deviceId"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误：需要邮箱和设备号"})
		return
	}

	u, err := h.Svc.LoginByTrustedDevice(body.Email, body.DeviceID, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		c.JSON(401, gin.H{"error": "设备未受信任或已被撤销"})
		return
	}

	accessToken, err := h.Svc.IssueAccessToken(u)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "签发访问令牌失败"})
		return
	}
	refreshToken, err := h.Svc.IssueRefreshToken(u)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "签发刷新令牌失败"})
		return
	}

	c.JSON(200, gin.H{
		"id":               u.ID,
		"name":             u.Name,
		"email":            u.Email,
		"emailVerified":    u.EmailVerified,
		"accessToken":      accessToken,
		"refreshToken":     refreshToken,
		"expiresIn":        h.Cfg.JWTAccessMinutes * 60,
		"refreshExpiresIn": h.Cfg.JWTRefreshDays * 24 * 60 * 60,
	})
}

// @Summary List trusted devices
// @Tags auth
// @Produce json
// @Success 200 {array} models.TrustedDevice
// @Router /api/mobile/devices [get]
func (h *HTTP) mobileListDevices(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(401, gin.H{"error": "未认证"})
		return
	}
	list, err := h.Svc.ListTrustedDevices(u.ID)
	if err != nil {
		c.JSON(500, gin.H{"error": "获取设备列表失败"})
		return
	}
	c.JSON(200, list)
}

// @Summary Revoke trusted device
// @Tags auth
// @Accept json
// @Produce json
// @Param body body map[string]string true "{deviceId}"
// @Success 204 {string} string ""
// @Router /api/mobile/devices/revoke [post]
func (h *HTTP) mobileRevokeDevice(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(401, gin.H{"error": "未认证"})
		return
	}
	var body struct {
		DeviceID string `json:"deviceId"`
	}
	if err := c.BindJSON(&body); err != nil || strings.TrimSpace(body.DeviceID) == "" {
		c.JSON(400, gin.H{"error": "缺少设备号"})
		return
	}
	if err := h.Svc.RevokeTrustedDevice(u.ID, body.DeviceID); err != nil {
		c.JSON(500, gin.H{"error": "撤销设备失败"})
		return
	}
	c.Status(204)
}

// @Summary Delete trusted device
// @Tags auth
// @Accept json
// @Produce json
// @Param body body map[string]string true "{deviceId}"
// @Success 204 {string} string ""
// @Router /api/mobile/devices/delete [post]
func (h *HTTP) mobileDeleteDevice(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(401, gin.H{"error": "未认证"})
		return
	}
	var body struct {
		DeviceID string `json:"deviceId"`
	}
	if err := c.BindJSON(&body); err != nil || strings.TrimSpace(body.DeviceID) == "" {
		c.JSON(400, gin.H{"error": "缺少设备号"})
		return
	}
	if err := h.Svc.DeleteTrustedDevice(u.ID, body.DeviceID); err != nil {
		// 如果设备不存在，也视为删除成功，避免重复点击报错
		if err != service.ErrNotFound {
			c.JSON(500, gin.H{"error": "删除设备失败"})
			return
		}
	}
	c.Status(204)
}

// @Summary Add a new trusted device
// @Tags auth
// @Accept json
// @Produce json
// @Param body body map[string]string true "{deviceName}"
// @Success 200 {object} models.TrustedDevice
// @Router /api/mobile/devices/add [post]
func (h *HTTP) mobileAddDevice(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(401, gin.H{"error": "未认证"})
		return
	}
	var body struct {
		DeviceName string `json:"deviceName"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}

	deviceName := strings.TrimSpace(body.DeviceName)
	if deviceName == "" {
		deviceName = "未命名设备"
	}

	device, err := h.Svc.AddTrustedDevice(u.ID, deviceName, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		c.JSON(500, gin.H{"error": "添加设备失败"})
		return
	}
	c.JSON(200, device)
}

// @Summary Request email verification token
// @Tags auth
// @Accept json
// @Produce json
// @Param body body map[string]string true "{email}"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/email/verify/request [post]
// requestEmailVerify 申请邮箱验证令牌。
// Method/Path: POST /api/email/verify/request
// 请求体: {"email": "待验证邮箱"}
// 响应: 200 {"sent": true}；在 debug 模式额外返回 token 字段便于开发调试。
// 错误: 400 邮箱无效或频率受限等服务层错误。
// 频率控制: 建议服务层实现最小间隔/每小时最大次数 (见 securityStatus 中的配置提示)。
// 安全: 生产环境不要返回 token；避免枚举邮箱时的响应差异 (当前逻辑可能不同错误信息需统一)。
func (h *HTTP) requestEmailVerify(c *gin.Context) {
	var body struct {
		Email string `json:"email"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误：需要邮箱"})
		return
	}
	tok, err := h.Svc.RequestEmailVerification(body.Email)
	if err != nil {
		logger.Errorf("[Auth] Failed to request email verification for %s: %v", body.Email, err)
		c.JSON(400, gin.H{"error": "发送验证邮件失败"})
		return
	}
	resp := gin.H{"sent": true}
	if h.Cfg != nil && strings.EqualFold(h.Cfg.GinMode, "debug") { // expose for dev only
		resp["token"] = tok
	}
	c.JSON(200, resp)
}

// @Summary Verify email
// @Tags auth
// @Accept json
// @Produce json
// @Param body body map[string]string true "{email,token}"
// @Success 204 {string} string ""
// @Router /api/email/verify [post]
// verifyEmail 校验邮箱与验证码令牌，成功后标记用户邮箱已验证。
// Method/Path: POST /api/email/verify
// 请求体: {"email": "邮箱", "token": "验证码"}
// 响应: 204 成功无内容。
// 错误: 400 参数错误或令牌失效/不匹配。
// 安全: 令牌应有过期时间与单次使用逻辑；失败计数可记录安全事件。
func (h *HTTP) verifyEmail(c *gin.Context) {
	var body struct {
		Email string `json:"email"`
		Token string `json:"token"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误：需要邮箱和验证码"})
		return
	}
	if err := h.Svc.VerifyEmail(body.Email, body.Token); err != nil {
		logger.Errorf("[Auth] Failed to verify email for %s: %v", body.Email, err)
		c.JSON(400, gin.H{"error": "验证失败"})
		return
	}
	c.Status(204)
}

// @Summary Request password reset
// @Tags auth
// @Accept json
// @Produce json
// @Param body body map[string]string true "{email}"
// @Success 200 {object} map[string]any
// @Router /api/password/reset/request [post]
// requestPasswordReset 申请密码重置令牌。
// Method/Path: POST /api/password/reset/request
// 请求体: {"email": "注册邮箱"}
// 响应: 200 {"sent": true}；debug 模式下包含 token 便于开发。
// 错误: 400 邮箱不存在 / 频率限制 / 服务层错误。
// 安全: 应避免根据返回差异确认邮箱是否存在（可统一返回 sent:true）。
func (h *HTTP) requestPasswordReset(c *gin.Context) {
	var body struct {
		Email string `json:"email"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误：需要邮箱"})
		return
	}
	tok, err := h.Svc.RequestPasswordReset(body.Email)
	if err != nil {
		// 安全：避免通过该接口探测邮箱是否存在
		if err == service.ErrNotFound {
			c.JSON(200, gin.H{"sent": true})
			return
		}
		logger.Errorf("[Auth] Failed to request password reset for %s: %v", body.Email, err)
		c.JSON(400, gin.H{"error": "发送重置邮件失败"})
		return
	}
	resp := gin.H{"sent": true}
	if h.Cfg != nil && strings.EqualFold(h.Cfg.GinMode, "debug") {
		resp["token"] = tok
	}
	c.JSON(200, resp)
}

// @Summary Reset password
// @Tags auth
// @Accept json
// @Produce json
// @Param body body map[string]string true "{email,token,newPassword}"
// @Success 204 {string} string ""
// @Router /api/password/reset [post]
// resetPassword 使用令牌重置密码。
// Method/Path: POST /api/password/reset
// 请求体: {"email": "邮箱", "token": "重置令牌", "newPassword": "新密码"}
// 响应: 204 成功。
// 错误: 400 令牌不匹配/过期/新密码不合规。
// 安全: 新密码策略与令牌失效逻辑需在 Service 中保证；可加入历史密码禁止重用策略。
func (h *HTTP) resetPassword(c *gin.Context) {
	var body struct {
		Email       string `json:"email"`
		Token       string `json:"token"`
		NewPassword string `json:"newPassword"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误：需要邮箱、验证码和新密码"})
		return
	}
	if err := h.Svc.ResetPassword(body.Email, body.Token, body.NewPassword); err != nil {
		logger.Errorf("[Auth] Failed to reset password for %s: %v", body.Email, err)
		c.JSON(400, gin.H{"error": "重置密码失败"})
		return
	}
	c.Status(204)
}
