package handlers

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"bubble/src/config"
	"bubble/src/db/models"
	"bubble/src/middleware"
	"bubble/src/service"

	"encoding/json"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
)

// RobotV1 handles all bot API v1 endpoints
// 机器人开放平台 API v1 版本
type RobotV1 struct {
	h *HTTP
}

// NewRobotV1 creates a new RobotV1 handler
func NewRobotV1(h *HTTP) *RobotV1 {
	return &RobotV1{h: h}
}

// svcMentionResolver 实现 MentionResolver, 通过 Service 查询用户/频道名称。
type svcMentionResolver struct {
	svc *service.Service
}

func (r *svcMentionResolver) ResolveUserName(uid uint) (string, string, bool) {
	usr, err := r.svc.GetUserByID(uid)
	if err != nil || usr == nil {
		return "", "", false
	}
	return usr.Name, usr.Avatar, true
}

func (r *svcMentionResolver) ResolveChannelName(cid uint) (string, bool) {
	ch, err := r.svc.Repo.GetChannel(cid)
	if err != nil || ch == nil {
		return "", false
	}
	return ch.Name, true
}

// ==================== Robot Constants ====================
// 统一管理本文件使用的可调参数，避免散落的魔法数字
const (
	// 开发者最多可创建的机器人数量
	MaxRobotsPerOwner = 3

	// WebSocket 心跳间隔（毫秒）
	BotGatewayHeartbeatIntervalMs = 30000

	// 机器人文件上传大小上限（字节）
	BotUploadMaxBytes = 15 * 1024 * 1024 // 15MB

	// ====== 配额相关（以 GiB 为单位的可读常量） ======
	QuotaGiB                 int64   = 1024 * 1024 * 1024
	QuotaDailyWithMsgGiB     int64   = 80
	QuotaMonthlyWithMsgGiB   int64   = 2000
	QuotaMonthlyOveragePct   float64 = 0.10 // 允许超额比例
	QuotaDailyNoMsgGiB       int64   = 2
	QuotaMonthlyNoMsgGiB     int64   = 60
	QuotaMonthlyUnlimitedGiB int64   = 10 // 无限制上传（无需关联消息、无每日限额）每月上限

	// ====== 分页与列表限制 ======
	DefaultChannelMessageLimit = 50
	MaxChannelMessageLimit     = 100
	DefaultDmMessageLimit      = 50
	MaxDmMessageLimit          = 100
	DefaultGuildMembersLimit   = 100
	MaxGuildMembersLimit       = 1000

	// ====== 存储分类与桶名称 ======
	CategoryGuildChatFiles   = "guild-chat-files"
	CategoryPrivateChatFiles = "private-chat-files"
	CategoryGroupChatFiles   = "private-chat-files" // 群聊文件与私聊共用桶
)

// RegisterPublic registers public robot routes (no auth required)
// 公开的机器人路由 - 用于分享页面、热门列表、搜索、排行榜
func (rv *RobotV1) RegisterPublic(r gin.IRouter) {
	// 获取机器人公开信息(用于分享页面)
	r.GET("/share/:id", rv.getRobotPublicInfo)
	// 热门机器人列表
	r.GET("/hot", rv.hotRobots)
	// 搜索机器人（按名字）
	r.GET("/search", rv.searchRobots)
	// 机器人分类列表
	r.GET("/categories", rv.listRobotCategories)
	// 机器人排行榜
	r.GET("/ranking", rv.getRobotRankings)
	// 排行榜查询类型枚举
	r.GET("/ranking/enums", rv.getRankingEnums)
}

// RegisterDeveloper registers developer management routes (owner auth required)
// 开发者管理路由 - 需要登录用户身份（作为机器人所有者）

// RegisterBot registers bot API routes
// 机器人 API 路由 - 需要机器人 token 认证
func (rv *RobotV1) RegisterBot(r gin.IRouter) {
	// 机器人需要 token 认证
	r.Use(middleware.BotAuthRequired(rv.h.Svc))

	// Bot 信息
	r.GET("/me", rv.botMe)

	// 消息相关
	r.POST("/channels/:channelId/messages", rv.botPostMessage)
	r.GET("/channels/:channelId/messages", rv.botGetMessages)
	r.GET("/channels/:channelId/messages/:messageId", rv.botGetMessage)
	r.DELETE("/channels/:channelId/messages/:messageId", rv.botDeleteMessage)
	r.PUT("/channels/:channelId/messages/:messageId", rv.botEditMessage)

	// 频道相关
	r.GET("/guilds/:guildId/channels", rv.botListGuildChannels)
	r.GET("/channels/:channelId", rv.botGetChannel)

	// 成员相关
	r.GET("/guilds/:guildId/members", rv.botListGuildMembers)
	r.GET("/guilds/:guildId/members/:userId", rv.botGetGuildMember)

	// 私聊相关
	r.GET("/users/:userId/dm", rv.botGetOrCreateDM)
	r.POST("/dm/threads/:threadId/messages", rv.botSendDmMessage)
	r.GET("/dm/threads/:threadId/messages", rv.botGetDmMessages)

	// 交互消息（用户发给机器人的隐藏消息）
	r.GET("/interactions", rv.botGetInteractions)

	// Webhook 回调测试
	r.POST("/webhooks/test", rv.testWebhook)

	// 文件上传（机器人）
	r.POST("/files/upload", rv.botUploadFile)

	// 机器人查询自身配额
	r.GET("/files/quota", rv.getRobotQuota)

	// ===== P0: Reactions =====
	r.GET("/channels/:channelId/messages/:messageId/reactions", rv.botListReactions)
	r.PUT("/channels/:channelId/messages/:messageId/reactions/:emoji", rv.botAddReaction)
	r.DELETE("/channels/:channelId/messages/:messageId/reactions/:emoji", rv.botRemoveReaction)

	// ===== P0: Pins =====
	r.GET("/channels/:channelId/pins", rv.botListPins)
	r.POST("/channels/:channelId/pins", rv.botPinMessage)
	r.DELETE("/channels/:channelId/pins/:messageId", rv.botUnpinMessage)

	// ===== P0: Batch delete =====
	r.POST("/channels/:channelId/messages/batch-delete", rv.botBatchDeleteMessages)

	// ===== P1: Member management =====
	r.DELETE("/guilds/:guildId/members/:userId", rv.botKickMember)
	r.PUT("/guilds/:guildId/members/:userId/mute", rv.botMuteMember)
	r.DELETE("/guilds/:guildId/members/:userId/mute", rv.botUnmuteMember)
	r.PUT("/guilds/:guildId/members/:userId/nickname", rv.botSetMemberNickname)

	// ===== P1: Role management =====
	r.GET("/guilds/:guildId/roles", rv.botListRoles)
	r.POST("/guilds/:guildId/roles", rv.botCreateRole)
	r.PUT("/guilds/:guildId/roles/:roleId", rv.botUpdateRole)
	r.DELETE("/guilds/:guildId/roles/:roleId", rv.botDeleteRole)
	r.POST("/guilds/:guildId/roles/:roleId/assign/:userId", rv.botAssignRole)
	r.POST("/guilds/:guildId/roles/:roleId/remove/:userId", rv.botRemoveRole)

	// ===== P1: Channel management =====
	r.POST("/guilds/:guildId/channels", rv.botCreateChannel)
	r.PUT("/channels/:channelId/settings", rv.botUpdateChannel)
	r.DELETE("/channels/:channelId", rv.botDeleteChannel)

	// ===== P2: Announcements =====
	r.GET("/guilds/:guildId/announcements", rv.botListAnnouncements)
	r.POST("/guilds/:guildId/announcements", rv.botCreateAnnouncement)
	r.GET("/announcements/:announcementId", rv.botGetAnnouncement)
	r.PUT("/announcements/:announcementId", rv.botUpdateAnnouncement)
	r.DELETE("/announcements/:announcementId", rv.botDeleteAnnouncement)
	r.PUT("/announcements/:announcementId/pin", rv.botPinAnnouncement)
	r.DELETE("/announcements/:announcementId/pin", rv.botUnpinAnnouncement)

	// ===== P2: Join requests =====
	r.GET("/guilds/:guildId/join-requests", rv.botListJoinRequests)
	r.POST("/guilds/:guildId/join-requests/:requestId/approve", rv.botApproveJoinRequest)
	r.POST("/guilds/:guildId/join-requests/:requestId/reject", rv.botRejectJoinRequest)

	// ===== P2: List guilds =====
	r.GET("/guilds", rv.botListGuilds)

	// ===== P2: DM edit/delete =====
	r.PUT("/dm/messages/:messageId", rv.botEditDmMessage)
	r.DELETE("/dm/messages/:messageId", rv.botDeleteDmMessage)

	// ===== P2: Channel categories =====
	r.GET("/guilds/:guildId/channel-categories", rv.botListChannelCategories)
	r.POST("/guilds/:guildId/channel-categories", rv.botCreateChannelCategory)
	r.PUT("/channel-categories/:categoryId", rv.botUpdateChannelCategory)
	r.DELETE("/channel-categories/:categoryId", rv.botDeleteChannelCategory)
}

// ==================== Bot APIs ====================

// @Summary Get bot info
// @Tags robot-bot
// @Security BotAuth
// @Produce json
// @Success 200 {object} map[string]any
// @Router /api/robot/v1/bot/me [get]
func (rv *RobotV1) botMe(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	robot := middleware.RobotFromCtx(c)
	if robot == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "机器人不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":       robot.ID,
		"botUser":  u,
		"robotId":  robot.ID,
		"ownerId":  robot.OwnerID,
		"verified": true,
	})
}

