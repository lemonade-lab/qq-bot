package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"bubble/src/config"
	"bubble/src/db/models"
)

// QRCodeLoginState 二维码登录状态
type QRCodeLoginState string

const (
	QRCodeStatePending   QRCodeLoginState = "pending"   // 等待扫描
	QRCodeStateScanned   QRCodeLoginState = "scanned"   // 已扫描，等待确认
	QRCodeStateConfirmed QRCodeLoginState = "confirmed" // 已确认
	QRCodeStateExpired   QRCodeLoginState = "expired"   // 已过期
	QRCodeStateCancelled QRCodeLoginState = "cancelled" // 已取消
)

// QRCodeType 二维码类型
type QRCodeType string

const (
	QRCodeTypeLogin   QRCodeType = "login"   // 登录
	QRCodeTypeUser    QRCodeType = "user"    // 用户卡片
	QRCodeTypeChannel QRCodeType = "channel" // 频道卡片
	QRCodeTypeGuild   QRCodeType = "guild"   // 服务器/群组
)

// QRCodeLoginData 二维码登录数据
type QRCodeLoginData struct {
	Code        string           `json:"code"`        // 二维码唯一标识
	Type        QRCodeType       `json:"type"`        // 二维码类型
	State       QRCodeLoginState `json:"state"`       // 当前状态
	UserID      uint             `json:"userId"`      // 扫描用户ID（确认后填充）
	ClientType  string           `json:"clientType"`  // 客户端类型：web 或 mobile(桌面端)
	TrustDevice bool             `json:"trustDevice"` // 扫码端是否选择信任设备
	Payload     map[string]any   `json:"payload"`     // 业务数据（如userId、channelId等）
	CreatedAt   time.Time        `json:"createdAt"`   // 创建时间
	ExpiresAt   time.Time        `json:"expiresAt"`   // 过期时间
	IP          string           `json:"ip"`          // 请求IP
	UserAgent   string           `json:"userAgent"`   // 用户代理
}

// generateQRCode 生成二维码唯一标识
func generateQRCode() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// redisKeyQRCode 生成二维码在 Redis 中的键
func redisKeyQRCode(code string) string {
	return fmt.Sprintf("qrcode:login:%s", code)
}

// redisKeyQRCodeWebSocket 生成二维码 WebSocket 订阅的键（用于推送状态变更）
func redisKeyQRCodeWebSocket(code string) string {
	return fmt.Sprintf("qrcode:ws:%s", code)
}

// CreateQRCodeLogin 创建二维码登录会话
func (s *Service) CreateQRCodeLogin(ip, userAgent, clientType string) (*QRCodeLoginData, error) {
	return s.CreateQRCode(QRCodeTypeLogin, nil, ip, userAgent, clientType)
}

// CreateQRCode 创建通用二维码（支持多种业务类型）
// qrType: 二维码类型（login/user/channel/guild）
// payload: 业务数据，例如 {"userId": 123} 或 {"channelId": 456}
func (s *Service) CreateQRCode(qrType QRCodeType, payload map[string]any, ip, userAgent, clientType string) (*QRCodeLoginData, error) {
	code, err := generateQRCode()
	if err != nil {
		return nil, fmt.Errorf("生成二维码失败: %w", err)
	}

	// 默认为 web，如果传入 mobile 则使用 mobile
	if clientType == "" {
		clientType = "web"
	}

	// 根据二维码类型设置不同的过期时间
	var expireSeconds int
	switch qrType {
	case QRCodeTypeLogin:
		expireSeconds = config.QRCodeLoginExpireSeconds // 5分钟
	case QRCodeTypeUser:
		expireSeconds = config.QRCodeUserExpireSeconds // 24小时
	case QRCodeTypeChannel:
		expireSeconds = config.QRCodeChannelExpireSeconds // 24小时
	case QRCodeTypeGuild:
		expireSeconds = config.QRCodeGuildExpireSeconds // 30天
	default:
		expireSeconds = config.QRCodeLoginExpireSeconds // 默认5分钟
	}

	now := time.Now()
	data := &QRCodeLoginData{
		Code:       code,
		Type:       qrType,
		State:      QRCodeStatePending,
		ClientType: clientType,
		Payload:    payload,
		CreatedAt:  now,
		ExpiresAt:  now.Add(time.Duration(expireSeconds) * time.Second),
		IP:         ip,
		UserAgent:  userAgent,
	}

	// 存储到 Redis
	ctx := context.Background()
	key := redisKeyQRCode(code)

	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("序列化二维码数据失败: %w", err)
	}

	// 使用与过期时间一致的 TTL
	ttl := time.Until(data.ExpiresAt)
	err = s.Repo.Redis.Set(ctx, key, jsonData, ttl).Err()
	if err != nil {
		return nil, fmt.Errorf("保存二维码到 Redis 失败: %w", err)
	}

	return data, nil
}

