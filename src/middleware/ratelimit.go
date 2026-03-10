package middleware

import (
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// token bucket limiter
type bucket struct {
	rate   float64 // tokens per second
	burst  float64 // capacity
	mu     sync.Mutex
	tokens float64
	last   time.Time
}

func newBucket(rate float64, burst int) *bucket {
	return &bucket{rate: rate, burst: float64(burst), tokens: float64(burst), last: time.Now()}
}

func (b *bucket) allow() (ok bool, remaining float64, resetAfter time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	// refill
	elapsed := now.Sub(b.last).Seconds()
	b.tokens = math.Min(b.burst, b.tokens+elapsed*b.rate)
	b.last = now
	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true, b.tokens, 0
	}
	// seconds to next token
	need := 1.0 - b.tokens
	reset := time.Duration(math.Ceil(need/b.rate*1000)) * time.Millisecond
	if reset < time.Second {
		reset = time.Second
	}
	return false, b.tokens, reset
}

// NewRateLimiter returns a Gin middleware limiting requests per token/IP.
// rateRPS: tokens added per second; burst: bucket capacity.
func NewRateLimiter(rateRPS int, burst int) gin.HandlerFunc {
	if rateRPS <= 0 {
		rateRPS = 5
	}
	if burst <= 0 {
		burst = 10
	}
	var (
		mu      sync.Mutex
		buckets = map[string]*bucket{}
	)
	get := func(key string) *bucket {
		mu.Lock()
		defer mu.Unlock()
		if b := buckets[key]; b != nil {
			return b
		}
		b := newBucket(float64(rateRPS), burst)
		buckets[key] = b
		return b
	}
	return func(c *gin.Context) {
		key := keyFromReq(c)
		b := get(key)
		ok, remaining, retry := b.allow()
		// headers
		c.Header("X-RateLimit-Limit", intToStr(rateRPS))
		c.Header("X-RateLimit-Remaining", intToStr(int(math.Floor(remaining))))
		if !ok {
			// RFC 标准头
			c.Header("Retry-After", formatSeconds(retry))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limited", "retryAfter": int(retry.Seconds())})
			return
		}
		c.Next()
	}
}

// DefaultRateLimiter 返回默认速率限制中间件（3 req/s, burst 6）
// 适用于大多数普通API
func DefaultRateLimiter() gin.HandlerFunc {
	return NewRateLimiter(3, 6)
}

// StrictRateLimiter 返回严格速率限制中间件（1 req/s, burst 2）
// 适用于敏感操作：登录、注册、密码重置等
func StrictRateLimiter() gin.HandlerFunc {
	return NewRateLimiter(1, 2)
}

// LooseRateLimiter 返回宽松速率限制中间件（10 req/s, burst 20）
// 适用于读取操作：查询、列表等
func LooseRateLimiter() gin.HandlerFunc {
	return NewRateLimiter(10, 20)
}

// ModerateRateLimiter 返回中等速率限制中间件（5 req/s, burst 10）
// 适用于写入操作：发消息、上传文件等
func ModerateRateLimiter() gin.HandlerFunc {
	return NewRateLimiter(5, 10)
}

// TokenRateLimiter 返回Token相关速率限制中间件（15 req/s, burst 30）
// 适用于token刷新、二维码轮询等高频操作
func TokenRateLimiter() gin.HandlerFunc {
	return NewRateLimiter(15, 30)
}

// BotRateLimiter 返回机器人速率限制中间件（20 req/s, burst 40）
// 适用于机器人API调用，需要更高频率处理服务器事件
func BotRateLimiter() gin.HandlerFunc {
	return NewRateLimiter(20, 40)
}

func keyFromReq(c *gin.Context) string {
	// prefer bearer token; fallback to client IP
	auth := c.GetHeader("Authorization")
	if auth != "" {
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
			return "tok:" + parts[1]
		}
		return "tok:" + auth
	}
	return "ip:" + c.ClientIP()
}

func intToStr(v int) string { return fmtInt(v) }

// small fast int to string without strconv import bloat
func fmtInt(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

func formatSeconds(d time.Duration) string {
	s := int(math.Ceil(d.Seconds()))
	if s < 1 {
		s = 1
	}
	return fmtInt(s)
}
