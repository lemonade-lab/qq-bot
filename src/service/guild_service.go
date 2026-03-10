package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"bubble/src/config"
	"bubble/src/db/models"
	"bubble/src/logger"

	"github.com/redis/go-redis/v9"
)

// AutoJoinMode values
const (
	AutoJoinRequireApproval    = "require_approval"
	AutoJoinNoApproval         = "no_approval"
	AutoJoinNoApprovalUnder100 = "no_approval_under_100"
)

// CreateGuildOptions 创建服务器的选项
type CreateGuildOptions struct {
	Name         string
	Description  string
	IsPrivate    *bool
	AutoJoinMode string
	Category     string
}

// CreateGuildWithOptions 创建服务器（可选字段）
func (s *Service) CreateGuildWithOptions(ownerID uint, opts CreateGuildOptions) (*models.Guild, error) {
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		return nil, ErrBadRequest
	}

	// 处理分类
	category := strings.TrimSpace(opts.Category)
	if category == "" {
		category = models.GuildCategoryOther // 默认为 other
	} else if !models.IsValidGuildCategory(category) {
		return nil, &Err{Code: 400, Msg: "无效的服务器分类"}
	}

	// 处理 isPrivate
	isPrivate := true // 默认私密
	if opts.IsPrivate != nil {
		isPrivate = *opts.IsPrivate
	}

	// 处理 autoJoinMode
	autoJoinMode := AutoJoinRequireApproval // 默认需要审批
	if opts.AutoJoinMode != "" {
		if opts.AutoJoinMode != AutoJoinRequireApproval &&
			opts.AutoJoinMode != AutoJoinNoApproval &&
			opts.AutoJoinMode != AutoJoinNoApprovalUnder100 {
			return nil, &Err{Code: 400, Msg: "无效的加入模式"}
		}
		autoJoinMode = opts.AutoJoinMode
	}

	g := &models.Guild{
		Name:         name,
		Description:  strings.TrimSpace(opts.Description),
		OwnerID:      ownerID,
		IsPrivate:    isPrivate,
		AutoJoinMode: autoJoinMode,
		Category:     category,
	}

	if err := s.Repo.CreateGuild(g); err != nil {
		return nil, err
	}
	// auto: add creator as member
	_ = s.Repo.AddMember(g.ID, ownerID)
	return g, nil
}

// CreateGuild 创建服务器（简化版本，保持向后兼容）
func (s *Service) CreateGuild(ownerID uint, name string) (*models.Guild, error) {
	return s.CreateGuildWithOptions(ownerID, CreateGuildOptions{Name: name})
}

// EffectiveGuildPerms returns the effective permission bitset for a user in a guild.
func (s *Service) EffectiveGuildPerms(guildID, userID uint) (uint64, error) {
	guild, err := s.Repo.GetGuild(guildID)
	if err == nil && guild != nil && guild.OwnerID == userID {
		return PermAll, nil
	}

	isMember, err := s.Repo.IsMember(guildID, userID)
	if err != nil {
		return 0, err
	}
	if !isMember {
		logger.Infof("[Permission] User %d is not a member of guild %d, returning 0 permissions", userID, guildID)
		return 0, nil
	}

	// Try short-TTL cache in Redis first
	var cacheKey string
	if s.Repo != nil && s.Repo.Redis != nil {
		cacheKey = fmt.Sprintf("perm:%d:%d", guildID, userID)
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		if val, err := s.Repo.Redis.Get(ctx, cacheKey).Result(); err == nil && val != "" {
			if u, perr := strconv.ParseUint(val, 10, 64); perr == nil {
				return u, nil
			}
		}
	}
	// @everyone 虚拟角色: 所有成员的基础权限
	base := uint64(PermViewChannel | PermSendMessages)

	roles, err := s.Repo.ListUserRoles(guildID, userID)
	if err != nil {
		return base, err
	}
	eff := base
	for _, r := range roles {
		eff |= r.Permissions
	}
	if cacheKey != "" && s.Repo.Redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		_ = s.Repo.Redis.Set(ctx, cacheKey, strconv.FormatUint(eff, 10), 10*time.Second).Err()
	}
	return eff, nil
}

// invalidatePermCacheForUsers 删除指定 guild 下若干用户的权限缓存键。
func (s *Service) invalidatePermCacheForUsers(guildID uint, userIDs []uint) {
	if s.Repo == nil || s.Repo.Redis == nil || len(userIDs) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	pipe := s.Repo.Redis.Pipeline()
	for _, uid := range userIDs {
		pipe.Del(ctx, fmt.Sprintf("perm:%d:%d", guildID, uid))
	}
	_, _ = pipe.Exec(ctx)
}

