package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Roles and delegated administration (PRD §7 PERM-01..03). The same functions serve
// the owner (limit == nil: anything) and a workspace member holding access.manage /
// members.manage (limit == their own effective access: never more than they have).

// AdminScope is a delegated admin working in their own workspace.
type AdminScope struct {
	WS           uuid.UUID
	Code         string
	IsPlatform   bool
	MembershipID uuid.UUID
	Limit        *access.Effective
}

func exceedsErr(list []string) error {
	return shared.Forbidden("exceeds_your_access", "You can't grant more than you have yourself: "+strings.Join(list, ", ")+".")
}

func checkWithin(r access.Rules, limit *access.Effective) error {
	if list := access.Exceeds(r, limit); len(list) > 0 {
		return exceedsErr(list)
	}
	return nil
}

// ---- roles ----

type WorkspaceRole struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspaceId"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	IsSystem    bool      `json:"isSystem"`
	Customized  bool      `json:"customized"`
	Rank        int       `json:"rank"`
	// ParentRoleID is the role this one reports to (D-48); nil only for Super Admin.
	ParentRoleID *uuid.UUID `json:"parentRoleId,omitempty"`
	// Rules: roles grant nothing but Super Admin's full access — permission sets do.
	Rules         access.Rules `json:"rules"`
	AssignedCount int          `json:"assignedCount"`
	CreatedAt     time.Time    `json:"createdAt"`
	UpdatedAt     time.Time    `json:"updatedAt"`
}

const roleSelect = `
	SELECT r.id, r.workspace_id, r.key, r.name, COALESCE(r.description, ''), r.is_system, r.customized, r.rank, r.base_rules,
	       r.created_at, r.updated_at, r.parent_role_id,
	       (SELECT count(*) FROM crm.role_assignments ra JOIN crm.memberships m ON m.id = ra.membership_id
	         WHERE ra.role_id = r.id AND m.status <> 'revoked')
	FROM crm.roles r`

func scanRole(row pgx.Row) (WorkspaceRole, error) {
	var ro WorkspaceRole
	var raw []byte
	err := row.Scan(&ro.ID, &ro.WorkspaceID, &ro.Key, &ro.Name, &ro.Description, &ro.IsSystem, &ro.Customized, &ro.Rank, &raw,
		&ro.CreatedAt, &ro.UpdatedAt, &ro.ParentRoleID, &ro.AssignedCount)
	if err != nil {
		return ro, err
	}
	ro.Rules = access.Normalize(access.Rules{})
	ro.Customized = false
	if ro.Key == "SUPER_ADMIN" {
		sr, _ := access.FindSystemRole(ro.Key)
		ro.Rules = sr.Rules
	}
	if ro.IsSystem && ro.Description == "" {
		ro.Description = map[string]string{
			"SUPER_ADMIN": "Top of the hierarchy: full access to every record, setting, user and role.",
			"ADMIN":       "Sees the records of everyone below them.",
			"STAFF":       "Sees their own records and those of anyone below them.",
			"END_USER":    "Customers and app users; sees only their own records.",
		}[ro.Key]
	}
	return ro, nil
}

func (h *Handler) listRoles(ctx context.Context, ws uuid.UUID) ([]WorkspaceRole, error) {
	rows, err := h.store.Pool.Query(ctx, roleSelect+` WHERE r.workspace_id = $1 ORDER BY r.rank DESC, r.is_system DESC, r.name`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkspaceRole{}
	for rows.Next() {
		ro, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ro)
	}
	return out, rows.Err()
}

type roleInput struct {
	Name         *string       `json:"name"`
	Description  *string       `json:"description"`
	ParentRoleID *uuid.UUID    `json:"parentRoleId"`
	Rules        *access.Rules `json:"rules"` // ignored: roles no longer grant permissions (D-48)
}

