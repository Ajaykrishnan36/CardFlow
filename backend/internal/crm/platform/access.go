package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Access administration (PRD §7, PERM-01..03): permission sets per workspace, membership
// role/products/permission sets, and giving a person a login.

// ---- catalog & pickers ----

type accessCatalog struct {
	Objects      []access.CatalogObject `json:"objects"`
	Actions      []access.CatalogEntry  `json:"actions"`
	Capabilities []access.CatalogEntry  `json:"capabilities"`
	Roles        []access.SystemRole    `json:"roles"`
}

func (h *Handler) handleAccessCatalog(w http.ResponseWriter, r *http.Request) {
	shared.WriteJSON(w, http.StatusOK, accessCatalog{
		Objects: access.CatalogObjects(), Actions: access.Actions, Capabilities: access.CapabilityCatalog, Roles: access.SystemRoles(),
	})
}

type nameRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type accessWorkspace struct {
	ID             uuid.UUID           `json:"id"`
	Code           string              `json:"code"`
	Name           string              `json:"name"`
	IsPlatform     bool                `json:"isPlatform"`
	Status         string              `json:"status"`
	Products       []access.ProductRef `json:"products"`
	PermissionSets []nameRef           `json:"permissionSets"`
	Roles          []roleRef           `json:"roles"`
}

type roleRef struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	IsSystem bool   `json:"isSystem"`
}

func (h *Handler) handleAccessWorkspaces(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := h.store.Pool.Query(ctx, `
		SELECT w.id, w.code, w.name, w.is_platform, w.status,
		       COALESCE((SELECT json_agg(json_build_object('id', p.id, 'key', p.key, 'name', p.name) ORDER BY p.name)
		                   FROM crm.workspace_products wp JOIN crm.products p ON p.id = wp.product_id
		                  WHERE wp.workspace_id = w.id AND wp.status = 'active' AND p.status <> 'archived'), '[]'),
		       COALESCE((SELECT json_agg(json_build_object('id', ps.id, 'name', ps.name) ORDER BY ps.name)
		                   FROM crm.permission_sets ps WHERE ps.workspace_id = w.id), '[]'),
		       COALESCE((SELECT json_agg(json_build_object('key', ro.key, 'name', ro.name, 'isSystem', ro.is_system) ORDER BY ro.rank DESC, ro.name)
		                   FROM crm.roles ro WHERE ro.workspace_id = w.id), '[]')
		FROM crm.workspaces w WHERE w.status = 'active'
		ORDER BY w.is_platform DESC, w.name`)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	out := []accessWorkspace{}
	for rows.Next() {
		var a accessWorkspace
		var prods, sets, roles []byte
		if err := rows.Scan(&a.ID, &a.Code, &a.Name, &a.IsPlatform, &a.Status, &prods, &sets, &roles); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		_ = json.Unmarshal(prods, &a.Products)
		_ = json.Unmarshal(sets, &a.PermissionSets)
		_ = json.Unmarshal(roles, &a.Roles)
		if a.IsPlatform {
			a.Name = "Your team · Platform CRM"
		}
		out = append(out, a)
	}
	writeResult(w, r, http.StatusOK, map[string]any{"data": out}, rows.Err())
}

// ---- permission sets ----

type PermissionSet struct {
	ID            uuid.UUID    `json:"id"`
	WorkspaceID   uuid.UUID    `json:"workspaceId"`
	Name          string       `json:"name"`
	Description   string       `json:"description,omitempty"`
	Rules         access.Rules `json:"rules"`
	AssignedCount int          `json:"assignedCount"`
	CreatedAt     time.Time    `json:"createdAt"`
	UpdatedAt     time.Time    `json:"updatedAt"`
}

const permissionSetSelect = `
	SELECT ps.id, ps.workspace_id, ps.name, COALESCE(ps.description, ''), ps.rules, ps.created_at, ps.updated_at,
	       (SELECT count(*) FROM crm.membership_permission_sets m WHERE m.permission_set_id = ps.id)
	FROM crm.permission_sets ps`

func scanPermissionSet(row pgx.Row) (PermissionSet, error) {
	var p PermissionSet
	var raw []byte
	err := row.Scan(&p.ID, &p.WorkspaceID, &p.Name, &p.Description, &raw, &p.CreatedAt, &p.UpdatedAt, &p.AssignedCount)
	if err == nil {
		var rules access.Rules
		_ = json.Unmarshal(raw, &rules)
		p.Rules = access.Normalize(rules)
	}
	return p, err
}

