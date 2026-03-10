package repository

import (
	"time"

	"bubble/src/db/models"
)

// ──────────────────────────────────────────────
// SubRoom (子房间) repository methods
// ──────────────────────────────────────────────

func (r *Repo) CreateSubRoom(room *models.SubRoom) error {
	return r.DB.Create(room).Error
}

func (r *Repo) GetSubRoom(id uint) (*models.SubRoom, error) {
	var room models.SubRoom
	if err := r.DB.First(&room, id).Error; err != nil {
		return nil, err
	}
	return &room, nil
}

func (r *Repo) UpdateSubRoom(id uint, updates map[string]interface{}) error {
	return r.DB.Model(&models.SubRoom{}).Where("id = ?", id).Updates(updates).Error
}

func (r *Repo) DeleteSubRoom(id uint) error {
	return r.DB.Delete(&models.SubRoom{}, id).Error
}

// ListSubRooms 列出频道下的所有子房间（所有服务器成员可见）
func (r *Repo) ListSubRooms(channelID uint) ([]models.SubRoom, error) {
	var list []models.SubRoom
	err := r.DB.Where("channel_id = ?", channelID).Order("created_at ASC").Find(&list).Error
	return list, err
}

// HasSubRoomInChannel 检查用户在该频道下是否已创建子房间
func (r *Repo) HasSubRoomInChannel(channelID, ownerID uint) (bool, error) {
	var count int64
	err := r.DB.Model(&models.SubRoom{}).Where("channel_id = ? AND owner_id = ?", channelID, ownerID).Count(&count).Error
	return count > 0, err
}

// ──────────────────────────────────────────────
// SubRoomMember
// ──────────────────────────────────────────────

func (r *Repo) AddSubRoomMember(m *models.SubRoomMember) error {
	return r.DB.Create(m).Error
}

func (r *Repo) RemoveSubRoomMember(roomID, userID uint) error {
	return r.DB.Where("room_id = ? AND user_id = ?", roomID, userID).Delete(&models.SubRoomMember{}).Error
}

func (r *Repo) GetSubRoomMember(roomID, userID uint) (*models.SubRoomMember, error) {
	var m models.SubRoomMember
	if err := r.DB.Where("room_id = ? AND user_id = ?", roomID, userID).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *Repo) IsSubRoomMember(roomID, userID uint) bool {
	var count int64
	r.DB.Model(&models.SubRoomMember{}).Where("room_id = ? AND user_id = ?", roomID, userID).Count(&count)
	return count > 0
}

func (r *Repo) ListSubRoomMembers(roomID uint) ([]models.SubRoomMember, error) {
	var list []models.SubRoomMember
	err := r.DB.Where("room_id = ?", roomID).Order("created_at ASC").Find(&list).Error
	return list, err
}

func (r *Repo) CountSubRoomMembers(roomID uint) (int64, error) {
	var count int64
	err := r.DB.Model(&models.SubRoomMember{}).Where("room_id = ?", roomID).Count(&count).Error
	return count, err
}

func (r *Repo) GetSubRoomMemberIDs(roomID uint) ([]uint, error) {
	var ids []uint
	err := r.DB.Model(&models.SubRoomMember{}).Where("room_id = ?", roomID).Pluck("user_id", &ids).Error
	return ids, err
}

// ──────────────────────────────────────────────
// SubRoomMessage
// ──────────────────────────────────────────────

func (r *Repo) CreateSubRoomMessage(m *models.SubRoomMessage) error {
	return r.DB.Create(m).Error
}

func (r *Repo) GetSubRoomMessage(id uint) (*models.SubRoomMessage, error) {
	var m models.SubRoomMessage
	if err := r.DB.First(&m, id).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *Repo) ListSubRoomMessages(roomID uint, limit int, beforeID, afterID uint) ([]models.SubRoomMessage, error) {
	q := r.DB.Where("room_id = ? AND deleted_at IS NULL", roomID)
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	if afterID > 0 {
		q = q.Where("id > ?", afterID)
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var list []models.SubRoomMessage
	err := q.Order("id DESC").Limit(limit).Find(&list).Error
	return list, err
}

func (r *Repo) DeleteSubRoomMessage(id uint) error {
	now := time.Now()
	return r.DB.Model(&models.SubRoomMessage{}).Where("id = ?", id).Update("deleted_at", &now).Error
}

// SetSubRoomHidden 设置子房间隐藏状态
func (r *Repo) SetSubRoomHidden(roomID, userID uint, hidden bool) error {
	return r.DB.Model(&models.SubRoomMember{}).
		Where("room_id = ? AND user_id = ?", roomID, userID).
		Update("hidden", hidden).Error
}

// ListHiddenSubRooms 列出用户隐藏的子房间
func (r *Repo) ListHiddenSubRooms(userID uint) ([]models.SubRoom, error) {
	var roomIDs []uint
	r.DB.Model(&models.SubRoomMember{}).Where("user_id = ? AND hidden = ?", userID, true).Pluck("room_id", &roomIDs)
	if len(roomIDs) == 0 {
		return []models.SubRoom{}, nil
	}
	var list []models.SubRoom
	err := r.DB.Where("id IN ?", roomIDs).Order("last_message_at DESC, id DESC").Find(&list).Error
	return list, err
}
