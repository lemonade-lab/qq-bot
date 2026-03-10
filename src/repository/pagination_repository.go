package repository

import (
	"bubble/src/db/models"
	"strings"
	"time"

	"gorm.io/gorm"
)

// ============== Guild Join Requests - Pagination Support ==============

// GetGuildJoinRequestByUserAndGuild finds a join request by user and guild.
func (r *Repo) GetGuildJoinRequestByUserAndGuild(userID, guildID uint) (*models.GuildJoinRequest, error) {
	var req models.GuildJoinRequest
	err := r.DB.Where("user_id = ? AND guild_id = ?", userID, guildID).
		Order("created_at DESC").
		First(&req).Error
	if err != nil {
		return nil, err
	}
	// 手动填充用户信息（该字段不是 GORM 关系）
	if user, uerr := r.GetUserByID(req.UserID); uerr == nil {
		req.User = user
	}
	return &req, nil
}

// ListGuildJoinRequests returns paginated join requests for a guild.
func (r *Repo) ListGuildJoinRequests(guildID uint, offset, limit int) ([]*models.GuildJoinRequest, error) {
	var requests []*models.GuildJoinRequest
	err := r.DB.Where("guild_id = ?", guildID).
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&requests).Error
	if err != nil {
		return nil, err
	}
	if len(requests) == 0 {
		return requests, nil
	}
	// 批量加载用户信息并回填到扩展字段
	idsSet := make(map[uint]struct{}, len(requests))
	for _, rqt := range requests {
		idsSet[rqt.UserID] = struct{}{}
	}
	ids := make([]uint, 0, len(idsSet))
	for id := range idsSet {
		ids = append(ids, id)
	}
	var users []models.User
	if uerr := r.DB.Where("id IN ?", ids).Find(&users).Error; uerr == nil {
		umap := make(map[uint]*models.User, len(users))
		for i := range users {
			u := users[i]
			umap[u.ID] = &u
		}
		for _, rqt := range requests {
			if u := umap[rqt.UserID]; u != nil {
				rqt.User = u
			}
		}
	}
	return requests, nil
}

// CountGuildJoinRequests counts total join requests for a guild.
func (r *Repo) CountGuildJoinRequests(guildID uint) (int, error) {
	var count int64
	err := r.DB.Model(&models.GuildJoinRequest{}).
		Where("guild_id = ?", guildID).
		Count(&count).Error
	return int(count), err
}

// UpdateGuildJoinRequest updates a join request with given fields.
func (r *Repo) UpdateGuildJoinRequest(req *models.GuildJoinRequest, fields map[string]any) error {
	return r.DB.Model(req).Updates(fields).Error
}

// ============== Paginated Lists ==============

// ListGuildMembers returns paginated guild members.
func (r *Repo) ListGuildMembers(guildID uint, offset, limit int) ([]*models.GuildMember, error) {
	var members []*models.GuildMember
	err := r.DB.Where("guild_id = ?", guildID).
		Order("id ASC").
		Offset(offset).
		Limit(limit).
		Find(&members).Error
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return members, nil
	}
	// 批量加载用户信息
	userIDs := make([]uint, 0, len(members))
	for _, m := range members {
		userIDs = append(userIDs, m.UserID)
	}
	var users []models.User
	if uerr := r.DB.Where("id IN ?", userIDs).Find(&users).Error; uerr == nil {
		umap := make(map[uint]*models.User, len(users))
		for i := range users {
			u := users[i]
			umap[u.ID] = &u
		}
		for _, m := range members {
			if u := umap[m.UserID]; u != nil {
				m.User = u
			}
		}
	}
	// 批量加载角色信息
	var mrs []models.MemberRole
	if r.DB.Where("guild_id = ? AND user_id IN ?", guildID, userIDs).Find(&mrs).Error == nil && len(mrs) > 0 {
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
		if r.DB.Where("guild_id = ? AND id IN ?", guildID, roleIDs).Find(&roles).Error == nil {
			rmap := make(map[uint]models.Role, len(roles))
			for _, rl := range roles {
				rmap[rl.ID] = rl
			}
			for _, m := range members {
				if rids := rolesByUser[m.UserID]; len(rids) > 0 {
					rs := make([]models.Role, 0, len(rids))
					for _, rid := range rids {
						if rl, ok := rmap[rid]; ok {
							rs = append(rs, rl)
						}
					}
					m.Roles = rs
				}
			}
		}
	}
	// 计算禁言状态
	now := time.Now()
	for _, m := range members {
		m.IsMuted = (m.MutedUntil != nil && m.MutedUntil.After(now))
	}
	return members, nil
}

