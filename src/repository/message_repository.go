package repository

import (
	"errors"
	"time"

	"bubble/src/db/models"
)

// ──────────────────────────────────────────────
// Message repository methods
// ──────────────────────────────────────────────

func (r *Repo) AddMessage(m *models.Message) error {
	var ch models.Channel
	if err := r.DB.First(&ch, m.ChannelID).Error; err != nil {
		return errors.New("频道不存在")
	}
	if err := r.DB.Create(m).Error; err != nil {
		return err
	}
	if m.ReplyToID != nil {
		var replyTo models.Message
		if err := r.DB.First(&replyTo, *m.ReplyToID).Error; err == nil {
			m.ReplyTo = &replyTo
		}
	}
	return nil
}

func (r *Repo) GetMessage(id uint) (*models.Message, error) {
	var m models.Message
	if err := r.DB.First(&m, id).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *Repo) GetMessages(channelID uint, limit int, beforeID, afterID uint) ([]models.Message, error) {
	if limit <= 0 {
		limit = 50
	} else if limit > 100 {
		limit = 100
	}
	var list []models.Message
	cutoff := time.Now().Add(-1 * time.Hour)
	q := r.DB.
		Where("channel_id = ? AND (deleted_at IS NULL OR deleted_at >= ?) AND type != 'interaction'", channelID, cutoff).
		Preload("ReplyTo")
	if afterID > 0 {
		q = q.Where("id > ?", afterID).Order("id asc").Limit(limit)
		if err := q.Find(&list).Error; err != nil {
			return nil, err
		}
		return list, nil
	}
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID).Order("id desc").Limit(limit)
		if err := q.Find(&list).Error; err != nil {
			return nil, err
		}
		for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
			list[i], list[j] = list[j], list[i]
		}
		return list, nil
	}
	if err := q.Order("id desc").Limit(limit).Find(&list).Error; err != nil {
		return nil, err
	}
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	return list, nil
}

// GetMessagesWithUsers 获取频道消息列表，并返回相关的用户数据（含角色信息）
func (r *Repo) GetMessagesWithUsers(channelID uint, limit int, beforeID, afterID uint) ([]models.Message, []models.User, error) {
	messages, err := r.GetMessages(channelID, limit, beforeID, afterID)
	if err != nil {
		return nil, nil, err
	}
	if len(messages) == 0 {
		return messages, []models.User{}, nil
	}

	userIDSet := make(map[uint]struct{})
	for _, msg := range messages {
		userIDSet[msg.AuthorID] = struct{}{}
		if msg.ReplyTo != nil {
			userIDSet[msg.ReplyTo.AuthorID] = struct{}{}
		}
	}

	userIDs := make([]uint, 0, len(userIDSet))
	for id := range userIDSet {
		userIDs = append(userIDs, id)
	}

	var users []models.User
	if len(userIDs) > 0 {
		if err := r.DB.Where("id IN ?", userIDs).Find(&users).Error; err != nil {
			return messages, []models.User{}, err
		}
	}

	if len(users) > 0 {
		ch, chErr := r.GetChannel(channelID)
		if chErr == nil && ch != nil && ch.GuildID > 0 {
			var mrs []models.MemberRole
			if r.DB.Where("guild_id = ? AND user_id IN ?", ch.GuildID, userIDs).Find(&mrs).Error == nil && len(mrs) > 0 {
				roleIDsSet := make(map[uint]struct{}, len(mrs))
				rolesByUser := make(map[uint][]uint, len(userIDs))
				for _, mr := range mrs {
					roleIDsSet[mr.RoleID] = struct{}{}
					rolesByUser[mr.UserID] = append(rolesByUser[mr.UserID], mr.RoleID)
				}
				roleIDs := make([]uint, 0, len(roleIDsSet))
				for id := range roleIDsSet {
					roleIDs = append(roleIDs, id)
				}
				var roles []models.Role
				if r.DB.Where("guild_id = ? AND id IN ?", ch.GuildID, roleIDs).Find(&roles).Error == nil {
					rmap := make(map[uint]models.Role, len(roles))
					for _, rl := range roles {
						rmap[rl.ID] = rl
					}
					for i := range users {
						if rids := rolesByUser[users[i].ID]; len(rids) > 0 {
							rs := make([]models.Role, 0, len(rids))
							for _, rid := range rids {
								if rl, ok := rmap[rid]; ok {
									rs = append(rs, rl)
								}
							}
							users[i].Roles = rs
						}
					}
				}
			}
		}
	}

	return messages, users, nil
}

