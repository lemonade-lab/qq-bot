package db

import (
	"bubble/src/logger"
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func New() *gorm.DB {
	return NewWith("mysql", "root:password@tcp(localhost:3306)/bubble?charset=utf8mb4&parseTime=True&loc=Local")
}

// NewWith 仅支持 MySQL
func NewWith(driver, dsn string) *gorm.DB {
	var (
		gdb *gorm.DB
		err error
	)
	switch driver {
	case "mysql":
		gdb, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	default:
		logger.Fatalf("unsupported db driver: %s (only mysql is supported)", driver)
	}
	if err != nil {
		logger.Fatalf("failed to connect database: %v", err)
	}
	return gdb
}

// NewRedis creates a new Redis client for session storage
func NewRedis(addr, password string, db int) *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		DialTimeout:  10 * time.Second, // 增加连接超时到 10 秒
		ReadTimeout:  5 * time.Second,  // 读取超时 5 秒
		WriteTimeout: 5 * time.Second,  // 写入超时 5 秒
		PoolSize:     10,               // 连接池大小
		MinIdleConns: 2,                // 最小空闲连接数
		Protocol:     2,                // 尝试 RESP2 协议
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		logger.Fatalf("failed to connect redis: %v", err)
	}

	logger.Infof("redis connected: %s db=%d", addr, db)
	return rdb
}

func AutoMigrate(db *gorm.DB, models ...any) {
	// 预迁移修复：将 phone 空字符串改为 NULL，避免唯一索引冲突
	if db.Dialector.Name() == "mysql" {
		db.Exec("UPDATE `users` SET `phone` = NULL WHERE `phone` = ''")
	}

	if err := db.AutoMigrate(models...); err != nil {
		panic(fmt.Errorf("auto migrate failed: %w", err))
	}

	// 手动迁移：确保 live_kit_rooms 表有 room_sid 字段
	// 因为 GORM AutoMigrate 可能不会自动添加字段到已存在的表
	driver := db.Dialector.Name()
	if driver == "mysql" {
		// 检查字段是否存在
		var count int64
		db.Raw(`
			SELECT COUNT(*) FROM information_schema.COLUMNS 
			WHERE TABLE_SCHEMA = DATABASE() 
			AND TABLE_NAME = 'live_kit_rooms' 
			AND COLUMN_NAME = 'room_sid'
		`).Scan(&count)

		if count == 0 {
			logger.Infof("Adding missing column 'room_sid' to live_kit_rooms table")
			if err := db.Exec("ALTER TABLE `live_kit_rooms` ADD COLUMN `room_sid` VARCHAR(128) DEFAULT '' AFTER `room_name`").Error; err != nil {
				logger.Warnf("Failed to add room_sid column: %v", err)
			} else {
				logger.Infof("Successfully added room_sid column to live_kit_rooms table")
			}
		}
	}

	// 设置User和Guild表的自增ID起始值为100000001
	// 通过GORM的命名策略获取实际表名
	for _, model := range models {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(model); err != nil {
			continue
		}
		tableName := stmt.Schema.Table

		// 只对User和Guild表设置自增起始值
		if tableName == "users" || tableName == "guilds" {
			setAutoIncrementStart(db, tableName, 100000001)
		}
	}
}

// setAutoIncrementStart 设置指定表的自增ID起始值
func setAutoIncrementStart(db *gorm.DB, tableName string, startValue uint) {
	driver := db.Dialector.Name()

	switch driver {
	case "mysql":
		// MySQL使用ALTER TABLE设置AUTO_INCREMENT
		// 检查表中是否已有数据
		var maxID uint
		db.Table(tableName).Select("COALESCE(MAX(id), 0)").Scan(&maxID)

		// 如果表中已有ID大于等于起始值的数据，则不修改AUTO_INCREMENT
		if maxID >= startValue {
			logger.Infof("table %s already has ID >= %d, skipping AUTO_INCREMENT setup", tableName, startValue)
			return
		}

		// 使用maxID和startValue中的较大值作为AUTO_INCREMENT起始值
		targetValue := startValue
		if maxID > 0 && maxID >= startValue {
			targetValue = maxID + 1
		}

		sql := fmt.Sprintf("ALTER TABLE `%s` AUTO_INCREMENT = %d", tableName, targetValue)
		if err := db.Exec(sql).Error; err != nil {
			logger.Warnf("failed to set AUTO_INCREMENT for table %s: %v", tableName, err)
		} else {
			logger.Infof("set AUTO_INCREMENT for table %s to %d", tableName, targetValue)
		}
	default:
		logger.Warnf("unsupported database driver %s for setting auto increment (only mysql is supported)", driver)
	}
}
