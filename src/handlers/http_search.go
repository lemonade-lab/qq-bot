package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"bubble/src/middleware"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// globalSearch 全局搜索（好友、群聊、聊天记录）
func (h *HTTP) globalSearch(c *gin.Context) {
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
		limit = 20
	}

	friends, err := h.Svc.SearchFriends(u.ID, q, limit)
	if err != nil {
		logger.Warnf("[Search] Failed to search friends for user %d: %v", u.ID, err)
		friends = nil
	}

	groups, err := h.Svc.SearchUserGroupThreads(u.ID, q, limit)
	if err != nil {
		logger.Warnf("[Search] Failed to search groups for user %d: %v", u.ID, err)
		groups = nil
	}

	messages, err := h.Svc.GlobalSearchMessages(u.ID, q, limit)
	if err != nil {
		logger.Warnf("[Search] Failed to search messages for user %d: %v", u.ID, err)
		messages = nil
	}

	c.JSON(200, gin.H{
		"friends":  friends,
		"groups":   groups,
		"messages": messages,
	})
}
