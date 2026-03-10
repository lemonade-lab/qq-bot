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

// ==================== User Operations ====================

// @Summary      Search users by name or ID
// @Tags         users
// @Security     BearerAuth
// @Produce      json
// @Param        q      query string true  "搜索关键词：用户名或ID (>=1 字符)"
// @Param        limit  query int    false "Limit (默认10, 最大50)"
// @Success      200    {array} map[string]any
// @Failure      400    {object} map[string]string
// @Failure      401    {object} map[string]string
// @Router       /api/users/search [get]
// searchUsers 用户名/ID搜索。
// Method/Path: GET /api/users/search?q=xxx&limit=10
// 认证: 需要 Bearer Token。
// 输入: q 至少 1 字符；limit 默认为 10，上限 50。
// 响应: 200 用户对象数组（不包含密码等敏感信息）。
// 错误: 400 参数不合法；401 未认证；500 数据库错误。
func (h *HTTP) searchUsers(c *gin.Context) {
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
	users, err := h.Svc.SearchUsers(q, limit)
	if err != nil {
		logger.Errorf("[Users] Failed to search users with query '%s': %v", q, err)
		c.JSON(500, gin.H{"error": "搜索失败"})
		return
	}
	c.JSON(200, users)
}

// @Summary      Search users by ID
// @Tags         users
// @Security     BearerAuth
// @Produce      json
// @Param        q      query string true  "用户ID"
// @Success      200    {object} map[string]any
// @Failure      400    {object} map[string]string
// @Failure      401    {object} map[string]string
// @Router       /api/users/search-by-id [get]
func (h *HTTP) searchUsersByID(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	idStr := strings.TrimSpace(c.Query("q"))
	if idStr == "" {
		c.JSON(400, gin.H{"error": "缺少搜索关键词"})
		return
	}
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的用户ID"})
		return
	}
	user, err := h.Svc.GetUserByID(uint(id))
	if err != nil {
		c.JSON(404, gin.H{"error": "用户不存在"})
		return
	}
	// 检查用户隐私设置
	if user.IsPrivate && !h.Svc.AreFriends(u.ID, user.ID) {
		c.JSON(404, gin.H{"error": "用户不存在"})
		return
	}
	c.JSON(200, user)
}

// @Summary      Greet user
// @Tags         users
// @Security     BearerAuth
// @Produce      json
// @Param        id   path  int  true  "User ID to greet"
// @Success      204  {string}  string  ""
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /api/users/{id}/greet [post]
// greetUser 向指定用户打招呼，只能打一次招呼
func (h *HTTP) greetUser(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	toUserID := uint(id)
	if err := h.Svc.GreetUser(u.ID, toUserID); err != nil {
		if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "用户不存在"})
		} else {
			logger.Errorf("[Users] Failed to greet user %d: %v", toUserID, err)
			c.JSON(400, gin.H{"error": "打招呼失败"})
		}
		return
	}
	c.Status(204)
}

// @Summary      Check if user is friend
// @Tags         users
// @Security     BearerAuth
// @Produce      json
// @Param        id   path  int  true  "User ID to check"
// @Success      200  {object}  map[string]any  "{\"isFriend\": true, \"status\": \"accepted\"}"
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /api/users/{id}/is-friend [get]
// checkIsFriend 检查当前用户与指定用户是否是好友关系
func (h *HTTP) checkIsFriend(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的用户ID"})
		return
	}
	targetUserID := uint(id)

	// 检查用户是否存在
	_, err = h.Svc.GetUserByID(targetUserID)
	if err != nil {
		c.JSON(404, gin.H{"error": "用户不存在"})
		return
	}

	// 检查是否是好友
	exists, status, err := h.Svc.Repo.ExistsFriendship(u.ID, targetUserID)
	if err != nil {
		logger.Errorf("[Users] Failed to check friendship status for user %d and %d: %v", u.ID, targetUserID, err)
		c.JSON(500, gin.H{"error": "查询失败"})
		return
	}

	isFriend := exists && status == "accepted"
	c.JSON(200, gin.H{
		"isFriend": isFriend,
		"status":   status,
	})
}

