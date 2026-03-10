package handlers

import (
	"net/http"
	"strings"

	"bubble/src/middleware"
	"bubble/src/service"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// ==================== V2 Authentication ====================
//
// V2 登录接口通过设备管理减少验证码使用频率：
//
//   POST /api/v2/login         — 统一登录入口（智能设备感知）
//   POST /api/v2/login/verify  — 新设备验证码确认
//   GET  /api/v2/devices       — 列出可信设备（需认证）
//   POST /api/v2/devices/trust — 信任新设备（需认证）
//   POST /api/v2/devices/revoke — 撤销设备（需认证）
//   POST /api/v2/devices/delete — 删除设备（需认证）

// loginV2 v2 统一登录入口
//
// @Summary      V2 Login (device-aware)
// @Description  智能设备感知登录：已信任设备跳过验证码，支持设备令牌免密登录
// @Tags         auth-v2
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "登录请求"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Failure      403   {object}  map[string]string
// @Failure      429   {object}  map[string]string
// @Router       /api/v2/login [post]
//
// 请求体:
//
//	{
//	  "email":       "user@example.com",        // 必填
//	  "password":    "...",                      // 新设备/已信任设备时必填，设备令牌登录时可省略
//	  "deviceId":    "client-generated-uuid",    // 可选，客户端设备标识（≥32字符高熵随机值）
//	  "deviceToken": "...",                      // 可选，上次登录返回的设备令牌（免密登录）
//	  "deviceName":  "iPhone 15 Pro",            // 可选，设备名称
//	  "platform":    "ios",                      // 可选: web/ios/android/desktop
//	  "trustDevice": true                        // 可选，是否信任此设备（省略后续验证码）
//	}
//
// 响应:
//
//	情况1 - 直接登录成功:
//	  { "user": {...}, "accessToken": "...", "refreshToken": "...", "deviceId": "...", "deviceToken": "..." }
//
//	情况2 - 需要验证码:
//	  { "requireVerification": true, "expiresIn": 300, "message": "..." }
func (h *HTTP) loginV2(c *gin.Context) {
	var body struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DeviceID    string `json:"deviceId"`
		DeviceToken string `json:"deviceToken"`
		DeviceName  string `json:"deviceName"`
		Platform    string `json:"platform"`
		TrustDevice bool   `json:"trustDevice"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}

	result, err := h.Svc.LoginV2(
		body.Email, body.Password,
		body.DeviceID, body.DeviceToken,
		body.DeviceName, body.Platform,
		c.ClientIP(), c.Request.UserAgent(),
	)
	if err != nil {
		handleAuthError(c, err)
		return
	}

	// 需要验证码
	if result.RequireVerification {
		resp := gin.H{
			"requireVerification": true,
			"expiresIn":           result.VerificationExpireIn,
			"message":             "登录验证码已发送到您的邮箱，请查收",
		}
		// debug 模式下方便调试
		if h.Cfg != nil && strings.EqualFold(h.Cfg.GinMode, "debug") {
			resp["_hint"] = "验证码已发送到邮箱，使用 POST /api/v2/login/verify 完成登录"
		}
		c.JSON(200, resp)
		return
	}

	// 邮箱验证检查
	u := result.User
	if h.Cfg != nil && h.Cfg.RequireEmailVerified && !u.EmailVerified {
		c.JSON(http.StatusForbidden, gin.H{"error": "邮箱未验证，请检查邮箱"})
		return
	}

	// 登录成功 → 分流 Web / Mobile
	h.respondLoginSuccessV2(c, result)
}

// verifyLoginV2 v2 验证码确认登录（新设备第二步）
//
// @Summary      V2 Verify login code
// @Description  新设备验证码确认登录，可选信任设备
// @Tags         auth-v2
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "验证请求"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Router       /api/v2/login/verify [post]
//
// 请求体:
//
//	{
//	  "email":       "user@example.com",
//	  "code":        "123456",
//	  "deviceId":    "...",       // 可选
//	  "deviceName":  "...",       // 可选
//	  "platform":    "ios",       // 可选
//	  "trustDevice": true         // 是否信任此设备
//	}
func (h *HTTP) verifyLoginV2(c *gin.Context) {
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

	result, err := h.Svc.VerifyLoginV2(
		body.Email, body.Code,
		body.DeviceID, body.DeviceName, body.Platform,
		c.ClientIP(), c.Request.UserAgent(),
		body.TrustDevice,
	)
	if err != nil {
		handleAuthError(c, err)
		return
	}

	// 邮箱验证检查
	u := result.User
	if h.Cfg != nil && h.Cfg.RequireEmailVerified && !u.EmailVerified {
		c.JSON(http.StatusForbidden, gin.H{"error": "邮箱未验证，请检查邮箱"})
		return
	}

	h.respondLoginSuccessV2(c, result)
}

// listDevicesV2 列出当前用户的可信设备
//
// @Summary      V2 List trusted devices
// @Tags         auth-v2
// @Produce      json
// @Success      200 {array} models.TrustedDevice
// @Router       /api/v2/devices [get]
func (h *HTTP) listDevicesV2(c *gin.Context) {
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
	c.JSON(200, gin.H{"devices": list})
}

// trustDeviceV2 为当前已登录用户信任一个新设备
//
// @Summary      V2 Trust a new device
// @Tags         auth-v2
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "{deviceName, platform}"
// @Success      200   {object}  map[string]any
// @Router       /api/v2/devices/trust [post]
func (h *HTTP) trustDeviceV2(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(401, gin.H{"error": "未认证"})
		return
	}
	var body struct {
		DeviceName string `json:"deviceName"`
		Platform   string `json:"platform"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}

	deviceName := strings.TrimSpace(body.DeviceName)
	if deviceName == "" {
		deviceName = "未命名设备"
	}
	platform := strings.TrimSpace(body.Platform)
	if platform == "" {
		platform = "unknown"
	}

	// 生成 deviceID + deviceToken
	td, deviceToken, err := h.Svc.TrustDeviceV2(u.ID, deviceName, platform, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		c.JSON(500, gin.H{"error": "信任设备失败"})
		return
	}

	c.JSON(200, gin.H{
		"device":      td,
		"deviceId":    td.DeviceID,
		"deviceToken": deviceToken,
	})
}

