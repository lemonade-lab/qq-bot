package middleware

import (
	"net/http"
	"strings"

	"bubble/src/db/models"
	"bubble/src/service"

	"github.com/gin-gonic/gin"
)

const CtxUserKey = "user"
const TokenCookieName = "bubble_chat_token"

// AuthRequired validates authentication with different strategies for web and mobile:
//
// Web端 (默认):
//   - 完全基于 Session Cookie 认证
//   - 每次请求验证 Session 是否有效且未撤销
//   - 不使用 JWT Token
//
// 移动端 (需设置 X-Client-Type: mobile):
//   - 使用 Bearer Access Token (JWT, 15分钟有效期)
//   - Access Token 过期后，客户端使用 Refresh Token 调用 /api/mobile/refresh 获取新 Token
//   - 不使用 Session
//
// 这确保:
// - Web端: 简单可靠，基于 Session 的即时撤销
// - 移动端: 双Token机制，减少刷新频率，提升用户体验
func AuthRequired(svc *service.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 判断客户端类型：移动端需设置 X-Client-Type: mobile
		isMobile := strings.EqualFold(c.GetHeader("X-Client-Type"), "mobile")

		if isMobile {
			// 移动端流程：仅验证 Bearer Access Token (JWT)
			token := bearerToken(c)
			if token == "" {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "缺少访问令牌"})
				return
			}

			// 解析 Access Token
			userID, err := svc.ParseAccessToken(token)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "访问令牌无效或已过期"})
				return
			}

			// 加载用户数据
			user, err := svc.GetUserByID(userID)
			if err != nil || user == nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "用户不存在"})
				return
			}

			// 注入用户到上下文
			c.Set(CtxUserKey, user)
			c.Next()
			return
		}

		// Web端流程：仅验证 Session Cookie
		sessionToken, err := c.Cookie(svc.Cfg.SessionCookieName)
		if err != nil || sessionToken == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
			return
		}

		// 验证 Session
		session, err := svc.ValidateSession(sessionToken)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "会话无效或已被撤销"})
			return
		}

		// 加载用户数据
		user, err := svc.GetUserByID(session.UserID)
		if err != nil || user == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "用户不存在"})
			return
		}

		// 注入用户和会话信息到上下文
		c.Set(CtxUserKey, user)
		c.Set("sessionID", session.ID)
		c.Next()
	}
}

func bearerToken(c *gin.Context) string {
	auth := c.GetHeader("Authorization")
	if auth == "" {
		return ""
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
		return parts[1]
	}
	return auth
}

// UserFromCtx returns the user set by AuthRequired.
func UserFromCtx(c *gin.Context) *models.User {
	if v, ok := c.Get(CtxUserKey); ok {
		if u, ok2 := v.(*models.User); ok2 {
			return u
		}
	}
	return nil
}

// RobotFromCtx retrieves the Robot object from the context
func RobotFromCtx(c *gin.Context) *models.Robot {
	if v, ok := c.Get("robot"); ok {
		if r, ok2 := v.(*models.Robot); ok2 {
			return r
		}
	}
	return nil
}

// OptionalAuth 可选的认证中间件，支持匿名访问
// 如果提供了有效的认证信息（Session 或 Token），则注入用户到上下文
// 如果没有提供认证信息或认证无效，不会阻止请求，继续处理（用户为 nil）
func OptionalAuth(svc *service.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 判断客户端类型
		isMobile := strings.EqualFold(c.GetHeader("X-Client-Type"), "mobile")

		if isMobile {
			// 移动端流程：尝试验证 Bearer Access Token
			token := bearerToken(c)
			if token != "" {
				userID, err := svc.ParseAccessToken(token)
				if err == nil {
					user, err := svc.GetUserByID(userID)
					if err == nil && user != nil {
						c.Set(CtxUserKey, user)
					}
				}
			}
		} else {
			// Web端流程：尝试验证 Session Cookie
			sessionToken, err := c.Cookie(svc.Cfg.SessionCookieName)
			if err == nil && sessionToken != "" {
				session, err := svc.ValidateSession(sessionToken)
				if err == nil {
					user, err := svc.GetUserByID(session.UserID)
					if err == nil && user != nil {
						c.Set(CtxUserKey, user)
						c.Set("sessionID", session.ID)
					}
				}
			}
		}

		// 无论是否认证成功，都继续处理请求
		c.Next()
	}
}

// BotAuthRequired 用于机器人的 Bearer token 验证。该中间件会根据机器人 token 查找 Robot 记录，
// 并将对应的 Bot User 注入到 Context(使用相同的 CtxUserKey)，同时把 Robot 对象放在 key "robot" 下。
func BotAuthRequired(svc *service.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerToken(c)
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未认证"})
			return
		}
		rb, err := svc.GetBotByToken(token)
		if err != nil || rb == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "机器人令牌无效"})
			return
		}
		// Ensure BotUser is loaded
		if rb.BotUser == nil && rb.BotUserID != 0 {
			if u, err := svc.GetUserByID(rb.BotUserID); err == nil {
				rb.BotUser = u
			}
		}
		if rb.BotUser == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "机器人用户不存在"})
			return
		}
		// Inject user and robot into context
		c.Set(CtxUserKey, rb.BotUser)
		c.Set("robot", rb)
		c.Next()
	}
}