// GetQRCodeLogin 获取二维码登录数据
func (s *Service) GetQRCodeLogin(code string) (*QRCodeLoginData, error) {
	ctx := context.Background()
	key := redisKeyQRCode(code)

	jsonData, err := s.Repo.Redis.Get(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("二维码不存在或已过期")
	}

	var data QRCodeLoginData
	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return nil, fmt.Errorf("解析二维码数据失败: %w", err)
	}

	// 检查是否过期
	if time.Now().After(data.ExpiresAt) {
		data.State = QRCodeStateExpired
		s.updateQRCodeLogin(&data)
		return &data, nil
	}

	return &data, nil
}

// updateQRCodeLogin 更新二维码登录数据
func (s *Service) updateQRCodeLogin(data *QRCodeLoginData) error {
	ctx := context.Background()
	key := redisKeyQRCode(data.Code)

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("序列化二维码数据失败: %w", err)
	}

	// 计算剩余过期时间
	ttl := time.Until(data.ExpiresAt)
	if ttl <= 0 {
		ttl = time.Second // 至少保留1秒，让客户端能收到过期状态
	}

	err = s.Repo.Redis.Set(ctx, key, jsonData, ttl).Err()
	if err != nil {
		return fmt.Errorf("更新二维码到 Redis 失败: %w", err)
	}

	return nil
}

// ScanQRCodeLogin 扫描二维码（移动端调用）
func (s *Service) ScanQRCodeLogin(code string, userID uint) error {
	data, err := s.GetQRCodeLogin(code)
	if err != nil {
		return err
	}

	if data.State != QRCodeStatePending {
		return fmt.Errorf("二维码状态不正确，当前状态: %s", data.State)
	}

	// 更新状态为已扫描
	data.State = QRCodeStateScanned
	data.UserID = userID

	if err := s.updateQRCodeLogin(data); err != nil {
		return err
	}

	// 通过 Redis Pub/Sub 通知 WebSocket 连接
	s.notifyQRCodeChange(data)

	return nil
}

// ConfirmQRCodeLogin 确认二维码登录（移动端调用）
func (s *Service) ConfirmQRCodeLogin(code string, userID uint, trustDevice bool) (*models.User, error) {
	data, err := s.GetQRCodeLogin(code)
	if err != nil {
		return nil, err
	}

	if data.State != QRCodeStateScanned {
		return nil, fmt.Errorf("二维码状态不正确，当前状态: %s", data.State)
	}

	if data.UserID != userID {
		return nil, fmt.Errorf("用户不匹配")
	}

	// 更新状态为已确认，并记录信任设备选择
	data.State = QRCodeStateConfirmed
	data.TrustDevice = trustDevice

	if err := s.updateQRCodeLogin(data); err != nil {
		return nil, err
	}

	// 获取用户信息
	user, err := s.Repo.GetUserByID(userID)
	if err != nil {
		return nil, fmt.Errorf("获取用户信息失败: %w", err)
	}

	// 通过 Redis Pub/Sub 通知 WebSocket 连接
	s.notifyQRCodeChange(data)

	return user, nil
}

