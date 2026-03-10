package config

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Port                 string
	DatabaseDSN          string
	GinMode              string
	DBDriver             string
	AllowAnonymousLogin  bool
	RequireEmailVerified bool
	JWTSecret            string
	JWTAccessMinutes     int    // Access token TTL (15 minutes for mobile clients)
	JWTRefreshDays       int    // Refresh token TTL (30 days for mobile clients)
	SessionCookieName    string // Session cookie name
	// Email settings - Tencent Cloud SES
	EmailEnabled      bool
	TencentSecretId   string // 腾讯云 SecretId
	TencentSecretKey  string // 腾讯云 SecretKey
	TencentSESRegion  string // SES 地域（如：ap-guangzhou）
	TencentTemplateID uint   // 邮件模板ID
	EmailFrom         string // 发件人邮箱
	EmailFromName     string // 发件人名称
	FrontendURL       string // 前端URL（用于邮件中的链接）
	// Redis settings for Session storage
	RedisAddr     string // Redis address (host:port)
	RedisPassword string // Redis password (optional)
	RedisDB       int    // Redis database number
	// MinIO settings for file storage
	MinIOEndpoint        string   // MinIO endpoint (e.g., localhost:9000)
	MinIOAccessKeyID     string   // MinIO access key
	MinIOSecretAccessKey string   // MinIO secret key
	MinIOUseSSL          bool     // Use SSL for MinIO
	MinIOBucket          string   // Default bucket name for avatars (deprecated, use MinIOBuckets)
	MinIOBuckets         []string // List of buckets to create and manage
	// LiveKit settings for video channels
	LiveKitURL       string // LiveKit server URL (e.g., ws://localhost:7880)
	LiveKitAPIKey    string // LiveKit API key
	LiveKitAPISecret string // LiveKit API secret
	// FFmpeg settings for audio conversion
	FFmpegURL string // FFmpeg service URL (e.g., http://localhost:19080)
	// Alibaba Cloud SMS settings
	SMSEnabled         bool   // Enable SMS authentication
	AliyunAccessKeyID  string // Alibaba Cloud AccessKey ID
	AliyunAccessSecret string // Alibaba Cloud AccessKey Secret
	// Logging settings
	LogLevel      string // Log level (debug, info, warn, error, fatal, panic)
	LogJSON       bool   // Use JSON format for logs
	LogEnableLoki bool   // Enable Loki integration
	LokiURL       string // Loki server URL (e.g., http://localhost:13100/loki/api/v1/push)
	AppName       string // Application name for Loki labels
	Environment   string // Environment (dev, staging, prod)
	// Robot ranking heat score weights
	RankWeightGuild       int // 存量服务器数权重 (默认 5)
	RankWeightGuildGrowth int // 周期内新增服务器数权重 (默认 20)
	RankWeightMessage     int // 周期内消息数权重 (默认 1)
	RankWeightInteraction int // 周期内被回复数权重 (默认 3)
	RankDecayDays         int // 不活跃衰减天数阈值 (默认 7，超过此天数无消息则衰减)
	RankDecayPercent      int // 衰减百分比 (默认 30，即衰减30%)
	RankCacheDailyMin     int // 日榜缓存时长（分钟，默认5）
	RankCacheWeeklyMin    int // 周榜缓存时长（分钟，默认15）
	RankCacheMonthlyMin   int // 月榜缓存时长（分钟，默认30）
}

