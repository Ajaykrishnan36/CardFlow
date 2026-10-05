package records

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Contact roles and record teams (D-122, D-123) — who is involved in a record.
//
// A contact role says what a contact is on a deal, account, case or contract: decision
// maker, billing contact, requester, signatory. It is a relationship (contact → record)
// with a role, a primary flag, an active flag and dates; one row per contact per record,
// one primary contact per record.
//
// A record team is the colleagues who work a record besides its owner — the account team,
// the opportunity team — each with a team role and an access level. Being on a record's
// team lets a member who otherwise sees only their own records open it (read), change it
// (write) or delete it (full). Workspace teams (crm.teams) are something else: groups of
// people, not people on a record.
//
//	GET    /w/{code}/crm/{object}/{id}/contact-roles
//	POST   /w/{code}/crm/{object}/{id}/contact-roles      {contactId, role, isPrimary, startDate, endDate, note}
//	PATCH  /w/{code}/contact-roles/{roleId}               {role, isPrimary, isActive, startDate, endDate, note}
//	DELETE /w/{code}/contact-roles/{roleId}
//	GET    /w/{code}/crm/{object}/{id}/team
//	PUT    /w/{code}/crm/{object}/{id}/team               {identityId, teamRole, accessLevel, isPrimary}
//	DELETE /w/{code}/crm/{object}/{id}/team/{identityId}

func (h *Handler) roleTeamRoutes(r chi.Router) {
	r.Get("/crm/{object}/{id}/contact-roles", h.handleListContactRoles)
	r.Post("/crm/{object}/{id}/contact-roles", h.handleAddContactRole)
	r.Patch("/contact-roles/{roleId}", h.handleUpdateContactRole)
	r.Delete("/contact-roles/{roleId}", h.handleDeleteContactRole)
	r.Get("/crm/{object}/{id}/team", h.handleListRecordTeam)
	r.Put("/crm/{object}/{id}/team", h.handleSaveRecordTeam)
	r.Delete("/crm/{object}/{id}/team/{identityId}", h.handleRemoveRecordTeam)
}

// contactRoleNames: the roles offered per object. Any text is accepted; these are the usual ones.
var contactRoleNames = map[string][]string{
	"opportunities": {"Decision maker", "Economic buyer", "Champion", "Evaluator", "Influencer", "Technical buyer", "Business user", "Other"},
	"accounts":      {"Primary contact", "Billing contact", "Executive sponsor", "Technical contact", "Procurement", "Other"},
	"cases":         {"Requester", "Contact", "Escalation contact", "Other"},
	"contracts":     {"Signatory", "Contract owner", "Legal contact", "Billing contact", "Other"},
	"work_orders":   {"Site contact", "Requester", "Other"},
}

var recordTeamRoles = map[string][]string{
	"accounts":      {"Account executive", "Account manager", "Sales engineer", "Customer success manager", "Support lead", "Executive sponsor"},
	"opportunities": {"Sales rep", "Sales engineer", "Presales", "Executive sponsor", "Deal desk", "Partner manager"},
	"cases":         {"Support agent", "Escalation engineer", "Account manager"},
	"contracts":     {"Contract manager", "Legal", "Account manager"},
	"work_orders":   {"Dispatcher", "Technician", "Supervisor"},
}

