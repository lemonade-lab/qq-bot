package handlers

import (
	"net/http"
	"strconv"

	"bubble/src/middleware"
	"bubble/src/service"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// @Summary      List blacklist
// @Tags         blacklist
// @Security     BearerAuth
// @Produce      json
// @Success      200  {array}  map[string]any
// @Router       /api/blacklist [get]
func (h *HTTP) listBlacklist(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	list, err := h.Svc.ListBlacklist(u.ID)
	if err != nil {
		logger.Errorf("[Blacklist] Failed to list blacklist for user %d: %v", u.ID, err)
		c.JSON(400, gin.H{"error": "获取黑名单失败"})
		return
	}
	c.JSON(200, list)
}

// @Summary      Add to blacklist
// @Tags         blacklist
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body body  object{userId=int}  true  "User to block"
// @Success      200
// @Router       /api/blacklist [post]
func (h *HTTP) addToBlacklist(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	var body struct {
		UserID uint `json:"userId"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "无效的JSON"})
		return
	}

	if err := h.Svc.AddToBlacklist(u.ID, body.UserID); err != nil {
		logger.Errorf("[Blacklist] Failed to add user %d to blacklist for user %d: %v", body.UserID, u.ID, err)
		c.JSON(400, gin.H{"error": "添加黑名单失败"})
		return
	}

	c.JSON(200, gin.H{})
}

// @Summary      Remove from blacklist
// @Tags         blacklist
// @Security     BearerAuth
// @Produce      json
// @Param        userId path int true "User ID"
// @Success      200
// @Router       /api/blacklist/{userId} [delete]
func (h *HTTP) removeFromBlacklist(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	userID, err := strconv.ParseUint(c.Param("userId"), 10, 32)
	if err != nil {
		c.JSON(400, gin.H{"error": "无效的用户ID"})
		return
	}

	if err := h.Svc.RemoveFromBlacklist(u.ID, uint(userID)); err != nil {
		// 如果不在黑名单中，也视为移除成功，避免重复点击报错
		if err != service.ErrNotFound {
			logger.Errorf("[Blacklist] Failed to remove user %d from blacklist for user %d: %v", userID, u.ID, err)
			c.JSON(400, gin.H{"error": "移除黑名单失败"})
			return
		}
	}

	c.JSON(200, gin.H{})
}
