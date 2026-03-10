package handlers

// 这是一个日志使用示例，展示如何在实际的 handler 中使用新的日志系统

import (
	"time"

	"bubble/src/logger"
	"bubble/src/service"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// ExampleHandler 示例 handler，展示日志最佳实践
type ExampleHandler struct {
	Svc *service.Service
}

// CreateGuildExample 创建公会的示例（展示完整的日志记录）
func (h *ExampleHandler) CreateGuildExample(c *gin.Context) {
	// 1. 获取请求上下文信息
	requestID := logger.GetRequestID(c)
	userID := c.GetString("user_id") // 从认证中间件获取

	// 2. 创建日志上下文
	logCtx := logger.NewLogContext(requestID, userID)

	// 3. 记录请求开始
	logCtx.WithAction("create_guild").
		WithResource("guild").
		Info(logger.LogTypeBusiness, "Processing create guild request")

	// 4. 解析请求
	var req struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		logCtx.WithField("error", err.Error()).
			Warn(logger.LogTypeBusiness, "Invalid request body")
		c.JSON(400, gin.H{"error": "Invalid request"})
		return
	}

	// 5. 添加请求参数到日志上下文
	logCtx.WithFields(logrus.Fields{
		"guild_name":      req.Name,
		"has_description": req.Description != "",
	})

	// 6. 调用业务逻辑（假设的示例）
	start := time.Now()

	// 模拟数据库操作
	// guild, err := h.Svc.CreateGuild(logCtx, userID, req.Name, req.Description)

	// 记录数据库操作耗时
	duration := time.Since(start)
	logger.LogDatabase(logCtx, "INSERT", "guilds", duration, nil)

	// 7. 处理业务逻辑成功
	guildID := "guild_123" // 示例
	logCtx.WithField("guild_id", guildID).
		Info(logger.LogTypeBusiness, "Guild created successfully")

	// 8. 返回响应
	c.JSON(200, gin.H{
		"id":   guildID,
		"name": req.Name,
	})
}

// LoginExample 登录示例（展示认证日志）
func (h *ExampleHandler) LoginExample(c *gin.Context) {
	requestID := logger.GetRequestID(c)
	logCtx := logger.NewLogContext(requestID, "")

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		logCtx.Warn(logger.LogTypeAuth, "Invalid login request")
		c.JSON(400, gin.H{"error": "Invalid request"})
		return
	}

	// 记录登录尝试
	logger.LogAuth(logCtx, "login_attempt", req.Email, true, "")

	// 模拟验证
	success := true // 假设验证成功

	if !success {
		logger.LogAuth(logCtx, "login_failed", req.Email, false, "invalid_credentials")
		c.JSON(401, gin.H{"error": "Invalid credentials"})
		return
	}

	// 登录成功
	userID := "user_123"
	logCtx.UserID = userID
	logger.LogAuth(logCtx, "login_success", req.Email, true, "")

	c.JSON(200, gin.H{"user_id": userID})
}

// ExternalAPIExample 调用外部 API 示例
func (h *ExampleHandler) ExternalAPIExample(c *gin.Context) {
	requestID := logger.GetRequestID(c)
	userID := c.GetString("user_id")
	logCtx := logger.NewLogContext(requestID, userID)

	// 调用外部 API
	start := time.Now()

	// 模拟 HTTP 请求
	// resp, err := http.Get("https://api.example.com/data")

	duration := time.Since(start)
	statusCode := 200 // 假设成功

	// 记录外部 API 调用
	logger.LogExternalAPI(logCtx, "example_api", "/data", statusCode, duration, nil)

	c.JSON(200, gin.H{"status": "ok"})
}

// CacheExample 缓存操作示例
func (h *ExampleHandler) CacheExample(c *gin.Context) {
	requestID := logger.GetRequestID(c)
	userID := c.GetString("user_id")
	logCtx := logger.NewLogContext(requestID, userID)

	cacheKey := "user:" + userID

	// 尝试从缓存获取
	// value, err := redisClient.Get(cacheKey).Result()
	hit := false // 假设缓存未命中

	logger.LogCache(logCtx, "GET", cacheKey, hit, nil)

	if !hit {
		// 缓存未命中，从数据库加载
		logCtx.Info(logger.LogTypeBusiness, "Cache miss, loading from database")

		// 加载数据...

		// 写入缓存
		// redisClient.Set(cacheKey, value, time.Hour)
		logger.LogCache(logCtx, "SET", cacheKey, true, nil)
	}

	c.JSON(200, gin.H{"data": "example"})
}

