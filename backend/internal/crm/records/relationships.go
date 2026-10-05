package records

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Relationship engine (D-111). Any record can be related to any other record of the same
// business, with a named relationship type: a contact "also works for" a second account,
// is the "decision maker for" a deal, "attends" an appointment; an account "owns" an
// asset. Lookup fields (contacts.account_id, a deal's accountId) stay what they are — the
// primary link. This adds the many-to-many links a lookup can't hold.
//
// Both ends must be records of the business in the URL that the caller may open; a record
// of another business simply isn't found, so a link across businesses can't be made.
//
//	GET    /w/{code}/relationship-types
//	POST   /w/{code}/relationship-types                     (customize)
//	DELETE /w/{code}/relationship-types/{key}               (customize; own types only)
//	GET    /w/{code}/crm/{object}/{id}/relationships
//	POST   /w/{code}/crm/{object}/{id}/relationships        {type, targetObject, targetId, note}
//	DELETE /w/{code}/relationships/{relationshipId}

func (h *Handler) relationshipRoutes(r chi.Router) {
	r.Get("/relationship-types", h.handleListRelationshipTypes)
	r.Post("/relationship-types", h.handleCreateRelationshipType)
	r.Delete("/relationship-types/{key}", h.handleDeleteRelationshipType)
	r.Get("/crm/{object}/{id}/relationships", h.handleListRelationships)
	r.Post("/crm/{object}/{id}/relationships", h.handleCreateRelationship)
	r.Delete("/relationships/{relationshipId}", h.handleDeleteRelationship)
}

type RelationshipType struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	InverseLabel string `json:"inverseLabel"`
	SourceObject string `json:"sourceObject,omitempty"`
	TargetObject string `json:"targetObject,omitempty"`
	Cardinality  string `json:"cardinality"`
	System       bool   `json:"system"`
}

func (h *Handler) relationshipTypes(ctx context.Context, q querier, ws uuid.UUID) ([]RelationshipType, error) {
	// A business's own type wins over a built-in one with the same key.
	rows, err := q.Query(ctx, `
		SELECT DISTINCT ON (key) key, label, inverse_label, COALESCE(source_object, ''), COALESCE(target_object, ''), cardinality, workspace_id IS NULL
		FROM crm.relationship_types WHERE (workspace_id IS NULL OR workspace_id = $1) AND key <> 'contact_role'
		ORDER BY key, (workspace_id IS NULL)`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RelationshipType{}
	for rows.Next() {
		var t RelationshipType
		if err := rows.Scan(&t.Key, &t.Label, &t.InverseLabel, &t.SourceObject, &t.TargetObject, &t.Cardinality, &t.System); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (h *Handler) handleListRelationshipTypes(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	list, err := h.relationshipTypes(r.Context(), h.store.Pool, sc.WS)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": list})
}

var relKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{1,40}$`)

func (h *Handler) handleCreateRelationshipType(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !sc.CanCustomize() {
		shared.WriteError(w, r, errForbidden)
		return
	}
	var in RelationshipType
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	in.Label, in.InverseLabel = strings.TrimSpace(in.Label), strings.TrimSpace(in.InverseLabel)
	if in.Key == "" {
		in.Key = snakeKey(in.Label)
	}
	if in.Cardinality == "" {
		in.Cardinality = "many_to_many"
	}
	fe := map[string]string{}
	if !relKeyRe.MatchString(in.Key) {
		fe["key"] = "Use 2–41 lower-case letters, digits or underscores, starting with a letter."
	}
	if in.Label == "" || len(in.Label) > 60 {
		fe["label"] = "Enter a name (up to 60 characters), e.g. “Referred by”."
	}
	if in.InverseLabel == "" {
		in.InverseLabel = in.Label
	}
	if !map[string]bool{"one_to_one": true, "one_to_many": true, "many_to_one": true, "many_to_many": true}[in.Cardinality] {
		fe["cardinality"] = "Choose how many records each side may have."
	}
	for field, object := range map[string]string{"sourceObject": in.SourceObject, "targetObject": in.TargetObject} {
		if object != "" && (specFor(object) == nil || !sc.Enabled(object)) {
			fe[field] = "Choose an object of this business."
		}
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	a := actorFromRequest(r, "ui")
	err := h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.relationship_types (workspace_id, key, label, inverse_label, source_object, target_object, cardinality, created_by)
			VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), $7, $8)
			ON CONFLICT (workspace_id, key) WHERE workspace_id IS NOT NULL DO UPDATE SET label = EXCLUDED.label, inverse_label = EXCLUDED.inverse_label,
			  source_object = EXCLUDED.source_object, target_object = EXCLUDED.target_object, cardinality = EXCLUDED.cardinality, updated_at = now()`,
			sc.WS, in.Key, in.Label, in.InverseLabel, in.SourceObject, in.TargetObject, in.Cardinality, a.ID); err != nil {
			return err
		}
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "relationship_type.saved", "relationship_type", nil, nil, in))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	in.System = false
	shared.WriteJSON(w, http.StatusCreated, in)
}

