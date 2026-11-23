package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"cogniz-crm-backend/internal/config"
	"github.com/redis/go-redis/v9"
)

type RedisClient struct {
	Client *redis.Client
}

func NewRedisClient(ctx context.Context, cfg *config.Config) (*RedisClient, error) {
	var opts *redis.Options
	if cfg.RedisURL != "" {
		parsedOpts, err := redis.ParseURL(cfg.RedisURL)
		if err == nil {
			opts = parsedOpts
		}
	}
	if opts == nil {
		opts = &redis.Options{
			Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		}
	}

	rdb := redis.NewClient(opts)

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := rdb.Ping(pingCtx).Err(); err != nil {
		slog.Warn("Redis connection unavailable (falling back to memory state safely)", "error", err)
		_ = rdb.Close()
		return nil, err
	}

	slog.Info("Connected to Redis successfully")
	return &RedisClient{Client: rdb}, nil
}

func (r *RedisClient) Close() error {
	if r != nil && r.Client != nil {
		return r.Client.Close()
	}
	return nil
}
