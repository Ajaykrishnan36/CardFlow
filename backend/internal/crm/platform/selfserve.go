package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Self-serve businesses (D-94). A signed-in person creates their own business: a workspace
// on the platform's standard CRM setup, with themselves as its Super Admin. They never see
// the words product, app or setup; those stay the platform's own configuration.

// StandardSetupKey is the one setup every self-serve business runs on. The owner changes it
// like any other setup; publishing a new version upgrades every business on it (D-45).
const StandardSetupKey = "standard_crm"

// StandardModules are the modules a self-serve business starts with. New modules added
// here reach existing businesses at the next start-up (see EnsureStandardSetup).
var StandardModules = []string{
	"leads", "accounts", "contacts", "opportunities", "tasks", "calendar", "notes", "files",
	"communications", "tickets", "catalog", "subscriptions", "workflows", "reports",
	"finance", "sales_docs", "knowledge", "cards",
}

// SelfServeSettings is the owner's switchboard for self-serve (crm.platform_settings 'self_serve').
type SelfServeSettings struct {
	Enabled       bool `json:"enabled"`
	MaxBusinesses int  `json:"maxBusinesses"` // businesses one person may create
}

func defaultSelfServe() SelfServeSettings { return SelfServeSettings{Enabled: true, MaxBusinesses: 5} }

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Setting reads a platform setting into dst; false when it isn't set.
func Setting(ctx context.Context, q querier, key string, dst any) bool {
	var raw []byte
	if err := q.QueryRow(ctx, `SELECT value FROM crm.platform_settings WHERE key = $1`, key).Scan(&raw); err != nil {
		return false
	}
	return json.Unmarshal(raw, dst) == nil
}

// SelfServe returns the current self-serve settings (defaults when the owner hasn't set any).
func SelfServe(ctx context.Context, q querier) SelfServeSettings {
	s := defaultSelfServe()
	var stored SelfServeSettings
	if Setting(ctx, q, "self_serve", &stored) {
		s = stored
		if s.MaxBusinesses <= 0 {
			s.MaxBusinesses = defaultSelfServe().MaxBusinesses
		}
	}
	return s
}

// EnsureStandardSetup makes sure the standard CRM setup exists and carries every standard
// module, and returns its id. It only ever adds: a missing setup, or missing modules as a
// new published version (businesses on the setup are moved to that version).
func EnsureStandardSetup(ctx context.Context, pool *pgxpool.Pool) (uuid.UUID, error) {
	var id uuid.UUID
	tx, err := pool.Begin(ctx)
	if err != nil {
		return id, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7203118)`); err != nil {
		return id, err
	}
	var version *int
	var raw []byte
	err = tx.QueryRow(ctx, `
		SELECT p.id, p.current_version, COALESCE(v.config, p.draft_config) FROM crm.products p
		LEFT JOIN crm.product_versions v ON v.product_id = p.id AND v.version = p.current_version
		WHERE p.key = $1`, StandardSetupKey).Scan(&id, &version, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		cfg := DefaultProductConfig()
		cfg.Modules = append([]string{}, StandardModules...)
		cfg.LoginMethods.Google, cfg.LoginMethods.Microsoft, cfg.LoginMethods.LinkedIn = true, true, true
		cfg.Integrations.APIAccess, cfg.Integrations.Webhooks = true, true
		cfg.Conversion.CreateContact = true
		body, _ := json.Marshal(cfg)
		if err := tx.QueryRow(ctx, `
			INSERT INTO crm.products (key, name, description, icon, status, current_version, draft_config)
			VALUES ($1, 'Standard CRM', 'The CRM every self-serve business starts with: sales, service, finance and business cards.', 'briefcase', 'active', 1, $2)
			RETURNING id`, StandardSetupKey, body).Scan(&id); err != nil {
			return id, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO crm.product_versions (product_id, version, config) VALUES ($1, 1, $2)`, id, body); err != nil {
			return id, err
		}
		return id, tx.Commit(ctx)
	}
	if err != nil {
		return id, err
	}
	cfg := decodeConfig(raw)
	have := map[string]bool{}
	for _, m := range cfg.Modules {
		have[m] = true
	}
	added := false
	for _, m := range StandardModules {
		if !have[m] {
			cfg.Modules = append(cfg.Modules, m)
			added = true
		}
	}
	if !added && version != nil {
		return id, tx.Commit(ctx)
	}
	next := 1
	if version != nil {
		next = *version + 1
	}
	body, _ := json.Marshal(cfg)
	if _, err := tx.Exec(ctx, `INSERT INTO crm.product_versions (product_id, version, config) VALUES ($1, $2, $3)`, id, next, body); err != nil {
		return id, err
	}
	if _, err := tx.Exec(ctx, `UPDATE crm.products SET current_version = $2, draft_config = $3, status = 'active', updated_at = now() WHERE id = $1`, id, next, body); err != nil {
		return id, err
	}
	if _, err := tx.Exec(ctx, `UPDATE crm.workspace_products SET config_version = $2 WHERE product_id = $1`, id, next); err != nil {
		return id, err
	}
	return id, tx.Commit(ctx)
}

