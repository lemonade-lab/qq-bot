package handlers

import (
	"net/http"

	"bubble/src/config"
	"bubble/src/middleware"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// @Summary      Get unread counts
// @Description  获取当前用户的所有未读统计（频道、私聊、@提及等）
// @Tags         me
// @Security     BearerAuth
// @Produce      json
// @Success      200  {object}  map[string]any  "未读统计信息"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      500  {object}  map[string]string  "服务器错误"
// @Router       /api/me/unread [get]
func (h *HTTP) getUnreadCounts(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	counts, err := h.Svc.GetUnreadCounts(u.ID)
	if err != nil {
		logger.Errorf("[ReadState] Failed to get unread counts for user %d: %v", u.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取未读统计失败"})
		return
	}

	c.JSON(http.StatusOK, counts)
}

// @Summary      Mark channel as read
// @Description  标记频道已读到某条消息
// @Tags         channels
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Channel ID"
// @Param        body body object{messageId=int} true "最后已读消息ID"
// @Success      204  {string}  string  "no content"
// @Failure      400  {object}  map[string]string  "请求参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      403  {object}  map[string]string  "无权限"
// @Failure      404  {object}  map[string]string  "频道不存在"
// @Failure      500  {object}  map[string]string  "服务器错误"
// @Router       /api/channels/{id}/read [post]
func (h *HTTP) markChannelRead(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	channelID, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的频道ID"})
		return
	}

	var body struct {
		MessageID uint `json:"messageId" binding:"required"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}

	if err := h.Svc.MarkChannelRead(u.ID, channelID, body.MessageID); err != nil {
		logger.Errorf("[ReadState] Failed to mark channel %d as read for user %d: %v", channelID, u.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "标记已读失败"})
		return
	}

	// WebSocket 推送红点更新（频道级别）
	if h.Gw != nil {
		h.Gw.BroadcastReadStateUpdate(u.ID, "channel", channelID)

		// 同时推送公会级别的红点更新（需要查询实际计数）
		channel, _ := h.Svc.GetChannel(channelID)
		if channel != nil {
			guildRS, _ := h.Svc.Repo.GetReadState(u.ID, "guild", channel.GuildID)
			if guildRS != nil {
				h.Gw.BroadcastToUsers([]uint{u.ID}, config.EventReadStateUpdate, gin.H{
					"type":              "guild",
					"id":                channel.GuildID,
					"lastReadMessageId": guildRS.LastReadMessageID,
					"unreadCount":       guildRS.UnreadCount,
					"mentionCount":      guildRS.MentionCount,
				})
			} else {
				h.Gw.BroadcastReadStateUpdate(u.ID, "guild", channel.GuildID)
			}
		}
	}

	c.Status(http.StatusNoContent)
}

// @Summary      Mark DM as read
// @Description  标记私聊已读到某条消息
// @Tags         dm
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Thread ID"
// @Param        body body object{messageId=int} true "最后已读消息ID"
// @Success      204  {string}  string  "no content"
// @Failure      400  {object}  map[string]string  "请求参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      403  {object}  map[string]string  "无权限"
// @Failure      404  {object}  map[string]string  "线程不存在"
// @Failure      500  {object}  map[string]string  "服务器错误"
// @Router       /api/dm/{id}/read [post]
func (h *HTTP) markDmRead(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	threadID, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的线程ID"})
		return
	}

	var body struct {
		MessageID uint `json:"messageId" binding:"required"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}

	if err := h.Svc.MarkDmRead(u.ID, threadID, body.MessageID); err != nil {
		logger.Errorf("[ReadState] Failed to mark DM %d as read for user %d: %v", threadID, u.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "标记已读失败"})
		return
	}

	// WebSocket 推送红点更新
	if h.Gw != nil {
		h.Gw.BroadcastReadStateUpdate(u.ID, "dm", threadID)
	}

	c.Status(http.StatusNoContent)
}

// @Summary      Mark guild as read
// @Description  标记整个服务器的所有频道为已读
// @Tags         guilds
// @Security     BearerAuth
// @Produce      json
// @Param        id   path  int  true  "Guild ID"
// @Success      204  {string}  string  "no content"
// @Failure      400  {object}  map[string]string  "请求参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      403  {object}  map[string]string  "无权限"
// @Failure      404  {object}  map[string]string  "服务器不存在"
// @Failure      500  {object}  map[string]string  "服务器错误"
// @Router       /api/guilds/{id}/read [post]
func (h *HTTP) markGuildRead(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	guildID, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}

	if err := h.Svc.MarkGuildRead(u.ID, guildID); err != nil {
		logger.Errorf("[ReadState] Failed to mark guild %d as read for user %d: %v", guildID, u.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "标记已读失败"})
		return
	}

	// WebSocket 推送红点更新
	if h.Gw != nil {
		h.Gw.BroadcastReadStateUpdate(u.ID, "guild", guildID)
	}

	c.Status(http.StatusNoContent)
}