func (h *Handler) handleDeleteRelationshipType(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !sc.CanCustomize() {
		shared.WriteError(w, r, errForbidden)
		return
	}
	key := chi.URLParam(r, "key")
	ctx := r.Context()
	var used int
	if err := h.store.Pool.QueryRow(ctx, `SELECT count(*) FROM crm.record_relationships WHERE workspace_id = $1 AND type_key = $2`, sc.WS, key).Scan(&used); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if used > 0 {
		shared.WriteError(w, r, shared.NewError(http.StatusConflict, "in_use", "This relationship is used by records. Remove those links first."))
		return
	}
	tag, err := h.store.Pool.Exec(ctx, `DELETE FROM crm.relationship_types WHERE workspace_id = $1 AND key = $2`, sc.WS, key)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		shared.WriteError(w, r, shared.NotFound("relationship_type_not_found"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Relationship is one link, read from the record it is shown on.
type Relationship struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Label     string    `json:"label"`     // as read from this record: "Decision maker for" / "Decision maker"
	Direction string    `json:"direction"` // out = this record is the source, in = it is the target
	Object    string    `json:"object"`    // the other record
	RecordID  string    `json:"recordId"`
	Code      string    `json:"code"`
	Title     string    `json:"title"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// relationshipsOf lists a record's links in both directions. Only links to records the
// caller may read are returned (others are neither shown nor counted).
func (h *Handler) relationshipsOf(ctx context.Context, sc *Scope, me uuid.UUID, object string, id uuid.UUID) ([]Relationship, error) {
	types, err := h.relationshipTypes(ctx, h.store.Pool, sc.WS)
	if err != nil {
		return nil, err
	}
	byKey := map[string]RelationshipType{}
	for _, t := range types {
		byKey[t.Key] = t
	}
	rows, err := h.store.Pool.Query(ctx, `
		SELECT id::text, type_key, 'out', target_object, target_id, note, created_at FROM crm.record_relationships
		WHERE workspace_id = $1 AND source_object = $2 AND source_id = $3 AND type_key <> 'contact_role'
		UNION ALL
		SELECT id::text, type_key, 'in', source_object, source_id, note, created_at FROM crm.record_relationships
		WHERE workspace_id = $1 AND target_object = $2 AND target_id = $3 AND type_key <> 'contact_role'
		ORDER BY 7`, sc.WS, object, id)
	if err != nil {
		return nil, err
	}
	all := []Relationship{}
	ids := map[string][]uuid.UUID{}
	for rows.Next() {
		var rel Relationship
		var other uuid.UUID
		if err := rows.Scan(&rel.ID, &rel.Type, &rel.Direction, &rel.Object, &other, &rel.Note, &rel.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		rel.RecordID = other.String()
		t := byKey[rel.Type]
		rel.Label = t.Label
		if rel.Direction == "in" {
			rel.Label = t.InverseLabel
		}
		if rel.Label == "" {
			rel.Label = humanizeKey(rel.Type)
		}
		all = append(all, rel)
		ids[rel.Object] = append(ids[rel.Object], other)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Titles of the other records: one query per object, with the caller's own row scope.
	type head struct{ code, title string }
	heads := map[string]head{}
	for obj, list := range ids {
		spec := specFor(obj)
		if spec == nil || !sc.Enabled(obj) || !sc.Can(obj, "read") {
			continue
		}
		q, err := h.store.Pool.Query(ctx, `SELECT t.id::text, t.code, `+spec.TitleSQL+` FROM crm.`+spec.Table+` t
			WHERE t.workspace_id = $1 AND t.id = ANY($2) AND t.deleted_at IS NULL AND ($3::uuid[] IS NULL OR t.owner_id = ANY($3))`,
			sc.WS, list, sc.OwnersFor(obj, me))
		if err != nil {
			return nil, err
		}
		for q.Next() {
			var rid string
			var x head
			if err := q.Scan(&rid, &x.code, &x.title); err != nil {
				q.Close()
				return nil, err
			}
			heads[obj+"/"+rid] = x
		}
		q.Close()
	}
	out := []Relationship{}
	for _, rel := range all {
		x, ok := heads[rel.Object+"/"+rel.RecordID]
		if !ok {
			continue
		}
		rel.Code, rel.Title = x.code, x.title
		out = append(out, rel)
	}
	return out, nil
}

// recordScope resolves {object}/{id} in the URL to a record the caller may read.
func (h *Handler) recordScope(r *http.Request, need string) (*Scope, *objectSpec, *Row, error) {
	spec, _, err := h.scope(r, need)
	if err != nil {
		return nil, nil, nil, err
	}
	sc := scopeFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return nil, nil, nil, shared.NotFound("record_not_found")
	}
	row, _, err := h.getRow(r.Context(), h.store.Pool, sc.WS, spec, id, ownerFilter(r, spec))
	return sc, spec, row, err
}

func (h *Handler) handleListRelationships(w http.ResponseWriter, r *http.Request) {
	sc, spec, row, err := h.recordScope(r, "read")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	list, err := h.relationshipsOf(r.Context(), sc, actor(r), spec.Key, row.uuid())
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": list, "canEdit": sc.Can(spec.Key, "update")})
}

func (h *Handler) handleCreateRelationship(w http.ResponseWriter, r *http.Request) {
	sc, spec, source, err := h.recordScope(r, "update")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in struct {
		Type         string `json:"type"`
		TargetObject string `json:"targetObject"`
		TargetID     string `json:"targetId"`
		Note         string `json:"note"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	me := actor(r)
	if in.Type == "" {
		in.Type = "related_to"
	}
	fe := map[string]string{}
	types, err := h.relationshipTypes(ctx, h.store.Pool, sc.WS)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var typ *RelationshipType
	for i := range types {
		if types[i].Key == in.Type {
			typ = &types[i]
		}
	}
	targetSpec := specFor(in.TargetObject)
	targetID, idErr := uuid.Parse(in.TargetID)
	switch {
	case typ == nil || typ.Key == "contact_role": // contact roles have their own API (D-122)
		fe["type"] = "Choose a relationship."
	case targetSpec == nil || !sc.Enabled(in.TargetObject) || idErr != nil:
		fe["targetId"] = "Choose a record to link."
	case typ.SourceObject != "" && typ.SourceObject != spec.Key:
		fe["type"] = "This relationship starts from " + typ.SourceObject + "."
	case typ.TargetObject != "" && typ.TargetObject != in.TargetObject:
		fe["type"] = "This relationship points to " + typ.TargetObject + "."
	case in.TargetObject == spec.Key && targetID == source.uuid():
		fe["targetId"] = "A record can't be related to itself."
	}
	if len(in.Note) > 500 {
		fe["note"] = "Use at most 500 characters."
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	// The other end: a record of THIS business the caller may open. A record of another
	// business is not found here, whatever id is sent.
	if !sc.Can(in.TargetObject, "read") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	if _, _, err := h.getRow(ctx, h.store.Pool, sc.WS, targetSpec, targetID, sc.OwnersFor(in.TargetObject, me)); err != nil {
		shared.WriteError(w, r, shared.NotFound("record_not_found"))
		return
	}
	a := actorFromRequest(r, "ui")
	var id uuid.UUID
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		// How many each side may have.
		oneTarget := typ.Cardinality == "one_to_one" || typ.Cardinality == "many_to_one" // the source may have one target
		oneSource := typ.Cardinality == "one_to_one" || typ.Cardinality == "one_to_many" // the target may have one source
		var busy bool
		if oneTarget {
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.record_relationships WHERE workspace_id = $1 AND type_key = $2 AND source_object = $3 AND source_id = $4)`,
				sc.WS, typ.Key, spec.Key, source.uuid()).Scan(&busy); err != nil {
				return err
			}
			if busy {
				return shared.Validation(map[string]string{"type": "This record already has a “" + typ.Label + "” link. Remove it first."})
			}
		}
		if oneSource {
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.record_relationships WHERE workspace_id = $1 AND type_key = $2 AND target_object = $3 AND target_id = $4)`,
				sc.WS, typ.Key, in.TargetObject, targetID).Scan(&busy); err != nil {
				return err
			}
			if busy {
				return shared.Validation(map[string]string{"targetId": "That record already has a “" + typ.InverseLabel + "” link."})
			}
		}
		err := tx.QueryRow(ctx, `
			INSERT INTO crm.record_relationships (workspace_id, type_key, source_object, source_id, target_object, target_id, note, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT DO NOTHING RETURNING id`,
			sc.WS, typ.Key, spec.Key, source.uuid(), in.TargetObject, targetID, strings.TrimSpace(in.Note), me).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.Validation(map[string]string{"targetId": "These two records are already linked this way."})
		}
		if err != nil {
			return err
		}
		sid := source.uuid()
		if err := insertActivity(ctx, tx, sc.WS, spec.Key, sid, "relationship.added", "Linked: "+typ.Label, map[string]any{"object": in.TargetObject, "recordId": targetID.String()}, a.ID); err != nil {
			return err
		}
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "relationship.created", "relationship", &id, nil,
			map[string]any{"type": typ.Key, "source": spec.Key + "/" + sid.String(), "target": in.TargetObject + "/" + targetID.String()}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	list, err := h.relationshipsOf(ctx, sc, me, spec.Key, source.uuid())
	respond(w, r, http.StatusCreated, map[string]any{"data": list, "id": id.String()}, err)
}

