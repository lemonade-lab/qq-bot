package handlers

import (
	"encoding/json"
	"fmt"
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

// @Summary      List my DM threads
// @Tags         dm
// @Security     BearerAuth
// @Produce      json
// @Success      200  {array}  map[string]any
// @Router       /api/dm/threads [get]
// listDmThreads 列出当前用户参与的所有私信线程
func (h *HTTP) listDmThreads(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	// Cursor-first support
	beforeID64, _ := strconv.ParseUint(c.Query("beforeId"), 10, 64)
	afterID64, _ := strconv.ParseUint(c.Query("afterId"), 10, 64)
	beforeID := uint(beforeID64)
	afterID := uint(afterID64)
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit < 1 || limit > 100 {
		limit = 10
	}
	if beforeID > 0 || afterID > 0 {
		threads, err := h.Svc.ListDmThreadsCursor(u.ID, limit, beforeID, afterID)
		if err != nil {
			logger.Errorf("[DM] Failed to list threads (cursor) for user %d: %v", u.ID, err)
			c.JSON(500, gin.H{"error": "获取私信列表失败"})
			return
		}
		// 补充mentions占位
		for i := range threads {
			if threads[i].Mentions == nil {
				threads[i].Mentions = make([]map[string]any, 0)
			}
		}
		c.JSON(200, gin.H{"data": threads})
		return
	}
	// Fallback to page-based
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	threads, total, err := h.Svc.ListDmThreads(u.ID, page, limit)
	if err != nil {
		logger.Errorf("[DM] Failed to list threads (page) for user %d: %v", u.ID, err)
		c.JSON(500, gin.H{"error": "获取私信列表失败"})
		return
	}
	for i := range threads {
		if threads[i].Mentions == nil {
			threads[i].Mentions = make([]map[string]any, 0)
		}
	}
	c.JSON(200, gin.H{
		"data": threads,
		"pagination": gin.H{
			"page":    page,
			"limit":   limit,
			"total":   total,
			"hasMore": page*limit < total,
		},
	})
}

// --- Direct Messages ---
// @Summary      Open or get DM thread
// @Tags         dm
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body body  map[string]any true "{toUserId, guildId?}"
// @Success      200  {object}  map[string]any
// @Router       /api/dm/open [post]
// openDm 打开或复用一个与指定用户的私信线程。
// Method/Path: POST /api/dm/open
// 认证: 需要 Bearer Token。
// 请求体: {"toUserId": <uint>, "guildId"?: <uint>}
//   - toUserId: 必填，对方用户 ID
//   - guildId: 可选，共同服务器ID（用于验证是否在同一服务器）
//
// 权限: 无额外权限要求，只要合法登陆用户即可；服务层会检查目标用户是否存在。
// 业务逻辑:
//   - 允许和自己创建（笔记本）
//   - 如果是好友，直接允许
//   - 如果传入guildId且在同一服务器，允许创建
//   - 检查对方的私聊隐私设置（friends_only 或 everyone）
//   - 若双方已有现有线程则复用返回；否则创建新线程。
//
// 响应: 200 返回线程对象字段 (id, participants, lastMessage 等具体以实现为准)。
// 常见错误:
//   - 401 未认证
//   - 400 body 解析失败 / 服务层返回的业务错误 (如用户不存在)
//   - 403 对方设置为仅好友可发起私聊
//
// 注意: 当前无速率限制单独策略，若需要防止滥用，可在上层加入频率控制。
func (h *HTTP) openDm(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	var body struct {
		ToUserID uint  `json:"toUserId"`
		GuildID  *uint `json:"guildId,omitempty"`
	}
	if err := c.BindJSON(&body); err != nil || body.ToUserID == 0 {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	t, err := h.Svc.OpenDm(u.ID, body.ToUserID, body.GuildID)
	if err != nil {
		if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
			return
		}
		logger.Errorf("[DM] Failed to open DM from user %d to %d: %v", u.ID, body.ToUserID, err)
		c.JSON(400, gin.H{"error": "创建私信失败"})
		return
	}
	c.JSON(200, t)
}

// @Summary      Pin/Unpin DM thread
// @Tags         dm
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Thread ID"
// @Param        body body  object{pinned=bool}  true  "Pin status"
// @Success      200
// @Router       /api/dm/threads/{id}/pin [put]
func (h *HTTP) pinDmThread(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	threadID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的私信线程ID"})
		return
	}

	var body struct {
		Pinned bool `json:"pinned"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "无效的JSON"})
		return
	}

	if err := h.Svc.PinDmThread(uint(threadID), u.ID, body.Pinned); err != nil {
		logger.Errorf("[DM] Failed to pin thread %d for user %d: %v", threadID, u.ID, err)
		c.JSON(400, gin.H{"error": "设置置顶失败"})
		return
	}

	c.JSON(200, gin.H{})
}

// @Summary      Block/Unblock DM thread
// @Tags         dm
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Thread ID"
// @Param        body body  object{blocked=bool}  true  "Block status"
// @Success      200
// @Router       /api/dm/threads/{id}/block [put]
func (h *HTTP) blockDmThread(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	threadID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的私信线程ID"})
		return
	}

	var body struct {
		Blocked bool `json:"blocked"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "无效的JSON"})
		return
	}

	if err := h.Svc.BlockDmThread(uint(threadID), u.ID, body.Blocked); err != nil {
		logger.Errorf("[DM] Failed to block thread %d for user %d: %v", threadID, u.ID, err)
		c.JSON(400, gin.H{"error": "设置屏蔽失败"})
		return
	}

	c.JSON(200, gin.H{})
}