func (s *Service) HasGuildPerm(guildID, userID uint, perm uint64) (bool, error) {
	eff, err := s.EffectiveGuildPerms(guildID, userID)
	if err != nil {
		return false, err
	}
	return (eff & perm) == perm, nil
}

// --- Role Management ---

// CreateRole creates a new role in the guild (requires PermManageRoles)
func (s *Service) CreateRole(guildID, userID uint, name string, permissions uint64, color string) (*models.Role, error) {
	ok, err := s.HasGuildPerm(guildID, userID, PermManageRoles)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrUnauthorized
	}

	callerPerms, err := s.EffectiveGuildPerms(guildID, userID)
	if err != nil {
		return nil, err
	}
	if callerPerms != PermAll && (permissions & ^callerPerms) != 0 {
		return nil, &Err{Code: 403, Msg: "不能授予自己没有的权限"}
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return nil, &Err{Code: 400, Msg: "角色名称不能为空"}
	}
	lowerName := strings.ToLower(name)
	if lowerName == "@everyone" || lowerName == "owner" || strings.HasPrefix(lowerName, "@") {
		return nil, &Err{Code: 400, Msg: "角色名称为保留字或格式不合法"}
	}

	color = strings.TrimSpace(color)
	if color != "" {
		if len(color) > 16 || !strings.HasPrefix(color, "#") {
			return nil, &Err{Code: 400, Msg: "颜色格式不合法（应以#开头，如 #5865F2）"}
		}
	}

	roles, err := s.Repo.ListRoles(guildID)
	if err != nil {
		return nil, err
	}
	if len(roles) >= int(config.MaxGuildRoles) {
		return nil, &Err{Code: 400, Msg: fmt.Sprintf("角色数量已达上限(%d)", config.MaxGuildRoles)}
	}
	for _, r := range roles {
		if r.Name == name {
			return nil, &Err{Code: 400, Msg: "角色名称已存在"}
		}
	}

	role := &models.Role{
		GuildID:     guildID,
		Name:        name,
		Color:       color,
		Permissions: permissions,
	}
	if err := s.Repo.CreateRole(role); err != nil {
		return nil, err
	}
	return role, nil
}

// UpdateRole updates an existing role (requires PermManageRoles)
func (s *Service) UpdateRole(guildID, userID, roleID uint, name *string, permissions *uint64, color *string) (*models.Role, error) {
	ok, err := s.HasGuildPerm(guildID, userID, PermManageRoles)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrUnauthorized
	}

	role, err := s.Repo.GetRole(roleID)
	if err != nil {
		return nil, ErrNotFound
	}
	if role.GuildID != guildID {
		return nil, ErrUnauthorized
	}

	if name != nil {
		*name = strings.TrimSpace(*name)
		if *name == "" {
			return nil, &Err{Code: 400, Msg: "角色名称不能为空"}
		}
		lowerName := strings.ToLower(*name)
		if lowerName == "@everyone" || lowerName == "owner" || strings.HasPrefix(lowerName, "@") {
			return nil, &Err{Code: 400, Msg: "角色名称为保留字或格式不合法"}
		}
		roles, err := s.Repo.ListRoles(guildID)
		if err != nil {
			return nil, err
		}
		for _, r := range roles {
			if r.Name == *name && r.ID != roleID {
				return nil, &Err{Code: 400, Msg: "角色名称已存在"}
			}
		}
		role.Name = *name
	}
	if permissions != nil {
		callerPerms, err := s.EffectiveGuildPerms(guildID, userID)
		if err != nil {
			return nil, err
		}
		if callerPerms != PermAll && (*permissions & ^callerPerms) != 0 {
			return nil, &Err{Code: 403, Msg: "不能授予自己没有的权限"}
		}
		role.Permissions = *permissions
	}
	if color != nil {
		c := strings.TrimSpace(*color)
		if c != "" && (len(c) > 16 || !strings.HasPrefix(c, "#")) {
			return nil, &Err{Code: 400, Msg: "颜色格式不合法（应以#开头，如 #5865F2）"}
		}
		role.Color = c
	}

	if err := s.Repo.UpdateRole(role); err != nil {
		return nil, err
	}
	if permissions != nil && s.Repo != nil {
		if ids, err := s.Repo.ListUserIDsByRole(guildID, roleID); err == nil {
			s.invalidatePermCacheForUsers(guildID, ids)
		}
	}
	return role, nil
}

