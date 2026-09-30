package records

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Saved views (D-54): a named list of an object — table, kanban board or calendar — with
// its filter, sorts, columns and grouping. Anyone saves personal views; people who
// customize the workspace also share views with everyone. Favorites pin records and
// views to a person's sidebar.

type ViewDefinition struct {
	Filter        *FilterNode    `json:"filter,omitempty"`
	Sorts         []SortSpec     `json:"sorts,omitempty"`
	Columns       []string       `json:"columns,omitempty"`
	ColumnWidths  map[string]int `json:"columnWidths,omitempty"`
	GroupBy       string         `json:"groupBy,omitempty"`       // grouped table / kanban column field
	CalendarField string         `json:"calendarField,omitempty"` // calendar view date field
	CalendarMode  string         `json:"calendarMode,omitempty"`  // month | week | day
	Aggregates    []string       `json:"aggregates,omitempty"`    // "amount:sum"
	Q             string         `json:"q,omitempty"`
	Density       string         `json:"density,omitempty"` // comfortable | compact
}

type View struct {
	ID         uuid.UUID      `json:"id"`
	Object     string         `json:"object"`
	Name       string         `json:"name"`
	Kind       string         `json:"kind"`
	Visibility string         `json:"visibility"`
	OwnerID    *uuid.UUID     `json:"ownerId,omitempty"`
	OwnerName  string         `json:"ownerName,omitempty"`
	Definition ViewDefinition `json:"definition"`
	Position   int            `json:"position"`
	CanEdit    bool           `json:"canEdit"`
	UpdatedAt  time.Time      `json:"updatedAt"`
}

type viewInput struct {
	Name       *string         `json:"name"`
	Kind       *string         `json:"kind"`
	Visibility *string         `json:"visibility"`
	Definition *ViewDefinition `json:"definition"`
	Position   *int            `json:"position"`
}

func canEditView(sc *Scope, me uuid.UUID, v *View) bool {
	if v.Visibility == "shared" {
		return sc.CanCustomize()
	}
	return v.OwnerID != nil && *v.OwnerID == me
}

// validateViewDef checks a definition against the fields the requester can see.
func (h *Handler) validateViewDef(ctx context.Context, ws uuid.UUID, spec *objectSpec, kind string, d *ViewDefinition, env filterEnv) error {
	fields, err := allFields(ctx, h.store.Pool, ws, spec)
	if err != nil {
		return err
	}
	byKey := map[string]Field{}
	for _, f := range fields {
		byKey[f.Key] = f
	}
	fe := map[string]string{}
	if d.Filter != nil {
		if _, errs := compileFilter(d.Filter, fields, env, &sqlBuilder{}); len(errs) > 0 {
			for k, v := range errs {
				fe["definition."+k] = v
			}
		}
	}
	if _, errs := orderSQL(d.Sorts, fields, spec); len(errs) > 0 {
		for k, v := range errs {
			fe["definition."+k] = v
		}
	}
	cols := []string{}
	for _, c := range d.Columns {
		if _, ok := byKey[c]; ok && !contains(cols, c) && len(cols) < 40 {
			cols = append(cols, c)
		}
	}
	d.Columns = cols
	for k := range d.ColumnWidths {
		if _, ok := byKey[k]; !ok && k != "title" {
			delete(d.ColumnWidths, k)
		} else if d.ColumnWidths[k] < 60 || d.ColumnWidths[k] > 800 {
			delete(d.ColumnWidths, k)
		}
	}
	if d.GroupBy != "" {
		f, ok := byKey[d.GroupBy]
		if !ok || (f.Type != "select" && f.Type != "boolean" && f.Type != "lookup" && f.Type != "rating") {
			fe["definition.groupBy"] = "Group by a pick-list, yes/no, rating or link field."
		}
	}
	if kind == "kanban" && d.GroupBy == "" {
		if spec.StatusField != "" {
			d.GroupBy = spec.StatusField
		} else {
			fe["definition.groupBy"] = "Pick the field whose values become the board's columns."
		}
	}
	if kind == "calendar" {
		f, ok := byKey[d.CalendarField]
		if !ok || (f.Type != "date" && f.Type != "datetime") {
			fe["definition.calendarField"] = "Pick the date field that places records on the calendar."
		}
		if d.CalendarMode != "week" && d.CalendarMode != "day" {
			d.CalendarMode = "month"
		}
	}
	if d.Density != "compact" {
		d.Density = ""
	}
	aggs := []string{}
	for _, a := range d.Aggregates {
		key, fn, _ := strings.Cut(a, ":")
		if f, ok := byKey[key]; ok && measureFns[fn] && fn != "count" && valueKind(f) == "num" && len(aggs) < 20 {
			aggs = append(aggs, key+":"+fn)
		}
	}
	d.Aggregates = aggs
	if len(d.Q) > 200 {
		d.Q = d.Q[:200]
	}
	if len(fe) > 0 {
		return shared.Validation(fe)
	}
	return nil
}

