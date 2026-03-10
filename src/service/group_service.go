package service

import (
	"context"
	"fmt"
	"time"

	"bubble/src/config"
	"bubble/src/db/models"

	"gorm.io/datatypes"
)

// ==================== GroupThread (群聊) ====================

// CreateGroupThread 创建群聊
func (s *Service) CreateGroupThread(ownerID uint, name string, memberIDs []uint) (*models.GroupThread, error) {
	if len([]rune(name)) == 0 || int64(len([]rune(name))) > config.MaxGroupNameLength {
		return nil, &Err{Code: 400, Msg: "群名称长度无效"}
	}

	owned, err := s.Repo.CountOwnedGroups(ownerID)
	if err != nil {
		return nil, err
	}
	if owned >= config.MaxOwnedGroups {
		return nil, &Err{Code: 400, Msg: "创建群聊数量已达上限"}
	}

	uniq := map[uint]bool{ownerID: true}
	for _, id := range memberIDs {
		uniq[id] = true
	}
	if int64(len(uniq)) > config.MaxGroupMembers {
		return nil, &Err{Code: 400, Msg: "群成员数量超出上限"}
	}

	for uid := range uniq {
		if _, err := s.GetUserByID(uid); err != nil {
			return nil, &Err{Code: 400, Msg: fmt.Sprintf("用户 %d 不存在", uid)}
		}
	}

	thread := &models.GroupThread{
		Name:    name,
		OwnerID: ownerID,
	}
	if err := s.Repo.CreateGroupThread(thread); err != nil {
		return nil, err
	}

	for uid := range uniq {
		role := "member"
		if uid == ownerID {
			role = "owner"
		}
		m := &models.GroupThreadMember{
			ThreadID: thread.ID,
			UserID:   uid,
			Role:     role,
		}
		if err := s.Repo.AddGroupMember(m); err != nil {
			return nil, err
		}
	}

	members, _ := s.Repo.ListGroupMembers(thread.ID)
	thread.Members = members
	thread.MemberCount = len(members)

	return thread, nil
}

// GetGroupThread 获取群聊详情（含成员列表）
func (s *Service) GetGroupThread(threadID, userID uint) (*models.GroupThread, error) {
	if !s.Repo.IsGroupMember(threadID, userID) {
		return nil, ErrUnauthorized
	}
	thread, err := s.Repo.GetGroupThread(threadID)
	if err != nil {
		return nil, ErrNotFound
	}

	members, _ := s.Repo.ListGroupMembers(threadID)
	for i := range members {
		if u, err := s.GetUserByID(members[i].UserID); err == nil {
			members[i].User = u
		}
	}
	thread.Members = members
	thread.MemberCount = len(members)

	if m, err := s.Repo.GetGroupMember(threadID, userID); err == nil {
		thread.IsMuted = m.IsMuted
	}

	return thread, nil
}

// UpdateGroupThread 更新群聊信息（群主/管理员）
func (s *Service) UpdateGroupThread(threadID, userID uint, name, avatar, banner, joinMode *string) error {
	m, err := s.Repo.GetGroupMember(threadID, userID)
	if err != nil {
		return ErrUnauthorized
	}
	if m.Role != "owner" && m.Role != "admin" {
		return ErrForbidden
	}
	updates := make(map[string]interface{})
	if name != nil {
		if len([]rune(*name)) == 0 || int64(len([]rune(*name))) > config.MaxGroupNameLength {
			return &Err{Code: 400, Msg: "群名称长度无效"}
		}
		updates["name"] = *name
	}
	if avatar != nil {
		updates["avatar"] = *avatar
	}
	if banner != nil {
		updates["banner"] = *banner
	}
	if joinMode != nil {
		switch *joinMode {
		case "invite_only", "free_join", "need_approval":
			// 仅群主可修改加入方式
			if m.Role != "owner" {
				return &Err{Code: 403, Msg: "仅群主可修改加入方式"}
			}
			updates["join_mode"] = *joinMode
		default:
			return &Err{Code: 400, Msg: "无效的加入方式，可选: invite_only, free_join, need_approval"}
		}
	}
	if len(updates) == 0 {
		return nil
	}
	return s.Repo.UpdateGroupThread(threadID, updates)
}

// DeleteGroupThread 解散群聊（仅群主）
func (s *Service) DeleteGroupThread(threadID, userID uint) error {
	thread, err := s.Repo.GetGroupThread(threadID)
	if err != nil {
		return ErrNotFound
	}
	if thread.OwnerID != userID {
		return ErrForbidden
	}
	return s.Repo.DeleteGroupThread(threadID)
}

