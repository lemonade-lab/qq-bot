package config

// 统一系统极限值常量，供所有接口使用
const (
	// ==================== 速率限制 ====================
	// 注意：WebSocket 连接 (/api/gateway, /api/bot/gateway) 不受HTTP速率限制影响
	// WebSocket 内部有自己的速率控制机制（见 config/ws.go）

	// 默认速率限制（通用API）: 300ms/request = ~3.3 req/s
	DefaultRateLimitRPS   int = 3 // 每秒请求数
	DefaultRateLimitBurst int = 6 // 突发容量

	// 严格速率限制（敏感操作：登录、注册、密码重置）: 1 req/s
	// burst=3 允许合理的重试（网络问题、验证码错误等）
	StrictRateLimitRPS   int = 1
	StrictRateLimitBurst int = 3

	// 宽松速率限制（读取操作：查询、列表）: 10 req/s
	LooseRateLimitRPS   int = 10
	LooseRateLimitBurst int = 20

	// 中等速率限制（写入操作：发消息、上传）: 5 req/s
	ModerateRateLimitRPS   int = 5
	ModerateRateLimitBurst int = 10

	// Token相关速率限制（token刷新、轮询）: 15 req/s
	// 适用于token刷新和二维码状态轮询等高频操作
	TokenRateLimitRPS   int = 15
	TokenRateLimitBurst int = 30

	// ==================== 消息/文本 ====================
	MaxMessageLength           int64 = 1600
	MaxUsernameLength          int64 = 8
	MinPasswordLength          int64 = 8
	MaxGuildDescriptionLength  int64 = 190
	MaxGuildAnnouncementLength int64 = 300
	MaxAnnouncementImages      int64 = 9
	MaxReactionsPerMessage     int64 = 20 // 每条消息最多不同 emoji 种类
	MaxUserNicknameLength      int64 = 32
	MaxUserBioLength           int64 = 300
	MaxGuildRulesLength        int64 = 2000

	// 数量上限（用户/结构）
	MaxFriends                 int64 = 1000
	MaxChannels                int64 = 50
	MaxGuilds                  int64 = 100
	MaxGuildMembers            int64 = 2000
	MaxOwnedChannels           int64 = 6
	MaxCustomEmojis            int64 = 50
	MaxGuildEmojis             int64 = 200
	MaxDmThreadsDisplay        int64 = 50
	MaxGroupMembers            int64 = 100               // 群聊最大成员数
	MaxGroupNameLength         int64 = 32                // 群名最大长度
	MaxOwnedGroups             int64 = 20                // 单用户最多拥有的群聊数
	MaxGroupAnnouncements      int64 = 20                // 群聊最大公告数
	MaxGroupAnnouncementLength int64 = 300               // 群公告最大内容长度
	MaxGroupAnnouncementImages int64 = 9                 // 群公告最大图片数
	MaxGroupFiles              int64 = 200               // 群文件最大数量
	MaxGroupFileSize           int64 = 100 * 1024 * 1024 // 群文件最大大小 100MB
	MaxGroupJoinRequests       int64 = 10                // 群加入申请最大数量
	MaxSubRoomMembers          int64 = 50                // 子房间最大成员数
	MaxSubRoomNameLength       int64 = 32                // 子房间名称最大长度
	MaxFriendRequests          int64 = 5
	MaxGuildJoinRequests       int64 = 10
	MaxGuildAnnouncements      int64 = 5
	MaxGuildPinnedMessages     int64 = 20
	MaxBlacklist               int64 = 500
	MaxFavorites               int64 = 1000
	MaxGuildFiles              int64 = 200               // 每个服务器最多文件数
	MaxGuildFileSize           int64 = 100 * 1024 * 1024 // 单个文件最大100MB

	// 分页与查询
	DefaultPageLimit int64 = 10
	MaxPageLimit     int64 = 100
	MaxSearchResults int64 = 100

	// 消息结构
	MaxMessageAttachments            int64 = 10
	MaxAttachmentNameLength          int64 = 128
	MaxAttachmentPerMessageTotalSize int64 = 20 * 1024 * 1024 // 20MB
	MaxMessageMentions               int64 = 20
	MaxMessageReplyDepth             int64 = 1

	// 服务器结构
	MaxGuildCategories    int64 = 20
	MaxGuildRoles         int64 = 50
	MaxRoleNameLength     int64 = 32
	MaxChannelNameLength  int64 = 64
	MaxCategoryNameLength int64 = 64

	// 媒体大小（字节）
	MaxImageSize       int64 = 30 * 1024 * 1024  // 30MB
	MaxFileSize        int64 = 100 * 1024 * 1024 // 100MB
	MaxAvatarSize      int64 = 20 * 1024 * 1024  // 20MB
	MaxGuildAvatarSize int64 = 20 * 1024 * 1024  // 20MB
	MaxGuildCoverSize  int64 = 20 * 1024 * 1024  // 20MB

	// 语音/实时
	MaxVoiceMessageLength          int64 = 60
	MaxVoiceParticipantsPerChannel int64 = 100
	MaxDmVoiceParticipants         int64 = 10

	// 论坛
	MaxForumPostTitleLength   int64 = 120
	MaxForumPostContentLength int64 = 5000
	MaxForumPostReplies       int64 = 500
	MaxForumReplyLength       int64 = 2000

	// 机器人平台
	MaxRobotsPerOwner    int64 = 20
	MaxBotUserNameLength int64 = 32 // BotUser 名称长度限制（社交属性存储在 User 表）
)
