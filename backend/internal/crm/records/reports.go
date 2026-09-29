package records

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Reports & dashboards (D-50). A report asks one object a question: filters, an optional
// group-by (with date buckets), a measure (count / sum / avg / min / max of a number
// field) and how to chart it. It always runs as the viewer: object permission, field
// access and the role hierarchy decide what it sees, so sharing a report never shares data.

type ReportFilter struct {
	Field string `json:"field"`
	Op    string `json:"op"` // eq neq contains gt gte lt lte empty notEmpty lastDays
	Value any    `json:"value,omitempty"`
}

type ReportMeasure struct {
	Fn    string `json:"fn"` // count sum avg min max
	Field string `json:"field,omitempty"`
}

type ReportDef struct {
	Filters    []ReportFilter `json:"filters"`
	GroupBy    string         `json:"groupBy,omitempty"`
	DateBucket string         `json:"dateBucket,omitempty"` // day week month quarter year
	Measure    ReportMeasure  `json:"measure"`
	Chart      string         `json:"chart"` // bar line donut number table
	Limit      int            `json:"limit,omitempty"`
}

type Report struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Object      string     `json:"object"`
	ObjectLabel string     `json:"objectLabel"`
	Definition  ReportDef  `json:"definition"`
	OwnerID     *uuid.UUID `json:"ownerId,omitempty"`
	OwnerName   string     `json:"ownerName"`
	CanEdit     bool       `json:"canEdit"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

type ResultRow struct {
	Key   string  `json:"key"`
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Count int     `json:"count"`
}

type ReportResult struct {
	Object       string      `json:"object"`
	ObjectLabel  string      `json:"objectLabel"`
	GroupLabel   string      `json:"groupLabel,omitempty"`
	MeasureLabel string      `json:"measureLabel"`
	Chart        string      `json:"chart"`
	Rows         []ResultRow `json:"rows"`
	Total        float64     `json:"total"`
	Count        int         `json:"count"`
	Currency     bool        `json:"currency,omitempty"`
}

var (
	chartTypes  = map[string]bool{"bar": true, "line": true, "donut": true, "number": true, "table": true}
	measureFns  = map[string]bool{"count": true, "sum": true, "avg": true, "min": true, "max": true}
	dateBuckets = map[string]string{"day": "YYYY-MM-DD", "week": "IYYY-\"W\"IW", "month": "YYYY-MM", "quarter": "YYYY-\"Q\"Q", "year": "YYYY"}
	numberTypes = map[string]bool{"number": true, "currency": true, "percent": true}
)

// fieldSQL is a field's value as SQL (table alias t) and its kind: num, date, time, bool or text.
func fieldSQL(f Field) (string, string) {
	kind := "text"
	switch {
	case numberTypes[f.Type]:
		kind = "num"
	case f.Type == "date":
		kind = "date"
	case f.Type == "datetime":
		kind = "time"
	case f.Type == "boolean":
		kind = "bool"
	}
	if f.inColumn() {
		return "t." + f.column, kind
	}
	raw := "NULLIF(t.custom->>'" + f.Key + "', '')"
	switch kind {
	case "num":
		return raw + "::numeric", kind
	case "date":
		return "(" + raw + ")::date", kind
	case "time":
		return "(" + raw + ")::timestamptz", kind
	case "bool":
		return raw + "::boolean", kind
	}
	return "t.custom->>'" + f.Key + "'", kind
}

func (d *ReportDef) normalize() {
	if !chartTypes[d.Chart] {
		d.Chart = "bar"
	}
	if !measureFns[d.Measure.Fn] {
		d.Measure.Fn = "count"
	}
	if d.Measure.Fn == "count" {
		d.Measure.Field = ""
	}
	if d.Limit <= 0 || d.Limit > 100 {
		d.Limit = 25
	}
	if d.Filters == nil {
		d.Filters = []ReportFilter{}
	}
	if _, ok := dateBuckets[d.DateBucket]; !ok {
		d.DateBucket = ""
	}
}

// runReport runs def on spec as the scope's viewer.
func (h *Handler) runReport(ctx context.Context, sc *Scope, me uuid.UUID, spec *objectSpec, def ReportDef) (*ReportResult, error) {
	def.normalize()
	fields, err := allFields(ctx, h.store.Pool, sc.WS, spec)
	if err != nil {
		return nil, err
	}
	byKey := map[string]Field{}
	for _, f := range fields {
		byKey[f.Key] = f
	}
	fe := map[string]string{}
	where := []string{"t.workspace_id = $1", "t.deleted_at IS NULL"}
	args := []any{sc.WS}
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if owners := sc.OwnersFor(spec.Key, me); owners != nil {
		where = append(where, "t.owner_id = ANY("+arg(owners)+"::uuid[])")
	}
	for i, fl := range def.Filters {
		f, ok := byKey[fl.Field]
		if !ok {
			fe[fmt.Sprintf("filters.%d", i)] = "Pick a field you can see."
			continue
		}
		expr, kind := fieldSQL(f)
		val := fl.Value
		str := strings.TrimSpace(fmt.Sprint(val))
		switch fl.Op {
		case "empty":
			where = append(where, "("+expr+") IS NULL")
		case "notEmpty":
			where = append(where, "("+expr+") IS NOT NULL")
		case "eq", "neq":
			op := "="
			if fl.Op == "neq" {
				op = "IS DISTINCT FROM"
			}
			switch kind {
			case "num":
				n, err := strconv.ParseFloat(str, 64)
				if err != nil {
					fe[fmt.Sprintf("filters.%d", i)] = "Enter a number."
					continue
				}
				where = append(where, "("+expr+") "+op+" "+arg(n))
			case "bool":
				where = append(where, "COALESCE(("+expr+"), false) "+op+" "+arg(str == "true"))
			case "date", "time":
				where = append(where, "("+expr+")::date "+op+" "+arg(str)+"::date")
			default:
				where = append(where, "("+expr+")::text "+op+" "+arg(str))
			}
		case "contains":
			where = append(where, "("+expr+")::text ILIKE "+arg("%"+strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(str)+"%"))
		case "gt", "gte", "lt", "lte":
			op := map[string]string{"gt": ">", "gte": ">=", "lt": "<", "lte": "<="}[fl.Op]
			switch kind {
			case "num":
				n, err := strconv.ParseFloat(str, 64)
				if err != nil {
					fe[fmt.Sprintf("filters.%d", i)] = "Enter a number."
					continue
				}
				where = append(where, "("+expr+") "+op+" "+arg(n))
			case "date", "time":
				where = append(where, "("+expr+")::date "+op+" "+arg(str)+"::date")
			default:
				fe[fmt.Sprintf("filters.%d", i)] = "Use this comparison with numbers or dates."
			}
		case "lastDays":
			n, err := strconv.Atoi(str)
			if err != nil || n < 1 || n > 3650 || (kind != "date" && kind != "time") {
				fe[fmt.Sprintf("filters.%d", i)] = "Pick a date field and a number of days."
				continue
			}
			where = append(where, "("+expr+") >= now() - "+arg(strconv.Itoa(n)+" days")+"::interval")
		default:
			fe[fmt.Sprintf("filters.%d", i)] = "Pick a comparison."
		}
	}
	measure := "count(*)::numeric"
	measureLabel := "Records"
	res := &ReportResult{Object: spec.Key, ObjectLabel: spec.Plural, Chart: def.Chart, Rows: []ResultRow{}}
	if def.Measure.Fn != "count" {
		f, ok := byKey[def.Measure.Field]
		if !ok || !numberTypes[f.Type] {
			fe["measure"] = "Pick a number field to add up."
		} else {
			expr, _ := fieldSQL(f)
			measure = "COALESCE(" + def.Measure.Fn + "(" + expr + "), 0)::numeric"
			measureLabel = map[string]string{"sum": "Total", "avg": "Average", "min": "Lowest", "max": "Highest"}[def.Measure.Fn] + " " + strings.ToLower(f.Label)
			res.Currency = f.Type == "currency"
		}
	}
	res.MeasureLabel = measureLabel
	var group Field
	groupSQL, groupKind := "", ""
	if def.GroupBy != "" {
		f, ok := byKey[def.GroupBy]
		if !ok || f.Type == "textarea" || f.Type == "multiselect" {
			fe["groupBy"] = "Pick a field you can see to group by."
		} else {
			group = f
			groupSQL, groupKind = fieldSQL(f)
			if (groupKind == "date" || groupKind == "time") && def.DateBucket == "" {
				def.DateBucket = "month"
			}
			if fmtStr, ok := dateBuckets[def.DateBucket]; ok && (groupKind == "date" || groupKind == "time") {
				trunc := def.DateBucket
				groupSQL = "to_char(date_trunc('" + trunc + "', (" + groupSQL + ")::timestamp), '" + fmtStr + "')"
			} else {
				groupSQL = "(" + groupSQL + ")::text"
				def.DateBucket = ""
			}
			res.GroupLabel = f.Label
		}
	}
	if len(fe) > 0 {
		return nil, shared.Validation(fe)
	}
	from := " FROM crm." + spec.Table + " t WHERE " + strings.Join(where, " AND ")
	if err := h.store.Pool.QueryRow(ctx, "SELECT count(*), "+measure+from, args...).Scan(&res.Count, &res.Total); err != nil {
		return nil, err
	}
	if groupSQL == "" {
		res.Rows = append(res.Rows, ResultRow{Key: "all", Label: measureLabel, Value: res.Total, Count: res.Count})
		return res, nil
	}
	order := "3 DESC, 1"
	if def.DateBucket != "" {
		order = "1 ASC"
	}
	rows, err := h.store.Pool.Query(ctx, "SELECT "+groupSQL+", count(*), "+measure+from+" GROUP BY 1 ORDER BY "+order+" LIMIT "+strconv.Itoa(def.Limit), args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var key *string
		var r ResultRow
		if err := rows.Scan(&key, &r.Count, &r.Value); err != nil {
			rows.Close()
			return nil, err
		}
		if key != nil {
			r.Key = *key
		}
		res.Rows = append(res.Rows, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return res, h.labelGroups(ctx, sc.WS, group, res.Rows)
}

// labelGroups turns group keys into what people read: option labels, record titles, names.
func (h *Handler) labelGroups(ctx context.Context, ws uuid.UUID, f Field, rows []ResultRow) error {
	labels := map[string]string{}
	switch {
	case f.Type == "select":
		for _, o := range f.Options {
			labels[o.Value] = o.Label
		}
	case f.Type == "boolean":
		labels["true"], labels["false"] = "Yes", "No"
	case f.Type == "lookup":
		ids := []string{}
		for _, r := range rows {
			if _, err := uuid.Parse(r.Key); err == nil {
				ids = append(ids, r.Key)
			}
		}
		if len(ids) > 0 {
			var sql string
			switch f.Lookup {
			case "users":
				sql = `SELECT id::text, display_name FROM crm.identities WHERE id = ANY($1::uuid[])`
			case "products":
				sql = `SELECT id::text, name FROM crm.products WHERE id = ANY($1::uuid[])`
			case "workspaces":
				sql = `SELECT id::text, name FROM crm.workspaces WHERE id = ANY($1::uuid[])`
			default:
				if s := specFor(f.Lookup); s != nil {
					sql = `SELECT t.id::text, ` + s.TitleSQL + ` FROM crm.` + s.Table + ` t WHERE t.id = ANY($1::uuid[]) AND t.workspace_id = '` + ws.String() + `'`
				}
			}
			if sql != "" {
				qrows, err := h.store.Pool.Query(ctx, sql, ids)
				if err != nil {
					return err
				}
				for qrows.Next() {
					var id, label string
					if err := qrows.Scan(&id, &label); err != nil {
						qrows.Close()
						return err
					}
					labels[id] = label
				}
				qrows.Close()
			}
		}
	}
	for i := range rows {
		switch {
		case rows[i].Key == "":
			rows[i].Label = "(empty)"
		case labels[rows[i].Key] != "":
			rows[i].Label = labels[rows[i].Key]
		default:
			rows[i].Label = rows[i].Key
		}
	}
	return nil
}

// ---- saved reports ----

const reportSelect = `
	SELECT r.id, r.name, r.description, r.object_key, r.definition, r.owner_id, COALESCE(i.display_name, ''), r.created_at, r.updated_at
	FROM crm.reports r LEFT JOIN crm.identities i ON i.id = r.owner_id`

func (h *Handler) scanReport(row pgx.Row, sc *Scope, me uuid.UUID) (*Report, error) {
	var r Report
	var raw []byte
	if err := row.Scan(&r.ID, &r.Name, &r.Description, &r.Object, &raw, &r.OwnerID, &r.OwnerName, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(raw, &r.Definition)
	r.Definition.normalize()
	if s := specFor(r.Object); s != nil {
		r.ObjectLabel = s.Plural
	}
	r.CanEdit = canManageItem(sc, me, r.OwnerID)
	return &r, nil
}

// canManageItem: the creator, the owner or anyone who manages roles can change a report or dashboard.
func canManageItem(sc *Scope, me uuid.UUID, owner *uuid.UUID) bool {
	return sc.Owner || sc.Eff.HasCapability(access.CapAccessManage) || (owner != nil && *owner == me)
}

type reportInput struct {
	Name        *string    `json:"name"`
	Description *string    `json:"description"`
	Object      *string    `json:"object"`
	Definition  *ReportDef `json:"definition"`
}

func reportObject(sc *Scope, key string) (*objectSpec, error) {
	spec := specFor(key)
	if spec == nil || !sc.Enabled(key) {
		return nil, shared.Validation(map[string]string{"object": "Pick an object."})
	}
	if !sc.Can(key, "read") {
		return nil, errForbidden
	}
	return spec, nil
}

func (h *Handler) handleRunReport(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	var in struct {
		Object     string    `json:"object"`
		Definition ReportDef `json:"definition"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	spec, err := reportObject(sc, in.Object)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	res, err := h.runReport(r.Context(), sc, actor(r), spec, in.Definition)
	respond(w, r, http.StatusOK, res, err)
}

