package models

import "time"

// RobotCategory 机器人分类
type RobotCategory struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Name      string    `json:"name" gorm:"size:64;uniqueIndex"` // 分类名称
	SortOrder int       `json:"sortOrder" gorm:"default:0"`      // 排序顺序，越小越靠前
	CreatedAt time.Time `json:"createdAt"`
}

// DefaultRobotCategories 默认机器人分类列表
var DefaultRobotCategories = []string{
	"娱乐", "工具", "游戏", "音乐", "社交", "管理", "AI", "教育", "其他",
}

// RobotRanking 机器人热度排行榜快照
// 存储每个统计周期内的机器人热度数据
type RobotRanking struct {
	ID               uint      `json:"id" gorm:"primaryKey"`
	RobotID          uint      `json:"robotId" gorm:"uniqueIndex:idx_ranking_unique"`
	PeriodType       string    `json:"periodType" gorm:"size:16;uniqueIndex:idx_ranking_unique"` // "daily" | "weekly" | "monthly"
	PeriodKey        string    `json:"periodKey" gorm:"size:16;uniqueIndex:idx_ranking_unique"`  // "2026-02-14" | "2026-W07" | "2026-02"
	HeatScore        int64     `json:"heatScore" gorm:"index"`                                   // 热度得分（加权+衰减后）
	RawScore         int64     `json:"rawScore"`                                                 // 衰减前的原始得分
	GuildCount       int64     `json:"guildCount"`                                               // 存量服务器数
	GuildGrowth      int64     `json:"guildGrowth"`                                              // 该周期内新增加入的服务器数
	MessageCount     int64     `json:"messageCount"`                                             // 该周期内机器人发送消息数
	InteractionCount int64     `json:"interactionCount"`                                         // 该周期内被其他用户回复的消息数
	DecayApplied     bool      `json:"decayApplied"`                                             // 是否应用了衰减
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`

	Robot *Robot `gorm:"foreignKey:RobotID" json:"robot,omitempty"`
}
