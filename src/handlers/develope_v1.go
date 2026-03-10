package handlers

import (
	"bubble/src/middleware"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// RegisterDeveloper registers developer (robot management) routes
// 开发者管理路由 - 需要用户认证
func (rv *RobotV1) RegisterDeveloper(r gin.IRouter) {
	// 开发者需要用户认证
	r.Use(middleware.AuthRequired(rv.h.Svc))

	r.POST("/robots", rv.createRobot)
	r.GET("/robots", rv.listRobots)
	r.GET("/robots/:id", rv.getRobot)
	r.PUT("/robots/:id", rv.updateRobot)
	r.DELETE("/robots/:id", rv.deleteRobot)
	r.POST("/robots/:id/reset-token", rv.resetRobotToken)
	r.PUT("/robots/:id/webhook", rv.updateWebhook)
	r.GET("/robots/:id/stats", rv.getRobotStats)
	// 机器人头像更新
	r.PUT("/robots/:id/avatar", rv.updateRobotAvatar)
	// 设置机器人隐私
	r.PUT("/robots/:id/privacy", rv.setRobotPrivacy)
	// 设置机器人分类
	r.PUT("/robots/:id/category", rv.setRobotCategory)
	// 机器人加入服务器
	r.POST("/robots/:id/guilds/:guildId/join", rv.addRobotToGuild)
	// 机器人退出服务器
	r.DELETE("/robots/:id/guilds/:guildId", rv.removeRobotFromGuild)
	// 查询机器人已加入的服务器列表（含详情）
	r.GET("/robots/:id/guilds", rv.getRobotJoinedGuilds)

	// Webhook 调用日志
	r.GET("/robots/:id/webhook/logs", rv.getWebhookLogs)

	// 开发者查询机器人配额
	r.GET("/robots/:id/quota", rv.getRobotQuotaDeveloper)
}

// ==================== Developer APIs ====================

// @Summary Create a robot
// @Tags robot-developer
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param body body map[string]string true "{name,description(optional)}"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /api/robot/v1/developer/robots [post]
func (rv *RobotV1) createRobot(c *gin.Context) {
	var body struct {
		Name        string `json:"name" binding:"required,min=2,max=32"`
		Description string `json:"description" binding:"max=200"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		logger.Errorf("[Developer] Invalid request body for creating robot: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}

	user := middleware.UserFromCtx(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	// 检查机器人数量限制 (可配置)
	list, _ := rv.h.Svc.ListRobotsByOwner(user.ID)
	if len(list) >= MaxRobotsPerOwner { // 限制每个用户最多 MaxRobotsPerOwner 个机器人
		c.JSON(http.StatusBadRequest, gin.H{"error": "机器人数量已达上限（最多" + strconv.Itoa(MaxRobotsPerOwner) + "个）"})
		return
	}

	rb, err := rv.h.Svc.CreateRobot(user.ID, body.Name, body.Description)
	if err != nil {
		logger.Errorf("[Developer] Failed to create robot for user %d: %v", user.ID, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "创建机器人失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":        rb.ID,
		"createdAt": rb.CreatedAt,
		"token":     rb.Token,
		"botUser":   rb.BotUser,
	})
}

// @Summary List robots
// @Tags robot-developer
// @Security BearerAuth
// @Produce json
// @Success 200 {object} map[string]any
// @Router /api/robot/v1/developer/robots [get]
func (rv *RobotV1) listRobots(c *gin.Context) {
	user := middleware.UserFromCtx(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	enrichedList, err := rv.h.Svc.ListRobotsWithOverview(user.ID)
	if err != nil {
		logger.Errorf("[Developer] Failed to list robots for user %d: %v", user.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取机器人列表失败"})
		return
	}

	// 返回机器人列表，包含概览数据
	c.JSON(http.StatusOK, gin.H{"list": enrichedList})

	// 异步尝试为未绑定 BotUser 的机器人进行补建与绑定
	go func() {
		rawList, _ := rv.h.Svc.ListRobotsByOwner(user.ID)
		for i := range rawList {
			if rawList[i].BotUser == nil {
				_ = rv.h.Svc.EnsureRobotUser(&rawList[i])
			}
		}
	}()

}

// @Summary Get robot details
// @Tags robot-developer
// @Security BearerAuth
// @Produce json
// @Param id path int true "Robot ID"
// @Success 200 {object} map[string]any
// @Router /api/robot/v1/developer/robots/{id} [get]
func (rv *RobotV1) getRobot(c *gin.Context) {
	user := middleware.UserFromCtx(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的机器人ID"})
		return
	}

	rb, err := rv.h.Svc.GetRobot(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "机器人不存在"})
		return
	}

	// 验证所有权
	if rb.OwnerID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	// 返回完整 Robot 信息，社交属性在 botUser 中
	c.JSON(http.StatusOK, gin.H{
		"id":         rb.ID,
		"ownerId":    rb.OwnerID,
		"botUserId":  rb.BotUserID,
		"isPrivate":  rb.IsPrivate,
		"category":   rb.Category,
		"webhookUrl": rb.WebhookURL,
		"createdAt":  rb.CreatedAt,
		"updatedAt":  rb.UpdatedAt,
		"botUser":    rb.BotUser,
	})
}

// @Summary Update robot
// @Tags robot-developer
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path int true "Robot ID"
// @Param body body map[string]string true "{name,description}"
// @Success 200 {object} map[string]any
// @Router /api/robot/v1/developer/robots/{id} [put]
func (rv *RobotV1) updateRobot(c *gin.Context) {
	user := middleware.UserFromCtx(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的机器人ID"})
		return
	}

	var body struct {
		Name        string `json:"name" binding:"omitempty,min=2,max=32"`
		Description string `json:"description" binding:"omitempty,max=200"`
		Banner      string `json:"banner" binding:"omitempty,max=512"`
		Category    string `json:"category" binding:"omitempty,max=64"`
		WebhookURL  string `json:"webhookUrl" binding:"omitempty,url"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		logger.Errorf("[Developer] Invalid request body for updating robot: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}

	rb, err := rv.h.Svc.GetRobot(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "机器人不存在"})
		return
	}

	if rb.OwnerID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	// 更新 Robot 自身属性（category / webhookUrl）
	robotUpdated := false
	if body.Category != "" {
		if err := rv.h.Svc.SetRobotCategory(rb.ID, body.Category); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		rb.Category = body.Category
		robotUpdated = true
	}
	if body.WebhookURL != "" {
		rb.WebhookURL = body.WebhookURL
		if err := rv.h.Svc.Repo.UpdateRobot(rb); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "更新回调地址失败"})
			return
		}
		robotUpdated = true
	}
	_ = robotUpdated

	// 所有社交属性统一更新到 BotUser
	if rb.BotUser == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "机器人用户数据异常"})
		return
	}

	botUserUpdates := map[string]any{}
	if body.Name != "" {
		botUserUpdates["name"] = body.Name
		rb.BotUser.Name = body.Name
	}
	if body.Description != "" {
		botUserUpdates["bio"] = body.Description
		rb.BotUser.Bio = body.Description
	}
	if body.Banner != "" {
		botUserUpdates["banner"] = body.Banner
		rb.BotUser.Banner = body.Banner
	}

	if len(botUserUpdates) > 0 {
		if err := rv.h.Svc.Repo.UpdateUser(rb.BotUser, botUserUpdates); err != nil {
			logger.Errorf("[Developer] Failed to update bot user for robot %d: %v", id, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "更新机器人信息失败"})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"id":         rb.ID,
		"ownerId":    rb.OwnerID,
		"isPrivate":  rb.IsPrivate,
		"category":   rb.Category,
		"webhookUrl": rb.WebhookURL,
		"createdAt":  rb.CreatedAt,
		"botUser":    rb.BotUser,
	})
}

