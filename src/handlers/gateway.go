package handlers

import (
	"encoding/json"
	"fmt"

	"bubble/src/logger"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"bubble/src/config"
	"bubble/src/db/models"
	"bubble/src/service"

	"context"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

// 特殊事件白名单使用集中配置

// WebSocket upgrader
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Discord-like minimal gateway op codes are centralized in config

type gwFrame struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d,omitempty"`
	S  *uint64         `json:"s,omitempty"`
	T  *string         `json:"t,omitempty"`
}

type helloPayload struct {
	HeartbeatInterval int `json:"heartbeat_interval"`
}

type subscribePayload struct {
	ChannelID uint     `json:"channelId"` // 用户协议使用
	ThreadID  uint     `json:"threadId"`  // 用户协议使用
	GuildID   uint     `json:"guildId"`   // 机器人协议扩展 - 订阅整个服务器的频道
	Events    []string `json:"events"`    // 机器人协议使用 - 订阅事件类型
}

// Voice call signaling payloads
type voiceSignalPayload struct {
	Type          string  `json:"type"` // offer, answer, ice_candidate, hangup
	ToUserID      uint    `json:"toUserId"`
	ThreadID      uint    `json:"threadId,omitempty"`
	IsVideoCall   *bool   `json:"isVideoCall,omitempty"`
	SDP           *string `json:"sdp,omitempty"`
	Candidate     *string `json:"candidate,omitempty"`
	SDPMLineIndex *int    `json:"sdpMLineIndex,omitempty"`
	SDPMid        *string `json:"sdpMid,omitempty"`
}

type voiceCallIncomingPayload struct {
	FromUserID   uint   `json:"fromUserId"`
	FromUserName string `json:"fromUserName"`
	ThreadID     uint   `json:"threadId"`
	IsVideoCall  bool   `json:"isVideoCall"`
}

type voiceCallAnswerPayload struct {
	FromUserID uint   `json:"fromUserId"`
	ThreadID   uint   `json:"threadId"`
	SDP        string `json:"sdp"`
}

type voiceCallICEPayload struct {
	FromUserID    uint   `json:"fromUserId"`
	ThreadID      uint   `json:"threadId"`
	Candidate     string `json:"candidate"`
	SDPMLineIndex int    `json:"sdpMLineIndex"`
	SDPMid        string `json:"sdpMid"`
}

type voiceCallHangupPayload struct {
	FromUserID uint `json:"fromUserId"`
	ThreadID   uint `json:"threadId"`
}

// READY payload dispatched after successful connection
type readyPayload struct {
	User      models.User       `json:"user"`
	Friends   []models.User     `json:"friends"`
	DmThreads []models.DmThread `json:"dmThreads"`
}

// READY_SYNC payload for paginated bootstrap data
type readySyncPayload struct {
	ResourceType string      `json:"resource_type"`
	Items        interface{} `json:"items"`
	Page         int         `json:"page"`
	HasMore      bool        `json:"has_more"`
	CreatedAt    time.Time   `json:"created_at"`
	CreatedTS    int64       `json:"created_ts"`
}

type gwConn struct {
	c            *websocket.Conn
	userID       uint
	lastHB       int64
	subs         map[uint]struct{} // channel ids (kept for compatibility)
	subsDm       map[uint]struct{} // dm thread ids
	botEventSubs map[string]bool   // 机器人事件类型订阅 (event_type -> enabled)
	// simple token-bucket for inbound frames (anti-spam)
	rlTokens float64
	rlLast   time.Time
	// 写互斥锁：gorilla/websocket 不支持并发写入，必须串行化
	wMu sync.Mutex
	// 连接状态标志，防止重复 disconnect
	disconnected int32 // 使用原子操作
}

type Gateway struct {
	svc        *service.Service
	hbInterval time.Duration
	mu         sync.RWMutex
	// channel subscriptions
	subs map[uint]map[*gwConn]struct{}
	// guild-level subscriptions (for events like member add/remove etc.)
	guildSubs map[uint]map[*gwConn]struct{}
	// dm subscriptions
	dmSubs map[uint]map[*gwConn]struct{}
	// user connections (userID -> set of connections)
	userConns map[uint]map[*gwConn]struct{}
	seq       uint64
	// graceful server reference for connection tracking
	gracefulServer interface {
		IncrementWSConn() int64
		DecrementWSConn() int64
		IsShuttingDown() bool
	}
	// Redis Pub/Sub
	rdb       *redis.Client
	topicSubs map[string]*redis.PubSub
}

func NewGateway(svc *service.Service) *Gateway {
	return &Gateway{
		svc:        svc,
		hbInterval: time.Duration(config.DefaultHeartbeatInterval) * time.Second,
		subs:       make(map[uint]map[*gwConn]struct{}),
		guildSubs:  make(map[uint]map[*gwConn]struct{}),
		dmSubs:     make(map[uint]map[*gwConn]struct{}),
		userConns:  make(map[uint]map[*gwConn]struct{}),
		rdb: func() *redis.Client {
			if svc != nil && svc.Repo != nil {
				return svc.Repo.Redis
			}
			return nil
		}(),
		topicSubs: make(map[string]*redis.PubSub),
	}
}

// SetGracefulServer 设置优雅关闭服务器引用（用于连接跟踪）
func (g *Gateway) SetGracefulServer(gs interface {
	IncrementWSConn() int64
	DecrementWSConn() int64
	IsShuttingDown() bool
}) {
	g.gracefulServer = gs
}

