package repository

import (
	"context"
	"fmt"
	"time"

	"bubble/src/db/models"

	"gorm.io/gorm"
)

// ──────────────────────────────────────────────
// Guild repository methods
// ──────────────────────────────────────────────

func (r *Repo) CreateGuild(g *models.Guild) error { return r.DB.Create(g).Error }

func (r *Repo) ListGuilds() ([]models.Guild, error) {
	var list []models.Guild
	return list, r.DB.Where("deleted_at IS NULL").Order("id asc").Find(&list).Error
}

func (r *Repo) ListGuildsByUserID(userID uint) ([]models.Guild, error) {
	var list []models.Guild
	err := r.DB.Table("guilds").
		Joins("JOIN guild_members ON guild_members.guild_id = guilds.id").
		Where("guild_members.user_id = ? AND guilds.deleted_at IS NULL", userID).
		Order("guild_members.sort_order asc, guilds.id asc").
		Find(&list).Error
	return list, err
}

// ListGuildsByUserIDCursor 游标分页获取用户加入的服务器列表
func (r *Repo) ListGuildsByUserIDCursor(userID uint, limit int, beforeID, afterID uint) ([]models.Guild, error) {
	var list []models.Guild
	query := r.DB.Table("guilds").
		Joins("JOIN guild_members ON guild_members.guild_id = guilds.id").
		Where("guild_members.user_id = ? AND guilds.deleted_at IS NULL", userID)

	if beforeID > 0 {
		query = query.Where("guilds.id < ?", beforeID)
	}
	if afterID > 0 {
		query = query.Where("guilds.id > ?", afterID)
	}

	err := query.Order("guild_members.sort_order asc, guilds.id asc").
		Limit(limit).
		Find(&list).Error
	return list, err
}

// ListGuildsByUserIDPage 页面分页获取用户加入的服务器列表
func (r *Repo) ListGuildsByUserIDPage(userID uint, page, limit int) ([]models.Guild, int64, error) {
	var list []models.Guild
	var total int64

	countQuery := r.DB.Table("guild_members").
		Where("user_id = ?", userID).
		Joins("JOIN guilds ON guilds.id = guild_members.guild_id AND guilds.deleted_at IS NULL")
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * limit
	err := r.DB.Table("guilds").
		Joins("JOIN guild_members ON guild_members.guild_id = guilds.id").
		Where("guild_members.user_id = ? AND guilds.deleted_at IS NULL", userID).
		Order("guild_members.sort_order asc, guilds.id asc").
		Offset(offset).
		Limit(limit).
		Find(&list).Error

	return list, total, err
}

func (r *Repo) DeleteGuild(id uint) error {
	now := time.Now()
	return r.DB.Model(&models.Guild{}).Where("id = ?", id).Update("deleted_at", now).Error
}

func (r *Repo) UpdateGuild(id uint, fields map[string]interface{}) error {
	return r.DB.Model(&models.Guild{}).Where("id = ?", id).Updates(fields).Error
}

func (r *Repo) GetGuild(id uint) (*models.Guild, error) {
	var g models.Guild
	if err := r.DB.Where("id = ? AND deleted_at IS NULL", id).First(&g).Error; err != nil {
		return nil, err
	}
	return &g, nil
}

// SearchGuildsByName 按名称模糊查询服务器
func (r *Repo) SearchGuildsByName(q string, limit int, category string) ([]models.Guild, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	like := "%" + q + "%"
	db := r.DB.Where("name LIKE ? AND deleted_at IS NULL AND is_private = ?", like, false)
	if category != "" {
		db = db.Where("category = ?", category)
	}
	var list []models.Guild
	err := db.Order("id asc").Limit(limit).Find(&list).Error
	return list, err
}

