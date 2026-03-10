package service

import (
	"time"

	"bubble/src/config"
	"bubble/src/db/models"
)

// ==================== SubRoom (频道子房间) ====================

// CreateSubRoom 创建频道子房间（每个成员每个频道限建一个）
func (s *Service) CreateSubRoom(guildID, channelID, ownerID uint, name string) (*models.SubRoom, error) {
	if len([]rune(name)) == 0 || int64(len([]rune(name))) > config.MaxSubRoomNameLength {
		return nil, &Err{Code: 400, Msg: "子房间名称长度无效"}
	}

	isMember, err := s.Repo.IsMember(guildID, ownerID)
	if err != nil || !isMember {
		return nil, ErrUnauthorized
	}

	ch, err := s.Repo.GetChannel(channelID)
	if err != nil || ch.GuildID != guildID {
		return nil, ErrNotFound
	}

	exists, _ := s.Repo.HasSubRoomInChannel(channelID, ownerID)
	if exists {
		return nil, &Err{Code: 409, Msg: "每个成员在同一频道下仅可创建一个子房间"}
	}

	room := &models.SubRoom{
		GuildID:   guildID,
		ChannelID: channelID,
		OwnerID:   ownerID,
		Name:      name,
	}
	if err := s.Repo.CreateSubRoom(room); err != nil {
		return nil, err
	}

	ownerMember := &models.SubRoomMember{
		RoomID: room.ID,
		UserID: ownerID,
		Role:   "owner",
	}
	_ = s.Repo.AddSubRoomMember(ownerMember)

	room.MemberCount = 1
	return room, nil
}

// GetSubRoom 获取子房间详情
func (s *Service) GetSubRoom(roomID, userID uint) (*models.SubRoom, error) {
	room, err := s.Repo.GetSubRoom(roomID)
	if err != nil {
		return nil, ErrNotFound
	}

	isMember, err := s.Repo.IsMember(room.GuildID, userID)
	if err != nil || !isMember {
		return nil, ErrUnauthorized
	}

	members, _ := s.Repo.ListSubRoomMembers(roomID)
	for i := range members {
		if u, err := s.GetUserByID(members[i].UserID); err == nil {
			members[i].User = u
		}
	}
	room.Members = members
	room.MemberCount = len(members)
	room.IsMember = s.Repo.IsSubRoomMember(roomID, userID)

	if owner, err := s.GetUserByID(room.OwnerID); err == nil {
		room.Owner = owner
	}

	return room, nil
}

// ListSubRooms 列出频道下的子房间（所有服务器成员可见）
func (s *Service) ListSubRooms(guildID, channelID, userID uint) ([]models.SubRoom, error) {
	isMember, err := s.Repo.IsMember(guildID, userID)
	if err != nil || !isMember {
		return nil, ErrUnauthorized
	}

	rooms, err := s.Repo.ListSubRooms(channelID)
	if err != nil {
		return nil, err
	}

	for i := range rooms {
		cnt, _ := s.Repo.CountSubRoomMembers(rooms[i].ID)
		rooms[i].MemberCount = int(cnt)
		rooms[i].IsMember = s.Repo.IsSubRoomMember(rooms[i].ID, userID)
		if owner, err := s.GetUserByID(rooms[i].OwnerID); err == nil {
			rooms[i].Owner = owner
		}
	}
	return rooms, nil
}

// UpdateSubRoom 更新子房间信息（仅房主可操作）
func (s *Service) UpdateSubRoom(roomID, userID uint, name, avatar *string) error {
	room, err := s.Repo.GetSubRoom(roomID)
	if err != nil {
		return ErrNotFound
	}
	if room.OwnerID != userID {
		return ErrForbidden
	}
	updates := make(map[string]interface{})
	if name != nil {
		if len([]rune(*name)) == 0 || int64(len([]rune(*name))) > config.MaxSubRoomNameLength {
			return &Err{Code: 400, Msg: "名称长度无效"}
		}
		updates["name"] = *name
	}
	if avatar != nil {
		updates["avatar"] = *avatar
	}
	if len(updates) == 0 {
		return nil
	}
	return s.Repo.UpdateSubRoom(roomID, updates)
}

// DeleteSubRoom 删除子房间（仅房主可操作）
func (s *Service) DeleteSubRoom(roomID, userID uint) error {
	room, err := s.Repo.GetSubRoom(roomID)
	if err != nil {
		return ErrNotFound
	}
	if room.OwnerID != userID {
		return ErrForbidden
	}
	return s.Repo.DeleteSubRoom(roomID)
}

// AddSubRoomMembers 添加子房间成员（仅房主可操作，且目标须是服务器成员）
func (s *Service) AddSubRoomMembers(roomID, operatorID uint, userIDs []uint) ([]models.SubRoomMember, error) {
	room, err := s.Repo.GetSubRoom(roomID)
	if err != nil {
		return nil, ErrNotFound
	}
	if room.OwnerID != operatorID {
		return nil, ErrForbidden
	}

	current, _ := s.Repo.CountSubRoomMembers(roomID)
	if current+int64(len(userIDs)) > config.MaxSubRoomMembers {
		return nil, &Err{Code: 400, Msg: "子房间成员数量超出上限"}
	}

	var added []models.SubRoomMember
	for _, uid := range userIDs {
		if s.Repo.IsSubRoomMember(roomID, uid) {
			continue
		}
		isMember, _ := s.Repo.IsMember(room.GuildID, uid)
		if !isMember {
			continue
		}
		m := &models.SubRoomMember{
			RoomID: roomID,
			UserID: uid,
			Role:   "member",
		}
		if err := s.Repo.AddSubRoomMember(m); err == nil {
			added = append(added, *m)
		}
	}
	return added, nil
}

