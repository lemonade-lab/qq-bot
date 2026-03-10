package repository

import (
	"bubble/src/db/models"
)

// ==================== Webhook 调用日志 ====================

// CreateWebhookLog 写入 webhook 调用日志
func (r *Repo) CreateWebhookLog(log *models.WebhookLog) error {
	return r.DB.Create(log).Error
}

// ListWebhookLogs 查询指定机器人的 webhook 调用日志（按时间倒序）
func (r *Repo) ListWebhookLogs(robotID uint, limit, offset int) ([]models.WebhookLog, int64, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	var total int64
	r.DB.Model(&models.WebhookLog{}).Where("robot_id = ?", robotID).Count(&total)

	var logs []models.WebhookLog
	err := r.DB.Where("robot_id = ?", robotID).
		Order("id DESC").
		Offset(offset).
		Limit(limit).
		Find(&logs).Error
	return logs, total, err
}

// DeleteWebhookLogsByRobotID 删除指定机器人的所有 webhook 日志
func (r *Repo) DeleteWebhookLogsByRobotID(robotID uint) error {
	return r.DB.Where("robot_id = ?", robotID).Delete(&models.WebhookLog{}).Error
}

// ==================== 机器人删除清理 ====================

// DeleteRankingsByRobotID 删除指定机器人的所有排行榜记录
func (r *Repo) DeleteRankingsByRobotID(robotID uint) error {
	return r.DB.Where("robot_id = ?", robotID).Delete(&models.RobotRanking{}).Error
}

// RemoveAllGuildMembersByUserID 将用户从所有服务器中移除
func (r *Repo) RemoveAllGuildMembersByUserID(userID uint) error {
	return r.DB.Where("user_id = ?", userID).Delete(&models.GuildMember{}).Error
}

// ListRobotJoinedGuilds 查询机器人已加入的服务器（含基本信息和成员数）
func (r *Repo) ListRobotJoinedGuilds(botUserID uint) ([]map[string]any, error) {
	var results []struct {
		ID          uint   `json:"id"`
		Name        string `json:"name"`
		Avatar      string `json:"avatar"`
		Description string `json:"description"`
		MemberCount int64  `json:"memberCount"`
	}

	err := r.DB.Table("guilds").
		Select("guilds.id, guilds.name, guilds.avatar, guilds.description, COUNT(gm2.id) AS member_count").
		Joins("INNER JOIN guild_members gm ON gm.guild_id = guilds.id AND gm.user_id = ?", botUserID).
		Joins("LEFT JOIN guild_members gm2 ON gm2.guild_id = guilds.id").
		Where("guilds.deleted_at IS NULL").
		Group("guilds.id").
		Order("guilds.id ASC").
		Scan(&results).Error
	if err != nil {
		return nil, err
	}

	list := make([]map[string]any, len(results))
	for i, r := range results {
		list[i] = map[string]any{
			"id":          r.ID,
			"name":        r.Name,
			"avatar":      r.Avatar,
			"description": r.Description,
			"memberCount": r.MemberCount,
		}
	}
	return list, nil
}
