package access

import (
	"sync"

	"github.com/google/uuid"
)

var (
	supportMu   sync.RWMutex
	supportWS   = map[uuid.UUID]bool{}
	supportCode string
)

// SetSupport marks a workspace as having a connected support source (a connector).
func SetSupport(ws uuid.UUID, code string) {
	supportMu.Lock()
	supportWS[ws] = true
	if supportCode == "" {
		supportCode = code
	}
	supportMu.Unlock()
}

// SupportPath is where the owner's "Support" goes: the first connected app's tickets.
func SupportPath() string {
	supportMu.RLock()
	defer supportMu.RUnlock()
	if supportCode == "" {
		return ""
	}
	return "/crm/w/" + supportCode + "/support"
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
	for _, o := range Objects {
		if o.Key == "ticket" {
			continue
		}
		if e.Can(o.Key, "read") {
			nav = append(nav, NavItem{Key: o.Module, Label: o.Label, Path: base + "/" + o.Module, Icon: icons[o.Key], Group: "CRM", Available: true})
		}
	}
	// Support tickets come from a connected app; the module is shown only where one is attached.
	if hasSupport && e.Can("ticket", "read") {
		nav = append(nav, NavItem{Key: "support", Label: "Support", Path: base + "/support", Icon: "life-buoy", Group: "CRM", Available: true})
	}
	if e.HasCapability(CapAccessManage) || e.HasCapability(CapMembersManage) {
		nav = append(nav, NavItem{Key: "admin", Label: "Users & access", Path: base + "/settings/access", Icon: "shield-check", Group: "Settings", Available: true})
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
