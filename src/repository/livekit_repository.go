package repository

import (
	"bubble/src/db/models"
)

// ──────────────────────────────────────────────
// LiveKit Room & Participant repository methods
// ──────────────────────────────────────────────

// LiveKit Rooms

func (r *Repo) CreateLiveKitRoom(room *models.LiveKitRoom) error {
	return r.DB.Create(room).Error
}

func (r *Repo) GetLiveKitRoomByChannelID(channelID uint) (*models.LiveKitRoom, error) {
	var room models.LiveKitRoom
	if err := r.DB.Where("channel_id = ?", channelID).First(&room).Error; err != nil {
		return nil, err
	}
	return &room, nil
}

func (r *Repo) GetLiveKitRoomByName(roomName string) (*models.LiveKitRoom, error) {
	var room models.LiveKitRoom
	if err := r.DB.Where("room_name = ?", roomName).First(&room).Error; err != nil {
		return nil, err
	}
	return &room, nil
}

func (r *Repo) UpdateLiveKitRoom(roomID uint, fields map[string]interface{}) error {
	return r.DB.Model(&models.LiveKitRoom{}).Where("id = ?", roomID).Updates(fields).Error
}

func (r *Repo) DeleteLiveKitRoomByChannelID(channelID uint) error {
	return r.DB.Where("channel_id = ?", channelID).Delete(&models.LiveKitRoom{}).Error
}

func (r *Repo) ListActiveLiveKitRooms() ([]models.LiveKitRoom, error) {
	var rooms []models.LiveKitRoom
	err := r.DB.Where("is_active = ? AND closed_at IS NULL", true).Find(&rooms).Error
	return rooms, err
}

// LiveKit Participants

func (r *Repo) CreateLiveKitParticipant(participant *models.LiveKitParticipant) error {
	return r.DB.Create(participant).Error
}

func (r *Repo) GetLiveKitParticipantsByRoom(roomID uint) ([]models.LiveKitParticipant, error) {
	var participants []models.LiveKitParticipant
	err := r.DB.Where("room_id = ? AND left_at IS NULL", roomID).Find(&participants).Error
	return participants, err
}

func (r *Repo) GetLiveKitParticipantByIdentity(identity string, roomID uint) (*models.LiveKitParticipant, error) {
	var participant models.LiveKitParticipant
	err := r.DB.Where("participant_identity = ? AND room_id = ? AND left_at IS NULL", identity, roomID).First(&participant).Error
	if err != nil {
		return nil, err
	}
	return &participant, nil
}

func (r *Repo) GetLiveKitParticipantBySID(sid string) (*models.LiveKitParticipant, error) {
	var participant models.LiveKitParticipant
	err := r.DB.Where("participant_sid = ? AND left_at IS NULL", sid).First(&participant).Error
	if err != nil {
		return nil, err
	}
	return &participant, nil
}

func (r *Repo) UpdateLiveKitParticipant(participantID uint, fields map[string]interface{}) error {
	return r.DB.Model(&models.LiveKitParticipant{}).Where("id = ?", participantID).Updates(fields).Error
}

// GetLiveKitRoomStats 获取房间统计信息
func (r *Repo) GetLiveKitRoomStats(roomID uint) (map[string]interface{}, error) {
	var result struct {
		TotalParticipants int
		TotalDuration     int
		AvgDuration       float64
	}

	err := r.DB.Model(&models.LiveKitParticipant{}).
		Where("room_id = ?", roomID).
		Select("COUNT(*) as total_participants, SUM(duration_seconds) as total_duration, AVG(duration_seconds) as avg_duration").
		Scan(&result).Error

	if err != nil {
		return nil, err
	}

	var activeCount int64
	r.DB.Model(&models.LiveKitParticipant{}).
		Where("room_id = ? AND left_at IS NULL", roomID).
		Count(&activeCount)

	return map[string]interface{}{
		"totalParticipants": result.TotalParticipants,
		"totalDuration":     result.TotalDuration,
		"avgDuration":       result.AvgDuration,
		"currentActive":     activeCount,
	}, nil
}

// GetLiveKitGlobalStats 获取全局统计信息
func (r *Repo) GetLiveKitGlobalStats() (map[string]interface{}, error) {
	var stats struct {
		TotalRooms          int64
		ActiveRooms         int64
		TotalParticipants   int64
		CurrentParticipants int64
		TotalDuration       int64
	}

	r.DB.Model(&models.LiveKitRoom{}).Count(&stats.TotalRooms)

	r.DB.Model(&models.LiveKitRoom{}).
		Where("is_active = ? AND closed_at IS NULL", true).
		Count(&stats.ActiveRooms)

	r.DB.Model(&models.LiveKitParticipant{}).Count(&stats.TotalParticipants)

	r.DB.Model(&models.LiveKitParticipant{}).
		Where("left_at IS NULL").
		Count(&stats.CurrentParticipants)

	r.DB.Model(&models.LiveKitParticipant{}).
		Select("SUM(duration_seconds)").
		Scan(&stats.TotalDuration)

	return map[string]interface{}{
		"totalRooms":           stats.TotalRooms,
		"activeRooms":          stats.ActiveRooms,
		"totalParticipants":    stats.TotalParticipants,
		"currentParticipants":  stats.CurrentParticipants,
		"totalDurationMinutes": stats.TotalDuration / 60,
	}, nil
}
