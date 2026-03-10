package main

// @title           Bubble Chat API
// @version         0.1.0
// @description     Simple chat server (Gin + Gorm)
// @BasePath        /
// @schemes         http
// @securityDefinitions.apikey BearerAuth
// @in              header
// @name            Authorization

import (
	"context"
	"time"

	"bubble/docs"
	"bubble/src/config"
	"bubble/src/db"
	"bubble/src/db/models"
	"bubble/src/handlers"
	"bubble/src/logger"
	"bubble/src/middleware"
	"bubble/src/repository"
	"bubble/src/server"
	"bubble/src/service"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

var Version = "0.1.0"
var BuildTime = "unknown"

func main() {
	// swagger info
	docs.SwaggerInfo.Title = "Bubble Chat API"
	docs.SwaggerInfo.Description = "Simple chat server (Gin + Gorm)"
	docs.SwaggerInfo.Version = Version
	docs.SwaggerInfo.BasePath = "/"

	// Initialize basic logger first (before config loading)
	logger.Init("info", false, false, "development")

	// load config (.env) and init
	cfg := config.Load()

	// Re-initialize logger with config parameters
	logger.Init(cfg.LogLevel, cfg.LogJSON, cfg.LogEnableLoki, cfg.Environment)

	// Add Loki hook if enabled
	if cfg.LogEnableLoki {
		if err := logger.AddLokiHook(cfg.LokiURL, cfg.AppName, cfg.Environment); err != nil {
			logger.Warnf("Failed to initialize Loki hook: %v", err)
		} else {
			logger.Info("Loki logging enabled")
		}
	}

	// Log configuration after logger is fully initialized
	logger.Infof("config loaded: PORT=%s GIN_MODE=%s DB_DRIVER=%s EMAIL_ENABLED=%v JWT_ACCESS_MINUTES=%d REDIS_ADDR=%s MINIO_ENDPOINT=%s LIVEKIT_URL=%s LOG_LEVEL=%s LOG_ENABLE_LOKI=%v",
		cfg.Port, cfg.GinMode, cfg.DBDriver, cfg.EmailEnabled, cfg.JWTAccessMinutes, cfg.RedisAddr, cfg.MinIOEndpoint, cfg.LiveKitURL, cfg.LogLevel, cfg.LogEnableLoki)

	logger.Infof("Starting %s v%s (built at %s) in %s mode", cfg.AppName, Version, BuildTime, cfg.Environment)

	// init db and migrate (Session is now in Redis, not SQL)
	gdb := db.NewWith(cfg.DBDriver, cfg.DatabaseDSN)
	db.AutoMigrate(gdb, &models.User{}, &models.TrustedDevice{}, &models.Guild{}, &models.ChannelCategory{}, &models.Channel{}, &models.Message{}, &models.GuildMember{}, &models.Role{}, &models.MemberRole{}, &models.Friendship{}, &models.DmThread{}, &models.DmMessage{}, &models.SecurityEvent{}, &models.Greeting{}, &models.Announcement{}, &models.PinnedMessage{}, &models.Blacklist{}, &models.GuildJoinRequest{}, &models.Moment{}, &models.MomentLike{}, &models.MomentComment{}, &models.FavoriteMessage{}, &models.LiveKitRoom{}, &models.LiveKitParticipant{}, &models.Robot{}, &models.ForumPost{}, &models.ForumReply{}, &models.ForumPostLike{}, &models.UserNotification{}, &models.UserApplication{}, &models.ReadState{}, &models.MessageReaction{}, &models.GroupThread{}, &models.GroupThreadMember{}, &models.GroupMessage{}, &models.SubRoom{}, &models.SubRoomMember{}, &models.SubRoomMessage{}, &models.GuildFile{}, &models.GroupJoinRequest{}, &models.GroupAnnouncement{}, &models.GroupFile{}, &models.RobotCategory{}, &models.RobotRanking{}, &models.WebhookLog{})

	// init Redis for session storage
	rdb := db.NewRedis(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)

	// wire repo and service with Redis support
	repo := repository.NewWithRedis(gdb, rdb)
	svc := service.New(repo, cfg)

	// 初始化默认机器人分类
	repo.EnsureDefaultCategories()

	// gin mode
	gin.SetMode(cfg.GinMode)

	// 创建路由
	r := gin.New()
	r.Use(logger.GinRecovery())
	r.Use(logger.RequestIDMiddleware()) // Add request ID tracking
	r.Use(logger.GinLogger())

	// 创建优雅关闭服务器（先创建，用于中间件和健康检查）
	addr := cfg.Port
	gracefulSrv := server.NewGracefulServer(addr, r)

	// Align max shutdown time with k8s terminationGracePeriodSeconds (300s),
	// leave a small buffer for SIGTERM handling: use 285s by default.
	gracefulSrv.SetMaxShutdownTime(285 * time.Second)

	// 设置连接跟踪中间件（必须在所有路由之前，但在 Recovery 之后）
	r.Use(middleware.ConnectionTracker(gracefulSrv))

	// 注册健康检查端点（需要在业务路由之前，确保健康检查可用）
	healthHandler := handlers.NewHealthHandler(gracefulSrv)
	healthHandler.SetService(svc) // 设置服务引用以检查外部依赖
	healthHandler.Register(r)

	// gin router
	h := handlers.NewHTTP(svc, cfg)
	h.Register(r)

	// swagger
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// 设置 Gateway 的连接跟踪（必须在 Register 之后，因为 Gateway 在 Register 时创建）
	if h.Gw != nil {
		h.Gw.SetGracefulServer(gracefulSrv)
	}

	// 启动 LiveKit 清理任务（在后台定期清理孤儿记录）
	cleanupCtx, cancelCleanup := context.WithCancel(context.Background())
	defer cancelCleanup()
	go svc.StartLiveKitCleanupWorker(cleanupCtx)

	// 启动服务器（支持优雅关闭）
	logger.Info("📊 Health check endpoints:")
	logger.Infof("   - Liveness:  http://%s/health/live", addr)
	logger.Infof("   - Readiness: http://%s/health/ready", addr)
	logger.Infof("   - Connections: http://%s/health/connections", addr)

	if err := gracefulSrv.Start(); err != nil {
		logger.Fatalf("Server error: %v", err)
	}

	logger.Info("✅ Server stopped gracefully")
}
