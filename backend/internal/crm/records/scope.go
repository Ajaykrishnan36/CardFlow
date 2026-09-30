package records

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Scope is who is working on which workspace's records. The owner (Platform CRM) may
// do everything; a member is limited by role + permission sets + products (D-30).
type Scope struct {
	WS           uuid.UUID
	Code         string
	Name         string
	IsPlatformWS bool
	Owner        bool
	Eff          *access.Effective
	MembershipID uuid.UUID
	// OwnerConsole: the owner's /platform routes, where lists span every workspace.
	OwnerConsole bool
	// Below: identities in roles under this member's role (record sharing, D-48).
	Below []uuid.UUID
}

// OwnersFor is whose records an "own" scope covers: the member and everyone in roles
// below theirs. nil when the member sees every record of the object.
func (s *Scope) OwnersFor(object string, me uuid.UUID) []uuid.UUID {
	if !s.OwnOnly(object) {
		return nil
	}
	return append([]uuid.UUID{me}, s.Below...)
}

type scopeKeyT struct{}

func scopeFrom(ctx context.Context) *Scope {
	s, _ := ctx.Value(scopeKeyT{}).(*Scope)
	return s
}

var singular = map[string]string{"leads": "lead", "accounts": "account", "contacts": "contact"}

// permKey is an object's key in the permission catalog (objects defined as data use their own key).
func permKey(object string) string {
	if k, ok := singular[object]; ok {
		return k
	}
	return object
}

func (s *Scope) Can(object, action string) bool {
	if s.Owner {
		return s.Enabled(object)
	}
	return s.Eff.Can(permKey(object), action)
}

// Enabled reports whether an object is switched on in this workspace by its products.
// Built-in objects are always addressable by the owner; objects defined as data only
// where a product includes their module.
func (s *Scope) Enabled(object string) bool {
	spec := specFor(object)
	if spec == nil || !spec.Custom || s.OwnerConsole || s.Eff == nil {
		return spec != nil || object == "users" || object == "workspaces"
	}
	return s.Eff.Objects[permKey(object)].ModuleEnabled
}

// OwnOnly reports whether the member only sees records they own.
func (s *Scope) OwnOnly(object string) bool {
	return !s.Owner && s.Eff.OwnOnly(permKey(object))
}

func (s *Scope) CanCustomize() bool {
	return s.Owner || s.Eff.HasCapability(access.CapMetadata)
}

var errForbidden = shared.Forbidden("forbidden", "You don't have permission to do that.")

// ownerScope binds Platform CRM routes to the platform workspace.
func (h *Handler) ownerScope(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := h.workspace(r.Context())
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		sc := &Scope{WS: ws, Code: "platform", IsPlatformWS: true, Owner: true, OwnerConsole: true}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), scopeKeyT{}, sc)))
	})
}

// memberScope resolves /w/{code} for the signed-in member and computes their access on
// every request, so permission changes apply immediately.
func (h *Handler) memberScope(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := identity.SessionFrom(r.Context())
		code := chi.URLParam(r, "code")
		if k := apiKeyFrom(r.Context()); k != nil {
			sc, err := h.apiKeyScope(r.Context(), code, k)
			if err != nil {
				shared.WriteError(w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), scopeKeyT{}, sc)))
			return
		}
		sc := &Scope{Code: code}
		// The platform owner may open any workspace with full access (PRD "Enter workspace", audited).
		if sess.IsPlatformOwner && sess.Audience == "owner" {
			err := h.store.Pool.QueryRow(r.Context(), `SELECT id, name, is_platform FROM crm.workspaces WHERE code = $1`, code).
				Scan(&sc.WS, &sc.Name, &sc.IsPlatformWS)
			if errors.Is(err, pgx.ErrNoRows) {
				shared.WriteError(w, r, shared.NotFound("workspace_not_found"))
				return
			}
			if err != nil {
				shared.WriteError(w, r, err)
				return
			}
			sc.Owner = true
			if sc.Eff, err = access.FullAccessIn(r.Context(), h.store.Pool, sc.WS); err != nil {
				shared.WriteError(w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), scopeKeyT{}, sc)))
			return
		}
		var membershipID uuid.UUID
		var mStatus, wStatus string
		err := h.store.Pool.QueryRow(r.Context(), `
			SELECT w.id, w.name, w.is_platform, w.status, m.id, m.status
			FROM crm.workspaces w JOIN crm.memberships m ON m.workspace_id = w.id AND m.identity_id = $2
			WHERE w.code = $1`, code, sess.IdentityID).Scan(&sc.WS, &sc.Name, &sc.IsPlatformWS, &wStatus, &membershipID, &mStatus)
		if errors.Is(err, pgx.ErrNoRows) {
			shared.WriteError(w, r, shared.Forbidden("no_workspace_access", "You don't have access to this workspace."))
			return
		}
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		if mStatus != "active" || wStatus != "active" {
			shared.WriteError(w, r, shared.Forbidden("no_workspace_access", "Your access to this workspace is paused. Contact your administrator."))
			return
		}
		// The product's setup decides how its people sign in (D-64).
		if err := identity.SessionMethodAllowed(r.Context(), sess, code); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		sc.MembershipID = membershipID
		if sc.Eff, err = access.ForMembership(r.Context(), h.store.Pool, membershipID); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		if sc.Below, err = access.IdentitiesBelow(r.Context(), h.store.Pool, membershipID); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), scopeKeyT{}, sc)))
	})
}

