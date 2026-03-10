package models

import "time"

type GuildMember struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	GuildID      uint       `gorm:"index" json:"guildId"`
	UserID       uint       `gorm:"index" json:"userId"`
	SortOrder    int        `gorm:"default:0" json:"sortOrder"`            // 该用户的服务器排序顺序
	MutedUntil   *time.Time `json:"mutedUntil,omitempty"`                  // 禁言到期时间，null表示未禁言
	TempNickname string     `gorm:"size:64" json:"tempNickname,omitempty"` // 服务器内临时昵称
	NotifyMuted  bool       `gorm:"default:false" json:"notifyMuted"`      // 用户自主免打扰
	CreatedAt    time.Time  `json:"createdAt"`                             // 加入服务器时间
	User         *User      `gorm:"-" json:"user,omitempty"`               // 用户信息，不存储在数据库中
	Roles        []Role     `gorm:"-" json:"roles,omitempty"`              // 角色列表，不存储在数据库中
	IsMuted      bool       `gorm:"-" json:"isMuted,omitempty"`            // 是否被禁言（计算字段）
}
