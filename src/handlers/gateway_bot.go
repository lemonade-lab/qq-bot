package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"bubble/src/config"
	"bubble/src/db/models"
	"bubble/src/logger"
	"bubble/src/service"
)

// Bot gateway route registration moved to http.go to avoid duplicate registration
// The RegisterBotGateway method has been removed - route is now registered directly in http.go

// connectBot 处理机器人WebSocket连接（从http.go直接调用）
func (g *Gateway) connectBot(c *Context) {
	// 1. 验证 Bot Token (仅允许 Authorization 头部，不支持 query/子协议)
	auth := c.Request.Header.Get("Authorization")
	if auth == "" {
		logger.Warn("[BotGateway] No Authorization header provided")
		http.Error(c.Writer, "unauthorized: missing Authorization header", http.StatusUnauthorized)
		return
	}
	parts := strings.SplitN(auth, " ", 2)
	if !(len(parts) == 2 && strings.EqualFold(parts[0], "bearer") && parts[1] != "") {
		logger.Warnf("[BotGateway] Invalid Authorization format: %s", auth)
		http.Error(c.Writer, "unauthorized: invalid Authorization header", http.StatusUnauthorized)
		return
	}
	token := parts[1]

	// 2. 验证 Robot Token 并获取 Robot 对象
	robot, err := g.svc.GetBotByToken(token)
	if err != nil || robot == nil {
		logger.Warnf("[BotGateway] Invalid bot token: %v", err)
		http.Error(c.Writer, "unauthorized: invalid bot token", http.StatusUnauthorized)
		return
	}

	// 3. 确保 BotUser 已加载
	if robot.BotUser == nil && robot.BotUserID != 0 {
		if u, err := g.svc.GetUserByID(robot.BotUserID); err == nil {
			robot.BotUser = u
		}
	}
	if robot.BotUser == nil {
		logger.Warnf("[BotGateway] Bot user not found for robot ID %d", robot.ID)
		http.Error(c.Writer, "unauthorized: bot user not found", http.StatusUnauthorized)
		return
	}

	// 4. 检查服务器是否正在关闭
	if g.gracefulServer != nil && g.gracefulServer.IsShuttingDown() {
		http.Error(c.Writer, "server is shutting down", http.StatusServiceUnavailable)
		return
	}

	// 5. 升级到 WebSocket
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Errorf("[BotGateway] WebSocket upgrade failed: %v", err)
		return
	}

	// 升级成功后增加连接计数
	if g.gracefulServer != nil {
		g.gracefulServer.IncrementWSConn()
	}

	// 6. 创建机器人连接对象
	gc := &gwConn{
		c:            conn,
		userID:       robot.BotUserID, // 使用 BotUser ID
		subs:         make(map[uint]struct{}),
		subsDm:       make(map[uint]struct{}),
		botEventSubs: make(map[string]bool), // 初始化事件订阅 map
		lastHB:       time.Now().UnixMilli(),
		rlTokens:     20, // 机器人有更高的速率限制
		rlLast:       time.Now(),
		disconnected: 0,
	}

	// 7. 注册用户连接 (Bot作为用户)
	g.mu.Lock()
	if g.userConns[robot.BotUserID] == nil {
		g.userConns[robot.BotUserID] = make(map[*gwConn]struct{})
	}
	g.userConns[robot.BotUserID][gc] = struct{}{}
	g.mu.Unlock()

	// 额外订阅 bot user 级别 topic，确保能接收发给机器人的私聊事件
	g.ensureSubscribeTopic(context.Background(), topicForUser(robot.BotUserID))

	logger.Infof("[BotGateway] Bot connected: Robot ID=%d, Name=%s, BotUserID=%d",
		robot.ID, robot.BotName(), robot.BotUserID)

	// 8. 发送 HELLO
	g.write(conn, gwFrame{
		Op: config.OpHello,
		D:  mustJSON(helloPayload{HeartbeatInterval: int(g.hbInterval.Milliseconds())}),
	})
	// 9. 自动订阅机器人加入的所有服务器和私聊 (频道维度)
	subscribedGuilds := g.autoSubscribeBotGuilds(gc, robot)
	subscribedDMs := g.autoSubscribeBotDMs(gc, robot)

	// 10. 发送 BOT_READY 事件(此时机器人已订阅所有频道和私聊,但事件类型需手动订阅)
	g.dispatch(gc, config.EventBotReady, map[string]any{
		"bot": map[string]any{
			"id":        robot.ID,
			"botUser":   robot.BotUser,
			"createdAt": robot.CreatedAt,
		},
		"user":             robot.BotUser,
		"subscribedGuilds": subscribedGuilds,
		"subscribedDMs":    subscribedDMs,
		"availableEvents":  config.Events,
	})

	// 11. 启动读取和心跳监控协程
	go g.readBotLoop(gc, robot)
	go g.liveness(gc)
}

