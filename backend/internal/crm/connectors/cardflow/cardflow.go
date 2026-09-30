// Package cardflow connects the Business Card Snap app (CardFlow, the app this CRM
// lives next to) to the CRM (PRD §14.3: an external app is metadata + shared modules).
//
//   - Every app user becomes a lead that is converted straight away into an account +
//     contact in the "Business Card Snap" customer workspace (D-36).
//   - Each app sign-in (users.last_login_at moving) is logged as activity.
//   - App support tickets (public.support_tickets) are answered from the workspace's
//     Support page; replies are written back so the app user sees them.
//   - App admins get a Super Admin invitation to the workspace.
//
// The app's own code is not involved: the connector reads its tables every few
// seconds, so the CRM being down never affects the app.
package cardflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/platform"
	"cardflow-backend/internal/crm/records"
	"cardflow-backend/internal/crm/shared"
	"cardflow-backend/internal/crm/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	System        = "cardflow"
	ProductKey    = "business_card_snap"
	WorkspaceCode = "business-card-snap"
	AppName       = "Business Card Snap"
	stateKey      = "cardflow"
	// syncInterval is the background sync. It's long on purpose: every query
	// keeps a serverless database (Neon free tier) awake, so frequent polling
	// used up its compute allowance. Opening the CRM's app pages syncs on
	// demand instead (at most every onDemandGap). CRM_SYNC_INTERVAL overrides.
	defaultSyncInterval = time.Hour
	onDemandGap         = time.Minute
)

var syncInterval = func() time.Duration {
	if d, err := time.ParseDuration(os.Getenv("CRM_SYNC_INTERVAL")); err == nil && d >= 10*time.Second {
		return d
	}
	return defaultSyncInterval
}()

type Connector struct {
	store    *store.Store
	cfg      shared.Config
	platform *platform.Handler

	mu          sync.RWMutex
	wsID        uuid.UUID
	productID   uuid.UUID
	lastSyncAt  time.Time
	lastError   string
	ready       bool
	hasBiz      bool
	hasCards    bool
	hasTickets  bool
	triggerSync chan struct{}
	lastDemand  time.Time
}

func New(st *store.Store, cfg shared.Config, p *platform.Handler) *Connector {
	return &Connector{store: st, cfg: cfg, platform: p, triggerSync: make(chan struct{}, 1)}
}

func (c *Connector) WorkspaceID() uuid.UUID {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.wsID
}

func (c *Connector) setError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil {
		c.lastError = ""
		c.lastSyncAt = time.Now()
		return
	}
	c.lastError = err.Error()
	slog.Warn("crm: cardflow sync failed", "error", err)
}

// Start bootstraps the product/workspace and then syncs every few seconds until ctx ends.
func (c *Connector) Start(ctx context.Context) {
	go func() {
		for {
			if err := c.bootstrap(ctx); err != nil {
				c.setError(fmt.Errorf("setup: %w", err))
				select {
				case <-ctx.Done():
					return
				case <-time.After(30 * time.Second):
					continue
				}
			}
			break
		}
		t := time.NewTicker(syncInterval)
		defer t.Stop()
		for {
			c.setError(c.Sync(ctx))
			records.KickEvents()
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			case <-c.triggerSync:
			}
		}
	}()
}

// ---- bootstrap ----

func tableExists(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, name string) bool {
	var ok bool
	_ = q.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+name).Scan(&ok)
	return ok
}