// @Summary      List DM messages
// @Description  获取指定私信线程中的消息列表，支持分页方向
// @Tags         dm
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        threadId query int true "Thread ID"
// @Param        limit    query int false "Limit (default 50, max 100)"
// @Param        beforeId query int false "id < beforeId"
// @Param        afterId  query int false "id > afterId"
// @Success      200  {array}  map[string]any
// @Failure      400  {object}  map[string]string  "参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Router       /api/dm/messages [get]
func (h *HTTP) getDmMessages(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	tid, _ := strconv.ParseUint(c.Query("threadId"), 10, 64)
	limit, _ := strconv.Atoi(c.Query("limit"))
	before, _ := strconv.ParseUint(c.Query("beforeId"), 10, 64)
	after, _ := strconv.ParseUint(c.Query("afterId"), 10, 64)
	if limit < 1 || limit > 100 {
		limit = 50
	}
	list, err := h.Svc.GetDmMessages(u.ID, uint(tid), limit, uint(before), uint(after))
	if err != nil {
		logger.Errorf("[DM] Failed to get messages for user %d, thread %d: %v", u.ID, tid, err)
		c.JSON(400, gin.H{"error": "获取消息失败"})
		return
	}
	// 判断是否还有更多数据
	hasMore := len(list) >= limit
	var nextCursor uint
	if hasMore && len(list) > 0 {
		// 向前翻页（beforeId）：nextCursor = 最小的 ID
		// 向后翻页（afterId）：nextCursor = 最大的 ID
		if before > 0 {
			nextCursor = list[len(list)-1].ID
		} else {
			nextCursor = list[len(list)-1].ID
		}
	}
	c.JSON(200, gin.H{
		"messages":   list,
		"hasMore":    hasMore,
		"nextCursor": nextCursor,
	})
}

