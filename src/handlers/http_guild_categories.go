package handlers

import (
	"net/http"
	"strconv"

	"bubble/src/middleware"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// @Summary      Get all guild categories
// @Description  获取所有服务器分类列表
// @Tags         guilds
// @Produce      json
// @Success      200  {array}   map[string]any
// @Router       /api/guild-categories [get]
func (h *HTTP) getAllGuildCategories(c *gin.Context) {
	categories := h.Svc.GetAllGuildCategories()
	c.JSON(200, categories)
}

// @Summary      List guilds by category
// @Description  按分类获取服务器列表，按成员数降序排序
// @Tags         guilds
// @Produce      json
// @Param        category query string true  "分类名称 (gaming, work, dev, study, entertainment, other)"
// @Param        limit    query int    false "返回数量限制 (默认20, 最大100)"
// @Success      200      {array}  map[string]any
// @Failure      400      {object} map[string]string
// @Router       /api/guilds/by-category [get]
func (h *HTTP) listGuildsByCategory(c *gin.Context) {
	category := c.Query("category")
	if category == "" {
		c.JSON(400, gin.H{"error": "分类参数不能为空"})
		return
	}

	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit < 1 {
		limit = 20
	}

	guilds, err := h.Svc.ListGuildsByCategory(category, limit)
	if err != nil {
		logger.Errorf("[GuildCategories] Failed to list guilds by category %s: %v", category, err)
		c.JSON(400, gin.H{"error": "获取分类服务器列表失败"})
		return
	}

	c.JSON(200, guilds)
}

// @Summary      Set guild category
// @Description  设置服务器分类（仅服务器所有者可操作）
// @Tags         guilds
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path int true "服务器ID"
// @Param        body body map[string]string true "{category: 'gaming'}"
// @Success      204  {string} string "no content"
// @Failure      400  {object} map[string]string
// @Failure      401  {object} map[string]string
// @Failure      403  {object} map[string]string
// @Router       /api/guilds/{id}/category [patch]
func (h *HTTP) setGuildCategory(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	gid, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if gid == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID"})
		return
	}

	var body struct {
		Category string `json:"category"`
	}
	if err := c.BindJSON(&body); err != nil || body.Category == "" {
		c.JSON(400, gin.H{"error": "请求体格式错误或分类不能为空"})
		return
	}

	if err := h.Svc.SetGuildCategory(uint(gid), u.ID, body.Category); err != nil {
		logger.Errorf("[GuildCategories] Failed to set category for guild %d: %v", gid, err)
		if err.Error() == "未授权" || err.Error() == "unauthorized" {
			c.JSON(403, gin.H{"error": "仅服务器所有者可以修改分类"})
		} else if err.Error() == "无效的分类" {
			c.JSON(400, gin.H{"error": "无效的分类"})
		} else {
			c.JSON(400, gin.H{"error": "设置分类失败"})
		}
		return
	}

	c.Status(204)
}

// @Summary      Get all guild levels
// @Description  获取所有服务器等级信息
// @Tags         guilds
// @Produce      json
// @Success      200  {array}   map[string]any
// @Router       /api/guild-levels [get]
func (h *HTTP) getAllGuildLevels(c *gin.Context) {
	levels := h.Svc.GetAllGuildLevels()
	c.JSON(200, levels)
}

// @Summary      Get guild member limit
// @Description  获取服务器的成员人数限制
// @Tags         guilds
// @Produce      json
// @Param        id path int true "服务器ID"
// @Success      200  {object}  map[string]int
// @Failure      404  {object}  map[string]string
// @Router       /api/guilds/{id}/member-limit [get]
func (h *HTTP) getGuildMemberLimit(c *gin.Context) {
	gid, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if gid == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID"})
		return
	}

	limit, err := h.Svc.GetGuildMemberLimit(uint(gid))
	if err != nil {
		c.JSON(404, gin.H{"error": "服务器不存在"})
		return
	}

	c.JSON(200, gin.H{"memberLimit": limit})
}

// @Summary      Set guild level
// @Description  设置服务器等级（仅服务器所有者可操作）
// @Tags         guilds
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path int true "服务器ID"
// @Param        body body map[string]int true "{level: 0}"
// @Success      204  {string} string "no content"
// @Failure      400  {object} map[string]string
// @Failure      401  {object} map[string]string
// @Failure      403  {object} map[string]string
// @Router       /api/guilds/{id}/level [patch]
func (h *HTTP) setGuildLevel(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	gid, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if gid == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID"})
		return
	}

	var body struct {
		Level int `json:"level"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}

	if err := h.Svc.SetGuildLevel(uint(gid), u.ID, body.Level); err != nil {
		logger.Errorf("[GuildLevels] Failed to set level for guild %d: %v", gid, err)
		if err.Error() == "未授权" || err.Error() == "unauthorized" {
			c.JSON(403, gin.H{"error": "仅服务器所有者可以修改等级"})
		} else if err.Error() == "无效的等级" {
			c.JSON(400, gin.H{"error": "无效的等级"})
		} else {
			c.JSON(400, gin.H{"error": "设置等级失败"})
		}
		return
	}

	c.Status(204)
}