// ---- member app: context & dashboard ----

type workspaceContext struct {
	Workspace struct {
		ID         uuid.UUID `json:"id"`
		Code       string    `json:"code"`
		Name       string    `json:"name"`
		IsPlatform bool      `json:"isPlatform"`
		Currency   string    `json:"currency"` // amounts without their own currency use this (D-76)
	} `json:"workspace"`
	Role *struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	} `json:"role,omitempty"`
	Effective     *access.Effective `json:"effective"`
	Navigation    []access.NavItem  `json:"navigation"`
	CanCustomize  bool              `json:"canCustomize"`
	ViewerIsOwner bool              `json:"viewerIsOwner"`
	Workspaces    []struct {
		Code string `json:"code"`
		Name string `json:"name"`
	} `json:"workspaces"`
	// The product's setup, where the app needs it (conversion defaults, sign-in, theme).
	Setup struct {
		CreateContact     bool   `json:"createContact"`
		CreateOpportunity bool   `json:"createOpportunity"`
		RequireQualified  bool   `json:"requireQualified"`
		AccentColor       string `json:"accentColor,omitempty"`
		APIAccess         bool   `json:"apiAccess"`
		Webhooks          bool   `json:"webhooks"`
	} `json:"setup"`
	// Apps are the setups installed in this product that the viewer may use (D-73).
	// Switching app changes the menu and colour; the records are shared.
	Apps []workspaceApp `json:"apps"`
	App  string         `json:"app,omitempty"` // the selected app's key
}