func (g *Gateway) connect(c *Context) {
	// 检查是否为移动端客户端
	isMobile := strings.EqualFold(c.Request.Header.Get("X-Client-Type"), "mobile") ||
		c.Request.URL.Query().Get("clientType") == "mobile"

	var session *models.Session
	var u *models.User
	var err error

	if isMobile {
		// 移动端认证：强制使用短期 Access Token（JWT）进行 WS 连接。
		// Access Token 过期后，客户端应调用 /api/mobile/refresh 使用 Refresh Token 刷新，再重连。

		// 获取 Access Token（支持多种方式）
		accessToken := ""

		// 1. 优先从 Authorization Header 获取（标准方式）
		authHeader := c.Request.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			accessToken = strings.TrimPrefix(authHeader, "Bearer ")
		}

		// 2. 降级到 URL 参数（兼容浏览器环境，如 Wails）
		if accessToken == "" {
			accessToken = c.Request.URL.Query().Get("token")
		}

		// 3. 最后尝试 X-Access-Token Header
		if accessToken == "" {
			accessToken = c.Request.Header.Get("X-Access-Token")
		}

		if accessToken == "" {
			logger.Warn("[Gateway] Mobile client: missing access token")
			http.Error(c.Writer, "未认证：缺少访问令牌", http.StatusUnauthorized)
			return
		}

		userID, err := g.svc.ParseAccessToken(accessToken)
		if err != nil || userID == 0 {
			logger.Warnf("[Gateway] Mobile client: invalid or expired access token: %v", err)
			http.Error(c.Writer, "访问令牌无效或已过期，请刷新后重试", http.StatusUnauthorized)
			return
		}

		u, err = g.svc.GetUserByID(userID)
		if err != nil || u == nil {
			logger.Warnf("[Gateway] Mobile client: user not found for ID %d", userID)
			http.Error(c.Writer, "未认证", http.StatusUnauthorized)
			return
		}
	} else {
		// Web 端认证：使用 Cookie
		// 1. 优先验证 Session (确保撤回立即生效)
		sessionCookie, err := c.Request.Cookie(g.svc.Cfg.SessionCookieName)
		if err != nil || sessionCookie == nil || sessionCookie.Value == "" {
			logger.Warn("[Gateway] No session cookie found, rejecting connection")
			http.Error(c.Writer, "未认证", http.StatusUnauthorized)
			return
		}

		// 2. 验证 Session (Session是最终权威)
		session, err = g.svc.ValidateSession(sessionCookie.Value)
		if err != nil || session == nil {
			logger.Warnf("[Gateway] Invalid or revoked session: %v", err)
			http.Error(c.Writer, "会话已被撤销", http.StatusUnauthorized)
			return
		}

		// 3. 获取用户
		u, err = g.svc.GetUserByID(session.UserID)
		if err != nil || u == nil {
			logger.Warnf("[Gateway] User not found for ID %d", session.UserID)
			http.Error(c.Writer, "未认证", http.StatusUnauthorized)
			return
		}
	}

	// 检查服务器是否正在关闭（在升级前检查，避免无效升级）
	if g.gracefulServer != nil && g.gracefulServer.IsShuttingDown() {
		http.Error(c.Writer, "服务器正在关闭", http.StatusServiceUnavailable)
		return
	}

	// 升级到 WebSocket
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Errorf("[Gateway] WebSocket upgrade failed: %v", err)
		return
	}

	// 升级成功后增加 WebSocket 连接计数
	// 注意：必须在升级成功后计数，失败时不计数
	if g.gracefulServer != nil {
		g.gracefulServer.IncrementWSConn()
	}

	gc := &gwConn{
		c:            conn,
		userID:       u.ID,
		subs:         make(map[uint]struct{}),
		subsDm:       make(map[uint]struct{}),
		lastHB:       time.Now().UnixMilli(),
		rlTokens:     10,
		rlLast:       time.Now(),
		disconnected: 0,
	}

	// 注册用户连接
	g.mu.Lock()
	if g.userConns[u.ID] == nil {
		g.userConns[u.ID] = make(map[*gwConn]struct{})
	}
	g.userConns[u.ID][gc] = struct{}{}
	g.mu.Unlock()

	// 发送 HELLO
	g.write(conn, gwFrame{Op: config.OpHello, D: mustJSON(helloPayload{HeartbeatInterval: int(g.hbInterval.Milliseconds())})})

	// 立即发送 READY（用户已验证）
	// 获取好友和私信线程（Gateway READY 不支持分页）
	friendsPtr, _, _ := g.svc.ListFriends(u.ID, 1, config.DefaultReadyFriendsLimit)
	threadsPtr, _, _ := g.svc.ListDmThreads(u.ID, 1, config.DefaultReadyDmThreadsLimit)
	// convert []*T -> []T for payload compatibility
	friends := make([]models.User, len(friendsPtr))
	for i, f := range friendsPtr {
		if f != nil {
			friends[i] = *f
		}
	}
	threads := make([]models.DmThread, len(threadsPtr))
	for i, t := range threadsPtr {
		if t != nil {
			threads[i] = *t
		}
	}
	g.dispatch(gc, config.EventReady, readyPayload{User: *u, Friends: friends, DmThreads: threads})

	// 订阅用户级 topic，这样用户无需逐线程订阅也能接收属于自己的私聊事件
	g.ensureSubscribeTopic(context.Background(), topicForUser(u.ID))

	// 异步发送 READY_SYNC 分批数据
	go g.sendReadySyncBatches(gc, u)

	// readers
	go g.readLoop(gc)
	// liveness monitor
	go g.liveness(gc)
}