// @Summary      List DM messages with users
// @Description  获取私聊消息列表，包含用户数据
// @Tags         dm
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        threadId  query  int  true   "Thread ID"
// @Param        limit     query  int  false  "Limit (default 50, max 100)"
// @Param        beforeId  query  int  false  "Return messages with id < beforeId (page up)"
// @Param        afterId   query  int  false  "Return messages with id > afterId (page down/live)"
// @Success      200       {object}  map[string]any  "{\"messages\": [], \"users\": []}"
// @Failure      400       {object} map[string]string  "参数错误"
// @Failure      401       {object} map[string]string  "未认证"
// @Router       /api/dm/messages/with-users [get]
func (h *HTTP) getDmMessagesWithUsers(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	tid, _ := strconv.ParseUint(c.Query("threadId"), 10, 64)
	if tid == 0 {
		c.JSON(400, gin.H{"error": "缺少必要参数"})
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit < 1 || limit > 100 {
		limit = 50
	}
	before, _ := strconv.ParseUint(c.Query("beforeId"), 10, 64)
	after, _ := strconv.ParseUint(c.Query("afterId"), 10, 64)
	messages, users, err := h.Svc.GetDmMessagesWithUsers(u.ID, uint(tid), limit, uint(before), uint(after))
	if err != nil {
		logger.Errorf("[DM] Failed to get messages with users for user %d, thread %d: %v", u.ID, tid, err)
		c.JSON(400, gin.H{"error": "获取消息失败"})
		return
	}
	// 判断是否还有更多数据
	hasMore := len(messages) >= limit
	var nextCursor uint
	if hasMore && len(messages) > 0 {
		// 向前翻页（beforeId）：nextCursor = 最小的 ID
		// 向后翻页（afterId）：nextCursor = 最大的 ID
		if before > 0 {
			nextCursor = messages[len(messages)-1].ID
		} else {
			nextCursor = messages[len(messages)-1].ID
		}
	}
	c.JSON(200, gin.H{
		"messages":   messages,
		"users":      users,
		"hasMore":    hasMore,
		"nextCursor": nextCursor,
	})
}

// @Summary      Send DM message
// @Tags         dm
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body body  map[string]any true "{threadId,content,replyToId}"
// @Success      200  {object}  map[string]any
// @Router       /api/dm/messages [post]
// postDmMessage 在指定私信线程里发送一条消息。
// Method/Path: POST /api/dm/messages
// 认证: Bearer Token。
// 请求体: {"threadId": <uint>, "content": "文本", "replyToId": <uint,optional>} content 不可为空白（服务层可二次校验）。
// 权限: 线程参与者即可。
// 响应: 200 返回创建后的消息对象。
// 侧效: 若 Gateway 已启用，广播事件 config.EventDmMessageCreate 到该线程在线连接。
// 错误: 401 未认证；400 body 解析失败 / 服务层校验失败。
// 建议: 可后续加入消息长度限制与内容过滤（脏词、XSS）。
func (h *HTTP) postDmMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	var body struct {
		ThreadID  uint        `json:"threadId"`
		Content   string      `json:"content"`
		ReplyToID *uint       `json:"replyToId"`
		Type      string      `json:"type"`
		Platform  string      `json:"platform"` // optional: web, mobile, desktop, 默认web
		FileMeta  interface{} `json:"fileMeta"`
		TempID    string      `json:"tempId"` // optional: 临时消息ID
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
	// 校验消息长度
	if len([]rune(body.Content)) > int(config.MaxMessageLength) {
		c.JSON(400, gin.H{"error": "消息过长"})
		return
	}
	m, err := h.Svc.SendDm(u.ID, body.ThreadID, body.Content, body.ReplyToID, msgType, platform, jm, body.TempID)
	if err != nil {
		logger.Errorf("[DM] Failed to send DM from user %d to thread %d: %v", u.ID, body.ThreadID, err)
		c.JSON(400, gin.H{"error": "发送消息失败"})
		return
	}

	// 检测并异步转换音频文件（不阻塞响应）
	if body.FileMeta != nil {
		h.handleAudioConversion(c.Request.Context(), body.FileMeta, u.ID, nil)
	}

	c.JSON(200, m)
	if h.Gw != nil {
		payload := h.buildDmMessagePayload(m)
		// 获取线程参与者，传入 Gateway 避免 Gateway 做 DB 查询
		var t models.DmThread
		if err := h.Svc.Repo.DB.First(&t, body.ThreadID).Error; err == nil {
			participantIDs := []uint{t.UserAID, t.UserBID}
			// 广播给已订阅此线程的用户以及参与者的 user: topic
			h.Gw.BroadcastToDM(body.ThreadID, config.EventDmMessageCreate, payload, participantIDs)

			// 通知对方用户刷新私聊列表
			otherUserID := t.UserAID
			if otherUserID == u.ID {
				otherUserID = t.UserBID
			}
			h.Gw.BroadcastToUsers([]uint{otherUserID}, config.EventDmMessageCreate, payload)

			// 更新红点系统：为对方用户增加未读计数
			go func() {
				if err := h.Svc.OnNewDmMessage(body.ThreadID, m.ID, u.ID, []uint{}); err != nil {
					logger.Warnf("[ReadState] Failed to update unread counts for DM thread %d: %v", body.ThreadID, err)
					return
				}
				h.broadcastDmReadStateUpdate(body.ThreadID, u.ID)
			}()
		} else {
			// 回退到不带 participantIDs 的调用（保持兼容）
			h.Gw.BroadcastToDM(body.ThreadID, config.EventDmMessageCreate, payload, nil)
		}
	}
}

// @Summary      Update DM message
// @Description  编辑一条私聊消息（仅作者）并广播 DM_MESSAGE_UPDATE
// @Tags         dm
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Message ID"
// @Param        body body  object{content=string} true "Updated content"
// @Success      200  {object}  models.DmMessage
// @Failure      400  {object}  map[string]string  "参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      403  {object}  map[string]string  "非作者"
// @Failure      404  {object}  map[string]string  "消息不存在"
// @Router       /api/dm/messages/{id} [put]
func (h *HTTP) updateDmMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	mid64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if mid64 == 0 {
		c.JSON(400, gin.H{"error": "无效的消息ID"})
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	// 长度校验
	if len([]rune(body.Content)) > int(config.MaxMessageLength) {
		c.JSON(400, gin.H{"error": "消息过长"})
		return
	}
	// 读取消息并校验作者
	var msg models.DmMessage
	if err := h.Svc.Repo.DB.First(&msg, uint(mid64)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "消息不存在"})
		return
	}
	if msg.AuthorID != u.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "仅作者可操作"})
		return
	}
	// 更新内容
	now := time.Now()
	if err := h.Svc.Repo.DB.Model(&msg).Updates(map[string]interface{}{
		"content":   body.Content,
		"edited_at": &now,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新失败"})
		return
	}
	// 重新加载以返回最新
	h.Svc.Repo.DB.First(&msg, msg.ID)
	c.JSON(200, msg)
	// 广播更新事件到线程与参与者默认订阅
	if h.Gw != nil {
		payload := h.buildDmMessagePayload(&msg)
		var t models.DmThread
		if err := h.Svc.Repo.DB.First(&t, msg.ThreadID).Error; err == nil {
			participants := []uint{t.UserAID, t.UserBID}
			h.Gw.BroadcastToDM(msg.ThreadID, config.EventDmMessageUpdate, payload, participants)
		} else {
			h.Gw.BroadcastToDM(msg.ThreadID, config.EventDmMessageUpdate, payload, nil)
		}
	}
}