type ContactRole struct {
	ID        string    `json:"id"`
	ContactID string    `json:"contactId"`
	Contact   string    `json:"contact"`
	Email     string    `json:"email,omitempty"`
	Phone     string    `json:"phone,omitempty"`
	Object    string    `json:"object"`
	RecordID  string    `json:"recordId"`
	Record    string    `json:"record"`
	Role      string    `json:"role"`
	Primary   bool      `json:"isPrimary"`
	Active    bool      `json:"isActive"`
	StartDate *string   `json:"startDate,omitempty"`
	EndDate   *string   `json:"endDate,omitempty"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// contactRoles lists the roles on a record — or, for a contact, the roles that contact holds.
func (h *Handler) contactRoles(ctx context.Context, sc *Scope, me uuid.UUID, object string, id uuid.UUID) ([]ContactRole, error) {
	where := `r.target_object = $2 AND r.target_id = $3`
	if object == "contacts" {
		where = `r.source_id = $3 AND $2 = 'contacts'`
	}
	rows, err := h.store.Pool.Query(ctx, `
		SELECT r.id::text, c.id::text, trim(COALESCE(c.first_name, '') || ' ' || COALESCE(c.last_name, '')), COALESCE(c.email, ''), COALESCE(c.phone, ''),
		       r.target_object, r.target_id::text, COALESCE(r.role, ''), r.is_primary, r.is_active, r.start_date::text, r.end_date::text, r.note, r.created_at
		FROM crm.record_relationships r JOIN crm.contacts c ON c.id = r.source_id AND c.workspace_id = r.workspace_id AND c.deleted_at IS NULL
		WHERE r.workspace_id = $1 AND r.type_key = 'contact_role' AND `+where+`
		  AND ($4::uuid[] IS NULL OR c.owner_id = ANY($4) OR $2 <> 'contacts')
		ORDER BY r.is_active DESC, r.is_primary DESC, r.created_at`, sc.WS, object, id, sc.OwnersFor("contacts", me))
	if err != nil {
		return nil, err
	}
	list := []ContactRole{}
	for rows.Next() {
		var x ContactRole
		if err := rows.Scan(&x.ID, &x.ContactID, &x.Contact, &x.Email, &x.Phone, &x.Object, &x.RecordID, &x.Role, &x.Primary, &x.Active, &x.StartDate, &x.EndDate, &x.Note, &x.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// On a contact's page: the records it has a role on, by name — only the ones the viewer may open.
	if object == "contacts" {
		kept := list[:0]
		for _, x := range list {
			spec := specFor(x.Object)
			rid, _ := uuid.Parse(x.RecordID)
			if spec == nil || !sc.Can(x.Object, "read") {
				continue
			}
			row, _, err := h.getRow(ctx, h.store.Pool, sc.WS, spec, rid, sc.OwnersFor(x.Object, me))
			if err != nil {
				continue
			}
			x.Record = row.Title
			kept = append(kept, x)
		}
		list = kept
	}
	return list, nil
}

func (h *Handler) handleListContactRoles(w http.ResponseWriter, r *http.Request) {
	sc, spec, row, err := h.recordScope(r, "read")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	list, err := h.contactRoles(r.Context(), sc, actor(r), spec.Key, row.uuid())
	names := contactRoleNames[spec.Key]
	if names == nil {
		names = []string{"Primary contact", "Contact", "Other"}
	}
	respond(w, r, http.StatusOK, map[string]any{"data": list, "roles": names, "canEdit": sc.Can(spec.Key, "update") && sc.Can("contacts", "read") && spec.Key != "contacts"}, err)
}

func cleanDate(p *string, fe map[string]string, field string) *string {
	if p == nil || strings.TrimSpace(*p) == "" {
		return nil
	}
	s := strings.TrimSpace(*p)
	if _, err := time.Parse("2006-01-02", s); err != nil {
		fe[field] = "Enter a date."
		return nil
	}
	return &s
}

func (h *Handler) handleAddContactRole(w http.ResponseWriter, r *http.Request) {
	sc, spec, target, err := h.recordScope(r, "update")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in struct {
		ContactID string  `json:"contactId"`
		Role      string  `json:"role"`
		Primary   bool    `json:"isPrimary"`
		StartDate *string `json:"startDate"`
		EndDate   *string `json:"endDate"`
		Note      string  `json:"note"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	me := actor(r)
	fe := map[string]string{}
	in.Role = strings.TrimSpace(in.Role)
	if in.Role == "" || len(in.Role) > 60 {
		fe["role"] = "Choose the contact's role (up to 60 characters)."
	}
	if spec.Key == "contacts" {
		fe["contactId"] = "Add the role from the deal, account, case or contract."
	}
	start, end := cleanDate(in.StartDate, fe, "startDate"), cleanDate(in.EndDate, fe, "endDate")
	if start != nil && end != nil && *end < *start {
		fe["endDate"] = "The end date can't be before the start date."
	}
	contact, perr := uuid.Parse(in.ContactID)
	if perr != nil || !sc.Can("contacts", "read") {
		fe["contactId"] = "Choose a contact."
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	// The contact must be one of this business that the caller may open: a contact of
	// another business is simply not found.
	if _, _, err := h.getRow(ctx, h.store.Pool, sc.WS, specFor("contacts"), contact, sc.OwnersFor("contacts", me)); err != nil {
		shared.WriteError(w, r, shared.NotFound("record_not_found"))
		return
	}
	a := actorFromRequest(r, "ui")
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		if in.Primary {
			if _, err := tx.Exec(ctx, `UPDATE crm.record_relationships SET is_primary = false, updated_at = now()
				WHERE workspace_id = $1 AND type_key = 'contact_role' AND target_object = $2 AND target_id = $3 AND is_primary`, sc.WS, spec.Key, target.uuid()); err != nil {
				return err
			}
		}
		var id uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO crm.record_relationships (workspace_id, type_key, source_object, source_id, target_object, target_id, note, role, is_primary, is_active, start_date, end_date, created_by)
			VALUES ($1, 'contact_role', 'contacts', $2, $3, $4, $5, $6, $7, true, $8::date, $9::date, $10)
			ON CONFLICT (workspace_id, type_key, source_object, source_id, target_object, target_id) DO NOTHING RETURNING id`,
			sc.WS, contact, spec.Key, target.uuid(), clipText(strings.TrimSpace(in.Note), 500), in.Role, in.Primary, start, end, me).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.Validation(map[string]string{"contactId": "This contact already has a role here. Change that role instead."})
		}
		if err != nil {
			return err
		}
		tid := target.uuid()
		if err := insertActivity(ctx, tx, sc.WS, spec.Key, tid, "contact_role.added", "Contact role: "+in.Role, map[string]any{"contactId": contact.String(), "primary": in.Primary}, a.ID); err != nil {
			return err
		}
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "contact_role.created", "contact_role", &id, nil,
			map[string]any{"contactId": contact.String(), "object": spec.Key, "recordId": tid.String(), "role": in.Role, "primary": in.Primary}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	list, err := h.contactRoles(ctx, sc, me, spec.Key, target.uuid())
	respond(w, r, http.StatusCreated, map[string]any{"data": list}, err)
}

// contactRoleScope loads a role and checks the caller may change the record it is on.
func (h *Handler) contactRoleScope(r *http.Request) (*Scope, uuid.UUID, string, uuid.UUID, error) {
	sc := scopeFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "roleId"))
	if err != nil {
		return nil, uuid.Nil, "", uuid.Nil, shared.NotFound("contact_role_not_found")
	}
	var object string
	var target uuid.UUID
	err = h.store.Pool.QueryRow(r.Context(), `SELECT target_object, target_id FROM crm.record_relationships WHERE id = $1 AND workspace_id = $2 AND type_key = 'contact_role'`, id, sc.WS).Scan(&object, &target)
	if err != nil {
		return nil, uuid.Nil, "", uuid.Nil, shared.NotFound("contact_role_not_found")
	}
	spec := specFor(object)
	if spec == nil || !sc.Can(object, "update") {
		return nil, uuid.Nil, "", uuid.Nil, errForbidden
	}
	if _, _, err := h.getRow(r.Context(), h.store.Pool, sc.WS, spec, target, sc.OwnersFor(object, actor(r))); err != nil {
		return nil, uuid.Nil, "", uuid.Nil, shared.NotFound("contact_role_not_found")
	}
	return sc, id, object, target, nil
}

func (h *Handler) handleUpdateContactRole(w http.ResponseWriter, r *http.Request) {
	sc, id, object, target, err := h.contactRoleScope(r)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in struct {
		Role      *string `json:"role"`
		Primary   *bool   `json:"isPrimary"`
		Active    *bool   `json:"isActive"`
		StartDate *string `json:"startDate"`
		EndDate   *string `json:"endDate"`
		Note      *string `json:"note"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	fe := map[string]string{}
	if in.Role != nil {
		*in.Role = strings.TrimSpace(*in.Role)
		if *in.Role == "" || len(*in.Role) > 60 {
			fe["role"] = "Choose the contact's role (up to 60 characters)."
		}
	}
	start, end := cleanDate(in.StartDate, fe, "startDate"), cleanDate(in.EndDate, fe, "endDate")
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	a := actorFromRequest(r, "ui")
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		if in.Primary != nil && *in.Primary {
			if _, err := tx.Exec(ctx, `UPDATE crm.record_relationships SET is_primary = false, updated_at = now()
				WHERE workspace_id = $1 AND type_key = 'contact_role' AND target_object = $2 AND target_id = $3 AND is_primary AND id <> $4`, sc.WS, object, target, id); err != nil {
				return err
			}
		}
		// A role that ended is no longer the primary one.
		if _, err := tx.Exec(ctx, `
			UPDATE crm.record_relationships SET role = COALESCE($3, role), is_active = COALESCE($5, is_active),
			  is_primary = COALESCE($4, is_primary) AND COALESCE($5, is_active),
			  start_date = CASE WHEN $6 THEN $7::date ELSE start_date END, end_date = CASE WHEN $8 THEN $9::date ELSE end_date END,
			  note = COALESCE($10, note), updated_at = now()
			WHERE id = $1 AND workspace_id = $2`, id, sc.WS, in.Role, in.Primary, in.Active, in.StartDate != nil, start, in.EndDate != nil, end, in.Note); err != nil {
			return err
		}
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "contact_role.updated", "contact_role", &id, nil, in))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	list, err := h.contactRoles(ctx, sc, actor(r), object, target)
	respond(w, r, http.StatusOK, map[string]any{"data": list}, err)
}