func (c *Connector) bootstrap(ctx context.Context) error {
	if !tableExists(ctx, c.store.Pool, "users") {
		return errors.New("the app's users table was not found")
	}
	c.hasBiz = tableExists(ctx, c.store.Pool, "businesses")
	c.hasCards = tableExists(ctx, c.store.Pool, "saved_cards")
	c.hasTickets = tableExists(ctx, c.store.Pool, "support_tickets")

	var ownerID uuid.UUID
	if err := c.store.Pool.QueryRow(ctx, `SELECT id FROM crm.identities WHERE is_platform_owner ORDER BY created_at LIMIT 1`).Scan(&ownerID); err != nil {
		return fmt.Errorf("no platform owner yet: %w", err)
	}

	var productID, wsID uuid.UUID
	err := c.store.WithTx(ctx, func(tx pgx.Tx) error {
		// 1. Product, published once (the owner may edit and republish it later).
		err := tx.QueryRow(ctx, `SELECT id FROM crm.products WHERE key = $1`, ProductKey).Scan(&productID)
		if errors.Is(err, pgx.ErrNoRows) {
			cfg := platform.DefaultProductConfig()
			cfg.AccentColor = "#2E1065"
			cfg.Modules = []string{"leads", "accounts", "contacts", "tickets", "reports"}
			cfg.UserTypes = []platform.UserType{
				{Key: "app_user", Label: "App user", Description: "Signed up in the app with phone + OTP.", AllowedRoles: []string{"END_USER"}},
				{Key: "app_admin", Label: "App admin", Description: "Runs the app's admin console.", AllowedRoles: []string{"SUPER_ADMIN", "ADMIN"}},
			}
			raw, _ := json.Marshal(cfg)
			if err := tx.QueryRow(ctx, `
				INSERT INTO crm.products (key, name, description, icon, status, current_version, draft_config, created_by)
				VALUES ($1, $2, 'Scan, save and share business cards — app users, sign-ins and support tickets flow in automatically.', 'store', 'active', 1, $3, $4)
				RETURNING id`, ProductKey, AppName, raw, ownerID).Scan(&productID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO crm.product_versions (product_id, version, config, published_by) VALUES ($1, 1, $2, $3)`,
				productID, raw, ownerID); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}

		// 2. Customer workspace with the product.
		err = tx.QueryRow(ctx, `SELECT id FROM crm.workspaces WHERE code = $1`, WorkspaceCode).Scan(&wsID)
		if errors.Is(err, pgx.ErrNoRows) {
			if wsID, err = platform.ProvisionTx(ctx, tx, ownerID, platform.ProvisionInput{
				Name: AppName, Code: WorkspaceCode, Timezone: "Asia/Kolkata", Locale: "en", Currency: "INR", ProductIDs: []uuid.UUID{productID},
			}, ""); err != nil {
				return err
			}
			// "Give all access": the workspace's Super Admin also manages roles and users.
			sa, _ := access.FindSystemRole("SUPER_ADMIN")
			rules := sa.Rules
			rules.Capabilities = access.AllCapabilities()
			raw, _ := json.Marshal(rules)
			if _, err := tx.Exec(ctx, `UPDATE crm.roles SET base_rules = $2, customized = true, updated_at = now() WHERE workspace_id = $1 AND key = 'SUPER_ADMIN'`,
				wsID, raw); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.workspace_products (workspace_id, product_id, config_version)
			SELECT $1, $2, COALESCE(current_version, 1) FROM crm.products WHERE id = $2
			ON CONFLICT DO NOTHING`, wsID, productID); err != nil {
			return err
		}

		// 3. Custom fields for app data, and an "App profile" section on the account page.
		if err := ensureFields(ctx, tx, wsID, ownerID); err != nil {
			return err
		}
		if err := ensureLayouts(ctx, tx, wsID, ownerID); err != nil {
			return err
		}
		if err := upgradeStandardModules(ctx, tx, productID); err != nil {
			return err
		}
		return upgradeAppAccess(ctx, tx, wsID, productID)
	})
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.wsID, c.productID, c.ready = wsID, productID, true
	c.mu.Unlock()
	if c.hasTickets {
		access.SetSupport(wsID, WorkspaceCode)
	}
	slog.Info("CRM connector ready", "app", AppName, "workspace", WorkspaceCode)
	return nil
}

type fieldDef struct {
	object, key, label, typ string
	choices                 [][2]string
	help                    string
}

var appFields = []fieldDef{
	{"accounts", "app_user_id", "App user ID", "text", nil, "The user's ID in " + AppName + "."},
	{"accounts", "app_role", "App role", "select", [][2]string{{"user", "User"}, {"admin", "Admin"}}, ""},
	{"accounts", "app_status", "App status", "select", [][2]string{{"pending_profile", "Profile pending"}, {"active", "Active"}, {"suspended", "Suspended"}, {"deleted", "Deleted"}}, ""},
	{"accounts", "app_plan", "Plan", "select", [][2]string{{"free", "Free"}, {"plus", "Plus"}, {"premium", "Premium"}}, ""},
	{"accounts", "app_subscribed", "Subscribed", "boolean", nil, ""},
	{"accounts", "app_subscription_expires", "Subscription expires", "datetime", nil, ""},
	{"accounts", "app_signed_up_at", "Signed up", "datetime", nil, ""},
	{"accounts", "app_last_login", "Last app sign-in", "datetime", nil, "Updated every time they sign in with phone + OTP."},
	{"accounts", "app_free_scans_left", "Free scans left", "number", nil, ""},
	{"accounts", "app_saved_cards", "Saved cards", "number", nil, ""},
	{"accounts", "app_businesses", "Businesses owned", "number", nil, ""},
	{"accounts", "app_business_id", "App business ID", "text", nil, "The business's ID in " + AppName + "."},
	{"accounts", "app_gstin", "GSTIN", "text", nil, ""},
	{"contacts", "app_user_id", "App user ID", "text", nil, "The user's ID in " + AppName + "."},
	{"contacts", "app_role", "App role", "select", [][2]string{{"user", "User"}, {"admin", "Admin"}}, ""},
	{"contacts", "app_status", "App status", "select", [][2]string{{"pending_profile", "Profile pending"}, {"active", "Active"}, {"suspended", "Suspended"}, {"deleted", "Deleted"}}, ""},
	{"contacts", "app_plan", "Plan", "select", [][2]string{{"free", "Free"}, {"plus", "Plus"}, {"premium", "Premium"}}, ""},
	{"contacts", "app_subscribed", "Subscribed", "boolean", nil, ""},
	{"contacts", "app_subscription_expires", "Subscription expires", "datetime", nil, ""},
	{"contacts", "app_signed_up_at", "Signed up", "datetime", nil, ""},
	{"contacts", "app_last_login", "Last app sign-in", "datetime", nil, "Updated every time they sign in with phone + OTP."},
	{"contacts", "app_free_scans_left", "Free scans left", "number", nil, ""},
	{"contacts", "app_saved_cards", "Saved cards", "number", nil, ""},
	{"contacts", "app_businesses", "Businesses owned", "number", nil, ""},
	{"leads", "app_user_id", "App user ID", "text", nil, "The user's ID in " + AppName + "."},
}

func ensureFields(ctx context.Context, tx pgx.Tx, wsID, ownerID uuid.UUID) error {
	for i, f := range append(append([]fieldDef{}, appFields...), caseFields...) {
		choices := []map[string]string{}
		for _, c := range f.choices {
			choices = append(choices, map[string]string{"value": c[0], "label": c[1]})
		}
		opts, _ := json.Marshal(map[string]any{"choices": choices})
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.field_definitions (workspace_id, object_key, key, label, type, options, help_text, position, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, $9)
			ON CONFLICT (workspace_id, object_key, key) DO NOTHING`,
			wsID, f.object, f.key, f.label, f.typ, opts, f.help, 100+i, ownerID); err != nil {
			return err
		}
	}
	return nil
}

func ensureLayouts(ctx context.Context, tx pgx.Tx, wsID, ownerID uuid.UUID) error {
	sections := map[string]records.Section{
		"accounts": {ID: "app_profile", Title: AppName + " profile", Columns: 2, Fields: []string{
			"app_user_id", "app_status", "app_role", "app_plan", "app_subscribed", "app_subscription_expires",
			"app_signed_up_at", "app_last_login", "app_free_scans_left", "app_saved_cards", "app_businesses"}},
		"contacts": {ID: "app_profile", Title: AppName + " profile", Columns: 2, Fields: contactProfileFields},
		"leads":    {ID: "app_profile", Title: AppName, Columns: 2, Fields: []string{"app_user_id"}},
		"cases":    {ID: "app_profile", Title: AppName, Columns: 1, Fields: []string{"app_reply", "app_category", caseTicketField}},
	}
	for object, sec := range sections {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.layouts WHERE workspace_id = $1 AND object_key = $2)`, wsID, object).Scan(&exists); err != nil {
			return err
		}
		if exists {
			// The workspace has its own layout; only widen the connector's own section (D-52).
			if err := upgradeProfileSection(ctx, tx, wsID, object, sec); err != nil {
				return err
			}
			continue
		}
		l := records.DefaultLayout(object)
		out := []records.Section{}
		for i, s := range l.Sections {
			out = append(out, s)
			if i == 0 {
				out = append(out, sec)
			}
		}
		l.Sections = out
		if object == "accounts" {
			l.Highlights = []string{"lifecycle", "phone", "email", "app_plan", "app_last_login", "app_saved_cards"}
		}
		raw, _ := json.Marshal(l)
		if _, err := tx.Exec(ctx, `INSERT INTO crm.layouts (workspace_id, object_key, definition, updated_by) VALUES ($1, $2, $3, $4)`,
			wsID, object, raw, ownerID); err != nil {
			return err
		}
	}
	return nil
}

