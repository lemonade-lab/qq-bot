package service

import (
	"crypto/rand"
	"encoding/base64"
	"strings"

	"bubble/src/db/models"
)

func generateDeviceID() (string, error) {
	// 生成 32 bytes 随机值，URL-safe base64（无 padding），长度约 43 字符
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// TrustDevice marks a device as trusted for a user.
func (s *Service) TrustDevice(userID uint, deviceID, name, ip, userAgent string) (*models.TrustedDevice, error) {
	deviceID = strings.TrimSpace(deviceID)
	name = strings.TrimSpace(name)
	// 升级：deviceId 作为“设备凭证”使用，必须是高熵不可预测。
	// 若客户端未提供，或长度过短（风险：可枚举/可撞库），则由服务端签发。
	if deviceID == "" || len(deviceID) < 32 {
		issued, err := generateDeviceID()
		if err != nil {
			return nil, err
		}
		deviceID = issued
	}
	return s.Repo.UpsertTrustedDevice(userID, deviceID, name, ip, userAgent)
}

// LoginByTrustedDevice authenticates a mobile login by (email + deviceId).
// This only checks that the device is trusted and not revoked.
func (s *Service) LoginByTrustedDevice(email, deviceID, ip, userAgent string) (*models.User, error) {
	u, err := s.Repo.GetUserByEmail(strings.TrimSpace(email))
	if err != nil || u == nil {
		return nil, ErrUnauthorized
	}
	ok, err := s.Repo.IsTrustedDevice(u.ID, strings.TrimSpace(deviceID))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrUnauthorized
	}
	_ = s.Repo.TouchTrustedDevice(u.ID, strings.TrimSpace(deviceID), ip, userAgent)
	return u, nil
}

func (s *Service) ListTrustedDevices(userID uint) ([]models.TrustedDevice, error) {
	return s.Repo.ListTrustedDevices(userID)
}

func (s *Service) RevokeTrustedDevice(userID uint, deviceID string) error {
	return s.Repo.RevokeTrustedDevice(userID, strings.TrimSpace(deviceID))
}

// DeleteTrustedDevice permanently removes a device record.
func (s *Service) DeleteTrustedDevice(userID uint, deviceID string) error {
	return s.Repo.DeleteTrustedDevice(userID, strings.TrimSpace(deviceID))
}

// AddTrustedDevice adds a new trusted device for the current user.
// This is typically used when a logged-in user wants to trust a new device.
func (s *Service) AddTrustedDevice(userID uint, deviceName, ip, userAgent string) (*models.TrustedDevice, error) {
	// Generate a new device ID for the user
	deviceID, err := generateDeviceID()
	if err != nil {
		return nil, err
	}
	return s.Repo.UpsertTrustedDevice(userID, deviceID, deviceName, ip, userAgent)
}
