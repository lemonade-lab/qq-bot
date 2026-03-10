package handlers

import (
	"net/http"
	"strconv"

	"bubble/src/config"
	"bubble/src/middleware"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// @Summary      Favorite a channel message
// @Tags         favorites
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body      map[string]int  true  "{messageId}"
// @Success      200   {string}  string  ""
// @Router       /api/favorites/channel [post]
func (h *HTTP) favoriteChannelMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	var body struct {
		MessageID uint `json:"messageId"`
	}
	if err := c.BindJSON(&body); err != nil || body.MessageID == 0 {
		c.JSON(400, gin.H{"error": "缺少消息ID"})
		return
	}
	if err := h.Svc.FavoriteMessage(u.ID, body.MessageID); err != nil {
		logger.Errorf("[Favorites] Failed to favorite channel message %d for user %d: %v", body.MessageID, u.ID, err)
		c.JSON(400, gin.H{"error": "收藏失败"})
		return
	}
	c.JSON(200, gin.H{"success": true})
}

// @Summary      Favorite a DM message
// @Tags         favorites
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body      map[string]int  true  "{dmMessageId}"
// @Success      200   {string}  string  ""
// @Router       /api/favorites/dm [post]
func (h *HTTP) favoriteDmMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	var body struct {
		DmMessageID uint `json:"dmMessageId"`
	}
	if err := c.BindJSON(&body); err != nil || body.DmMessageID == 0 {
		c.JSON(400, gin.H{"error": "缺少私信消息ID"})
		return
	}
	if err := h.Svc.FavoriteDmMessage(u.ID, body.DmMessageID); err != nil {
		logger.Errorf("[Favorites] Failed to favorite DM message %d for user %d: %v", body.DmMessageID, u.ID, err)
		c.JSON(400, gin.H{"error": "收藏失败"})
		return
	}
	c.JSON(200, gin.H{"success": true})
}

// @Summary      Unfavorite a message
// @Tags         favorites
// @Security     BearerAuth
// @Produce      json
// @Param        id  path  int  true  "Favorite ID"
// @Success      204   {string}  string  ""
// @Router       /api/favorites/{id} [delete]
func (h *HTTP) unfavoriteMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if err := h.Svc.UnfavoriteMessage(u.ID, uint(id64)); err != nil {
		logger.Errorf("[Favorites] Failed to unfavorite message %d for user %d: %v", id64, u.ID, err)
		c.JSON(400, gin.H{"error": "取消收藏失败"})
		return
	}
	c.Status(204)
}

// @Summary      List favorite messages
// @Tags         favorites
// @Security     BearerAuth
// @Produce      json
// @Param        limit  query  int     false  "Limit (default 50, max 100)"
// @Param        type   query  string  false  "Filter by type: all, image, video, file"
// @Success      200   {array}  models.FavoriteMessage
// @Router       /api/favorites [get]
func (h *HTTP) listFavorites(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit <= 0 {
		limit = 50
	}
	filterType := c.Query("type") // all, image, video, file
	favorites, err := h.Svc.ListFavorites(u.ID, limit, filterType)
	if err != nil {
		logger.Errorf("[Favorites] Failed to list favorites for user %d: %v", u.ID, err)
		c.JSON(500, gin.H{"error": "获取收藏列表失败"})
		return
	}
	c.JSON(200, favorites)
}