func (h *Handler) handleListPermissionSets(w http.ResponseWriter, r *http.Request) {
	wsID, err := idParam(r, "id", "workspace_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	list, err := h.listPermissionSets(r.Context(), wsID)
	writeResult(w, r, http.StatusOK, map[string]any{"data": list}, err)
}

type permissionSetInput struct {
	Name        *string       `json:"name"`
	Description *string       `json:"description"`
	Rules       *access.Rules `json:"rules"`
}

func (h *Handler) createPermissionSet(ctx context.Context, r *http.Request, wsID uuid.UUID, in permissionSetInput, limit *access.Effective) (*PermissionSet, error) {
	name, desc := "", ""
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		desc = strings.TrimSpace(*in.Description)
	}
	fe := map[string]string{}
	if name == "" || len(name) > 80 {
		fe["name"] = "Enter a name (up to 80 characters)."
	}
	if len(desc) > 500 {
		fe["description"] = "Use at most 500 characters."
	}
	if len(fe) > 0 {
		return nil, shared.Validation(fe)
	}
	rules := access.Rules{}
	if in.Rules != nil {
		rules = *in.Rules
	}
	rules = access.Normalize(rules)
	if err := checkWithin(rules, limit); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(rules)
	var id uuid.UUID
	err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.workspaces WHERE id = $1)`, wsID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return shared.NotFound("workspace_not_found")
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO crm.permission_sets (workspace_id, name, description, rules, created_by) VALUES ($1, $2, NULLIF($3, ''), $4, $5)
			RETURNING id`, wsID, name, desc, raw, actorID(r)).Scan(&id); err != nil {
			if isUniqueViolation(err, "") {
				return shared.Validation(map[string]string{"name": "A permission set with this name already exists in this workspace."})
			}
			return err
		}
		return shared.WriteAudit(ctx, tx, auditEvent(r, "permission_set.created", "permission_set", &id, &wsID, nil,
			map[string]any{"name": name, "rules": rules}))
	})
	if err != nil {
		return nil, err
	}
	p, err := scanPermissionSet(h.store.Pool.QueryRow(ctx, permissionSetSelect+` WHERE ps.id = $1`, id))
	return &p, err
}

