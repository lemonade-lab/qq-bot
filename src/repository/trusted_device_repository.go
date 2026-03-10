package repository

import (
	"time"

	"bubble/src/db/models"

	"gorm.io/gorm"
)

// UpsertTrustedDevice creates or updates a trusted device record.
// If the device was previously revoked, this will re-trust it (by clearing RevokedAt and setting Trusted=true).
func (r *Repo) UpsertTrustedDevice(userID uint, deviceID, name, ip, userAgent string) (*models.TrustedDevice, error) {
	if deviceID == "" {
		return nil, gorm.ErrInvalidData
	}
	var td models.TrustedDevice
	err := r.DB.Where("user_id = ? AND device_id = ?", userID, deviceID).First(&td).Error
	now := time.Now()
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			td = models.TrustedDevice{
				UserID:    userID,
				DeviceID:  deviceID,
				Name:      name,
				Trusted:   true,
				LastIP:    ip,
				UserAgent: userAgent,
				LastSeen:  &now,
			}
			if err := r.DB.Create(&td).Error; err != nil {
				return nil, err
			}
			return &td, nil
		}
		return nil, err
	}

	updates := map[string]any{
		"trusted":    true,
		"revoked_at": nil,
		"last_ip":    ip,
		"user_agent": userAgent,
		"last_seen":  &now,
	}
	if name != "" {
		updates["name"] = name
	}
	if err := r.DB.Model(&td).Updates(updates).Error; err != nil {
		return nil, err
	}
	return &td, nil
}

// UpsertTrustedDeviceV2 creates or updates a trusted device with platform and device token support.
func (r *Repo) UpsertTrustedDeviceV2(userID uint, deviceID, name, platform, deviceTokenHash, ip, userAgent string, tokenExpire *time.Time) (*models.TrustedDevice, error) {
	if deviceID == "" {
		return nil, gorm.ErrInvalidData
	}
	var td models.TrustedDevice
	err := r.DB.Where("user_id = ? AND device_id = ?", userID, deviceID).First(&td).Error
	now := time.Now()
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			td = models.TrustedDevice{
				UserID:            userID,
				DeviceID:          deviceID,
				Name:              name,
				Platform:          platform,
				Trusted:           true,
				DeviceTokenHash:   deviceTokenHash,
				DeviceTokenExpire: tokenExpire,
				LastIP:            ip,
				UserAgent:         userAgent,
				LastSeen:          &now,
				LastLoginAt:       &now,
			}
			if err := r.DB.Create(&td).Error; err != nil {
				return nil, err
			}
			return &td, nil
		}
		return nil, err
	}

	updates := map[string]any{
		"trusted":             true,
		"revoked_at":          nil,
		"device_token_hash":   deviceTokenHash,
		"device_token_expire": tokenExpire,
		"last_ip":             ip,
		"user_agent":          userAgent,
		"last_seen":           &now,
		"last_login_at":       &now,
	}
	if name != "" {
		updates["name"] = name
	}
	if platform != "" {
		updates["platform"] = platform
	}
	if err := r.DB.Model(&td).Updates(updates).Error; err != nil {
		return nil, err
	}
	td.Trusted = true
	td.RevokedAt = nil
	td.DeviceTokenHash = deviceTokenHash
	td.DeviceTokenExpire = tokenExpire
	td.LastLoginAt = &now
	return &td, nil
}

// GetTrustedDevice returns a trusted device by userID and deviceID.
func (r *Repo) GetTrustedDevice(userID uint, deviceID string) (*models.TrustedDevice, error) {
	if deviceID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var td models.TrustedDevice
	err := r.DB.Where("user_id = ? AND device_id = ?", userID, deviceID).First(&td).Error
	if err != nil {
		return nil, err
	}
	return &td, nil
}

// RotateDeviceToken updates the device token hash and expiry (token rotation on each use).
func (r *Repo) RotateDeviceToken(userID uint, deviceID, newTokenHash string, newExpire *time.Time, ip, userAgent string) error {
	if deviceID == "" {
		return gorm.ErrInvalidData
	}
	now := time.Now()
	return r.DB.Model(&models.TrustedDevice{}).
		Where("user_id = ? AND device_id = ? AND trusted = ? AND revoked_at IS NULL", userID, deviceID, true).
		Updates(map[string]any{
			"device_token_hash":   newTokenHash,
			"device_token_expire": newExpire,
			"last_ip":             ip,
			"user_agent":          userAgent,
			"last_seen":           &now,
			"last_login_at":       &now,
		}).Error
}

// ClearDeviceToken removes the device token (e.g., on logout from device).
func (r *Repo) ClearDeviceToken(userID uint, deviceID string) error {
	if deviceID == "" {
		return gorm.ErrInvalidData
	}
	return r.DB.Model(&models.TrustedDevice{}).
		Where("user_id = ? AND device_id = ?", userID, deviceID).
		Updates(map[string]any{
			"device_token_hash":   "",
			"device_token_expire": nil,
		}).Error
}

func (r *Repo) IsTrustedDevice(userID uint, deviceID string) (bool, error) {
	if deviceID == "" {
		return false, nil
	}
	var td models.TrustedDevice
	err := r.DB.Where("user_id = ? AND device_id = ?", userID, deviceID).First(&td).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return false, nil
		}
		return false, err
	}
	if !td.Trusted || td.RevokedAt != nil {
		return false, nil
	}
	return true, nil
}

func (r *Repo) TouchTrustedDevice(userID uint, deviceID, ip, userAgent string) error {
	if deviceID == "" {
		return gorm.ErrInvalidData
	}
	now := time.Now()
	return r.DB.Model(&models.TrustedDevice{}).
		Where("user_id = ? AND device_id = ? AND revoked_at IS NULL AND trusted = ?", userID, deviceID, true).
		Updates(map[string]any{"last_seen": &now, "last_ip": ip, "user_agent": userAgent}).Error
}

func (r *Repo) ListTrustedDevices(userID uint) ([]models.TrustedDevice, error) {
	var list []models.TrustedDevice
	err := r.DB.Where("user_id = ?", userID).Order("id desc").Find(&list).Error
	return list, err
}

func (r *Repo) RevokeTrustedDevice(userID uint, deviceID string) error {
	if deviceID == "" {
		return gorm.ErrInvalidData
	}
	now := time.Now()
	return r.DB.Model(&models.TrustedDevice{}).
		Where("user_id = ? AND device_id = ? AND revoked_at IS NULL", userID, deviceID).
		Updates(map[string]any{"revoked_at": &now, "trusted": false}).Error
}

// DeleteTrustedDevice permanently deletes a device record from the database.
func (r *Repo) DeleteTrustedDevice(userID uint, deviceID string) error {
	if deviceID == "" {
		return gorm.ErrInvalidData
	}
	return r.DB.Where("user_id = ? AND device_id = ?", userID, deviceID).
		Delete(&models.TrustedDevice{}).Error
}
