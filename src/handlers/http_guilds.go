package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"bubble/src/config"
	"bubble/src/db/models"
	"bubble/src/middleware"
	"bubble/src/service"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// @Summary      Create guild
// @Tags         guilds
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body      map[string]any  true  "{name, description?, category?, isPrivate?, autoJoinMode?}"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Router       /api/guilds [post]
// @Description  创建服务器。支持传入更多可选信息：
//
//   - name (必须): 服务器名称
//   - description (可选): 服务器简介
//   - category (可选): 服务器分类 (gaming, work, dev, study, entertainment, other)，默认 other
//   - isPrivate (可选): 是否私密，默认 true
//   - autoJoinMode (可选): 加入模式 (require_approval, no_approval, no_approval_under_100)，默认 require_approval
//
// createGuild 创建一个新的服务器
func (h *HTTP) createGuild(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	var body struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		Category     string `json:"category"`
		IsPrivate    *bool  `json:"isPrivate"` // 使用指针以区分未传和传 false
		AutoJoinMode string `json:"autoJoinMode"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}

	// 验证必填字段
	if strings.TrimSpace(body.Name) == "" {
		c.JSON(400, gin.H{"error": "服务器名称不能为空"})
		return
	}

	g, err := h.Svc.CreateGuildWithOptions(u.ID, service.CreateGuildOptions{
		Name:         body.Name,
		Description:  body.Description,
		Category:     body.Category,
		IsPrivate:    body.IsPrivate,
		AutoJoinMode: body.AutoJoinMode,
	})
	if err != nil {
		logger.Errorf("[Guilds] Failed to create guild by user %d: %v", u.ID, err)
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, g)
}

// @Summary      Delete guild (soft delete)
// @Tags         guilds
// @Security     BearerAuth
// @Produce      json
// @Param        id   path  int  true  "Guild ID"
// @Success      204  {string}  string  "no content"
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /api/guilds/{id} [delete]
// deleteGuild 软删除服务器（仅 owner 可执行）
func (h *HTTP) deleteGuild(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if gid == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID"})
		return
	}
	if err := h.Svc.DeleteGuild(uint(gid), u.ID); err != nil {
		if err == service.ErrNotFound {
			// 服务器不存在也视为删除成功，避免重复点击报错
			c.Status(204)
			return
		} else if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "仅群主可删除"})
		} else {
			logger.Errorf("[Guilds] Failed to delete guild %d by user %d: %v", gid, u.ID, err)
			c.JSON(400, gin.H{"error": "删除服务器失败"})
		}
		return
	}
	c.Status(204)
}

// @Summary      Set guild privacy
// @Tags         guilds
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Guild ID"
// @Param        body body  map[string]bool true "{isPrivate}"
// @Success      204  {string}  string  "no content"
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /api/guilds/{id}/privacy [put]
// setGuildPrivacy 设置服务器为公开或私密（仅 owner 可执行）
func (h *HTTP) setGuildPrivacy(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if gid == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID"})
		return
	}
	var body struct {
		IsPrivate bool `json:"isPrivate"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if err := h.Svc.SetGuildPrivacy(uint(gid), u.ID, body.IsPrivate); err != nil {
		if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "服务器不存在"})
		} else if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "仅群主可修改隐私设置"})
		} else {
			logger.Errorf("[Guilds] Failed to set privacy for guild %d by user %d: %v", gid, u.ID, err)
			c.JSON(400, gin.H{"error": "设置隐私失败"})
		}
		return
	}
	c.Status(204)
}