// Business is one business the signed-in person belongs to.
type Business struct {
	ID          uuid.UUID      `json:"id"`
	Code        string         `json:"code"`
	Name        string         `json:"name"`
	RoleKey     string         `json:"roleKey"`
	RoleName    string         `json:"roleName"`
	IsPlatform  bool           `json:"isPlatform"`
	Currency    string         `json:"currency"`
	Timezone    string         `json:"timezone"`
	AccentColor string         `json:"accentColor,omitempty"`
	Origin      string         `json:"origin"`
	Profile     map[string]any `json:"profile"`
	Members     int            `json:"members"`
	CreatedAt   time.Time      `json:"createdAt"`
}

const businessSelect = `
	SELECT w.id, w.code, w.name, COALESCE(r.key, ''), COALESCE(r.name, ''), w.is_platform, w.currency, w.timezone,
	       COALESCE(w.branding->>'accentColor', ''), w.origin, w.profile, w.created_at,
	       (SELECT count(*) FROM crm.memberships mm WHERE mm.workspace_id = w.id AND mm.status = 'active')
	FROM crm.memberships m
	JOIN crm.workspaces w ON w.id = m.workspace_id
	LEFT JOIN LATERAL (
		SELECT ro.key, ro.name FROM crm.role_assignments ra JOIN crm.roles ro ON ro.id = ra.role_id
		WHERE ra.membership_id = m.id AND (ra.expires_at IS NULL OR ra.expires_at > now())
		ORDER BY ro.rank DESC LIMIT 1
	) r ON true
	WHERE m.identity_id = $1 AND m.status = 'active' AND w.status = 'active'`

func scanBusiness(row pgx.Row) (*Business, error) {
	var b Business
	var profile []byte
	if err := row.Scan(&b.ID, &b.Code, &b.Name, &b.RoleKey, &b.RoleName, &b.IsPlatform, &b.Currency, &b.Timezone,
		&b.AccentColor, &b.Origin, &profile, &b.CreatedAt, &b.Members); err != nil {
		return nil, err
	}
	b.Profile = map[string]any{}
	_ = json.Unmarshal(profile, &b.Profile)
	return &b, nil
}

