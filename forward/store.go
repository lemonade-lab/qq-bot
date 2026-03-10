package forward

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// Client represents a connected WebSocket client with a write mutex.
type Client struct {
	ID   string
	Conn *websocket.Conn
	mu   sync.Mutex
}

// WriteJSON writes a JSON message to the client (thread-safe).
func (cl *Client) WriteJSON(v interface{}) error {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	return cl.Conn.WriteJSON(v)
}

// WriteBytes writes raw bytes to the client (thread-safe).
func (cl *Client) WriteBytes(data []byte) error {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	return cl.Conn.WriteMessage(websocket.TextMessage, data)
}

// Bot holds per-tenant state: keys, token, WebSocket clients.
type Bot struct {
	AppID   string
	Secret  string
	PrivKey ed25519.PrivateKey
	PubKey  ed25519.PublicKey

	TokenMgr *TokenManager

	mu      sync.RWMutex
	clients map[string]*Client
}

// HasKeys returns true if the bot has Ed25519 keys (registered with secret).
func (b *Bot) HasKeys() bool {
	return b.PubKey != nil
}

// BotStore manages all registered bots (multi-tenant).
type BotStore struct {
	mu   sync.RWMutex
	bots map[string]*Bot

	tokenMu  sync.RWMutex
	tokenMap map[string]string // access_token -> app_id
}

func NewBotStore() *BotStore {
	return &BotStore{
		bots:     make(map[string]*Bot),
		tokenMap: make(map[string]string),
	}
}

// Register creates or updates a bot with full credentials (secret + Ed25519 keys).
func (s *BotStore) Register(appID, secret, initialToken string, expiresIn int) *Bot {
	s.mu.Lock()
	defer s.mu.Unlock()

	bot, ok := s.bots[appID]
	if ok {
		if bot.Secret != secret && secret != "" {
			bot.Secret = secret
			bot.PrivKey, bot.PubKey = DeriveKeyPair(secret)
			bot.TokenMgr = NewTokenManager(appID, secret)
			log.Printf("[store] bot %s updated with new secret", appID)
		}
	} else {
		privKey, pubKey := DeriveKeyPair(secret)
		bot = &Bot{
			AppID:    appID,
			Secret:   secret,
			PrivKey:  privKey,
			PubKey:   pubKey,
			TokenMgr: NewTokenManager(appID, secret),
			clients:  make(map[string]*Client),
		}
		s.bots[appID] = bot
		log.Printf("[store] bot %s registered", appID)
	}

	bot.TokenMgr.SetInitialToken(initialToken, expiresIn)

	s.tokenMu.Lock()
	s.tokenMap[initialToken] = appID
	s.tokenMu.Unlock()

	return bot
}

// RegisterLight registers a bot without secret (no Ed25519 keys).
func (s *BotStore) RegisterLight(appID, accessToken string) *Bot {
	s.mu.Lock()
	defer s.mu.Unlock()

	bot, ok := s.bots[appID]
	if !ok {
		bot = &Bot{
			AppID:    appID,
			TokenMgr: NewTokenManager(appID, ""),
			clients:  make(map[string]*Client),
		}
		s.bots[appID] = bot
		log.Printf("[store] bot %s registered (light)", appID)
	}

	bot.TokenMgr.mu.Lock()
	bot.TokenMgr.accessToken = accessToken
	bot.TokenMgr.mu.Unlock()

	s.tokenMu.Lock()
	s.tokenMap[accessToken] = appID
	s.tokenMu.Unlock()

	return bot
}

// StoreTokenMapping stores an access_token -> app_id mapping.
func (s *BotStore) StoreTokenMapping(appID, accessToken string) {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()

	s.tokenMap[accessToken] = appID
}

// LookupAppID returns the app_id for an access_token, or empty string.
func (s *BotStore) LookupAppID(accessToken string) string {
	s.tokenMu.RLock()
	defer s.tokenMu.RUnlock()

	return s.tokenMap[accessToken]
}

// Get returns a bot by app_id, or nil if not registered.
func (s *BotStore) Get(appID string) *Bot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.bots[appID]
}

// Stats returns bot count and total client count.
func (s *BotStore) Stats() (bots int, clients int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	bots = len(s.bots)
	for _, bot := range s.bots {
		bot.mu.RLock()
		clients += len(bot.clients)
		bot.mu.RUnlock()
	}

	return
}

// AllBotInfo returns summary info for all registered bots.
func (s *BotStore) AllBotInfo() []gin.H {
	s.mu.RLock()
	defer s.mu.RUnlock()

	info := make([]gin.H, 0, len(s.bots))
	for _, bot := range s.bots {
		bot.mu.RLock()
		info = append(info, gin.H{
			"app_id":    bot.AppID,
			"clients":   len(bot.clients),
			"has_token": bot.TokenMgr.Token() != "",
			"has_keys":  bot.HasKeys(),
		})
		bot.mu.RUnlock()
	}

	return info
}

// --- Bot client management ---

// AddClient registers a WebSocket connection to this bot.
func (b *Bot) AddClient(conn *websocket.Conn) *Client {
	client := &Client{
		ID:   generateID(),
		Conn: conn,
	}

	b.mu.Lock()
	b.clients[client.ID] = client
	b.mu.Unlock()

	log.Printf("[ws] bot=%s client=%s connected (total: %d)", b.AppID, client.ID, b.ClientCount())

	return client
}

// RemoveClient removes a WebSocket connection.
func (b *Bot) RemoveClient(id string) {
	b.mu.Lock()
	delete(b.clients, id)
	b.mu.Unlock()

	log.Printf("[ws] bot=%s client=%s disconnected (total: %d)", b.AppID, id, b.ClientCount())
}

// Broadcast sends data to all WebSocket clients of this bot.
func (b *Bot) Broadcast(data []byte) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, client := range b.clients {
		if err := client.WriteBytes(data); err != nil {
			log.Printf("[ws] bot=%s send to %s failed: %v", b.AppID, client.ID, err)
		}
	}
}

// ClientCount returns the number of connected clients.
func (b *Bot) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return len(b.clients)
}

// ForwardEvent builds the forwarded message (with access_token) and broadcasts.
func (b *Bot) ForwardEvent(payload WebhookPayload) {
	msg := map[string]interface{}{
		"op": payload.Op,
		"t":  payload.T,
		"d":  json.RawMessage(payload.D),
	}

	if payload.ID != "" {
		msg["id"] = payload.ID
	}

	if token := b.TokenMgr.Token(); token != "" {
		msg["access_token"] = token
	}

	data, _ := json.Marshal(msg)
	b.Broadcast(data)
}

func generateID() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)

	return fmt.Sprintf("%x", buf)
}

// jsonUnmarshal is a package-level helper.
func jsonUnmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