// sendReadySyncBatches streams paginated bootstrap datasets to the client
func (g *Gateway) sendReadySyncBatches(gc *gwConn, u *models.User) {
	const batchSize = 50
	now := time.Now()
	ts := now.UnixMilli()

	// user (完整用户信息，包含 email, emailVerified, isPrivate 等所有字段)
	{
		g.dispatch(gc, config.EventReadySync, readySyncPayload{
			ResourceType: "user",
			Items:        []models.User{*u},
			Page:         1,
			HasMore:      false,
			CreatedAt:    now,
			CreatedTS:    ts,
		})
	}

	// friends (single batch: latest ≤50)
	{
		friendsPtr, _, err := g.svc.ListFriends(u.ID, 1, batchSize)
		if err == nil {
			friends := make([]models.User, len(friendsPtr))
			for i, f := range friendsPtr {
				if f != nil {
					friends[i] = *f
				}
			}
			g.dispatch(gc, config.EventReadySync, readySyncPayload{
				ResourceType: "friends",
				Items:        friends,
				Page:         1,
				HasMore:      false,
				CreatedAt:    now,
				CreatedTS:    ts,
			})
		}
	}

	// dm_threads (single batch: latest ≤50)
	{
		threadsPtr, _, err := g.svc.ListDmThreads(u.ID, 1, batchSize)
		if err == nil {
			threads := make([]models.DmThread, len(threadsPtr))
			for i, t := range threadsPtr {
				if t != nil {
					threads[i] = *t
					if threads[i].Mentions == nil {
						threads[i].Mentions = make([]map[string]any, 0)
					}
				}
			}
			g.dispatch(gc, config.EventReadySync, readySyncPayload{
				ResourceType: "dm_threads",
				Items:        threads,
				Page:         1,
				HasMore:      false,
				CreatedAt:    now,
				CreatedTS:    ts,
			})
		}
	}

	// 预留：blacklist, notices, applications, moments, my_moments, guilds(+perms)
	// blacklist (single batch)
	if bl, err := g.svc.ListBlacklist(u.ID); err == nil {
		g.dispatch(gc, config.EventReadySync, readySyncPayload{
			ResourceType: "blacklist",
			Items:        bl,
			Page:         1,
			HasMore:      false,
			CreatedAt:    now,
			CreatedTS:    ts,
		})
	}

	// notifications (single batch, default limit 50) - 带完整关联信息
	if notifs, users, guilds, channels, messages, dmMessages, err := g.svc.ListUserNotificationsWithInfo(u.ID, 50, 0, 0); err == nil {
		// 构建完整的通知数据
		notifItems := make([]map[string]any, len(notifs))
		for i, n := range notifs {
			item := map[string]any{
				"id":         n.ID,
				"userId":     n.UserID,
				"type":       n.Type,
				"sourceType": n.SourceType,
				"read":       n.Read,
				"createdAt":  n.CreatedAt,
			}

			if n.GuildID != nil {
				item["guildId"] = *n.GuildID
				if guild, ok := guilds[*n.GuildID]; ok {
					item["guild"] = map[string]any{
						"id":     guild.ID,
						"name":   guild.Name,
						"avatar": guild.Avatar,
					}
				}
			}

			if n.ChannelID != nil {
				item["channelId"] = *n.ChannelID
				if channel, ok := channels[*n.ChannelID]; ok {
					item["channel"] = map[string]any{
						"id":   channel.ID,
						"name": channel.Name,
						"type": channel.Type,
					}
				}
			}

			if n.ThreadID != nil {
				item["threadId"] = *n.ThreadID
			}

			if n.MessageID != nil {
				item["messageId"] = *n.MessageID
			}

			if n.AuthorID != nil {
				item["authorId"] = *n.AuthorID
				if author, ok := users[*n.AuthorID]; ok {
					item["author"] = map[string]any{
						"id":     author.ID,
						"name":   author.Name,
						"avatar": author.Avatar,
					}
				}
			}

			// 添加消息内容（如果有）
			if n.MessageID != nil {
				if n.SourceType == "channel" {
					if msg, ok := messages[*n.MessageID]; ok {
						item["message"] = map[string]any{
							"id":      msg.ID,
							"content": msg.Content,
							"type":    msg.Type,
						}
					}
				} else if n.SourceType == "dm" {
					if msg, ok := dmMessages[*n.MessageID]; ok {
						item["message"] = map[string]any{
							"id":      msg.ID,
							"content": msg.Content,
							"type":    msg.Type,
						}
					}
				}
			}

			// 添加通知状态（用于好友申请等）
			if n.Status != nil {
				item["status"] = *n.Status
			}

			notifItems[i] = item
		}

		g.dispatch(gc, config.EventReadySync, readySyncPayload{
			ResourceType: "notices",
			Items:        notifItems,
			Page:         1,
			HasMore:      false,
			CreatedAt:    now,
			CreatedTS:    ts,
		})
	}

	// applications (single batch, default limit 50)
	if apps, err := g.svc.ListUserApplications(u.ID, 50, 0, 0); err == nil {
		// 填充前端兼容字段
		for i := range apps {
			apps[i].UserID = apps[i].FromUserID
			apps[i].GuildID = apps[i].TargetGuildID
		}
		g.dispatch(gc, config.EventReadySync, readySyncPayload{
			ResourceType: "applications",
			Items:        apps,
			Page:         1,
			HasMore:      false,
			CreatedAt:    now,
			CreatedTS:    ts,
		})
	}

	// guilds (single batch, filtered by user, with effective permissions)
	if guilds, err := g.svc.ListUserGuilds(u.ID); err == nil {
		// assemble payload items with perms
		items := make([]map[string]any, 0, len(guilds))

		// 批量查询用户在各服务器的免打扰状态
		guildIDs := make([]uint, len(guilds))
		for i, gld := range guilds {
			guildIDs[i] = gld.ID
		}
		var members []models.GuildMember
		g.svc.Repo.DB.Where("user_id = ? AND guild_id IN ?", u.ID, guildIDs).Find(&members)
		mutedMap := make(map[uint]bool)
		for _, m := range members {
			mutedMap[m.GuildID] = m.NotifyMuted
		}

		for _, gld := range guilds {
			perms, perr := g.svc.EffectiveGuildPerms(gld.ID, u.ID)
			if perr != nil {
				perms = 0
			}
			items = append(items, map[string]any{
				"guild":       gld,
				"permissions": perms,
				"notifyMuted": mutedMap[gld.ID],
			})
		}
		g.dispatch(gc, config.EventReadySync, readySyncPayload{
			ResourceType: "guilds",
			Items:        items,
			Page:         1,
			HasMore:      false,
			CreatedAt:    now,
			CreatedTS:    ts,
		})
	}

	// moments timeline (single batch: latest ≤50)
	if items, _, err := g.svc.ListTimelineMoments(u.ID, 1, batchSize); err == nil {
		// 批量加载用户信息
		momentUserIDs := make(map[uint]bool)
		for _, m := range items {
			momentUserIDs[m.UserID] = true
		}
		momentUserMap := make(map[uint]*models.User)
		if len(momentUserIDs) > 0 {
			uids := make([]uint, 0, len(momentUserIDs))
			for uid := range momentUserIDs {
				uids = append(uids, uid)
			}
			var mUsers []models.User
			if err := g.svc.Repo.DB.Where("id IN ?", uids).Find(&mUsers).Error; err == nil {
				for i := range mUsers {
					momentUserMap[mUsers[i].ID] = &mUsers[i]
				}
			}
		}
		// 填充前端兼容字段
		for i := range items {
			items[i].AuthorId = items[i].UserID
			items[i].CommentsCount = items[i].CommentCount
			items[i].CreatedTs = items[i].CreatedAt.UnixMilli()
			items[i].User = momentUserMap[items[i].UserID]
		}
		g.dispatch(gc, config.EventReadySync, readySyncPayload{
			ResourceType: "moments",
			Items:        items,
			Page:         1,
			HasMore:      false,
			CreatedAt:    now,
			CreatedTS:    ts,
		})
	}

	// my_moments (single batch: latest ≤50)
	if items, _, err := g.svc.ListMyMoments(u.ID, 1, batchSize); err == nil {
		// 填充前端兼容字段
		for i := range items {
			items[i].AuthorId = items[i].UserID
			items[i].CommentsCount = items[i].CommentCount
			items[i].CreatedTs = items[i].CreatedAt.UnixMilli()
			items[i].User = u
		}
		g.dispatch(gc, config.EventReadySync, readySyncPayload{
			ResourceType: "my_moments",
			Items:        items,
			Page:         1,
			HasMore:      false,
			CreatedAt:    now,
			CreatedTS:    ts,
		})
	}

	// group_threads (single batch: latest ≤50)
	if threads, err := g.svc.ListUserGroupThreads(u.ID, batchSize, 0, 0); err == nil {
		g.dispatch(gc, config.EventReadySync, readySyncPayload{
			ResourceType: "group_threads",
			Items:        threads,
			Page:         1,
			HasMore:      false,
			CreatedAt:    now,
			CreatedTS:    ts,
		})
	}
}

