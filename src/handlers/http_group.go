package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bubble/src/config"
	"bubble/src/db/models"
	"bubble/src/middleware"
	"bubble/src/service"

	"gorm.io/datatypes"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// ==================== 群聊线程 ====================

// createGroupThread 创建群聊
func (h *HTTP) createGroupThread(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	var body struct {
		Name      string `json:"name"`
		MemberIDs []uint `json:"memberIds"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	thread, err := h.Svc.CreateGroupThread(u.ID, body.Name, body.MemberIDs)
	if err != nil {
		if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
			return
		}
		logger.Errorf("[Group] Failed to create group by user %d: %v", u.ID, err)
		c.JSON(400, gin.H{"error": "创建群聊失败"})
		return
	}

	// 广播给所有成员
	if h.Gw != nil {
		memberIDs, _ := h.Svc.Repo.GetGroupMemberIDs(thread.ID)
		h.Gw.BroadcastToUsers(memberIDs, config.EventGroupThreadUpdate, gin.H{
			"action": "created",
			"thread": thread,
		})
	}

	c.JSON(200, thread)
}

// getGroupThread 获取群聊详情
func (h *HTTP) getGroupThread(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	thread, err := h.Svc.GetGroupThread(id, u.ID)
	if err != nil {
		if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "群聊不存在"})
		} else if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "非群成员"})
		} else {
			c.JSON(500, gin.H{"error": "服务器错误"})
		}
		return
	}
	c.JSON(200, thread)
}

// listGroupThreads 列出我的群聊列表
func (h *HTTP) listGroupThreads(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	beforeID64, _ := strconv.ParseUint(c.Query("beforeId"), 10, 64)
	afterID64, _ := strconv.ParseUint(c.Query("afterId"), 10, 64)
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit < 1 || limit > 100 {
		limit = 50
	}
	threads, err := h.Svc.ListUserGroupThreads(u.ID, limit, uint(beforeID64), uint(afterID64))
	if err != nil {
		logger.Errorf("[Group] Failed to list group threads for user %d: %v", u.ID, err)
		c.JSON(500, gin.H{"error": "获取群聊列表失败"})
		return
	}
	c.JSON(200, gin.H{"data": threads})
}

// updateGroupThread 更新群聊信息
func (h *HTTP) updateGroupThread(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	var body struct {
		Name     *string `json:"name"`
		Avatar   *string `json:"avatar"`
		Banner   *string `json:"banner"`
		JoinMode *string `json:"joinMode"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if err := h.Svc.UpdateGroupThread(id, u.ID, body.Name, body.Avatar, body.Banner, body.JoinMode); err != nil {
		if err == service.ErrForbidden {
			c.JSON(403, gin.H{"error": "权限不足"})
		} else if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
		} else {
			c.JSON(400, gin.H{"error": "更新失败"})
		}
		return
	}

	// 广播更新事件
	if h.Gw != nil {
		memberIDs, _ := h.Svc.Repo.GetGroupMemberIDs(id)
		thread, _ := h.Svc.Repo.GetGroupThread(id)
		h.Gw.BroadcastToUsers(memberIDs, config.EventGroupThreadUpdate, gin.H{
			"action": "updated",
			"thread": thread,
		})
	}
	c.JSON(200, gin.H{})
}

// deleteGroupThread 解散群聊
func (h *HTTP) deleteGroupThread(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}

	// 先获取成员列表用于广播
	memberIDs, _ := h.Svc.Repo.GetGroupMemberIDs(id)

	if err := h.Svc.DeleteGroupThread(id, u.ID); err != nil {
		if err == service.ErrForbidden {
			c.JSON(403, gin.H{"error": "仅群主可解散"})
		} else if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "群聊不存在"})
		} else {
			c.JSON(400, gin.H{"error": "解散失败"})
		}
		return
	}

	// 广播解散事件
	if h.Gw != nil && len(memberIDs) > 0 {
		h.Gw.BroadcastToUsers(memberIDs, config.EventGroupThreadUpdate, gin.H{
			"action":   "deleted",
			"threadId": id,
		})
	}

	c.Status(http.StatusNoContent)
}

// ==================== 群成员管理 ====================

