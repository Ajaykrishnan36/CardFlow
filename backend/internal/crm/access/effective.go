package access

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/google/uuid"
)

// Effective is what a membership can actually do (PRD §7 policy order: active workspace
// → active membership → product enabled and assigned → role + permission-set grants).

type EffectiveObject struct {
	Actions       []string `json:"actions"`
	Scope         string   `json:"scope"`
	Sources       []string `json:"sources"`
	ModuleEnabled bool     `json:"moduleEnabled"`
}

type CapabilityGrant struct {
	Key     string   `json:"key"`
	Sources []string `json:"sources"`
}

type ProductRef struct {
	ID   uuid.UUID `json:"id"`
	Key  string    `json:"key"`
	Name string    `json:"name"`
}

type Effective struct {
	Objects      map[string]EffectiveObject `json:"objects"`
	Capabilities []CapabilityGrant          `json:"capabilities"`
	Products     []ProductRef               `json:"products"`
	Modules      []string                   `json:"modules"`

	// Filled by ForMembership for callers; not serialised.
	WorkspaceID  uuid.UUID `json:"-"`
	MembershipID uuid.UUID `json:"-"`
	RoleKey      string    `json:"-"`
	RoleName     string    `json:"-"`
}

// Can reports whether action on obj is allowed (module on + granted).
func (e *Effective) Can(obj, action string) bool {
	if e == nil {
		return false
	}
	o, ok := e.Objects[obj]
	if !ok || !o.ModuleEnabled {
		return false
	}
	for _, a := range o.Actions {
		if a == action {
			return true
		}
	}
	return false
}

// OwnOnly reports whether obj is limited to records the user owns.
func (e *Effective) OwnOnly(obj string) bool {
	return e == nil || e.Objects[obj].Scope != "workspace"
}

func (e *Effective) HasCapability(key string) bool {
	if e == nil {
		return false
	}
	for _, c := range e.Capabilities {
		if c.Key == key {
			return true
		}
	}
	return false
}

type grantSource struct {
	label string
	rules Rules
}

// Combine merges grants into an Effective limited to the enabled modules.
func Combine(sources []grantSource, modules map[string]bool, products []ProductRef) *Effective {
	e := &Effective{Objects: map[string]EffectiveObject{}, Capabilities: []CapabilityGrant{}, Products: products, Modules: []string{}}
	if e.Products == nil {
		e.Products = []ProductRef{}
	}
	for m := range modules {
		e.Modules = append(e.Modules, m)
	}
	sort.Strings(e.Modules)

	caps := map[string][]string{}
	for _, o := range CatalogObjects() {
		eo := EffectiveObject{Actions: []string{}, Scope: "own", Sources: []string{}, ModuleEnabled: modules[o.Module]}
		set := map[string]bool{}
		for _, s := range sources {
			r := Normalize(s.rules)
			acts, ok := r.Objects[o.Key]
			if !ok {
				continue
			}
			for _, a := range acts {
				set[a] = true
			}
			if r.Rows[o.Key]["scope"] == "workspace" {
				eo.Scope = "workspace"
			}
			eo.Sources = append(eo.Sources, s.label)
		}
		for _, a := range o.Actions {
			if set[a] {
				eo.Actions = append(eo.Actions, a)
			}
		}
		e.Objects[o.Key] = eo
	}
	for _, s := range sources {
		for _, c := range Normalize(s.rules).Capabilities {
			caps[c] = append(caps[c], s.label)
		}
	}
	for _, c := range CapabilityCatalog {
		if src, ok := caps[c.Key]; ok {
			e.Capabilities = append(e.Capabilities, CapabilityGrant{Key: c.Key, Sources: src})
		}
	}
	return e
}

