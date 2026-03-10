package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"bubble/src/config"
	"bubble/src/db/models"
	"bubble/src/logger"
)

// GetChannel 获取频道信息
func (s *Service) GetChannel(channelID uint) (*models.Channel, error) {
	return s.Repo.GetChannel(channelID)
}

// SetChannelCategory 设置频道所属分类
func (s *Service) SetChannelCategory(channelID uint, categoryID uint) error {
	return s.Repo.SetChannelCategory(channelID, categoryID)
}

// ClearChannelCategory 清除频道分类
func (s *Service) ClearChannelCategory(channelID uint) error {
	return s.Repo.SetChannelCategory(channelID, 0)
}

// CreateChannel 创建频道
func (s *Service) CreateChannel(guildID uint, name string, channelType string, parentID *uint) (*models.Channel, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrBadRequest
	}
	if channelType == "" {
		channelType = "text"
	}
	if channelType != "text" && channelType != "media" && channelType != "forum" {
		return nil, &Err{Code: 400, Msg: "频道类型不合法：必须为文字、音视频或论坛"}
	}
	c := &models.Channel{
		GuildID:  guildID,
		Name:     name,
		Type:     channelType,
		ParentID: parentID,
	}

	if err := s.Repo.CreateChannel(c); err != nil {
		return nil, err
	}

	// 如果是音视频频道，创建 LiveKitRoom 数据库记录
	if channelType == "media" && s.LiveKit != nil {
		roomName := fmt.Sprintf("channel_%d", c.ID)
		livekitRoom := &models.LiveKitRoom{
			ChannelID:       c.ID,
			RoomName:        roomName,
			IsActive:        true,
			MaxParticipants: 50,
			CreatedAt:       time.Now(),
		}
		if err := s.Repo.CreateLiveKitRoom(livekitRoom); err != nil {
			logger.Warnf("[LiveKit] Failed to create room record for channel %d: %v", c.ID, err)
		}
	}

	return c, nil
}

func (s *Service) ListChannels(guildID uint) ([]models.Channel, error) {
	return s.Repo.ListChannels(guildID)
}

// DeleteChannel soft-deletes a channel and all sub-channels; requires PermManageChannels.
func (s *Service) DeleteChannel(channelID, userID uint) error {
	ch, err := s.Repo.GetChannel(channelID)
	if err != nil || ch == nil {
		return ErrNotFound
	}
	ok, err := s.HasGuildPerm(ch.GuildID, userID, PermManageChannels)
	if err != nil {
		return err
	}
	if !ok {
		return ErrUnauthorized
	}

	// 如果是音视频频道，清理 LiveKit 资源
	if ch.Type == "media" && s.LiveKit != nil {
		if err := s.Repo.DeleteLiveKitRoomByChannelID(channelID); err != nil {
			logger.Warnf("[LiveKit] Failed to delete room record for channel %d: %v", channelID, err)
		}

		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Infof("[LiveKit] Panic in DeleteRoom goroutine: %v", r)
				}
			}()

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			roomName := fmt.Sprintf("channel_%d", channelID)
			if err := s.LiveKit.DeleteRoom(ctx, roomName); err != nil {
				logger.Errorf("[LiveKit] Failed to delete room on server for channel %d: %v", channelID, err)
			} else {
				logger.Infof("[LiveKit] Room deleted on server for channel %d", channelID)
			}
		}()
	}

	return s.Repo.DeleteChannel(channelID)
}

// ReorderChannels 批量更新频道排序
func (s *Service) ReorderChannels(guildID, userID uint, orders []struct {
	ID        uint `json:"id"`
	SortOrder int  `json:"sortOrder"`
}) error {
	ok, err := s.HasGuildPerm(guildID, userID, PermManageChannels)
	if err != nil {
		return err
	}
	if !ok {
		return ErrUnauthorized
	}
	for _, o := range orders {
		ch, err := s.Repo.GetChannel(o.ID)
		if err != nil || ch == nil {
			return ErrNotFound
		}
		if ch.GuildID != guildID {
			return ErrBadRequest
		}
	}
	return s.Repo.BatchUpdateChannelOrders(orders)
}

// UpdateChannel 更新频道信息（名称、横幅等，需要管理频道权限）
func (s *Service) UpdateChannel(channelID, userID uint, name, banner *string) error {
	ch, err := s.Repo.GetChannel(channelID)
	if err != nil || ch == nil {
		return ErrNotFound
	}

	has, err := s.HasGuildPerm(ch.GuildID, userID, PermManageChannels)
	if err != nil || !has {
		return ErrUnauthorized
	}

	updates := make(map[string]any)

	if name != nil {
		n := strings.TrimSpace(*name)
		if n == "" {
			return &Err{Code: 400, Msg: "频道名称不合法"}
		}
		if len([]rune(n)) > int(config.MaxChannelNameLength) {
			return &Err{Code: 400, Msg: "频道名称过长"}
		}
		updates["name"] = n
	}

	if banner != nil {
		b := strings.TrimSpace(*banner)
		if len(b) > 512 {
			return &Err{Code: 400, Msg: "横幅路径过长（最多512个字符）"}
		}
		updates["banner"] = b
	}

	if len(updates) == 0 {
		return nil
	}

	return s.Repo.DB.Model(&models.Channel{}).Where("id = ?", channelID).Updates(updates).Error
}