func (h *Handler) handleListReports(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	rows, err := h.store.Pool.Query(r.Context(), reportSelect+` WHERE r.workspace_id = $1 ORDER BY r.updated_at DESC`, sc.WS)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	out := []*Report{}
	for rows.Next() {
		rep, err := h.scanReport(rows, sc, actor(r))
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		// Only reports about objects the viewer can read.
		if specFor(rep.Object) != nil && sc.Enabled(rep.Object) && sc.Can(rep.Object, "read") {
			out = append(out, rep)
		}
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) loadReport(ctx context.Context, sc *Scope, me uuid.UUID, id string) (*Report, error) {
	rid, err := uuid.Parse(id)
	if err != nil {
		return nil, shared.NotFound("report_not_found")
	}
	rep, err := h.scanReport(h.store.Pool.QueryRow(ctx, reportSelect+` WHERE r.id = $1 AND r.workspace_id = $2`, rid, sc.WS), sc, me)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NotFound("report_not_found")
	}
	return rep, err
}

func (h *Handler) handleGetReport(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	rep, err := h.loadReport(r.Context(), sc, actor(r), chi.URLParam(r, "id"))
	if err == nil {
		_, err = reportObject(sc, rep.Object)
	}
	respond(w, r, http.StatusOK, rep, err)
}

