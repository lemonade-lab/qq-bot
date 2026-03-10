package models

import (
	"time"

	"gorm.io/datatypes"
)

// SecurityEvent captures security/audit events (verification, reset, failures).
type SecurityEvent struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	UserID    *uint     `json:"userId,omitempty"` // may be nil if user not resolved yet
	Email     string    `gorm:"size:128" json:"email"`
	Type      string    `gorm:"size:64;index" json:"type"`
	IP        string    `gorm:"size:64" json:"ip"`
	UserAgent string    `gorm:"size:256" json:"userAgent"`
	Meta      string    `gorm:"type:text" json:"meta"`
	CreatedAt time.Time `json:"createdAt"`
}

// TrustedDevice 记录移动端“可信设备”。
// 设计目标：允许移动端在首次完成强认证（账号密码 + 邮箱验证码）后，绑定一个 deviceId。
// 后续登录可通过 (email + deviceId) 换取 Token（适用于移动端 Token 机制），同时支持设备管理（列出/撤销）。
//
// 安全建议：deviceId 应为客户端生成的高熵随机值（例如 UUIDv4 或更长随机串），不要使用可预测的硬件标识。
type TrustedDevice struct {
	ID                uint       `gorm:"primaryKey" json:"id"`
	UserID            uint       `gorm:"index:idx_user_device,unique,priority:1" json:"userId"`
	DeviceID          string     `gorm:"size:128;index:idx_user_device,unique,priority:2" json:"deviceId"`
	Name              string     `gorm:"size:128" json:"name"`
	Platform          string     `gorm:"size:16;default:'unknown'" json:"platform"` // web, ios, android, desktop
	Trusted           bool       `gorm:"default:true" json:"trusted"`
	DeviceTokenHash   string     `gorm:"size:128" json:"-"` // 设备令牌哈希（用于免密/免验证码登录）
	DeviceTokenExpire *time.Time `json:"-"`                 // 设备令牌过期时间
	LastIP            string     `gorm:"size:64" json:"-"`
	UserAgent         string     `gorm:"size:256" json:"-"`
	LastSeen          *time.Time `json:"lastSeen,omitempty"`
	LastLoginAt       *time.Time `json:"lastLoginAt,omitempty"` // 最后通过此设备登录的时间
	CreatedAt         time.Time  `json:"createdAt"`
	RevokedAt         *time.Time `gorm:"index" json:"revokedAt,omitempty"`
}

type Message struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	ChannelID uint           `gorm:"index:idx_channel_id_id,priority:1" json:"channelId"`
	AuthorID  uint           `gorm:"index" json:"authorId"`
	Author    string         `gorm:"size:64" json:"author"`
	Content   string         `gorm:"type:text" json:"content"`
	Type      string         `gorm:"size:32;default:'text'" json:"type"`
	Platform  string         `gorm:"size:16;default:'web'" json:"platform"` // 消息来源设备: web, mobile, desktop
	FileMeta  datatypes.JSON `gorm:"type:json" json:"fileMeta,omitempty"`
	Mentions  datatypes.JSON `gorm:"type:json" json:"mentions,omitempty"`           // 提及列表: [{type, id, name?, avatar?}]
	ReplyToID *uint          `gorm:"index" json:"replyToId,omitempty"`              // 回复的消息ID
	ReplyTo   *Message       `gorm:"foreignKey:ReplyToID" json:"replyTo,omitempty"` // 关联的被回复消息
	TempID    string         `gorm:"size:128" json:"tempId,omitempty"`              // 临时消息ID，用于匹配WebSocket消息
	EditedAt  *time.Time     `json:"editedAt,omitempty"`                            // 编辑时间，nil表示未编辑
	CreatedAt time.Time      `json:"createdAt"`
	DeletedAt *time.Time     `gorm:"index" json:"deletedAt,omitempty"` // 撤回时间，nil表示未撤回

	// 非数据库字段，API 返回时填充
	Reactions []AggregatedReaction `gorm:"-" json:"reactions,omitempty"` // 聚合后的表态
}