// readBotLoop 处理机器人的 WebSocket 消息
func (g *Gateway) readBotLoop(gc *gwConn, robot *models.Robot) {
	for {
		var f gwFrame
		if err := gc.c.ReadJSON(&f); err != nil {
			g.disconnect(gc)
			return
		}

		// 速率限制 (机器人有更高的限制: 10 rps, burst 20)
		if f.Op != config.OpHeartbeat && !allowFrame(gc, config.BotRateLimitRPS, config.BotRateLimitBurst) {
			logger.Warnf("[BotGateway] Rate limit exceeded for bot %s", robot.BotName())
			g.disconnect(gc)
			return
		}

		switch f.Op {
		case config.OpHeartbeat:
			// 更新心跳时间
			gc.lastHB = time.Now().UnixMilli()
			g.write(gc.c, gwFrame{Op: config.OpHeartbeatAck})

		case config.OpSubscribe:
			// 机器人订阅：事件类型或资源（频道/私聊）
			var p subscribePayload
			if err := json.Unmarshal(f.D, &p); err != nil {
				logger.Warnf("[BotGateway] Invalid subscribe payload: %v", err)
				break
			}

			// 如果携带 GuildID，订阅该服务器的所有频道
			if p.GuildID != 0 {
				guild, err := g.svc.Repo.GetGuild(p.GuildID)
				if err != nil || guild == nil {
					g.dispatch(gc, config.EventSubscribeDenied, map[string]any{
						"resource": "guild",
						"id":       p.GuildID,
						"reason":   "not_found",
					})
				} else {
					// 权限检查与用户一致：需要 VIEW_CHANNEL 有效权限
					ok, err := g.svc.HasGuildPerm(p.GuildID, robot.BotUserID, service.PermViewChannel)
					if err != nil || !ok {
						g.dispatch(gc, config.EventSubscribeDenied, map[string]any{
							"resource": "guild",
							"id":       p.GuildID,
							"reason":   "forbidden",
						})
					} else {
						channels, err := g.svc.Repo.GetChannelsByGuildID(p.GuildID)
						if err != nil {
							logger.Errorf("[BotGateway] Failed to get channels for guild %d: %v", p.GuildID, err)
						} else {
							count := 0
							for _, ch := range channels {
								g.mu.Lock()
								if g.subs[ch.ID] == nil {
									g.subs[ch.ID] = make(map[*gwConn]struct{})
								}
								g.subs[ch.ID][gc] = struct{}{}
								gc.subs[ch.ID] = struct{}{}
								g.mu.Unlock()
								g.ensureSubscribeTopic(context.Background(), topicForChannel(ch.ID))
								count++
							}
							logger.Infof("[BotGateway] Bot %s subscribed to %d channels via guild %d OpSubscribe", robot.BotName(), count, p.GuildID)
						}
					}
				}
			}

			// 如果携带频道ID，加入频道订阅集合
			if p.ChannelID != 0 {
				g.mu.Lock()
				if g.subs[p.ChannelID] == nil {
					g.subs[p.ChannelID] = make(map[*gwConn]struct{})
				}
				g.subs[p.ChannelID][gc] = struct{}{}
				gc.subs[p.ChannelID] = struct{}{}
				g.mu.Unlock()
				logger.Infof("[BotGateway] Bot %s subscribed to channel %d via OpSubscribe", robot.BotName(), p.ChannelID)
				g.ensureSubscribeTopic(context.Background(), topicForChannel(p.ChannelID))
			}
			// 如果携带线程ID，加入私聊订阅集合
			if p.ThreadID != 0 {
				g.mu.Lock()
				if g.dmSubs[p.ThreadID] == nil {
					g.dmSubs[p.ThreadID] = make(map[*gwConn]struct{})
				}
				g.dmSubs[p.ThreadID][gc] = struct{}{}
				gc.subsDm[p.ThreadID] = struct{}{}
				g.mu.Unlock()
				logger.Infof("[BotGateway] Bot %s subscribed to DM thread %d via OpSubscribe", robot.BotName(), p.ThreadID)
				g.ensureSubscribeTopic(context.Background(), topicForDM(p.ThreadID))
			}

			// 事件类型订阅（白名单）
			if len(p.Events) > 0 {
				g.handleBotEventSubscribe(gc, robot, p.Events)
			}
			// 如果既没有资源也没有事件，给出提示
			if p.ChannelID == 0 && p.ThreadID == 0 && p.GuildID == 0 && len(p.Events) == 0 {
				g.dispatch(gc, config.EventSubscribeDenied, map[string]any{
					"reason": "empty_subscribe_payload",
				})
			}

		case config.OpUnsubscribe:
			// 机器人取消订阅事件类型
			var p subscribePayload
			if err := json.Unmarshal(f.D, &p); err != nil {
				logger.Warnf("[BotGateway] Invalid unsubscribe payload: %v", err)
				break
			}

			if len(p.Events) > 0 {
				// 取消订阅事件类型
				g.handleBotEventUnsubscribe(gc, robot, p.Events)
			}

		default:
			// 忽略未知操作码
			logger.Infof("[BotGateway] Unknown opcode %d from bot %s", f.Op, robot.BotName())
		}
	}
}

