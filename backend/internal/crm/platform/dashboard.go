package platform

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
)

type KPI struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value int    `json:"value"`
	Hint  string `json:"hint,omitempty"`
	Path  string `json:"path,omitempty"`
}

type ChecklistStep struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"` // done | todo | upcoming (module not built yet)
	Path        string `json:"path,omitempty"`
}

type RecentWorkspace struct {
	ID        uuid.UUID `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Products  int       `json:"products"`
	Setups    []string  `json:"setupNames"`
	Members   int       `json:"members"`
	CreatedAt time.Time `json:"createdAt"`
}

type Dashboard struct {
	KPIs             []KPI             `json:"kpis"`
	Checklist        []ChecklistStep   `json:"checklist"`
	RecentWorkspaces []RecentWorkspace `json:"recentWorkspaces"`
	RefreshedAt      time.Time         `json:"refreshedAt"`
}

func (h *Handler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	d, err := h.dashboard(r.Context(), ownerFilterFrom(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, d)
}

// ownerFilter narrows the owner console to one product and/or one app (D-74).
type ownerFilter struct {
	Workspace *uuid.UUID // a product (workspace)
	App       *uuid.UUID // an app (setup)
}

func (f ownerFilter) on() bool { return f.Workspace != nil || f.App != nil }

func ownerFilterFrom(r *http.Request) ownerFilter {
	var f ownerFilter
	if id, err := uuid.Parse(r.URL.Query().Get("product")); err == nil {
		f.Workspace = &id
	}
	if id, err := uuid.Parse(r.URL.Query().Get("app")); err == nil {
		f.App = &id
	}
	return f
}

// filteredWorkspaces is the SQL for the products a filter keeps ($1 product, $2 app).
const filteredWorkspaces = `SELECT w.id FROM crm.workspaces w WHERE NOT w.is_platform
	AND ($1::uuid IS NULL OR w.id = $1::uuid)
	AND ($2::uuid IS NULL OR EXISTS (SELECT 1 FROM crm.workspace_products wp WHERE wp.workspace_id = w.id AND wp.product_id = $2::uuid AND wp.status = 'active'))`

// filteredWorkspacesAt is filteredWorkspaces with the product and app at other placeholders.
func filteredWorkspacesAt(product, app int) string {
	return strings.NewReplacer("$1", fmt.Sprintf("$%d", product), "$2", fmt.Sprintf("$%d", app)).Replace(filteredWorkspaces)
}

func (h *Handler) dashboard(ctx context.Context, f ownerFilter) (*Dashboard, error) {
	var (
		activeProducts, draftProducts         int
		activeWorkspaces, totalWorkspaces     int
		activeIdentities, suspendedIdentities int
		invitedMembers, pendingInvites        int
		customPermissionSets, testedCustomers int
		totalLeads, openLeads, accounts       int
		totalUsers, contacts                  int
		installedApps, appProducts            int
	)
	err := h.store.Pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM crm.products WHERE status = 'active'),
		  (SELECT count(*) FROM crm.products WHERE status = 'draft'),
		  (SELECT count(*) FROM crm.workspaces WHERE NOT is_platform AND status = 'active'),
		  (SELECT count(*) FROM crm.workspaces WHERE NOT is_platform),
		  (SELECT count(*) FROM crm.identities WHERE status = 'active'), -- the Users page lists the owner too
		  (SELECT count(*) FROM crm.identities WHERE status = 'suspended'),
		  (SELECT count(*) FROM crm.memberships WHERE status = 'invited'),
		  (SELECT count(*) FROM crm.invitations WHERE status IN ('pending', 'delivered') AND expires_at > now()),
		  (SELECT count(*) FROM crm.permission_sets),
		  (SELECT count(DISTINCT i.id)
		     FROM crm.identities i
		     JOIN crm.memberships m ON m.identity_id = i.id AND m.status = 'active'
		     JOIN crm.workspaces w ON w.id = m.workspace_id AND NOT w.is_platform
		     JOIN crm.role_assignments ra ON ra.membership_id = m.id
		     JOIN crm.roles ro ON ro.id = ra.role_id AND ro.key = 'SUPER_ADMIN'
		    WHERE i.last_login_at IS NOT NULL),
		  (SELECT count(*) FROM crm.leads l WHERE l.deleted_at IS NULL),
		  (SELECT count(*) FROM crm.leads l WHERE l.deleted_at IS NULL AND l.status NOT IN ('converted', 'lost')),
		  (SELECT count(*) FROM crm.accounts a WHERE a.deleted_at IS NULL),
		  (SELECT count(*) FROM crm.identities WHERE status <> 'deleted'),
		  (SELECT count(*) FROM crm.contacts c WHERE c.deleted_at IS NULL),
		  (SELECT count(*) FROM crm.workspace_products wp JOIN crm.workspaces w ON w.id = wp.workspace_id AND NOT w.is_platform WHERE wp.status = 'active'),
		  (SELECT count(DISTINCT wp.workspace_id) FROM crm.workspace_products wp JOIN crm.workspaces w ON w.id = wp.workspace_id AND NOT w.is_platform WHERE wp.status = 'active')`).Scan(
		&activeProducts, &draftProducts, &activeWorkspaces, &totalWorkspaces,
		&activeIdentities, &suspendedIdentities, &invitedMembers, &pendingInvites,
		&customPermissionSets, &testedCustomers, &totalLeads, &openLeads, &accounts, &totalUsers, &contacts, &installedApps, &appProducts)
	if err != nil {
		return nil, err
	}
	// The setup checklist is about the whole platform, whatever the filter.
	allLeads, allActiveWorkspaces := totalLeads, activeWorkspaces
	if f.on() {
		// Counts inside the selected product / app only.
		err := h.store.Pool.QueryRow(ctx, `
			WITH ws AS (`+filteredWorkspaces+`)
			SELECT
			  (SELECT count(*) FROM crm.workspaces w WHERE w.id IN (SELECT id FROM ws) AND w.status = 'active'),
			  (SELECT count(*) FROM ws),
			  (SELECT count(*) FROM crm.workspace_products wp WHERE wp.workspace_id IN (SELECT id FROM ws) AND wp.status = 'active'
			     AND ($2::uuid IS NULL OR wp.product_id = $2::uuid)),
			  (SELECT count(DISTINCT wp.workspace_id) FROM crm.workspace_products wp WHERE wp.workspace_id IN (SELECT id FROM ws) AND wp.status = 'active'
			     AND ($2::uuid IS NULL OR wp.product_id = $2::uuid)),
			  (SELECT count(*) FROM crm.leads l WHERE l.deleted_at IS NULL AND l.workspace_id IN (SELECT id FROM ws)),
			  (SELECT count(*) FROM crm.leads l WHERE l.deleted_at IS NULL AND l.status NOT IN ('converted', 'lost') AND l.workspace_id IN (SELECT id FROM ws)),
			  (SELECT count(*) FROM crm.accounts a WHERE a.deleted_at IS NULL AND a.workspace_id IN (SELECT id FROM ws)),
			  (SELECT count(*) FROM crm.contacts c WHERE c.deleted_at IS NULL AND c.workspace_id IN (SELECT id FROM ws)),
			  (SELECT count(DISTINCT m.identity_id) FROM crm.memberships m WHERE m.workspace_id IN (SELECT id FROM ws) AND m.status <> 'revoked'),
			  (SELECT count(DISTINCT m.identity_id) FROM crm.memberships m WHERE m.workspace_id IN (SELECT id FROM ws) AND m.status = 'active'),
			  (SELECT count(*) FROM crm.invitations i WHERE i.workspace_id IN (SELECT id FROM ws) AND i.status IN ('pending', 'delivered') AND i.expires_at > now())`,
			f.Workspace, f.App).Scan(&activeWorkspaces, &totalWorkspaces, &installedApps, &appProducts,
			&totalLeads, &openLeads, &accounts, &contacts, &totalUsers, &activeIdentities, &pendingInvites)
		if err != nil {
			return nil, err
		}
		suspendedIdentities = 0
	}

	d := &Dashboard{
		KPIs: []KPI{
			// "Products" counts the same rows as the Products page (every customer product).
			{Key: "workspaces", Label: "Products", Value: totalWorkspaces, Hint: productsHint(activeWorkspaces, totalWorkspaces-activeWorkspaces), Path: "/crm/owner/workspaces"},
			// Apps installed across products (a product can have several, D-73).
			{Key: "apps", Label: "Apps", Value: installedApps, Hint: plural(appProducts, "products"), Path: "/crm/owner/apps"},
			{Key: "leads", Label: "Leads", Value: totalLeads, Hint: plural(openLeads, "open"), Path: "/crm/owner/leads"},
			{Key: "accounts", Label: "Accounts", Value: accounts, Path: "/crm/owner/accounts"},
			{Key: "contacts", Label: "Contacts", Value: contacts, Path: "/crm/owner/contacts"},
			{Key: "users", Label: "Users", Value: totalUsers, Hint: activeHint(activeIdentities, suspendedIdentities), Path: "/crm/owner/users"},
			{Key: "invites", Label: "Invitations", Value: pendingInvites, Hint: "pending", Path: "/crm/owner/users?status=invited"},
		},
		Checklist: []ChecklistStep{
			step("lead", "Add a customer lead", "Capture the customer you're onboarding in Platform CRM.", allLeads > 0, false, "/crm/owner/leads"),
			step("provision", "Add a product for the customer", "Name it, invite its Super Admin, then add its first app (objects, roles, sales process, sign-in) from the Apps tab.", allActiveWorkspaces > 0, false, "/crm/owner/workspaces/new"),
			step("product", "Publish the app", "Publishing makes the app live for the product's users.", activeProducts > 0, false, "/crm/owner/workspaces"),
			step("permissions", "Set up permissions", "Create permission sets and give users exactly the access they need.", customPermissionSets > 0, false, "/crm/owner/users?tab=permissions"),
			step("test", "First sign-in", "The customer's Super Admin signs in for the first time.", testedCustomers > 0, false, "/crm/owner/workspaces"),
		},
		RecentWorkspaces: []RecentWorkspace{},
		RefreshedAt:      time.Now().UTC(),
	}

	rows, err := h.store.Pool.Query(ctx, `
		SELECT w.id, w.code, w.name, w.status, w.created_at,
		       (SELECT count(*) FROM crm.workspace_products wp WHERE wp.workspace_id = w.id AND wp.status = 'active'),
		       (SELECT count(*) FROM crm.memberships m WHERE m.workspace_id = w.id AND m.status = 'active'),
		       COALESCE((SELECT array_agg(p.name || ' v' || wp.config_version ORDER BY p.name) FROM crm.workspace_products wp
		                  JOIN crm.products p ON p.id = wp.product_id WHERE wp.workspace_id = w.id AND wp.status = 'active'), '{}')
		FROM crm.workspaces w
		WHERE w.id IN (`+filteredWorkspaces+`)
		ORDER BY w.created_at DESC
		LIMIT 5`, f.Workspace, f.App)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var rw RecentWorkspace
		if err := rows.Scan(&rw.ID, &rw.Code, &rw.Name, &rw.Status, &rw.CreatedAt, &rw.Products, &rw.Members, &rw.Setups); err != nil {
			return nil, err
		}
		d.RecentWorkspaces = append(d.RecentWorkspaces, rw)
	}
	return d, rows.Err()
}

