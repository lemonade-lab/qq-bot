package service

import (
	"bubble/src/db/models"
	"errors"
	"strings"
	"time"
)

// ============== Guild Join Requests ==============

// ApplyGuildJoin creates a new guild join request.
func (s *Service) ApplyGuildJoin(guildID, userID uint, note string) (*models.GuildJoinRequest, *models.Guild, *models.UserNotification, error) {
	// Check if user is already a member
	isMember, err := s.Repo.IsMember(guildID, userID)
	if err != nil {
		return nil, nil, nil, err
	}
	if isMember {
		return nil, nil, nil, ErrAlreadyExists
	}

	// Get guild info (and autoJoinMode)
	guild, err := s.Repo.GetGuild(guildID)
	if err != nil || guild == nil {
		return nil, nil, nil, ErrNotFound
	}
	mode := guild.AutoJoinMode
	if strings.TrimSpace(mode) == "" {
		mode = AutoJoinRequireApproval
	}

	// If guild allows auto-join, join directly (no join request)
	switch mode {
	case AutoJoinNoApproval:
		if err := s.Repo.AddMember(guildID, userID); err != nil {
			return nil, nil, nil, err
		}
		// 清理可能存在的pending申请（避免残留）
		_ = s.Repo.DB.Where("guild_id = ? AND user_id = ? AND status = ?", guildID, userID, "pending").Delete(&models.GuildJoinRequest{}).Error
		s.ClearGuildMemberCountCache(guildID)
		return nil, guild, nil, nil
	case AutoJoinNoApprovalUnder100:
		count, err := s.Repo.CountGuildMembers(guildID)
		if err != nil {
			return nil, nil, nil, err
		}
		if count < 100 {
			if err := s.Repo.AddMember(guildID, userID); err != nil {
				return nil, nil, nil, err
			}
			_ = s.Repo.DB.Where("guild_id = ? AND user_id = ? AND status = ?", guildID, userID, "pending").Delete(&models.GuildJoinRequest{}).Error
			s.ClearGuildMemberCountCache(guildID)
			return nil, guild, nil, nil
		}
		// else: fallthrough to require approval
	}

	// Check if there's already a pending request
	pending, err := s.Repo.GetPendingGuildJoinRequest(guildID, userID)
	if err == nil && pending != nil {
		return nil, nil, nil, ErrAlreadyExists
	}

	// Create the join request
	request := &models.GuildJoinRequest{
		GuildID: guildID,
		UserID:  userID,
		Note:    note,
		Status:  "pending",
	}

	if err := s.Repo.CreateGuildJoinRequest(request); err != nil {
		return nil, nil, nil, err
	}

	// Create notification for guild owner
	notif, err := s.Repo.CreateGuildJoinRequestNotification(guild.OwnerID, userID, guildID)
	if err != nil {
		// Non-critical error, log but continue
		// Still return the request successfully created
	}

	// Preload user info
	request, _ = s.Repo.GetGuildJoinRequestByID(request.ID)
	return request, guild, notif, nil
}

// ListGuildJoinRequests returns paginated guild join requests (for admins).
func (s *Service) ListGuildJoinRequests(guildID, requesterID uint, page, limit int) ([]*models.GuildJoinRequest, int, error) {
	// Check if requester has permission
	ok, err := s.HasGuildPerm(guildID, requesterID, PermManageGuild)
	if err != nil {
		return nil, 0, err
	}
	if !ok {
		return nil, 0, ErrUnauthorized
	}

	// Get total count
	total, err := s.Repo.CountGuildJoinRequests(guildID)
	if err != nil {
		return nil, 0, err
	}

	// Get paginated results
	offset := (page - 1) * limit
	requests, err := s.Repo.ListGuildJoinRequests(guildID, offset, limit)
	if err != nil {
		return nil, 0, err
	}

	return requests, total, nil
}

// GetUserGuildJoinRequestStatus gets a user's join request status for a guild.
func (s *Service) GetUserGuildJoinRequestStatus(guildID, userID uint) (*models.GuildJoinRequest, error) {
	// User can only check their own status
	request, err := s.Repo.GetGuildJoinRequestByUserAndGuild(userID, guildID)
	if err != nil {
		return nil, ErrNotFound
	}
	return request, nil
}

