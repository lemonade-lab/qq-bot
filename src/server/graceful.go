package server

import (
	"bubble/src/logger"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// GracefulServer 提供连接感知的优雅关闭服务器
type GracefulServer struct {
	httpServer      *http.Server
	activeConns     int64 // 活跃 HTTP 连接数
	activeWSConns   int64 // 活跃 WebSocket 连接数
	shutdownChan    chan struct{}
	shutdownOnce    sync.Once
	draining        int32 // 0 = not draining, 1 = draining (pre-shutdown)
	maxShutdownTime time.Duration
}

// NewGracefulServer 创建新的优雅关闭服务器
func NewGracefulServer(addr string, handler http.Handler) *GracefulServer {
	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
		// 注意：连接跟踪通过中间件实现，不在这里处理
	}

	return &GracefulServer{
		httpServer:      srv,
		shutdownChan:    make(chan struct{}),
		maxShutdownTime: 30 * time.Second, // 最大关闭时间 30 秒
	}
}

// SetMaxShutdownTime 设置最大关闭等待时间
func (gs *GracefulServer) SetMaxShutdownTime(d time.Duration) {
	gs.maxShutdownTime = d
}

// IncrementConn 增加活跃连接计数（HTTP）
func (gs *GracefulServer) IncrementConn() int64 {
	return atomic.AddInt64(&gs.activeConns, 1)
}

// DecrementConn 减少活跃连接计数（HTTP）
func (gs *GracefulServer) DecrementConn() int64 {
	return atomic.AddInt64(&gs.activeConns, -1)
}

// IncrementWSConn 增加活跃 WebSocket 连接计数
func (gs *GracefulServer) IncrementWSConn() int64 {
	return atomic.AddInt64(&gs.activeWSConns, 1)
}

// DecrementWSConn 减少活跃 WebSocket 连接计数
func (gs *GracefulServer) DecrementWSConn() int64 {
	return atomic.AddInt64(&gs.activeWSConns, -1)
}

// GetActiveConns 获取当前活跃连接数（HTTP + WebSocket）
func (gs *GracefulServer) GetActiveConns() int64 {
	return atomic.LoadInt64(&gs.activeConns) + atomic.LoadInt64(&gs.activeWSConns)
}

// GetActiveHTTPConns 获取当前活跃 HTTP 连接数
func (gs *GracefulServer) GetActiveHTTPConns() int64 {
	return atomic.LoadInt64(&gs.activeConns)
}

// GetActiveWSConns 获取当前活跃 WebSocket 连接数
func (gs *GracefulServer) GetActiveWSConns() int64 {
	return atomic.LoadInt64(&gs.activeWSConns)
}

// IsShuttingDown 检查服务器是否正在关闭
func (gs *GracefulServer) IsShuttingDown() bool {
	// 如果处于 draining（预耗尽）或 shutdownChan 已关闭，都视为正在关闭/耗尽阶段
	if atomic.LoadInt32(&gs.draining) == 1 {
		return true
	}
	select {
	case <-gs.shutdownChan:
		return true
	default:
		return false
	}
}

// BeginDrain 将服务器置为 draining 状态（预先开始拒绝新连接，但不立即关闭 HTTP 服务）
// 可由 preStop 钩子调用，主进程随后会收到 SIGTERM 并调用 Shutdown
func (gs *GracefulServer) BeginDrain() bool {
	// 将 draining 标志从 0 置为 1，返回是否是第一次设置
	return atomic.CompareAndSwapInt32(&gs.draining, 0, 1)
}

// Start 启动服务器并监听信号
func (gs *GracefulServer) Start() error {
	// 监听系统信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 在 goroutine 中启动服务器
	serverErrChan := make(chan error, 1)
	go func() {
		logger.Infof("🚀 Server starting on %s", gs.httpServer.Addr)
		if err := gs.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrChan <- err
		}
		close(serverErrChan) // 确保 channel 关闭
	}()

	// 等待信号或服务器错误
	select {
	case sig := <-sigChan:
		logger.Infof("📡 Received signal: %v, starting graceful shutdown...", sig)
		// 在调用 Shutdown 前先进入 draining 状态，确保 readiness 立即返回 503
		gs.BeginDrain()
		return gs.Shutdown(context.Background())
	case err := <-serverErrChan:
		return fmt.Errorf("server error: %w", err)
	}
}

// Shutdown 优雅关闭服务器
func (gs *GracefulServer) Shutdown(ctx context.Context) error {
	var shutdownErr error
	gs.shutdownOnce.Do(func() {
		close(gs.shutdownChan)

		// 创建带超时的上下文
		shutdownCtx, cancel := context.WithTimeout(ctx, gs.maxShutdownTime)
		defer cancel()

		logger.Infof("⏳ Waiting for active connections to drain...")
		logger.Infof("   Active HTTP connections: %d", gs.GetActiveHTTPConns())
		logger.Infof("   Active WebSocket connections: %d", gs.GetActiveWSConns())

		// 轮询等待连接耗尽
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		drained := false
		for !drained {
			select {
			case <-shutdownCtx.Done():
				logger.Infof("⚠️  Shutdown timeout reached, forcing close...")
				drained = true
			case <-ticker.C:
				active := gs.GetActiveConns()
				if active == 0 {
					logger.Infof("✅ All connections drained")
					drained = true
				} else {
					logger.Infof("   Still waiting... Active connections: %d (HTTP: %d, WS: %d)",
						active, gs.GetActiveHTTPConns(), gs.GetActiveWSConns())
				}
			}
		}

		// 停止接受新连接
		logger.Infof("🛑 Stopping HTTP server...")
		if err := gs.httpServer.Shutdown(shutdownCtx); err != nil {
			shutdownErr = fmt.Errorf("HTTP server shutdown error: %w", err)
		} else {
			logger.Infof("✅ HTTP server stopped gracefully")
		}
	})

	return shutdownErr
}

// ListenAndServe 兼容标准接口
func (gs *GracefulServer) ListenAndServe() error {
	return gs.Start()
}
