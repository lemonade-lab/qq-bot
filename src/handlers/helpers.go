package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"bubble/src/config"
	"bubble/src/db/models"
	"bubble/src/middleware"
	"bubble/src/service"

	"github.com/bytedance/gopkg/util/logger"
	"github.com/gin-gonic/gin"
)

// ──────────────────────────────────────────────
// READ_STATE_UPDATE 广播辅助方法
// ──────────────────────────────────────────────

// broadcastChannelReadStateUpdates 广播频道消息产生的红点更新给所有成员（排除发送者）
// 应在 OnNewChannelMessage 之后异步调用
func (h *HTTP) broadcastChannelReadStateUpdates(channelID, senderID uint) {
	if h.Gw == nil {
		return
	}

	ch, err := h.Svc.GetChannel(channelID)
	if err != nil || ch == nil {
		return
	}

	members, err := h.Svc.Repo.ListMembers(ch.GuildID)
	if err != nil {
		return
	}

	for _, m := range members {
		if m.UserID == senderID {
			continue
		}

		// 频道级别
		rs, _ := h.Svc.Repo.GetReadState(m.UserID, "channel", channelID)
		if rs != nil {
			h.Gw.BroadcastToUsers([]uint{m.UserID}, config.EventReadStateUpdate, gin.H{
				"type":              "channel",
				"id":                channelID,
				"guildId":           ch.GuildID,
				"lastReadMessageId": rs.LastReadMessageID,
				"unreadCount":       rs.UnreadCount,
				"mentionCount":      rs.MentionCount,
			})
		}

		// 公会级别
		grs, _ := h.Svc.Repo.GetReadState(m.UserID, "guild", ch.GuildID)
		if grs != nil {
			h.Gw.BroadcastToUsers([]uint{m.UserID}, config.EventReadStateUpdate, gin.H{
				"type":              "guild",
				"id":                ch.GuildID,
				"lastReadMessageId": grs.LastReadMessageID,
				"unreadCount":       grs.UnreadCount,
				"mentionCount":      grs.MentionCount,
			})
		}
	}
}

// broadcastDmReadStateUpdate 广播私聊消息产生的红点更新给接收者
// 应在 OnNewDmMessage 之后异步调用
func (h *HTTP) broadcastDmReadStateUpdate(threadID, senderID uint) {
	if h.Gw == nil {
		return
	}

	thread, err := h.Svc.Repo.GetDmThread(threadID)
	if err != nil || thread == nil {
		return
	}

	recipientID := thread.UserAID
	if recipientID == senderID {
		recipientID = thread.UserBID
	}

	rs, _ := h.Svc.Repo.GetReadState(recipientID, "dm", threadID)
	if rs != nil {
		h.Gw.BroadcastToUsers([]uint{recipientID}, config.EventReadStateUpdate, gin.H{
			"type":              "dm",
			"id":                threadID,
			"lastReadMessageId": rs.LastReadMessageID,
			"unreadCount":       rs.UnreadCount,
			"mentionCount":      rs.MentionCount,
		})
	}
}

// broadcastGroupReadStateUpdate 广播群聊消息产生的红点更新给所有群成员（排除发送者）
// 应在 OnNewGroupMessage 之后异步调用
func (h *HTTP) broadcastGroupReadStateUpdate(groupThreadID, senderID uint) {
	if h.Gw == nil {
		return
	}

	memberIDs, err := h.Svc.Repo.GetGroupMemberIDs(groupThreadID)
	if err != nil || len(memberIDs) == 0 {
		return
	}

	for _, uid := range memberIDs {
		if uid == senderID {
			continue
		}

		rs, _ := h.Svc.Repo.GetReadState(uid, "group", groupThreadID)
		if rs != nil {
			h.Gw.BroadcastToUsers([]uint{uid}, config.EventReadStateUpdate, gin.H{
				"type":              "group",
				"id":                groupThreadID,
				"lastReadMessageId": rs.LastReadMessageID,
				"unreadCount":       rs.UnreadCount,
				"mentionCount":      rs.MentionCount,
			})
		}
	}
}

// setSecureTokenCookie 设置安全的 HttpOnly Cookie 来存储 token
func setSecureTokenCookie(c *gin.Context, token string) {
	// Cookie 有效期: 7 天 (虽然Cookie持续7天,但Token本身5分钟后失效,需要刷新)
	maxAge := 7 * 24 * 60 * 60

	// 判断是否为 HTTPS
	secure := c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https"

	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		middleware.TokenCookieName, // name
		token,                      // value
		maxAge,                     // maxAge (seconds)
		"/",                        // path
		"",                         // domain (空字符串表示当前域)
		secure,                     // secure (仅 HTTPS 传输)
		true,                       // httpOnly (防止 JavaScript 访问)
	)
}

// clearTokenCookie 清除 token cookie
func clearTokenCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		middleware.TokenCookieName,
		"",
		-1,
		"/",
		"",
		false,
		true,
	)
}

