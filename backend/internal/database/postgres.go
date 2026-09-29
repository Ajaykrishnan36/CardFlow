package database

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"cardflow-backend/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

func normalizeDatabaseURL(connStr string) string {
	connStr = strings.TrimSpace(connStr)
	if connStr == "" {
		return connStr
	}
	if !strings.Contains(connStr, "sslmode=") {
		sep := "?"
		if strings.Contains(connStr, "?") {
			sep = "&"
		}
		connStr += sep + "sslmode=require"
	}
	return connStr
}

type DB struct {
	Pool *pgxpool.Pool
}

func NewPostgresPool(ctx context.Context, cfg *config.Config) (*DB, error) {
	var connStr string
	if cfg.DatabaseURL != "" {
		connStr = normalizeDatabaseURL(cfg.DatabaseURL)
	} else {
		connStr = fmt.Sprintf(
			"postgres://%s:%s@%s:%s/%s?sslmode=%s",
			cfg.DBUser,
			cfg.DBPassword,
			cfg.DBHost,
			cfg.DBPort,
			cfg.DBName,
			cfg.DBSSLMode,
		)
	}

	poolConfig, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("unable to parse db config: %w", err)
	}

	poolConfig.MaxConns = int32(cfg.DBMaxOpenConns)
	// No always-open connections, and idle ones close after 2 minutes: an open
	// connection keeps a serverless database (Neon free tier) awake and burning
	// its compute allowance even when nobody is using the app.
	poolConfig.MinConns = 0
	poolConfig.MaxConnIdleTime = 2 * time.Minute

	if lifetime, err := time.ParseDuration(cfg.DBConnMaxLifetime); err == nil {
		poolConfig.MaxConnLifetime = lifetime
	} else {
		poolConfig.MaxConnLifetime = 5 * time.Minute
	}

	// A serverless database (Neon free tier) sleeps when idle and can take
	// several seconds to wake — and the free web instance wakes it at the same
	// moment. Retry for up to ~90s instead of running without a database until
	// the next restart.
	var pool *pgxpool.Pool
	var lastErr error
	for attempt := 1; attempt <= 7; attempt++ {
		pool, err = pgxpool.NewWithConfig(ctx, poolConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create postgres pool: %w", err)
		}
		pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		lastErr = pool.Ping(pingCtx)
		cancel()
		if lastErr == nil {
			break
		}
		pool.Close()
		pool = nil
		slog.Warn("Postgres not reachable yet, retrying", "attempt", attempt, "error", lastErr)
		if ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(3 * time.Second):
		}
	}
	if pool == nil {
		slog.Warn("Postgres connection unavailable (falling back to memory state safely)", "error", lastErr)
		return nil, lastErr
	}

	slog.Info("Connected to PostgreSQL + PostGIS database successfully")
	return &DB{Pool: pool}, nil
}

func (db *DB) Close() {
	if db != nil && db.Pool != nil {
		db.Pool.Close()
	}
}