// @Summary Post message to channel
// @Tags robot-bot
// @Security BotAuth
// @Accept json
// @Produce json
// @Param channelId path int true "Channel ID"
// @Param body body map[string]any true "{content,type,embed}"
// @Success 200 {object} map[string]any
// @Router /api/robot/v1/bot/channels/{channelId}/messages [post]
func (rv *RobotV1) botPostMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	channelID, err := strconv.ParseUint(c.Param("channelId"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的频道ID"})
		return
	}
	// 仅支持 JSON 载荷：
	// application/json: { content?, type?, embed?, attachments?: [{path, url, contentType, size, filename}] }

	var (
		content     string
		mType       string
		embed       map[string]any
		attachments []map[string]any
		replyToID   *uint
	)

	var notifyMentions bool // 机器人默认不触发 mention 通知，需显式 opt-in

	ct := c.GetHeader("Content-Type")
	if strings.HasPrefix(ct, "application/json") {
		var body struct {
			Content        string         `json:"content"`
			Type           string         `json:"type"`
			Embed          map[string]any `json:"embed"`
			ReplyToID      *uint          `json:"replyToId"`
			NotifyMentions bool           `json:"notify_mentions"` // 可选：是否触发 @mention 通知，默认 false
			Attachments    []struct {
				Path        string `json:"path"`
				URL         string `json:"url"`
				ContentType string `json:"contentType"`
				Size        int64  `json:"size"`
				Filename    string `json:"filename"`
				Width       int    `json:"width,omitempty"`
				Height      int    `json:"height,omitempty"`
			} `json:"attachments"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			logger.Errorf("[RobotAPI] Invalid request body for posting message: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
			return
		}
		content = body.Content
		mType = body.Type
		embed = body.Embed
		replyToID = body.ReplyToID
		notifyMentions = body.NotifyMentions
		for _, a := range body.Attachments {
			att := gin.H{
				"path":        a.Path,
				"url":         a.URL,
				"contentType": a.ContentType,
				"size":        a.Size,
				"filename":    a.Filename,
			}
			if a.Width > 0 {
				att["width"] = a.Width
			}
			if a.Height > 0 {
				att["height"] = a.Height
			}
			// 如果客户端未提供宽高且是图片，则服务端检测
			if a.Width == 0 && a.Height == 0 && a.Path != "" && isImageContentType(a.ContentType) {
				if rv.h.Svc.MinIO != nil {
					if dims, err := rv.h.Svc.MinIO.DetectMediaDimensions(c.Request.Context(), a.Path, a.ContentType); err == nil && dims != nil {
						att["width"] = dims.Width
						att["height"] = dims.Height
					}
				}
			}
			attachments = append(attachments, att)
		}
	} else if strings.HasPrefix(ct, "multipart/form-data") {
		// 明确拒绝 form-data：要求先调用文件上传接口再以 JSON 引用附件
		c.JSON(http.StatusBadRequest, gin.H{"error": "不支持multipart；请先调用 /api/robot/v1/bot/files/upload 上传文件，然后在消息JSON中引用attachments"})
		return
	} else {
		// 默认按 JSON 解析尝试
		var body struct {
			Content        string         `json:"content"`
			Type           string         `json:"type"`
			Embed          map[string]any `json:"embed"`
			ReplyToID      *uint          `json:"replyToId"`
			NotifyMentions bool           `json:"notify_mentions"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "不支持的Content-Type"})
			return
		}
		content = body.Content
		mType = body.Type
		notifyMentions = body.NotifyMentions
		embed = body.Embed
		replyToID = body.ReplyToID
	}

	if mType == "" {
		if len(attachments) > 0 {
			// 根据首个附件类型推断
			if ct0, ok := attachments[0]["contentType"].(string); ok {
				if strings.HasPrefix(ct0, "image/") {
					mType = "image"
				} else if strings.HasPrefix(ct0, "audio/") {
					mType = "voice"
				} else {
					mType = "file"
				}
			} else {
				mType = "file"
			}
		} else {
			mType = "text"
		}
	}

	// 将附件信息塞入扩展字段（embed），以保持向后兼容
	if len(attachments) > 0 {
		if embed == nil {
			embed = map[string]any{}
		}
		embed["attachments"] = attachments
	}

	// 创建消息 - 使用 AddMessage 服务（扩展字段传入 embed JSON）
	var embedJSON datatypes.JSON
	if embed != nil {
		if b, err := json.Marshal(embed); err == nil {
			embedJSON = datatypes.JSON(b)
		}
	}

	// 自动解析消息内容中的简写格式（<@uid> <#cid> <@everyone>），生成结构化 mentions 数据并清理无效引用
	resolver := &svcMentionResolver{svc: rv.h.Svc}
	content, parsedMentions := parseMentionsFromContent(content, resolver)
	var mentionsJSON datatypes.JSON
	if len(parsedMentions) > 0 {
		if bs, err := json.Marshal(parsedMentions); err == nil {
			mentionsJSON = datatypes.JSON(bs)
		}
	}

	msg, err := rv.h.Svc.AddMessage(uint(channelID), u.ID, u.Name, content, replyToID, mType, "web", embedJSON, "", mentionsJSON)
	if err != nil {
		logger.Errorf("[RobotAPI] Failed to post message to channel %d: %v", channelID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "发送消息失败"})
		return
	}

	// 检测并异步转换音频文件（不阻塞响应）
	if len(attachments) > 0 {
		// 检查第一个附件是否为音频
		rv.h.handleAudioConversion(c.Request.Context(), attachments[0], u.ID, nil)
	}

	// 通过 Gateway 广播消息（使用与普通用户相同的 payload 构建）
	if rv.h.Gw != nil {
		payload := rv.h.buildChannelMessagePayload(msg)
		// 添加 mentions 数据到广播载荷
		if len(parsedMentions) > 0 {
			payload["mentions"] = parsedMentions
		} else {
			payload["mentions"] = []map[string]any{}
		}
		rv.h.Gw.BroadcastToChannel(uint(channelID), config.EventMessageCreate, payload, u.ID)

		// 提取被 @mention 的用户 ID（用于红点中的 mentionCount）
		// 机器人默认不触发 mention 通知；仅当 notify_mentions=true 时才传递
		var mentionedUserIDs []uint
		if notifyMentions {
			for _, m := range parsedMentions {
				if t, ok := m["type"].(string); ok && t == "user" {
					if uid, ok := m["id"].(uint); ok && uid > 0 {
						mentionedUserIDs = append(mentionedUserIDs, uid)
					}
				}
			}
		}

		// 异步更新红点计数并广播 ReadState 事件
		// 注意：机器人消息只触发红点，不生成通知（不调用 BroadcastMention / CreateMentionNotification）
		go func() {
			if err := rv.h.Svc.OnNewChannelMessage(uint(channelID), msg.ID, u.ID, mentionedUserIDs); err != nil {
				logger.Warnf("[RobotAPI] Failed to update unread counts for channel %d: %v", channelID, err)
				return
			}

			ch, err := rv.h.Svc.GetChannel(uint(channelID))
			if err != nil {
				return
			}

			members, err := rv.h.Svc.Repo.ListMembers(ch.GuildID)
			if err != nil {
				return
			}

			for _, member := range members {
				if member.UserID == u.ID {
					continue
				}
				// 广播频道级别的 READ_STATE_UPDATE
				channelRS, _ := rv.h.Svc.Repo.GetReadState(member.UserID, "channel", uint(channelID))
				if channelRS != nil {
					rv.h.Gw.BroadcastToUsers([]uint{member.UserID}, config.EventReadStateUpdate, gin.H{
						"type":              "channel",
						"id":                uint(channelID),
						"lastReadMessageId": channelRS.LastReadMessageID,
						"unreadCount":       channelRS.UnreadCount,
						"mentionCount":      channelRS.MentionCount,
					})
				}
				// 广播公会级别的 READ_STATE_UPDATE
				guildRS, _ := rv.h.Svc.Repo.GetReadState(member.UserID, "guild", ch.GuildID)
				if guildRS != nil {
					rv.h.Gw.BroadcastToUsers([]uint{member.UserID}, config.EventReadStateUpdate, gin.H{
						"type":              "guild",
						"id":                ch.GuildID,
						"lastReadMessageId": guildRS.LastReadMessageID,
						"unreadCount":       guildRS.UnreadCount,
						"mentionCount":      guildRS.MentionCount,
					})
				}
			}
		}()
	}

	// 返回包含附件的富响应
	resp := gin.H{
		"id":        msg.ID,
		"channelId": msg.ChannelID,
		"content":   msg.Content,
		"type":      msg.Type,
		"authorId":  msg.AuthorID,
		"author":    msg.Author,
		"createdAt": msg.CreatedAt,
	}
	if len(attachments) > 0 {
		resp["attachments"] = attachments
	}
	c.JSON(http.StatusOK, resp)
}

// @Summary Get messages from channel
// @Tags robot-bot
// @Security BotAuth
// @Produce json
// @Param channelId path int true "Channel ID"
// @Param limit query int false "Limit (default 50, max 100)"
// @Param before query int false "Message ID to get messages before"
// @Success 200 {array} map[string]any
// @Router /api/robot/v1/bot/channels/{channelId}/messages [get]
func (rv *RobotV1) botGetMessages(c *gin.Context) {
	channelID, err := strconv.ParseUint(c.Param("channelId"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的频道ID"})
		return
	}

	limit := DefaultChannelMessageLimit
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= MaxChannelMessageLimit {
			limit = parsed
		}
	}

	before := uint(0)
	if b := c.Query("before"); b != "" {
		if parsed, err := strconv.ParseUint(b, 10, 64); err == nil {
			before = uint(parsed)
		}
	}

	messages, err := rv.h.Svc.GetMessagesWithPagination(uint(channelID), limit, before)
	if err != nil {
		logger.Errorf("[RobotAPI] Failed to get messages from channel %d: %v", channelID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取消息失败"})
		return
	}

	c.JSON(http.StatusOK, messages)
}

// @Summary Get message by ID
// @Tags robot-bot
// @Security BotAuth
// @Produce json
// @Param channelId path int true "Channel ID"
// @Param messageId path int true "Message ID"
// @Success 200 {object} map[string]any
// @Router /api/robot/v1/bot/channels/{channelId}/messages/{messageId} [get]
func (rv *RobotV1) botGetMessage(c *gin.Context) {
	channelID, err := strconv.ParseUint(c.Param("channelId"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的频道ID"})
		return
	}

	messageID, err := strconv.ParseUint(c.Param("messageId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的消息ID"})
		return
	}

	msg, err := rv.h.Svc.Repo.GetMessageByID(uint(messageID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "消息不存在"})
		return
	}

	// 确保消息属于指定频道
	if msg.ChannelID != uint(channelID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "消息不存在"})
		return
	}

	c.JSON(http.StatusOK, msg)
}

// @Summary Delete message
// @Tags robot-bot
// @Security BotAuth
// @Param channelId path int true "Channel ID"
// @Param messageId path int true "Message ID"
// @Success 204
// @Router /api/robot/v1/bot/channels/{channelId}/messages/{messageId} [delete]
func (rv *RobotV1) botDeleteMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	messageID, err := strconv.ParseUint(c.Param("messageId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的消息ID"})
		return
	}

	msg, err := rv.h.Svc.Repo.GetMessageByID(uint(messageID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "消息不存在"})
		return
	}

	// 只能删除自己的消息
	if msg.AuthorID != u.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	if err := rv.h.Svc.Repo.DeleteMessage(uint(messageID)); err != nil {
		logger.Errorf("[RobotAPI] Failed to delete message %d: %v", messageID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除消息失败"})
		return
	}

	// 广播删除事件
	if rv.h.Gw != nil {
		payload := gin.H{
			"id":        msg.ID,
			"channelId": msg.ChannelID,
		}
		// 添加 guildId
		if ch, err := rv.h.Svc.Repo.GetChannel(msg.ChannelID); err == nil && ch != nil {
			payload["guildId"] = ch.GuildID
		}
		rv.h.Gw.BroadcastToChannel(msg.ChannelID, config.EventMessageDelete, payload, u.ID)
	}

	c.Status(http.StatusNoContent)
}

// @Summary Edit message
// @Tags robot-bot
// @Security BotAuth
// @Accept json
// @Produce json
// @Param channelId path int true "Channel ID"
// @Param messageId path int true "Message ID"
// @Param body body map[string]string true "{content}"
// @Success 200 {object} map[string]any
// @Router /api/robot/v1/bot/channels/{channelId}/messages/{messageId} [put]
func (rv *RobotV1) botEditMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	messageID, err := strconv.ParseUint(c.Param("messageId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的消息ID"})
		return
	}

	var body struct {
		Content string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}

	msg, err := rv.h.Svc.Repo.GetMessageByID(uint(messageID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "消息不存在"})
		return
	}

	if msg.AuthorID != u.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	// 重新解析编辑后内容中的简写格式并清理无效引用
	resolver := &svcMentionResolver{svc: rv.h.Svc}
	cleanedContent, parsedMentions := parseMentionsFromContent(body.Content, resolver)
	msg.Content = cleanedContent
	if len(parsedMentions) > 0 {
		if bs, err := json.Marshal(parsedMentions); err == nil {
			msg.Mentions = datatypes.JSON(bs)
		}
	} else {
		msg.Mentions = nil
	}

	if err := rv.h.Svc.Repo.UpdateMessage(msg); err != nil {
		logger.Errorf("[RobotAPI] Failed to edit message %d: %v", messageID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "编辑消息失败"})
		return
	}

	// 广播更新事件（使用与普通用户相同的 payload 构建）
	if rv.h.Gw != nil {
		payload := rv.h.buildChannelMessagePayload(msg)
		if len(parsedMentions) > 0 {
			payload["mentions"] = parsedMentions
		} else {
			payload["mentions"] = []map[string]any{}
		}
		rv.h.Gw.BroadcastToChannel(msg.ChannelID, config.EventMessageUpdate, payload, u.ID)
	}

	c.JSON(http.StatusOK, msg)
}

