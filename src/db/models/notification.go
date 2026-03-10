package models

import "time"

// UserNotification 个人级别通知（例如被提及、好友申请、公会加入申请等）
type UserNotification struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	UserID     uint      `gorm:"index" json:"userId"`
	Type       string    `gorm:"size:32;index" json:"type"`        // e.g., "mention", "friend_request", "guild_join_request"
	SourceType string    `gorm:"size:16" json:"sourceType"`        // "channel" | "dm" | "friend" | "guild"
	GuildID    *uint     `gorm:"index" json:"guildId,omitempty"`   // 所属公会（频道消息时有值，公会申请时有值）
	ChannelID  *uint     `gorm:"index" json:"channelId,omitempty"` // 所属频道
	ThreadID   *uint     `json:"threadId,omitempty"`               // 私聊线程
	MessageID  *uint     `json:"messageId,omitempty"`              // 消息ID
	AuthorID   *uint     `gorm:"index" json:"authorId,omitempty"`  // 提及者ID/好友申请发起人ID/公会申请人ID
	Status     *string   `gorm:"size:20" json:"status,omitempty"`  // 用于申请类通知：pending, accepted, rejected
	Read       bool      `gorm:"index" json:"read"`
	CreatedAt  time.Time `json:"createdAt"`
}
