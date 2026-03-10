package service

import (
	"bubble/src/db/models"
	"sort"
	"strings"
	"time"

	"gorm.io/datatypes"
)

// --- Direct Messages ---
// OpenDm 创建或获取私聊线程
// 验证规则：
// 1. 允许和自己创建线程（笔记本）
// 2. 如果是好友，直接允许
// 3. 如果传入guildId，检查是否在同一服务器（允许创建）
// 4. 检查对方的私聊隐私设置：friends_only 或 everyone
// 5. 其他情况禁止创建
func (s *Service) OpenDm(meID, otherID uint, guildID *uint) (*models.DmThread, error) {
	// 1. 允许和自己创建（笔记本）
	if meID == otherID {
		return s.Repo.GetOrCreateDmThread(meID, otherID)
	}

	// 2. 检查发起方是否为机器人，机器人可绕过隐私限制
	meUser, err := s.Repo.GetUserByID(meID)
	if err != nil || meUser == nil {
		return nil, ErrNotFound
	}
	if meUser.IsBot {
		return s.Repo.GetOrCreateDmThread(meID, otherID)
	}

	// 3. 检查对方是否存在
	otherUser, err := s.Repo.GetUserByID(otherID)
	if err != nil || otherUser == nil {
		return nil, ErrNotFound
	}

	// 4. 如果是好友，直接允许
	if s.AreFriends(meID, otherID) {
		return s.Repo.GetOrCreateDmThread(meID, otherID)
	}

	// 5. 如果传入guildId，检查是否在同一服务器
	if guildID != nil {
		// 检查双方是否都在该服务器
		myMember, _ := s.Repo.IsMember(*guildID, meID)
		otherMember, _ := s.Repo.IsMember(*guildID, otherID)
		if myMember && otherMember {
			// 在同一服务器，允许创建
			return s.Repo.GetOrCreateDmThread(meID, otherID)
		}
	}

	// 6. 检查对方的私聊隐私设置
	dmPrivacyMode := otherUser.DmPrivacyMode
	if dmPrivacyMode == "" {
		dmPrivacyMode = "friends_only" // 默认仅好友
	}

	if dmPrivacyMode == "everyone" {
		// 对方允许任何人创建私聊
		return s.Repo.GetOrCreateDmThread(meID, otherID)
	}

	// dmPrivacyMode == "friends_only" 且不是好友，禁止创建
	return nil, &Err{Code: 403, Msg: "对方设置为仅好友可发起私聊"}
}

// NOTE: ListDmThreads with pagination is now in pagination_service.go

func (s *Service) SendDm(meID, threadID uint, content string, replyToID *uint, msgType, platform string, fileMeta datatypes.JSON, tempID string) (*models.DmMessage, error) {
	content = strings.TrimSpace(content)
	if content == "" && msgType == "text" {
		return nil, ErrBadRequest
	}
	// participant check
	var t models.DmThread
	if err := s.Repo.DB.First(&t, threadID).Error; err != nil {
		return nil, ErrNotFound
	}
	if !(t.UserAID == meID || t.UserBID == meID) {
		return nil, ErrUnauthorized
	}
	// 检查对方是否屏蔽了我的消息
	if t.UserAID == meID && t.BlockedByUserB {
		return nil, &Err{Code: 403, Msg: "对方屏蔽了你的消息"}
	}
	if t.UserBID == meID && t.BlockedByUserA {
		return nil, &Err{Code: 403, Msg: "对方屏蔽了你的消息"}
	}
	// If replying, check if original message exists and is in the same thread
	if replyToID != nil {
		orig, err := s.Repo.GetDmMessage(*replyToID)
		if err != nil {
			return nil, ErrNotFound
		}
		if orig.ThreadID != threadID {
			return nil, ErrBadRequest
		}
	}
	// 默认为web
	if platform == "" {
		platform = "web"
	}
	m := &models.DmMessage{
		ThreadID:  threadID,
		AuthorID:  meID,
		Content:   content,
		ReplyToID: replyToID,
		Type:      msgType,
		Platform:  platform,
		TempID:    tempID,
	}
	if len(fileMeta) != 0 {
		m.FileMeta = fileMeta
	}
	return s.Repo.AddDmMessage(m)
}