// updatePermissionSet edits a set; with ws != Nil it must belong to that workspace.
func (h *Handler) updatePermissionSet(ctx context.Context, r *http.Request, id, ws uuid.UUID, in permissionSetInput, limit *access.Effective) (*PermissionSet, error) {
	err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		cur, err := scanPermissionSet(tx.QueryRow(ctx, permissionSetSelect+` WHERE ps.id = $1 FOR UPDATE OF ps`, id))
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && ws != uuid.Nil && cur.WorkspaceID != ws) {
			return shared.NotFound("permission_set_not_found")
		}
		if err != nil {
			return err
		}
		if limit != nil {
			if err := checkWithin(cur.Rules, limit); err != nil {
				return shared.Forbidden("exceeds_your_access", "This permission set grants more than you have, so you can't change it.")
			}
		}
		name, desc, rules := cur.Name, cur.Description, cur.Rules
		fe := map[string]string{}
		if in.Name != nil {
			name = strings.TrimSpace(*in.Name)
			if name == "" || len(name) > 80 {
				fe["name"] = "Enter a name (up to 80 characters)."
			}
		}
		if in.Description != nil {
			desc = strings.TrimSpace(*in.Description)
			if len(desc) > 500 {
				fe["description"] = "Use at most 500 characters."
			}
		}
		if in.Rules != nil {
			rules = access.Normalize(*in.Rules)
			if err := checkWithin(rules, limit); err != nil {
				return err
			}
		}
		if len(fe) > 0 {
			return shared.Validation(fe)
		}
		raw, _ := json.Marshal(rules)
		if _, err := tx.Exec(ctx, `
			UPDATE crm.permission_sets SET name = $2, description = NULLIF($3, ''), rules = $4, updated_at = now() WHERE id = $1`,
			id, name, desc, raw); err != nil {
			if isUniqueViolation(err, "") {
				return shared.Validation(map[string]string{"name": "A permission set with this name already exists in this workspace."})
			}
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE crm.memberships SET auth_version = auth_version + 1
			WHERE id IN (SELECT membership_id FROM crm.membership_permission_sets WHERE permission_set_id = $1)`, id); err != nil {
			return err
		}
		wsID := cur.WorkspaceID
		return shared.WriteAudit(ctx, tx, auditEvent(r, "permission_set.updated", "permission_set", &id, &wsID,
			map[string]any{"name": cur.Name, "rules": cur.Rules}, map[string]any{"name": name, "rules": rules}))
	})
	if err != nil {
		return nil, err
	}
	p, err := scanPermissionSet(h.store.Pool.QueryRow(ctx, permissionSetSelect+` WHERE ps.id = $1`, id))
	return &p, err
}

func (h *Handler) deletePermissionSet(ctx context.Context, r *http.Request, id, ws uuid.UUID, limit *access.Effective) error {
	return h.store.WithTx(ctx, func(tx pgx.Tx) error {
		cur, err := scanPermissionSet(tx.QueryRow(ctx, permissionSetSelect+` WHERE ps.id = $1 FOR UPDATE OF ps`, id))
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && ws != uuid.Nil && cur.WorkspaceID != ws) {
			return shared.NotFound("permission_set_not_found")
		}
		if err != nil {
			return err
		}
		if limit != nil {
			if err := checkWithin(cur.Rules, limit); err != nil {
				return shared.Forbidden("exceeds_your_access", "This permission set grants more than you have, so you can't delete it.")
			}
		}
		if cur.AssignedCount > 0 {
			return shared.NewError(http.StatusConflict, "permission_set_in_use",
				"This permission set is assigned to users. Remove it from them first.")
		}
		var apiKeys int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM crm.api_keys WHERE permission_set_id = $1`, id).Scan(&apiKeys); err != nil {
			return err
		}
		if apiKeys > 0 {
			return shared.NewError(http.StatusConflict, "permission_set_in_use", "An API key uses this permission set.")
		}
		if _, err := tx.Exec(ctx, `DELETE FROM crm.permission_sets WHERE id = $1`, id); err != nil {
			return err
		}
		wsID := cur.WorkspaceID
		return shared.WriteAudit(ctx, tx, auditEvent(r, "permission_set.deleted", "permission_set", &id, &wsID,
			map[string]any{"name": cur.Name}, nil))
	})
}

