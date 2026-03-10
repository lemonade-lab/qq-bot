package handlers

import (
	"net/http"
	"strings"

	"bubble/src/config"
	"bubble/src/middleware"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// @Summary      Me
// @Tags         me
// @Security     BearerAuth
// @Produce      json
// @Success      200   {object}  map[string]any
// @Router       /api/me [get]
// me 返回当前认证用户的用户对象（包含 id, name, status, emailVerified 等）。
// Method/Path: GET /api/me
// 认证: Bearer Token。
// 响应: 200 用户对象。
// 错误: 401 未认证。
// 用途: 客户端启动后获取自身资料或刷新状态。
func (h *HTTP) me(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	// 获取头像URL
	var avatarURL string
	if u.Avatar != "" && h.Svc.MinIO != nil {
		avatarURL = h.Svc.MinIO.GetAvatarURL(u.Avatar)
	}
	// 获取横幅URL
	var bannerURL string
	if u.Banner != "" && h.Svc.MinIO != nil {
		bannerURL = h.Svc.MinIO.GetAvatarURL(u.Banner) // 复用GetAvatarURL，因为都是同一个bucket
	}
	c.JSON(200, gin.H{
		"id":                    u.ID,
		"name":                  u.Name,
		"displayName":           u.DisplayName,
		"email":                 u.Email,
		"emailVerified":         u.EmailVerified,
		"status":                u.Status,
		"customStatus":          u.CustomStatus,
		"isPrivate":             u.IsPrivate,
		"avatar":                avatarURL,
		"banner":                bannerURL,
		"bannerColor":           u.BannerColor,
		"bio":                   u.Bio,
		"gender":                u.Gender,
		"birthday":              u.Birthday,
		"pronouns":              u.Pronouns,
		"region":                u.Region,
		"phone":                 u.Phone,
		"link":                  u.Link,
		"requireFriendApproval": u.RequireFriendApproval,
		"friendRequestMode":     u.FriendRequestMode,
		"friendVerifyQuestion":  u.FriendVerifyQuestion,
		"dmPrivacyMode":         u.DmPrivacyMode,
		"createdAt":             u.CreatedAt,
	})
}

// @Summary      Logout
// @Tags         me
// @Security     BearerAuth
// @Produce      json
// @Success      204   {string}  string  ""
// @Router       /api/logout [post]
// logout 撤销Session并立即使所有请求失效。
// Method/Path: POST /api/logout
// 认证: Session Cookie (必需)
// 响应: 204 无内容。
// 错误: 401 未认证。
// 重要: Session撤销后立即生效,所有后续请求都会被拒绝(不再有延迟)
func (h *HTTP) logout(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	// 1. 撤销 Redis Session (立即生效 - 下一个请求就会被拒绝)
	if sessionToken, err := c.Cookie(h.Cfg.SessionCookieName); err == nil && sessionToken != "" {
		_ = h.Svc.RevokeSession(sessionToken)
	}

	// 2. 设置用户状态为 offline
	_ = h.Svc.SetStatus(u, "offline")

	// 3. 清除双 Cookie
	clearTokenCookie(c)
	clearSessionCookie(c, h.Cfg.SessionCookieName)

	c.Status(204)
}

// @Summary      Update profile
// @Tags         me
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body      map[string]string  true  "{name}"
// @Success      204   {string}  string  ""
// @Router       /api/me/profile [put]
// updateProfile 更新用户昵称等基本资料。
// Method/Path: PUT /api/me/profile
// 认证: Bearer Token。
// 请求体: {"name": "新昵称"}
// 响应: 204 无内容。
// 错误: 401 未认证；400 名称非法或服务层错误。
// 注意: 若未来加入更多字段（avatar, bio），应同步更新注释与校验。
func (h *HTTP) updateProfile(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误：缺少name"})
		return
	}
	// 校验用户名长度
	name := strings.TrimSpace(body.Name)
	if len([]rune(name)) == 0 || len([]rune(name)) > int(config.MaxUsernameLength) {
		c.JSON(400, gin.H{"error": "昵称长度不合法"})
		return
	}
	if err := h.Svc.UpdateProfile(u, body.Name); err != nil {
		logger.Errorf("[Me] Failed to update profile for user %d: %v", u.ID, err)
		c.JSON(400, gin.H{"error": "更新资料失败"})
		return
	}
	c.Status(204)
}

