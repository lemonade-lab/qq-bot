package handlers

import (
	"net/http"

	"bubble/src/server"

	"github.com/gin-gonic/gin"
)

// HealthHandler 健康检查处理器
type HealthHandler struct {
	gracefulServer *server.GracefulServer
	svc            interface {
		GetLiveKitHealth() bool
	}
}

// NewHealthHandler 创建健康检查处理器
func NewHealthHandler(gs *server.GracefulServer) *HealthHandler {
	return &HealthHandler{gracefulServer: gs}
}

// SetService 设置服务引用（用于检查 LiveKit 等外部依赖）
func (h *HealthHandler) SetService(svc interface {
	GetLiveKitHealth() bool
}) {
	h.svc = svc
}

// Register 注册健康检查路由
func (h *HealthHandler) Register(r *gin.Engine) {
	// Liveness probe: 检查服务是否存活
	r.GET("/health/live", h.liveness)

	// Readiness probe: 检查服务是否就绪（连接耗尽时返回 503）
	r.GET("/health/ready", h.readiness)

	// 连接状态端点（用于监控）
	r.GET("/health/connections", h.connections)
	// Drain endpoint: 允许外部（例如 k8s preStop）触发 drain
	r.POST("/health/drain", h.drain)

	// 兼容 internal/drain 路径（deployment preStop 调用此路径）
	r.POST("/internal/drain", h.drain)
}

// liveness 存活检查：服务是否在运行
func (h *HealthHandler) liveness(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "alive",
	})
}

// readiness 就绪检查：服务是否准备好接受请求
func (h *HealthHandler) readiness(c *gin.Context) {
	response := gin.H{
		"status": "ready",
	}

	// 检查是否正在关闭
	if h.gracefulServer != nil && h.gracefulServer.IsShuttingDown() {
		activeConns := h.gracefulServer.GetActiveConns()
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":            "shutting_down",
			"activeConnections": activeConns,
			"message":           "服务器正在释放连接",
		})
		return
	}

	// 检查 LiveKit 健康状态（可选依赖，不影响就绪状态）
	if h.svc != nil {
		livekitHealthy := h.svc.GetLiveKitHealth()
		response["livekit"] = map[string]interface{}{
			"healthy": livekitHealthy,
			"status":  getHealthStatus(livekitHealthy),
		}
	}

	c.JSON(http.StatusOK, response)
}

func getHealthStatus(healthy bool) string {
	if healthy {
		return "healthy"
	}
	return "unavailable"
}

// connections 返回当前连接状态
func (h *HealthHandler) connections(c *gin.Context) {
	if h.gracefulServer == nil {
		c.JSON(http.StatusOK, gin.H{
			"httpConnections":  0,
			"wsConnections":    0,
			"totalConnections": 0,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"httpConnections":  h.gracefulServer.GetActiveHTTPConns(),
		"wsConnections":    h.gracefulServer.GetActiveWSConns(),
		"totalConnections": h.gracefulServer.GetActiveConns(),
		"shuttingDown":     h.gracefulServer.IsShuttingDown(),
	})
}

// drain 触发服务器进入 draining 状态，返回 202 Accepted
func (h *HealthHandler) drain(c *gin.Context) {
	if h.gracefulServer == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未配置优雅关闭服务"})
		return
	}
	first := h.gracefulServer.BeginDrain()
	if first {
		c.JSON(http.StatusAccepted, gin.H{"status": "draining"})
	} else {
		c.JSON(http.StatusAccepted, gin.H{"status": "already_draining"})
	}
}