// @Summary Delete robot
// @Tags robot-developer
// @Security BearerAuth
// @Param id path int true "Robot ID"
// @Success 204
// @Router /api/robot/v1/developer/robots/{id} [delete]
func (rv *RobotV1) deleteRobot(c *gin.Context) {
	user := middleware.UserFromCtx(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的机器人ID"})
		return
	}

	rb, err := rv.h.Svc.GetRobot(uint(id))
	if err != nil {
		// 机器人不存在也视为删除成功，避免重复点击报错
		c.Status(http.StatusNoContent)
		return
	}

	if rb.OwnerID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	if err := rv.h.Svc.DeleteRobot(uint(id)); err != nil {
		logger.Errorf("[Developer] Failed to delete robot %d: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除机器人失败"})
		return
	}

	c.Status(http.StatusNoContent)
}

// @Summary Reset robot token
// @Tags robot-developer
// @Security BearerAuth
// @Produce json
// @Param id path int true "Robot ID"
// @Success 200 {object} map[string]any
// @Router /api/robot/v1/developer/robots/{id}/reset-token [post]
func (rv *RobotV1) resetRobotToken(c *gin.Context) {
	user := middleware.UserFromCtx(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的机器人ID"})
		return
	}

	rb, err := rv.h.Svc.GetRobot(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "机器人不存在"})
		return
	}

	if rb.OwnerID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	newToken, err := rv.h.Svc.ResetRobotToken(uint(id))
	if err != nil {
		logger.Errorf("[Developer] Failed to reset robot token for %d: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "重置令牌失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": newToken,
	})
}