func (h *Handler) handleCreatePermissionSet(w http.ResponseWriter, r *http.Request) {
	wsID, err := idParam(r, "id", "workspace_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in permissionSetInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	p, err := h.createPermissionSet(r.Context(), r, wsID, in, nil)
	writeResult(w, r, http.StatusCreated, p, err)
}

func (h *Handler) handleUpdatePermissionSet(w http.ResponseWriter, r *http.Request) {
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
	p, err := h.updatePermissionSet(r.Context(), r, id, uuid.Nil, in, nil)
	writeResult(w, r, http.StatusOK, p, err)
}

func (h *Handler) handleDeletePermissionSet(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id", "permission_set_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if err := h.deletePermissionSet(r.Context(), r, id, uuid.Nil, nil); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// validatePermissionSets checks that every id belongs to the workspace.
func validatePermissionSets(ctx context.Context, tx pgx.Tx, wsID uuid.UUID, ids []uuid.UUID, fieldPrefix string) ([]uuid.UUID, error) {
	ids = uniqueUUIDs(ids)
	if len(ids) == 0 {
		return []uuid.UUID{}, nil
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM crm.permission_sets WHERE workspace_id = $1 AND id = ANY($2::uuid[])`, wsID, ids).Scan(&n); err != nil {
		return nil, err
	}
	if n != len(ids) {
		return nil, shared.Validation(map[string]string{fieldPrefix + "permissionSetIds": "Pick permission sets from this workspace."})
	}
	return ids, nil
}

// workspaceProducts resolves the product grant: default all active products; otherwise
// they must be assigned to the workspace. The platform workspace has none.
func workspaceProducts(ctx context.Context, tx pgx.Tx, wsID uuid.UUID, requested []uuid.UUID, fieldPrefix string) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(array_agg(product_id), '{}') FROM crm.workspace_products
		WHERE workspace_id = $1 AND status = 'active' AND (COALESCE(cardinality($2::uuid[]), 0) = 0 OR product_id = ANY($2::uuid[]))`,
		wsID, requested).Scan(&ids); err != nil {
		return nil, err
	}
	if len(requested) > 0 && len(ids) != len(uniqueUUIDs(requested)) {
		return nil, shared.Validation(map[string]string{fieldPrefix + "productIds": "Pick products assigned to this workspace."})
	}
	return ids, nil
}

// setMembershipAccess replaces a membership's role (with products) and/or permission sets.
func setMembershipAccess(ctx context.Context, tx pgx.Tx, actor, wsID, membershipID uuid.UUID, roleKey *string, productIDs *[]uuid.UUID, setIDs *[]uuid.UUID, fieldPrefix string) error {
	if roleKey != nil || productIDs != nil {
		key := ""
		if roleKey != nil {
			key = *roleKey
		} else if err := tx.QueryRow(ctx, `
			SELECT r.key FROM crm.role_assignments ra JOIN crm.roles r ON r.id = ra.role_id
			WHERE ra.membership_id = $1 ORDER BY r.rank DESC LIMIT 1`, membershipID).Scan(&key); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if key == "" {
			key = "END_USER"
		}
		var roleID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM crm.roles WHERE workspace_id = $1 AND key = $2`, wsID, key).Scan(&roleID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return shared.Validation(map[string]string{fieldPrefix + "roleKey": "Pick a role."})
			}
			return err
		}
		var requested []uuid.UUID
		if productIDs != nil {
			requested = *productIDs
		} else if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT product_ids FROM crm.role_assignments WHERE membership_id = $1 LIMIT 1), '{}')`,
			membershipID).Scan(&requested); err != nil {
			return err
		}
		pids, err := workspaceProducts(ctx, tx, wsID, requested, fieldPrefix)
		if err != nil {
			return err
		}
		if productIDs != nil && len(*productIDs) == 0 {
			pids = []uuid.UUID{} // explicit "no products"
		}
		if _, err := tx.Exec(ctx, `DELETE FROM crm.role_assignments WHERE membership_id = $1`, membershipID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.role_assignments (workspace_id, membership_id, role_id, product_ids, granted_by) VALUES ($1, $2, $3, $4, $5)`,
			wsID, membershipID, roleID, pids, actor); err != nil {
			return err
		}
	}
	if setIDs != nil {
		ids, err := validatePermissionSets(ctx, tx, wsID, *setIDs, fieldPrefix)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM crm.membership_permission_sets WHERE membership_id = $1`, membershipID); err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := tx.Exec(ctx, `
				INSERT INTO crm.membership_permission_sets (workspace_id, membership_id, permission_set_id, granted_by) VALUES ($1, $2, $3, $4)`,
				wsID, membershipID, id, actor); err != nil {
				return err
			}
		}
	}
	_, err := tx.Exec(ctx, `UPDATE crm.memberships SET auth_version = auth_version + 1 WHERE id = $1`, membershipID)
	return err
}

// ---- giving a person a login ----

type Person struct {
	Name  string
	Email string
	Phone string
}

type GiveLoginWorkspace struct {
	Mode        string      `json:"mode"` // platform | existing | new
	WorkspaceID uuid.UUID   `json:"workspaceId"`
	Name        string      `json:"name"`
	Code        string      `json:"code"`
	ProductIDs  []uuid.UUID `json:"productIds"`
	Timezone    string      `json:"timezone"`
	Currency    string      `json:"currency"`
}

type GiveLoginInput struct {
	Workspace        GiveLoginWorkspace `json:"workspace"`
	RoleKey          string             `json:"roleKey"`
	ProductIDs       []uuid.UUID        `json:"productIds"`
	PermissionSetIDs []uuid.UUID        `json:"permissionSetIds"`
	Method           string             `json:"method"` // invite | password
	Password         string             `json:"password"`
}

type GiveLoginResult struct {
	IdentityID    uuid.UUID   `json:"identityId"`
	WorkspaceID   uuid.UUID   `json:"workspaceId"`
	MembershipID  uuid.UUID   `json:"membershipId"`
	Invitation    *Invitation `json:"invitation,omitempty"`
	ExistingLogin bool        `json:"existingLogin,omitempty"`
}