// ListGuildMembersCursor returns members using cursor (guild_members.id) pagination.
func (r *Repo) ListGuildMembersCursor(guildID uint, limit int, before, after uint) ([]*models.GuildMember, error) {
	var members []*models.GuildMember
	q := r.DB.Where("guild_id = ?", guildID)
	if after > 0 {
		q = q.Where("id > ?", after).Order("id ASC").Limit(limit)
	} else if before > 0 {
		q = q.Where("id < ?", before).Order("id DESC").Limit(limit)
	} else {
		q = q.Order("id DESC").Limit(limit)
	}
	if err := q.Find(&members).Error; err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return members, nil
	}
	// 批量加载用户信息
	userIDs := make([]uint, 0, len(members))
	for _, m := range members {
		userIDs = append(userIDs, m.UserID)
	}
	var users []models.User
	if uerr := r.DB.Where("id IN ?", userIDs).Find(&users).Error; uerr == nil {
		umap := make(map[uint]*models.User, len(users))
		for i := range users {
			u := users[i]
			umap[u.ID] = &u
		}
		for _, m := range members {
			if u := umap[m.UserID]; u != nil {
				m.User = u
			}
		}
	}
	// 批量加载角色信息
	var mrs []models.MemberRole
	if r.DB.Where("guild_id = ? AND user_id IN ?", guildID, userIDs).Find(&mrs).Error == nil && len(mrs) > 0 {
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
		if r.DB.Where("guild_id = ? AND id IN ?", guildID, roleIDs).Find(&roles).Error == nil {
			rmap := make(map[uint]models.Role, len(roles))
			for _, rl := range roles {
				rmap[rl.ID] = rl
			}
			for _, m := range members {
				if rids := rolesByUser[m.UserID]; len(rids) > 0 {
					rs := make([]models.Role, 0, len(rids))
					for _, rid := range rids {
						if rl, ok := rmap[rid]; ok {
							rs = append(rs, rl)
						}
					}
					m.Roles = rs
				}
			}
		}
	}
	// 计算禁言状态
	now := time.Now()
	for _, m := range members {
		m.IsMuted = (m.MutedUntil != nil && m.MutedUntil.After(now))
	}
	return members, nil
}

// SearchGuildMembers searches guild members by name or nickname.
func (r *Repo) SearchGuildMembers(guildID uint, query string, limit int) ([]*models.GuildMember, error) {
	if query == "" || limit < 1 {
		return []*models.GuildMember{}, nil
	}

	// 先获取成员列表
	var members []*models.GuildMember
	err := r.DB.Where("guild_id = ?", guildID).Find(&members).Error
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return members, nil
	}

	// 批量加载用户信息
	userIDs := make([]uint, 0, len(members))
	for _, m := range members {
		userIDs = append(userIDs, m.UserID)
	}

	// 搜索用户名匹配的用户
	pattern := "%" + query + "%"
	var users []models.User
	err = r.DB.Where("id IN ? AND name LIKE ?", userIDs, pattern).Limit(limit).Find(&users).Error
	if err != nil {
		return nil, err
	}

	// 构建用户映射
	umap := make(map[uint]*models.User, len(users))
	for i := range users {
		u := users[i]
		umap[u.ID] = &u
	}

	// 筛选匹配的成员
	var result []*models.GuildMember
	for _, m := range members {
		// 检查用户名是否匹配
		if u := umap[m.UserID]; u != nil {
			m.User = u
			result = append(result, m)
			if len(result) >= limit {
				break
			}
			continue
		}
		// 检查服务器昵称是否匹配
		if m.TempNickname != "" && containsIgnoreCase(m.TempNickname, query) {
			// 加载用户信息
			if u, err := r.GetUserByID(m.UserID); err == nil {
				m.User = u
			}
			result = append(result, m)
			if len(result) >= limit {
				break
			}
		}
	}

	// 批量加载角色信息
	if len(result) > 0 {
		resultUserIDs := make([]uint, 0, len(result))
		for _, m := range result {
			resultUserIDs = append(resultUserIDs, m.UserID)
		}
		var mrs []models.MemberRole
		if r.DB.Where("guild_id = ? AND user_id IN ?", guildID, resultUserIDs).Find(&mrs).Error == nil && len(mrs) > 0 {
			roleIDsSet := make(map[uint]struct{}, len(mrs))
			rolesByUser := make(map[uint][]uint, len(resultUserIDs))
			for _, mr := range mrs {
				roleIDsSet[mr.RoleID] = struct{}{}
				rolesByUser[mr.UserID] = append(rolesByUser[mr.UserID], mr.RoleID)
			}
			roleIDs := make([]uint, 0, len(roleIDsSet))
			for id := range roleIDsSet {
				roleIDs = append(roleIDs, id)
			}
			var roles []models.Role
			if r.DB.Where("guild_id = ? AND id IN ?", guildID, roleIDs).Find(&roles).Error == nil {
				rmap := make(map[uint]models.Role, len(roles))
				for _, rl := range roles {
					rmap[rl.ID] = rl
				}
				for _, m := range result {
					if rids := rolesByUser[m.UserID]; len(rids) > 0 {
						rs := make([]models.Role, 0, len(rids))
						for _, rid := range rids {
							if rl, ok := rmap[rid]; ok {
								rs = append(rs, rl)
							}
						}
						m.Roles = rs
					}
				}
			}
		}
	}

	// 计算禁言状态
	now := time.Now()
	for _, m := range result {
		m.IsMuted = (m.MutedUntil != nil && m.MutedUntil.After(now))
	}

	return result, nil
}

