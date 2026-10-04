package plans

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/shared"
	"cardflow-backend/internal/crm/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Handler struct{ store *store.Store }

func NewHandler(st *store.Store) *Handler { return &Handler{store: st} }

// Overview is what a member sees on the plan page of their business.
type Overview struct {
	Entitlement
	Usage Usage  `json:"usage"`
	Plans []Plan `json:"plans"`
}

// Overview of one business: its plan, what it uses, and the plans on offer.
func (h *Handler) Overview(r *http.Request, ws uuid.UUID) (*Overview, error) {
	ctx := r.Context()
	ent, err := For(ctx, h.store.Pool, ws)
	if err != nil {
		return nil, err
	}
	usage, err := UsageOf(ctx, h.store.Pool, ws)
	if err != nil {
		return nil, err
	}
	list, err := List(ctx, h.store.Pool, true)
	if err != nil {
		return nil, err
	}
	return &Overview{Entitlement: *ent, Usage: usage, Plans: list}, nil
}

// OwnerRoutes: the platform owner manages plans and each business's subscription.
//
//	GET /platform/plans                         PUT /platform/plans/{key}
//	GET /platform/subscriptions                 PUT /platform/subscriptions/{workspaceId}
//	GET /platform/subscriptions/{workspaceId}/events
func (h *Handler) OwnerRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(identity.RequireOwner)
		r.Get("/platform/plans", h.handleListPlans)
		r.Put("/platform/plans/{key}", h.handleSavePlan)
		r.Get("/platform/subscriptions", h.handleListSubscriptions)
		r.Put("/platform/subscriptions/{workspaceId}", h.handleSetSubscription)
		r.Get("/platform/subscriptions/{workspaceId}/events", h.handleSubscriptionEvents)
	})
}

func (h *Handler) handleListPlans(w http.ResponseWriter, r *http.Request) {
	list, err := List(r.Context(), h.store.Pool, false)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": list})
}

var planKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{1,30}$`)

func (h *Handler) handleSavePlan(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	var in Plan
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	fe := map[string]string{}
	if !planKeyRe.MatchString(key) || key == "managed" {
		fe["key"] = "Use 2–31 lower-case letters, digits or underscores, starting with a letter."
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 60 {
		fe["name"] = "Enter a name (up to 60 characters)."
	}
	if in.PriceMonthly < 0 || in.PriceYearly < 0 {
		fe["priceMonthly"] = "A price can't be negative."
	}
	if in.Limits.Members < 0 || in.Limits.Records < 0 || in.Limits.CardScansPerMonth < 0 {
		fe["limits"] = "A limit can't be negative. Use 0 for unlimited."
	}
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if in.Currency == "" {
		in.Currency = "INR"
	}
	if len(in.Currency) != 3 {
		fe["currency"] = "Use a 3-letter currency code."
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	if in.Features == nil {
		in.Features = []string{}
	}
	limits, _ := json.Marshal(in.Limits)
	features, _ := json.Marshal(in.Features)
	me := identity.SessionFrom(r.Context()).IdentityID
	meta := identity.Meta(r)
	err := h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		if in.IsDefault {
			if _, err := tx.Exec(ctx, `UPDATE crm.plans SET is_default = false WHERE is_default AND key <> $1`, key); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.plans (key, name, description, price_monthly, price_yearly, currency, limits, features, is_default, is_active, position)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (key) DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description, price_monthly = EXCLUDED.price_monthly,
			  price_yearly = EXCLUDED.price_yearly, currency = EXCLUDED.currency, limits = EXCLUDED.limits, features = EXCLUDED.features,
			  is_default = EXCLUDED.is_default, is_active = EXCLUDED.is_active, position = EXCLUDED.position, updated_at = now()`,
			key, in.Name, strings.TrimSpace(in.Description), in.PriceMonthly, in.PriceYearly, in.Currency, limits, features, in.IsDefault, in.IsActive, in.Position); err != nil {
			return err
		}
		return shared.WriteAudit(ctx, tx, shared.AuditEvent{ActorID: &me, Action: "plan.saved", EntityType: "plan", After: in, IP: meta.IP, RequestID: meta.RequestID})
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	p, err := get(r.Context(), h.store.Pool, key)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, p)
}

