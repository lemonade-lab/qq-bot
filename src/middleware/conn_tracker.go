package middleware

import (
	"bubble/src/server"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// ConnectionTracker 连接跟踪中间件
// 用于跟踪活跃的 HTTP 连接，支持优雅关闭
func ConnectionTracker(gs *server.GracefulServer) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 如果服务器正在关闭，拒绝新请求
		if gs.IsShuttingDown() {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "server is shutting down",
			})
			c.Abort()
			return
		}

		// 增加连接计数
		connID := gs.IncrementConn()
		defer func() {
			// 请求完成后减少连接计数
			gs.DecrementConn()
		}()

		// 注意：这里不需要修改 Context，defer 会确保计数正确减少

		// 继续处理请求
		c.Next()

		// 记录连接信息（可选，用于调试）
		if gin.Mode() == gin.DebugMode {
			start := time.Now()
			latency := time.Since(start)
			_ = latency
			_ = connID
		}
	}
}