// revokeDeviceV2 撤销可信设备
//
// @Summary      V2 Revoke a trusted device
// @Tags         auth-v2
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "{deviceId}"
// @Success      204   {string}  string ""
// @Router       /api/v2/devices/revoke [post]
func (h *HTTP) revokeDeviceV2(c *gin.Context) {
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

// deleteDeviceV2 永久删除可信设备
//
// @Summary      V2 Delete a trusted device
// @Tags         auth-v2
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "{deviceId}"
// @Success      204   {string}  string ""
// @Router       /api/v2/devices/delete [post]
func (h *HTTP) deleteDeviceV2(c *gin.Context) {
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
		if err != service.ErrNotFound {
			c.JSON(500, gin.H{"error": "删除设备失败"})
			return
		}
	}
	c.Status(204)
}

// ──────────────────────────────────────────────
// 内部方法
// ──────────────────────────────────────────────

// respondLoginSuccessV2 统一响应登录成功
func (h *HTTP) respondLoginSuccessV2(c *gin.Context, result *service.LoginV2Result) {
	u := result.User

	if strings.EqualFold(c.GetHeader("X-Client-Type"), "mobile") {
		// 移动端：签发双 Token
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建会话失败"})
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
	}
	if result.DeviceID != "" {
		resp["deviceId"] = result.DeviceID
	}
	if result.DeviceToken != "" {
		resp["deviceToken"] = result.DeviceToken
	}
	c.JSON(200, resp)
}

// handleAuthError 统一错误响应
func handleAuthError(c *gin.Context, err error) {
	if svcErr, ok := err.(*service.Err); ok {
		c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
		return
	}
	if err == service.ErrBadRequest {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if err == service.ErrUnauthorized {
		c.JSON(401, gin.H{"error": "邮箱或密码错误"})
		return
	}
	if err == service.ErrTooManyRequests {
		c.JSON(429, gin.H{"error": "请求过于频繁"})
		return
	}
	logger.Errorf("[AuthV2] Unexpected error: %v", err)
	c.JSON(500, gin.H{"error": "服务器内部错误"})
}
