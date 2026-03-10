package repository

import (
	"bubble/src/db/models"
	"fmt"
	"time"
)

// ==================== 机器人分类 ====================

// ListRobotCategories 获取所有机器人分类（按 sort_order 升序）
func (r *Repo) ListRobotCategories() ([]models.RobotCategory, error) {
	var list []models.RobotCategory
	err := r.DB.Order("sort_order ASC, id ASC").Find(&list).Error
	return list, err
}

// EnsureDefaultCategories 确保默认分类存在（幂等操作）
func (r *Repo) EnsureDefaultCategories() error {
	for i, name := range models.DefaultRobotCategories {
		cat := models.RobotCategory{
			Name:      name,
			SortOrder: i,
		}
		// 使用 FirstOrCreate 保证幂等
		r.DB.Where("name = ?", name).FirstOrCreate(&cat)
	}
	return nil
}

// ==================== 热度统计辅助查询 ====================

// HeatWeights 热度计算权重参数
type HeatWeights struct {
	Guild       int // 存量服务器数权重
	GuildGrowth int // 新增服务器数权重
	Message     int // 消息数权重
	Interaction int // 被回复数权重
}

// CalcAllRobotHeatScores 计算所有机器人在指定时间范围内的热度
// 4 个维度：存量服务器数、新增服务器数、发送消息数、被回复数
func (r *Repo) CalcAllRobotHeatScores(periodStart, periodEnd time.Time, w HeatWeights) ([]RobotHeatResult, error) {
	// gc: 存量服务器数（当前在多少服务器中）
	// gg: 该周期内新被添加到的服务器数（guild_members.created_at 在周期内）
	// mc: 该周期内机器人发送的消息数
	// ic: 该周期内在机器人发送的消息所在频道中、其他用户回复的消息数
	query := `
		SELECT
			rb.id AS robot_id,
			COALESCE(lm.last_msg_at, rb.created_at) AS last_active_at,
			COALESCE(gc.guild_count, 0) AS guild_count,
			COALESCE(gg.guild_growth, 0) AS guild_growth,
			COALESCE(mc.message_count, 0) AS message_count,
			COALESCE(ic.interaction_count, 0) AS interaction_count,
			(
				COALESCE(gc.guild_count, 0) * ? +
				COALESCE(gg.guild_growth, 0) * ? +
				COALESCE(mc.message_count, 0) * ? +
				COALESCE(ic.interaction_count, 0) * ?
			) AS heat_score
		FROM robots rb
		LEFT JOIN (
			SELECT gm.user_id, COUNT(DISTINCT gm.guild_id) AS guild_count
			FROM guild_members gm
			INNER JOIN robots r ON r.bot_user_id = gm.user_id
			GROUP BY gm.user_id
		) gc ON gc.user_id = rb.bot_user_id
		LEFT JOIN (
			SELECT gm.user_id, COUNT(DISTINCT gm.guild_id) AS guild_growth
			FROM guild_members gm
			INNER JOIN robots r ON r.bot_user_id = gm.user_id
			WHERE gm.created_at >= ? AND gm.created_at < ?
			GROUP BY gm.user_id
		) gg ON gg.user_id = rb.bot_user_id
		LEFT JOIN (
			SELECT m.author_id AS user_id, COUNT(*) AS message_count
			FROM messages m
			INNER JOIN robots r ON r.bot_user_id = m.author_id
			WHERE m.created_at >= ? AND m.created_at < ?
			GROUP BY m.author_id
		) mc ON mc.user_id = rb.bot_user_id
		LEFT JOIN (
			SELECT bot_msg.author_id AS user_id, COUNT(reply.id) AS interaction_count
			FROM messages bot_msg
			INNER JOIN robots r ON r.bot_user_id = bot_msg.author_id
			INNER JOIN messages reply ON reply.channel_id = bot_msg.channel_id
				AND reply.author_id != bot_msg.author_id
				AND reply.created_at >= bot_msg.created_at
				AND reply.created_at < DATE_ADD(bot_msg.created_at, INTERVAL 1 HOUR)
			WHERE bot_msg.created_at >= ? AND bot_msg.created_at < ?
			GROUP BY bot_msg.author_id
		) ic ON ic.user_id = rb.bot_user_id
		LEFT JOIN (
			SELECT m.author_id AS user_id, MAX(m.created_at) AS last_msg_at
			FROM messages m
			INNER JOIN robots r ON r.bot_user_id = m.author_id
			GROUP BY m.author_id
		) lm ON lm.user_id = rb.bot_user_id
		ORDER BY heat_score DESC
	`

	var results []RobotHeatResult
	err := r.DB.Raw(query,
		w.Guild, w.GuildGrowth, w.Message, w.Interaction,
		periodStart, periodEnd,
		periodStart, periodEnd,
		periodStart, periodEnd,
	).Scan(&results).Error
	return results, err
}

