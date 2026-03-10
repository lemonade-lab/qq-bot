package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"bubble/src/config"
	"bubble/src/db/models"
	"bubble/src/logger"
	"bubble/src/repository"

	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

// ──────────────────────────────────────────────
// Service — 核心结构体
// ──────────────────────────────────────────────

type Service struct {
	Repo           *repository.Repo
	Cfg            *config.Config
	Email          *EmailService
	SMS            *SMSService // 阿里云短信服务
	MinIO          *MinIOService
	LiveKit        *LiveKitService
	AudioConverter *AudioConverterService // 音频转换服务
	RedisClient    *redis.Client          // 用于缓存热门列表等数据
}

// ──────────────────────────────────────────────
// Permission bits (guild-level)
// ──────────────────────────────────────────────

const (
	PermViewChannel    uint64 = 1 << 0  // 查看频道
	PermSendMessages   uint64 = 1 << 1  // 发送消息
	PermManageGuild    uint64 = 1 << 2  // 管理服务器
	PermManageChannels uint64 = 1 << 3  // 管理频道
	PermManageRoles    uint64 = 1 << 4  // 管理角色
	PermManageMembers  uint64 = 1 << 5  // 管理成员
	PermKickMembers    uint64 = 1 << 6  // 踢出成员
	PermBanMembers     uint64 = 1 << 7  // 封禁成员
	PermManageMessages uint64 = 1 << 8  // 管理消息（删除他人消息）
	PermMuteMembers    uint64 = 1 << 9  // 禁言成员
	PermManageEmojis   uint64 = 1 << 10 // 管理表情
	PermManageFiles    uint64 = 1 << 11 // 文件管理
)

// PermAll 包含所有权限（Owner默认权限）
const PermAll = PermViewChannel | PermSendMessages | PermManageGuild | PermManageChannels |
	PermManageRoles | PermManageMembers | PermKickMembers | PermBanMembers |
	PermManageMessages | PermMuteMembers | PermManageEmojis | PermManageFiles

