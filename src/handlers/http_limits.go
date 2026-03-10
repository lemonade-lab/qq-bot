package handlers

import (
	"net/http"

	"bubble/src/config"

	"github.com/gin-gonic/gin"
)

// @Summary      Get global limits
// @Tags         enums
// @Produce      json
// @Success      200  {object}  map[string]int64
// @Router       /api/limits [get]
func (h *HTTP) getGlobalLimits(c *gin.Context) {
	// 统一的极限值映射，后端维护，前端直接使用
	// 数值统一使用 int64，便于前端处理大整数（如字节大小）
	limits := gin.H{
		// 最大字符长度
		"MAX_MESSAGE_LENGTH": config.MaxMessageLength,
		// 好友数量最多
		"MAX_FRIENDS": config.MaxFriends,
		// 频道数量最多
		"MAX_CHANNELS": config.MaxChannels,
		// 服务器数量最多
		"MAX_GUILDS": config.MaxGuilds,
		// 服务器人数最多
		"MAX_GUILD_MEMBERS": config.MaxGuildMembers,
		// 个人表情数量最多
		"MAX_CUSTOM_EMOJIS": config.MaxCustomEmojis,
		// 服务器表情数量最多
		"MAX_GUILD_EMOJIS": config.MaxGuildEmojis,
		// 私聊列表展示最多
		"MAX_DM_THREADS_DISPLAY": config.MaxDmThreadsDisplay,
		// 服务器简介字数最多
		"MAX_GUILD_DESCRIPTION_LENGTH": config.MaxGuildDescriptionLength,
		// 服务器公告字数最多
		"MAX_GUILD_ANNOUNCEMENT_LENGTH": config.MaxGuildAnnouncementLength,
		// 用户名最大长度
		"MAX_USERNAME_LENGTH": config.MaxUsernameLength,
		// 个人创建的频道数量最多
		"MAX_OWNED_CHANNELS": config.MaxOwnedChannels,
		// 个人密码最小长度
		"MIN_PASSWORD_LENGTH": config.MinPasswordLength,
		// 同时申请加好友最大数量
		"MAX_FRIEND_REQUESTS": config.MaxFriendRequests,
		// 同时申请加入服务器最大数量
		"MAX_GUILD_JOIN_REQUESTS": config.MaxGuildJoinRequests,
		// 服务器公告数量最多创建
		"MAX_GUILD_ANNOUNCEMENTS": config.MaxGuildAnnouncements,
		// 会话精华消息数量最多
		"MAX_GUILD_PINNED_MESSAGES": config.MaxGuildPinnedMessages,
		// 单条语音消息最长时间（秒）
		"MAX_VOICE_MESSAGE_LENGTH": config.MaxVoiceMessageLength,
		// 图片大小限制（字节）
		"MAX_IMAGE_SIZE": config.MaxImageSize,
		// 文件大小限制（字节）
		"MAX_FILE_SIZE": config.MaxFileSize,
		// 个人头像最大大小（字节）
		"MAX_AVATAR_SIZE": config.MaxAvatarSize,
		// 服务器头像最大大小（字节）
		"MAX_GUILD_AVATAR_SIZE": config.MaxGuildAvatarSize,
		// 服务器封面最大大小（字节）
		"MAX_GUILD_COVER_SIZE": config.MaxGuildCoverSize,
		// 每天可发送图片消息数量
		"DAILY_IMAGE_MESSAGE_LIMIT": int64(1000),
		// 每天可发送文件消息数量
		"DAILY_FILE_MESSAGE_LIMIT": int64(500),
		// 每天可发送语音消息数量
		"DAILY_VOICE_MESSAGE_LIMIT": int64(1000),
		// 每天可发送文字消息数量
		"DAILY_TEXT_MESSAGE_LIMIT": int64(1000 * 100),

		// ===== 新增：分页与查询限制 =====
		"DEFAULT_PAGE_LIMIT": config.DefaultPageLimit,
		"MAX_PAGE_LIMIT":     config.MaxPageLimit,
		"MAX_SEARCH_RESULTS": config.MaxSearchResults,

		// ===== 新增：消息结构限制 =====
		"MAX_MESSAGE_ATTACHMENTS":               config.MaxMessageAttachments,
		"MAX_ATTACHMENT_NAME_LENGTH":            config.MaxAttachmentNameLength,
		"MAX_ATTACHMENT_PER_MESSAGE_TOTAL_SIZE": config.MaxAttachmentPerMessageTotalSize,
		"MAX_MESSAGE_MENTIONS":                  config.MaxMessageMentions,
		"MAX_MESSAGE_REPLY_DEPTH":               config.MaxMessageReplyDepth,

		// ===== 新增：服务器结构限制 =====
		"MAX_GUILD_CATEGORIES":     config.MaxGuildCategories,
		"MAX_GUILD_ROLES":          config.MaxGuildRoles,
		"MAX_ROLE_NAME_LENGTH":     config.MaxRoleNameLength,
		"MAX_CHANNEL_NAME_LENGTH":  config.MaxChannelNameLength,
		"MAX_CATEGORY_NAME_LENGTH": config.MaxCategoryNameLength,

		// ===== 新增：好友/黑名单/收藏 =====
		"MAX_BLACKLIST": config.MaxBlacklist,
		"MAX_FAVORITES": config.MaxFavorites,

		// ===== 新增：LiveKit/语音频道 =====
		"MAX_VOICE_PARTICIPANTS_PER_CHANNEL": config.MaxVoiceParticipantsPerChannel,
		"MAX_DM_VOICE_PARTICIPANTS":          config.MaxDmVoiceParticipants,

		// ===== 新增：论坛/帖子 =====
		"MAX_FORUM_POST_TITLE_LENGTH":   config.MaxForumPostTitleLength,
		"MAX_FORUM_POST_CONTENT_LENGTH": config.MaxForumPostContentLength,
		"MAX_FORUM_POST_REPLIES":        config.MaxForumPostReplies,
		"MAX_FORUM_REPLY_LENGTH":        config.MaxForumReplyLength,

		// ===== 新增：公告/描述/昵称等文本 =====
		"MAX_USER_NICKNAME_LENGTH": config.MaxUserNicknameLength,
		"MAX_USER_BIO_LENGTH":      config.MaxUserBioLength,
		"MAX_GUILD_RULES_LENGTH":   config.MaxGuildRulesLength,

		// ===== 新增：机器人/开放平台 =====
		"MAX_ROBOTS_PER_OWNER":     config.MaxRobotsPerOwner,
		"MAX_BOT_USER_NAME_LENGTH": config.MaxBotUserNameLength,
	}
	c.JSON(http.StatusOK, limits)
}