// @Summary List guild channels
// @Tags robot-bot
// @Security BotAuth
// @Produce json
// @Param guildId path int true "Guild ID"
// @Success 200 {array} map[string]any
// @Router /api/robot/v1/bot/guilds/{guildId}/channels [get]
func (rv *RobotV1) botListGuildChannels(c *gin.Context) {
	guildID, err := strconv.ParseUint(c.Param("guildId"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}

	channels, err := rv.h.Svc.Repo.GetChannelsByGuildID(uint(guildID))
	if err != nil {
		logger.Errorf("[RobotAPI] Failed to list channels in guild %d: %v", guildID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取频道列表失败"})
		return
	}

	c.JSON(http.StatusOK, channels)
}

// @Summary Get channel info
// @Tags robot-bot
// @Security BotAuth
// @Produce json
// @Param channelId path int true "Channel ID"
// @Success 200 {object} map[string]any
// @Router /api/robot/v1/bot/channels/{channelId} [get]
func (rv *RobotV1) botGetChannel(c *gin.Context) {
	channelID, err := strconv.ParseUint(c.Param("channelId"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的频道ID"})
		return
	}

	channel, err := rv.h.Svc.Repo.GetChannelByID(uint(channelID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "频道不存在"})
		return
	}

	c.JSON(http.StatusOK, channel)
}

// @Summary List guild members
// @Tags robot-bot
// @Security BotAuth
// @Produce json
// @Param guildId path int true "Guild ID"
// @Param limit query int false "Limit (default 100, max 1000)"
// @Success 200 {array} map[string]any
// @Router /api/robot/v1/bot/guilds/{guildId}/members [get]
func (rv *RobotV1) botListGuildMembers(c *gin.Context) {
	guildID, err := strconv.ParseUint(c.Param("guildId"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}

	limit := DefaultGuildMembersLimit
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= MaxGuildMembersLimit {
			limit = parsed
		}
	}

	members, err := rv.h.Svc.Repo.GetMembersByGuildID(uint(guildID), limit)
	if err != nil {
		logger.Errorf("[RobotAPI] Failed to list members in guild %d: %v", guildID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取成员列表失败"})
		return
	}

	c.JSON(http.StatusOK, members)
}

// @Summary Get guild member
// @Tags robot-bot
// @Security BotAuth
// @Produce json
// @Param guildId path int true "Guild ID"
// @Param userId path int true "User ID"
// @Success 200 {object} map[string]any
// @Router /api/robot/v1/bot/guilds/{guildId}/members/{userId} [get]
func (rv *RobotV1) botGetGuildMember(c *gin.Context) {
	guildID, err := strconv.ParseUint(c.Param("guildId"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}

	userID, err := strconv.ParseUint(c.Param("userId"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的用户ID"})
		return
	}

	member, err := rv.h.Svc.Repo.GetMemberByGuildAndUser(uint(guildID), uint(userID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "成员不存在"})
		return
	}

	c.JSON(http.StatusOK, member)
}

// @Summary Test webhook
// @Tags robot-bot
// @Security BotAuth
// @Accept json
// @Produce json
// @Param body body map[string]any true "Test payload"
// @Success 200 {object} map[string]any
// @Router /api/robot/v1/bot/webhooks/test [post]
func (rv *RobotV1) testWebhook(c *gin.Context) {
	robot := middleware.RobotFromCtx(c)
	if robot == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		logger.Errorf("[RobotAPI] Invalid webhook test payload: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}

	// 发送 webhook 测试请求
	result, err := rv.h.Svc.SendWebhookTest(robot.ID, payload)
	if err != nil {
		logger.Errorf("[Robot] Failed to send webhook test for robot %d: %v", robot.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "发送Webhook测试失败"})
		return
	}

	c.JSON(http.StatusOK, result)
}

// ==================== Bot DM APIs ====================

// @Summary Get or create DM thread with user
// @Tags robot-bot
// @Security BotAuth
// @Produce json
// @Param userId path int true "User ID"
// @Success 200 {object} map[string]any
// @Router /api/robot/v1/bot/users/{userId}/dm [get]
func (rv *RobotV1) botGetOrCreateDM(c *gin.Context) {
	robot := middleware.RobotFromCtx(c)
	if robot == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	// 确保 BotUser 已加载
	if robot.BotUser == nil && robot.BotUserID != 0 {
		if u, err := rv.h.Svc.GetUserByID(robot.BotUserID); err == nil {
			robot.BotUser = u
		}
	}
	if robot.BotUser == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "机器人用户不存在"})
		return
	}

	userID, err := strconv.ParseUint(c.Param("userId"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的用户ID"})
		return
	}

	// 检查目标用户是否存在
	targetUser, err := rv.h.Svc.GetUserByID(uint(userID))
	if err != nil || targetUser == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}

	// 创建或获取 DM 线程（机器人不受私聊限制，guildID 传 nil）
	thread, err := rv.h.Svc.OpenDm(robot.BotUserID, uint(userID), nil)
	if err != nil {
		logger.Errorf("[RobotAPI] Failed to open DM with user %d: %v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "打开私聊失败"})
		return
	}

	c.JSON(http.StatusOK, thread)
}

// @Summary Send DM message
// @Tags robot-bot
// @Security BotAuth
// @Accept json
// @Produce json
// @Param threadId path int true "Thread ID"
// @Param body body map[string]string true "{content}"
// @Success 200 {object} map[string]any
// @Router /api/robot/v1/bot/dm/threads/{threadId}/messages [post]
func (rv *RobotV1) botSendDmMessage(c *gin.Context) {
	robot := middleware.RobotFromCtx(c)
	if robot == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	// 确保 BotUser 已加载
	if robot.BotUser == nil && robot.BotUserID != 0 {
		if u, err := rv.h.Svc.GetUserByID(robot.BotUserID); err == nil {
			robot.BotUser = u
		}
	}
	if robot.BotUser == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "机器人用户不存在"})
		return
	}

	threadID, err := strconv.ParseUint(c.Param("threadId"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的私信线程ID"})
		return
	}

	var body struct {
		Content  string         `json:"content" binding:"required"`
		FileMeta map[string]any `json:"fileMeta"` // 支持文件元数据
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		logger.Errorf("[Robot] Invalid request body for DM: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}

	// 验证机器人是此线程的参与者
	var thread models.DmThread
	if err := rv.h.Svc.Repo.DB.First(&thread, threadID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "私信线程不存在"})
		return
	}

	if thread.UserAID != robot.BotUserID && thread.UserBID != robot.BotUserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "机器人不在该私信线程中"})
		return
	}

	// 发送消息
	message, err := rv.h.Svc.SendDm(robot.BotUserID, uint(threadID), body.Content, nil, "text", "web", nil, "")
	if err != nil {
		logger.Errorf("[RobotAPI] Failed to send DM to thread %d: %v", threadID, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "发送私信失败"})
		return
	}

	// 检测并异步转换音频文件（不阻塞响应）
	if body.FileMeta != nil {
		rv.h.handleAudioConversion(c.Request.Context(), body.FileMeta, robot.BotUserID, nil)
	}

	// 广播事件
	if rv.h.Gw != nil {
		rv.h.Gw.BroadcastToDM(uint(threadID), config.EventDmMessageCreate, message, []uint{thread.UserAID, thread.UserBID})

		// 通知对方用户
		otherUserID := thread.UserAID
		if otherUserID == robot.BotUserID {
			otherUserID = thread.UserBID
		}
		rv.h.Gw.BroadcastToUsers([]uint{otherUserID}, config.EventDmMessageCreate, message)
	}

	c.JSON(http.StatusOK, message)
}

// @Summary Get DM messages
// @Tags robot-bot
// @Security BotAuth
// @Produce json
// @Param threadId path int true "Thread ID"
// @Param limit query int false "Limit (default 50, max 100)"
// @Param beforeId query int false "Get messages before this ID"
// @Param afterId query int false "Get messages after this ID"
// @Success 200 {array} map[string]any
// @Router /api/robot/v1/bot/dm/threads/{threadId}/messages [get]
func (rv *RobotV1) botGetDmMessages(c *gin.Context) {
	robot := middleware.RobotFromCtx(c)
	if robot == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	// 确保 BotUser 已加载
	if robot.BotUser == nil && robot.BotUserID != 0 {
		if u, err := rv.h.Svc.GetUserByID(robot.BotUserID); err == nil {
			robot.BotUser = u
		}
	}
	if robot.BotUser == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "机器人用户不存在"})
		return
	}

	threadID, err := strconv.ParseUint(c.Param("threadId"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的私信线程ID"})
		return
	}

	// 验证机器人是此线程的参与者
	var thread models.DmThread
	if err := rv.h.Svc.Repo.DB.First(&thread, threadID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "私信线程不存在"})
		return
	}

	if thread.UserAID != robot.BotUserID && thread.UserBID != robot.BotUserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "机器人不在该私信线程中"})
		return
	}

	// 解析查询参数
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit <= 0 || limit > MaxDmMessageLimit {
		limit = DefaultDmMessageLimit
	}

	beforeID, _ := strconv.ParseUint(c.Query("beforeId"), 10, 64)
	afterID, _ := strconv.ParseUint(c.Query("afterId"), 10, 64)

	// 获取消息
	messages, err := rv.h.Svc.GetDmMessages(robot.BotUserID, uint(threadID), limit, uint(beforeID), uint(afterID))
	if err != nil {
		logger.Errorf("[RobotAPI] Failed to get DM messages from thread %d: %v", threadID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取私信消息失败"})
		return
	}

	c.JSON(http.StatusOK, messages)
}

// ==================== Public APIs ====================

// getOptionalUserID 尝试从请求中获取当前登录用户ID（可选认证）
// 用于公开路由中需要区分登录/未登录用户的场景
// 返回 0 表示未登录或认证失败
func (rv *RobotV1) getOptionalUserID(c *gin.Context) uint {
	// 尝试从 session cookie 获取用户
	sessionToken, err := c.Cookie(rv.h.Svc.Cfg.SessionCookieName)
	if err != nil || sessionToken == "" {
		return 0
	}

	session, err := rv.h.Svc.ValidateSession(sessionToken)
	if err != nil || session == nil {
		return 0
	}

	return session.UserID
}

// @Summary Get robot public info
// @Tags robot-public
// @Produce json
// @Param id path int true "Robot ID"
// @Success 200 {object} map[string]any
// @Router /api/robot/v1/public/robots/{id}/public [get]
func (rv *RobotV1) getRobotPublicInfo(c *gin.Context) {
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

	// 私密机器人只对 owner 可见
	if rb.IsPrivate {
		currentUserID := rv.getOptionalUserID(c)
		// 如果没有登录或者不是 owner，则返回 404
		if currentUserID == 0 || currentUserID != rb.OwnerID {
			c.JSON(http.StatusNotFound, gin.H{"error": "机器人不存在"})
			return
		}
		// 是 owner，允许继续访问
	}

	// 只返回公开信息，社交属性在 botUser 中
	c.JSON(http.StatusOK, gin.H{
		"id":        rb.ID,
		"category":  rb.Category,
		"createdAt": rb.CreatedAt,
		"botUser":   rb.BotUser,
	})
}

