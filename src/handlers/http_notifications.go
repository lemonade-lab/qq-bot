package handlers

import (
	"net/http"
	"strconv"

	"bubble/src/config"
	"bubble/src/middleware"
	"bubble/src/service"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// @Summary      List my notifications
// @Tags         me
// @Security     BearerAuth
// @Produce      json
// @Param        limit    query int false "Limit (default 50, max 100)"
// @Param        beforeId query int false "id < beforeId"
// @Param        afterId  query int false "id > afterId"
// @Success      200  {array}  map[string]any
// @Router       /api/me/notifications [get]
func (h *HTTP) listMyNotifications(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	// limit 默认与最大值使用集中常量
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
	notifs, users, guilds, channels, messages, dmMessages, err := h.Svc.ListUserNotificationsWithInfo(u.ID, limit, beforeID, afterID)
	if err != nil {
		logger.Errorf("[Notifications] Failed to list notifications for user %d: %v", u.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取通知列表失败"})
		return
	}

	// 构建完整的响应数据
	result := make([]gin.H, len(notifs))
	for i, n := range notifs {
		item := gin.H{
			"id":         n.ID,
			"userId":     n.UserID,
			"type":       n.Type,
			"sourceType": n.SourceType,
			"read":       n.Read,
			"createdAt":  n.CreatedAt,
		}

		if n.Status != nil {
			item["status"] = *n.Status
		}

		if n.GuildID != nil {
			item["guildId"] = *n.GuildID
			if guild, ok := guilds[*n.GuildID]; ok {
				item["guild"] = gin.H{
					"id":     guild.ID,
					"name":   guild.Name,
					"avatar": guild.Avatar,
				}
			}
		}

		if n.ChannelID != nil {
			item["channelId"] = *n.ChannelID
			if channel, ok := channels[*n.ChannelID]; ok {
				item["channel"] = gin.H{
					"id":   channel.ID,
					"name": channel.Name,
					"type": channel.Type,
				}
			}
		}

		if n.ThreadID != nil {
			item["threadId"] = *n.ThreadID
		}

		if n.MessageID != nil {
			item["messageId"] = *n.MessageID
			// 根据 sourceType 添加消息预览
			if n.SourceType == "channel" {
				if msg, ok := messages[*n.MessageID]; ok {
					item["message"] = gin.H{
						"id":      msg.ID,
						"content": msg.Content,
						"type":    msg.Type,
					}
				}
			} else if n.SourceType == "dm" {
				if dmMsg, ok := dmMessages[*n.MessageID]; ok {
					item["message"] = gin.H{
						"id":      dmMsg.ID,
						"content": dmMsg.Content,
						"type":    dmMsg.Type,
					}
				}
			}
		}

		if n.AuthorID != nil {
			item["authorId"] = *n.AuthorID
			if author, ok := users[*n.AuthorID]; ok {
				item["author"] = gin.H{
					"id":     author.ID,
					"name":   author.Name,
					"avatar": author.Avatar,
				}
			}
		}

		result[i] = item
	}

	c.JSON(http.StatusOK, result)
}

// @Summary      Mark notification as read
// @Tags         me
// @Security     BearerAuth
// @Produce      json
// @Param        id   path  int  true  "Notification ID"
// @Success      204  {string}  string  "no content"
// @Router       /api/me/notifications/{id}/read [put]
func (h *HTTP) markNotificationRead(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := parseUintParam(c, "id")
	if err != nil {
		logger.Errorf("[Notifications] Invalid notification ID: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的通知ID"})
		return
	}
	if err := h.Svc.MarkNotificationRead(id, u.ID); err != nil {
		logger.Errorf("[Notifications] Failed to mark notification %d as read for user %d: %v", id, u.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "标记通知失败"})
		return
	}
	// WS: 推送通知状态更新给用户
	if h.Gw != nil {
		h.Gw.BroadcastNoticeUpdate(u.ID, gin.H{
			"id":   id,
			"read": true,
		})
	}
	c.Status(http.StatusNoContent)
}

