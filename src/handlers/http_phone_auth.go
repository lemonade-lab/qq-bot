package handlers

import (
	"net/http"
	"strings"

	"bubble/src/middleware"
	"bubble/src/service"

	"github.com/gin-gonic/gin"
)

// ==================== Phone Authentication (SMS) ====================

// @Summary      Send SMS verification code
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      map[string]string  true  "{phone}"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Failure      429   {object}  map[string]string
// @Router       /api/phone/send-code [post]
// phoneSendCode 发送手机短信验证码（登录即注册）
// Method/Path: POST /api/phone/send-code
// 请求体: {"phone": "手机号"}
// 响应: 200 {"sent": true, "expiresIn": 300}
// 错误: 400 手机号格式不正确 / 429 发送过于频繁
func (h *HTTP) phoneSendCode(c *gin.Context) {
	var body struct {
		Phone string `json:"phone"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误：需要手机号"})
		return
	}
	phone := strings.TrimSpace(body.Phone)
	if phone == "" {
		c.JSON(400, gin.H{"error": "请求体格式错误：需要手机号"})
		return
	}

	result, err := h.Svc.PhoneSendCode(phone, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
			return
		}
		c.JSON(400, gin.H{"error": "手机号格式不正确"})
		return
	}

	c.JSON(200, gin.H{
		"sent":      result.Sent,
		"expiresIn": result.ExpiresIn,
	})
}

// @Summary      Verify SMS code (login/register)
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      map[string]string  true  "{phone,code}"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Failure      403   {object}  map[string]string
// @Router       /api/phone/verify [post]
// phoneVerifyCode 验证手机短信验证码，完成登录/注册
// Method/Path: POST /api/phone/verify
// 请求体: {"phone": "手机号", "code": "验证码"}
// 响应:
//   - Web端: 200 用户对象 + Session Cookie
//   - 移动端: 200 用户对象 + Access/Refresh Token
//
// 错误: 400 参数错误 / 401 验证码错误或过期 / 403 账户锁定
func (h *HTTP) phoneVerifyCode(c *gin.Context) {
	var body struct {
		Phone string `json:"phone"`
		Code  string `json:"code"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误：需要手机号和验证码"})
		return
	}
	phone := strings.TrimSpace(body.Phone)
	code := strings.TrimSpace(body.Code)
	if phone == "" || code == "" {
		c.JSON(400, gin.H{"error": "请求体格式错误：需要手机号和验证码"})
		return
	}

	u, err := h.Svc.PhoneVerifyCode(phone, code, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
			return
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "验证码错误或已过期"})
		return
	}

	// Web / Mobile 分流
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
			"phone":            u.Phone,
			"phoneVerified":    u.PhoneVerified,
			"email":            u.Email,
			"accessToken":      accessToken,
			"refreshToken":     refreshToken,
			"expiresIn":        h.Cfg.JWTAccessMinutes * 60,
			"refreshExpiresIn": h.Cfg.JWTRefreshDays * 24 * 60 * 60,
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
		"phone":         u.Phone,
		"phoneVerified": u.PhoneVerified,
		"email":         u.Email,
		"status":        u.Status,
		"createdAt":     u.CreatedAt,
	})
}

// @Summary      Bind phone number to existing account
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      map[string]string  true  "{phone}"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Failure      429   {object}  map[string]string
// @Router       /api/phone/bind/send-code [post]
// phoneBindSendCode 绑定手机号 - 步骤1：发送验证码到新手机号（需要已登录）
func (h *HTTP) phoneBindSendCode(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	uid := u.ID

	var body struct {
		Phone string `json:"phone"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误：需要手机号"})
		return
	}
	phone := strings.TrimSpace(body.Phone)
	if phone == "" {
		c.JSON(400, gin.H{"error": "请求体格式错误：需要手机号"})
		return
	}

	result, err := h.Svc.PhoneBindSendCode(uid, phone, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
			return
		}
		c.JSON(400, gin.H{"error": "手机号格式不正确"})
		return
	}

	c.JSON(200, gin.H{
		"sent":      result.Sent,
		"expiresIn": result.ExpiresIn,
	})
}

// @Summary      Verify phone bind code
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      map[string]string  true  "{phone,code}"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Router       /api/phone/bind/verify [post]
// phoneBindVerify 绑定手机号 - 步骤2：验证码确认，完成绑定（需要已登录）
func (h *HTTP) phoneBindVerify(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	uid := u.ID

	var body struct {
		Phone string `json:"phone"`
		Code  string `json:"code"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误：需要手机号和验证码"})
		return
	}
	phone := strings.TrimSpace(body.Phone)
	code := strings.TrimSpace(body.Code)
	if phone == "" || code == "" {
		c.JSON(400, gin.H{"error": "请求体格式错误：需要手机号和验证码"})
		return
	}

	if err := h.Svc.PhoneBindVerify(uid, phone, code, c.ClientIP(), c.Request.UserAgent()); err != nil {
		if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
			return
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "验证码错误或已过期"})
		return
	}

	c.JSON(200, gin.H{"bound": true, "phone": phone})
}
