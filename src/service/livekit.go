package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"bubble/src/db/models"

	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
)

// LiveKitService 提供 LiveKit 相关功能
type LiveKitService struct {
	url       string
	apiKey    string
	apiSecret string
	client    *lksdk.RoomServiceClient
	mu        sync.Mutex      // 保护并发创建房间
	creating  map[string]bool // 正在创建的房间
}

// NewLiveKitService 创建 LiveKit 服务实例
func NewLiveKitService(url, apiKey, apiSecret string) *LiveKitService {
	return &LiveKitService{
		url:       url,
		apiKey:    apiKey,
		apiSecret: apiSecret,
		client:    lksdk.NewRoomServiceClient(url, apiKey, apiSecret),
		creating:  make(map[string]bool),
	}
}

// GenerateRoomToken 为用户生成加入房间的 Token
// userID: 用户 ID
// userName: 用户名称
// roomName: 房间名称 (通常是 channel_<channelID>)
// canPublish: 是否允许发布音视频流
// canSubscribe: 是否允许订阅其他人的流
func (s *LiveKitService) GenerateRoomToken(userID uint, userName, roomName string, canPublish, canSubscribe bool) (string, error) {
	at := auth.NewAccessToken(s.apiKey, s.apiSecret)

	grant := &auth.VideoGrant{
		RoomJoin:     true,
		Room:         roomName,
		CanPublish:   &canPublish,
		CanSubscribe: &canSubscribe,
	}

	at.AddGrant(grant).
		SetIdentity(fmt.Sprintf("user_%d", userID)).
		SetName(userName).
		SetValidFor(24 * time.Hour) // Token 有效期 24 小时

	token, err := at.ToJWT()
	if err != nil {
		return "", fmt.Errorf("生成令牌失败：%w", err)
	}

	return token, nil
}

// CreateRoom 创建 LiveKit 房间
func (s *LiveKitService) CreateRoom(ctx context.Context, roomName string, maxParticipants int) (*livekit.Room, error) {
	req := &livekit.CreateRoomRequest{
		Name:            roomName,
		EmptyTimeout:    300, // 5 分钟无人自动关闭
		MaxParticipants: uint32(maxParticipants),
	}

	room, err := s.client.CreateRoom(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("创建房间失败：%w", err)
	}

	return room, nil
}

// GetRoom 获取房间信息
func (s *LiveKitService) GetRoom(ctx context.Context, roomName string) (*livekit.Room, error) {
	req := &livekit.ListRoomsRequest{
		Names: []string{roomName},
	}

	resp, err := s.client.ListRooms(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("获取房间失败：%w", err)
	}

	if len(resp.Rooms) == 0 {
		return nil, fmt.Errorf("房间不存在")
	}

	return resp.Rooms[0], nil
}

// GetOrCreateRoom 获取或创建房间（并发安全）
func (s *LiveKitService) GetOrCreateRoom(ctx context.Context, channel *models.Channel) (*livekit.Room, error) {
	roomName := fmt.Sprintf("channel_%d", channel.ID)

	// 尝试获取已存在的房间
	room, err := s.GetRoom(ctx, roomName)
	if err == nil {
		return room, nil
	}

	// 使用锁防止并发创建同一个房间
	s.mu.Lock()

	// 检查是否正在创建中
	if s.creating[roomName] {
		s.mu.Unlock()
		// 等待一小段时间后重试获取
		time.Sleep(100 * time.Millisecond)
		return s.GetRoom(ctx, roomName)
	}

	// 标记为正在创建
	s.creating[roomName] = true
	s.mu.Unlock()

	// 创建房间（释放锁后执行，避免阻塞）
	defer func() {
		s.mu.Lock()
		delete(s.creating, roomName)
		s.mu.Unlock()
	}()

	// 再次检查（可能在等待锁期间已被创建）
	room, err = s.GetRoom(ctx, roomName)
	if err == nil {
		return room, nil
	}

	// 确实不存在，创建新房间
	return s.CreateRoom(ctx, roomName, 50) // 默认最多 50 人
}

// DeleteRoom 删除房间
func (s *LiveKitService) DeleteRoom(ctx context.Context, roomName string) error {
	req := &livekit.DeleteRoomRequest{
		Room: roomName,
	}

	_, err := s.client.DeleteRoom(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to delete room: %w", err)
	}

	return nil
}

// ListParticipants 列出房间中的参与者
func (s *LiveKitService) ListParticipants(ctx context.Context, roomName string) ([]*livekit.ParticipantInfo, error) {
	req := &livekit.ListParticipantsRequest{
		Room: roomName,
	}

	resp, err := s.client.ListParticipants(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to list participants: %w", err)
	}

	return resp.Participants, nil
}

// RemoveParticipant 将用户从房间中移除
func (s *LiveKitService) RemoveParticipant(ctx context.Context, roomName, participantIdentity string) error {
	req := &livekit.RoomParticipantIdentity{
		Room:     roomName,
		Identity: participantIdentity,
	}

	_, err := s.client.RemoveParticipant(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to remove participant: %w", err)
	}

	return nil
}

// MuteParticipant 将用户静音
func (s *LiveKitService) MuteParticipant(ctx context.Context, roomName, participantIdentity string, trackSid string, muted bool) error {
	req := &livekit.MuteRoomTrackRequest{
		Room:     roomName,
		Identity: participantIdentity,
		TrackSid: trackSid,
		Muted:    muted,
	}

	_, err := s.client.MutePublishedTrack(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to mute participant: %w", err)
	}

	return nil
}

// GetRoomStats 获取房间统计信息
type RoomStats struct {
	RoomName         string
	NumParticipants  int
	CreatedAt        time.Time
	ParticipantNames []string
}

func (s *LiveKitService) GetRoomStats(ctx context.Context, roomName string) (*RoomStats, error) {
	room, err := s.GetRoom(ctx, roomName)
	if err != nil {
		return nil, err
	}

	participants, err := s.ListParticipants(ctx, roomName)
	if err != nil {
		return nil, err
	}

	stats := &RoomStats{
		RoomName:         room.Name,
		NumParticipants:  int(room.NumParticipants),
		CreatedAt:        time.Unix(room.CreationTime, 0),
		ParticipantNames: make([]string, 0, len(participants)),
	}

	for _, p := range participants {
		stats.ParticipantNames = append(stats.ParticipantNames, p.Name)
	}

	return stats, nil
}

// IsHealthy 检查 LiveKit 服务是否健康
func (s *LiveKitService) IsHealthy(ctx context.Context) bool {
	// 尝试列出房间，如果成功说明服务健康
	_, err := s.client.ListRooms(ctx, &livekit.ListRoomsRequest{})
	return err == nil
}

// GetURL 获取 LiveKit 服务 URL
func (s *LiveKitService) GetURL() string {
	return s.url
}

// GetAPIKey 获取 API Key
func (s *LiveKitService) GetAPIKey() string {
	return s.apiKey
}

// GetAPISecret 获取 API Secret
func (s *LiveKitService) GetAPISecret() string {
	return s.apiSecret
}

// Close 关闭 LiveKit 服务（清理资源）
func (s *LiveKitService) Close() error {
	// RoomServiceClient 基于 gRPC，不需要显式关闭
	// 清理 creating map
	s.mu.Lock()
	defer s.mu.Unlock()
	s.creating = make(map[string]bool)
	return nil
}
