package models

import "time"

// ReadState 用户阅读状态（红点系统核心）
// 记录用户在各个资源（频道/DM/服务器）的阅读进度和未读统计
type ReadState struct {
	ID                uint       `gorm:"primaryKey" json:"id"`
	UserID            uint       `gorm:"index:idx_user_resource,unique,priority:1" json:"userId"`
	ResourceType      string     `gorm:"size:16;index:idx_user_resource,unique,priority:2" json:"resourceType"` // "channel" | "dm" | "guild"
	ResourceID        uint       `gorm:"index:idx_user_resource,unique,priority:3" json:"resourceId"`           // 对应的频道ID/DM线程ID/服务器ID
	LastReadMessageID *uint      `json:"lastReadMessageId,omitempty"`                                           // 最后已读消息ID
	LastReadAt        *time.Time `json:"lastReadAt,omitempty"`                                                  // 最后阅读时间
	MentionCount      int        `gorm:"default:0" json:"mentionCount"`                                         // @我的未读数量
	UnreadCount       int        `gorm:"default:0" json:"unreadCount"`                                          // 总未读消息数
	UpdatedAt         time.Time  `json:"updatedAt"`
	CreatedAt         time.Time  `json:"createdAt"`
}
