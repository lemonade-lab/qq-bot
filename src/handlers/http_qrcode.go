package handlers

import (
	"bytes"
	"image/png"
	"net/http"
	"strings"

	"bubble/src/middleware"
	"bubble/src/service"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/qr"
	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// ==================== QR Code Login ====================

// @Summary      生成二维码（统一接口）
// @Tags         qrcode
// @Accept       json
// @Produce      json
// @Param        body  body      object  false  "二维码参数"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Failure      500   {object}  map[string]string
// @Router       /api/qrcode/generate [post]
// generateQRCode 生成二维码（统一接口，支持多种类型）
// Method/Path: POST /api/qrcode/generate
// 请求头: X-Client-Type: mobile (桌面端) 或不设置 (Web端) - 仅用于登录类型
//
//	请求体: {
//	  "type": "login|user|channel|guild",  // 可选，默认 "login"
//	  "targetId": 123                       // type非login时必填
//	}
//
// 响应: 200 返回二维码数据 {code, type, expiresAt, expiresIn}
// 错误: 400 参数错误; 500 生成失败
func (h *HTTP) generateQRCode(c *gin.Context) {
	var req struct {
		Type     string `json:"type"`     // 二维码类型：login, user, channel, guild
		TargetID uint   `json:"targetId"` // 目标ID（用户ID、频道ID、服务器ID）
	}

	// 尝试解析请求体，如果为空或解析失败，使用默认值
	// 使用 ShouldBindJSON 而不是 BindJSON，避免自动设置 400 状态码
	_ = c.ShouldBindJSON(&req)

	// 默认为登录类型
	if req.Type == "" {
		req.Type = "login"
	}

	// 获取客户端类型（仅用于登录类型）
	clientType := "web"
	if strings.EqualFold(c.GetHeader("X-Client-Type"), "mobile") {
		clientType = "mobile"
	}

	var data *service.QRCodeLoginData
	var err error
	var payload map[string]any

	// 根据类型生成不同的二维码
	switch req.Type {
	case "login":
		data, err = h.Svc.CreateQRCodeLogin(c.ClientIP(), c.Request.UserAgent(), clientType)

	case "user":
		if req.TargetID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "targetId 不能为空"})
			return
		}
		payload = map[string]any{"userId": req.TargetID}
		data, err = h.Svc.CreateQRCode("user", payload, c.ClientIP(), c.Request.UserAgent(), "")

	case "channel":
		if req.TargetID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "targetId 不能为空"})
			return
		}
		payload = map[string]any{"channelId": req.TargetID}
		data, err = h.Svc.CreateQRCode("channel", payload, c.ClientIP(), c.Request.UserAgent(), "")

	case "guild":
		if req.TargetID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "targetId 不能为空"})
			return
		}
		payload = map[string]any{"guildId": req.TargetID}
		data, err = h.Svc.CreateQRCode("guild", payload, c.ClientIP(), c.Request.UserAgent(), "")

	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "不支持的二维码类型: " + req.Type})
		return
	}

	if err != nil {
		logger.Errorf("[QRCode] Failed to generate QR code: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成二维码失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":      data.Code,
		"type":      data.Type,
		"expiresAt": data.ExpiresAt,
		"expiresIn": int(data.ExpiresAt.Sub(data.CreatedAt).Seconds()),
	})
}

// @Summary      获取二维码卡片信息
// @Tags         qrcode
// @Accept       json
// @Produce      json
// @Param        code  path      string  true  "二维码"
// @Success      200   {object}  map[string]any
// @Failure      404   {object}  map[string]string
// @Router       /api/qrcode/{code}/card [get]
// getQRCodeCard 获取二维码卡片信息（移动端扫码后调用）
// Method/Path: GET /api/qrcode/:code/card
// 响应: 200 返回卡片数据 {code, type, title, description, user/channel/guild}
// 错误: 404 二维码不存在或已过期
func (h *HTTP) getQRCodeCard(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少二维码参数"})
		return
	}

	cardData, err := h.Svc.GetQRCodeCardData(code)
	if err != nil {
		logger.Errorf("[QRCode] Failed to get QR code card data for code %s: %v", code, err)
		c.JSON(http.StatusNotFound, gin.H{"error": "二维码不存在或已过期"})
		return
	}

	c.JSON(http.StatusOK, cardData)
}