// Friendship represents friend relationship or pending request.
type Friendship struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	FromUserID      uint      `gorm:"index" json:"fromUserId"`
	ToUserID        uint      `gorm:"index" json:"toUserId"`
	Status          string    `gorm:"size:16;index" json:"status"`                     // pending | accepted
	NicknameFrom    string    `gorm:"size:64" json:"nicknameFrom"`                     // FromUser为ToUser设置的备注
	NicknameTo      string    `gorm:"size:64" json:"nicknameTo"`                       // ToUser为FromUser设置的备注
	PrivacyModeFrom string    `gorm:"size:16;default:'normal'" json:"privacyModeFrom"` // FromUser对ToUser的隐私模式: normal | chat_only
	PrivacyModeTo   string    `gorm:"size:16;default:'normal'" json:"privacyModeTo"`   // ToUser对FromUser的隐私模式: normal | chat_only
	CreatedAt       time.Time `json:"createdAt"`
}

// Greeting 记录打招呼状态，每个用户对另一个用户只能打一次招呼
type Greeting struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	FromUserID uint      `gorm:"index:idx_greeting_pair,unique" json:"fromUserId"`
	ToUserID   uint      `gorm:"index:idx_greeting_pair,unique" json:"toUserId"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Blacklist 黑名单
type Blacklist struct {
	UserID    uint      `gorm:"primaryKey;autoIncrement:false;index:idx_user_blocked,unique" json:"userId"`    // 拉黑的用户
	BlockedID uint      `gorm:"primaryKey;autoIncrement:false;index:idx_user_blocked,unique" json:"blockedId"` // 被拉黑的用户
	CreatedAt time.Time `json:"createdAt"`
	// Extended field for API response
	BlockedUser *User `gorm:"-" json:"blockedUser,omitempty"`
}

// AutoMigrate helper: include new models in migrations via application code

// Announcement 服务器公告
type Announcement struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	GuildID   uint           `gorm:"index" json:"guildId"`
	AuthorID  uint           `gorm:"index" json:"authorId"`
	Author    *User          `gorm:"foreignKey:AuthorID" json:"author,omitempty"` // 公告作者
	Title     string         `gorm:"size:256" json:"title"`
	Content   string         `gorm:"type:text" json:"content"`
	Images    datatypes.JSON `gorm:"type:json" json:"images,omitempty"`   // 图片列表(最多9张): [{"path":"...","url":"..."}]
	IsPinned  bool           `gorm:"default:false;index" json:"isPinned"` // 是否置顶
	PinnedAt  *time.Time     `json:"pinnedAt,omitempty"`                  // 置顶时间
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt *time.Time     `json:"updatedAt,omitempty"` // 最后编辑时间
}

// PinnedMessage 精华消息（PIN消息）
type PinnedMessage struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	MessageID      uint      `gorm:"index:idx_pinned_message,unique" json:"messageId"`
	ChannelID      *uint     `gorm:"index" json:"channelId,omitempty"`      // 频道消息时使用
	ThreadID       *uint     `gorm:"index" json:"threadId,omitempty"`       // 私聊消息时使用
	GuildID        *uint     `gorm:"index" json:"guildId,omitempty"`        // 频道消息时使用
	GroupThreadID  *uint     `gorm:"index" json:"groupThreadId,omitempty"`  // 群聊消息时使用
	GroupMessageID *uint     `gorm:"index" json:"groupMessageId,omitempty"` // 群聊消息ID
	PinnedBy       uint      `gorm:"index" json:"pinnedBy"`                 // 谁PIN的
	CreatedAt      time.Time `json:"createdAt"`
}

// Session represents user session stored in Redis - the "ultimate authority" for authentication.
// Sessions are long-lived (30 days) while tokens are short-lived (15 minutes).
// Note: This model is stored in Redis, not in SQL database.
type Session struct {
	ID                uint       `json:"id"`
	UserID            uint       `json:"userId"`
	SessionToken      string     `json:"-"` // secure random token (not exposed in JSON)
	IP                string     `json:"ip"`
	UserAgent         string     `json:"userAgent"`
	DeviceFingerprint string     `json:"deviceFingerprint,omitempty"` // browser/device fingerprint for enhanced security
	ExpiresAt         time.Time  `json:"expiresAt"`
	LastUsedAt        time.Time  `json:"lastUsedAt"`
	CreatedAt         time.Time  `json:"createdAt"`
	RevokedAt         *time.Time `json:"revokedAt,omitempty"` // for manual logout
}

// GuildJoinRequest represents an application by a user to join a guild.
type GuildJoinRequest struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	GuildID   uint       `gorm:"index" json:"guildId"`
	UserID    uint       `gorm:"index" json:"userId"`
	Note      string     `gorm:"type:text" json:"note,omitempty"`
	Status    string     `gorm:"size:16;default:'pending'" json:"status"` // pending|approved|rejected
	HandledBy *uint      `json:"handledBy,omitempty"`
	HandledAt *time.Time `json:"handledAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	User      *User      `gorm:"-" json:"user,omitempty"` // 用户信息，不存储在数据库中
}

