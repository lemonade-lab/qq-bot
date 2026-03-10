package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"bubble/src/config"
	"bubble/src/db/models"
	"bubble/src/middleware"
	"bubble/src/service"

	"gorm.io/datatypes"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// ==================== 频道子房间 ====================

// createSubRoom 创建频道子房间
func (h *HTTP) createSubRoom(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	channelID, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	var body struct {
		GuildID uint   `json:"guildId"`
		Name    string `json:"name"`
	}
	if err := c.BindJSON(&body); err != nil || body.GuildID == 0 {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}

	room, err := h.Svc.CreateSubRoom(body.GuildID, channelID, u.ID, body.Name)
	if err != nil {
		if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
			return
		}
		if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "非服务器成员"})
			return
		}
		if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "频道不存在"})
			return
		}
		logger.Errorf("[SubRoom] Failed to create subroom by user %d in channel %d: %v", u.ID, channelID, err)
		c.JSON(400, gin.H{"error": "创建子房间失败"})
		return
	}

	// 广播给频道（所有服务器成员可见）
	if h.Gw != nil {
		h.Gw.BroadcastToChannel(channelID, config.EventSubRoomUpdate, gin.H{
			"action": "created",
			"room":   room,
		})
	}

	c.JSON(200, room)
}

// getSubRoom 获取子房间详情
func (h *HTTP) getSubRoom(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	roomID, err := parseUintParam(c, "roomId")
	if err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	room, err := h.Svc.GetSubRoom(roomID, u.ID)
	if err != nil {
		if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "子房间不存在"})
		} else if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "非服务器成员"})
		} else {
			c.JSON(500, gin.H{"error": "服务器错误"})
		}
		return
	}
	c.JSON(200, room)
}

// listSubRooms 列出频道下所有子房间
func (h *HTTP) listSubRooms(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	channelID, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	guildID64, _ := strconv.ParseUint(c.Query("guildId"), 10, 64)
	if guildID64 == 0 {
		c.JSON(400, gin.H{"error": "缺少 guildId"})
		return
	}
	rooms, err := h.Svc.ListSubRooms(uint(guildID64), channelID, u.ID)
	if err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "非服务器成员"})
		} else {
			c.JSON(500, gin.H{"error": "获取子房间列表失败"})
		}
		return
	}
	c.JSON(200, gin.H{"data": rooms})
}

