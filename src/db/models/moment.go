package models

import (
	"time"

	"gorm.io/datatypes"
)

// Moment 朋友圈动态
type Moment struct {
	ID         uint           `gorm:"primaryKey" json:"id"`
	UserID     uint           `gorm:"index" json:"userId"`
	Content    string         `gorm:"type:text" json:"content"`                // 文字内容
	Media      datatypes.JSON `gorm:"type:json" json:"media,omitempty"`        // 图片/视频数组 JSON
	Location   string         `gorm:"size:256" json:"location,omitempty"`      // 位置信息
	Visibility string         `gorm:"size:16;default:'all'" json:"visibility"` // all(所有人可见), friends(仅好友), private(仅自己)
	CreatedAt  time.Time      `json:"createdAt"`
	DeletedAt  *time.Time     `gorm:"index" json:"deletedAt,omitempty"`

	// 扩展字段（不存储在数据库，仅用于API响应）
	User          *User           `gorm:"-" json:"user,omitempty"`
	LikeCount     int             `gorm:"-" json:"likeCount"`
	CommentCount  int             `gorm:"-" json:"commentCount"`
	CommentsCount int             `gorm:"-" json:"commentsCount"` // 前端兼容别名
	IsLiked       bool            `gorm:"-" json:"isLiked"`       // 当前用户是否已点赞
	Likes         []MomentLike    `gorm:"-" json:"likes,omitempty"`
	Comments      []MomentComment `gorm:"-" json:"comments,omitempty"`
	AuthorId      uint            `gorm:"-" json:"authorId"`  // 前端兼容别名（映射 UserID）
	CreatedTs     int64           `gorm:"-" json:"createdTs"` // Unix 毫秒时间戳
}

// MomentLike 朋友圈点赞
type MomentLike struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	MomentID  uint      `gorm:"index:idx_moment_like,unique,priority:1" json:"momentId"`
	UserID    uint      `gorm:"index:idx_moment_like,unique,priority:2" json:"userId"`
	CreatedAt time.Time `json:"createdAt"`

	// 扩展字段
	User *User `gorm:"-" json:"user,omitempty"`
}

// MomentComment 朋友圈评论
type MomentComment struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	MomentID    uint       `gorm:"index" json:"momentId"`
	UserID      uint       `gorm:"index" json:"userId"`
	Content     string     `gorm:"type:text" json:"content"`
	ReplyToID   *uint      `gorm:"index" json:"replyToId,omitempty"` // 回复的评论ID
	ReplyToUser *uint      `json:"replyToUserId,omitempty"`          // 回复的用户ID
	CreatedAt   time.Time  `json:"createdAt"`
	DeletedAt   *time.Time `gorm:"index" json:"deletedAt,omitempty"`

	// 扩展字段
	User            *User `gorm:"-" json:"user,omitempty"`
	ReplyToUserInfo *User `gorm:"-" json:"replyToUser,omitempty"`
}