// GetQRCodeCardData 获取二维码对应的卡片数据（移动端扫码后调用）
func (s *Service) GetQRCodeCardData(code string) (map[string]any, error) {
	data, err := s.GetQRCodeLogin(code)
	if err != nil {
		return nil, err
	}

	result := map[string]any{
		"code":  data.Code,
		"type":  data.Type,
		"state": data.State,
	}

	// 辅助函数：从payload中提取uint
	getUintFromPayload := func(key string) (uint, bool) {
		if data.Payload == nil {
			return 0, false
		}
		switch v := data.Payload[key].(type) {
		case float64:
			return uint(v), true
		case string:
			// 尝试解析字符串
			var id uint
			if _, err := fmt.Sscanf(v, "%d", &id); err == nil {
				return id, true
			}
		case int:
			return uint(v), true
		case uint:
			return v, true
		}
		return 0, false
	}

	// 根据二维码类型返回对应的卡片数据
	switch data.Type {
	case QRCodeTypeLogin:
		result["title"] = "扫码登录"
		result["description"] = "确认登录到新设备"

	case QRCodeTypeUser:
		// 获取用户信息
		if userID, ok := getUintFromPayload("userId"); ok {
			user, err := s.Repo.GetUserByID(userID)
			if err == nil {
				result["user"] = map[string]any{
					"id":     user.ID,
					"name":   user.Name,
					"avatar": user.Avatar,
					"bio":    user.Bio,
				}
				result["title"] = "用户信息"
				result["description"] = fmt.Sprintf("查看 %s 的个人资料", user.Name)
			}
		}

	case QRCodeTypeChannel:
		// 获取频道信息
		if channelID, ok := getUintFromPayload("channelId"); ok {
			channel, err := s.Repo.GetChannel(channelID)
			if err == nil {
				guild, _ := s.Repo.GetGuild(channel.GuildID)
				result["channel"] = map[string]any{
					"id":      channel.ID,
					"name":    channel.Name,
					"type":    channel.Type,
					"banner":  channel.Banner,
					"guildId": channel.GuildID,
				}
				if guild != nil {
					result["guild"] = map[string]any{
						"id":     guild.ID,
						"name":   guild.Name,
						"avatar": guild.Avatar,
					}
				}
				result["title"] = "频道信息"
				result["description"] = fmt.Sprintf("查看频道 #%s", channel.Name)
			}
		}

	case QRCodeTypeGuild:
		// 获取服务器信息
		if guildID, ok := getUintFromPayload("guildId"); ok {
			guild, err := s.Repo.GetGuild(guildID)
			if err == nil {
				result["guild"] = map[string]any{
					"id":          guild.ID,
					"name":        guild.Name,
					"description": guild.Description,
					"avatar":      guild.Avatar,
					"banner":      guild.Banner,
				}
				result["title"] = "服务器邀请"
				result["description"] = fmt.Sprintf("加入服务器 %s", guild.Name)
			}
		}
	}

	return result, nil
}

// CancelQRCodeLogin 取消二维码登录（移动端或 PC 端调用）
func (s *Service) CancelQRCodeLogin(code string) error {
	data, err := s.GetQRCodeLogin(code)
	if err != nil {
		return err
	}

	// 更新状态为已取消
	data.State = QRCodeStateCancelled

	if err := s.updateQRCodeLogin(data); err != nil {
		return err
	}

	// 通过 Redis Pub/Sub 通知 WebSocket 连接
	s.notifyQRCodeChange(data)

	return nil
}

// notifyQRCodeChange 通知二维码状态变更（通过 Redis Pub/Sub）
func (s *Service) notifyQRCodeChange(data *QRCodeLoginData) {
	ctx := context.Background()
	channel := redisKeyQRCodeWebSocket(data.Code)

	payload := map[string]interface{}{
		"code":   data.Code,
		"state":  data.State,
		"userId": data.UserID,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return
	}

	s.Repo.Redis.Publish(ctx, channel, jsonData)
}
