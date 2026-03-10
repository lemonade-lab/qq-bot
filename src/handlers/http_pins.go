package handlers

import (
	"net/http"

	"bubble/src/config"
	"bubble/src/db/models"
	"bubble/src/middleware"
	"bubble/src/service"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// @Summary      List pinned messages
// @Tags         pins
// @Security     BearerAuth
// @Produce      json
// @Param        id   path      int  true  "Channel ID"
// @Success      200   {array}   models.PinnedMessage
// @Failure      401   {object}  map[string]string
// @Router       /api/channels/{id}/pins [get]
// 列出频道精华消息
func (h *HTTP) listPinnedMessages(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	cid, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	list, err := h.Svc.ListPinnedMessages(cid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	c.JSON(http.StatusOK, list)
}

// @Summary      Pin message
// @Tags         pins
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path      int  true  "Channel ID"
// @Param        body  body      map[string]int  true  "{messageId}"
// @Success      200   {object}  models.PinnedMessage
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Failure      403   {object}  map[string]string
// @Router       /api/channels/{id}/pins [post]
// 频道消息设为精华
func (h *HTTP) pinMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	cid, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	var body struct {
		MessageID uint `json:"messageId"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	pm, err := h.Svc.PinMessage(cid, body.MessageID, u.ID)
	if err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else if err == service.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "消息不存在"})
		} else {
			logger.Errorf("[Pins] Failed to pin message in channel %d: %v", cid, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "设置精华失败"})
		}
		return
	}
	// 广播频道精华新增
	if h.Gw != nil {
		h.Gw.BroadcastToChannel(cid, config.EventMessagePin, gin.H{"id": pm.MessageID, "channelId": cid})
	}
	c.JSON(http.StatusOK, pm)
}

// @Summary      Unpin message
// @Tags         pins
// @Security     BearerAuth
// @Produce      json
// @Param        id   path      int  true  "Channel ID"
// @Param        messageId   path      int  true  "Message ID"
// @Success      204   {string}  string  ""
// @Failure      401   {object}  map[string]string
// @Failure      403   {object}  map[string]string
// @Failure      404   {object}  map[string]string
// @Router       /api/channels/{id}/pins/{messageId} [delete]
// 取消频道精华
func (h *HTTP) unpinMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	channelID, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	messageID, err := parseUintParam(c, "messageId")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	if err := h.Svc.UnpinMessage(messageID, u.ID); err != nil {
		if err == service.ErrNotFound {
			// 消息不存在或已经取消精华，视为成功
			c.Status(http.StatusNoContent)
			return
		} else if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
			return
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
			return
		}
	}
	if h.Gw != nil {
		h.Gw.BroadcastToChannel(channelID, config.EventMessageUnpin, gin.H{"id": messageID, "channelId": channelID})
	}
	c.Status(http.StatusNoContent)
}

// @Summary      List pinned DM messages
// @Tags         pins
// @Security     BearerAuth
// @Produce      json
// @Param        id   path      int  true  "Thread ID"
// @Success      200   {array}   models.PinnedMessage
// @Failure      401   {object}  map[string]string
// @Router       /api/dm/threads/{id}/pins [get]
// 列出私信精华消息
// @Summary      List pinned DM messages
// @Description  列出私聊中的置顶消息
// @Tags         pins
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id  path  int  true  "DM Thread ID"
// @Success      200  {array}   models.Message
// @Failure      400  {object}  map[string]string  "参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      403  {object}  map[string]string  "权限不足"
// @Failure      404  {object}  map[string]string  "会话不存在"
// @Failure      500  {object}  map[string]string  "服务器错误"
// @Router       /api/dm/threads/{id}/pins [get]
func (h *HTTP) listPinnedDmMessages(c *gin.Context) {
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
	list, err := h.Svc.ListPinnedDmMessages(tid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	c.JSON(http.StatusOK, list)
}

// @Summary      Pin DM message
// @Tags         pins
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path      int  true  "Thread ID"
// @Param        body  body      map[string]int  true  "{messageId}"
// @Success      200   {object}  models.PinnedMessage
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Router       /api/dm/threads/{id}/pins [post]
// 私信消息设为精华
// @Summary      Pin DM message
// @Description  置顶私聊消息
// @Tags         pins
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id         path  int  true  "DM Thread ID"
// @Param        messageId  path  int  true  "Message ID"
// @Success      200        {object}  map[string]string
// @Failure      400        {object}  map[string]string  "参数错误"
// @Failure      401        {object}  map[string]string  "未认证"
// @Failure      403        {object}  map[string]string  "权限不足"
// @Failure      404        {object}  map[string]string  "消息不存在"
// @Failure      500        {object}  map[string]string  "服务器错误"
// @Router       /api/dm/threads/{id}/pins/{messageId} [put]
func (h *HTTP) pinDmMessage(c *gin.Context) {
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
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	pm, err := h.Svc.PinDmMessage(tid, body.MessageID, u.ID)
	if err != nil {
		if err == service.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "消息或会话不存在"})
		} else if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else {
			logger.Errorf("[Pins] Failed to pin DM message in thread %d: %v", tid, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "设置精华失败"})
		}
		return
	}
	// 广播私信精华新增（带参与者，确保默认用户订阅收到）
	if h.Gw != nil {
		var t models.DmThread
		if err := h.Svc.Repo.DB.First(&t, tid).Error; err == nil {
			participantIDs := []uint{t.UserAID, t.UserBID}
			h.Gw.BroadcastToDM(tid, config.EventDmMessagePin, gin.H{"id": pm.MessageID, "threadId": tid}, participantIDs)
		} else {
			h.Gw.BroadcastToDM(tid, config.EventDmMessagePin, gin.H{"id": pm.MessageID, "threadId": tid}, nil)
		}
	}
	c.JSON(http.StatusOK, pm)
}

// @Summary      Unpin DM message
// @Tags         pins
// @Security     BearerAuth
// @Produce      json
// @Param        id   path      int  true  "Thread ID"
// @Param        messageId   path      int  true  "Message ID"
// @Success      204   {string}  string  ""
// @Failure      401   {object}  map[string]string
// @Failure      404   {object}  map[string]string
// @Router       /api/dm/threads/{id}/pins/{messageId} [delete]
// 取消私信精华
// @Summary      Unpin DM message
// @Description  取消置顶私聊消息
// @Tags         pins
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id         path  int  true  "DM Thread ID"
// @Param        messageId  path  int  true  "Message ID"
// @Success      200        {object}  map[string]string
// @Failure      400        {object}  map[string]string  "参数错误"
// @Failure      401        {object}  map[string]string  "未认证"
// @Failure      403        {object}  map[string]string  "权限不足"
// @Failure      404        {object}  map[string]string  "消息不存在"
// @Failure      500        {object}  map[string]string  "服务器错误"
// @Router       /api/dm/threads/{id}/pins/{messageId} [delete]
func (h *HTTP) unpinDmMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	threadID, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	messageID, err := parseUintParam(c, "messageId")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	if err := h.Svc.UnpinMessage(messageID, u.ID); err != nil {
		if err == service.ErrNotFound {
			// 消息不存在或已经取消精华，视为成功
			c.Status(http.StatusNoContent)
			return
		} else if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
			return
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
			return
		}
	}
	if h.Gw != nil {
		// 获取线程参与者并传入 Gateway
		var t models.DmThread
		if err := h.Svc.Repo.DB.First(&t, threadID).Error; err == nil {
			participantIDs := []uint{t.UserAID, t.UserBID}
			h.Gw.BroadcastToDM(threadID, config.EventDmMessageUnpin, gin.H{"id": messageID, "threadId": threadID}, participantIDs)
		} else {
			h.Gw.BroadcastToDM(threadID, config.EventDmMessageUnpin, gin.H{"id": messageID, "threadId": threadID}, nil)
		}
	}
	c.Status(http.StatusNoContent)
}
