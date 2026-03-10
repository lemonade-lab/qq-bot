package handlers

import (
	"net/http"
	"os"
	"strings"

	"bubble/src/config"
	"bubble/src/middleware"
	"bubble/src/service"

	"github.com/gin-gonic/gin"
)

type HTTP struct {
	Svc *service.Service
	Cfg *config.Config
	Gw  *Gateway
}

func NewHTTP(s *service.Service, cfg *config.Config) *HTTP { return &HTTP{Svc: s, Cfg: cfg} }

// Register 负责在 gin.Engine 上注册全部 HTTP 路由。
func (h *HTTP) Register(r *gin.Engine) {
	r.Use(cors())

	// ==================== WebSocket 连接 - 不受HTTP速率限制 ====================
	// WebSocket 有自己的内部速率控制机制
	// 用户 Gateway
	r.GET("/api/gateway", func(c *gin.Context) {
		if h.Gw == nil {
			h.Gw = NewGateway(h.Svc)
		}
		h.Gw.connect(&Context{Writer: c.Writer, Request: c.Request})
	})
	r.GET("/gateway", func(c *gin.Context) {
		if h.Gw == nil {
			h.Gw = NewGateway(h.Svc)
		}
		h.Gw.connect(&Context{Writer: c.Writer, Request: c.Request})
	})
	// 机器人 Gateway
	r.GET("/api/bot/gateway", func(c *gin.Context) {
		if h.Gw == nil {
			h.Gw = NewGateway(h.Svc)
		}
		// 直接调用 gateway_bot.go 中的逻辑
		h.Gw.connectBot(&Context{Writer: c.Writer, Request: c.Request})
	})

	// 全局默认速率限制：300ms/request (~3 req/s, burst 6)
	// 注意：这个限制不会影响上面的 WebSocket 路由
	r.Use(middleware.DefaultRateLimiter())

	// ==================== Token相关 - 宽松速率限制 ====================
	// Token刷新和二维码轮询使用宽松限制（15 req/s, burst 30）
	tokenGroup := r.Group("/")
	tokenGroup.Use(middleware.TokenRateLimiter())
	{
		// Token刷新 - Access Token 15分钟过期，需要高频刷新
		tokenGroup.POST("/api/mobile/refresh", h.mobileRefresh)
		// 二维码状态轮询 - 客户端每2秒轮询一次
		tokenGroup.GET("/api/qrcode/:code/status", h.getQRCodeStatus)
	}
	// ==================== 敏感操作 - 严格速率限制 ====================
	// 登录、注册、密码重置等操作使用严格限制（1 req/s, burst 3）
	strict := r.Group("/")
	strict.Use(middleware.StrictRateLimiter())
	{
		// 注册相关
		strict.POST("/api/register/email", h.registerByEmail)
		strict.POST("/api/register/email/verify/request", h.requestRegisterEmailVerification)
		strict.POST("/api/register/email/verify", h.verifyRegisterEmail)
		// 登录相关
		strict.POST("/api/login", h.loginByEmail)
		strict.POST("/api/login/email", h.requestLoginVerification)
		strict.POST("/api/login/email/verify", h.verifyLoginCode)
		strict.POST("/api/mobile/login/device", h.mobileLoginByDevice)
		// 密码重置
		strict.POST("/api/password/reset/request", h.requestPasswordReset)
		strict.POST("/api/password/reset", h.resetPassword)
		// 邮箱验证
		strict.POST("/api/email/verify/request", h.requestEmailVerify)
		strict.POST("/api/email/verify", h.verifyEmail)
		// 手机号短信登录/注册
		strict.POST("/api/phone/send-code", h.phoneSendCode)
		strict.POST("/api/phone/verify", h.phoneVerifyCode)
		// 二维码登录相关操作（生成、取消、完成）
		strict.POST("/api/qrcode/generate", h.generateQRCode)
		strict.POST("/api/qrcode/login", h.completeQRCodeLogin)
	}

	// ==================== 公开API - 默认速率限制 ====================
	// 二维码取消
	r.POST("/api/qrcode/:code/cancel", h.cancelQRCode)
	// 二维码卡片信息获取（公开访问）
	r.GET("/api/qrcode/:code/card", h.getQRCodeCard)
	// 二维码扫描和确认（需要认证）
	r.POST("/api/qrcode/:code/scan", middleware.AuthRequired(h.Svc), h.scanQRCode)
	r.POST("/api/qrcode/:code/confirm", middleware.AuthRequired(h.Svc), h.confirmQRCode)

	r.GET("/api/enums/status", h.getStatusEnums)
	r.GET("/api/enums/channel-types", h.getChannelTypeEnums)
	// global limits
	r.GET("/api/limits", h.getGlobalLimits)
	r.GET("/api/permissions", h.getAllPermissions)
	// guild categories (public)
	r.GET("/api/guild-categories", h.getAllGuildCategories)
	r.GET("/api/guilds/by-category", h.listGuildsByCategory)
	// guild levels (public)
	r.GET("/api/guild-levels", h.getAllGuildLevels)
	r.GET("/api/guilds/:id/member-limit", h.getGuildMemberLimit)
	// public share info
	r.GET("/api/share/guild/:id", h.getGuildShareInfo)
	r.GET("/api/share/user/:id", h.getUserShareInfo)
	// public hot guilds (no auth)
	r.GET("/api/public/guilds/hot", h.hotGuilds)

	// ==================== Guilds API v2 ====================
	// v2 版本的服务器接口 - 支持临时身份访问
	v2 := r.Group("/api/v2")
	{
		// 热门服务器列表（支持匿名访问，可选认证）
		v2.GET("/guilds/hot", middleware.OptionalAuth(h.Svc), h.hotGuildsV2)
		// 服务器详情（支持匿名访问，可选认证），通过id获取
		v2.GET("/guilds/:id", middleware.OptionalAuth(h.Svc), h.GetGuildDetailV2)

		// （需要认证）
		v2Auth := v2.Group("/")
		v2Auth.Use(middleware.AuthRequired(h.Svc))
		// 申请加入服务器
		v2Auth.POST("/guilds/:id/apply", h.applyToJoinGuildV2)
		// 模糊查询
		v2.GET("/guilds/search", h.SearchGuildsV2)
	}

	// ==================== Auth & Device API v2 ====================
	// v2 登录接口 - 智能设备感知（严格速率限制）
	v2AuthStrict := r.Group("/api/v2")
	v2AuthStrict.Use(middleware.StrictRateLimiter())
	{
		v2AuthStrict.POST("/login", h.loginV2)
		v2AuthStrict.POST("/login/verify", h.verifyLoginV2)
		// Magic Login — 邮箱登录即注册（验证码登录，无需密码）
		v2AuthStrict.POST("/magic/request", h.magicLoginRequest)
		v2AuthStrict.POST("/magic/verify", h.magicLoginVerify)
	}
	// v2 设备管理接口（需要认证）
	v2Devices := r.Group("/api/v2")
	v2Devices.Use(middleware.AuthRequired(h.Svc))
	{
		v2Devices.GET("/devices", h.listDevicesV2)
		v2Devices.POST("/devices/trust", h.trustDeviceV2)
		v2Devices.POST("/devices/revoke", h.revokeDeviceV2)
		v2Devices.POST("/devices/delete", h.deleteDeviceV2)
	}

	// ==================== API v1 ====================
	// v1 版本的通用 API
	v1 := r.Group("/api/v1")
	{
		// 二维码图片生成（公开访问）
		v1.GET("/create-qr-code", h.createQRCodeImage)
	}

	// ==================== Robot Platform API v1 ====================
	// 机器人开放平台 API - 版本隔离
	robotV1 := NewRobotV1(h)

	// 公开 API - 机器人分享页面
	publicV1 := r.Group("/api/bot/v1/pub")
	robotV1.RegisterPublic(publicV1)

	// 开发者 API - 管理机器人
	developerV1 := r.Group("/api/developer/v1")
	robotV1.RegisterDeveloper(developerV1)

	// 机器人 API - 机器人调用的接口
	// 使用更高的速率限制（20 req/s, burst 40）
	botV1 := r.Group("/api/bot/v1")
	botV1.Use(middleware.BotRateLimiter())
	robotV1.RegisterBot(botV1)

	// ==================== 认证保护的路由 ====================
	auth := r.Group("/")
	auth.Use(middleware.AuthRequired(h.Svc))

	// 消息发送 - 使用中等速率限制（5 req/s, burst 10）
	authModerate := auth.Group("/")
	authModerate.Use(middleware.ModerateRateLimiter())
	{
		authModerate.POST("/api/messages", h.postMessage)
		authModerate.POST("/api/dm/messages", h.postDmMessage)
		authModerate.POST("/api/group/threads/:id/messages", h.postGroupMessage)
		authModerate.POST("/api/subrooms/:roomId/messages", h.postSubRoomMessage)
		authModerate.POST("/api/files/upload", h.uploadFile)
		authModerate.POST("/api/moments", h.createMoment)
		authModerate.POST("/api/interactions", h.postInteraction) // 用户→机器人交互消息
	}

	// 其他认证路由使用默认速率限制（300ms/request）
	auth.GET("/api/channels", h.listChannels)
	auth.GET("/api/messages", h.getMessages)
	auth.GET("/api/messages/with-users", h.getMessagesWithUsers)
	auth.DELETE("/api/messages/:id", h.deleteMessage)
	auth.POST("/api/channels/:id/messages/batch-delete", h.batchDeleteMessages)
	auth.PUT("/api/messages/:id", h.updateMessage)

	// me: profile/password/status
	auth.PUT("/api/me/profile", h.updateProfile)
	auth.PUT("/api/me/profile/extended", h.updateExtendedProfile)
	auth.PUT("/api/me/settings", h.updateUserSettings)
	auth.PUT("/api/me/password", h.changePassword)
	auth.PUT("/api/me/status", h.updateStatus)
	auth.PUT("/api/me/avatar", h.updateAvatar)
	auth.GET("/api/me", h.me)
	auth.GET("/api/validity", h.Validity)
	// 手机号绑定（需要登录）
	auth.POST("/api/phone/bind/send-code", h.phoneBindSendCode)
	auth.POST("/api/phone/bind/verify", h.phoneBindVerify)
	// 红点系统 - 未读统计
	auth.GET("/api/me/unread", h.getUnreadCounts)
	// 移动端设备管理（web 也可通过 session 调用）
	auth.GET("/api/mobile/devices", h.mobileListDevices)
	auth.POST("/api/mobile/devices/add", h.mobileAddDevice)
	auth.POST("/api/mobile/devices/revoke", h.mobileRevokeDevice)
	auth.POST("/api/mobile/devices/delete", h.mobileDeleteDevice)

	// favorites
	auth.POST("/api/favorites/channel", h.favoriteChannelMessage)
	auth.POST("/api/favorites/dm", h.favoriteDmMessage)
	auth.POST("/api/favorites/group", h.favoriteGroupMessage)
	auth.DELETE("/api/favorites/:id", h.unfavoriteMessage)
	auth.GET("/api/favorites", h.listFavorites)
	auth.POST("/api/favorites/:id/send", h.sendFavoriteAsMessage)
	// 列出文件（按分类，当前用户）
	auth.GET("/api/files/list", h.listFiles)
	// 删除个人表情（仅允许删除属于当前用户的 emoji）
	auth.POST("/api/emojis/delete", h.deleteEmoji)
	auth.POST("/api/logout", h.logout)
	// channel categories
	auth.GET("/api/guilds/:id/categories", h.listChannelCategories)
	auth.POST("/api/guilds/:id/categories", h.createChannelCategory)
	auth.PUT("/api/guilds/:id/categories/reorder", h.reorderChannelCategories)
	auth.PUT("/api/guilds/:id/categories/:categoryId", h.updateChannelCategory)
	auth.DELETE("/api/guilds/:id/categories/:categoryId", h.deleteChannelCategory)
	auth.POST("/api/channels", h.createChannel)
	auth.GET("/api/channels/:id", h.getChannel)
	auth.GET("/api/channels/:id/livekit-token", h.getChannelLiveKitToken)
	auth.GET("/api/channels/:id/participants", h.getChannelParticipants)
	auth.GET("/api/channels/:id/stats", h.getChannelStats)
	auth.PUT("/api/channels/:id/category", h.setChannelCategory)
	auth.PUT("/api/channels/:id", h.updateChannel)
	// forum posts
	auth.GET("/api/channels/:id/posts", h.listForumPosts)
	auth.POST("/api/channels/:id/posts", h.createForumPost)
	auth.GET("/api/channels/:id/posts/:postId", h.getForumPost)
	auth.POST("/api/channels/:id/posts/:postId/like", h.likeForumPost)
	auth.PUT("/api/channels/:id/posts/:postId", h.updateForumPost)
	auth.DELETE("/api/channels/:id/posts/:postId", h.deleteForumPost)
	auth.POST("/api/channels/:id/posts/:postId/pin", h.pinForumPost)
	auth.POST("/api/channels/:id/posts/:postId/lock", h.lockForumPost)
	// forum replies
	auth.GET("/api/channels/:id/posts/:postId/replies", h.listForumReplies)
	auth.POST("/api/channels/:id/posts/:postId/replies", h.createForumReply)
	auth.DELETE("/api/channels/:id/posts/:postId/replies/:replyId", h.deleteForumReply)
	// 红点系统 - 标记已读
	auth.POST("/api/channels/:id/read", h.markChannelRead)
	// structured channels for a guild
	auth.GET("/api/guilds/:id/channels/structured", h.getGuildChannelStructure)
	// LiveKit 统计 API (管理员)
	auth.GET("/api/livekit/stats", h.getLiveKitStats)
	// LiveKit Webhook (无需认证，由 LiveKit 签名验证)
	r.POST("/api/livekit/webhook", h.handleLiveKitWebhook)
	auth.POST("/api/guilds/:id/join", h.joinGuild)
	// guild join requests (apply + manage)
	auth.POST("/api/guilds/:id/join-requests", h.applyGuildJoin)
	auth.GET("/api/guilds/:id/join-requests", h.listGuildJoinRequests)
	auth.GET("/api/guilds/:id/join-requests/my-status", h.getUserGuildJoinRequestStatus)
	auth.POST("/api/guilds/join-requests/my-status/batch", h.getUserGuildJoinRequestStatusBatch)
	auth.POST("/api/guilds/:id/join-requests/:requestId/approve", h.approveGuildJoin)
	auth.POST("/api/guilds/:id/join-requests/:requestId/reject", h.rejectGuildJoin)
	auth.POST("/api/guilds/:id/leave", h.leaveGuild)
	auth.GET("/api/guilds/:id/members", h.listMembers)
	auth.GET("/api/guilds/:id/members/search", h.searchMembers)
	auth.DELETE("/api/guilds/:id/members/:userId", h.kickMember)
	auth.PUT("/api/guilds/:id/members/:userId/mute", h.muteMember)
	auth.DELETE("/api/guilds/:id/members/:userId/mute", h.unmuteMember)
	auth.PUT("/api/guilds/:id/members/:userId/nickname", h.setMemberNickname)
	auth.PUT("/api/guilds/:id/notify-mute", h.setGuildNotifyMuted)
	auth.GET("/api/guilds/:id/permissions", h.getGuildPermissions)
	auth.GET("/api/guilds/search", h.searchGuilds)
	auth.GET("/api/guilds/hot", h.hotGuilds)
	auth.GET("/api/guilds", h.listGuilds)
	auth.POST("/api/guilds", h.createGuild)
	auth.DELETE("/api/guilds/:id", h.deleteGuild)
	auth.PATCH("/api/guilds/:id", h.updateGuild)
	auth.PUT("/api/guilds/reorder", h.reorderGuilds)
	// 红点系统 - 标记整个服务器已读
	auth.POST("/api/guilds/:id/read", h.markGuildRead)
	auth.PUT("/api/guilds/:id/privacy", h.setGuildPrivacy)
	auth.PUT("/api/guilds/:id/auto-join-mode", h.setGuildAutoJoinMode)
	// GET 允许匿名访问（前端可用于展示当前加入模式）
	r.GET("/api/guilds/:id/auto-join-mode", h.getGuildAutoJoinMode)
	auth.PUT("/api/guilds/:id/description", h.updateGuildDescription)
	auth.PUT("/api/guilds/:id/avatar", h.updateGuildAvatar)
	auth.PATCH("/api/guilds/:id/category", h.setGuildCategory)
	auth.PATCH("/api/guilds/:id/level", h.setGuildLevel)
	// guild media
	auth.GET("/api/guilds/:id/media/images", h.getGuildImages)
	auth.GET("/api/guilds/:id/media/videos", h.getGuildVideos)
	auth.GET("/api/guilds/:id/media/files", h.getGuildFiles)
	auth.DELETE("/api/guilds/:id/media", h.deleteGuildMedia)
	auth.DELETE("/api/channels/:id", h.deleteChannel)
	auth.PUT("/api/guilds/:id/channels/reorder", h.reorderChannels)
	// roles mgmt
	auth.GET("/api/guilds/:id/roles", h.listRoles)
	auth.POST("/api/guilds/:id/roles", h.createRole)
	auth.PUT("/api/guilds/:id/roles/:roleId", h.updateRole)
	auth.DELETE("/api/guilds/:id/roles/:roleId", h.deleteRole)
	auth.POST("/api/guilds/:id/roles/:roleId/assign/:userId", h.assignRole)
	auth.POST("/api/guilds/:id/roles/:roleId/remove/:userId", h.removeRole)
	// announcements
	auth.GET("/api/guilds/:id/announcements/featured", h.getFeaturedAnnouncement)
	auth.GET("/api/guilds/:id/announcements", h.listAnnouncements)
	auth.POST("/api/guilds/:id/announcements", h.createAnnouncement)
	auth.GET("/api/announcements/:id", h.getAnnouncement)
	auth.PUT("/api/announcements/:id", h.updateAnnouncement)
	auth.DELETE("/api/announcements/:id", h.deleteAnnouncement)
	auth.PUT("/api/announcements/:id/pin", h.pinAnnouncement)
	auth.DELETE("/api/announcements/:id/pin", h.unpinAnnouncement)
	// guild files (server file system)
	auth.GET("/api/guilds/:id/files", h.listGuildFiles)
	auth.POST("/api/guilds/:id/files", h.uploadGuildFile)
	auth.DELETE("/api/guilds/:id/files/:fileId", h.deleteGuildFile)
	auth.PATCH("/api/guilds/:id/files/:fileId/rename", h.renameGuildFile)
	auth.POST("/api/guilds/:id/files/batch-delete", h.batchDeleteGuildFiles)
	// pinned messages
	auth.GET("/api/channels/:id/pins", h.listPinnedMessages)
	auth.POST("/api/channels/:id/pins", h.pinMessage)
	auth.DELETE("/api/channels/:id/pins/:messageId", h.unpinMessage)
	// message reactions
	auth.GET("/api/messages/:id/reactions", h.listReactions)
	auth.PUT("/api/messages/:id/reactions/:emoji", h.addReaction)
	auth.DELETE("/api/messages/:id/reactions/:emoji", h.removeReaction)
	// pinned dm messages
	auth.GET("/api/dm/threads/:id/pins", h.listPinnedDmMessages)
	auth.POST("/api/dm/threads/:id/pins", h.pinDmMessage)
	auth.DELETE("/api/dm/threads/:id/pins/:messageId", h.unpinDmMessage)

	// friends
	auth.GET("/api/users/search", h.searchUsers)
	auth.GET("/api/users/search-by-id", h.searchUsersByID)
	auth.GET("/api/friends/search", h.searchFriends)

	// 全局搜索
	auth.GET("/api/search", h.globalSearch)
	auth.POST("/api/friends/requests", h.sendFriendRequest)
	auth.GET("/api/friends/requests", h.listFriendRequests)
	auth.POST("/api/friends/accept", h.acceptFriendRequest)
	auth.DELETE("/api/friends", h.removeFriend)
	auth.GET("/api/friends", h.listFriends)
	auth.PUT("/api/friends/:userId/nickname", h.setFriendNickname)
	auth.PUT("/api/friends/:userId/privacy", h.setFriendPrivacyMode)
	// 好友验证模式设置
	auth.PUT("/api/me/friend-request-mode", h.setFriendRequestMode)
	// 获取好友的朋友圈列表（仅限好友）
	auth.GET("/api/friends/:userId/moments", h.listFriendMoments)

	// direct messages
	auth.POST("/api/dm/open", h.openDm)
	auth.GET("/api/dm/threads", h.listDmThreads)
	auth.GET("/api/dm/threads/:id/livekit-token", h.getDmLiveKitToken)
	auth.PUT("/api/dm/threads/:id/pin", h.pinDmThread)
	auth.PUT("/api/dm/threads/:id/block", h.blockDmThread)
	// 红点系统 - 标记DM已读
	auth.POST("/api/dm/:id/read", h.markDmRead)
	auth.GET("/api/dm/messages", h.getDmMessages)
	auth.GET("/api/dm/messages/with-users", h.getDmMessagesWithUsers)
	auth.PUT("/api/dm/messages/:id", h.updateDmMessage)
	auth.DELETE("/api/dm/messages/:id", h.deleteDmMessage)
	auth.POST("/api/dm/messages/batch-delete", h.batchDeleteDmMessages)
	auth.DELETE("/api/dm/threads/:id", h.deleteDmThread)

	// group threads (群聊)
	auth.POST("/api/group/create", h.createGroupThread)
	auth.GET("/api/group/threads", h.listGroupThreads)
	auth.GET("/api/group/threads/:id", h.getGroupThread)
	auth.PUT("/api/group/threads/:id", h.updateGroupThread)
	auth.DELETE("/api/group/threads/:id", h.deleteGroupThread)
	auth.POST("/api/group/threads/:id/members", h.addGroupMembers)
	auth.DELETE("/api/group/threads/:id/members/:userId", h.removeGroupMember)
	auth.PUT("/api/group/threads/:id/members/:userId/role", h.updateGroupMemberRole)
	auth.POST("/api/group/threads/:id/transfer", h.transferGroupOwner)
	auth.PUT("/api/group/threads/:id/mute", h.setGroupMuted)
	auth.GET("/api/group/threads/:id/messages", h.getGroupMessages)
	auth.PUT("/api/group/messages/:messageId", h.updateGroupMessage)
	auth.DELETE("/api/group/messages/:messageId", h.deleteGroupMessage)
	auth.POST("/api/group/messages/batch-delete", h.batchDeleteGroupMessages)
	auth.POST("/api/group/threads/:id/read", h.markGroupRead)
	// 群聊精华消息
	auth.GET("/api/group/threads/:id/pins", h.listPinnedGroupMessages)
	auth.POST("/api/group/threads/:id/pins", h.pinGroupMessage)
	auth.DELETE("/api/group/threads/:id/pins/:messageId", h.unpinGroupMessage)
	// 群聊成员搜索（支持@提及）
	auth.GET("/api/group/threads/:id/members/search", h.searchGroupMembers)
	// 群聊加入申请
	auth.POST("/api/group/threads/:id/join-requests", h.applyGroupJoin)
	auth.GET("/api/group/threads/:id/join-requests", h.listGroupJoinRequests)
	auth.GET("/api/group/threads/:id/join-requests/me", h.getUserGroupJoinRequestStatus)
	auth.POST("/api/group/threads/:id/join-requests/:requestId/approve", h.approveGroupJoin)
	auth.POST("/api/group/threads/:id/join-requests/:requestId/reject", h.rejectGroupJoin)
	// 群公告
	auth.GET("/api/group/threads/:id/announcements", h.listGroupAnnouncements)
	auth.POST("/api/group/threads/:id/announcements", h.createGroupAnnouncement)
	auth.GET("/api/group/threads/:id/announcements/featured", h.getFeaturedGroupAnnouncement)
	auth.GET("/api/group/announcements/:announcementId", h.getGroupAnnouncement)
	auth.PUT("/api/group/announcements/:announcementId", h.updateGroupAnnouncement)
	auth.DELETE("/api/group/announcements/:announcementId", h.deleteGroupAnnouncement)
	auth.POST("/api/group/announcements/:announcementId/pin", h.pinGroupAnnouncement)
	auth.DELETE("/api/group/announcements/:announcementId/pin", h.unpinGroupAnnouncement)
	// 群文件
	auth.POST("/api/group/threads/:id/files", h.uploadGroupFile)
	auth.GET("/api/group/threads/:id/files", h.listGroupFiles)
	auth.DELETE("/api/group/files/:fileId", h.deleteGroupFile)
	auth.POST("/api/group/files/batch-delete", h.batchDeleteGroupFiles)

	// subrooms (频道子房间)
	auth.POST("/api/channels/:id/subrooms", h.createSubRoom)
	auth.GET("/api/channels/:id/subrooms", h.listSubRooms)
	auth.GET("/api/subrooms/:roomId", h.getSubRoom)
	auth.PUT("/api/subrooms/:roomId", h.updateSubRoom)
	auth.DELETE("/api/subrooms/:roomId", h.deleteSubRoom)
	auth.POST("/api/subrooms/:roomId/members", h.addSubRoomMembers)
	auth.DELETE("/api/subrooms/:roomId/members/:userId", h.removeSubRoomMember)
	auth.GET("/api/subrooms/:roomId/messages", h.getSubRoomMessages)
	auth.DELETE("/api/subrooms/messages/:messageId", h.deleteSubRoomMessage)
	auth.POST("/api/subrooms/:roomId/read", h.markSubRoomRead)

	// 隐藏会话管理
	auth.PUT("/api/dm/threads/:id/hidden", h.setDmThreadHidden)
	auth.GET("/api/dm/hidden-threads", h.listHiddenDmThreads)
	auth.PUT("/api/group/threads/:id/hidden", h.setGroupThreadHidden)
	auth.GET("/api/group/hidden-threads", h.listHiddenGroupThreads)
	auth.PUT("/api/subrooms/:roomId/hidden", h.setSubRoomHidden)
	auth.GET("/api/hidden-subrooms", h.listHiddenSubRooms)
	// 群聊置顶
	auth.PUT("/api/group/threads/:id/pin", h.pinGroupThread)
	// 统一查询接口
	auth.GET("/api/threads", h.listAllThreads)                // 统一线程列表（支持 type=dm/group, filter=hidden/pinned）
	auth.GET("/api/pinned-messages", h.listAllPinnedMessages) // 统一精华消息列表（支持 type=channel/dm/group）

	// blacklist
	auth.GET("/api/blacklist", h.listBlacklist)
	auth.POST("/api/blacklist", h.addToBlacklist)
	auth.DELETE("/api/blacklist/:userId", h.removeFromBlacklist)
	// greetings
	auth.POST("/api/users/:id/greet", h.greetUser)
	// user relationships
	auth.GET("/api/users/:id/is-friend", h.checkIsFriend)
	auth.GET("/api/users/:id/shared-guilds", h.getSharedGuilds)
	auth.GET("/api/users/:id/detail", h.getUserDetail)
	// privacy settings
	auth.PUT("/api/me/privacy", h.setPrivacy)
	auth.GET("/api/security/status", h.securityStatus)

	// personal notifications & applications
	auth.GET("/api/me/notifications", h.listMyNotifications)
	auth.PUT("/api/me/notifications/:id/read", h.markNotificationRead)
	auth.PUT("/api/me/notifications/read", h.markNotificationsRead)               // 批量标记已读
	auth.PUT("/api/me/notifications/read-all", h.markAllNotificationsRead)        // 全部标记已读
	auth.POST("/api/notifications/:id/accept", h.acceptFriendRequestNotification) // 接受好友申请通知
	auth.POST("/api/notifications/:id/reject", h.rejectFriendRequestNotification) // 拒绝好友申请通知
	auth.GET("/api/me/applications", h.listMyApplications)                        // 已废弃，建议使用通知系统
	auth.POST("/api/applications", h.createApplication)
	auth.PUT("/api/applications/:id", h.updateApplicationStatus)

	// moments (朋友圈) - 读取操作
	auth.GET("/api/moments", h.listMoments)
	auth.DELETE("/api/moments/:id", h.deleteMoment)
	auth.POST("/api/moments/:id/like", h.likeMoment)
	auth.DELETE("/api/moments/:id/like", h.unlikeMoment)
	auth.POST("/api/moments/:id/comments", h.commentMoment)
	auth.DELETE("/api/moments/comments/:id", h.deleteComment)

}

