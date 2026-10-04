package main

// build check: dummy sync commit

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cardflow-backend/internal/config"
	"cardflow-backend/internal/database"
	"cardflow-backend/internal/server"
)

//go:embed dist/*
var embeddedFrontend embed.FS

func main() {
	// 1. Load configuration and setup structured logger
	cfg := config.Load()
	config.SetupLogger(cfg.Env)

	slog.Info("Starting CardFlow Modular Monolith API Server...", "env", cfg.Env, "port", cfg.Port)

	// 2. Initialize Database and Redis connections
	// Generous: connecting retries while a sleeping database wakes, and the
	// migrations run on the same context.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	dbPool, err := database.NewPostgresPool(ctx, cfg)
	if err != nil {
		slog.Warn("PostgreSQL pool initialization note", "error", err)
	} else {
		defer dbPool.Close()
		if migErr := database.RunMigrations(ctx, dbPool); migErr != nil {
			slog.Error("Database migrations failed", "error", migErr)
		}
	}

	if dbPool == nil || dbPool.Pool == nil {
		slog.Error("PostgreSQL is NOT connected — card save, business post, and vault will fail. Set DATABASE_URL in Render Environment (Neon/Supabase free tier). See docs/FREE_HOSTING_GUIDE.md")
	} else {
		slog.Info("PostgreSQL ready for card vault and business persistence")
	}

	redisClient, err := database.NewRedisClient(ctx, cfg)
	if err != nil {
		slog.Warn("Redis connection note", "error", err)
	} else {
		defer redisClient.Close()
	}

	var frontend fs.FS
	if subFS, err := fs.Sub(embeddedFrontend, "dist"); err == nil {
		frontend = subFS
	}
	r, _ := server.New(server.Deps{Cfg: cfg, DB: dbPool, Redis: redisClient, Frontend: frontend})

	// 7. Start HTTP Server with Graceful Shutdown
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info(fmt.Sprintf("CardFlow Go Backend & Frontend running at http://localhost:%s", cfg.Port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server listen error", "error", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("Shutting down server gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
	}

	slog.Info("Server stopped cleanly")
}

type ioReadSeeker interface {
	fs.File
	Seek(offset int64, whence int) (int64, error)
}
