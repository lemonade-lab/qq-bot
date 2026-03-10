package handlers

import (
	"strings"

	"bubble/src/service"

	"github.com/gin-gonic/gin"
)

// ==================== Magic Login — 邮箱登录即注册 ====================
//
// 降低注册门槛：用户只需输入邮箱 → 收到验证码 → 输入验证码即完成登录/注册。
// 无需密码、无需区分登录和注册页面。

// magicLoginRequest 邮箱登录即注册 - 步骤1：发送验证码
//
// @Summary      Magic Login - 发送验证码（登录即注册）
// @Description  输入邮箱即可登录或注册。邮箱已注册则发送登录验证码，未注册则自动创建账号并发送验证码。
// @Tags         auth-v2
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "请求体"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Failure      403   {object}  map[string]string
// @Failure      429   {object}  map[string]string
// @Router       /api/v2/magic/request [post]
//
// 请求体:
//
//	{
//	  "email": "user@example.com"    // 必填
//	}
//
// 响应:
//
//	{
//	  "sent": true,
//	  "expiresIn": 300,   // 验证码有效期（秒）
//	  "message": "验证码已发送到您的邮箱，请查收"
//	}
func (h *HTTP) magicLoginRequest(c *gin.Context) {
	var body struct {
		Email string `json:"email"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误：需要邮箱"})
		return
	}

	email := strings.TrimSpace(strings.ToLower(body.Email))
	if email == "" {
		c.JSON(400, gin.H{"error": "请提供邮箱地址"})
		return
	}

	result, err := h.Svc.MagicLoginRequest(email, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		handleAuthError(c, err)
		return
	}

	resp := gin.H{
		"sent":      result.Sent,
		"expiresIn": result.ExpiresIn,
		"message":   "验证码已发送到您的邮箱，请查收",
	}

	// debug 模式下返回额外信息便于开发调试
	if h.Cfg != nil && strings.EqualFold(h.Cfg.GinMode, "debug") {
		resp["isNewUser"] = result.IsNewUser
		resp["_hint"] = "验证码已发送到邮箱，使用 POST /api/v2/magic/verify 完成登录"
	}

	c.JSON(200, resp)
}

// magicLoginVerify 邮箱登录即注册 - 步骤2：验证码确认，完成登录/注册
//
// @Summary      Magic Login - 验证码确认（完成登录/注册）
// @Description  验证码确认后完成登录。新用户自动完成注册，邮箱自动标记为已验证。支持设备信任。
// @Tags         auth-v2
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "验证请求"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Router       /api/v2/magic/verify [post]
//
// 请求体:
//
//	{
//	  "email":       "user@example.com",    // 必填
//	  "code":        "123456",              // 必填，6位验证码
//	  "deviceId":    "...",                 // 可选，客户端设备标识
//	  "deviceName":  "iPhone 15 Pro",       // 可选，设备名称
//	  "platform":    "ios",                 // 可选: web/ios/android/desktop
//	  "trustDevice": true                   // 可选，是否信任此设备
//	}
//
// 响应（登录成功）:
//
//	{
//	  "user": { "id": 1, "name": "...", "email": "...", ... },
//	  "isNewUser": true,                      // 是否为新注册用户
//	  "accessToken": "...",                   // 移动端返回
//	  "refreshToken": "...",                  // 移动端返回
//	  "deviceId": "...",                      // 信任设备时返回
//	  "deviceToken": "..."                    // 信任设备时返回
//	}
func (h *HTTP) magicLoginVerify(c *gin.Context) {
	var body struct {
		Email       string `json:"email"`
		Code        string `json:"code"`
		DeviceID    string `json:"deviceId"`
		DeviceName  string `json:"deviceName"`
		Platform    string `json:"platform"`
		TrustDevice bool   `json:"trustDevice"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误：需要邮箱和验证码"})
		return
	}

	email := strings.TrimSpace(strings.ToLower(body.Email))
	code := strings.TrimSpace(body.Code)
	if email == "" || code == "" {
		c.JSON(400, gin.H{"error": "请提供邮箱和验证码"})
		return
	}

	result, isNewUser, err := h.Svc.MagicLoginVerify(
		email, code,
		body.DeviceID, body.DeviceName, body.Platform,
		c.ClientIP(), c.Request.UserAgent(),
		body.TrustDevice,
	)
	if err != nil {
		if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
			return
		}
		handleAuthError(c, err)
		return
	}

	// 使用统一的 V2 登录成功响应，并附加 isNewUser 和 needSetPassword 字段
	h.respondMagicLoginSuccess(c, result, isNewUser)
}

// respondMagicLoginSuccess 统一响应 Magic Login 登录/注册成功
func (h *HTTP) respondMagicLoginSuccess(c *gin.Context, result *service.LoginV2Result, isNewUser bool) {
	u := result.User
	needSetPassword := isNewUser // 新用户需要引导设置密码

	if strings.EqualFold(c.GetHeader("X-Client-Type"), "mobile") {
		// 移动端：签发双 Token
		accessToken, err := h.Svc.IssueAccessToken(u)
		if err != nil {
			c.JSON(500, gin.H{"error": "签发访问令牌失败"})
			return
		}
		refreshToken, err := h.Svc.IssueRefreshToken(u)
		if err != nil {
			c.JSON(500, gin.H{"error": "签发刷新令牌失败"})
			return
		}

		resp := gin.H{
			"user": gin.H{
				"id":            u.ID,
				"name":          u.Name,
				"email":         u.Email,
				"emailVerified": u.EmailVerified,
				"status":        u.Status,
				"avatar":        u.Avatar,
				"displayName":   u.DisplayName,
			},
			"isNewUser":        isNewUser,
			"needSetPassword":  needSetPassword,
			"accessToken":      accessToken,
			"refreshToken":     refreshToken,
			"expiresIn":        h.Cfg.JWTAccessMinutes * 60,
			"refreshExpiresIn": h.Cfg.JWTRefreshDays * 24 * 60 * 60,
		}
		if result.DeviceID != "" {
			resp["deviceId"] = result.DeviceID
		}
		if result.DeviceToken != "" {
			resp["deviceToken"] = result.DeviceToken
		}
		c.JSON(200, resp)
		return
	}

	// Web端：Session Cookie
	deviceFingerprint := getDeviceFingerprint(c)
	session, err := h.Svc.CreateSession(u.ID, c.ClientIP(), c.Request.UserAgent(), deviceFingerprint)
	if err != nil {
		c.JSON(500, gin.H{"error": "创建会话失败"})
		return
	}
	setSecureSessionCookie(c, session.SessionToken, h.Cfg.SessionCookieName)

	resp := gin.H{
		"user": gin.H{
			"id":            u.ID,
			"name":          u.Name,
			"email":         u.Email,
			"emailVerified": u.EmailVerified,
			"status":        u.Status,
			"avatar":        u.Avatar,
			"displayName":   u.DisplayName,
		},
		"isNewUser":       isNewUser,
		"needSetPassword": needSetPassword,
	}
	if result.DeviceID != "" {
		resp["deviceId"] = result.DeviceID
	}
	if result.DeviceToken != "" {
		resp["deviceToken"] = result.DeviceToken
	}
	c.JSON(200, resp)
}
