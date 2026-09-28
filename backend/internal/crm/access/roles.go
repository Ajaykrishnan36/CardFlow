package access

// Rules is the permission JSON stored in roles.base_rules and permission_sets.rules
// (PRD §7). Objects are singular keys (lead, account, contact); a row scope is "own"
// or "workspace". Grants are additive: a user's access is the union of their role and
// every permission set assigned to them (DECISIONS D-30).
type Rules struct {
	Objects      map[string][]string          `json:"objects"`
	Rows         map[string]map[string]string `json:"rows"`
	Capabilities []string                     `json:"capabilities"`
}

type SystemRole struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Rank        int    `json:"rank"`
	Description string `json:"description"`
	Rules       Rules  `json:"rules"`
}

// ---- catalog ----

type CatalogObject struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Module  string   `json:"module"`
	Actions []string `json:"actions"`
}

type CatalogEntry struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

var Objects = []CatalogObject{
	{Key: "lead", Label: "Leads", Module: "leads", Actions: []string{"read", "create", "update", "delete", "convert", "export"}},
	{Key: "account", Label: "Accounts", Module: "accounts", Actions: []string{"read", "create", "update", "delete", "export"}},
	{Key: "contact", Label: "Contacts", Module: "contacts", Actions: []string{"read", "create", "update", "delete", "export"}},
	{Key: "ticket", Label: "Support tickets", Module: "tickets", Actions: []string{"read", "update"}},
}

var Actions = []CatalogEntry{
	{Key: "read", Label: "View"}, {Key: "create", Label: "Create"}, {Key: "update", Label: "Edit"},
	{Key: "delete", Label: "Delete"}, {Key: "convert", Label: "Convert"}, {Key: "export", Label: "Export"},
}

// actionLabelFor names an action for one object (tickets: update = reply).
func actionLabelFor(obj, a string) string {
	if obj == "ticket" && a == "update" {
		return "Reply"
	}
	return actionLabel(a)
}

const (
	CapDashboard     = "dashboard.view"
	CapMetadata      = "metadata.manage"
	CapAccessManage  = "access.manage"
	CapMembersManage = "members.manage"
)

var CapabilityCatalog = []CatalogEntry{
	{Key: CapDashboard, Label: "View dashboard", Description: "See the workspace dashboard with totals and recent records."},
	{Key: CapMetadata, Label: "Customize page layouts & fields", Description: "Arrange record pages and add custom fields."},
	{Key: CapAccessManage, Label: "Manage roles & permission sets",
		Description: "Create and edit roles and permission sets in this workspace — never beyond their own access."},
	{Key: CapMembersManage, Label: "Manage users",
		Description: "Invite users and change their access in this workspace — never beyond their own access."},
}

// RankFor ranks a custom role: administrative capabilities make it privileged (MFA
// outside local, like Admin); otherwise it sits with Staff.
func RankFor(r Rules) int {
	for _, c := range r.Capabilities {
		if c == CapAccessManage || c == CapMembersManage {
			return RankAdmin
		}
	}
	return 60
}

func objectAllowed(obj string) (CatalogObject, bool) {
	for _, o := range Objects {
		if o.Key == obj {
			return o, true
		}
	}
	return CatalogObject{}, false
}

func grantAll(actions func(o CatalogObject) []string, scope string) (map[string][]string, map[string]map[string]string) {
	objs := map[string][]string{}
	rows := map[string]map[string]string{}
	for _, o := range Objects {
		a := actions(o)
		if len(a) == 0 {
			continue
		}
		objs[o.Key] = a
		rows[o.Key] = map[string]string{"scope": scope}
	}
	return objs, rows
}

func without(list []string, drop ...string) []string {
	out := []string{}
	for _, v := range list {
		skip := false
		for _, d := range drop {
			if v == d {
				skip = true
			}
		}
		if !skip {
			out = append(out, v)
		}
	}
	return out
}

func only(list []string, keep ...string) []string {
	out := []string{}
	for _, v := range list {
		for _, k := range keep {
			if v == k {
				out = append(out, v)
			}
		}
	}
	return out
}

