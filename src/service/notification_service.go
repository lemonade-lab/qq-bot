package service

import (
	"bubble/src/db/models"
	"time"
)

// ===== Personal Notifications & Applications =====

func (s *Service) ListUserNotifications(userID uint, limit int, beforeID, afterID uint) ([]models.UserNotification, error) {
	return s.Repo.ListUserNotifications(userID, limit, beforeID, afterID)
}

func (s *Service) ListUserNotificationsWithInfo(userID uint, limit int, beforeID, afterID uint) ([]models.UserNotification, map[uint]models.User, map[uint]models.Guild, map[uint]models.Channel, map[uint]models.Message, map[uint]models.DmMessage, error) {
	return s.Repo.ListUserNotificationsWithInfo(userID, limit, beforeID, afterID)
}

func (s *Service) ListUserApplications(userID uint, limit int, beforeID, afterID uint) ([]models.UserApplication, error) {
	return s.Repo.ListUserApplications(userID, limit, beforeID, afterID)
}

// ListUserApplicationsWithInfo 获取申请列表及关联信息
func (s *Service) ListUserApplicationsWithInfo(userID uint, limit int, beforeID, afterID uint) ([]models.UserApplication, map[uint]models.User, map[uint]models.Guild, error) {
	return s.Repo.ListUserApplicationsWithInfo(userID, limit, beforeID, afterID)
}

// CreateMentionNotification 创建"被提及"通知并返回记录
func (s *Service) CreateMentionNotification(userID uint, sourceType string, guildID *uint, channelID *uint, threadID *uint, messageID *uint, authorID *uint) (*models.UserNotification, error) {
	n := &models.UserNotification{
		UserID:     userID,
		Type:       "mention",
		SourceType: sourceType,
		GuildID:    guildID,
		ChannelID:  channelID,
		ThreadID:   threadID,
		MessageID:  messageID,
		AuthorID:   authorID,
		Read:       false,
		CreatedAt:  time.Now(),
	}
	if err := s.Repo.CreateNotification(n); err != nil {
		return nil, err
	}
	return n, nil
}

// MarkNotificationRead 标记通知已读
func (s *Service) MarkNotificationRead(id, userID uint) error {
	return s.Repo.MarkNotificationRead(id, userID)
}

// MarkNotificationsRead 批量标记通知已读
func (s *Service) MarkNotificationsRead(ids []uint, userID uint) error {
	return s.Repo.MarkNotificationsRead(ids, userID)
}

// MarkAllNotificationsRead 标记所有通知为已读
func (s *Service) MarkAllNotificationsRead(userID uint) error {
	return s.Repo.MarkAllNotificationsRead(userID)
}

