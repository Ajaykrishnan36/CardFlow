// Package crm is Ajay's CRM, a modular monolith living alongside CardFlow in the same
// binary. Everything it owns sits under /api/crm/v1 and the Postgres schema "crm";
// see docs/DECISIONS.md for how and why it differs from the PRD's greenfield layout.
package crm

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"cardflow-backend/internal/crm/connectors/cardflow"
	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/mail"
	"cardflow-backend/internal/crm/platform"
	"cardflow-backend/internal/crm/records"
	"cardflow-backend/internal/crm/seed"
	"cardflow-backend/internal/crm/shared"
	"cardflow-backend/internal/crm/store"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Module struct {
	cfg            shared.Config
	disabledReason string
	identity       *identity.Service
	platform       *platform.Handler
	records        *records.Handler
	cardflow       *cardflow.Connector
}

// New prepares the CRM: config, migrations, safety checks and seed. It never returns
// an error that could stop CardFlow — on any problem it returns a module that answers
// 503 crm_disabled and logs why (DECISIONS D-06).
func New(ctx context.Context, pool *pgxpool.Pool, cardflowEnv string) *Module {
	cfg, problems := shared.LoadConfig(cardflowEnv)
	m := &Module{cfg: cfg}

	if pool == nil {
		return m.disable("PostgreSQL is not connected")
	}
	if len(problems) > 0 {
		return m.disable(strings.Join(problems, "; "))
	}

	st := store.New(pool)
	migrateCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := st.Migrate(migrateCtx); err != nil {
		return m.disable("migrations failed: " + err.Error())
	}
	if err := seed.CheckProductionSafety(migrateCtx, st, cfg); err != nil {
		return m.disable("production safety check failed: " + err.Error())
	}
	if err := seed.Run(migrateCtx, st, cfg); err != nil {
		return m.disable("seed failed: " + err.Error())
	}

	mailer := mail.New(cfg)
	m.identity = identity.NewService(st, cfg, mailer)
	m.platform = platform.NewHandler(st, cfg, mailer, m.identity)
	m.records = records.NewHandler(st, cfg, m.platform)
	// Connected app: Business Card Snap (CardFlow) users, sign-ins and support tickets (D-36).
	if os.Getenv("CRM_CARDFLOW_SYNC") != "false" {
		m.cardflow = cardflow.New(st, cfg, m.platform)
		m.records.Extend(m.cardflow.Extension())
		m.cardflow.Start(context.Background())
	}
	slog.Info("CRM module ready", "env", cfg.AppEnv, "base_url", cfg.BaseURL)
	return m
}

func (m *Module) disable(reason string) *Module {
	m.disabledReason = reason
	slog.Error("CRM module disabled — CardFlow keeps running", "reason", reason, "env", m.cfg.AppEnv)
	return m
}

// Mount registers every CRM API route under /api/crm/v1.
func (m *Module) Mount(r chi.Router) {
	r.Route("/api/crm/v1", func(r chi.Router) {
		r.Use(securityHeaders)
		if m.disabledReason != "" {
			r.HandleFunc("/*", func(w http.ResponseWriter, req *http.Request) {
				shared.WriteError(w, req, shared.ServiceUnavailable("crm_disabled", "The CRM is temporarily unavailable."))
			})
			return
		}
		r.Use(m.identity.CSRF)
		r.Use(m.identity.LoadSession)

		r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
			shared.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "app": m.cfg.AppName})
		})
		m.identity.Routes(r)
		m.platform.Routes(r)
		m.records.Routes(r)
		if m.cardflow != nil {
			r.Group(func(r chi.Router) {
				r.Use(identity.RequireOwner)
				m.cardflow.OwnerRoutes(r)
			})
		}

		r.NotFound(func(w http.ResponseWriter, req *http.Request) {
			shared.WriteError(w, req, shared.NotFound("route_not_found"))
		})
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}