func (h *Handler) handleReportResult(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	rep, err := h.loadReport(r.Context(), sc, actor(r), chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	spec, err := reportObject(sc, rep.Object)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	res, err := h.runReport(r.Context(), sc, actor(r), spec, rep.Definition)
	respond(w, r, http.StatusOK, res, err)
}

func (h *Handler) validateReport(sc *Scope, in reportInput, cur *Report) (name, desc, object string, def ReportDef, err error) {
	fe := map[string]string{}
	if cur != nil {
		name, desc, object, def = cur.Name, cur.Description, cur.Object, cur.Definition
	}
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		desc = strings.TrimSpace(*in.Description)
	}
	if in.Object != nil {
		object = *in.Object
	}
	if in.Definition != nil {
		def = *in.Definition
	}
	def.normalize()
	if name == "" || len(name) > 120 {
		fe["name"] = "Name the report (up to 120 characters)."
	}
	if len(desc) > 500 {
		fe["description"] = "Use at most 500 characters."
	}
	if len(def.Filters) > 20 {
		fe["filters"] = "Use at most 20 filters."
	}
	if len(fe) > 0 {
		return "", "", "", def, shared.Validation(fe)
	}
	if _, err := reportObject(sc, object); err != nil {
		return "", "", "", def, err
	}
	return name, desc, object, def, nil
}

