package records

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/plans"
	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// One write path for records (D-56): the record pages, bulk actions, CSV import, the
// public API and workflows all create, change and delete records through these
// functions, so validation, unique values, history, audit and events are the same
// whoever makes the change.

// actorInfo is who makes a change and through what.
type actorInfo struct {
	ID        *uuid.UUID // nil = the system (workflows)
	Kind      string     // identity | api_key | system
	Source    string     // ui | api | import | bulk | workflow | merge
	IP        string
	RequestID string
	// Workflow runs: which workflow made the change and how deep the chain is.
	WorkflowID *uuid.UUID
	Depth      int
}

func actorFromRequest(r *http.Request, source string) actorInfo {
	sess := identity.SessionFrom(r.Context())
	meta := identity.Meta(r)
	a := actorInfo{Kind: "identity", Source: source, IP: meta.IP, RequestID: meta.RequestID}
	if sess != nil && sess.IdentityID != uuid.Nil {
		id := sess.IdentityID
		a.ID = &id
	}
	if k := apiKeyFrom(r.Context()); k != nil {
		a.Kind, a.Source = "api_key", "api"
	}
	return a
}

func systemActor(source string) actorInfo { return actorInfo{Kind: "system", Source: source} }

func (a actorInfo) uuidOrNil() uuid.UUID {
	if a.ID == nil {
		return uuid.Nil
	}
	return *a.ID
}

func (a actorInfo) audit(ws uuid.UUID, action, entityType string, entityID *uuid.UUID, before, after any) shared.AuditEvent {
	w := ws
	return shared.AuditEvent{WorkspaceID: &w, ActorID: a.ID, ActorKind: a.Kind, Action: action, EntityType: entityType, EntityID: entityID,
		Before: before, After: after, IP: a.IP, RequestID: a.RequestID}
}

func entityName(spec *objectSpec) string { return strings.TrimSuffix(spec.Key, "s") }

// rowSnapshot is a record as events and webhooks carry it.
// Field values sit next to id, code, version and displayName (the record's name); a
// field can't replace those.
func rowSnapshot(r *Row) map[string]any {
	out := map[string]any{}
	for k, v := range r.Values {
		out[k] = v
	}
	out["id"], out["code"], out["version"], out["displayName"] = r.ID, r.Code, r.Version, r.Title
	if _, has := r.Values["title"]; !has {
		out["title"] = r.Title
	}
	return out
}

// insertActivity adds one entry to a record's timeline.
func insertActivity(ctx context.Context, tx pgx.Tx, ws uuid.UUID, object string, recordID uuid.UUID, kind, title string, detail map[string]any, actor *uuid.UUID) error {
	if detail == nil {
		detail = map[string]any{}
	}
	raw, _ := json.Marshal(detail)
	_, err := tx.Exec(ctx, `
		INSERT INTO crm.activities (workspace_id, object_key, record_id, kind, title, detail, source, actor_id)
		VALUES ($1, $2, $3, $4, $5, $6, 'crm', $7)`, ws, object, recordID, kind, title, raw, actor)
	return err
}

