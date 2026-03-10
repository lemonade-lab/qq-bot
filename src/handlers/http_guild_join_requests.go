package handlers

import (
	"net/http"
	"strconv"

	"bubble/src/config"
	"bubble/src/middleware"
	"bubble/src/service"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// @Summary      Apply to join guild
// @Tags         guild-join-requests
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Guild ID"
// @Param        body body  object{note=string}  true  "Application note"
// @Success      200  {object}  models.GuildJoinRequest
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Router       /api/guilds/{id}/join-requests [post]
func (h *HTTP) applyGuildJoin(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	guildID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || guildID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}

	var body struct {
		Note string `json:"note"`
	}
	if err := c.BindJSON(&body); err != nil {
		// Note is optional, so binding error is acceptable if empty body
		body.Note = ""
	}

	req, guild, notif, err := h.Svc.ApplyGuildJoin(uint(guildID), u.ID, body.Note)
	if err != nil {
		if err == service.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "服务器不存在"})
			return
		} else if err == service.ErrAlreadyExists {
			// 已是成员或已申请，返回友好提示而非错误
			isMember, _ := h.Svc.IsMember(uint(guildID), u.ID)
			if isMember {
				c.JSON(http.StatusOK, gin.H{"status": "already_member", "message": "您已经是该服务器的成员"})
			} else {
				c.JSON(http.StatusOK, gin.H{"status": "already_applied", "message": "您已提交申请，请等待审核"})
			}
			return
		} else {
			logger.Errorf("[JoinRequests] Failed to apply join guild %d by user %d: %v", guildID, u.ID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "申请加入服务器失败"})
			return
		}
	}

	// autoJoinMode 允许直接加入时，ApplyGuildJoin 会直接加成员并返回 req=nil
	if req == nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  "join",
			"message": "已加入",
		})
		return
	}

	// 推送通知给公会 owner
	if notif != nil && guild != nil && h.Gw != nil {
		notifPayload := gin.H{
			"id":         notif.ID,
			"userId":     notif.UserID,
			"type":       notif.Type,
			"sourceType": notif.SourceType,
			"status":     "pending",
			"read":       false,
			"createdAt":  notif.CreatedAt,
		}

		// 添加公会信息
		if notif.GuildID != nil {
			notifPayload["guildId"] = *notif.GuildID
			notifPayload["guild"] = gin.H{
				"id":     guild.ID,
				"name":   guild.Name,
				"avatar": guild.Avatar,
			}
		}

		// 添加申请人信息
		if notif.AuthorID != nil {
			notifPayload["authorId"] = *notif.AuthorID
			notifPayload["author"] = gin.H{
				"id":     u.ID,
				"name":   u.Name,
				"avatar": u.Avatar,
			}
		}

		h.Gw.BroadcastNotice(guild.OwnerID, notifPayload)
	}

	c.JSON(http.StatusOK, req)
}

// @Summary      List guild join requests (with pagination)
// @Tags         guild-join-requests
// @Security     BearerAuth
// @Produce      json
// @Param        id    path   int  true   "Guild ID"
// @Param        page  query  int  false  "Page number (default 1)"
// @Param        limit query  int  false  "Items per page (default 20, max 100)"
// @Success      200   {object}  map[string]any
// @Failure      401   {object}  map[string]string
// @Failure      403   {object}  map[string]string
// @Router       /api/guilds/{id}/join-requests [get]
func (h *HTTP) listGuildJoinRequests(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	guildID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || guildID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}

	// Parse pagination params
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit < 1 || limit > 100 {
		limit = 10
	}

	requests, total, err := h.Svc.ListGuildJoinRequests(uint(guildID), u.ID, page, limit)
	if err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else if err == service.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "服务器不存在"})
		} else {
			logger.Errorf("[JoinRequests] Failed to list join requests for guild %d by user %d: %v", guildID, u.ID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "获取申请列表失败"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": requests,
		"pagination": gin.H{
			"page":    page,
			"limit":   limit,
			"total":   total,
			"hasMore": page*limit < total,
		},
	})
}

