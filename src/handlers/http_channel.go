package handlers

import (
	"net/http"
	"strconv"

	"bubble/src/middleware"
	"bubble/src/service"

	"github.com/gin-gonic/gin"
)

// getChannel 获取单个频道信息
// GET /api/channels/:id
func (h *HTTP) getChannel(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	// 解析频道 ID
	channelIDStr := c.Param("id")
	channelID, err := strconv.ParseUint(channelIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的频道ID"})
		return
	}

	// 获取频道信息
	channel, err := h.Svc.GetChannel(uint(channelID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "频道不存在"})
		return
	}

	// 检查权限：用户必须有查看频道权限
	hasPerm, err := h.Svc.HasGuildPerm(channel.GuildID, u.ID, service.PermViewChannel)
	if err != nil || !hasPerm {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	c.JSON(http.StatusOK, channel)
}