// @Summary      Set guild auto-join mode
// @Tags         guilds
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Guild ID"
// @Param        body body  map[string]string true "{autoJoinMode}"
// @Success      204  {string}  string  "no content"
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /api/guilds/{id}/auto-join-mode [put]
// setGuildAutoJoinMode 设置服务器加入验证模式（仅owner可操作）
func (h *HTTP) setGuildAutoJoinMode(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if gid == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID"})
		return
	}
	var body struct {
		AutoJoinMode string `json:"autoJoinMode"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if err := h.Svc.SetGuildAutoJoinMode(uint(gid), u.ID, body.AutoJoinMode); err != nil {
		if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "服务器不存在"})
		} else if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "仅群主可修改"})
		} else if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
		} else {
			logger.Errorf("[Guilds] Failed to set auto join mode for guild %d by user %d: %v", gid, u.ID, err)
			c.JSON(400, gin.H{"error": "设置自动加入模式失败"})
		}
		return
	}
	c.Status(204)
}

// @Summary      Get guild auto-join mode
// @Description  获取服务器加入模式（任何用户可查看）
// @Tags         guilds
// @Security     BearerAuth
// @Produce      json
// @Param        id path int true "Guild ID"
// @Success      200  {object}  map[string]string  "{autoJoinMode}"
// @Failure      404  {object}  map[string]string
// @Router       /api/guilds/{id}/auto-join-mode [get]
func (h *HTTP) getGuildAutoJoinMode(c *gin.Context) {
	gid, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if gid == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID"})
		return
	}

	guild, err := h.Svc.Repo.GetGuild(uint(gid))
	if err != nil || guild == nil {
		c.JSON(404, gin.H{"error": "服务器不存在"})
		return
	}

	autoJoinMode := guild.AutoJoinMode
	if autoJoinMode == "" {
		autoJoinMode = "require_approval" // 默认值
	}

	c.JSON(200, gin.H{
		"autoJoinMode": autoJoinMode,
		"description":  getAutoJoinModeDescription(autoJoinMode),
	})
}

// getAutoJoinModeDescription 返回加入模式的描述
func getAutoJoinModeDescription(mode string) string {
	switch mode {
	case "no_approval":
		return "无需审核，直接加入"
	case "no_approval_under_100":
		return "成员数小于100时直接加入，否则需要审核"
	case "require_approval":
		return "需要审核后才能加入"
	default:
		return "需要审核后才能加入"
	}
}

// @Summary      Update guild info
// @Description  更新服务器信息（名称、简介、头像等，仅owner可修改）
// @Tags         guilds
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Guild ID"
// @Param        body body  map[string]string true "{name?, description?, avatar?}"
// @Success      200  {object}  map[string]any
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /api/guilds/{id} [patch]
// updateGuild 整合的更新服务器信息接口（仅 owner 可执行）
func (h *HTTP) updateGuild(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if gid == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID"})
		return
	}
	var body struct {
		Name              *string `json:"name,omitempty"`
		Description       *string `json:"description,omitempty"`
		Avatar            *string `json:"avatar,omitempty"`
		Banner            *string `json:"banner,omitempty"`
		AllowMemberUpload *bool   `json:"allowMemberUpload,omitempty"`
		ShowRoleNames     *bool   `json:"showRoleNames,omitempty"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if err := h.Svc.UpdateGuild(uint(gid), u.ID, body.Name, body.Description, body.Avatar, body.Banner, body.AllowMemberUpload, body.ShowRoleNames); err != nil {
		if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "服务器不存在"})
		} else if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "仅群主可修改服务器信息"})
		} else {
			logger.Errorf("[Guilds] Failed to update guild %d by user %d: %v", gid, u.ID, err)
			c.JSON(400, gin.H{"error": "更新服务器信息失败"})
		}
		return
	}

	// 返回更新后的信息
	response := gin.H{"success": true}
	data := gin.H{}

	if body.Avatar != nil && h.Svc.MinIO != nil {
		avatarURL := h.Svc.MinIO.GetFileURL(*body.Avatar)
		data["avatar"] = gin.H{
			"path": *body.Avatar,
			"url":  avatarURL,
		}
	}

	if body.Banner != nil && h.Svc.MinIO != nil {
		bannerURL := h.Svc.MinIO.GetFileURL(*body.Banner)
		data["banner"] = gin.H{
			"path": *body.Banner,
			"url":  bannerURL,
		}
	}

	if len(data) > 0 {
		response["data"] = data
	}

	c.JSON(200, response)
}