var contactProfileFields = []string{
	"app_user_id", "app_status", "app_role", "app_plan", "app_subscribed", "app_subscription_expires",
	"app_signed_up_at", "app_last_login", "app_free_scans_left", "app_saved_cards", "app_businesses"}

// upgradeProfileSection adds fields the connector now shows to its own
// "app_profile" section of an existing layout (other sections untouched).
func upgradeProfileSection(ctx context.Context, tx pgx.Tx, wsID uuid.UUID, object string, sec records.Section) error {
	if object != "contacts" {
		return nil
	}
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT definition FROM crm.layouts WHERE workspace_id = $1 AND object_key = $2`, wsID, object).Scan(&raw); err != nil {
		return err
	}
	var l records.Layout
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil
	}
	changed := false
	for i, s := range l.Sections {
		if s.ID != sec.ID {
			continue
		}
		have := map[string]bool{}
		for _, f := range s.Fields {
			have[f] = true
		}
		for _, f := range sec.Fields {
			if !have[f] {
				l.Sections[i].Fields = append(l.Sections[i].Fields, f)
				changed = true
			}
		}
		if l.Sections[i].Title != sec.Title {
			l.Sections[i].Title = sec.Title
			changed = true
		}
	}
	if !changed {
		return nil
	}
	out, _ := json.Marshal(l)
	_, err := tx.Exec(ctx, `UPDATE crm.layouts SET definition = $3 WHERE workspace_id = $1 AND object_key = $2`, wsID, object, out)
	return err
}

// ---- sync ----

type appUser struct {
	ID                    string     `json:"id"`
	Phone                 string     `json:"phone"`
	Name                  *string    `json:"name"`
	Email                 *string    `json:"email"`
	City                  *string    `json:"city"`
	State                 *string    `json:"state"`
	Country               *string    `json:"country"`
	Role                  string     `json:"role"`
	Plan                  *string    `json:"plan"`
	Status                *string    `json:"status"`
	IsSubscribed          *bool      `json:"is_subscribed"`
	SubscriptionExpiresAt *time.Time `json:"subscription_expires_at"`
	FreeScansRemaining    *int       `json:"free_scans_remaining"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             *time.Time `json:"updated_at"`
	LastLoginAt           *time.Time `json:"last_login_at"`
	DeletedAt             *time.Time `json:"deleted_at"`
	SavedCards            int        `json:"_saved_cards"`
	Businesses            int        `json:"_businesses"`
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

func (u appUser) displayName() string {
	if n := str(u.Name); n != "" && n != "CardFlow User" {
		return n
	}
	return "App user " + u.Phone
}

func (u appUser) nameParts() (first, last string) {
	n := u.displayName()
	if i := strings.LastIndex(n, " "); i > 0 && !strings.HasPrefix(n, "App user ") {
		return n[:i], n[i+1:]
	}
	return "", n
}

func (u appUser) lifecycle() string {
	switch {
	case u.DeletedAt != nil:
		return "churned"
	case str(u.Status) == "suspended" || str(u.Status) == "deleted":
		return "churned"
	case str(u.Status) == "pending_profile":
		return "onboarding"
	}
	return "active"
}

func (u appUser) custom(savedCards, businesses bool) map[string]any {
	c := map[string]any{"app_user_id": u.ID, "app_role": u.Role, "app_signed_up_at": u.CreatedAt.UTC().Format(time.RFC3339)}
	if s := str(u.Status); s != "" {
		c["app_status"] = s
	}
	if u.DeletedAt != nil {
		c["app_status"] = "deleted"
	}
	if p := str(u.Plan); p != "" {
		c["app_plan"] = p
	}
	if u.IsSubscribed != nil {
		c["app_subscribed"] = *u.IsSubscribed
	}
	if u.SubscriptionExpiresAt != nil {
		c["app_subscription_expires"] = u.SubscriptionExpiresAt.UTC().Format(time.RFC3339)
	}
	if u.LastLoginAt != nil {
		c["app_last_login"] = u.LastLoginAt.UTC().Format(time.RFC3339)
	}
	if u.FreeScansRemaining != nil {
		c["app_free_scans_left"] = *u.FreeScansRemaining
	}
	if savedCards {
		c["app_saved_cards"] = u.SavedCards
	}
	if businesses {
		c["app_businesses"] = u.Businesses
	}
	return c
}

type syncState struct {
	UsersSince   time.Time `json:"usersSince"`
	TicketsSince time.Time `json:"ticketsSince"`
	// BusinessesSince covers every app business (D-52). A new key, so the
	// first run after the upgrade also gives owner-registered businesses accounts.
	BusinessesSince time.Time `json:"businessesSince"`
	// CasesSince: tickets mirrored into Cases up to this update time (D-72).
	CasesSince time.Time `json:"casesSince"`
}

func (c *Connector) loadState(ctx context.Context) syncState {
	var st syncState
	var raw []byte
	if err := c.store.Pool.QueryRow(ctx, `SELECT value FROM crm.connector_state WHERE key = $1`, stateKey).Scan(&raw); err == nil {
		_ = json.Unmarshal(raw, &st)
	}
	return st
}

func (c *Connector) saveState(ctx context.Context, st syncState) error {
	raw, _ := json.Marshal(st)
	_, err := c.store.Pool.Exec(ctx, `
		INSERT INTO crm.connector_state (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, stateKey, raw)
	return err
}

// SyncOnDemand asks for a sync when someone opens a CRM page showing app
// data, at most once per onDemandGap, so the CRM is fresh while in use and the
// database can sleep while it isn't.
func (c *Connector) SyncOnDemand() {
	c.mu.Lock()
	due := time.Since(c.lastDemand) >= onDemandGap
	if due {
		c.lastDemand = time.Now()
	}
	c.mu.Unlock()
	if due {
		c.TriggerSync()
	}
}

// TriggerSync asks the loop to sync now (non-blocking).
func (c *Connector) TriggerSync() {
	select {
	case c.triggerSync <- struct{}{}:
	default:
	}
}

// Sync imports users changed since the last run (all of them the first time) and new tickets.
func (c *Connector) Sync(ctx context.Context) error {
	c.mu.RLock()
	ready, wsID := c.ready, c.wsID
	c.mu.RUnlock()
	if !ready {
		return errors.New("not set up yet")
	}
	st := c.loadState(ctx)

	extra := `0 AS _saved_cards, 0 AS _businesses`
	if c.hasCards && c.hasBiz {
		extra = `(SELECT count(*) FROM saved_cards sc WHERE sc.user_id = u.id) AS _saved_cards,
		         (SELECT count(*) FROM businesses b WHERE b.owner_user_id = u.id) AS _businesses`
	} else if c.hasCards {
		extra = `(SELECT count(*) FROM saved_cards sc WHERE sc.user_id = u.id) AS _saved_cards, 0 AS _businesses`
	}
	// A 2-second overlap covers rows committed slightly out of order; upserts are idempotent.
	since := st.UsersSince.Add(-2 * time.Second)
	for {
		rows, err := c.store.Pool.Query(ctx, `
			SELECT to_jsonb(x), x._changed FROM (
				SELECT u.*, `+extra+`,
				       GREATEST(u.created_at, COALESCE(u.updated_at, u.created_at), COALESCE(u.last_login_at, u.created_at),
				                COALESCE(u.deleted_at, u.created_at)) AS _changed
				FROM public.users u
			) x
			WHERE x._changed > $1
			ORDER BY x._changed LIMIT 200`, since)
		if err != nil {
			return err
		}
		type item struct {
			u       appUser
			changed time.Time
		}
		var batch []item
		for rows.Next() {
			var raw []byte
			var changed time.Time
			if err := rows.Scan(&raw, &changed); err != nil {
				rows.Close()
				return err
			}
			var u appUser
			if err := json.Unmarshal(raw, &u); err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, item{u, changed})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, it := range batch {
			if err := c.upsertUser(ctx, wsID, it.u); err != nil {
				return fmt.Errorf("user %s: %w", it.u.ID, err)
			}
			if it.changed.After(st.UsersSince) {
				st.UsersSince = it.changed
			}
		}
		if err := c.saveState(ctx, st); err != nil {
			return err
		}
		if len(batch) < 200 {
			break
		}
		since = st.UsersSince
	}
	// Businesses → leads / accounts (after users, so an owner's contact exists).
	if err := c.syncBusinesses(ctx, wsID, &st); err != nil {
		return err
	}
	if err := c.saveState(ctx, st); err != nil {
		return err
	}
	if c.hasTickets {
		if err := c.syncTicketActivity(ctx, wsID, &st); err != nil {
			return err
		}
		if err := c.syncTicketCases(ctx, wsID, &st); err != nil {
			return err
		}
		if err := c.saveState(ctx, st); err != nil {
			return err
		}
	}
	return nil
}

type link struct {
	leadID, contactID uuid.UUID
	// accountID is nil for a person without a business (D-52: contact first).
	accountID  *uuid.UUID
	identityID *uuid.UUID
	lastLogin  *time.Time
}

func (c *Connector) upsertUser(ctx context.Context, wsID uuid.UUID, u appUser) error {
	var sent *platform.SentInvitation
	err := c.store.WithTx(ctx, func(tx pgx.Tx) error {
		var owner uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM crm.identities WHERE is_platform_owner ORDER BY created_at LIMIT 1`).Scan(&owner); err != nil {
			return err
		}
		var l link
		err := tx.QueryRow(ctx, `
			SELECT lead_id, account_id, contact_id, identity_id, last_login_at FROM crm.external_links
			WHERE system = $1 AND external_type = 'user' AND external_id = $2 FOR UPDATE`, System, u.ID).
			Scan(&l.leadID, &l.accountID, &l.contactID, &l.identityID, &l.lastLogin)
		first, last := u.nameParts()
		customAcc, _ := json.Marshal(u.custom(c.hasCards, c.hasBiz))
		customRef, _ := json.Marshal(map[string]any{"app_user_id": u.ID})
		email := strings.ToLower(str(u.Email))
		country := str(u.Country)
		if country == "IN" {
			country = "India"
		}

		if errors.Is(err, pgx.ErrNoRows) {
			// New app user → lead, converted into a contact (D-52). An account is
			// created only when they have a business (see syncBusinesses).
			codeL, err := records.NextCode(ctx, tx, wsID, "L")
			if err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `
				INSERT INTO crm.leads (workspace_id, code, first_name, last_name, email, mobile, phone, city, state, country, source, status,
				                       description, user_type, owner_id, created_by, updated_by, custom, converted_at, created_at)
				VALUES ($1, $2, NULLIF($3, ''), $4, NULLIF($5, ''), $6, $6, NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''), 'app_signup', 'converted',
				        $10, 'app_user', $11, $11, $11, $12, now(), $13)
				RETURNING id`, wsID, codeL, first, last, email, u.Phone, str(u.City), str(u.State), country,
				"Signed up in "+AppName+" with phone + OTP.", owner, customRef, u.CreatedAt).Scan(&l.leadID); err != nil {
				return err
			}
			codeC, err := records.NextCode(ctx, tx, wsID, "C")
			if err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `
				INSERT INTO crm.contacts (workspace_id, code, first_name, last_name, email, phone, mobile, lead_source,
				                          mailing_city, mailing_state, mailing_country, owner_id, created_by, updated_by, custom, created_at)
				VALUES ($1, $2, NULLIF($3, ''), $4, NULLIF($5, ''), $6, $6, 'app_signup', NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''),
				        $10, $10, $10, $11, $12)
				RETURNING id`, wsID, codeC, first, last, email, u.Phone, str(u.City), str(u.State), country,
				owner, customAcc, u.CreatedAt).Scan(&l.contactID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE crm.leads SET converted_contact_id = $2 WHERE id = $1`, l.leadID, l.contactID); err != nil {
				return err
			}
			if err := records.EmitRecordEvent(ctx, tx, wsID, "leads", l.leadID, "record.created", "app"); err != nil {
				return err
			}
			if err := records.EmitRecordEvent(ctx, tx, wsID, "contacts", l.contactID, "record.created", "app"); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO crm.lead_conversions (workspace_id, lead_id, contact_id, trigger, converted_by)
				VALUES ($1, $2, $3, 'app_signup', $4)`, wsID, l.leadID, l.contactID, owner); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO crm.external_links (workspace_id, system, external_type, external_id, lead_id, contact_id)
				VALUES ($1, $2, 'user', $3, $4, $5)`, wsID, System, u.ID, l.leadID, l.contactID); err != nil {
				return err
			}
			if err := addActivity(ctx, tx, wsID, l, "app.signed_up", "Signed up in "+AppName, u.CreatedAt,
				"cardflow:signup:"+u.ID, map[string]any{"phone": u.Phone}); err != nil {
				return err
			}
			if err := shared.WriteAudit(ctx, tx, shared.AuditEvent{WorkspaceID: &wsID, ActorKind: "system", Action: "connector.user_imported",
				EntityType: "contact", EntityID: &l.contactID, After: map[string]any{"app": AppName, "appUserId": u.ID}}); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			// Known user: the app is the source of truth for its profile fields.
			if _, err := tx.Exec(ctx, `
				UPDATE crm.accounts SET name = $2, email = COALESCE(NULLIF($3, ''), email), phone = $4,
				       billing_city = COALESCE(NULLIF($5, ''), billing_city), billing_state = COALESCE(NULLIF($6, ''), billing_state),
				       lifecycle = $7, custom = custom || $8::jsonb, updated_at = now()
				WHERE id = $1 AND kind = 'individual' AND (name, COALESCE(email, ''), phone, lifecycle, custom) IS DISTINCT FROM
				      ($2, COALESCE(NULLIF($3, ''), email, ''), $4, $7, custom || $8::jsonb)`,
				l.accountID, u.displayName(), email, u.Phone, str(u.City), str(u.State), u.lifecycle(), customAcc); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE crm.contacts SET first_name = NULLIF($2, ''), last_name = $3, email = COALESCE(NULLIF($4, ''), email), phone = $5, mobile = $5,
				       mailing_city = COALESCE(NULLIF($6, ''), mailing_city), mailing_state = COALESCE(NULLIF($7, ''), mailing_state),
				       custom = custom || $8::jsonb, updated_at = now()
				WHERE id = $1 AND (COALESCE(first_name, ''), last_name, COALESCE(email, ''), COALESCE(phone, ''), custom) IS DISTINCT FROM
				      ($2, $3, COALESCE(NULLIF($4, ''), email, ''), $5, custom || $8::jsonb)`,
				l.contactID, first, last, email, u.Phone, str(u.City), str(u.State), customAcc); err != nil {
				return err
			}
		}

		// Sign-in activity: one entry per observed last_login_at (dedupe by timestamp).
		if u.LastLoginAt != nil && (l.lastLogin == nil || u.LastLoginAt.After(*l.lastLogin)) &&
			u.LastLoginAt.Sub(u.CreatedAt) > 5*time.Second {
			if err := addActivity(ctx, tx, wsID, l, "app.signed_in", "Signed in to "+AppName, *u.LastLoginAt,
				fmt.Sprintf("cardflow:login:%s:%d", u.ID, u.LastLoginAt.Unix()), map[string]any{"method": "phone + OTP"}); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE crm.external_links SET last_login_at = GREATEST(last_login_at, $4), synced_at = now()
			WHERE system = $1 AND external_type = 'user' AND external_id = $2 AND workspace_id = $3`, System, u.ID, wsID, u.LastLoginAt); err != nil {
			return err
		}

		// App admins get a Super Admin invitation (once).
		if u.Role == "admin" && l.identityID == nil && email != "" && u.DeletedAt == nil {
			var err error
			sent, err = c.platform.InviteTx(ctx, tx, owner, wsID, platform.InviteInput{Name: u.displayName(), Email: email, RoleKey: "SUPER_ADMIN"}, "")
			if err != nil {
				var e *shared.Error
				if errors.As(err, &e) && e.Status == 422 {
					sent = nil // already a member etc. — nothing to do
				} else {
					return err
				}
			}
			var identityID uuid.UUID
			if err := tx.QueryRow(ctx, `
				SELECT identity_id FROM crm.verified_identifiers WHERE kind = 'email' AND namespace = 'global' AND value_normalized = $1`, email).
				Scan(&identityID); err == nil {
				if phone, ok := normalizePhone(u.Phone); ok {
					_, _ = tx.Exec(ctx, `INSERT INTO crm.verified_identifiers (identity_id, kind, value_normalized, verified_at)
						VALUES ($1, 'phone', $2, now()) ON CONFLICT DO NOTHING`, identityID, phone)
				}
				if _, err := tx.Exec(ctx, `UPDATE crm.external_links SET identity_id = $4 WHERE system = $1 AND external_type = 'user' AND external_id = $2 AND workspace_id = $3`,
					System, u.ID, wsID, identityID); err != nil {
					return err
				}
				if l.accountID != nil {
					if _, err := tx.Exec(ctx, `UPDATE crm.accounts SET identity_id = $2 WHERE id = $1`, *l.accountID, identityID); err != nil {
						return err
					}
				}
				if _, err := tx.Exec(ctx, `UPDATE crm.contacts SET identity_id = $2 WHERE id = $1`, l.contactID, identityID); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err == nil && sent != nil {
		c.platform.DeliverInvitation(ctx, sent)
		slog.Info("CRM connector invited an app admin as Super Admin", "app", AppName, "email", sent.Email)
	}
	return err
}

func addActivity(ctx context.Context, tx pgx.Tx, wsID uuid.UUID, l link, kind, title string, at time.Time, dedupe string, detail map[string]any) error {
	raw, _ := json.Marshal(detail)
	_, err := tx.Exec(ctx, `
		INSERT INTO crm.activities (workspace_id, account_id, contact_id, lead_id, kind, title, detail, source, dedupe_key, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (dedupe_key) DO NOTHING`, wsID, l.accountID, l.contactID, l.leadID, kind, title, raw, System, dedupe, at)
	return err
}

func normalizePhone(raw string) (string, bool) {
	d := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, raw)
	switch {
	case len(d) == 10:
		return "+91" + d, true
	case len(d) >= 11 && len(d) <= 15:
		return "+" + d, true
	}
	return "", false
}

// syncTicketActivity logs newly opened app tickets on the person's account timeline.
func (c *Connector) syncTicketActivity(ctx context.Context, wsID uuid.UUID, st *syncState) error {
	rows, err := c.store.Pool.Query(ctx, `
		SELECT t.id, t.subject, t.created_at, l.lead_id, l.account_id, l.contact_id
		FROM public.support_tickets t
		JOIN crm.external_links l ON l.system = $1 AND l.external_type = 'user' AND l.external_id = t.user_id::text
		WHERE t.created_at > $2 ORDER BY t.created_at LIMIT 200`, System, st.TicketsSince.Add(-2*time.Second))
	if err != nil {
		return err
	}
	type tk struct {
		id, subject string
		at          time.Time
		l           link
	}
	var list []tk
	for rows.Next() {
		var t tk
		if err := rows.Scan(&t.id, &t.subject, &t.at, &t.l.leadID, &t.l.accountID, &t.l.contactID); err != nil {
			rows.Close()
			return err
		}
		list = append(list, t)
	}
	rows.Close()
	for _, t := range list {
		err := c.store.WithTx(ctx, func(tx pgx.Tx) error {
			return addActivity(ctx, tx, wsID, t.l, "ticket.opened", "Opened support ticket: "+t.subject, t.at, "cardflow:ticket:"+t.id,
				map[string]any{"ticketId": t.id})
		})
		if err != nil {
			return err
		}
		if t.at.After(st.TicketsSince) {
			st.TicketsSince = t.at
		}
	}
	return rows.Err()
}