// @Summary      Delete DM thread
// @Description  软删除：只隐藏线程，不删除聊天记录
// @Tags         dm
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path      int  true  "Thread ID"
// @Success      204   {string}  string  ""
// @Failure      401   {object}  map[string]string  "未认证"
// @Failure      403   {object}  map[string]string  "非参与者"
// @Failure      404   {object}  map[string]string  "线程不存在"
// @Router       /api/dm/threads/{id} [delete]
func (h *HTTP) deleteDmThread(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	threadID := uint(id)

	// Check if user is a participant
	var t models.DmThread
	if err := h.Svc.Repo.DB.First(&t, threadID).Error; err != nil {
		// 线程不存在也视为删除成功
		c.Status(http.StatusNoContent)
		return
	}
	if !(t.UserAID == u.ID || t.UserBID == u.ID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "非参与者"})
		return
	}

	// 软删除：只隐藏线程，不删除聊天记录
	updates := make(map[string]interface{})
	if t.UserAID == u.ID {
		updates["hidden_by_user_a"] = true
	} else {
		updates["hidden_by_user_b"] = true
	}
	if err := h.Svc.Repo.DB.Model(&t).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "隐藏会话失败"})
		return
	}

	c.Status(http.StatusNoContent)
}

// @Summary      Delete DM message (recall)
// @Description  撤回私聊消息（只能撤回自己的消息）
// @Tags         dm
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Message ID"
// @Success      204  {string}  string  "no content"
// @Failure      400  {object}  map[string]string  "消息已撤回"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      403  {object}  map[string]string  "权限不足（非消息作者）"
// @Failure      404  {object}  map[string]string  "消息不存在"
// @Router       /api/dm/messages/{id} [delete]
func (h *HTTP) deleteDmMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	messageID := uint(id)

	// 获取消息以获取threadID用于广播
	msg, err := h.Svc.Repo.GetDmMessage(messageID)
	if err != nil {
		// 消息不存在也视为删除成功
		c.Status(204)
		return
	}

	// 撤回消息
	if err := h.Svc.DeleteDmMessage(messageID, u.ID); err != nil {
		if err == service.ErrNotFound {
			// 消息不存在也视为删除成功
			c.Status(204)
			return
		} else if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "只能撤回自己的消息"})
			return
		} else {
			logger.Errorf("[DM] Failed to delete message %d by user %d: %v", messageID, u.ID, err)
			c.JSON(400, gin.H{"error": "删除消息失败"})
			return
		}
	}

	// 广播撤回事件
	if h.Gw != nil {
		payload := h.buildDmMessagePayload(msg)
		payload["deleted"] = true
		var t models.DmThread
		if err := h.Svc.Repo.DB.First(&t, msg.ThreadID).Error; err == nil {
			participantIDs := []uint{t.UserAID, t.UserBID}
			h.Gw.BroadcastToDM(msg.ThreadID, config.EventDmMessageDelete, payload, participantIDs)

			otherUserID := t.UserAID
			if otherUserID == u.ID {
				otherUserID = t.UserBID
			}
			h.Gw.BroadcastToUsers([]uint{otherUserID}, config.EventDmMessageDelete, payload)
		} else {
			h.Gw.BroadcastToDM(msg.ThreadID, config.EventDmMessageDelete, payload, nil)
		}
	}

	c.Status(204)
}

