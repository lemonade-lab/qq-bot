package service

import (
	"bubble/src/db/models"
	"context"
	"strconv"
	"strings"
	"time"
)

// Users

func (s *Service) CreateUser(name string) (*models.User, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrBadRequest
	}
	// keep legacy Token column unique but not used for auth (JWT replaces it)
	u := &models.User{Name: name, Token: "legacy_" + randToken(8)}
	if err := s.createUserWithUniqueToken(u); err != nil {
		return nil, err
	}
	return u, nil
}

// GetUserByToken validates a JWT token and returns the user
func (s *Service) GetUserByToken(token string) (*models.User, error) {
	// Parse JWT token to get user ID
	userID, err := s.ParseAccessToken(token)
	if err != nil {
		return nil, err
	}
	// Fetch user by ID
	return s.Repo.GetUserByID(userID)
}

func (s *Service) GetUserByID(id uint) (*models.User, error) { return s.Repo.GetUserByID(id) }

// SearchUsers 用户名/ID搜索。
// q 不可为空；limit 默认 10，上限 50。
// 支持ID搜索（精确匹配）和名称模糊搜索
func (s *Service) SearchUsers(q string, limit int) ([]models.User, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, ErrBadRequest
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	// 支持ID搜索（只返回非私密用户）
	if id, err := strconv.ParseUint(q, 10, 32); err == nil {
		user, err := s.Repo.GetUserByID(uint(id))
		if err == nil && user != nil && !user.IsPrivate {
			return []models.User{*user}, nil
		}
	}
	// 名称模糊搜索
	return s.Repo.SearchUsersByName(q, limit)
}

// Profile update
func (s *Service) UpdateProfile(u *models.User, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return ErrBadRequest
	}
	return s.Repo.UpdateUser(u, map[string]any{"name": newName})
}

// UpdateAvatar 更新用户头像
// avatarPath 是MinIO中的object name(文件路径)
func (s *Service) UpdateAvatar(u *models.User, avatarPath string) error {
	if s.MinIO == nil {
		return &Err{Code: 500, Msg: "文件存储不可用"}
	}
	// 如果用户已有头像，删除旧头像
	if u.Avatar != "" && u.Avatar != avatarPath {
		ctx := context.Background()
		_ = s.MinIO.DeleteAvatar(ctx, u.Avatar) // 忽略删除错误，继续更新
	}
	// 更新数据库中的头像路径
	return s.Repo.UpdateUser(u, map[string]any{"avatar": avatarPath})
}

func (s *Service) ChangePassword(u *models.User, oldPw, newPw string) error {
	if !verifyPassword(u.PasswordHash, oldPw) {
		return ErrUnauthorized
	}
	hash, err := hashPassword(newPw)
	if err != nil {
		return err
	}
	return s.Repo.SetUserPassword(u, hash)
}

func (s *Service) SetStatus(u *models.User, status string) error {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "online", "offline", "dnd", "idle", "busy":
		return s.Repo.UpdateUserStatus(u, status)
	default:
		return ErrBadRequest
	}
}

// SetPrivateAccount 设置账号为私密或公开
func (s *Service) SetPrivateAccount(userID uint, isPrivate bool) error {
	u, err := s.Repo.GetUserByID(userID)
	if err != nil || u == nil {
		return ErrNotFound
	}
	return s.Repo.UpdateUser(u, map[string]any{"is_private": isPrivate})
}

// UpdateUserProfile 更新用户资料（横幅、简介等）
func (s *Service) UpdateUserProfile(u *models.User, updates map[string]any) error {
	allowed := map[string]bool{
		"banner":        true,
		"banner_color":  true,
		"bio":           true,
		"display_name":  true,
		"custom_status": true,
		"pronouns":      true,
		"birthday":      true,
		"link":          true,
	}
	for k := range updates {
		if !allowed[k] {
			return &Err{Code: 400, Msg: "不支持的字段：" + k}
		}
	}
	return s.Repo.UpdateUser(u, updates)
}

