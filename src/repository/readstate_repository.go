package repository

import (
	"errors"
	"time"

	"bubble/src/db/models"

	"gorm.io/gorm"
)

// ──────────────────────────────────────────────
// Read State (红点系统) repository methods
// ──────────────────────────────────────────────

// GetReadState 获取用户在某个资源的阅读状态
func (r *Repo) GetReadState(userID uint, resourceType string, resourceID uint) (*models.ReadState, error) {
	var rs models.ReadState
	err := r.DB.Where("user_id = ? AND resource_type = ? AND resource_id = ?", userID, resourceType, resourceID).
		First(&rs).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &rs, nil
}

// GetOrCreateReadState 获取或创建阅读状态
func (r *Repo) GetOrCreateReadState(userID uint, resourceType string, resourceID uint) (*models.ReadState, error) {
	rs, err := r.GetReadState(userID, resourceType, resourceID)
	if err != nil {
		return nil, err
	}
	if rs != nil {
		return rs, nil
	}

	now := time.Now()
	rs = &models.ReadState{
		UserID:       userID,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		UnreadCount:  0,
		MentionCount: 0,
		UpdatedAt:    now,
		CreatedAt:    now,
	}
	if err := r.DB.Create(rs).Error; err != nil {
		return nil, err
	}
	return rs, nil
}

// UpdateReadState 更新阅读状态（标记已读到某条消息）
func (r *Repo) UpdateReadState(userID uint, resourceType string, resourceID uint, messageID uint) error {
	now := time.Now()
	rs, err := r.GetOrCreateReadState(userID, resourceType, resourceID)
	if err != nil {
		return err
	}

	return r.DB.Model(rs).Updates(map[string]any{
		"last_read_message_id": messageID,
		"last_read_at":         &now,
		"unread_count":         0,
		"mention_count":        0,
		"updated_at":           now,
	}).Error
}

// UpdateReadStateCounts 更新阅读状态的未读计数
func (r *Repo) UpdateReadStateCounts(userID uint, resourceType string, resourceID uint, unreadCount uint, mentionCount uint) error {
	now := time.Now()
	rs, err := r.GetOrCreateReadState(userID, resourceType, resourceID)
	if err != nil {
		return err
	}

	return r.DB.Model(rs).Updates(map[string]any{
		"unread_count":  unreadCount,
		"mention_count": mentionCount,
		"updated_at":    now,
	}).Error
}

// GetReadStatesByUser 批量获取用户的阅读状态
func (r *Repo) GetReadStatesByUser(userID uint, resourceType string, resourceIDs []uint) (map[uint]*models.ReadState, error) {
	if len(resourceIDs) == 0 {
		return make(map[uint]*models.ReadState), nil
	}

	var states []models.ReadState
	err := r.DB.Where("user_id = ? AND resource_type = ? AND resource_id IN ?", userID, resourceType, resourceIDs).
		Find(&states).Error
	if err != nil {
		return nil, err
	}

	result := make(map[uint]*models.ReadState)
	for i := range states {
		result[states[i].ResourceID] = &states[i]
	}
	return result, nil
}

// GetAllReadStatesByUser 获取用户所有阅读状态（用于获取总未读数）
func (r *Repo) GetAllReadStatesByUser(userID uint) ([]models.ReadState, error) {
	var states []models.ReadState
	err := r.DB.Where("user_id = ? AND (unread_count > 0 OR mention_count > 0)", userID).
		Find(&states).Error
	if err != nil {
		return nil, err
	}
	return states, nil
}

// IncrementUnreadCount 增加未读计数（新消息到达时调用）
func (r *Repo) IncrementUnreadCount(userID uint, resourceType string, resourceID uint, isMention bool) error {
	rs, err := r.GetOrCreateReadState(userID, resourceType, resourceID)
	if err != nil {
		return err
	}

	updates := map[string]any{
		"unread_count": gorm.Expr("unread_count + 1"),
		"updated_at":   time.Now(),
	}
	if isMention {
		updates["mention_count"] = gorm.Expr("mention_count + 1")
	}

	return r.DB.Model(rs).Updates(updates).Error
}

// BatchIncrementUnreadCount 批量增加未读计数（优化性能）
func (r *Repo) BatchIncrementUnreadCount(userIDs []uint, resourceType string, resourceID uint, mentionedUserIDs []uint) error {
	if len(userIDs) == 0 {
		return nil
	}

	now := time.Now()
	mentionMap := make(map[uint]bool)
	for _, uid := range mentionedUserIDs {
		mentionMap[uid] = true
	}

	// 如果是频道消息，需要同时更新公会级别的未读数
	var guildID uint
	if resourceType == "channel" {
		channel, err := r.GetChannel(resourceID)
		if err == nil && channel != nil {
			guildID = channel.GuildID
		}
	}

	for _, userID := range userIDs {
		isMention := mentionMap[userID]

		// 1. 更新频道/DM级别的未读计数
		updates := map[string]any{
			"unread_count": gorm.Expr("unread_count + 1"),
			"updated_at":   now,
		}
		if isMention {
			updates["mention_count"] = gorm.Expr("mention_count + 1")
		}

		result := r.DB.Model(&models.ReadState{}).
			Where("user_id = ? AND resource_type = ? AND resource_id = ?", userID, resourceType, resourceID).
			Updates(updates)

		if result.Error != nil {
			return result.Error
		}

		if result.RowsAffected == 0 {
			rs := &models.ReadState{
				UserID:       userID,
				ResourceType: resourceType,
				ResourceID:   resourceID,
				UnreadCount:  1,
				MentionCount: 0,
				UpdatedAt:    now,
				CreatedAt:    now,
			}
			if isMention {
				rs.MentionCount = 1
			}
			if err := r.DB.Create(rs).Error; err != nil {
				return err
			}
		}

		// 2. 如果是频道消息，同时更新公会级别的未读数
		if guildID > 0 {
			guildUpdates := map[string]any{
				"unread_count": gorm.Expr("unread_count + 1"),
				"updated_at":   now,
			}
			if isMention {
				guildUpdates["mention_count"] = gorm.Expr("mention_count + 1")
			}

			guildResult := r.DB.Model(&models.ReadState{}).
				Where("user_id = ? AND resource_type = ? AND resource_id = ?", userID, "guild", guildID).
				Updates(guildUpdates)

			if guildResult.Error != nil {
				return guildResult.Error
			}

			if guildResult.RowsAffected == 0 {
				guildRS := &models.ReadState{
					UserID:       userID,
					ResourceType: "guild",
					ResourceID:   guildID,
					UnreadCount:  1,
					MentionCount: 0,
					UpdatedAt:    now,
					CreatedAt:    now,
				}
				if isMention {
					guildRS.MentionCount = 1
				}
				if err := r.DB.Create(guildRS).Error; err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// ClearReadState 清除阅读状态（用于退出服务器或删除DM等场景）
func (r *Repo) ClearReadState(userID uint, resourceType string, resourceID uint) error {
	return r.DB.Where("user_id = ? AND resource_type = ? AND resource_id = ?", userID, resourceType, resourceID).
		Delete(&models.ReadState{}).Error
}