// @Summary      Update guild description
// @Tags         guilds
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Guild ID"
// @Param        body body  map[string]string true "{description}"
// @Success      204  {string}  string  "no content"
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /api/guilds/{id}/description [put]
// updateGuildDescription 更新服务器简介（仅 owner 可执行）
func (h *HTTP) updateGuildDescription(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if gid == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID"})
		return
	}
	var body struct {
		Description string `json:"description"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if len([]rune(body.Description)) > int(config.MaxGuildDescriptionLength) {
		c.JSON(400, gin.H{"error": "简介过长"})
		return
	}
	// 调用统一的 UpdateGuild 方法
	if err := h.Svc.UpdateGuild(uint(gid), u.ID, nil, &body.Description, nil, nil); err != nil {
		if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "服务器不存在"})
		} else if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "仅群主可修改简介"})
		} else {
			logger.Errorf("[Guilds] Failed to update description for guild %d by user %d: %v", gid, u.ID, err)
			c.JSON(400, gin.H{"error": "更新简介失败"})
		}
		return
	}
	c.Status(204)
}

// @Summary      Update guild avatar
// @Tags         guilds
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Guild ID"
// @Param        body body  map[string]string true "{path}"
// @Success      200  {object}  map[string]string
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /api/guilds/{id}/avatar [put]
// updateGuildAvatar 更新服务器头像（仅 owner 可执行）
func (h *HTTP) updateGuildAvatar(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if gid == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID"})
		return
	}
	var body struct {
		Path string `json:"path"` // MinIO object name
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误：缺少路径"})
		return
	}

	if body.Path == "" {
		c.JSON(400, gin.H{"error": "路径不能为空"})
		return
	}

	// 验证路径格式: {bucket}/{category}/{userID}/{timestamp}.{ext}
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

	// 对于服务器头像，必须使用 avatars 存储桶
	if bucketName != "avatars" {
		c.JSON(400, gin.H{"error": "服务器头像必须在avatars存储桶中"})
		return
	}

	// 验证分类部分（第二个部分）应该是 avatars
	if len(pathParts) >= 2 && pathParts[1] != "avatars" {
		c.JSON(400, gin.H{"error": "服务器头像路径的分类必须为'avatars'"})
		return
	}

	// 调用统一的 UpdateGuild 方法
	if err := h.Svc.UpdateGuild(uint(gid), u.ID, nil, nil, &body.Path, nil); err != nil {
		if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "服务器不存在"})
		} else if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "仅群主可修改头像"})
		} else {
			logger.Errorf("[Guilds] Failed to update avatar for guild %d by user %d: %v", gid, u.ID, err)
			c.JSON(400, gin.H{"error": "更新头像失败"})
		}
		return
	}

	// 返回结构化的头像信息
	var avatarURL string
	if h.Svc.MinIO != nil {
		avatarURL = h.Svc.MinIO.GetFileURL(body.Path)
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

// @Summary      Search guilds by name
// @Tags         guilds
// @Security     BearerAuth
// @Produce      json
// @Param        q      query string true  "搜索关键词 (>=1 字符)"
// @Param        limit  query int    false "Limit (默认20, 最大100)"
// @Success      200    {array} map[string]any
// @Failure      400    {object} map[string]string
// @Failure      500    {object} map[string]string
// @Router       /api/guilds/search [get]
// searchGuilds 服务器名称模糊搜索
func (h *HTTP) searchGuilds(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		c.JSON(400, gin.H{"error": "缺少搜索关键词"})
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	guilds, err := h.Svc.SearchGuilds(q, limit, "")
	if err != nil {
		if se, ok := err.(*service.Err); ok && se.Code == 400 {
			c.JSON(400, gin.H{"error": se.Msg})
			return
		}
		logger.Errorf("[Guilds] Failed to search guilds with query '%s': %v", q, err)
		c.JSON(500, gin.H{"error": "搜索服务器失败"})
		return
	}
	c.JSON(200, guilds)
}

// @Summary      List hot guilds (by member count)
// @Description  返回成员数量排名靠前的服务器列表
// @Tags         guilds
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        limit query int false "Limit (1-6, 默认6)"
// @Success      200  {array} map[string]any
// @Failure      500  {object} map[string]string  "服务器错误"
// @Router       /api/guilds/hot [get]
func (h *HTTP) hotGuilds(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	guilds, err := h.Svc.HotGuilds(limit)
	if err != nil {
		logger.Errorf("[Guilds] Failed to get hot guilds: %v", err)
		c.JSON(500, gin.H{"error": "获取热门服务器失败"})
		return
	}
	c.JSON(200, guilds)
}

// @Summary      List guilds (user's guilds only)
// @Description  返回当前用户加入的所有服务器列表，支持游标分页
// @Tags         guilds
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        limit    query  int  false  "每页数量 (默认10, 最大100)"
// @Param        beforeId query  int  false  "游标ID，返回ID小于此值的记录"
// @Param        afterId  query  int  false  "游标ID，返回ID大于此值的记录"
// @Param        page     query  int  false  "页码 (默认1, 当未使用游标时生效)"
// @Success      200   {object}  map[string]any
// @Failure      401   {object}  map[string]string  "未认证"
// @Failure      500   {object}  map[string]string  "服务器错误"
// @Router       /api/guilds [get]
func (h *HTTP) listGuilds(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit < 1 || limit > 100 {
		limit = 10
	}
	beforeID64, _ := strconv.ParseUint(c.Query("beforeId"), 10, 64)
	afterID64, _ := strconv.ParseUint(c.Query("afterId"), 10, 64)
	beforeID := uint(beforeID64)
	afterID := uint(afterID64)

	// 优先使用游标分页
	if beforeID > 0 || afterID > 0 {
		guilds, err := h.Svc.ListUserGuildsWithMemberCountCursor(u.ID, limit, beforeID, afterID)
		if err != nil {
			logger.Errorf("[Guilds] Failed to list guilds (cursor) for user %d: %v", u.ID, err)
			c.JSON(500, gin.H{"error": "获取服务器列表失败"})
			return
		}
		c.JSON(200, gin.H{"data": guilds})
		return
	}

	// 回退到页面分页
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	guilds, total, err := h.Svc.ListUserGuildsWithMemberCountPage(u.ID, page, limit)
	if err != nil {
		logger.Errorf("[Guilds] Failed to list guilds (page) for user %d: %v", u.ID, err)
		c.JSON(500, gin.H{"error": "获取服务器列表失败"})
		return
	}
	c.JSON(200, gin.H{
		"data":  guilds,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// @Summary      Join guild
// @Description  点击加入。行为受服务器 autoJoinMode 控制：no_approval 直接加入；no_approval_under_100 成员数<100 直接加入，否则提交申请；require_approval 提交申请等待审批
// @Tags         guilds
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Guild ID"
// @Success      204  {string}  string  ""
// @Failure      400  {object}  map[string]string  "加入失败"
// @Failure      401  {object}  map[string]string  "未认证"
// @Router       /api/guilds/{id}/join [post]
func (h *HTTP) joinGuild(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if err := h.Svc.JoinGuild(uint(gid64), u.ID); err != nil {
		logger.Errorf("[Guilds] Failed to join guild %d by user %d: %v", gid64, u.ID, err)
		c.JSON(400, gin.H{"error": "加入服务器失败"})
		return
	}
	c.Status(204)
}

// @Summary      Leave guild
// @Description  退出指定服务器
// @Tags         guilds
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Guild ID"
// @Success      204  {string}  string  ""
// @Failure      400  {object}  map[string]string  "退出失败"
// @Failure      401  {object}  map[string]string  "未认证"
// @Router       /api/guilds/{id}/leave [post]
func (h *HTTP) leaveGuild(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	// 检查是否是成员（用于广播）
	wasMember, _ := h.Svc.IsMember(uint(gid64), u.ID)
	if err := h.Svc.LeaveGuild(uint(gid64), u.ID); err != nil {
		// 即使退出失败（比如已经不是成员），也视为成功
		// 这样避免用户困惑
	}
	// 只有确实是成员时才广播离开事件
	if wasMember && h.Gw != nil {
		h.Gw.BroadcastToGuild(uint(gid64), config.EventGuildMemberRemove, gin.H{"userId": u.ID, "operatorId": u.ID})
	}
	c.Status(204)
}

// @Summary      Kick member from guild
// @Description  踢出成员
// @Tags         guilds
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id       path  int  true  "Guild ID"
// @Param        userId   path  int  true  "User ID to kick"
// @Success      200  {object}  map[string]any
// @Failure      400  {object}  map[string]string  "参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      403  {object}  map[string]string  "权限不足"
// @Failure      404  {object}  map[string]string  "服务器或成员不存在"
// @Router       /api/guilds/{id}/members/{userId} [delete]
func (h *HTTP) kickMember(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	uid64, _ := strconv.ParseUint(c.Param("userId"), 10, 64)
	if gid64 == 0 || uid64 == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID或用户ID"})
		return
	}

	if err := h.Svc.KickMember(uint(gid64), uint(uid64), u.ID); err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "权限不足"})
			return
		} else if err == service.ErrNotFound {
			// 成员不存在也视为成功（可能已经离开或被踢出）
			c.JSON(200, gin.H{"success": true, "message": "该用户已不在服务器中"})
			return
		} else {
			logger.Errorf("[Guilds] Failed to kick member %d from guild %d by user %d: %v", uid64, gid64, u.ID, err)
			c.JSON(400, gin.H{"error": "踢出成员失败"})
			return
		}
	}
	// 广播成员被移除
	if h.Gw != nil {
		h.Gw.BroadcastToGuild(uint(gid64), config.EventGuildMemberRemove, gin.H{"userId": uint(uid64), "operatorId": u.ID})
	}
	c.JSON(200, gin.H{"success": true})
}

// @Summary      Mute guild member
// @Description  禁言成员
// @Tags         guilds
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id       path  int  true  "Guild ID"
// @Param        userId   path  int  true  "User ID to mute"
// @Param        body     body  object  true  "{duration: 3600}"
// @Success      200  {object}  map[string]any
// @Failure      400  {object}  map[string]string  "参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      403  {object}  map[string]string  "权限不足"
// @Failure      404  {object}  map[string]string  "服务器或成员不存在"
// @Router       /api/guilds/{id}/members/{userId}/mute [put]
func (h *HTTP) muteMember(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	uid64, _ := strconv.ParseUint(c.Param("userId"), 10, 64)
	if gid64 == 0 || uid64 == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID或用户ID"})
		return
	}

	var body struct {
		Duration int `json:"duration"` // 禁言时长(秒)
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if body.Duration <= 0 {
		c.JSON(400, gin.H{"error": "禁言时长必须为正数"})
		return
	}

	duration := time.Duration(body.Duration) * time.Second
	if err := h.Svc.MuteMember(uint(gid64), uint(uid64), u.ID, duration); err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "权限不足"})
		} else if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "服务器或成员不存在"})
		} else {
			logger.Errorf("[Guilds] Failed to mute member %d in guild %d by user %d: %v", uid64, gid64, u.ID, err)
			c.JSON(400, gin.H{"error": "禁言成员失败"})
		}
		return
	}
	c.JSON(200, gin.H{"success": true})
}

// @Summary      Unmute guild member
// @Tags         guilds
// @Security     BearerAuth
// @Produce      json
// @Param        id       path  int  true  "Guild ID"
// @Param        userId   path  int  true  "User ID to unmute"
// @Success      200  {object}  map[string]any
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Router       /api/guilds/{id}/members/{userId}/mute [delete]
// unmuteMember 解除禁言
func (h *HTTP) unmuteMember(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	uid64, _ := strconv.ParseUint(c.Param("userId"), 10, 64)
	if gid64 == 0 || uid64 == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID或用户ID"})
		return
	}

	if err := h.Svc.UnmuteMember(uint(gid64), uint(uid64), u.ID); err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "权限不足"})
			return
		} else if err == service.ErrNotFound {
			// 成员不存在也视为解除禁言成功
			c.JSON(200, gin.H{"success": true})
			return
		} else {
			logger.Errorf("[Guilds] Failed to unmute member %d in guild %d by user %d: %v", uid64, gid64, u.ID, err)
			c.JSON(400, gin.H{"error": "解除禁言失败"})
			return
		}
	}
	// 广播成员更新（解除禁言）
	if h.Gw != nil {
		h.Gw.BroadcastToGuild(uint(gid64), config.EventGuildMemberUpdate, gin.H{"userId": uint(uid64), "action": "unmuted", "operatorId": u.ID})
	}
	c.JSON(200, gin.H{"success": true})
}

// @Summary      Toggle guild notification mute
// @Description  用户自主设置服务器免打扰
// @Tags         guilds
// @Accept       json
// @Produce      json
// @Param        id    path  int     true   "Guild ID"
// @Param        body  body  object  true   "{muted: bool}"
// @Success      200   {object}  map[string]bool
// @Router       /api/guilds/{id}/notify-mute [put]
func (h *HTTP) setGuildNotifyMuted(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if gid64 == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID"})
		return
	}

	var body struct {
		Muted bool `json:"muted"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}

	// 检查是否是成员
	isMember, _ := h.Svc.Repo.IsMember(uint(gid64), u.ID)
	if !isMember {
		c.JSON(403, gin.H{"error": "非服务器成员"})
		return
	}

	// 更新免打扰状态
	if err := h.Svc.Repo.DB.Model(&models.GuildMember{}).
		Where("guild_id = ? AND user_id = ?", gid64, u.ID).
		Update("notify_muted", body.Muted).Error; err != nil {
		c.JSON(500, gin.H{"error": "更新免打扰状态失败"})
		return
	}

	c.JSON(200, gin.H{"success": true, "muted": body.Muted})
}