// @Summary      Send favorite message as new message
// @Description  将收藏的消息作为新消息发送到指定频道、私聊或群聊
// @Tags         favorites
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id    path  int  true  "Favorite ID"
// @Param        body  body  object  true  "{channelId?: number, threadId?: number, groupThreadId?: number}"
// @Success      200   {object}  models.Message
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Router       /api/favorites/{id}/send [post]
func (h *HTTP) sendFavoriteAsMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	favoriteID, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if favoriteID == 0 {
		c.JSON(400, gin.H{"error": "无效的收藏ID"})
		return
	}

	var body struct {
		ChannelID     *uint `json:"channelId,omitempty"`
		ThreadID      *uint `json:"threadId,omitempty"`
		GroupThreadID *uint `json:"groupThreadId,omitempty"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}

	// 必须指定频道、私聊或群聊
	if body.ChannelID == nil && body.ThreadID == nil && body.GroupThreadID == nil {
		c.JSON(400, gin.H{"error": "必须指定 channelId、threadId 或 groupThreadId"})
		return
	}

	if body.ChannelID != nil {
		// 发送到频道
		msg, err := h.Svc.SendFavoriteToChannel(u.ID, uint(favoriteID), *body.ChannelID)
		if err != nil {
			logger.Errorf("[Favorites] Failed to send favorite %d to channel %d for user %d: %v", favoriteID, *body.ChannelID, u.ID, err)
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, msg)

		// WebSocket 广播给频道所有成员
		if h.Gw != nil {
			payload := h.buildChannelMessagePayload(msg)
			h.Gw.BroadcastToChannel(*body.ChannelID, config.EventMessageCreate, payload)
		}

		// 异步更新未读计数并广播红点
		go func() {
			if err := h.Svc.OnNewChannelMessage(*body.ChannelID, msg.ID, u.ID, []uint{}); err != nil {
				logger.Warnf("[ReadState] Failed to update unread counts for channel %d: %v", *body.ChannelID, err)
				return
			}
			h.broadcastChannelReadStateUpdates(*body.ChannelID, u.ID)
		}()
	} else if body.ThreadID != nil {
		// 发送到私聊
		dmMsg, err := h.Svc.SendFavoriteToDm(u.ID, uint(favoriteID), *body.ThreadID)
		if err != nil {
			logger.Errorf("[Favorites] Failed to send favorite %d to DM thread %d for user %d: %v", favoriteID, *body.ThreadID, u.ID, err)
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, dmMsg)

		// WebSocket 广播给私聊双方
		if h.Gw != nil {
			payload := h.buildDmMessagePayload(dmMsg)
			if th, err := h.Svc.Repo.GetDmThread(*body.ThreadID); err == nil && th != nil {
				participantIDs := []uint{th.UserAID, th.UserBID}
				h.Gw.BroadcastToDM(*body.ThreadID, config.EventDmMessageCreate, payload, participantIDs)

				// 通知对方用户刷新私聊列表
				otherUserID := th.UserAID
				if otherUserID == u.ID {
					otherUserID = th.UserBID
				}
				h.Gw.BroadcastToUsers([]uint{otherUserID}, config.EventDmMessageCreate, payload)
			} else {
				h.Gw.BroadcastToDM(*body.ThreadID, config.EventDmMessageCreate, payload, nil)
			}
		}

		// 异步更新未读计数并广播红点
		go func() {
			if err := h.Svc.OnNewDmMessage(*body.ThreadID, dmMsg.ID, u.ID, []uint{}); err != nil {
				logger.Warnf("[ReadState] Failed to update unread counts for DM thread %d: %v", *body.ThreadID, err)
				return
			}
			h.broadcastDmReadStateUpdate(*body.ThreadID, u.ID)
		}()
	} else if body.GroupThreadID != nil {
		// 发送到群聊
		groupMsg, err := h.Svc.SendFavoriteToGroupThread(u.ID, uint(favoriteID), *body.GroupThreadID)
		if err != nil {
			logger.Errorf("[Favorites] Failed to send favorite %d to group thread %d for user %d: %v", favoriteID, *body.GroupThreadID, u.ID, err)
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, groupMsg)

		// WebSocket 广播给群聊所有成员
		if h.Gw != nil {
			author, _ := h.Svc.GetUserByID(u.ID)
			payload := h.buildGroupMessagePayload(groupMsg, author)
			memberIDs, _ := h.Svc.Repo.GetGroupMemberIDs(*body.GroupThreadID)
			h.Gw.BroadcastToUsers(memberIDs, config.EventGroupMessageCreate, payload)
		}

		// 异步更新未读计数
		go func() {
			if err := h.Svc.OnNewGroupMessage(*body.GroupThreadID, groupMsg.ID, u.ID); err != nil {
				logger.Warnf("[ReadState] Failed to update unread counts for group thread %d: %v", *body.GroupThreadID, err)
			}
		}()
	}
}