func Load() *Config {
	// 加载本地的 .env 文件（如果存在）
	_ = godotenv.Load()

	cfg := &Config{
		Port:                 getEnv("PORT", ":8080"),
		DatabaseDSN:          getEnv("DATABASE_DSN", "root:password@tcp(localhost:3306)/bubble?charset=utf8mb4&parseTime=True&loc=Local"),
		GinMode:              getEnv("GIN_MODE", "release"),
		DBDriver:             getEnv("DB_DRIVER", "mysql"),
		AllowAnonymousLogin:  getEnvBool("ALLOW_ANON_LOGIN", true),
		RequireEmailVerified: getEnvBool("REQUIRE_EMAIL_VERIFIED", false),
		JWTSecret:            getEnv("JWT_SECRET", randomSecret()),
		JWTAccessMinutes:     getEnvInt("JWT_ACCESS_MINUTES", 15), // 15-minute access token for mobile clients
		JWTRefreshDays:       getEnvInt("JWT_REFRESH_DAYS", 30),   // 30-day refresh token for mobile clients
		SessionCookieName:    getEnv("SESSION_COOKIE_NAME", "bubble_session"),
		// Email settings - Tencent Cloud SES
		EmailEnabled:      getEnvBool("EMAIL_ENABLED", false),
		TencentSecretId:   getEnv("TENCENT_SECRET_ID", ""),
		TencentSecretKey:  getEnv("TENCENT_SECRET_KEY", ""),
		TencentSESRegion:  getEnv("TENCENT_SES_REGION", "ap-guangzhou"),
		TencentTemplateID: uint(getEnvInt("TENCENT_TEMPLATE_ID", 0)),
		EmailFrom:         getEnv("EMAIL_FROM", "app@e.alemonjs.com"),
		EmailFromName:     getEnv("EMAIL_FROM_NAME", "Bubble"),
		FrontendURL:       getEnv("FRONTEND_URL", "http://localhost:5173"),
		// Redis settings
		RedisAddr:     getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       getEnvInt("REDIS_DB", 0),
		// MinIO settings
		MinIOEndpoint:        getEnv("MINIO_ENDPOINT", "localhost:9000"),
		MinIOAccessKeyID:     getEnv("MINIO_ACCESS_KEY_ID", "lemonade"),
		MinIOSecretAccessKey: getEnv("MINIO_SECRET_ACCESS_KEY", "lemonade"),
		MinIOUseSSL:          getEnvBool("MINIO_USE_SSL", false),
		MinIOBucket:          getEnv("MINIO_BUCKET", "avatars"), // 保持向后兼容
		MinIOBuckets:         getEnvBuckets("MINIO_BUCKETS", []string{"bubble", "backups", "avatars", "covers", "emojis", "temp", "guild-chat-files", "private-chat-files"}),
		// LiveKit settings
		LiveKitURL:       getEnv("LIVEKIT_URL", ""),
		LiveKitAPIKey:    getEnv("LIVEKIT_API_KEY", ""),
		LiveKitAPISecret: getEnv("LIVEKIT_API_SECRET", ""),
		// FFmpeg settings
		FFmpegURL: getEnv("FFMPEG_URL", ""),
		// Alibaba Cloud SMS settings
		SMSEnabled:         getEnvBool("SMS_ENABLED", false),
		AliyunAccessKeyID:  getEnv("ALIYUN_ACCESS_KEY_ID", ""),
		AliyunAccessSecret: getEnv("ALIYUN_ACCESS_SECRET", ""),
		// Logging settings
		LogLevel:      getEnv("LOG_LEVEL", "info"),
		LogJSON:       getEnvBool("LOG_JSON", true),
		LogEnableLoki: getEnvBool("LOG_ENABLE_LOKI", false),
		LokiURL:       getEnv("LOKI_URL", "http://localhost:13100/loki/api/v1/push"),
		AppName:       getEnv("APP_NAME", "bubble"),
		Environment:   getEnv("ENVIRONMENT", "development"),
		// Robot ranking heat score weights
		RankWeightGuild:       getEnvInt("RANK_WEIGHT_GUILD", 5),
		RankWeightGuildGrowth: getEnvInt("RANK_WEIGHT_GUILD_GROWTH", 20),
		RankWeightMessage:     getEnvInt("RANK_WEIGHT_MESSAGE", 1),
		RankWeightInteraction: getEnvInt("RANK_WEIGHT_INTERACTION", 3),
		RankDecayDays:         getEnvInt("RANK_DECAY_DAYS", 7),
		RankDecayPercent:      getEnvInt("RANK_DECAY_PERCENT", 30),
		RankCacheDailyMin:     getEnvInt("RANK_CACHE_DAILY_MIN", 5),
		RankCacheWeeklyMin:    getEnvInt("RANK_CACHE_WEEKLY_MIN", 15),
		RankCacheMonthlyMin:   getEnvInt("RANK_CACHE_MONTHLY_MIN", 30),
	}
	// Log config after logger is initialized in main.go
	return cfg
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	v = strings.ToLower(strings.TrimSpace(v))
	return v == "1" || v == "true" || v == "yes"
}

func getEnvInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	var n int
	for _, ch := range v { // simple parse (avoid strconv import addition)
		if ch < '0' || ch > '9' {
			return def
		}
		n = n*10 + int(ch-'0')
	}
	if n <= 0 { // prevent non-positive ttl
		return def
	}
	return n
}

func getEnvBuckets(key string, def []string) []string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	// 支持逗号分隔的存储桶列表
	buckets := strings.Split(v, ",")
	result := make([]string, 0, len(buckets))
	for _, b := range buckets {
		b = strings.TrimSpace(b)
		if b != "" {
			result = append(result, b)
		}
	}
	if len(result) == 0 {
		return def
	}
	return result
}

func randomSecret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