type workspaceApp struct {
	ID          uuid.UUID `json:"id"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Icon        string    `json:"icon,omitempty"`
	AccentColor string    `json:"accentColor,omitempty"`
	Version     int       `json:"version"`
	modules     map[string]bool
}

// appsFor lists the product's active apps the viewer may use (owner: all).
func (h *Handler) appsFor(ctx context.Context, sc *Scope) []workspaceApp {
	out := []workspaceApp{}
	if sc.IsPlatformWS {
		return out
	}
	allowed := map[uuid.UUID]bool{}
	for _, p := range sc.Eff.Products {
		allowed[p.ID] = true
	}
	rows, err := h.store.Pool.Query(ctx, `
		SELECT p.id, p.key, p.name, COALESCE(p.icon, ''), wp.config_version, v.config
		FROM crm.workspace_products wp
		JOIN crm.products p ON p.id = wp.product_id AND p.status <> 'archived'
		JOIN crm.product_versions v ON v.product_id = wp.product_id AND v.version = wp.config_version
		WHERE wp.workspace_id = $1 AND wp.status = 'active' ORDER BY wp.created_at, p.name`, sc.WS)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var a workspaceApp
		var raw []byte
		if rows.Scan(&a.ID, &a.Key, &a.Name, &a.Icon, &a.Version, &raw) != nil {
			continue
		}
		if !sc.Owner && !allowed[a.ID] {
			continue
		}
		var cfg struct {
			AccentColor string   `json:"accentColor"`
			Modules     []string `json:"modules"`
		}
		_ = json.Unmarshal(raw, &cfg)
		a.AccentColor, a.modules = cfg.AccentColor, map[string]bool{}
		for _, m := range cfg.Modules {
			a.modules[m] = true
		}
		out = append(out, a)
	}
	return out
}

// navForApp keeps the record objects the selected app switches on; dashboard,
// reports, automation and settings stay.
func navForApp(nav []access.NavItem, app workspaceApp) []access.NavItem {
	keys := map[string]bool{"app-users": app.modules["app_users"], "businesses": app.modules["directory"]}
	for _, o := range access.CatalogObjects() {
		if o.App {
			continue
		}
		on := app.modules[o.Module]
		keys[o.Module] = keys[o.Module] || on
		keys[o.Key] = keys[o.Key] || on
	}
	out := make([]access.NavItem, 0, len(nav))
	for _, n := range nav {
		if n.Group == "CRM" || n.Group == "App" {
			if on, known := keys[n.Key]; known && !on {
				continue
			}
		}
		out = append(out, n)
	}
	return out
}

func displayWorkspaceName(name string, isPlatform bool) string {
	if isPlatform {
		return "Platform CRM"
	}
	return name
}

func (h *Handler) handleContext(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	sess := identity.SessionFrom(r.Context())
	var out workspaceContext
	out.Workspace.ID, out.Workspace.Code, out.Workspace.IsPlatform = sc.WS, sc.Code, sc.IsPlatformWS
	out.Workspace.Name = displayWorkspaceName(sc.Name, sc.IsPlatformWS)
	_ = h.store.Pool.QueryRow(r.Context(), `SELECT COALESCE(currency, 'INR') FROM crm.workspaces WHERE id = $1`, sc.WS).Scan(&out.Workspace.Currency)
	if out.Workspace.Currency == "" {
		out.Workspace.Currency = "INR"
	}
	if sc.Eff.RoleKey != "" {
		out.Role = &struct {
			Key  string `json:"key"`
			Name string `json:"name"`
		}{sc.Eff.RoleKey, sc.Eff.RoleName}
	}
	out.Effective = sc.Eff
	out.Navigation = access.WorkspaceNav(sc.Code, sc.Eff, access.HasSupport(sc.WS))
	out.CanCustomize = sc.CanCustomize()
	out.ViewerIsOwner = sc.Owner
	if cfg, ok := cachedSetup(r.Context(), h.store.Pool, sc.WS); ok {
		out.Setup.CreateContact, out.Setup.CreateOpportunity, out.Setup.RequireQualified = cfg.Conversion.CreateContact, cfg.Conversion.CreateOpportunity, cfg.Conversion.RequireQualified
		out.Setup.AccentColor, out.Setup.APIAccess, out.Setup.Webhooks = cfg.AccentColor, cfg.Integrations.APIAccess, cfg.Integrations.Webhooks
	}
	// Default "All apps": the whole product's menu. Picking one app narrows it (D-73).
	out.Apps = h.appsFor(r.Context(), sc)
	out.App = "all"
	want := r.URL.Query().Get("app")
	for _, a := range out.Apps {
		if len(out.Apps) > 1 && a.Key == want {
			out.App = a.Key
			out.Navigation = navForApp(out.Navigation, a)
			if a.AccentColor != "" {
				out.Setup.AccentColor = a.AccentColor
			}
		}
	}
	if len(out.Apps) == 1 {
		out.App = out.Apps[0].Key
	}
	out.Workspaces = []struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}{}
	if sc.Owner {
		ws := sc.WS
		_ = shared.WriteAudit(r.Context(), h.store.Pool, audit(r, ws, "workspace.entered", "workspace", &ws, nil, nil))
		shared.WriteJSON(w, http.StatusOK, out)
		return
	}
	ms, err := access.ListActiveMemberships(r.Context(), h.store.Pool, sess.IdentityID)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	for _, m := range ms {
		out.Workspaces = append(out.Workspaces, struct {
			Code string `json:"code"`
			Name string `json:"name"`
		}{m.WorkspaceCode, displayWorkspaceName(m.WorkspaceName, m.IsPlatformWorkspace)})
	}
	shared.WriteJSON(w, http.StatusOK, out)
}

// KPI is a dashboard card; extensions (connectors) may add their own.
type KPI = dashboardKPI

type dashboardKPI struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value int    `json:"value"`
	Hint  string `json:"hint,omitempty"`
	Path  string `json:"path,omitempty"`
	Icon  string `json:"icon,omitempty"` // nav icon key (objects defined as data, connected apps)
}

type recentList struct {
	Object string       `json:"object"`
	Label  string       `json:"label"`
	Rows   []RelatedRow `json:"rows"`
}

func (h *Handler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !sc.Owner && !sc.Eff.HasCapability(access.CapDashboard) {
		shared.WriteError(w, r, errForbidden)
		return
	}
	me := actor(r)
	out := struct {
		KPIs   []dashboardKPI `json:"kpis"`
		Recent []recentList   `json:"recent"`
	}{KPIs: []dashboardKPI{}, Recent: []recentList{}}
	keys := []string{"leads", "accounts", "contacts"}
	for _, d := range dynamicSpecs() {
		keys = append(keys, d.Key)
	}
	for _, key := range keys {
		spec := specFor(key)
		if spec == nil || !sc.Can(key, "read") {
			continue
		}
		if spec.Custom && len(out.KPIs) >= 8 {
			continue // keep the dashboard readable; every object is one click away in the sidebar
		}
		where := " WHERE t.workspace_id = $1 AND t.deleted_at IS NULL"
		args := []any{sc.WS}
		if sc.OwnOnly(key) {
			where += " AND t.owner_id = ANY($2::uuid[])"
			args = append(args, sc.OwnersFor(key, me))
		}
		var total, recent int
		if err := h.store.Pool.QueryRow(r.Context(), `SELECT count(*), count(*) FILTER (WHERE t.created_at > now() - interval '7 days') FROM crm.`+
			spec.Table+` t`+where, args...).Scan(&total, &recent); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		k := dashboardKPI{Key: key, Label: spec.Plural, Value: total, Path: "/crm/w/" + sc.Code + "/" + key, Icon: spec.Icon}
		if sc.OwnOnly(key) {
			k.Label = "My " + spec.Plural
		}
		if recent > 0 {
			k.Hint = strconv.Itoa(recent) + " new this week"
		}
		if key == "leads" {
			var open int
			if err := h.store.Pool.QueryRow(r.Context(), `SELECT count(*) FROM crm.leads t`+where+` AND t.status NOT IN ('converted', 'lost')`, args...).Scan(&open); err != nil {
				shared.WriteError(w, r, err)
				return
			}
			k.Hint = strconv.Itoa(open) + " open"
		}
		out.KPIs = append(out.KPIs, k)

		list := recentList{Object: key, Label: "Recent " + spec.Plural, Rows: []RelatedRow{}}
		rows, err := h.store.Pool.Query(r.Context(), `SELECT t.id::text, t.code, `+spec.TitleSQL+`, '', '' FROM crm.`+spec.Table+` t`+where+
			` ORDER BY t.updated_at DESC LIMIT 5`, args...)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		for rows.Next() {
			var rr RelatedRow
			if err := rows.Scan(&rr.ID, &rr.Code, &rr.Title, &rr.Subtitle, &rr.Status); err != nil {
				rows.Close()
				shared.WriteError(w, r, err)
				return
			}
			list.Rows = append(list.Rows, rr)
		}
		rows.Close()
		out.Recent = append(out.Recent, list)
	}
	for _, ext := range h.extensions {
		if ext.KPIs == nil {
			continue
		}
		extra, err := ext.KPIs(r.Context(), sc)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		out.KPIs = append(out.KPIs, extra...)
	}
	shared.WriteJSON(w, http.StatusOK, out)
}

// ScopeFrom exposes the request's scope to extensions.
func ScopeFrom(ctx context.Context) *Scope { return scopeFrom(ctx) }

// Extension lets connectors add routes, related lists and KPIs without the record
// engine knowing about them.
type Extension struct {
	MemberRoutes func(r chi.Router) // mounted inside /w/{code} (scope available via ScopeFrom)
	Related      func(ctx context.Context, sc *Scope, object, recordID string) ([]RelatedList, error)
	KPIs         func(ctx context.Context, sc *Scope) ([]KPI, error)
	// OnEvent sees every record event as it is relayed (inside the relay's transaction).
	OnEvent func(ctx context.Context, tx pgx.Tx, ev Event) error
	// BeforeList runs when someone opens a list (e.g. to fetch fresh data from an app).
	BeforeList func(ws uuid.UUID, object string)
}

// Extend registers an extension; call before Routes.
func (h *Handler) Extend(e Extension) { h.extensions = append(h.extensions, e) }
