package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"bubble/src/db/models"
	"bubble/src/logger"

	"github.com/gin-gonic/gin"
	"github.com/livekit/protocol/livekit"
)

// LiveKit Webhook 事件处理
// 文档: https://docs.livekit.io/realtime/server/webhooks/

// handleLiveKitWebhook 处理 LiveKit Webhook 事件
func (h *HTTP) handleLiveKitWebhook(c *gin.Context) {
	// 审计日志：记录所有 Webhook 请求
	clientIP := c.ClientIP()
	logger.Infof("[LiveKit Webhook] Received request from IP: %s, User-Agent: %s",
		clientIP, c.Request.UserAgent())

	// 验证 LiveKit 是否配置
	if h.Svc.LiveKit == nil {
		logger.Error("[LiveKit Webhook] ERROR: LiveKit not configured")
		c.JSON(503, gin.H{"error": "音视频服务未配置"})
		return
	}

	// 读取请求体
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.Errorf("[LiveKit Webhook] ERROR: Failed to read body from %s: %v", clientIP, err)
		c.JSON(400, gin.H{"error": "无效的请求"})
		return
	}

	// 解析 Webhook 事件
	// 注意：LiveKit 可能发送字符串类型的 participant.state，但 Go 结构体期望枚举类型
	// 使用两步解析：先解析为 map，然后手动处理 state 字段
	var rawEvent map[string]interface{}
	if err := json.Unmarshal(body, &rawEvent); err != nil {
		logger.Errorf("[LiveKit Webhook] ERROR: Failed to parse event JSON from %s: %v", clientIP, err)
		c.JSON(400, gin.H{"error": "无效的事件"})
		return
	}

	// 修复 participant.state 字段：如果是字符串，则删除（让 protobuf 使用默认值）
	if participant, ok := rawEvent["participant"].(map[string]interface{}); ok {
		if state, ok := participant["state"].(string); ok {
			logger.Debugf("[LiveKit Webhook] Converting participant.state from string '%s' to enum", state)
			delete(participant, "state") // 删除字符串类型的 state，让 protobuf 使用默认值
		}
	}

	// 重新序列化为 JSON，然后解析为 protobuf 结构
	fixedBody, err := json.Marshal(rawEvent)
	if err != nil {
		logger.Errorf("[LiveKit Webhook] ERROR: Failed to re-marshal fixed event: %v", err)
		c.JSON(400, gin.H{"error": "无效的事件"})
		return
	}

	event := &livekit.WebhookEvent{}
	if err := json.Unmarshal(fixedBody, event); err != nil {
		logger.Errorf("[LiveKit Webhook] ERROR: Failed to parse event from %s: %v", clientIP, err)
		logger.Errorf("[LiveKit Webhook] Event JSON (first 500 chars): %s", string(body[:min(500, len(body))]))
		c.JSON(400, gin.H{"error": "无效的事件"})
		return
	}

	// 验证 Webhook 签名（LiveKit 使用 JWT token 签名）
	// LiveKit webhook 使用 Authorization header 中的 JWT token 进行签名验证
	// 注意：当前实现为宽松模式，仅记录警告，生产环境应启用严格验证
	authHeader := c.Request.Header.Get("Authorization")
	if authHeader != "" {
		// LiveKit 发送的是 JWT token，不是简单的 Bearer token
		// 这里我们暂时只记录，不进行严格验证（开发环境）
		logger.Infof("[LiveKit Webhook] Received Authorization header (JWT token)")

		// 如果需要严格验证，可以使用 LiveKit SDK 的 webhook 验证功能
		// 但需要先安装依赖：go get github.com/livekit/protocol/webhook
		// 然后使用 webhook.ReceiveWebhookEvent() 进行验证
	} else {
		logger.Warnf("[LiveKit Webhook] No Authorization header from %s", clientIP)
		logger.Infof("[LiveKit Webhook]   Event Type: %s", event.Event)
		logger.Warn("[LiveKit Webhook]   Continuing with unverified event (development mode)")
	}

	// 记录事件类型（用于监控）
	logger.Infof("[LiveKit Webhook] Processing event: %s (Room: %s)", event.Event,
		func() string {
			if event.Room != nil {
				return event.Room.Name
			}
			return "N/A"
		}())

	// 处理不同类型的事件
	switch event.Event {
	case "participant_joined":
		h.handleParticipantJoined(event)
	case "participant_left":
		h.handleParticipantLeft(event)
	case "participant_connection_aborted":
		h.handleParticipantConnectionAborted(event)
	case "track_published":
		h.handleTrackPublished(event)
	case "track_unpublished":
		h.handleTrackUnpublished(event)
	case "room_started":
		h.handleRoomStarted(event)
	case "room_finished":
		h.handleRoomFinished(event)
	default:
		logger.Infof("[LiveKit Webhook] Unhandled event type: %s", event.Event)
	}

	c.JSON(200, gin.H{"status": "成功"})
}

