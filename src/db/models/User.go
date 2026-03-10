package models

import (
	"time"
)

type User struct {
	ID                      uint       `gorm:"primaryKey" json:"id"`
	Email                   string     `gorm:"size:128;uniqueIndex" json:"email"`
	Name                    string     `gorm:"size:64;index" json:"name"`
	Token                   string     `gorm:"size:128;uniqueIndex" json:"token"`
	PasswordHash            string     `gorm:"size:100" json:"-"`
	EmailVerified           bool       `gorm:"default:false" json:"emailVerified"`
	EmailVerifyToken        string     `gorm:"size:128" json:"-"` // legacy plaintext (kept for backward compatibility, now unused)
	ResetToken              string     `gorm:"size:128" json:"-"` // legacy plaintext (unused)
	ResetTokenExpires       *time.Time `json:"-"`
	EmailVerifyTokenHash    string     `gorm:"size:128" json:"-"`
	ResetTokenHash          string     `gorm:"size:128" json:"-"`
	EmailVerifyRequestedAt  *time.Time `json:"-"`
	EmailVerifyRequestCount int        `gorm:"default:0" json:"-"`
	ResetRequestedAt        *time.Time `json:"-"`
	ResetRequestCount       int        `gorm:"default:0" json:"-"`
	// 登录二次验证字段（邮箱登录验证码）
	LoginVerifyCodeHash    string     `gorm:"size:128" json:"-"` // 登录验证码哈希
	LoginVerifyCodeSentAt  *time.Time `json:"-"`                 // 登录验证码发送时间
	LoginVerifyCodeExpires *time.Time `json:"-"`                 // 登录验证码过期时间
	// 登录安全相关字段
	LoginFailedCount      int        `gorm:"default:0" json:"-"`    // 连续登录失败次数
	LoginFailedAt         *time.Time `json:"-"`                     // 最后一次登录失败时间
	AccountLockedUntil    *time.Time `json:"-"`                     // 账户锁定到期时间
	LastLoginAt           *time.Time `json:"lastLoginAt,omitempty"` // 最后一次成功登录时间
	LastLoginIP           string     `gorm:"size:64" json:"-"`      // 最后一次登录IP
	Status                string     `gorm:"size:16;default:'offline'" json:"status"`
	CustomStatus          string     `gorm:"size:128" json:"customStatus,omitempty"`                   // 自定义状态文字(如 "正在学习Go")
	IsPrivate             bool       `gorm:"default:false" json:"isPrivate"`                           // 私密账号，默认公开
	IsBot                 bool       `gorm:"default:false" json:"isBot,omitempty"`                     // 机器人账号标记
	Avatar                string     `gorm:"size:512" json:"avatar,omitempty"`                         // 头像文件路径(MinIO object name)
	Banner                string     `gorm:"size:512" json:"banner,omitempty"`                         // 横幅背景图片路径(MinIO object name)
	BannerColor           string     `gorm:"size:16" json:"bannerColor,omitempty"`                     // 横幅背景颜色(如 #5865F2)
	DisplayName           string     `gorm:"size:32" json:"displayName,omitempty"`                     // 展示名称（区别于用户名 Name）
	Bio                   string     `gorm:"size:256" json:"bio,omitempty"`                            // 个人简介
	Gender                string     `gorm:"size:16" json:"gender,omitempty"`                          // 性别: male, female, other
	Birthday              string     `gorm:"size:10" json:"birthday,omitempty"`                        // 生日: YYYY-MM-DD 格式
	Pronouns              string     `gorm:"size:32" json:"pronouns,omitempty"`                        // 称谓代词: he/him, she/her, they/them, ask, custom
	Region                string     `gorm:"size:128" json:"region,omitempty"`                         // 地区
	Phone                 *string    `gorm:"size:32;uniqueIndex" json:"phone,omitempty"`               // 手机号（唯一索引，支持手机号注册登录, *string 使空值存为 NULL）
	PendingPhone          *string    `gorm:"size:32" json:"-"`                                         // 待绑定手机号（验证码确认前的临时存储）
	PhoneVerified         bool       `gorm:"default:false" json:"phoneVerified"`                       // 手机号是否已验证
	PhoneVerifyCodeSentAt *time.Time `json:"-"`                                                        // 手机验证码发送时间（用于频率限制）
	Link                  string     `gorm:"size:256" json:"link,omitempty"`                           // 个人链接(个人网站/社交媒体)
	RequireFriendApproval bool       `gorm:"default:false" json:"requireFriendApproval"`               // 加好友是否需要验证（已废弃，改用FriendRequestMode）
	FriendRequestMode     string     `gorm:"size:32;default:'need_approval'" json:"friendRequestMode"` // 加好友验证模式: need_approval(需要验证) | everyone(允许任何人) | need_question(回答问题后验证)
	FriendVerifyQuestion  string     `gorm:"size:256" json:"friendVerifyQuestion,omitempty"`           // 验证问题（当FriendRequestMode为need_question时使用）
	FriendVerifyAnswer    string     `gorm:"size:256" json:"-"`                                        // 验证问题答案（不对外暴露）
	DmPrivacyMode         string     `gorm:"size:32;default:'friends_only'" json:"dmPrivacyMode"`      // 私聊隐私模式: friends_only(仅好友) | everyone(所有人)
	CreatedAt             time.Time  `json:"createdAt"`
	// 扩展字段（不存储在数据库，仅用于API响应）
	Nickname string `gorm:"-" json:"nickname,omitempty"` // 当前用户为该好友设置的备注
	Roles    []Role `gorm:"-" json:"roles,omitempty"`    // 用户在当前上下文（公会）中的角色列表
}