// PrepareGiveLogin validates input that doesn't need the database and hashes a
// temporary password outside any transaction (argon2 is deliberately slow).
func PrepareGiveLogin(in *GiveLoginInput, p Person, fieldPrefix string) (string, error) {
	fe := map[string]string{}
	if _, ok := normalizeEmail(p.Email); !ok {
		fe[fieldPrefix+"email"] = "Add a valid email address first — it's what they sign in with."
	}
	if strings.TrimSpace(in.RoleKey) == "" {
		fe[fieldPrefix+"roleKey"] = "Pick a role."
	}
	switch in.Workspace.Mode {
	case "platform", "existing", "new":
	default:
		fe[fieldPrefix+"workspace.mode"] = "Choose a workspace."
	}
	if in.Workspace.Mode == "existing" && in.Workspace.WorkspaceID == uuid.Nil {
		fe[fieldPrefix+"workspace.workspaceId"] = "Choose a workspace."
	}
	hash := ""
	switch in.Method {
	case "invite":
	case "password":
		if msg := identity.ValidateNewPassword(in.Password); msg != "" {
			fe[fieldPrefix+"password"] = msg
		}
	default:
		fe[fieldPrefix+"method"] = "Choose how they get their password."
	}
	if len(fe) > 0 {
		return "", shared.Validation(fe)
	}
	if in.Method == "password" {
		var err error
		if hash, err = identity.HashPassword(in.Password); err != nil {
			return "", err
		}
	}
	return hash, nil
}

