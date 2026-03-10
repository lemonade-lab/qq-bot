package routes

import (
	_ "bubble/docs"
	"bubble/src/handlers"
	"bubble/src/server"

	"github.com/gin-gonic/gin"
)

func SetupRouter(h *handlers.HTTP, gracefulSrv *server.GracefulServer) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())

	// 连接跟踪中间件（必须在路由注册前）
	if gracefulSrv != nil {
		// 注意：这里需要导入 middleware，但为了避免循环依赖，我们在 main.go 中设置
		// 这里只是预留接口
	}

	h.Register(r)
	return r
}