// handleParticipantJoined 处理参与者加入事件
func (h *HTTP) handleParticipantJoined(event *livekit.WebhookEvent) {
	participant := event.Participant
	room := event.Room

	if participant == nil || room == nil {
		return
	}

	logger.Infof("[LiveKit Webhook] Participant joined: room=%s, identity=%s, name=%s",
		room.Name, participant.Identity, participant.Name)

	// 支持频道与私聊两种房间名
	var channelID uint
	var threadID uint
	var isDM bool
	if _, err := parseChannelID(room.Name, &channelID); err != nil {
		// 尝试解析私聊房间 (dm_<id>)
		if _, err2 := fmt.Sscanf(room.Name, "dm_%d", &threadID); err2 == nil {
			isDM = true
		} else {
			logger.Warnf("[LiveKit Webhook] Invalid room name format: %s", room.Name)
			return
		}
	}

	// 解析用户 ID (identity 格式: user_<id>)
	var userID uint
	if _, err := parseUserID(participant.Identity, &userID); err != nil {
		logger.Warnf("[LiveKit Webhook] Invalid identity format: %s", participant.Identity)
		return
	}

	// 记录到数据库（仅频道房间有记录；DM房间不落库）
	var livekitRoom *models.LiveKitRoom
	if !isDM {
		lr, err := h.Svc.Repo.GetLiveKitRoomByChannelID(channelID)
		if err != nil || lr == nil {
			logger.Infof("[LiveKit Webhook] Room record not found for channel %d", channelID)
			return
		}
		livekitRoom = lr
	}

	// 幂等性检查: 避免重复创建参与者记录
	var existingParticipant *models.LiveKitParticipant
	if livekitRoom != nil {
		ep, err := h.Svc.Repo.GetLiveKitParticipantByIdentity(participant.Identity, livekitRoom.ID)
		if err == nil {
			existingParticipant = ep
		}
	}
	if existingParticipant != nil {
		if existingParticipant.LeftAt == nil {
			// 参与者已存在且未离开，可能是重复的 webhook 事件
			logger.Infof("[LiveKit Webhook] Participant %s already exists in room (not left), skipping duplicate join event", participant.Identity)
			return
		} else {
			// 参与者之前离开过，更新记录为重新加入
			logger.Infof("[LiveKit Webhook] Participant %s rejoining, updating record", participant.Identity)
			// 更新现有记录：清除离开时间，更新 SID 和加入时间
			_ = h.Svc.Repo.UpdateLiveKitParticipant(existingParticipant.ID, map[string]interface{}{
				"participant_sid": participant.Sid,
				"joined_at":       time.Now(),
				"left_at":         nil,
			})
			// 继续广播 joined 事件
		}
	}

	// 只有在没有现有记录时才创建新记录
	if !isDM && existingParticipant == nil && livekitRoom != nil {
		participantRecord := &models.LiveKitParticipant{
			RoomID:              livekitRoom.ID,
			UserID:              userID,
			ParticipantSID:      participant.Sid,
			ParticipantIdentity: participant.Identity,
			JoinedAt:            time.Now(),
		}

		if err := h.Svc.Repo.CreateLiveKitParticipant(participantRecord); err != nil {
			logger.Errorf("[LiveKit Webhook] Failed to create participant record: %v", err)
			return // 创建失败则不广播
		}
	}

	// 通过 Gateway 广播事件
	if h.Gw != nil {
		state := map[string]interface{}{
			"isMuted":  !participant.Permission.CanPublish,
			"hasVideo": false,
		}
		if isDM {
			h.Gw.BroadcastDmVoiceState(threadID, userID, participant.Name, "joined", state)
		} else {
			h.Gw.BroadcastChannelVoiceState(channelID, userID, participant.Name, "joined", state)
		}
	}
}