func (h *Handler) handleCreateReport(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	var in reportInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	name, desc, object, def, err := h.validateReport(sc, in, nil)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	// Check the definition runs before saving it.
	if _, err := h.runReport(r.Context(), sc, actor(r), specFor(object), def); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	raw, _ := json.Marshal(def)
	me := actor(r)
	var id uuid.UUID
	if err := h.store.Pool.QueryRow(r.Context(), `
		INSERT INTO crm.reports (workspace_id, name, description, object_key, definition, owner_id) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		sc.WS, name, desc, object, raw, me).Scan(&id); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	_ = shared.WriteAudit(r.Context(), h.store.Pool, audit(r, sc.WS, "report.created", "report", &id, nil, map[string]any{"name": name, "object": object}))
	rep, err := h.loadReport(r.Context(), sc, me, id.String())
	respond(w, r, http.StatusCreated, rep, err)
}

func (h *Handler) handleUpdateReport(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	me := actor(r)
	cur, err := h.loadReport(r.Context(), sc, me, chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if !cur.CanEdit {
		shared.WriteError(w, r, shared.Forbidden("forbidden", "Only the person who made this report (or an admin) can change it."))
		return
	}
	var in reportInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	name, desc, object, def, err := h.validateReport(sc, in, cur)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if _, err := h.runReport(r.Context(), sc, me, specFor(object), def); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	raw, _ := json.Marshal(def)
	if _, err := h.store.Pool.Exec(r.Context(), `
		UPDATE crm.reports SET name = $2, description = $3, object_key = $4, definition = $5, updated_at = now() WHERE id = $1`,
		cur.ID, name, desc, object, raw); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	rep, err := h.loadReport(r.Context(), sc, me, cur.ID.String())
	respond(w, r, http.StatusOK, rep, err)
}

func (h *Handler) handleDeleteReport(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	cur, err := h.loadReport(r.Context(), sc, actor(r), chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if !cur.CanEdit {
		shared.WriteError(w, r, shared.Forbidden("forbidden", "Only the person who made this report (or an admin) can delete it."))
		return
	}
	if _, err := h.store.Pool.Exec(r.Context(), `DELETE FROM crm.reports WHERE id = $1`, cur.ID); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	// Dashboards drop the widgets that showed it.
	if _, err := h.store.Pool.Exec(r.Context(), `
		UPDATE crm.dashboards SET widgets = COALESCE((SELECT jsonb_agg(w) FROM jsonb_array_elements(widgets) w WHERE w->>'reportId' <> $2), '[]')
		WHERE workspace_id = $1`, sc.WS, cur.ID.String()); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	_ = shared.WriteAudit(r.Context(), h.store.Pool, audit(r, sc.WS, "report.deleted", "report", &cur.ID, map[string]any{"name": cur.Name}, nil))
	w.WriteHeader(http.StatusNoContent)
}

// ---- dashboards ----

type Widget struct {
	ReportID uuid.UUID `json:"reportId"`
	Chart    string    `json:"chart,omitempty"` // overrides the report's chart
	Size     string    `json:"size"`            // sm md lg
}

type DashboardWidget struct {
	Widget
	Report *Report       `json:"report,omitempty"`
	Result *ReportResult `json:"result,omitempty"`
	Error  string        `json:"error,omitempty"`
}

type Dashboard struct {
	ID          uuid.UUID         `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Widgets     []DashboardWidget `json:"widgets"`
	OwnerID     *uuid.UUID        `json:"ownerId,omitempty"`
	OwnerName   string            `json:"ownerName"`
	CanEdit     bool              `json:"canEdit"`
	UpdatedAt   time.Time         `json:"updatedAt"`
}

func (h *Handler) loadDashboard(ctx context.Context, sc *Scope, me uuid.UUID, id string, withResults bool) (*Dashboard, error) {
	did, err := uuid.Parse(id)
	if err != nil {
		return nil, shared.NotFound("dashboard_not_found")
	}
	var d Dashboard
	var raw []byte
	err = h.store.Pool.QueryRow(ctx, `
		SELECT d.id, d.name, d.description, d.widgets, d.owner_id, COALESCE(i.display_name, ''), d.updated_at
		FROM crm.dashboards d LEFT JOIN crm.identities i ON i.id = d.owner_id WHERE d.id = $1 AND d.workspace_id = $2`, did, sc.WS).
		Scan(&d.ID, &d.Name, &d.Description, &raw, &d.OwnerID, &d.OwnerName, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NotFound("dashboard_not_found")
	}
	if err != nil {
		return nil, err
	}
	var ws []Widget
	_ = json.Unmarshal(raw, &ws)
	d.CanEdit = canManageItem(sc, me, d.OwnerID)
	d.Widgets = []DashboardWidget{}
	for _, wd := range ws {
		dw := DashboardWidget{Widget: wd}
		rep, err := h.loadReport(ctx, sc, me, wd.ReportID.String())
		if err != nil {
			dw.Error = "This report was deleted."
			d.Widgets = append(d.Widgets, dw)
			continue
		}
		dw.Report = rep
		if withResults {
			spec, err := reportObject(sc, rep.Object)
			if err != nil {
				dw.Error = "You don't have access to " + rep.ObjectLabel + "."
			} else {
				def := rep.Definition
				if chartTypes[wd.Chart] {
					def.Chart = wd.Chart
				}
				if dw.Result, err = h.runReport(ctx, sc, me, spec, def); err != nil {
					dw.Error = "This report can't run for you."
					dw.Result = nil
				}
			}
		}
		d.Widgets = append(d.Widgets, dw)
	}
	return &d, nil
}

func (h *Handler) handleListDashboards(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	me := actor(r)
	rows, err := h.store.Pool.Query(r.Context(), `
		SELECT d.id, d.name, d.description, jsonb_array_length(d.widgets), d.owner_id, COALESCE(i.display_name, ''), d.updated_at
		FROM crm.dashboards d LEFT JOIN crm.identities i ON i.id = d.owner_id WHERE d.workspace_id = $1 ORDER BY d.updated_at DESC`, sc.WS)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	type item struct {
		ID          uuid.UUID  `json:"id"`
		Name        string     `json:"name"`
		Description string     `json:"description"`
		Widgets     int        `json:"widgets"`
		OwnerID     *uuid.UUID `json:"ownerId,omitempty"`
		OwnerName   string     `json:"ownerName"`
		CanEdit     bool       `json:"canEdit"`
		UpdatedAt   time.Time  `json:"updatedAt"`
	}
	out := []item{}
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.Name, &it.Description, &it.Widgets, &it.OwnerID, &it.OwnerName, &it.UpdatedAt); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		it.CanEdit = canManageItem(sc, me, it.OwnerID)
		out = append(out, it)
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}