// batchDeleteDmMessages 批量撤回私聊消息
// @Summary      Batch delete DM messages
// @Description  批量撤回私聊消息（仅消息作者可撤回）
// @Tags         dm
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "{messageIds: [uint]}"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Router       /api/dm/messages/batch-delete [post]
func (h *HTTP) batchDeleteDmMessages(c *gin.Context) {
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
		// 获取消息用于广播
		msg, err := h.Svc.Repo.GetDmMessage(mid)
		if err != nil {
			succeeded = append(succeeded, mid)
			continue
		}

		if err := h.Svc.DeleteDmMessage(mid, u.ID); err != nil {
			reason := "撤回失败"
			if err == service.ErrUnauthorized {
				reason = "只能撤回自己的消息"
			}
			failed = append(failed, gin.H{"messageId": mid, "error": reason})
			continue
		}

		succeeded = append(succeeded, mid)

		// 广播撤回事件
		if h.Gw != nil {
			payload := h.buildDmMessagePayload(msg)
			payload["deleted"] = true
			var t models.DmThread
			if err := h.Svc.Repo.DB.First(&t, msg.ThreadID).Error; err == nil {
				participantIDs := []uint{t.UserAID, t.UserBID}
				h.Gw.BroadcastToDM(msg.ThreadID, config.EventDmMessageDelete, payload, participantIDs)

				otherUserID := t.UserAID
				if otherUserID == u.ID {
					otherUserID = t.UserBID
				}
				h.Gw.BroadcastToUsers([]uint{otherUserID}, config.EventDmMessageDelete, payload)
			} else {
				h.Gw.BroadcastToDM(msg.ThreadID, config.EventDmMessageDelete, payload, nil)
			}
		}
	}

	c.JSON(200, gin.H{
		"succeeded": succeeded,
		"failed":    failed,
	})
}

