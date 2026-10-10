package access

import (
	"encoding/json"
	"regexp"
	"sync"

	"github.com/google/uuid"
)

// Rules is the permission JSON stored in roles.base_rules and permission_sets.rules
// (PRD §7). Objects are singular keys (lead, account, contact); a row scope is "own"
// or "workspace". Grants are additive: a user's access is the union of their role and
// every permission set assigned to them (DECISIONS D-30).
type Rules struct {
	Objects      map[string][]string          `json:"objects"`
	Rows         map[string]map[string]string `json:"rows"`
	Capabilities []string                     `json:"capabilities"`
	// Fields restricts single fields of a granted object (D-46): field key → "read"
	// (visible, not editable) or "hidden" (left out of pages and API responses). Fields
	// not listed are editable. Restrictions apply only to objects this rule set grants.
	Fields map[string]map[string]string `json:"fields,omitempty"`
}

// Field access levels, lowest first.
const (
	FieldHidden = "hidden"
	FieldRead   = "read"
	FieldEdit   = "edit"
)

var fieldRank = map[string]int{FieldHidden: 0, FieldRead: 1, FieldEdit: 2}

func FieldRank(level string) int {
	if r, ok := fieldRank[level]; ok {
		return r
	}
	return 2
}

var fieldKeyRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,60}$`)

// fieldLevel is the level a rule set gives one field of an object it grants.
func (r Rules) fieldLevel(obj, field string) string {
	if l := r.Fields[obj][field]; l == FieldHidden || l == FieldRead {
		return l
	}
	return FieldEdit
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
	// App objects live in a connected app (Business Card Snap), not in the CRM's own
	// tables; they appear only in workspaces with that app attached.
	App bool `json:"app,omitempty"`
	// Custom marks objects defined as data (standard objects behind modules and the
	// owner's own objects, D-45); Route is their URL segment and Icon their nav icon.
	Custom bool   `json:"custom,omitempty"`
	Route  string `json:"route,omitempty"`
	Icon   string `json:"icon,omitempty"`
	// WorkspaceID: an object a product created for itself (D-79); nil = platform-wide.
	WorkspaceID *uuid.UUID `json:"-"`
}

// CatalogObjectsFor is the catalog one workspace may see: platform-wide objects and its own.
func CatalogObjectsFor(ws uuid.UUID) []CatalogObject {
	out := []CatalogObject{}
	for _, o := range CatalogObjects() {
		if o.WorkspaceID == nil || *o.WorkspaceID == ws {
			out = append(out, o)
		}
	}
	return out
}

type CatalogEntry struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

var builtinObjects = []CatalogObject{
	{Key: "lead", Label: "Leads", Module: "leads", Actions: RecordActions("convert")},
	{Key: "account", Label: "Accounts", Module: "accounts", Actions: RecordActions()},
	{Key: "contact", Label: "Contacts", Module: "contacts", Actions: RecordActions()},
	{Key: "ticket", Label: "Support tickets", Module: "tickets", Actions: []string{"read", "update"}},
	// update = edit profile, grant or revoke premium access, change app role / status.
	{Key: "app_user", Label: "App users", Module: "app_users", Actions: []string{"read", "update", "delete"}, App: true},
	// update = edit the listing, its verification badge and search visibility.
	{Key: "app_business", Label: "Business listings", Module: "directory", Actions: []string{"read", "update", "delete"}, App: true},
}

// RecordActions are the actions on a CRM record object (plus any extra, e.g. "convert").
// destroy = delete permanently from the recycle bin; import/export = CSV.
func RecordActions(extra ...string) []string {
	out := []string{"read", "create", "update", "delete"}
	out = append(out, extra...)
	return append(out, "import", "export", "destroy")
}

var (
	customMu      sync.RWMutex
	customObjects []CatalogObject
)

// SetCustomObjects replaces the objects defined as data (the record engine calls it
// whenever object definitions change).
func SetCustomObjects(list []CatalogObject) {
	customMu.Lock()
	customObjects = append([]CatalogObject{}, list...)
	customMu.Unlock()
}

// CatalogObjects is every permission object: built-in, connected-app and custom.
func CatalogObjects() []CatalogObject {
	customMu.RLock()
	defer customMu.RUnlock()
	return append(append([]CatalogObject{}, builtinObjects...), customObjects...)
}

var Actions = []CatalogEntry{
	{Key: "read", Label: "View"}, {Key: "create", Label: "Create"}, {Key: "update", Label: "Edit"},
	{Key: "delete", Label: "Delete"}, {Key: "convert", Label: "Convert"}, {Key: "import", Label: "Import"}, {Key: "export", Label: "Export"},
	{Key: "destroy", Label: "Delete permanently"},
}

// actionLabelFor names an action for one object (tickets: update = reply).
func actionLabelFor(obj, a string) string {
	if obj == "ticket" && a == "update" {
		return "Reply"
	}
	if obj == "app_user" && a == "update" {
		return "Edit & grant access"
	}
	return actionLabel(a)
}

const (
	CapDashboard     = "dashboard.view"
	CapMetadata      = "metadata.manage"
	CapAccessManage  = "access.manage"
	CapMembersManage = "members.manage"
	CapWorkflows     = "workflows.manage"
	CapDeveloper     = "developer.manage"
	CapEmailSend     = "email.send"
	CapCampaigns     = "campaigns.manage"
	// CapForecast: set sales targets and approve or override the team's forecasts (D-112).
	CapForecast = "forecast.manage"
	// CapPricing: price book entries, bundles, pricing and discount rules, exchange rates (D-118…D-120).
	CapPricing = "pricing.manage"
	// CapApprovals: set approval thresholds; decide any approval request (D-121).
	CapApprovals = "approvals.manage"
	// CapTerritories: who and which accounts belong to a territory (D-124).
	CapTerritories = "territory.manage"
	// CapRecordTeams: add people to the team of a record they don't own (D-123).
	CapRecordTeams = "record_teams.manage"
	// CapScheduling: book and move appointments for any resource (D-127).
	CapScheduling = "scheduling.manage"
)

var CapabilityCatalog = []CatalogEntry{
	{Key: CapDashboard, Label: "View dashboard", Description: "See the workspace dashboard with totals and recent records."},
	{Key: CapMetadata, Label: "Customize page layouts & fields", Description: "Arrange record pages and add custom fields."},
	{Key: CapAccessManage, Label: "Manage roles & permission sets",
		Description: "Create and edit roles and permission sets in this workspace — never beyond their own access."},
	{Key: CapWorkflows, Label: "Manage workflows", Description: "Build, turn on and run automations (they act with full access to the records they touch)."},
	{Key: CapDeveloper, Label: "Manage API keys & webhooks", Description: "Create API keys and webhooks for this workspace (needs API access in the product's setup)."},
	{Key: CapEmailSend, Label: "Send email", Description: "Send emails from records, from a connected mailbox or the CRM's address."},
	{Key: CapCampaigns, Label: "Manage email campaigns", Description: "Send one email to many contacts at once."},
	{Key: CapForecast, Label: "Manage forecasts", Description: "Set sales targets for people and teams, and approve or adjust submitted forecasts."},
	{Key: CapPricing, Label: "Manage pricing", Description: "Edit price book entries, bundles, pricing and discount rules, and exchange rates; override a price on a quote."},
	{Key: CapApprovals, Label: "Manage approvals", Description: "Set the limits above which discounts, refunds, credit notes and write-offs need approval, and decide any request."},
	{Key: CapTerritories, Label: "Manage territories", Description: "Assign accounts, people and teams to sales territories."},
	{Key: CapRecordTeams, Label: "Manage record teams", Description: "Add or remove people on the team of any account, deal, case or contract."},
	{Key: CapScheduling, Label: "Manage scheduling", Description: "Book, move and cancel appointments for any service resource."},
	{Key: CapMembersManage, Label: "Manage users",
		Description: "Invite users and change their access in this workspace — never beyond their own access."},
}

// RankFor ranks a custom role: administrative capabilities make it privileged (MFA
// outside local, like Admin); otherwise it sits with Staff.
func RankFor(r Rules) int {
	for _, c := range r.Capabilities {
		switch c {
		case CapAccessManage, CapMembersManage, CapWorkflows, CapDeveloper:
			return RankAdmin
		}
	}
	return 60
}

func objectAllowed(obj string) (CatalogObject, bool) {
	for _, o := range CatalogObjects() {
		if o.Key == obj {
			return o, true
		}
	}
	return CatalogObject{}, false
}

func grantAll(actions func(o CatalogObject) []string, scope string) (map[string][]string, map[string]map[string]string) {
	objs := map[string][]string{}
	rows := map[string]map[string]string{}
	for _, o := range CatalogObjects() {
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
	adObj, adRows := grantAll(func(o CatalogObject) []string { return without(o.Actions, "export", "destroy") }, "workspace")
	stObj, stRows := grantAll(func(o CatalogObject) []string {
		if o.App {
			return only(o.Actions, "read") // staff can look up app data but not change it
		}
		if o.Module == "finance" {
			return nil // money in and out is for admins unless a permission set says otherwise (D-96)
		}
		return only(o.Actions, "read", "create", "update", "convert")
	}, "own")
	return []SystemRole{
		{Key: "SUPER_ADMIN", Name: "Super Admin", Rank: RankSuperAdmin,
			Description: "Full access to every record, setting, user and role in the workspace — always.",
			Rules:       Rules{Objects: saObj, Rows: saRows, Capabilities: AllCapabilities()}},
		{Key: "ADMIN", Name: "Admin", Rank: RankAdmin,
			Description: "Works every record in the workspace and adds staff; can't export, delete permanently or customize.",
			Rules:       Rules{Objects: adObj, Rows: adRows, Capabilities: []string{CapDashboard, CapEmailSend, CapMembersManage}}},
		{Key: "STAFF", Name: "Staff", Rank: RankStaff,
			Description: "Views, creates and edits their own records.",
			Rules:       Rules{Objects: stObj, Rows: stRows, Capabilities: []string{CapDashboard, CapEmailSend}}},
		{Key: "END_USER", Name: "End user", Rank: RankEndUser,
			Description: "No CRM access by itself — grant exactly what's needed with permission sets.",
			Rules:       Rules{Objects: map[string][]string{}, Rows: map[string]map[string]string{}, Capabilities: []string{}}},
	}
}

// AllCapabilities lists every capability key ("give all access").
func AllCapabilities() []string {
	out := make([]string, len(CapabilityCatalog))
	for i, c := range CapabilityCatalog {
		out[i] = c.Key
	}
	return out
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
		for field, level := range in.Fields[obj] {
			if (level == FieldHidden || level == FieldRead) && fieldKeyRe.MatchString(field) {
				if out.Fields == nil {
					out.Fields = map[string]map[string]string{}
				}
				if out.Fields[obj] == nil {
					out.Fields[obj] = map[string]string{}
				}
				out.Fields[obj][field] = level
			}
		}
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
		if len(o.Fields) > 0 {
			if out.Fields == nil {
				out.Fields = map[string]map[string]string{}
			}
			out.Fields[key] = map[string]string{}
			for f, l := range o.Fields {
				out.Fields[key][f] = l
			}
		}
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
	for _, o := range CatalogObjects() {
		acts := r.Objects[o.Key]
		for _, a := range acts {
			if !limit.Can(o.Key, a) {
				out = append(out, o.Label+": "+actionLabelFor(o.Key, a))
			}
		}
		if len(acts) > 0 && r.Rows[o.Key]["scope"] == "workspace" && limit.Can(o.Key, "read") && limit.Objects[o.Key].Scope != "workspace" {
			out = append(out, o.Label+": all records")
		}
		// Field access: a delegated admin can't open up a field they can't see or edit themselves.
		if len(acts) > 0 {
			for field, lim := range limit.Objects[o.Key].Fields {
				if FieldRank(r.fieldLevel(o.Key, field)) > FieldRank(lim) {
					what := "edit"
					if lim == FieldHidden {
						what = "see"
					}
					out = append(out, o.Label+": "+what+" the "+field+" field")
				}
			}
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
	rules.Capabilities = AllCapabilities()
	modules := map[string]bool{}
	for _, o := range CatalogObjects() {
		modules[o.Module] = true
	}
	e := Combine([]grantSource{{label: "Platform owner", rules: rules}}, modules, nil)
	e.RoleKey, e.RoleName = "PLATFORM_OWNER", "Platform owner"
	return e
}

// ParseRules reads stored rules JSON and normalises it (unknown objects/actions dropped).
func ParseRules(raw []byte) Rules {
	var r Rules
	_ = json.Unmarshal(raw, &r)
	return Normalize(r)
}
