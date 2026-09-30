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

type UserSummary struct {
	ID              uuid.UUID  `json:"id"`
	DisplayName     string     `json:"displayName"`
	Email           string     `json:"email,omitempty"`
	Phone           string     `json:"phone,omitempty"`
	Status          string     `json:"status"`
	IsPlatformOwner bool       `json:"isPlatformOwner"`
	MFAEnrolled     bool       `json:"mfaEnrolled"`
	HasPassword     bool       `json:"hasPassword"`
	LastLoginAt     *time.Time `json:"lastLoginAt,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	WorkspaceNames  []string   `json:"workspaceNames"`
}

type UserMembership struct {
	ID                  uuid.UUID         `json:"id"`
	WorkspaceID         uuid.UUID         `json:"workspaceId"`
	WorkspaceCode       string            `json:"workspaceCode"`
	WorkspaceName       string            `json:"workspaceName"`
	IsPlatformWorkspace bool              `json:"isPlatformWorkspace"`
	Status              string            `json:"status"`
	RoleKey             string            `json:"roleKey,omitempty"`
	RoleName            string            `json:"roleName,omitempty"`
	CreatedAt           time.Time         `json:"createdAt"`
	ProductIDs          []uuid.UUID       `json:"productIds"`
	PermissionSets      []nameRef         `json:"permissionSets"`
	Effective           *access.Effective `json:"effective"`
}

type LinkedRecord struct {
	Object string    `json:"object"`
	ID     uuid.UUID `json:"id"`
	Code   string    `json:"code"`
	Name   string    `json:"name"`
}

type UserInvitation struct {
	Invitation
	WorkspaceName string `json:"workspaceName"`
}

type UserDetail struct {
	UserSummary
	Locale         string           `json:"locale"`
	Timezone       string           `json:"timezone"`
	EmailVerified  bool             `json:"emailVerified"`
	PhoneVerified  bool             `json:"phoneVerified"`
	ActiveSessions int              `json:"activeSessions"`
	Memberships    []UserMembership `json:"memberships"`
	LinkedRecords  []LinkedRecord   `json:"linkedRecords"`
	Invitations    []UserInvitation `json:"invitations"`
	RecentActivity []AuditEntry     `json:"recentActivity"`
}

const userSummarySelect = `
	SELECT i.id, i.display_name, COALESCE(e.value_normalized, ''), COALESCE(p.value_normalized, ''), i.status,
	       i.is_platform_owner,
	       EXISTS (SELECT 1 FROM crm.mfa_methods mm WHERE mm.identity_id = i.id AND mm.confirmed_at IS NOT NULL),
	       EXISTS (SELECT 1 FROM crm.password_credentials pc WHERE pc.identity_id = i.id),
	       i.last_login_at, i.created_at,
	       COALESCE((SELECT array_agg(CASE WHEN w.is_platform THEN 'Your team' ELSE w.name END ORDER BY w.is_platform DESC, w.name)
	                  FROM crm.memberships m JOIN crm.workspaces w ON w.id = m.workspace_id
	                  WHERE m.identity_id = i.id AND m.status IN ('active', 'invited') AND NOT i.is_platform_owner), '{}')
	FROM crm.identities i
	LEFT JOIN crm.verified_identifiers e ON e.identity_id = i.id AND e.kind = 'email' AND e.namespace = 'global'
	LEFT JOIN crm.verified_identifiers p ON p.identity_id = i.id AND p.kind = 'phone' AND p.namespace = 'global'`

func scanUserSummary(row pgx.Row) (UserSummary, error) {
	var u UserSummary
	err := row.Scan(&u.ID, &u.DisplayName, &u.Email, &u.Phone, &u.Status, &u.IsPlatformOwner, &u.MFAEnrolled,
		&u.HasPassword, &u.LastLoginAt, &u.CreatedAt, &u.WorkspaceNames)
	return u, err
}

func (h *Handler) handleListUsers(w http.ResponseWriter, r *http.Request) {
	q := likePattern(r.URL.Query().Get("q"))
	status := r.URL.Query().Get("status")
	if status != "active" && status != "suspended" && status != "invited" {
		status = ""
	}
	limit := queryInt(r, "limit", 50, 1, 200)
	offset := queryInt(r, "offset", 0, 0, 1_000_000)
	// The owner's Product / App filter (D-74, D-86): only people in the chosen products,
	// the same set the Overview's Users and Invitations cards count.
	f := ownerFilterFrom(r)
	on := f.on()
	where := `
		WHERE i.status <> 'deleted' AND ($1 = '' OR i.display_name ILIKE $1 OR e.value_normalized ILIKE $1 OR p.value_normalized ILIKE $1)
		  AND ($2 = '' OR $2 = 'invited' OR i.status = $2)
		  AND (NOT $5 OR EXISTS (SELECT 1 FROM crm.memberships fm WHERE fm.identity_id = i.id AND fm.status <> 'revoked' AND fm.workspace_id IN (` + filteredWorkspacesAt(3, 4) + `)))
		  AND ($2 <> 'invited' OR EXISTS (SELECT 1 FROM crm.invitations iv JOIN crm.memberships im ON im.id = iv.membership_id
		        WHERE im.identity_id = i.id AND iv.status IN ('pending', 'delivered') AND iv.expires_at > now()
		          AND (NOT $5 OR iv.workspace_id IN (` + filteredWorkspacesAt(3, 4) + `))))`
	var total int
	if err := h.store.Pool.QueryRow(r.Context(), `
		SELECT count(*) FROM crm.identities i
		LEFT JOIN crm.verified_identifiers e ON e.identity_id = i.id AND e.kind = 'email' AND e.namespace = 'global'
		LEFT JOIN crm.verified_identifiers p ON p.identity_id = i.id AND p.kind = 'phone' AND p.namespace = 'global'`+where, q, status, f.Workspace, f.App, on).Scan(&total); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	rows, err := h.store.Pool.Query(r.Context(), userSummarySelect+where+`
		ORDER BY i.is_platform_owner DESC, i.created_at DESC LIMIT $6 OFFSET $7`, q, status, f.Workspace, f.App, on, limit, offset)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	list := []UserSummary{}
	for rows.Next() {
		u, err := scanUserSummary(rows)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		list = append(list, u)
	}
	writeResult(w, r, http.StatusOK, map[string]any{"data": list, "total": total}, rows.Err())
}

func (h *Handler) getUser(ctx context.Context, id uuid.UUID) (*UserDetail, error) {
	sum, err := scanUserSummary(h.store.Pool.QueryRow(ctx, userSummarySelect+` WHERE i.id = $1 AND i.status <> 'deleted'`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NotFound("user_not_found")
	}
	if err != nil {
		return nil, err
	}
	d := &UserDetail{UserSummary: sum, Memberships: []UserMembership{}, LinkedRecords: []LinkedRecord{}, Invitations: []UserInvitation{}}
	if err := h.store.Pool.QueryRow(ctx, `
		SELECT i.locale, i.timezone,
		       EXISTS (SELECT 1 FROM crm.verified_identifiers v WHERE v.identity_id = i.id AND v.kind = 'email' AND v.verified_at IS NOT NULL),
		       EXISTS (SELECT 1 FROM crm.verified_identifiers v WHERE v.identity_id = i.id AND v.kind = 'phone' AND v.verified_at IS NOT NULL),
		       (SELECT count(*) FROM crm.sessions s WHERE s.identity_id = i.id AND s.revoked_at IS NULL
		           AND s.idle_expires_at > now() AND s.absolute_expires_at > now())
		FROM crm.identities i WHERE i.id = $1`, id).Scan(&d.Locale, &d.Timezone, &d.EmailVerified, &d.PhoneVerified, &d.ActiveSessions); err != nil {
		return nil, err
	}

	rows, err := h.store.Pool.Query(ctx, `
		SELECT m.id, w.id, w.code, w.name, w.is_platform, m.status, COALESCE(r.key, ''), COALESCE(r.name, ''), m.created_at,
		       COALESCE(r.product_ids, '{}'),
		       COALESCE((SELECT json_agg(json_build_object('id', ps.id, 'name', ps.name) ORDER BY ps.name)
		                   FROM crm.membership_permission_sets mps JOIN crm.permission_sets ps ON ps.id = mps.permission_set_id
		                  WHERE mps.membership_id = m.id), '[]')
		FROM crm.memberships m JOIN crm.workspaces w ON w.id = m.workspace_id
		LEFT JOIN LATERAL (
			SELECT ro.key, ro.name, ra.product_ids FROM crm.role_assignments ra JOIN crm.roles ro ON ro.id = ra.role_id
			WHERE ra.membership_id = m.id ORDER BY ro.rank DESC LIMIT 1
		) r ON true
		WHERE m.identity_id = $1 ORDER BY w.is_platform DESC, w.name`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m UserMembership
		var sets []byte
		if err := rows.Scan(&m.ID, &m.WorkspaceID, &m.WorkspaceCode, &m.WorkspaceName, &m.IsPlatformWorkspace, &m.Status, &m.RoleKey, &m.RoleName, &m.CreatedAt,
			&m.ProductIDs, &sets); err != nil {
			rows.Close()
			return nil, err
		}
		_ = json.Unmarshal(sets, &m.PermissionSets)
		if m.IsPlatformWorkspace {
			m.WorkspaceName = "Your team · Platform CRM"
		}
		d.Memberships = append(d.Memberships, m)
	}
	rows.Close()
	for i := range d.Memberships {
		if d.Memberships[i].Effective, err = access.ForMembership(ctx, h.store.Pool, d.Memberships[i].ID); err != nil {
			return nil, err
		}
	}

	rows, err = h.store.Pool.Query(ctx, `
		SELECT 'leads', id, code, COALESCE(NULLIF(trim(concat_ws(' ', first_name, last_name)), ''), organization, code) FROM crm.leads WHERE identity_id = $1 AND deleted_at IS NULL
		UNION ALL
		SELECT 'accounts', id, code, name FROM crm.accounts WHERE identity_id = $1 AND deleted_at IS NULL
		UNION ALL
		SELECT 'contacts', id, code, COALESCE(NULLIF(trim(concat_ws(' ', first_name, last_name)), ''), code) FROM crm.contacts WHERE identity_id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var l LinkedRecord
		if err := rows.Scan(&l.Object, &l.ID, &l.Code, &l.Name); err != nil {
			rows.Close()
			return nil, err
		}
		d.LinkedRecords = append(d.LinkedRecords, l)
	}
	rows.Close()

	invs, err := h.listInvitations(ctx, `m.identity_id = $1`, id)
	if err != nil {
		return nil, err
	}
	for _, inv := range invs {
		var wsName string
		_ = h.store.Pool.QueryRow(ctx, `SELECT w.name FROM crm.invitations i JOIN crm.workspaces w ON w.id = i.workspace_id WHERE i.id = $1`, inv.ID).Scan(&wsName)
		d.Invitations = append(d.Invitations, UserInvitation{Invitation: inv, WorkspaceName: wsName})
	}

	d.RecentActivity, _, err = h.listAudit(ctx, 30, 0, &id, true)
	return d, err
}

