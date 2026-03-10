// Package server retained for reference of previous in-memory implementation.
package server

import (
	"bubble/src/logger"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// --- Data Models ---
type User struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Token string `json:"token"`
}

type Guild struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	OwnerID int64  `json:"ownerId"`
}

type Channel struct {
	ID      int64  `json:"id"`
	GuildID int64  `json:"guildId"`
	Name    string `json:"name"`
}

type Message struct {
	ID        int64  `json:"id"`
	ChannelID int64  `json:"channelId"`
	AuthorID  int64  `json:"authorId"`
	Author    string `json:"author"`
	Content   string `json:"content"`
	// 下面是扩展字段，用于携带关联信息（公会/用户详情/mentions/附件等）
	GuildID     int64        `json:"guildId,omitempty"`
	AuthorInfo  *AuthorInfo  `json:"authorInfo,omitempty"`
	Mentions    []int64      `json:"mentions,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	CreatedAt   time.Time    `json:"createdAt"`
}

// AuthorInfo 包含更完整的作者信息，用于客户端展示
type AuthorInfo struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Token string `json:"token,omitempty"`
	IsBot bool   `json:"isBot,omitempty"`
}

// Attachment 表示消息所带附件的元信息
type Attachment struct {
	ID   string `json:"id,omitempty"`
	URL  string `json:"url,omitempty"`
	Type string `json:"type,omitempty"` // e.g., image, file, video
	Name string `json:"name,omitempty"`
	Size int64  `json:"size,omitempty"`
}

// --- In-memory Store ---
type Store struct {
	mu           sync.RWMutex
	nextUserID   int64
	nextGuildID  int64
	nextChanID   int64
	nextMsgID    int64
	usersByToken map[string]*User
	users        map[int64]*User
	guilds       map[int64]*Guild
	channels     map[int64]*Channel
	guildChans   map[int64][]int64
	messages     map[int64][]*Message // by channel id
}

func NewStore() *Store {
	return &Store{
		nextUserID:   1,
		nextGuildID:  1,
		nextChanID:   1,
		nextMsgID:    1,
		usersByToken: make(map[string]*User),
		users:        make(map[int64]*User),
		guilds:       make(map[int64]*Guild),
		channels:     make(map[int64]*Channel),
		guildChans:   make(map[int64][]int64),
		messages:     make(map[int64][]*Message),
	}
}

func (s *Store) CreateUser(name string) *User {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextUserID
	s.nextUserID++
	token := fmt.Sprintf("tok_%d_%d", id, time.Now().UnixNano())
	u := &User{ID: id, Name: name, Token: token}
	s.users[id] = u
	s.usersByToken[token] = u
	return u
}

func (s *Store) GetUserByToken(token string) *User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.usersByToken[token]
}

func (s *Store) CreateGuild(ownerID int64, name string) *Guild {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextGuildID
	s.nextGuildID++
	g := &Guild{ID: id, Name: name, OwnerID: ownerID}
	s.guilds[id] = g
	return g
}

func (s *Store) ListGuilds() []*Guild {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Guild, 0, len(s.guilds))
	for _, g := range s.guilds {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *Store) CreateChannel(guildID int64, name string) (*Channel, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.guilds[guildID]; !ok {
		return nil, false
	}
	id := s.nextChanID
	s.nextChanID++
	c := &Channel{ID: id, GuildID: guildID, Name: name}
	s.channels[id] = c
	s.guildChans[guildID] = append(s.guildChans[guildID], id)
	return c, true
}

func (s *Store) ListChannels(guildID int64) []*Channel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := s.guildChans[guildID]
	out := make([]*Channel, 0, len(ids))
	for _, id := range ids {
		if ch := s.channels[id]; ch != nil {
			out = append(out, ch)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// AddMessage creates a new message and stores it in memory. 额外支持 mentions 与 attachments。
func (s *Store) AddMessage(channelID, authorID int64, author string, content string, mentions []int64, attachments []Attachment) (*Message, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.channels[channelID]; !ok {
		return nil, false
	}
	id := s.nextMsgID
	s.nextMsgID++
	// 从 channel 中填充 GuildID（公会 id）以便消息包含关联信息
	var guildID int64
	if ch, ok := s.channels[channelID]; ok && ch != nil {
		guildID = ch.GuildID
	}
	m := &Message{
		ID:          id,
		ChannelID:   channelID,
		GuildID:     guildID,
		AuthorID:    authorID,
		Author:      author,
		Content:     content,
		Mentions:    mentions,
		Attachments: attachments,
		CreatedAt:   time.Now(),
	}
	s.messages[channelID] = append(s.messages[channelID], m)
	return m, true
}

func (s *Store) GetMessages(channelID int64, limit int) []*Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	msgs := s.messages[channelID]
	if limit > 0 && len(msgs) > limit {
		return append([]*Message(nil), msgs[len(msgs)-limit:]...)
	}
	return append([]*Message(nil), msgs...)
}

// --- WebSocket Hub per Channel ---
type Hub struct {
	mu        sync.RWMutex
	clients   map[*websocket.Conn]struct{}
	broadcast chan *Message
}

func NewHub() *Hub {
	return &Hub{
		clients:   make(map[*websocket.Conn]struct{}),
		broadcast: make(chan *Message, 64),
	}
}

func (h *Hub) Run() {
	for msg := range h.broadcast {
		h.mu.RLock()
		for c := range h.clients {
			c.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := c.WriteJSON(msg); err != nil {
				// drop the client on write error
				h.mu.RUnlock()
				h.mu.Lock()
				c.Close()
				delete(h.clients, c)
				h.mu.Unlock()
				h.mu.RLock()
			}
		}
		h.mu.RUnlock()
	}
}

func (h *Hub) Add(c *websocket.Conn) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) Remove(c *websocket.Conn) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		c.Close()
	}
	h.mu.Unlock()
}

// --- Server ---
type Server struct {
	store *Store
	hubs  map[int64]*Hub // channelID -> hub
	mu    sync.Mutex
}

func NewServer() *Server {
	return &Server{store: NewStore(), hubs: make(map[int64]*Hub)}
}

func (s *Server) getHub(channelID int64) *Hub {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.hubs[channelID]
	if h == nil {
		h = NewHub()
		s.hubs[channelID] = h
		go h.Run()
	}
	return h
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authUser(r *http.Request) *User {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return nil
	}
	parts := strings.SplitN(auth, " ", 2)
	var token string
	if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
		token = parts[1]
	} else {
		token = auth
	}
	return s.store.GetUserByToken(token)
}

// Handlers
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	type req struct {
		Name string `json:"name"`
	}
	var body req
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体格式错误"})
		return
	}
	u := s.store.CreateUser(body.Name)
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleCreateGuild(w http.ResponseWriter, r *http.Request) {
	u := s.authUser(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未认证"})
		return
	}
	type req struct {
		Name string `json:"name"`
	}
	var body req
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体格式错误"})
		return
	}
	g := s.store.CreateGuild(u.ID, body.Name)
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) handleListGuilds(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.ListGuilds())
}

func (s *Server) handleCreateChannel(w http.ResponseWriter, r *http.Request) {
	type req struct {
		GuildID int64  `json:"guildId"`
		Name    string `json:"name"`
	}
	u := s.authUser(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未认证"})
		return
	}
	var body req
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体格式错误"})
		return
	}
	if ch, ok := s.store.CreateChannel(body.GuildID, body.Name); !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "服务器不存在"})
	} else {
		writeJSON(w, http.StatusOK, ch)
	}
}

func (s *Server) handleListChannels(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("guildId")
	gid, _ := strconv.ParseInt(q, 10, 64)
	writeJSON(w, http.StatusOK, s.store.ListChannels(gid))
}

func (s *Server) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	cid, _ := strconv.ParseInt(r.URL.Query().Get("channelId"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	writeJSON(w, http.StatusOK, s.store.GetMessages(cid, limit))
}

func (s *Server) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	u := s.authUser(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未认证"})
		return
	}
	type req struct {
		ChannelID   int64        `json:"channelId"`
		Content     string       `json:"content"`
		Mentions    []int64      `json:"mentions,omitempty"`
		Attachments []Attachment `json:"attachments,omitempty"`
	}
	var body req
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Content) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体格式错误"})
		return
	}
	// 创建消息并广播
	if msg, ok := s.store.AddMessage(body.ChannelID, u.ID, u.Name, body.Content, body.Mentions, body.Attachments); ok {
		// 附带更完整的作者信息
		msg.AuthorInfo = &AuthorInfo{ID: u.ID, Name: u.Name, Token: u.Token}
		s.getHub(body.ChannelID).broadcast <- msg
		writeJSON(w, http.StatusOK, msg)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "频道不存在"})
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	// Query: channelId, token
	cid, _ := strconv.ParseInt(r.URL.Query().Get("channelId"), 10, 64)
	token := r.URL.Query().Get("token")
	u := s.store.GetUserByToken(token)
	if u == nil || cid == 0 {
		http.Error(w, "未认证或频道不合法", http.StatusUnauthorized)
		return
	}
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Info("upgrade:", err)
		return
	}
	hub := s.getHub(cid)
	hub.Add(c)

	// Send recent messages upon connect
	recent := s.store.GetMessages(cid, 50)
	for _, m := range recent {
		c.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := c.WriteJSON(m); err != nil {
			hub.Remove(c)
			return
		}
	}

	// Reader loop, if client sends JSON {content:string} -> post message
	type inbound struct {
		Content     string       `json:"content"`
		Mentions    []int64      `json:"mentions,omitempty"`
		Attachments []Attachment `json:"attachments,omitempty"`
	}
	for {
		var in inbound
		if err := c.ReadJSON(&in); err != nil {
			hub.Remove(c)
			return
		}
		if strings.TrimSpace(in.Content) == "" {
			continue
		}
		if msg, ok := s.store.AddMessage(cid, u.ID, u.Name, in.Content, in.Mentions, in.Attachments); ok {
			msg.AuthorInfo = &AuthorInfo{ID: u.ID, Name: u.Name, Token: u.Token}
			hub.broadcast <- msg
		}
	}
}

// Routes exposes the HTTP routes without serving any static frontend.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/guilds", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.handleListGuilds(w, r)
		case http.MethodPost:
			s.handleCreateGuild(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/channels", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.handleListChannels(w, r)
		case http.MethodPost:
			s.handleCreateChannel(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/messages", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.handleGetMessages(w, r)
		case http.MethodPost:
			s.handlePostMessage(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/ws", s.handleWS)
	// 兼容旧版本路径
	mux.HandleFunc("/ws", s.handleWS)

	// Note: frontend/static serving removed on purpose.
	// Any "/" route will 404 unless handled by reverse proxy or separate frontend project.

	return s.withCORS(mux)
}