// RemoveSubRoomMember 移除子房间成员（房主踢人 或 自己退出）
func (s *Service) RemoveSubRoomMember(roomID, operatorID, targetUserID uint) error {
	room, err := s.Repo.GetSubRoom(roomID)
	if err != nil {
		return ErrNotFound
	}

	if targetUserID == room.OwnerID {
		return &Err{Code: 400, Msg: "房主不能退出，请直接删除子房间"}
	}

	if operatorID == targetUserID {
		if !s.Repo.IsSubRoomMember(roomID, targetUserID) {
			return ErrNotFound
		}
		return s.Repo.RemoveSubRoomMember(roomID, targetUserID)
	}

	if room.OwnerID != operatorID {
		return ErrForbidden
	}
	if !s.Repo.IsSubRoomMember(roomID, targetUserID) {
		return ErrNotFound
	}
	return s.Repo.RemoveSubRoomMember(roomID, targetUserID)
}

// SendSubRoomMessage 发送子房间消息（仅成员可发送）
func (s *Service) SendSubRoomMessage(authorID, roomID uint, content string, replyToID *uint, msgType, platform string, fileMeta []byte, tempID string) (*models.SubRoomMessage, error) {
	if !s.Repo.IsSubRoomMember(roomID, authorID) {
		return nil, ErrUnauthorized
	}

	msg := &models.SubRoomMessage{
		RoomID:   roomID,
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

	if err := s.Repo.CreateSubRoomMessage(msg); err != nil {
		return nil, err
	}

	now := time.Now()
	_ = s.Repo.UpdateSubRoom(roomID, map[string]interface{}{"last_message_at": &now})

	return msg, nil
}

// GetSubRoomMessages 获取子房间消息列表（仅成员可查看）
func (s *Service) GetSubRoomMessages(userID, roomID uint, limit int, beforeID, afterID uint) ([]models.SubRoomMessage, error) {
	if !s.Repo.IsSubRoomMember(roomID, userID) {
		return nil, ErrUnauthorized
	}
	return s.Repo.ListSubRoomMessages(roomID, limit, beforeID, afterID)
}

// DeleteSubRoomMessage 撤回子房间消息（作者或房主可操作）
func (s *Service) DeleteSubRoomMessage(messageID, userID uint) error {
	msg, err := s.Repo.GetSubRoomMessage(messageID)
	if err != nil {
		return ErrNotFound
	}

	if msg.AuthorID == userID {
		return s.Repo.DeleteSubRoomMessage(messageID)
	}

	room, err := s.Repo.GetSubRoom(msg.RoomID)
	if err != nil {
		return ErrNotFound
	}
	if room.OwnerID == userID {
		return s.Repo.DeleteSubRoomMessage(messageID)
	}

	return ErrForbidden
}

// OnNewSubRoomMessage 子房间新消息：更新成员未读计数，并自动恢复隐藏会话
func (s *Service) OnNewSubRoomMessage(roomID, messageID, authorID uint) error {
	memberIDs, err := s.Repo.GetSubRoomMemberIDs(roomID)
	if err != nil {
		return err
	}
	for _, uid := range memberIDs {
		if uid == authorID {
			continue
		}
		_ = s.Repo.IncrementUnreadCount(uid, "subroom", roomID, false)
		_ = s.Repo.SetSubRoomHidden(roomID, uid, false)
	}
	return nil
}

// MarkSubRoomRead 标记子房间已读
func (s *Service) MarkSubRoomRead(roomID, userID, messageID uint) error {
	if !s.Repo.IsSubRoomMember(roomID, userID) {
		return ErrUnauthorized
	}
	return s.Repo.UpdateReadState(userID, "subroom", roomID, messageID)
}

// ==================== 子房间会话隐藏管理 ====================

// SetSubRoomHidden 设置子房间会话隐藏状态
func (s *Service) SetSubRoomHidden(roomID, userID uint, hidden bool) error {
	if !s.Repo.IsSubRoomMember(roomID, userID) {
		return ErrUnauthorized
	}
	return s.Repo.SetSubRoomHidden(roomID, userID, hidden)
}

// ListHiddenSubRooms 列出隐藏的子房间会话
func (s *Service) ListHiddenSubRooms(userID uint) ([]models.SubRoom, error) {
	rooms, err := s.Repo.ListHiddenSubRooms(userID)
	if err != nil {
		return nil, err
	}
	for i := range rooms {
		cnt, _ := s.Repo.CountSubRoomMembers(rooms[i].ID)
		rooms[i].MemberCount = int(cnt)
		rooms[i].IsMember = true
		if owner, err := s.GetUserByID(rooms[i].OwnerID); err == nil {
			rooms[i].Owner = owner
		}
	}
	return rooms, nil
}