// Route registration moved to http.go to avoid duplicate registration
// The Register method has been removed - routes are now registered directly in http.go

type Context struct {
	Writer  http.ResponseWriter
	Request *http.Request
}

func (g *Gateway) readLoop(gc *gwConn) {
	for {
		var f gwFrame
		if err := gc.c.ReadJSON(&f); err != nil {
			g.disconnect(gc)
			return
		}
		// inbound rate limit except heartbeat
		if f.Op != config.OpHeartbeat && !allowFrame(gc, config.UserRateLimitRPS, config.UserRateLimitBurst) {
			g.disconnect(gc)
			return
		}
		switch f.Op {
		case config.OpHeartbeat:
			gc.lastHB = time.Now().UnixMilli()
			g.writeGC(gc, gwFrame{Op: config.OpHeartbeatAck})
		// IDENTIFY 已移除：连接时已通过 Cookie 验证，无需再次验证
		case config.OpSubscribe:
			var p subscribePayload
			_ = json.Unmarshal(f.D, &p)
			// 权限校验与订阅
			if p.ChannelID != 0 {
				ch, err := g.svc.GetChannel(p.ChannelID)
				if err != nil || ch == nil {
					g.dispatch(gc, config.EventSubscribeDenied, map[string]any{"resource": "channel", "id": p.ChannelID, "reason": "not_found"})
					break
				}
				ok, perr := g.svc.HasGuildPerm(ch.GuildID, gc.userID, service.PermViewChannel)
				if perr != nil || !ok {
					g.dispatch(gc, config.EventSubscribeDenied, map[string]any{"resource": "channel", "id": p.ChannelID, "reason": "forbidden"})
					break
				}
				g.mu.Lock()
				if g.subs[p.ChannelID] == nil {
					g.subs[p.ChannelID] = make(map[*gwConn]struct{})
				}
				g.subs[p.ChannelID][gc] = struct{}{}
				gc.subs[p.ChannelID] = struct{}{}
				g.mu.Unlock()
				logger.Infof("[Gateway] User %d subscribed to channel %d", gc.userID, p.ChannelID)
				// ensure Redis subscription for this channel
				g.ensureSubscribeTopic(context.Background(), topicForChannel(p.ChannelID))
			}
			if p.ThreadID != 0 {
				th, err := g.svc.Repo.GetDmThread(p.ThreadID)
				if err != nil || th == nil {
					g.dispatch(gc, config.EventSubscribeDenied, map[string]any{"resource": "dm", "id": p.ThreadID, "reason": "not_found"})
					break
				}
				if th.UserAID != gc.userID && th.UserBID != gc.userID {
					g.dispatch(gc, config.EventSubscribeDenied, map[string]any{"resource": "dm", "id": p.ThreadID, "reason": "forbidden"})
					break
				}
				g.mu.Lock()
				if g.dmSubs[p.ThreadID] == nil {
					g.dmSubs[p.ThreadID] = make(map[*gwConn]struct{})
				}
				g.dmSubs[p.ThreadID][gc] = struct{}{}
				gc.subsDm[p.ThreadID] = struct{}{}
				g.mu.Unlock()
				logger.Infof("[Gateway] User %d subscribed to DM thread %d", gc.userID, p.ThreadID)
				// ensure Redis subscription for this DM thread
				g.ensureSubscribeTopic(context.Background(), topicForDM(p.ThreadID))
			}
			if p.GuildID != 0 {
				// guild-level subscription (robots/admins may use this)
				ok, perr := g.svc.HasGuildPerm(p.GuildID, gc.userID, service.PermViewChannel)
				if perr != nil || !ok {
					g.dispatch(gc, config.EventSubscribeDenied, map[string]any{"resource": "guild", "id": p.GuildID, "reason": "forbidden"})
					break
				}
				g.mu.Lock()
				if g.guildSubs[p.GuildID] == nil {
					g.guildSubs[p.GuildID] = make(map[*gwConn]struct{})
				}
				g.guildSubs[p.GuildID][gc] = struct{}{}
				g.mu.Unlock()
				logger.Infof("[Gateway] User %d subscribed to guild %d", gc.userID, p.GuildID)
				// ensure Redis subscription for this guild
				g.ensureSubscribeTopic(context.Background(), topicForGuild(p.GuildID))
			}
		case config.OpUnsubscribe:
			var p subscribePayload
			_ = json.Unmarshal(f.D, &p)
			if p.ChannelID != 0 {
				g.mu.Lock()
				if set := g.subs[p.ChannelID]; set != nil {
					delete(set, gc)
					if len(set) == 0 {
						delete(g.subs, p.ChannelID)
					}
				}
				delete(gc.subs, p.ChannelID)
				g.mu.Unlock()
				logger.Infof("[Gateway] User %d unsubscribed from channel %d", gc.userID, p.ChannelID)
			}
			if p.ThreadID != 0 {
				g.mu.Lock()
				if set := g.dmSubs[p.ThreadID]; set != nil {
					delete(set, gc)
					if len(set) == 0 {
						delete(g.dmSubs, p.ThreadID)
					}
				}
				delete(gc.subsDm, p.ThreadID)
				g.mu.Unlock()
				logger.Infof("[Gateway] User %d unsubscribed from DM thread %d", gc.userID, p.ThreadID)
			}
			if p.GuildID != 0 {
				g.mu.Lock()
				if set := g.guildSubs[p.GuildID]; set != nil {
					delete(set, gc)
					if len(set) == 0 {
						delete(g.guildSubs, p.GuildID)
					}
				}
				g.mu.Unlock()
				logger.Infof("[Gateway] User %d unsubscribed from guild %d", gc.userID, p.GuildID)
			}
		case config.OpVoiceSignal:
			var p voiceSignalPayload
			if err := json.Unmarshal(f.D, &p); err != nil {
				logger.Errorf("[Gateway] Failed to parse voice signal: %v", err)
				continue
			}
			g.handleVoiceSignal(gc, &p)
		case config.OpChannelVoiceJoin:
			g.handleChannelVoiceJoin(gc, f.D)
		case config.OpChannelVoiceLeave:
			g.handleChannelVoiceLeave(gc, f.D)
		case config.OpChannelVoiceUpdate:
			g.handleChannelVoiceUpdate(gc, f.D)
		case config.OpDmCallStart:
			g.handleDmCallStart(gc, f.D)
		case config.OpDmCallAccept:
			g.handleDmCallAccept(gc, f.D)
		case config.OpDmCallReject:
			g.handleDmCallReject(gc, f.D)
		case config.OpDmCallCancel:
			g.handleDmCallCancel(gc, f.D)
		case config.OpQRCodeRequest:
			g.handleQRCodeRequest(gc, f.D)
		}
	}
}