// addGroupMembers 添加群成员
func (h *HTTP) addGroupMembers(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	var body struct {
		UserIDs []uint `json:"userIds"`
	}
	if err := c.BindJSON(&body); err != nil || len(body.UserIDs) == 0 {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	added, err := h.Svc.AddGroupMembers(id, u.ID, body.UserIDs)
	if err != nil {
		if err == service.ErrForbidden {
			c.JSON(403, gin.H{"error": "权限不足"})
		} else if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
		} else {
			c.JSON(400, gin.H{"error": "添加成员失败"})
		}
		return
	}

	// 广播成员新增事件
	if h.Gw != nil {
		memberIDs, _ := h.Svc.Repo.GetGroupMemberIDs(id)
		for _, m := range added {
			userInfo, _ := h.Svc.GetUserByID(m.UserID)
			h.Gw.BroadcastToUsers(memberIDs, config.EventGroupMemberAdd, gin.H{
				"threadId": id,
				"member":   m,
				"user":     userInfo,
			})
		}
	}

	c.JSON(200, gin.H{"added": added})
}

// removeGroupMember 移除群成员 / 退出群聊
func (h *HTTP) removeGroupMember(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	targetUID, err := parseUintParam(c, "userId")
	if err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}

	// 先获取成员列表用于广播
	memberIDs, _ := h.Svc.Repo.GetGroupMemberIDs(id)

	if err := h.Svc.RemoveGroupMember(id, u.ID, targetUID); err != nil {
		if err == service.ErrForbidden {
			c.JSON(403, gin.H{"error": "权限不足"})
		} else if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "成员不存在"})
		} else if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
		} else {
			c.JSON(400, gin.H{"error": "移除成员失败"})
		}
		return
	}

	// 广播成员移除事件
	if h.Gw != nil && len(memberIDs) > 0 {
		h.Gw.BroadcastToUsers(memberIDs, config.EventGroupMemberRemove, gin.H{
			"threadId": id,
			"userId":   targetUID,
			"isLeave":  u.ID == targetUID,
		})
	}

	c.Status(http.StatusNoContent)
}

// updateGroupMemberRole 更新成员角色
func (h *HTTP) updateGroupMemberRole(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	targetUID, err := parseUintParam(c, "userId")
	if err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	var body struct {
		Role string `json:"role"` // admin | member
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if err := h.Svc.UpdateGroupMemberRole(id, u.ID, targetUID, body.Role); err != nil {
		if err == service.ErrForbidden {
			c.JSON(403, gin.H{"error": "仅群主可操作"})
		} else if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
		} else {
			c.JSON(400, gin.H{"error": "更新角色失败"})
		}
		return
	}

	// 广播角色变更
	if h.Gw != nil {
		memberIDs, _ := h.Svc.Repo.GetGroupMemberIDs(id)
		h.Gw.BroadcastToUsers(memberIDs, config.EventGroupThreadUpdate, gin.H{
			"action":   "role_updated",
			"threadId": id,
			"userId":   targetUID,
			"role":     body.Role,
		})
	}

	c.JSON(200, gin.H{})
}

// transferGroupOwner 转让群主
func (h *HTTP) transferGroupOwner(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	var body struct {
		NewOwnerID uint `json:"newOwnerId"`
	}
	if err := c.BindJSON(&body); err != nil || body.NewOwnerID == 0 {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if err := h.Svc.TransferGroupOwner(id, u.ID, body.NewOwnerID); err != nil {
		if err == service.ErrForbidden {
			c.JSON(403, gin.H{"error": "仅群主可转让"})
		} else if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "目标用户非群成员"})
		} else {
			c.JSON(400, gin.H{"error": "转让失败"})
		}
		return
	}

	// 广播群主变更
	if h.Gw != nil {
		memberIDs, _ := h.Svc.Repo.GetGroupMemberIDs(id)
		h.Gw.BroadcastToUsers(memberIDs, config.EventGroupThreadUpdate, gin.H{
			"action":     "owner_transferred",
			"threadId":   id,
			"newOwnerId": body.NewOwnerID,
		})
	}

	c.JSON(200, gin.H{})
}

