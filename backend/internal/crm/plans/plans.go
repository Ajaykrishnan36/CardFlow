// Package plans decides what a business may use (D-101). A plan belongs to a business
// (a workspace), never to a person: every member of a business gets the same limits.
// Limits are checked on the server, where records, members and card scans are created.
package plans

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Limits of a plan. 0 = unlimited.
type Limits struct {
	Members           int `json:"members"`
	Records           int `json:"records"`
	CardScansPerMonth int `json:"cardScansPerMonth"`
}

type Plan struct {
	Key          string   `json:"key"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	PriceMonthly float64  `json:"priceMonthly"`
	PriceYearly  float64  `json:"priceYearly"`
	Currency     string   `json:"currency"`
	Limits       Limits   `json:"limits"`
	Features     []string `json:"features"`
	IsDefault    bool     `json:"isDefault"`
	IsActive     bool     `json:"isActive"`
	Position     int      `json:"position"`
}

// Entitlement is the plan a business is on right now and why.
type Entitlement struct {
	Plan Plan `json:"plan"`
	// Status: active | trialing | past_due | cancelled | expired.
	Status string `json:"status"`
	// Source: manual (set by the platform owner) | revenuecat | grandfathered | system
	// (a business the owner provisioned) | app_premium (the creator's app subscription) |
	// default (no subscription).
	Source           string     `json:"source"`
	CurrentPeriodEnd *time.Time `json:"currentPeriodEnd,omitempty"`
}

type Usage struct {
	Members   int `json:"members"`
	Records   int `json:"records"`
	CardScans int `json:"cardScansThisMonth"`
}

// Querier is a pool or a transaction.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

const planColumns = `key, name, description, price_monthly::float8, price_yearly::float8, trim(currency), limits, features, is_default, is_active, position`

func scanPlan(row pgx.Row) (*Plan, error) {
	var p Plan
	var limits, features []byte
	if err := row.Scan(&p.Key, &p.Name, &p.Description, &p.PriceMonthly, &p.PriceYearly, &p.Currency, &limits, &features, &p.IsDefault, &p.IsActive, &p.Position); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(limits, &p.Limits)
	p.Features = []string{}
	_ = json.Unmarshal(features, &p.Features)
	return &p, nil
}

// List returns every plan, in display order.
func List(ctx context.Context, q Querier, onlyActive bool) ([]Plan, error) {
	rows, err := q.Query(ctx, `SELECT `+planColumns+` FROM crm.plans WHERE ($1 = false OR is_active) ORDER BY position, key`, onlyActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Plan{}
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func get(ctx context.Context, q Querier, key string) (*Plan, error) {
	p, err := scanPlan(q.QueryRow(ctx, `SELECT `+planColumns+` FROM crm.plans WHERE key = $1`, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NotFound("plan_not_found")
	}
	return p, err
}

// unlimited is what businesses provisioned by the platform owner get until a plan is set.
var unlimited = Plan{Key: "managed", Name: "Managed", Description: "Set up by the platform owner.", Currency: "INR", Features: []string{}, IsActive: true}

// For resolves the plan of a business:
//  1. its own subscription, while it is active (or in trial) and not past its end;
//  2. a business the platform owner provisioned (or the platform itself): no limits;
//  3. the creator has the app's premium subscription: Pro for the businesses they created;
//  4. the default plan.
func For(ctx context.Context, q Querier, ws uuid.UUID) (*Entitlement, error) {
	var origin string
	var isPlatform bool
	var creator *uuid.UUID
	if err := q.QueryRow(ctx, `SELECT origin, is_platform, created_by_identity FROM crm.workspaces WHERE id = $1`, ws).Scan(&origin, &isPlatform, &creator); err != nil {
		return nil, err
	}
	var planKey, status, source string
	var end *time.Time
	err := q.QueryRow(ctx, `SELECT plan_key, status, source, current_period_end FROM crm.workspace_subscriptions WHERE workspace_id = $1`, ws).
		Scan(&planKey, &status, &source, &end)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		live := (status == "active" || status == "trialing") && (end == nil || end.After(time.Now()))
		if live {
			p, err := get(ctx, q, planKey)
			if err != nil {
				return nil, err
			}
			return &Entitlement{Plan: *p, Status: status, Source: source, CurrentPeriodEnd: end}, nil
		}
		if status == "active" || status == "trialing" {
			status = "expired"
		}
	}
	if isPlatform || origin != "self_serve" {
		return &Entitlement{Plan: unlimited, Status: "active", Source: "system"}, nil
	}
	if creator != nil && appPremium(ctx, q, *creator) {
		if p, err := get(ctx, q, "pro"); err == nil {
			return &Entitlement{Plan: *p, Status: "active", Source: "app_premium"}, nil
		}
	}
	p, err := scanPlan(q.QueryRow(ctx, `SELECT `+planColumns+` FROM crm.plans WHERE is_default LIMIT 1`))
	if errors.Is(err, pgx.ErrNoRows) {
		return &Entitlement{Plan: unlimited, Status: "active", Source: "default"}, nil
	}
	if err != nil {
		return nil, err
	}
	ent := &Entitlement{Plan: *p, Status: "active", Source: "default"}
	if status != "" {
		ent.Status = status // the paid plan lapsed; the business is back on the default plan
	}
	return ent, nil
}

// appPremium: the person holds the mobile app's premium subscription (bought in the app
// stores, kept on their app profile).
func appPremium(ctx context.Context, q Querier, identityID uuid.UUID) bool {
	var has bool
	if err := q.QueryRow(ctx, `SELECT to_regclass('public.users') IS NOT NULL`).Scan(&has); err != nil || !has {
		return false
	}
	var premium bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM public.users u WHERE u.identity_id = $1 AND u.is_subscribed
		               AND (u.subscription_expires_at IS NULL OR u.subscription_expires_at > now()))`, identityID).Scan(&premium)
	return err == nil && premium
}