func (h *Handler) handleGetUser(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id", "user_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	d, err := h.getUser(r.Context(), id)
	writeResult(w, r, http.StatusOK, d, err)
}

type userUpdateInput struct {
	DisplayName *string `json:"displayName"`
	Email       *string `json:"email"`
	Phone       *string `json:"phone"`
	Locale      *string `json:"locale"`
	Timezone    *string `json:"timezone"`
	Status      *string `json:"status"`
}

// upsertIdentifier sets (or clears, when value is "") the identity's global email/phone.
// Values set by the owner count as verified; they are the platform administrator.
func upsertIdentifier(ctx context.Context, tx pgx.Tx, identityID uuid.UUID, kind, value string) error {
	if value == "" {
		_, err := tx.Exec(ctx, `DELETE FROM crm.verified_identifiers WHERE identity_id = $1 AND kind = $2 AND namespace = 'global'`, identityID, kind)
		return err
	}
	var owner uuid.UUID
	err := tx.QueryRow(ctx, `SELECT identity_id FROM crm.verified_identifiers WHERE kind = $1 AND namespace = 'global' AND value_normalized = $2`,
		kind, value).Scan(&owner)
	if err == nil && owner != identityID {
		return shared.Validation(map[string]string{kind: "Another user already uses this " + kind + "."})
	}
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE crm.verified_identifiers SET value_normalized = $3, verified_at = now()
		WHERE identity_id = $1 AND kind = $2 AND namespace = 'global'`, identityID, kind, value)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		_, err = tx.Exec(ctx, `INSERT INTO crm.verified_identifiers (identity_id, kind, value_normalized, verified_at) VALUES ($1, $2, $3, now())`,
			identityID, kind, value)
	}
	return err
}

func (h *Handler) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id", "user_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in userUpdateInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		before, err := scanUserSummary(tx.QueryRow(r.Context(), userSummarySelect+` WHERE i.id = $1 AND i.status <> 'deleted'`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.NotFound("user_not_found")
		}
		if err != nil {
			return err
		}
		var locale, tz string
		if err := tx.QueryRow(r.Context(), `SELECT locale, timezone FROM crm.identities WHERE id = $1 FOR UPDATE`, id).Scan(&locale, &tz); err != nil {
			return err
		}
		name, status := before.DisplayName, before.Status
		fields := map[string]string{}
		if in.DisplayName != nil {
			name = strings.TrimSpace(*in.DisplayName)
			if name == "" || len(name) > 80 {
				fields["displayName"] = "Enter a name (up to 80 characters)."
			}
		}
		var email, phone *string
		if in.Email != nil {
			v := strings.TrimSpace(*in.Email)
			if v != "" {
				n, ok := identity.NormalizeEmail(v)
				if !ok {
					fields["email"] = "Enter a valid email address."
				}
				v = n
			} else if before.IsPlatformOwner {
				fields["email"] = "The platform owner needs an email address."
			}
			email = &v
		}
		if in.Phone != nil {
			v := strings.TrimSpace(*in.Phone)
			if v != "" {
				n, ok := identity.NormalizePhone(v)
				if !ok {
					fields["phone"] = "Enter a valid phone number."
				}
				v = n
			}
			phone = &v
		}
		if in.Locale != nil {
			if !supportedLocale[*in.Locale] {
				fields["locale"] = "Pick a supported language."
			}
			locale = *in.Locale
		}
		if in.Timezone != nil {
			if _, err := time.LoadLocation(*in.Timezone); err != nil || *in.Timezone == "" {
				fields["timezone"] = "Pick a valid timezone."
			}
			tz = *in.Timezone
		}
		if in.Status != nil {
			switch {
			case *in.Status != "active" && *in.Status != "suspended":
				fields["status"] = "Status must be active or suspended."
			case before.IsPlatformOwner && *in.Status != "active":
				fields["status"] = "The platform owner can't be suspended."
			default:
				status = *in.Status
			}
		}
		if len(fields) > 0 {
			return shared.Validation(fields)
		}
		if email != nil {
			if err := upsertIdentifier(r.Context(), tx, id, "email", *email); err != nil {
				return err
			}
		}
		if phone != nil {
			if err := upsertIdentifier(r.Context(), tx, id, "phone", *phone); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(r.Context(), `
			UPDATE crm.identities SET display_name = $2, locale = $3, timezone = $4, status = $5, updated_at = now() WHERE id = $1`,
			id, name, locale, tz, status); err != nil {
			return err
		}
		if status == "suspended" && before.Status != "suspended" {
			if _, err := tx.Exec(r.Context(), `UPDATE crm.sessions SET revoked_at = now() WHERE identity_id = $1 AND revoked_at IS NULL`, id); err != nil {
				return err
			}
		}
		after := map[string]any{"displayName": name, "locale": locale, "timezone": tz, "status": status}
		if email != nil {
			after["email"] = *email
		}
		if phone != nil {
			after["phone"] = *phone
		}
		return shared.WriteAudit(r.Context(), tx, auditEvent(r, "identity.updated", "identity", &id, nil,
			map[string]any{"displayName": before.DisplayName, "email": before.Email, "phone": before.Phone, "status": before.Status}, after))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	d, err := h.getUser(r.Context(), id)
	writeResult(w, r, http.StatusOK, d, err)
}

func (h *Handler) handleUserPasswordReset(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id", "user_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	link, err := h.identity.IssuePasswordReset(r.Context(), id, actorID(r), identity.Meta(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	resp := map[string]string{"message": "Password reset link sent."}
	if h.cfg.IsLocalOrDev() {
		resp["devResetUrl"] = link
	}
	shared.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) userAction(w http.ResponseWriter, r *http.Request, action string, fn func(ctx context.Context, tx pgx.Tx, id uuid.UUID) error) {
	id, err := idParam(r, "id", "user_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM crm.identities WHERE id = $1 AND status <> 'deleted')`, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return shared.NotFound("user_not_found")
		}
		if err := fn(r.Context(), tx, id); err != nil {
			return err
		}
		return shared.WriteAudit(r.Context(), tx, auditEvent(r, action, "identity", &id, nil, nil, nil))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	d, err := h.getUser(r.Context(), id)
	writeResult(w, r, http.StatusOK, d, err)
}