type dashboardInput struct {
	Name        *string   `json:"name"`
	Description *string   `json:"description"`
	Widgets     *[]Widget `json:"widgets"`
}

func (h *Handler) cleanWidgets(ctx context.Context, sc *Scope, list []Widget) ([]Widget, error) {
	if len(list) > 24 {
		return nil, shared.Validation(map[string]string{"widgets": "Use at most 24 charts on a dashboard."})
	}
	out := []Widget{}
	for _, wd := range list {
		var ok bool
		if err := h.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.reports WHERE id = $1 AND workspace_id = $2)`, wd.ReportID, sc.WS).Scan(&ok); err != nil {
			return nil, err
		}
		if !ok {
			return nil, shared.Validation(map[string]string{"widgets": "Pick reports from this project."})
		}
		if !chartTypes[wd.Chart] {
			wd.Chart = ""
		}
		if wd.Size != "sm" && wd.Size != "lg" {
			wd.Size = "md"
		}
		out = append(out, wd)
	}
	return out, nil
}

func (h *Handler) handleCreateDashboard(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	var in dashboardInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	name, desc := "", ""
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		desc = strings.TrimSpace(*in.Description)
	}
	if name == "" || len(name) > 120 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"name": "Name the dashboard (up to 120 characters)."}))
		return
	}
	widgets := []Widget{}
	if in.Widgets != nil {
		var err error
		if widgets, err = h.cleanWidgets(r.Context(), sc, *in.Widgets); err != nil {
			shared.WriteError(w, r, err)
			return
		}
	}
	raw, _ := json.Marshal(widgets)
	me := actor(r)
	var id uuid.UUID
	if err := h.store.Pool.QueryRow(r.Context(), `
		INSERT INTO crm.dashboards (workspace_id, name, description, widgets, owner_id) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		sc.WS, name, desc, raw, me).Scan(&id); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	_ = shared.WriteAudit(r.Context(), h.store.Pool, audit(r, sc.WS, "dashboard.created", "dashboard", &id, nil, map[string]any{"name": name}))
	d, err := h.loadDashboard(r.Context(), sc, me, id.String(), true)
	respond(w, r, http.StatusCreated, d, err)
}