// CreateApplication 创建申请（例如好友或公会加入）
func (s *Service) CreateApplication(typ string, fromUserID, toUserID uint, targetGuildID *uint) (*models.UserApplication, error) {
	a := &models.UserApplication{
		Type:          typ,
		FromUserID:    fromUserID,
		ToUserID:      toUserID,
		TargetGuildID: targetGuildID,
		Status:        "pending",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	if err := s.Repo.CreateApplication(a); err != nil {
		return nil, err
	}
	return a, nil
}

// UpdateApplicationStatus 更新申请状态（由接收者审批）
func (s *Service) UpdateApplicationStatus(id uint, approverUserID uint, status string) (*models.UserApplication, error) {
	// 状态校验
	switch status {
	case "approved", "rejected":
	default:
		return nil, ErrBadRequest
	}
	return s.Repo.UpdateApplicationStatus(id, approverUserID, status)
}

// ===== Read State (红点系统) =====

// GetUnreadCounts 获取用户的所有未读统计
func (s *Service) GetUnreadCounts(userID uint) (map[string]any, error) {
	// 获取所有有未读的阅读状态
	states, err := s.Repo.GetAllReadStatesByUser(userID)
	if err != nil {
		return nil, err
	}

	totalUnread := 0
	totalMentions := 0
	guilds := make(map[uint]map[string]any) // guildID -> {unreadCount, mentionCount, channels}
	dms := make(map[uint]map[string]any)    // threadID -> {unreadCount, mentionCount}
	groups := make(map[uint]map[string]any) // groupThreadID -> {unreadCount, mentionCount}
	channelGuildMap := make(map[uint]uint)  // channelID -> guildID（缓存）

	for _, state := range states {
		totalUnread += state.UnreadCount
		totalMentions += state.MentionCount

		switch state.ResourceType {
		case "channel":
			// 查找频道所属的服务器
			var guildID uint
			if cached, ok := channelGuildMap[state.ResourceID]; ok {
				guildID = cached
			} else {
				ch, err := s.Repo.GetChannel(state.ResourceID)
				if err != nil || ch == nil {
					continue
				}
				guildID = ch.GuildID
				channelGuildMap[state.ResourceID] = guildID
			}

			// 初始化服务器统计
			if guilds[guildID] == nil {
				guilds[guildID] = map[string]any{
					"unreadCount":  0,
					"mentionCount": 0,
					"channels":     make(map[uint]map[string]int),
				}
			}

			// 累加服务器级别统计
			guilds[guildID]["unreadCount"] = guilds[guildID]["unreadCount"].(int) + state.UnreadCount
			guilds[guildID]["mentionCount"] = guilds[guildID]["mentionCount"].(int) + state.MentionCount

			// 添加频道级别统计
			channels := guilds[guildID]["channels"].(map[uint]map[string]int)
			channels[state.ResourceID] = map[string]int{
				"unreadCount":  state.UnreadCount,
				"mentionCount": state.MentionCount,
			}

		case "dm":
			dms[state.ResourceID] = map[string]any{
				"unreadCount":  state.UnreadCount,
				"mentionCount": state.MentionCount,
			}

		case "group":
			groups[state.ResourceID] = map[string]any{
				"unreadCount":  state.UnreadCount,
				"mentionCount": state.MentionCount,
			}

		case "guild":
			// 服务器级别的未读（暂时不使用，预留）
		}
	}

	return map[string]any{
		"total":    totalUnread,
		"mentions": totalMentions,
		"guilds":   guilds,
		"dms":      dms,
		"groups":   groups,
	}, nil
}

// MarkChannelRead 标记频道已读
func (s *Service) MarkChannelRead(userID, channelID, messageID uint) error {
	// 验证用户是否有权限访问该频道
	ch, err := s.Repo.GetChannel(channelID)
	if err != nil || ch == nil {
		return ErrNotFound
	}

	ok, err := s.HasGuildPerm(ch.GuildID, userID, PermViewChannel)
	if err != nil || !ok {
		return ErrForbidden
	}

	// 更新频道级别的已读状态
	if err := s.Repo.UpdateReadState(userID, "channel", channelID, messageID); err != nil {
		return err
	}

	// 重新计算并更新公会级别的已读状态
	// 获取该公会所有频道的未读数
	channels, err := s.Repo.ListChannels(ch.GuildID)
	if err != nil {
		return err
	}

	var totalUnread, totalMentions int
	for _, channel := range channels {
		readState, err := s.Repo.GetReadState(userID, "channel", channel.ID)
		if err != nil {
			continue
		}
		if readState != nil {
			totalUnread += readState.UnreadCount
			totalMentions += readState.MentionCount
		}
	}

	// 更新公会级别的统计（转换为uint）
	return s.Repo.UpdateReadStateCounts(userID, "guild", ch.GuildID, uint(totalUnread), uint(totalMentions))
}

// MarkDmRead 标记私聊已读
func (s *Service) MarkDmRead(userID, threadID, messageID uint) error {
	// 验证用户是否是该私聊线程的参与者
	thread, err := s.Repo.GetDmThread(threadID)
	if err != nil || thread == nil {
		return ErrNotFound
	}

	if thread.UserAID != userID && thread.UserBID != userID {
		return ErrForbidden
	}

	return s.Repo.UpdateReadState(userID, "dm", threadID, messageID)
}

// MarkGuildRead 标记整个服务器的所有频道已读
func (s *Service) MarkGuildRead(userID, guildID uint) error {
	// 验证用户是否是该服务器成员
	isMember, err := s.Repo.IsMember(guildID, userID)
	if err != nil || !isMember {
		return ErrForbidden
	}

	// 获取服务器的所有频道
	channels, err := s.Repo.ListChannels(guildID)
	if err != nil {
		return err
	}

	// 为每个频道标记已读（使用最大消息ID）
	for _, ch := range channels {
		// 获取频道最新消息
		messages, err := s.Repo.GetMessages(ch.ID, 1, 0, 0)
		if err != nil || len(messages) == 0 {
			continue
		}

		latestMessageID := messages[0].ID
		if err := s.Repo.UpdateReadState(userID, "channel", ch.ID, latestMessageID); err != nil {
			return err
		}
	}

	return nil
}

// OnNewChannelMessage 新频道消息时更新未读计数
func (s *Service) OnNewChannelMessage(channelID uint, messageID uint, authorID uint, mentionedUserIDs []uint) error {
	// 获取频道信息
	ch, err := s.Repo.GetChannel(channelID)
	if err != nil || ch == nil {
		return err
	}

	// 获取频道所有成员（排除发送者）
	members, err := s.Repo.ListMembers(ch.GuildID)
	if err != nil {
		return err
	}

	userIDs := make([]uint, 0, len(members))
	for _, m := range members {
		if m.UserID != authorID { // 排除发送者自己
			userIDs = append(userIDs, m.UserID)
		}
	}

	if len(userIDs) == 0 {
		return nil
	}

	// 批量增加未读计数
	return s.Repo.BatchIncrementUnreadCount(userIDs, "channel", channelID, mentionedUserIDs)
}

// OnNewDmMessage 新私聊消息时更新未读计数
func (s *Service) OnNewDmMessage(threadID uint, messageID uint, authorID uint, mentionedUserIDs []uint) error {
	// 获取私聊线程
	thread, err := s.Repo.GetDmThread(threadID)
	if err != nil || thread == nil {
		return err
	}

	// 确定接收者（对方用户）
	var recipientID uint
	if thread.UserAID == authorID {
		recipientID = thread.UserBID
	} else {
		recipientID = thread.UserAID
	}

	// 增加接收者的未读计数
	isMention := false
	for _, uid := range mentionedUserIDs {
		if uid == recipientID {
			isMention = true
			break
		}
	}

	return s.Repo.IncrementUnreadCount(recipientID, "dm", threadID, isMention)
}

// CreateBotJoinRequestNotification 创建 "用户请求将机器人加入服务器" 的通知给服主
func (s *Service) CreateBotJoinRequestNotification(ownerID, requestorID, botUserID, guildID uint) (*models.UserNotification, error) {
	pendingStatus := "pending"
	n := &models.UserNotification{
		UserID:     ownerID,
		Type:       "bot_join_request",
		SourceType: "guild",
		GuildID:    &guildID,
		AuthorID:   &requestorID,
		Status:     &pendingStatus,
		Read:       false,
		CreatedAt:  time.Now(),
	}
	if err := s.Repo.DB.Create(n).Error; err != nil {
		return nil, err
	}
	return n, nil
}
