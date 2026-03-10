package repository

import "bubble/src/db/models"

// ──────────────────────────────────────────────
// User repository methods
// ──────────────────────────────────────────────

func (r *Repo) CreateUser(u *models.User) error { return r.DB.Create(u).Error }

func (r *Repo) GetUserByToken(token string) (*models.User, error) {
	var u models.User
	if err := r.DB.Where("token = ?", token).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *Repo) GetUserByID(id uint) (*models.User, error) {
	var u models.User
	if err := r.DB.First(&u, id).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *Repo) GetUserByName(name string) (*models.User, error) {
	var u models.User
	if err := r.DB.Where("name = ?", name).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *Repo) GetUserByEmail(email string) (*models.User, error) {
	var u models.User
	if err := r.DB.Where("email = ?", email).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// GetUserByPhone 通过手机号查找用户
func (r *Repo) GetUserByPhone(phone string) (*models.User, error) {
	var u models.User
	if err := r.DB.Where("phone = ? AND phone != ''", phone).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *Repo) UpdateUser(u *models.User, fields map[string]any) error {
	return r.DB.Model(u).Updates(fields).Error
}

func (r *Repo) SetUserPassword(u *models.User, hash string) error {
	return r.DB.Model(u).Update("password_hash", hash).Error
}

func (r *Repo) UpdateUserStatus(u *models.User, status string) error {
	return r.DB.Model(u).Update("status", status).Error
}

// SearchUsersByName 按用户名模糊搜索用户，排除私密账号
func (r *Repo) SearchUsersByName(q string, limit int) ([]models.User, error) {
	var users []models.User
	pattern := "%" + q + "%"
	err := r.DB.Where("name LIKE ? AND is_private = ?", pattern, false).Limit(limit).Order("id asc").Find(&users).Error
	return users, err
}

// Robots
func (r *Repo) CreateRobot(rb *models.Robot) error {
	return r.DB.Create(rb).Error
}

func (r *Repo) ListRobotsByOwnerID(ownerID uint) ([]models.Robot, error) {
	var list []models.Robot
	err := r.DB.Preload("BotUser").Where("owner_id = ?", ownerID).Order("id desc").Find(&list).Error
	return list, err
}

func (r *Repo) GetRobotByToken(token string) (*models.Robot, error) {
	var rb models.Robot
	if err := r.DB.Preload("BotUser").Where("token = ?", token).First(&rb).Error; err != nil {
		return nil, err
	}
	return &rb, nil
}

// Security Events
func (r *Repo) CreateSecurityEvent(ev *models.SecurityEvent) error {
	return r.DB.Create(ev).Error
}

// Greetings
func (r *Repo) CreateGreeting(fromUserID, toUserID uint) error {
	greeting := &models.Greeting{FromUserID: fromUserID, ToUserID: toUserID}
	return r.DB.Create(greeting).Error
}

func (r *Repo) HasGreeted(fromUserID, toUserID uint) (bool, error) {
	var count int64
	err := r.DB.Model(&models.Greeting{}).
		Where("from_user_id = ? AND to_user_id = ?", fromUserID, toUserID).
		Count(&count).Error
	return count > 0, err
}
