package handlers

import (
	"net/http"
	"strconv"

	"bubble/src/middleware"
	"bubble/src/service"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// @Summary      List channel categories
// @Description  列出服务器的所有频道分类
// @Tags         categories
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Guild ID"
// @Success      200  {array}  map[string]any
// @Failure      400  {object}  map[string]string  "参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      500  {object}  map[string]string  "服务器错误"
// @Router       /api/guilds/{id}/categories [get]
func (h *HTTP) listChannelCategories(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if gid64 == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID"})
		return
	}

	cats, err := h.Svc.ListChannelCategories(uint(gid64))
	if err != nil {
		logger.Errorf("[Categories] Failed to list categories for guild %d: %v", gid64, err)
		c.JSON(500, gin.H{"error": "获取分类列表失败"})
		return
	}
	c.JSON(200, cats)
}

// @Summary      Create channel category
// @Description  创建频道分类
// @Tags         categories
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Guild ID"
// @Param        body body  map[string]string true "{name}"
// @Success      200  {object}  map[string]any
// @Failure      400  {object}  map[string]string  "参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      403  {object}  map[string]string  "权限不足"
// @Router       /api/guilds/{id}/categories [post]
func (h *HTTP) createChannelCategory(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if gid64 == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID"})
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}

	cat, err := h.Svc.CreateChannelCategory(uint(gid64), u.ID, body.Name)
	if err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "权限不足"})
		} else {
			logger.Errorf("[Categories] Failed to create category for guild %d by user %d: %v", gid64, u.ID, err)
			c.JSON(400, gin.H{"error": "创建分类失败"})
		}
		return
	}
	c.JSON(200, cat)
}

// @Summary      Update channel category
// @Description  更新频道分类名称
// @Tags         categories
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id          path  int  true  "Guild ID"
// @Param        categoryId  path  int  true  "Category ID"
// @Param        body        body  map[string]string true "{name}"
// @Success      200  {object}  map[string]any
// @Failure      400  {object}  map[string]string  "参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      403  {object}  map[string]string  "权限不足"
// @Failure      404  {object}  map[string]string  "分类不存在"
// @Router       /api/guilds/{id}/categories/{categoryId} [put]
func (h *HTTP) updateChannelCategory(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	catID64, _ := strconv.ParseUint(c.Param("categoryId"), 10, 64)
	if catID64 == 0 {
		c.JSON(400, gin.H{"error": "无效的分类ID"})
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}

	gid64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	cat, err := h.Svc.UpdateChannelCategory(uint(gid64), uint(catID64), u.ID, body.Name)
	if err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "权限不足"})
		} else if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "分类不存在"})
		} else {
			logger.Errorf("[Categories] Failed to update category %d for guild %d by user %d: %v", catID64, gid64, u.ID, err)
			c.JSON(400, gin.H{"error": "更新分类失败"})
		}
		return
	}
	c.JSON(200, cat)
}

// @Summary      Delete channel category
// @Description  删除频道分类（所属频道变为无分类）
// @Tags         categories
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id          path  int  true  "Guild ID"
// @Param        categoryId  path  int  true  "Category ID"
// @Success      200  {object}  map[string]bool
// @Failure      400  {object}  map[string]string  "参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      403  {object}  map[string]string  "权限不足"
// @Failure      404  {object}  map[string]string  "分类不存在"
// @Router       /api/guilds/{id}/categories/{categoryId} [delete]
func (h *HTTP) deleteChannelCategory(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	catID64, _ := strconv.ParseUint(c.Param("categoryId"), 10, 64)
	if catID64 == 0 {
		c.JSON(400, gin.H{"error": "无效的分类ID"})
		return
	}

	gid64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if err := h.Svc.DeleteChannelCategory(uint(gid64), uint(catID64), u.ID); err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "权限不足"})
			return
		} else if err == service.ErrNotFound {
			// 分类不存在也视为删除成功
			c.JSON(200, gin.H{"success": true})
			return
		} else {
			logger.Errorf("[Categories] Failed to delete category %d for guild %d by user %d: %v", catID64, gid64, u.ID, err)
			c.JSON(400, gin.H{"error": "删除分类失败"})
			return
		}
	}
	c.JSON(200, gin.H{"success": true})
}

// @Summary      Reorder channel categories
// @Description  批量更新频道分类排序
// @Tags         categories
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Guild ID"
// @Param        body body  object  true  "Array of {id, sortOrder}"
// @Success      200  {object}  map[string]bool
// @Failure      400  {object}  map[string]string  "参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      403  {object}  map[string]string  "权限不足"
// @Router       /api/guilds/{id}/categories/reorder [put]
func (h *HTTP) reorderChannelCategories(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	gid64, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	if gid64 == 0 {
		c.JSON(400, gin.H{"error": "无效的服务器ID"})
		return
	}

	var body struct {
		Orders []struct {
			ID        uint `json:"id"`
			SortOrder int  `json:"sortOrder"`
		} `json:"orders"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "请求体格式错误"})
		return
	}

	if err := h.Svc.ReorderChannelCategories(uint(gid64), u.ID, body.Orders); err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(403, gin.H{"error": "权限不足"})
		} else if err == service.ErrNotFound {
			c.JSON(404, gin.H{"error": "分类不存在"})
		} else {
			logger.Errorf("[Categories] Failed to reorder categories in guild %d by user %d: %v", gid64, u.ID, err)
			c.JSON(400, gin.H{"error": "重新排序失败"})
		}
		return
	}
	c.JSON(200, gin.H{"success": true})
}