// checkParent validates that parent can sit above role (same workspace, no loop).
func checkParent(ctx context.Context, tx pgx.Tx, ws, role, parent uuid.UUID) error {
	var ok bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.roles WHERE id = $1 AND workspace_id = $2)`, parent, ws).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return shared.Validation(map[string]string{"parentRoleId": "Pick a role from this workspace."})
	}
	if role == uuid.Nil {
		return nil
	}
	var loop bool
	if err := tx.QueryRow(ctx, `
		WITH RECURSIVE up AS (SELECT id, parent_role_id FROM crm.roles WHERE id = $1
		                      UNION SELECT r.id, r.parent_role_id FROM crm.roles r JOIN up ON r.id = up.parent_role_id)
		SELECT EXISTS (SELECT 1 FROM up WHERE id = $2)`, parent, role).Scan(&loop); err != nil {
		return err
	}
	if loop {
		return shared.Validation(map[string]string{"parentRoleId": "A role can't report to itself or to a role below it."})
	}
	return nil
}

func roleKeyFor(name string) string {
	var b strings.Builder
	under := false
	for _, r := range strings.ToUpper(name) {
		switch {
		case (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			under = false
		case !under && b.Len() > 0:
			b.WriteByte('_')
			under = true
		}
	}
	k := strings.Trim(b.String(), "_")
	if k == "" || (k[0] >= '0' && k[0] <= '9') {
		k = "ROLE_" + k
	}
	if len(k) > 40 {
		k = strings.TrimRight(k[:40], "_")
	}
	return k
}

func (h *Handler) createRole(ctx context.Context, r *http.Request, ws uuid.UUID, in roleInput, limit *access.Effective) (*WorkspaceRole, error) {
	name, desc := "", ""
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		desc = strings.TrimSpace(*in.Description)
	}
	fe := map[string]string{}
	if name == "" || len(name) > 60 {
		fe["name"] = "Enter a name (up to 60 characters)."
	}
	if len(desc) > 300 {
		fe["description"] = "Use at most 300 characters."
	}
	if len(fe) > 0 {
		return nil, shared.Validation(fe)
	}
	rules := access.Normalize(access.Rules{})
	raw, _ := json.Marshal(rules)
	var id uuid.UUID
	err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		parent := uuid.Nil
		if in.ParentRoleID != nil {
			parent = *in.ParentRoleID
		} else if err := tx.QueryRow(ctx, `SELECT id FROM crm.roles WHERE workspace_id = $1 AND key = 'SUPER_ADMIN'`, ws).Scan(&parent); err != nil {
			return err
		}
		if err := checkParent(ctx, tx, ws, uuid.Nil, parent); err != nil {
			return err
		}
		var dup bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.roles WHERE workspace_id = $1 AND lower(name) = lower($2))`, ws, name).Scan(&dup); err != nil {
			return err
		}
		if dup {
			return shared.Validation(map[string]string{"name": "A role with this name already exists in this workspace."})
		}
		base := roleKeyFor(name)
		key := base
		for i := 2; ; i++ {
			_, system := access.FindSystemRole(key)
			var taken bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.roles WHERE workspace_id = $1 AND key = $2)`, ws, key).Scan(&taken); err != nil {
				return err
			}
			if !system && !taken {
				break
			}
			key = base + "_" + strconv.Itoa(i)
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO crm.roles (workspace_id, key, name, description, is_system, rank, base_rules, created_by, parent_role_id)
			VALUES ($1, $2, $3, NULLIF($4, ''), false, $5, $6, $7, $8) RETURNING id`,
			ws, key, name, desc, access.RankFor(rules), raw, actorID(r), parent).Scan(&id); err != nil {
			return err
		}
		return shared.WriteAudit(ctx, tx, auditEvent(r, "role.created", "role", &id, &ws, nil, map[string]any{"key": key, "name": name, "parentRoleId": parent}))
	})
	if err != nil {
		return nil, err
	}
	ro, err := scanRole(h.store.Pool.QueryRow(ctx, roleSelect+` WHERE r.id = $1`, id))
	return &ro, err
}