// FavoriteMessage 收藏的消息
type FavoriteMessage struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	UserID         uint      `gorm:"index:idx_user_favorite,unique" json:"userId"`                   // 收藏者
	MessageID      *uint     `gorm:"index:idx_user_favorite,unique" json:"messageId,omitempty"`      // 频道消息ID
	DmMessageID    *uint     `gorm:"index:idx_user_favorite,unique" json:"dmMessageId,omitempty"`    // 私聊消息ID
	GroupMessageID *uint     `gorm:"index:idx_user_favorite,unique" json:"groupMessageId,omitempty"` // 群聊消息ID
	MessageType    string    `gorm:"size:16" json:"messageType"`                                     // "channel", "dm", or "group"
	CreatedAt      time.Time `json:"createdAt"`
	// 扩展字段（不存储在数据库，仅用于API响应）
	Message      *Message      `gorm:"-" json:"message,omitempty"`      // 频道消息内容
	DmMessage    *DmMessage    `gorm:"-" json:"dmMessage,omitempty"`    // 私聊消息内容
	GroupMessage *GroupMessage `gorm:"-" json:"groupMessage,omitempty"` // 群聊消息内容
}

// LiveKitRoom LiveKit 房间映射表
type LiveKitRoom struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	ChannelID       uint       `gorm:"uniqueIndex" json:"channelId"`
	RoomName        string     `gorm:"size:128;uniqueIndex" json:"roomName"`
	RoomSID         string     `gorm:"size:128" json:"roomSid,omitempty"`
	IsActive        bool       `gorm:"default:true" json:"isActive"`
	MaxParticipants int        `gorm:"default:50" json:"maxParticipants"`
	CreatedAt       time.Time  `json:"createdAt"`
	ClosedAt        *time.Time `json:"closedAt,omitempty"`
}

// LiveKitParticipant LiveKit 参与者记录表
type LiveKitParticipant struct {
	ID                  uint       `gorm:"primaryKey" json:"id"`
	RoomID              uint       `gorm:"index:idx_room_active,priority:1;index:idx_room_user;index:idx_unique_active,priority:1" json:"roomId"` // 复合索引优化查询
	UserID              uint       `gorm:"index:idx_room_user;index:idx_user_history" json:"userId"`
	ParticipantSID      string     `gorm:"size:128;uniqueIndex" json:"participantSid,omitempty"`                                   // 唯一索引：SID 全局唯一
	ParticipantIdentity string     `gorm:"size:128;index:idx_unique_active,priority:2;index" json:"participantIdentity,omitempty"` // 添加到唯一复合索引
	JoinedAt            time.Time  `gorm:"index:idx_user_history" json:"joinedAt"`                                                 // 用于时间范围查询
	LeftAt              *time.Time `gorm:"index:idx_room_active,priority:2" json:"leftAt,omitempty"`                               // 复合索引的第二部分
	DurationSeconds     int        `gorm:"default:0" json:"durationSeconds"`
}

// 索引说明:
// idx_room_active: (room_id, left_at) - 快速查询房间活跃参与者
// idx_room_user: (room_id, user_id) - 快速查询特定用户在房间的记录
// idx_user_history: (user_id, joined_at) - 用户参与历史查询
// idx_unique_active: (room_id, participant_identity) - 确保同一房间中身份唯一（需要配合应用逻辑处理 left_at）
// participant_sid: 唯一索引 - LiveKit 的 SID 全局唯一