// checkUnique reports values of unique fields already used by another record.
func checkUnique(ctx context.Context, q querier, ws uuid.UUID, spec *objectSpec, fields []Field, cs *changeSet, exclude uuid.UUID) map[string]string {
	fe := map[string]string{}
	for _, f := range fields {
		if !f.Unique || f.inColumn() {
			continue
		}
		v, ok := cs.custom[f.Key]
		if !ok || isEmpty(v) {
			continue
		}
		var taken bool
		err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.`+spec.Table+` t WHERE t.workspace_id = $1 AND t.deleted_at IS NULL
			AND t.id <> $2 AND lower(t.custom->>'`+f.Key+`') = lower($3))`, ws, exclude, fmt.Sprint(v)).Scan(&taken)
		if err == nil && taken {
			fe[f.Key] = fmt.Sprintf("Another %s already has this %s.", strings.ToLower(spec.Singular), strings.ToLower(f.Label))
		}
	}
	return fe
}

// createRecord validates values and inserts a record (owner = values.ownerId or the actor).
func (h *Handler) createRecord(ctx context.Context, tx pgx.Tx, ws uuid.UUID, spec *objectSpec, a actorInfo, values map[string]any) (*Row, error) {
	if values == nil {
		values = map[string]any{}
	}
	// The business's plan decides how many records it may hold (D-101). Work the system
	// does on its own (workflows, the app connector) is never blocked.
	if a.Kind != "system" {
		if err := plans.CheckRecords(ctx, tx, ws); err != nil {
			return nil, err
		}
	}
	fields, err := allFields(ctx, tx, ws, spec)
	if err != nil {
		return nil, err
	}
	applyStageDefaults(ctx, tx, ws, spec, values)
	cs, fe := buildChanges(ctx, tx, ws, fields, values, true)
	for k, v := range objectRules(spec, values, map[string]any{}, true) {
		fe[k] = v
	}
	for k, v := range checkUnique(ctx, tx, ws, spec, fields, cs, uuid.Nil) {
		fe[k] = v
	}
	if len(fe) > 0 {
		return nil, shared.Validation(fe)
	}
	id, err := insertRecord(ctx, tx, ws, spec, a.ID, cs)
	if err != nil {
		return nil, err
	}
	row, _, err := h.getRow(ctx, tx, ws, spec, id, nil)
	if err != nil {
		return nil, err
	}
	if err := insertActivity(ctx, tx, ws, spec.Key, id, "record.created", "Created", map[string]any{"source": a.Source}, a.ID); err != nil {
		return nil, err
	}
	if err := shared.WriteAudit(ctx, tx, a.audit(ws, "record.created", entityName(spec), &id, nil, cs.after)); err != nil {
		return nil, err
	}
	if err := emitEvent(ctx, tx, Event{Type: "record.created", WorkspaceID: ws, Object: spec.Key, RecordID: id, ActorID: a.ID,
		Source: a.Source, Record: rowSnapshot(row), Title: row.Title, WorkflowID: a.WorkflowID, Depth: a.Depth}); err != nil {
		return nil, err
	}
	return row, nil
}

// fieldChange is one line of a record's history.
type fieldChange struct {
	Field string `json:"field"`
	Label string `json:"label"`
	From  any    `json:"from"`
	To    any    `json:"to"`
}

func displayValue(r *Row, f Field) any {
	if l, ok := r.Lookups[f.Key]; ok {
		return l.Label
	}
	if ls, ok := r.Links[f.Key]; ok {
		out := make([]string, len(ls))
		for i, l := range ls {
			out[i] = l.Label
		}
		return out
	}
	v := r.Values[f.Key]
	if f.Type == "select" {
		for _, o := range f.Options {
			if o.Value == v {
				return o.Label
			}
		}
	}
	return v
}

// updateValues changes some fields of a record. expected (optional) is the version the
// caller saw; owners limits own-scope members to records they may see.
func (h *Handler) updateValues(ctx context.Context, tx pgx.Tx, ws uuid.UUID, spec *objectSpec, id uuid.UUID, a actorInfo, values map[string]any,
	expected *int, owners []uuid.UUID) (*Row, error) {
	current, fields, err := h.getRow(ctx, tx, ws, spec, id, owners)
	if err != nil {
		return nil, err
	}
	if expected != nil && current.Version != *expected {
		return nil, shared.NewError(http.StatusConflict, "version_conflict", "Someone else changed this record. Reload to see the latest version.")
	}
	if len(values) == 0 {
		return current, nil
	}
	applyStageDefaults(ctx, tx, ws, spec, values)
	cs, fe := buildChanges(ctx, tx, ws, fields, values, false)
	for k, v := range objectRules(spec, values, current.Values, false) {
		fe[k] = v
	}
	for k, v := range checkUnique(ctx, tx, ws, spec, fields, cs, id) {
		fe[k] = v
	}
	if len(fe) > 0 {
		return nil, shared.Validation(fe)
	}
	if err := updateRecord(ctx, tx, ws, spec, id, a.ID, current.Version, cs); err != nil {
		return nil, err
	}
	after, _, err := h.getRow(ctx, tx, ws, spec, id, nil)
	if err != nil {
		return nil, err
	}
	changes := []fieldChange{}
	changed := []string{}
	previous := map[string]any{}
	for _, f := range fields {
		if _, given := values[f.Key]; !given {
			continue
		}
		before, _ := json.Marshal(current.Values[f.Key])
		now, _ := json.Marshal(after.Values[f.Key])
		if string(before) == string(now) {
			continue
		}
		changed = append(changed, f.Key)
		previous[f.Key] = current.Values[f.Key]
		changes = append(changes, fieldChange{Field: f.Key, Label: f.Label, From: displayValue(current, f), To: displayValue(after, f)})
	}
	if len(changes) == 0 {
		return after, nil
	}
	if err := insertActivity(ctx, tx, ws, spec.Key, id, "record.updated", "Updated", map[string]any{"changes": changes, "source": a.Source}, a.ID); err != nil {
		return nil, err
	}
	auditBefore := map[string]any{}
	for _, k := range changed {
		auditBefore[k] = current.Values[k]
	}
	if err := shared.WriteAudit(ctx, tx, a.audit(ws, "record.updated", entityName(spec), &id, auditBefore, cs.after)); err != nil {
		return nil, err
	}
	if err := emitEvent(ctx, tx, Event{Type: "record.updated", WorkspaceID: ws, Object: spec.Key, RecordID: id, ActorID: a.ID, Source: a.Source,
		Record: rowSnapshot(after), Previous: previous, Changed: changed, Title: after.Title, WorkflowID: a.WorkflowID, Depth: a.Depth}); err != nil {
		return nil, err
	}
	return after, nil
}

// deleteRecord moves a record to the recycle bin.
func (h *Handler) deleteRecord(ctx context.Context, tx pgx.Tx, ws uuid.UUID, spec *objectSpec, id uuid.UUID, a actorInfo, owners []uuid.UUID) error {
	row, _, err := h.getRow(ctx, tx, ws, spec, id, owners)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, "UPDATE crm."+spec.Table+" SET deleted_at = now(), deleted_by = $3, updated_by = COALESCE($3, updated_by) WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL",
		id, ws, a.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NotFound("record_not_found")
	}
	if err := insertActivity(ctx, tx, ws, spec.Key, id, "record.deleted", "Moved to the recycle bin", map[string]any{"source": a.Source}, a.ID); err != nil {
		return err
	}
	if err := shared.WriteAudit(ctx, tx, a.audit(ws, "record.deleted", entityName(spec), &id, nil, nil)); err != nil {
		return err
	}
	return emitEvent(ctx, tx, Event{Type: "record.deleted", WorkspaceID: ws, Object: spec.Key, RecordID: id, ActorID: a.ID, Source: a.Source,
		Record: rowSnapshot(row), Title: row.Title, WorkflowID: a.WorkflowID, Depth: a.Depth})
}

// restoreRecord brings a record back from the recycle bin.
func (h *Handler) restoreRecord(ctx context.Context, tx pgx.Tx, ws uuid.UUID, spec *objectSpec, id uuid.UUID, a actorInfo, owners []uuid.UUID) (*Row, error) {
	tag, err := tx.Exec(ctx, "UPDATE crm."+spec.Table+" SET deleted_at = NULL, deleted_by = NULL, version = version + 1, updated_at = now(), updated_by = COALESCE($3, updated_by)"+
		" WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NOT NULL AND ($4::uuid[] IS NULL OR owner_id = ANY($4))", id, ws, a.ID, owners)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, shared.NotFound("record_not_found")
	}
	row, _, err := h.getRow(ctx, tx, ws, spec, id, nil)
	if err != nil {
		return nil, err
	}
	if err := insertActivity(ctx, tx, ws, spec.Key, id, "record.restored", "Restored from the recycle bin", map[string]any{"source": a.Source}, a.ID); err != nil {
		return nil, err
	}
	if err := shared.WriteAudit(ctx, tx, a.audit(ws, "record.restored", entityName(spec), &id, nil, nil)); err != nil {
		return nil, err
	}
	return row, emitEvent(ctx, tx, Event{Type: "record.restored", WorkspaceID: ws, Object: spec.Key, RecordID: id, ActorID: a.ID, Source: a.Source,
		Record: rowSnapshot(row), Title: row.Title})
}

// destroyRecord deletes a record from the recycle bin for good. Links to it from other
// records are cleared (their history stays); its files, favorites and timeline go too.
func (h *Handler) destroyRecord(ctx context.Context, tx pgx.Tx, ws uuid.UUID, spec *objectSpec, id uuid.UUID, a actorInfo, owners []uuid.UUID) error {
	var title string
	err := tx.QueryRow(ctx, "SELECT "+spec.TitleSQL+" FROM crm."+spec.Table+" t WHERE t.id = $1 AND t.workspace_id = $2 AND t.deleted_at IS NOT NULL"+
		" AND ($3::uuid[] IS NULL OR t.owner_id = ANY($3)) FOR UPDATE", id, ws, owners).Scan(&title)
	if errors.Is(err, pgx.ErrNoRows) {
		return shared.NewError(http.StatusUnprocessableEntity, "not_in_recycle_bin", "Only records in the recycle bin can be deleted permanently.")
	}
	if err != nil {
		return err
	}
	// Statements with $3 get the object key; the rest only the workspace and record.
	byKey := []string{
		`DELETE FROM crm.files WHERE workspace_id = $1 AND object_key = $3 AND record_id = $2`,
		`DELETE FROM crm.message_links WHERE workspace_id = $1 AND object_key = $3 AND record_id = $2`,
		`DELETE FROM crm.activities WHERE workspace_id = $1 AND object_key = $3 AND record_id = $2`,
	}
	plain := []string{`DELETE FROM crm.favorites WHERE workspace_id = $1 AND kind = 'record' AND target_id = $2`}
	switch spec.Key {
	case "leads":
		plain = append(plain,
			`DELETE FROM crm.lead_conversions WHERE workspace_id = $1 AND lead_id = $2`,
			`UPDATE crm.activities SET lead_id = NULL WHERE workspace_id = $1 AND lead_id = $2`,
			`UPDATE crm.external_links SET lead_id = NULL WHERE workspace_id = $1 AND lead_id = $2`)
	case "accounts":
		plain = append(plain,
			`UPDATE crm.contacts SET account_id = NULL WHERE workspace_id = $1 AND account_id = $2`,
			`UPDATE crm.accounts SET parent_account_id = NULL WHERE workspace_id = $1 AND parent_account_id = $2`,
			`UPDATE crm.leads SET converted_account_id = NULL WHERE workspace_id = $1 AND converted_account_id = $2`,
			`UPDATE crm.lead_conversions SET account_id = NULL WHERE workspace_id = $1 AND account_id = $2`,
			`UPDATE crm.activities SET account_id = NULL WHERE workspace_id = $1 AND account_id = $2`,
			`UPDATE crm.external_links SET account_id = NULL WHERE workspace_id = $1 AND account_id = $2`)
	case "contacts":
		plain = append(plain,
			`UPDATE crm.leads SET converted_contact_id = NULL WHERE workspace_id = $1 AND converted_contact_id = $2`,
			`UPDATE crm.lead_conversions SET contact_id = NULL WHERE workspace_id = $1 AND contact_id = $2`,
			`UPDATE crm.activities SET contact_id = NULL WHERE workspace_id = $1 AND contact_id = $2`,
			`UPDATE crm.external_links SET contact_id = NULL WHERE workspace_id = $1 AND contact_id = $2`)
	}
	for _, sql := range byKey {
		if _, err := tx.Exec(ctx, sql, ws, id, spec.Key); err != nil {
			return err
		}
	}
	for _, sql := range plain {
		if _, err := tx.Exec(ctx, sql, ws, id); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, "DELETE FROM crm."+spec.Table+" WHERE id = $1 AND workspace_id = $2", id, ws); err != nil {
		return shared.NewError(http.StatusConflict, "in_use", "This record is still used elsewhere and can't be deleted permanently.")
	}
	if err := shared.WriteAudit(ctx, tx, a.audit(ws, "record.destroyed", entityName(spec), &id, map[string]any{"title": title}, nil)); err != nil {
		return err
	}
	return emitEvent(ctx, tx, Event{Type: "record.destroyed", WorkspaceID: ws, Object: spec.Key, RecordID: id, ActorID: a.ID, Source: a.Source, Title: title})
}

// afterRawCreate gives a record inserted directly (lead conversion) the same history and
// event as one made through createRecord.
func (h *Handler) afterRawCreate(ctx context.Context, tx pgx.Tx, ws uuid.UUID, spec *objectSpec, id uuid.UUID, actorID uuid.UUID) error {
	row, _, err := h.getRow(ctx, tx, ws, spec, id, nil)
	if err != nil {
		return err
	}
	a := &actorID
	if err := insertActivity(ctx, tx, ws, spec.Key, id, "record.created", "Created from a lead conversion", map[string]any{"source": "convert"}, a); err != nil {
		return err
	}
	return emitEvent(ctx, tx, Event{Type: "record.created", WorkspaceID: ws, Object: spec.Key, RecordID: id, ActorID: a, Source: "convert", Record: rowSnapshot(row), Title: row.Title})
}

// applyStageDefaults fills an opportunity's probability from its stage (the setup's pipeline).
func applyStageDefaults(ctx context.Context, q querier, ws uuid.UUID, spec *objectSpec, values map[string]any) {
	if spec.Key != "opportunities" {
		return
	}
	st, ok := values["status"].(string)
	if !ok || st == "" {
		return
	}
	if _, given := values["probability"]; given {
		return
	}
	if p, ok := stageProbability(ctx, q, ws, st); ok {
		values["probability"] = float64(p)
	}
}

var eventHandler *Handler

// EmitRecordEvent lets a connector that writes records directly (the app sync) raise the
// same event as a change made in the CRM, so workflows, webhooks and live pages see it.
func EmitRecordEvent(ctx context.Context, tx pgx.Tx, ws uuid.UUID, object string, id uuid.UUID, eventType, source string) error {
	h := eventHandler
	spec := specFor(object)
	if h == nil || spec == nil {
		return nil
	}
	row, _, err := h.getRow(ctx, tx, ws, spec, id, nil)
	if err != nil {
		return nil // the record isn't readable yet; nothing to announce
	}
	if eventType == "record.created" {
		if err := insertActivity(ctx, tx, ws, object, id, "record.created", "Created by the app sync", map[string]any{"source": source}, nil); err != nil {
			return err
		}
	}
	return emitEvent(ctx, tx, Event{Type: eventType, WorkspaceID: ws, Object: object, RecordID: id, Source: source, Record: rowSnapshot(row), Title: row.Title})
}

// KickEvents wakes the event worker (after a connector's sync).
func KickEvents() {
	if eventHandler != nil {
		eventHandler.bus.Kick()
	}
}
