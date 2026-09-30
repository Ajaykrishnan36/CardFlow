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
			{Key: "accounts", Label: "Accounts", Path: "/crm/owner/accounts", Icon: "briefcase", Group: "Platform CRM", Available: true},
			{Key: "contacts", Label: "Contacts", Path: "/crm/owner/contacts", Icon: "contact", Group: "Platform CRM", Available: true},
			// Each app is a product (shown as Products; D-49, D-53); its setup lives inside it.
			{Key: "workspaces", Label: "Products", Path: "/crm/owner/workspaces", Icon: "building-2", Group: "Administration", Available: true},
			{Key: "apps", Label: "Apps", Path: "/crm/owner/apps", Icon: "layout-grid", Group: "Administration", Available: true},
			{Key: "objects", Label: "Objects", Path: "/crm/owner/objects", Icon: "box", Group: "Administration", Available: true},
			{Key: "users", Label: "Users & access", Path: "/crm/owner/users", Icon: "shield-check", Group: "Administration", Available: true},
			{Key: "integrations", Label: "Integrations", Path: "/crm/owner/integrations", Icon: "plug", Group: "Settings", Available: true},
			{Key: "audit", Label: "Audit log", Path: "/crm/owner/audit", Icon: "scroll-text", Group: "Settings", Available: true},
			{Key: "settings", Label: "Profile & security", Path: "/crm/me", Icon: "settings", Group: "Settings", Available: true},
		},
	}
	return c
}