// ListTopGuilds 根据成员数量返回热门服务器
func (r *Repo) ListTopGuilds(limit int) ([]models.Guild, error) {
	if limit <= 0 {
		limit = 6
	}
	if limit > 50 {
		limit = 50
	}
	var list []models.Guild
	err := r.DB.Table("guilds").
		Select("guilds.*").
		Joins("LEFT JOIN guild_members gm ON gm.guild_id = guilds.id").
		Where("guilds.deleted_at IS NULL AND guilds.is_private = ?", false).
		Group("guilds.id").
		Order("COUNT(gm.user_id) DESC").
		Order("guilds.id ASC").
		Limit(limit).Find(&list).Error
	return list, err
}

// ListGuildsByCategory 按分类查询服务器，按成员数降序排序
func (r *Repo) ListGuildsByCategory(category string, limit int) ([]models.Guild, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var list []models.Guild
	err := r.DB.Table("guilds").
		Select("guilds.*").
		Joins("LEFT JOIN guild_members gm ON gm.guild_id = guilds.id").
		Where("guilds.deleted_at IS NULL AND guilds.is_private = ? AND guilds.category = ?", false, category).
		Group("guilds.id").
		Order("COUNT(gm.user_id) DESC").
		Order("guilds.id ASC").
		Limit(limit).Find(&list).Error
	return list, err
}

// ──────────────────────────────────────────────
// Members
// ──────────────────────────────────────────────

func (r *Repo) AddMember(guildID, userID uint) error {
	gm := models.GuildMember{GuildID: guildID, UserID: userID}
	return r.DB.Where("guild_id=? AND user_id=?", guildID, userID).FirstOrCreate(&gm).Error
}

func (r *Repo) RemoveMember(guildID, userID uint) error {
	return r.DB.Where("guild_id=? AND user_id=?", guildID, userID).Delete(&models.GuildMember{}).Error
}

func (r *Repo) ListMembers(guildID uint) ([]models.GuildMember, error) {
	var list []models.GuildMember
	if err := r.DB.Where("guild_id=?", guildID).Find(&list).Error; err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return list, nil
	}
	// Batch load users
	userIDs := make([]uint, 0, len(list))
	for i := range list {
		userIDs = append(userIDs, list[i].UserID)
	}
	var users []models.User
	if err := r.DB.Where("id IN ?", userIDs).Find(&users).Error; err == nil {
		umap := make(map[uint]*models.User, len(users))
		for i := range users {
			u := users[i]
			umap[u.ID] = &u
		}
		for i := range list {
			if umap[list[i].UserID] != nil {
				list[i].User = umap[list[i].UserID]
			}
		}
	}
	// Batch load member_roles and roles
	var mrs []models.MemberRole
	if err := r.DB.Where("guild_id = ? AND user_id IN ?", guildID, userIDs).Find(&mrs).Error; err == nil && len(mrs) > 0 {
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
		if err := r.DB.Where("guild_id = ? AND id IN ?", guildID, roleIDs).Find(&roles).Error; err == nil {
			rmap := make(map[uint]models.Role, len(roles))
			for _, rle := range roles {
				rmap[rle.ID] = rle
			}
			for i := range list {
				rids := rolesByUser[list[i].UserID]
				if len(rids) > 0 {
					rs := make([]models.Role, 0, len(rids))
					for _, rid := range rids {
						if rl, ok := rmap[rid]; ok {
							rs = append(rs, rl)
						}
					}
					list[i].Roles = rs
				}
			}
		}
	}
	// Compute muted flag
	now := time.Now()
	for i := range list {
		list[i].IsMuted = (list[i].MutedUntil != nil && list[i].MutedUntil.After(now))
	}
	return list, nil
}

func (r *Repo) IsMember(guildID, userID uint) (bool, error) {
	var cnt int64
	if err := r.DB.Model(&models.GuildMember{}).Where("guild_id=? AND user_id=?", guildID, userID).Count(&cnt).Error; err != nil {
		return false, err
	}
	return cnt > 0, nil
}