func (h *Handler) handleDeleteRelationship(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	ctx := r.Context()
	me := actor(r)
	id, err := uuid.Parse(chi.URLParam(r, "relationshipId"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("relationship_not_found"))
		return
	}
	var typeKey, sObj, tObj string
	var sID, tID uuid.UUID
	err = h.store.Pool.QueryRow(ctx, `SELECT type_key, source_object, source_id, target_object, target_id FROM crm.record_relationships WHERE id = $1 AND workspace_id = $2`,
		id, sc.WS).Scan(&typeKey, &sObj, &sID, &tObj, &tID)
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("relationship_not_found"))
		return
	}
	// Removing a link needs edit rights on one of its two records (and sight of it).
	allowed := false
	for _, end := range []struct {
		obj string
		id  uuid.UUID
	}{{sObj, sID}, {tObj, tID}} {
		spec := specFor(end.obj)
		if spec == nil || !sc.Enabled(end.obj) || !sc.Can(end.obj, "update") {
			continue
		}
		if _, _, err := h.getRow(ctx, h.store.Pool, sc.WS, spec, end.id, sc.OwnersFor(end.obj, me)); err == nil {
			allowed = true
		}
	}
	if !allowed {
		shared.WriteError(w, r, shared.NotFound("relationship_not_found"))
		return
	}
	a := actorFromRequest(r, "ui")
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM crm.record_relationships WHERE id = $1 AND workspace_id = $2`, id, sc.WS); err != nil {
			return err
		}
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "relationship.deleted", "relationship", &id,
			map[string]any{"type": typeKey, "source": sObj + "/" + sID.String(), "target": tObj + "/" + tID.String()}, nil))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
