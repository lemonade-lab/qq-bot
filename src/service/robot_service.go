package service

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"bubble/src/db/models"
	"bubble/src/repository"

	"gorm.io/gorm"
)

// CreateRobot 创建机器人
// name 用作 BotUser 的用户名（社交属性），Robot 本身不存储 name
func (s *Service) CreateRobot(ownerID uint, name string, description ...string) (*models.Robot, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, &Err{Code: 400, Msg: "机器人名称不合法"}
	}
	var createdRobot *models.Robot
	err := s.Repo.DB.Transaction(func(tx *gorm.DB) error {
		botName := name
		placeholderEmail := "bot_" + randToken(8) + "@bot.local"
		botUser := &models.User{Name: botName, Email: placeholderEmail, Token: "bot_user_" + randToken(8), IsBot: true}
		if len(description) > 0 && description[0] != "" {
			botUser.Bio = description[0]
		}

		var lastErr error
		for i := 0; i < 5; i++ {
			if err := tx.Create(botUser).Error; err != nil {
				lastErr = err
				se := err.Error()
				if strings.Contains(se, "Duplicate entry") && (strings.Contains(se, "token") || strings.Contains(se, "idx_users_token")) {
					if strings.HasPrefix(botUser.Token, "legacy_") {
						botUser.Token = "legacy_" + randToken(8)
					} else if strings.HasPrefix(botUser.Token, "bot_user_") {
						botUser.Token = "bot_user_" + randToken(8)
					} else {
						botUser.Token = randToken(32)
					}
					continue
				}
				break
			}
			lastErr = nil
			break
		}
		if lastErr != nil {
			botUser.Name = botName + "#" + randToken(6)
			for i := 0; i < 5; i++ {
				if err := tx.Create(botUser).Error; err != nil {
					lastErr = err
					se := err.Error()
					if strings.Contains(se, "Duplicate entry") && (strings.Contains(se, "token") || strings.Contains(se, "idx_users_token")) {
						botUser.Token = "bot_user_" + randToken(8)
						continue
					}
					break
				}
				lastErr = nil
				break
			}
		}
		if lastErr != nil {
			return lastErr
		}

		var rb *models.Robot
		maxTokenAttempts := 5
		for i := 0; i < maxTokenAttempts; i++ {
			token := randToken(32)
			if strings.TrimSpace(token) == "" {
				token = randToken(32)
			}
			rb = &models.Robot{OwnerID: ownerID, BotUserID: botUser.ID, Token: token, CreatedAt: time.Now()}
			if err := tx.Create(rb).Error; err != nil {
				se := err.Error()
				if strings.Contains(se, "Duplicate entry") && (strings.Contains(se, "token") || strings.Contains(se, "idx_robots_token")) {
					continue
				}
				return err
			}
			createdRobot = rb
			break
		}
		if createdRobot == nil {
			return fmt.Errorf("创建机器人失败，请稍后重试")
		}
		createdRobot.BotUser = botUser
		return nil
	})
	if err != nil {
		return nil, err
	}
	return createdRobot, nil
}

func (s *Service) ListRobotsByOwner(ownerID uint) ([]models.Robot, error) {
	return s.Repo.ListRobotsByOwnerID(ownerID)
}

// EnsureRobotUser 确保机器人对应的用户存在
func (s *Service) EnsureRobotUser(rb *models.Robot) error {
	if rb == nil {
		return &Err{Code: 400, Msg: "机器人不能为空"}
	}
	if rb.BotUser != nil && rb.BotUser.ID != 0 {
		return nil
	}
	if rb.BotUserID != 0 {
		if u, err := s.Repo.GetUserByID(rb.BotUserID); err == nil && u != nil && u.ID != 0 {
			rb.BotUser = u
			return nil
		}
	}
	// BotUser 缺失时用 robot ID 作为 fallback 名称
	botName := "Bot#" + strconv.Itoa(int(rb.ID))
	placeholderEmail := "bot_" + randToken(8) + "@bot.local"
	u := &models.User{Name: botName, Email: placeholderEmail, Token: "bot_user_" + randToken(8), IsBot: true}
	if err := s.createUserWithUniqueToken(u); err != nil {
		return err
	}
	rb.BotUserID = u.ID
	rb.BotUser = u
	return s.Repo.UpdateRobot(rb)
}

// GetBotByToken 查找机器人记录（含 bot user）
func (s *Service) GetBotByToken(token string) (*models.Robot, error) {
	return s.Repo.GetRobotByToken(token)
}

