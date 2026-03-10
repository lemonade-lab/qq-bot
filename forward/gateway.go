package forward

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// HandleGetAppAccessToken proxies POST /app/getAppAccessToken to official QQ Bot API.
// Captures app_id + secret → derives Ed25519 keys → stores access_token→app_id mapping.
func HandleGetAppAccessToken(store *BotStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		rawBody, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"msg": "cannot read body"})

			return
		}

		var req authRequest
		if err := jsonUnmarshal(rawBody, &req); err != nil || req.AppID == "" || req.ClientSecret == "" {
			c.JSON(http.StatusBadRequest, gin.H{"msg": "invalid request body"})

			return
		}

		// Proxy to official QQ Bot API
		resp, err := http.Post(authURL, "application/json", bytes.NewReader(rawBody))
		if err != nil {
			log.Printf("[auth-proxy] proxy error: %v", err)
			c.JSON(http.StatusBadGateway, gin.H{"msg": "upstream error"})

			return
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)

		var authResp authResponse
		if err := jsonUnmarshal(respBody, &authResp); err != nil || authResp.AccessToken == "" {
			c.Data(resp.StatusCode, "application/json", respBody)

			return
		}

		// Register bot: derive Ed25519 keys, store token mapping
		store.Register(req.AppID, req.ClientSecret, authResp.AccessToken, authResp.ExpiresIn)
		log.Printf("[auth-proxy] bot=%s registered", req.AppID)

		c.Data(http.StatusOK, "application/json", respBody)
	}
}

// HandleGatewayAPI handles GET /gateway.
// Returns the pre-configured WebSocket URL from env.
func HandleGatewayAPI(store *BotStore, wsURL string) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if auth == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"msg": "missing Authorization header"})

			return
		}

		// Extract and store access_token → app_id mapping
		if strings.HasPrefix(auth, "QQBot ") {
			appID := c.GetHeader("X-Union-Appid")
			accessToken := strings.TrimPrefix(auth, "QQBot ")

			if appID != "" && accessToken != "" {
				store.StoreTokenMapping(appID, accessToken)
			}
		} else if strings.HasPrefix(auth, "Bot ") {
			parts := strings.SplitN(strings.TrimPrefix(auth, "Bot "), ".", 2)
			if len(parts) == 2 && parts[0] != "" {
				store.StoreTokenMapping(parts[0], parts[1])
			}
		}

		log.Printf("[gateway] returning ws url: %s", wsURL)

		c.JSON(http.StatusOK, gin.H{
			"url": wsURL,
		})
	}
}