func (s *Service) GetDmMessages(meID, threadID uint, limit int, beforeID, afterID uint) ([]models.DmMessage, error) {
	// participant check
	var t models.DmThread
	if err := s.Repo.DB.First(&t, threadID).Error; err != nil {
		return nil, ErrNotFound
	}
	if !(t.UserAID == meID || t.UserBID == meID) {
		return nil, ErrUnauthorized
	}
	msgs, err := s.Repo.GetDmMessages(threadID, limit, beforeID, afterID)
	if err != nil {
		return nil, err
	}
	s.populateDmMessageReactions(msgs)
	return msgs, nil
}

// GetDmMessagesWithUsers 获取私聊消息列表，并返回相关的用户数据
func (s *Service) GetDmMessagesWithUsers(meID, threadID uint, limit int, beforeID, afterID uint) ([]models.DmMessage, []models.User, error) {
	// participant check
	var t models.DmThread
	if err := s.Repo.DB.First(&t, threadID).Error; err != nil {
		return nil, nil, ErrNotFound
	}
	if !(t.UserAID == meID || t.UserBID == meID) {
		return nil, nil, ErrUnauthorized
	}
	msgs, users, err := s.Repo.GetDmMessagesWithUsers(threadID, limit, beforeID, afterID)
	if err != nil {
		return nil, nil, err
	}
	s.populateDmMessageReactions(msgs)
	return msgs, users, nil
}

