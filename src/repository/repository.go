package repository

import (
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// Repo is the core repository struct providing database and Redis access.
// Domain-specific methods are defined in their respective files:
//   - user_repository.go       (Users, Robots, SecurityEvents, Greetings)
//   - guild_repository.go      (Guilds, Members, JoinRequests, Roles, Announcements, GuildFiles)
//   - channel_repository.go    (Channels, Categories)
//   - message_repository.go    (Messages, Reactions, PinnedMessages, Favorites, GuildMedia)
//   - dm_repository.go         (DmThreads, Blacklist, DmMessages)
//   - friend_repository.go     (Friendships, FriendRequests)
//   - notification_repository.go (Notifications, Applications)
//   - group_repository.go      (GroupThreads, GroupMembers, GroupMessages)
//   - subroom_repository.go    (SubRooms, SubRoomMembers, SubRoomMessages)
//   - livekit_repository.go    (LiveKitRooms, LiveKitParticipants, Stats)
//   - readstate_repository.go  (ReadState / 红点系统)
//   - session_redis.go         (Redis session management)
//   - robot_repository.go      (Robot commands)
//   - pagination_repository.go (Cursor/Page pagination helpers)
//   - trusted_device_repository.go (Trusted device management)
type Repo struct {
	DB    *gorm.DB
	Redis *redis.Client
}

// New creates a Repo backed by a GORM DB connection.
func New(db *gorm.DB) *Repo { return &Repo{DB: db} }

// NewWithRedis creates a Repo with both GORM and Redis support.
func NewWithRedis(db *gorm.DB, rdb *redis.Client) *Repo {
	return &Repo{DB: db, Redis: rdb}
}