// handleBotEventSubscribe 处理机器人订阅事件类型
func (g *Gateway) handleBotEventSubscribe(gc *gwConn, robot *models.Robot, events []string) {
	// 支持的事件类型 (与普通用户协议保持一致)
	validEvents := config.EventsMap

	subscribedEvents := []string{}
	invalidEvents := []string{}

	// 验证并订阅事件
	for _, event := range events {
		if validEvents[event] {
			gc.botEventSubs[event] = true
			subscribedEvents = append(subscribedEvents, event)
		} else {
			invalidEvents = append(invalidEvents, event)
		}
	}

	// 发送订阅成功确认
	g.dispatch(gc, config.EventEventsSubscribed, map[string]any{
		"subscribedEvents": subscribedEvents,
		"invalidEvents":    invalidEvents,
	})

	logger.Infof("[BotGateway] Bot %s subscribed to events: %v", robot.BotName(), subscribedEvents)
}

// handleBotEventUnsubscribe 处理机器人取消订阅事件类型
func (g *Gateway) handleBotEventUnsubscribe(gc *gwConn, robot *models.Robot, events []string) {
	unsubscribedEvents := []string{}

	for _, event := range events {
		if gc.botEventSubs[event] {
			delete(gc.botEventSubs, event)
			unsubscribedEvents = append(unsubscribedEvents, event)
		}
	}

	// 发送取消订阅成功确认
	g.dispatch(gc, config.EventEventsUnsubscribed, map[string]any{
		"unsubscribedEvents": unsubscribedEvents,
	})

	logger.Infof("[BotGateway] Bot %s unsubscribed from events: %v", robot.BotName(), unsubscribedEvents)
}