// handleParticipantLeft 处理参与者离开事件
func (h *HTTP) handleParticipantLeft(event *livekit.WebhookEvent) {
	participant := event.Participant
	room := event.Room

	if participant == nil || room == nil {
		return
	}

	logger.Infof("[LiveKit Webhook] Participant left: room=%s, identity=%s, name=%s",
		room.Name, participant.Identity, participant.Name)

	// 解析房间类型
	var channelID, userID uint
	var threadID uint
	var isDM bool
	if _, err := parseChannelID(room.Name, &channelID); err != nil {
		if _, err2 := fmt.Sscanf(room.Name, "dm_%d", &threadID); err2 == nil {
			isDM = true
		} else {
			return
		}
	}
	if _, err := parseUserID(participant.Identity, &userID); err != nil {
		return
	}

	// 更新数据库记录
	if !isDM {
		livekitRoom, err := h.Svc.Repo.GetLiveKitRoomByChannelID(channelID)
		if err == nil && livekitRoom != nil {
			// 直接通过 identity 查找参与者记录（高效）
			p, err := h.Svc.Repo.GetLiveKitParticipantByIdentity(participant.Identity, livekitRoom.ID)
			if err == nil && p != nil && p.LeftAt == nil {
				now := time.Now()
				duration := int(now.Sub(p.JoinedAt).Seconds())
				_ = h.Svc.Repo.UpdateLiveKitParticipant(p.ID, map[string]interface{}{
					"left_at":          now,
					"duration_seconds": duration,
				})
			}
		}
	}

	// 通过 Gateway 广播事件
	if h.Gw != nil {
		if isDM {
			h.Gw.BroadcastDmVoiceState(threadID, userID, participant.Name, "left", map[string]interface{}{})
		} else {
			h.Gw.BroadcastChannelVoiceState(channelID, userID, participant.Name, "left", map[string]interface{}{})
		}
	}
}

// handleParticipantConnectionAborted 处理参与者连接中止事件（网络/权限等导致未成功建立连接）
func (h *HTTP) handleParticipantConnectionAborted(event *livekit.WebhookEvent) {
	participant := event.Participant
	room := event.Room

	if participant == nil || room == nil {
		return
	}

	logger.Infof("[LiveKit Webhook] Participant connection aborted: room=%s, identity=%s, name=%s",
		room.Name, participant.Identity, participant.Name)

	// 解析 ID
	var channelID, userID uint
	if _, err := parseChannelID(room.Name, &channelID); err != nil {
		return
	}
	if _, err := parseUserID(participant.Identity, &userID); err != nil {
		return
	}

	// 将状态视为一次未建立成功的加入，若存在参与者记录则标记离开并给出极短时长
	livekitRoom, err := h.Svc.Repo.GetLiveKitRoomByChannelID(channelID)
	if err == nil && livekitRoom != nil {
		p, err := h.Svc.Repo.GetLiveKitParticipantByIdentity(participant.Identity, livekitRoom.ID)
		if err == nil && p != nil && p.LeftAt == nil {
			now := time.Now()
			duration := int(now.Sub(p.JoinedAt).Seconds())
			_ = h.Svc.Repo.UpdateLiveKitParticipant(p.ID, map[string]interface{}{
				"left_at":          now,
				"duration_seconds": duration,
			})
		}
	}

	// 广播 aborted 事件（前端可用于提示连接失败）
	if h.Gw != nil {
		h.Gw.BroadcastChannelVoiceState(
			channelID,
			userID,
			participant.Name,
			"aborted",
			map[string]interface{}{},
		)
	}
}

// handleTrackPublished 处理媒体轨道发布事件
func (h *HTTP) handleTrackPublished(event *livekit.WebhookEvent) {
	participant := event.Participant
	room := event.Room
	track := event.Track

	if participant == nil || room == nil || track == nil {
		return
	}

	logger.Infof("[LiveKit Webhook] Track published: room=%s, identity=%s, track=%s, type=%s",
		room.Name, participant.Identity, track.Name, track.Type)

	// 解析房间类型
	var channelID, userID uint
	var threadID uint
	var isDM bool
	if _, err := parseChannelID(room.Name, &channelID); err != nil {
		if _, err2 := fmt.Sscanf(room.Name, "dm_%d", &threadID); err2 == nil {
			isDM = true
		} else {
			return
		}
	}
	if _, err := parseUserID(participant.Identity, &userID); err != nil {
		return
	}

	// 根据轨道类型确定状态
	action := "updated"
	state := make(map[string]interface{})

	switch track.Type {
	case livekit.TrackType_VIDEO:
		action = "video_on"
		state["hasVideo"] = true
	case livekit.TrackType_AUDIO:
		action = "unmuted"
		state["isMuted"] = false
	}

	// 广播状态更新
	if h.Gw != nil && action != "updated" {
		if isDM {
			h.Gw.BroadcastDmVoiceState(threadID, userID, participant.Name, action, state)
		} else {
			h.Gw.BroadcastChannelVoiceState(channelID, userID, participant.Name, action, state)
		}
	}
}

