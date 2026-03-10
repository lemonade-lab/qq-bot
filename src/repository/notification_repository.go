package repository

import (
	"time"

	"bubble/src/db/models"
)

// ──────────────────────────────────────────────
// Notification repository methods
// ──────────────────────────────────────────────

func (r *Repo) ListUserNotifications(userID uint, limit int, beforeID, afterID uint) ([]models.UserNotification, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	q := r.DB.Model(&models.UserNotification{}).Where("user_id = ?", userID)
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	if afterID > 0 {
		q = q.Where("id > ?", afterID)
	}
	var list []models.UserNotification
	if err := q.Order("id DESC").Limit(limit).Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *Repo) ListUserNotificationsWithInfo(userID uint, limit int, beforeID, afterID uint) ([]models.UserNotification, map[uint]models.User, map[uint]models.Guild, map[uint]models.Channel, map[uint]models.Message, map[uint]models.DmMessage, error) {
	notifs, err := r.ListUserNotifications(userID, limit, beforeID, afterID)
	if err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}

	userIDs := make(map[uint]bool)
	guildIDs := make(map[uint]bool)
	channelIDs := make(map[uint]bool)
	messageIDs := make(map[uint]bool)
	dmMessageIDs := make(map[uint]bool)

	for _, n := range notifs {
		if n.AuthorID != nil {
			userIDs[*n.AuthorID] = true
		}
		if n.GuildID != nil {
			guildIDs[*n.GuildID] = true
		}
		if n.ChannelID != nil {
			channelIDs[*n.ChannelID] = true
		}
		if n.MessageID != nil {
			if n.SourceType == "channel" {
				messageIDs[*n.MessageID] = true
			} else if n.SourceType == "dm" {
				dmMessageIDs[*n.MessageID] = true
			}
		}
	}

	users := make(map[uint]models.User)
	if len(userIDs) > 0 {
		uids := make([]uint, 0, len(userIDs))
		for uid := range userIDs {
			uids = append(uids, uid)
		}
		var userList []models.User
		if err := r.DB.Where("id IN ?", uids).Find(&userList).Error; err == nil {
			for _, u := range userList {
				users[u.ID] = u
			}
		}
	}

	guilds := make(map[uint]models.Guild)
	if len(guildIDs) > 0 {
		gids := make([]uint, 0, len(guildIDs))
		for gid := range guildIDs {
			gids = append(gids, gid)
		}
		var guildList []models.Guild
		if err := r.DB.Where("id IN ?", gids).Find(&guildList).Error; err == nil {
			for _, g := range guildList {
				guilds[g.ID] = g
			}
		}
	}

	channels := make(map[uint]models.Channel)
	if len(channelIDs) > 0 {
		cids := make([]uint, 0, len(channelIDs))
		for cid := range channelIDs {
			cids = append(cids, cid)
		}
		var channelList []models.Channel
		if err := r.DB.Where("id IN ?", cids).Find(&channelList).Error; err == nil {
			for _, ch := range channelList {
				channels[ch.ID] = ch
			}
		}
	}

	messages := make(map[uint]models.Message)
	if len(messageIDs) > 0 {
		mids := make([]uint, 0, len(messageIDs))
		for mid := range messageIDs {
			mids = append(mids, mid)
		}
		var msgList []models.Message
		if err := r.DB.Where("id IN ?", mids).Find(&msgList).Error; err == nil {
			for _, msg := range msgList {
				messages[msg.ID] = msg
			}
		}
	}

	dmMessages := make(map[uint]models.DmMessage)
	if len(dmMessageIDs) > 0 {
		dmids := make([]uint, 0, len(dmMessageIDs))
		for mid := range dmMessageIDs {
			dmids = append(dmids, mid)
		}
		var dmMsgList []models.DmMessage
		if err := r.DB.Where("id IN ?", dmids).Find(&dmMsgList).Error; err == nil {
			for _, msg := range dmMsgList {
				dmMessages[msg.ID] = msg
			}
		}
	}

	return notifs, users, guilds, channels, messages, dmMessages, nil
}