// ForMembership computes the effective access of one membership. Inactive memberships
// or workspaces yield no grants.
func ForMembership(ctx context.Context, q Querier, membershipID uuid.UUID) (*Effective, error) {
	var wsID uuid.UUID
	var isPlatform, active bool
	if err := q.QueryRow(ctx, `
		SELECT w.id, w.is_platform, (m.status = 'active' AND w.status = 'active')
		FROM crm.memberships m JOIN crm.workspaces w ON w.id = m.workspace_id WHERE m.id = $1`, membershipID).
		Scan(&wsID, &isPlatform, &active); err != nil {
		return nil, err
	}

	var sources []grantSource
	productIDs := map[uuid.UUID]bool{}
	var roleKey, roleName string
	roleRank := -1
	rows, err := q.Query(ctx, `
		SELECT r.key, r.name, r.rank, r.is_system AND NOT r.customized, r.base_rules, ra.product_ids
		FROM crm.role_assignments ra JOIN crm.roles r ON r.id = ra.role_id
		WHERE ra.membership_id = $1 AND (ra.expires_at IS NULL OR ra.expires_at > now())`, membershipID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var key, name string
		var rank int
		var system bool
		var raw []byte
		var pids []uuid.UUID
		if err := rows.Scan(&key, &name, &rank, &system, &raw, &pids); err != nil {
			rows.Close()
			return nil, err
		}
		rules := Rules{}
		// Untouched built-in roles follow the code defaults; edited and custom roles use their stored rules.
		// Super Admin is the workspace's owner role: it always has everything (D-45), whatever was stored.
		if sr, ok := FindSystemRole(key); ok && (system || key == "SUPER_ADMIN") {
			rules = sr.Rules
		} else {
			_ = json.Unmarshal(raw, &rules)
		}
		sources = append(sources, grantSource{label: "Role: " + name, rules: rules})
		for _, p := range pids {
			productIDs[p] = true
		}
		if rank > roleRank {
			roleRank, roleKey, roleName = rank, key, name
		}
	}
	rows.Close()

	rows, err = q.Query(ctx, `
		SELECT ps.name, ps.rules FROM crm.membership_permission_sets mps
		JOIN crm.permission_sets ps ON ps.id = mps.permission_set_id
		WHERE mps.membership_id = $1 AND (mps.expires_at IS NULL OR mps.expires_at > now())
		ORDER BY ps.name`, membershipID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name string
		var raw []byte
		if err := rows.Scan(&name, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		var r Rules
		_ = json.Unmarshal(raw, &r)
		sources = append(sources, grantSource{label: "Permission set: " + name, rules: r})
	}
	rows.Close()

	only := productIDs
	if roleKey == "SUPER_ADMIN" {
		only = nil // the workspace owner gets every product, including ones added later
	}
	modules, products, err := workspaceModules(ctx, q, wsID, isPlatform, only)
	if err != nil {
		return nil, err
	}

	if !active {
		sources, modules = nil, map[string]bool{}
	}
	e := Combine(sources, modules, products)
	e.WorkspaceID, e.MembershipID, e.RoleKey, e.RoleName = wsID, membershipID, roleKey, roleName
	return e, nil
}

// workspaceModules is what a workspace's products switch on. only limits it to some of
// the products (a member's assignment); nil means every product of the workspace.
func workspaceModules(ctx context.Context, q Querier, wsID uuid.UUID, isPlatform bool, only map[uuid.UUID]bool) (map[string]bool, []ProductRef, error) {
	modules := map[string]bool{}
	products := []ProductRef{}
	if isPlatform {
		// The owner's own workspace has no products; its members work the Platform CRM.
		for _, o := range CatalogObjects() {
			modules[o.Module] = true
		}
		return modules, products, nil
	}
	rows, err := q.Query(ctx, `
		SELECT p.id, p.key, p.name, v.config
		FROM crm.workspace_products wp
		JOIN crm.products p ON p.id = wp.product_id AND p.status <> 'archived'
		JOIN crm.product_versions v ON v.product_id = wp.product_id AND v.version = wp.config_version
		WHERE wp.workspace_id = $1 AND wp.status = 'active'
		ORDER BY p.name`, wsID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p ProductRef
		var raw []byte
		if err := rows.Scan(&p.ID, &p.Key, &p.Name, &raw); err != nil {
			return nil, nil, err
		}
		if only != nil && !only[p.ID] {
			continue
		}
		products = append(products, p)
		var cfg struct {
			Modules []string `json:"modules"`
		}
		_ = json.Unmarshal(raw, &cfg)
		for _, m := range cfg.Modules {
			modules[m] = true
		}
	}
	return modules, products, rows.Err()
}

// WorkspaceModules is the set of modules switched on in a workspace (every product).
func WorkspaceModules(ctx context.Context, q Querier, wsID uuid.UUID) (map[string]bool, error) {
	var isPlatform bool
	if err := q.QueryRow(ctx, `SELECT is_platform FROM crm.workspaces WHERE id = $1`, wsID).Scan(&isPlatform); err != nil {
		return nil, err
	}
	m, _, err := workspaceModules(ctx, q, wsID, isPlatform, nil)
	return m, err
}

// FullAccessIn is the platform owner's access inside one workspace: everything, for the
// modules that workspace's products switch on (so the owner sees what its users see).
func FullAccessIn(ctx context.Context, q Querier, wsID uuid.UUID) (*Effective, error) {
	var isPlatform bool
	if err := q.QueryRow(ctx, `SELECT is_platform FROM crm.workspaces WHERE id = $1`, wsID).Scan(&isPlatform); err != nil {
		return nil, err
	}
	modules, products, err := workspaceModules(ctx, q, wsID, isPlatform, nil)
	if err != nil {
		return nil, err
	}
	sa, _ := FindSystemRole("SUPER_ADMIN")
	e := Combine([]grantSource{{label: "Platform owner", rules: sa.Rules}}, modules, products)
	e.WorkspaceID, e.RoleKey, e.RoleName = wsID, "SUPER_ADMIN", "Platform owner"
	return e, nil
}