// ==================== Helper Functions ====================
// Handler implementations moved to dedicated files:
// - http_auth.go: registerByEmail, loginByEmail, requestEmailVerify, verifyEmail, requestPasswordReset, resetPassword
// - http_enums.go: getStatusEnums
// - http_users.go: searchUsers, greetUser
// - http_security.go: securityStatus

func cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")

		// ✅ CORS白名单机制 (生产环境必须配置)
		// 从环境变量读取允许的源,用逗号分隔
		// 例如: ALLOWED_ORIGINS=http://localhost:5173,https://yourdomain.com
		allowedOriginsEnv := os.Getenv("ALLOWED_ORIGINS")
		var allowedOrigins []string
		if allowedOriginsEnv != "" {
			allowedOrigins = strings.Split(allowedOriginsEnv, ",")
		} else {
			// 默认开发环境白名单
			allowedOrigins = []string{
				"http://localhost:5173",
				"http://localhost:3000",
				"http://127.0.0.1:5173",
				"http://127.0.0.1:3000",
			}
		}

		// 检查Origin是否在白名单中
		allowed := false
		for _, allowedOrigin := range allowedOrigins {
			if strings.TrimSpace(allowedOrigin) == origin {
				allowed = true
				break
			}
		}

		// 只允许白名单内的源
		if allowed {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		} else if origin == "" {
			// 无Origin的请求 (如Postman/curl) - 不设置CORS头,由浏览器外客户端自行处理
			// 注意: 不设置 * 因为我们需要credentials
		}
		// else: origin不在白名单中,拒绝CORS访问 (不设置Allow-Origin头)

		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