// ForumPost 论坛帖子
type ForumPost struct {
	ID         uint           `gorm:"primaryKey" json:"id"`
	ChannelID  uint           `gorm:"index:idx_channel_posts" json:"channelId"`    // 所属频道
	AuthorID   uint           `gorm:"index" json:"authorId"`                       // 作者ID
	Title      string         `gorm:"size:256;not null" json:"title"`              // 帖子标题
	Content    string         `gorm:"type:text;not null" json:"content"`           // 帖子内容（Markdown）
	Media      datatypes.JSON `gorm:"type:json" json:"media,omitempty"`            // 媒体附件 [{type,url}]
	IsPinned   bool           `gorm:"default:false;index" json:"isPinned"`         // 是否置顶
	IsLocked   bool           `gorm:"default:false" json:"isLocked"`               // 是否锁定（禁止回复）
	ViewCount  int            `gorm:"default:0" json:"viewCount"`                  // 浏览次数
	ReplyCount int            `gorm:"default:0" json:"replyCount"`                 // 回复数量
	CreatedAt  time.Time      `gorm:"index:idx_channel_posts" json:"createdAt"`    // 创建时间
	UpdatedAt  time.Time      `json:"updatedAt"`                                   // 更新时间
	DeletedAt  *time.Time     `gorm:"index" json:"deletedAt,omitempty"`            // 软删除
	Author     *User          `gorm:"foreignKey:AuthorID" json:"author,omitempty"` // 作者信息
	// 扩展字段（不存储在数据库，仅用于API响应）
	LikeCount int  `gorm:"-" json:"likeCount"` // 点赞数量
	IsLiked   bool `gorm:"-" json:"isLiked"`   // 浏览者点赞标记
}

// ForumReply 论坛回复
type ForumReply struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	PostID    uint           `gorm:"index:idx_post_replies" json:"postId"`        // 所属帖子
	AuthorID  uint           `gorm:"index" json:"authorId"`                       // 回复者ID
	Content   string         `gorm:"type:text;not null" json:"content"`           // 回复内容（Markdown）
	Type      string         `gorm:"size:20;default:'text'" json:"type"`          // 回复类型：text / voice / image
	FileMeta  datatypes.JSON `gorm:"type:json" json:"fileMeta,omitempty"`         // 语音/图片文件元数据
	ReplyToID *uint          `gorm:"index" json:"replyToId,omitempty"`            // 回复的回复ID（支持嵌套回复）
	CreatedAt time.Time      `gorm:"index:idx_post_replies" json:"createdAt"`     // 创建时间
	UpdatedAt time.Time      `json:"updatedAt"`                                   // 更新时间
	DeletedAt *time.Time     `gorm:"index" json:"deletedAt,omitempty"`            // 软删除
	Author    *User          `gorm:"foreignKey:AuthorID" json:"author,omitempty"` // 回复者信息
}

// 索引说明:
// idx_like_count: (post_id, is_liked) - 快速查询帖子点赞总数

// ForumPostLike 论坛帖子点赞
type ForumPostLike struct {
	PostID         uint `gorm:"primaryKey;index:idx_like_count" json:"postId"`
	OperatorUserID uint `gorm:"primaryKey" json:"operatorUserId"`
	IsLiked        bool `gorm:"default:false;index:idx_like_count;not null" json:"isLiked"`
}

// MessageReaction 消息表态（emoji 反应）
type MessageReaction struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	MessageID uint      `gorm:"index:idx_reaction_msg_user,unique,priority:1" json:"messageId"`
	UserID    uint      `gorm:"index:idx_reaction_msg_user,unique,priority:2" json:"userId"`
	Emoji     string    `gorm:"size:64;index:idx_reaction_msg_user,unique,priority:3" json:"emoji"` // emoji 字符，如 👍 🎉
	CreatedAt time.Time `json:"createdAt"`
}

// AggregatedReaction 聚合后的表态信息（用于 API 返回）
type AggregatedReaction struct {
	Emoji string `json:"emoji"`
	Count int    `json:"count"`
	Users []uint `json:"users"`
}

// GuildFile 服务器文件系统
type GuildFile struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	GuildID     uint      `gorm:"index" json:"guildId"`
	UploaderID  uint      `gorm:"index" json:"uploaderId"`
	Uploader    *User     `gorm:"foreignKey:UploaderID" json:"uploader,omitempty"`
	FileName    string    `gorm:"size:256" json:"fileName"`
	FilePath    string    `gorm:"size:512" json:"filePath"`    // MinIO object name
	FileURL     string    `gorm:"-" json:"fileUrl,omitempty"`  // 运行时填充的完整URL
	FileSize    int64     `json:"fileSize"`                    // 文件大小（字节）
	ContentType string    `gorm:"size:128" json:"contentType"` // MIME类型
	CreatedAt   time.Time `json:"createdAt"`
}
