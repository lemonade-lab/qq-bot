package models

import (
	"time"

	"gorm.io/datatypes"
)

// GroupThread 群聊线程（多人线程）
type GroupThread struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	Name          string     `gorm:"size:32" json:"name"`                           // 群名称
	Avatar        string     `gorm:"size:512" json:"avatar,omitempty"`              // 群头像
	Banner        string     `gorm:"size:512" json:"banner,omitempty"`              // 群横幅
	OwnerID       uint       `gorm:"index" json:"ownerId"`                          // 群主用户ID
	JoinMode      string     `gorm:"size:32;default:'invite_only'" json:"joinMode"` // 加入方式: invite_only(仅邀请) | free_join(自由加入) | need_approval(需要审批)
	LastMessageAt *time.Time `gorm:"index" json:"lastMessageAt,omitempty"`          // 最后一条消息时间
	CreatedAt     time.Time  `json:"createdAt"`
	// 扩展字段（不存储在数据库，仅用于API响应）
	Members     []GroupThreadMember `gorm:"-" json:"members,omitempty"`     // 成员列表
	MemberCount int                 `gorm:"-" json:"memberCount,omitempty"` // 成员数
	LastMessage *GroupMessage       `gorm:"-" json:"lastMessage,omitempty"` // 最新消息
	IsMuted     bool                `gorm:"-" json:"isMuted,omitempty"`     // 当前用户是否开启免打扰
	IsHidden    bool                `gorm:"-" json:"isHidden,omitempty"`    // 当前用户是否隐藏此会话
	IsPinned    bool                `gorm:"-" json:"isPinned,omitempty"`    // 当前用户是否置顶此会话
}

// GroupJoinRequest 群聊加入申请
type GroupJoinRequest struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	ThreadID  uint       `gorm:"index" json:"threadId"`                   // 群聊ID
	UserID    uint       `gorm:"index" json:"userId"`                     // 申请人ID
	Note      string     `gorm:"type:text" json:"note,omitempty"`         // 申请备注
	Status    string     `gorm:"size:16;default:'pending'" json:"status"` // pending | approved | rejected
	HandledBy *uint      `json:"handledBy,omitempty"`                     // 处理人ID
	HandledAt *time.Time `json:"handledAt,omitempty"`                     // 处理时间
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	User      *User      `gorm:"-" json:"user,omitempty"` // 申请人信息
}

// GroupAnnouncement 群公告
type GroupAnnouncement struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	ThreadID  uint           `gorm:"index" json:"threadId"` // 群聊ID
	AuthorID  uint           `gorm:"index" json:"authorId"` // 发布者ID
	Author    *User          `gorm:"foreignKey:AuthorID" json:"author,omitempty"`
	Title     string         `gorm:"size:256" json:"title"`             // 公告标题
	Content   string         `gorm:"type:text" json:"content"`          // 公告内容
	Images    datatypes.JSON `gorm:"type:json" json:"images,omitempty"` // 图片列表
	IsPinned  bool           `gorm:"default:false;index" json:"isPinned"`
	PinnedAt  *time.Time     `json:"pinnedAt,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt *time.Time     `json:"updatedAt,omitempty"`
}

// GroupFile 群文件
type GroupFile struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	ThreadID    uint      `gorm:"index" json:"threadId"`   // 群聊ID
	UploaderID  uint      `gorm:"index" json:"uploaderId"` // 上传者ID
	Uploader    *User     `gorm:"foreignKey:UploaderID" json:"uploader,omitempty"`
	FileName    string    `gorm:"size:256" json:"fileName"`    // 文件名
	FilePath    string    `gorm:"size:512" json:"filePath"`    // MinIO 路径
	FileURL     string    `gorm:"-" json:"fileUrl,omitempty"`  // 文件访问URL
	FileSize    int64     `json:"fileSize"`                    // 文件大小
	ContentType string    `gorm:"size:128" json:"contentType"` // MIME类型
	CreatedAt   time.Time `json:"createdAt"`
}

// GroupThreadMember 群聊成员
type GroupThreadMember struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ThreadID  uint      `gorm:"index:idx_group_member,unique,priority:1" json:"threadId"` // 群聊ID
	UserID    uint      `gorm:"index:idx_group_member,unique,priority:2;index" json:"userId"`
	Role      string    `gorm:"size:16;default:'member'" json:"role"` // owner | admin | member
	Nickname  string    `gorm:"size:32" json:"nickname,omitempty"`    // 群内昵称
	IsMuted   bool      `gorm:"default:false" json:"isMuted"`         // 是否免打扰
	Hidden    bool      `gorm:"default:false" json:"hidden"`          // 是否隐藏会话
	Pinned    bool      `gorm:"default:false" json:"pinned"`          // 是否置顶会话
	CreatedAt time.Time `json:"createdAt"`
	// 扩展字段
	User *User `gorm:"-" json:"user,omitempty"` // 用户信息
}

// GroupMessage 群聊消息
type GroupMessage struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	ThreadID  uint           `gorm:"index:idx_group_tid_id,priority:1" json:"threadId"`
	AuthorID  uint           `gorm:"index" json:"authorId"`
	Content   string         `gorm:"type:text" json:"content"`
	Type      string         `gorm:"size:32;default:'text'" json:"type"`
	Platform  string         `gorm:"size:16;default:'web'" json:"platform"` // web, mobile, desktop
	FileMeta  datatypes.JSON `gorm:"type:json" json:"fileMeta,omitempty"`
	Mentions  datatypes.JSON `gorm:"type:json" json:"mentions,omitempty"` // 提及列表
	ReplyToID *uint          `gorm:"index" json:"replyToId,omitempty"`    // 回复消息ID
	ReplyTo   *GroupMessage  `gorm:"foreignKey:ReplyToID" json:"replyTo,omitempty"`
	TempID    string         `gorm:"size:128" json:"tempId,omitempty"`
	EditedAt  *time.Time     `json:"editedAt,omitempty"` // 编辑时间
	CreatedAt time.Time      `json:"createdAt"`
	DeletedAt *time.Time     `gorm:"index" json:"deletedAt,omitempty"` // 撤回

	// 非数据库字段，API 返回时填充
	Reactions []AggregatedReaction `gorm:"-" json:"reactions,omitempty"` // 聚合后的表态
}