// updateRole edits a role in ws (when ws != Nil the role must belong to it).
func (h *Handler) updateRole(ctx context.Context, r *http.Request, id, ws uuid.UUID, in roleInput, limit *access.Effective) (*WorkspaceRole, error) {
	err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		cur, err := scanRole(tx.QueryRow(ctx, roleSelect+` WHERE r.id = $1 FOR UPDATE OF r`, id))
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && ws != uuid.Nil && cur.WorkspaceID != ws) {
			return shared.NotFound("role_not_found")
		}
		if err != nil {
			return err
		}
		if in.ParentRoleID != nil {
			if cur.Key == "SUPER_ADMIN" {
				return shared.Validation(map[string]string{"parentRoleId": "Super Admin is always at the top of the hierarchy."})
			}
			if err := checkParent(ctx, tx, cur.WorkspaceID, id, *in.ParentRoleID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE crm.roles SET parent_role_id = $2, updated_at = now() WHERE id = $1`, id, *in.ParentRoleID); err != nil {
				return err
			}
		}
		in.Rules = nil // roles don't carry permissions any more (D-48)
		name, desc, rules := cur.Name, cur.Description, cur.Rules
		fe := map[string]string{}
		if in.Name != nil && strings.TrimSpace(*in.Name) != cur.Name {
			if cur.IsSystem {
				fe["name"] = "Built-in roles can't be renamed. Create a custom role instead."
			}
			name = strings.TrimSpace(*in.Name)
			if name == "" || len(name) > 60 {
				fe["name"] = "Enter a name (up to 60 characters)."
			}
		}
		if in.Description != nil && !cur.IsSystem {
			desc = strings.TrimSpace(*in.Description)
			if len(desc) > 300 {
				fe["description"] = "Use at most 300 characters."
			}
		}
		if len(fe) > 0 {
			return shared.Validation(fe)
		}
		if in.Rules != nil {
			rules = access.Normalize(*in.Rules)
			if err := checkWithin(rules, limit); err != nil {
				return err
			}
			// A delegated admin can't edit a role that already grants more than they hold.
			if limit != nil {
				if err := checkWithin(cur.Rules, limit); err != nil {
					return shared.Forbidden("exceeds_your_access", "This role grants more than you have, so you can't change it.")
				}
			}
		}
		raw, _ := json.Marshal(rules)
		rank := cur.Rank
		if !cur.IsSystem {
			rank = access.RankFor(rules)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE crm.roles SET name = $2, description = NULLIF($3, ''), base_rules = $4, rank = $5,
			       customized = (is_system AND $6), updated_at = now()
			WHERE id = $1`, id, name, desc, raw, rank, cur.Customized || in.Rules != nil); err != nil {
			if isUniqueViolation(err, "") {
				return shared.Validation(map[string]string{"name": "A role with this name already exists in this workspace."})
			}
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE crm.memberships SET auth_version = auth_version + 1
			WHERE id IN (SELECT membership_id FROM crm.role_assignments WHERE role_id = $1)`, id); err != nil {
			return err
		}
		wsID := cur.WorkspaceID
		return shared.WriteAudit(ctx, tx, auditEvent(r, "role.updated", "role", &id, &wsID,
			map[string]any{"name": cur.Name, "rules": cur.Rules}, map[string]any{"name": name, "rules": rules}))
	})
	if err != nil {
		return nil, err
	}
	ro, err := scanRole(h.store.Pool.QueryRow(ctx, roleSelect+` WHERE r.id = $1`, id))
	return &ro, err
}

func (h *Handler) deleteRole(ctx context.Context, r *http.Request, id, ws uuid.UUID, limit *access.Effective) error {
	return h.store.WithTx(ctx, func(tx pgx.Tx) error {
		cur, err := scanRole(tx.QueryRow(ctx, roleSelect+` WHERE r.id = $1 FOR UPDATE OF r`, id))
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && ws != uuid.Nil && cur.WorkspaceID != ws) {
			return shared.NotFound("role_not_found")
		}
		if err != nil {
			return err
		}
		if cur.IsSystem {
			return shared.Forbidden("system_role", "Built-in roles can't be deleted.")
		}
		if limit != nil {
			if err := checkWithin(cur.Rules, limit); err != nil {
				return shared.Forbidden("exceeds_your_access", "This role grants more than you have, so you can't delete it.")
			}
		}
		var used int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM crm.role_assignments WHERE role_id = $1`, id).Scan(&used); err != nil {
			return err
		}
		var invited int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM crm.invitations WHERE intended_role_id = $1 AND status IN ('pending', 'delivered')`, id).Scan(&invited); err != nil {
			return err
		}
		if used > 0 || invited > 0 {
			return shared.NewError(http.StatusConflict, "role_in_use", "People still have this role. Give them another role first.")
		}
		// People below this role now report to the role above it.
		if _, err := tx.Exec(ctx, `UPDATE crm.roles SET parent_role_id = $2 WHERE parent_role_id = $1`, id, cur.ParentRoleID); err != nil {
			return err
		}
		// Old, closed invitations reference the role; drop them with it.
		if _, err := tx.Exec(ctx, `DELETE FROM crm.invitations WHERE intended_role_id = $1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM crm.roles WHERE id = $1`, id); err != nil {
			return err
		}
		wsID := cur.WorkspaceID
		return shared.WriteAudit(ctx, tx, auditEvent(r, "role.deleted", "role", &id, &wsID, map[string]any{"name": cur.Name}, nil))
	})
}

func (h *Handler) resetRole(ctx context.Context, r *http.Request, id, ws uuid.UUID, limit *access.Effective) (*WorkspaceRole, error) {
	err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		cur, err := scanRole(tx.QueryRow(ctx, roleSelect+` WHERE r.id = $1 FOR UPDATE OF r`, id))
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && ws != uuid.Nil && cur.WorkspaceID != ws) {
			return shared.NotFound("role_not_found")
		}
		if err != nil {
			return err
		}
		sr, ok := access.FindSystemRole(cur.Key)
		if !cur.IsSystem || !ok {
			return shared.Validation(map[string]string{"role": "Only built-in roles can be reset."})
		}
		if err := checkWithin(sr.Rules, limit); err != nil {
			return err
		}
		raw, _ := json.Marshal(sr.Rules)
		if _, err := tx.Exec(ctx, `UPDATE crm.roles SET base_rules = $2, customized = false, updated_at = now() WHERE id = $1`, id, raw); err != nil {
			return err
		}
		wsID := cur.WorkspaceID
		return shared.WriteAudit(ctx, tx, auditEvent(r, "role.reset", "role", &id, &wsID, map[string]any{"rules": cur.Rules}, map[string]any{"rules": sr.Rules}))
	})
	if err != nil {
		return nil, err
	}
	ro, err := scanRole(h.store.Pool.QueryRow(ctx, roleSelect+` WHERE r.id = $1`, id))
	return &ro, err
}

// roleRules loads the effective rules of a role key in ws (for subset checks).
func (h *Handler) roleRules(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, ws uuid.UUID, key string) (access.Rules, error) {
	ro, err := scanRole(q.QueryRow(ctx, roleSelect+` WHERE r.workspace_id = $1 AND r.key = $2`, ws, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return access.Rules{}, shared.Validation(map[string]string{"roleKey": "Pick a role."})
	}
	return ro.Rules, err
}

// ---- owner handlers ----

func (h *Handler) handleListRoles(w http.ResponseWriter, r *http.Request) {
	ws, err := idParam(r, "id", "workspace_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	list, err := h.listRoles(r.Context(), ws)
	writeResult(w, r, http.StatusOK, map[string]any{"data": list}, err)
}

func (h *Handler) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	ws, err := idParam(r, "id", "workspace_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in roleInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var exists bool
	if err := h.store.Pool.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM crm.workspaces WHERE id = $1)`, ws).Scan(&exists); err != nil || !exists {
		shared.WriteError(w, r, shared.NotFound("workspace_not_found"))
		return
	}
	ro, err := h.createRole(r.Context(), r, ws, in, nil)
	writeResult(w, r, http.StatusCreated, ro, err)
}

