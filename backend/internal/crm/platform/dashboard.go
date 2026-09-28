package platform

import (
	"context"
	"net/http"
	"strconv"
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
	d, err := h.dashboard(r.Context())
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, d)
}

func (h *Handler) dashboard(ctx context.Context) (*Dashboard, error) {
	var (
		activeProducts, draftProducts         int
		activeWorkspaces, totalWorkspaces     int
		activeIdentities, suspendedIdentities int
		invitedMembers, pendingInvites        int
		customPermissionSets, testedCustomers int
		totalLeads, openLeads, accounts       int
		totalUsers                            int
	)
	err := h.store.Pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM crm.products WHERE status = 'active'),
		  (SELECT count(*) FROM crm.products WHERE status = 'draft'),
		  (SELECT count(*) FROM crm.workspaces WHERE NOT is_platform AND status = 'active'),
		  (SELECT count(*) FROM crm.workspaces WHERE NOT is_platform),
		  (SELECT count(*) FROM crm.identities WHERE status = 'active' AND NOT is_platform_owner),
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
		  (SELECT count(*) FROM crm.identities WHERE status <> 'deleted' AND NOT is_platform_owner)`).Scan(
		&activeProducts, &draftProducts, &activeWorkspaces, &totalWorkspaces,
		&activeIdentities, &suspendedIdentities, &invitedMembers, &pendingInvites,
		&customPermissionSets, &testedCustomers, &totalLeads, &openLeads, &accounts, &totalUsers)
	if err != nil {
		return nil, err
	}

	d := &Dashboard{
		KPIs: []KPI{
			{Key: "products", Label: "Active products", Value: activeProducts, Hint: plural(draftProducts, "draft"), Path: "/crm/owner/products"},
			{Key: "leads", Label: "Total leads", Value: totalLeads, Hint: plural(openLeads, "open"), Path: "/crm/owner/leads"},
			{Key: "accounts", Label: "Accounts", Value: accounts, Path: "/crm/owner/accounts"},
			{Key: "users", Label: "Total users", Value: totalUsers, Hint: activeHint(activeIdentities, suspendedIdentities), Path: "/crm/owner/users"},
			{Key: "workspaces", Label: "Customer workspaces", Value: activeWorkspaces, Hint: plural(totalWorkspaces-activeWorkspaces, "not active"), Path: "/crm/owner/workspaces"},
			{Key: "invites", Label: "Pending invites", Value: pendingInvites, Hint: plural(invitedMembers, "awaiting acceptance"), Path: "/crm/owner/workspaces"},
		},
		Checklist: []ChecklistStep{
			step("product", "Create a product", "Define modules, roles and login methods, then publish.", activeProducts > 0, false, "/crm/owner/products"),
			step("lead", "Add a customer lead", "Capture the customer you're onboarding in Platform CRM.", totalLeads > 0, false, "/crm/owner/leads"),
			step("provision", "Provision the customer", "Convert the lead or create their workspace and invite a Super Admin.", activeWorkspaces > 0, false, "/crm/owner/workspaces/new"),
			step("permissions", "Configure permissions", "Create permission sets and give users exactly the access they need.", customPermissionSets > 0, false, "/crm/owner/users?tab=permissions"),
			step("test", "Test the workspace", "The customer's Super Admin signs in for the first time.", testedCustomers > 0, false, "/crm/owner/workspaces"),
		},
		RecentWorkspaces: []RecentWorkspace{},
		RefreshedAt:      time.Now().UTC(),
	}

	rows, err := h.store.Pool.Query(ctx, `
		SELECT w.id, w.code, w.name, w.status, w.created_at,
		       (SELECT count(*) FROM crm.workspace_products wp WHERE wp.workspace_id = w.id AND wp.status = 'active'),
		       (SELECT count(*) FROM crm.memberships m WHERE m.workspace_id = w.id AND m.status = 'active')
		FROM crm.workspaces w
		WHERE NOT w.is_platform
		ORDER BY w.created_at DESC
		LIMIT 5`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var rw RecentWorkspace
		if err := rows.Scan(&rw.ID, &rw.Code, &rw.Name, &rw.Status, &rw.CreatedAt, &rw.Products, &rw.Members); err != nil {
			return nil, err
		}
		d.RecentWorkspaces = append(d.RecentWorkspaces, rw)
	}
	return d, rows.Err()
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
