package handlers

import (
	"net/http"
	"net/url"

	"bubble/src/config"
	"bubble/src/middleware"
	"bubble/src/service"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// @Summary      List reactions
// @Tags         reactions
// @Security     BearerAuth
// @Produce      json
// @Param        id   path      int  true  "Message ID"
// @Success      200   {array}   map[string]any
// @Failure      401   {object}  map[string]string
// @Router       /api/messages/{id}/reactions [get]
// 列出消息的所有表态（聚合格式）
func (h *HTTP) listReactions(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	msgID, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	list, err := h.Svc.ListReactions(msgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	c.JSON(http.StatusOK, list)
}

// @Summary      Add reaction
// @Tags         reactions
// @Security     BearerAuth
// @Produce      json
// @Param        id     path  int     true  "Message ID"
// @Param        emoji  path  string  true  "Emoji"
// @Success      200    {object}  models.MessageReaction
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Router       /api/messages/{id}/reactions/{emoji} [put]
// 为消息添加表态
func (h *HTTP) addReaction(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	msgID, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	emoji, err := url.PathUnescape(c.Param("emoji"))
	if err != nil || emoji == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "emoji 参数错误"})
		return
	}

	reaction, err := h.Svc.AddReaction(msgID, u.ID, emoji)
	if err != nil {
		if err == service.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "消息不存在"})
		} else if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else if se, ok := err.(*service.Err); ok {
			c.JSON(se.Code, gin.H{"error": se.Msg})
		} else {
			logger.Errorf("[Reactions] Failed to add reaction on message %d: %v", msgID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		}
		return
	}

	// 广播表态新增事件
	if h.Gw != nil {
		msg, _ := h.Svc.Repo.GetMessage(msgID)
		if msg != nil {
			h.Gw.BroadcastToChannel(msg.ChannelID, config.EventMessageReactionAdd, gin.H{
				"messageId": msgID,
				"channelId": msg.ChannelID,
				"userId":    u.ID,
				"emoji":     emoji,
				"user": gin.H{
					"id":     u.ID,
					"name":   u.Name,
					"avatar": u.Avatar,
				},
			})
		}
	}

	c.JSON(http.StatusOK, reaction)
}

// @Summary      Remove reaction
// @Tags         reactions
// @Security     BearerAuth
// @Produce      json
// @Param        id     path  int     true  "Message ID"
// @Param        emoji  path  string  true  "Emoji"
// @Success      204   {string}  string  ""
// @Failure      401   {object}  map[string]string
// @Router       /api/messages/{id}/reactions/{emoji} [delete]
// 移除消息表态
func (h *HTTP) removeReaction(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	msgID, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	emoji, err := url.PathUnescape(c.Param("emoji"))
	if err != nil || emoji == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "emoji 参数错误"})
		return
	}

	if err := h.Svc.RemoveReaction(msgID, u.ID, emoji); err != nil {
		if err == service.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "消息不存在"})
		} else if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else {
			logger.Errorf("[Reactions] Failed to remove reaction on message %d: %v", msgID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		}
		return
	}

	// 广播表态移除事件
	if h.Gw != nil {
		msg, _ := h.Svc.Repo.GetMessage(msgID)
		if msg != nil {
			h.Gw.BroadcastToChannel(msg.ChannelID, config.EventMessageReactionRemove, gin.H{
				"messageId": msgID,
				"channelId": msg.ChannelID,
				"userId":    u.ID,
				"emoji":     emoji,
			})
		}
	}

	c.JSON(http.StatusNoContent, nil)
}