// SecurityExample 安全事件示例
func (h *ExampleHandler) SecurityExample(c *gin.Context) {
	requestID := logger.GetRequestID(c)
	logCtx := logger.NewLogContext(requestID, "")

	ip := c.ClientIP()

	// 检测可疑活动
	attempts := 10 // 假设登录失败次数

	if attempts > 5 {
		logger.LogSecurity(logCtx, "brute_force_detected", "high", logrus.Fields{
			"ip":       ip,
			"attempts": attempts,
			"action":   "account_locked",
		})

		c.JSON(403, gin.H{"error": "Account locked due to suspicious activity"})
		return
	}

	c.JSON(200, gin.H{"status": "ok"})
}

// DatabaseOperationExample 数据库操作详细示例
func (h *ExampleHandler) DatabaseOperationExample(c *gin.Context) {
	requestID := logger.GetRequestID(c)
	userID := c.GetString("user_id")
	logCtx := logger.NewLogContext(requestID, userID)

	// 示例：查询用户的所有公会
	logCtx.WithAction("list_guilds").
		WithResource("guilds").
		Info(logger.LogTypeBusiness, "Fetching user guilds")

	start := time.Now()

	// 模拟数据库查询
	// var guilds []Guild
	// err := db.Where("user_id = ?", userID).Find(&guilds).Error

	duration := time.Since(start)

	// 记录数据库操作
	logger.LogDatabase(logCtx, "SELECT", "guilds", duration, nil)

	// 如果查询慢，额外记录警告
	if duration > 500*time.Millisecond {
		logCtx.WithFields(logrus.Fields{
			"duration_ms": duration.Milliseconds(),
			"query":       "list_guilds",
		}).Warn(logger.LogTypeDatabase, "Slow database query detected")
	}

	logCtx.WithField("count", 5).
		Info(logger.LogTypeBusiness, "User guilds retrieved")

	c.JSON(200, gin.H{"guilds": []string{}})
}

// WebSocketExample WebSocket 连接示例
func (h *ExampleHandler) WebSocketExample(c *gin.Context) {
	requestID := logger.GetRequestID(c)
	userID := c.GetString("user_id")
	logCtx := logger.NewLogContext(requestID, userID)

	connectionID := requestID // 使用 request ID 作为连接 ID

	// WebSocket 连接建立
	logger.LogWebSocket(logCtx, "connected", connectionID, logrus.Fields{
		"protocol": c.GetHeader("Sec-WebSocket-Protocol"),
	})

	// 消息处理
	logger.LogWebSocket(logCtx, "message_received", connectionID, logrus.Fields{
		"message_type": "heartbeat",
	})

	// 连接断开
	logger.LogWebSocket(logCtx, "disconnected", connectionID, logrus.Fields{
		"reason":           "client_closed",
		"duration_seconds": 120,
	})

	c.JSON(200, gin.H{"status": "ok"})
}

// ErrorHandlingExample 错误处理示例
func (h *ExampleHandler) ErrorHandlingExample(c *gin.Context) {
	requestID := logger.GetRequestID(c)
	userID := c.GetString("user_id")
	logCtx := logger.NewLogContext(requestID, userID)

	// 业务逻辑
	err := processBusinessLogic()

	if err != nil {
		// 记录详细的错误信息
		logCtx.WithFields(logrus.Fields{
			"error":      err.Error(),
			"error_type": "business_error",
			"action":     "process_payment",
		}).Error(logger.LogTypeBusiness, "Payment processing failed")

		c.JSON(500, gin.H{"error": "Payment failed"})
		return
	}

	c.JSON(200, gin.H{"status": "ok"})
}

// 辅助函数
func processBusinessLogic() error {
	return nil
}
