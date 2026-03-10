package models

import "time"

// UserApplication 个人级别申请（好友、公会加入等）
type UserApplication struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	Type          string    `gorm:"size:32;index" json:"type"` // "guild_join" | "friend" | ...
	FromUserID    uint      `gorm:"index" json:"fromUserId"`
	ToUserID      uint      `gorm:"index" json:"toUserId"`
	TargetGuildID *uint     `json:"targetGuildId,omitempty"`
	Status        string    `gorm:"size:16;index" json:"status"` // "pending" | "approved" | "rejected"
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`

	// 前端兼容字段（别名）
	UserID  uint  `gorm:"-" json:"userId"`  // 映射 FromUserID
	GuildID *uint `gorm:"-" json:"guildId"` // 映射 TargetGuildID
}