// setRobotPrivacy 设置机器人隐私标记（仅机器人所有者可修改）
// @Summary      Set robot privacy
// @Tags         robot-developer
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id    path  int   true  "Robot ID"
// @Param        body  body  object true  "{isPrivate: boolean}"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Failure      403   {object}  map[string]string
// @Failure      404   {object}  map[string]string
// @Router       /api/developer/v1/robots/{id}/privacy [put]
func (rv *RobotV1) setRobotPrivacy(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的机器人ID"})
		return
	}
	var body struct {
		IsPrivate bool `json:"isPrivate"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	rb, err := rv.h.Svc.GetRobot(uint(id))
	if err != nil || rb == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "机器人不存在"})
		return
	}
	if rb.OwnerID != u.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "仅所有者可操作"})
		return
	}
	rb.IsPrivate = body.IsPrivate
	if err := rv.h.Svc.Repo.UpdateRobot(rb); err != nil {
		logger.Errorf("[RobotAPI] Failed to set robot %d privacy: %v", id, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "设置隐私失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": rb.ID, "isPrivate": rb.IsPrivate})
}

// quotaLimit describes limits for associated-message, no-message, and unlimited uploads
type quotaLimit struct {
	DailyWithMsgBytes     int64 `json:"dailyWithMsgBytes"`
	MonthlyWithMsgBytes   int64 `json:"monthlyWithMsgBytes"`
	MonthlyWithMsgMax     int64 `json:"monthlyWithMsgMax"`
	DailyNoMsgBytes       int64 `json:"dailyNoMsgBytes"`
	MonthlyNoMsgBytes     int64 `json:"monthlyNoMsgBytes"`
	MonthlyUnlimitedBytes int64 `json:"monthlyUnlimitedBytes"` // 无限制上传：无每日限额，仅月度总量
}

func getQuotaLimits() quotaLimit {
	monthlyWithBytes := QuotaMonthlyWithMsgGiB * QuotaGiB
	return quotaLimit{
		DailyWithMsgBytes:     QuotaDailyWithMsgGiB * QuotaGiB,
		MonthlyWithMsgBytes:   monthlyWithBytes,
		MonthlyWithMsgMax:     int64(float64(monthlyWithBytes) * (1 + QuotaMonthlyOveragePct)),
		DailyNoMsgBytes:       QuotaDailyNoMsgGiB * QuotaGiB,
		MonthlyNoMsgBytes:     QuotaMonthlyNoMsgGiB * QuotaGiB,
		MonthlyUnlimitedBytes: QuotaMonthlyUnlimitedGiB * QuotaGiB,
	}
}

// getRobotQuota 机器人主动查询当前配额使用情况（当日与当月）
// 返回两组用量：withMessage 与 noMessage 的上限；实际使用以总上传字节数计算（统一计量键）。
func (rv *RobotV1) getRobotQuota(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil || !u.IsBot {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	if rv.h.Svc.Repo.Redis == nil {
		c.JSON(http.StatusOK, gin.H{"error": "配额存储服务不可用"})
		return
	}
	now := time.Now()
	dayKey := now.Format("20060102")
	monthKey := now.Format("200601")
	dailyKey := "bot:upload:daily:" + strconv.Itoa(int(u.ID)) + ":" + dayKey
	monthlyKey := "bot:upload:monthly:" + strconv.Itoa(int(u.ID)) + ":" + monthKey
	unlimitedMonthlyKey := "bot:upload:unlimited:monthly:" + strconv.Itoa(int(u.ID)) + ":" + monthKey
	ctx := c.Request.Context()
	dailyUsed, _ := rv.h.Svc.Repo.Redis.Get(ctx, dailyKey).Int64()
	monthlyUsed, _ := rv.h.Svc.Repo.Redis.Get(ctx, monthlyKey).Int64()
	unlimitedMonthlyUsed, _ := rv.h.Svc.Repo.Redis.Get(ctx, unlimitedMonthlyKey).Int64()

	limits := getQuotaLimits()
	c.JSON(http.StatusOK, gin.H{
		"userId": u.ID,
		"date":   dayKey,
		"month":  monthKey,
		"usage": gin.H{
			"dailyUsedBytes":            dailyUsed,
			"monthlyUsedBytes":          monthlyUsed,
			"unlimitedMonthlyUsedBytes": unlimitedMonthlyUsed,
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
			"unlimited": gin.H{
				"monthlyBytes": limits.MonthlyUnlimitedBytes,
			},
		},
	})
}

// botUploadFile 机器人文件上传，带配额与消息校验
// 表单: file (必需), channelId/threadId/groupThreadId (默认模式三选一必填，unlimited模式可选), messageId (可选), mode (可选: "unlimited")
// 规则:
// - 文件大小: 最大 15MB
// - mode=unlimited: 无限制上传模式，无每日限额，仅检查月度总量 10G；无需关联消息；无需提供目标ID
// - 默认模式:
//   - channelId/threadId/groupThreadId 三选一必填
//   - 每日配额: 默认 80G；若未携带 messageId，则每日限额 2G
//   - 每月配额: 2000G，最多允许超额 10%（即 2200G）
//
// - 消息校验: 若提供 messageId，则必须为 1 小时内创建且未撤回的消息，并且归属对应的 channel/thread/groupThread
func (rv *RobotV1) botUploadFile(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	// 仅允许机器人用户上传
	if u == nil || !u.IsBot {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	// 确保文件存储服务可用
	if rv.h.Svc.MinIO == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "文件存储服务不可用"})
		return
	}

	// 读取上传文件
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少文件"})
		return
	}
	// 文件大小限制
	if file.Size > BotUploadMaxBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件过大，最大允许15MB"})
		return
	}

	// 解析目标：频道、私聊线程或群聊线程
	channelIdStr := c.PostForm("channelId")
	threadIdStr := c.PostForm("threadId")
	groupThreadIdStr := c.PostForm("groupThreadId")
	messageIdStr := c.PostForm("messageId")

	var channelID uint
	var threadID uint
	var groupThreadID uint
	var messageID uint

	// 判断上传模式（提前解析，后续目标校验依赖该值）
	uploadMode := c.PostForm("mode") // 可选值: "unlimited"

	// 统计提供了几个目标 ID
	targetCount := 0
	if channelIdStr != "" {
		targetCount++
	}
	if threadIdStr != "" {
		targetCount++
	}
	if groupThreadIdStr != "" {
		targetCount++
	}

	if uploadMode == "unlimited" {
		// unlimited 模式：目标 ID 可选，但最多只能提供一个
		if targetCount > 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "只能提供频道ID、私信线程ID、群聊线程ID中的一个"})
			return
		}
	} else {
		// 默认模式：必须提供且仅提供一个目标 ID
		if targetCount == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "必须提供频道ID、私信线程ID或群聊线程ID（三选一）"})
			return
		}
		if targetCount > 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "只能提供频道ID、私信线程ID、群聊线程ID中的一个，不能同时提供"})
			return
		}
	}
	if channelIdStr != "" {
		if v, err := strconv.ParseUint(channelIdStr, 10, 32); err == nil {
			channelID = uint(v)
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的频道ID"})
			return
		}
	}
	if threadIdStr != "" {
		if v, err := strconv.ParseUint(threadIdStr, 10, 32); err == nil {
			threadID = uint(v)
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的私信线程ID"})
			return
		}
	}
	if groupThreadIdStr != "" {
		if v, err := strconv.ParseUint(groupThreadIdStr, 10, 32); err == nil {
			groupThreadID = uint(v)
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的群聊线程ID"})
			return
		}
	}
	if messageIdStr != "" {
		if v, err := strconv.ParseUint(messageIdStr, 10, 64); err == nil {
			messageID = uint(v)
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的消息ID"})
			return
		}
	}

	// 若提供 messageId，校验消息: 1小时内且未撤回，且归属对应的目标
	if messageID != 0 {
		// 校验消息归属与状态
		if channelID != 0 {
			msg, err := rv.h.Svc.Repo.GetMessageByID(messageID)
			if err != nil || msg == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "消息不存在"})
				return
			}
			if msg.ChannelID != channelID {
				c.JSON(http.StatusBadRequest, gin.H{"error": "消息不属于该频道"})
				return
			}
			if msg.DeletedAt != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "消息已撤回"})
				return
			}
			if time.Since(msg.CreatedAt) > time.Hour {
				c.JSON(http.StatusBadRequest, gin.H{"error": "消息已超过1小时"})
				return
			}
		} else if threadID != 0 {
			dm, err := rv.h.Svc.Repo.GetDmMessage(messageID)
			if err != nil || dm == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "私信消息不存在"})
				return
			}
			if dm.ThreadID != threadID {
				c.JSON(http.StatusBadRequest, gin.H{"error": "消息不属于该私信线程"})
				return
			}
			if dm.DeletedAt != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "消息已撤回"})
				return
			}
			if time.Since(dm.CreatedAt) > time.Hour {
				c.JSON(http.StatusBadRequest, gin.H{"error": "消息已超过1小时"})
				return
			}
		} else if groupThreadID != 0 {
			gm, err := rv.h.Svc.Repo.GetGroupMessage(messageID)
			if err != nil || gm == nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "群聊消息不存在"})
				return
			}
			if gm.ThreadID != groupThreadID {
				c.JSON(http.StatusBadRequest, gin.H{"error": "消息不属于该群聊线程"})
				return
			}
			if gm.DeletedAt != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "消息已撤回"})
				return
			}
			if time.Since(gm.CreatedAt) > time.Hour {
				c.JSON(http.StatusBadRequest, gin.H{"error": "消息已超过1小时"})
				return
			}
		}
	}

	// 计算并检查配额（Redis 记录以 BotUserID 为维度）
	// 键: bot:upload:daily:{userID}:{YYYYMMDD} ; bot:upload:monthly:{userID}:{YYYYMM}
	// 使用统一的限额配置 getQuotaLimits()
	limits := getQuotaLimits()

	now := time.Now()
	dayKey := now.Format("20060102")
	monthKey := now.Format("200601")

	// 取当前已用
	getUsage := func(key string) int64 {
		if rv.h.Svc.Repo.Redis == nil {
			return 0
		}
		v, err := rv.h.Svc.Repo.Redis.Get(c.Request.Context(), key).Int64()
		if err != nil {
			return 0
		}
		return v
	}
	dailyKey := "bot:upload:daily:" + strconv.Itoa(int(u.ID)) + ":" + dayKey
	monthlyKey := "bot:upload:monthly:" + strconv.Itoa(int(u.ID)) + ":" + monthKey

	dailyUsed := getUsage(dailyKey)
	monthlyUsed := getUsage(monthlyKey)

	if uploadMode == "unlimited" {
		// 无限制上传模式：无每日限额，仅检查月度总量 10G
		unlimitedMonthlyKey := "bot:upload:unlimited:monthly:" + strconv.Itoa(int(u.ID)) + ":" + monthKey
		unlimitedMonthlyUsed := getUsage(unlimitedMonthlyKey)
		if unlimitedMonthlyUsed+file.Size > limits.MonthlyUnlimitedBytes {
			c.JSON(http.StatusBadRequest, gin.H{"error": "已超过无限制上传的每月配额（10G）"})
			return
		}
	} else {
		// 原有模式：区分 withMessage / noMessage
		dailyCap := limits.DailyWithMsgBytes
		if messageID == 0 {
			dailyCap = limits.DailyNoMsgBytes
		}

		if dailyUsed+file.Size > dailyCap {
			c.JSON(http.StatusBadRequest, gin.H{"error": "已超过每日配额"})
			return
		}
		if messageID != 0 {
			// 基础配额（允许10%超额）
			if monthlyUsed+file.Size > limits.MonthlyWithMsgMax {
				c.JSON(http.StatusBadRequest, gin.H{"error": "已超过每月配额"})
				return
			}
		} else {
			// 限制配额（无超额）
			if monthlyUsed+file.Size > limits.MonthlyNoMsgBytes {
				c.JSON(http.StatusBadRequest, gin.H{"error": "已超过每月配额（未关联消息）"})
				return
			}
		}
	}

	// 打开并上传到 MinIO
	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "打开文件失败"})
		return
	}
	defer src.Close()

	contentType := file.Header.Get("Content-Type")
	category := CategoryGuildChatFiles
	if threadID != 0 {
		category = CategoryPrivateChatFiles
	} else if groupThreadID != 0 {
		category = CategoryGroupChatFiles
	}

	// 可见性与权限校验（unlimited 模式且未提供目标时跳过）
	if channelID != 0 {
		ch, err := rv.h.Svc.Repo.GetChannelByID(channelID)
		if err != nil || ch == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "频道不存在"})
			return
		}
		// 机器人是否有查看频道权限
		ok, err := rv.h.Svc.HasGuildPerm(ch.GuildID, u.ID, service.PermViewChannel)
		if err != nil || !ok {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足：机器人无法查看该频道"})
			return
		}
	} else if threadID != 0 {
		// 私聊是否包含机器人用户
		th, err := rv.h.Svc.Repo.GetDmThread(threadID)
		if err != nil || th == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "私信线程不存在"})
			return
		}
		if th.UserAID != u.ID && th.UserBID != u.ID {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足：机器人不在该私信线程中"})
			return
		}
	} else if groupThreadID != 0 {
		// 群聊是否包含机器人用户
		if !rv.h.Svc.Repo.IsGroupMember(groupThreadID, u.ID) {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足：机器人不在该群聊中"})
			return
		}
	}
	// unlimited 模式且未提供目标时，跳过权限校验，统一使用 guild-chat-files

	objectName, errUpload := rv.h.Svc.MinIO.UploadFile(c.Request.Context(), category, u.ID, src, file.Size, contentType)
	if errUpload != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "上传文件失败: " + errUpload.Error()})
		return
	}

	// 增加用量（设置 TTL：日用量到当天结束；月用量到当月结束）
	if rv.h.Svc.Repo.Redis != nil {
		// 月 TTL 到当月最后一天 23:59:59
		firstOfNextMonth := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, now.Location())
		endOfMonth := firstOfNextMonth.Add(-time.Second)
		monthTTL := time.Until(endOfMonth)

		if uploadMode == "unlimited" {
			// 无限制模式：仅累计独立的月度 key
			unlimitedMonthlyKey := "bot:upload:unlimited:monthly:" + strconv.Itoa(int(u.ID)) + ":" + monthKey
			rv.h.Svc.Repo.Redis.IncrBy(c.Request.Context(), unlimitedMonthlyKey, file.Size)
			rv.h.Svc.Repo.Redis.Expire(c.Request.Context(), unlimitedMonthlyKey, monthTTL)
		} else {
			// 原有模式：累计日 + 月
			endOfDay := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, now.Location())
			dayTTL := time.Until(endOfDay)
			rv.h.Svc.Repo.Redis.IncrBy(c.Request.Context(), dailyKey, file.Size)
			rv.h.Svc.Repo.Redis.Expire(c.Request.Context(), dailyKey, dayTTL)
			rv.h.Svc.Repo.Redis.IncrBy(c.Request.Context(), monthlyKey, file.Size)
			rv.h.Svc.Repo.Redis.Expire(c.Request.Context(), monthlyKey, monthTTL)
		}
	}

	fileURL := rv.h.Svc.MinIO.GetFileURL(objectName)
	fileInfo := gin.H{
		"path":          objectName,
		"url":           fileURL,
		"category":      category,
		"size":          file.Size,
		"contentType":   contentType,
		"filename":      file.Filename,
		"channelId":     channelID,
		"threadId":      threadID,
		"groupThreadId": groupThreadID,
		"messageId":     messageID,
	}

	// 检测图片/视频宽高并返回
	if isImageContentType(contentType) {
		if dims, err := rv.h.Svc.MinIO.DetectMediaDimensions(c.Request.Context(), objectName, contentType); err == nil && dims != nil {
			fileInfo["width"] = dims.Width
			fileInfo["height"] = dims.Height
		}
	} else if isVideoContentType(contentType) {
		if dims, err := rv.h.Svc.MinIO.DetectMediaDimensions(c.Request.Context(), objectName, contentType); err == nil && dims != nil {
			fileInfo["width"] = dims.Width
			fileInfo["height"] = dims.Height
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"file": fileInfo,
		},
	})
}

// @Summary      List hot robots (by creation time)
// @Tags         robot-public
// @Produce      json
// @Param        limit query int false "Limit (1-50, 默认20)"
// @Success      200  {array} map[string]any
// @Failure      500  {object} map[string]string
// @Router       /api/bot/v1/pub/hot [get]
// hotRobots 返回热门机器人列表（按所在服务器数量降序）
func (rv *RobotV1) hotRobots(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	robots, err := rv.h.Svc.HotRobots(limit)
	if err != nil {
		logger.Errorf("[RobotAPI] Failed to get hot robots: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取热门机器人失败"})
		return
	}
	// 格式化返回数据，包含机器人信息和用户信息（隐私字段除外）
	result := make([]gin.H, 0, len(robots))
	for _, robot := range robots {
		item := gin.H{
			"id":         robot.ID,
			"ownerId":    robot.OwnerID,
			"botUserId":  robot.BotUserID,
			"isPrivate":  robot.IsPrivate,
			"category":   robot.Category,
			"guildCount": robot.GuildCount,
			"createdAt":  robot.CreatedAt,
			"updatedAt":  robot.UpdatedAt,
		}
		if robot.BotUser != nil {
			item["botUser"] = gin.H{
				"id":           robot.BotUser.ID,
				"name":         robot.BotUser.Name,
				"avatar":       robot.BotUser.Avatar,
				"displayName":  robot.BotUser.DisplayName,
				"bio":          robot.BotUser.Bio,
				"banner":       robot.BotUser.Banner,
				"bannerColor":  robot.BotUser.BannerColor,
				"status":       robot.BotUser.Status,
				"customStatus": robot.BotUser.CustomStatus,
				"isBot":        robot.BotUser.IsBot,
				"createdAt":    robot.BotUser.CreatedAt,
			}
		}
		result = append(result, item)
	}
	c.JSON(http.StatusOK, result)
}

// @Summary      Search robots by name
// @Tags         robot-public
// @Produce      json
// @Param        q      query string true  "搜索关键词 (>=1 字符)"
// @Param        limit  query int    false "Limit (默认20, 最大100)"
// @Success      200    {array} map[string]any
// @Failure      400    {object} map[string]string
// @Failure      500    {object} map[string]string
// @Router       /api/bot/v1/pub/search [get]
// searchRobots 机器人名称模糊搜索
func (rv *RobotV1) searchRobots(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少搜索关键词"})
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	robots, err := rv.h.Svc.SearchRobots(q, limit)
	if err != nil {
		if se, ok := err.(*service.Err); ok && se.Code == 400 {
			c.JSON(http.StatusBadRequest, gin.H{"error": se.Msg})
			return
		}
		logger.Errorf("[RobotAPI] Failed to search robots with query '%s': %v", q, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "搜索机器人失败"})
		return
	}
	// 格式化返回数据，包含机器人信息和用户信息（隐私字段除外）
	result := make([]gin.H, 0, len(robots))
	for _, robot := range robots {
		item := gin.H{
			"id":        robot.ID,
			"ownerId":   robot.OwnerID,
			"botUserId": robot.BotUserID,
			"isPrivate": robot.IsPrivate,
			"category":  robot.Category,
			"createdAt": robot.CreatedAt,
			"updatedAt": robot.UpdatedAt,
		}
		if robot.BotUser != nil {
			item["botUser"] = gin.H{
				"id":           robot.BotUser.ID,
				"name":         robot.BotUser.Name,
				"avatar":       robot.BotUser.Avatar,
				"displayName":  robot.BotUser.DisplayName,
				"bio":          robot.BotUser.Bio,
				"banner":       robot.BotUser.Banner,
				"bannerColor":  robot.BotUser.BannerColor,
				"status":       robot.BotUser.Status,
				"customStatus": robot.BotUser.CustomStatus,
				"isBot":        robot.BotUser.IsBot,
				"createdAt":    robot.BotUser.CreatedAt,
			}
		}
		result = append(result, item)
	}
	c.JSON(http.StatusOK, result)
}

// ==================== 机器人分类 & 排行榜 APIs ====================

// @Summary      获取排行榜查询类型枚举
// @Tags         robot-public
// @Produce      json
// @Success      200  {object} map[string]any
// @Router       /api/bot/v1/pub/ranking/enums [get]
func (rv *RobotV1) getRankingEnums(c *gin.Context) {
	// 周期类型
	periodTypes := []gin.H{
		{"value": "daily", "label": "日榜", "description": "每日排行，统计当天 00:00 到次日 00:00"},
		{"value": "weekly", "label": "周榜", "description": "每周排行，统计周一 00:00 到下周一 00:00（ISO Week）"},
		{"value": "monthly", "label": "月榜", "description": "每月排行，统计1号 00:00 到下月1号 00:00"},
	}

	// 分类列表（从数据库动态获取）
	categories := []gin.H{
		{"value": "", "label": "全部", "description": "不限分类，展示所有公开机器人"},
	}
	if dbCategories, err := rv.h.Svc.ListRobotCategories(); err == nil {
		for _, cat := range dbCategories {
			categories = append(categories, gin.H{
				"value": cat.Name,
				"label": cat.Name,
			})
		}
	}

	// 排序维度说明
	scoreDimensions := []gin.H{
		{"field": "guildCount", "label": "存量服务器数", "description": "机器人当前所在的服务器总数"},
		{"field": "guildGrowth", "label": "新增服务器数", "description": "该周期内新加入的服务器数量"},
		{"field": "messageCount", "label": "发送消息数", "description": "该周期内机器人发送的消息总数"},
		{"field": "interactionCount", "label": "被回复数", "description": "该周期内其他用户对机器人消息的回复数"},
	}

	c.JSON(200, gin.H{
		"periodTypes":     periodTypes,
		"categories":      categories,
		"scoreDimensions": scoreDimensions,
	})
}

// @Summary      获取机器人分类列表
// @Tags         robot-public
// @Produce      json
// @Success      200  {array} map[string]any
// @Router       /api/bot/v1/pub/categories [get]
func (rv *RobotV1) listRobotCategories(c *gin.Context) {
	categories, err := rv.h.Svc.ListRobotCategories()
	if err != nil {
		logger.Errorf("[RobotAPI] Failed to list robot categories: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取分类列表失败"})
		return
	}
	c.JSON(http.StatusOK, categories)
}

// @Summary      获取机器人排行榜
// @Tags         robot-public
// @Produce      json
// @Param        period   query string false "排行榜周期: daily(日榜), weekly(周榜), monthly(月榜), 默认daily"
// @Param        key      query string false "周期标识, 留空使用当前周期. daily: 2026-02-14, weekly: 2026-W07, monthly: 2026-02"
// @Param        category query string false "分类过滤, 留空或'全部'表示不过滤"
// @Param        limit    query int    false "每页数量 (1-100, 默认20)"
// @Param        offset   query int    false "偏移量 (默认0)"
// @Success      200  {object} map[string]any
// @Failure      500  {object} map[string]string
// @Router       /api/bot/v1/pub/ranking [get]
func (rv *RobotV1) getRobotRankings(c *gin.Context) {
	periodType := c.DefaultQuery("period", "daily")
	periodKey := c.Query("key")
	category := c.Query("category")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	// 验证 period 类型
	switch periodType {
	case "daily", "weekly", "monthly":
		// ok
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的周期类型，可选: daily, weekly, monthly"})
		return
	}

	rankings, total, err := rv.h.Svc.GetRobotRankings(periodType, periodKey, category, limit, offset)
	if err != nil {
		logger.Errorf("[RobotAPI] Failed to get robot rankings (period=%s, key=%s): %v", periodType, periodKey, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取排行榜失败"})
		return
	}

	// 格式化返回数据
	result := make([]gin.H, 0, len(rankings))
	for rank, r := range rankings {
		item := gin.H{
			"rank":             offset + rank + 1,
			"robotId":          r.RobotID,
			"heatScore":        r.HeatScore,
			"rawScore":         r.RawScore,
			"guildCount":       r.GuildCount,
			"guildGrowth":      r.GuildGrowth,
			"messageCount":     r.MessageCount,
			"interactionCount": r.InteractionCount,
			"decayApplied":     r.DecayApplied,
			"periodType":       r.PeriodType,
			"periodKey":        r.PeriodKey,
		}
		if r.Robot != nil {
			item["category"] = r.Robot.Category
			item["isPrivate"] = r.Robot.IsPrivate
			if r.Robot.BotUser != nil {
				item["botUser"] = gin.H{
					"id":           r.Robot.BotUser.ID,
					"name":         r.Robot.BotUser.Name,
					"avatar":       r.Robot.BotUser.Avatar,
					"displayName":  r.Robot.BotUser.DisplayName,
					"bio":          r.Robot.BotUser.Bio,
					"banner":       r.Robot.BotUser.Banner,
					"bannerColor":  r.Robot.BotUser.BannerColor,
					"status":       r.Robot.BotUser.Status,
					"customStatus": r.Robot.BotUser.CustomStatus,
					"isBot":        r.Robot.BotUser.IsBot,
					"createdAt":    r.Robot.BotUser.CreatedAt,
				}
			}
		}
		result = append(result, item)
	}

	c.JSON(http.StatusOK, gin.H{
		"rankings":   result,
		"total":      total,
		"periodType": periodType,
		"periodKey":  periodKey,
		"category":   category,
		"limit":      limit,
		"offset":     offset,
	})
}

// @Summary      设置机器人分类
// @Tags         robot-developer
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id    path  int    true  "Robot ID"
// @Param        body  body  object true  "{category: string}"
// @Success      200   {object}  map[string]any
// @Failure      400   {object}  map[string]string
// @Failure      401   {object}  map[string]string
// @Failure      403   {object}  map[string]string
// @Failure      404   {object}  map[string]string
// @Router       /api/developer/v1/robots/{id}/category [put]
func (rv *RobotV1) setRobotCategory(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的机器人ID"})
		return
	}
	var body struct {
		Category string `json:"category" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误，需要提供 category 字段"})
		return
	}
	rb, err := rv.h.Svc.GetRobot(uint(id))
	if err != nil || rb == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "机器人不存在"})
		return
	}
	if rb.OwnerID != u.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "仅所有者可操作"})
		return
	}
	if err := rv.h.Svc.SetRobotCategory(uint(id), body.Category); err != nil {
		if se, ok := err.(*service.Err); ok {
			c.JSON(se.Code, gin.H{"error": se.Msg})
			return
		}
		logger.Errorf("[RobotAPI] Failed to set robot %d category: %v", id, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "设置分类失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": rb.ID, "category": body.Category})
}