// HotRobots 返回热门机器人列表
func (s *Service) HotRobots(limit int) ([]models.Robot, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	if s.RedisClient != nil {
		cacheKey := fmt.Sprintf("hot_robots:%d", limit)
		var cached []models.Robot
		if err := s.getCache(cacheKey, &cached); err == nil && len(cached) > 0 {
			return cached, nil
		}
		robots, err := s.Repo.ListTopRobots(limit)
		if err == nil && len(robots) > 0 {
			_ = s.setCache(cacheKey, robots, 5*time.Minute)
		}
		return robots, err
	}
	return s.Repo.ListTopRobots(limit)
}

// SearchRobots 机器人名称模糊搜索
func (s *Service) SearchRobots(q string, limit int) ([]models.Robot, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, ErrBadRequest
	}
	if id, err := strconv.ParseUint(q, 10, 32); err == nil {
		robot, err := s.Repo.GetRobotByID(uint(id))
		if err == nil && robot != nil && !robot.IsPrivate {
			return []models.Robot{*robot}, nil
		}
	}
	return s.Repo.SearchRobotsByName(q, limit)
}

// GetRobot retrieves a robot by ID
func (s *Service) GetRobot(id uint) (*models.Robot, error) {
	return s.Repo.GetRobotByID(id)
}

// UpdateRobot updates robot information
func (s *Service) UpdateRobot(robot *models.Robot) error {
	return s.Repo.UpdateRobot(robot)
}

// DeleteRobot deletes a robot and cleans up ALL associated data
func (s *Service) DeleteRobot(id uint) error {
	robot, err := s.Repo.GetRobotByID(id)
	if err != nil {
		return err
	}

	// 1. 将机器人从所有服务器中移除
	if robot.BotUserID > 0 {
		_ = s.Repo.RemoveAllGuildMembersByUserID(robot.BotUserID)
	}

	// 2. 删除排行榜记录
	_ = s.Repo.DeleteRankingsByRobotID(id)

	// 3. 删除 Webhook 调用日志
	_ = s.Repo.DeleteWebhookLogsByRobotID(id)

	// 4. 清理 Redis 中的配额 key
	if s.RedisClient != nil && robot.BotUserID > 0 {
		prefix := "bot:upload:" // daily/monthly keys
		_ = s.deleteCacheByPrefix(prefix + "daily:" + strconv.Itoa(int(robot.BotUserID)) + ":")
		_ = s.deleteCacheByPrefix(prefix + "monthly:" + strconv.Itoa(int(robot.BotUserID)) + ":")
	}

	// 5. 删除 Robot 记录
	if err := s.Repo.DeleteRobot(id); err != nil {
		return err
	}

	// 6. 删除关联的 BotUser
	if robot.BotUserID > 0 {
		_ = s.Repo.DeleteUser(robot.BotUserID)
	}

	return nil
}

// ResetRobotToken generates a new token for the robot
func (s *Service) ResetRobotToken(id uint) (string, error) {
	robot, err := s.Repo.GetRobotByID(id)
	if err != nil {
		return "", err
	}

	// Generate new token
	newToken := generateSecureToken(32)
	robot.Token = newToken

	if err := s.Repo.UpdateRobot(robot); err != nil {
		return "", err
	}

	return newToken, nil
}

// UpdateRobotWebhook updates the webhook URL for a robot
func (s *Service) UpdateRobotWebhook(id uint, webhookURL string) error {
	robot, err := s.Repo.GetRobotByID(id)
	if err != nil {
		return err
	}

	// Update webhook URL in robot metadata (需要在 Robot model 中添加 WebhookURL 字段)
	// 这里假设已经有该字段
	robot.WebhookURL = webhookURL
	return s.Repo.UpdateRobot(robot)
}

// GetRobotStats returns comprehensive statistics for a robot, including multi-dimensional ranking data
func (s *Service) GetRobotStats(id uint) (map[string]any, error) {
	robot, err := s.Repo.GetRobotByID(id)
	if err != nil {
		return nil, err
	}

	// ---------- 基础总量统计 ----------
	messageCount, _ := s.Repo.CountMessagesByUserID(robot.BotUserID)
	guildCount, _ := s.Repo.CountGuildsByUserID(robot.BotUserID)

	// ---------- 真实 lastActiveAt ----------
	var lastActiveAt any
	if t, err := s.Repo.GetLastMessageTime(robot.BotUserID); err == nil && t != nil {
		lastActiveAt = t
	} else {
		lastActiveAt = robot.UpdatedAt
	}

	// ---------- 各周期排行榜数据（实时计算） ----------
	now := time.Now()
	daily, weekly, monthly := repository.GetCurrentPeriodKeys(now)

	periodMap := make(map[string]map[string]any)
	for _, pt := range []struct{ key, pkey string }{
		{"daily", daily}, {"weekly", weekly}, {"monthly", monthly},
	} {
		rank, data := s.GetSingleRobotRanking(robot.ID, pt.key, pt.pkey)
		if data != nil {
			periodMap[pt.key] = map[string]any{
				"periodKey":        pt.pkey,
				"rank":             rank,
				"heatScore":        data.HeatScore,
				"rawScore":         data.RawScore,
				"guildCount":       data.GuildCount,
				"guildGrowth":      data.GuildGrowth,
				"messageCount":     data.MessageCount,
				"interactionCount": data.InteractionCount,
				"decayApplied":     data.DecayApplied,
			}
		} else {
			periodMap[pt.key] = map[string]any{
				"periodKey":        pt.pkey,
				"rank":             0,
				"heatScore":        0,
				"rawScore":         0,
				"guildCount":       0,
				"guildGrowth":      0,
				"messageCount":     0,
				"interactionCount": 0,
				"decayApplied":     false,
			}
		}
	}

	stats := map[string]any{
		"robotId":      robot.ID,
		"category":     robot.Category,
		"isPrivate":    robot.IsPrivate,
		"createdAt":    robot.CreatedAt,
		"lastActiveAt": lastActiveAt,
		"total": map[string]any{
			"messageCount": messageCount,
			"guildCount":   guildCount,
		},
		"rankings": periodMap,
	}

	return stats, nil
}