// containsIgnoreCase checks if s contains substr (case-insensitive)
func containsIgnoreCase(s, substr string) bool {
	s = strings.ToLower(s)
	substr = strings.ToLower(substr)
	return strings.Contains(s, substr)
}

// CountGuildMembers counts total members in a guild.
func (r *Repo) CountGuildMembers(guildID uint) (int, error) {
	var count int64
	err := r.DB.Model(&models.GuildMember{}).
		Where("guild_id = ?", guildID).
		Count(&count).Error
	return int(count), err
}

// BatchCountGuildMembers 批量获取多个服务器的成员数
func (r *Repo) BatchCountGuildMembers(guildIDs []uint) (map[uint]int, error) {
	if len(guildIDs) == 0 {
		return make(map[uint]int), nil
	}

	type CountResult struct {
		GuildID uint  `gorm:"column:guild_id"`
		Count   int64 `gorm:"column:count"`
	}

	var results []CountResult
	err := r.DB.Model(&models.GuildMember{}).
		Select("guild_id, COUNT(*) as count").
		Where("guild_id IN ?", guildIDs).
		Group("guild_id").
		Find(&results).Error

	if err != nil {
		return nil, err
	}

	countMap := make(map[uint]int)
	for _, result := range results {
		countMap[result.GuildID] = int(result.Count)
	}

	return countMap, nil
}

// CountFriends counts total friends for a user.
func (r *Repo) CountFriends(userID uint) (int, error) {
	var count int64
	err := r.DB.Table("friendships").
		Where("(from_user_id = ? OR to_user_id = ?) AND status = ?", userID, userID, "accepted").
		Count(&count).Error
	return int(count), err
}

// CountFriendRequests counts pending friend requests for a user.
func (r *Repo) CountFriendRequests(userID uint) (int, error) {
	var count int64
	err := r.DB.Model(&models.Friendship{}).
		Where("to_user_id = ? AND status = ?", userID, "pending").
		Count(&count).Error
	return int(count), err
}

// ListFriendRequestsCursor returns pending friend requests using cursor (friendship id) pagination.
func (r *Repo) ListFriendRequestsCursor(userID uint, limit int, before, after uint) ([]*models.User, error) {
	var results []struct {
		models.User
		Fid uint `gorm:"column:id"`
	}
	q := `
		SELECT u.*, f.id
		FROM users u
		JOIN friendships f ON f.from_user_id = u.id
		WHERE f.to_user_id = ? AND f.status = 'pending'
	`
	// apply cursor conditions
	var args []any
	args = append(args, userID)
	if after > 0 {
		q += " AND f.id > ?"
		args = append(args, after)
		q += " ORDER BY f.id ASC LIMIT ?"
		args = append(args, limit)
	} else if before > 0 {
		q += " AND f.id < ?"
		args = append(args, before)
		q += " ORDER BY f.id DESC LIMIT ?"
		args = append(args, limit)
	} else {
		q += " ORDER BY f.id DESC LIMIT ?"
		args = append(args, limit)
	}
	if err := r.DB.Raw(q, args...).Scan(&results).Error; err != nil {
		return nil, err
	}
	users := make([]*models.User, len(results))
	for i := range results {
		u := results[i].User
		users[i] = &u
	}
	// if before pagination used, results are DESC; client may keep as-is for infinite scroll upwards
	return users, nil
}

