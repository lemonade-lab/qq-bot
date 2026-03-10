package models

import (
	"time"

	"gorm.io/datatypes"
)

// SubRoom 频道子房间（频道内的多人线程）
// 挂在指定服务器的指定频道下，每个服务器成员在每个频道下仅可创建一个子房间
type SubRoom struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	GuildID       uint       `gorm:"index:idx_subroom_guild;index:idx_subroom_channel_owner,unique,priority:1" json:"guildId"`     // 所属服务器
	ChannelID     uint       `gorm:"index:idx_subroom_channel;index:idx_subroom_channel_owner,unique,priority:2" json:"channelId"` // 所属频道
	OwnerID       uint       `gorm:"index;index:idx_subroom_channel_owner,unique,priority:3" json:"ownerId"`                       // 房主（创建者）
	Name          string     `gorm:"size:32" json:"name"`                                                                          // 子房间名称
	Avatar        string     `gorm:"size:512" json:"avatar,omitempty"`                                                             // 子房间头像
	LastMessageAt *time.Time `gorm:"index" json:"lastMessageAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	// 扩展字段（不存储在数据库，仅用于API响应）
	Members     []SubRoomMember `gorm:"-" json:"members,omitempty"`
	MemberCount int             `gorm:"-" json:"memberCount,omitempty"`
	Owner       *User           `gorm:"-" json:"owner,omitempty"`    // 房主信息
	IsMember    bool            `gorm:"-" json:"isMember,omitempty"` // 当前用户是否为成员
	LastMessage *SubRoomMessage `gorm:"-" json:"lastMessage,omitempty"`
}

// SubRoomMember 子房间成员（仅房主可添加/移除）
type SubRoomMember struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	RoomID    uint      `gorm:"index:idx_subroom_member,unique,priority:1" json:"roomId"`
	UserID    uint      `gorm:"index:idx_subroom_member,unique,priority:2;index" json:"userId"`
	Role      string    `gorm:"size:16;default:'member'" json:"role"` // owner | member
	Hidden    bool      `gorm:"default:false" json:"hidden"`          // 是否隐藏会话
	CreatedAt time.Time `json:"createdAt"`
	// 扩展字段
	User *User `gorm:"-" json:"user,omitempty"`
}

// SubRoomMessage 子房间消息
type SubRoomMessage struct {
	ID        uint            `gorm:"primaryKey" json:"id"`
	RoomID    uint            `gorm:"index:idx_subroom_msg_rid,priority:1" json:"roomId"`
	AuthorID  uint            `gorm:"index" json:"authorId"`
	Content   string          `gorm:"type:text" json:"content"`
	Type      string          `gorm:"size:32;default:'text'" json:"type"`
	Platform  string          `gorm:"size:16;default:'web'" json:"platform"`
	FileMeta  datatypes.JSON  `gorm:"type:json" json:"fileMeta,omitempty"`
	Mentions  datatypes.JSON  `gorm:"type:json" json:"mentions,omitempty"`
	ReplyToID *uint           `gorm:"index" json:"replyToId,omitempty"`
	ReplyTo   *SubRoomMessage `gorm:"foreignKey:ReplyToID" json:"replyTo,omitempty"`
	TempID    string          `gorm:"size:128" json:"tempId,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
	DeletedAt *time.Time      `gorm:"index" json:"deletedAt,omitempty"`
}