// @Summary      Update user profile fields (banner, bio, etc)
// @Tags         me
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body      map[string]string  true  "{banner, bannerColor, bio}"
// @Success      204   {string}  string  ""
// @Router       /api/me/profile/extended [put]
func (h *HTTP) updateExtendedProfile(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	var body map[string]any
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if bioRaw, ok := body["bio"].(string); ok {
		if len([]rune(bioRaw)) > int(config.MaxUserBioLength) {
			c.JSON(400, gin.H{"error": "个人简介过长"})
			return
		}
	}
	if err := h.Svc.UpdateUserProfile(u, body); err != nil {
		logger.Errorf("[Me] Failed to update extended profile for user %d: %v", u.ID, err)
		c.JSON(400, gin.H{"error": "更新资料失败"})
		return
	}
	c.Status(204)
}

// @Summary      Update avatar
// @Tags         me
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body      map[string]string  true  "{path}"
// @Success      200   {object}  map[string]string
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Router       /api/me/avatar [put]
// updateAvatar 更新用户头像（绑定已上传的文件路径）。
// Method/Path: PUT /api/me/avatar
// 认证: Bearer Token。
// 请求体: {"path": "avatars/1/1704067200.jpg"} (通过 /api/files/upload 获取)
// 响应: 200 返回结构化的头像信息
//
//	{
//	  "data": {
//	    "avatar": {
//	      "path": "avatars/1/1704067200.jpg",
//	      "url": "avatars/avatars/1/1704067200.jpg"
//	    }
//	  }
//	}
//
// 错误: 401 未认证；400 路径无效或服务层错误。
func (h *HTTP) updateAvatar(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	var body struct {
		Path string `json:"path"` // MinIO object name
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误：缺少path"})
		return
	}

	if body.Path == "" {
		c.JSON(400, gin.H{"error": "路径不能为空"})
		return
	}

	// 验证路径格式: {bucket}/{category}/{userID}/{timestamp}.{ext}
	// 对于头像，必须使用 avatars 存储桶
	pathParts := strings.Split(body.Path, "/")
	if len(pathParts) < 3 {
		c.JSON(400, gin.H{"error": "路径格式错误，期望：{存储桶}/{分类}/{用户ID}/..."})
		return
	}

	// 检查第一个部分是否是有效的存储桶
	validBuckets := []string{"avatars", "covers", "emojis", "guild-chat-files", "private-chat-files", "temp", "bubble"}
	bucketName := pathParts[0]
	isValidBucket := false
	for _, b := range validBuckets {
		if b == bucketName {
			isValidBucket = true
			break
		}
	}

	if !isValidBucket {
		c.JSON(400, gin.H{"error": "路径中的存储桶无效，可选：" + strings.Join(validBuckets, ", ")})
		return
	}

	// 对于头像更新，必须使用 avatars 存储桶
	if bucketName != "avatars" {
		c.JSON(400, gin.H{"error": "头像必须在avatars存储桶中"})
		return
	}

	// 验证分类部分（第二个部分）应该是 avatars
	if len(pathParts) >= 2 && pathParts[1] != "avatars" {
		c.JSON(400, gin.H{"error": "头像path的category必须为avatars"})
		return
	}

	// 更新用户头像
	if err := h.Svc.UpdateAvatar(u, body.Path); err != nil {
		logger.Errorf("[Me] Failed to update avatar for user %d: %v", u.ID, err)
		c.JSON(400, gin.H{"error": "更新头像失败"})
		return
	}

	// 返回结构化的头像信息
	var avatarURL string
	if h.Svc.MinIO != nil {
		avatarURL = h.Svc.MinIO.GetAvatarURL(body.Path)
	}
	c.JSON(200, gin.H{
		"data": gin.H{
			"avatar": gin.H{
				"path": body.Path,
				"url":  avatarURL,
			},
		},
	})
}

// @Summary      Change password
// @Description  修改当前用户密码
// @Tags         me
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body      map[string]string  true  "{old,new}"
// @Success      204   {string}  string  ""
// @Failure      400   {object}  map[string]string  "旧密码不匹配或新密码不符合策略"
// @Failure      401   {object}  map[string]string  "未认证"
// @Router       /api/me/password [put]
func (h *HTTP) changePassword(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	var body struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误：需要old和new"})
		return
	}
	if err := h.Svc.ChangePassword(u, body.Old, body.New); err != nil {
		logger.Errorf("[Me] Failed to change password for user %d: %v", u.ID, err)
		c.JSON(400, gin.H{"error": "修改密码失败"})
		return
	}
	c.Status(204)
}

