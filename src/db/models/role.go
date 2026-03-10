package models

import "time"

// Role represents a guild role with a permission bitset.
type Role struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	GuildID     uint      `gorm:"index:idx_guild_role_name,unique" json:"guildId"`
	Name        string    `gorm:"size:64;index:idx_guild_role_name,unique" json:"name"`
	Color       string    `gorm:"size:16" json:"color,omitempty"` // 角色颜色(如 #5865F2)
	Permissions uint64    `gorm:"default:0" json:"permissions"`
	CreatedAt   time.Time `json:"createdAt"`
}

// MemberRole maps a user to a role within a guild.
type MemberRole struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	GuildID   uint      `gorm:"index:idx_member_role,unique" json:"guildId"`
	UserID    uint      `gorm:"index:idx_member_role,unique" json:"userId"`
	RoleID    uint      `gorm:"index:idx_member_role,unique" json:"roleId"`
	CreatedAt time.Time `json:"createdAt"`
}
