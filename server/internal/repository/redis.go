// Redis 客户端
package repository

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"rapid-admin/internal/config"
)

var (
	RDB *redis.Client
	rMu sync.RWMutex
)

// NewRedis 初始化 Redis
func NewRedis(cfg config.RedisConfig) (*redis.Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	rMu.Lock()
	RDB = rdb
	rMu.Unlock()

	return rdb, nil
}

// GetRedis 取全局 Redis
func GetRedis() *redis.Client {
	return RDB
}