func (r *Repo) GetSharedGuilds(userID1, userID2 uint) ([]models.Guild, error) {
	var guilds []models.Guild
	err := r.DB.
		Table("guilds").
		Joins("INNER JOIN guild_members gm1 ON gm1.guild_id = guilds.id AND gm1.user_id = ?", userID1).
		Joins("INNER JOIN guild_members gm2 ON gm2.guild_id = guilds.id AND gm2.user_id = ?", userID2).
		Where("guilds.deleted_at IS NULL").
		Find(&guilds).Error
	return guilds, err
}

func (r *Repo) UpdateGuildMemberOrder(guildID, userID uint, sortOrder int) error {
	return r.DB.Model(&models.GuildMember{}).
		Where("guild_id = ? AND user_id = ?", guildID, userID).
		Update("sort_order", sortOrder).Error
}

func (r *Repo) BatchUpdateGuildMemberOrders(userID uint, updates []struct {
	GuildID   uint
	SortOrder int
}) error {
	return r.DB.Transaction(func(tx *gorm.DB) error {
		for _, u := range updates {
			if err := tx.Model(&models.GuildMember{}).
				Where("guild_id = ? AND user_id = ?", u.GuildID, userID).
				Update("sort_order", u.SortOrder).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Repo) MuteMember(guildID, userID uint, mutedUntil time.Time) error {
	return r.DB.Model(&models.GuildMember{}).
		Where("guild_id = ? AND user_id = ?", guildID, userID).
		Update("muted_until", mutedUntil).Error
}

func (r *Repo) UnmuteMember(guildID, userID uint) error {
	return r.DB.Model(&models.GuildMember{}).
		Where("guild_id = ? AND user_id = ?", guildID, userID).
		Update("muted_until", nil).Error
}

func (r *Repo) UpdateMemberNickname(guildID, userID uint, nickname string) error {
	return r.DB.Model(&models.GuildMember{}).
		Where("guild_id = ? AND user_id = ?", guildID, userID).
		Update("temp_nickname", nickname).Error
}

// CountGuildMembers is defined in pagination_repository.go

// ──────────────────────────────────────────────
// Guild Join Requests
// ──────────────────────────────────────────────

func (r *Repo) CreateGuildJoinRequest(gjr *models.GuildJoinRequest) error {
	return r.DB.Create(gjr).Error
}

func (r *Repo) ListGuildJoinRequestsByGuild(guildID uint) ([]models.GuildJoinRequest, error) {
	var list []models.GuildJoinRequest
	err := r.DB.Where("guild_id = ?", guildID).Order("id desc").Find(&list).Error
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return list, nil
	}
	// 批量加载用户信息
	userIDs := make([]uint, len(list))
	for i, req := range list {
		userIDs[i] = req.UserID
	}
	var users []models.User
	r.DB.Where("id IN ?", userIDs).Find(&users)
	userMap := make(map[uint]*models.User, len(users))
	for i := range users {
		userMap[users[i].ID] = &users[i]
	}
	for i := range list {
		if u, ok := userMap[list[i].UserID]; ok {
			list[i].User = u
		}
	}
	return list, nil
}

func (r *Repo) GetGuildJoinRequestByID(id uint) (*models.GuildJoinRequest, error) {
	var g models.GuildJoinRequest
	if err := r.DB.First(&g, id).Error; err != nil {
		return nil, err
	}
	return &g, nil
}

func (r *Repo) UpdateGuildJoinRequestStatus(id uint, status string, handledBy *uint) error {
	now := time.Now()
	updates := map[string]any{"status": status, "handled_at": now}
	if handledBy != nil {
		updates["handled_by"] = *handledBy
	}
	return r.DB.Model(&models.GuildJoinRequest{}).Where("id = ?", id).Updates(updates).Error
}

func (r *Repo) GetPendingGuildJoinRequest(guildID, userID uint) (*models.GuildJoinRequest, error) {
	var g models.GuildJoinRequest
	if err := r.DB.Where("guild_id = ? AND user_id = ? AND status = ?", guildID, userID, "pending").First(&g).Error; err != nil {
		return nil, err
	}
	return &g, nil
}

func (r *Repo) GetUserGuildJoinRequest(guildID, userID uint) (*models.GuildJoinRequest, error) {
	var g models.GuildJoinRequest
	if err := r.DB.Where("guild_id = ? AND user_id = ?", guildID, userID).Order("id desc").First(&g).Error; err != nil {
		return nil, err
	}
	if user, err := r.GetUserByID(g.UserID); err == nil {
		g.User = user
	}
	return &g, nil
}

func (r *Repo) GetUserGuildJoinRequestsByGuildIDs(userID uint, guildIDs []uint) ([]models.GuildJoinRequest, error) {
	if len(guildIDs) == 0 {
		return []models.GuildJoinRequest{}, nil
	}
	var list []models.GuildJoinRequest
	if err := r.DB.Where("user_id = ? AND guild_id IN ?", userID, guildIDs).
		Order("guild_id asc, id desc").Find(&list).Error; err != nil {
		return nil, err
	}
	seen := make(map[uint]bool, len(guildIDs))
	dedup := make([]models.GuildJoinRequest, 0, len(guildIDs))
	for _, it := range list {
		if seen[it.GuildID] {
			continue
		}
		seen[it.GuildID] = true
		dedup = append(dedup, it)
	}
	return dedup, nil
}

// ──────────────────────────────────────────────
// Roles
// ──────────────────────────────────────────────

func (r *Repo) CreateRole(role *models.Role) error {
	return r.DB.Create(role).Error
}

func (r *Repo) GetEveryoneRole(guildID uint) (*models.Role, error) {
	var role models.Role
	if err := r.DB.Where("guild_id=? AND name=?", guildID, "@everyone").First(&role).Error; err != nil {
		return nil, err
	}
	return &role, nil
}

func (r *Repo) AssignRole(guildID, userID, roleID uint) error {
	mr := models.MemberRole{GuildID: guildID, UserID: userID, RoleID: roleID}
	if err := r.DB.Where("guild_id=? AND user_id=? AND role_id=?", guildID, userID, roleID).FirstOrCreate(&mr).Error; err != nil {
		return err
	}
	if r.Redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_ = r.Redis.Del(ctx, fmt.Sprintf("perm:%d:%d", guildID, userID)).Err()
	}
	return nil
}

func (r *Repo) RemoveRole(guildID, userID, roleID uint) error {
	if err := r.DB.Where("guild_id=? AND user_id=? AND role_id=?", guildID, userID, roleID).Delete(&models.MemberRole{}).Error; err != nil {
		return err
	}
	if r.Redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_ = r.Redis.Del(ctx, fmt.Sprintf("perm:%d:%d", guildID, userID)).Err()
	}
	return nil
}

