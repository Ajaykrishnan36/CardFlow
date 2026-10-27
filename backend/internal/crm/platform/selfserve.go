package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
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
	"finance", "sales_docs", "knowledge", "cards", "dealership", "insurance", "salon", "real_estate",
	"care", "memberships", "inventory", "reservations", "checklists",
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
	// The tenant (brand) the business belongs to, and its permanent address (D-156).
	TenantSlug string `json:"tenantSlug"`
	TenantName string `json:"tenantName"`
}

const businessSelect = `
	SELECT w.id, w.code, w.name, COALESCE(r.key, ''), COALESCE(r.name, ''), w.is_platform, w.currency, w.timezone,
	       COALESCE(w.branding->>'accentColor', ''), w.origin, w.profile, w.created_at,
	       (SELECT count(*) FROM crm.memberships mm WHERE mm.workspace_id = w.id AND mm.status = 'active'),
	       COALESCE(t.slug, w.code), COALESCE(t.name, w.name)
	FROM crm.memberships m
	JOIN crm.workspaces w ON w.id = m.workspace_id
	LEFT JOIN crm.tenants t ON t.id = w.tenant_id
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
		&b.AccentColor, &b.Origin, &profile, &b.CreatedAt, &b.Members, &b.TenantSlug, &b.TenantName); err != nil {
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

// CreateBusinessInput is what a person types to start a business. The sign-up form needs the
// name and one tax ID (GSTIN, PAN or TAN); internal callers may leave the tax ID out.
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
	// Tenant is ignored since D-158: every business gets its own address. Old clients still send it.
	Tenant string `json:"tenant"`
	// Mode: "first" is the first-time form at the site's own address, refused to anyone who
	// is already a Super Admin (they add businesses from inside); "add" or empty is Add business.
	Mode string `json:"mode"`
	// D-158: the tax ID the address is made from, and the size of the team.
	TaxIDType string `json:"taxIdType"`
	TaxID     string `json:"taxId"`
	TeamSize  string `json:"teamSize"`
	// Onboarding (D-155): the person, their address, and the business's own details.
	DisplayName   string   `json:"displayName"`
	DateOfBirth   string   `json:"dateOfBirth"`
	PersonalEmail string   `json:"personalEmail"`
	Personal      *Address `json:"personalAddress"`
	Whatsapp      string   `json:"whatsapp"`
	GSTIN         string   `json:"gstin"`
	Address       *Address `json:"address"`
}

// CreateBusinessTx creates the business and makes the creator its Super Admin. It is the
// single path for self-serve creation (the API, and the sample-data seed).
func CreateBusinessTx(ctx context.Context, tx pgx.Tx, creator uuid.UUID, setupID uuid.UUID, in CreateBusinessInput) (uuid.UUID, string, error) {
	in.Name = strings.TrimSpace(in.Name)
	if fe := in.normalizeOnboarding(); len(fe) > 0 {
		return uuid.Nil, "", shared.Validation(fe)
	}
	if in.TaxID != "" {
		// One tax ID belongs to one owner: the same owner may reuse it for another business.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "taxid:"+in.TaxID); err != nil {
			return uuid.Nil, "", err
		}
		var taken bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.workspaces WHERE (profile->>'taxId' = $1 OR profile->>'gstin' = $1 OR profile->>'pan' = $1 OR profile->>'tan' = $1)
			AND created_by_identity IS DISTINCT FROM $2)`, in.TaxID, creator).Scan(&taken); err != nil {
			return uuid.Nil, "", err
		}
		if taken {
			return uuid.Nil, "", shared.Validation(map[string]string{"taxId": "This " + strings.ToUpper(in.TaxIDType) + " is already registered by another business owner. Contact support if it is yours."})
		}
	}
	code, err := GenerateBusinessCode(ctx, tx, time.Now(), in.TaxID)
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
	if err := savePersonDetails(ctx, tx, creator, in.DisplayName, in.DateOfBirth, in.PersonalEmail, in.Personal); err != nil {
		return uuid.Nil, "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE crm.tenants SET owner_identity = $2, name = $3 WHERE id = (SELECT tenant_id FROM crm.workspaces WHERE id = $1)`, wsID, creator, in.Name); err != nil {
		return uuid.Nil, "", err
	}
	rawProfile, _ := json.Marshal(businessProfileFrom(in))
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
		h.onboardingRoutes(r)
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
	if strings.TrimSpace(in.TaxID) == "" && strings.TrimSpace(in.GSTIN) == "" {
		shared.WriteError(w, r, shared.Validation(map[string]string{"taxId": "Enter your GSTIN, PAN or TAN."}))
		return
	}
	if in.Mode == "first" {
		if err := h.refuseExistingOwner(ctx, me); err != nil {
			shared.WriteError(w, r, err)
			return
		}
	}
	status, resp, err := h.createBusiness(ctx, me, r.Header.Get("Idempotency-Key"), in)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, status, resp)
}

// createBusiness is the one path that creates a self-serve business for a person: the
// owner's switch and limit, the business, its listing. Signed in (Add business) and
// "Create your own CRM" (D-160) both use it.
func (h *Handler) createBusiness(ctx context.Context, me uuid.UUID, idemKey string, in CreateBusinessInput) (int, any, error) {
	return shared.Idempotent(ctx, h.store.Pool, me, idemKey, in, func() (int, any, error) {
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
		// The business's public listing (directory, digital card, QR) is made by the app
		// side; a problem there never undoes the business itself.
		if AfterBusinessCreated != nil && strings.TrimSpace(in.ListingID) == "" {
			AfterBusinessCreated(ctx, me, wsID, in)
		}
		b, err := scanBusiness(h.store.Pool.QueryRow(ctx, businessSelect+` AND w.id = $2`, me, wsID))
		if err != nil {
			return 0, nil, err
		}
		return http.StatusCreated, map[string]any{"business": b, "next": "/" + b.TenantSlug + "/home"}, nil
	})
}

// refuseExistingOwner: someone who is already a Super Admin adds businesses from inside;
// the first-time form shows them their own addresses instead (D-158).
func (h *Handler) refuseExistingOwner(ctx context.Context, me uuid.UUID) error {
	rows, err := h.store.Pool.Query(ctx, `
		SELECT w.name, t.slug FROM crm.memberships m
		JOIN crm.workspaces w ON w.id = m.workspace_id AND w.status = 'active'
		JOIN crm.tenants t ON t.id = w.tenant_id AND t.status = 'active'
		JOIN crm.role_assignments ra ON ra.membership_id = m.id JOIN crm.roles r ON r.id = ra.role_id AND r.key = 'SUPER_ADMIN'
		WHERE m.identity_id = $1 AND m.status = 'active' AND (ra.expires_at IS NULL OR ra.expires_at > now())
		ORDER BY w.created_at`, me)
	if err != nil {
		return err
	}
	defer rows.Close()
	list := []map[string]string{}
	for rows.Next() {
		var name, slug string
		if err := rows.Scan(&name, &slug); err != nil {
			return err
		}
		list = append(list, map[string]string{"name": name, "slug": slug, "display": DisplayCode(slug), "loginPath": "/" + slug + "/login"})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(list) == 0 {
		return nil
	}
	e := shared.NewError(http.StatusConflict, "already_registered", "You already have an account. Sign in at your business URL below, or contact support.")
	e.Details = map[string]any{"businesses": list}
	return e
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

// AfterBusinessCreated is set by the server: it gives a new business its public listing
// in the directory (D-105). nil = no directory on this server.
var AfterBusinessCreated func(ctx context.Context, creator, workspaceID uuid.UUID, in CreateBusinessInput)