// autoSubscribeBotGuilds 自动订阅机器人加入的所有服务器
func (g *Gateway) autoSubscribeBotGuilds(gc *gwConn, robot *models.Robot) []map[string]any {
	logger.Infof("[BotGateway] Auto-subscribing guilds for bot %s (BotUserID: %d)", robot.BotName(), robot.BotUserID)

	// 1. 获取机器人加入的所有服务器
	guilds, err := g.svc.ListUserGuilds(robot.BotUserID)
	if err != nil {
		logger.Errorf("[BotGateway] Failed to get guilds for bot %s: %v", robot.BotName(), err)
		return []map[string]any{}
	}

	logger.Infof("[BotGateway] Bot %s is in %d guilds", robot.BotName(), len(guilds))

	subscribedGuilds := []map[string]any{}

	// 2. 遍历每个服务器,订阅所有有权限查看的频道
	for _, guild := range guilds {
		logger.Infof("[BotGateway] Processing guild %d (%s)", guild.ID, guild.Name)

		// 与普通用户一致：基于 EffectiveGuildPerms/@everyone，检查 VIEW_CHANNEL 权限
		hasGuildPermission, err := g.svc.HasGuildPerm(guild.ID, robot.BotUserID, service.PermViewChannel)
		if err != nil {
			logger.Warnf("[BotGateway] Permission check failed for guild %d: %v", guild.ID, err)
			continue
		}
		if !hasGuildPermission {
			logger.Debugf("[BotGateway] Bot %s lacks VIEW_CHANNEL by effective perms in guild %d (%s); skipping guild subscription", robot.BotName(), guild.ID, guild.Name)
			continue
		}

		// 获取该服务器的所有频道
		channels, err := g.svc.Repo.GetChannelsByGuildID(guild.ID)
		if err != nil {
			logger.Errorf("[BotGateway] Failed to get channels for guild %d: %v", guild.ID, err)
			continue
		}

		logger.Infof("[BotGateway] Guild %d has %d channels; bot passes VIEW_CHANNEL check", guild.ID, len(channels))

		// 订阅每个频道
		subscribedChannels := []map[string]any{}
		for _, channel := range channels {
			// 添加订阅
			g.mu.Lock()
			if g.subs[channel.ID] == nil {
				g.subs[channel.ID] = make(map[*gwConn]struct{})
			}
			g.subs[channel.ID][gc] = struct{}{}
			gc.subs[channel.ID] = struct{}{}
			g.mu.Unlock()

			// 确保 Redis 订阅
			g.ensureSubscribeTopic(context.Background(), topicForChannel(channel.ID))

			logger.Infof("[BotGateway] Bot %s subscribed to channel %d (%s) in guild %d", robot.BotName(), channel.ID, channel.Name, guild.ID)

			subscribedChannels = append(subscribedChannels, map[string]any{
				"channelId":   channel.ID,
				"channelName": channel.Name,
			})
		}

		// 记录已订阅的服务器信息
		subscribedGuilds = append(subscribedGuilds, map[string]any{
			"guildId":      guild.ID,
			"guildName":    guild.Name,
			"channelCount": len(subscribedChannels),
			"channels":     subscribedChannels,
		})
	}

	logger.Infof("[BotGateway] Bot %s auto-subscribed to %d guilds", robot.BotName(), len(subscribedGuilds))
	return subscribedGuilds
}

// autoSubscribeBotDMs 自动订阅机器人的所有私聊
func (g *Gateway) autoSubscribeBotDMs(gc *gwConn, robot *models.Robot) []map[string]any {
	logger.Infof("[BotGateway] Auto-subscribing DMs for bot %s", robot.BotName())

	// 获取机器人的所有私聊线程
	threads, err := g.svc.Repo.ListDmThreadsPaginated(robot.BotUserID, 0, 1000)
	if err != nil {
		logger.Errorf("[BotGateway] Failed to get DM threads for bot %s: %v", robot.BotName(), err)
		return []map[string]any{}
	}

	subscribedDMs := []map[string]any{}

	// 订阅每个私聊线程
	for _, thread := range threads {
		// 添加订阅
		g.mu.Lock()
		if g.dmSubs[thread.ID] == nil {
			g.dmSubs[thread.ID] = make(map[*gwConn]struct{})
		}
		g.dmSubs[thread.ID][gc] = struct{}{}
		gc.subsDm[thread.ID] = struct{}{}
		g.mu.Unlock()

		// 确保 Redis 订阅
		g.ensureSubscribeTopic(context.Background(), topicForDM(thread.ID))

		// 获取对方用户信息
		otherUserID := thread.UserAID
		if otherUserID == robot.BotUserID {
			otherUserID = thread.UserBID
		}
		otherUser, _ := g.svc.GetUserByID(otherUserID)

		subscribedDMs = append(subscribedDMs, map[string]any{
			"threadId": thread.ID,
			"otherUser": map[string]any{
				"id": otherUserID,
				"name": func() string {
					if otherUser != nil {
						return otherUser.Name
					}
					return ""
				}(),
			},
		})
	}

	logger.Infof("[BotGateway] Bot %s auto-subscribed to %d DM threads", robot.BotName(), len(subscribedDMs))
	return subscribedDMs
}

// BroadcastToBots 向所有连接的机器人广播事件
// 用于系统级通知,如机器人配置更新等
func (g *Gateway) BroadcastToBots(event string, payload any) {
	g.mu.Lock()
	defer g.mu.Unlock()

	for userID, conns := range g.userConns {
		// 检查是否是机器人用户
		u, err := g.svc.GetUserByID(userID)
		if err != nil || u == nil || !u.IsBot {
			continue
		}

		for gc := range conns {
			g.dispatch(gc, event, payload)
		}
	}
}

// GetBotConnectionCount 获取当前连接的机器人数量
func (g *Gateway) GetBotConnectionCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()

	count := 0
	for userID := range g.userConns {
		u, err := g.svc.GetUserByID(userID)
		if err == nil && u != nil && u.IsBot {
			count++
		}
	}
	return count
}