func (r *Repo) ListUserRoles(guildID, userID uint) ([]models.Role, error) {
	var roles []models.Role
	err := r.DB.Table("roles").Joins("JOIN member_roles mr ON mr.role_id = roles.id AND mr.guild_id = roles.guild_id").
		Where("mr.guild_id=? AND mr.user_id=?", guildID, userID).Find(&roles).Error
	return roles, err
}

func (r *Repo) ListRoles(guildID uint) ([]models.Role, error) {
	var roles []models.Role
	return roles, r.DB.Where("guild_id=?", guildID).Order("id asc").Find(&roles).Error
}

func (r *Repo) GetRole(roleID uint) (*models.Role, error) {
	var role models.Role
	if err := r.DB.First(&role, roleID).Error; err != nil {
		return nil, err
	}
	return &role, nil
}

func (r *Repo) UpdateRole(role *models.Role) error {
	return r.DB.Save(role).Error
}

func (r *Repo) DeleteRole(roleID uint) error {
	return r.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("role_id = ?", roleID).Delete(&models.MemberRole{}).Error; err != nil {
			return err
		}
		return tx.Delete(&models.Role{}, roleID).Error
	})
}

func (r *Repo) ListUserIDsByRole(guildID, roleID uint) ([]uint, error) {
	var ids []uint
	rows, err := r.DB.Table("member_roles").
		Select("user_id").
		Where("guild_id = ? AND role_id = ?", guildID, roleID).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var uid uint
		if err := rows.Scan(&uid); err == nil {
			ids = append(ids, uid)
		}
	}
	return ids, nil
}