func (h *Handler) handleUpdateRole(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id", "role_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in roleInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ro, err := h.updateRole(r.Context(), r, id, uuid.Nil, in, nil)
	writeResult(w, r, http.StatusOK, ro, err)
}

func (h *Handler) handleDeleteRole(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id", "role_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if err := h.deleteRole(r.Context(), r, id, uuid.Nil, nil); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleResetRole(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id", "role_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ro, err := h.resetRole(r.Context(), r, id, uuid.Nil, nil)
	writeResult(w, r, http.StatusOK, ro, err)
}

// ---- delegated admin (members with access.manage / members.manage) ----

// AdminRoutes mounts /admin/* inside a /w/{code} group whose middleware put the
// caller's AdminScope in the context via scopeFn.
func (h *Handler) AdminRoutes(r chi.Router, scopeFn func(*http.Request) *AdminScope) {
	needAccess := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !scopeFn(r).Limit.HasCapability(access.CapAccessManage) {
				shared.WriteError(w, r, shared.Forbidden("forbidden", "You don't have permission to manage roles and permission sets."))
				return
			}
			next(w, r)
		}
	}
	needMembers := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !scopeFn(r).Limit.HasCapability(access.CapMembersManage) {
				shared.WriteError(w, r, shared.Forbidden("forbidden", "You don't have permission to manage users."))
				return
			}
			next(w, r)
		}
	}
	r.Get("/admin/options", func(w http.ResponseWriter, r *http.Request) { h.adminOptions(w, r, scopeFn(r)) })
	r.Get("/admin/members", needMembers(func(w http.ResponseWriter, r *http.Request) { h.adminMembers(w, r, scopeFn(r)) }))
	r.Post("/admin/members", needMembers(func(w http.ResponseWriter, r *http.Request) { h.adminInvite(w, r, scopeFn(r)) }))
	r.Patch("/admin/members/{membershipId}", needMembers(func(w http.ResponseWriter, r *http.Request) { h.adminUpdateMember(w, r, scopeFn(r)) }))

	r.Post("/admin/roles", needAccess(func(w http.ResponseWriter, r *http.Request) {
		sc := scopeFn(r)
		var in roleInput
		if err := shared.DecodeJSON(w, r, &in); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		ro, err := h.createRole(r.Context(), r, sc.WS, in, sc.Limit)
		writeResult(w, r, http.StatusCreated, ro, err)
	}))
	r.Patch("/admin/roles/{id}", needAccess(func(w http.ResponseWriter, r *http.Request) {
		sc := scopeFn(r)
		id, err := idParam(r, "id", "role_not_found")
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		var in roleInput
		if err := shared.DecodeJSON(w, r, &in); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		ro, err := h.updateRole(r.Context(), r, id, sc.WS, in, sc.Limit)
		writeResult(w, r, http.StatusOK, ro, err)
	}))
	r.Delete("/admin/roles/{id}", needAccess(func(w http.ResponseWriter, r *http.Request) {
		sc := scopeFn(r)
		id, err := idParam(r, "id", "role_not_found")
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		if err := h.deleteRole(r.Context(), r, id, sc.WS, sc.Limit); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	r.Post("/admin/permission-sets", needAccess(func(w http.ResponseWriter, r *http.Request) {
		sc := scopeFn(r)
		var in permissionSetInput
		if err := shared.DecodeJSON(w, r, &in); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		p, err := h.createPermissionSet(r.Context(), r, sc.WS, in, sc.Limit)
		writeResult(w, r, http.StatusCreated, p, err)
	}))
	r.Patch("/admin/permission-sets/{id}", needAccess(func(w http.ResponseWriter, r *http.Request) {
		sc := scopeFn(r)
		id, err := idParam(r, "id", "permission_set_not_found")
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		var in permissionSetInput
		if err := shared.DecodeJSON(w, r, &in); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		p, err := h.updatePermissionSet(r.Context(), r, id, sc.WS, in, sc.Limit)
		writeResult(w, r, http.StatusOK, p, err)
	}))
	r.Delete("/admin/permission-sets/{id}", needAccess(func(w http.ResponseWriter, r *http.Request) {
		sc := scopeFn(r)
		id, err := idParam(r, "id", "permission_set_not_found")
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		if err := h.deletePermissionSet(r.Context(), r, id, sc.WS, sc.Limit); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
}

func (h *Handler) workspaceProductRefs(ctx context.Context, ws uuid.UUID) ([]access.ProductRef, error) {
	rows, err := h.store.Pool.Query(ctx, `
		SELECT p.id, p.key, p.name FROM crm.workspace_products wp JOIN crm.products p ON p.id = wp.product_id
		WHERE wp.workspace_id = $1 AND wp.status = 'active' AND p.status <> 'archived' ORDER BY p.name`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []access.ProductRef{}
	for rows.Next() {
		var p access.ProductRef
		if err := rows.Scan(&p.ID, &p.Key, &p.Name); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (h *Handler) listPermissionSets(ctx context.Context, ws uuid.UUID) ([]PermissionSet, error) {
	rows, err := h.store.Pool.Query(ctx, permissionSetSelect+` WHERE ps.workspace_id = $1 ORDER BY ps.name`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PermissionSet{}
	for rows.Next() {
		p, err := scanPermissionSet(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (h *Handler) adminOptions(w http.ResponseWriter, r *http.Request, sc *AdminScope) {
	ctx := r.Context()
	canAccess, canMembers := sc.Limit.HasCapability(access.CapAccessManage), sc.Limit.HasCapability(access.CapMembersManage)
	if !canAccess && !canMembers {
		shared.WriteError(w, r, shared.Forbidden("forbidden", "You don't manage users or access in this workspace."))
		return
	}
	roles, err := h.listRoles(ctx, sc.WS)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	sets, err := h.listPermissionSets(ctx, sc.WS)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	userTypes := []UserType{}
	if setup, err := WorkspaceSetup(ctx, h.store.Pool, sc.WS); err == nil && setup.UserTypes != nil {
		userTypes = setup.UserTypes
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{
		"canManageAccess": canAccess, "canManageMembers": canMembers, "userTypes": userTypes,
		"grantable": sc.Limit.AsRules(),
		"catalog":   accessCatalog{Objects: access.CatalogObjectsFor(sc.WS), Actions: access.Actions, Capabilities: access.CapabilityCatalog, Roles: access.SystemRoles()},
		"roles":     roles, "permissionSets": sets, "products": sc.Limit.Products, "isPlatform": sc.IsPlatform,
	})
}

type adminMember struct {
	WorkspaceMember
	ProductIDs     []uuid.UUID       `json:"productIds"`
	PermissionSets []nameRef         `json:"permissionSets"`
	Effective      *access.Effective `json:"effective"`
	IsSelf         bool              `json:"isSelf"`
	Editable       bool              `json:"editable"`
	LockedReason   string            `json:"lockedReason,omitempty"`
	UserType       string            `json:"userType,omitempty"`
	Phone          string            `json:"phone,omitempty"`
	// AddedBy: who gave this person access (empty for the founder of the business).
	AddedBy string `json:"addedBy,omitempty"`
}

func productsWithin(ids []uuid.UUID, limit *access.Effective) bool {
	allowed := map[uuid.UUID]bool{}
	for _, p := range limit.Products {
		allowed[p.ID] = true
	}
	for _, id := range ids {
		if !allowed[id] {
			return false
		}
	}
	return true
}

func (h *Handler) loadAdminMember(ctx context.Context, sc *AdminScope, membershipID uuid.UUID) (*adminMember, error) {
	var m adminMember
	var sets []byte
	var isOwner bool
	err := h.store.Pool.QueryRow(ctx, `
		SELECT m.id, i.id, i.display_name, COALESCE(e.value_normalized, ''), m.status, COALESCE(r.key, ''), COALESCE(r.name, ''),
		       i.last_login_at, m.created_at, COALESCE(r.product_ids, '{}'), i.is_platform_owner, COALESCE(m.user_type, ''),
		       COALESCE((SELECT p.value_normalized FROM crm.verified_identifiers p WHERE p.identity_id = i.id AND p.kind = 'phone' AND p.namespace = 'global' LIMIT 1), ''),
		       COALESCE((SELECT c.display_name FROM crm.identities c WHERE c.id = m.created_by AND c.id <> i.id), ''),
		       COALESCE((SELECT json_agg(json_build_object('id', ps.id, 'name', ps.name) ORDER BY ps.name)
		                   FROM crm.membership_permission_sets mps JOIN crm.permission_sets ps ON ps.id = mps.permission_set_id
		                  WHERE mps.membership_id = m.id), '[]')
		FROM crm.memberships m
		JOIN crm.identities i ON i.id = m.identity_id
		LEFT JOIN crm.verified_identifiers e ON e.identity_id = i.id AND e.kind = 'email' AND e.namespace = 'global'
		LEFT JOIN LATERAL (
			SELECT ro.key, ro.name, ra.product_ids FROM crm.role_assignments ra JOIN crm.roles ro ON ro.id = ra.role_id
			WHERE ra.membership_id = m.id ORDER BY ro.rank DESC LIMIT 1
		) r ON true
		WHERE m.id = $1 AND m.workspace_id = $2`, membershipID, sc.WS).Scan(
		&m.MembershipID, &m.IdentityID, &m.DisplayName, &m.Email, &m.Status, &m.RoleKey, &m.RoleName,
		&m.LastLoginAt, &m.CreatedAt, &m.ProductIDs, &isOwner, &m.UserType, &m.Phone, &m.AddedBy, &sets)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NotFound("membership_not_found")
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(sets, &m.PermissionSets)
	if m.Effective, err = access.ForMembership(ctx, h.store.Pool, m.MembershipID); err != nil {
		return nil, err
	}
	m.IsSelf = m.MembershipID == sc.MembershipID
	switch {
	case m.IsSelf:
		m.LockedReason = "You can't change your own access."
	case isOwner:
		m.LockedReason = "The platform owner's access can't be changed here."
	case m.Status == "revoked":
		m.LockedReason = "This membership was revoked."
	case len(access.Exceeds(m.Effective.AsRules(), sc.Limit)) > 0 || (!sc.IsPlatform && !productsWithin(m.ProductIDs, sc.Limit)):
		m.LockedReason = "They have more access than you, so only someone with more access can change it."
	default:
		m.Editable = true
	}
	return &m, nil
}

func (h *Handler) adminMembers(w http.ResponseWriter, r *http.Request, sc *AdminScope) {
	ctx := r.Context()
	rows, err := h.store.Pool.Query(ctx, `
		SELECT m.id FROM crm.memberships m JOIN crm.identities i ON i.id = m.identity_id
		WHERE m.workspace_id = $1 AND m.status <> 'invited' ORDER BY m.status = 'active' DESC, i.display_name`, sc.WS)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			shared.WriteError(w, r, err)
			return
		}
		ids = append(ids, id)
	}
	rows.Close()
	out := []adminMember{}
	for _, id := range ids {
		m, err := h.loadAdminMember(ctx, sc, id)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		out = append(out, *m)
	}
	invs, err := h.listInvitations(ctx, `inv.workspace_id = $1`, sc.WS)
	writeResult(w, r, http.StatusOK, map[string]any{"data": out, "invitations": invs}, err)
}

// checkGrant verifies a role + permission sets + products are all within the admin's access.
func (h *Handler) checkGrant(ctx context.Context, sc *AdminScope, roleKey *string, setIDs *[]uuid.UUID, productIDs *[]uuid.UUID) error {
	if roleKey != nil {
		rules, err := h.roleRules(ctx, h.store.Pool, sc.WS, *roleKey)
		if err != nil {
			return err
		}
		if list := access.Exceeds(rules, sc.Limit); len(list) > 0 {
			return exceedsErr(list)
		}
	}
	if setIDs != nil {
		for _, id := range *setIDs {
			p, err := scanPermissionSet(h.store.Pool.QueryRow(ctx, permissionSetSelect+` WHERE ps.id = $1 AND ps.workspace_id = $2`, id, sc.WS))
			if errors.Is(err, pgx.ErrNoRows) {
				return shared.Validation(map[string]string{"permissionSetIds": "Pick permission sets from this workspace."})
			}
			if err != nil {
				return err
			}
			if list := access.Exceeds(p.Rules, sc.Limit); len(list) > 0 {
				return exceedsErr(list)
			}
		}
	}
	if productIDs != nil && !sc.IsPlatform && !productsWithin(*productIDs, sc.Limit) {
		return shared.Forbidden("exceeds_your_access", "You can only give access to products you have yourself.")
	}
	return nil
}

func (h *Handler) adminUpdateMember(w http.ResponseWriter, r *http.Request, sc *AdminScope) {
	ctx := r.Context()
	mid, err := idParam(r, "membershipId", "membership_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in struct {
		Status           *string      `json:"status"`
		RoleKey          *string      `json:"roleKey"`
		ProductIDs       *[]uuid.UUID `json:"productIds"`
		PermissionSetIDs *[]uuid.UUID `json:"permissionSetIds"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	target, err := h.loadAdminMember(ctx, sc, mid)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if !target.Editable {
		shared.WriteError(w, r, shared.Forbidden("exceeds_your_access", target.LockedReason))
		return
	}
	if sc.IsPlatform {
		in.ProductIDs = nil
	}
	if err := h.checkGrant(ctx, sc, in.RoleKey, in.PermissionSetIDs, in.ProductIDs); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		after := map[string]any{}
		if in.Status != nil {
			if *in.Status != "active" && *in.Status != "suspended" {
				return shared.Validation(map[string]string{"status": "Status must be active or suspended."})
			}
			if _, err := tx.Exec(ctx, `UPDATE crm.memberships SET status = $2, auth_version = auth_version + 1 WHERE id = $1`, mid, *in.Status); err != nil {
				return err
			}
			if *in.Status == "suspended" {
				if _, err := tx.Exec(ctx, `UPDATE crm.sessions SET revoked_at = now() WHERE identity_id = $1 AND revoked_at IS NULL`, target.IdentityID); err != nil {
					return err
				}
			}
			after["status"] = *in.Status
		}
		if in.RoleKey != nil || in.ProductIDs != nil || in.PermissionSetIDs != nil {
			if err := setMembershipAccess(ctx, tx, actorID(r), sc.WS, mid, in.RoleKey, in.ProductIDs, in.PermissionSetIDs, ""); err != nil {
				return err
			}
			after["roleKey"], after["productIds"], after["permissionSetIds"] = in.RoleKey, in.ProductIDs, in.PermissionSetIDs
		}
		ws := sc.WS
		return shared.WriteAudit(ctx, tx, auditEvent(r, "membership.updated", "membership", &mid, &ws, map[string]any{"delegated": true}, after))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	m, err := h.loadAdminMember(ctx, sc, mid)
	writeResult(w, r, http.StatusOK, m, err)
}

func (h *Handler) adminInvite(w http.ResponseWriter, r *http.Request, sc *AdminScope) {
	ctx := r.Context()
	var in struct {
		DisplayName      string      `json:"displayName"`
		Email            string      `json:"email"`
		Phone            string      `json:"phone"`
		RoleKey          string      `json:"roleKey"`
		ProductIDs       []uuid.UUID `json:"productIds"`
		PermissionSetIDs []uuid.UUID `json:"permissionSetIds"`
		Method           string      `json:"method"`
		Password         string      `json:"password"`
		UserType         string      `json:"userType"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if n := strings.TrimSpace(in.DisplayName); n == "" || len(n) > 80 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"displayName": "Enter a name (up to 80 characters)."}))
		return
	}
	if strings.TrimSpace(in.Phone) != "" {
		if _, ok := identity.NormalizePhone(in.Phone); !ok {
			shared.WriteError(w, r, shared.Validation(map[string]string{"phone": "Enter a valid phone number."}))
			return
		}
	}
	if in.RoleKey == "" {
		shared.WriteError(w, r, shared.Validation(map[string]string{"roleKey": "Pick a role."}))
		return
	}
	// With no products chosen, a delegated admin hands out their own products (never "all").
	if !sc.IsPlatform && len(in.ProductIDs) == 0 {
		for _, p := range sc.Limit.Products {
			in.ProductIDs = append(in.ProductIDs, p.ID)
		}
	}
	if err := h.checkGrant(ctx, sc, &in.RoleKey, &in.PermissionSetIDs, &in.ProductIDs); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	// A teammate added by mobile number alone signs in with a code sent to that number
	// (D-93): no email, no password, no invitation link. Access is theirs from the first sign-in.
	if phone, ok := identity.NormalizePhone(in.Phone); ok && strings.TrimSpace(in.Email) == "" {
		var res *GiveLoginResult
		err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
			var err error
			res, err = h.addMemberByPhone(ctx, tx, actorID(r), sc.WS, strings.TrimSpace(in.DisplayName), phone, in.RoleKey, in.ProductIDs, in.PermissionSetIDs, in.UserType)
			return err
		})
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		shared.WriteJSON(w, http.StatusCreated, res)
		return
	}
	gl := GiveLoginInput{
		Workspace: GiveLoginWorkspace{Mode: "existing", WorkspaceID: sc.WS}, RoleKey: in.RoleKey, ProductIDs: in.ProductIDs,
		PermissionSetIDs: in.PermissionSetIDs, Method: in.Method, Password: in.Password, UserType: in.UserType,
	}
	person := Person{Name: in.DisplayName, Email: in.Email, Phone: in.Phone}
	hash, err := PrepareGiveLogin(&gl, person, "")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var res *GiveLoginResult
	var sent *SentInvitation
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		var err error
		res, sent, err = h.GiveLoginTx(ctx, tx, actorID(r), person, gl, hash, "")
		return err
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if sent != nil {
		res.Invitation = h.DeliverInvitation(ctx, sent)
	}
	shared.WriteJSON(w, http.StatusCreated, res)
}

// addMemberByPhone gives a person access to a workspace by their mobile number. The
// number is theirs once they sign in with a code sent to it; until then nobody can use it.
func (h *Handler) addMemberByPhone(ctx context.Context, tx pgx.Tx, actor, ws uuid.UUID, name, phone, roleKey string,
	productIDs, permissionSetIDs []uuid.UUID, userType string) (*GiveLoginResult, error) {
	res := &GiveLoginResult{WorkspaceID: ws}
	if err := checkUserType(ctx, tx, ws, userType, roleKey, ""); err != nil {
		return nil, err
	}
	err := tx.QueryRow(ctx, `SELECT identity_id FROM crm.verified_identifiers WHERE kind = 'phone' AND namespace = 'global' AND value_normalized = $1`,
		phone).Scan(&res.IdentityID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if err := tx.QueryRow(ctx, `INSERT INTO crm.identities (display_name, source) VALUES ($1, 'invited_by_phone') RETURNING id`, name).Scan(&res.IdentityID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO crm.verified_identifiers (identity_id, kind, value_normalized, namespace) VALUES ($1, 'phone', $2, 'global')`,
			res.IdentityID, phone); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	default:
		res.ExistingLogin = true
	}
	var mStatus string
	err = tx.QueryRow(ctx, `SELECT id, status FROM crm.memberships WHERE workspace_id = $1 AND identity_id = $2 FOR UPDATE`, ws, res.IdentityID).
		Scan(&res.MembershipID, &mStatus)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if err := checkSeat(ctx, tx, ws); err != nil {
			return nil, err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO crm.memberships (workspace_id, identity_id, status, created_by) VALUES ($1, $2, 'active', $3) RETURNING id`,
			ws, res.IdentityID, actor).Scan(&res.MembershipID); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	case mStatus == "active":
		return nil, shared.Validation(map[string]string{"phone": "This person already has access. Change their access from the members list."})
	default:
		if _, err := tx.Exec(ctx, `UPDATE crm.memberships SET status = 'active' WHERE id = $1`, res.MembershipID); err != nil {
			return nil, err
		}
	}
	role, pids, sets := roleKey, productIDs, permissionSetIDs
	// A role is a place in the hierarchy; what a person may do comes from permission sets
	// (D-48). With none chosen, an Admin or Staff member gets that role's default set.
	if len(sets) == 0 {
		if id, ok := DefaultPermissionSetFor(ctx, tx, ws, roleKey); ok {
			sets = []uuid.UUID{id}
		}
	}
	if err := setMembershipAccess(ctx, tx, actor, ws, res.MembershipID, &role, &pids, &sets, ""); err != nil {
		return nil, err
	}
	if err := setUserType(ctx, tx, res.MembershipID, userType); err != nil {
		return nil, err
	}
	if len(productIDs) == 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE crm.role_assignments SET product_ids = COALESCE((SELECT array_agg(product_id) FROM crm.workspace_products
			  WHERE workspace_id = $1 AND status = 'active'), '{}') WHERE membership_id = $2`, ws, res.MembershipID); err != nil {
			return nil, err
		}
	}
	return res, shared.WriteAudit(ctx, tx, shared.AuditEvent{
		WorkspaceID: &ws, ActorID: &actor, Action: "identity.login_granted", EntityType: "identity", EntityID: &res.IdentityID,
		After: map[string]any{"method": "phone", "role": roleKey, "membershipId": res.MembershipID, "existing": res.ExistingLogin},
	})
}
