package records

import (
	"context"
	"errors"
	"net/http"
	"time"

	"cardflow-backend/internal/crm/platform"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type RelatedRow struct {
	ID       string `json:"id"`
	Code     string `json:"code,omitempty"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	Status   string `json:"status,omitempty"`
}

type RelatedList struct {
	Key    string       `json:"key"`
	Label  string       `json:"label"`
	Object string       `json:"object"`
	Rows   []RelatedRow `json:"rows"`
}

type Conversion struct {
	ConvertedAt time.Time            `json:"convertedAt"`
	Account     *LookupValue         `json:"account,omitempty"`
	Contact     *LookupValue         `json:"contact,omitempty"`
	Workspace   *LookupValue         `json:"workspace,omitempty"`
	Invitation  *platform.Invitation `json:"invitation,omitempty"`
}

type Detail struct {
	Record     *Row          `json:"record"`
	Related    []RelatedList `json:"related"`
	Conversion *Conversion   `json:"conversion,omitempty"`
}

func (h *Handler) relatedList(ctx context.Context, key, label, object, sql string, args ...any) (RelatedList, error) {
	l := RelatedList{Key: key, Label: label, Object: object, Rows: []RelatedRow{}}
	rows, err := h.store.Pool.Query(ctx, sql, args...)
	if err != nil {
		return l, err
	}
	defer rows.Close()
	for rows.Next() {
		var r RelatedRow
		if err := rows.Scan(&r.ID, &r.Code, &r.Title, &r.Subtitle, &r.Status); err != nil {
			return l, err
		}
		l.Rows = append(l.Rows, r)
	}
	return l, rows.Err()
}

func (h *Handler) related(ctx context.Context, ws uuid.UUID, spec *objectSpec, row *Row) ([]RelatedList, error) {
	id := row.ID
	var specsList []struct{ key, label, object, sql string }
	switch spec.Key {
	case "accounts":
		specsList = []struct{ key, label, object, sql string }{
			{"contacts", "Contacts", "contacts", `
				SELECT id::text, code, COALESCE(NULLIF(trim(concat_ws(' ', first_name, last_name)), ''), code),
				       COALESCE(NULLIF(concat_ws(' · ', title, email), ''), ''), ''
				FROM crm.contacts WHERE account_id = $1 AND workspace_id = $2 AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 50`},
			{"leads", "Converted leads", "leads", `
				SELECT id::text, code, COALESCE(NULLIF(trim(concat_ws(' ', first_name, last_name)), ''), organization, code),
				       COALESCE(organization, ''), status
				FROM crm.leads WHERE converted_account_id = $1 AND workspace_id = $2 AND deleted_at IS NULL ORDER BY created_at DESC LIMIT 50`},
			{"children", "Child accounts", "accounts", `
				SELECT id::text, code, name, COALESCE(type, ''), lifecycle
				FROM crm.accounts WHERE parent_account_id = $1 AND workspace_id = $2 AND deleted_at IS NULL ORDER BY name LIMIT 50`},
			{"workspaces", "Customer workspace", "workspaces", `
				SELECT w.id::text, w.code, w.name, w.code, w.status
				FROM crm.accounts a JOIN crm.workspaces w ON w.id = a.customer_workspace_id
				WHERE a.id = $1 AND a.workspace_id = $2`},
			{"users", "Logins", "users", `
				SELECT DISTINCT i.id::text, '', i.display_name, COALESCE(e.value_normalized, ''), i.status
				FROM crm.identities i
				LEFT JOIN crm.verified_identifiers e ON e.identity_id = i.id AND e.kind = 'email' AND e.namespace = 'global'
				WHERE i.id IN (
					SELECT identity_id FROM crm.accounts WHERE id = $1 AND workspace_id = $2 AND identity_id IS NOT NULL
					UNION SELECT identity_id FROM crm.contacts WHERE account_id = $1 AND workspace_id = $2 AND identity_id IS NOT NULL AND deleted_at IS NULL
					UNION SELECT m.identity_id FROM crm.accounts a JOIN crm.memberships m ON m.workspace_id = a.customer_workspace_id
					      WHERE a.id = $1 AND a.workspace_id = $2 AND m.status <> 'revoked')`},
		}
	case "contacts":
		specsList = []struct{ key, label, object, sql string }{
			{"account", "Account", "accounts", `
				SELECT a.id::text, a.code, a.name, COALESCE(a.industry, ''), a.lifecycle
				FROM crm.contacts c JOIN crm.accounts a ON a.id = c.account_id AND a.deleted_at IS NULL
				WHERE c.id = $1 AND c.workspace_id = $2`},
			{"leads", "Converted from lead", "leads", `
				SELECT id::text, code, COALESCE(NULLIF(trim(concat_ws(' ', first_name, last_name)), ''), organization, code),
				       COALESCE(organization, ''), status
				FROM crm.leads WHERE converted_contact_id = $1 AND workspace_id = $2 AND deleted_at IS NULL`},
		}
	}
	out := []RelatedList{}
	for _, s := range specsList {
		l, err := h.relatedList(ctx, s.key, s.label, s.object, s.sql, id, ws)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	// Records of other objects that point at this one (an account's opportunities, tasks, notes…).
	sc := scopeFrom(ctx)
	linked, err := h.relatedObjects(ctx, ws.String(), spec.Key, id, func(o *objectSpec) bool { return sc == nil || sc.Can(o.Key, "read") })
	if err != nil {
		return nil, err
	}
	out = append(out, linked...)
	// Activity timeline (sign-ups, app sign-ins, tickets…) for the built-in objects.
	col := map[string]string{"leads": "lead_id", "accounts": "account_id", "contacts": "contact_id"}[spec.Key]
	if col == "" {
		return out, nil
	}
	act, err := h.relatedList(ctx, "activity", "Activity", "activities", `
		SELECT id::text, '', title, to_char(occurred_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), kind
		FROM crm.activities WHERE `+col+` = $1 AND workspace_id = $2 ORDER BY occurred_at DESC LIMIT 50`, id, ws)
	if err != nil {
		return nil, err
	}
	out = append(out, act)
	return out, nil
}

func (h *Handler) conversion(ctx context.Context, ws uuid.UUID, row *Row) (*Conversion, error) {
	var c Conversion
	var accountID, contactID, workspaceID, invitationID *uuid.UUID
	err := h.store.Pool.QueryRow(ctx, `
		SELECT lc.created_at, lc.account_id, lc.contact_id, lc.workspace_provisioned_id, lc.invitation_id
		FROM crm.lead_conversions lc WHERE lc.lead_id = $1 AND lc.workspace_id = $2 AND lc.status = 'converted'`, row.ID, ws).
		Scan(&c.ConvertedAt, &accountID, &contactID, &workspaceID, &invitationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	label := func(sql string, id *uuid.UUID, object string) *LookupValue {
		if id == nil {
			return nil
		}
		var l string
		if err := h.store.Pool.QueryRow(ctx, sql, *id).Scan(&l); err != nil {
			return nil
		}
		return &LookupValue{ID: id.String(), Label: l, Object: object}
	}
	c.Account = label(`SELECT name || ' · ' || code FROM crm.accounts WHERE id = $1`, accountID, "accounts")
	c.Contact = label(`SELECT COALESCE(NULLIF(trim(concat_ws(' ', first_name, last_name)), ''), code) || ' · ' || code FROM crm.contacts WHERE id = $1`, contactID, "contacts")
	c.Workspace = label(`SELECT name FROM crm.workspaces WHERE id = $1`, workspaceID, "workspaces")
	if invitationID != nil {
		var inv platform.Invitation
		err := h.store.Pool.QueryRow(ctx, `
			SELECT inv.id, COALESCE(e.value_normalized, ''), i.display_name, r.key, r.name,
			       CASE WHEN inv.status IN ('pending', 'delivered') AND inv.expires_at < now() THEN 'expired' ELSE inv.status END,
			       inv.expires_at, inv.created_at, inv.accepted_at
			FROM crm.invitations inv
			JOIN crm.memberships m ON m.id = inv.membership_id
			JOIN crm.identities i ON i.id = m.identity_id
			JOIN crm.roles r ON r.id = inv.intended_role_id
			LEFT JOIN crm.verified_identifiers e ON e.identity_id = i.id AND e.kind = 'email' AND e.namespace = 'global'
			WHERE inv.id = $1`, *invitationID).Scan(&inv.ID, &inv.Email, &inv.DisplayName, &inv.RoleKey, &inv.RoleName, &inv.Status,
			&inv.ExpiresAt, &inv.CreatedAt, &inv.AcceptedAt)
		if err == nil {
			c.Invitation = &inv
		}
	}
	return &c, nil
}

func (h *Handler) detail(ctx context.Context, sc *Scope, me uuid.UUID, spec *objectSpec, id uuid.UUID) (*Detail, error) {
	ws := sc.WS
	var own *uuid.UUID
	if sc.OwnOnly(spec.Key) {
		own = &me
	}
	row, _, err := h.getRow(ctx, h.store.Pool, ws, spec, id, own)
	if err != nil {
		return nil, err
	}
	d := &Detail{Record: row}
	if d.Related, err = h.related(ctx, ws, spec, row); err != nil {
		return nil, err
	}
	for _, ext := range h.extensions {
		if ext.Related == nil {
			continue
		}
		extra, err := ext.Related(ctx, sc, spec.Key, row.ID)
		if err != nil {
			return nil, err
		}
		d.Related = append(d.Related, extra...)
	}
	if !sc.Owner {
		if d.Related, err = h.filterRelated(ctx, sc, me, d.Related); err != nil {
			return nil, err
		}
	}
	if spec.Key == "leads" {
		if d.Conversion, err = h.conversion(ctx, ws, row); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// filterRelated hides related lists a member can't read and, for own-scope objects,
// the rows they don't own. Users and workspaces are owner-only.
func (h *Handler) filterRelated(ctx context.Context, sc *Scope, me uuid.UUID, lists []RelatedList) ([]RelatedList, error) {
	out := []RelatedList{}
	for _, l := range lists {
		if l.Object == "activities" {
			out = append(out, l)
			continue
		}
		if l.Object == "tickets" {
			if sc.Eff.Can("ticket", "read") {
				out = append(out, l)
			}
			continue
		}
		// The connected app's data about this person (profile, businesses, saved cards).
		if perm, ok := map[string]string{"app-users": "app_user", "app-cards": "app_user", "app-businesses": "app_business"}[l.Object]; ok {
			if sc.Eff.Can(perm, "read") {
				out = append(out, l)
			}
			continue
		}
		spec := specFor(l.Object)
		if spec == nil || !sc.Can(l.Object, "read") {
			continue
		}
		if sc.OwnOnly(l.Object) && len(l.Rows) > 0 {
			ids := make([]string, len(l.Rows))
			for i, r := range l.Rows {
				ids[i] = r.ID
			}
			rows, err := h.store.Pool.Query(ctx, `SELECT id::text FROM crm.`+spec.Table+` WHERE id = ANY($1::uuid[]) AND owner_id = $2`, ids, me)
			if err != nil {
				return nil, err
			}
			mine := map[string]bool{}
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return nil, err
				}
				mine[id] = true
			}
			rows.Close()
			kept := []RelatedRow{}
			for _, r := range l.Rows {
				if mine[r.ID] {
					kept = append(kept, r)
				}
			}
			l.Rows = kept
		}
		out = append(out, l)
	}
	return out, nil
}

func (h *Handler) handleGet(w http.ResponseWriter, r *http.Request) {
	spec, _, err := h.scope(r, "read")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("record_not_found"))
		return
	}
	d, err := h.detail(r.Context(), scopeFrom(r.Context()), actor(r), spec, id)
	respond(w, r, http.StatusOK, d, err)
}
