package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"bubble/src/middleware"
	"bubble/src/service"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// @Summary      List incoming friend requests
// @Tags         friends
// @Security     BearerAuth
// @Produce      json
// @Success      200   {array}  map[string]any
// @Router       /api/friends/requests [get]
// listFriendRequests 列出当前用户收到的所有待处理好友请求
func (h *HTTP) listFriendRequests(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	// Support cursor params for infinite scroll
	beforeID64, _ := strconv.ParseUint(c.Query("beforeId"), 10, 64)
	afterID64, _ := strconv.ParseUint(c.Query("afterId"), 10, 64)
	beforeID := uint(beforeID64)
	afterID := uint(afterID64)

	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit < 1 || limit > 100 {
		limit = 10
	}

	if beforeID > 0 || afterID > 0 {
		users, err := h.Svc.ListFriendRequestsCursor(u.ID, limit, beforeID, afterID)
		if err != nil {
			logger.Errorf("[Friends] Failed to list friend requests (cursor) for user %d: %v", u.ID, err)
			c.JSON(500, gin.H{"error": "获取好友申请失败"})
			return
		}
		c.JSON(200, gin.H{"data": users})
		return
	}

	// Fallback to page-based pagination
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	users, total, err := h.Svc.ListFriendRequests(u.ID, page, limit)
	if err != nil {
		logger.Errorf("[Friends] Failed to list friend requests (page) for user %d: %v", u.ID, err)
		c.JSON(500, gin.H{"error": "获取好友申请失败"})
		return
	}
	c.JSON(200, gin.H{
		"data": users,
		"pagination": gin.H{
			"page":    page,
			"limit":   limit,
			"total":   total,
			"hasMore": page*limit < total,
		},
	})
}

// @Summary      List friends
// @Tags         friends
// @Security     BearerAuth
// @Produce      json
// @Success      200   {array}  map[string]any
// @Router       /api/friends [get]
// listFriends 获取当前用户所有好友列表
func (h *HTTP) listFriends(c *gin.Context) {
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
		users, err := h.Svc.ListFriendsCursor(u.ID, limit, beforeID, afterID)
		if err != nil {
			logger.Errorf("[Friends] Failed to list friends (cursor) for user %d: %v", u.ID, err)
			c.JSON(500, gin.H{"error": "获取好友列表失败"})
			return
		}
		c.JSON(200, gin.H{"data": users})
		return
	}
	// Fallback to page-based
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	users, total, err := h.Svc.ListFriends(u.ID, page, limit)
	if err != nil {
		logger.Errorf("[Friends] Failed to list friends (page) for user %d: %v", u.ID, err)
		c.JSON(500, gin.H{"error": "获取好友列表失败"})
		return
	}
	c.JSON(200, gin.H{
		"data": users,
		"pagination": gin.H{
			"page":    page,
			"limit":   limit,
			"total":   total,
			"hasMore": page*limit < total,
		},
	})
}

// @Summary      Search friends by name or ID
// @Tags         friends
// @Security     BearerAuth
// @Produce      json
// @Param        q      query string true  "搜索关键词：用户名、备注或ID"
// @Param        limit  query int    false "Limit (默认10, 最大50)"
// @Success      200    {array} map[string]any
// @Failure      400    {object} map[string]string
// @Failure      401    {object} map[string]string
// @Router       /api/friends/search [get]
func (h *HTTP) searchFriends(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		c.JSON(400, gin.H{"error": "缺少搜索关键词"})
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	friends, err := h.Svc.SearchFriends(u.ID, q, limit)
	if err != nil {
		logger.Errorf("[Friends] Failed to search friends for user %d: %v", u.ID, err)
		c.JSON(500, gin.H{"error": "搜索好友失败"})
		return
	}
	c.JSON(200, friends)
}