// @Summary      查询二维码状态
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        code  path      string  true  "二维码"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Failure      404   {object}  map[string]string
// @Router       /api/qrcode/{code}/status [get]
// getQRCodeStatus 查询二维码状态（用于轮询）
// Method/Path: GET /api/qrcode/:code/status
// 响应: 200 返回二维码状态 {code, state, userId}
// 错误: 404 二维码不存在或已过期
func (h *HTTP) getQRCodeStatus(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少二维码参数"})
		return
	}

	data, err := h.Svc.GetQRCodeLogin(code)
	if err != nil {
		logger.Errorf("[QRCode] Failed to get QR code status for code %s: %v", code, err)
		c.JSON(http.StatusNotFound, gin.H{"error": "二维码不存在或已过期"})
		return
	}

	response := gin.H{
		"code":  data.Code,
		"state": data.State,
	}

	// 如果已确认，返回用户ID
	if data.State == "confirmed" {
		response["userId"] = data.UserID
	}

	c.JSON(http.StatusOK, response)
}

// @Summary      扫描二维码
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        code  path      string  true  "二维码"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Failure      404   {object}  map[string]string
// @Router       /api/qrcode/{code}/scan [post]
// scanQRCode 扫描二维码（移动端已登录用户调用）
// Method/Path: POST /api/qrcode/:code/scan
// 响应: 200 扫描成功
// 错误: 401 未登录; 404 二维码不存在或已过期; 400 状态不正确
func (h *HTTP) scanQRCode(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少二维码参数"})
		return
	}

	// 从上下文获取已认证的用户
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	err := h.Svc.ScanQRCodeLogin(code, u.ID)
	if err != nil {
		logger.Errorf("[QRCode] Failed to scan QR code %s: %v", code, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "扫码失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "扫描成功",
		"state":   "scanned",
	})
}

// @Summary      确认二维码登录
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        code  path      string  true  "二维码"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Failure      404   {object}  map[string]string
// @Router       /api/qrcode/{code}/confirm [post]
// confirmQRCode 确认二维码登录（移动端已登录用户调用）
// Method/Path: POST /api/qrcode/:code/confirm
// 响应: 200 确认成功，返回用户信息
// 错误: 401 未登录; 404 二维码不存在或已过期; 400 状态不正确
func (h *HTTP) confirmQRCode(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少二维码参数"})
		return
	}

	// 解析请求体中的 trustDevice 选项
	var body struct {
		TrustDevice bool `json:"trustDevice"`
	}
	_ = c.ShouldBindJSON(&body) // 可选字段，解析失败不阻断

	// 从上下文获取已认证的用户
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	user, err := h.Svc.ConfirmQRCodeLogin(code, u.ID, body.TrustDevice)
	if err != nil {
		logger.Errorf("[QRCode] Failed to confirm QR code %s: %v", code, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "确认失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "确认成功",
		"state":   "confirmed",
		"user": gin.H{
			"id":    user.ID,
			"name":  user.Name,
			"email": user.Email,
		},
	})
}

// @Summary      取消二维码登录
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        code  path      string  true  "二维码"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Failure      404   {object}  map[string]string
// @Router       /api/qrcode/{code}/cancel [post]
// cancelQRCode 取消二维码登录（移动端或 PC 端调用）
// Method/Path: POST /api/qrcode/:code/cancel
// 响应: 200 取消成功
// 错误: 404 二维码不存在或已过期
func (h *HTTP) cancelQRCode(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少二维码参数"})
		return
	}

	err := h.Svc.CancelQRCodeLogin(code)
	if err != nil {
		logger.Errorf("[QRCode] Failed to cancel QR code %s: %v", code, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "取消失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "取消成功",
		"state":   "cancelled",
	})
}