func (h *Handler) handleUserRevokeSessions(w http.ResponseWriter, r *http.Request) {
	self := identity.SessionFrom(r.Context())
	h.userAction(w, r, "identity.sessions_revoked", func(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
		// Keep the owner's current session when they sign themselves out elsewhere.
		_, err := tx.Exec(ctx, `UPDATE crm.sessions SET revoked_at = now() WHERE identity_id = $1 AND revoked_at IS NULL AND id <> $2`, id, self.ID)
		return err
	})
}

func (h *Handler) handleUserResetMFA(w http.ResponseWriter, r *http.Request) {
	self := identity.SessionFrom(r.Context())
	h.userAction(w, r, "identity.mfa_reset", func(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
		if _, err := tx.Exec(ctx, `DELETE FROM crm.mfa_methods WHERE identity_id = $1`, id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE crm.sessions SET revoked_at = now() WHERE identity_id = $1 AND revoked_at IS NULL AND id <> $2`, id, self.ID)
		return err
	})
}

func (h *Handler) handleUpdateMembership(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id", "user_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
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
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		var wsID uuid.UUID
		var status string
		var isOwner bool
		err := tx.QueryRow(r.Context(), `
			SELECT m.workspace_id, m.status, i.is_platform_owner FROM crm.memberships m JOIN crm.identities i ON i.id = m.identity_id
			WHERE m.id = $1 AND m.identity_id = $2 FOR UPDATE OF m`, mid, id).Scan(&wsID, &status, &isOwner)
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.NotFound("membership_not_found")
		}
		if err != nil {
			return err
		}
		if isOwner {
			return shared.Forbidden("platform_owner", "The platform owner's access can't be changed.")
		}
		after := map[string]any{}
		if in.Status != nil {
			if *in.Status != "active" && *in.Status != "suspended" {
				return shared.Validation(map[string]string{"status": "Status must be active or suspended."})
			}
			if status == "invited" || status == "revoked" {
				return shared.Validation(map[string]string{"status": "Only accepted memberships can be suspended or reactivated."})
			}
			if _, err := tx.Exec(r.Context(), `UPDATE crm.memberships SET status = $2, auth_version = auth_version + 1 WHERE id = $1`, mid, *in.Status); err != nil {
				return err
			}
			after["status"] = *in.Status
		}
		if in.RoleKey != nil || in.ProductIDs != nil || in.PermissionSetIDs != nil {
			if err := setMembershipAccess(r.Context(), tx, actorID(r), wsID, mid, in.RoleKey, in.ProductIDs, in.PermissionSetIDs, ""); err != nil {
				return err
			}
			after["roleKey"], after["productIds"], after["permissionSetIds"] = in.RoleKey, in.ProductIDs, in.PermissionSetIDs
		}
		if in.Status != nil && *in.Status == "suspended" {
			// Access is decided per request from memberships, but end sessions so the change is immediate.
			if _, err := tx.Exec(r.Context(), `UPDATE crm.sessions SET revoked_at = now() WHERE identity_id = $1 AND revoked_at IS NULL`, id); err != nil {
				return err
			}
		}
		return shared.WriteAudit(r.Context(), tx, auditEvent(r, "membership.updated", "membership", &mid, &wsID, map[string]any{"status": status}, after))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	d, err := h.getUser(r.Context(), id)
	writeResult(w, r, http.StatusOK, d, err)
}
