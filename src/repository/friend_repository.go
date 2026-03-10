package repository

import (
	"bubble/src/db/models"

	"gorm.io/gorm"
)

// ──────────────────────────────────────────────
// Friend repository methods
// ──────────────────────────────────────────────

func (r *Repo) CreateFriendRequest(fromID, toID uint) error {
	f := &models.Friendship{FromUserID: fromID, ToUserID: toID, Status: "pending"}
	return r.DB.Create(f).Error
}

func (r *Repo) CreateAcceptedFriendship(fromID, toID uint) error {
	f := &models.Friendship{FromUserID: fromID, ToUserID: toID, Status: "accepted"}
	return r.DB.Create(f).Error
}

func (r *Repo) AcceptFriendRequest(fromID, toID uint) error {
	return r.DB.Model(&models.Friendship{}).Where("from_user_id=? AND to_user_id=? AND status=?", fromID, toID, "pending").Update("status", "accepted").Error
}

func (r *Repo) DeleteFriendship(a, b uint) error {
	return r.DB.Where("(from_user_id=? AND to_user_id=?) OR (from_user_id=? AND to_user_id=?)", a, b, b, a).Delete(&models.Friendship{}).Error
}

func (r *Repo) ListFriends(userID uint) ([]models.User, error) {
	var results []struct {
		models.User
		NicknameFrom string
		NicknameTo   string
		FromUserID   uint
		ToUserID     uint
	}
	err := r.DB.Raw(`
		SELECT u.*, f.nickname_from, f.nickname_to, f.from_user_id, f.to_user_id 
		FROM users u
		JOIN friendships f ON (
			(f.from_user_id = ? AND f.to_user_id = u.id) OR
			(f.to_user_id = ? AND f.from_user_id = u.id)
		) AND f.status = 'accepted'
	`, userID, userID).Scan(&results).Error
	if err != nil {
		return nil, err
	}

	users := make([]models.User, len(results))
	for i, r := range results {
		users[i] = r.User
		if r.FromUserID == userID {
			users[i].Nickname = r.NicknameFrom
		} else {
			users[i].Nickname = r.NicknameTo
		}
	}
	return users, nil
}

func (r *Repo) ExistsFriendship(a, b uint) (bool, string, error) {
	var f models.Friendship
	if err := r.DB.Where("(from_user_id=? AND to_user_id=?) OR (from_user_id=? AND to_user_id=?)", a, b, b, a).
		Order("id desc").First(&f).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return false, "", nil
		}
		return false, "", err
	}
	return true, f.Status, nil
}

func (r *Repo) ListFriendRequests(userID uint) ([]models.User, error) {
	var users []models.User
	err := r.DB.Raw(`
		SELECT u.* FROM users u
		JOIN friendships f ON f.from_user_id = u.id
		WHERE f.to_user_id = ? AND f.status = 'pending'
		ORDER BY f.id DESC
	`, userID).Scan(&users).Error
	return users, err
}

func (r *Repo) SetFriendNickname(userID, friendID uint, nickname string) error {
	var f models.Friendship
	err := r.DB.Where("(from_user_id=? AND to_user_id=?) OR (from_user_id=? AND to_user_id=?)",
		userID, friendID, friendID, userID).First(&f).Error
	if err != nil {
		return err
	}
	if f.FromUserID == userID {
		return r.DB.Model(&f).Update("nickname_from", nickname).Error
	}
	return r.DB.Model(&f).Update("nickname_to", nickname).Error
}

func (r *Repo) SearchFriends(userID uint, q string, limit int) ([]models.User, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	pattern := "%" + q + "%"
	var results []struct {
		models.User
		NicknameFrom string
		NicknameTo   string
		FromUserID   uint
		ToUserID     uint
	}

	err := r.DB.Raw(`
		SELECT u.*, f.nickname_from, f.nickname_to, f.from_user_id, f.to_user_id 
		FROM users u
		JOIN friendships f ON (
			(f.from_user_id = ? AND f.to_user_id = u.id) OR
			(f.to_user_id = ? AND f.from_user_id = u.id)
		) AND f.status = 'accepted'
		WHERE u.name LIKE ? 
			OR (f.from_user_id = ? AND f.nickname_from LIKE ?)
			OR (f.to_user_id = ? AND f.nickname_to LIKE ?)
		LIMIT ?
	`, userID, userID, pattern, userID, pattern, userID, pattern, limit).Scan(&results).Error

	if err != nil {
		return nil, err
	}

	users := make([]models.User, len(results))
	for i, r := range results {
		users[i] = r.User
		if r.FromUserID == userID {
			users[i].Nickname = r.NicknameFrom
		} else {
			users[i].Nickname = r.NicknameTo
		}
	}
	return users, nil
}

func (r *Repo) GetFriendshipStatus(userID1, userID2 uint) (string, error) {
	var f models.Friendship
	err := r.DB.Where("(from_user_id=? AND to_user_id=?) OR (from_user_id=? AND to_user_id=?)",
		userID1, userID2, userID2, userID1).Order("id desc").First(&f).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", nil
		}
		return "", err
	}
	return f.Status, nil
}