// @Summary      Send friend request
// @Tags         friends
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body      map[string]string  true  "{toName}"
// @Success      204   {string}  string  ""
// @Router       /api/friends/requests [post]
// sendFriendRequest 发送好友请求
func (h *HTTP) sendFriendRequest(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	var body struct {
		ToName string `json:"toName"`
		Answer string `json:"answer"` // 验证问题答案（可选）
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	toUser, notif, err := h.Svc.SendFriendRequestWithAnswer(u.ID, body.ToName, body.Answer)
	if err != nil {
		// 检查是否是友好提示（状态码200）
		if svcErr, ok := err.(*service.Err); ok {
			if svcErr.Code == 200 {
				c.JSON(200, gin.H{"message": svcErr.Msg})
				return
			}
			// 需要回答验证问题
			if svcErr.Msg == "需要回答验证问题" && svcErr.Data != nil {
				c.JSON(400, gin.H{
					"error":    svcErr.Msg,
					"question": svcErr.Data["question"],
				})
				return
			}
			// 其他service错误
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
			return
		}
		logger.Errorf("[Friends] Failed to send friend request from user %d to %s: %v", u.ID, body.ToName, err)
		c.JSON(400, gin.H{"error": "发送好友申请失败"})
		return
	}

	// 如果创建了通知，推送给接收者
	if notif != nil && toUser != nil && h.Gw != nil {
		notifPayload := gin.H{
			"id":         notif.ID,
			"userId":     notif.UserID,
			"type":       notif.Type,
			"sourceType": notif.SourceType,
			"status":     "pending",
			"read":       false,
			"createdAt":  notif.CreatedAt,
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

		h.Gw.BroadcastNotice(toUser.ID, notifPayload)
	}

	c.Status(204)
}

// @Summary      Accept friend request
// @Tags         friends
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body  body      map[string]string  true  "{fromName}"
// @Success      204   {string}  string  ""
// @Router       /api/friends/accept [post]
// acceptFriendRequest 接受来自指定用户名的好友请求。
// Method/Path: POST /api/friends/accept
// 请求体: {"fromName": "请求发起方用户名"}
// 响应: 204 成功建立好友关系。
// 错误: 401 未认证；400 请求不存在/已过期。
// acceptFriendRequest 接受好友请求
func (h *HTTP) acceptFriendRequest(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	var body struct {
		FromName string `json:"fromName"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if err := h.Svc.AcceptFriendRequest(u.ID, body.FromName); err != nil {
		if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "好友请求不存在或已过期"})
		} else if err == service.ErrBadRequest {
			c.JSON(400, gin.H{"error": "好友请求不存在或已处理"})
		} else {
			logger.Errorf("[Friends] Failed to accept friend request from %s to user %d: %v", body.FromName, u.ID, err)
			c.JSON(500, gin.H{"error": "接受好友申请失败"})
		}
		return
	}
	c.Status(204)
}

// @Summary      Remove friend
// @Tags         friends
// @Security     BearerAuth
// @Produce      json
// @Param        userId  query int true "Friend User ID"
// @Success      204   {string}  string  ""
// @Router       /api/friends [delete]
// removeFriend 解除好友关系
func (h *HTTP) removeFriend(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	fid64, _ := strconv.ParseUint(c.Query("userId"), 10, 64)
	if err := h.Svc.RemoveFriend(u.ID, uint(fid64)); err != nil {
		// 删除好友时，即使已经不是好友也视为成功
		// 避免用户重复点击时收到错误提示
	}
	c.Status(204)
}

// @Summary      Set friend nickname
// @Tags         friends
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        userId path int true "Friend User ID"
// @Param        body  body      map[string]string  true  "{nickname}"
// @Success      204   {string}  string  ""
// @Router       /api/friends/{userId}/nickname [put]
// setFriendNickname 设置好友备注
// @Summary      Set friend nickname
// @Description  设置好友备注
// @Tags         friends
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        userId  path  int     true  "Friend user ID"
// @Param        body    body  object  true  "{nickname}"
// @Success      200     {object}  map[string]any
// @Failure      400     {object}  map[string]string  "参数错误"
// @Failure      401     {object}  map[string]string  "未认证"
// @Failure      403     {object}  map[string]string  "不是好友关系"
// @Failure      404     {object}  map[string]string  "用户不存在"
// @Failure      500     {object}  map[string]string  "服务器错误"
// @Router       /api/friends/{id}/nickname [put]
// setFriendPrivacyMode 设置对好友的隐私模式
func (h *HTTP) setFriendPrivacyMode(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	friendID64, err := strconv.ParseUint(c.Param("userId"), 10, 64)
	if err != nil || friendID64 == 0 {
		c.JSON(400, gin.H{"error": "无效的用户ID"})
		return
	}
	friendID := uint(friendID64)

	var body struct {
		Mode string `json:"mode"` // normal | chat_only
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}

	body.Mode = strings.TrimSpace(body.Mode)
	if body.Mode != "normal" && body.Mode != "chat_only" {
		c.JSON(400, gin.H{"error": "隐私模式只能是 normal 或 chat_only"})
		return
	}

	if err := h.Svc.SetFriendPrivacyMode(u.ID, friendID, body.Mode); err != nil {
		if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
			return
		}
		logger.Errorf("[Friends] Failed to set privacy mode for user %d -> friend %d: %v", u.ID, friendID, err)
		c.JSON(500, gin.H{"error": "设置隐私模式失败"})
		return
	}

	c.JSON(200, gin.H{"message": "设置成功"})
}

// setFriendRequestMode 设置加好友验证模式
func (h *HTTP) setFriendRequestMode(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	var body struct {
		Mode     string `json:"mode"`               // need_approval | everyone | need_question
		Question string `json:"question,omitempty"` // 验证问题（mode为need_question时必填）
		Answer   string `json:"answer,omitempty"`   // 验证答案（mode为need_question时必填）
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}

	if err := h.Svc.SetFriendRequestMode(u.ID, body.Mode, body.Question, body.Answer); err != nil {
		if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
			return
		}
		logger.Errorf("[Friends] Failed to set friend request mode for user %d: %v", u.ID, err)
		c.JSON(500, gin.H{"error": "设置验证模式失败"})
		return
	}

	c.JSON(200, gin.H{"message": "设置成功"})
}

func (h *HTTP) setFriendNickname(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	friendID64, _ := strconv.ParseUint(c.Param("userId"), 10, 64)
	friendID := uint(friendID64)
	if friendID == 0 {
		c.JSON(400, gin.H{"error": "无效的用户ID"})
		return
	}
	var body struct {
		Nickname string `json:"nickname"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}
	if err := h.Svc.SetFriendNickname(u.ID, friendID, body.Nickname); err != nil {
		logger.Errorf("[Friends] Failed to set friend nickname for user %d, friend %d: %v", u.ID, friendID, err)
		c.JSON(400, gin.H{"error": "设置好友备注失败"})
		return
	}
	c.Status(204)
}