// ListBusinesses returns the businesses a person belongs to, newest first.
func ListBusinesses(ctx context.Context, pool *pgxpool.Pool, identityID uuid.UUID) ([]Business, error) {
	rows, err := pool.Query(ctx, businessSelect+` ORDER BY w.is_platform, w.created_at DESC`, identityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Business{}
	for rows.Next() {
		b, err := scanBusiness(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

// CreateBusinessInput is what a person types to start a business. Only the name is required.
type CreateBusinessInput struct {
	Name     string `json:"name"`
	Industry string `json:"industry"`
	Phone    string `json:"phone"`
	Email    string `json:"email"`
	Website  string `json:"website"`
	City     string `json:"city"`
	State    string `json:"state"`
	Country  string `json:"country"`
	Timezone string `json:"timezone"`
	Currency string `json:"currency"`
	Locale   string `json:"locale"`
	// ListingID links one of the person's own directory listings as the business's public profile.
	ListingID string `json:"listingId"`
}

var slugStrip = regexp.MustCompile(`[^a-z0-9]+`)

// businessCode turns a name into a unique workspace code; the person never types one.
func businessCode(ctx context.Context, tx pgx.Tx, name string) (string, error) {
	base := strings.Trim(slugStrip.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(base) > 30 {
		base = strings.Trim(base[:30], "-")
	}
	if len(base) < 3 || base == "platform" {
		base = "business"
	}
	code := base
	for i := 0; i < 8; i++ {
		var taken bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.workspaces WHERE code = $1)`, code).Scan(&taken); err != nil {
			return "", err
		}
		if !taken {
			return code, nil
		}
		suffix, err := shared.RandomToken(4)
		if err != nil {
			return "", err
		}
		code = base + "-" + strings.ToLower(slugStrip.ReplaceAllString(strings.ToLower(suffix), ""))[:4]
	}
	return "", errors.New("could not find a free business code")
}

// CreateBusinessTx creates the business and makes the creator its Super Admin. It is the
// single path for self-serve creation (the API, and the sample-data seed).
func CreateBusinessTx(ctx context.Context, tx pgx.Tx, creator uuid.UUID, setupID uuid.UUID, in CreateBusinessInput) (uuid.UUID, string, error) {
	in.Name = strings.TrimSpace(in.Name)
	code, err := businessCode(ctx, tx, in.Name)
	if err != nil {
		return uuid.Nil, "", err
	}
	prov := ProvisionInput{Name: in.Name, Code: code, Timezone: in.Timezone, Locale: in.Locale, Currency: in.Currency, ProductIDs: []uuid.UUID{setupID}}
	if f := prov.normalizeAndValidate(""); len(f) > 0 {
		if msg, ok := f["name"]; ok {
			f["name"] = strings.Replace(msg, "the customer's workspace name", "your business name", 1)
		}
		return uuid.Nil, "", shared.Validation(f)
	}
	wsID, err := ProvisionTx(ctx, tx, creator, prov, "")
	if err != nil {
		return uuid.Nil, "", err
	}
	profile := map[string]string{}
	for k, v := range map[string]string{"industry": in.Industry, "phone": in.Phone, "email": in.Email, "website": in.Website,
		"city": in.City, "state": in.State, "country": in.Country} {
		if v = strings.TrimSpace(v); v != "" {
			if len(v) > 200 {
				v = v[:200]
			}
			profile[k] = v
		}
	}
	rawProfile, _ := json.Marshal(profile)
	if _, err := tx.Exec(ctx, `UPDATE crm.workspaces SET created_by_identity = $2, origin = 'self_serve', profile = $3 WHERE id = $1`, wsID, creator, rawProfile); err != nil {
		return uuid.Nil, "", err
	}
	var membershipID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO crm.memberships (workspace_id, identity_id, status, created_by) VALUES ($1, $2, 'active', $2) RETURNING id`, wsID, creator).Scan(&membershipID); err != nil {
		return uuid.Nil, "", err
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO crm.role_assignments (workspace_id, membership_id, role_id, product_ids, granted_by)
		SELECT $1, $2, r.id, ARRAY[$3::uuid], $4 FROM crm.roles r WHERE r.workspace_id = $1 AND r.key = 'SUPER_ADMIN'`,
		wsID, membershipID, setupID, creator)
	if err != nil {
		return uuid.Nil, "", err
	}
	if tag.RowsAffected() != 1 {
		return uuid.Nil, "", errors.New("the new business has no Super Admin role")
	}
	if id, err := uuid.Parse(strings.TrimSpace(in.ListingID)); err == nil {
		// Only the person's own listing, and only one that isn't another business's profile.
		if _, err := tx.Exec(ctx, `
			UPDATE public.businesses b SET workspace_id = $1, updated_at = now()
			FROM public.users u
			WHERE b.id = $2 AND b.owner_user_id = u.id AND u.identity_id = $3 AND b.workspace_id IS NULL AND b.deleted_at IS NULL`,
			wsID, id, creator); err != nil {
			return uuid.Nil, "", err
		}
	}
	return wsID, code, shared.WriteAudit(ctx, tx, shared.AuditEvent{WorkspaceID: &wsID, ActorID: &creator, Action: "business.created",
		EntityType: "workspace", EntityID: &wsID, After: map[string]any{"code": code, "name": in.Name, "selfServe": true}})
}

// SelfServeRoutes mounts the endpoints any signed-in person may call.
func (h *Handler) SelfServeRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(identity.RequireReady)
		r.Get("/businesses", h.handleListBusinesses)
		r.Post("/businesses", h.handleCreateBusiness)
	})
	r.Group(func(r chi.Router) {
		r.Use(identity.RequireOwner)
		r.Get("/platform/settings/self-serve", func(w http.ResponseWriter, r *http.Request) {
			shared.WriteJSON(w, http.StatusOK, SelfServe(r.Context(), h.store.Pool))
		})
		r.Put("/platform/settings/self-serve", h.handleSaveSelfServe)
	})
}

func (h *Handler) handleListBusinesses(w http.ResponseWriter, r *http.Request) {
	me := actorID(r)
	list, err := ListBusinesses(r.Context(), h.store.Pool, me)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	s := SelfServe(r.Context(), h.store.Pool)
	var created int
	_ = h.store.Pool.QueryRow(r.Context(), `SELECT count(*) FROM crm.workspaces WHERE created_by_identity = $1 AND origin = 'self_serve'`, me).Scan(&created)
	shared.WriteJSON(w, http.StatusOK, map[string]any{
		"data":      list,
		"canCreate": s.Enabled && created < s.MaxBusinesses,
		"limit":     s.MaxBusinesses,
		"created":   created,
	})
}

func (h *Handler) handleCreateBusiness(w http.ResponseWriter, r *http.Request) {
	var in CreateBusinessInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	me := actorID(r)
	ctx := r.Context()
	status, resp, err := shared.Idempotent(ctx, h.store.Pool, me, r.Header.Get("Idempotency-Key"), in, func() (int, any, error) {
		settings := SelfServe(ctx, h.store.Pool)
		if !settings.Enabled {
			return 0, nil, shared.Forbidden("self_serve_off", "Creating a business isn't open right now. Ask for an invitation instead.")
		}
		setupID, err := EnsureStandardSetup(ctx, h.store.Pool)
		if err != nil {
			return 0, nil, err
		}
		var wsID uuid.UUID
		err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
			// One creation at a time per person, so the limit can't be raced.
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "business:"+me.String()); err != nil {
				return err
			}
			var created int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM crm.workspaces WHERE created_by_identity = $1 AND origin = 'self_serve'`, me).Scan(&created); err != nil {
				return err
			}
			if created >= settings.MaxBusinesses {
				return shared.Forbidden("business_limit", "You've reached the number of businesses one account can create. Contact support to add more.")
			}
			id, _, err := CreateBusinessTx(ctx, tx, me, setupID, in)
			wsID = id
			return err
		})
		if err != nil {
			return 0, nil, err
		}
		b, err := scanBusiness(h.store.Pool.QueryRow(ctx, businessSelect+` AND w.id = $2`, me, wsID))
		if err != nil {
			return 0, nil, err
		}
		return http.StatusCreated, map[string]any{"business": b, "next": "/crm/w/" + b.Code + "/home"}, nil
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, status, resp)
}

func (h *Handler) handleSaveSelfServe(w http.ResponseWriter, r *http.Request) {
	var in SelfServeSettings
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if in.MaxBusinesses < 1 || in.MaxBusinesses > 100 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"maxBusinesses": "Choose a number from 1 to 100."}))
		return
	}
	raw, _ := json.Marshal(in)
	me := actorID(r)
	before := SelfServe(r.Context(), h.store.Pool)
	err := h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO crm.platform_settings (key, value, updated_by) VALUES ('self_serve', $1, $2)
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = now()`, raw, me); err != nil {
			return err
		}
		return shared.WriteAudit(r.Context(), tx, auditEvent(r, "platform.self_serve.updated", "platform_settings", nil, nil, before, in))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, in)
}
