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
	"strings"
	"syscall"
	"time"

	"cardflow-backend/internal/auth"
	"cardflow-backend/internal/billing"
	"cardflow-backend/internal/business"
	"cardflow-backend/internal/card"
	"cardflow-backend/internal/config"
	"cardflow-backend/internal/contacts"
	"cardflow-backend/internal/crm"
	"cardflow-backend/internal/database"
	"cardflow-backend/internal/discovery"
	"cardflow-backend/internal/enquiry"
	"cardflow-backend/internal/extractor"
	"cardflow-backend/internal/middleware"
	"cardflow-backend/internal/storage"
	"cardflow-backend/internal/support"
	"cardflow-backend/pkg/response"
	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
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

	// Ajay's CRM lives in its own module/schema; it can only disable itself, never CardFlow.
	var crmPool *pgxpool.Pool
	if dbPool != nil {
		crmPool = dbPool.Pool
	}
	crmModule := crm.New(context.Background(), crmPool, cfg.Env)

	redisClient, err := database.NewRedisClient(ctx, cfg)
	if err != nil {
		slog.Warn("Redis connection note", "error", err)
	} else {
		defer redisClient.Close()
	}

	// 3. Initialize Services
	jwtSvc := auth.NewJWTService(cfg)
	authSvc := auth.NewAuthService(dbPool, redisClient, jwtSvc, cfg)
	discoverySvc := discovery.NewDiscoveryService(dbPool)
	businessSvc := business.NewBusinessService(dbPool)
	var s3Svc *storage.S3Service
	if cfg.S3Enabled() {
		svc, s3err := storage.NewS3Service(context.Background(), cfg)
		if s3err != nil {
			slog.Warn("S3 client not available; original images will store in PostgreSQL", "error", s3err)
		} else {
			s3Svc = svc
		}
	} else {
		slog.Info("S3 disabled (localhost or unset); original card images persist in PostgreSQL")
	}
	geminiSvc := extractor.NewGeminiService(cfg)
	cardSvc := card.NewCardService(dbPool, s3Svc, geminiSvc)
	// Link cards saved before one-business-per-GSTIN existed (idempotent).
	go cardSvc.BackfillCardBusinesses(context.Background())

	// 4. Initialize Handlers & Middlewares
	appMiddleware := middleware.NewMiddleware(jwtSvc, dbPool)
	authHandler := auth.NewAuthHandler(authSvc)
	discoveryHandler := discovery.NewDiscoveryHandler(discoverySvc)
	businessHandler := business.NewBusinessHandler(businessSvc)
	cardHandler := card.NewCardHandler(cardSvc, s3Svc)
	enquiryHandler := enquiry.NewEnquiryHandler(dbPool)
	billingHandler := billing.NewBillingHandler(dbPool, cfg)
	contactsHandler := contacts.NewContactsHandler(dbPool)
	supportHandler := support.NewSupportHandler(dbPool)

	// 5. Setup Router & Routes
	r := chi.NewRouter()

	// Global Middlewares
	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.RealIP)
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.Timeout(60 * time.Second))
	r.Use(chiMiddleware.RequestSize(32 << 20)) // 32MB for card/business image uploads

	// CORS Configuration (Permissive for Web Browsers & Testing)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD"},
		AllowedHeaders:   []string{"*"},
		ExposedHeaders:   []string{"*"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// Health Check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		dbStatus := "disconnected"
		if dbPool != nil && dbPool.Pool != nil {
			pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			if dbPool.Pool.Ping(pingCtx) == nil {
				dbStatus = "connected"
			}
			cancel()
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"status":    "healthy",
			"timestamp": time.Now(),
			"version":   "1.0.0",
			"env":       cfg.Env,
			"database":  dbStatus,
		})
	})
	r.Head("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Public Web Profile Route (e.g. https://cardflow-api-fsij.onrender.com/b/kovai-precision-tools)
	r.Get("/b/{slug}", discoveryHandler.RenderPublicHTML)

	// RevenueCat server-to-server webhook (no user JWT) — authenticated by the
	// Authorization header configured in the RevenueCat dashboard.
	r.Post("/api/webhooks/revenuecat", billingHandler.Webhook)

	// API v1 Routes
	r.Route("/api/v1", func(r chi.Router) {
		// 1. Auth Endpoints
		r.Route("/auth", func(r chi.Router) {
			r.Post("/otp/send", authHandler.SendOTP)
			r.Post("/otp/verify", authHandler.VerifyOTP)
			r.Post("/refresh", authHandler.RefreshToken)
			r.Post("/logout-all", authHandler.LogoutAll)
		})

		// 2. Discovery Endpoints (Public)
		r.Get("/categories", discoveryHandler.GetCategories)
		r.Get("/businesses/search", discoveryHandler.SearchBusinesses)
		r.Get("/businesses/{id}", discoveryHandler.GetBusiness)
		r.Get("/businesses/slug/{slug}", discoveryHandler.GetBusinessBySlug)
		r.Post("/cards/scan", cardHandler.ScanCard)
		// Shareable card link — recipient may not have an account yet.
		r.Get("/public/cards/{id}", cardHandler.PublicGetCard)
		r.Get("/public/cards/{id}/original-image", cardHandler.PublicGetOriginalImage)

		// 3. User Account Endpoints (Protected)
		r.Group(func(r chi.Router) {
			r.Use(appMiddleware.Authenticate)

			r.Get("/users/me", authHandler.GetMe)
			r.Patch("/users/me", authHandler.UpdateMe)
			r.Patch("/users/me/phone", authHandler.ChangePhone)
			r.Delete("/users/me", authHandler.DeleteMe)
			r.Get("/users/me/export", authHandler.ExportMe)

			r.Get("/businesses/{id}/card-image", businessHandler.GetCardImage)

			// Card Vault & OCR Scanner
			r.Get("/cards", cardHandler.ListCards)
			r.Post("/cards", cardHandler.CreateCard)
			r.Patch("/cards/{id}", cardHandler.UpdateCard)
			r.Get("/cards/{id}/original-image", cardHandler.GetOriginalImage)
			r.Post("/cards/{id}/original-image", cardHandler.UploadOriginalImage)
			r.Post("/cards/upload-url", cardHandler.GetUploadURL)
			r.Post("/cards/scan", cardHandler.ScanCard)
			r.Delete("/cards/{id}", cardHandler.DeleteCard)

			// Customer Enquiries & In-App Support Tickets
			r.Post("/enquiries", enquiryHandler.CreateEnquiry)
			r.Post("/support/tickets", supportHandler.CreateTicket)
			r.Get("/support/tickets/my", supportHandler.GetMyTickets)
			r.Get("/support/tickets/my/{id}", supportHandler.GetMyTicket)
			r.Post("/support/tickets/my/{id}/messages", supportHandler.AddMyMessage)

			// Billing: CardFlow Premium via RevenueCat (purchases happen in the SDKs)
			r.Get("/billing/status", billingHandler.GetStatus)
			r.Post("/billing/sync", billingHandler.Sync)
			r.Get("/billing/transactions", billingHandler.GetTransactions)
			r.Get("/billing/credits", billingHandler.GetCredits)

			// Phone contacts backup/restore (native app only — the web build never calls these)
			r.Post("/contacts/backup", contactsHandler.BackupContacts)
			r.Get("/contacts/backup/status", contactsHandler.GetBackupStatus)
			r.Get("/contacts/backup", contactsHandler.GetBackup)

			// Business Owner Endpoints (Multi-Business 1..N)
			r.Route("/owner", func(r chi.Router) {
				r.Get("/businesses", businessHandler.ListMyBusinesses)
				r.Post("/businesses", businessHandler.CreateBusiness)
				r.Patch("/businesses/{id}", businessHandler.UpdateBusiness)
				r.Post("/businesses/{id}/card-image", businessHandler.UploadCardImage)
				r.Get("/businesses/{id}/card-image", businessHandler.GetCardImage)
				r.Get("/businesses/{id}/analytics", businessHandler.GetBusinessAnalytics)
				r.Post("/businesses/{id}/verify/gst", businessHandler.VerifyGST)
				r.Get("/businesses/{id}/card", businessHandler.GetDigitalCard)
				r.Get("/businesses/{id}/enquiries", enquiryHandler.ListBusinessEnquiries)
			})
		})
	})

	crmModule.Mount(r)

	// 6. Serve Embedded Production Frontend Web Application at Root
	subFS, err := fs.Sub(embeddedFrontend, "dist")
	if err == nil {
		fileServer := http.FileServer(http.FS(subFS))
		r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
			path := strings.TrimPrefix(req.URL.Path, "/")
			// If file exists in embedded dist, serve it
			if f, err := subFS.Open(path); err == nil && path != "" {
				_ = f.Close()
				fileServer.ServeHTTP(w, req)
				return
			}
			// Otherwise serve index.html for React SPA client-side routing
			indexData, err := fs.ReadFile(subFS, "index.html")
			if err == nil {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(indexData)
			} else {
				fileServer.ServeHTTP(w, req)
			}
		})
	}

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