// @Summary Update webhook URL
// @Tags robot-developer
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path int true "Robot ID"
// @Param body body map[string]string true "{webhookUrl}"
// @Success 200 {object} map[string]any
// @Router /api/robot/v1/developer/robots/{id}/webhook [put]
func (rv *RobotV1) updateWebhook(c *gin.Context) {
	user := middleware.UserFromCtx(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的机器人ID"})
		return
	}

	var body struct {
		WebhookURL string `json:"webhookUrl" binding:"omitempty,url"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "回调地址不合法"})
		return
	}

	rb, err := rv.h.Svc.GetRobot(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "机器人不存在"})
		return
	}

	if rb.OwnerID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	if err := rv.h.Svc.UpdateRobotWebhook(uint(id), body.WebhookURL); err != nil {
		logger.Errorf("[Developer] Failed to update webhook for robot %d: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新回调地址失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"webhookUrl": body.WebhookURL,
	})
}

// @Summary Get robot statistics
// @Tags robot-developer
// @Security BearerAuth
// @Produce json
// @Param id path int true "Robot ID"
// @Success 200 {object} map[string]any
// @Router /api/robot/v1/developer/robots/{id}/stats [get]
func (rv *RobotV1) getRobotStats(c *gin.Context) {
	user := middleware.UserFromCtx(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的机器人ID"})
		return
	}

	rb, err := rv.h.Svc.GetRobot(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "机器人不存在"})
		return
	}

	if rb.OwnerID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	stats, err := rv.h.Svc.GetRobotStats(uint(id))
	if err != nil {
		logger.Errorf("[Developer] Failed to get robot stats for %d: %v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取统计数据失败"})
		return
	}

	// 聚合配额数据
	var quota any
	if rv.h.Svc.Repo.Redis != nil {
		now := time.Now()
		dayKey := now.Format("20060102")
		monthKey := now.Format("200601")
		dailyKey := "bot:upload:daily:" + strconv.Itoa(int(rb.BotUserID)) + ":" + dayKey
		monthlyKey := "bot:upload:monthly:" + strconv.Itoa(int(rb.BotUserID)) + ":" + monthKey
		ctx := c.Request.Context()
		dailyUsed, _ := rv.h.Svc.Repo.Redis.Get(ctx, dailyKey).Int64()
		monthlyUsed, _ := rv.h.Svc.Repo.Redis.Get(ctx, monthlyKey).Int64()
		limits := getQuotaLimits()
		quota = gin.H{
			"botUserId": rb.BotUserID,
			"date":      dayKey,
			"month":     monthKey,
			"usage": gin.H{
				"dailyUsedBytes":   dailyUsed,
				"monthlyUsedBytes": monthlyUsed,
			},
			"limits": gin.H{
				"withMessage": gin.H{
					"dailyBytes":   limits.DailyWithMsgBytes,
					"monthlyBytes": limits.MonthlyWithMsgBytes,
					"monthlyMax":   limits.MonthlyWithMsgMax,
				},
				"noMessage": gin.H{
					"dailyBytes":   limits.DailyNoMsgBytes,
					"monthlyBytes": limits.MonthlyNoMsgBytes,
				},
			},
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"stats": stats,
		"quota": quota,
	})
}

// @Summary Add robot to guild
// @Description 服主直接拉入；非服主代替机器人向服主发送加入申请通知
// @Tags robot-developer
// @Security BearerAuth
// @Produce json
// @Param id path int true "Robot ID"
// @Param guildId path int true "Guild ID"
// @Success 200 {object} map[string]any
// @Router /api/robot/v1/developer/robots/{id}/guilds/{guildId}/join [post]
func (rv *RobotV1) addRobotToGuild(c *gin.Context) {
	user := middleware.UserFromCtx(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	robotID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的机器人ID"})
		return
	}

	guildID, err := strconv.ParseUint(c.Param("guildId"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}

	// 检查机器人是否存在
	rb, err := rv.h.Svc.GetRobot(uint(robotID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "机器人不存在"})
		return
	}

	// 检查用户是否是该服务器的成员
	isMember, err := rv.h.Svc.IsMember(uint(guildID), user.ID)
	if err != nil || !isMember {
		c.JSON(http.StatusForbidden, gin.H{"error": "非服务器成员"})
		return
	}

	// 检查机器人是否已加入该服务器
	botAlreadyMember, _ := rv.h.Svc.IsMember(uint(guildID), rb.BotUserID)
	if botAlreadyMember {
		c.JSON(http.StatusOK, gin.H{
			"action":  "already_joined",
			"message": "机器人已在该服务器中",
		})
		return
	}

	// 获取服务器信息，判断用户是否是服主
	guild, err := rv.h.Svc.Repo.GetGuild(uint(guildID))
	if err != nil || guild == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "服务器不存在"})
		return
	}

	if guild.OwnerID == user.ID {
		// 服主：直接将机器人拉入服务器
		if err := rv.h.Svc.JoinGuild(uint(guildID), rb.BotUserID); err != nil {
			logger.Errorf("[Developer] Failed to join robot %d to guild %d: %v", robotID, guildID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "机器人加入服务器失败"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"action":  "joined",
			"message": "机器人已加入服务器",
		})
	} else {
		// 非服主：代替机器人向服主发送申请通知
		// 创建通知给服主
		notif, err := rv.h.Svc.CreateBotJoinRequestNotification(guild.OwnerID, user.ID, rb.BotUserID, uint(guildID))
		if err != nil {
			logger.Errorf("[Developer] Failed to create bot join notification for guild %d: %v", guildID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "提交申请失败"})
			return
		}

		// 推送实时通知给服主
		if notif != nil && rv.h.Gw != nil {
			notifPayload := gin.H{
				"id":         notif.ID,
				"userId":     notif.UserID,
				"type":       notif.Type,
				"sourceType": notif.SourceType,
				"status":     "pending",
				"read":       false,
				"createdAt":  notif.CreatedAt,
				"guildId":    guildID,
				"guild": gin.H{
					"id":          guild.ID,
					"name":        guild.Name,
					"avatar":      guild.Avatar,
					"banner":      guild.Banner,
					"description": guild.Description,
				},
				"authorId": user.ID,
				"author": gin.H{
					"id":     user.ID,
					"name":   user.Name,
					"avatar": user.Avatar,
					"bio":    user.Bio,
					"banner": user.Banner,
				},
			}
			rv.h.Gw.BroadcastNotice(guild.OwnerID, notifPayload)
		}

		c.JSON(http.StatusOK, gin.H{
			"action":  "requested",
			"message": "已向服主发送添加机器人申请",
		})
	}
}

// @Summary Get guilds that a robot has joined (with details)
// @Tags robot-developer
// @Security BearerAuth
// @Produce json
// @Param id path int true "Robot ID"
// @Success 200 {object} map[string]any
// @Router /api/robot/v1/developer/robots/{id}/guilds [get]
func (rv *RobotV1) getRobotJoinedGuilds(c *gin.Context) {
	user := middleware.UserFromCtx(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	robotID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的机器人ID"})
		return
	}

	rb, err := rv.h.Svc.GetRobot(uint(robotID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "机器人不存在"})
		return
	}

	if rb.OwnerID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	guilds, err := rv.h.Svc.Repo.ListRobotJoinedGuilds(rb.BotUserID)
	if err != nil {
		logger.Errorf("[Developer] Failed to list guilds for robot %d: %v", robotID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"guilds": guilds,
		"total":  len(guilds),
	})
}

// @Summary Remove robot from guild
// @Tags robot-developer
// @Security BearerAuth
// @Param id path int true "Robot ID"
// @Param guildId path int true "Guild ID"
// @Success 204
// @Router /api/robot/v1/developer/robots/{id}/guilds/{guildId} [delete]
func (rv *RobotV1) removeRobotFromGuild(c *gin.Context) {
	user := middleware.UserFromCtx(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	robotID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的机器人ID"})
		return
	}

	guildID, err := strconv.ParseUint(c.Param("guildId"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}

	rb, err := rv.h.Svc.GetRobot(uint(robotID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "机器人不存在"})
		return
	}

	if rb.OwnerID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	if err := rv.h.Svc.LeaveGuild(uint(guildID), rb.BotUserID); err != nil {
		logger.Errorf("[Developer] Failed to remove robot %d from guild %d: %v", robotID, guildID, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "退出服务器失败"})
		return
	}

	c.Status(http.StatusNoContent)
}

// @Summary Get webhook call logs
// @Tags robot-developer
// @Security BearerAuth
// @Produce json
// @Param id path int true "Robot ID"
// @Param limit query int false "每页条数(默认20,上限100)"
// @Param offset query int false "偏移量"
// @Success 200 {object} map[string]any
// @Router /api/robot/v1/developer/robots/{id}/webhook/logs [get]
func (rv *RobotV1) getWebhookLogs(c *gin.Context) {
	user := middleware.UserFromCtx(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	robotID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的机器人ID"})
		return
	}

	rb, err := rv.h.Svc.GetRobot(uint(robotID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "机器人不存在"})
		return
	}

	if rb.OwnerID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	logs, total, err := rv.h.Svc.ListWebhookLogs(uint(robotID), limit, offset)
	if err != nil {
		logger.Errorf("[Developer] Failed to get webhook logs for robot %d: %v", robotID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询日志失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"logs":  logs,
		"total": total,
	})
}

// @Summary Update robot avatar
// @Tags robot-developer
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path int true "Robot ID"
// @Param body body map[string]string true "{path: MinIO path}"
// @Success 200 {object} map[string]any
// @Router /api/robot/v1/developer/robots/{id}/avatar [put]
func (rv *RobotV1) updateRobotAvatar(c *gin.Context) {
	user := middleware.UserFromCtx(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的机器人ID"})
		return
	}

	var body struct {
		Path string `json:"path"` // MinIO object name
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误：缺少路径"})
		return
	}

	if body.Path == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "路径不能为空"})
		return
	}

	// 获取机器人
	rb, err := rv.h.Svc.GetRobot(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "机器人不存在"})
		return
	}

	// 验证所有权
	if rb.OwnerID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	// 验证路径格式: {bucket}/{category}/{userID}/{timestamp}.{ext}
	// 对于头像，必须使用 avatars 存储桶
	pathParts := strings.Split(body.Path, "/")
	if len(pathParts) < 3 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "路径格式错误，期望：{存储桶}/{分类}/{用户ID}/..."})
		return
	}

	// 检查第一个部分是否是有效的存储桶
	validBuckets := []string{"avatars", "covers", "emojis", "guild-chat-files", "private-chat-files", "temp", "bubble"}
	bucketName := pathParts[0]
	isValidBucket := false
	for _, b := range validBuckets {
		if b == bucketName {
			isValidBucket = true
			break
		}
	}

	if !isValidBucket {
		c.JSON(http.StatusBadRequest, gin.H{"error": "路径中的存储桶无效，可选：" + strings.Join(validBuckets, ", ")})
		return
	}

	// 对于头像更新，必须使用 avatars 存储桶
	if bucketName != "avatars" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "头像必须在avatars存储桶中"})
		return
	}

	// 验证分类部分（第二个部分）应该是 avatars
	if len(pathParts) >= 2 && pathParts[1] != "avatars" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "头像路径的分类必须为'avatars'"})
		return
	}

	// 更新机器人用户头像
	if err := rv.h.Svc.UpdateAvatar(rb.BotUser, body.Path); err != nil {
		logger.Errorf("[Developer] Failed to update avatar for robot %d: %v", id, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "更新头像失败"})
		return
	}

	// 返回结构化的头像信息
	var avatarURL string
	if rv.h.Svc.MinIO != nil {
		avatarURL = rv.h.Svc.MinIO.GetAvatarURL(body.Path)
	}
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"avatar": gin.H{
				"path": body.Path,
				"url":  avatarURL,
			},
		},
	})
}

// getRobotQuotaDeveloper 开发者查询指定机器人（BotUser）的配额
func (rv *RobotV1) getRobotQuotaDeveloper(c *gin.Context) {
	user := middleware.UserFromCtx(c)
	if user == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的机器人ID"})
		return
	}
	rb, err := rv.h.Svc.GetRobot(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "机器人不存在"})
		return
	}
	if rb.OwnerID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}
	if rv.h.Svc.Repo.Redis == nil {
		c.JSON(http.StatusOK, gin.H{"error": "配额存储服务不可用"})
		return
	}
	now := time.Now()
	dayKey := now.Format("20060102")
	monthKey := now.Format("200601")
	// 使用 BotUserID 作为用量维度，与上传一致
	dailyKey := "bot:upload:daily:" + strconv.Itoa(int(rb.BotUserID)) + ":" + dayKey
	monthlyKey := "bot:upload:monthly:" + strconv.Itoa(int(rb.BotUserID)) + ":" + monthKey
	ctx := c.Request.Context()
	dailyUsed, _ := rv.h.Svc.Repo.Redis.Get(ctx, dailyKey).Int64()
	monthlyUsed, _ := rv.h.Svc.Repo.Redis.Get(ctx, monthlyKey).Int64()

	limits := getQuotaLimits()
	c.JSON(http.StatusOK, gin.H{
		"robotId":   rb.ID,
		"botUserId": rb.BotUserID,
		"date":      dayKey,
		"month":     monthKey,
		"usage": gin.H{
			"dailyUsedBytes":   dailyUsed,
			"monthlyUsedBytes": monthlyUsed,
		},
		"limits": gin.H{
			"withMessage": gin.H{
				"dailyBytes":   limits.DailyWithMsgBytes,
				"monthlyBytes": limits.MonthlyWithMsgBytes,
				"monthlyMax":   limits.MonthlyWithMsgMax,
			},
			"noMessage": gin.H{
				"dailyBytes":   limits.DailyNoMsgBytes,
				"monthlyBytes": limits.MonthlyNoMsgBytes,
			},
		},
	})
}
