package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"bubble/src/config"
	"bubble/src/db/models"

	"gorm.io/datatypes"
)

// ==================== Channel Messages ====================

func (s *Service) AddMessage(channelID, authorID uint, author, content string, replyToID *uint, msgType, platform string, fileMeta datatypes.JSON, tempID string, mentions datatypes.JSON) (*models.Message, error) {
	content = strings.TrimSpace(content)
	if content == "" && msgType == "text" {
		return nil, ErrBadRequest
	}
	if replyToID != nil {
		orig, err := s.Repo.GetMessage(*replyToID)
		if err != nil {
			return nil, ErrNotFound
		}
		if orig.ChannelID != channelID {
			return nil, ErrBadRequest
		}
	}
	if platform == "" {
		platform = "web"
	}
	m := &models.Message{ChannelID: channelID, AuthorID: authorID, Author: author, Content: content, ReplyToID: replyToID, Type: msgType, Platform: platform, TempID: tempID}
	if len(fileMeta) != 0 {
		m.FileMeta = fileMeta
	}
	if len(mentions) != 0 {
		m.Mentions = mentions
	}
	return m, s.Repo.AddMessage(m)
}

func (s *Service) GetMessages(channelID uint, limit int, beforeID, afterID uint) ([]models.Message, error) {
	msgs, err := s.Repo.GetMessages(channelID, limit, beforeID, afterID)
	if err != nil {
		return nil, err
	}
	s.populateMessageReactions(msgs)
	return msgs, nil
}

// GetMessagesWithUsers 获取频道消息列表，并返回相关的用户数据
func (s *Service) GetMessagesWithUsers(channelID uint, limit int, beforeID, afterID uint) ([]models.Message, []models.User, error) {
	msgs, users, err := s.Repo.GetMessagesWithUsers(channelID, limit, beforeID, afterID)
	if err != nil {
		return nil, nil, err
	}
	s.populateMessageReactions(msgs)
	return msgs, users, nil
}

// DeleteMessage 撤回频道消息
func (s *Service) DeleteMessage(messageID, userID uint) error {
	msg, err := s.Repo.GetMessage(messageID)
	if err != nil {
		return ErrNotFound
	}

	if msg.DeletedAt != nil {
		return &Err{Code: 400, Msg: "消息已被撤回"}
	}

	canDelete := false
	if msg.AuthorID == userID {
		canDelete = true
	} else {
		channel, err := s.Repo.GetChannel(msg.ChannelID)
		if err == nil {
			guild, err := s.Repo.GetGuild(channel.GuildID)
			if err == nil {
				if guild.OwnerID == userID {
					canDelete = true
				} else {
					perms, err := s.EffectiveGuildPerms(channel.GuildID, userID)
					if err == nil && (perms&PermManageMessages) == PermManageMessages {
						canDelete = true
					}
				}
			}
		}
	}

	if !canDelete {
		return ErrUnauthorized
	}

	pinned, _ := s.Repo.GetPinnedMessage(messageID)
	if pinned != nil {
		return &Err{Code: 400, Msg: "已精华消息无法删除"}
	}

	return s.Repo.DeleteMessage(messageID)
}

// ==================== MessageReactions ====================

// AddReaction 为消息添加表态
func (s *Service) AddReaction(messageID, userID uint, emoji string) (*models.MessageReaction, error) {
	msg, err := s.Repo.GetMessage(messageID)
	if err != nil {
		return nil, ErrNotFound
	}

	ch, err := s.Repo.GetChannel(msg.ChannelID)
	if err != nil {
		return nil, ErrNotFound
	}
	hasPerm, err := s.HasGuildPerm(ch.GuildID, userID, PermViewChannel)
	if err != nil || !hasPerm {
		return nil, ErrUnauthorized
	}

	count, err := s.Repo.CountReactionEmojis(messageID)
	if err != nil {
		return nil, err
	}
	if count >= int64(config.MaxReactionsPerMessage) {
		existing, _ := s.Repo.ListReactions(messageID)
		emojiExists := false
		for _, r := range existing {
			if r.Emoji == emoji {
				emojiExists = true
				break
			}
		}
		if !emojiExists {
			return nil, &Err{Code: 400, Msg: "该消息的表态种类已达上限"}
		}
	}

	return s.Repo.AddReaction(messageID, userID, emoji)
}