// @Summary      Update status
// @Description  更新在线状态（如 online/offline/busy/away）
// @Tags         me
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body      map[string]string  true  "{status}"
// @Success      204   {string}  string  ""
// @Failure      400   {object}  map[string]string  "状态非法"
// @Failure      401   {object}  map[string]string  "未认证"
// @Router       /api/me/status [put]
func (h *HTTP) updateStatus(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if err := h.Svc.SetStatus(u, body.Status); err != nil {
		logger.Errorf("[Me] Failed to update status for user %d: %v", u.ID, err)
		c.JSON(400, gin.H{"error": "更新状态失败"})
		return
	}
	c.Status(204)
}

// @Summary      Update user settings
// @Description  更新用户个人设置（性别、地区、手机号、好友验证、头像、横幅、个人简介、生日、称谓代词、自定义状态、展示名称、个人链接等）
// @Tags         me
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body      map[string]any  true  "{gender?, region?, phone?, requireFriendApproval?, avatar?, banner?, bannerColor?, bio?, birthday?, pronouns?, customStatus?, displayName?, link?}"
// @Success      204   {string}  string  ""
// @Failure      400   {object}  map[string]string  "参数错误"
// @Failure      401   {object}  map[string]string  "未认证"
// @Router       /api/me/settings [put]
func (h *HTTP) updateUserSettings(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	var body struct {
		Gender                *string `json:"gender,omitempty"`
		Region                *string `json:"region,omitempty"`
		Phone                 *string `json:"phone,omitempty"`
		RequireFriendApproval *bool   `json:"requireFriendApproval,omitempty"`
		Avatar                *string `json:"avatar,omitempty"`
		Banner                *string `json:"banner,omitempty"`
		BannerColor           *string `json:"bannerColor,omitempty"`
		Bio                   *string `json:"bio,omitempty"`
		Birthday              *string `json:"birthday,omitempty"`
		Pronouns              *string `json:"pronouns,omitempty"`
		CustomStatus          *string `json:"customStatus,omitempty"`
		DisplayName           *string `json:"displayName,omitempty"`
		Link                  *string `json:"link,omitempty"`
		DmPrivacyMode         *string `json:"dmPrivacyMode,omitempty"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if err := h.Svc.UpdateUserSettings(
		u.ID,
		body.Gender,
		body.Region,
		body.Phone,
		body.RequireFriendApproval,
		body.Avatar,
		body.Banner,
		body.BannerColor,
		body.Bio,
		body.Birthday,
		body.Pronouns,
		body.CustomStatus,
		body.DisplayName,
		body.Link,
	); err != nil {
		logger.Errorf("[Me] Failed to update user settings for user %d: %v", u.ID, err)
		c.JSON(400, gin.H{"error": "更新设置失败"})
		return
	}
	// 私聊隐私模式单独处理
	if body.DmPrivacyMode != nil {
		mode := *body.DmPrivacyMode
		if mode == "friends_only" || mode == "everyone" {
			if err := h.Svc.SetDmPrivacyMode(u.ID, mode); err != nil {
				logger.Errorf("[Me] Failed to update DM privacy mode for user %d: %v", u.ID, err)
				c.JSON(400, gin.H{"error": "更新私聊设置失败"})
				return
			}
		}
	}
	c.Status(204)
}

// @Summary      Set privacy setting
// @Description  设置账号为私密或公开
// @Tags         me
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body      map[string]bool  true  "{isPrivate}"
// @Success      204   {string}  string  ""
// @Failure      400   {object}  map[string]string  "参数错误"
// @Failure      401   {object}  map[string]string  "未认证"
// @Router       /api/me/privacy [put]
func (h *HTTP) setPrivacy(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	var body struct {
		IsPrivate bool `json:"isPrivate"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if err := h.Svc.SetPrivateAccount(u.ID, body.IsPrivate); err != nil {
		logger.Errorf("[Me] Failed to set privacy for user %d: %v", u.ID, err)
		c.JSON(400, gin.H{"error": "设置隐私失败"})
		return
	}
	c.Status(204)
}

// @Summary      Validity check
// @Description  信息有效性检查（无任何意义）
// @Tags         me
// @Accept       json
// @Produce      json
// @Success      200  {object}  map[string]any
// @Router       /api/validity [get]
func (h *HTTP) Validity(c *gin.Context) {
	c.JSON(200, gin.H{
		"data": nil,
	})
}