// ==================== 交互消息 ====================

// @Summary Get pending interactions
// @Description 获取发给本机器人的交互消息（type=interaction），支持分页和按频道过滤
// @Tags robot-bot
// @Security BotAuth
// @Produce json
// @Param channelId query int false "频道ID（可选，过滤特定频道）"
// @Param limit query int false "每页数量（默认50，最大100）"
// @Param after query int false "返回ID大于此值的消息（用于拉取新消息）"
// @Param before query int false "返回ID小于此值的消息（用于向前翻页）"
// @Success 200 {array} map[string]any
// @Router /api/robot/v1/bot/interactions [get]
func (rv *RobotV1) botGetInteractions(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}

	limit := 50
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	// 查询所有机器人所在公会的频道中，type=interaction 的消息
	// 通过公会成员关系确定机器人可见范围
	query := rv.h.Svc.Repo.DB.Model(&models.Message{}).
		Where("type = 'interaction' AND deleted_at IS NULL")

	// 按频道过滤（可选）
	if chID := c.Query("channelId"); chID != "" {
		if id, err := strconv.ParseUint(chID, 10, 64); err == nil {
			query = query.Where("channel_id = ?", id)
		}
	}

	if after := c.Query("after"); after != "" {
		if id, err := strconv.ParseUint(after, 10, 64); err == nil {
			query = query.Where("id > ?", id)
		}
	}
	if before := c.Query("before"); before != "" {
		if id, err := strconv.ParseUint(before, 10, 64); err == nil {
			query = query.Where("id < ?", id)
		}
	}

	var messages []models.Message
	if err := query.Order("id desc").Limit(limit).Find(&messages).Error; err != nil {
		logger.Errorf("[RobotAPI] Failed to get interactions: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取交互消息失败"})
		return
	}

	// 构建带用户信息的响应
	result := make([]gin.H, 0, len(messages))
	for _, msg := range messages {
		item := gin.H{
			"id":        msg.ID,
			"channelId": msg.ChannelID,
			"authorId":  msg.AuthorID,
			"content":   msg.Content,
			"createdAt": msg.CreatedAt,
			"timestamp": msg.CreatedAt.UnixMilli(),
		}
		// 填充 data（存储在 fileMeta/embed 字段中）
		if len(msg.FileMeta) > 0 {
			var data any
			if err := json.Unmarshal(msg.FileMeta, &data); err == nil {
				item["data"] = data
			}
		}
		// 填充用户信息
		if usr, err := rv.h.Svc.GetUserByID(msg.AuthorID); err == nil && usr != nil {
			item["user"] = gin.H{
				"id":     usr.ID,
				"name":   usr.Name,
				"avatar": usr.Avatar,
				"isBot":  usr.IsBot,
			}
		}
		// 填充频道和公会信息
		if ch, err := rv.h.Svc.Repo.GetChannel(msg.ChannelID); err == nil && ch != nil {
			item["guildId"] = ch.GuildID
		}
		result = append(result, item)
	}

	c.JSON(http.StatusOK, result)
}