func (h *Handler) handleGetDashboard(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	d, err := h.loadDashboard(r.Context(), sc, actor(r), chi.URLParam(r, "id"), true)
	respond(w, r, http.StatusOK, d, err)
}

func (h *Handler) handleUpdateDashboard(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	me := actor(r)
	cur, err := h.loadDashboard(r.Context(), sc, me, chi.URLParam(r, "id"), false)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if !cur.CanEdit {
		shared.WriteError(w, r, shared.Forbidden("forbidden", "Only the person who made this dashboard (or an admin) can change it."))
		return
	}
	var in dashboardInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	name, desc := cur.Name, cur.Description
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		desc = strings.TrimSpace(*in.Description)
	}
	if name == "" || len(name) > 120 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"name": "Name the dashboard (up to 120 characters)."}))
		return
	}
	widgets := make([]Widget, 0, len(cur.Widgets))
	for _, dw := range cur.Widgets {
		widgets = append(widgets, dw.Widget)
	}
	if in.Widgets != nil {
		if widgets, err = h.cleanWidgets(r.Context(), sc, *in.Widgets); err != nil {
			shared.WriteError(w, r, err)
			return
		}
	}
	raw, _ := json.Marshal(widgets)
	if _, err := h.store.Pool.Exec(r.Context(), `UPDATE crm.dashboards SET name = $2, description = $3, widgets = $4, updated_at = now() WHERE id = $1`,
		cur.ID, name, desc, raw); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	d, err := h.loadDashboard(r.Context(), sc, me, cur.ID.String(), true)
	respond(w, r, http.StatusOK, d, err)
}