// AddGroupMembers 添加群成员（群主/管理员）
func (s *Service) AddGroupMembers(threadID, operatorID uint, userIDs []uint) ([]models.GroupThreadMember, error) {
	opMember, err := s.Repo.GetGroupMember(threadID, operatorID)
	if err != nil {
		return nil, ErrUnauthorized
	}
	if opMember.Role != "owner" && opMember.Role != "admin" {
		return nil, ErrForbidden
	}

	current, _ := s.Repo.CountGroupMembers(threadID)
	if current+int64(len(userIDs)) > config.MaxGroupMembers {
		return nil, &Err{Code: 400, Msg: "群成员数量超出上限"}
	}

	var added []models.GroupThreadMember
	for _, uid := range userIDs {
		if s.Repo.IsGroupMember(threadID, uid) {
			continue
		}
		if _, err := s.GetUserByID(uid); err != nil {
			continue
		}
		m := &models.GroupThreadMember{
			ThreadID: threadID,
			UserID:   uid,
			Role:     "member",
		}
		if err := s.Repo.AddGroupMember(m); err == nil {
			added = append(added, *m)
		}
	}
	return added, nil
}

// RemoveGroupMember 移除群成员 / 退出群聊
func (s *Service) RemoveGroupMember(threadID, operatorID, targetUserID uint) error {
	thread, err := s.Repo.GetGroupThread(threadID)
	if err != nil {
		return ErrNotFound
	}

	if operatorID == targetUserID {
		if thread.OwnerID == operatorID {
			return &Err{Code: 400, Msg: "群主需先转让群主后才能退出"}
		}
		return s.Repo.RemoveGroupMember(threadID, targetUserID)
	}

	opMember, err := s.Repo.GetGroupMember(threadID, operatorID)
	if err != nil {
		return ErrUnauthorized
	}
	targetMember, err := s.Repo.GetGroupMember(threadID, targetUserID)
	if err != nil {
		return ErrNotFound
	}

	if opMember.Role == "owner" {
		return s.Repo.RemoveGroupMember(threadID, targetUserID)
	}
	if opMember.Role == "admin" && targetMember.Role == "member" {
		return s.Repo.RemoveGroupMember(threadID, targetUserID)
	}
	return ErrForbidden
}

// UpdateGroupMemberRole 更新成员角色（仅群主）
func (s *Service) UpdateGroupMemberRole(threadID, ownerID, targetUserID uint, role string) error {
	thread, err := s.Repo.GetGroupThread(threadID)
	if err != nil {
		return ErrNotFound
	}
	if thread.OwnerID != ownerID {
		return ErrForbidden
	}
	if targetUserID == ownerID {
		return &Err{Code: 400, Msg: "不能修改自己的角色"}
	}
	if role != "admin" && role != "member" {
		return &Err{Code: 400, Msg: "无效的角色"}
	}
	if !s.Repo.IsGroupMember(threadID, targetUserID) {
		return ErrNotFound
	}
	return s.Repo.UpdateGroupMemberRole(threadID, targetUserID, role)
}

// TransferGroupOwner 转让群主
func (s *Service) TransferGroupOwner(threadID, ownerID, newOwnerID uint) error {
	thread, err := s.Repo.GetGroupThread(threadID)
	if err != nil {
		return ErrNotFound
	}
	if thread.OwnerID != ownerID {
		return ErrForbidden
	}
	if !s.Repo.IsGroupMember(threadID, newOwnerID) {
		return ErrNotFound
	}
	if err := s.Repo.UpdateGroupMemberRole(threadID, newOwnerID, "owner"); err != nil {
		return err
	}
	if err := s.Repo.UpdateGroupMemberRole(threadID, ownerID, "member"); err != nil {
		return err
	}
	return s.Repo.UpdateGroupThread(threadID, map[string]interface{}{"owner_id": newOwnerID})
}

// ListUserGroupThreads 列出用户的所有群聊
func (s *Service) ListUserGroupThreads(userID uint, limit int, beforeID, afterID uint) ([]models.GroupThread, error) {
	threads, err := s.Repo.ListUserGroupThreads(userID, limit, beforeID, afterID)
	if err != nil {
		return nil, err
	}

	for i := range threads {
		cnt, _ := s.Repo.CountGroupMembers(threads[i].ID)
		threads[i].MemberCount = int(cnt)
		if m, err := s.Repo.GetGroupMember(threads[i].ID, userID); err == nil {
			threads[i].IsMuted = m.IsMuted
			threads[i].IsPinned = m.Pinned
		}
	}
	return threads, nil
}