// @Summary      Get shared guilds with user
// @Tags         users
// @Security     BearerAuth
// @Produce      json
// @Param        id   path  int  true  "User ID to check"
// @Success      200  {object}  map[string]any  "{\"sharedGuilds\": [{\"id\": 1, \"name\": \"Guild Name\"}]}"
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /api/users/{id}/shared-guilds [get]
// getSharedGuilds 获取当前用户与指定用户共同加入的服务器列表
// @Summary      Get shared guilds
// @Description  获取与指定用户的共同服务器
// @Tags         users
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id  path  int  true  "User ID"
// @Success      200  {array}   models.Guild
// @Failure      400  {object}  map[string]string  "参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      404  {object}  map[string]string  "用户不存在"
// @Failure      500  {object}  map[string]string  "服务器错误"
// @Router       /api/users/{id}/guilds [get]
func (h *HTTP) getSharedGuilds(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的用户ID"})
		return
	}
	targetUserID := uint(id)

	// 检查用户是否存在
	_, err = h.Svc.GetUserByID(targetUserID)
	if err != nil {
		c.JSON(404, gin.H{"error": "用户不存在"})
		return
	}

	// 获取共同加入的服务器
	sharedGuilds, err := h.Svc.GetSharedGuilds(u.ID, targetUserID)
	if err != nil {
		logger.Errorf("[Users] Failed to get shared guilds for user %d and %d: %v", u.ID, targetUserID, err)
		c.JSON(500, gin.H{"error": "获取共同服务器失败"})
		return
	}

	c.JSON(200, gin.H{
		"sharedGuilds": sharedGuilds,
	})
}

// @Summary      Get user detail (friend-only)
// @Tags         users
// @Security     BearerAuth
// @Produce      json
// @Param        id   path  int  true  "User ID"
// @Success      200  {object}  map[string]any
// @Failure      400  {object}  map[string]string
// @Failure      401  {object}  map[string]string
// @Failure      403  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /api/users/{id}/detail [get]
// getUserDetail 返回指定用户的详细信息，前提：与当前用户互为好友且双方不在黑名单中。
// @Summary      Get user detail
// @Description  获取用户详细信息
// @Tags         users
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id  path  int  true  "User ID"
// @Success      200  {object}  models.User
// @Failure      400  {object}  map[string]string  "参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      404  {object}  map[string]string  "用户不存在"
// @Failure      500  {object}  map[string]string  "服务器错误"
// @Router       /api/users/{id} [get]
func (h *HTTP) getUserDetail(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(400, gin.H{"error": "无效的用户ID"})
		return
	}
	targetID := uint(id)

	// 目标用户存在性校验
	tu, err := h.Svc.GetUserByID(targetID)
	if err != nil || tu == nil {
		c.JSON(404, gin.H{"error": "用户不存在"})
		return
	}

	// 好友校验
	isFriend, status, err := h.Svc.Repo.ExistsFriendship(u.ID, targetID)
	if err != nil {
		logger.Errorf("[Users] Failed to check friendship for user detail access %d and %d: %v", u.ID, targetID, err)
		c.JSON(500, gin.H{"error": "查询失败"})
		return
	}
	if !(isFriend && status == "accepted") {
		c.JSON(403, gin.H{"error": "非好友关系"})
		return
	}

	// 黑名单双向校验：任一方向拉黑均拒绝
	if blk, err := h.Svc.Repo.IsBlocked(u.ID, targetID); err == nil && blk {
		c.JSON(403, gin.H{"error": "已被拉黑"})
		return
	}
	if blk, err := h.Svc.Repo.IsBlocked(targetID, u.ID); err == nil && blk {
		c.JSON(403, gin.H{"error": "已被拉黑"})
		return
	}

	// 返回详细信息（隐私友好：不包含邮箱等敏感字段）
	resp := gin.H{
		"id":          tu.ID,
		"name":        tu.Name,
		"avatar":      tu.Avatar,
		"banner":      tu.Banner,
		"bannerColor": tu.BannerColor,
		"bio":         tu.Bio,
		"status":      tu.Status,
		"createdAt":   tu.CreatedAt,
		"isPrivate":   tu.IsPrivate,
	}
	c.JSON(200, resp)
}
