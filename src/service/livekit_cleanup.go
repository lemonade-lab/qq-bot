package service

import (
	"context"
	"time"

	"bubble/src/db/models"
	"bubble/src/logger"
)

// StartLiveKitCleanupWorker 启动 LiveKit 清理任务（定期清理孤儿记录）
func (s *Service) StartLiveKitCleanupWorker(ctx context.Context) {
	if s.LiveKit == nil {
		logger.Infof("[LiveKit Cleanup] Skipped: LiveKit not configured")
		return
	}

	logger.Infof("[LiveKit Cleanup] Worker started")
	ticker := time.NewTicker(5 * time.Minute) // 每5分钟运行一次
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Infof("[LiveKit Cleanup] Worker stopped")
			return
		case <-ticker.C:
			s.cleanupStaleParticipants()
		}
	}
}

// cleanupStaleParticipants 清理陈旧的参与者记录
// 原理：对比 LiveKit 服务器的实际参与者列表和数据库中的活跃记录，
// 如果数据库中记录为活跃但服务器上已不存在，则标记为已离开
func (s *Service) cleanupStaleParticipants() {
	logger.Infof("[LiveKit Cleanup] Starting stale participant cleanup")

	// 获取所有活跃的房间
	rooms, err := s.Repo.ListActiveLiveKitRooms()
	if err != nil {
		logger.Errorf("[LiveKit Cleanup] Failed to list active rooms: %v", err)
		return
	}

	cleanedCount := 0
	for _, room := range rooms {
		// 使用函数作用域避免 defer 累积
		func(room models.LiveKitRoom) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			serverParticipants, err := s.LiveKit.ListParticipants(ctx, room.RoomName)
			if err != nil {
				// 房间可能已关闭但数据库未更新
				logger.Infof("[LiveKit Cleanup] Room %s not found on server, marking as closed", room.RoomName)
				now := time.Now()
				_ = s.Repo.UpdateLiveKitRoom(room.ID, map[string]interface{}{
					"is_active": false,
					"closed_at": now,
				})

				// 关闭所有未离开的参与者
				participants, _ := s.Repo.GetLiveKitParticipantsByRoom(room.ID)
				for _, p := range participants {
					if p.LeftAt == nil {
						duration := int(now.Sub(p.JoinedAt).Seconds())
						_ = s.Repo.UpdateLiveKitParticipant(p.ID, map[string]interface{}{
							"left_at":          now,
							"duration_seconds": duration,
						})
						cleanedCount++
					}
				}
				return
			}

			// 构建服务器端参与者 identity 集合
			serverIdentities := make(map[string]bool)
			for _, sp := range serverParticipants {
				serverIdentities[sp.Identity] = true
			}

			// 获取数据库中的活跃参与者
			dbParticipants, err := s.Repo.GetLiveKitParticipantsByRoom(room.ID)
			if err != nil {
				return
			}

			// 检查数据库中的参与者是否仍在服务器上
			now := time.Now()
			for _, p := range dbParticipants {
				if p.LeftAt == nil && !serverIdentities[p.ParticipantIdentity] {
					// 数据库认为活跃，但服务器上不存在 → 孤儿记录
					duration := int(now.Sub(p.JoinedAt).Seconds())
					_ = s.Repo.UpdateLiveKitParticipant(p.ID, map[string]interface{}{
						"left_at":          now,
						"duration_seconds": duration,
					})
					logger.Infof("[LiveKit Cleanup] Cleaned stale participant: room=%s, identity=%s, duration=%ds",
						room.RoomName, p.ParticipantIdentity, duration)
					cleanedCount++
				}
			}
		}(room)
	}

	if cleanedCount > 0 {
		logger.Infof("[LiveKit Cleanup] Cleaned %d stale participant records", cleanedCount)
	} else {
		logger.Infof("[LiveKit Cleanup] No stale records found")
	}
}