// ──────────────────────────────────────────────
// Announcements
// ──────────────────────────────────────────────

func (r *Repo) CreateAnnouncement(a *models.Announcement) error {
	return r.DB.Create(a).Error
}

func (r *Repo) ListAnnouncements(guildID uint) ([]models.Announcement, error) {
	var list []models.Announcement
	err := r.DB.Where("guild_id = ?", guildID).Order("created_at DESC").Find(&list).Error
	return list, err
}

func (r *Repo) GetAnnouncement(id uint) (*models.Announcement, error) {
	var a models.Announcement
	if err := r.DB.First(&a, id).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *Repo) UpdateAnnouncement(a *models.Announcement) error {
	return r.DB.Save(a).Error
}

func (r *Repo) DeleteAnnouncement(id uint) error {
	return r.DB.Delete(&models.Announcement{}, id).Error
}

func (r *Repo) PinAnnouncement(guildID, announcementID uint) error {
	return r.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Announcement{}).
			Where("guild_id = ? AND is_pinned = ?", guildID, true).
			Updates(map[string]any{"is_pinned": false, "pinned_at": nil}).Error; err != nil {
			return err
		}
		now := time.Now()
		return tx.Model(&models.Announcement{}).
			Where("id = ? AND guild_id = ?", announcementID, guildID).
			Updates(map[string]any{"is_pinned": true, "pinned_at": now}).Error
	})
}

func (r *Repo) UnpinAnnouncement(guildID, announcementID uint) error {
	return r.DB.Model(&models.Announcement{}).
		Where("id = ? AND guild_id = ?", announcementID, guildID).
		Updates(map[string]any{"is_pinned": false, "pinned_at": nil}).Error
}

func (r *Repo) GetFeaturedAnnouncement(guildID uint) (*models.Announcement, error) {
	var a models.Announcement
	err := r.DB.Where("guild_id = ? AND is_pinned = ?", guildID, true).
		Preload("Author").First(&a).Error
	if err == nil {
		return &a, nil
	}
	err = r.DB.Where("guild_id = ?", guildID).
		Preload("Author").Order("created_at DESC").First(&a).Error
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ──────────────────────────────────────────────
// Guild Files
// ──────────────────────────────────────────────

func (r *Repo) CreateGuildFile(f *models.GuildFile) error {
	return r.DB.Create(f).Error
}

func (r *Repo) ListGuildFiles(guildID uint, limit int, before uint) ([]models.GuildFile, error) {
	var list []models.GuildFile
	q := r.DB.Where("guild_id = ?", guildID).Preload("Uploader")
	if before > 0 {
		q = q.Where("id < ?", before)
	}
	err := q.Order("id DESC").Limit(limit).Find(&list).Error
	return list, err
}

func (r *Repo) GetGuildFile(id uint) (*models.GuildFile, error) {
	var f models.GuildFile
	if err := r.DB.Preload("Uploader").First(&f, id).Error; err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *Repo) UpdateGuildFileName(id uint, newName string) error {
	return r.DB.Model(&models.GuildFile{}).Where("id = ?", id).Update("file_name", newName).Error
}

func (r *Repo) DeleteGuildFile(id uint) error {
	return r.DB.Delete(&models.GuildFile{}, id).Error
}

func (r *Repo) CountGuildFiles(guildID uint) (int64, error) {
	var count int64
	err := r.DB.Model(&models.GuildFile{}).Where("guild_id = ?", guildID).Count(&count).Error
	return count, err
}