func month() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// UsageOf counts what a business uses against its limits.
func UsageOf(ctx context.Context, q Querier, ws uuid.UUID) (Usage, error) {
	var u Usage
	err := q.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM crm.memberships WHERE workspace_id = $1 AND status IN ('active', 'invited')),
		       (SELECT count(*) FROM crm.leads WHERE workspace_id = $1 AND deleted_at IS NULL)
		     + (SELECT count(*) FROM crm.contacts WHERE workspace_id = $1 AND deleted_at IS NULL)
		     + (SELECT count(*) FROM crm.accounts WHERE workspace_id = $1 AND deleted_at IS NULL)
		     + (SELECT count(*) FROM crm.object_records WHERE workspace_id = $1 AND deleted_at IS NULL),
		       COALESCE((SELECT value FROM crm.usage_counters WHERE workspace_id = $1 AND metric = 'card_scans' AND period = $2), 0)`,
		ws, month()).Scan(&u.Members, &u.Records, &u.CardScans)
	return u, err
}

func limitError(what, plan string, limit int) error {
	return &shared.Error{Status: http.StatusPaymentRequired, Code: "plan_limit",
		Message: fmt.Sprintf("Your %s plan allows %d %s. Upgrade the plan to add more.", plan, limit, what),
		Details: map[string]any{"limit": limit, "plan": plan, "what": what}}
}

// CheckRecords fails with 402 plan_limit when the business has reached its record limit.
func CheckRecords(ctx context.Context, q Querier, ws uuid.UUID) error {
	ent, err := For(ctx, q, ws)
	if err != nil || ent.Plan.Limits.Records <= 0 {
		return err
	}
	u, err := UsageOf(ctx, q, ws)
	if err != nil {
		return err
	}
	if u.Records >= ent.Plan.Limits.Records {
		return limitError("records", ent.Plan.Name, ent.Plan.Limits.Records)
	}
	return nil
}

// CheckSeats fails with 402 plan_limit when the business can't take another member.
func CheckSeats(ctx context.Context, q Querier, ws uuid.UUID) error {
	ent, err := For(ctx, q, ws)
	if err != nil || ent.Plan.Limits.Members <= 0 {
		return err
	}
	u, err := UsageOf(ctx, q, ws)
	if err != nil {
		return err
	}
	if u.Members >= ent.Plan.Limits.Members {
		return limitError("team members", ent.Plan.Name, ent.Plan.Limits.Members)
	}
	return nil
}

// CountCardScan checks the monthly scan limit and counts one more scan.
func CountCardScan(ctx context.Context, q Querier, ws uuid.UUID) error {
	ent, err := For(ctx, q, ws)
	if err != nil {
		return err
	}
	if limit := ent.Plan.Limits.CardScansPerMonth; limit > 0 {
		var used int
		_ = q.QueryRow(ctx, `SELECT COALESCE((SELECT value FROM crm.usage_counters WHERE workspace_id = $1 AND metric = 'card_scans' AND period = $2), 0)`, ws, month()).Scan(&used)
		if used >= limit {
			return limitError("card scans a month", ent.Plan.Name, limit)
		}
	}
	_, err = q.Exec(ctx, `
		INSERT INTO crm.usage_counters (workspace_id, metric, period, value) VALUES ($1, 'card_scans', $2, 1)
		ON CONFLICT (workspace_id, metric, period) DO UPDATE SET value = crm.usage_counters.value + 1`, ws, month())
	return err
}
