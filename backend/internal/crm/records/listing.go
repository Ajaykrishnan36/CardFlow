package records

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Lists (D-54): filter trees on any field, several sort levels, footer totals, grouped
// boards (kanban), the recycle bin, and bulk actions on a selection or on everything a
// filter matches.

// querySpec is a selection of records, as a list, export or bulk action sends it.
type querySpec struct {
	Q         string      `json:"q,omitempty"`
	Status    string      `json:"status,omitempty"`
	Filter    *FilterNode `json:"filter,omitempty"`
	Sorts     []SortSpec  `json:"sorts,omitempty"`
	IDs       []uuid.UUID `json:"ids,omitempty"`
	Deleted   bool        `json:"deleted,omitempty"`
	Workspace string      `json:"workspace,omitempty"`
	App       string      `json:"app,omitempty"` // owner lists: an app of the chosen product (D-89)
}

// envFor is who "me" and "my team" are for relative filters, and the workspace's time zone.
func (h *Handler) envFor(ctx context.Context, ws, me uuid.UUID) filterEnv {
	env := filterEnv{Me: me, TZ: "Asia/Kolkata"}
	var tz string
	if err := h.store.Pool.QueryRow(ctx, `SELECT timezone FROM crm.workspaces WHERE id = $1`, ws).Scan(&tz); err == nil {
		if _, err := time.LoadLocation(tz); err == nil && tz != "" {
			env.TZ = tz
		}
	}
	rows, err := h.store.Pool.Query(ctx, `
		SELECT DISTINCT m2.identity_id FROM crm.team_members tm
		JOIN crm.memberships m ON m.id = tm.membership_id
		JOIN crm.team_members tm2 ON tm2.team_id = tm.team_id
		JOIN crm.memberships m2 ON m2.id = tm2.membership_id AND m2.status = 'active'
		WHERE m.identity_id = $1 AND tm.workspace_id = $2`, me, ws)
	if err == nil {
		for rows.Next() {
			var id uuid.UUID
			if rows.Scan(&id) == nil && id != me {
				env.Team = append(env.Team, id)
			}
		}
		rows.Close()
	}
	return env
}