// GiveLoginTx creates or links the person's identity and gives it access to a workspace
// — either an invitation (72 h link) or, with a temporary password, active access right
// away with a forced password change at first sign-in. An existing password is never
// overwritten.
func (h *Handler) GiveLoginTx(ctx context.Context, tx pgx.Tx, actor uuid.UUID, p Person, in GiveLoginInput, passwordHash, fieldPrefix string) (*GiveLoginResult, *SentInvitation, error) {
	email, _ := normalizeEmail(p.Email)
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = email
	}
	res := &GiveLoginResult{}

	// 1. Workspace
	switch in.Workspace.Mode {
	case "platform":
		id, err := PlatformWorkspaceID(ctx, tx)
		if err != nil {
			return nil, nil, err
		}
		res.WorkspaceID = id
	case "existing":
		var status string
		var isPlatform bool
		err := tx.QueryRow(ctx, `SELECT status, is_platform FROM crm.workspaces WHERE id = $1`, in.Workspace.WorkspaceID).Scan(&status, &isPlatform)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, shared.Validation(map[string]string{fieldPrefix + "workspace.workspaceId": "Choose a workspace."})
		}
		if err != nil {
			return nil, nil, err
		}
		if status != "active" {
			return nil, nil, shared.Validation(map[string]string{fieldPrefix + "workspace.workspaceId": "That workspace isn't active."})
		}
		res.WorkspaceID = in.Workspace.WorkspaceID
	case "new":
		pin := ProvisionInput{Name: in.Workspace.Name, Code: in.Workspace.Code, Timezone: in.Workspace.Timezone,
			Currency: in.Workspace.Currency, ProductIDs: in.Workspace.ProductIDs}
		if fe := pin.normalizeAndValidate(fieldPrefix + "workspace."); len(fe) > 0 {
			return nil, nil, shared.Validation(fe)
		}
		id, err := ProvisionTx(ctx, tx, actor, pin, fieldPrefix+"workspace.")
		if err != nil {
			return nil, nil, err
		}
		res.WorkspaceID = id
	}

	// 2a. Invitation
	if in.Method == "invite" {
		var hasPassword bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM crm.verified_identifiers vi JOIN crm.password_credentials pc ON pc.identity_id = vi.identity_id
			               WHERE vi.kind = 'email' AND vi.namespace = 'global' AND vi.value_normalized = $1)`, email).Scan(&hasPassword); err != nil {
			return nil, nil, err
		}
		sent, err := h.InviteTx(ctx, tx, actor, res.WorkspaceID, InviteInput{
			Name: name, Email: email, RoleKey: in.RoleKey, ProductIDs: in.ProductIDs, PermissionSetIDs: in.PermissionSetIDs,
		}, fieldPrefix)
		if err != nil {
			return nil, nil, err
		}
		res.IdentityID, res.MembershipID, res.ExistingLogin = sent.IdentityID, sent.MembershipID, hasPassword
		return res, sent, nil
	}

	// 2b. Temporary password: identity (verified email, forced change) + active membership.
	err := tx.QueryRow(ctx, `
		SELECT identity_id FROM crm.verified_identifiers WHERE kind = 'email' AND namespace = 'global' AND value_normalized = $1`,
		email).Scan(&res.IdentityID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if err := tx.QueryRow(ctx, `INSERT INTO crm.identities (display_name) VALUES ($1) RETURNING id`, name).Scan(&res.IdentityID); err != nil {
			return nil, nil, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.verified_identifiers (identity_id, kind, value_normalized, verified_at) VALUES ($1, 'email', $2, now())`,
			res.IdentityID, email); err != nil {
			return nil, nil, err
		}
		if phone, ok := identity.NormalizePhone(p.Phone); ok {
			_, _ = tx.Exec(ctx, `
				INSERT INTO crm.verified_identifiers (identity_id, kind, value_normalized, verified_at) VALUES ($1, 'phone', $2, now())
				ON CONFLICT DO NOTHING`, res.IdentityID, phone)
		}
	case err != nil:
		return nil, nil, err
	default:
		res.ExistingLogin = true
		// The owner vouches for the address; mark it verified so the person can sign in.
		if _, err := tx.Exec(ctx, `UPDATE crm.verified_identifiers SET verified_at = COALESCE(verified_at, now())
			WHERE identity_id = $1 AND kind = 'email' AND value_normalized = $2`, res.IdentityID, email); err != nil {
			return nil, nil, err
		}
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO crm.password_credentials (identity_id, hash, must_change) VALUES ($1, $2, true)
		ON CONFLICT (identity_id) DO NOTHING`, res.IdentityID, passwordHash)
	if err != nil {
		return nil, nil, err
	}
	if tag.RowsAffected() == 1 {
		res.ExistingLogin = false // they had an identity but no password until now
	}

	var mStatus string
	err = tx.QueryRow(ctx, `SELECT id, status FROM crm.memberships WHERE workspace_id = $1 AND identity_id = $2 FOR UPDATE`,
		res.WorkspaceID, res.IdentityID).Scan(&res.MembershipID, &mStatus)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if err := tx.QueryRow(ctx, `
			INSERT INTO crm.memberships (workspace_id, identity_id, status, created_by) VALUES ($1, $2, 'active', $3) RETURNING id`,
			res.WorkspaceID, res.IdentityID, actor).Scan(&res.MembershipID); err != nil {
			return nil, nil, err
		}
	case err != nil:
		return nil, nil, err
	case mStatus == "active":
		return nil, nil, shared.Validation(map[string]string{fieldPrefix + "email": "This person already has access to that workspace. Change their access from their user page."})
	default:
		if _, err := tx.Exec(ctx, `UPDATE crm.memberships SET status = 'active' WHERE id = $1`, res.MembershipID); err != nil {
			return nil, nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE crm.invitations SET status = 'revoked' WHERE membership_id = $1 AND status IN ('pending', 'delivered', 'delivery_failed')`,
			res.MembershipID); err != nil {
			return nil, nil, err
		}
	}
	role := in.RoleKey
	pids := in.ProductIDs
	sets := in.PermissionSetIDs
	if err := setMembershipAccess(ctx, tx, actor, res.WorkspaceID, res.MembershipID, &role, &pids, &sets, fieldPrefix); err != nil {
		return nil, nil, err
	}
	if len(in.ProductIDs) == 0 { // "all products" default
		if _, err := tx.Exec(ctx, `
			UPDATE crm.role_assignments SET product_ids = COALESCE((SELECT array_agg(product_id) FROM crm.workspace_products
			  WHERE workspace_id = $1 AND status = 'active'), '{}') WHERE membership_id = $2`, res.WorkspaceID, res.MembershipID); err != nil {
			return nil, nil, err
		}
	}
	wsID := res.WorkspaceID
	return res, nil, shared.WriteAudit(ctx, tx, shared.AuditEvent{
		WorkspaceID: &wsID, ActorID: &actor, Action: "identity.login_granted", EntityType: "identity", EntityID: &res.IdentityID,
		After: map[string]any{"method": "password", "role": in.RoleKey, "membershipId": res.MembershipID, "existing": res.ExistingLogin},
	})
}

// ---- users: create / add membership ----

type createUserInput struct {
	GiveLoginInput
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	Phone       string `json:"phone"`
}