// @Summary      Set member temporary nickname
// @Description  Set a temporary nickname for a guild member (admins can set for others, users can set for themselves)
// @Tags         guilds
// @Accept       json
// @Produce      json
// @Param        id        path      int                true   "Guild ID"
// @Param        userId    path      int                true   "User ID"
// @Param        body      body      map[string]string  true   "{nickname}"
// @Success      200       {object}  map[string]bool
// @Router       /api/guilds/{id}/members/{userId}/nickname [put]
// setMemberNickname 设置成员临时昵称
func (h *HTTP) setMemberNickname(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	uid64, _ := strconv.ParseUint(c.Param("userId"), 10, 64)
	if gid64 == 0 || uid64 == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID或用户ID"})
		return
	}

	var body struct {
		Nickname string `json:"nickname"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}

	// 昵称长度限制（可选）
	if len(body.Nickname) > 32 {
		c.JSON(400, gin.H{"error": "昵称过长（最多32个字符）"})
		return
	}

	if err := h.Svc.SetMemberNickname(uint(gid64), uint(uid64), u.ID, body.Nickname); err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "权限不足"})
		} else if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "服务器或成员不存在"})
		} else {
			logger.Errorf("[Guilds] Failed to set nickname for member %d in guild %d by user %d: %v", uid64, gid64, u.ID, err)
			c.JSON(400, gin.H{"error": "设置昵称失败"})
		}
		return
	}
	// 广播成员更新（昵称变更）
	if h.Gw != nil {
		h.Gw.BroadcastToGuild(uint(gid64), config.EventGuildMemberUpdate, gin.H{"userId": uint(uid64), "nickname": body.Nickname, "operatorId": u.ID})
	}
	c.JSON(200, gin.H{"success": true})
}

// @Summary      Get user permissions in guild
// @Tags         guilds
// @Security     BearerAuth
// @Produce      json
// @Param        id   path  int  true  "Guild ID"
// @Success      200  {object}  map[string]any
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Router       /api/guilds/{id}/permissions [get]
// getGuildPermissions 获取当前用户在指定服务器的权限
func (h *HTTP) getGuildPermissions(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	gid := uint(gid64)
	// 检查用户是否是该服务器的成员
	isMember, err := h.Svc.IsMember(gid, u.ID)
	if err != nil {
		logger.Errorf("[Guilds] Failed to check membership for guild %d and user %d: %v", gid, u.ID, err)
		c.JSON(500, gin.H{"error": "检查成员状态失败"})
		return
	}
	if !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "非服务器成员"})
		return
	}
	perms, err := h.Svc.EffectiveGuildPerms(gid, u.ID)
	if err != nil {
		logger.Errorf("[Guilds] Failed to get permissions for guild %d and user %d: %v", gid, u.ID, err)
		c.JSON(500, gin.H{"error": "获取权限失败"})
		return
	}
	c.JSON(200, gin.H{"permissions": perms})
}

// @Summary      Reorder guilds
// @Tags         guilds
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body body  object  true  "Array of {id, sortOrder}"
// @Success      200  {object}  map[string]any
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Router       /api/guilds/reorder [put]
// reorderGuilds 批量更新用户的服务器排序
func (h *HTTP) reorderGuilds(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	var body struct {
		Orders []struct {
			ID        uint `json:"id"`
			SortOrder int  `json:"sortOrder"`
		} `json:"orders"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if err := h.Svc.ReorderGuilds(u.ID, body.Orders); err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "非服务器成员"})
		} else {
			logger.Errorf("[Guilds] Failed to reorder guilds for user %d: %v", u.ID, err)
			c.JSON(400, gin.H{"error": "重新排序失败"})
		}
		return
	}
	c.JSON(200, gin.H{"success": true})
}