// DeleteRole deletes a role (requires PermManageRoles)
func (s *Service) DeleteRole(guildID, userID, roleID uint) error {
	ok, err := s.HasGuildPerm(guildID, userID, PermManageRoles)
	if err != nil {
		return err
	}
	if !ok {
		return ErrUnauthorized
	}

	role, err := s.Repo.GetRole(roleID)
	if err != nil {
		return ErrNotFound
	}
	if role.GuildID != guildID {
		return ErrUnauthorized
	}

	var affected []uint
	if s.Repo != nil {
		if ids, err := s.Repo.ListUserIDsByRole(guildID, roleID); err == nil {
			affected = ids
		}
	}
	if err := s.Repo.DeleteRole(roleID); err != nil {
		return err
	}
	if len(affected) > 0 {
		s.invalidatePermCacheForUsers(guildID, affected)
	}
	return nil
}

// ListGuildRoles lists all roles in a guild
func (s *Service) ListGuildRoles(guildID, userID uint) ([]models.Role, error) {
	isMember, err := s.Repo.IsMember(guildID, userID)
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, ErrUnauthorized
	}
	return s.Repo.ListRoles(guildID)
}

// AssignRoleToMember assigns a role to a member (requires PermManageRoles)
func (s *Service) AssignRoleToMember(guildID, userID, targetUserID, roleID uint) error {
	ok, err := s.HasGuildPerm(guildID, userID, PermManageRoles)
	if err != nil {
		return err
	}
	if !ok {
		return ErrUnauthorized
	}

	guild, err := s.Repo.GetGuild(guildID)
	if err != nil || guild == nil {
		return ErrNotFound
	}
	if userID == targetUserID && guild.OwnerID != userID {
		return &Err{Code: 403, Msg: "不能给自己分配角色"}
	}

	role, err := s.Repo.GetRole(roleID)
	if err != nil {
		return ErrNotFound
	}
	if role.GuildID != guildID {
		return ErrBadRequest
	}

	if guild.OwnerID != userID {
		callerPerms, err := s.EffectiveGuildPerms(guildID, userID)
		if err != nil {
			return err
		}
		if (role.Permissions & ^callerPerms) != 0 {
			return &Err{Code: 403, Msg: "不能分配包含您没有的权限的角色"}
		}
	}

	isMember, err := s.Repo.IsMember(guildID, targetUserID)
	if err != nil {
		return err
	}
	if !isMember {
		return &Err{Code: 400, Msg: "目标用户不是成员"}
	}

	if err := s.Repo.AssignRole(guildID, targetUserID, roleID); err != nil {
		return err
	}
	s.invalidatePermCacheForUsers(guildID, []uint{targetUserID})
	return nil
}

// RemoveRoleFromMember removes a role from a member (requires PermManageRoles)
func (s *Service) RemoveRoleFromMember(guildID, userID, targetUserID, roleID uint) error {
	ok, err := s.HasGuildPerm(guildID, userID, PermManageRoles)
	if err != nil {
		return err
	}
	if !ok {
		return ErrUnauthorized
	}

	role, err := s.Repo.GetRole(roleID)
	if err != nil {
		return ErrNotFound
	}
	if role.GuildID != guildID {
		return ErrBadRequest
	}

	if err := s.Repo.RemoveRole(guildID, targetUserID, roleID); err != nil {
		return err
	}
	s.invalidatePermCacheForUsers(guildID, []uint{targetUserID})
	return nil
}

func (s *Service) ListGuilds() ([]models.Guild, error) { return s.Repo.ListGuilds() }

func (s *Service) ListUserGuilds(userID uint) ([]models.Guild, error) {
	return s.Repo.ListGuildsByUserID(userID)
}

// ListUserGuildsCursor 游标分页获取用户加入的服务器列表
func (s *Service) ListUserGuildsCursor(userID uint, limit int, beforeID, afterID uint) ([]models.Guild, error) {
	return s.Repo.ListGuildsByUserIDCursor(userID, limit, beforeID, afterID)
}

// ListUserGuildsPage 页面分页获取用户加入的服务器列表
func (s *Service) ListUserGuildsPage(userID uint, page, limit int) ([]models.Guild, int64, error) {
	return s.Repo.ListGuildsByUserIDPage(userID, page, limit)
}

// GuildWithMemberCount 服务器及其成员数
type GuildWithMemberCount struct {
	models.Guild
	MemberCount int `json:"memberCount"`
}

// ListUserGuildsWithMemberCountCursor 游标分页获取用户加入的服务器列表（附带成员数）
func (s *Service) ListUserGuildsWithMemberCountCursor(userID uint, limit int, beforeID, afterID uint) ([]GuildWithMemberCount, error) {
	guilds, err := s.Repo.ListGuildsByUserIDCursor(userID, limit, beforeID, afterID)
	if err != nil {
		return nil, err
	}

	guildIDs := make([]uint, len(guilds))
	for i, g := range guilds {
		guildIDs[i] = g.ID
	}

	countMap, err := s.batchGetGuildMemberCountWithCache(guildIDs)
	if err != nil {
		return nil, err
	}

	result := make([]GuildWithMemberCount, len(guilds))
	for i, g := range guilds {
		result[i] = GuildWithMemberCount{
			Guild:       g,
			MemberCount: countMap[g.ID],
		}
	}

	return result, nil
}