// @Summary      二维码登录完成后获取会话
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      map[string]string  true  "{code}"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Failure      404   {object}  map[string]string
// @Router       /api/qrcode/login [post]
// completeQRCodeLogin 二维码登录完成，PC 端获取会话 token
// Method/Path: POST /api/qrcode/login
// 请求体: {"code": "二维码"}
// 响应: 200 返回用户信息和 token（通过 Cookie 或返回体）
// 错误: 400 请求体错误或二维码状态不正确; 404 二维码不存在
func (h *HTTP) completeQRCodeLogin(c *gin.Context) {
	var body struct {
		Code       string `json:"code"`
		DeviceID   string `json:"deviceId"`
		DeviceName string `json:"deviceName"`
		Platform   string `json:"platform"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误：需要二维码"})
		return
	}

	data, err := h.Svc.GetQRCodeLogin(body.Code)
	if err != nil {
		logger.Errorf("[QRCode] Failed to get QR code login for code %s: %v", body.Code, err)
		c.JSON(http.StatusNotFound, gin.H{"error": "二维码不存在"})
		return
	}

	if data.State != "confirmed" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "二维码未确认"})
		return
	}

	// 获取用户信息
	user, err := h.Svc.Repo.GetUserByID(data.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取用户信息失败"})
		return
	}

	// 根据生成二维码时记录的客户端类型返回对应的认证凭据
	if data.ClientType == "mobile" {
		// 移动端：签发双 Token (Access + Refresh)
		accessToken, err := h.Svc.IssueAccessToken(user)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "签发访问令牌失败"})
			return
		}

		refreshToken, err := h.Svc.IssueRefreshToken(user)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "签发刷新令牌失败"})
			return
		}

		resp := gin.H{
			"id":               user.ID,
			"name":             user.Name,
			"email":            user.Email,
			"avatar":           user.Avatar,
			"accessToken":      accessToken,
			"refreshToken":     refreshToken,
			"expiresIn":        h.Cfg.JWTAccessMinutes * 60,
			"refreshExpiresIn": h.Cfg.JWTRefreshDays * 24 * 60 * 60,
		}

		// 设备信任：扫码端确认时选择了信任设备，且登录端传了 deviceId
		if data.TrustDevice && body.DeviceID != "" {
			loginResult, err := h.Svc.CompleteLoginWithDeviceTrust(
				user, body.DeviceID, body.DeviceName, body.Platform,
				c.ClientIP(), c.Request.UserAgent(),
			)
			if err == nil && loginResult != nil {
				if loginResult.DeviceID != "" {
					resp["deviceId"] = loginResult.DeviceID
				}
				if loginResult.DeviceToken != "" {
					resp["deviceToken"] = loginResult.DeviceToken
				}
			}
		}

		c.JSON(http.StatusOK, resp)
		return
	}

	// Web端：创建 Session Cookie
	deviceFingerprint := getDeviceFingerprint(c)
	session, err := h.Svc.CreateSession(user.ID, c.ClientIP(), c.Request.UserAgent(), deviceFingerprint)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建会话失败"})
		return
	}

	setSecureSessionCookie(c, session.SessionToken, h.Cfg.SessionCookieName)

	c.JSON(http.StatusOK, gin.H{
		"id":            user.ID,
		"name":          user.Name,
		"email":         user.Email,
		"emailVerified": user.EmailVerified,
		"status":        user.Status,
		"createdAt":     user.CreatedAt,
	})
}

// @Summary      生成二维码图片
// @Tags         auth
// @Accept       json
// @Produce      image/png
// @Param        code  query     string  true  "二维码"
// @Success      200   {file}    image/png
// @Failure      400   {object}  map[string]string
// @Router       /api/v1/create-qr-code [get]
// createQRCodeImage 生成二维码图片
// Method/Path: GET /api/v1/create-qr-code?code=xxx
// 响应: 200 返回 PNG 格式的二维码图片
// 错误: 400 缺少二维码参数或生成失败
func (h *HTTP) createQRCodeImage(c *gin.Context) {
	code := c.Query("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少二维码参数"})
		return
	}

	// 生成二维码
	qrCode, err := qr.Encode(code, qr.M, qr.Auto)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成二维码失败"})
		return
	}

	// 缩放到 240x240
	qrCode, err = barcode.Scale(qrCode, 240, 240)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "缩放二维码失败"})
		return
	}

	// 编码为 PNG
	var buf bytes.Buffer
	if err := png.Encode(&buf, qrCode); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "编码二维码失败"})
		return
	}

	// 返回图片
	c.Data(http.StatusOK, "image/png", buf.Bytes())
}