// updateSubRoom 更新子房间信息
func (h *HTTP) updateSubRoom(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	roomID, err := parseUintParam(c, "roomId")
	if err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	var body struct {
		Name   *string `json:"name"`
		Avatar *string `json:"avatar"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if err := h.Svc.UpdateSubRoom(roomID, u.ID, body.Name, body.Avatar); err != nil {
		if err == service.ErrForbidden {
			c.JSON(403, gin.H{"error": "仅房主可操作"})
		} else if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "子房间不存在"})
		} else if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
		} else {
			c.JSON(400, gin.H{"error": "更新失败"})
		}
		return
	}

	// 广播更新
	if h.Gw != nil {
		room, _ := h.Svc.Repo.GetSubRoom(roomID)
		if room != nil {
			memberIDs, _ := h.Svc.Repo.GetSubRoomMemberIDs(roomID)
			h.Gw.BroadcastToUsers(memberIDs, config.EventSubRoomUpdate, gin.H{
				"action": "updated",
				"room":   room,
			})
		}
	}

	c.JSON(200, gin.H{})
}

// deleteSubRoom 删除子房间
func (h *HTTP) deleteSubRoom(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	roomID, err := parseUintParam(c, "roomId")
	if err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}

	// 先获取信息用于广播
	room, _ := h.Svc.Repo.GetSubRoom(roomID)
	memberIDs, _ := h.Svc.Repo.GetSubRoomMemberIDs(roomID)

	if err := h.Svc.DeleteSubRoom(roomID, u.ID); err != nil {
		if err == service.ErrForbidden {
			c.JSON(403, gin.H{"error": "仅房主可删除"})
		} else if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "子房间不存在"})
		} else {
			c.JSON(400, gin.H{"error": "删除失败"})
		}
		return
	}

	// 广播删除事件给成员 + 频道（让列表刷新）
	if h.Gw != nil {
		if len(memberIDs) > 0 {
			h.Gw.BroadcastToUsers(memberIDs, config.EventSubRoomUpdate, gin.H{
				"action": "deleted",
				"roomId": roomID,
			})
		}
		if room != nil {
			h.Gw.BroadcastToChannel(room.ChannelID, config.EventSubRoomUpdate, gin.H{
				"action": "deleted",
				"roomId": roomID,
			})
		}
	}

	c.Status(http.StatusNoContent)
}

// ==================== 子房间成员管理 ====================

// addSubRoomMembers 添加子房间成员
func (h *HTTP) addSubRoomMembers(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	roomID, err := parseUintParam(c, "roomId")
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

	added, err := h.Svc.AddSubRoomMembers(roomID, u.ID, body.UserIDs)
	if err != nil {
		if err == service.ErrForbidden {
			c.JSON(403, gin.H{"error": "仅房主可添加成员"})
		} else if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "子房间不存在"})
		} else if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
		} else {
			c.JSON(400, gin.H{"error": "添加成员失败"})
		}
		return
	}

	// 广播成员新增事件
	if h.Gw != nil {
		memberIDs, _ := h.Svc.Repo.GetSubRoomMemberIDs(roomID)
		for _, m := range added {
			userInfo, _ := h.Svc.GetUserByID(m.UserID)
			h.Gw.BroadcastToUsers(memberIDs, config.EventSubRoomMemberAdd, gin.H{
				"roomId": roomID,
				"member": m,
				"user":   userInfo,
			})
		}
	}

	c.JSON(200, gin.H{"added": added})
}

// removeSubRoomMember 移除子房间成员 / 退出子房间
func (h *HTTP) removeSubRoomMember(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	roomID, err := parseUintParam(c, "roomId")
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
	memberIDs, _ := h.Svc.Repo.GetSubRoomMemberIDs(roomID)

	if err := h.Svc.RemoveSubRoomMember(roomID, u.ID, targetUID); err != nil {
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
		h.Gw.BroadcastToUsers(memberIDs, config.EventSubRoomMemberRemove, gin.H{
			"roomId":  roomID,
			"userId":  targetUID,
			"isLeave": u.ID == targetUID,
		})
	}

	c.Status(http.StatusNoContent)
}

// ==================== 子房间消息 ====================

// getSubRoomMessages 获取子房间消息列表
func (h *HTTP) getSubRoomMessages(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	roomID, err := parseUintParam(c, "roomId")
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

	list, err := h.Svc.GetSubRoomMessages(u.ID, roomID, limit, uint(before), uint(after))
	if err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "非子房间成员"})
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
		"hasMore":    hasMore,
		"nextCursor": nextCursor,
	})
}

// postSubRoomMessage 发送子房间消息
func (h *HTTP) postSubRoomMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	roomID, err := parseUintParam(c, "roomId")
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

	msg, err := h.Svc.SendSubRoomMessage(u.ID, roomID, body.Content, body.ReplyToID, msgType, platform, jm, body.TempID)
	if err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "非子房间成员"})
		} else {
			logger.Errorf("[SubRoom] Failed to send message from user %d to room %d: %v", u.ID, roomID, err)
			c.JSON(400, gin.H{"error": "发送消息失败"})
		}
		return
	}

	c.JSON(200, msg)

	// 广播消息
	if h.Gw != nil {
		payload := h.buildSubRoomMessagePayload(msg, u)
		memberIDs, _ := h.Svc.Repo.GetSubRoomMemberIDs(roomID)
		h.Gw.BroadcastToUsers(memberIDs, config.EventSubRoomMessageCreate, payload)

		// 更新红点
		go func() {
			if err := h.Svc.OnNewSubRoomMessage(roomID, msg.ID, u.ID); err != nil {
				logger.Warnf("[ReadState] Failed to update unread for subroom %d: %v", roomID, err)
			}
		}()
	}
}

// deleteSubRoomMessage 撤回子房间消息
func (h *HTTP) deleteSubRoomMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	messageID, err := parseUintParam(c, "messageId")
	if err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}

	msg, err := h.Svc.Repo.GetSubRoomMessage(messageID)
	if err != nil {
		c.Status(204)
		return
	}
	roomID := msg.RoomID

	if err := h.Svc.DeleteSubRoomMessage(messageID, u.ID); err != nil {
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
		memberIDs, _ := h.Svc.Repo.GetSubRoomMemberIDs(roomID)
		h.Gw.BroadcastToUsers(memberIDs, config.EventSubRoomMessageDelete, gin.H{
			"id":      messageID,
			"roomId":  roomID,
			"deleted": true,
		})
	}

	c.Status(204)
}

// markSubRoomRead 标记子房间已读
func (h *HTTP) markSubRoomRead(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	roomID, err := parseUintParam(c, "roomId")
	if err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	var body struct {
		MessageID uint `json:"messageId"`
	}
	_ = c.BindJSON(&body)

	if err := h.Svc.MarkSubRoomRead(roomID, u.ID, body.MessageID); err != nil {
		c.JSON(400, gin.H{"error": "标记已读失败"})
		return
	}

	// 广播红点更新
	if h.Gw != nil {
		h.Gw.BroadcastToUsers([]uint{u.ID}, config.EventReadStateUpdate, gin.H{
			"resourceType": "subroom",
			"resourceId":   roomID,
			"unreadCount":  0,
			"mentionCount": 0,
		})
	}

	c.JSON(200, gin.H{})
}

// buildSubRoomMessagePayload 构建子房间消息 payload
func (h *HTTP) buildSubRoomMessagePayload(msg *models.SubRoomMessage, author *models.User) gin.H {
	payload := gin.H{
		"id":        msg.ID,
		"roomId":    msg.RoomID,
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