// UpdateUserSettings 更新用户个人设置（包括公开信息：头像、横幅、简介等）
func (s *Service) UpdateUserSettings(userID uint, gender, region, phone *string, requireFriendApproval *bool, avatar, banner, bannerColor, bio, birthday, pronouns, customStatus, displayName, link *string) error {
	updates := make(map[string]any)

	// 性别验证
	if gender != nil {
		g := strings.TrimSpace(*gender)
		if g != "" && g != "male" && g != "female" && g != "other" {
			return &Err{Code: 400, Msg: "性别不合法"}
		}
		updates["gender"] = g
	}

	// 地区验证
	if region != nil {
		r := strings.TrimSpace(*region)
		if len(r) > 128 {
			return &Err{Code: 400, Msg: "地区过长（最多128个字符）"}
		}
		updates["region"] = r
	}

	// 手机号验证
	if phone != nil {
		p := strings.TrimSpace(*phone)
		if len(p) > 32 {
			return &Err{Code: 400, Msg: "手机号过长（最多32个字符）"}
		}
		updates["phone"] = p
	}

	// 好友验证开关
	if requireFriendApproval != nil {
		updates["require_friend_approval"] = *requireFriendApproval
	}

	// 头像路径验证
	if avatar != nil {
		a := strings.TrimSpace(*avatar)
		if len(a) > 512 {
			return &Err{Code: 400, Msg: "头像路径过长（最多512个字符）"}
		}
		updates["avatar"] = a
	}

	// 横幅路径验证
	if banner != nil {
		b := strings.TrimSpace(*banner)
		if len(b) > 512 {
			return &Err{Code: 400, Msg: "横幅路径过长（最多512个字符）"}
		}
		updates["banner"] = b
	}

	// 横幅颜色验证（十六进制颜色格式）
	if bannerColor != nil {
		bc := strings.TrimSpace(*bannerColor)
		if bc != "" && len(bc) > 16 {
			return &Err{Code: 400, Msg: "横幅颜色过长（最多16个字符）"}
		}
		// 简单验证十六进制颜色格式
		if bc != "" && !strings.HasPrefix(bc, "#") {
			return &Err{Code: 400, Msg: "横幅颜色格式不合法（应以#开头）"}
		}
		updates["banner_color"] = bc
	}

	// 个人简介验证
	if bio != nil {
		bioStr := strings.TrimSpace(*bio)
		if len(bioStr) > 256 {
			return &Err{Code: 400, Msg: "个人简介过长（最多256个字符）"}
		}
		updates["bio"] = bioStr
	}

	// 生日验证 (YYYY-MM-DD 格式)
	if birthday != nil {
		b := strings.TrimSpace(*birthday)
		if b != "" {
			if len(b) > 10 {
				return &Err{Code: 400, Msg: "生日格式不合法（应为 YYYY-MM-DD）"}
			}
			_, err := time.Parse("2006-01-02", b)
			if err != nil {
				return &Err{Code: 400, Msg: "生日格式不合法（应为 YYYY-MM-DD）"}
			}
		}
		updates["birthday"] = b
	}

	// 称谓代词验证
	if pronouns != nil {
		p := strings.TrimSpace(*pronouns)
		if len([]rune(p)) > 32 {
			return &Err{Code: 400, Msg: "称谓代词过长（最多32个字符）"}
		}
		updates["pronouns"] = p
	}

	// 自定义状态验证
	if customStatus != nil {
		cs := strings.TrimSpace(*customStatus)
		if len([]rune(cs)) > 128 {
			return &Err{Code: 400, Msg: "自定义状态过长（最多128个字符）"}
		}
		updates["custom_status"] = cs
	}

	// 展示名称验证
	if displayName != nil {
		dn := strings.TrimSpace(*displayName)
		if len([]rune(dn)) > 32 {
			return &Err{Code: 400, Msg: "展示名称过长（最多32个字符）"}
		}
		updates["display_name"] = dn
	}

	// 个人链接验证
	if link != nil {
		l := strings.TrimSpace(*link)
		if len(l) > 256 {
			return &Err{Code: 400, Msg: "个人链接过长（最多256个字符）"}
		}
		updates["link"] = l
	}

	if len(updates) == 0 {
		return nil
	}

	u, err := s.Repo.GetUserByID(userID)
	if err != nil {
		return ErrNotFound
	}
	return s.Repo.UpdateUser(u, updates)
}