// RobotHeatResult 热度计算原始结果
type RobotHeatResult struct {
	RobotID          uint      `json:"robotId"`
	LastActiveAt     time.Time `json:"lastActiveAt"`
	GuildCount       int64     `json:"guildCount"`
	GuildGrowth      int64     `json:"guildGrowth"`
	MessageCount     int64     `json:"messageCount"`
	InteractionCount int64     `json:"interactionCount"`
	HeatScore        int64     `json:"heatScore"`
}

// GetLastMessageTime 获取用户最后一条消息的时间
func (r *Repo) GetLastMessageTime(userID uint) (*time.Time, error) {
	// 使用原始 SQL 以匹配 messages 表实际列名（与 CalcAllRobotHeatScores 保持一致）
	var result struct {
		CreatedAt *time.Time
	}
	err := r.DB.Raw("SELECT created_at FROM messages WHERE author_id = ? ORDER BY id DESC LIMIT 1", userID).Scan(&result).Error
	if err != nil || result.CreatedAt == nil {
		return nil, err
	}
	return result.CreatedAt, nil
}

// GetCurrentPeriodKeys 获取当前各周期的 key
func GetCurrentPeriodKeys(now time.Time) (daily, weekly, monthly string) {
	daily = now.Format("2006-01-02")

	// ISO 周：周一到周日
	year, week := now.ISOWeek()
	weekly = fmt.Sprintf("%d-W%02d", year, week)

	monthly = now.Format("2006-01")
	return
}

// GetPeriodTimeRange 获取指定周期类型和key对应的时间范围
func GetPeriodTimeRange(periodType, periodKey string) (start, end time.Time, err error) {
	loc := time.Now().Location()

	switch periodType {
	case "daily":
		start, err = time.ParseInLocation("2006-01-02", periodKey, loc)
		if err != nil {
			return
		}
		end = start.AddDate(0, 0, 1)

	case "weekly":
		// periodKey 格式: "2026-W07"
		var year, week int
		_, err = fmt.Sscanf(periodKey, "%d-W%02d", &year, &week)
		if err != nil {
			return
		}
		// ISO 8601: Jan 4 一定在该年的 ISO W01
		jan4 := time.Date(year, 1, 4, 0, 0, 0, 0, loc)
		// 找到 Jan 4 所在周的周一（即 ISO W01 的周一）
		wd := jan4.Weekday()
		if wd == time.Sunday {
			wd = 7
		}
		w01Monday := jan4.AddDate(0, 0, -int(wd-time.Monday))
		// 目标周的周一 = W01 周一 + (week-1)*7
		start = w01Monday.AddDate(0, 0, (week-1)*7)
		end = start.AddDate(0, 0, 7)

	case "monthly":
		start, err = time.ParseInLocation("2006-01", periodKey, loc)
		if err != nil {
			return
		}
		end = start.AddDate(0, 1, 0)

	default:
		err = fmt.Errorf("unknown period type: %s", periodType)
	}
	return
}