// @Summary      Get LiveKit token for DM thread
// @Description  获取 LiveKit 房间 Token（用于私聊语音/视频）
// @Tags         dm
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "DM Thread ID"
// @Success      200  {object}  map[string]any
// @Failure      400  {object}  map[string]string  "参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      403  {object}  map[string]string  "非参与者"
// @Failure      404  {object}  map[string]string  "线程不存在"
// @Failure      500  {object}  map[string]string  "服务器错误"
// @Failure      503  {object}  map[string]string  "音视频服务未配置"
// @Router       /api/dm/threads/{id}/livekit-token [get]
func (h *HTTP) getDmLiveKitToken(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	if h.Svc.LiveKit == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "音视频服务未配置"})
		return
	}

	// 解析线程ID
	threadID64, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || threadID64 == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的私信线程ID"})
		return
	}
	threadID := uint(threadID64)

	// 校验参与者资格
	var t models.DmThread
	if err := h.Svc.Repo.DB.First(&t, threadID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "私信线程不存在"})
		return
	}
	if !(t.UserAID == u.ID || t.UserBID == u.ID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "非参与者"})
		return
	}

	// 生成房间名并签发 Token
	roomName := fmt.Sprintf("dm_%d", threadID)

	// 确保房间存在：若不存在则创建（最多 10 人，便于后续临时加入）
	// 与频道不同，DM 默认不落库，但房间在 LiveKit 侧应存在以支持后续多人加入
	if h.Svc.LiveKit != nil {
		ctx := c.Request.Context()
		if _, err := h.Svc.LiveKit.GetRoom(ctx, roomName); err != nil {
			// 房间不存在则创建一个轻量房间
			if _, cerr := h.Svc.LiveKit.CreateRoom(ctx, roomName, 10); cerr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "创建/获取私信房间失败"})
				return
			}
		}
	}
	canPublish := true
	canSubscribe := true

	token, err := h.Svc.LiveKit.GenerateRoomToken(u.ID, u.Name, roomName, canPublish, canSubscribe)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成令牌失败"})
		return
	}

	c.JSON(200, gin.H{
		"token":    token,
		"url":      h.Svc.LiveKit.GetURL(),
		"roomName": roomName,
	})
}

// buildDmMessagePayload 统一封装私聊消息载荷
func (h *HTTP) buildDmMessagePayload(msg *models.DmMessage) gin.H {
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

	// tempId 用于前端去重，避免临时消息与真实消息重复显示
	if msg.TempID != "" {
		payload["tempId"] = msg.TempID
	}

	// 添加作者完整用户信息
	if usr, err := h.Svc.GetUserByID(msg.AuthorID); err == nil && usr != nil {
		authorInfo := gin.H{
			"id":     usr.ID,
			"name":   usr.Name,
			"avatar": usr.Avatar,
			"status": usr.Status,
			"isBot":  usr.IsBot,
		}
		payload["authorInfo"] = authorInfo
	}

	// participants（占位/填充）：根据线程解析
	if th, err := h.Svc.Repo.GetDmThread(msg.ThreadID); err == nil && th != nil {
		payload["participants"] = []uint{th.UserAID, th.UserBID}

		// 添加线程完整信息
		threadInfo := gin.H{
			"id":         th.ID,
			"userAId":    th.UserAID,
			"userBId":    th.UserBID,
			"isNotebook": th.IsNotebook,
		}
		if th.LastMessageAt != nil {
			threadInfo["lastMessageAt"] = th.LastMessageAt
		}
		// 为前端设备视图分组提供Notebook线程的设备信息
		if th.IsNotebook {
			payload["isNotebook"] = true
			if strings.TrimSpace(th.DeviceType) != "" {
				threadInfo["deviceType"] = th.DeviceType
				payload["deviceType"] = th.DeviceType
			}
			if strings.TrimSpace(th.DeviceID) != "" {
				threadInfo["deviceId"] = th.DeviceID
				payload["deviceId"] = th.DeviceID
			}
		}

		// 添加参与者的完整用户信息
		usersInfo := make([]gin.H, 0, 2)
		for _, uid := range []uint{th.UserAID, th.UserBID} {
			if usr, err := h.Svc.GetUserByID(uid); err == nil && usr != nil {
				usersInfo = append(usersInfo, gin.H{
					"id":     usr.ID,
					"name":   usr.Name,
					"avatar": usr.Avatar,
					"status": usr.Status,
				})
			}
		}
		threadInfo["users"] = usersInfo
		payload["threadInfo"] = threadInfo
	} else {
		payload["participants"] = []uint{}
	}
	// mentions: 从数据库加载的 JSON 字段
	if len(msg.Mentions) > 0 {
		payload["mentions"] = msg.Mentions
	} else {
		payload["mentions"] = []map[string]any{}
	}
	return payload
}