type subscriptionRow struct {
	WorkspaceID string `json:"workspaceId"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Origin      string `json:"origin"`
	Owner       string `json:"owner"`
	Entitlement
	Usage Usage  `json:"usage"`
	Notes string `json:"notes,omitempty"`
}

func (h *Handler) handleListSubscriptions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := "%" + strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))) + "%"
	rows, err := h.store.Pool.Query(ctx, `
		SELECT w.id, w.code, w.name, w.origin, COALESCE(i.display_name, ''), COALESCE(s.notes, '')
		FROM crm.workspaces w
		LEFT JOIN crm.identities i ON i.id = w.created_by_identity
		LEFT JOIN crm.workspace_subscriptions s ON s.workspace_id = w.id
		WHERE NOT w.is_platform AND (lower(w.name) LIKE $1 OR lower(w.code) LIKE $1)
		ORDER BY w.created_at DESC LIMIT 200`, q)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	type head struct {
		id                               uuid.UUID
		code, name, origin, owner, notes string
	}
	heads := []head{}
	for rows.Next() {
		var x head
		if err := rows.Scan(&x.id, &x.code, &x.name, &x.origin, &x.owner, &x.notes); err != nil {
			rows.Close()
			shared.WriteError(w, r, err)
			return
		}
		heads = append(heads, x)
	}
	rows.Close()
	out := make([]subscriptionRow, 0, len(heads))
	for _, x := range heads {
		ent, err := For(ctx, h.store.Pool, x.id)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		usage, err := UsageOf(ctx, h.store.Pool, x.id)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		out = append(out, subscriptionRow{WorkspaceID: x.id.String(), Code: x.code, Name: x.name, Origin: x.origin, Owner: x.owner, Entitlement: *ent, Usage: usage, Notes: x.notes})
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) handleSetSubscription(w http.ResponseWriter, r *http.Request) {
	ws, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("workspace_not_found"))
		return
	}
	var in struct {
		PlanKey          string  `json:"planKey"`
		Status           string  `json:"status"`
		CurrentPeriodEnd *string `json:"currentPeriodEnd"`
		Notes            string  `json:"notes"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	fe := map[string]string{}
	if _, err := get(ctx, h.store.Pool, in.PlanKey); err != nil {
		fe["planKey"] = "Choose a plan."
	}
	if in.Status == "" {
		in.Status = "active"
	}
	if !map[string]bool{"active": true, "trialing": true, "past_due": true, "cancelled": true, "expired": true}[in.Status] {
		fe["status"] = "Choose a status."
	}
	var end *time.Time
	if in.CurrentPeriodEnd != nil && strings.TrimSpace(*in.CurrentPeriodEnd) != "" {
		t, err := time.Parse(time.RFC3339, *in.CurrentPeriodEnd)
		if err != nil {
			if t, err = time.Parse("2006-01-02", *in.CurrentPeriodEnd); err != nil {
				fe["currentPeriodEnd"] = "Enter a date."
			} else {
				t = t.Add(24*time.Hour - time.Second)
			}
		}
		end = &t
	}
	if len(in.Notes) > 500 {
		fe["notes"] = "Use at most 500 characters."
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	me := identity.SessionFrom(ctx).IdentityID
	meta := identity.Meta(r)
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.workspaces WHERE id = $1 AND NOT is_platform)`, ws).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return shared.NotFound("workspace_not_found")
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.workspace_subscriptions (workspace_id, plan_key, status, source, current_period_end, notes, updated_by)
			VALUES ($1, $2, $3, 'manual', $4, $5, $6)
			ON CONFLICT (workspace_id) DO UPDATE SET plan_key = EXCLUDED.plan_key, status = EXCLUDED.status, source = 'manual',
			  current_period_end = EXCLUDED.current_period_end, notes = EXCLUDED.notes, updated_by = EXCLUDED.updated_by, updated_at = now()`,
			ws, in.PlanKey, in.Status, end, strings.TrimSpace(in.Notes), me); err != nil {
			return err
		}
		detail, _ := json.Marshal(map[string]any{"status": in.Status, "currentPeriodEnd": end, "notes": in.Notes})
		if _, err := tx.Exec(ctx, `INSERT INTO crm.subscription_events (workspace_id, type, plan_key, detail, actor_id) VALUES ($1, 'set_by_owner', $2, $3, $4)`,
			ws, in.PlanKey, detail, me); err != nil {
			return err
		}
		return shared.WriteAudit(ctx, tx, shared.AuditEvent{WorkspaceID: &ws, ActorID: &me, Action: "subscription.set", EntityType: "workspace", EntityID: &ws,
			After: map[string]any{"plan": in.PlanKey, "status": in.Status, "currentPeriodEnd": end}, IP: meta.IP, RequestID: meta.RequestID})
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ent, err := For(ctx, h.store.Pool, ws)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, ent)
}

func (h *Handler) handleSubscriptionEvents(w http.ResponseWriter, r *http.Request) {
	ws, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("workspace_not_found"))
		return
	}
	rows, err := h.store.Pool.Query(r.Context(), `
		SELECT e.type, COALESCE(e.plan_key, ''), e.detail, COALESCE(i.display_name, ''), e.created_at
		FROM crm.subscription_events e LEFT JOIN crm.identities i ON i.id = e.actor_id
		WHERE e.workspace_id = $1 ORDER BY e.created_at DESC LIMIT 100`, ws)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	type event struct {
		Type    string          `json:"type"`
		PlanKey string          `json:"planKey"`
		Detail  json.RawMessage `json:"detail"`
		Actor   string          `json:"actor"`
		At      time.Time       `json:"at"`
	}
	out := []event{}
	for rows.Next() {
		var e event
		var detail []byte
		if err := rows.Scan(&e.Type, &e.PlanKey, &detail, &e.Actor, &e.At); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		e.Detail = detail
		out = append(out, e)
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}
