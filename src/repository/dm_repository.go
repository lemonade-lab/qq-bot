package repository

import (
	"time"

	"bubble/src/db/models"

	"gorm.io/gorm"
)

// ──────────────────────────────────────────────
// DM (Direct Message) repository methods
// ──────────────────────────────────────────────

func orderPair(a, b uint) (uint, uint) {
	if a < b {
		return a, b
	}
	return b, a
}

func (r *Repo) GetDmThread(id uint) (*models.DmThread, error) {
	var t models.DmThread
	if err := r.DB.First(&t, id).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *Repo) GetOrCreateDmThread(a, b uint) (*models.DmThread, error) {
	if a == b {
		var t models.DmThread
		if err := r.DB.Where("user_a_id=? AND user_b_id=? AND is_notebook=?", a, a, true).First(&t).Error; err != nil {
			if err != gorm.ErrRecordNotFound {
				return nil, err
			}
			t = models.DmThread{
				UserAID:    a,
				UserBID:    a,
				IsNotebook: true,
			}
			if err := r.DB.Create(&t).Error; err != nil {
				return nil, err
			}
		}
		return &t, nil
	}

	x, y := orderPair(a, b)
	var t models.DmThread
	if err := r.DB.Where("user_a_id=? AND user_b_id=? AND is_notebook=?", x, y, false).First(&t).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			return nil, err
		}
		t = models.DmThread{
			UserAID:    x,
			UserBID:    y,
			IsNotebook: false,
		}
		if err := r.DB.Create(&t).Error; err != nil {
			return nil, err
		}
	}
	return &t, nil
}

func (r *Repo) ListDmThreads(me uint) ([]models.DmThread, error) {
	var list []models.DmThread
	err := r.DB.Where("(user_a_id=? OR user_b_id=?) AND NOT ((user_a_id=? AND hidden_by_user_a=?) OR (user_b_id=? AND hidden_by_user_b=?))",
		me, me, me, true, me, true).
		Order(gorm.Expr("CASE WHEN (user_a_id=? AND pinned_by_user_a=?) OR (user_b_id=? AND pinned_by_user_b=?) THEN 0 ELSE 1 END ASC", me, true, me, true)).
		Order("CASE WHEN last_message_at IS NULL THEN 1 ELSE 0 END ASC").
		Order("last_message_at DESC").
		Order("id DESC").
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return list, nil
	}

	peerIDs := make([]uint, 0, len(list))
	threadIDs := make([]uint, 0, len(list))
	for i := range list {
		thread := &list[i]
		threadIDs = append(threadIDs, thread.ID)
		var peerID uint
		if thread.UserAID == me {
			peerID = thread.UserBID
			thread.IsPinned = thread.PinnedByUserA
			thread.IsBlocked = thread.BlockedByUserA
		} else {
			peerID = thread.UserAID
			thread.IsPinned = thread.PinnedByUserB
			thread.IsBlocked = thread.BlockedByUserB
		}
		peerIDs = append(peerIDs, peerID)
	}

	var users []models.User
	r.DB.Where("id IN ?", peerIDs).Find(&users)
	userMap := make(map[uint]*models.User, len(users))
	for i := range users {
		userMap[users[i].ID] = &users[i]
	}

	var friendships []models.Friendship
	r.DB.Where("((from_user_id=? AND to_user_id IN ?) OR (from_user_id IN ? AND to_user_id=?)) AND status='accepted'",
		me, peerIDs, peerIDs, me).Find(&friendships)
	nicknameMap := make(map[uint]string, len(friendships))
	for _, f := range friendships {
		if f.FromUserID == me {
			if f.NicknameFrom != "" {
				nicknameMap[f.ToUserID] = f.NicknameFrom
			}
		} else {
			if f.NicknameTo != "" {
				nicknameMap[f.FromUserID] = f.NicknameTo
			}
		}
	}

	var lastMessages []models.DmMessage
	r.DB.Raw(`
		SELECT dm.* FROM dm_messages dm
		INNER JOIN (
			SELECT thread_id, MAX(id) AS max_id
			FROM dm_messages
			WHERE thread_id IN ?
			GROUP BY thread_id
		) latest ON dm.id = latest.max_id
	`, threadIDs).Scan(&lastMessages)
	lastMsgMap := make(map[uint]*models.DmMessage, len(lastMessages))
	for i := range lastMessages {
		lastMsgMap[lastMessages[i].ThreadID] = &lastMessages[i]
	}

	for i := range list {
		thread := &list[i]
		peerID := peerIDs[i]

		if u, ok := userMap[peerID]; ok {
			peerCopy := *u
			if nick, ok := nicknameMap[peerID]; ok {
				peerCopy.Nickname = nick
			}
			thread.PeerUser = &peerCopy
		}

		if msg, ok := lastMsgMap[thread.ID]; ok {
			thread.LastMessage = msg
		}
	}

	return list, nil
}

func (r *Repo) PinDmThread(threadID, userID uint, pinned bool) error {
	var thread models.DmThread
	err := r.DB.Where("id=? AND (user_a_id=? OR user_b_id=?)", threadID, userID, userID).First(&thread).Error
	if err != nil {
		return err
	}
	if thread.UserAID == userID {
		return r.DB.Model(&thread).Update("pinned_by_user_a", pinned).Error
	}
	return r.DB.Model(&thread).Update("pinned_by_user_b", pinned).Error
}