func (g *Gateway) liveness(gc *gwConn) {
	ticker := time.NewTicker(g.hbInterval)
	defer ticker.Stop()
	for range ticker.C {
		if time.Since(time.UnixMilli(gc.lastHB)) > 2*g.hbInterval {
			g.disconnect(gc)
			return
		}
	}
}

func (g *Gateway) disconnect(gc *gwConn) {
	// 使用原子操作确保只执行一次 disconnect
	if !atomic.CompareAndSwapInt32(&gc.disconnected, 0, 1) {
		// 已经断开，直接返回
		return
	}

	// 先清理订阅关系
	g.mu.Lock()
	for ch := range gc.subs {
		if set := g.subs[ch]; set != nil {
			delete(set, gc)
			if len(set) == 0 {
				delete(g.subs, ch)
			}
		}
	}
	for th := range gc.subsDm {
		if set := g.dmSubs[th]; set != nil {
			delete(set, gc)
			if len(set) == 0 {
				delete(g.dmSubs, th)
			}
		}
	}
	// remove from user connections
	if gc.userID != 0 {
		if set := g.userConns[gc.userID]; set != nil {
			delete(set, gc)
			if len(set) == 0 {
				delete(g.userConns, gc.userID)
			}
		}
	}
	g.mu.Unlock()

	// 关闭连接
	if gc.c != nil {
		gc.c.Close()
	}

	// 最后减少 WebSocket 连接计数（确保在连接关闭后）
	if g.gracefulServer != nil {
		g.gracefulServer.DecrementWSConn()
	}
}

func (g *Gateway) write(c *websocket.Conn, f gwFrame) {
	_ = g.writeConn(nil, c, f)
}

// writeGC 使用 gwConn 的写互斥锁安全地写入 WebSocket 帧，写入失败时主动断开连接。
func (g *Gateway) writeGC(gc *gwConn, f gwFrame) bool {
	gc.wMu.Lock()
	gc.c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err := gc.c.WriteJSON(f)
	gc.wMu.Unlock()
	if err != nil {
		logger.Warnf("[Gateway] Write failed for user %d: %v", gc.userID, err)
		g.disconnect(gc)
		return false
	}
	return true
}

// writeConn 带可选 gwConn 的写入。如果 gc 非 nil 则使用其互斥锁。
func (g *Gateway) writeConn(gc *gwConn, c *websocket.Conn, f gwFrame) error {
	if gc != nil {
		gc.wMu.Lock()
		defer gc.wMu.Unlock()
	}
	c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.WriteJSON(f)
}

