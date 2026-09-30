package access

import (
	"sync"

	"github.com/google/uuid"
)

var (
	supportMu sync.RWMutex
	supportWS = map[uuid.UUID]bool{}
)

// SetSupport marks a workspace as having a connected support source (a connector).
func SetSupport(ws uuid.UUID, code string) {
	supportMu.Lock()
	supportWS[ws] = true
	supportMu.Unlock()
}

func HasSupport(ws uuid.UUID) bool {
	supportMu.RLock()
	defer supportMu.RUnlock()
	return supportWS[ws]
}

// WorkspaceNav is a member's sidebar for one workspace: only modules they may read,
// plus the dashboard when granted (PRD §10.1: an item appears only with read
// permission and an enabled module).
func WorkspaceNav(code string, e *Effective, hasSupport bool) []NavItem {
	base := "/crm/w/" + code
	nav := []NavItem{}
	if e.HasCapability(CapDashboard) {
		nav = append(nav, NavItem{Key: "home", Label: "Dashboard", Path: base + "/home", Icon: "layout-dashboard", Available: true})
	}
	icons := map[string]string{"lead": "user-plus", "account": "briefcase", "contact": "contact"}
	for _, o := range CatalogObjects() {
		if o.Key == "ticket" || o.App {
			continue
		}
		if e.Can(o.Key, "read") {
			if o.Custom {
				nav = append(nav, NavItem{Key: o.Key, Label: o.Label, Path: base + "/" + o.Route, Icon: o.Icon, Group: "CRM", Available: true})
				continue
			}
			nav = append(nav, NavItem{Key: o.Module, Label: o.Label, Path: base + "/" + o.Module, Icon: icons[o.Key], Group: "CRM", Available: true})
		}
	}
	// A connected app's support tickets are Cases like in every other product (D-72).
	// The connected app's own data (Business Card Snap): users with their access, and business listings.
	if hasSupport && e.Can("app_user", "read") {
		nav = append(nav, NavItem{Key: "app-users", Label: "App users", Path: base + "/app-users", Icon: "smartphone", Group: "App", Available: true})
	}
	if hasSupport && e.Can("app_business", "read") {
		nav = append(nav, NavItem{Key: "businesses", Label: "Businesses", Path: base + "/businesses", Icon: "store", Group: "App", Available: true})
	}
	// Reports & dashboards run as the viewer, so anyone with the dashboard can use them (D-50).
	if e.HasCapability(CapDashboard) {
		nav = append(nav,
			NavItem{Key: "reports", Label: "Reports", Path: base + "/reports", Icon: "bar-chart-3", Group: "Analytics", Available: true},
			NavItem{Key: "dashboards", Label: "Dashboards", Path: base + "/dashboards", Icon: "layout-grid", Group: "Analytics", Available: true},
		)
	}
	// Automation & outreach (D-63, D-65, D-66).
	if e.HasCapability(CapWorkflows) {
		nav = append(nav, NavItem{Key: "workflows", Label: "Workflows", Path: base + "/workflows", Icon: "workflow", Group: "Automation", Available: true})
	}
	if e.HasCapability(CapCampaigns) {
		nav = append(nav, NavItem{Key: "campaigns", Label: "Email campaigns", Path: base + "/campaigns", Icon: "megaphone", Group: "Automation", Available: true})
	}
	if e.HasCapability(CapEmailSend) {
		nav = append(nav, NavItem{Key: "mailboxes", Label: "Email & calendar", Path: base + "/settings/email", Icon: "mail", Group: "Settings", Available: true})
	}
	if e.HasCapability(CapAccessManage) || e.HasCapability(CapMembersManage) {
		nav = append(nav,
			NavItem{Key: "admin", Label: "Users & access", Path: base + "/settings/access", Icon: "shield-check", Group: "Settings", Available: true},
			NavItem{Key: "teams", Label: "Teams", Path: base + "/settings/teams", Icon: "users", Group: "Settings", Available: true},
		)
	}
	if e.HasCapability(CapAccessManage) {
		nav = append(nav, NavItem{Key: "sso", Label: "Single sign-on", Path: base + "/settings/sso", Icon: "key-round", Group: "Settings", Available: true})
	}
	// A product's own objects and fields (D-79).
	if e.HasCapability(CapMetadata) {
		nav = append(nav, NavItem{Key: "objects", Label: "Objects & fields", Path: base + "/settings/objects", Icon: "box", Group: "Settings", Available: true})
	}
	if e.HasCapability(CapDeveloper) {
		nav = append(nav, NavItem{Key: "developer", Label: "API & webhooks", Path: base + "/settings/developer", Icon: "code", Group: "Settings", Available: true})
	}
	nav = append(nav, NavItem{Key: "settings", Label: "Profile & security", Path: "/crm/me", Icon: "settings", Group: "Account", Available: true})
	return nav
}

// DefaultMembership picks the membership a member lands in: the first customer
// workspace, otherwise the platform team. nil when there are none.
func DefaultMembership(ms []Membership) *Membership {
	for i := range ms {
		if !ms[i].IsPlatformWorkspace {
			return &ms[i]
		}
	}
	if len(ms) > 0 {
		return &ms[0]
	}
	return nil
}
