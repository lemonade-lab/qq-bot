package models

import (
	"time"
)

type Guild struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	Name        string `gorm:"size:128;index" json:"name"`
	Description string `gorm:"size:512" json:"description"` // 服务器简介
	OwnerID     uint   `json:"ownerId"`
	IsPrivate   bool   `gorm:"default:true" json:"isPrivate"` // 私密服务器，默认私密，无法被搜索
	// AutoJoinMode 控制用户通过 "点加" 是否能直接加入服务器。
	// 值域: require_approval | no_approval | no_approval_under_100
	// 默认: require_approval
	AutoJoinMode      string     `gorm:"size:32;default:'require_approval'" json:"autoJoinMode"`
	Category          string     `gorm:"size:32;index;default:'other'" json:"category"` // 服务器分类: gaming, work, dev, study, entertainment, other
	Level             int        `gorm:"default:0" json:"level"`                        // 服务器等级，默认0级，影响成员人数限制
	Avatar            string     `gorm:"size:512" json:"avatar,omitempty"`              // 服务器头像文件路径(MinIO object name)
	Banner            string     `gorm:"size:512" json:"banner,omitempty"`              // 服务器横幅文件路径(MinIO object name)
	AllowMemberUpload bool       `gorm:"default:false" json:"allowMemberUpload"`        // 是否允许普通成员上传文件（默认仅管理员）
	ShowRoleNames     bool       `gorm:"default:true" json:"showRoleNames"`             // 是否在消息和成员列表中显示角色名牌
	CreatedAt         time.Time  `json:"createdAt"`
	DeletedAt         *time.Time `gorm:"index" json:"deletedAt,omitempty"`
}

// GuildCategory 服务器分类常量
const (
	GuildCategoryGaming        = "gaming"        // 游戏
	GuildCategoryWork          = "work"          // 办公
	GuildCategoryDev           = "dev"           // 开发
	GuildCategoryStudy         = "study"         // 学习
	GuildCategoryEntertainment = "entertainment" // 娱乐
	GuildCategoryOther         = "other"         // 其他
)

// GuildCategoryInfo 分类信息
type GuildCategoryInfo struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// GetAllGuildCategories 获取所有服务器分类
func GetAllGuildCategories() []GuildCategoryInfo {
	return []GuildCategoryInfo{
		{Value: GuildCategoryGaming, Label: "游戏", Description: "游戏相关的服务器"},
		{Value: GuildCategoryWork, Label: "办公", Description: "工作、办公协作相关"},
		{Value: GuildCategoryDev, Label: "开发", Description: "编程、开发技术交流"},
		{Value: GuildCategoryStudy, Label: "学习", Description: "学习、教育相关"},
		{Value: GuildCategoryEntertainment, Label: "娱乐", Description: "娱乐、兴趣爱好"},
		{Value: GuildCategoryOther, Label: "其他", Description: "其他类型"},
	}
}

// IsValidGuildCategory 验证分类是否有效
func IsValidGuildCategory(category string) bool {
	switch category {
	case GuildCategoryGaming, GuildCategoryWork, GuildCategoryDev,
		GuildCategoryStudy, GuildCategoryEntertainment, GuildCategoryOther:
		return true
	default:
		return false
	}
}

// GuildLevel 服务器等级相关
const (
	GuildLevelDefault = 0 // 默认等级
)

// GuildLevelInfo 等级信息
type GuildLevelInfo struct {
	Level       int    `json:"level"`
	Name        string `json:"name"`
	MemberLimit int    `json:"memberLimit"`
	Description string `json:"description"`
}

// GetGuildLevelInfo 获取等级信息
func GetGuildLevelInfo(level int) GuildLevelInfo {
	switch level {
	case 0:
		return GuildLevelInfo{
			Level:       0,
			Name:        "普通服务器",
			MemberLimit: 2000,
			Description: "基础服务器，支持2000人",
		}
	// 预留更高等级，后续扩展
	default:
		// 未定义的等级默认使用0级配置
		return GuildLevelInfo{
			Level:       level,
			Name:        "普通服务器",
			MemberLimit: 2000,
			Description: "基础服务器，支持2000人",
		}
	}
}

// GetAllGuildLevels 获取所有等级信息（当前只有0级）
func GetAllGuildLevels() []GuildLevelInfo {
	return []GuildLevelInfo{
		GetGuildLevelInfo(0),
		// 后续可扩展更多等级
	}
}

// GetMemberLimitByLevel 根据等级获取成员人数限制
func GetMemberLimitByLevel(level int) int {
	return GetGuildLevelInfo(level).MemberLimit
}

type ChannelCategory struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	GuildID   uint       `gorm:"index" json:"guildId"`
	Name      string     `gorm:"size:128" json:"name"`
	SortOrder int        `gorm:"default:0" json:"sortOrder"` // 分类排序
	CreatedAt time.Time  `json:"createdAt"`
	DeletedAt *time.Time `gorm:"index" json:"deletedAt,omitempty"`
}

type Channel struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	GuildID    uint       `gorm:"index" json:"guildId"`
	ParentID   *uint      `gorm:"index" json:"parentId,omitempty"`   // null = top-level channel
	CategoryID *uint      `gorm:"index" json:"categoryId,omitempty"` // 所属分类，null表示无分类
	Name       string     `gorm:"size:16;index" json:"name"`
	Type       string     `gorm:"size:16;default:'text';index" json:"type"` // 频道类型: text, media, forum
	SortOrder  int        `gorm:"default:0" json:"sortOrder"`               // 排序顺序，数字越小越靠前
	Banner     string     `gorm:"size:512" json:"banner,omitempty"`         // 频道横幅图片路径(MinIO object name)
	CreatedAt  time.Time  `json:"createdAt"`
	DeletedAt  *time.Time `gorm:"index" json:"deletedAt,omitempty"`
}
