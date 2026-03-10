package service

import "bubble/src/db/models"

// ListTimelineMoments 返回用户朋友圈时间线（自己的 + 好友的），分页
func (s *Service) ListTimelineMoments(userID uint, page int, limit int) ([]models.Moment, int, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	// 计算好友列表，排除设置了chat_only隐私模式的好友
	var friendships []models.Friendship
	if err := s.Repo.DB.Where(
		"(from_user_id = ? OR to_user_id = ?) AND status = 'accepted'",
		userID, userID,
	).Find(&friendships).Error; err != nil {
		return nil, 0, err
	}
	friendIDs := []uint{userID}
	for _, f := range friendships {
		if f.FromUserID == userID {
			if f.PrivacyModeTo != "chat_only" {
				friendIDs = append(friendIDs, f.ToUserID)
			}
		} else {
			if f.PrivacyModeFrom != "chat_only" {
				friendIDs = append(friendIDs, f.FromUserID)
			}
		}
	}

	var total int64
	if err := s.Repo.DB.Model(&models.Moment{}).
		Where("deleted_at IS NULL").
		Where("(user_id = ?) OR (user_id IN ? AND visibility IN ('all','friends'))", userID, friendIDs).
		Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var moments []models.Moment
	if err := s.Repo.DB.Model(&models.Moment{}).
		Where("deleted_at IS NULL").
		Where("(user_id = ?) OR (user_id IN ? AND visibility IN ('all','friends'))", userID, friendIDs).
		Order("id DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&moments).Error; err != nil {
		return nil, 0, err
	}
	return moments, int(total), nil
}

// ListMyMoments 返回用户自己发布的朋友圈，分页
func (s *Service) ListMyMoments(userID uint, page int, limit int) ([]models.Moment, int, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var total int64
	if err := s.Repo.DB.Model(&models.Moment{}).
		Where("deleted_at IS NULL AND user_id = ?", userID).
		Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var moments []models.Moment
	if err := s.Repo.DB.Model(&models.Moment{}).
		Where("deleted_at IS NULL AND user_id = ?", userID).
		Order("id DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&moments).Error; err != nil {
		return nil, 0, err
	}
	return moments, int(total), nil
}