// SendGroupMessage 发送群聊消息
func (s *Service) SendGroupMessage(authorID, threadID uint, content string, replyToID *uint, msgType, platform string, fileMeta []byte, tempID string) (*models.GroupMessage, error) {
	if !s.Repo.IsGroupMember(threadID, authorID) {
		return nil, ErrUnauthorized
	}

	msg := &models.GroupMessage{
		ThreadID: threadID,
		AuthorID: authorID,
		Content:  content,
		Type:     msgType,
		Platform: platform,
		TempID:   tempID,
	}
	if replyToID != nil {
		msg.ReplyToID = replyToID
	}
	if len(fileMeta) > 0 {
		msg.FileMeta = fileMeta
	}

	if err := s.Repo.CreateGroupMessage(msg); err != nil {
		return nil, err
	}

	now := time.Now()
	_ = s.Repo.UpdateGroupThread(threadID, map[string]interface{}{"last_message_at": &now})

	return msg, nil
}

// GetGroupMessages 获取群聊消息列表
func (s *Service) GetGroupMessages(userID, threadID uint, limit int, beforeID, afterID uint) ([]models.GroupMessage, error) {
	if !s.Repo.IsGroupMember(threadID, userID) {
		return nil, ErrUnauthorized
	}
	msgs, err := s.Repo.ListGroupMessages(threadID, limit, beforeID, afterID)
	if err != nil {
		return nil, err
	}
	s.populateGroupMessageReactions(msgs)
	return msgs, nil
}

// GetGroupMessagesWithUsers 获取群聊消息列表，并返回相关用户数据
func (s *Service) GetGroupMessagesWithUsers(userID, threadID uint, limit int, beforeID, afterID uint) ([]models.GroupMessage, []models.User, error) {
	if !s.Repo.IsGroupMember(threadID, userID) {
		return nil, nil, ErrUnauthorized
	}
	msgs, users, err := s.Repo.ListGroupMessagesWithUsers(threadID, limit, beforeID, afterID)
	if err != nil {
		return nil, nil, err
	}
	s.populateGroupMessageReactions(msgs)
	return msgs, users, nil
}

