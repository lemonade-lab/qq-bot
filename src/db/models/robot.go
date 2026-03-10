package models

import "time"

// Robot represents a bot created by a developer user.
// OwnerID: 用户(开发者)ID
// BotUserID: 机器人在 users 表中的用户记录 ID(机器人作为一等公民)
//
// 设计原则：Robot 模型只保存开发者相关属性（Token、Webhook、隐私、所有权等）。
// 所有社交属性（名称、头像、横幅、简介等）都存储在关联的 BotUser（User 模型）上。
type Robot struct {
	ID         uint      `json:"id" gorm:"primaryKey"`
	OwnerID    uint      `json:"ownerId" gorm:"index"`                 // 开发者用户ID
	BotUserID  uint      `json:"botUserId" gorm:"index"`               // 机器人对应的用户记录ID
	Token      string    `json:"token" gorm:"size:128;uniqueIndex"`    // 机器人 API Token
	WebhookURL string    `json:"webhookUrl,omitempty" gorm:"size:512"` // Webhook 回调地址
	IsPrivate  bool      `json:"isPrivate" gorm:"default:true"`        // 私密机器人，默认私密，仅对公开查询隐藏
	Category   string    `json:"category" gorm:"size:64;default:其他"`   // 机器人分类，默认"其他"
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
	BotUser    *User     `gorm:"foreignKey:BotUserID" json:"botUser,omitempty"` // 关联的用户记录（承载所有社交属性）
	GuildCount int       `gorm:"-" json:"guildCount,omitempty"`                 // 所在服务器数量（仅查询时填充，不入库）
}

// BotName 返回机器人的展示名称，从关联的 BotUser 获取。
// 如果 BotUser 未加载则返回空字符串。
func (r *Robot) BotName() string {
	if r.BotUser != nil {
		return r.BotUser.Name
	}
	return ""
}