// ListUserGuildsWithMemberCountPage 页面分页获取用户加入的服务器列表（附带成员数）
func (s *Service) ListUserGuildsWithMemberCountPage(userID uint, page, limit int) ([]GuildWithMemberCount, int64, error) {
	guilds, total, err := s.Repo.ListGuildsByUserIDPage(userID, page, limit)
	if err != nil {
		return nil, 0, err
	}

	guildIDs := make([]uint, len(guilds))
	for i, g := range guilds {
		guildIDs[i] = g.ID
	}

	countMap, err := s.batchGetGuildMemberCountWithCache(guildIDs)
	if err != nil {
		return nil, 0, err
	}

	result := make([]GuildWithMemberCount, len(guilds))
	for i, g := range guilds {
		result[i] = GuildWithMemberCount{
			Guild:       g,
			MemberCount: countMap[g.ID],
		}
	}

	return result, total, nil
}

// batchGetGuildMemberCountWithCache 批量获取服务器成员数（带Redis缓存）
func (s *Service) batchGetGuildMemberCountWithCache(guildIDs []uint) (map[uint]int, error) {
	if len(guildIDs) == 0 {
		return make(map[uint]int), nil
	}

	countMap := make(map[uint]int)
	uncachedIDs := make([]uint, 0)

	if s.RedisClient != nil {
		ctx := context.Background()
		pipe := s.RedisClient.Pipeline()

		cmds := make(map[uint]*redis.StringCmd)
		for _, guildID := range guildIDs {
			key := getGuildMemberCountCacheKey(guildID)
			cmds[guildID] = pipe.Get(ctx, key)
		}

		_, _ = pipe.Exec(ctx)

		for guildID, cmd := range cmds {
			val, err := cmd.Result()
			if err == nil {
				var count int
				if err := json.Unmarshal([]byte(val), &count); err == nil {
					countMap[guildID] = count
				} else {
					uncachedIDs = append(uncachedIDs, guildID)
				}
			} else {
				uncachedIDs = append(uncachedIDs, guildID)
			}
		}
	} else {
		uncachedIDs = guildIDs
	}

	if len(uncachedIDs) > 0 {
		dbCounts, err := s.Repo.BatchCountGuildMembers(uncachedIDs)
		if err != nil {
			return nil, err
		}

		if s.RedisClient != nil {
			ctx := context.Background()
			pipe := s.RedisClient.Pipeline()

			for guildID, count := range dbCounts {
				countMap[guildID] = count
				key := getGuildMemberCountCacheKey(guildID)
				data, _ := json.Marshal(count)
				pipe.Set(ctx, key, data, 5*time.Minute)
			}

			for _, guildID := range uncachedIDs {
				if _, exists := countMap[guildID]; !exists {
					countMap[guildID] = 0
					key := getGuildMemberCountCacheKey(guildID)
					data, _ := json.Marshal(0)
					pipe.Set(ctx, key, data, 5*time.Minute)
				}
			}

			_, _ = pipe.Exec(ctx)
		} else {
			for guildID, count := range dbCounts {
				countMap[guildID] = count
			}
			for _, guildID := range uncachedIDs {
				if _, exists := countMap[guildID]; !exists {
					countMap[guildID] = 0
				}
			}
		}
	}

	return countMap, nil
}

// GetGuildMemberCounts 批量获取服务器成员数量（带缓存）
func (s *Service) GetGuildMemberCounts(guildIDs []uint) (map[uint]int, error) {
	return s.batchGetGuildMemberCountWithCache(guildIDs)
}

// SearchGuilds 服务器名称模糊搜索
func (s *Service) SearchGuilds(q string, limit int, category string) ([]models.Guild, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, ErrBadRequest
	}
	category = strings.TrimSpace(category)

	if id, err := strconv.ParseUint(q, 10, 32); err == nil {
		guild, err := s.Repo.GetGuild(uint(id))
		if err == nil && guild != nil && !guild.IsPrivate {
			if category == "" || guild.Category == category {
				return []models.Guild{*guild}, nil
			}
		}
	}
	return s.Repo.SearchGuildsByName(q, limit, category)
}