// handleTrackUnpublished 处理媒体轨道取消发布事件
func (h *HTTP) handleTrackUnpublished(event *livekit.WebhookEvent) {
	participant := event.Participant
	room := event.Room
	track := event.Track

	if participant == nil || room == nil || track == nil {
		return
	}

	logger.Infof("[LiveKit Webhook] Track unpublished: room=%s, identity=%s, track=%s, type=%s",
		room.Name, participant.Identity, track.Name, track.Type)

	// 解析房间类型
	var channelID, userID uint
	var threadID uint
	var isDM bool
	if _, err := parseChannelID(room.Name, &channelID); err != nil {
		if _, err2 := fmt.Sscanf(room.Name, "dm_%d", &threadID); err2 == nil {
			isDM = true
		} else {
			return
		}
	}
	if _, err := parseUserID(participant.Identity, &userID); err != nil {
		return
	}

	// 根据轨道类型确定状态
	action := "updated"
	state := make(map[string]interface{})

	switch track.Type {
	case livekit.TrackType_VIDEO:
		action = "video_off"
		state["hasVideo"] = false
	case livekit.TrackType_AUDIO:
		action = "muted"
		state["isMuted"] = true
	}

	// 广播状态更新
	if h.Gw != nil && action != "updated" {
		if isDM {
			h.Gw.BroadcastDmVoiceState(threadID, userID, participant.Name, action, state)
		} else {
			h.Gw.BroadcastChannelVoiceState(channelID, userID, participant.Name, action, state)
		}
	}
}

// handleRoomStarted 处理房间启动事件
func (h *HTTP) handleRoomStarted(event *livekit.WebhookEvent) {
	room := event.Room
	if room == nil {
		return
	}

	logger.Infof("[LiveKit Webhook] Room started: name=%s, sid=%s", room.Name, room.Sid)

	// 解析频道 ID
	var channelID uint
	if _, err := parseChannelID(room.Name, &channelID); err != nil {
		return
	}

	// 更新数据库记录
	livekitRoom, err := h.Svc.Repo.GetLiveKitRoomByChannelID(channelID)
	if err == nil && livekitRoom != nil {
		_ = h.Svc.Repo.UpdateLiveKitRoom(livekitRoom.ID, map[string]interface{}{
			"room_sid":  room.Sid,
			"is_active": true,
		})
	}
}

// handleRoomFinished 处理房间结束事件
func (h *HTTP) handleRoomFinished(event *livekit.WebhookEvent) {
	room := event.Room
	if room == nil {
		return
	}

	logger.Infof("[LiveKit Webhook] Room finished: name=%s, sid=%s", room.Name, room.Sid)

	// 解析频道 ID
	var channelID uint
	if _, err := parseChannelID(room.Name, &channelID); err != nil {
		return
	}

	// 更新数据库记录
	livekitRoom, err := h.Svc.Repo.GetLiveKitRoomByChannelID(channelID)
	if err == nil && livekitRoom != nil {
		now := time.Now()
		_ = h.Svc.Repo.UpdateLiveKitRoom(livekitRoom.ID, map[string]interface{}{
			"is_active": false,
			"closed_at": now,
		})

		// 更新所有未离开的参与者记录
		participants, err := h.Svc.Repo.GetLiveKitParticipantsByRoom(livekitRoom.ID)
		if err == nil {
			for _, p := range participants {
				if p.LeftAt == nil {
					duration := int(now.Sub(p.JoinedAt).Seconds())
					_ = h.Svc.Repo.UpdateLiveKitParticipant(p.ID, map[string]interface{}{
						"left_at":          now,
						"duration_seconds": duration,
					})
				}
			}
		}
	}
}

// 辅助函数：解析频道 ID (格式: channel_123)
func parseChannelID(roomName string, channelID *uint) (int, error) {
	var id uint
	n, err := fmt.Sscanf(roomName, "channel_%d", &id)
	if err == nil && n > 0 {
		*channelID = id
		return n, nil
	}
	return 0, fmt.Errorf("invalid room name format: %s", roomName)
}

// 辅助函数：解析用户 ID (格式: user_123)
func parseUserID(identity string, userID *uint) (int, error) {
	var id uint
	n, err := fmt.Sscanf(identity, "user_%d", &id)
	if err == nil && n > 0 {
		*userID = id
		return n, nil
	}
	return 0, fmt.Errorf("invalid identity format: %s", identity)
}

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