func (g *Gateway) dispatch(gc *gwConn, event string, payload any) {
	// 检查是否应该发送该事件
	if !shouldDispatchEvent(gc, event) {
		return
	}

	seq := atomic.AddUint64(&g.seq, 1)
	s := seq
	t := event
	g.writeGC(gc, gwFrame{Op: config.OpDispatch, D: mustJSON(payload), S: &s, T: &t})
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// NOTE: per-thread auto-subscribe for normal users removed. Gateway now subscribes to `user:` topic on connect.

// ===== Redis Pub/Sub helpers =====
type pubSubEnvelope struct {
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload"`
	// optional direct user broadcast
	UserIDs []uint `json:"userIds,omitempty"`
	// 排除机器人自身连接（防止机器人收到自己发的消息）
	ExcludeBotUserID uint `json:"excludeBotUserId,omitempty"`
}

func topicForChannel(channelID uint) string {
	return config.TopicPrefixChannel + strconvFormatUint(channelID)
}
func topicForDM(threadID uint) string   { return config.TopicPrefixDM + strconvFormatUint(threadID) }
func topicForUser(userID uint) string   { return config.TopicPrefixUser + strconvFormatUint(userID) }
func topicForGuild(guildID uint) string { return config.TopicPrefixGuild + strconvFormatUint(guildID) }
func strconvFormatUint(v uint) string   { return strconv.FormatUint(uint64(v), 10) }

func (g *Gateway) ensureSubscribeTopic(ctx context.Context, topic string) {
	if g.rdb == nil {
		return
	}
	g.mu.Lock()
	if g.topicSubs[topic] != nil {
		g.mu.Unlock()
		return
	}
	sub := g.rdb.Subscribe(ctx, topic)
	g.topicSubs[topic] = sub
	g.mu.Unlock()
	// start reader with auto-reconnect
	go g.pubSubReader(topic)
}

func parseUint(s string) uint {
	u64, _ := strconv.ParseUint(s, 10, 64)
	return uint(u64)
}

// pubSubReader reads messages for a topic and auto-reconnects on channel close.
func (g *Gateway) pubSubReader(topic string) {
	backoff := time.Second
	maxBackoff := 30 * time.Second
	for {
		g.mu.Lock()
		sub := g.topicSubs[topic]
		g.mu.Unlock()
		if sub == nil {
			if g.rdb == nil {
				return
			}
			sub = g.rdb.Subscribe(context.Background(), topic)
			g.mu.Lock()
			g.topicSubs[topic] = sub
			g.mu.Unlock()
		}
		ch := sub.Channel()
		for {
			msg, ok := <-ch
			if !ok {
				logger.Warnf("[Gateway] PubSub channel closed for topic %s, reconnecting...", topic)
				if err := sub.Close(); err != nil {
					logger.Warnf("[Gateway] PubSub close error: %v", err)
				}
				time.Sleep(backoff)
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				sub = g.rdb.Subscribe(context.Background(), topic)
				g.mu.Lock()
				g.topicSubs[topic] = sub
				g.mu.Unlock()
				ch = sub.Channel()
				continue
			}
			backoff = time.Second
			var env pubSubEnvelope
			if err := json.Unmarshal([]byte(msg.Payload), &env); err != nil {
				logger.Errorf("[Gateway] PubSub unmarshal error: %v", err)
				continue
			}
			g.mu.RLock()
			var conns []*gwConn
			if len(topic) > len(config.TopicPrefixChannel) && topic[:len(config.TopicPrefixChannel)] == config.TopicPrefixChannel {
				idStr := topic[len(config.TopicPrefixChannel):]
				id := parseUint(idStr)
				if set := g.subs[id]; set != nil {
					conns = make([]*gwConn, 0, len(set))
					for c := range set {
						conns = append(conns, c)
					}
				}
			} else if len(topic) > len(config.TopicPrefixDM) && topic[:len(config.TopicPrefixDM)] == config.TopicPrefixDM {
				idStr := topic[len(config.TopicPrefixDM):]
				id := parseUint(idStr)
				if set := g.dmSubs[id]; set != nil {
					conns = make([]*gwConn, 0, len(set))
					for c := range set {
						conns = append(conns, c)
					}
				}
			} else if len(topic) > len(config.TopicPrefixUser) && topic[:len(config.TopicPrefixUser)] == config.TopicPrefixUser {
				idStr := topic[len(config.TopicPrefixUser):]
				id := parseUint(idStr)
				if set := g.userConns[id]; set != nil {
					conns = make([]*gwConn, 0, len(set))
					for c := range set {
						conns = append(conns, c)
					}
				}
			} else if len(topic) > len(config.TopicPrefixGuild) && topic[:len(config.TopicPrefixGuild)] == config.TopicPrefixGuild {
				idStr := topic[len(config.TopicPrefixGuild):]
				id := parseUint(idStr)
				if set := g.guildSubs[id]; set != nil {
					conns = make([]*gwConn, 0, len(set))
					for c := range set {
						conns = append(conns, c)
					}
				}
			}
			g.mu.RUnlock()
			if len(conns) == 0 {
				continue
			}
			seq := atomic.AddUint64(&g.seq, 1)
			s := seq
			t := env.Event
			frame := gwFrame{Op: config.OpDispatch, D: env.Payload, S: &s, T: &t}
			for _, c := range conns {
				// 跳过机器人自身连接（防止机器人收到自己发的消息）
				if env.ExcludeBotUserID != 0 && c.botEventSubs != nil && c.userID == env.ExcludeBotUserID {
					continue
				}
				// 检查机器人事件订阅
				if !shouldDispatchEvent(c, env.Event) {
					continue
				}
				g.writeGC(c, frame)
			}
		}
	}
}

// shouldDispatchEvent 检查是否应该向连接发送事件
// 对于机器人连接,检查是否订阅了该事件类型
// 对于普通用户连接,始终发送
func shouldDispatchEvent(gc *gwConn, event string) bool {
	// 普通用户连接：gc.botEventSubs 为 nil，始终发送
	if gc.botEventSubs == nil {
		return true
	}

	// 机器人连接：默认不发送，除非订阅了该事件或在特殊事件白名单中
	if config.SpecialEventsMap[event] {
		return true
	}

	// 显式订阅的事件才发送
	if gc.botEventSubs[event] {
		return true
	}

	// 未订阅则不发送
	logger.Infof("[Gateway] Bot (userID=%d) not subscribed to event %s (subscriptions: %v)", gc.userID, event, gc.botEventSubs)
	return false
}

// simple token bucket limiter for gateway frames
func allowFrame(gc *gwConn, rate float64, burst float64) bool {
	now := time.Now()
	// refill
	elapsed := now.Sub(gc.rlLast).Seconds()
	gc.rlTokens += elapsed * rate
	if gc.rlTokens > burst {
		gc.rlTokens = burst
	}
	gc.rlLast = now
	if gc.rlTokens >= 1.0 {
		gc.rlTokens -= 1.0
		return true
	}
	return false
}

// BroadcastToChannel sends DISPATCH MESSAGE_CREATE to subscribers.
// excludeBotUserID 可选参数：如果提供，会跳过该用户 ID 对应的机器人连接（防止机器人收到自己发的消息）。
func (g *Gateway) BroadcastToChannel(channelID uint, event string, payload any, excludeBotUserID ...uint) {
	var excludeID uint
	if len(excludeBotUserID) > 0 {
		excludeID = excludeBotUserID[0]
	}
	// If Redis is available, publish and let subscribers consume
	if g.rdb != nil {
		env := pubSubEnvelope{Event: event, Payload: mustJSON(payload), ExcludeBotUserID: excludeID}
		bs, _ := json.Marshal(env)
		if err := g.rdb.Publish(context.Background(), topicForChannel(channelID), bs).Err(); err != nil {
			logger.Errorf("[Gateway] Redis publish error (channel %d): %v", channelID, err)
		} else {
			logger.Debugf("[Gateway] Published %s event to channel %d via Redis", event, channelID)
		}
		return
	}
	// Fallback to local in-memory broadcast
	g.mu.RLock()
	conns := make([]*gwConn, 0, len(g.subs[channelID]))
	for c := range g.subs[channelID] {
		conns = append(conns, c)
	}
	g.mu.RUnlock()
	logger.Infof("[Gateway] Broadcasting %s event to channel %d (%d subscribers)", event, channelID, len(conns))
	if len(conns) == 0 {
		return
	}
	seq := atomic.AddUint64(&g.seq, 1)
	s := seq
	t := event
	frame := gwFrame{Op: config.OpDispatch, D: mustJSON(payload), S: &s, T: &t}
	sentCount := 0
	skippedCount := 0
	for _, c := range conns {
		// 跳过机器人自身连接（防止机器人收到自己发的消息）
		if excludeID != 0 && c.botEventSubs != nil && c.userID == excludeID {
			skippedCount++
			continue
		}
		// 检查机器人事件订阅
		if !shouldDispatchEvent(c, event) {
			skippedCount++
			continue
		}
		g.writeGC(c, frame)
		sentCount++
	}
	if skippedCount > 0 {
		logger.Debugf("[Gateway] Channel %d: sent to %d, skipped %d (not subscribed or bot-self for %s)", channelID, sentCount, skippedCount, event)
	}
}

// BroadcastToGuild sends DISPATCH for guild-level events (member add/remove, role updates, etc.)
func (g *Gateway) BroadcastToGuild(guildID uint, event string, payload any) {
	if g.rdb != nil {
		env := pubSubEnvelope{Event: event, Payload: mustJSON(payload)}
		bs, _ := json.Marshal(env)
		if err := g.rdb.Publish(context.Background(), topicForGuild(guildID), bs).Err(); err != nil {
			logger.Errorf("[Gateway] Redis publish error (guild %d): %v", guildID, err)
		} else {
			logger.Debugf("[Gateway] Published %s event to guild %d via Redis", event, guildID)
		}
		return
	}

	// Fallback to local in-memory broadcast
	g.mu.RLock()
	conns := make([]*gwConn, 0, len(g.guildSubs[guildID]))
	for c := range g.guildSubs[guildID] {
		conns = append(conns, c)
	}
	g.mu.RUnlock()
	if len(conns) == 0 {
		return
	}
	seq := atomic.AddUint64(&g.seq, 1)
	s := seq
	t := event
	frame := gwFrame{Op: config.OpDispatch, D: mustJSON(payload), S: &s, T: &t}
	for _, c := range conns {
		if !shouldDispatchEvent(c, event) {
			continue
		}
		g.writeGC(c, frame)
	}
}

// BroadcastToDM sends DISPATCH to subscribers of the DM thread
func (g *Gateway) BroadcastToDM(threadID uint, event string, payload any, participantIDs []uint) {
	if g.rdb != nil {
		env := pubSubEnvelope{Event: event, Payload: mustJSON(payload)}
		bs, _ := json.Marshal(env)
		// publish to dm topic
		if err := g.rdb.Publish(context.Background(), topicForDM(threadID), bs).Err(); err != nil {
			logger.Errorf("[Gateway] Redis publish error (dm %d): %v", threadID, err)
		} else {
			logger.Debugf("[Gateway] Published %s event to dm %d via Redis", event, threadID)
		}
		// publish to provided participant user: topics so they receive DM events without per-thread subscription
		if len(participantIDs) > 0 {
			for _, uid := range participantIDs {
				if err := g.rdb.Publish(context.Background(), topicForUser(uid), bs).Err(); err != nil {
					logger.Errorf("[Gateway] Redis publish error (user %d): %v", uid, err)
				}
			}
		}
		return
	}

	// In-memory fallback: deliver to dm subscribers and to user connections of participants
	targetConns := make(map[*gwConn]struct{})

	g.mu.RLock()
	for c := range g.dmSubs[threadID] {
		targetConns[c] = struct{}{}
	}
	// include user connections of provided participants
	if len(participantIDs) > 0 {
		for _, uid := range participantIDs {
			if set := g.userConns[uid]; set != nil {
				for c := range set {
					targetConns[c] = struct{}{}
				}
			}
		}
	}
	g.mu.RUnlock()

	if len(targetConns) == 0 {
		return
	}

	seq := atomic.AddUint64(&g.seq, 1)
	s := seq
	t := event
	frame := gwFrame{Op: config.OpDispatch, D: mustJSON(payload), S: &s, T: &t}
	for c := range targetConns {
		if !shouldDispatchEvent(c, event) {
			continue
		}
		g.writeGC(c, frame)
	}
}

// BroadcastToUsers sends DISPATCH to specific users by ID (regardless of subscriptions)
func (g *Gateway) BroadcastToUsers(userIDs []uint, event string, payload any) {
	if g.rdb != nil {
		// publish once per user topic to avoid large payloads
		env := pubSubEnvelope{Event: event, Payload: mustJSON(payload), UserIDs: userIDs}
		bs, _ := json.Marshal(env)
		for _, uid := range userIDs {
			if err := g.rdb.Publish(context.Background(), topicForUser(uid), bs).Err(); err != nil {
				logger.Errorf("[Gateway] Redis publish error (user %d): %v", uid, err)
			}
		}
		return
	}
	g.mu.RLock()
	conns := make([]*gwConn, 0)
	for _, uid := range userIDs {
		if set := g.userConns[uid]; set != nil {
			for conn := range set {
				conns = append(conns, conn)
			}
		}
	}
	g.mu.RUnlock()
	if len(conns) == 0 {
		return
	}
	seq := atomic.AddUint64(&g.seq, 1)
	s := seq
	t := event
	frame := gwFrame{Op: config.OpDispatch, D: mustJSON(payload), S: &s, T: &t}
	for _, c := range conns {
		g.writeGC(c, frame)
	}
}

// ===== Personal-level broadcast helpers =====
// BroadcastNotice 发布个人通知到指定用户（默认订阅 user:<uid>）
func (g *Gateway) BroadcastNotice(toUserID uint, payload any) {
	g.BroadcastToUsers([]uint{toUserID}, config.EventNoticeCreate, payload)
}

// BroadcastApply 发布个人申请到指定用户（默认订阅 user:<uid>）
func (g *Gateway) BroadcastApply(toUserID uint, payload any) {
	g.BroadcastToUsers([]uint{toUserID}, config.EventApplyCreate, payload)
}

// BroadcastMention 发布被提及事件（细分通知类型，可与 NOTICE_CREATE 并存）
func (g *Gateway) BroadcastMention(toUserID uint, payload any) {
	g.BroadcastToUsers([]uint{toUserID}, config.EventMentionCreate, payload)
}

// BroadcastApplicationUpdate 发布申请状态更新（approved/rejected）
// 已弃用：请使用 BroadcastApplyUpdate
func (g *Gateway) BroadcastApplicationUpdate(toUserID uint, payload any) {
	g.BroadcastToUsers([]uint{toUserID}, config.EventApplicationUpdate, payload)
}

// BroadcastApplyUpdate 发布申请状态更新（使用前端期望的 APPLY_UPDATE 事件名）
func (g *Gateway) BroadcastApplyUpdate(toUserID uint, payload any) {
	g.BroadcastToUsers([]uint{toUserID}, config.EventApplyUpdate, payload)
}

// BroadcastNoticeUpdate 发布通知状态更新（例如已读）
func (g *Gateway) BroadcastNoticeUpdate(toUserID uint, payload any) {
	g.BroadcastToUsers([]uint{toUserID}, config.EventNoticeUpdate, payload)
}

// BroadcastReadStateUpdate 发布红点状态更新（标记已读后清除红点）
func (g *Gateway) BroadcastReadStateUpdate(toUserID uint, resourceType string, resourceID uint) {
	payload := gin.H{
		"type":              resourceType,
		"id":                resourceID,
		"lastReadMessageId": 0,
		"unreadCount":       0,
		"mentionCount":      0,
	}
	g.BroadcastToUsers([]uint{toUserID}, config.EventReadStateUpdate, payload)
}

// handleVoiceSignal processes WebRTC signaling for voice calls
func (g *Gateway) handleVoiceSignal(gc *gwConn, p *voiceSignalPayload) {
	logger.Debugf("[Gateway] Voice signal from user %d: type=%s, toUser=%d", gc.userID, p.Type, p.ToUserID)

	// Get sender user info
	sender, err := g.svc.GetUserByID(gc.userID)
	if err != nil || sender == nil {
		logger.Errorf("[Gateway] Failed to get sender user: %v", err)
		return
	}

	switch p.Type {
	case "offer":
		// Forward offer to the target user
		if p.SDP == nil {
			logger.Warnf("[Gateway] Offer missing SDP")
			return
		}
		isVideoCall := false
		if p.IsVideoCall != nil {
			isVideoCall = *p.IsVideoCall
		}
		payload := voiceCallIncomingPayload{
			FromUserID:   gc.userID,
			FromUserName: sender.Name,
			ThreadID:     p.ThreadID,
			IsVideoCall:  isVideoCall,
		}
		g.BroadcastToUsers([]uint{p.ToUserID}, config.EventVoiceCallIncoming, payload)

		// Also send the actual offer
		offerPayload := map[string]any{
			"fromUserId": gc.userID,
			"threadId":   p.ThreadID,
			"sdp":        *p.SDP,
		}
		g.BroadcastToUsers([]uint{p.ToUserID}, config.EventVoiceCallOffer, offerPayload)

	case "answer":
		// Forward answer to the caller
		if p.SDP == nil {
			logger.Warnf("[Gateway] Answer missing SDP")
			return
		}
		payload := voiceCallAnswerPayload{
			FromUserID: gc.userID,
			ThreadID:   p.ThreadID,
			SDP:        *p.SDP,
		}
		g.BroadcastToUsers([]uint{p.ToUserID}, config.EventVoiceCallAnswer, payload)

	case "ice_candidate":
		// Forward ICE candidate
		if p.Candidate == nil || p.SDPMLineIndex == nil || p.SDPMid == nil {
			logger.Warnf("[Gateway] ICE candidate missing fields")
			return
		}
		payload := voiceCallICEPayload{
			FromUserID:    gc.userID,
			ThreadID:      p.ThreadID,
			Candidate:     *p.Candidate,
			SDPMLineIndex: *p.SDPMLineIndex,
			SDPMid:        *p.SDPMid,
		}
		g.BroadcastToUsers([]uint{p.ToUserID}, config.EventVoiceCallICE, payload)

	case "hangup":
		// Forward hangup signal
		payload := voiceCallHangupPayload{
			FromUserID: gc.userID,
			ThreadID:   p.ThreadID,
		}
		g.BroadcastToUsers([]uint{p.ToUserID}, config.EventVoiceCallHangup, payload)

	default:
		logger.Warnf("[Gateway] Unknown voice signal type: %s", p.Type)
	}
}

// ========== QR Code Login WebSocket Handlers ==========

type qrCodeRequestPayload struct {
	Code string `json:"code"` // 二维码标识
}

// handleQRCodeRequest 处理二维码登录请求（PC 端通过 WebSocket 监听状态变化）
func (g *Gateway) handleQRCodeRequest(gc *gwConn, data json.RawMessage) {
	var p qrCodeRequestPayload
	if err := json.Unmarshal(data, &p); err != nil {
		logger.Warnf("[Gateway] Failed to parse QR code request: %v", err)
		return
	}

	if p.Code == "" {
		logger.Warnf("[Gateway] QR code request missing code")
		return
	}

	// 验证二维码是否存在
	qrData, err := g.svc.GetQRCodeLogin(p.Code)
	if err != nil {
		// 二维码不存在或已过期
		g.writeGC(gc, gwFrame{
			Op: config.OpQRCodeExpired,
			D:  mustJSON(map[string]any{"code": p.Code, "error": "二维码不存在或已过期"}),
		})
		return
	}

	// 返回当前状态
	g.writeGC(gc, gwFrame{
		Op: config.OpQRCodeGenerated,
		D:  mustJSON(map[string]any{"code": qrData.Code, "state": qrData.State}),
	})

	// 订阅二维码状态变更（通过 Redis Pub/Sub）
	go g.subscribeQRCodeUpdates(gc, p.Code)
}

// subscribeQRCodeUpdates 订阅二维码状态变更（Redis Pub/Sub）
func (g *Gateway) subscribeQRCodeUpdates(gc *gwConn, code string) {
	if g.rdb == nil {
		return
	}

	ctx := context.Background()
	channel := fmt.Sprintf("qrcode:ws:%s", code)

	pubsub := g.rdb.Subscribe(ctx, channel)
	defer pubsub.Close()

	// 设置超时（登录二维码过期时间：5分钟）
	timeout := time.After(time.Duration(config.QRCodeLoginExpireSeconds) * time.Second)

	for {
		select {
		case <-timeout:
			// 超时，发送过期通知
			g.writeGC(gc, gwFrame{
				Op: config.OpQRCodeExpired,
				D:  mustJSON(map[string]any{"code": code}),
			})
			return

		case msg, ok := <-pubsub.Channel():
			if !ok {
				return
			}

			// 解析消息
			var payload map[string]any
			if err := json.Unmarshal([]byte(msg.Payload), &payload); err != nil {
				logger.Errorf("[Gateway] Failed to parse QR code update: %v", err)
				continue
			}

			state, _ := payload["state"].(string)

			switch state {
			case "scanned":
				// 已扫描，通知 PC 端
				g.writeGC(gc, gwFrame{
					Op: config.OpQRCodeScanned,
					D:  mustJSON(map[string]any{"code": code}),
				})

			case "confirmed":
				// 已确认，通知 PC 端并提供用户信息
				userID, _ := payload["userId"].(float64)
				if userID > 0 {
					// 获取用户信息
					user, err := g.svc.Repo.GetUserByID(uint(userID))
					if err == nil {
						g.writeGC(gc, gwFrame{
							Op: config.OpQRCodeConfirmed,
							D: mustJSON(map[string]any{
								"code":   code,
								"userId": user.ID,
								"user": map[string]any{
									"id":    user.ID,
									"name":  user.Name,
									"email": user.Email,
								},
							}),
						})
					}
				}
				return

			case "cancelled":
				// 已取消
				g.writeGC(gc, gwFrame{
					Op: config.OpQRCodeCancelled,
					D:  mustJSON(map[string]any{"code": code}),
				})
				return

			case "expired":
				// 已过期
				g.writeGC(gc, gwFrame{
					Op: config.OpQRCodeExpired,
					D:  mustJSON(map[string]any{"code": code}),
				})
				return
			}
		}
	}
}