// @Summary      Get guild share info (public)
// @Tags         share
// @Produce      json
// @Param        id   path  int  true  "Guild ID"
// @Success      200  {object}  map[string]any
// @Failure      404  {object}  map[string]string
// @Router       /api/share/guild/{id} [get]
// getGuildShareInfo 获取服务器公开分享信息（不需要认证）
func (h *HTTP) getGuildShareInfo(c *gin.Context) {
	gid, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if gid == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID"})
		return
	}

	guild, err := h.Svc.Repo.GetGuild(uint(gid))
	if err != nil {
		c.JSON(404, gin.H{"error": "服务器不存在"})
		return
	}

	// 只返回公开信息
	shareInfo := gin.H{
		"id":          guild.ID,
		"name":        guild.Name,
		"description": guild.Description,
		"avatar":      guild.Avatar,
		"banner":      guild.Banner,
		"isPrivate":   guild.IsPrivate,
		"createdAt":   guild.CreatedAt,
	}

	// 获取成员数量（使用COUNT避免加载全量列表）
	if count, err := h.Svc.Repo.CountGuildMembers(uint(gid)); err == nil {
		shareInfo["memberCount"] = count
	} else {
		// 计数失败时回退为0，并记录最小信息
		shareInfo["memberCount"] = 0
	}

	c.JSON(200, shareInfo)
}

// @Summary      Get user share info (public)
// @Tags         share
// @Produce      json
// @Param        id   path  int  true  "User ID"
// @Success      200  {object}  map[string]any
// @Failure      404  {object}  map[string]string
// @Router       /api/share/user/{id} [get]
// getUserShareInfo 获取用户公开分享信息（不需要认证）
func (h *HTTP) getUserShareInfo(c *gin.Context) {
	uid, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if uid == 0 {
		c.JSON(400, gin.H{"error": "无效的用户ID"})
		return
	}

	user, err := h.Svc.Repo.GetUserByID(uint(uid))
	if err != nil {
		c.JSON(404, gin.H{"error": "用户不存在"})
		return
	}

	// 如果用户设置为私密账号，只返回基本信息
	shareInfo := gin.H{
		"id":        user.ID,
		"name":      user.Name,
		"avatar":    user.Avatar,
		"isPrivate": user.IsPrivate,
		"bio":       user.Bio,
		"banner":    user.Banner,
		"createdAt": user.CreatedAt,
	}

	// 非私密账号返回状态
	if !user.IsPrivate {
		shareInfo["status"] = user.Status
	}

	c.JSON(200, shareInfo)
}