func (h *Handler) handleDeleteDashboard(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	cur, err := h.loadDashboard(r.Context(), sc, actor(r), chi.URLParam(r, "id"), false)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if !cur.CanEdit {
		shared.WriteError(w, r, shared.Forbidden("forbidden", "Only the person who made this dashboard (or an admin) can delete it."))
		return
	}
	if _, err := h.store.Pool.Exec(r.Context(), `DELETE FROM crm.dashboards WHERE id = $1`, cur.ID); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// reportObjects lists what a viewer can report on, with fields they can see (builder options).
func (h *Handler) handleReportObjects(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	type obj struct {
		Key    string  `json:"key"`
		Label  string  `json:"label"`
		Icon   string  `json:"icon,omitempty"`
		Fields []Field `json:"fields"`
	}
	out := []obj{}
	all := []*objectSpec{specs["leads"], specs["accounts"], specs["contacts"]}
	all = append(all, dynamicSpecs()...)
	for _, s := range all {
		if !sc.Enabled(s.Key) || !sc.Can(s.Key, "read") {
			continue
		}
		fields, err := allFields(r.Context(), h.store.Pool, sc.WS, s)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		out = append(out, obj{Key: s.Key, Label: s.Plural, Icon: s.Icon, Fields: fields})
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) reportRoutes(r chi.Router) {
	r.Get("/reports/objects", h.handleReportObjects)
	r.Post("/reports/run", h.handleRunReport)
	r.Get("/reports", h.handleListReports)
	r.Post("/reports", h.handleCreateReport)
	r.Get("/reports/{id}", h.handleGetReport)
	r.Get("/reports/{id}/result", h.handleReportResult)
	r.Patch("/reports/{id}", h.handleUpdateReport)
	r.Delete("/reports/{id}", h.handleDeleteReport)
	r.Get("/dashboards", h.handleListDashboards)
	r.Post("/dashboards", h.handleCreateDashboard)
	r.Get("/dashboards/{id}", h.handleGetDashboard)
	r.Patch("/dashboards/{id}", h.handleUpdateDashboard)
	r.Delete("/dashboards/{id}", h.handleDeleteDashboard)
}