func (r *Repo) ListUserApplications(userID uint, limit int, beforeID, afterID uint) ([]models.UserApplication, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	q := r.DB.Model(&models.UserApplication{}).Where("to_user_id = ?", userID)
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	if afterID > 0 {
		q = q.Where("id > ?", afterID)
	}
	var list []models.UserApplication
	if err := q.Order("id DESC").Limit(limit).Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *Repo) ListUserApplicationsWithInfo(userID uint, limit int, beforeID, afterID uint) ([]models.UserApplication, map[uint]models.User, map[uint]models.Guild, error) {
	apps, err := r.ListUserApplications(userID, limit, beforeID, afterID)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(apps) == 0 {
		return apps, make(map[uint]models.User), make(map[uint]models.Guild), nil
	}

	userIDSet := make(map[uint]struct{})
	guildIDSet := make(map[uint]struct{})
	for _, app := range apps {
		userIDSet[app.FromUserID] = struct{}{}
		userIDSet[app.ToUserID] = struct{}{}
		if app.TargetGuildID != nil {
			guildIDSet[*app.TargetGuildID] = struct{}{}
		}
	}

	userIDList := make([]uint, 0, len(userIDSet))
	for id := range userIDSet {
		userIDList = append(userIDList, id)
	}
	var users []models.User
	userMap := make(map[uint]models.User)
	if len(userIDList) > 0 {
		if err := r.DB.Where("id IN ?", userIDList).Find(&users).Error; err == nil {
			for _, u := range users {
				userMap[u.ID] = u
			}
		}
	}

	guildIDList := make([]uint, 0, len(guildIDSet))
	for id := range guildIDSet {
		guildIDList = append(guildIDList, id)
	}
	var guilds []models.Guild
	guildMap := make(map[uint]models.Guild)
	if len(guildIDList) > 0 {
		if err := r.DB.Where("id IN ? AND deleted_at IS NULL", guildIDList).Find(&guilds).Error; err == nil {
			for _, g := range guilds {
				guildMap[g.ID] = g
			}
		}
	}

	return apps, userMap, guildMap, nil
}

func (r *Repo) CreateNotification(n *models.UserNotification) error {
	return r.DB.Create(n).Error
}

func (r *Repo) MarkNotificationRead(id uint, userID uint) error {
	return r.DB.Model(&models.UserNotification{}).
		Where("id = ? AND user_id = ?", id, userID).
		Updates(map[string]interface{}{"read": true}).Error
}

func (r *Repo) MarkNotificationsRead(ids []uint, userID uint) error {
	if len(ids) == 0 {
		return nil
	}
	return r.DB.Model(&models.UserNotification{}).
		Where("id IN ? AND user_id = ?", ids, userID).
		Updates(map[string]interface{}{"read": true}).Error
}

func (r *Repo) MarkAllNotificationsRead(userID uint) error {
	return r.DB.Model(&models.UserNotification{}).
		Where("user_id = ? AND `read` = ?", userID, false).
		Updates(map[string]interface{}{"read": true}).Error
}

func (r *Repo) CreateFriendRequestNotification(toUserID, fromUserID uint) (*models.UserNotification, error) {
	pendingStatus := "pending"
	n := &models.UserNotification{
		UserID:     toUserID,
		Type:       "friend_request",
		SourceType: "friend",
		AuthorID:   &fromUserID,
		Status:     &pendingStatus,
		Read:       false,
		CreatedAt:  time.Now(),
	}
	if err := r.DB.Create(n).Error; err != nil {
		return nil, err
	}
	return n, nil
}

func (r *Repo) UpdateNotificationStatus(notificationID uint, userID uint, status string) error {
	return r.DB.Model(&models.UserNotification{}).
		Where("id = ? AND user_id = ?", notificationID, userID).
		Update("status", status).Error
}

func (r *Repo) GetNotificationByID(notificationID uint, userID uint) (*models.UserNotification, error) {
	var n models.UserNotification
	if err := r.DB.Where("id = ? AND user_id = ?", notificationID, userID).First(&n).Error; err != nil {
		return nil, err
	}
	return &n, nil
}

func (r *Repo) CreateGuildJoinRequestNotification(ownerID, applicantID, guildID uint) (*models.UserNotification, error) {
	pendingStatus := "pending"
	n := &models.UserNotification{
		UserID:     ownerID,
		Type:       "guild_join_request",
		SourceType: "guild",
		GuildID:    &guildID,
		AuthorID:   &applicantID,
		Status:     &pendingStatus,
		Read:       false,
		CreatedAt:  time.Now(),
	}
	if err := r.DB.Create(n).Error; err != nil {
		return nil, err
	}
	return n, nil
}

func (r *Repo) CreateApplication(a *models.UserApplication) error {
	return r.DB.Create(a).Error
}

func (r *Repo) UpdateApplicationStatus(id uint, toUserID uint, status string) (*models.UserApplication, error) {
	var app models.UserApplication
	if err := r.DB.Where("id = ? AND to_user_id = ?", id, toUserID).First(&app).Error; err != nil {
		return nil, err
	}
	app.Status = status
	app.UpdatedAt = time.Now()
	if err := r.DB.Model(&models.UserApplication{}).Where("id = ? AND to_user_id = ?", id, toUserID).Updates(map[string]any{
		"status":     app.Status,
		"updated_at": app.UpdatedAt,
	}).Error; err != nil {
		return nil, err
	}
	return &app, nil
}