// PermissionInfo 权限信息
type PermissionInfo struct {
	Bit         uint64 `json:"bit"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// GetAllPermissions 返回所有可用的权限列表
func GetAllPermissions() []PermissionInfo {
	return []PermissionInfo{
		{PermViewChannel, "查看频道", "允许查看频道内容"},
		{PermSendMessages, "发送消息", "允许在频道中发送消息"},
		{PermManageGuild, "管理服务器", "允许修改服务器设置"},
		{PermManageChannels, "管理频道", "允许创建、编辑和删除频道"},
		{PermManageRoles, "管理角色", "允许创建、编辑和删除角色"},
		{PermManageMembers, "管理成员", "允许修改成员设置"},
		{PermKickMembers, "踢出成员", "允许踢出服务器成员"},
		{PermBanMembers, "封禁成员", "允许封禁服务器成员"},
		{PermManageMessages, "管理消息", "允许删除他人的消息"},
		{PermMuteMembers, "禁言成员", "允许禁言服务器成员"},
		{PermManageEmojis, "管理表情", "允许添加和删除服务器表情"},
		{PermManageFiles, "文件管理", "允许上传和删除服务器文件"},
	}
}

// ──────────────────────────────────────────────
// Constructor
// ──────────────────────────────────────────────

func New(repo *repository.Repo, cfg *config.Config) *Service {
	// 初始化腾讯云 SES 邮件服务
	emailSvc := NewEmailService(
		cfg.EmailEnabled,
		cfg.TencentSecretId,
		cfg.TencentSecretKey,
		cfg.TencentSESRegion,
		cfg.TencentTemplateID,
		cfg.EmailFrom,
		cfg.EmailFromName,
	)
	if cfg.EmailEnabled {
		logger.Infof("Email service initialized: provider=Tencent-SES region=%s template=%d", cfg.TencentSESRegion, cfg.TencentTemplateID)
	}
	// 初始化阿里云短信服务
	smsSvc := NewSMSService(
		cfg.SMSEnabled,
		cfg.AliyunAccessKeyID,
		cfg.AliyunAccessSecret,
		"速通互联验证码",
		"手机号认证",
	)
	// 初始化MinIO服务(如果失败，MinIO为nil，上传功能将不可用)
	minioSvc, err := NewMinIOService(cfg)
	if err != nil {
		logger.Warnf("MinIO service initialization failed: %v", err)
		logger.Warnf("MinIO config: endpoint=%s bucket=%s", cfg.MinIOEndpoint, cfg.MinIOBucket)
	} else {
		logger.Infof("MinIO service initialized successfully: endpoint=%s bucket=%s", cfg.MinIOEndpoint, cfg.MinIOBucket)
	}
	// 初始化LiveKit服务(如果配置不完整，LiveKit为nil，视频频道功能将不可用)
	var livekitSvc *LiveKitService
	if cfg.LiveKitURL != "" && cfg.LiveKitAPIKey != "" && cfg.LiveKitAPISecret != "" {
		// 验证URL格式
		if !strings.HasPrefix(cfg.LiveKitURL, "ws://") && !strings.HasPrefix(cfg.LiveKitURL, "wss://") &&
			!strings.HasPrefix(cfg.LiveKitURL, "http://") && !strings.HasPrefix(cfg.LiveKitURL, "https://") {
			logger.Warnf("Invalid LiveKit URL format: %s (should start with ws://, wss://, http://, or https://)", cfg.LiveKitURL)
			logger.Warn("LiveKit initialization skipped")
		} else {
			livekitSvc = NewLiveKitService(cfg.LiveKitURL, cfg.LiveKitAPIKey, cfg.LiveKitAPISecret)
			logger.Infof("LiveKit service initialized: url=%s", cfg.LiveKitURL)

			// 测试连接（非阻塞，最多等待3秒）
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if livekitSvc.IsHealthy(ctx) {
				logger.Infof("LiveKit service health check: ✓ PASSED")
			} else {
				logger.Warnf("LiveKit service health check: ✗ FAILED (service may be unavailable)")
			}
		}
	} else {
		logger.Warnf("LiveKit not configured, video channels disabled")
		logger.Warn("  Required env vars: LIVEKIT_URL, LIVEKIT_API_KEY, LIVEKIT_API_SECRET")
	}
	// 初始化音频转换服务（如果MinIO可用且FFmpeg服务URL已配置）
	var audioConverterSvc *AudioConverterService
	if minioSvc != nil && cfg.FFmpegURL != "" {
		audioConverterSvc, err = NewAudioConverterService(minioSvc, cfg.FFmpegURL)
		if err != nil {
			logger.Warnf("Audio converter service initialization failed: %v", err)
		} else {
			logger.Infof("Audio converter service initialized successfully: url=%s", cfg.FFmpegURL)
		}
	} else if cfg.FFmpegURL == "" {
		logger.Info("FFmpeg service URL not configured, audio conversion disabled")
	}
	return &Service{
		Repo:           repo,
		Cfg:            cfg,
		Email:          emailSvc,
		SMS:            smsSvc,
		MinIO:          minioSvc,
		LiveKit:        livekitSvc,
		AudioConverter: audioConverterSvc,
		RedisClient:    repo.Redis, // 从 Repo 获取 Redis 客户端
	}
}

// ──────────────────────────────────────────────
// Redis Cache helpers
// ──────────────────────────────────────────────

// getCache 从 Redis 获取缓存数据并反序列化到 dest
func (s *Service) getCache(key string, dest interface{}) error {
	if s.RedisClient == nil {
		return fmt.Errorf("redis not available")
	}
	ctx := context.Background()
	data, err := s.RedisClient.Get(ctx, key).Bytes()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

// setCache 序列化数据并存入 Redis，设置过期时间
func (s *Service) setCache(key string, value interface{}, expiration time.Duration) error {
	if s.RedisClient == nil {
		return fmt.Errorf("redis not available")
	}
	ctx := context.Background()
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.RedisClient.Set(ctx, key, data, expiration).Err()
}

// deleteCache 删除 Redis 缓存
func (s *Service) deleteCache(key string) error {
	if s.RedisClient == nil {
		return nil
	}
	ctx := context.Background()
	return s.RedisClient.Del(ctx, key).Err()
}

// deleteCacheByPrefix 使用 SCAN 按前缀批量删除 Redis 缓存
func (s *Service) deleteCacheByPrefix(prefix string) error {
	if s.RedisClient == nil {
		return nil
	}
	ctx := context.Background()
	var cursor uint64
	for {
		keys, nextCursor, err := s.RedisClient.Scan(ctx, cursor, prefix+"*", 100).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			s.RedisClient.Del(ctx, keys...)
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	return nil
}

// getGuildMemberCountCacheKey 获取服务器成员数缓存键
func getGuildMemberCountCacheKey(guildID uint) string {
	return fmt.Sprintf("guild:%d:member_count", guildID)
}

// ClearGuildMemberCountCache 清除指定服务器的成员数缓存
func (s *Service) ClearGuildMemberCountCache(guildID uint) {
	_ = s.deleteCache(getGuildMemberCountCacheKey(guildID))
}

// ──────────────────────────────────────────────
// LiveKit health
// ──────────────────────────────────────────────

// GetLiveKitHealth 检查 LiveKit 服务健康状态
func (s *Service) GetLiveKitHealth() bool {
	if s.LiveKit == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return s.LiveKit.IsHealthy(ctx)
}

// ──────────────────────────────────────────────
// Token / code / user creation helpers
// ──────────────────────────────────────────────

func randToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// randNumericCode 生成指定长度的数字验证码
func randNumericCode(length int) string {
	const digits = "0123456789"
	b := make([]byte, length)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = digits[b[i]%10]
	}
	return string(b)
}

// createUserWithUniqueToken attempts to create a user and retries when
// the failure is caused by a token-unique-index collision. It will
// regenerate the Token field on collision and retry a few times.
func (s *Service) createUserWithUniqueToken(u *models.User) error {
	var lastErr error
	for i := 0; i < 5; i++ {
		if err := s.Repo.CreateUser(u); err != nil {
			lastErr = err
			se := err.Error()
			// MySQL duplicate key error contains "Duplicate entry" and the index name.
			if strings.Contains(se, "Duplicate entry") && (strings.Contains(se, "token") || strings.Contains(se, "idx_users_token")) {
				// regenerate token keeping common prefixes if present
				if strings.HasPrefix(u.Token, "legacy_") {
					u.Token = "legacy_" + randToken(8)
				} else if strings.HasPrefix(u.Token, "bot_user_") {
					u.Token = "bot_user_" + randToken(8)
				} else {
					// fallback: generate a longer random token
					u.Token = randToken(32)
				}
				// try again
				continue
			}
			return err
		}
		return nil
	}
	return lastErr
}

// ──────────────────────────────────────────────
// Password helpers
// ──────────────────────────────────────────────

func hashPassword(pw string) (string, error) {
	pw = strings.TrimSpace(pw)
	if !isStrongPassword(pw) {
		return "", &Err{Code: 400, Msg: "密码强度不足"}
	}
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func verifyPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// ──────────────────────────────────────────────
// Email / password validation
// ──────────────────────────────────────────────

var emailRegex = regexp.MustCompile(`^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$`)
var pwLetter = regexp.MustCompile(`[A-Za-z]`)
var pwDigit = regexp.MustCompile(`[0-9]`)

func isValidEmail(e string) bool {
	e = strings.TrimSpace(e)
	if len(e) < 6 || len(e) > 128 {
		return false
	}
	return emailRegex.MatchString(e)
}

func isStrongPassword(pw string) bool {
	pw = strings.TrimSpace(pw)
	if len(pw) < 8 || len(pw) > 72 { // bcrypt max practical length
		return false
	}
	if !pwLetter.MatchString(pw) || !pwDigit.MatchString(pw) {
		return false
	}
	return true
}

// ──────────────────────────────────────────────
// Token hashing (SHA-256, for verification codes etc.)
// ──────────────────────────────────────────────

// hashToken returns a hex SHA256 of the provided token (for storage instead of plaintext).
func hashToken(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

// verifyToken 验证 token 是否匹配哈希值
func verifyToken(hash, tok string) bool {
	return hashToken(tok) == hash
}

// ──────────────────────────────────────────────
// Sentinel errors
// ──────────────────────────────────────────────

var (
	ErrBadRequest      = &Err{Code: 400, Msg: "参数错误"}
	ErrUnauthorized    = &Err{Code: 401, Msg: "未认证"}
	ErrForbidden       = &Err{Code: 403, Msg: "无权限"}
	ErrNotFound        = &Err{Code: 404, Msg: "未找到"}
	ErrTooManyRequests = &Err{Code: 429, Msg: "请求过于频繁"}
	ErrAlreadyExists   = &Err{Code: 409, Msg: "已存在"}
)

type Err struct {
	Code int
	Msg  string
	Data map[string]string // 额外数据（如验证问题）
}

func (e *Err) Error() string { return e.Msg }
