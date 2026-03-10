package repository

import (
	"time"

	"bubble/src/db/models"

	"gorm.io/gorm"
)

// ──────────────────────────────────────────────
// GroupThread (群聊) repository methods
// ──────────────────────────────────────────────

func (r *Repo) CreateGroupThread(t *models.GroupThread) error {
	return r.DB.Create(t).Error
}

func (r *Repo) GetGroupThread(id uint) (*models.GroupThread, error) {
	var t models.GroupThread
	if err := r.DB.First(&t, id).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *Repo) UpdateGroupThread(id uint, updates map[string]interface{}) error {
	return r.DB.Model(&models.GroupThread{}).Where("id = ?", id).Updates(updates).Error
}

func (r *Repo) DeleteGroupThread(id uint) error {
	return r.DB.Delete(&models.GroupThread{}, id).Error
}

func (r *Repo) ListUserGroupThreads(userID uint, limit int, beforeID, afterID uint) ([]models.GroupThread, error) {
	// 获取未隐藏的群聊ID及其置顶状态
	type memberInfo struct {
		ThreadID uint
		Pinned   bool
	}
	var members []memberInfo
	r.DB.Model(&models.GroupThreadMember{}).
		Select("thread_id, pinned").
		Where("user_id = ? AND hidden = ?", userID, false).
		Find(&members)
	if len(members) == 0 {
		return []models.GroupThread{}, nil
	}
	threadIDs := make([]uint, len(members))
	pinnedMap := make(map[uint]bool)
	for i, m := range members {
		threadIDs[i] = m.ThreadID
		pinnedMap[m.ThreadID] = m.Pinned
	}
	q := r.DB.Where("id IN ?", threadIDs)
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	if afterID > 0 {
		q = q.Where("id > ?", afterID)
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var list []models.GroupThread
	err := q.Order("last_message_at DESC, id DESC").Limit(limit).Find(&list).Error
	if err != nil {
		return nil, err
	}
	// 填充 IsPinned 并按置顶优先排序
	for i := range list {
		list[i].IsPinned = pinnedMap[list[i].ID]
	}
	// 稳定排序：置顶在前
	pinnedThreads := make([]models.GroupThread, 0)
	normalThreads := make([]models.GroupThread, 0)
	for _, t := range list {
		if t.IsPinned {
			pinnedThreads = append(pinnedThreads, t)
		} else {
			normalThreads = append(normalThreads, t)
		}
	}
	result := append(pinnedThreads, normalThreads...)
	return result, nil
}

// ListHiddenGroupThreads 列出用户隐藏的群聊
func (r *Repo) ListHiddenGroupThreads(userID uint, limit int, beforeID, afterID uint) ([]models.GroupThread, error) {
	var threadIDs []uint
	r.DB.Model(&models.GroupThreadMember{}).Where("user_id = ? AND hidden = ?", userID, true).Pluck("thread_id", &threadIDs)
	if len(threadIDs) == 0 {
		return []models.GroupThread{}, nil
	}
	q := r.DB.Where("id IN ?", threadIDs)
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	if afterID > 0 {
		q = q.Where("id > ?", afterID)
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var list []models.GroupThread
	err := q.Order("last_message_at DESC, id DESC").Limit(limit).Find(&list).Error
	return list, err
}

// SetGroupThreadHidden 设置群聊隐藏状态
func (r *Repo) SetGroupThreadHidden(threadID, userID uint, hidden bool) error {
	return r.DB.Model(&models.GroupThreadMember{}).
		Where("thread_id = ? AND user_id = ?", threadID, userID).
		Update("hidden", hidden).Error
}

// SetGroupThreadPinned 设置群聊置顶状态
func (r *Repo) SetGroupThreadPinned(threadID, userID uint, pinned bool) error {
	return r.DB.Model(&models.GroupThreadMember{}).
		Where("thread_id = ? AND user_id = ?", threadID, userID).
		Update("pinned", pinned).Error
}

// ListPinnedGroupThreads 列出用户置顶的群聊
func (r *Repo) ListPinnedGroupThreads(userID uint) ([]models.GroupThread, error) {
	var threadIDs []uint
	r.DB.Model(&models.GroupThreadMember{}).Where("user_id = ? AND pinned = ?", userID, true).Pluck("thread_id", &threadIDs)
	if len(threadIDs) == 0 {
		return []models.GroupThread{}, nil
	}
	var list []models.GroupThread
	err := r.DB.Where("id IN ?", threadIDs).Order("last_message_at DESC, id DESC").Find(&list).Error
	return list, err
}

func (r *Repo) CountOwnedGroups(userID uint) (int64, error) {
	var count int64
	err := r.DB.Model(&models.GroupThread{}).Where("owner_id = ?", userID).Count(&count).Error
	return count, err
}

// ──────────────────────────────────────────────
// GroupThreadMember
// ──────────────────────────────────────────────

func (r *Repo) AddGroupMember(m *models.GroupThreadMember) error {
	return r.DB.Create(m).Error
}

func (r *Repo) RemoveGroupMember(threadID, userID uint) error {
	return r.DB.Where("thread_id = ? AND user_id = ?", threadID, userID).Delete(&models.GroupThreadMember{}).Error
}

func (r *Repo) GetGroupMember(threadID, userID uint) (*models.GroupThreadMember, error) {
	var m models.GroupThreadMember
	if err := r.DB.Where("thread_id = ? AND user_id = ?", threadID, userID).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *Repo) IsGroupMember(threadID, userID uint) bool {
	var count int64
	r.DB.Model(&models.GroupThreadMember{}).Where("thread_id = ? AND user_id = ?", threadID, userID).Count(&count)
	return count > 0
}

func (r *Repo) ListGroupMembers(threadID uint) ([]models.GroupThreadMember, error) {
	var list []models.GroupThreadMember
	err := r.DB.Where("thread_id = ?", threadID).Order("created_at ASC").Find(&list).Error
	return list, err
}

func (r *Repo) CountGroupMembers(threadID uint) (int64, error) {
	var count int64
	err := r.DB.Model(&models.GroupThreadMember{}).Where("thread_id = ?", threadID).Count(&count).Error
	return count, err
}

func (r *Repo) UpdateGroupMemberRole(threadID, userID uint, role string) error {
	return r.DB.Model(&models.GroupThreadMember{}).Where("thread_id = ? AND user_id = ?", threadID, userID).Update("role", role).Error
}

func (r *Repo) GetGroupMemberIDs(threadID uint) ([]uint, error) {
	var ids []uint
	err := r.DB.Model(&models.GroupThreadMember{}).Where("thread_id = ?", threadID).Pluck("user_id", &ids).Error
	return ids, err
}

// ──────────────────────────────────────────────
// GroupMessage
// ──────────────────────────────────────────────

func (r *Repo) CreateGroupMessage(m *models.GroupMessage) error {
	return r.DB.Create(m).Error
}

func (r *Repo) GetGroupMessage(id uint) (*models.GroupMessage, error) {
	var m models.GroupMessage
	if err := r.DB.First(&m, id).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *Repo) ListGroupMessages(threadID uint, limit int, beforeID, afterID uint) ([]models.GroupMessage, error) {
	q := r.DB.Where("thread_id = ? AND deleted_at IS NULL", threadID)
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	if afterID > 0 {
		q = q.Where("id > ?", afterID)
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var list []models.GroupMessage
	err := q.Order("id DESC").Limit(limit).Find(&list).Error
	return list, err
}

// ListGroupMessagesWithUsers 获取群聊消息列表，并返回相关的用户数据
func (r *Repo) ListGroupMessagesWithUsers(threadID uint, limit int, beforeID, afterID uint) ([]models.GroupMessage, []models.User, error) {
	msgs, err := r.ListGroupMessages(threadID, limit, beforeID, afterID)
	if err != nil {
		return nil, nil, err
	}
	if len(msgs) == 0 {
		return msgs, []models.User{}, nil
	}

	userIDSet := make(map[uint]struct{})
	for _, m := range msgs {
		userIDSet[m.AuthorID] = struct{}{}
	}
	userIDs := make([]uint, 0, len(userIDSet))
	for id := range userIDSet {
		userIDs = append(userIDs, id)
	}

	var users []models.User
	if len(userIDs) > 0 {
		if err := r.DB.Where("id IN ?", userIDs).Find(&users).Error; err != nil {
			return msgs, []models.User{}, err
		}
	}
	return msgs, users, nil
}

func (r *Repo) DeleteGroupMessage(id uint) error {
	now := time.Now()
	return r.DB.Model(&models.GroupMessage{}).Where("id = ?", id).Update("deleted_at", &now).Error
}

// ──────────────────────────────────────────────
// Group Pinned Messages
// ──────────────────────────────────────────────

// ListPinnedGroupMessages 列出群聊精华消息记录
func (r *Repo) ListPinnedGroupMessages(groupThreadID uint) ([]models.PinnedMessage, error) {
	var list []models.PinnedMessage
	err := r.DB.Where("group_thread_id = ?", groupThreadID).Order("created_at DESC").Find(&list).Error
	return list, err
}

// GetPinnedGroupMessage 查询群聊消息是否已被精华（通过 group_message_id）
func (r *Repo) GetPinnedGroupMessage(groupMessageID uint) (*models.PinnedMessage, error) {
	var pm models.PinnedMessage
	if err := r.DB.Where("group_message_id = ?", groupMessageID).First(&pm).Error; err != nil {
		return nil, err
	}
	return &pm, nil
}

// DeletePinnedGroupMessage 删除群聊精华记录（通过 group_message_id）
func (r *Repo) DeletePinnedGroupMessage(groupMessageID uint) error {
	return r.DB.Where("group_message_id = ?", groupMessageID).Delete(&models.PinnedMessage{}).Error
}

// ──────────────────────────────────────────────
// Group Favorite Messages
// ──────────────────────────────────────────────

// GetFavoriteGroupMessage 查询用户是否已收藏该群聊消息
func (r *Repo) GetFavoriteGroupMessage(userID, groupMessageID uint) (*models.FavoriteMessage, error) {
	var fm models.FavoriteMessage
	if err := r.DB.Where("user_id = ? AND group_message_id = ?", userID, groupMessageID).First(&fm).Error; err != nil {
		return nil, err
	}
	return &fm, nil
}

// ──────────────────────────────────────────────
// Group Member Search
// ──────────────────────────────────────────────

// SearchGroupMembers 在群聊成员中搜索（按用户名或群昵称模糊匹配）
func (r *Repo) SearchUserGroupThreads(userID uint, query string, limit int) ([]models.GroupThread, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	var threadIDs []uint
	r.DB.Model(&models.GroupThreadMember{}).Where("user_id = ?", userID).Pluck("thread_id", &threadIDs)
	if len(threadIDs) == 0 {
		return []models.GroupThread{}, nil
	}
	pattern := "%" + query + "%"
	var list []models.GroupThread
	err := r.DB.Where("id IN ? AND name LIKE ?", threadIDs, pattern).
		Order("last_message_at DESC").Limit(limit).Find(&list).Error
	return list, err
}

func (r *Repo) SearchGroupMembers(threadID uint, query string, limit int) ([]models.GroupThreadMember, error) {
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	pattern := "%" + query + "%"
	var members []models.GroupThreadMember
	err := r.DB.Where("thread_id = ?", threadID).
		Where(r.DB.Where("nickname LIKE ?", pattern).
			Or("user_id IN (?)", r.DB.Model(&models.User{}).Select("id").Where("name LIKE ?", pattern))).
		Limit(limit).
		Find(&members).Error
	return members, err
}

// ──────────────────────────────────────────────
// GroupJoinRequest (群聊加入申请)
// ──────────────────────────────────────────────

func (r *Repo) CreateGroupJoinRequest(req *models.GroupJoinRequest) error {
	return r.DB.Create(req).Error
}

func (r *Repo) GetGroupJoinRequestByID(id uint) (*models.GroupJoinRequest, error) {
	var req models.GroupJoinRequest
	if err := r.DB.First(&req, id).Error; err != nil {
		return nil, err
	}
	// Preload user info
	if u, err := r.GetUserByID(req.UserID); err == nil && u != nil {
		req.User = u
	}
	return &req, nil
}

func (r *Repo) GetPendingGroupJoinRequest(threadID, userID uint) (*models.GroupJoinRequest, error) {
	var req models.GroupJoinRequest
	if err := r.DB.Where("thread_id = ? AND user_id = ? AND status = ?", threadID, userID, "pending").First(&req).Error; err != nil {
		return nil, err
	}
	return &req, nil
}

func (r *Repo) GetGroupJoinRequestByUserAndThread(userID, threadID uint) (*models.GroupJoinRequest, error) {
	var req models.GroupJoinRequest
	if err := r.DB.Where("user_id = ? AND thread_id = ?", userID, threadID).Order("id DESC").First(&req).Error; err != nil {
		return nil, err
	}
	return &req, nil
}

func (r *Repo) ListGroupJoinRequests(threadID uint, offset, limit int) ([]*models.GroupJoinRequest, error) {
	var list []*models.GroupJoinRequest
	err := r.DB.Where("thread_id = ? AND status = ?", threadID, "pending").
		Order("created_at DESC").
		Offset(offset).Limit(limit).
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	for i := range list {
		if u, err := r.GetUserByID(list[i].UserID); err == nil && u != nil {
			list[i].User = u
		}
	}
	return list, nil
}

func (r *Repo) CountGroupJoinRequests(threadID uint) (int, error) {
	var count int64
	err := r.DB.Model(&models.GroupJoinRequest{}).Where("thread_id = ? AND status = ?", threadID, "pending").Count(&count).Error
	return int(count), err
}

func (r *Repo) UpdateGroupJoinRequest(req *models.GroupJoinRequest, fields map[string]any) error {
	return r.DB.Model(req).Updates(fields).Error
}

// ──────────────────────────────────────────────
// GroupAnnouncement (群公告)
// ──────────────────────────────────────────────

func (r *Repo) CreateGroupAnnouncement(a *models.GroupAnnouncement) error {
	return r.DB.Create(a).Error
}

func (r *Repo) GetGroupAnnouncement(id uint) (*models.GroupAnnouncement, error) {
	var a models.GroupAnnouncement
	if err := r.DB.Preload("Author").First(&a, id).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *Repo) ListGroupAnnouncementsPaginated(threadID uint, offset, limit int) ([]*models.GroupAnnouncement, error) {
	var list []*models.GroupAnnouncement
	err := r.DB.Where("thread_id = ?", threadID).
		Preload("Author").
		Order("is_pinned DESC, created_at DESC").
		Offset(offset).Limit(limit).
		Find(&list).Error
	return list, err
}

func (r *Repo) CountGroupAnnouncements(threadID uint) (int, error) {
	var count int64
	err := r.DB.Model(&models.GroupAnnouncement{}).Where("thread_id = ?", threadID).Count(&count).Error
	return int(count), err
}

func (r *Repo) UpdateGroupAnnouncement(a *models.GroupAnnouncement) error {
	return r.DB.Save(a).Error
}

func (r *Repo) DeleteGroupAnnouncement(id uint) error {
	return r.DB.Delete(&models.GroupAnnouncement{}, id).Error
}

func (r *Repo) PinGroupAnnouncement(threadID, announcementID uint) error {
	return r.DB.Transaction(func(tx *gorm.DB) error {
		// 取消当前置顶
		if err := tx.Model(&models.GroupAnnouncement{}).
			Where("thread_id = ? AND is_pinned = ?", threadID, true).
			Updates(map[string]any{"is_pinned": false, "pinned_at": nil}).Error; err != nil {
			return err
		}
		now := time.Now()
		return tx.Model(&models.GroupAnnouncement{}).
			Where("id = ? AND thread_id = ?", announcementID, threadID).
			Updates(map[string]any{"is_pinned": true, "pinned_at": now}).Error
	})
}

func (r *Repo) UnpinGroupAnnouncement(threadID, announcementID uint) error {
	return r.DB.Model(&models.GroupAnnouncement{}).
		Where("id = ? AND thread_id = ?", announcementID, threadID).
		Updates(map[string]any{"is_pinned": false, "pinned_at": nil}).Error
}

func (r *Repo) GetFeaturedGroupAnnouncement(threadID uint) (*models.GroupAnnouncement, error) {
	var a models.GroupAnnouncement
	err := r.DB.Where("thread_id = ? AND is_pinned = ?", threadID, true).
		Preload("Author").First(&a).Error
	if err == nil {
		return &a, nil
	}
	err = r.DB.Where("thread_id = ?", threadID).
		Preload("Author").Order("created_at DESC").First(&a).Error
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ──────────────────────────────────────────────
// GroupFile (群文件)
// ──────────────────────────────────────────────

func (r *Repo) CreateGroupFile(f *models.GroupFile) error {
	return r.DB.Create(f).Error
}

func (r *Repo) ListGroupFiles(threadID uint, limit int, before uint) ([]models.GroupFile, error) {
	var list []models.GroupFile
	q := r.DB.Where("thread_id = ?", threadID).Preload("Uploader")
	if before > 0 {
		q = q.Where("id < ?", before)
	}
	err := q.Order("id DESC").Limit(limit).Find(&list).Error
	return list, err
}

func (r *Repo) GetGroupFile(id uint) (*models.GroupFile, error) {
	var f models.GroupFile
	if err := r.DB.Preload("Uploader").First(&f, id).Error; err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *Repo) DeleteGroupFile(id uint) error {
	return r.DB.Delete(&models.GroupFile{}, id).Error
}

func (r *Repo) CountGroupFiles(threadID uint) (int64, error) {
	var count int64
	err := r.DB.Model(&models.GroupFile{}).Where("thread_id = ?", threadID).Count(&count).Error
	return count, err
}