const viewSelect = `SELECT v.id, v.object_key, v.name, v.kind, v.visibility, v.owner_id, COALESCE(i.display_name, ''), v.definition, v.position, v.updated_at
	FROM crm.views v LEFT JOIN crm.identities i ON i.id = v.owner_id`

func scanView(row pgx.Row) (*View, error) {
	v := &View{}
	var raw []byte
	if err := row.Scan(&v.ID, &v.Object, &v.Name, &v.Kind, &v.Visibility, &v.OwnerID, &v.OwnerName, &raw, &v.Position, &v.UpdatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(raw, &v.Definition)
	return v, nil
}

func (h *Handler) handleListViews(w http.ResponseWriter, r *http.Request) {
	spec, ws, err := h.scope(r, "read")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	sc, me := scopeFrom(r.Context()), actor(r)
	h.ensureDefaultViews(r.Context(), ws, spec)
	rows, err := h.store.Pool.Query(r.Context(), viewSelect+` WHERE v.workspace_id = $1 AND v.object_key = $2 AND (v.visibility = 'shared' OR v.owner_id = $3)
		ORDER BY v.visibility DESC, v.position, v.created_at`, ws, spec.Key, me)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	out := []*View{}
	for rows.Next() {
		v, err := scanView(rows)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		v.CanEdit = canEditView(sc, me, v)
		out = append(out, v)
	}
	respond(w, r, http.StatusOK, map[string]any{"data": out, "canShare": sc.CanCustomize()}, rows.Err())
}

func (h *Handler) applyViewInput(r *http.Request, spec *objectSpec, ws uuid.UUID, v *View, in viewInput, creating bool) error {
	sc := scopeFrom(r.Context())
	fe := map[string]string{}
	if in.Name != nil || creating {
		name := ""
		if in.Name != nil {
			name = strings.TrimSpace(*in.Name)
		}
		if name == "" || len(name) > 60 {
			fe["name"] = "Name the view (up to 60 characters)."
		}
		v.Name = name
	}
	if in.Kind != nil {
		switch *in.Kind {
		case "table", "kanban", "calendar":
			v.Kind = *in.Kind
		default:
			fe["kind"] = "Pick table, board or calendar."
		}
	}
	if v.Kind == "" {
		v.Kind = "table"
	}
	if in.Visibility != nil {
		switch *in.Visibility {
		case "shared":
			if !sc.CanCustomize() {
				fe["visibility"] = "Only people who customize the workspace can share views with everyone."
			}
			v.Visibility = "shared"
		case "personal":
			v.Visibility = "personal"
		default:
			fe["visibility"] = "Pick who sees this view."
		}
	}
	if v.Visibility == "" {
		v.Visibility = "personal"
	}
	if in.Definition != nil {
		v.Definition = *in.Definition
	}
	if in.Position != nil {
		v.Position = *in.Position
	}
	if len(fe) > 0 {
		return shared.Validation(fe)
	}
	return h.validateViewDef(r.Context(), ws, spec, v.Kind, &v.Definition, h.envFor(r.Context(), ws, actor(r)))
}

func (h *Handler) handleCreateView(w http.ResponseWriter, r *http.Request) {
	spec, ws, err := h.scope(r, "read")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in viewInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	me := actor(r)
	v := &View{Object: spec.Key, OwnerID: &me}
	if err := h.applyViewInput(r, spec, ws, v, in, true); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var count int
	_ = h.store.Pool.QueryRow(r.Context(), `SELECT count(*) FROM crm.views WHERE workspace_id = $1 AND object_key = $2 AND (visibility = 'shared' OR owner_id = $3)`,
		ws, spec.Key, me).Scan(&count)
	if count >= 100 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"name": "You have 100 views of this object — delete some first."}))
		return
	}
	raw, _ := json.Marshal(v.Definition)
	err = h.store.Pool.QueryRow(r.Context(), `INSERT INTO crm.views (workspace_id, object_key, name, kind, visibility, owner_id, definition, position)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`, ws, spec.Key, v.Name, v.Kind, v.Visibility, me, raw, count).Scan(&v.ID)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.writeViewResponse(w, r, http.StatusCreated, v.ID)
}

