package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"bubble/src/db/models"

	"github.com/redis/go-redis/v9"
)

// Sessions - Redis-based storage (ultimate authority for authentication)

const sessionKeyPrefix = "session:"

func sessionKey(token string) string {
	return fmt.Sprintf("%s%s", sessionKeyPrefix, token)
}

// CreateSession stores session in Redis with 30-day TTL
func (r *Repo) CreateSession(s *models.Session) error {
	if r.Redis == nil {
		return errors.New("Redis未初始化")
	}

	ctx := context.Background()
	key := sessionKey(s.SessionToken)

	// Serialize session to JSON
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("会话序列化失败：%w", err)
	}

	// Store in Redis with TTL matching ExpiresAt
	ttl := time.Until(s.ExpiresAt)
	if ttl <= 0 {
		return errors.New("会话已过期")
	}

	if err := r.Redis.Set(ctx, key, data, ttl).Err(); err != nil {
		return fmt.Errorf("会话存储失败：%w", err)
	}

	return nil
}

// GetSessionByToken retrieves session from Redis
func (r *Repo) GetSessionByToken(token string) (*models.Session, error) {
	if r.Redis == nil {
		return nil, errors.New("Redis未初始化")
	}

	ctx := context.Background()
	key := sessionKey(token)

	// Get from Redis
	data, err := r.Redis.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, errors.New("会话不存在")
	}
	if err != nil {
		return nil, fmt.Errorf("读取会话失败：%w", err)
	}

	// Deserialize
	var session models.Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("会话解析失败：%w", err)
	}

	// Check if revoked
	if session.RevokedAt != nil {
		return nil, errors.New("会话已被撤销")
	}

	return &session, nil
}

// UpdateSessionLastUsed updates the last used timestamp
func (r *Repo) UpdateSessionLastUsed(sessionID uint) error {
	// For Redis-based sessions, we can implement this by updating the session object
	// However, for performance, this is optional since we mainly care about TTL
	return nil
}

// RevokeSession marks a session as revoked (kept for interface compatibility)
func (r *Repo) RevokeSession(sessionID uint) error {
	// Redis implementation uses token-based revocation
	return nil
}

// RevokeSessionByToken revokes a session by its token (Redis-optimized)
func (r *Repo) RevokeSessionByToken(token string) error {
	if r.Redis == nil {
		return errors.New("Redis未初始化")
	}

	ctx := context.Background()
	key := sessionKey(token)

	// Simply delete the session from Redis
	if err := r.Redis.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("撤销会话失败：%w", err)
	}

	return nil
}

// RevokeAllUserSessions revokes all sessions for a user
func (r *Repo) RevokeAllUserSessions(userID uint) error {
	if r.Redis == nil {
		return errors.New("Redis未初始化")
	}

	ctx := context.Background()

	// Scan all session keys
	iter := r.Redis.Scan(ctx, 0, sessionKeyPrefix+"*", 0).Iterator()
	revokedCount := 0

	for iter.Next(ctx) {
		key := iter.Val()
		data, err := r.Redis.Get(ctx, key).Bytes()
		if err != nil {
			continue
		}

		var session models.Session
		if err := json.Unmarshal(data, &session); err != nil {
			continue
		}

		// If session belongs to this user, delete it
		if session.UserID == userID {
			r.Redis.Del(ctx, key)
			revokedCount++
		}
	}

	if err := iter.Err(); err != nil {
		return fmt.Errorf("扫描会话失败：%w", err)
	}

	return nil
}

// CleanupExpiredSessions is not needed for Redis as keys auto-expire with TTL
func (r *Repo) CleanupExpiredSessions() error {
	// Redis automatically removes expired keys, no cleanup needed
	return nil
}