// RemoveReaction 移除消息表态
func (s *Service) RemoveReaction(messageID, userID uint, emoji string) error {
	msg, err := s.Repo.GetMessage(messageID)
	if err != nil {
		return ErrNotFound
	}

	ch, err := s.Repo.GetChannel(msg.ChannelID)
	if err != nil {
		return ErrNotFound
	}
	hasPerm, err := s.HasGuildPerm(ch.GuildID, userID, PermViewChannel)
	if err != nil || !hasPerm {
		return ErrUnauthorized
	}

	return s.Repo.RemoveReaction(messageID, userID, emoji)
}

// populateMessageReactions 批量为消息填充聚合后的 reactions
func (s *Service) populateMessageReactions(msgs []models.Message) {
	if len(msgs) == 0 {
		return
	}
	ids := make([]uint, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	reactions, err := s.Repo.ListReactionsByMessageIDs(ids)
	if err != nil || len(reactions) == 0 {
		return
	}
	// 按 messageID + emoji 聚合
	type key struct {
		msgID uint
		emoji string
	}
	order := make(map[uint][]string) // messageID -> emoji 出现顺序
	groups := make(map[key]*models.AggregatedReaction)
	for _, r := range reactions {
		k := key{r.MessageID, r.Emoji}
		g, ok := groups[k]
		if !ok {
			g = &models.AggregatedReaction{Emoji: r.Emoji}
			groups[k] = g
			order[r.MessageID] = append(order[r.MessageID], r.Emoji)
		}
		g.Count++
		g.Users = append(g.Users, r.UserID)
	}
	// 填充到消息上
	msgIndex := make(map[uint]int, len(msgs))
	for i := range msgs {
		msgIndex[msgs[i].ID] = i
	}
	for msgID, emojis := range order {
		idx, ok := msgIndex[msgID]
		if !ok {
			continue
		}
		agg := make([]models.AggregatedReaction, 0, len(emojis))
		for _, emoji := range emojis {
			k := key{msgID, emoji}
			if g, ok := groups[k]; ok {
				agg = append(agg, *g)
			}
		}
		msgs[idx].Reactions = agg
	}
}

// ListReactions 获取消息的所有表态（聚合格式）
func (s *Service) ListReactions(messageID uint) ([]map[string]any, error) {
	reactions, err := s.Repo.ListReactions(messageID)
	if err != nil {
		return nil, err
	}

	type emojiGroup struct {
		Emoji string           `json:"emoji"`
		Count int              `json:"count"`
		Users []map[string]any `json:"users"`
	}
	groups := make(map[string]*emojiGroup)
	order := make([]string, 0)

	for _, r := range reactions {
		g, ok := groups[r.Emoji]
		if !ok {
			g = &emojiGroup{Emoji: r.Emoji}
			groups[r.Emoji] = g
			order = append(order, r.Emoji)
		}
		g.Count++
		g.Users = append(g.Users, map[string]any{
			"userId":    r.UserID,
			"createdAt": r.CreatedAt,
		})
	}

	result := make([]map[string]any, 0, len(order))
	for _, emoji := range order {
		g := groups[emoji]
		result = append(result, map[string]any{
			"emoji": g.Emoji,
			"count": g.Count,
			"users": g.Users,
		})
	}
	return result, nil
}

// ==================== PinnedMessages ====================

func (s *Service) PinMessage(channelID, messageID, userID uint) (*models.PinnedMessage, error) {
	msg, err := s.Repo.GetMessage(messageID)
	if err != nil {
		return nil, ErrNotFound
	}
	if msg.ChannelID != channelID {
		return nil, ErrBadRequest
	}

	existing, _ := s.Repo.GetPinnedMessage(messageID)
	if existing != nil {
		return nil, &Err{Code: 400, Msg: "消息已精华"}
	}

	ch, err := s.Repo.GetChannel(channelID)
	if err != nil {
		return nil, err
	}
	hasPerm, err := s.HasGuildPerm(ch.GuildID, userID, PermManageChannels)
	if err != nil {
		return nil, err
	}
	if !hasPerm {
		guild, err := s.Repo.GetGuild(ch.GuildID)
		if err != nil {
			return nil, err
		}
		if guild.OwnerID != userID {
			return nil, ErrUnauthorized
		}
	}

	pm := &models.PinnedMessage{
		MessageID: messageID,
		ChannelID: &channelID,
		GuildID:   &ch.GuildID,
		PinnedBy:  userID,
		CreatedAt: time.Now(),
	}
	if err := s.Repo.CreatePinnedMessage(pm); err != nil {
		return nil, err
	}
	return pm, nil
}

func (s *Service) UnpinMessage(messageID, userID uint) error {
	pm, err := s.Repo.GetPinnedMessage(messageID)
	if err != nil {
		return ErrNotFound
	}

	if pm.ChannelID != nil {
		ch, err := s.Repo.GetChannel(*pm.ChannelID)
		if err != nil {
			return err
		}
		hasPerm, err := s.HasGuildPerm(ch.GuildID, userID, PermManageChannels)
		if err != nil {
			return err
		}
		if !hasPerm {
			guild, err := s.Repo.GetGuild(ch.GuildID)
			if err != nil {
				return err
			}
			if guild.OwnerID != userID && pm.PinnedBy != userID {
				return ErrUnauthorized
			}
		}
	} else if pm.ThreadID != nil {
		thread, err := s.Repo.GetDmThread(*pm.ThreadID)
		if err != nil {
			return ErrNotFound
		}
		if thread.UserAID != userID && thread.UserBID != userID && pm.PinnedBy != userID {
			return ErrUnauthorized
		}
	}

	return s.Repo.DeletePinnedMessage(messageID)
}

func (s *Service) ListPinnedMessages(channelID uint) ([]models.Message, error) {
	pinnedList, err := s.Repo.ListPinnedMessages(channelID)
	if err != nil {
		return nil, err
	}

	type messageWithPinnedTime struct {
		message    models.Message
		pinnedTime time.Time
	}

	var messagesWithTime []messageWithPinnedTime

	for _, pm := range pinnedList {
		msg, err := s.Repo.GetMessage(pm.MessageID)
		if err != nil {
			continue
		}
		if msg.DeletedAt != nil {
			continue
		}
		if msg.ReplyToID != nil {
			replyTo, _ := s.Repo.GetMessage(*msg.ReplyToID)
			if replyTo != nil && replyTo.DeletedAt == nil {
				msg.ReplyTo = replyTo
			}
		}
		messagesWithTime = append(messagesWithTime, messageWithPinnedTime{
			message:    *msg,
			pinnedTime: pm.CreatedAt,
		})
	}

	sort.Slice(messagesWithTime, func(i, j int) bool {
		return messagesWithTime[i].pinnedTime.After(messagesWithTime[j].pinnedTime)
	})

	messages := make([]models.Message, len(messagesWithTime))
	for i, mwt := range messagesWithTime {
		messages[i] = mwt.message
	}

	return messages, nil
}

// ============ 收藏功能 ============

// FavoriteMessage 收藏频道消息
func (s *Service) FavoriteMessage(userID, messageID uint) error {
	msg, err := s.Repo.GetMessage(messageID)
	if err != nil || msg == nil {
		return ErrNotFound
	}

	existing, _ := s.Repo.GetFavoriteMessage(userID, messageID, 0)
	if existing != nil {
		return &Err{Code: 400, Msg: "已收藏"}
	}

	fm := &models.FavoriteMessage{
		UserID:      userID,
		MessageID:   &messageID,
		MessageType: "channel",
		CreatedAt:   time.Now(),
	}
	return s.Repo.CreateFavoriteMessage(fm)
}

// FavoriteDmMessage 收藏私聊消息
func (s *Service) FavoriteDmMessage(userID, dmMessageID uint) error {
	msg, err := s.Repo.GetDmMessage(dmMessageID)
	if err != nil || msg == nil {
		return ErrNotFound
	}

	thread, err := s.Repo.GetDmThread(msg.ThreadID)
	if err != nil {
		return ErrNotFound
	}
	if thread.UserAID != userID && thread.UserBID != userID {
		return ErrUnauthorized
	}

	existing, _ := s.Repo.GetFavoriteMessage(userID, 0, dmMessageID)
	if existing != nil {
		return &Err{Code: 400, Msg: "已收藏"}
	}

	fm := &models.FavoriteMessage{
		UserID:      userID,
		DmMessageID: &dmMessageID,
		MessageType: "dm",
		CreatedAt:   time.Now(),
	}
	return s.Repo.CreateFavoriteMessage(fm)
}

// UnfavoriteMessage 取消收藏
func (s *Service) UnfavoriteMessage(userID, favoriteID uint) error {
	fm, err := s.Repo.GetFavoriteByID(favoriteID)
	if err != nil || fm == nil {
		return ErrNotFound
	}
	if fm.UserID != userID {
		return ErrUnauthorized
	}
	return s.Repo.DeleteFavoriteMessage(favoriteID)
}

// ListFavorites 获取用户的收藏列表
func (s *Service) ListFavorites(userID uint, limit int, filterType string) ([]models.FavoriteMessage, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	favorites, err := s.Repo.ListFavoriteMessages(userID, limit)
	if err != nil {
		return nil, err
	}

	filtered := make([]models.FavoriteMessage, 0, len(favorites))
	for i := range favorites {
		if favorites[i].MessageType == "channel" && favorites[i].MessageID != nil {
			msg, _ := s.Repo.GetMessage(*favorites[i].MessageID)
			favorites[i].Message = msg
			if shouldIncludeMessage(msg, filterType) {
				filtered = append(filtered, favorites[i])
			}
		} else if favorites[i].MessageType == "dm" && favorites[i].DmMessageID != nil {
			msg, _ := s.Repo.GetDmMessage(*favorites[i].DmMessageID)
			favorites[i].DmMessage = msg
			if shouldIncludeDmMessage(msg, filterType) {
				filtered = append(filtered, favorites[i])
			}
		} else if favorites[i].MessageType == "group" && favorites[i].GroupMessageID != nil {
			msg, _ := s.Repo.GetGroupMessage(*favorites[i].GroupMessageID)
			favorites[i].GroupMessage = msg
			if shouldIncludeGroupMessage(msg, filterType) {
				filtered = append(filtered, favorites[i])
			}
		}
	}

	return filtered, nil
}

// shouldIncludeMessage 判断频道消息是否符合过滤条件
func shouldIncludeMessage(msg *models.Message, filterType string) bool {
	if msg == nil || filterType == "" || filterType == "all" {
		return true
	}
	switch filterType {
	case "image":
		return msg.Type == "image"
	case "video":
		return msg.Type == "video"
	case "file":
		return msg.Type == "file"
	default:
		return true
	}
}

// shouldIncludeDmMessage 判断私聊消息是否符合过滤条件
func shouldIncludeDmMessage(msg *models.DmMessage, filterType string) bool {
	if msg == nil || filterType == "" || filterType == "all" {
		return true
	}
	switch filterType {
	case "image":
		return msg.Type == "image"
	case "video":
		return msg.Type == "video"
	case "file":
		return msg.Type == "file"
	default:
		return true
	}
}

// shouldIncludeGroupMessage 判断群聊消息是否符合过滤条件
func shouldIncludeGroupMessage(msg *models.GroupMessage, filterType string) bool {
	if msg == nil || filterType == "" || filterType == "all" {
		return true
	}
	switch filterType {
	case "image":
		return msg.Type == "image"
	case "video":
		return msg.Type == "video"
	case "file":
		return msg.Type == "file"
	default:
		return true
	}
}

// SendFavoriteToChannel 将收藏的消息发送到指定频道
func (s *Service) SendFavoriteToChannel(userID, favoriteID, channelID uint) (*models.Message, error) {
	favorite, err := s.Repo.GetFavoriteByID(favoriteID)
	if err != nil {
		return nil, ErrNotFound
	}
	if favorite.UserID != userID {
		return nil, &Err{Code: 403, Msg: "无权访问该收藏"}
	}

	// 获取原始消息内容（支持频道和私聊收藏）
	var content string
	var msgType string
	var fileMeta datatypes.JSON
	var mentions datatypes.JSON

	if favorite.MessageType == "channel" && favorite.MessageID != nil {
		originalMsg, err := s.Repo.GetMessage(*favorite.MessageID)
		if err != nil || originalMsg == nil {
			return nil, &Err{Code: 404, Msg: "原始消息不存在"}
		}
		content = originalMsg.Content
		msgType = originalMsg.Type
		fileMeta = originalMsg.FileMeta
		mentions = originalMsg.Mentions
	} else if favorite.MessageType == "dm" && favorite.DmMessageID != nil {
		originalDmMsg, err := s.Repo.GetDmMessage(*favorite.DmMessageID)
		if err != nil || originalDmMsg == nil {
			return nil, &Err{Code: 404, Msg: "原始消息不存在"}
		}
		content = originalDmMsg.Content
		msgType = originalDmMsg.Type
		fileMeta = originalDmMsg.FileMeta
		mentions = originalDmMsg.Mentions
	} else if favorite.MessageType == "group" && favorite.GroupMessageID != nil {
		originalGroupMsg, err := s.Repo.GetGroupMessage(*favorite.GroupMessageID)
		if err != nil || originalGroupMsg == nil {
			return nil, &Err{Code: 404, Msg: "原始消息不存在"}
		}
		content = originalGroupMsg.Content
		msgType = originalGroupMsg.Type
		fileMeta = originalGroupMsg.FileMeta
		mentions = originalGroupMsg.Mentions
	} else {
		return nil, &Err{Code: 400, Msg: "无效的收藏类型"}
	}

	channel, err := s.Repo.GetChannel(channelID)
	if err != nil || channel == nil {
		return nil, &Err{Code: 404, Msg: "频道不存在"}
	}
	member, _ := s.Repo.GetMemberByGuildAndUser(channel.GuildID, userID)
	if member == nil {
		return nil, &Err{Code: 403, Msg: "无权在该频道发送消息"}
	}

	newMsg := &models.Message{
		ChannelID: channelID,
		AuthorID:  userID,
		Content:   content,
		Type:      msgType,
		Platform:  "web",
		FileMeta:  fileMeta,
		Mentions:  mentions,
	}
	if err := s.Repo.DB.Create(newMsg).Error; err != nil {
		return nil, &Err{Code: 500, Msg: "发送消息失败"}
	}

	// 预加载作者信息
	if usr, err := s.GetUserByID(userID); err == nil && usr != nil {
		newMsg.Author = usr.Name
	}

	return newMsg, nil
}

// SendFavoriteToDm 将收藏的消息发送到指定私聊
func (s *Service) SendFavoriteToDm(userID, favoriteID, threadID uint) (*models.DmMessage, error) {
	favorite, err := s.Repo.GetFavoriteByID(favoriteID)
	if err != nil {
		return nil, ErrNotFound
	}
	if favorite.UserID != userID {
		return nil, &Err{Code: 403, Msg: "无权访问该收藏"}
	}

	// 获取原始消息内容（支持频道和私聊收藏）
	var content string
	var msgType string
	var fileMeta datatypes.JSON
	var mentions datatypes.JSON

	if favorite.MessageType == "channel" && favorite.MessageID != nil {
		originalMsg, err := s.Repo.GetMessage(*favorite.MessageID)
		if err != nil || originalMsg == nil {
			return nil, &Err{Code: 404, Msg: "原始消息不存在"}
		}
		content = originalMsg.Content
		msgType = originalMsg.Type
		fileMeta = originalMsg.FileMeta
		mentions = originalMsg.Mentions
	} else if favorite.MessageType == "dm" && favorite.DmMessageID != nil {
		originalDmMsg, err := s.Repo.GetDmMessage(*favorite.DmMessageID)
		if err != nil || originalDmMsg == nil {
			return nil, &Err{Code: 404, Msg: "原始消息不存在"}
		}
		content = originalDmMsg.Content
		msgType = originalDmMsg.Type
		fileMeta = originalDmMsg.FileMeta
		mentions = originalDmMsg.Mentions
	} else if favorite.MessageType == "group" && favorite.GroupMessageID != nil {
		originalGroupMsg, err := s.Repo.GetGroupMessage(*favorite.GroupMessageID)
		if err != nil || originalGroupMsg == nil {
			return nil, &Err{Code: 404, Msg: "原始消息不存在"}
		}
		content = originalGroupMsg.Content
		msgType = originalGroupMsg.Type
		fileMeta = originalGroupMsg.FileMeta
		mentions = originalGroupMsg.Mentions
	} else {
		return nil, &Err{Code: 400, Msg: "无效的收藏类型"}
	}

	thread, err := s.Repo.GetDmThread(threadID)
	if err != nil || thread == nil {
		return nil, &Err{Code: 404, Msg: "私聊线程不存在"}
	}
	if thread.UserAID != userID && thread.UserBID != userID {
		return nil, &Err{Code: 403, Msg: "无权在该私聊发送消息"}
	}

	newDmMsg := &models.DmMessage{
		ThreadID: threadID,
		AuthorID: userID,
		Content:  content,
		Type:     msgType,
		Platform: "web",
		FileMeta: fileMeta,
		Mentions: mentions,
	}
	if err := s.Repo.DB.Create(newDmMsg).Error; err != nil {
		return nil, &Err{Code: 500, Msg: "发送消息失败"}
	}

	s.Repo.DB.Model(thread).Update("last_message_at", time.Now())

	return newDmMsg, nil
}

// ==================== GuildMedia ====================

// GetGuildMediaByType 获取服务器的媒体文件（图片/视频/文件）
func (s *Service) GetGuildMediaByType(guildID uint, mediaType string, limit int, before uint) ([]models.Message, error) {
	guild, err := s.Repo.GetGuild(guildID)
	if err != nil || guild == nil {
		return nil, ErrNotFound
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return s.Repo.GetGuildMediaByType(guildID, mediaType, limit, before)
}

// DeleteGuildMedia 批量删除服务器媒体文件（仅 owner 可操作）
func (s *Service) DeleteGuildMedia(guildID, userID uint, messageIDs []uint) (int, error) {
	guild, err := s.Repo.GetGuild(guildID)
	if err != nil || guild == nil {
		return 0, ErrNotFound
	}
	if guild.OwnerID != userID {
		return 0, ErrUnauthorized
	}
	return s.Repo.DeleteGuildMessages(guildID, messageIDs)
}

// ==================== GuildFiles ====================

// UploadGuildFile 上传文件到服务器文件系统
func (s *Service) UploadGuildFile(guildID, uploaderID uint, fileName, filePath, contentType string, fileSize int64) (*models.GuildFile, error) {
	isMember, err := s.Repo.IsMember(guildID, uploaderID)
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, ErrUnauthorized
	}

	guild, err := s.Repo.GetGuild(guildID)
	if err != nil || guild == nil {
		return nil, ErrNotFound
	}

	if !guild.AllowMemberUpload {
		hasPerm, err := s.HasGuildPerm(guildID, uploaderID, PermManageFiles)
		if err != nil {
			return nil, err
		}
		if !hasPerm {
			hasManage, err := s.HasGuildPerm(guildID, uploaderID, PermManageGuild)
			if err != nil {
				return nil, err
			}
			if !hasManage && guild.OwnerID != uploaderID {
				return nil, &Err{Code: 403, Msg: "仅管理员可上传文件，可在管理页开启允许任何人上传"}
			}
		}
	}

	count, err := s.Repo.CountGuildFiles(guildID)
	if err != nil {
		return nil, err
	}
	if count >= config.MaxGuildFiles {
		return nil, &Err{Code: 400, Msg: "服务器文件数量已达上限"}
	}

	f := &models.GuildFile{
		GuildID:     guildID,
		UploaderID:  uploaderID,
		FileName:    fileName,
		FilePath:    filePath,
		FileSize:    fileSize,
		ContentType: contentType,
	}
	if err := s.Repo.CreateGuildFile(f); err != nil {
		return nil, err
	}
	return f, nil
}

// ListGuildFiles 列出服务器文件（任何成员可查看）
func (s *Service) ListGuildFiles(guildID, userID uint, limit int, before uint) ([]models.GuildFile, error) {
	isMember, err := s.Repo.IsMember(guildID, userID)
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, ErrUnauthorized
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	files, err := s.Repo.ListGuildFiles(guildID, limit, before)
	if err != nil {
		return nil, err
	}
	if s.MinIO != nil {
		for i := range files {
			files[i].FileURL = s.MinIO.GetFileURL(files[i].FilePath)
		}
	}
	return files, nil
}

// RenameGuildFile 重命名服务器文件（上传者本人、文件管理权限、管理员或群主）
func (s *Service) RenameGuildFile(fileID, userID uint, newName string) (*models.GuildFile, error) {
	f, err := s.Repo.GetGuildFile(fileID)
	if err != nil {
		return nil, ErrNotFound
	}
	// 权限检查：上传者本人 or 有管理权限
	if f.UploaderID != userID {
		hasPerm, err := s.HasGuildPerm(f.GuildID, userID, PermManageFiles)
		if err != nil {
			return nil, err
		}
		if !hasPerm {
			hasManage, err := s.HasGuildPerm(f.GuildID, userID, PermManageGuild)
			if err != nil {
				return nil, err
			}
			if !hasManage {
				guild, err := s.Repo.GetGuild(f.GuildID)
				if err != nil {
					return nil, err
				}
				if guild.OwnerID != userID {
					return nil, ErrUnauthorized
				}
			}
		}
	}
	if err := s.Repo.UpdateGuildFileName(fileID, newName); err != nil {
		return nil, err
	}
	f.FileName = newName
	if s.MinIO != nil {
		f.FileURL = s.MinIO.GetFileURL(f.FilePath)
	}
	return f, nil
}

// DeleteGuildFile 删除服务器文件（上传者本人、管理员或群主）
func (s *Service) DeleteGuildFile(fileID, userID uint) error {
	f, err := s.Repo.GetGuildFile(fileID)
	if err != nil {
		return ErrNotFound
	}
	if f.UploaderID != userID {
		hasPerm, err := s.HasGuildPerm(f.GuildID, userID, PermManageFiles)
		if err != nil {
			return err
		}
		if !hasPerm {
			hasManage, err := s.HasGuildPerm(f.GuildID, userID, PermManageGuild)
			if err != nil {
				return err
			}
			if !hasManage {
				guild, err := s.Repo.GetGuild(f.GuildID)
				if err != nil {
					return err
				}
				if guild.OwnerID != userID {
					return ErrUnauthorized
				}
			}
		}
	}
	if s.MinIO != nil {
		_ = s.MinIO.DeleteFile(context.Background(), f.FilePath)
	}
	return s.Repo.DeleteGuildFile(fileID)
}
