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
		// Line items are edited from their quote, order or invoice, not from the menu.
		if o.Key == "ticket" || o.App || o.Key == "line_items" {
			continue
		}
		if e.Can(o.Key, "read") {
			if o.Custom {
				nav = append(nav, NavItem{Key: o.Key, Label: o.Label, Path: base + "/" + o.Route, Icon: o.Icon, Group: navGroup(o.Key), Available: true})
				continue
			}
			nav = append(nav, NavItem{Key: o.Module, Label: o.Label, Path: base + "/" + o.Module, Icon: icons[o.Key], Group: "Sales", Available: true})
		}
	}
	// Forecasts are worked out from opportunities (D-112).
	if e.Can("opportunities", "read") {
		nav = append(nav, NavItem{Key: "forecasts", Label: "Forecasts", Path: base + "/forecasts", Icon: "trending-up", Group: "Sales", Available: true})
	}
	// Requests waiting for a decision: discounts, refunds, credit notes, write-offs (D-121).
	if e.Can("quotes", "read") || e.Can("invoices", "read") || e.HasCapability(CapApprovals) {
		nav = append(nav, NavItem{Key: "approvals", Label: "Approvals", Path: base + "/approvals", Icon: "badge-check", Group: "Sales operations", Available: true})
	}
	// Scanned business cards (D-98): for anyone who works with leads or contacts.
	if e.Can("lead", "read") || e.Can("contact", "read") {
		nav = append(nav, NavItem{Key: "cards", Label: "Business cards", Path: base + "/cards", Icon: "scan-line", Group: "Sales", Available: true})
	}
	// Vendors are accounts of type Vendor or Supplier (D-114): the same records, a filtered list.
	if e.Can("account", "read") {
		nav = append(nav, NavItem{Key: "vendors", Label: "Vendors", Path: base + "/accounts?type=vendor", Icon: "truck", Group: "Purchasing", Available: true})
	}
	// Services are catalog items of type Service (D-114).
	if e.Can("catalog_items", "read") {
		nav = append(nav, NavItem{Key: "services", Label: "Services", Path: base + "/catalog_items?itemType=service", Icon: "wrench", Group: "Products", Available: true})
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
	// What each campaign brought in (D-125).
	if e.HasCapability(CapCampaigns) || e.Can("opportunities", "read") {
		nav = append(nav, NavItem{Key: "campaign-results", Label: "Campaign results", Path: base + "/campaign-results", Icon: "megaphone", Group: "Automation", Available: true})
	}
	if e.HasCapability(CapPricing) || e.HasCapability(CapApprovals) {
		nav = append(nav, NavItem{Key: "pricing", Label: "Pricing & approvals", Path: base + "/settings/pricing", Icon: "sliders", Group: "Settings", Available: true})
	}
	if e.HasCapability(CapEmailSend) {
		nav = append(nav, NavItem{Key: "mailboxes", Label: "Email & calendar", Path: base + "/settings/email", Icon: "mail", Group: "Settings", Available: true})
	}
	if e.HasCapability(CapAccessManage) || e.HasCapability(CapMembersManage) {
		nav = append(nav,
			NavItem{Key: "business", Label: "Business profile", Path: base + "/settings/business", Icon: "store", Group: "Settings", Available: true},
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

// navGroups: the sidebar section of each object (D-117). Anything not listed (a
// business's own objects) goes under CRM.
var navGroups = map[string]string{
	"opportunities": "Sales", "tasks": "Sales", "events": "Sales", "notes": "Sales", "communications": "Sales",
	"catalog_items": "Products", "price_books": "Products", "assets": "Products",
	"quotes": "Sales operations", "sales_orders": "Sales operations", "invoices": "Sales operations", "contracts": "Sales operations",
	"subscriptions": "Sales operations",
	"cases":         "Service", "appointments": "Service", "entitlements": "Service", "sla_policies": "Service", "solutions": "Service",
	"purchase_orders": "Purchasing",
	"income":          "Finance", "expenses": "Finance", "payments": "Finance", "refunds": "Finance", "credit_notes": "Finance",
	"debit_notes": "Finance", "adjustments": "Finance",
	"territories": "Sales operations", "work_orders": "Service", "service_resources": "Service", "resource_absences": "Service",
}

// navGroup is the sidebar section of an object.
func navGroup(key string) string {
	if g, ok := navGroups[key]; ok {
		return g
	}
	return "CRM"
}

// IsRecordGroup reports whether a sidebar section lists record objects (an app's menu
// may hide those; settings and analytics always stay).
func IsRecordGroup(g string) bool {
	switch g {
	case "CRM", "App", "Sales", "Products", "Sales operations", "Service", "Purchasing", "Finance":
		return true
	}
	return false
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
