package repository

import (
	"errors"
	"time"

	"bubble/src/db/models"

	"gorm.io/gorm"
)

// ──────────────────────────────────────────────
// Channel repository methods
// ──────────────────────────────────────────────

func (r *Repo) CreateChannel(c *models.Channel) error {
	var g models.Guild
	if err := r.DB.Where("id = ? AND deleted_at IS NULL", c.GuildID).First(&g).Error; err != nil {
		return errors.New("服务器不存在")
	}
	if c.ParentID != nil {
		var parent models.Channel
		if err := r.DB.Where("id = ? AND guild_id = ? AND deleted_at IS NULL", *c.ParentID, c.GuildID).First(&parent).Error; err != nil {
			return errors.New("上级频道不存在")
		}
	}
	return r.DB.Create(c).Error
}

func (r *Repo) ListChannels(guildID uint) ([]models.Channel, error) {
	var list []models.Channel
	return list, r.DB.Where("guild_id = ? AND deleted_at IS NULL", guildID).Order("sort_order asc, id asc").Find(&list).Error
}

func (r *Repo) GetChannel(id uint) (*models.Channel, error) {
	var c models.Channel
	if err := r.DB.Where("id = ? AND deleted_at IS NULL", id).First(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repo) DeleteChannel(id uint) error {
	now := time.Now()
	return r.DB.Model(&models.Channel{}).
		Where("id = ? OR parent_id = ?", id, id).
		Update("deleted_at", now).Error
}

func (r *Repo) UpdateChannelOrder(channelID uint, sortOrder int) error {
	return r.DB.Model(&models.Channel{}).Where("id = ?", channelID).Update("sort_order", sortOrder).Error
}

func (r *Repo) BatchUpdateChannelOrders(updates []struct {
	ID        uint `json:"id"`
	SortOrder int  `json:"sortOrder"`
}) error {
	return r.DB.Transaction(func(tx *gorm.DB) error {
		for _, u := range updates {
			if err := tx.Model(&models.Channel{}).Where("id = ?", u.ID).Update("sort_order", u.SortOrder).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// ──────────────────────────────────────────────
// Channel Categories
// ──────────────────────────────────────────────

func (r *Repo) CreateChannelCategory(category *models.ChannelCategory) error {
	return r.DB.Create(category).Error
}

func (r *Repo) ListChannelCategories(guildID uint) ([]models.ChannelCategory, error) {
	var list []models.ChannelCategory
	return list, r.DB.Where("guild_id = ? AND deleted_at IS NULL", guildID).Order("sort_order asc").Find(&list).Error
}

func (r *Repo) GetChannelCategory(id uint) (*models.ChannelCategory, error) {
	var cat models.ChannelCategory
	err := r.DB.Where("id = ? AND deleted_at IS NULL", id).First(&cat).Error
	if err != nil {
		return nil, err
	}
	return &cat, nil
}

func (r *Repo) UpdateChannelCategory(id uint, name string) error {
	return r.DB.Model(&models.ChannelCategory{}).Where("id = ?", id).Update("name", name).Error
}

func (r *Repo) DeleteChannelCategory(id uint) error {
	return r.DB.Model(&models.ChannelCategory{}).Where("id = ?", id).Update("deleted_at", time.Now()).Error
}

func (r *Repo) RemoveCategoryFromChannels(categoryID uint) error {
	return r.DB.Model(&models.Channel{}).Where("category_id = ?", categoryID).Update("category_id", nil).Error
}

func (r *Repo) SetChannelCategory(channelID uint, categoryID uint) error {
	if categoryID == 0 {
		return r.DB.Model(&models.Channel{}).Where("id = ?", channelID).Update("category_id", nil).Error
	}
	return r.DB.Model(&models.Channel{}).Where("id = ?", channelID).Update("category_id", categoryID).Error
}

func (r *Repo) BatchUpdateCategoryOrders(updates []struct {
	ID        uint `json:"id"`
	SortOrder int  `json:"sortOrder"`
}) error {
	return r.DB.Transaction(func(tx *gorm.DB) error {
		for _, u := range updates {
			if err := tx.Model(&models.ChannelCategory{}).Where("id = ?", u.ID).Update("sort_order", u.SortOrder).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