func productsHint(active, inactive int) string {
	return joinHints(plural(active, "active"), plural(inactive, "not active"))
}

// joinHints joins the non-empty parts with " · ".
func joinHints(list ...string) string {
	parts := []string{}
	for _, p := range list {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " · ")
}

func step(key, title, desc string, done, upcoming bool, path string) ChecklistStep {
	status := "todo"
	switch {
	case done:
		status = "done"
	case upcoming:
		status = "upcoming"
	}
	return ChecklistStep{Key: key, Title: title, Description: desc, Status: status, Path: path}
}

func activeHint(active, suspended int) string {
	h := strconv.Itoa(active) + " active"
	if suspended > 0 {
		h += " · " + strconv.Itoa(suspended) + " suspended"
	}
	return h
}

func plural(n int, label string) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n) + " " + label
}

// InstalledApp is one app (a setup) installed in one product.
type InstalledApp struct {
	WorkspaceID   uuid.UUID `json:"workspaceId"`
	WorkspaceCode string    `json:"workspaceCode"`
	WorkspaceName string    `json:"workspaceName"`
	ProductID     uuid.UUID `json:"productId"`
	Key           string    `json:"key"`
	Name          string    `json:"name"`
	Icon          string    `json:"icon,omitempty"`
	Version       int       `json:"version"`
	LatestVersion int       `json:"latestVersion"`
	Status        string    `json:"status"`
	Modules       int       `json:"modules"`
	InstalledAt   time.Time `json:"installedAt"`
}