// populateDmMessageReactions 批量为私聊消息填充聚合后的 reactions
func (s *Service) populateDmMessageReactions(msgs []models.DmMessage) {
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
	type key struct {
		msgID uint
		emoji string
	}
	order := make(map[uint][]string)
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

// DeleteDmMessage 撤回私聊消息（只能撤回自己的消息）
func (s *Service) DeleteDmMessage(messageID, userID uint) error {
	msg, err := s.Repo.GetDmMessage(messageID)
	if err != nil {
		return ErrNotFound
	}
	// 检查用户是否是线程参与者
	thread, err := s.Repo.GetDmThread(msg.ThreadID)
	if err != nil {
		return ErrNotFound
	}
	if thread.UserAID != userID && thread.UserBID != userID {
		return ErrUnauthorized
	}
	// 只能撤回自己的消息
	if msg.AuthorID != userID {
		return ErrUnauthorized
	}
	// 检查是否已经撤回
	if msg.DeletedAt != nil {
		return &Err{Code: 400, Msg: "消息已被撤回"}
	}
	// 检查消息是否被标记为精华
	pinned, _ := s.Repo.GetPinnedMessage(messageID)
	if pinned != nil {
		return &Err{Code: 400, Msg: "已精华消息无法删除"}
	}
	return s.Repo.DeleteDmMessage(messageID)
}

// PinDmThread 置顶/取消置顶私聊线程
func (s *Service) PinDmThread(threadID, userID uint, pinned bool) error {
	// 验证用户是否是该线程的参与者
	thread, err := s.Repo.GetDmThread(threadID)
	if err != nil {
		return ErrNotFound
	}
	if thread.UserAID != userID && thread.UserBID != userID {
		return ErrForbidden
	}
	return s.Repo.PinDmThread(threadID, userID, pinned)
}

// ListPinnedDmThreads 列出用户置顶的私聊线程
func (s *Service) ListPinnedDmThreads(userID uint) ([]models.DmThread, error) {
	return s.Repo.ListPinnedDmThreads(userID)
}

// BlockDmThread 屏蔽/取消屏蔽私聊消息
func (s *Service) BlockDmThread(threadID, userID uint, blocked bool) error {
	// 验证用户是否是该线程的参与者
	thread, err := s.Repo.GetDmThread(threadID)
	if err != nil {
		return ErrNotFound
	}
	if thread.UserAID != userID && thread.UserBID != userID {
		return ErrForbidden
	}
	return s.Repo.BlockDmThread(threadID, userID, blocked)
}

// PinDmMessage PIN私聊消息
func (s *Service) PinDmMessage(threadID, messageID, userID uint) (*models.PinnedMessage, error) {
	// 检查消息是否存在
	msg, err := s.Repo.GetDmMessage(messageID)
	if err != nil {
		return nil, ErrNotFound
	}
	if msg.ThreadID != threadID {
		return nil, ErrBadRequest
	}

	// 检查用户是否是线程参与者
	thread, err := s.Repo.GetDmThread(threadID)
	if err != nil {
		return nil, ErrNotFound
	}
	if thread.UserAID != userID && thread.UserBID != userID {
		return nil, ErrUnauthorized
	}

	// 检查是否已经PIN过
	existing, _ := s.Repo.GetPinnedMessage(messageID)
	if existing != nil {
		return nil, &Err{Code: 400, Msg: "消息已精华"}
	}

	pm := &models.PinnedMessage{
		MessageID: messageID,
		ThreadID:  &threadID,
		PinnedBy:  userID,
		CreatedAt: time.Now(),
	}
	if err := s.Repo.CreatePinnedMessage(pm); err != nil {
		return nil, err
	}
	return pm, nil
}

func (s *Service) ListPinnedDmMessages(threadID uint) ([]models.DmMessage, error) {
	// 获取精华消息记录（已按置顶时间倒序排序）
	pinnedList, err := s.Repo.ListPinnedMessagesByThread(threadID)
	if err != nil {
		return nil, err
	}

	// 创建一个结构来保存消息和置顶时间
	type messageWithPinnedTime struct {
		message    models.DmMessage
		pinnedTime time.Time
	}

	var messagesWithTime []messageWithPinnedTime

	// 获取实际的消息内容，并过滤已删除的消息
	for _, pm := range pinnedList {
		msg, err := s.Repo.GetDmMessage(pm.MessageID)
		if err != nil {
			continue // 消息不存在，跳过
		}
		// 过滤已删除的消息（SQL查询时已过滤，这里双重保险）
		if msg.DeletedAt != nil {
			continue
		}
		// 预加载 ReplyTo
		if msg.ReplyToID != nil {
			replyTo, _ := s.Repo.GetDmMessage(*msg.ReplyToID)
			if replyTo != nil && replyTo.DeletedAt == nil {
				msg.ReplyTo = replyTo
			}
		}
		messagesWithTime = append(messagesWithTime, messageWithPinnedTime{
			message:    *msg,
			pinnedTime: pm.CreatedAt, // 置顶时间
		})
	}

	// 按置顶时间倒序排序（最新置顶的在前）
	sort.Slice(messagesWithTime, func(i, j int) bool {
		return messagesWithTime[j].pinnedTime.Before(messagesWithTime[i].pinnedTime)
	})

	// 提取消息列表
	messages := make([]models.DmMessage, len(messagesWithTime))
	for i, mwt := range messagesWithTime {
		messages[i] = mwt.message
	}

	return messages, nil
}

// SetDmThreadHidden 设置私信会话隐藏状态
func (s *Service) SetDmThreadHidden(threadID, userID uint, hidden bool) error {
	return s.Repo.SetDmThreadHidden(threadID, userID, hidden)
}

// ListHiddenDmThreads 列出隐藏的私信会话
func (s *Service) ListHiddenDmThreads(userID uint, page, limit int) ([]*models.DmThread, int, error) {
	total, err := s.Repo.CountHiddenDmThreads(userID)
	if err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * limit
	threads, err := s.Repo.ListHiddenDmThreads(userID, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	return threads, total, nil
}