func (r *Repo) DeleteMessage(messageID uint) error {
	now := time.Now()
	return r.DB.Model(&models.Message{}).Where("id = ?", messageID).Update("deleted_at", now).Error
}

// ──────────────────────────────────────────────
// Message Search
// ──────────────────────────────────────────────

func (r *Repo) SearchDmMessages(userID uint, query string, limit int) ([]models.DmMessage, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	var threadIDs []uint
	r.DB.Model(&models.DmThread{}).
		Where("user_a_id = ? OR user_b_id = ?", userID, userID).
		Pluck("id", &threadIDs)
	if len(threadIDs) == 0 {
		return []models.DmMessage{}, nil
	}
	pattern := "%" + query + "%"
	var list []models.DmMessage
	err := r.DB.Where("thread_id IN ? AND content LIKE ? AND deleted_at IS NULL", threadIDs, pattern).
		Order("created_at DESC").Limit(limit).Find(&list).Error
	return list, err
}

func (r *Repo) SearchGroupMessages(userID uint, query string, limit int) ([]models.GroupMessage, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	var threadIDs []uint
	r.DB.Model(&models.GroupThreadMember{}).Where("user_id = ?", userID).Pluck("thread_id", &threadIDs)
	if len(threadIDs) == 0 {
		return []models.GroupMessage{}, nil
	}
	pattern := "%" + query + "%"
	var list []models.GroupMessage
	err := r.DB.Where("thread_id IN ? AND content LIKE ? AND deleted_at IS NULL", threadIDs, pattern).
		Order("created_at DESC").Limit(limit).Find(&list).Error
	return list, err
}

// ──────────────────────────────────────────────
// Reactions
// ──────────────────────────────────────────────

func (r *Repo) AddReaction(messageID, userID uint, emoji string) (*models.MessageReaction, error) {
	reaction := models.MessageReaction{
		MessageID: messageID,
		UserID:    userID,
		Emoji:     emoji,
	}
	if err := r.DB.Where("message_id = ? AND user_id = ? AND emoji = ?", messageID, userID, emoji).
		FirstOrCreate(&reaction).Error; err != nil {
		return nil, err
	}
	return &reaction, nil
}

func (r *Repo) RemoveReaction(messageID, userID uint, emoji string) error {
	return r.DB.Where("message_id = ? AND user_id = ? AND emoji = ?", messageID, userID, emoji).
		Delete(&models.MessageReaction{}).Error
}

func (r *Repo) ListReactions(messageID uint) ([]models.MessageReaction, error) {
	var list []models.MessageReaction
	err := r.DB.Where("message_id = ?", messageID).Order("created_at ASC").Find(&list).Error
	return list, err
}

// ListReactionsByMessageIDs 批量获取多条消息的表态
func (r *Repo) ListReactionsByMessageIDs(messageIDs []uint) ([]models.MessageReaction, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}
	var list []models.MessageReaction
	err := r.DB.Where("message_id IN ?", messageIDs).Order("created_at ASC").Find(&list).Error
	return list, err
}

func (r *Repo) CountReactionEmojis(messageID uint) (int64, error) {
	var count int64
	err := r.DB.Model(&models.MessageReaction{}).
		Where("message_id = ?", messageID).
		Select("COUNT(DISTINCT emoji)").
		Scan(&count).Error
	return count, err
}

// ──────────────────────────────────────────────
// Pinned Messages
// ──────────────────────────────────────────────

func (r *Repo) CreatePinnedMessage(pm *models.PinnedMessage) error {
	return r.DB.Create(pm).Error
}

func (r *Repo) ListPinnedMessages(channelID uint) ([]models.PinnedMessage, error) {
	var list []models.PinnedMessage
	err := r.DB.Where("channel_id = ?", channelID).Order("created_at DESC").Find(&list).Error
	return list, err
}