// @Summary      Get user's guild join request status
// @Tags         guild-join-requests
// @Security     BearerAuth
// @Produce      json
// @Param        id   path  int  true  "Guild ID"
// @Success      200  {object}  models.GuildJoinRequest  "Returns the join request if found, or null if not found"
// @Failure      400  {object}  map[string]string
// @Router       /api/guilds/{id}/join-requests/my-status [get]
// @Summary      Get user's guild join request status
// @Description  获取用户在特定服务器的加入请求状态
// @Tags         guild-join-requests
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id  path  int  true  "Guild ID"
// @Success      200  {object}  models.GuildJoinRequest
// @Failure      400  {object}  map[string]string  "参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      404  {object}  map[string]string  "无请求记录"
// @Failure      500  {object}  map[string]string  "服务器错误"
// @Router       /api/guilds/{id}/join-requests/me [get]
func (h *HTTP) getUserGuildJoinRequestStatus(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	guildID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || guildID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}

	req, err := h.Svc.GetUserGuildJoinRequestStatus(uint(guildID), u.ID)
	if err != nil {
		if err == service.ErrNotFound {
			// 没有找到记录是正常情况,返回 null
			c.JSON(http.StatusOK, nil)
		} else {
			logger.Errorf("[JoinRequests] Failed to get join request status for guild %d and user %d: %v", guildID, u.ID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "获取申请状态失败"})
		}
		return
	}

	c.JSON(http.StatusOK, req)
}