// ==================== P0: Reactions ====================

// botListReactions 列出消息的所有表态
func (rv *RobotV1) botListReactions(c *gin.Context) {
	msgID, err := strconv.ParseUint(c.Param("messageId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的消息ID"})
		return
	}
	list, err := rv.h.Svc.ListReactions(uint(msgID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取表态失败"})
		return
	}
	c.JSON(http.StatusOK, list)
}

// botAddReaction 为消息添加表态
func (rv *RobotV1) botAddReaction(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	msgID, err := strconv.ParseUint(c.Param("messageId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的消息ID"})
		return
	}
	emoji, err := url.PathUnescape(c.Param("emoji"))
	if err != nil || emoji == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "emoji 参数错误"})
		return
	}

	reaction, err := rv.h.Svc.AddReaction(uint(msgID), u.ID, emoji)
	if err != nil {
		if err == service.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "消息不存在"})
		} else if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else if se, ok := err.(*service.Err); ok {
			c.JSON(se.Code, gin.H{"error": se.Msg})
		} else {
			logger.Errorf("[RobotAPI] Failed to add reaction on message %d: %v", msgID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "添加表态失败"})
		}
		return
	}

	// 广播表态新增事件
	if rv.h.Gw != nil {
		msg, _ := rv.h.Svc.Repo.GetMessage(uint(msgID))
		if msg != nil {
			rv.h.Gw.BroadcastToChannel(msg.ChannelID, config.EventMessageReactionAdd, gin.H{
				"messageId": uint(msgID),
				"channelId": msg.ChannelID,
				"userId":    u.ID,
				"emoji":     emoji,
				"user": gin.H{
					"id":     u.ID,
					"name":   u.Name,
					"avatar": u.Avatar,
				},
			})
		}
	}
	c.JSON(http.StatusOK, reaction)
}

// botRemoveReaction 移除消息表态
func (rv *RobotV1) botRemoveReaction(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	msgID, err := strconv.ParseUint(c.Param("messageId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的消息ID"})
		return
	}
	emoji, err := url.PathUnescape(c.Param("emoji"))
	if err != nil || emoji == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "emoji 参数错误"})
		return
	}

	if err := rv.h.Svc.RemoveReaction(uint(msgID), u.ID, emoji); err != nil {
		if err == service.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "消息不存在"})
		} else if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else {
			logger.Errorf("[RobotAPI] Failed to remove reaction on message %d: %v", msgID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "移除表态失败"})
		}
		return
	}

	if rv.h.Gw != nil {
		msg, _ := rv.h.Svc.Repo.GetMessage(uint(msgID))
		if msg != nil {
			rv.h.Gw.BroadcastToChannel(msg.ChannelID, config.EventMessageReactionRemove, gin.H{
				"messageId": uint(msgID),
				"channelId": msg.ChannelID,
				"userId":    u.ID,
				"emoji":     emoji,
			})
		}
	}
	c.JSON(http.StatusNoContent, nil)
}

// ==================== P0: Pins ====================

// botListPins 列出频道精华消息
func (rv *RobotV1) botListPins(c *gin.Context) {
	channelID, err := strconv.ParseUint(c.Param("channelId"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的频道ID"})
		return
	}
	list, err := rv.h.Svc.ListPinnedMessages(uint(channelID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取精华消息失败"})
		return
	}
	c.JSON(http.StatusOK, list)
}

// botPinMessage 设置消息为精华
func (rv *RobotV1) botPinMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	channelID, err := strconv.ParseUint(c.Param("channelId"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的频道ID"})
		return
	}
	var body struct {
		MessageID uint `json:"messageId"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	pm, err := rv.h.Svc.PinMessage(uint(channelID), body.MessageID, u.ID)
	if err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else if err == service.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "消息不存在"})
		} else {
			logger.Errorf("[RobotAPI] Failed to pin message in channel %d: %v", channelID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "设置精华失败"})
		}
		return
	}
	if rv.h.Gw != nil {
		rv.h.Gw.BroadcastToChannel(uint(channelID), config.EventMessagePin, gin.H{"id": pm.MessageID, "channelId": uint(channelID)})
	}
	c.JSON(http.StatusOK, pm)
}

// botUnpinMessage 取消精华
func (rv *RobotV1) botUnpinMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	channelID, err := strconv.ParseUint(c.Param("channelId"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的频道ID"})
		return
	}
	messageID, err := strconv.ParseUint(c.Param("messageId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的消息ID"})
		return
	}
	if err := rv.h.Svc.UnpinMessage(uint(messageID), u.ID); err != nil {
		if err == service.ErrNotFound {
			c.Status(http.StatusNoContent)
			return
		} else if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "取消精华失败"})
		return
	}
	if rv.h.Gw != nil {
		rv.h.Gw.BroadcastToChannel(uint(channelID), config.EventMessageUnpin, gin.H{"id": uint(messageID), "channelId": uint(channelID)})
	}
	c.Status(http.StatusNoContent)
}

// ==================== P0: Batch Delete ====================

// botBatchDeleteMessages 批量删除频道消息
func (rv *RobotV1) botBatchDeleteMessages(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	channelID, err := strconv.ParseUint(c.Param("channelId"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的频道ID"})
		return
	}
	var body struct {
		MessageIDs []uint `json:"messageIds" binding:"required"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	if len(body.MessageIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "messageIds 不能为空"})
		return
	}
	if len(body.MessageIDs) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "单次最多删除100条消息"})
		return
	}

	succeeded := make([]uint, 0, len(body.MessageIDs))
	failed := make([]gin.H, 0)

	for _, mid := range body.MessageIDs {
		msg, err := rv.h.Svc.Repo.GetMessage(mid)
		if err != nil {
			succeeded = append(succeeded, mid)
			continue
		}
		// 确保消息属于该频道
		if msg.ChannelID != uint(channelID) {
			failed = append(failed, gin.H{"messageId": mid, "error": "消息不属于该频道"})
			continue
		}
		if err := rv.h.Svc.DeleteMessage(mid, u.ID); err != nil {
			reason := "删除失败"
			if err == service.ErrUnauthorized {
				reason = "权限不足"
			}
			failed = append(failed, gin.H{"messageId": mid, "error": reason})
			continue
		}
		succeeded = append(succeeded, mid)
		if rv.h.Gw != nil {
			payload := rv.h.buildChannelMessagePayload(msg)
			payload["deleted"] = true
			rv.h.Gw.BroadcastToChannel(msg.ChannelID, config.EventMessageDelete, payload)
		}
	}

	c.JSON(http.StatusOK, gin.H{"succeeded": succeeded, "failed": failed})
}

// ==================== P1: Member Management ====================