// paramsFrom turns a selection into list parameters for the scope of the request.
func (h *Handler) paramsFrom(r *http.Request, spec *objectSpec, qs querySpec) (listParams, error) {
	sc := scopeFrom(r.Context())
	p := listParams{Q: strings.TrimSpace(qs.Q), Status: qs.Status, Filter: qs.Filter, Sorts: qs.Sorts, IDs: qs.IDs, Deleted: qs.Deleted}
	p.Owners = ownerFilter(r, spec)
	p.Env = h.envFor(r.Context(), sc.WS, actor(r))
	if sc.OwnerConsole {
		facets, err := h.workspaceFacets(r.Context(), spec)
		if err != nil {
			return p, err
		}
		switch sel := qs.Workspace; sel {
		case "platform":
		case "", "all":
			p.Workspaces = []uuid.UUID{}
			for _, f := range facets {
				p.Workspaces = append(p.Workspaces, uuid.MustParse(f.ID))
			}
		default:
			p.Workspaces = []uuid.UUID{}
			for _, f := range facets {
				if f.Code == sel {
					p.Workspaces = append(p.Workspaces, uuid.MustParse(f.ID))
				}
			}
			// An app narrows its product the way the Overview counts it (D-87): the product's
			// records while that app is active in it, none otherwise.
			if app, err := uuid.Parse(qs.App); err == nil && len(p.Workspaces) > 0 {
				var active bool
				if err := h.store.Pool.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM crm.workspace_products
					WHERE workspace_id = ANY($1) AND product_id = $2 AND status = 'active')`, p.Workspaces, app).Scan(&active); err != nil {
					return p, err
				}
				if !active {
					p.Workspaces = []uuid.UUID{}
				}
			}
		}
	}
	return p, nil
}

// querySpecFromURL reads a selection from query parameters (GET lists and exports).
func querySpecFromURL(r *http.Request) (querySpec, error) {
	q := r.URL.Query()
	qs := querySpec{Q: q.Get("q"), Status: q.Get("status"), Deleted: q.Get("deleted") == "1" || q.Get("deleted") == "true", Workspace: q.Get("workspace"), App: q.Get("app")}
	if raw := q.Get("filter"); raw != "" {
		var n FilterNode
		if err := json.Unmarshal([]byte(raw), &n); err != nil {
			return qs, shared.BadRequest("The filter isn't valid.")
		}
		qs.Filter = &n
	}
	if raw := q.Get("sorts"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &qs.Sorts); err != nil {
			return qs, shared.BadRequest("The sort isn't valid.")
		}
	} else if s := q.Get("sort"); s != "" {
		dir := "desc"
		if q.Get("dir") == "asc" {
			dir = "asc"
		}
		qs.Sorts = []SortSpec{{Field: s, Dir: dir}}
	}
	if raw := q.Get("ids"); raw != "" {
		for _, s := range strings.Split(raw, ",") {
			if id, err := uuid.Parse(strings.TrimSpace(s)); err == nil {
				qs.IDs = append(qs.IDs, id)
			}
		}
	}
	return qs, nil
}

// ---- list ----

func (h *Handler) handleList(w http.ResponseWriter, r *http.Request) {
	need := "read"
	if d := r.URL.Query().Get("deleted"); d == "1" || d == "true" {
		need = "delete" // the recycle bin is for people who may delete
	}
	spec, ws, err := h.scope(r, need)
	if err == nil {
		for _, x := range h.extensions {
			if x.BeforeList != nil {
				x.BeforeList(ws, spec.Key)
			}
		}
	}
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	qs, err := querySpecFromURL(r)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if len(qs.Sorts) == 0 && !qs.Deleted {
		qs.Sorts = []SortSpec{{Field: "createdAt", Dir: "desc"}}
	}
	p, err := h.paramsFrom(r, spec, qs)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	p.Limit = queryInt(r, "limit", 50, 1, 200)
	p.Offset = queryInt(r, "offset", 0, 0, 1_000_000)
	resp := map[string]any{}
	if sc := scopeFrom(r.Context()); sc.OwnerConsole {
		facets, err := h.workspaceFacets(r.Context(), spec)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		resp["workspaces"] = facets
	}
	rows, total, err := h.list(r.Context(), ws, spec, p)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	resp["data"], resp["total"] = rows, total
	if raw := r.URL.Query().Get("agg"); raw != "" {
		agg, err := h.aggregates(r.Context(), ws, spec, p, raw)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		resp["aggregates"] = agg
	}
	if p.Deleted {
		if err := h.attachDeletedBy(r.Context(), spec, rows); err != nil {
			shared.WriteError(w, r, err)
			return
		}
	}
	shared.WriteJSON(w, http.StatusOK, resp)
}

// aggregates computes footer totals: "amount:sum,probability:avg".
func (h *Handler) aggregates(ctx context.Context, ws uuid.UUID, spec *objectSpec, p listParams, raw string) (map[string]map[string]float64, error) {
	fields, err := allFields(ctx, h.store.Pool, ws, spec)
	if err != nil {
		return nil, err
	}
	byKey := map[string]Field{}
	for _, f := range fields {
		byKey[f.Key] = f
	}
	type want struct{ field, fn string }
	var wants []want
	cols := []string{}
	for _, part := range strings.Split(raw, ",") {
		key, fn, _ := strings.Cut(strings.TrimSpace(part), ":")
		f, ok := byKey[key]
		if !ok || !measureFns[fn] || fn == "count" || valueKind(f) != "num" || len(wants) >= 20 {
			continue
		}
		wants = append(wants, want{key, fn})
		cols = append(cols, "COALESCE("+fn+"(("+filterExpr(f)+")::numeric), 0)::float8")
	}
	out := map[string]map[string]float64{}
	if len(wants) == 0 {
		return out, nil
	}
	b := &sqlBuilder{}
	where, fe := whereFor(spec, fields, ws, p, b)
	if len(fe) > 0 {
		return nil, shared.Validation(fe)
	}
	vals := make([]float64, len(wants))
	dest := make([]any, len(wants))
	for i := range vals {
		dest[i] = &vals[i]
	}
	if err := h.store.Pool.QueryRow(ctx, "SELECT "+strings.Join(cols, ", ")+" FROM crm."+spec.Table+" t"+where, b.args...).Scan(dest...); err != nil {
		return nil, err
	}
	for i, w := range wants {
		if out[w.field] == nil {
			out[w.field] = map[string]float64{}
		}
		out[w.field][w.fn] = vals[i]
	}
	return out, nil
}

func (h *Handler) attachDeletedBy(ctx context.Context, spec *objectSpec, rows []Row) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	q, err := h.store.Pool.Query(ctx, `SELECT t.id::text, COALESCE(i.display_name, '') FROM crm.`+spec.Table+` t
		LEFT JOIN crm.identities i ON i.id = t.deleted_by WHERE t.id = ANY($1::uuid[])`, ids)
	if err != nil {
		return err
	}
	defer q.Close()
	by := map[string]string{}
	for q.Next() {
		var id, name string
		if err := q.Scan(&id, &name); err != nil {
			return err
		}
		by[id] = name
	}
	for i := range rows {
		rows[i].Lookups["deletedBy"] = LookupValue{Label: by[rows[i].ID], Object: "users"}
	}
	return q.Err()
}

// ---- groups (kanban board, grouped lists) ----

type recordGroup struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Tone  string `json:"tone,omitempty"`
	Count int    `json:"count"`
	Rows  []Row  `json:"rows"`
}

// GET /crm/{object}/groups?field=status&limit=20 (+ the list's selection parameters)
func (h *Handler) handleGroups(w http.ResponseWriter, r *http.Request) {
	spec, ws, err := h.scope(r, "read")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	qs, err := querySpecFromURL(r)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if len(qs.Sorts) == 0 {
		qs.Sorts = []SortSpec{{Field: "updatedAt", Dir: "desc"}}
	}
	p, err := h.paramsFrom(r, spec, qs)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	fields, err := allFields(r.Context(), h.store.Pool, ws, spec)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	f, ok := findField(fields, r.URL.Query().Get("field"))
	if !ok || (f.Type != "select" && f.Type != "boolean" && f.Type != "lookup" && f.Type != "rating") {
		shared.WriteError(w, r, shared.Validation(map[string]string{"field": "Group by a pick-list, yes/no, rating or link field."}))
		return
	}
	limit := queryInt(r, "limit", 20, 0, 100)
	// Counts per value in one query.
	b := &sqlBuilder{}
	where, fe := whereFor(spec, fields, ws, p, b)
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	counts := map[string]int{}
	cq, err := h.store.Pool.Query(r.Context(), "SELECT COALESCE(("+filterExpr(f)+")::text, ''), count(*) FROM crm."+spec.Table+" t"+where+" GROUP BY 1", b.args...)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	for cq.Next() {
		var v string
		var n int
		if err := cq.Scan(&v, &n); err != nil {
			cq.Close()
			shared.WriteError(w, r, err)
			return
		}
		counts[v] = n
	}
	cq.Close()
	groups := []recordGroup{}
	switch f.Type {
	case "select":
		tones := map[string]string{}
		if f.Key == spec.StatusField {
			for _, s := range statusesFor(r.Context(), h.store.Pool, ws, spec) {
				tones[s.Value] = s.Tone
			}
		}
		known := map[string]bool{"": true}
		for _, o := range f.Options {
			groups = append(groups, recordGroup{Value: o.Value, Label: o.Label, Tone: tones[o.Value], Count: counts[o.Value]})
			known[o.Value] = true
		}
		// Values no longer in the pick-list (an older setup) still get a column.
		for v, n := range counts {
			if !known[v] && n > 0 {
				groups = append(groups, recordGroup{Value: v, Label: humanizeKey(v), Tone: "neutral", Count: n})
			}
		}
	case "boolean":
		groups = append(groups, recordGroup{Value: "true", Label: "Yes", Count: counts["true"]}, recordGroup{Value: "false", Label: "No", Count: counts["false"]})
	case "rating":
		for i := 5; i >= 1; i-- {
			v := strconv.Itoa(i)
			groups = append(groups, recordGroup{Value: v, Label: strings.Repeat("★", i), Count: counts[v]})
		}
	case "lookup":
		keys := []string{}
		for k := range counts {
			if k != "" {
				keys = append(keys, k)
			}
		}
		sort.Slice(keys, func(i, j int) bool { return counts[keys[i]] > counts[keys[j]] })
		if len(keys) > 20 {
			keys = keys[:20]
		}
		rr := make([]ResultRow, len(keys))
		for i, k := range keys {
			rr[i] = ResultRow{Key: k}
		}
		if err := h.labelGroups(r.Context(), ws, f, rr); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		for i, k := range keys {
			groups = append(groups, recordGroup{Value: k, Label: rr[i].Label, Count: counts[k]})
		}
	}
	if counts[""] > 0 || f.Type == "select" || f.Type == "lookup" {
		groups = append(groups, recordGroup{Value: "", Label: "No " + strings.ToLower(f.Label), Count: counts[""]})
	}
	for i := range groups {
		groups[i].Rows = []Row{}
		if limit == 0 || groups[i].Count == 0 {
			continue
		}
		gp := p
		cond := FilterNode{Field: f.Key, Op: "eq", Value: groups[i].Value}
		if groups[i].Value == "" {
			cond = FilterNode{Field: f.Key, Op: "empty"}
		}
		if f.Type == "boolean" && groups[i].Value == "false" {
			cond = FilterNode{Field: f.Key, Op: "eq", Value: "false"}
		}
		if p.Filter != nil {
			gp.Filter = &FilterNode{Op: "and", Filters: []FilterNode{*p.Filter, cond}}
		} else {
			gp.Filter = &FilterNode{Op: "and", Filters: []FilterNode{cond}}
		}
		gp.Limit, gp.Offset = limit, 0
		rows, _, err := h.list(r.Context(), ws, spec, gp)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		groups[i].Rows = rows
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"field": f.Key, "groups": groups})
}

// ---- bulk actions ----

type bulkInput struct {
	Action string         `json:"action"` // update | delete | restore | destroy
	Values map[string]any `json:"values,omitempty"`
	Query  querySpec      `json:"query"`
}

type bulkFailure struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	Error string `json:"error"`
}

const maxBulk = 10_000

// selectionIDs resolves a selection to record ids (at most maxBulk).
func (h *Handler) selectionIDs(ctx context.Context, ws uuid.UUID, spec *objectSpec, p listParams) ([]uuid.UUID, error) {
	fields, err := allFields(ctx, h.store.Pool, ws, spec)
	if err != nil {
		return nil, err
	}
	b := &sqlBuilder{}
	where, fe := whereFor(spec, fields, ws, p, b)
	if len(fe) > 0 {
		return nil, shared.Validation(fe)
	}
	rows, err := h.store.Pool.Query(ctx, "SELECT t.id FROM crm."+spec.Table+" t"+where+" ORDER BY t.created_at LIMIT "+strconv.Itoa(maxBulk+1), b.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	if len(out) > maxBulk {
		return nil, shared.NewError(http.StatusUnprocessableEntity, "too_many", fmt.Sprintf("Pick at most %d records at once — narrow the filter.", maxBulk))
	}
	return out, rows.Err()
}

func errMessage(err error) string {
	if e, ok := err.(*shared.Error); ok {
		if len(e.FieldErrors) > 0 {
			parts := []string{}
			for _, v := range e.FieldErrors {
				parts = append(parts, v)
			}
			sort.Strings(parts)
			return strings.Join(parts, " ")
		}
		return e.Message
	}
	return "Something went wrong."
}

func (h *Handler) handleBulk(w http.ResponseWriter, r *http.Request) {
	var in bulkInput
	if err := shared.DecodeJSONLimit(w, r, &in, 2<<20); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	need := map[string]string{"update": "update", "delete": "delete", "restore": "delete", "destroy": "destroy"}[in.Action]
	if need == "" {
		shared.WriteError(w, r, shared.Validation(map[string]string{"action": "Pick what to do with the records."}))
		return
	}
	spec, ws, err := h.scope(r, need)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if in.Action == "update" && len(in.Values) == 0 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"values": "Pick a field and a value."}))
		return
	}
	in.Query.Deleted = in.Action == "restore" || in.Action == "destroy"
	p, err := h.paramsFrom(r, spec, in.Query)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if in.Query.IDs == nil && in.Query.Filter == nil && in.Query.Q == "" && in.Query.Status == "" && !in.Query.Deleted {
		// "Everything" must be asked for explicitly with an empty group filter.
		shared.WriteError(w, r, shared.Validation(map[string]string{"query": "Select records first."}))
		return
	}
	ids, err := h.selectionIDs(r.Context(), ws, spec, p)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	a := actorFromRequest(r, "bulk")
	failures := []bulkFailure{}
	done := 0
	owners := ownerFilter(r, spec)
	for start := 0; start < len(ids); start += 200 {
		end := start + 200
		if end > len(ids) {
			end = len(ids)
		}
		err := h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
			for _, id := range ids[start:end] {
				sp, err := tx.Begin(r.Context())
				if err != nil {
					return err
				}
				switch in.Action {
				case "update":
					_, err = h.updateValues(r.Context(), sp, ws, spec, id, a, in.Values, nil, owners)
				case "delete":
					err = h.deleteRecord(r.Context(), sp, ws, spec, id, a, owners)
				case "restore":
					_, err = h.restoreRecord(r.Context(), sp, ws, spec, id, a, owners)
				case "destroy":
					err = h.destroyRecord(r.Context(), sp, ws, spec, id, a, owners)
				}
				if err != nil {
					_ = sp.Rollback(r.Context())
					if len(failures) < 200 {
						failures = append(failures, bulkFailure{ID: id.String(), Error: errMessage(err)})
					}
					continue
				}
				if err := sp.Commit(r.Context()); err != nil {
					return err
				}
				done++
			}
			return nil
		})
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
	}
	h.bus.Kick()
	shared.WriteJSON(w, http.StatusOK, map[string]any{"processed": done, "failed": len(ids) - done, "failures": failures})
}

// POST /crm/{object}/{id}/restore
func (h *Handler) handleRestore(w http.ResponseWriter, r *http.Request) {
	spec, ws, err := h.scope(r, "delete")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("record_not_found"))
		return
	}
	var row *Row
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		var err error
		row, err = h.restoreRecord(r.Context(), tx, ws, spec, id, actorFromRequest(r, "ui"), ownerFilter(r, spec))
		return err
	})
	if err == nil {
		h.bus.Kick()
	}
	respond(w, r, http.StatusOK, row, err)
}

// ---- global search (⌘K) ----

type searchGroup struct {
	Object string       `json:"object"`
	Label  string       `json:"label"`
	Icon   string       `json:"icon,omitempty"`
	Rows   []RelatedRow `json:"rows"`
}

func (h *Handler) handleSearch(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	out := []searchGroup{}
	if len(q) < 2 {
		shared.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
		return
	}
	per := queryInt(r, "limit", 5, 1, 20)
	all := []*objectSpec{&leadSpec, &accountSpec, &contactSpec}
	all = append(all, dynamicSpecs()...)
	me := actor(r)
	for _, spec := range all {
		if !sc.Enabled(spec.Key) || !sc.Can(spec.Key, "read") {
			continue
		}
		fields, err := allFields(r.Context(), h.store.Pool, sc.WS, spec)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		b := &sqlBuilder{}
		where, _ := whereFor(spec, fields, sc.WS, listParams{Q: q, Owners: sc.OwnersFor(spec.Key, me)}, b)
		status := "''"
		if spec.StatusField != "" {
			f, _ := spec.field(spec.StatusField)
			if f.inColumn() {
				status = "COALESCE(t." + f.column + ", '')"
			}
		}
		subtitle := "''"
		switch spec.Key {
		case "leads":
			subtitle = "COALESCE(NULLIF(concat_ws(' · ', t.organization, t.email), ''), '')"
		case "contacts":
			subtitle = "COALESCE(NULLIF(concat_ws(' · ', t.title, t.email), ''), '')"
		case "accounts":
			subtitle = "COALESCE(NULLIF(concat_ws(' · ', t.industry, t.phone), ''), '')"
		}
		rows, err := h.store.Pool.Query(r.Context(), "SELECT t.id::text, t.code, "+spec.TitleSQL+", "+subtitle+", "+status+" FROM crm."+spec.Table+" t"+where+
			" ORDER BY t.updated_at DESC LIMIT "+strconv.Itoa(per), b.args...)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		g := searchGroup{Object: spec.Key, Label: spec.Plural, Icon: spec.Icon, Rows: []RelatedRow{}}
		for rows.Next() {
			var rr RelatedRow
			if err := rows.Scan(&rr.ID, &rr.Code, &rr.Title, &rr.Subtitle, &rr.Status); err != nil {
				rows.Close()
				shared.WriteError(w, r, err)
				return
			}
			g.Rows = append(g.Rows, rr)
		}
		rows.Close()
		if len(g.Rows) > 0 {
			out = append(out, g)
		}
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}

// ---- duplicates & merge ----

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	if len(d) > 10 {
		d = d[len(d)-10:]
	}
	return d
}

// GET /crm/{object}/{id}/duplicates — records that look like the same person or company.
func (h *Handler) handleDuplicates(w http.ResponseWriter, r *http.Request) {
	spec, ws, err := h.scope(r, "read")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("record_not_found"))
		return
	}
	row, _, err := h.getRow(r.Context(), h.store.Pool, ws, spec, id, ownerFilter(r, spec))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	conds := []string{}
	args := []any{ws, id}
	add := func(cond string, v any) {
		args = append(args, v)
		conds = append(conds, strings.ReplaceAll(cond, "$v", "$"+strconv.Itoa(len(args))))
	}
	str := func(k string) string { s, _ := row.Values[k].(string); return strings.TrimSpace(s) }
	switch spec.Key {
	case "leads", "contacts":
		if e := str("email"); e != "" {
			add("lower(t.email) = lower($v)", e)
		}
		for _, k := range []string{"phone", "mobile"} {
			if d := digitsOnly(str(k)); len(d) >= 7 {
				add("(right(regexp_replace(COALESCE(t.phone, ''), '\\D', '', 'g'), 10) = $v OR right(regexp_replace(COALESCE(t.mobile, ''), '\\D', '', 'g'), 10) = $v)", d)
			}
		}
		if n := strings.TrimSpace(str("firstName") + " " + str("lastName")); len(n) > 2 {
			add("lower(trim(concat_ws(' ', t.first_name, t.last_name))) = lower($v)", n)
		}
	case "accounts":
		if n := str("name"); n != "" {
			add("lower(t.name) = lower($v)", n)
		}
		if e := str("email"); e != "" {
			add("lower(t.email) = lower($v)", e)
		}
		if d := digitsOnly(str("phone")); len(d) >= 7 {
			add("right(regexp_replace(COALESCE(t.phone, ''), '\\D', '', 'g'), 10) = $v", d)
		}
		if web := strings.TrimPrefix(strings.TrimPrefix(str("website"), "https://"), "http://"); web != "" {
			add("lower(regexp_replace(COALESCE(t.website, ''), '^https?://', '')) = lower($v)", web)
		}
	default:
		if n := str("name"); n != "" {
			add("lower(t.name) = lower($v)", n)
		}
	}
	out := []RelatedRow{}
	if len(conds) > 0 {
		own := ownerFilter(r, spec)
		args = append(args, own)
		rows, err := h.store.Pool.Query(r.Context(), "SELECT t.id::text, t.code, "+spec.TitleSQL+", '', '' FROM crm."+spec.Table+
			" t WHERE t.workspace_id = $1 AND t.id <> $2 AND t.deleted_at IS NULL AND ($"+strconv.Itoa(len(args))+"::uuid[] IS NULL OR t.owner_id = ANY($"+
			strconv.Itoa(len(args))+")) AND ("+strings.Join(conds, " OR ")+") ORDER BY t.created_at LIMIT 10", args...)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		for rows.Next() {
			var rr RelatedRow
			if err := rows.Scan(&rr.ID, &rr.Code, &rr.Title, &rr.Subtitle, &rr.Status); err != nil {
				rows.Close()
				shared.WriteError(w, r, err)
				return
			}
			out = append(out, rr)
		}
		rows.Close()
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}

// linkField is a field (on any object) that points at records of one object.
type linkField struct {
	spec  *objectSpec
	field Field
}

// linkFieldsTo lists every link field in a workspace that points at target.
func (h *Handler) linkFieldsTo(ctx context.Context, q querier, ws uuid.UUID, target string) ([]linkField, error) {
	all := []*objectSpec{&leadSpec, &accountSpec, &contactSpec}
	all = append(all, dynamicSpecs()...)
	out := []linkField{}
	for _, s := range all {
		fields, err := allFieldsRaw(ctx, q, ws, s)
		if err != nil {
			return nil, err
		}
		for _, f := range fields {
			if isLinkType(f.Type) && f.Lookup == target && !f.inColumn() {
				out = append(out, linkField{s, f})
			}
		}
	}
	return out, nil
}

type mergeInput struct {
	PrimaryID    uuid.UUID      `json:"primaryId"`
	DuplicateIDs []uuid.UUID    `json:"duplicateIds"`
	Values       map[string]any `json:"values"`
}

// POST /crm/{object}/merge — keep one record, move everything linked to the others onto
// it, then put the others in the recycle bin (restorable).
func (h *Handler) handleMerge(w http.ResponseWriter, r *http.Request) {
	spec, ws, err := h.scope(r, "update")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if !scopeFrom(r.Context()).Can(spec.Key, "delete") {
		shared.WriteError(w, r, shared.Forbidden("forbidden", "Merging needs permission to edit and delete these records."))
		return
	}
	var in mergeInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if len(in.DuplicateIDs) == 0 || len(in.DuplicateIDs) > 5 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"duplicateIds": "Pick 1 to 5 records to merge into this one."}))
		return
	}
	a := actorFromRequest(r, "merge")
	owners := ownerFilter(r, spec)
	var row *Row
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		for _, d := range in.DuplicateIDs {
			if d == in.PrimaryID {
				return shared.Validation(map[string]string{"duplicateIds": "A record can't be merged into itself."})
			}
			if _, _, err := h.getRow(ctx, tx, ws, spec, d, owners); err != nil {
				return err
			}
		}
		if len(in.Values) > 0 {
			if _, err := h.updateValues(ctx, tx, ws, spec, in.PrimaryID, a, in.Values, nil, owners); err != nil {
				return err
			}
		} else if _, _, err := h.getRow(ctx, tx, ws, spec, in.PrimaryID, owners); err != nil {
			return err
		}
		dups := make([]string, len(in.DuplicateIDs))
		for i, d := range in.DuplicateIDs {
			dups[i] = d.String()
		}
		p := in.PrimaryID
		moves := []string{
			`UPDATE crm.activities SET record_id = $2 WHERE workspace_id = $1 AND object_key = $4 AND record_id = ANY($3::uuid[])`,
			`UPDATE crm.files SET record_id = $2 WHERE workspace_id = $1 AND object_key = $4 AND record_id = ANY($3::uuid[])`,
			`INSERT INTO crm.message_links (message_id, workspace_id, object_key, record_id)
			 SELECT message_id, workspace_id, object_key, $2 FROM crm.message_links WHERE workspace_id = $1 AND object_key = $4 AND record_id = ANY($3::uuid[])
			 ON CONFLICT DO NOTHING`,
			`DELETE FROM crm.message_links WHERE workspace_id = $1 AND object_key = $4 AND record_id = ANY($3::uuid[]) AND record_id <> $2`,
		}
		for _, sql := range moves {
			if _, err := tx.Exec(ctx, sql, ws, p, dups, spec.Key); err != nil {
				return err
			}
		}
		fk := map[string][]string{
			"accounts": {
				`UPDATE crm.contacts SET account_id = $2 WHERE workspace_id = $1 AND account_id = ANY($3::uuid[])`,
				`UPDATE crm.accounts SET parent_account_id = $2 WHERE workspace_id = $1 AND parent_account_id = ANY($3::uuid[]) AND id <> $2`,
				`UPDATE crm.leads SET converted_account_id = $2 WHERE workspace_id = $1 AND converted_account_id = ANY($3::uuid[])`,
				`UPDATE crm.lead_conversions SET account_id = $2 WHERE workspace_id = $1 AND account_id = ANY($3::uuid[])`,
				`UPDATE crm.activities SET account_id = $2 WHERE workspace_id = $1 AND account_id = ANY($3::uuid[])`,
			},
			"contacts": {
				`UPDATE crm.leads SET converted_contact_id = $2 WHERE workspace_id = $1 AND converted_contact_id = ANY($3::uuid[])`,
				`UPDATE crm.lead_conversions SET contact_id = $2 WHERE workspace_id = $1 AND contact_id = ANY($3::uuid[])`,
				`UPDATE crm.activities SET contact_id = $2 WHERE workspace_id = $1 AND contact_id = ANY($3::uuid[])`,
			},
			"leads": {
				`UPDATE crm.activities SET lead_id = $2 WHERE workspace_id = $1 AND lead_id = ANY($3::uuid[])`,
			},
		}
		for _, sql := range fk[spec.Key] {
			if _, err := tx.Exec(ctx, sql, ws, p, dups); err != nil {
				return err
			}
		}
		links, err := h.linkFieldsTo(ctx, tx, ws, spec.Key)
		if err != nil {
			return err
		}
		for _, l := range links {
			key := l.field.Key
			if l.field.Type == "lookup" {
				if _, err := tx.Exec(ctx, `UPDATE crm.`+l.spec.Table+` SET custom = jsonb_set(custom, '{`+key+`}', to_jsonb($2::text))
					WHERE workspace_id = $1 AND custom->>'`+key+`' = ANY($3::text[])`, ws, p.String(), dups); err != nil {
					return err
				}
				continue
			}
			if _, err := tx.Exec(ctx, `UPDATE crm.`+l.spec.Table+` SET custom = jsonb_set(custom, '{`+key+`}', (
					SELECT COALESCE(jsonb_agg(DISTINCT CASE WHEN e = ANY($3::text[]) THEN $2::text ELSE e END), '[]'::jsonb)
					FROM jsonb_array_elements_text(custom->'`+key+`') e))
				WHERE workspace_id = $1 AND jsonb_typeof(custom->'`+key+`') = 'array' AND custom->'`+key+`' ?| $3::text[]`, ws, p.String(), dups); err != nil {
				return err
			}
		}
		for _, d := range in.DuplicateIDs {
			if err := h.deleteRecord(ctx, tx, ws, spec, d, a, owners); err != nil {
				return err
			}
		}
		if err := insertActivity(ctx, tx, ws, spec.Key, p, "record.merged", fmt.Sprintf("Merged %d duplicate record(s) into this one", len(dups)),
			map[string]any{"merged": dups}, a.ID); err != nil {
			return err
		}
		row, _, err = h.getRow(ctx, tx, ws, spec, p, nil)
		return err
	})
	if err == nil {
		h.bus.Kick()
	}
	respond(w, r, http.StatusOK, row, err)
}

func humanizeKey(s string) string {
	s = strings.ReplaceAll(s, "_", " ")
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