// @Summary      Batch get user's guild join request status
// @Tags         guild-join-requests
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body  object{guildIds=[]uint}  true  "Guild ID array"
// @Success      200   {object}  map[string]any "data: map[guildId]GuildJoinRequest|null"
// @Failure      400   {object}  map[string]string
// @Router       /api/guilds/join-requests/my-status/batch [post]
// @Summary      Get user's guild join request status batch
// @Description  批量获取用户在多个服务器的加入请求状态
// @Tags         guild-join-requests
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        guild_ids  query  string  true  "Comma-separated guild IDs"
// @Success      200        {array}   models.GuildJoinRequest
// @Failure      400        {object}  map[string]string  "参数错误"
// @Failure      401        {object}  map[string]string  "未认证"
// @Failure      500        {object}  map[string]string  "服务器错误"
// @Router       /api/guilds/join-requests/me [get]
func (h *HTTP) getUserGuildJoinRequestStatusBatch(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	var body struct {
		GuildIDs []uint `json:"guildIds"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	if len(body.GuildIDs) == 0 {
		c.JSON(http.StatusOK, gin.H{"data": gin.H{}})
		return
	}
	if len(body.GuildIDs) > 200 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "数量过多（最多200）"})
		return
	}

	mp, err := h.Svc.GetUserGuildJoinRequestStatusBatch(u.ID, body.GuildIDs)
	if err != nil {
		logger.Errorf("[JoinRequests] Failed to batch get join request status for user %d: %v", u.ID, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "批量获取申请状态失败"})
		return
	}

	// 返回统一 data 包装, key 使用字符串以兼容 JSON
	data := gin.H{}
	for gid, req := range mp {
		data[strconv.FormatUint(uint64(gid), 10)] = req
	}
	c.JSON(http.StatusOK, gin.H{"data": data})
}

// @Summary      Approve guild join request
// @Tags         guild-join-requests
// @Security     BearerAuth
// @Produce      json
// @Param        id        path  int  true  "Guild ID"
// @Param        requestId path  int  true  "Request ID"
// @Success      200       {object}  map[string]bool
// @Failure      401       {object}  map[string]string
// @Failure      403       {object}  map[string]string
// @Failure      404       {object}  map[string]string
// @Router       /api/guilds/{id}/join-requests/{requestId}/approve [post]
// @Summary      Approve guild join request
// @Description  批准用户的服务器加入请求（需要管理员权限）
// @Tags         guild-join-requests
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id      path  int  true  "Guild ID"
// @Param        userId  path  int  true  "User ID"
// @Success      200     {object}  map[string]string
// @Failure      400     {object}  map[string]string  "参数错误"
// @Failure      401     {object}  map[string]string  "未认证"
// @Failure      403     {object}  map[string]string  "权限不足"
// @Failure      404     {object}  map[string]string  "请求不存在"
// @Failure      500     {object}  map[string]string  "服务器错误"
// @Router       /api/guilds/{id}/join-requests/{userId}/approve [post]
func (h *HTTP) approveGuildJoin(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	guildID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || guildID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}

	requestID, err := strconv.ParseUint(c.Param("requestId"), 10, 64)
	if err != nil || requestID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求ID"})
		return
	}

	// 先获取请求信息（在批准前）
	req, err := h.Svc.Repo.GetGuildJoinRequestByID(uint(requestID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "请求不存在"})
		return
	}

	if err := h.Svc.ApproveGuildJoinRequest(uint(guildID), uint(requestID), u.ID); err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else if err == service.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "请求不存在"})
		} else {
			logger.Errorf("[JoinRequests] Failed to approve join request %d for guild %d by user %d: %v", requestID, guildID, u.ID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "批准申请失败"})
		}
		return
	}

	// 广播成员加入到公会
	if h.Gw != nil && req != nil {
		payload := gin.H{"userId": req.UserID}
		h.Gw.BroadcastToGuild(uint(guildID), config.EventGuildMemberAdd, payload)

		// 通知申请者其申请已被批准
		guild, _ := h.Svc.Repo.GetGuild(uint(guildID))
		notificationPayload := gin.H{
			"type":       "guild_join_approved",
			"sourceType": "system",
			"guildId":    guildID,
			"requestId":  requestID,
			"status":     "approved",
			"read":       false,
		}
		if guild != nil {
			notificationPayload["guild"] = gin.H{
				"id":     guild.ID,
				"name":   guild.Name,
				"avatar": guild.Avatar,
			}
		}
		h.Gw.BroadcastNotice(req.UserID, notificationPayload)
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// @Summary      Reject guild join request
// @Tags         guild-join-requests
// @Security     BearerAuth
// @Produce      json
// @Param        id        path  int  true  "Guild ID"
// @Param        requestId path  int  true  "Request ID"
// @Success      200       {object}  map[string]bool
// @Failure      401       {object}  map[string]string
// @Failure      403       {object}  map[string]string
// @Failure      404       {object}  map[string]string
// @Router       /api/guilds/{id}/join-requests/{requestId}/reject [post]
// @Summary      Reject guild join request
// @Description  拒绝用户的服务器加入请求（需要管理员权限）
// @Tags         guild-join-requests
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id      path  int  true  "Guild ID"
// @Param        userId  path  int  true  "User ID"
// @Success      200     {object}  map[string]string
// @Failure      400     {object}  map[string]string  "参数错误"
// @Failure      401     {object}  map[string]string  "未认证"
// @Failure      403     {object}  map[string]string  "权限不足"
// @Failure      404     {object}  map[string]string  "请求不存在"
// @Failure      500     {object}  map[string]string  "服务器错误"
// @Router       /api/guilds/{id}/join-requests/{userId}/reject [post]
func (h *HTTP) rejectGuildJoin(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	guildID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || guildID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}

	requestID, err := strconv.ParseUint(c.Param("requestId"), 10, 64)
	if err != nil || requestID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求ID"})
		return
	}

	// 先获取请求信息（在拒绝前）
	req, err := h.Svc.Repo.GetGuildJoinRequestByID(uint(requestID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "请求不存在"})
		return
	}

	if err := h.Svc.RejectGuildJoinRequest(uint(guildID), uint(requestID), u.ID); err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else if err == service.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "请求不存在"})
		} else {
			logger.Errorf("[JoinRequests] Failed to reject join request %d for guild %d by user %d: %v", requestID, guildID, u.ID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "拒绝申请失败"})
		}
		return
	}

	// 通知申请者其申请已被拒绝
	if h.Gw != nil && req != nil {
		guild, _ := h.Svc.Repo.GetGuild(uint(guildID))
		notificationPayload := gin.H{
			"type":       "guild_join_rejected",
			"sourceType": "system",
			"guildId":    guildID,
			"requestId":  requestID,
			"status":     "rejected",
			"read":       false,
		}
		if guild != nil {
			notificationPayload["guild"] = gin.H{
				"id":     guild.ID,
				"name":   guild.Name,
				"avatar": guild.Avatar,
			}
		}
		h.Gw.BroadcastNotice(req.UserID, notificationPayload)
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}