// setSecureSessionCookie 设置安全的 HttpOnly Cookie 来存储 session token
func setSecureSessionCookie(c *gin.Context, sessionToken, cookieName string) {
	// Cookie 有效期: 30 天 (与 Session TTL 一致)
	maxAge := 30 * 24 * 60 * 60

	// 判断是否为 HTTPS
	secure := c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https"

	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		cookieName,   // name
		sessionToken, // value
		maxAge,       // maxAge (seconds)
		"/",          // path
		"",           // domain (空字符串表示当前域)
		secure,       // secure (仅 HTTPS 传输)
		true,         // httpOnly (防止 JavaScript 访问)
	)
}

// clearSessionCookie 清除 session cookie
func clearSessionCookie(c *gin.Context, cookieName string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		cookieName,
		"",
		-1,
		"/",
		"",
		false,
		true,
	)
}

// hashFingerprint hashes a fingerprint string using SHA256
func hashFingerprint(input string) string {
	hash := sha256.Sum256([]byte(input))
	return hex.EncodeToString(hash[:])
}

// getDeviceFingerprint extracts device fingerprint from request headers.
// Combines multiple browser/device identifiers to create a semi-unique fingerprint.
// This is used for session security - detecting suspicious session reuse across different devices.
//
// 安全设计: 指纹完全由服务端控制,不接受客户端提供的指纹值
// 原因: 防止攻击者伪造设备指纹绕过安全检测
func getDeviceFingerprint(c *gin.Context) string {
	// 服务端单方面生成指纹: 组合多个HTTP特征
	// 注意: 不从 X-Device-Fingerprint header 读取,防止伪造
	components := []string{
		c.GetHeader("User-Agent"),
		c.GetHeader("Accept-Language"),
		c.GetHeader("Accept-Encoding"),
		c.GetHeader("Accept"),
		c.ClientIP(), // IP作为重要特征
	}

	// 拼接后哈希 (SHA256)
	combined := strings.Join(components, "|")
	return hashFingerprint(combined)
}

// parseUintParam parses uint parameter from URL path
func parseUintParam(c *gin.Context, key string) (uint, error) {
	v, err := strconv.ParseUint(c.Param(key), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("无效的%s", key)
	}
	return uint(v), nil
}

// parseUintQuery parses uint parameter from query string
func parseUintQuery(c *gin.Context, key string, defaultVal uint) uint {
	v, _ := strconv.ParseUint(c.Query(key), 10, 64)
	if v == 0 {
		return defaultVal
	}
	return uint(v)
}

// requireAuth ensures user is authenticated
func requireAuth(c *gin.Context) (*models.User, bool) {
	u, exists := c.Get("user")
	if !exists {
		return nil, false
	}
	user, ok := u.(*models.User)
	return user, ok
}

// requireGuildPerm checks if user has specific guild permission
func requireGuildPerm(svc *service.Service, guildID, userID uint, perm uint64) error {
	has, err := svc.HasGuildPerm(guildID, userID, perm)
	if err != nil {
		return err
	}
	if !has {
		return service.ErrUnauthorized
	}
	return nil
}

// requireChannelAccess checks if user can access a channel
func requireChannelAccess(svc *service.Service, channelID, userID uint, perm uint64) (*models.Channel, error) {
	ch, err := svc.Repo.GetChannel(channelID)
	if err != nil {
		return nil, service.ErrNotFound
	}

	has, err := svc.HasGuildPerm(ch.GuildID, userID, perm)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, service.ErrUnauthorized
	}

	return ch, nil
}

// errorResponse returns a standardized error response
func errorResponse(c *gin.Context, status int, err error) {
	logger.Errorf("[Helper] Error response: %v", err)
	c.JSON(status, gin.H{"error": "操作失败"})
}

// enrichFileMetaDimensions 为 fileMeta 补全 width/height（如果是图片且缺失宽高）
// 直接修改传入的 fileMeta map，无返回值。检测失败静默忽略不影响消息发送。
func (h *HTTP) enrichFileMetaDimensions(ctx context.Context, fileMeta interface{}) {
	if h.Svc.MinIO == nil {
		return
	}
	fm, ok := fileMeta.(map[string]interface{})
	if !ok {
		return
	}
	// 已有宽高则跳过
	if hasPositiveDim(fm, "width") && hasPositiveDim(fm, "height") {
		return
	}
	path, _ := fm["path"].(string)
	contentType, _ := fm["contentType"].(string)
	if path == "" || contentType == "" {
		return
	}
	dims, err := h.Svc.MinIO.DetectMediaDimensions(ctx, path, contentType)
	if err != nil || dims == nil {
		return
	}
	fm["width"] = dims.Width
	fm["height"] = dims.Height
}

// hasPositiveDim 检查 map 中指定 key 是否为正数
func hasPositiveDim(m map[string]interface{}, key string) bool {
	v, ok := m[key]
	if !ok {
		return false
	}
	switch n := v.(type) {
	case float64:
		return n > 0
	case int:
		return n > 0
	case int64:
		return n > 0
	}
	return false
}