// ListPinnedDmThreads 列出用户置顶的私聊线程
func (r *Repo) ListPinnedDmThreads(userID uint) ([]models.DmThread, error) {
	var threads []models.DmThread
	err := r.DB.Where(
		"((user_a_id = ? AND pinned_by_user_a = ?) OR (user_b_id = ? AND pinned_by_user_b = ?)) AND "+
			"((user_a_id = ? AND hidden_by_user_a = ?) OR (user_b_id = ? AND hidden_by_user_b = ?))",
		userID, true, userID, true,
		userID, false, userID, false,
	).Order("last_message_at DESC, id DESC").Find(&threads).Error
	if err != nil {
		return nil, err
	}
	// 填充 PeerUser 和 IsPinned
	for i := range threads {
		threads[i].IsPinned = true
		peerID := threads[i].UserBID
		if threads[i].UserBID == userID {
			peerID = threads[i].UserAID
		}
		if usr, err := r.GetUserByID(peerID); err == nil {
			threads[i].PeerUser = usr
		}
	}
	return threads, nil
}

func (r *Repo) BlockDmThread(threadID, userID uint, blocked bool) error {
	var thread models.DmThread
	err := r.DB.Where("id=? AND (user_a_id=? OR user_b_id=?)", threadID, userID, userID).First(&thread).Error
	if err != nil {
		return err
	}
	if thread.UserAID == userID {
		return r.DB.Model(&thread).Update("blocked_by_user_a", blocked).Error
	}
	return r.DB.Model(&thread).Update("blocked_by_user_b", blocked).Error
}

// SetDmThreadHidden is defined in pagination_repository.go

// CountHiddenDmThreads is defined in pagination_repository.go

// ListHiddenDmThreads is defined in pagination_repository.go

// ──────────────────────────────────────────────
// Blacklist
// ──────────────────────────────────────────────

func (r *Repo) AddToBlacklist(userID, blockedID uint) error {
	bl := &models.Blacklist{UserID: userID, BlockedID: blockedID}
	return r.DB.Create(bl).Error
}

func (r *Repo) RemoveFromBlacklist(userID, blockedID uint) error {
	return r.DB.Where("user_id=? AND blocked_id=?", userID, blockedID).Delete(&models.Blacklist{}).Error
}

func (r *Repo) IsBlocked(userID, blockedID uint) (bool, error) {
	var count int64
	err := r.DB.Model(&models.Blacklist{}).Where("user_id=? AND blocked_id=?", userID, blockedID).Count(&count).Error
	return count > 0, err
}

func (r *Repo) ListBlacklist(userID uint) ([]models.Blacklist, error) {
	var list []models.Blacklist
	err := r.DB.Where("user_id = ?", userID).Order("created_at DESC").Find(&list).Error
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return list, nil
	}
	blockedIDs := make([]uint, len(list))
	for i, b := range list {
		blockedIDs[i] = b.BlockedID
	}
	var users []models.User
	r.DB.Where("id IN ?", blockedIDs).Find(&users)
	userMap := make(map[uint]*models.User, len(users))
	for i := range users {
		userMap[users[i].ID] = &users[i]
	}
	for i := range list {
		if u, ok := userMap[list[i].BlockedID]; ok {
			list[i].BlockedUser = u
		}
	}
	return list, nil
}

// ──────────────────────────────────────────────
// DM Messages
// ──────────────────────────────────────────────

func (r *Repo) AddDmMessage(m *models.DmMessage) (*models.DmMessage, error) {
	if err := r.DB.Create(m).Error; err != nil {
		return nil, err
	}
	if m.ReplyToID != nil {
		var replyTo models.DmMessage
		if err := r.DB.First(&replyTo, *m.ReplyToID).Error; err == nil {
			m.ReplyTo = &replyTo
		}
	}
	now := time.Now()
	var thread models.DmThread
	if err := r.DB.First(&thread, m.ThreadID).Error; err != nil {
		return m, err
	}
	updates := map[string]interface{}{"last_message_at": &now}
	if thread.UserAID == m.AuthorID {
		updates["hidden_by_user_b"] = false
	} else if thread.UserBID == m.AuthorID {
		updates["hidden_by_user_a"] = false
	}
	if err := r.DB.Model(&models.DmThread{}).Where("id=?", m.ThreadID).Updates(updates).Error; err != nil {
		return m, err
	}
	return m, nil
}

func (r *Repo) GetDmMessage(id uint) (*models.DmMessage, error) {
	var m models.DmMessage
	if err := r.DB.First(&m, id).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *Repo) GetDmMessages(threadID uint, limit int, beforeID, afterID uint) ([]models.DmMessage, error) {
	if limit <= 0 {
		limit = 50
	} else if limit > 100 {
		limit = 100
	}
	var list []models.DmMessage
	cutoff := time.Now().Add(-1 * time.Hour)
	q := r.DB.
		Where("thread_id = ? AND (deleted_at IS NULL OR deleted_at >= ?)", threadID, cutoff).
		Preload("ReplyTo")
	if afterID > 0 {
		if err := q.Where("id > ?", afterID).Order("id asc").Limit(limit).Find(&list).Error; err != nil {
			return nil, err
		}
		return list, nil
	}
	if beforeID > 0 {
		if err := q.Where("id < ?", beforeID).Order("id desc").Limit(limit).Find(&list).Error; err != nil {
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

func (r *Repo) GetDmMessagesWithUsers(threadID uint, limit int, beforeID, afterID uint) ([]models.DmMessage, []models.User, error) {
	messages, err := r.GetDmMessages(threadID, limit, beforeID, afterID)
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

	return messages, users, nil
}

func (r *Repo) DeleteDmMessage(messageID uint) error {
	now := time.Now()
	return r.DB.Model(&models.DmMessage{}).Where("id = ?", messageID).Update("deleted_at", now).Error
}