// botKickMember 踢出成员
func (rv *RobotV1) botKickMember(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	guildID, err := strconv.ParseUint(c.Param("guildId"), 10, 64)
	if err != nil || guildID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}
	userID, err := strconv.ParseUint(c.Param("userId"), 10, 64)
	if err != nil || userID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的用户ID"})
		return
	}

	if err := rv.h.Svc.KickMember(uint(guildID), uint(userID), u.ID); err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
			return
		} else if err == service.ErrNotFound {
			c.JSON(http.StatusOK, gin.H{"success": true, "message": "该用户已不在服务器中"})
			return
		}
		logger.Errorf("[RobotAPI] Failed to kick member %d from guild %d: %v", userID, guildID, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "踢出成员失败"})
		return
	}
	if rv.h.Gw != nil {
		rv.h.Gw.BroadcastToGuild(uint(guildID), config.EventGuildMemberRemove, gin.H{"userId": uint(userID), "operatorId": u.ID})
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// botMuteMember 禁言成员
func (rv *RobotV1) botMuteMember(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	guildID, err := strconv.ParseUint(c.Param("guildId"), 10, 64)
	if err != nil || guildID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}
	userID, err := strconv.ParseUint(c.Param("userId"), 10, 64)
	if err != nil || userID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的用户ID"})
		return
	}
	var body struct {
		Duration int `json:"duration"` // 秒
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	if body.Duration <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "禁言时长必须为正数"})
		return
	}

	duration := time.Duration(body.Duration) * time.Second
	if err := rv.h.Svc.MuteMember(uint(guildID), uint(userID), u.ID, duration); err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else if err == service.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "服务器或成员不存在"})
		} else {
			logger.Errorf("[RobotAPI] Failed to mute member %d in guild %d: %v", userID, guildID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "禁言失败"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// botUnmuteMember 解除禁言
func (rv *RobotV1) botUnmuteMember(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	guildID, err := strconv.ParseUint(c.Param("guildId"), 10, 64)
	if err != nil || guildID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}
	userID, err := strconv.ParseUint(c.Param("userId"), 10, 64)
	if err != nil || userID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的用户ID"})
		return
	}

	if err := rv.h.Svc.UnmuteMember(uint(guildID), uint(userID), u.ID); err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
			return
		} else if err == service.ErrNotFound {
			c.JSON(http.StatusOK, gin.H{"success": true})
			return
		}
		logger.Errorf("[RobotAPI] Failed to unmute member %d in guild %d: %v", userID, guildID, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "解除禁言失败"})
		return
	}
	if rv.h.Gw != nil {
		rv.h.Gw.BroadcastToGuild(uint(guildID), config.EventGuildMemberUpdate, gin.H{"userId": uint(userID), "action": "unmuted", "operatorId": u.ID})
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// botSetMemberNickname 设置成员昵称
func (rv *RobotV1) botSetMemberNickname(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	guildID, err := strconv.ParseUint(c.Param("guildId"), 10, 64)
	if err != nil || guildID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}
	userID, err := strconv.ParseUint(c.Param("userId"), 10, 64)
	if err != nil || userID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的用户ID"})
		return
	}
	var body struct {
		Nickname string `json:"nickname"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	if len(body.Nickname) > 32 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "昵称过长（最多32个字符）"})
		return
	}

	if err := rv.h.Svc.SetMemberNickname(uint(guildID), uint(userID), u.ID, body.Nickname); err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else if err == service.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "服务器或成员不存在"})
		} else {
			logger.Errorf("[RobotAPI] Failed to set nickname for member %d in guild %d: %v", userID, guildID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "设置昵称失败"})
		}
		return
	}
	if rv.h.Gw != nil {
		rv.h.Gw.BroadcastToGuild(uint(guildID), config.EventGuildMemberUpdate, gin.H{"userId": uint(userID), "nickname": body.Nickname, "operatorId": u.ID})
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ==================== P1: Role Management ====================

// botListRoles 列出服务器角色
func (rv *RobotV1) botListRoles(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	guildID, err := strconv.ParseUint(c.Param("guildId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}
	roles, err := rv.h.Svc.ListGuildRoles(uint(guildID), u.ID)
	if err != nil {
		if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
		} else {
			logger.Errorf("[RobotAPI] Failed to list roles in guild %d: %v", guildID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "获取角色列表失败"})
		}
		return
	}
	c.JSON(http.StatusOK, roles)
}

// botCreateRole 创建角色
func (rv *RobotV1) botCreateRole(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	guildID, err := strconv.ParseUint(c.Param("guildId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}
	var body struct {
		Name        string `json:"name"`
		Permissions uint64 `json:"permissions"`
		Color       string `json:"color"`
	}
	if err := c.BindJSON(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	if l := len([]rune(strings.TrimSpace(body.Name))); l == 0 || l > int(config.MaxRoleNameLength) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "角色名称长度不合法"})
		return
	}
	role, err := rv.h.Svc.CreateRole(uint(guildID), u.ID, body.Name, body.Permissions, body.Color)
	if err != nil {
		if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
		} else {
			logger.Errorf("[RobotAPI] Failed to create role in guild %d: %v", guildID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "创建角色失败"})
		}
		return
	}
	c.JSON(http.StatusOK, role)
}

// botUpdateRole 更新角色
func (rv *RobotV1) botUpdateRole(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	guildID, err := strconv.ParseUint(c.Param("guildId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}
	roleID, err := strconv.ParseUint(c.Param("roleId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的角色ID"})
		return
	}
	var body struct {
		Name        *string `json:"name"`
		Permissions *uint64 `json:"permissions"`
		Color       *string `json:"color"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	if body.Name != nil {
		if l := len([]rune(strings.TrimSpace(*body.Name))); l == 0 || l > int(config.MaxRoleNameLength) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "角色名称长度不合法"})
			return
		}
	}
	role, err := rv.h.Svc.UpdateRole(uint(guildID), u.ID, uint(roleID), body.Name, body.Permissions, body.Color)
	if err != nil {
		if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
		} else {
			logger.Errorf("[RobotAPI] Failed to update role %d in guild %d: %v", roleID, guildID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "更新角色失败"})
		}
		return
	}
	c.JSON(http.StatusOK, role)
}

// botDeleteRole 删除角色
func (rv *RobotV1) botDeleteRole(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	guildID, err := strconv.ParseUint(c.Param("guildId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}
	roleID, err := strconv.ParseUint(c.Param("roleId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的角色ID"})
		return
	}
	if err := rv.h.Svc.DeleteRole(uint(guildID), u.ID, uint(roleID)); err != nil {
		if svcErr, ok := err.(*service.Err); ok {
			if svcErr.Code == 404 {
				c.Status(http.StatusNoContent)
				return
			}
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
		} else {
			logger.Errorf("[RobotAPI] Failed to delete role %d in guild %d: %v", roleID, guildID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "删除角色失败"})
		}
		return
	}
	c.Status(http.StatusNoContent)
}

// botAssignRole 为成员分配角色
func (rv *RobotV1) botAssignRole(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	guildID, err := strconv.ParseUint(c.Param("guildId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}
	roleID, err := strconv.ParseUint(c.Param("roleId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的角色ID"})
		return
	}
	userID, err := strconv.ParseUint(c.Param("userId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的用户ID"})
		return
	}
	if err := rv.h.Svc.AssignRoleToMember(uint(guildID), u.ID, uint(userID), uint(roleID)); err != nil {
		if svcErr, ok := err.(*service.Err); ok {
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
		} else {
			logger.Errorf("[RobotAPI] Failed to assign role %d to user %d in guild %d: %v", roleID, userID, guildID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "分配角色失败"})
		}
		return
	}
	if rv.h.Gw != nil {
		rv.h.Gw.BroadcastToGuild(uint(guildID), config.EventGuildMemberUpdate, gin.H{
			"userId":     uint(userID),
			"operatorId": u.ID,
			"action":     "roles_added",
			"roleId":     uint(roleID),
		})
	}
	c.Status(http.StatusNoContent)
}

// botRemoveRole 移除成员角色
func (rv *RobotV1) botRemoveRole(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	guildID, err := strconv.ParseUint(c.Param("guildId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}
	roleID, err := strconv.ParseUint(c.Param("roleId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的角色ID"})
		return
	}
	userID, err := strconv.ParseUint(c.Param("userId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的用户ID"})
		return
	}
	if err := rv.h.Svc.RemoveRoleFromMember(uint(guildID), u.ID, uint(userID), uint(roleID)); err != nil {
		if svcErr, ok := err.(*service.Err); ok {
			if svcErr.Code == 404 {
				c.Status(http.StatusNoContent)
				return
			}
			c.JSON(svcErr.Code, gin.H{"error": svcErr.Msg})
		} else {
			logger.Errorf("[RobotAPI] Failed to remove role %d from user %d in guild %d: %v", roleID, userID, guildID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "移除角色失败"})
		}
		return
	}
	if rv.h.Gw != nil {
		rv.h.Gw.BroadcastToGuild(uint(guildID), config.EventGuildMemberUpdate, gin.H{
			"userId":     uint(userID),
			"operatorId": u.ID,
			"action":     "roles_removed",
			"roleId":     uint(roleID),
		})
	}
	c.Status(http.StatusNoContent)
}

// ==================== P1: Channel Management ====================

// botCreateChannel 创建频道
func (rv *RobotV1) botCreateChannel(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	guildID, err := strconv.ParseUint(c.Param("guildId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}
	var body struct {
		Name       string `json:"name"`
		Type       string `json:"type"`
		ParentID   *uint  `json:"parentId,omitempty"`
		CategoryID *uint  `json:"categoryId,omitempty"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	n := strings.TrimSpace(body.Name)
	if len([]rune(n)) == 0 || len([]rune(n)) > int(config.MaxChannelNameLength) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "频道名称长度不合法"})
		return
	}
	has, err := rv.h.Svc.HasGuildPerm(uint(guildID), u.ID, service.PermManageChannels)
	if err != nil || !has {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}
	channelType := body.Type
	if channelType == "" {
		channelType = "text"
	}
	ch, err := rv.h.Svc.CreateChannel(uint(guildID), body.Name, channelType, body.ParentID)
	if err != nil {
		logger.Errorf("[RobotAPI] Failed to create channel in guild %d: %v", guildID, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "创建频道失败"})
		return
	}
	if body.CategoryID != nil {
		_ = rv.h.Svc.SetChannelCategory(ch.ID, *body.CategoryID)
	}
	c.JSON(http.StatusOK, ch)
}

// botUpdateChannel 更新频道
func (rv *RobotV1) botUpdateChannel(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	channelID, err := strconv.ParseUint(c.Param("channelId"), 10, 64)
	if err != nil || channelID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的频道ID"})
		return
	}
	var body struct {
		Name   *string `json:"name,omitempty"`
		Banner *string `json:"banner,omitempty"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	if err := rv.h.Svc.UpdateChannel(uint(channelID), u.ID, body.Name, body.Banner); err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else if err == service.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "频道不存在"})
		} else {
			logger.Errorf("[RobotAPI] Failed to update channel %d: %v", channelID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "更新频道失败"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// botDeleteChannel 删除频道
func (rv *RobotV1) botDeleteChannel(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	channelID, err := strconv.ParseUint(c.Param("channelId"), 10, 64)
	if err != nil || channelID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的频道ID"})
		return
	}
	if err := rv.h.Svc.DeleteChannel(uint(channelID), u.ID); err != nil {
		if err == service.ErrNotFound {
			c.Status(http.StatusNoContent)
			return
		} else if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "删除频道失败"})
		return
	}
	c.Status(http.StatusNoContent)
}

// ==================== P2: Announcements ====================

// botListAnnouncements 列出公告
func (rv *RobotV1) botListAnnouncements(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	guildID, err := strconv.ParseUint(c.Param("guildId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if limit < 1 || limit > 100 {
		limit = 10
	}
	beforeID64, _ := strconv.ParseUint(c.Query("beforeId"), 10, 64)
	afterID64, _ := strconv.ParseUint(c.Query("afterId"), 10, 64)
	beforeID := uint(beforeID64)
	afterID := uint(afterID64)

	if beforeID > 0 || afterID > 0 {
		list, err := rv.h.Svc.ListAnnouncementsCursor(uint(guildID), limit, beforeID, afterID)
		if err != nil {
			logger.Errorf("[RobotAPI] Failed to list announcements for guild %d: %v", guildID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "获取公告列表失败"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": list})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	list, total, err := rv.h.Svc.ListAnnouncements(uint(guildID), page, limit)
	if err != nil {
		logger.Errorf("[RobotAPI] Failed to list announcements for guild %d: %v", guildID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取公告列表失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data": list,
		"pagination": gin.H{
			"page":    page,
			"limit":   limit,
			"total":   total,
			"hasMore": page*limit < total,
		},
	})
}

// botCreateAnnouncement 创建公告
func (rv *RobotV1) botCreateAnnouncement(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	guildID, err := strconv.ParseUint(c.Param("guildId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}
	var body struct {
		Title   string           `json:"title"`
		Content string           `json:"content"`
		Images  []map[string]any `json:"images"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	if len([]rune(body.Content)) > int(config.MaxGuildAnnouncementLength) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "公告内容过长"})
		return
	}
	if int64(len(body.Images)) > config.MaxAnnouncementImages {
		c.JSON(http.StatusBadRequest, gin.H{"error": "图片数量超过限制(最多9张)"})
		return
	}
	var imagesJSON datatypes.JSON
	if len(body.Images) > 0 {
		bs, _ := json.Marshal(body.Images)
		imagesJSON = datatypes.JSON(bs)
	}
	a, err := rv.h.Svc.CreateAnnouncement(uint(guildID), u.ID, body.Title, body.Content, imagesJSON)
	if err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else {
			logger.Errorf("[RobotAPI] Failed to create announcement for guild %d: %v", guildID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "创建公告失败"})
		}
		return
	}
	c.JSON(http.StatusOK, a)
}

// botGetAnnouncement 获取公告详情
func (rv *RobotV1) botGetAnnouncement(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("announcementId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的公告ID"})
		return
	}
	a, err := rv.h.Svc.GetAnnouncement(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "公告不存在"})
		return
	}
	c.JSON(http.StatusOK, a)
}

// botUpdateAnnouncement 更新公告
func (rv *RobotV1) botUpdateAnnouncement(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := strconv.ParseUint(c.Param("announcementId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的公告ID"})
		return
	}
	var body struct {
		Title   *string           `json:"title"`
		Content *string           `json:"content"`
		Images  *[]map[string]any `json:"images"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	var imagesJSON *datatypes.JSON
	if body.Images != nil {
		if int64(len(*body.Images)) > config.MaxAnnouncementImages {
			c.JSON(http.StatusBadRequest, gin.H{"error": "图片数量超过限制(最多9张)"})
			return
		}
		bs, _ := json.Marshal(*body.Images)
		j := datatypes.JSON(bs)
		imagesJSON = &j
	}
	a, err := rv.h.Svc.UpdateAnnouncement(uint(id), u.ID, body.Title, body.Content, imagesJSON)
	if err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else if err == service.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "公告不存在"})
		} else {
			logger.Errorf("[RobotAPI] Failed to update announcement %d: %v", id, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "更新公告失败"})
		}
		return
	}
	c.JSON(http.StatusOK, a)
}

// botDeleteAnnouncement 删除公告
func (rv *RobotV1) botDeleteAnnouncement(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := strconv.ParseUint(c.Param("announcementId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的公告ID"})
		return
	}
	if err := rv.h.Svc.DeleteAnnouncement(uint(id), u.ID); err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else if err == service.ErrNotFound {
			c.Status(http.StatusNoContent)
			return
		} else {
			logger.Errorf("[RobotAPI] Failed to delete announcement %d: %v", id, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "删除公告失败"})
		}
		return
	}
	c.Status(http.StatusNoContent)
}

