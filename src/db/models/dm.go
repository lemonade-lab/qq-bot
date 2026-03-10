package models

import (
	"time"

	"gorm.io/datatypes"
)

// Direct Message (DM)
type DmThread struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	UserAID        uint       `gorm:"index:idx_dm_pair,unique" json:"userAId"`
	UserBID        uint       `gorm:"index:idx_dm_pair,unique" json:"userBId"`
	HiddenByUserA  bool       `gorm:"default:false" json:"hiddenByUserA"`    // UserA是否隐藏了此线程
	HiddenByUserB  bool       `gorm:"default:false" json:"hiddenByUserB"`    // UserB是否隐藏了此线程
	PinnedByUserA  bool       `gorm:"default:false" json:"pinnedByUserA"`    // UserA是否置顶了此线程
	PinnedByUserB  bool       `gorm:"default:false" json:"pinnedByUserB"`    // UserB是否置顶了此线程
	BlockedByUserA bool       `gorm:"default:false" json:"blockedByUserA"`   // UserA是否屏蔽了UserB的消息
	BlockedByUserB bool       `gorm:"default:false" json:"blockedByUserB"`   // UserB是否屏蔽了UserA的消息
	IsNotebook     bool       `gorm:"default:false;index" json:"isNotebook"` // 是否为笔记本线程（自聊，每用户唯一）
	DeviceType     string     `gorm:"size:16" json:"deviceType,omitempty"`   // [已废弃] 笔记本设备类型，保留以兼容旧数据
	DeviceID       string     `gorm:"size:64" json:"deviceId,omitempty"`     // [已废弃] 设备指纹/标识，保留以兼容旧数据
	LastMessageAt  *time.Time `gorm:"index" json:"lastMessageAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	// 扩展字段（不存储在数据库，仅用于API响应）
	PeerUser    *User            `gorm:"-" json:"peerUser,omitempty"`    // 对方用户信息
	LastMessage *DmMessage       `gorm:"-" json:"lastMessage,omitempty"` // 最新消息
	IsPinned    bool             `gorm:"-" json:"isPinned,omitempty"`    // 当前用户是否置顶了此线程
	IsBlocked   bool             `gorm:"-" json:"isBlocked,omitempty"`   // 当前用户是否屏蔽了对方的消息
	IsHidden    bool             `gorm:"-" json:"isHidden,omitempty"`    // 当前用户是否隐藏了此会话
	Mentions    []map[string]any `gorm:"-" json:"mentions,omitempty"`    // 线程级mentions占位（如取自最新消息）
}

type DmMessage struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	ThreadID  uint           `gorm:"index:idx_dm_tid_id,priority:1" json:"threadId"`
	AuthorID  uint           `gorm:"index" json:"authorId"`
	Content   string         `gorm:"type:text" json:"content"`
	Type      string         `gorm:"size:32;default:'text'" json:"type"`
	Platform  string         `gorm:"size:16;default:'web'" json:"platform"` // 消息来源设备: web, mobile, desktop
	FileMeta  datatypes.JSON `gorm:"type:json" json:"fileMeta,omitempty"`
	Mentions  datatypes.JSON `gorm:"type:json" json:"mentions,omitempty"`           // 提及列表: [{type, id}]
	ReplyToID *uint          `gorm:"index" json:"replyToId,omitempty"`              // 回复的消息ID
	ReplyTo   *DmMessage     `gorm:"foreignKey:ReplyToID" json:"replyTo,omitempty"` // 关联的被回复消息
	TempID    string         `gorm:"size:128" json:"tempId,omitempty"`              // 临时消息ID，用于匹配WebSocket消息
	EditedAt  *time.Time     `json:"editedAt,omitempty"`                            // 编辑时间，nil表示未编辑
	CreatedAt time.Time      `json:"createdAt"`
	DeletedAt *time.Time     `gorm:"index" json:"deletedAt,omitempty"` // 撤回时间，nil表示未撤回

	// 非数据库字段，API 返回时填充
	Reactions []AggregatedReaction `gorm:"-" json:"reactions,omitempty"` // 聚合后的表态
}