// SystemRoles are the same in every workspace (PRD §2.1). Their rules are defined here,
// not read from roles.base_rules, so improving a role reaches existing workspaces.
func SystemRoles() []SystemRole {
	saObj, saRows := grantAll(func(o CatalogObject) []string { return o.Actions }, "workspace")
	adObj, adRows := grantAll(func(o CatalogObject) []string { return without(o.Actions, "export") }, "workspace")
	stObj, stRows := grantAll(func(o CatalogObject) []string { return only(o.Actions, "read", "create", "update", "convert") }, "own")
	return []SystemRole{
		{Key: "SUPER_ADMIN", Name: "Super Admin", Rank: RankSuperAdmin,
			Description: "Full access to every record and setting in the workspace.",
			Rules:       Rules{Objects: saObj, Rows: saRows, Capabilities: []string{CapDashboard, CapMetadata}}},
		{Key: "ADMIN", Name: "Admin", Rank: RankAdmin,
			Description: "Works every record in the workspace; can't export or customize.",
			Rules:       Rules{Objects: adObj, Rows: adRows, Capabilities: []string{CapDashboard}}},
		{Key: "STAFF", Name: "Staff", Rank: RankStaff,
			Description: "Views, creates and edits their own records.",
			Rules:       Rules{Objects: stObj, Rows: stRows, Capabilities: []string{CapDashboard}}},
		{Key: "END_USER", Name: "End user", Rank: RankEndUser,
			Description: "No CRM access by itself — grant exactly what's needed with permission sets.",
			Rules:       Rules{Objects: map[string][]string{}, Rows: map[string]map[string]string{}, Capabilities: []string{}}},
	}
}

func FindSystemRole(key string) (SystemRole, bool) {
	for _, r := range SystemRoles() {
		if r.Key == key {
			return r, true
		}
	}
	return SystemRole{}, false
}

// Normalize drops unknown objects/actions/capabilities, adds the implied "read", and
// defaults scopes to "own" (least privilege).
func Normalize(in Rules) Rules {
	out := Rules{Objects: map[string][]string{}, Rows: map[string]map[string]string{}, Capabilities: []string{}}
	for obj, actions := range in.Objects {
		o, ok := objectAllowed(obj)
		if !ok {
			continue
		}
		set := map[string]bool{}
		for _, a := range actions {
			for _, allowed := range o.Actions {
				if a == allowed {
					set[a] = true
				}
			}
		}
		if len(set) == 0 {
			continue
		}
		set["read"] = true
		list := []string{}
		for _, a := range o.Actions { // catalog order
			if set[a] {
				list = append(list, a)
			}
		}
		out.Objects[obj] = list
		scope := "own"
		if in.Rows != nil && in.Rows[obj]["scope"] == "workspace" {
			scope = "workspace"
		}
		out.Rows[obj] = map[string]string{"scope": scope}
	}
	seen := map[string]bool{}
	for _, c := range in.Capabilities {
		for _, known := range CapabilityCatalog {
			if c == known.Key && !seen[c] {
				seen[c] = true
				out.Capabilities = append(out.Capabilities, c)
			}
		}
	}
	return out
}

func actionLabel(a string) string {
	for _, x := range Actions {
		if x.Key == a {
			return x.Label
		}
	}
	return a
}

func capabilityLabel(c string) string {
	for _, x := range CapabilityCatalog {
		if x.Key == c {
			return x.Label
		}
	}
	return c
}

// AsRules turns effective access back into rules: the most its holder can grant.
func (e *Effective) AsRules() Rules {
	out := Rules{Objects: map[string][]string{}, Rows: map[string]map[string]string{}, Capabilities: []string{}}
	if e == nil {
		return out
	}
	for key, o := range e.Objects {
		if !o.ModuleEnabled || len(o.Actions) == 0 {
			continue
		}
		out.Objects[key] = append([]string{}, o.Actions...)
		out.Rows[key] = map[string]string{"scope": o.Scope}
	}
	for _, c := range e.Capabilities {
		out.Capabilities = append(out.Capabilities, c.Key)
	}
	return out
}

// Exceeds lists every grant in r that limit doesn't hold itself (PRD PERM-03: a
// delegated admin can never grant more than they have). A nil limit is the owner.
func Exceeds(r Rules, limit *Effective) []string {
	if limit == nil {
		return nil
	}
	r = Normalize(r)
	var out []string
	for _, o := range Objects {
		acts := r.Objects[o.Key]
		for _, a := range acts {
			if !limit.Can(o.Key, a) {
				out = append(out, o.Label+": "+actionLabelFor(o.Key, a))
			}
		}
		if len(acts) > 0 && r.Rows[o.Key]["scope"] == "workspace" && limit.Can(o.Key, "read") && limit.Objects[o.Key].Scope != "workspace" {
			out = append(out, o.Label+": all records")
		}
	}
	for _, c := range r.Capabilities {
		if !limit.HasCapability(c) {
			out = append(out, capabilityLabel(c))
		}
	}
	return out
}

// FullAccess is the platform owner's access inside any workspace (owner "enter workspace").
func FullAccess() *Effective {
	sa, _ := FindSystemRole("SUPER_ADMIN")
	rules := sa.Rules
	rules.Capabilities = []string{CapDashboard, CapMetadata, CapAccessManage, CapMembersManage}
	modules := map[string]bool{}
	for _, o := range Objects {
		modules[o.Module] = true
	}
	e := Combine([]grantSource{{label: "Platform owner", rules: rules}}, modules, nil)
	e.RoleKey, e.RoleName = "PLATFORM_OWNER", "Platform owner"
	return e
}