// ListFriendsPaginated returns paginated friends for a user.
func (r *Repo) ListFriendsPaginated(userID uint, offset, limit int) ([]*models.User, error) {
	var results []struct {
		models.User
		NicknameFrom string
		NicknameTo   string
		FromUserID   uint
		ToUserID     uint
	}
	// 使用数据库分页而不是查询所有记录
	err := r.DB.Raw(`
		SELECT u.*, f.nickname_from, f.nickname_to, f.from_user_id, f.to_user_id 
		FROM users u
		JOIN friendships f ON (
			(f.from_user_id = ? AND f.to_user_id = u.id) OR
			(f.to_user_id = ? AND f.from_user_id = u.id)
		) AND f.status = 'accepted'
		ORDER BY u.id ASC
		LIMIT ? OFFSET ?
	`, userID, userID, limit, offset).Scan(&results).Error
	if err != nil {
		return nil, err
	}

	// 填充 Nickname 字段
	users := make([]*models.User, len(results))
	for i, r := range results {
		u := r.User
		// 判断当前用户是 from 还是 to，选择对应的 nickname
		if r.FromUserID == userID {
			u.Nickname = r.NicknameFrom
		} else {
			u.Nickname = r.NicknameTo
		}
		users[i] = &u
	}
	return users, nil
}

// ListFriendsCursor returns friends using cursor (friendships.id) pagination with nickname enrichment.
func (r *Repo) ListFriendsCursor(userID uint, limit int, before, after uint) ([]*models.User, error) {
	var results []struct {
		models.User
		NicknameFrom string
		NicknameTo   string
		FromUserID   uint
		ToUserID     uint
		Fid          uint `gorm:"column:id"`
	}
	base := `
		SELECT u.*, f.nickname_from, f.nickname_to, f.from_user_id, f.to_user_id, f.id
		FROM users u
		JOIN friendships f ON (
			(f.from_user_id = ? AND f.to_user_id = u.id) OR
			(f.to_user_id = ? AND f.from_user_id = u.id)
		) AND f.status = 'accepted'
	`
	var q string
	var args []any
	args = append(args, userID, userID)
	if after > 0 {
		q = base + " AND f.id > ? ORDER BY f.id ASC LIMIT ?"
		args = append(args, after, limit)
	} else if before > 0 {
		q = base + " AND f.id < ? ORDER BY f.id DESC LIMIT ?"
		args = append(args, before, limit)
	} else {
		q = base + " ORDER BY f.id DESC LIMIT ?"
		args = append(args, limit)
	}
	if err := r.DB.Raw(q, args...).Scan(&results).Error; err != nil {
		return nil, err
	}
	users := make([]*models.User, len(results))
	for i, r := range results {
		u := r.User
		if r.FromUserID == userID {
			u.Nickname = r.NicknameFrom
		} else {
			u.Nickname = r.NicknameTo
		}
		users[i] = &u
	}
	return users, nil
}