func (r *Repo) ListPinnedMessagesByThread(threadID uint) ([]models.PinnedMessage, error) {
	var list []models.PinnedMessage
	err := r.DB.Where("thread_id = ?", threadID).Order("created_at DESC").Find(&list).Error
	return list, err
}

func (r *Repo) GetPinnedMessage(messageID uint) (*models.PinnedMessage, error) {
	var pm models.PinnedMessage
	if err := r.DB.Where("message_id = ?", messageID).First(&pm).Error; err != nil {
		return nil, err
	}
	return &pm, nil
}

func (r *Repo) DeletePinnedMessage(messageID uint) error {
	return r.DB.Where("message_id = ?", messageID).Delete(&models.PinnedMessage{}).Error
}

// ──────────────────────────────────────────────
// Favorite Messages
// ──────────────────────────────────────────────

func (r *Repo) CreateFavoriteMessage(fm *models.FavoriteMessage) error {
	return r.DB.Create(fm).Error
}

func (r *Repo) GetFavoriteMessage(userID uint, messageID, dmMessageID uint) (*models.FavoriteMessage, error) {
	var fm models.FavoriteMessage
	query := r.DB.Where("user_id = ?", userID)
	if messageID > 0 {
		query = query.Where("message_id = ?", messageID)
	} else if dmMessageID > 0 {
		query = query.Where("dm_message_id = ?", dmMessageID)
	}
	if err := query.First(&fm).Error; err != nil {
		return nil, err
	}
	return &fm, nil
}

func (r *Repo) GetFavoriteByID(id uint) (*models.FavoriteMessage, error) {
	var fm models.FavoriteMessage
	if err := r.DB.First(&fm, id).Error; err != nil {
		return nil, err
	}
	return &fm, nil
}

func (r *Repo) ListFavoriteMessages(userID uint, limit int) ([]models.FavoriteMessage, error) {
	var list []models.FavoriteMessage
	err := r.DB.Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit).
		Find(&list).Error
	return list, err
}

func (r *Repo) DeleteFavoriteMessage(id uint) error {
	return r.DB.Delete(&models.FavoriteMessage{}, id).Error
}

// ──────────────────────────────────────────────
// Guild Media
// ──────────────────────────────────────────────

func (r *Repo) GetGuildMediaByType(guildID uint, mediaType string, limit int, before uint) ([]models.Message, error) {
	var channelIDs []uint
	err := r.DB.Model(&models.Channel{}).
		Where("guild_id = ? AND deleted_at IS NULL", guildID).
		Pluck("id", &channelIDs).Error
	if err != nil {
		return nil, err
	}
	if len(channelIDs) == 0 {
		return []models.Message{}, nil
	}

	query := r.DB.Model(&models.Message{}).
		Where("channel_id IN ? AND deleted_at IS NULL", channelIDs).
		Where("file_meta IS NOT NULL AND file_meta != 'null' AND file_meta != '{}'")

	switch mediaType {
	case "image":
		query = query.Where("type = ? OR type LIKE ?", "image", "image/%")
	case "video":
		query = query.Where("type = ? OR type LIKE ?", "video", "video/%")
	case "file":
		query = query.Where("(type = ? OR type LIKE ?) AND type NOT LIKE ? AND type NOT LIKE ?",
			"file", "file/%", "image%", "video%")
	}

	if before > 0 {
		query = query.Where("id < ?", before)
	}

	var messages []models.Message
	err = query.Order("id DESC").Limit(limit).Find(&messages).Error
	return messages, err
}

func (r *Repo) DeleteGuildMessages(guildID uint, messageIDs []uint) (int, error) {
	if len(messageIDs) == 0 {
		return 0, nil
	}
	var channelIDs []uint
	err := r.DB.Model(&models.Channel{}).
		Where("guild_id = ? AND deleted_at IS NULL", guildID).
		Pluck("id", &channelIDs).Error
	if err != nil {
		return 0, err
	}
	if len(channelIDs) == 0 {
		return 0, nil
	}
	result := r.DB.Model(&models.Message{}).
		Where("id IN ? AND channel_id IN ? AND deleted_at IS NULL", messageIDs, channelIDs).
		Update("deleted_at", time.Now())
	return int(result.RowsAffected), result.Error
}
