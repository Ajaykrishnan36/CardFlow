package access

type NavItem struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Path      string `json:"path"`
	Icon      string `json:"icon"`
	Group     string `json:"group,omitempty"`
	Available bool   `json:"available"` // false = module not built yet (shown as "Soon")
}

type Capabilities struct {
	Audience   string    `json:"audience"` // owner | workspace | portal
	Actions    []string  `json:"actions"`
	Navigation []NavItem `json:"navigation"`
}

// OwnerCapabilities is the Platform Owner's action list and sidebar (PRD §10.1).
func OwnerCapabilities() Capabilities {
	c := Capabilities{
		Audience: "owner",
		Actions: []string{
			"platform.dashboard.view", "platform.product.manage", "platform.workspace.manage",
			"platform.plan.manage", "owner.access", "audit.view",
		},
		Navigation: []NavItem{
			{Key: "overview", Label: "Overview", Path: "/crm/owner/dashboard", Icon: "layout-dashboard", Available: true},
			{Key: "leads", Label: "Leads", Path: "/crm/owner/leads", Icon: "user-plus", Group: "Platform CRM", Available: true},
			// What the website brought in (D-181): the same leads, filtered.
			{Key: "call_requests", Label: "Call requests", Path: "/crm/owner/leads?requestType=call", Icon: "phone", Group: "Platform CRM", Available: true},
			{Key: "website_enquiries", Label: "Website enquiries", Path: "/crm/owner/leads?requestType=enquiry", Icon: "message-square", Group: "Platform CRM", Available: true},
			// Contacts = the people (one mobile number each) who created a CRM; Apps = their
			// businesses (D-161). The Platform CRM contacts list is reached from here, not the menu.
			{Key: "customers", Label: "Contacts", Path: "/crm/owner/customers", Icon: "users", Group: "Administration", Available: true},
			{Key: "billing", Label: "Billing", Path: "/crm/owner/billing", Icon: "credit-card", Group: "Administration", Available: true},
			{Key: "workspaces", Label: "Apps", Path: "/crm/owner/workspaces", Icon: "layout-grid", Group: "Administration", Available: true},
			{Key: "objects", Label: "Objects", Path: "/crm/owner/objects", Icon: "box", Group: "Administration", Available: true},
			{Key: "users", Label: "Users & access", Path: "/crm/owner/users", Icon: "shield-check", Group: "Administration", Available: true},
			{Key: "integrations", Label: "Integrations", Path: "/crm/owner/integrations", Icon: "plug", Group: "Settings", Available: true},
			{Key: "audit", Label: "Audit log", Path: "/crm/owner/audit", Icon: "scroll-text", Group: "Settings", Available: true},
			{Key: "settings", Label: "Profile & security", Path: "/crm/me", Icon: "settings", Group: "Settings", Available: true},
		},
	}
	return c
}