// botPinAnnouncement 置顶公告
func (rv *RobotV1) botPinAnnouncement(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := strconv.ParseUint(c.Param("announcementId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的公告ID"})
		return
	}
	a, err := rv.h.Svc.PinAnnouncement(uint(id), u.ID)
	if err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else if err == service.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "公告不存在"})
		} else {
			logger.Errorf("[RobotAPI] Failed to pin announcement %d: %v", id, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "置顶公告失败"})
		}
		return
	}
	c.JSON(http.StatusOK, a)
}

// botUnpinAnnouncement 取消置顶公告
func (rv *RobotV1) botUnpinAnnouncement(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	id, err := strconv.ParseUint(c.Param("announcementId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的公告ID"})
		return
	}
	if err := rv.h.Svc.UnpinAnnouncement(uint(id), u.ID); err != nil {
		if err == service.ErrNotFound {
			c.Status(http.StatusNoContent)
			return
		} else if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
			return
		}
		logger.Errorf("[RobotAPI] Failed to unpin announcement %d: %v", id, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "取消置顶失败"})
		return
	}
	c.Status(http.StatusNoContent)
}

// ==================== P2: Join Requests ====================

// botListJoinRequests 列出加入申请
func (rv *RobotV1) botListJoinRequests(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	guildID, err := strconv.ParseUint(c.Param("guildId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	requests, total, err := rv.h.Svc.ListGuildJoinRequests(uint(guildID), u.ID, page, limit)
	if err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else {
			logger.Errorf("[RobotAPI] Failed to list join requests for guild %d: %v", guildID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "获取加入申请失败"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":  requests,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// botApproveJoinRequest 批准加入申请
func (rv *RobotV1) botApproveJoinRequest(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	guildID, err := strconv.ParseUint(c.Param("guildId"), 10, 64)
	if err != nil || guildID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}
	requestID, err := strconv.ParseUint(c.Param("requestId"), 10, 64)
	if err != nil || requestID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求ID"})
		return
	}

	req, err := rv.h.Svc.Repo.GetGuildJoinRequestByID(uint(requestID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "请求不存在"})
		return
	}

	if err := rv.h.Svc.ApproveGuildJoinRequest(uint(guildID), uint(requestID), u.ID); err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else if err == service.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "请求不存在"})
		} else {
			logger.Errorf("[RobotAPI] Failed to approve join request %d for guild %d: %v", requestID, guildID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "批准申请失败"})
		}
		return
	}

	if rv.h.Gw != nil && req != nil {
		rv.h.Gw.BroadcastToGuild(uint(guildID), config.EventGuildMemberAdd, gin.H{"userId": req.UserID})
		guild, _ := rv.h.Svc.Repo.GetGuild(uint(guildID))
		notificationPayload := gin.H{
			"type":       "guild_join_approved",
			"sourceType": "system",
			"guildId":    guildID,
			"requestId":  requestID,
			"status":     "approved",
			"read":       false,
		}
		if guild != nil {
			notificationPayload["guild"] = gin.H{"id": guild.ID, "name": guild.Name, "avatar": guild.Avatar}
		}
		rv.h.Gw.BroadcastNotice(req.UserID, notificationPayload)
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// botRejectJoinRequest 拒绝加入申请
func (rv *RobotV1) botRejectJoinRequest(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	guildID, err := strconv.ParseUint(c.Param("guildId"), 10, 64)
	if err != nil || guildID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}
	requestID, err := strconv.ParseUint(c.Param("requestId"), 10, 64)
	if err != nil || requestID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求ID"})
		return
	}

	req, err := rv.h.Svc.Repo.GetGuildJoinRequestByID(uint(requestID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "请求不存在"})
		return
	}

	if err := rv.h.Svc.RejectGuildJoinRequest(uint(guildID), uint(requestID), u.ID); err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else if err == service.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "请求不存在"})
		} else {
			logger.Errorf("[RobotAPI] Failed to reject join request %d for guild %d: %v", requestID, guildID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "拒绝申请失败"})
		}
		return
	}

	if rv.h.Gw != nil && req != nil {
		guild, _ := rv.h.Svc.Repo.GetGuild(uint(guildID))
		notificationPayload := gin.H{
			"type":       "guild_join_rejected",
			"sourceType": "system",
			"guildId":    guildID,
			"requestId":  requestID,
			"status":     "rejected",
			"read":       false,
		}
		if guild != nil {
			notificationPayload["guild"] = gin.H{"id": guild.ID, "name": guild.Name, "avatar": guild.Avatar}
		}
		rv.h.Gw.BroadcastNotice(req.UserID, notificationPayload)
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ==================== P2: List Guilds ====================

// botListGuilds 列出机器人加入的服务器
func (rv *RobotV1) botListGuilds(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit < 1 || limit > 100 {
		limit = 10
	}
	beforeID64, _ := strconv.ParseUint(c.Query("beforeId"), 10, 64)
	afterID64, _ := strconv.ParseUint(c.Query("afterId"), 10, 64)
	beforeID := uint(beforeID64)
	afterID := uint(afterID64)

	if beforeID > 0 || afterID > 0 {
		guilds, err := rv.h.Svc.ListUserGuildsWithMemberCountCursor(u.ID, limit, beforeID, afterID)
		if err != nil {
			logger.Errorf("[RobotAPI] Failed to list guilds for bot %d: %v", u.ID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "获取服务器列表失败"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": guilds})
		return
	}

	page, _ := strconv.Atoi(c.Query("page"))
	if page < 1 {
		page = 1
	}
	guilds, total, err := rv.h.Svc.ListUserGuildsWithMemberCountPage(u.ID, page, limit)
	if err != nil {
		logger.Errorf("[RobotAPI] Failed to list guilds for bot %d: %v", u.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取服务器列表失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":  guilds,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

// ==================== P2: DM Edit/Delete ====================

// botEditDmMessage 编辑私聊消息
func (rv *RobotV1) botEditDmMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	messageID, err := strconv.ParseUint(c.Param("messageId"), 10, 64)
	if err != nil || messageID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的消息ID"})
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := c.BindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	if len([]rune(body.Content)) > int(config.MaxMessageLength) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "消息过长"})
		return
	}

	var msg models.DmMessage
	if err := rv.h.Svc.Repo.DB.First(&msg, uint(messageID)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "消息不存在"})
		return
	}
	if msg.AuthorID != u.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "仅作者可编辑"})
		return
	}

	now := time.Now()
	if err := rv.h.Svc.Repo.DB.Model(&msg).Updates(map[string]interface{}{
		"content":   body.Content,
		"edited_at": &now,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新失败"})
		return
	}
	rv.h.Svc.Repo.DB.First(&msg, msg.ID)

	if rv.h.Gw != nil {
		payload := rv.h.buildDmMessagePayload(&msg)
		var t models.DmThread
		if err := rv.h.Svc.Repo.DB.First(&t, msg.ThreadID).Error; err == nil {
			participants := []uint{t.UserAID, t.UserBID}
			rv.h.Gw.BroadcastToDM(msg.ThreadID, config.EventDmMessageUpdate, payload, participants)
		} else {
			rv.h.Gw.BroadcastToDM(msg.ThreadID, config.EventDmMessageUpdate, payload, nil)
		}
	}
	c.JSON(http.StatusOK, msg)
}

// botDeleteDmMessage 删除私聊消息
func (rv *RobotV1) botDeleteDmMessage(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	messageID, err := strconv.ParseUint(c.Param("messageId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的消息ID"})
		return
	}

	msg, err := rv.h.Svc.Repo.GetDmMessage(uint(messageID))
	if err != nil {
		c.Status(http.StatusNoContent)
		return
	}

	if err := rv.h.Svc.DeleteDmMessage(uint(messageID), u.ID); err != nil {
		if err == service.ErrNotFound {
			c.Status(http.StatusNoContent)
			return
		} else if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "只能删除自己的消息"})
			return
		}
		logger.Errorf("[RobotAPI] Failed to delete DM message %d: %v", messageID, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "删除消息失败"})
		return
	}

	if rv.h.Gw != nil {
		payload := rv.h.buildDmMessagePayload(msg)
		payload["deleted"] = true
		var t models.DmThread
		if err := rv.h.Svc.Repo.DB.First(&t, msg.ThreadID).Error; err == nil {
			participantIDs := []uint{t.UserAID, t.UserBID}
			rv.h.Gw.BroadcastToDM(msg.ThreadID, config.EventDmMessageDelete, payload, participantIDs)
			otherUserID := t.UserAID
			if otherUserID == u.ID {
				otherUserID = t.UserBID
			}
			rv.h.Gw.BroadcastToUsers([]uint{otherUserID}, config.EventDmMessageDelete, payload)
		} else {
			rv.h.Gw.BroadcastToDM(msg.ThreadID, config.EventDmMessageDelete, payload, nil)
		}
	}
	c.Status(http.StatusNoContent)
}

// ==================== P2: Channel Categories ====================

// botListChannelCategories 列出频道分类
func (rv *RobotV1) botListChannelCategories(c *gin.Context) {
	guildID, err := strconv.ParseUint(c.Param("guildId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}
	categories, err := rv.h.Svc.ListChannelCategories(uint(guildID))
	if err != nil {
		logger.Errorf("[RobotAPI] Failed to list channel categories for guild %d: %v", guildID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取分类列表失败"})
		return
	}
	c.JSON(http.StatusOK, categories)
}

// botCreateChannelCategory 创建频道分类
func (rv *RobotV1) botCreateChannelCategory(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	guildID, err := strconv.ParseUint(c.Param("guildId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := c.BindJSON(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误或名称不能为空"})
		return
	}
	cat, err := rv.h.Svc.CreateChannelCategory(uint(guildID), u.ID, body.Name)
	if err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else {
			logger.Errorf("[RobotAPI] Failed to create channel category in guild %d: %v", guildID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "创建分类失败"})
		}
		return
	}
	c.JSON(http.StatusOK, cat)
}

// botUpdateChannelCategory 更新频道分类
func (rv *RobotV1) botUpdateChannelCategory(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	guildID, err := strconv.ParseUint(c.Param("guildId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}
	categoryID, err := strconv.ParseUint(c.Param("categoryId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的分类ID"})
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := c.BindJSON(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误或名称不能为空"})
		return
	}
	cat, err := rv.h.Svc.UpdateChannelCategory(uint(guildID), uint(categoryID), u.ID, body.Name)
	if err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else if err == service.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "分类不存在"})
		} else {
			logger.Errorf("[RobotAPI] Failed to update channel category %d: %v", categoryID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "更新分类失败"})
		}
		return
	}
	c.JSON(http.StatusOK, cat)
}

// botDeleteChannelCategory 删除频道分类
func (rv *RobotV1) botDeleteChannelCategory(c *gin.Context) {
	u := middleware.UserFromCtx(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
		return
	}
	guildID, err := strconv.ParseUint(c.Param("guildId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的服务器ID"})
		return
	}
	categoryID, err := strconv.ParseUint(c.Param("categoryId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的分类ID"})
		return
	}
	if err := rv.h.Svc.DeleteChannelCategory(uint(guildID), uint(categoryID), u.ID); err != nil {
		if err == service.ErrUnauthorized {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		} else if err == service.ErrNotFound {
			c.Status(http.StatusNoContent)
			return
		} else {
			logger.Errorf("[RobotAPI] Failed to delete channel category %d: %v", categoryID, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "删除分类失败"})
		}
		return
	}
	c.Status(http.StatusNoContent)
}