// handleAudioConversion 检测并异步转换音频文件
// fileMeta: 文件元数据（包含 path、url、contentType 等）
// userID: 用户 ID
// guildID: 可选的公会 ID（私信时为 nil）
func (h *HTTP) handleAudioConversion(ctx context.Context, fileMeta interface{}, userID uint, guildID *uint) {
	// 检查音频转换服务是否可用
	if h.Svc.AudioConverter == nil {
		return
	}

	// 将 fileMeta 转换为 map
	fileMetaMap, ok := fileMeta.(map[string]interface{})
	if !ok {
		return
	}

	// 提取文件路径和 Content-Type
	path, pathOk := fileMetaMap["path"].(string)
	contentType, typeOk := fileMetaMap["contentType"].(string)

	if !pathOk || !typeOk {
		return
	}

	// 检查是否为音频文件且需要转换
	if !service.IsAudioFile(contentType) || !service.NeedsConversion(contentType) {
		return
	}

	// 提取 category（如果有）
	category := "guild-chat-files"
	if cat, ok := fileMetaMap["category"].(string); ok && cat != "" {
		category = cat
	}

	// 创建转换任务
	job := service.AudioConversionJob{
		OriginalPath: path,
		Category:     category,
		UserID:       userID,
		GuildID:      guildID,
	}

	// 异步转换音频文件
	h.Svc.AudioConverter.ConvertAudioAsync(ctx, job)
}

// ==================== Mention 解析 ====================

// 简写格式正则（机器人常用）
var mentionShortUserRegex = regexp.MustCompile(`<@(\d+)>`)        // <@uid>
var mentionShortChannelRegex = regexp.MustCompile(`<#(\d+)>`)     // <#channelId>
var mentionShortEveryoneRegex = regexp.MustCompile(`<@everyone>`) // <@everyone>

// MentionResolver 提供用户/频道名称查询能力，供 mentions 解析时富化和清理内容。
type MentionResolver interface {
	ResolveUserName(uid uint) (name string, avatar string, ok bool)
	ResolveChannelName(cid uint) (name string, ok bool)
}

// parseMentionsFromContent 从消息内容中解析简写格式的 mentions，
// 同时将简写替换为干净的可读文本，移除无效引用。
//
// 支持的格式：
//   - <@42>       → @用户名  (查到) 或 直接移除 (查不到)
//   - <#7>        → #频道名  (查到) 或 直接移除 (查不到)
//   - <@everyone> → @全体成员
//
// 返回值：
//   - cleanedContent: 替换/清理后的消息文本
//   - mentions: 有效的结构化 mentions 列表
func parseMentionsFromContent(content string, resolver MentionResolver) (string, []map[string]any) {
	hasAtSign := strings.Contains(content, "<@")
	hasHashSign := strings.Contains(content, "<#")

	if !hasAtSign && !hasHashSign {
		return content, nil
	}

	seen := make(map[string]struct{})
	var result []map[string]any

	// 1) <@everyone> → @全体成员
	if hasAtSign {
		content = mentionShortEveryoneRegex.ReplaceAllStringFunc(content, func(_ string) string {
			key := "everyone:0"
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				result = append(result, map[string]any{"type": "everyone"})
			}
			return "@全体成员"
		})
	}

	// 2) <@uid> → @用户名 或移除
	if hasAtSign {
		content = mentionShortUserRegex.ReplaceAllStringFunc(content, func(match string) string {
			sm := mentionShortUserRegex.FindStringSubmatch(match)
			if len(sm) < 2 {
				return "" // 格式异常，移除
			}
			uid, err := strconv.ParseUint(sm[1], 10, 64)
			if err != nil || uid == 0 {
				return "" // 无效ID，移除
			}
			if resolver != nil {
				if name, avatar, ok := resolver.ResolveUserName(uint(uid)); ok {
					key := fmt.Sprintf("user:%d", uid)
					if _, dup := seen[key]; !dup {
						seen[key] = struct{}{}
						m := map[string]any{"type": "user", "id": uint(uid), "name": name}
						if avatar != "" {
							m["avatar"] = avatar
						}
						result = append(result, m)
					}
					return "@" + name
				}
			}
			return "" // 查不到用户，移除
		})
	}

	// 3) <#channelId> → #频道名 或移除
	if hasHashSign {
		content = mentionShortChannelRegex.ReplaceAllStringFunc(content, func(match string) string {
			cm := mentionShortChannelRegex.FindStringSubmatch(match)
			if len(cm) < 2 {
				return ""
			}
			cid, err := strconv.ParseUint(cm[1], 10, 64)
			if err != nil || cid == 0 {
				return ""
			}
			if resolver != nil {
				if name, ok := resolver.ResolveChannelName(uint(cid)); ok {
					key := fmt.Sprintf("channel:%d", cid)
					if _, dup := seen[key]; !dup {
						seen[key] = struct{}{}
						result = append(result, map[string]any{"type": "channel", "id": uint(cid), "name": name})
					}
					return "#" + name
				}
			}
			return ""
		})
	}

	return content, result
}
