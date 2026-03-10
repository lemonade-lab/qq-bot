package repository

import (
	"bubble/src/db/models"
	"time"
)

// GetRobotByID retrieves a robot by ID with its bot user
func (r *Repo) GetRobotByID(id uint) (*models.Robot, error) {
	var robot models.Robot
	err := r.DB.Preload("BotUser").First(&robot, id).Error
	return &robot, err
}

// GetRobotsByIDs 批量按 ID 查询机器人（含 BotUser），返回 map[id]*Robot
func (r *Repo) GetRobotsByIDs(ids []uint) (map[uint]*models.Robot, error) {
	if len(ids) == 0 {
		return map[uint]*models.Robot{}, nil
	}
	var robots []models.Robot
	err := r.DB.Preload("BotUser").Where("id IN ?", ids).Find(&robots).Error
	if err != nil {
		return nil, err
	}
	m := make(map[uint]*models.Robot, len(robots))
	for i := range robots {
		m[robots[i].ID] = &robots[i]
	}
	return m, nil
}

// UpdateRobot updates robot information
func (r *Repo) UpdateRobot(robot *models.Robot) error {
	return r.DB.Save(robot).Error
}

// DeleteRobot deletes a robot by ID
func (r *Repo) DeleteRobot(id uint) error {
	return r.DB.Delete(&models.Robot{}, id).Error
}

// DeleteUser deletes a user by ID
func (r *Repo) DeleteUser(id uint) error {
	return r.DB.Delete(&models.User{}, id).Error
}

// CountMessagesByUserID counts messages sent by a user
func (r *Repo) CountMessagesByUserID(userID uint) (int64, error) {
	var count int64
	err := r.DB.Model(&models.Message{}).Where("user_id = ?", userID).Count(&count).Error
	return count, err
}

// CountGuildsByUserID counts guilds where user is a member
func (r *Repo) CountGuildsByUserID(userID uint) (int64, error) {
	var count int64
	err := r.DB.Model(&models.GuildMember{}).Where("user_id = ?", userID).Count(&count).Error
	return count, err
}

// GetMessagesWithPagination retrieves messages with pagination
func (r *Repo) GetMessagesWithPagination(channelID uint, limit int, before uint) ([]models.Message, error) {
	var messages []models.Message
	cutoff := time.Now().Add(-1 * time.Hour)
	query := r.DB.Where("channel_id = ? AND (deleted_at IS NULL OR deleted_at >= ?) AND type != 'interaction'", channelID, cutoff).
		Order("id desc").
		Limit(limit)

	if before > 0 {
		query = query.Where("id < ?", before)
	}

	err := query.Preload("User").Find(&messages).Error
	return messages, err
}

// GetMessageByID retrieves a message by ID
func (r *Repo) GetMessageByID(id uint) (*models.Message, error) {
	var message models.Message
	err := r.DB.Preload("User").First(&message, id).Error
	return &message, err
}

// UpdateMessage updates a message
func (r *Repo) UpdateMessage(message *models.Message) error {
	return r.DB.Save(message).Error
}

// GetChannelsByGuildID retrieves all channels in a guild
func (r *Repo) GetChannelsByGuildID(guildID uint) ([]models.Channel, error) {
	var channels []models.Channel
	// Use a stable order; some schemas may not have a 'position' column.
	// Fallback to ordering by 'id asc' to avoid SQL errors.
	err := r.DB.Where("guild_id = ?", guildID).Order("id asc").Find(&channels).Error
	return channels, err
}

// GetChannelByID retrieves a channel by ID
func (r *Repo) GetChannelByID(id uint) (*models.Channel, error) {
	var channel models.Channel
	err := r.DB.First(&channel, id).Error
	return &channel, err
}

// GetMembersByGuildID retrieves members of a guild with limit
func (r *Repo) GetMembersByGuildID(guildID uint, limit int) ([]models.GuildMember, error) {
	var members []models.GuildMember
	err := r.DB.Where("guild_id = ?", guildID).Limit(limit).Preload("User").Find(&members).Error
	return members, err
}

// GetMemberByGuildAndUser retrieves a specific member
func (r *Repo) GetMemberByGuildAndUser(guildID, userID uint) (*models.GuildMember, error) {
	var member models.GuildMember
	err := r.DB.Where("guild_id = ? AND user_id = ?", guildID, userID).Preload("User").First(&member).Error
	return &member, err
}

// ListTopRobots 返回热门机器人列表，按所在服务器数量降序排序（服务器最多的在前）。
// limit 建议 1-50 之间，内部做上限保护（最大 50）。
func (r *Repo) ListTopRobots(limit int) ([]models.Robot, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	var list []models.Robot
	// 按机器人所在的服务器数量降序排序
	// 通过 JOIN guild_members 表统计每个机器人（bot_user_id）在多少个服务器中
	err := r.DB.Table("robots").
		Select("robots.*, COUNT(DISTINCT gm.guild_id) AS guild_count").
		Joins("INNER JOIN users ON users.id = robots.bot_user_id").
		Joins("LEFT JOIN guild_members gm ON gm.user_id = robots.bot_user_id").
		Where("robots.is_private = ?", false).
		Group("robots.id").
		Order("guild_count DESC").
		Order("robots.id ASC").
		Limit(limit).
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	// 手动 Preload BotUser
	for i := range list {
		var botUser models.User
		if err := r.DB.First(&botUser, list[i].BotUserID).Error; err == nil {
			list[i].BotUser = &botUser
		}
	}
	return list, nil
}

// SearchRobotsByName 按名称模糊查询机器人（搜索 BotUser.Name），大小写敏感由底层数据库决定。
// q: 关键词，建议前端最少 1 个字符；limit: 返回上限（默认 20, 最大 100）。
func (r *Repo) SearchRobotsByName(q string, limit int) ([]models.Robot, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	like := "%" + q + "%"
	var list []models.Robot
	err := r.DB.Preload("BotUser").
		Joins("INNER JOIN users ON users.id = robots.bot_user_id").
		Where("users.name LIKE ? AND robots.is_private = ?", like, false).
		Order("robots.id ASC").
		Limit(limit).
		Find(&list).Error
	return list, err
}
