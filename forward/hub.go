package forward

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// QQ Bot WebSocket protocol opcodes
const (
	OpDispatch     = 0
	OpHeartbeat    = 1
	OpIdentify     = 2
	OpInvalidSess  = 9
	OpHello        = 10
	OpHeartbeatACK = 11
)

const defaultHeartbeatInterval = 30000 // ms

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// WSMessage is the standard QQ Bot WebSocket message format.
type WSMessage struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d,omitempty"`
	T  string          `json:"t,omitempty"`
	S  int             `json:"s,omitempty"`
}

type helloData struct {
	HeartbeatInterval int `json:"heartbeat_interval"`
}

type identifyData struct {
	Token      string                 `json:"token"`
	Intents    int                    `json:"intents"`
	Shard      []int                  `json:"shard,omitempty"`
	Properties map[string]interface{} `json:"properties,omitempty"`
}

type readyData struct {
	SessionID string `json:"session_id"`
	AppID     string `json:"app_id"`
}

// HandleGatewayWS implements the QQ Bot WebSocket protocol.
//
// Supports two token formats in op=2 Identify:
//   - "QQBot {access_token}" — seamless mode (zero code change)
//   - "Bot {app_id}.{secret}" — direct mode
func HandleGatewayWS(store *BotStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Printf("[gateway] upgrade error: %v", err)

			return
		}

		// Phase 1: Send Hello
		helloPayload, _ := json.Marshal(helloData{
			HeartbeatInterval: defaultHeartbeatInterval,
		})
		if err := conn.WriteJSON(WSMessage{Op: OpHello, D: helloPayload}); err != nil {
			log.Printf("[gateway] send hello error: %v", err)
			conn.Close()

			return
		}

		// Phase 2: Wait for Identify (30s timeout)
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))

		var identifyMsg WSMessage
		if err := conn.ReadJSON(&identifyMsg); err != nil {
			log.Printf("[gateway] read identify error: %v", err)
			conn.Close()

			return
		}

		if identifyMsg.Op != OpIdentify {
			log.Printf("[gateway] expected op=2, got op=%d", identifyMsg.Op)
			sendInvalidSession(conn, "expected Identify (op=2)")
			conn.Close()

			return
		}

		var identify identifyData
		if err := json.Unmarshal(identifyMsg.D, &identify); err != nil {
			log.Printf("[gateway] parse identify error: %v", err)
			sendInvalidSession(conn, "invalid identify payload")
			conn.Close()

			return
		}

		// Phase 3: Authenticate
		bot, err := authenticateToken(store, identify.Token)
		if err != nil {
			log.Printf("[gateway] auth failed: %v", err)
			sendInvalidSession(conn, err.Error())
			conn.Close()

			return
		}

		// Phase 4: Register client and send READY
		client := bot.AddClient(conn)
		sessionID := generateID()

		readyPayload, _ := json.Marshal(readyData{
			SessionID: sessionID,
			AppID:     bot.AppID,
		})
		if err := client.WriteJSON(WSMessage{
			Op: OpDispatch,
			T:  "READY",
			D:  readyPayload,
		}); err != nil {
			log.Printf("[gateway] bot=%s send READY error: %v", bot.AppID, err)
			bot.RemoveClient(client.ID)
			conn.Close()

			return
		}

		log.Printf("[gateway] bot=%s client=%s authenticated, session=%s", bot.AppID, client.ID, sessionID)

		// Phase 5: Read loop (heartbeat + close detection)
		heartbeatTimeout := time.Duration(defaultHeartbeatInterval*2) * time.Millisecond

		defer func() {
			bot.RemoveClient(client.ID)
			conn.Close()
		}()

		for {
			conn.SetReadDeadline(time.Now().Add(heartbeatTimeout))

			var msg WSMessage
			if err := conn.ReadJSON(&msg); err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					log.Printf("[gateway] bot=%s client=%s read error: %v", bot.AppID, client.ID, err)
				}

				return
			}

			switch msg.Op {
			case OpHeartbeat:
				if err := client.WriteJSON(WSMessage{Op: OpHeartbeatACK}); err != nil {
					log.Printf("[gateway] bot=%s heartbeat ack error: %v", bot.AppID, err)

					return
				}
			default:
				// Ignore other messages
			}
		}
	}
}

// authenticateToken handles both token formats.
func authenticateToken(store *BotStore, token string) (*Bot, error) {
	token = strings.TrimSpace(token)

	if strings.HasPrefix(token, "QQBot ") {
		return authQQBotToken(store, strings.TrimPrefix(token, "QQBot "))
	}

	if strings.HasPrefix(token, "Bot ") {
		return authBotToken(store, strings.TrimPrefix(token, "Bot "))
	}

	return nil, fmt.Errorf("unsupported token format, expected 'QQBot {access_token}' or 'Bot {app_id}.{secret}'")
}

// authQQBotToken authenticates using "QQBot {access_token}".
func authQQBotToken(store *BotStore, accessToken string) (*Bot, error) {
	if accessToken == "" {
		return nil, fmt.Errorf("empty access_token")
	}

	appID := store.LookupAppID(accessToken)
	if appID == "" {
		return nil, fmt.Errorf("unknown access_token, call /app/getAppAccessToken first")
	}

	bot := store.Get(appID)
	if bot == nil {
		bot = store.RegisterLight(appID, accessToken)
	}

	return bot, nil
}

// authBotToken authenticates using "Bot {app_id}.{secret}".
func authBotToken(store *BotStore, tokenBody string) (*Bot, error) {
	idx := strings.Index(tokenBody, ".")
	if idx < 0 {
		return nil, fmt.Errorf("invalid Bot token format, expected: Bot {app_id}.{secret}")
	}

	appID := tokenBody[:idx]
	secret := tokenBody[idx+1:]

	if appID == "" || secret == "" {
		return nil, fmt.Errorf("app_id and secret cannot be empty")
	}

	accessToken, expiresIn, err := FetchAccessToken(appID, secret)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}

	bot := store.Register(appID, secret, accessToken, expiresIn)

	return bot, nil
}

// sendInvalidSession sends op=9 with error info.
func sendInvalidSession(conn *websocket.Conn, reason string) {
	d, _ := json.Marshal(map[string]string{"reason": reason})
	_ = conn.WriteJSON(WSMessage{Op: OpInvalidSess, D: d})
}