// HotGuilds 返回热门服务器列表（按成员数降序）
func (s *Service) HotGuilds(limit int) ([]models.Guild, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	if s.RedisClient != nil {
		cacheKey := fmt.Sprintf("hot_guilds:%d", limit)
		var cached []models.Guild
		if err := s.getCache(cacheKey, &cached); err == nil && len(cached) > 0 {
			return cached, nil
		}
		guilds, err := s.Repo.ListTopGuilds(limit)
		if err == nil && len(guilds) > 0 {
			_ = s.setCache(cacheKey, guilds, 5*time.Minute)
		}
		return guilds, err
	}
	return s.Repo.ListTopGuilds(limit)
}

// ListGuildsByCategory 按分类获取服务器列表（按成员数降序）
func (s *Service) ListGuildsByCategory(category string, limit int) ([]models.Guild, error) {
	if !models.IsValidGuildCategory(category) {
		return nil, ErrBadRequest
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	if s.RedisClient != nil {
		cacheKey := fmt.Sprintf("guilds_by_category:%s:%d", category, limit)
		var cached []models.Guild
		if err := s.getCache(cacheKey, &cached); err == nil && len(cached) > 0 {
			return cached, nil
		}
		guilds, err := s.Repo.ListGuildsByCategory(category, limit)
		if err == nil {
			_ = s.setCache(cacheKey, guilds, 5*time.Minute)
		}
		return guilds, err
	}
	return s.Repo.ListGuildsByCategory(category, limit)
}

// GetAllGuildCategories 获取所有服务器分类列表
func (s *Service) GetAllGuildCategories() []models.GuildCategoryInfo {
	return models.GetAllGuildCategories()
}

// SetGuildCategory 设置服务器分类，只有所有者可以修改
func (s *Service) SetGuildCategory(guildID, userID uint, category string) error {
	if !models.IsValidGuildCategory(category) {
		return &Err{Code: 400, Msg: "无效的分类"}
	}
	g, err := s.Repo.GetGuild(guildID)
	if err != nil || g == nil {
		return ErrNotFound
	}
	if g.OwnerID != userID {
		return ErrUnauthorized
	}

	err = s.Repo.UpdateGuild(guildID, map[string]interface{}{"category": category})
	if err == nil {
		if s.RedisClient != nil {
			for _, limit := range []int{20, 50, 100} {
				_ = s.deleteCache(fmt.Sprintf("guilds_by_category:%s:%d", g.Category, limit))
				_ = s.deleteCache(fmt.Sprintf("guilds_by_category:%s:%d", category, limit))
			}
		}
	}
	return err
}

// GetGuildMemberLimit 获取服务器的成员人数限制
func (s *Service) GetGuildMemberLimit(guildID uint) (int, error) {
	g, err := s.Repo.GetGuild(guildID)
	if err != nil || g == nil {
		return 0, ErrNotFound
	}
	return models.GetMemberLimitByLevel(g.Level), nil
}

// GetAllGuildLevels 获取所有服务器等级信息
func (s *Service) GetAllGuildLevels() []models.GuildLevelInfo {
	return models.GetAllGuildLevels()
}

// SetGuildLevel 设置服务器等级
func (s *Service) SetGuildLevel(guildID, userID uint, level int) error {
	if level < 0 {
		return &Err{Code: 400, Msg: "无效的等级"}
	}
	g, err := s.Repo.GetGuild(guildID)
	if err != nil || g == nil {
		return ErrNotFound
	}
	if g.OwnerID != userID {
		return ErrUnauthorized
	}
	return s.Repo.UpdateGuild(guildID, map[string]interface{}{"level": level})
}

// BatchCheckMembership 批量检查用户在多个服务器的成员状态
func (s *Service) BatchCheckMembership(userID uint, guildIDs []uint) (map[uint]bool, error) {
	if len(guildIDs) == 0 {
		return make(map[uint]bool), nil
	}

	var members []models.GuildMember
	err := s.Repo.DB.Where("user_id = ? AND guild_id IN ?", userID, guildIDs).
		Find(&members).Error
	if err != nil {
		return nil, err
	}

	result := make(map[uint]bool)
	for _, m := range members {
		result[m.GuildID] = true
	}
	return result, nil
}

// DeleteGuild soft-deletes a guild; only the owner can delete.
func (s *Service) DeleteGuild(guildID, userID uint) error {
	g, err := s.Repo.GetGuild(guildID)
	if err != nil || g == nil {
		return ErrNotFound
	}
	if g.OwnerID != userID {
		return ErrUnauthorized
	}
	return s.Repo.DeleteGuild(guildID)
}

// SetGuildPrivacy 设置服务器为公开或私密
func (s *Service) SetGuildPrivacy(guildID, userID uint, isPrivate bool) error {
	g, err := s.Repo.GetGuild(guildID)
	if err != nil || g == nil {
		return ErrNotFound
	}
	if g.OwnerID != userID {
		return ErrUnauthorized
	}
	return s.Repo.UpdateGuild(guildID, map[string]interface{}{"is_private": isPrivate})
}

// SetGuildAutoJoinMode 设置服务器加入验证模式
func (s *Service) SetGuildAutoJoinMode(guildID, userID uint, mode string) error {
	g, err := s.Repo.GetGuild(guildID)
	if err != nil || g == nil {
		return ErrNotFound
	}
	if g.OwnerID != userID {
		return ErrUnauthorized
	}
	if mode != AutoJoinRequireApproval && mode != AutoJoinNoApproval && mode != AutoJoinNoApprovalUnder100 {
		return &Err{Code: 400, Msg: "加入验证模式不合法"}
	}
	return s.Repo.UpdateGuild(guildID, map[string]interface{}{"auto_join_mode": mode})
}

// UpdateGuild 通用的更新服务器信息方法（仅owner可修改）
func (s *Service) UpdateGuild(guildID, userID uint, name, description, avatar, banner *string, optionalBools ...*bool) error {
	g, err := s.Repo.GetGuild(guildID)
	if err != nil || g == nil {
		return ErrNotFound
	}
	if g.OwnerID != userID {
		return ErrUnauthorized
	}

	updates := make(map[string]interface{})

	if name != nil {
		if len([]rune(*name)) == 0 || len([]rune(*name)) > 50 {
			return &Err{Code: 400, Msg: "服务器名称长度必须在1-50个字符之间"}
		}
		updates["name"] = *name
	}

	if description != nil {
		if len(*description) > 512 {
			return &Err{Code: 400, Msg: "简介过长"}
		}
		updates["description"] = *description
	}

	if avatar != nil {
		if *avatar == "" {
			return &Err{Code: 400, Msg: "头像路径不能为空"}
		}
		if g.Avatar != "" && g.Avatar != *avatar && s.MinIO != nil {
			ctx := context.Background()
			_ = s.MinIO.DeleteFile(ctx, g.Avatar)
		}
		updates["avatar"] = *avatar
	}

	if banner != nil {
		if *banner == "" {
			return &Err{Code: 400, Msg: "横幅路径不能为空"}
		}
		if g.Banner != "" && g.Banner != *banner && s.MinIO != nil {
			ctx := context.Background()
			_ = s.MinIO.DeleteFile(ctx, g.Banner)
		}
		updates["banner"] = *banner
	}

	if len(optionalBools) > 0 && optionalBools[0] != nil {
		updates["allow_member_upload"] = *optionalBools[0]
	}

	if len(optionalBools) > 1 && optionalBools[1] != nil {
		updates["show_role_names"] = *optionalBools[1]
	}

	if len(updates) == 0 {
		return nil
	}

	return s.Repo.UpdateGuild(guildID, updates)
}

// UpdateGuildDescription 更新服务器简介
func (s *Service) UpdateGuildDescription(guildID, userID uint, description string) error {
	return s.UpdateGuild(guildID, userID, nil, &description, nil, nil)
}

// UpdateGuildAvatar 更新服务器头像
func (s *Service) UpdateGuildAvatar(guildID, userID uint, avatarPath string) error {
	return s.UpdateGuild(guildID, userID, nil, nil, &avatarPath, nil)
}

// UpdateGuildName 更新服务器名称
func (s *Service) UpdateGuildName(guildID, userID uint, name string) error {
	return s.UpdateGuild(guildID, userID, &name, nil, nil, nil)
}

// JoinGuild 加入服务器
func (s *Service) JoinGuild(guildID, userID uint) error {
	guild, err := s.Repo.GetGuild(guildID)
	if err != nil || guild == nil {
		return ErrNotFound
	}

	isMember, err := s.Repo.IsMember(guildID, userID)
	if err != nil {
		return err
	}
	if isMember {
		return nil
	}

	memberLimit := models.GetMemberLimitByLevel(guild.Level)
	currentCount, err := s.Repo.CountGuildMembers(guildID)
	if err != nil {
		return err
	}
	if currentCount >= memberLimit {
		return &Err{Code: 403, Msg: "服务器成员已达上限"}
	}

	mode := guild.AutoJoinMode
	if mode == "" {
		mode = AutoJoinRequireApproval
	}

	switch mode {
	case AutoJoinNoApproval:
		err := s.Repo.AddMember(guildID, userID)
		if err == nil {
			s.ClearGuildMemberCountCache(guildID)
		}
		return err
	case AutoJoinNoApprovalUnder100:
		if currentCount < 100 {
			err := s.Repo.AddMember(guildID, userID)
			if err == nil {
				s.ClearGuildMemberCountCache(guildID)
			}
			return err
		}
		fallthrough
	case AutoJoinRequireApproval:
		_, _, _, err := s.ApplyGuildJoin(guildID, userID, "")
		if err != nil {
			return err
		}
		return nil
	default:
		_, _, _, err := s.ApplyGuildJoin(guildID, userID, "")
		if err != nil {
			return err
		}
		return nil
	}
}

func (s *Service) LeaveGuild(guildID, userID uint) error {
	err := s.Repo.RemoveMember(guildID, userID)
	if err == nil {
		s.ClearGuildMemberCountCache(guildID)
	}
	return err
}

func (s *Service) IsMember(guildID, userID uint) (bool, error) {
	return s.Repo.IsMember(guildID, userID)
}

// GetSharedGuilds 获取两个用户共同加入的服务器列表
func (s *Service) GetSharedGuilds(userID1, userID2 uint) ([]models.Guild, error) {
	return s.Repo.GetSharedGuilds(userID1, userID2)
}

// KickMember 踢出成员
func (s *Service) KickMember(guildID, targetUserID, operatorID uint) error {
	guild, err := s.Repo.GetGuild(guildID)
	if err != nil || guild == nil {
		return ErrNotFound
	}

	if guild.OwnerID != operatorID {
		ok, err := s.HasGuildPerm(guildID, operatorID, PermKickMembers)
		if err != nil {
			return err
		}
		if !ok {
			return ErrUnauthorized
		}
	}

	if guild.OwnerID == targetUserID {
		return &Err{Code: 400, Msg: "不能踢出服务器拥有者"}
	}

	if targetUserID == operatorID {
		return &Err{Code: 400, Msg: "不能踢出自己，请使用退出服务器"}
	}

	err = s.Repo.RemoveMember(guildID, targetUserID)
	if err == nil {
		s.ClearGuildMemberCountCache(guildID)
	}
	return err
}

// MuteMember 禁言成员
func (s *Service) MuteMember(guildID, targetUserID, operatorID uint, duration time.Duration) error {
	guild, err := s.Repo.GetGuild(guildID)
	if err != nil || guild == nil {
		return ErrNotFound
	}

	if guild.OwnerID != operatorID {
		ok, err := s.HasGuildPerm(guildID, operatorID, PermMuteMembers)
		if err != nil {
			return err
		}
		if !ok {
			return ErrUnauthorized
		}
	}

	if guild.OwnerID == targetUserID {
		return &Err{Code: 400, Msg: "不能禁言服务器拥有者"}
	}

	if targetUserID == operatorID {
		return &Err{Code: 400, Msg: "不能禁言自己"}
	}

	mutedUntil := time.Now().Add(duration)
	return s.Repo.MuteMember(guildID, targetUserID, mutedUntil)
}

// UnmuteMember 解除禁言
func (s *Service) UnmuteMember(guildID, targetUserID, operatorID uint) error {
	guild, err := s.Repo.GetGuild(guildID)
	if err != nil || guild == nil {
		return ErrNotFound
	}

	if guild.OwnerID != operatorID {
		ok, err := s.HasGuildPerm(guildID, operatorID, PermMuteMembers)
		if err != nil {
			return err
		}
		if !ok {
			return ErrUnauthorized
		}
	}

	return s.Repo.UnmuteMember(guildID, targetUserID)
}

// SetMemberNickname 设置成员临时昵称
func (s *Service) SetMemberNickname(guildID, targetUserID, currentUserID uint, nickname string) error {
	guild, err := s.Repo.GetGuild(guildID)
	if err != nil || guild == nil {
		return ErrNotFound
	}

	isMember, err := s.Repo.IsMember(guildID, targetUserID)
	if err != nil {
		return err
	}
	if !isMember {
		return ErrNotFound
	}

	isOwner := guild.OwnerID == currentUserID
	isSelf := currentUserID == targetUserID

	if !isSelf {
		if !isOwner {
			ok, err := s.HasGuildPerm(guildID, currentUserID, PermManageGuild)
			if err != nil {
				return err
			}
			if !ok {
				return ErrUnauthorized
			}
		}
	}

	return s.Repo.UpdateMemberNickname(guildID, targetUserID, nickname)
}

// ReorderGuilds 批量更新用户的服务器排序
func (s *Service) ReorderGuilds(userID uint, orders []struct {
	ID        uint `json:"id"`
	SortOrder int  `json:"sortOrder"`
}) error {
	for _, o := range orders {
		isMember, err := s.Repo.IsMember(o.ID, userID)
		if err != nil {
			return err
		}
		if !isMember {
			return ErrUnauthorized
		}
	}
	updates := make([]struct {
		GuildID   uint
		SortOrder int
	}, len(orders))
	for i, o := range orders {
		updates[i].GuildID = o.ID
		updates[i].SortOrder = o.SortOrder
	}
	return s.Repo.BatchUpdateGuildMemberOrders(userID, updates)
}

// GetUserGuildJoinRequestStatusBatch 批量获取用户在多个服务器的加入申请状态（最近一条记录）。
// 返回 map[guildID]*GuildJoinRequest，若不存在记录，则该 guildID 映射为 nil。
func (s *Service) GetUserGuildJoinRequestStatusBatch(userID uint, guildIDs []uint) (map[uint]*models.GuildJoinRequest, error) {
	res := make(map[uint]*models.GuildJoinRequest, len(guildIDs))
	if len(guildIDs) == 0 {
		return res, nil
	}
	list, err := s.Repo.GetUserGuildJoinRequestsByGuildIDs(userID, guildIDs)
	if err != nil {
		return nil, err
	}
	// 先初始化为 nil
	for _, gid := range guildIDs {
		res[gid] = nil
	}
	// 覆盖为最新记录
	for i := range list {
		it := list[i]
		// 可选：填充用户信息
		if u, err := s.Repo.GetUserByID(it.UserID); err == nil {
			it.User = u
		}
		item := it
		res[it.GuildID] = &item
	}
	return res, nil
}

// HasValidGuildApplication 检查用户是否有有效的（未过期的）服务器申请
func (s *Service) HasValidGuildApplication(guildID, userID uint) (bool, error) {
	oneMonthAgo := time.Now().AddDate(0, -1, 0)

	var count int64
	err := s.Repo.DB.Model(&models.GuildJoinRequest{}).
		Where("guild_id = ? AND user_id = ? AND created_at > ?", guildID, userID, oneMonthAgo).
		Count(&count).Error

	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// ProcessGuildApplicationV2 处理 v2 版本的服务器申请逻辑
func (s *Service) ProcessGuildApplicationV2(guildID, userID uint, note string) (*models.Guild, *models.UserNotification, error) {
	guild, err := s.Repo.GetGuild(guildID)
	if err != nil || guild == nil {
		return nil, nil, ErrNotFound
	}

	isMember, err := s.Repo.IsMember(guildID, userID)
	if err != nil {
		return nil, nil, err
	}
	if isMember {
		return guild, nil, nil
	}

	mode := strings.TrimSpace(guild.AutoJoinMode)
	if mode == "" {
		mode = AutoJoinRequireApproval
	}

	switch mode {
	case AutoJoinNoApproval:
		if err := s.Repo.AddMember(guildID, userID); err != nil {
			return nil, nil, err
		}
		_ = s.Repo.DB.Where("guild_id = ? AND user_id = ? AND status = ?", guildID, userID, "pending").Delete(&models.GuildJoinRequest{}).Error
		s.ClearGuildMemberCountCache(guildID)
		return guild, nil, nil
	case AutoJoinNoApprovalUnder100:
		count, err := s.Repo.CountGuildMembers(guildID)
		if err != nil {
			return nil, nil, err
		}
		if count < 100 {
			if err := s.Repo.AddMember(guildID, userID); err != nil {
				return nil, nil, err
			}
			_ = s.Repo.DB.Where("guild_id = ? AND user_id = ? AND status = ?", guildID, userID, "pending").Delete(&models.GuildJoinRequest{}).Error
			s.ClearGuildMemberCountCache(guildID)
			return guild, nil, nil
		}
	}

	oneDayAgo := time.Now().Add(-24 * time.Hour)

	var existingRequest models.GuildJoinRequest
	err = s.Repo.DB.Where(
		"guild_id = ? AND user_id = ? AND status = ? AND created_at > ?",
		guildID, userID, "pending", oneDayAgo,
	).First(&existingRequest).Error

	if err == nil {
		return nil, nil, nil
	}

	newRequest := models.GuildJoinRequest{
		GuildID:   guildID,
		UserID:    userID,
		Note:      note,
		Status:    "pending",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	err = s.Repo.DB.Create(&newRequest).Error
	if err != nil {
		return nil, nil, err
	}

	notif, err := s.Repo.CreateGuildJoinRequestNotification(guild.OwnerID, userID, guildID)
	if err != nil {
		logger.Warnf("[GuildV2] Failed to create notification for guild join request (guild=%d, user=%d): %v", guildID, userID, err)
		return guild, nil, nil
	}

	return guild, notif, nil
}

// GetGuildDetailV2 获取服务器详情（v2 版本，支持匿名访问）
func (s *Service) GetGuildDetailV2(guildID uint, user *models.User) (*models.Guild, error) {
	guild, err := s.Repo.GetGuild(guildID)
	if err != nil {
		return nil, err
	}
	if guild == nil {
		return nil, ErrNotFound
	}
	return guild, nil
}