func (h *Handler) handleDeleteContactRole(w http.ResponseWriter, r *http.Request) {
	sc, id, object, target, err := h.contactRoleScope(r)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	a := actorFromRequest(r, "ui")
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `DELETE FROM crm.record_relationships WHERE id = $1 AND workspace_id = $2`, id, sc.WS); err != nil {
			return err
		}
		if err := insertActivity(r.Context(), tx, sc.WS, object, target, "contact_role.removed", "Contact role removed", nil, a.ID); err != nil {
			return err
		}
		return shared.WriteAudit(r.Context(), tx, a.audit(sc.WS, "contact_role.deleted", "contact_role", &id, map[string]any{"object": object, "recordId": target.String()}, nil))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- record teams

// teamMemberSQL: true when one of the given people is on the record's team. `owners` is a
// SQL placeholder holding the viewer and everyone in the roles below theirs.
func teamMemberSQL(spec *objectSpec, owners string) string {
	return `EXISTS (SELECT 1 FROM crm.record_team_members rtm WHERE rtm.workspace_id = t.workspace_id AND rtm.object_key = '` + spec.Key +
		`' AND rtm.record_id = t.id AND rtm.identity_id = ANY(` + owners + `::uuid[]))`
}

// teamMayChange: a caller who reaches a record only through its team (not as its owner or
// from a role above the owner) needs that team's write — or, to delete, full — access.
func (h *Handler) teamMayChange(ctx context.Context, q querier, ws uuid.UUID, spec *objectSpec, row *Row, owners []uuid.UUID, need string) error {
	if owners == nil {
		return nil
	}
	owner := row.id("ownerId")
	for _, o := range owners {
		if o == owner {
			return nil
		}
	}
	levels := []string{"write", "full"}
	if need == "full" {
		levels = []string{"full"}
	}
	var ok bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.record_team_members WHERE workspace_id = $1 AND object_key = $2 AND record_id = $3
		AND identity_id = ANY($4) AND access_level = ANY($5))`, ws, spec.Key, row.uuid(), owners, levels).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return shared.Forbidden("team_read_only", "You are on this record's team with read access. Ask its owner for write access to change it.")
	}
	return nil
}

type RecordTeamMember struct {
	IdentityID  string    `json:"identityId"`
	Name        string    `json:"name"`
	Email       string    `json:"email,omitempty"`
	TeamRole    string    `json:"teamRole"`
	AccessLevel string    `json:"accessLevel"`
	Primary     bool      `json:"isPrimary"`
	IsOwner     bool      `json:"isOwner,omitempty"`
	AddedAt     time.Time `json:"addedAt"`
}

func (h *Handler) recordTeam(ctx context.Context, ws uuid.UUID, object string, id uuid.UUID) ([]RecordTeamMember, error) {
	rows, err := h.store.Pool.Query(ctx, `
		SELECT m.identity_id::text, COALESCE(i.display_name, ''), COALESCE((SELECT v.value_normalized FROM crm.verified_identifiers v WHERE v.identity_id = i.id AND v.kind = 'email' ORDER BY v.verified_at NULLS LAST LIMIT 1), ''), m.team_role, m.access_level, m.is_primary, m.created_at
		FROM crm.record_team_members m JOIN crm.identities i ON i.id = m.identity_id
		WHERE m.workspace_id = $1 AND m.object_key = $2 AND m.record_id = $3 ORDER BY m.is_primary DESC, m.created_at`, ws, object, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []RecordTeamMember{}
	for rows.Next() {
		var x RecordTeamMember
		if err := rows.Scan(&x.IdentityID, &x.Name, &x.Email, &x.TeamRole, &x.AccessLevel, &x.Primary, &x.AddedAt); err != nil {
			return nil, err
		}
		list = append(list, x)
	}
	return list, rows.Err()
}

// mayManageTeam: the record's owner (or someone above them), a member with full access on
// its team, or anyone with the "Manage record teams" permission.
func (h *Handler) mayManageTeam(ctx context.Context, sc *Scope, me uuid.UUID, spec *objectSpec, row *Row) bool {
	if !sc.Can(spec.Key, "update") {
		return false
	}
	if sc.Owner || sc.Eff.HasCapability(access.CapRecordTeams) {
		return true
	}
	owner := row.id("ownerId")
	for _, o := range append([]uuid.UUID{me}, sc.Below...) {
		if o == owner {
			return true
		}
	}
	var full bool
	_ = h.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.record_team_members WHERE workspace_id = $1 AND object_key = $2 AND record_id = $3 AND identity_id = $4 AND access_level = 'full')`,
		sc.WS, spec.Key, row.uuid(), me).Scan(&full)
	return full
}