// ListDmThreadsPaginated returns paginated DM threads for a user.
func (r *Repo) ListDmThreadsPaginated(userID uint, offset, limit int) ([]*models.DmThread, error) {
	var threads []*models.DmThread
	err := r.DB.Where("(user_a_id = ? OR user_b_id = ?) AND NOT ((user_a_id = ? AND hidden_by_user_a = ?) OR (user_b_id = ? AND hidden_by_user_b = ?))",
		userID, userID, userID, true, userID, true).
		Order(gorm.Expr("CASE WHEN (user_a_id = ? AND pinned_by_user_a = ?) OR (user_b_id = ? AND pinned_by_user_b = ?) THEN 0 ELSE 1 END ASC", userID, true, userID, true)).
		Order("CASE WHEN last_message_at IS NULL THEN 1 ELSE 0 END ASC").
		Order("last_message_at DESC").
		Order("id DESC").
		Offset(offset).
		Limit(limit).
		Find(&threads).Error
	if err != nil {
		return nil, err
	}

	// 填充对端用户、置顶/屏蔽状态，以及最新一条消息
	for i := range threads {
		thread := threads[i]
		var peerID uint
		if thread.UserAID == userID {
			peerID = thread.UserBID
			thread.IsPinned = thread.PinnedByUserA
			thread.IsBlocked = thread.BlockedByUserA
		} else {
			peerID = thread.UserAID
			thread.IsPinned = thread.PinnedByUserB
			thread.IsBlocked = thread.BlockedByUserB
		}

		// 加载对方用户信息 + 好友备注
		if peerUser, uerr := r.GetUserByID(peerID); uerr == nil && peerUser != nil {
			var f models.Friendship
			ferr := r.DB.Where("((from_user_id = ? AND to_user_id = ?) OR (from_user_id = ? AND to_user_id = ?)) AND status = 'accepted'",
				userID, peerID, peerID, userID).First(&f).Error
			if ferr == nil {
				if f.FromUserID == userID {
					peerUser.Nickname = f.NicknameFrom
				} else {
					peerUser.Nickname = f.NicknameTo
				}
			}
			thread.PeerUser = peerUser
		}

		// 加载最新一条消息
		var lastMsg models.DmMessage
		cutoff := time.Now().Add(-1 * time.Hour)
		if lerr := r.DB.Where("thread_id = ? AND (deleted_at IS NULL OR deleted_at >= ?)", thread.ID, cutoff).Order("created_at DESC").First(&lastMsg).Error; lerr == nil {
			thread.LastMessage = &lastMsg
		}
	}

	return threads, nil
}

// ListDmThreadsCursor returns DM threads using cursor on dm_threads.id with enrichment.
func (r *Repo) ListDmThreadsCursor(userID uint, limit int, before, after uint) ([]*models.DmThread, error) {
	var threads []*models.DmThread
	q := r.DB.Where("(user_a_id = ? OR user_b_id = ?) AND NOT ((user_a_id = ? AND hidden_by_user_a = ?) OR (user_b_id = ? AND hidden_by_user_b = ?))",
		userID, userID, userID, true, userID, true)
	if after > 0 {
		q = q.Where("id > ?", after).Order("id ASC").Limit(limit)
	} else if before > 0 {
		q = q.Where("id < ?", before).Order("id DESC").Limit(limit)
	} else {
		q = q.Order("id DESC").Limit(limit)
	}
	if err := q.Find(&threads).Error; err != nil {
		return nil, err
	}
	// 填充对端用户、置顶/屏蔽状态，以及最新一条消息（保持与分页实现一致）
	for i := range threads {
		thread := threads[i]
		var peerID uint
		if thread.UserAID == userID {
			peerID = thread.UserBID
			thread.IsPinned = thread.PinnedByUserA
			thread.IsBlocked = thread.BlockedByUserA
		} else {
			peerID = thread.UserAID
			thread.IsPinned = thread.PinnedByUserB
			thread.IsBlocked = thread.BlockedByUserB
		}
		if peerUser, uerr := r.GetUserByID(peerID); uerr == nil && peerUser != nil {
			var f models.Friendship
			ferr := r.DB.Where("((from_user_id = ? AND to_user_id = ?) OR (from_user_id = ? AND to_user_id = ?)) AND status = 'accepted'",
				userID, peerID, peerID, userID).First(&f).Error
			if ferr == nil {
				if f.FromUserID == userID {
					peerUser.Nickname = f.NicknameFrom
				} else {
					peerUser.Nickname = f.NicknameTo
				}
			}
			thread.PeerUser = peerUser
		}
		var lastMsg models.DmMessage
		cutoff := time.Now().Add(-1 * time.Hour)
		if lerr := r.DB.Where("thread_id = ? AND (deleted_at IS NULL OR deleted_at >= ?)", thread.ID, cutoff).Order("created_at DESC").First(&lastMsg).Error; lerr == nil {
			thread.LastMessage = &lastMsg
		}
	}
	return threads, nil
}

// CountDmThreads counts total active DM threads for a user.
func (r *Repo) CountDmThreads(userID uint) (int, error) {
	var count int64
	err := r.DB.Model(&models.DmThread{}).
		Where("(user_a_id = ? OR user_b_id = ?) AND NOT ((user_a_id = ? AND hidden_by_user_a = ?) OR (user_b_id = ? AND hidden_by_user_b = ?))",
			userID, userID, userID, true, userID, true).
		Count(&count).Error
	return int(count), err
}