// @Summary      Mark multiple notifications as read
// @Tags         me
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        body body object{ids=[]int} true "Notification IDs"
// @Success      204  {string}  string  "no content"
// @Router       /api/me/notifications/read [put]
func (h *HTTP) markNotificationsRead(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	var body struct {
		IDs []uint `json:"ids" binding:"required"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	if len(body.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "编号列表不能为空"})
		return
	}
	if err := h.Svc.MarkNotificationsRead(body.IDs, u.ID); err != nil {
		logger.Errorf("[Notifications] Failed to mark notifications as read for user %d: %v", u.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "批量标记通知失败"})
		return
	}
	// WS: 推送批量通知状态更新给用户
	if h.Gw != nil {
		for _, id := range body.IDs {
			h.Gw.BroadcastNoticeUpdate(u.ID, gin.H{
				"id":   id,
				"read": true,
			})
		}
	}
	c.Status(http.StatusNoContent)
}

// @Summary      Mark all notifications as read
// @Tags         me
// @Security     BearerAuth
// @Produce      json
// @Success      204  {string}  string  "no content"
// @Router       /api/me/notifications/read-all [put]
// @Summary      Mark all notifications as read
// @Description  将所有通知标记为已读
// @Tags         notifications
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Success      200  {object}  map[string]string
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      500  {object}  map[string]string  "服务器错误"
// @Router       /api/notifications/read [post]
func (h *HTTP) markAllNotificationsRead(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	if err := h.Svc.MarkAllNotificationsRead(u.ID); err != nil {
		logger.Errorf("[Notifications] Failed to mark all notifications as read for user %d: %v", u.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "标记所有通知失败"})
		return
	}
	// WS: 推送全部已读通知
	if h.Gw != nil {
		h.Gw.BroadcastNoticeUpdate(u.ID, gin.H{
			"readAll": true,
		})
	}
	c.Status(http.StatusNoContent)
}

// @Summary      Accept friend request notification
// @Description  通过通知接受好友申请
// @Tags         notifications
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Notification ID"
// @Success      200  {object}  map[string]string
// @Failure      400  {object}  map[string]string  "参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      404  {object}  map[string]string  "通知不存在"
// @Router       /api/notifications/{id}/accept [post]
func (h *HTTP) acceptFriendRequestNotification(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的通知ID"})
		return
	}

	// 先获取通知，按类型分发处理
	notif, nerr := h.Svc.Repo.GetNotificationByID(id, u.ID)
	if nerr != nil || notif == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "通知不存在"})
		return
	}

	switch notif.Type {
	case "friend_request":
		if err := h.Svc.AcceptFriendRequestNotification(u.ID, id); err != nil {
			if svcErr, ok := err.(*service.Err); ok {
				c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "接受好友申请失败"})
			return
		}

		// 推送通知状态更新
		if h.Gw != nil {
			h.Gw.BroadcastNoticeUpdate(u.ID, gin.H{
				"id":     id,
				"status": "accepted",
			})
		}

		c.JSON(http.StatusOK, gin.H{"message": "已接受好友申请"})
		return

	case "guild_join_request":
		// 验证状态
		if notif.Status == nil || *notif.Status != "pending" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "申请已处理"})
			return
		}
		if notif.GuildID == nil || notif.AuthorID == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "通知信息缺失"})
			return
		}

		guildID := *notif.GuildID
		applicantID := *notif.AuthorID

		req, rerr := h.Svc.Repo.GetPendingGuildJoinRequest(guildID, applicantID)
		if rerr != nil || req == nil {
			_ = h.Svc.Repo.UpdateNotificationStatus(id, u.ID, "accepted")
			c.JSON(http.StatusBadRequest, gin.H{"error": "加入申请不存在或已处理"})
			return
		}

		if err := h.Svc.ApproveGuildJoinRequest(guildID, req.ID, u.ID); err != nil {
			if err == service.ErrUnauthorized {
				c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
				return
			}
			if err == service.ErrNotFound {
				c.JSON(http.StatusNotFound, gin.H{"error": "请求不存在"})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": "批准申请失败"})
			return
		}

		// 更新通知状态
		_ = h.Svc.Repo.UpdateNotificationStatus(id, u.ID, "accepted")

		// 推送通知状态更新 + 成员加入 + 通知申请者
		if h.Gw != nil {
			h.Gw.BroadcastNoticeUpdate(u.ID, gin.H{
				"id":     id,
				"status": "accepted",
			})

			h.Gw.BroadcastToGuild(guildID, config.EventGuildMemberAdd, gin.H{"userId": applicantID})

			guild, _ := h.Svc.Repo.GetGuild(guildID)
			notificationPayload := gin.H{
				"type":       "guild_join_approved",
				"sourceType": "system",
				"guildId":    guildID,
				"requestId":  req.ID,
				"status":     "approved",
				"read":       false,
			}
			if guild != nil {
				notificationPayload["guild"] = gin.H{
					"id":     guild.ID,
					"name":   guild.Name,
					"avatar": guild.Avatar,
				}
			}
			h.Gw.BroadcastNotice(applicantID, notificationPayload)
		}

		c.JSON(http.StatusOK, gin.H{"message": "已批准入会申请"})
		return
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "通知类型错误"})
		return
	}
}

// @Summary      Reject friend request notification
// @Description  通过通知拒绝好友申请
// @Tags         notifications
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id   path  int  true  "Notification ID"
// @Success      200  {object}  map[string]string
// @Failure      400  {object}  map[string]string  "参数错误"
// @Failure      401  {object}  map[string]string  "未认证"
// @Failure      404  {object}  map[string]string  "通知不存在"
// @Router       /api/notifications/{id}/reject [post]
func (h *HTTP) rejectFriendRequestNotification(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	id, err := parseUintParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的通知ID"})
		return
	}

	// 先获取通知，按类型分发处理
	notif, nerr := h.Svc.Repo.GetNotificationByID(id, u.ID)
	if nerr != nil || notif == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "通知不存在"})
		return
	}

	switch notif.Type {
	case "friend_request":
		if err := h.Svc.RejectFriendRequestNotification(u.ID, id); err != nil {
			if svcErr, ok := err.(*service.Err); ok {
				c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "拒绝好友申请失败"})
			return
		}

		// 推送通知状态更新
		if h.Gw != nil {
			h.Gw.BroadcastNoticeUpdate(u.ID, gin.H{
				"id":     id,
				"status": "rejected",
			})
		}

		c.JSON(http.StatusOK, gin.H{"message": "已拒绝好友申请"})
		return

	case "guild_join_request":
		// 验证状态
		if notif.Status == nil || *notif.Status != "pending" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "申请已处理"})
			return
		}
		if notif.GuildID == nil || notif.AuthorID == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "通知信息缺失"})
			return
		}

		guildID := *notif.GuildID
		applicantID := *notif.AuthorID

		req, rerr := h.Svc.Repo.GetPendingGuildJoinRequest(guildID, applicantID)
		if rerr != nil || req == nil {
			_ = h.Svc.Repo.UpdateNotificationStatus(id, u.ID, "rejected")
			c.JSON(http.StatusBadRequest, gin.H{"error": "加入申请不存在或已处理"})
			return
		}

		if err := h.Svc.RejectGuildJoinRequest(guildID, req.ID, u.ID); err != nil {
			if err == service.ErrUnauthorized {
				c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
				return
			}
			if err == service.ErrNotFound {
				c.JSON(http.StatusNotFound, gin.H{"error": "请求不存在"})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": "拒绝申请失败"})
			return
		}

		// 更新通知状态
		_ = h.Svc.Repo.UpdateNotificationStatus(id, u.ID, "rejected")

		// 推送通知状态更新 + 通知申请者
		if h.Gw != nil {
			h.Gw.BroadcastNoticeUpdate(u.ID, gin.H{
				"id":     id,
				"status": "rejected",
			})

			guild, _ := h.Svc.Repo.GetGuild(guildID)
			notificationPayload := gin.H{
				"type":       "guild_join_rejected",
				"sourceType": "system",
				"guildId":    guildID,
				"requestId":  req.ID,
				"status":     "rejected",
				"read":       false,
			}
			if guild != nil {
				notificationPayload["guild"] = gin.H{
					"id":     guild.ID,
					"name":   guild.Name,
					"avatar": guild.Avatar,
				}
			}
			h.Gw.BroadcastNotice(applicantID, notificationPayload)
		}

		c.JSON(http.StatusOK, gin.H{"message": "已拒绝入会申请"})
		return
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "通知类型错误"})
		return
	}
}
