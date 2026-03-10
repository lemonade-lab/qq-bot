package main

import (
	"log"
	"os"

	"qqbot/forward"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

var Version = "0.0.1"

func main() {
	_ = godotenv.Load()

	port := os.Getenv("PORT")
	ginMode := os.Getenv("GIN_MODE")
	adminKey := os.Getenv("ADMIN_KEY")
	gatewayWSURL := os.Getenv("GATEWAY_WS_URL")

	if port == "" {
		port = ":9000"
	}

	if gatewayWSURL == "" {
		gatewayWSURL = "ws://localhost" + port + "/websocket"
	}

	store := forward.NewBotStore()

	if ginMode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()

	// API Proxy
	r.POST("/app/getAppAccessToken", forward.HandleGetAppAccessToken(store))
	r.GET("/gateway", forward.HandleGatewayAPI(store, gatewayWSURL))

	// Core
	r.POST("/webhook/:app_id", forward.HandleWebhook(store))
	r.GET("/websocket", forward.HandleGatewayWS(store))

	// Health & Admin
	r.GET("/health", func(c *gin.Context) {
		bots, clients := store.Stats()
		c.JSON(200, gin.H{
			"status":  "ok",
			"bots":    bots,
			"clients": clients,
		})
	})

	r.GET("/admin/bots", func(c *gin.Context) {
		if adminKey != "" && c.GetHeader("X-Admin-Key") != adminKey {
			c.JSON(403, gin.H{"msg": "forbidden"})

			return
		}

		c.JSON(200, store.AllBotInfo())
	})

	log.Println("╔════════════════════════════════════════════╗")
	log.Println("║       QQ Bot WebSocket Gateway             ║")
	log.Println("╚════════════════════════════════════════════╝")
	log.Println()
	log.Printf("  POST  /app/getAppAccessToken  (auth proxy)")
	log.Printf("  GET   /gateway                (returns: %s)", gatewayWSURL)
	log.Printf("  POST  /webhook/:app_id        (QQ Bot webhook)")
	log.Printf("  GET   /websocket              (WS gateway)")
	log.Printf("  GET   /health")
	log.Printf("  GET   /admin/bots")
	log.Println()
	log.Printf("Listening on %s", port)

	if err := r.Run(port); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