// ListHiddenDmThreads 列出用户隐藏的私信线程
func (r *Repo) ListHiddenDmThreads(userID uint, offset, limit int) ([]*models.DmThread, error) {
	var threads []*models.DmThread
	err := r.DB.Where("(user_a_id = ? OR user_b_id = ?) AND ((user_a_id = ? AND hidden_by_user_a = ?) OR (user_b_id = ? AND hidden_by_user_b = ?))",
		userID, userID, userID, true, userID, true).
		Order("last_message_at DESC, id DESC").
		Offset(offset).Limit(limit).
		Find(&threads).Error
	if err != nil {
		return nil, err
	}

	for i := range threads {
		thread := threads[i]
		var peerID uint
		if thread.UserAID == userID {
			peerID = thread.UserBID
			thread.IsPinned = thread.PinnedByUserA
			thread.IsBlocked = thread.BlockedByUserA
		} else {
			peerID = thread.UserAID
			thread.IsPinned = thread.PinnedByUserB
			thread.IsBlocked = thread.BlockedByUserB
		}
		thread.IsHidden = true
		if peerUser, uerr := r.GetUserByID(peerID); uerr == nil && peerUser != nil {
			var f models.Friendship
			ferr := r.DB.Where("((from_user_id = ? AND to_user_id = ?) OR (from_user_id = ? AND to_user_id = ?)) AND status = 'accepted'",
				userID, peerID, peerID, userID).First(&f).Error
			if ferr == nil {
				if f.FromUserID == userID {
					peerUser.Nickname = f.NicknameFrom
				} else {
					peerUser.Nickname = f.NicknameTo
				}
			}
			thread.PeerUser = peerUser
		}
		var lastMsg models.DmMessage
		cutoff := time.Now().Add(-1 * time.Hour)
		if lerr := r.DB.Where("thread_id = ? AND (deleted_at IS NULL OR deleted_at >= ?)", thread.ID, cutoff).Order("created_at DESC").First(&lastMsg).Error; lerr == nil {
			thread.LastMessage = &lastMsg
		}
	}
	return threads, nil
}

// CountHiddenDmThreads 统计用户隐藏的私信数量
func (r *Repo) CountHiddenDmThreads(userID uint) (int, error) {
	var count int64
	err := r.DB.Model(&models.DmThread{}).
		Where("(user_a_id = ? OR user_b_id = ?) AND ((user_a_id = ? AND hidden_by_user_a = ?) OR (user_b_id = ? AND hidden_by_user_b = ?))",
			userID, userID, userID, true, userID, true).
		Count(&count).Error
	return int(count), err
}

// SetDmThreadHidden 设置私信线程隐藏状态
func (r *Repo) SetDmThreadHidden(threadID, userID uint, hidden bool) error {
	var thread models.DmThread
	if err := r.DB.First(&thread, threadID).Error; err != nil {
		return err
	}
	updates := make(map[string]interface{})
	if thread.UserAID == userID {
		updates["hidden_by_user_a"] = hidden
	} else if thread.UserBID == userID {
		updates["hidden_by_user_b"] = hidden
	} else {
		return gorm.ErrRecordNotFound
	}
	return r.DB.Model(&thread).Updates(updates).Error
}

// ListAnnouncementsPaginated returns paginated announcements for a guild.
func (r *Repo) ListAnnouncementsPaginated(guildID uint, offset, limit int) ([]*models.Announcement, error) {
	var announcements []*models.Announcement
	err := r.DB.Where("guild_id = ?", guildID).
		Preload("Author").
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&announcements).Error
	return announcements, err
}

// ListAnnouncementsCursor returns announcements using cursor based on id.
func (r *Repo) ListAnnouncementsCursor(guildID uint, limit int, before, after uint) ([]*models.Announcement, error) {
	var list []*models.Announcement
	q := r.DB.Where("guild_id = ?", guildID).Preload("Author")
	if after > 0 {
		q = q.Where("id > ?", after).Order("id ASC").Limit(limit)
	} else if before > 0 {
		q = q.Where("id < ?", before).Order("id DESC").Limit(limit)
	} else {
		q = q.Order("id DESC").Limit(limit)
	}
	if err := q.Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// CountAnnouncements counts total announcements for a guild.
func (r *Repo) CountAnnouncements(guildID uint) (int, error) {
	var count int64
	err := r.DB.Model(&models.Announcement{}).
		Where("guild_id = ?", guildID).
		Count(&count).Error
	return int(count), err
}