// SendWebhookTest sends a test webhook request and records the call log
func (s *Service) SendWebhookTest(robotID uint, payload map[string]any) (map[string]any, error) {
	robot, err := s.Repo.GetRobotByID(robotID)
	if err != nil {
		return nil, err
	}

	if robot.WebhookURL == "" {
		return nil, errors.New("回调地址未配置")
	}

	// Prepare webhook payload
	webhookPayload := map[string]any{
		"type":      "TEST",
		"robotId":   robot.ID,
		"timestamp": time.Now().Unix(),
		"data":      payload,
	}

	jsonData, err := json.Marshal(webhookPayload)
	if err != nil {
		return nil, err
	}

	// Send HTTP POST request & record log
	start := time.Now()
	client := &http.Client{Timeout: 10 * time.Second}
	resp, reqErr := client.Post(robot.WebhookURL, "application/json", bytes.NewBuffer(jsonData))
	latency := time.Since(start).Milliseconds()

	log := &models.WebhookLog{
		RobotID:   robot.ID,
		EventType: "TEST",
		URL:       robot.WebhookURL,
		LatencyMs: latency,
		CreatedAt: time.Now(),
	}

	if reqErr != nil {
		log.Success = false
		log.Error = reqErr.Error()
		_ = s.Repo.CreateWebhookLog(log)
		return map[string]any{
			"success":   false,
			"error":     reqErr.Error(),
			"latencyMs": latency,
		}, nil
	}
	defer resp.Body.Close()

	log.StatusCode = resp.StatusCode
	log.Success = resp.StatusCode >= 200 && resp.StatusCode < 300
	_ = s.Repo.CreateWebhookLog(log)

	return map[string]any{
		"success":    log.Success,
		"statusCode": resp.StatusCode,
		"latencyMs":  latency,
		"sent":       true,
	}, nil
}

// ListWebhookLogs 查询指定机器人的 Webhook 调用日志
func (s *Service) ListWebhookLogs(robotID uint, limit, offset int) ([]models.WebhookLog, int64, error) {
	return s.Repo.ListWebhookLogs(robotID, limit, offset)
}

// ListRobotsWithOverview 列出开发者的机器人并附带概览数据
func (s *Service) ListRobotsWithOverview(ownerID uint) ([]map[string]any, error) {
	list, err := s.Repo.ListRobotsByOwnerID(ownerID)
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, len(list))
	for i, rb := range list {
		guildCount, _ := s.Repo.CountGuildsByUserID(rb.BotUserID)
		var lastActiveAt any
		if t, err := s.Repo.GetLastMessageTime(rb.BotUserID); err == nil && t != nil {
			lastActiveAt = t
		} else {
			lastActiveAt = rb.UpdatedAt
		}
		result[i] = map[string]any{
			"id":           rb.ID,
			"ownerId":      rb.OwnerID,
			"botUserId":    rb.BotUserID,
			"isPrivate":    rb.IsPrivate,
			"category":     rb.Category,
			"webhookUrl":   rb.WebhookURL,
			"createdAt":    rb.CreatedAt,
			"updatedAt":    rb.UpdatedAt,
			"botUser":      rb.BotUser,
			"guildCount":   guildCount,
			"lastActiveAt": lastActiveAt,
		}
	}
	return result, nil
}

// GetMessagesWithPagination retrieves messages with pagination support
func (s *Service) GetMessagesWithPagination(channelID uint, limit int, before uint) ([]models.Message, error) {
	return s.Repo.GetMessagesWithPagination(channelID, limit, before)
}

// generateSecureToken generates a cryptographically secure random token
func generateSecureToken(length int) string {
	b := make([]byte, length)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
