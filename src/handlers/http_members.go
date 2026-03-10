package handlers

import (
	"net/http"
	"strconv"

	"bubble/src/middleware"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// @Summary      List guild members
// @Description  列出服务器成员列表，支持分页和游标分页
// @Tags         members
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id        path   int  true   "Guild ID"
// @Param        page      query  int  false  "Page number (default 1)"
// @Param        limit     query  int  false  "Items per page (default 10, max 100)"
// @Param        beforeId  query  int  false  "Cursor: return members with id < beforeId"
// @Param        afterId   query  int  false  "Cursor: return members with id > afterId"
// @Success      200       {object}  map[string]any  "{\"data\": [], \"pagination\": {}}"
// @Failure      401       {object}  map[string]string  "未认证"
// @Failure      403       {object}  map[string]string  "非成员"
// @Failure      500       {object}  map[string]string  "服务器错误"
// @Router       /api/guilds/{id}/members [get]
func (h *HTTP) listMembers(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	gid := uint(gid64)
	isMember, err := h.Svc.IsMember(gid, u.ID)
	if err != nil {
		c.JSON(500, gin.H{"error": "服务器错误"})
		return
	}
	if !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "您不是该服务器的成员"})
		return
	}
	// Cursor support
	beforeID64, _ := strconv.ParseUint(c.Query("beforeId"), 10, 64)
	afterID64, _ := strconv.ParseUint(c.Query("afterId"), 10, 64)
	beforeID := uint(beforeID64)
	afterID := uint(afterID64)
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit < 1 || limit > 100 {
		limit = 10
	}
	if beforeID > 0 || afterID > 0 {
		members, err := h.Svc.ListMembersCursor(gid, limit, beforeID, afterID)
		if err != nil {
			logger.Errorf("[Members] Failed to list members by cursor for guild %d: %v", gid, err)
			c.JSON(500, gin.H{"error": "获取成员列表失败"})
			return
		}
		c.JSON(200, gin.H{"data": members})
		return
	}
	// Fallback to page-based
	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	members, total, err := h.Svc.ListMembers(gid, page, limit)
	if err != nil {
		logger.Errorf("[Members] Failed to list members for guild %d: %v", gid, err)
		c.JSON(500, gin.H{"error": "获取成员列表失败"})
		return
	}
	c.JSON(200, gin.H{
		"data": members,
		"pagination": gin.H{
			"page":    page,
			"limit":   limit,
			"total":   total,
			"hasMore": page*limit < total,
		},
	})
}

// @Summary      Search guild members
// @Description  搜索服务器成员（通过名称/昵称）
// @Tags         members
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id     path   int     true   "Guild ID"
// @Param        q      query  string  true   "Search query"
// @Param        limit  query  int     false  "Limit (default 10, max 20)"
// @Success      200    {object}  map[string]any  "{\"data\": []}"
// @Failure      400    {object}  map[string]string  "参数错误"
// @Failure      401    {object}  map[string]string  "未认证"
// @Failure      403    {object}  map[string]string  "非成员"
// @Failure      500    {object}  map[string]string  "服务器错误"
// @Router       /api/guilds/{id}/members/search [get]
func (h *HTTP) searchMembers(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	gid := uint(gid64)
	// 检查是否为成员
	isMember, err := h.Svc.IsMember(gid, u.ID)
	if err != nil {
		c.JSON(500, gin.H{"error": "服务器错误"})
		return
	}
	if !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "您不是该服务器的成员"})
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

	members, err := h.Svc.SearchMembers(gid, query, limit)
	if err != nil {
		logger.Errorf("[Members] Failed to search members in guild %d with query '%s': %v", gid, query, err)
		c.JSON(500, gin.H{"error": "搜索成员失败"})
		return
	}

	c.JSON(200, gin.H{"data": members})
}
