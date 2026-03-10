package service

import (
	"time"

	"bubble/src/db/models"

	"gorm.io/datatypes"
)

// Announcements

func (s *Service) CreateAnnouncement(guildID, authorID uint, title, content string, images datatypes.JSON) (*models.Announcement, error) {
	hasPerm, err := s.HasGuildPerm(guildID, authorID, PermManageGuild)
	if err != nil {
		return nil, err
	}
	if !hasPerm {
		guild, err := s.Repo.GetGuild(guildID)
		if err != nil {
			return nil, err
		}
		if guild.OwnerID != authorID {
			return nil, ErrUnauthorized
		}
	}

	a := &models.Announcement{
		GuildID:   guildID,
		AuthorID:  authorID,
		Title:     title,
		Content:   content,
		CreatedAt: time.Now(),
	}
	if len(images) > 0 {
		a.Images = images
	}
	if err := s.Repo.CreateAnnouncement(a); err != nil {
		return nil, err
	}
	return a, nil
}

func (s *Service) GetAnnouncement(id uint) (*models.Announcement, error) {
	return s.Repo.GetAnnouncement(id)
}

func (s *Service) UpdateAnnouncement(id, userID uint, title, content *string, images *datatypes.JSON) (*models.Announcement, error) {
	announcement, err := s.Repo.GetAnnouncement(id)
	if err != nil {
		return nil, ErrNotFound
	}
	hasPerm, err := s.HasGuildPerm(announcement.GuildID, userID, PermManageGuild)
	if err != nil {
		return nil, err
	}
	if !hasPerm {
		guild, err := s.Repo.GetGuild(announcement.GuildID)
		if err != nil {
			return nil, err
		}
		if guild.OwnerID != userID && announcement.AuthorID != userID {
			return nil, ErrUnauthorized
		}
	}
	if title != nil {
		announcement.Title = *title
	}
	if content != nil {
		announcement.Content = *content
	}
	if images != nil {
		announcement.Images = *images
	}
	now := time.Now()
	announcement.UpdatedAt = &now
	if err := s.Repo.UpdateAnnouncement(announcement); err != nil {
		return nil, err
	}
	return announcement, nil
}

func (s *Service) DeleteAnnouncement(id, userID uint) error {
	announcement, err := s.Repo.GetAnnouncement(id)
	if err != nil {
		return err
	}
	hasPerm, err := s.HasGuildPerm(announcement.GuildID, userID, PermManageGuild)
	if err != nil {
		return err
	}
	if !hasPerm {
		guild, err := s.Repo.GetGuild(announcement.GuildID)
		if err != nil {
			return err
		}
		if guild.OwnerID != userID && announcement.AuthorID != userID {
			return ErrUnauthorized
		}
	}
	return s.Repo.DeleteAnnouncement(id)
}

// PinAnnouncement 置顶公告
func (s *Service) PinAnnouncement(announcementID, userID uint) (*models.Announcement, error) {
	a, err := s.Repo.GetAnnouncement(announcementID)
	if err != nil {
		return nil, ErrNotFound
	}
	hasPerm, err := s.HasGuildPerm(a.GuildID, userID, PermManageGuild)
	if err != nil {
		return nil, err
	}
	if !hasPerm {
		guild, err := s.Repo.GetGuild(a.GuildID)
		if err != nil {
			return nil, err
		}
		if guild.OwnerID != userID {
			return nil, ErrUnauthorized
		}
	}
	if err := s.Repo.PinAnnouncement(a.GuildID, announcementID); err != nil {
		return nil, err
	}
	return s.Repo.GetAnnouncement(announcementID)
}

// UnpinAnnouncement 取消置顶公告
func (s *Service) UnpinAnnouncement(announcementID, userID uint) error {
	a, err := s.Repo.GetAnnouncement(announcementID)
	if err != nil {
		return ErrNotFound
	}
	hasPerm, err := s.HasGuildPerm(a.GuildID, userID, PermManageGuild)
	if err != nil {
		return err
	}
	if !hasPerm {
		guild, err := s.Repo.GetGuild(a.GuildID)
		if err != nil {
			return err
		}
		if guild.OwnerID != userID {
			return ErrUnauthorized
		}
	}
	return s.Repo.UnpinAnnouncement(a.GuildID, announcementID)
}

// GetFeaturedAnnouncement 获取精选公告（置顶优先，否则最新）
func (s *Service) GetFeaturedAnnouncement(guildID uint) (*models.Announcement, error) {
	return s.Repo.GetFeaturedAnnouncement(guildID)
}
