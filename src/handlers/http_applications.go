package handlers

import (
	"net/http"
	"strconv"

	"bubble/src/config"
	"bubble/src/middleware"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// @Summary      List my applications
// @Tags         me
// @Security     BearerAuth
// @Produce      json
// @Param        limit    query int false "Limit (default 50, max 100)"
// @Param        beforeId query int false "id < beforeId"
// @Param        afterId  query int false "id > afterId"
// @Success      200  {array}  map[string]any
// @Router       /api/me/applications [get]
func (h *HTTP) listMyApplications(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	limit := int(config.DefaultPageLimit)
	if v := c.Query("limit"); v != "" {
		if iv, err := strconv.Atoi(v); err == nil {
			if iv > 0 && int64(iv) <= config.MaxPageLimit {
				limit = iv
			}
		}
	}
	beforeID := parseUintQuery(c, "beforeId", 0)
	afterID := parseUintQuery(c, "afterId", 0)
	apps, users, guilds, err := h.Svc.ListUserApplicationsWithInfo(u.ID, limit, beforeID, afterID)
	if err != nil {
		logger.Errorf("[Applications] Failed to list applications for user %d: %v", u.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取申请列表失败"})
		return
	}

	// 构建完整的响应数据
	result := make([]gin.H, len(apps))
	for i, app := range apps {
		item := gin.H{
			"id":         app.ID,
			"type":       app.Type,
			"fromUserId": app.FromUserID,
			"toUserId":   app.ToUserID,
			"status":     app.Status,
			"createdAt":  app.CreatedAt,
			"updatedAt":  app.UpdatedAt,
		}

		// 添加申请发起者信息
		if fromUser, ok := users[app.FromUserID]; ok {
			item["fromUser"] = gin.H{
				"id":     fromUser.ID,
				"name":   fromUser.Name,
				"avatar": fromUser.Avatar,
			}
		}

		// 添加申请接收者信息
		if toUser, ok := users[app.ToUserID]; ok {
			item["toUser"] = gin.H{
				"id":     toUser.ID,
				"name":   toUser.Name,
				"avatar": toUser.Avatar,
			}
		}

		// 添加目标服务器信息（如果是服务器加入申请）
		if app.TargetGuildID != nil {
			item["targetGuildId"] = *app.TargetGuildID
			if guild, ok := guilds[*app.TargetGuildID]; ok {
				item["guild"] = gin.H{
					"id":     guild.ID,
					"name":   guild.Name,
					"avatar": guild.Avatar,
				}
			}
		}

		result[i] = item
	}

	c.JSON(http.StatusOK, result)
}

// @Summary      Create application (friend or guild join)
// @Tags         me
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body body  object{"type":string,"toUserId":int,"targetGuildId":int} true "Application payload"
// @Success      200  {object}  map[string]any
// @Router       /api/applications [post]
func (h *HTTP) createApplication(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	var body struct {
		Type          string `json:"type" binding:"required"`
		ToUserID      uint   `json:"toUserId"`
		TargetGuildID *uint  `json:"targetGuildId"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	app, err := h.Svc.CreateApplication(body.Type, u.ID, body.ToUserID, body.TargetGuildID)
	if err != nil {
		logger.Errorf("[Applications] Failed to create application for user %d: %v", u.ID, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "创建申请失败"})
		return
	}

	// 构建完整的申请响应数据（包含用户和服务器信息）
	appPayload := gin.H{
		"id":         app.ID,
		"type":       app.Type,
		"fromUserId": app.FromUserID,
		"toUserId":   app.ToUserID,
		"status":     app.Status,
		"createdAt":  app.CreatedAt,
		"updatedAt":  app.UpdatedAt,
	}

	// 添加申请发起者信息（当前用户）
	appPayload["fromUser"] = gin.H{
		"id":     u.ID,
		"name":   u.Name,
		"avatar": u.Avatar,
	}

	// 添加申请接收者信息
	if toUser, err := h.Svc.Repo.GetUserByID(body.ToUserID); err == nil && toUser != nil {
		appPayload["toUser"] = gin.H{
			"id":     toUser.ID,
			"name":   toUser.Name,
			"avatar": toUser.Avatar,
		}
	}

	// 添加目标服务器信息（如果是服务器加入申请）
	if app.TargetGuildID != nil {
		appPayload["targetGuildId"] = *app.TargetGuildID
		if guild, err := h.Svc.Repo.GetGuild(*app.TargetGuildID); err == nil && guild != nil {
			appPayload["guild"] = gin.H{
				"id":     guild.ID,
				"name":   guild.Name,
				"avatar": guild.Avatar,
			}
		}
	}

	// WS: 推送个人申请给目标用户（APPLY_CREATE 用于实时更新申请列表）
	if h.Gw != nil {
		h.Gw.BroadcastApply(body.ToUserID, appPayload)
	}

	c.JSON(http.StatusOK, appPayload)
}

// @Summary      Approve/Reject application
// @Tags         me
// @Security     BearerAuth
// @Produce      json
// @Param        id   path  int  true  "Application ID"
// @Param        body body  object{"status":string} true "approved or rejected"
// @Success      204  {string}  string  "no content"
// @Router       /api/applications/{id} [put]
func (h *HTTP) updateApplicationStatus(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		logger.Errorf("[Applications] Invalid application ID: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的申请ID"})
		return
	}
	var body struct {
		Status string `json:"status" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	app, err := h.Svc.UpdateApplicationStatus(id, u.ID, body.Status)
	if err != nil {
		logger.Errorf("[Applications] Failed to update application %d status: %v", id, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "更新申请状态失败"})
		return
	}

	// 构建完整的申请更新数据（包含用户和服务器信息）
	if h.Gw != nil && app != nil {
		appPayload := gin.H{
			"id":         app.ID,
			"type":       app.Type,
			"fromUserId": app.FromUserID,
			"toUserId":   app.ToUserID,
			"status":     app.Status,
			"createdAt":  app.CreatedAt,
			"updatedAt":  app.UpdatedAt,
		}

		// 添加申请发起者信息
		if fromUser, err := h.Svc.Repo.GetUserByID(app.FromUserID); err == nil && fromUser != nil {
			appPayload["fromUser"] = gin.H{
				"id":     fromUser.ID,
				"name":   fromUser.Name,
				"avatar": fromUser.Avatar,
			}
		}

		// 添加审批者信息（当前用户）
		appPayload["toUser"] = gin.H{
			"id":     u.ID,
			"name":   u.Name,
			"avatar": u.Avatar,
		}

		// 添加目标服务器信息（如果是服务器加入申请）
		if app.TargetGuildID != nil {
			appPayload["targetGuildId"] = *app.TargetGuildID
			if guild, err := h.Svc.Repo.GetGuild(*app.TargetGuildID); err == nil && guild != nil {
				appPayload["guild"] = gin.H{
					"id":     guild.ID,
					"name":   guild.Name,
					"avatar": guild.Avatar,
				}
			}
		}

		// WS: 推送申请状态更新给申请发起者（APPLY_UPDATE）
		h.Gw.BroadcastApplyUpdate(app.FromUserID, appPayload)
	}

	c.Status(http.StatusNoContent)
}