func (h *Handler) loadView(ctx context.Context, ws uuid.UUID, object string, id uuid.UUID) (*View, error) {
	v, err := scanView(h.store.Pool.QueryRow(ctx, viewSelect+` WHERE v.id = $1 AND v.workspace_id = $2 AND v.object_key = $3`, id, ws, object))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NotFound("view_not_found")
	}
	return v, err
}

func (h *Handler) writeViewResponse(w http.ResponseWriter, r *http.Request, status int, id uuid.UUID) {
	spec, ws, _ := h.scope(r, "read")
	v, err := h.loadView(r.Context(), ws, spec.Key, id)
	if err == nil {
		v.CanEdit = canEditView(scopeFrom(r.Context()), actor(r), v)
	}
	respond(w, r, status, v, err)
}

func (h *Handler) handleUpdateView(w http.ResponseWriter, r *http.Request) {
	spec, ws, err := h.scope(r, "read")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "viewId"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("view_not_found"))
		return
	}
	v, err := h.loadView(r.Context(), ws, spec.Key, id)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if v.Visibility == "personal" && (v.OwnerID == nil || *v.OwnerID != actor(r)) {
		shared.WriteError(w, r, shared.NotFound("view_not_found"))
		return
	}
	if !canEditView(scopeFrom(r.Context()), actor(r), v) {
		shared.WriteError(w, r, shared.Forbidden("forbidden", "Only people who customize the workspace can change shared views. Save your own copy instead."))
		return
	}
	var in viewInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if err := h.applyViewInput(r, spec, ws, v, in, false); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	raw, _ := json.Marshal(v.Definition)
	if _, err := h.store.Pool.Exec(r.Context(), `UPDATE crm.views SET name = $2, kind = $3, visibility = $4, definition = $5, position = $6, updated_at = now()
		WHERE id = $1`, id, v.Name, v.Kind, v.Visibility, raw, v.Position); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.writeViewResponse(w, r, http.StatusOK, id)
}

func (h *Handler) handleDeleteView(w http.ResponseWriter, r *http.Request) {
	spec, ws, err := h.scope(r, "read")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "viewId"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("view_not_found"))
		return
	}
	v, err := h.loadView(r.Context(), ws, spec.Key, id)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if !canEditView(scopeFrom(r.Context()), actor(r), v) {
		shared.WriteError(w, r, shared.Forbidden("forbidden", "You can't delete this view."))
		return
	}
	if _, err := h.store.Pool.Exec(r.Context(), `DELETE FROM crm.views WHERE id = $1`, id); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	_, _ = h.store.Pool.Exec(r.Context(), `DELETE FROM crm.favorites WHERE kind = 'view' AND target_id = $1`, id)
	w.WriteHeader(http.StatusNoContent)
}

// ---- favorites ----

type Favorite struct {
	ID       uuid.UUID `json:"id"`
	Kind     string    `json:"kind"`
	Object   string    `json:"object"`
	TargetID uuid.UUID `json:"targetId"`
	Label    string    `json:"label"`
	Icon     string    `json:"icon,omitempty"`
	Path     string    `json:"path"`
}