// ApproveGuildJoinRequest approves a join request and adds the user to the guild.
func (s *Service) ApproveGuildJoinRequest(guildID, requestID, handlerID uint) error {
	// Check permission
	ok, err := s.HasGuildPerm(guildID, handlerID, PermManageGuild)
	if err != nil {
		return err
	}
	if !ok {
		return ErrUnauthorized
	}

	// Get the request
	request, err := s.Repo.GetGuildJoinRequestByID(requestID)
	if err != nil {
		return ErrNotFound
	}

	if request.GuildID != guildID {
		return errors.New("加入申请不属于该服务器")
	}

	if request.Status != "pending" {
		return errors.New("加入申请不是待处理状态")
	}

	// 检查等级人数上限
	guild, err := s.Repo.GetGuild(guildID)
	if err != nil || guild == nil {
		return ErrNotFound
	}
	memberLimit := models.GetMemberLimitByLevel(guild.Level)
	currentCount, err := s.Repo.CountGuildMembers(guildID)
	if err != nil {
		return err
	}
	if currentCount >= memberLimit {
		return &Err{Code: 403, Msg: "服务器成员已达上限"}
	}

	// Add the user as a member (如果已经是成员，FirstOrCreate 会自动处理，不报错)
	if err := s.Repo.AddMember(guildID, request.UserID); err != nil {
		// 即使添加成员失败，我们仍然更新申请状态为approved
		// 因为可能用户已经通过其他方式加入了
	} else {
		// 成功添加成员，清除缓存
		s.ClearGuildMemberCountCache(guildID)
	}

	// Update the request status
	now := time.Now()
	fields := map[string]any{
		"status":     "approved",
		"handled_by": handlerID,
		"handled_at": now,
	}
	if err := s.Repo.UpdateGuildJoinRequest(request, fields); err != nil {
		return err
	}

	return nil
}

// RejectGuildJoinRequest rejects a join request.
func (s *Service) RejectGuildJoinRequest(guildID, requestID, handlerID uint) error {
	// Check permission
	ok, err := s.HasGuildPerm(guildID, handlerID, PermManageGuild)
	if err != nil {
		return err
	}
	if !ok {
		return ErrUnauthorized
	}

	// Get the request
	request, err := s.Repo.GetGuildJoinRequestByID(requestID)
	if err != nil {
		return ErrNotFound
	}

	if request.GuildID != guildID {
		return errors.New("加入申请不属于该服务器")
	}

	if request.Status != "pending" {
		return errors.New("加入申请不是待处理状态")
	}

	// Update the request status
	now := time.Now()
	fields := map[string]any{
		"status":     "rejected",
		"handled_by": handlerID,
		"handled_at": now,
	}
	if err := s.Repo.UpdateGuildJoinRequest(request, fields); err != nil {
		return err
	}

	return nil
}

// ============== Paginated Lists ==============

// ListMembers returns paginated guild members.
func (s *Service) ListMembers(guildID uint, page, limit int) ([]*models.GuildMember, int, error) {
	// Get total count
	total, err := s.Repo.CountGuildMembers(guildID)
	if err != nil {
		return nil, 0, err
	}

	// Get paginated results
	offset := (page - 1) * limit
	members, err := s.Repo.ListGuildMembers(guildID, offset, limit)
	if err != nil {
		return nil, 0, err
	}

	return members, total, nil
}

// ListMembersCursor returns members using cursor pagination.
func (s *Service) ListMembersCursor(guildID uint, limit int, before, after uint) ([]*models.GuildMember, error) {
	return s.Repo.ListGuildMembersCursor(guildID, limit, before, after)
}

// SearchMembers searches guild members by name or nickname.
func (s *Service) SearchMembers(guildID uint, query string, limit int) ([]*models.GuildMember, error) {
	return s.Repo.SearchGuildMembers(guildID, query, limit)
}

// ListFriends returns paginated friends for a user.
func (s *Service) ListFriends(userID uint, page, limit int) ([]*models.User, int, error) {
	// Get total count
	total, err := s.Repo.CountFriends(userID)
	if err != nil {
		return nil, 0, err
	}

	// Use database-level pagination instead of loading all friends
	offset := (page - 1) * limit
	friends, err := s.Repo.ListFriendsPaginated(userID, offset, limit)
	if err != nil {
		return nil, 0, err
	}

	return friends, total, nil
}

// ListFriendsCursor returns friends using cursor pagination.
func (s *Service) ListFriendsCursor(userID uint, limit int, before, after uint) ([]*models.User, error) {
	return s.Repo.ListFriendsCursor(userID, limit, before, after)
}

// ListFriendRequestsCursor returns pending friend requests using cursor pagination.
func (s *Service) ListFriendRequestsCursor(userID uint, limit int, before, after uint) ([]*models.User, error) {
	return s.Repo.ListFriendRequestsCursor(userID, limit, before, after)
}

// ListFriendRequests returns paginated friend requests for a user.
func (s *Service) ListFriendRequests(userID uint, page, limit int) ([]*models.User, int, error) {
	// Get total count
	total, err := s.Repo.CountFriendRequests(userID)
	if err != nil {
		return nil, 0, err
	}

	// Get all friend requests (existing method returns all)
	allRequests, err := s.Repo.ListFriendRequests(userID)
	if err != nil {
		return nil, 0, err
	}

	// Manual pagination
	start := (page - 1) * limit
	end := start + limit
	if start >= len(allRequests) {
		return []*models.User{}, total, nil
	}
	if end > len(allRequests) {
		end = len(allRequests)
	}

	// Convert to pointers
	requests := make([]*models.User, end-start)
	for i := start; i < end; i++ {
		req := allRequests[i]
		requests[i-start] = &req
	}

	return requests, total, nil
}