func (h *Handler) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var in createUserInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	fe := map[string]string{}
	if n := strings.TrimSpace(in.DisplayName); n == "" || len(n) > 80 {
		fe["displayName"] = "Enter a name (up to 80 characters)."
	}
	if strings.TrimSpace(in.Phone) != "" {
		if _, ok := identity.NormalizePhone(in.Phone); !ok {
			fe["phone"] = "Enter a valid phone number."
		}
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	person := Person{Name: in.DisplayName, Email: in.Email, Phone: in.Phone}
	hash, err := PrepareGiveLogin(&in.GiveLoginInput, person, "")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var res *GiveLoginResult
	var sent *SentInvitation
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		var err error
		res, sent, err = h.GiveLoginTx(r.Context(), tx, actorID(r), person, in.GiveLoginInput, hash, "")
		return err
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if sent != nil {
		res.Invitation = h.DeliverInvitation(r.Context(), sent)
	}
	user, err := h.getUser(r.Context(), res.IdentityID)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusCreated, map[string]any{
		"identityId": res.IdentityID, "workspaceId": res.WorkspaceID, "membershipId": res.MembershipID,
		"invitation": res.Invitation, "existingLogin": res.ExistingLogin, "user": user,
	})
}

func (h *Handler) handleAddMembership(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id", "user_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in struct {
		WorkspaceID      uuid.UUID   `json:"workspaceId"`
		RoleKey          string      `json:"roleKey"`
		ProductIDs       []uuid.UUID `json:"productIds"`
		PermissionSetIDs []uuid.UUID `json:"permissionSetIds"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if strings.TrimSpace(in.RoleKey) == "" {
		shared.WriteError(w, r, shared.Validation(map[string]string{"roleKey": "Pick a role."}))
		return
	}
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		var isOwner bool
		if err := tx.QueryRow(r.Context(), `SELECT is_platform_owner FROM crm.identities WHERE id = $1 AND status <> 'deleted'`, id).Scan(&isOwner); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return shared.NotFound("user_not_found")
			}
			return err
		}
		if isOwner {
			return shared.Forbidden("platform_owner", "The platform owner already has access to everything.")
		}
		var status string
		if err := tx.QueryRow(r.Context(), `SELECT status FROM crm.workspaces WHERE id = $1`, in.WorkspaceID).Scan(&status); err != nil || status != "active" {
			return shared.Validation(map[string]string{"workspaceId": "Choose an active workspace."})
		}
		var mid uuid.UUID
		var mStatus string
		err := tx.QueryRow(r.Context(), `SELECT id, status FROM crm.memberships WHERE workspace_id = $1 AND identity_id = $2 FOR UPDATE`,
			in.WorkspaceID, id).Scan(&mid, &mStatus)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if err := tx.QueryRow(r.Context(), `
				INSERT INTO crm.memberships (workspace_id, identity_id, status, created_by) VALUES ($1, $2, 'active', $3) RETURNING id`,
				in.WorkspaceID, id, actorID(r)).Scan(&mid); err != nil {
				return err
			}
		case err != nil:
			return err
		case mStatus == "active" || mStatus == "suspended":
			return shared.Validation(map[string]string{"workspaceId": "They're already a member of this workspace."})
		default:
			if _, err := tx.Exec(r.Context(), `UPDATE crm.memberships SET status = 'active' WHERE id = $1`, mid); err != nil {
				return err
			}
		}
		role, pids, sets := in.RoleKey, in.ProductIDs, in.PermissionSetIDs
		if err := setMembershipAccess(r.Context(), tx, actorID(r), in.WorkspaceID, mid, &role, &pids, &sets, ""); err != nil {
			return err
		}
		if len(in.ProductIDs) == 0 {
			if _, err := tx.Exec(r.Context(), `
				UPDATE crm.role_assignments SET product_ids = COALESCE((SELECT array_agg(product_id) FROM crm.workspace_products
				  WHERE workspace_id = $1 AND status = 'active'), '{}') WHERE membership_id = $2`, in.WorkspaceID, mid); err != nil {
				return err
			}
		}
		ws := in.WorkspaceID
		return shared.WriteAudit(r.Context(), tx, auditEvent(r, "membership.created", "membership", &mid, &ws, nil,
			map[string]any{"identityId": id, "role": in.RoleKey}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	d, err := h.getUser(r.Context(), id)
	writeResult(w, r, http.StatusCreated, d, err)
}