func (h *Handler) handleListRecordTeam(w http.ResponseWriter, r *http.Request) {
	sc, spec, row, err := h.recordScope(r, "read")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	list, err := h.recordTeam(r.Context(), sc.WS, spec.Key, row.uuid())
	roles := recordTeamRoles[spec.Key]
	if roles == nil {
		roles = []string{"Member"}
	}
	respond(w, r, http.StatusOK, map[string]any{"data": list, "roles": roles, "ownerId": row.text("ownerId"),
		"canEdit": h.mayManageTeam(r.Context(), sc, actor(r), spec, row)}, err)
}

func (h *Handler) handleSaveRecordTeam(w http.ResponseWriter, r *http.Request) {
	sc, spec, row, err := h.recordScope(r, "read")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	me := actor(r)
	if !h.mayManageTeam(ctx, sc, me, spec, row) {
		shared.WriteError(w, r, errForbidden)
		return
	}
	var in struct {
		IdentityID  string `json:"identityId"`
		TeamRole    string `json:"teamRole"`
		AccessLevel string `json:"accessLevel"`
		Primary     bool   `json:"isPrimary"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if in.AccessLevel == "" {
		in.AccessLevel = "read"
	}
	in.TeamRole = strings.TrimSpace(in.TeamRole)
	fe := map[string]string{}
	person, perr := uuid.Parse(in.IdentityID)
	// Only an active member of THIS business can be on its records' teams.
	var member bool
	if perr == nil {
		_ = h.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.memberships WHERE workspace_id = $1 AND identity_id = $2 AND status = 'active')`, sc.WS, person).Scan(&member)
	}
	if !member {
		fe["identityId"] = "Choose someone who works in this business."
	}
	if in.AccessLevel != "read" && in.AccessLevel != "write" && in.AccessLevel != "full" {
		fe["accessLevel"] = "Choose read, read and write, or full access."
	}
	if len(in.TeamRole) > 60 {
		fe["teamRole"] = "Use at most 60 characters."
	}
	if person.String() == row.text("ownerId") {
		fe["identityId"] = "The owner already has full access."
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	a := actorFromRequest(r, "ui")
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		if in.Primary {
			if _, err := tx.Exec(ctx, `UPDATE crm.record_team_members SET is_primary = false WHERE workspace_id = $1 AND object_key = $2 AND record_id = $3`, sc.WS, spec.Key, row.uuid()); err != nil {
				return err
			}
		}
		var before *string
		_ = tx.QueryRow(ctx, `SELECT access_level FROM crm.record_team_members WHERE workspace_id = $1 AND object_key = $2 AND record_id = $3 AND identity_id = $4`, sc.WS, spec.Key, row.uuid(), person).Scan(&before)
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.record_team_members (workspace_id, object_key, record_id, identity_id, team_role, access_level, is_primary, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (workspace_id, object_key, record_id, identity_id) DO UPDATE SET team_role = EXCLUDED.team_role, access_level = EXCLUDED.access_level,
			  is_primary = EXCLUDED.is_primary, updated_at = now()`, sc.WS, spec.Key, row.uuid(), person, in.TeamRole, in.AccessLevel, in.Primary, me); err != nil {
			var pe *pgconn.PgError
			if errors.As(err, &pe) && pe.Code == "23503" {
				return shared.Validation(map[string]string{"identityId": "Choose someone who works in this business."})
			}
			return err
		}
		rid := row.uuid()
		if before == nil {
			if err := insertActivity(ctx, tx, sc.WS, spec.Key, rid, "team.added", "Added to the team: "+in.TeamRole, map[string]any{"identityId": person.String(), "access": in.AccessLevel}, a.ID); err != nil {
				return err
			}
			h.notify(ctx, tx, sc.WS, person, "team.added", "You were added to a team", row.Title, "/crm/w/"+sc.Code+"/"+spec.Key+"/"+row.ID, a.ID)
		}
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "record_team.saved", "record_team_member", &rid, map[string]any{"accessLevel": before},
			map[string]any{"object": spec.Key, "identityId": person.String(), "teamRole": in.TeamRole, "accessLevel": in.AccessLevel, "primary": in.Primary}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	list, err := h.recordTeam(ctx, sc.WS, spec.Key, row.uuid())
	respond(w, r, http.StatusOK, map[string]any{"data": list}, err)
}

func (h *Handler) handleRemoveRecordTeam(w http.ResponseWriter, r *http.Request) {
	sc, spec, row, err := h.recordScope(r, "read")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	me := actor(r)
	person, perr := uuid.Parse(chi.URLParam(r, "identityId"))
	// Anyone may take themselves off a team; otherwise it needs the right to manage it.
	if perr != nil || (person != me && !h.mayManageTeam(ctx, sc, me, spec, row)) {
		shared.WriteError(w, r, errForbidden)
		return
	}
	a := actorFromRequest(r, "ui")
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM crm.record_team_members WHERE workspace_id = $1 AND object_key = $2 AND record_id = $3 AND identity_id = $4`, sc.WS, spec.Key, row.uuid(), person)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return shared.NotFound("team_member_not_found")
		}
		rid := row.uuid()
		if err := insertActivity(ctx, tx, sc.WS, spec.Key, rid, "team.removed", "Removed from the team", map[string]any{"identityId": person.String()}, a.ID); err != nil {
			return err
		}
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "record_team.removed", "record_team_member", &rid, map[string]any{"object": spec.Key, "identityId": person.String()}, nil))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