// setGroupMuted 设置群聊免打扰
func (h *HTTP) setGroupMuted(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	var body struct {
		Muted bool `json:"muted"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if err := h.Svc.SetGroupMuted(id, u.ID, body.Muted); err != nil {
		c.JSON(400, gin.H{"error": "设置失败"})
		return
	}
	c.JSON(200, gin.H{})
}

// ==================== 群聊消息 ====================

// getGroupMessages 获取群聊消息列表（含用户信息）
func (h *HTTP) getGroupMessages(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	before, _ := strconv.ParseUint(c.Query("beforeId"), 10, 64)
	after, _ := strconv.ParseUint(c.Query("afterId"), 10, 64)
	if limit < 1 || limit > 100 {
		limit = 50
	}
	list, users, err := h.Svc.GetGroupMessagesWithUsers(u.ID, id, limit, uint(before), uint(after))
	if err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "非群成员"})
		} else {
			c.JSON(500, gin.H{"error": "获取消息失败"})
		}
		return
	}
	hasMore := len(list) >= limit
	var nextCursor uint
	if hasMore && len(list) > 0 {
		nextCursor = list[len(list)-1].ID
	}
	c.JSON(200, gin.H{
		"messages":   list,
		"users":      users,
		"hasMore":    hasMore,
		"nextCursor": nextCursor,
	})
}

// postGroupMessage 发送群聊消息
func (h *HTTP) postGroupMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	var body struct {
		Content   string      `json:"content"`
		ReplyToID *uint       `json:"replyToId"`
		Type      string      `json:"type"`
		Platform  string      `json:"platform"`
		FileMeta  interface{} `json:"fileMeta"`
		TempID    string      `json:"tempId"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	var jm datatypes.JSON
	if body.FileMeta != nil {
		h.enrichFileMetaDimensions(c.Request.Context(), body.FileMeta)
		bs, _ := json.Marshal(body.FileMeta)
		jm = datatypes.JSON(bs)
	}
	msgType := "text"
	if strings.TrimSpace(body.Type) != "" {
		msgType = body.Type
	}
	platform := "web"
	if strings.TrimSpace(body.Platform) != "" {
		platform = body.Platform
	}
	if len([]rune(body.Content)) > int(config.MaxMessageLength) {
		c.JSON(400, gin.H{"error": "消息过长"})
		return
	}

	msg, err := h.Svc.SendGroupMessage(u.ID, id, body.Content, body.ReplyToID, msgType, platform, jm, body.TempID)
	if err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "非群成员"})
		} else {
			logger.Errorf("[Group] Failed to send message from user %d to group %d: %v", u.ID, id, err)
			c.JSON(400, gin.H{"error": "发送消息失败"})
		}
		return
	}

	c.JSON(200, msg)

	// 广播消息
	if h.Gw != nil {
		payload := h.buildGroupMessagePayload(msg, u)
		memberIDs, _ := h.Svc.Repo.GetGroupMemberIDs(id)
		h.Gw.BroadcastToUsers(memberIDs, config.EventGroupMessageCreate, payload)

		// 更新红点
		go func() {
			if err := h.Svc.OnNewGroupMessage(id, msg.ID, u.ID); err != nil {
				logger.Warnf("[ReadState] Failed to update unread for group %d: %v", id, err)
			}
			h.broadcastGroupReadStateUpdate(id, u.ID)
		}()
	}
}

// updateGroupMessage 编辑群聊消息
func (h *HTTP) updateGroupMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	mid, err := parseUintParam(c, "messageId")
	if err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if len([]rune(body.Content)) > int(config.MaxMessageLength) {
		c.JSON(400, gin.H{"error": "消息过长"})
		return
	}

	msg, err := h.Svc.Repo.GetGroupMessage(mid)
	if err != nil {
		c.JSON(404, gin.H{"error": "消息不存在"})
		return
	}
	if msg.AuthorID != u.ID {
		c.JSON(403, gin.H{"error": "仅作者可编辑"})
		return
	}
	if err := h.Svc.Repo.DB.Model(msg).Updates(map[string]interface{}{
		"content":   body.Content,
		"edited_at": time.Now(),
	}).Error; err != nil {
		c.JSON(500, gin.H{"error": "更新失败"})
		return
	}
	h.Svc.Repo.DB.First(msg, msg.ID)
	c.JSON(200, msg)

	// 广播编辑事件
	if h.Gw != nil {
		payload := h.buildGroupMessagePayload(msg, u)
		memberIDs, _ := h.Svc.Repo.GetGroupMemberIDs(msg.ThreadID)
		h.Gw.BroadcastToUsers(memberIDs, config.EventGroupMessageUpdate, payload)
	}
}