// populateGroupMessageReactions 批量为群聊消息填充聚合后的 reactions
func (s *Service) populateGroupMessageReactions(msgs []models.GroupMessage) {
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

// DeleteGroupMessage 撤回群聊消息（作者或管理员/群主可操作）
func (s *Service) DeleteGroupMessage(messageID, userID uint) error {
	msg, err := s.Repo.GetGroupMessage(messageID)
	if err != nil {
		return ErrNotFound
	}

	if msg.AuthorID == userID {
		return s.Repo.DeleteGroupMessage(messageID)
	}

	m, err := s.Repo.GetGroupMember(msg.ThreadID, userID)
	if err != nil {
		return ErrUnauthorized
	}
	if m.Role == "owner" || m.Role == "admin" {
		return s.Repo.DeleteGroupMessage(messageID)
	}

	return ErrForbidden
}

// SetGroupMuted 设置群聊免打扰
func (s *Service) SetGroupMuted(threadID, userID uint, muted bool) error {
	if !s.Repo.IsGroupMember(threadID, userID) {
		return ErrUnauthorized
	}
	return s.Repo.DB.Model(&models.GroupThreadMember{}).
		Where("thread_id = ? AND user_id = ?", threadID, userID).
		Update("is_muted", muted).Error
}

// OnNewGroupMessage 群聊新消息：更新其他成员未读计数，并自动恢复隐藏会话
func (s *Service) OnNewGroupMessage(threadID, messageID, authorID uint) error {
	memberIDs, err := s.Repo.GetGroupMemberIDs(threadID)
	if err != nil {
		return err
	}
	for _, uid := range memberIDs {
		if uid == authorID {
			continue
		}
		_ = s.Repo.IncrementUnreadCount(uid, "group", threadID, false)
		_ = s.Repo.SetGroupThreadHidden(threadID, uid, false)
	}
	return nil
}

// MarkGroupRead 标记群聊已读
func (s *Service) MarkGroupRead(threadID, userID, messageID uint) error {
	if !s.Repo.IsGroupMember(threadID, userID) {
		return ErrUnauthorized
	}
	return s.Repo.UpdateReadState(userID, "group", threadID, messageID)
}

// ==================== 群聊会话隐藏管理 ====================

// SetGroupThreadHidden 设置群聊会话隐藏状态
func (s *Service) SetGroupThreadHidden(threadID, userID uint, hidden bool) error {
	if !s.Repo.IsGroupMember(threadID, userID) {
		return ErrUnauthorized
	}
	return s.Repo.SetGroupThreadHidden(threadID, userID, hidden)
}

// SetGroupThreadPinned 设置群聊会话置顶状态
func (s *Service) SetGroupThreadPinned(threadID, userID uint, pinned bool) error {
	if !s.Repo.IsGroupMember(threadID, userID) {
		return ErrUnauthorized
	}
	return s.Repo.SetGroupThreadPinned(threadID, userID, pinned)
}

// ListPinnedGroupThreads 列出用户置顶的群聊会话
func (s *Service) ListPinnedGroupThreads(userID uint) ([]models.GroupThread, error) {
	threads, err := s.Repo.ListPinnedGroupThreads(userID)
	if err != nil {
		return nil, err
	}
	for i := range threads {
		cnt, _ := s.Repo.CountGroupMembers(threads[i].ID)
		threads[i].MemberCount = int(cnt)
		threads[i].IsPinned = true
		if m, err := s.Repo.GetGroupMember(threads[i].ID, userID); err == nil {
			threads[i].IsMuted = m.IsMuted
			threads[i].IsHidden = m.Hidden
		}
	}
	return threads, nil
}

// ListHiddenGroupThreads 列出隐藏的群聊会话
func (s *Service) ListHiddenGroupThreads(userID uint, limit int, beforeID, afterID uint) ([]models.GroupThread, error) {
	threads, err := s.Repo.ListHiddenGroupThreads(userID, limit, beforeID, afterID)
	if err != nil {
		return nil, err
	}
	for i := range threads {
		cnt, _ := s.Repo.CountGroupMembers(threads[i].ID)
		threads[i].MemberCount = int(cnt)
		threads[i].IsHidden = true
		if m, err := s.Repo.GetGroupMember(threads[i].ID, userID); err == nil {
			threads[i].IsMuted = m.IsMuted
		}
	}
	return threads, nil
}

// ==================== 群聊精华消息 ====================

// PinGroupMessage 将群聊消息设为精华（群主/管理员可操作）
func (s *Service) PinGroupMessage(threadID, messageID, userID uint) (*models.PinnedMessage, error) {
	// 检查消息存在且属于此群聊
	msg, err := s.Repo.GetGroupMessage(messageID)
	if err != nil {
		return nil, ErrNotFound
	}
	if msg.ThreadID != threadID {
		return nil, ErrBadRequest
	}

	// 检查操作者权限（群主或管理员）
	m, err := s.Repo.GetGroupMember(threadID, userID)
	if err != nil {
		return nil, ErrUnauthorized
	}
	if m.Role != "owner" && m.Role != "admin" {
		return nil, ErrForbidden
	}

	// 检查是否已精华
	existing, _ := s.Repo.GetPinnedGroupMessage(messageID)
	if existing != nil {
		return nil, &Err{Code: 400, Msg: "消息已精华"}
	}

	pm := &models.PinnedMessage{
		MessageID:      messageID,
		GroupThreadID:  &threadID,
		GroupMessageID: &messageID,
		PinnedBy:       userID,
		CreatedAt:      time.Now(),
	}
	if err := s.Repo.CreatePinnedMessage(pm); err != nil {
		return nil, err
	}
	return pm, nil
}

// UnpinGroupMessage 取消群聊消息精华（群主/管理员/原始设置者可操作）
func (s *Service) UnpinGroupMessage(threadID, messageID, userID uint) error {
	pm, err := s.Repo.GetPinnedGroupMessage(messageID)
	if err != nil {
		return ErrNotFound
	}

	// 检查操作者权限
	m, err := s.Repo.GetGroupMember(threadID, userID)
	if err != nil {
		return ErrUnauthorized
	}
	if m.Role != "owner" && m.Role != "admin" && pm.PinnedBy != userID {
		return ErrForbidden
	}

	return s.Repo.DeletePinnedGroupMessage(messageID)
}

// ListPinnedGroupMessages 获取群聊精华消息列表
func (s *Service) ListPinnedGroupMessages(threadID, userID uint) ([]models.GroupMessage, error) {
	if !s.Repo.IsGroupMember(threadID, userID) {
		return nil, ErrUnauthorized
	}

	pinnedList, err := s.Repo.ListPinnedGroupMessages(threadID)
	if err != nil {
		return nil, err
	}

	var messages []models.GroupMessage
	for _, pm := range pinnedList {
		if pm.GroupMessageID == nil {
			continue
		}
		msg, err := s.Repo.GetGroupMessage(*pm.GroupMessageID)
		if err != nil {
			continue
		}
		if msg.DeletedAt != nil {
			continue
		}
		if msg.ReplyToID != nil {
			replyTo, _ := s.Repo.GetGroupMessage(*msg.ReplyToID)
			if replyTo != nil && replyTo.DeletedAt == nil {
				msg.ReplyTo = replyTo
			}
		}
		messages = append(messages, *msg)
	}
	return messages, nil
}

// ==================== 群聊收藏 ====================

// FavoriteGroupMessage 收藏群聊消息
func (s *Service) FavoriteGroupMessage(userID, groupMessageID uint) error {
	msg, err := s.Repo.GetGroupMessage(groupMessageID)
	if err != nil || msg == nil {
		return ErrNotFound
	}

	// 检查是否是群成员
	if !s.Repo.IsGroupMember(msg.ThreadID, userID) {
		return ErrUnauthorized
	}

	existing, _ := s.Repo.GetFavoriteGroupMessage(userID, groupMessageID)
	if existing != nil {
		return &Err{Code: 400, Msg: "已收藏"}
	}

	fm := &models.FavoriteMessage{
		UserID:         userID,
		GroupMessageID: &groupMessageID,
		MessageType:    "group",
		CreatedAt:      time.Now(),
	}
	return s.Repo.CreateFavoriteMessage(fm)
}

// SendFavoriteToGroupThread 将收藏的消息发送到指定群聊
func (s *Service) SendFavoriteToGroupThread(userID, favoriteID, groupThreadID uint) (*models.GroupMessage, error) {
	favorite, err := s.Repo.GetFavoriteByID(favoriteID)
	if err != nil {
		return nil, ErrNotFound
	}
	if favorite.UserID != userID {
		return nil, &Err{Code: 403, Msg: "无权访问该收藏"}
	}

	// 获取原始消息内容
	var content string
	var msgType string
	var fileMeta []byte
	var mentions []byte

	switch favorite.MessageType {
	case "channel":
		if favorite.MessageID == nil {
			return nil, &Err{Code: 400, Msg: "原始消息不存在"}
		}
		orig, err := s.Repo.GetMessage(*favorite.MessageID)
		if err != nil || orig == nil {
			return nil, &Err{Code: 404, Msg: "原始消息不存在"}
		}
		content = orig.Content
		msgType = orig.Type
		fileMeta = orig.FileMeta
		mentions = orig.Mentions
	case "dm":
		if favorite.DmMessageID == nil {
			return nil, &Err{Code: 400, Msg: "原始消息不存在"}
		}
		orig, err := s.Repo.GetDmMessage(*favorite.DmMessageID)
		if err != nil || orig == nil {
			return nil, &Err{Code: 404, Msg: "原始消息不存在"}
		}
		content = orig.Content
		msgType = orig.Type
		fileMeta = orig.FileMeta
		mentions = orig.Mentions
	case "group":
		if favorite.GroupMessageID == nil {
			return nil, &Err{Code: 400, Msg: "原始消息不存在"}
		}
		orig, err := s.Repo.GetGroupMessage(*favorite.GroupMessageID)
		if err != nil || orig == nil {
			return nil, &Err{Code: 404, Msg: "原始消息不存在"}
		}
		content = orig.Content
		msgType = orig.Type
		fileMeta = orig.FileMeta
		mentions = orig.Mentions
	default:
		return nil, &Err{Code: 400, Msg: "无效的收藏类型"}
	}

	// 检查是否为群成员
	if !s.Repo.IsGroupMember(groupThreadID, userID) {
		return nil, &Err{Code: 403, Msg: "无权在该群聊发送消息"}
	}

	newMsg := &models.GroupMessage{
		ThreadID: groupThreadID,
		AuthorID: userID,
		Content:  content,
		Type:     msgType,
		Platform: "web",
	}
	if len(fileMeta) > 0 {
		newMsg.FileMeta = fileMeta
	}
	if len(mentions) > 0 {
		newMsg.Mentions = mentions
	}

	if err := s.Repo.CreateGroupMessage(newMsg); err != nil {
		return nil, &Err{Code: 500, Msg: "发送消息失败"}
	}

	now := time.Now()
	_ = s.Repo.UpdateGroupThread(groupThreadID, map[string]interface{}{"last_message_at": &now})

	return newMsg, nil
}

// ==================== 群聊成员搜索 ====================

// SearchGroupMembers 在群成员中搜索（按用户名或群昵称模糊匹配）
func (s *Service) SearchGroupMembers(threadID, userID uint, query string, limit int) ([]models.GroupThreadMember, error) {
	if !s.Repo.IsGroupMember(threadID, userID) {
		return nil, ErrUnauthorized
	}
	members, err := s.Repo.SearchGroupMembers(threadID, query, limit)
	if err != nil {
		return nil, err
	}
	// 填充用户信息
	for i := range members {
		if u, err := s.GetUserByID(members[i].UserID); err == nil {
			members[i].User = u
		}
	}
	return members, nil
}

// ==================== 群聊加入申请 ====================

// ApplyGroupJoin 申请加入群聊
func (s *Service) ApplyGroupJoin(threadID, userID uint, note string) (*models.GroupJoinRequest, error) {
	// 检查是否已是成员
	if s.Repo.IsGroupMember(threadID, userID) {
		return nil, &Err{Code: 409, Msg: "您已经是该群聊的成员"}
	}

	// 获取群聊信息
	thread, err := s.Repo.GetGroupThread(threadID)
	if err != nil {
		return nil, ErrNotFound
	}

	joinMode := thread.JoinMode
	if joinMode == "" {
		joinMode = "invite_only"
	}

	switch joinMode {
	case "invite_only":
		return nil, &Err{Code: 403, Msg: "该群聊仅允许邀请加入"}
	case "free_join":
		// 直接加入
		current, _ := s.Repo.CountGroupMembers(threadID)
		if current >= config.MaxGroupMembers {
			return nil, &Err{Code: 400, Msg: "群成员数量已达上限"}
		}
		m := &models.GroupThreadMember{
			ThreadID: threadID,
			UserID:   userID,
			Role:     "member",
		}
		if err := s.Repo.AddGroupMember(m); err != nil {
			return nil, err
		}
		// 清理可能存在的 pending 申请
		_ = s.Repo.DB.Where("thread_id = ? AND user_id = ? AND status = ?", threadID, userID, "pending").Delete(&models.GroupJoinRequest{}).Error
		return nil, nil // nil 表示直接加入
	case "need_approval":
		// 检查是否已有 pending 申请
		pending, err := s.Repo.GetPendingGroupJoinRequest(threadID, userID)
		if err == nil && pending != nil {
			return nil, &Err{Code: 409, Msg: "您已提交申请，请等待审核"}
		}
		// 创建申请
		req := &models.GroupJoinRequest{
			ThreadID: threadID,
			UserID:   userID,
			Note:     note,
			Status:   "pending",
		}
		if err := s.Repo.CreateGroupJoinRequest(req); err != nil {
			return nil, err
		}
		// Preload user info
		req, _ = s.Repo.GetGroupJoinRequestByID(req.ID)
		return req, nil
	default:
		return nil, &Err{Code: 403, Msg: "该群聊不允许申请加入"}
	}
}

// ListGroupJoinRequests 列出群聊加入申请（群主/管理员）
func (s *Service) ListGroupJoinRequests(threadID, requesterID uint, page, limit int) ([]*models.GroupJoinRequest, int, error) {
	m, err := s.Repo.GetGroupMember(threadID, requesterID)
	if err != nil {
		return nil, 0, ErrUnauthorized
	}
	if m.Role != "owner" && m.Role != "admin" {
		return nil, 0, ErrForbidden
	}
	total, err := s.Repo.CountGroupJoinRequests(threadID)
	if err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * limit
	requests, err := s.Repo.ListGroupJoinRequests(threadID, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	return requests, total, nil
}

// GetUserGroupJoinRequestStatus 获取用户自己的群聊加入申请状态
func (s *Service) GetUserGroupJoinRequestStatus(threadID, userID uint) (*models.GroupJoinRequest, error) {
	req, err := s.Repo.GetGroupJoinRequestByUserAndThread(userID, threadID)
	if err != nil {
		return nil, ErrNotFound
	}
	return req, nil
}

// ApproveGroupJoinRequest 批准群聊加入申请
func (s *Service) ApproveGroupJoinRequest(threadID, requestID, handlerID uint) error {
	m, err := s.Repo.GetGroupMember(threadID, handlerID)
	if err != nil {
		return ErrUnauthorized
	}
	if m.Role != "owner" && m.Role != "admin" {
		return ErrForbidden
	}
	req, err := s.Repo.GetGroupJoinRequestByID(requestID)
	if err != nil {
		return ErrNotFound
	}
	if req.ThreadID != threadID {
		return &Err{Code: 400, Msg: "申请不属于该群聊"}
	}
	if req.Status != "pending" {
		return &Err{Code: 400, Msg: "申请不是待处理状态"}
	}

	// 检查成员上限
	current, _ := s.Repo.CountGroupMembers(threadID)
	if current >= config.MaxGroupMembers {
		return &Err{Code: 400, Msg: "群成员数量已达上限"}
	}

	// 添加成员
	newMember := &models.GroupThreadMember{
		ThreadID: threadID,
		UserID:   req.UserID,
		Role:     "member",
	}
	_ = s.Repo.AddGroupMember(newMember)

	// 更新申请状态
	now := time.Now()
	return s.Repo.UpdateGroupJoinRequest(req, map[string]any{
		"status":     "approved",
		"handled_by": handlerID,
		"handled_at": now,
	})
}

// RejectGroupJoinRequest 拒绝群聊加入申请
func (s *Service) RejectGroupJoinRequest(threadID, requestID, handlerID uint) error {
	m, err := s.Repo.GetGroupMember(threadID, handlerID)
	if err != nil {
		return ErrUnauthorized
	}
	if m.Role != "owner" && m.Role != "admin" {
		return ErrForbidden
	}
	req, err := s.Repo.GetGroupJoinRequestByID(requestID)
	if err != nil {
		return ErrNotFound
	}
	if req.ThreadID != threadID {
		return &Err{Code: 400, Msg: "申请不属于该群聊"}
	}
	if req.Status != "pending" {
		return &Err{Code: 400, Msg: "申请不是待处理状态"}
	}
	now := time.Now()
	return s.Repo.UpdateGroupJoinRequest(req, map[string]any{
		"status":     "rejected",
		"handled_by": handlerID,
		"handled_at": now,
	})
}

// ==================== 群公告 ====================

// CreateGroupAnnouncement 创建群公告（群主/管理员）
func (s *Service) CreateGroupAnnouncement(threadID, authorID uint, title, content string, images datatypes.JSON) (*models.GroupAnnouncement, error) {
	m, err := s.Repo.GetGroupMember(threadID, authorID)
	if err != nil {
		return nil, ErrUnauthorized
	}
	if m.Role != "owner" && m.Role != "admin" {
		return nil, ErrForbidden
	}

	count, err := s.Repo.CountGroupAnnouncements(threadID)
	if err != nil {
		return nil, err
	}
	if int64(count) >= config.MaxGroupAnnouncements {
		return nil, &Err{Code: 400, Msg: "群公告数量已达上限"}
	}

	a := &models.GroupAnnouncement{
		ThreadID:  threadID,
		AuthorID:  authorID,
		Title:     title,
		Content:   content,
		CreatedAt: time.Now(),
	}
	if len(images) > 0 {
		a.Images = images
	}
	if err := s.Repo.CreateGroupAnnouncement(a); err != nil {
		return nil, err
	}
	// Reload with author
	return s.Repo.GetGroupAnnouncement(a.ID)
}

// GetGroupAnnouncement 获取单条群公告
func (s *Service) GetGroupAnnouncement(id uint) (*models.GroupAnnouncement, error) {
	return s.Repo.GetGroupAnnouncement(id)
}

// ListGroupAnnouncements 列出群公告（分页）
func (s *Service) ListGroupAnnouncements(threadID, userID uint, page, limit int) ([]*models.GroupAnnouncement, int, error) {
	if !s.Repo.IsGroupMember(threadID, userID) {
		return nil, 0, ErrUnauthorized
	}
	total, err := s.Repo.CountGroupAnnouncements(threadID)
	if err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * limit
	list, err := s.Repo.ListGroupAnnouncementsPaginated(threadID, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// UpdateGroupAnnouncement 编辑群公告
func (s *Service) UpdateGroupAnnouncement(id, userID uint, title, content *string, images *datatypes.JSON) (*models.GroupAnnouncement, error) {
	a, err := s.Repo.GetGroupAnnouncement(id)
	if err != nil {
		return nil, ErrNotFound
	}
	m, err := s.Repo.GetGroupMember(a.ThreadID, userID)
	if err != nil {
		return nil, ErrUnauthorized
	}
	if m.Role != "owner" && m.Role != "admin" && a.AuthorID != userID {
		return nil, ErrForbidden
	}
	if title != nil {
		a.Title = *title
	}
	if content != nil {
		a.Content = *content
	}
	if images != nil {
		a.Images = *images
	}
	now := time.Now()
	a.UpdatedAt = &now
	if err := s.Repo.UpdateGroupAnnouncement(a); err != nil {
		return nil, err
	}
	return a, nil
}

// DeleteGroupAnnouncement 删除群公告
func (s *Service) DeleteGroupAnnouncement(id, userID uint) error {
	a, err := s.Repo.GetGroupAnnouncement(id)
	if err != nil {
		return ErrNotFound
	}
	m, err := s.Repo.GetGroupMember(a.ThreadID, userID)
	if err != nil {
		return ErrUnauthorized
	}
	if m.Role != "owner" && m.Role != "admin" && a.AuthorID != userID {
		return ErrForbidden
	}
	return s.Repo.DeleteGroupAnnouncement(id)
}

// PinGroupAnnouncement 置顶群公告
func (s *Service) PinGroupAnnouncement(announcementID, userID uint) (*models.GroupAnnouncement, error) {
	a, err := s.Repo.GetGroupAnnouncement(announcementID)
	if err != nil {
		return nil, ErrNotFound
	}
	m, err := s.Repo.GetGroupMember(a.ThreadID, userID)
	if err != nil {
		return nil, ErrUnauthorized
	}
	if m.Role != "owner" && m.Role != "admin" {
		return nil, ErrForbidden
	}
	if err := s.Repo.PinGroupAnnouncement(a.ThreadID, announcementID); err != nil {
		return nil, err
	}
	return s.Repo.GetGroupAnnouncement(announcementID)
}

// UnpinGroupAnnouncement 取消置顶群公告
func (s *Service) UnpinGroupAnnouncement(announcementID, userID uint) error {
	a, err := s.Repo.GetGroupAnnouncement(announcementID)
	if err != nil {
		return ErrNotFound
	}
	m, err := s.Repo.GetGroupMember(a.ThreadID, userID)
	if err != nil {
		return ErrUnauthorized
	}
	if m.Role != "owner" && m.Role != "admin" {
		return ErrForbidden
	}
	return s.Repo.UnpinGroupAnnouncement(a.ThreadID, announcementID)
}

// GetFeaturedGroupAnnouncement 获取精选群公告（置顶优先，否则最新）
func (s *Service) GetFeaturedGroupAnnouncement(threadID, userID uint) (*models.GroupAnnouncement, error) {
	if !s.Repo.IsGroupMember(threadID, userID) {
		return nil, ErrUnauthorized
	}
	return s.Repo.GetFeaturedGroupAnnouncement(threadID)
}

// ==================== 群文件 ====================

// UploadGroupFile 上传群文件
func (s *Service) UploadGroupFile(threadID, uploaderID uint, fileName, filePath, contentType string, fileSize int64) (*models.GroupFile, error) {
	m, err := s.Repo.GetGroupMember(threadID, uploaderID)
	if err != nil {
		return nil, ErrUnauthorized
	}
	_ = m // 任何成员可上传

	count, err := s.Repo.CountGroupFiles(threadID)
	if err != nil {
		return nil, err
	}
	if count >= config.MaxGroupFiles {
		return nil, &Err{Code: 400, Msg: "群文件数量已达上限"}
	}

	f := &models.GroupFile{
		ThreadID:    threadID,
		UploaderID:  uploaderID,
		FileName:    fileName,
		FilePath:    filePath,
		FileSize:    fileSize,
		ContentType: contentType,
	}
	if err := s.Repo.CreateGroupFile(f); err != nil {
		return nil, err
	}
	return f, nil
}

// ListGroupFiles 列出群文件
func (s *Service) ListGroupFiles(threadID, userID uint, limit int, before uint) ([]models.GroupFile, error) {
	if !s.Repo.IsGroupMember(threadID, userID) {
		return nil, ErrUnauthorized
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	files, err := s.Repo.ListGroupFiles(threadID, limit, before)
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

// DeleteGroupFile 删除群文件（上传者本人、管理员或群主）
func (s *Service) DeleteGroupFile(fileID, userID uint) error {
	f, err := s.Repo.GetGroupFile(fileID)
	if err != nil {
		return ErrNotFound
	}
	if f.UploaderID != userID {
		m, err := s.Repo.GetGroupMember(f.ThreadID, userID)
		if err != nil {
			return ErrUnauthorized
		}
		if m.Role != "owner" && m.Role != "admin" {
			return ErrForbidden
		}
	}
	if s.MinIO != nil {
		_ = s.MinIO.DeleteFile(context.Background(), f.FilePath)
	}
	return s.Repo.DeleteGroupFile(fileID)
}