func (h *Handler) favorites(ctx context.Context, sc *Scope, me uuid.UUID) ([]Favorite, error) {
	rows, err := h.store.Pool.Query(ctx, `SELECT id, kind, object_key, target_id FROM crm.favorites WHERE workspace_id = $1 AND identity_id = $2 ORDER BY position, created_at`,
		sc.WS, me)
	if err != nil {
		return nil, err
	}
	list := []Favorite{}
	for rows.Next() {
		var f Favorite
		if err := rows.Scan(&f.ID, &f.Kind, &f.Object, &f.TargetID); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, f)
	}
	rows.Close()
	base := "/crm/w/" + sc.Code
	if sc.OwnerConsole {
		base = "/crm/owner"
	}
	out := []Favorite{}
	for _, f := range list {
		spec := specFor(f.Object)
		if spec == nil || !sc.Can(f.Object, "read") {
			continue
		}
		f.Icon = spec.Icon
		switch f.Kind {
		case "record":
			if err := h.store.Pool.QueryRow(ctx, `SELECT `+spec.TitleSQL+` FROM crm.`+spec.Table+` t WHERE t.id = $1 AND t.workspace_id = $2 AND t.deleted_at IS NULL`,
				f.TargetID, sc.WS).Scan(&f.Label); err != nil {
				continue
			}
			f.Path = base + "/" + f.Object + "/" + f.TargetID.String()
		case "view":
			if err := h.store.Pool.QueryRow(ctx, `SELECT name FROM crm.views WHERE id = $1 AND (visibility = 'shared' OR owner_id = $2)`, f.TargetID, me).Scan(&f.Label); err != nil {
				continue
			}
			f.Label = spec.Plural + ": " + f.Label
			f.Path = base + "/" + f.Object + "?view=" + f.TargetID.String()
		}
		out = append(out, f)
	}
	return out, nil
}

func (h *Handler) handleListFavorites(w http.ResponseWriter, r *http.Request) {
	list, err := h.favorites(r.Context(), scopeFrom(r.Context()), actor(r))
	respond(w, r, http.StatusOK, map[string]any{"data": list}, err)
}

func (h *Handler) handleAddFavorite(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	var in struct {
		Kind     string    `json:"kind"`
		Object   string    `json:"object"`
		TargetID uuid.UUID `json:"targetId"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	spec := specFor(in.Object)
	if spec == nil || !sc.Enabled(in.Object) || !sc.Can(in.Object, "read") || (in.Kind != "record" && in.Kind != "view") {
		shared.WriteError(w, r, shared.Validation(map[string]string{"targetId": "Pick a record or view you can open."}))
		return
	}
	var n int
	_ = h.store.Pool.QueryRow(r.Context(), `SELECT count(*) FROM crm.favorites WHERE workspace_id = $1 AND identity_id = $2`, sc.WS, actor(r)).Scan(&n)
	if n >= 50 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"targetId": "You have 50 favorites — remove one first."}))
		return
	}
	if _, err := h.store.Pool.Exec(r.Context(), `INSERT INTO crm.favorites (workspace_id, identity_id, kind, object_key, target_id, position)
		VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (workspace_id, identity_id, kind, target_id) DO NOTHING`, sc.WS, actor(r), in.Kind, in.Object, in.TargetID, n); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.handleListFavorites(w, r)
}

func (h *Handler) handleRemoveFavorite(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "favId"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("favorite_not_found"))
		return
	}
	// Either the favorite's id or the record/view id it points at.
	if _, err := h.store.Pool.Exec(r.Context(), `DELETE FROM crm.favorites WHERE workspace_id = $1 AND identity_id = $2 AND (id = $3 OR target_id = $3)`,
		sc.WS, actor(r), id); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.handleListFavorites(w, r)
}

func (h *Handler) handleReorderFavorites(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	var in struct {
		IDs []uuid.UUID `json:"ids"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	for i, id := range in.IDs {
		_, _ = h.store.Pool.Exec(r.Context(), `UPDATE crm.favorites SET position = $4 WHERE workspace_id = $1 AND identity_id = $2 AND id = $3`, sc.WS, actor(r), id, i)
	}
	h.handleListFavorites(w, r)
}