// deleteGroupMessage 撤回群聊消息
func (h *HTTP) deleteGroupMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	mid, err := parseUintParam(c, "messageId")
	if err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}

	msg, err := h.Svc.Repo.GetGroupMessage(mid)
	if err != nil {
		c.Status(204)
		return
	}
	threadID := msg.ThreadID

	if err := h.Svc.DeleteGroupMessage(mid, u.ID); err != nil {
		if err == service.ErrForbidden {
			c.JSON(403, gin.H{"error": "权限不足"})
		} else if err == service.ErrNotFound {
			c.Status(204)
		} else {
			c.JSON(400, gin.H{"error": "删除失败"})
		}
		return
	}

	// 广播撤回事件
	if h.Gw != nil {
		memberIDs, _ := h.Svc.Repo.GetGroupMemberIDs(threadID)
		h.Gw.BroadcastToUsers(memberIDs, config.EventGroupMessageDelete, gin.H{
			"id":       mid,
			"threadId": threadID,
			"deleted":  true,
		})
	}

	c.Status(204)
}

// batchDeleteGroupMessages 批量撤回群聊消息
// @Summary      Batch delete group messages
// @Description  批量撤回群聊消息（消息作者、管理员或群主）
// @Tags         group
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "{messageIds: [uint]}"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Router       /api/group/messages/batch-delete [post]
func (h *HTTP) batchDeleteGroupMessages(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	var body struct {
		MessageIDs []uint `json:"messageIds" binding:"required"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	if len(body.MessageIDs) == 0 {
		c.JSON(400, gin.H{"error": "messageIds 不能为空"})
		return
	}
	if len(body.MessageIDs) > 100 {
		c.JSON(400, gin.H{"error": "单次最多撤回100条消息"})
		return
	}

	succeeded := make([]uint, 0, len(body.MessageIDs))
	failed := make([]gin.H, 0)

	for _, mid := range body.MessageIDs {
		msg, err := h.Svc.Repo.GetGroupMessage(mid)
		if err != nil {
			succeeded = append(succeeded, mid)
			continue
		}
		threadID := msg.ThreadID

		if err := h.Svc.DeleteGroupMessage(mid, u.ID); err != nil {
			reason := "撤回失败"
			if err == service.ErrForbidden {
				reason = "权限不足"
			}
			failed = append(failed, gin.H{"messageId": mid, "error": reason})
			continue
		}

		succeeded = append(succeeded, mid)

		// 广播撤回事件
		if h.Gw != nil {
			memberIDs, _ := h.Svc.Repo.GetGroupMemberIDs(threadID)
			h.Gw.BroadcastToUsers(memberIDs, config.EventGroupMessageDelete, gin.H{
				"id":       mid,
				"threadId": threadID,
				"deleted":  true,
			})
		}
	}

	c.JSON(200, gin.H{
		"succeeded": succeeded,
		"failed":    failed,
	})
}

// markGroupRead 标记群聊已读
func (h *HTTP) markGroupRead(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	var body struct {
		MessageID uint `json:"messageId"`
	}
	_ = c.BindJSON(&body)

	if err := h.Svc.MarkGroupRead(id, u.ID, body.MessageID); err != nil {
		c.JSON(400, gin.H{"error": "标记已读失败"})
		return
	}

	// 广播红点更新
	if h.Gw != nil {
		h.Gw.BroadcastToUsers([]uint{u.ID}, config.EventReadStateUpdate, gin.H{
			"type":              "group",
			"id":                id,
			"lastReadMessageId": body.MessageID,
			"unreadCount":       0,
			"mentionCount":      0,
		})
	}

	c.JSON(200, gin.H{})
}

// buildGroupMessagePayload 构建群聊消息 payload
func (h *HTTP) buildGroupMessagePayload(msg *models.GroupMessage, author *models.User) gin.H {
	payload := gin.H{
		"id":        msg.ID,
		"threadId":  msg.ThreadID,
		"authorId":  msg.AuthorID,
		"content":   msg.Content,
		"type":      msg.Type,
		"fileMeta":  msg.FileMeta,
		"replyToId": msg.ReplyToID,
		"createdAt": msg.CreatedAt,
		"createdTs": msg.CreatedAt.UnixMilli(),
	}
	if msg.TempID != "" {
		payload["tempId"] = msg.TempID
	}
	if author != nil {
		payload["authorInfo"] = gin.H{
			"id":     author.ID,
			"name":   author.Name,
			"avatar": author.Avatar,
		}
	}
	if len(msg.Mentions) > 0 {
		payload["mentions"] = msg.Mentions
	} else {
		payload["mentions"] = []map[string]any{}
	}
	return payload
}

// ==================== 群聊精华消息 ====================

// listPinnedGroupMessages 列出群聊精华消息
func (h *HTTP) listPinnedGroupMessages(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	tid, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	list, err := h.Svc.ListPinnedGroupMessages(tid, u.ID)
	if err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权限"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	c.JSON(http.StatusOK, list)
}

// pinGroupMessage 群聊消息设为精华
func (h *HTTP) pinGroupMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	tid, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	var body struct {
		MessageID uint `json:"messageId"`
	}
	if err := c.BindJSON(&body); err != nil || body.MessageID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	pm, err := h.Svc.PinGroupMessage(tid, body.MessageID, u.ID)
	if err != nil {
		if err == service.ErrForbidden {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else if err == service.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "消息不存在"})
		} else if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else {
			logger.Errorf("[Pins] Failed to pin group message in thread %d: %v", tid, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		}
		return
	}
	// 广播群聊精华新增
	if h.Gw != nil {
		memberIDs, _ := h.Svc.Repo.GetGroupMemberIDs(tid)
		h.Gw.BroadcastToUsers(memberIDs, config.EventGroupMessagePin, gin.H{"id": pm.MessageID, "threadId": tid})
	}
	c.JSON(http.StatusOK, pm)
}

// unpinGroupMessage 取消群聊消息精华
func (h *HTTP) unpinGroupMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	tid, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	messageID, err := parseUintParam(c, "messageId")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	if err := h.Svc.UnpinGroupMessage(tid, messageID, u.ID); err != nil {
		if err == service.ErrNotFound {
			c.Status(http.StatusNoContent)
			return
		} else if err == service.ErrForbidden || err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
			return
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
			return
		}
	}
	// 广播群聊精华移除
	if h.Gw != nil {
		memberIDs, _ := h.Svc.Repo.GetGroupMemberIDs(tid)
		h.Gw.BroadcastToUsers(memberIDs, config.EventGroupMessageUnpin, gin.H{"id": messageID, "threadId": tid})
	}
	c.Status(http.StatusNoContent)
}

// ==================== 群聊收藏 ====================

// favoriteGroupMessage 收藏群聊消息
func (h *HTTP) favoriteGroupMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	var body struct {
		GroupMessageID uint `json:"groupMessageId"`
	}
	if err := c.BindJSON(&body); err != nil || body.GroupMessageID == 0 {
		c.JSON(400, gin.H{"error": "缺少群聊消息ID"})
		return
	}
	if err := h.Svc.FavoriteGroupMessage(u.ID, body.GroupMessageID); err != nil {
		logger.Errorf("[Favorites] Failed to favorite group message %d for user %d: %v", body.GroupMessageID, u.ID, err)
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true})
}

// ==================== 群聊成员搜索 ====================

// searchGroupMembers 搜索群聊成员（支持@提及）
func (h *HTTP) searchGroupMembers(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	tid, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	query := c.Query("q")
	if query == "" {
		c.JSON(400, gin.H{"error": "缺少搜索关键词"})
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit < 1 || limit > 20 {
		limit = 10
	}
	members, err := h.Svc.SearchGroupMembers(tid, u.ID, query, limit)
	if err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权限"})
			return
		}
		logger.Errorf("[Group] Failed to search members in group %d with query '%s': %v", tid, query, err)
		c.JSON(500, gin.H{"error": "搜索成员失败"})
		return
	}
	c.JSON(200, gin.H{"data": members})
}
