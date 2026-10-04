// Package server builds the HTTP handler: the app's /api/v1 routes, the CRM under
// /api/crm/v1 and the embedded web bundle. main starts it; the end-to-end tests call it
// against a throwaway database.
package server

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
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

// originAllowed decides which web origins may call the API from a browser.
func originAllowed(cfg *config.Config) func(r *http.Request, origin string) bool {
	allowed := map[string]bool{}
	for _, o := range cfg.AllowedOrigins {
		if o = strings.TrimRight(strings.TrimSpace(o), "/"); o != "" {
			allowed[o] = true
		}
	}
	return func(r *http.Request, origin string) bool {
		if !cfg.IsProduction() {
			return true
		}
		origin = strings.TrimRight(origin, "/")
		if allowed[origin] {
			return true
		}
		// The Capacitor shells (Android: https://localhost, iOS: capacitor://localhost).
		return origin == "https://localhost" || origin == "capacitor://localhost" || origin == "ionic://localhost"
	}
}

// Deps is what the handler is built from. Frontend may be nil (tests).
type Deps struct {
	Cfg      *config.Config
	DB       *database.DB
	Redis    *database.RedisClient
	Frontend fs.FS
}

// New wires every service and route and returns the handler with the CRM module.
func New(d Deps) (http.Handler, *crm.Module) {
	// Ajay's CRM lives in its own module/schema; it can only disable itself, never CardFlow.
	var crmPool *pgxpool.Pool
	if d.DB != nil {
		crmPool = d.DB.Pool
	}
	crmModule := crm.New(context.Background(), crmPool, d.Cfg.Env)

	// 3. Initialize Services
	jwtSvc := auth.NewJWTService(d.Cfg)
	authSvc := auth.NewAuthService(d.DB, d.Redis, jwtSvc, d.Cfg)
	// One identity for the app and the CRM (D-93): the app's sign-in codes and sessions
	// come from the CRM identity service.
	authSvc.SetIdentity(crmModule.Identity())
	discoverySvc := discovery.NewDiscoveryService(d.DB)
	businessSvc := business.NewBusinessService(d.DB)
	var s3Svc *storage.S3Service
	if d.Cfg.S3Enabled() {
		svc, s3err := storage.NewS3Service(context.Background(), d.Cfg)
		if s3err != nil {
			slog.Warn("S3 client not available; original images will store in PostgreSQL", "error", s3err)
		} else {
			s3Svc = svc
		}
	} else {
		slog.Info("S3 disabled (localhost or unset); original card images persist in PostgreSQL")
	}
	geminiSvc := extractor.NewGeminiService(d.Cfg)
	cardSvc := card.NewCardService(d.DB, s3Svc, geminiSvc)
	// Link cards saved before one-business-per-GSTIN existed (idempotent).
	go cardSvc.BackfillCardBusinesses(context.Background())

	// 4. Initialize Handlers & Middlewares
	appMiddleware := middleware.NewMiddleware(jwtSvc, d.DB)
	appMiddleware.SetIdentity(crmModule.Identity())
	authHandler := auth.NewAuthHandler(authSvc)
	discoveryHandler := discovery.NewDiscoveryHandler(discoverySvc)
	businessHandler := business.NewBusinessHandler(businessSvc)
	cardHandler := card.NewCardHandler(cardSvc, s3Svc)
	enquiryHandler := enquiry.NewEnquiryHandler(d.DB)
	billingHandler := billing.NewBillingHandler(d.DB, d.Cfg)
	contactsHandler := contacts.NewContactsHandler(d.DB)
	supportHandler := support.NewSupportHandler(d.DB)

	// 5. Setup Router & Routes
	r := chi.NewRouter()

	// Global Middlewares
	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.RealIP)
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.Timeout(60 * time.Second))
	r.Use(chiMiddleware.RequestSize(32 << 20)) // 32MB for card/business image uploads

	// CORS. The app's web build (another origin) and the native shells call this API with
	// a bearer token, never cookies, so credentials stay off. In production only the
	// configured origins and the native shells are allowed; elsewhere any origin is.
	r.Use(cors.Handler(cors.Options{
		AllowOriginFunc:  originAllowed(d.Cfg),
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "Idempotency-Key", "X-CSRF-Token", "X-Session-Transport", "X-Requested-With", "Accept"},
		ExposedHeaders:   []string{"X-Session-Token", "Retry-After", "X-Request-Id"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// Health Check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		dbStatus := "disconnected"
		if d.DB != nil && d.DB.Pool != nil {
			pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			if d.DB.Pool.Ping(pingCtx) == nil {
				dbStatus = "connected"
			}
			cancel()
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"status":    "healthy",
			"timestamp": time.Now(),
			"version":   "1.0.0",
			"env":       d.Cfg.Env,
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
	if subFS := d.Frontend; subFS != nil {
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

	return r, crmModule
}