// ListDmThreads returns paginated DM threads for a user.
func (s *Service) ListDmThreads(userID uint, page, limit int) ([]*models.DmThread, int, error) {
	// Get total count
	total, err := s.Repo.CountDmThreads(userID)
	if err != nil {
		return nil, 0, err
	}

	// Get paginated results
	offset := (page - 1) * limit
	threads, err := s.Repo.ListDmThreadsPaginated(userID, offset, limit)
	if err != nil {
		return nil, 0, err
	}

	return threads, total, nil
}

// ListDmThreadsCursor returns DM threads using cursor pagination.
func (s *Service) ListDmThreadsCursor(userID uint, limit int, before, after uint) ([]*models.DmThread, error) {
	return s.Repo.ListDmThreadsCursor(userID, limit, before, after)
}

// ListAnnouncements returns paginated announcements for a guild.
func (s *Service) ListAnnouncements(guildID uint, page, limit int) ([]*models.Announcement, int, error) {
	// Get total count
	total, err := s.Repo.CountAnnouncements(guildID)
	if err != nil {
		return nil, 0, err
	}

	// Get paginated results
	offset := (page - 1) * limit
	announcements, err := s.Repo.ListAnnouncementsPaginated(guildID, offset, limit)
	if err != nil {
		return nil, 0, err
	}

	return announcements, total, nil
}

// ListAnnouncementsCursor returns announcements using cursor pagination.
func (s *Service) ListAnnouncementsCursor(guildID uint, limit int, before, after uint) ([]*models.Announcement, error) {
	return s.Repo.ListAnnouncementsCursor(guildID, limit, before, after)
}

// ============== Channel Categories ==============

// ListChannelCategories returns all categories for a guild.
func (s *Service) ListChannelCategories(guildID uint) ([]models.ChannelCategory, error) {
	categories, err := s.Repo.ListChannelCategories(guildID)
	if err != nil {
		return nil, err
	}
	return categories, nil
}

// CreateChannelCategory creates a new channel category.
func (s *Service) CreateChannelCategory(guildID, userID uint, name string) (*models.ChannelCategory, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrBadRequest
	}

	// Check permission
	ok, err := s.HasGuildPerm(guildID, userID, PermManageChannels)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrUnauthorized
	}

	category := &models.ChannelCategory{
		GuildID:   guildID,
		Name:      name,
		SortOrder: 0,
	}

	if err := s.Repo.CreateChannelCategory(category); err != nil {
		return nil, err
	}

	return category, nil
}

// UpdateChannelCategory updates a channel category.
func (s *Service) UpdateChannelCategory(guildID, categoryID, userID uint, name string) (*models.ChannelCategory, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrBadRequest
	}

	// Check permission
	ok, err := s.HasGuildPerm(guildID, userID, PermManageChannels)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrUnauthorized
	}

	// Get the category
	category, err := s.Repo.GetChannelCategory(categoryID)
	if err != nil {
		return nil, ErrNotFound
	}

	if category.GuildID != guildID {
		return nil, ErrBadRequest
	}

	// Update fields
	if err := s.Repo.UpdateChannelCategory(categoryID, name); err != nil {
		return nil, err
	}

	category.Name = name
	return category, nil
}

// DeleteChannelCategory deletes a channel category.
func (s *Service) DeleteChannelCategory(guildID, categoryID, userID uint) error {
	// Check permission
	ok, err := s.HasGuildPerm(guildID, userID, PermManageChannels)
	if err != nil {
		return err
	}
	if !ok {
		return ErrUnauthorized
	}

	// Get the category
	category, err := s.Repo.GetChannelCategory(categoryID)
	if err != nil {
		return ErrNotFound
	}

	if category.GuildID != guildID {
		return ErrBadRequest
	}

	// Delete channels' category references first
	if err := s.Repo.RemoveCategoryFromChannels(categoryID); err != nil {
		return err
	}

	// Delete the category
	return s.Repo.DeleteChannelCategory(categoryID)
}

// ReorderChannelCategories批量更新频道分类排序
func (s *Service) ReorderChannelCategories(guildID, userID uint, orders []struct {
	ID        uint `json:"id"`
	SortOrder int  `json:"sortOrder"`
}) error {
	// 检查权限
	ok, err := s.HasGuildPerm(guildID, userID, PermManageChannels)
	if err != nil {
		return err
	}
	if !ok {
		return ErrUnauthorized
	}
	// 验证所有分类都属于该服务器
	for _, o := range orders {
		cat, err := s.Repo.GetChannelCategory(o.ID)
		if err != nil || cat == nil {
			return ErrNotFound
		}
		if cat.GuildID != guildID {
			return ErrBadRequest
		}
	}
	// 批量更新
	return s.Repo.BatchUpdateCategoryOrders(orders)
}