// GET /platform/apps — every app installed in every product (D-73).
func (h *Handler) handleListApps(w http.ResponseWriter, r *http.Request) {
	f := ownerFilterFrom(r)
	rows, err := h.store.Pool.Query(r.Context(), `
		SELECT w.id, w.code, w.name, p.id, p.key, p.name, COALESCE(p.icon, ''), wp.config_version, COALESCE(p.current_version, wp.config_version),
		       wp.status, COALESCE(jsonb_array_length(v.config->'modules'), 0), wp.created_at
		FROM crm.workspace_products wp
		JOIN crm.workspaces w ON w.id = wp.workspace_id AND NOT w.is_platform
		JOIN crm.products p ON p.id = wp.product_id
		LEFT JOIN crm.product_versions v ON v.product_id = wp.product_id AND v.version = wp.config_version
		WHERE ($1::uuid IS NULL OR w.id = $1::uuid) AND ($2::uuid IS NULL OR p.id = $2::uuid)
		ORDER BY w.name, wp.created_at`, f.Workspace, f.App)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	out := []InstalledApp{}
	for rows.Next() {
		var a InstalledApp
		if err := rows.Scan(&a.WorkspaceID, &a.WorkspaceCode, &a.WorkspaceName, &a.ProductID, &a.Key, &a.Name, &a.Icon, &a.Version,
			&a.LatestVersion, &a.Status, &a.Modules, &a.InstalledAt); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		out = append(out, a)
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}
