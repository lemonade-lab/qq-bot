package handlers

import (
	"github.com/gin-gonic/gin"
)

// ==================== Enums & System Info ====================

// @Summary      Get status enums
// @Tags         enums
// @Produce      json
// @Success      200   {object}  map[string]any
// @Router       /api/enums/status [get]
// getStatusEnums 返回所有支持的用户状态枚举值。
// Method/Path: GET /api/enums/status
// 认证: 公开接口，无需认证。
// 响应: 200 返回状态枚举数组及说明。
// 用途: 前端获取所有可用状态选项，避免硬编码。
func (h *HTTP) getStatusEnums(c *gin.Context) {
	c.JSON(200, gin.H{
		"statuses": []gin.H{
			{"value": "online", "label": "在线", "description": "用户当前在线"},
			{"value": "offline", "label": "离线", "description": "用户离线"},
			{"value": "busy", "label": "忙碌", "description": "用户忙碌中，请勿打扰"},
			{"value": "dnd", "label": "请勿打扰", "description": "用户设置了请勿打扰模式"},
			{"value": "idle", "label": "闲置", "description": "用户闲置或暂时离开"},
		},
	})
}

// @Summary      Get channel type enums
// @Tags         enums
// @Produce      json
// @Success      200   {object}  map[string]any
// @Router       /api/enums/channel-types [get]
// getChannelTypeEnums 返回所有支持的频道类型枚举值。
// Method/Path: GET /api/enums/channel-types
// 认证: 公开接口，无需认证。
// 响应: 200 返回频道类型枚举数组及说明。
// 用途: 前端获取所有可用频道类型选项，避免硬编码。
func (h *HTTP) getChannelTypeEnums(c *gin.Context) {
	c.JSON(200, gin.H{
		"channelTypes": []gin.H{
			{"value": "text", "label": "文本频道", "description": "普通文本聊天频道"},
			{"value": "media", "label": "音视频频道", "description": "支持语音和视频通话的频道"},
			{"value": "forum", "label": "论坛频道", "description": "支持发布论坛帖子的频道"},
		},
	})
}
