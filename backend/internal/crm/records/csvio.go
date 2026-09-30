package records

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CSV export and import (D-58). Export writes what a list shows (its filter, sorts and
// columns) with readable values — option labels, linked records' names. Import maps
// spreadsheet columns to fields, reads the same readable values back, can update
// existing records matched on a field, and has a dry run that reports every problem
// before anything is saved. Both need their own permission (export / import).

const maxExportRows = 20_000
const maxImportRows = 10_000

func exportValue(r Row, f Field) string {
	v := r.Values[f.Key]
	if l, ok := r.Lookups[f.Key]; ok {
		return l.Label
	}
	if ls, ok := r.Links[f.Key]; ok {
		names := make([]string, len(ls))
		for i, l := range ls {
			names[i] = l.Label
		}
		return strings.Join(names, "; ")
	}
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		if f.Type == "select" {
			for _, o := range f.Options {
				if o.Value == x {
					return o.Label
				}
			}
		}
		return x
	case bool:
		if x {
			return "Yes"
		}
		return "No"
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case []any:
		parts := []string{}
		for _, item := range x {
			switch it := item.(type) {
			case string:
				label := it
				for _, o := range f.Options {
					if o.Value == it {
						label = o.Label
					}
				}
				parts = append(parts, label)
			case map[string]any:
				if n, ok := it["name"].(string); ok {
					parts = append(parts, n)
				}
			default:
				parts = append(parts, fmt.Sprint(it))
			}
		}
		return strings.Join(parts, "; ")
	case map[string]any:
		if f.Type == "address" {
			parts := []string{}
			for _, k := range []string{"street", "street2", "city", "state", "postalCode", "country"} {
				if s, _ := x[k].(string); s != "" {
					parts = append(parts, s)
				}
			}
			return strings.Join(parts, ", ")
		}
		if f.Type == "fullName" {
			fn, _ := x["firstName"].(string)
			ln, _ := x["lastName"].(string)
			return strings.TrimSpace(fn + " " + ln)
		}
	}
	raw, _ := json.Marshal(v)
	return string(raw)
}

// GET /crm/{object}/export?columns=a,b (+ the list's selection parameters) → CSV
func (h *Handler) handleExport(w http.ResponseWriter, r *http.Request) {
	spec, ws, err := h.scope(r, "export")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	qs, err := querySpecFromURL(r)
	if err != nil {
		shared.WriteError(w, r, err)
		return
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
	byKey := map[string]Field{}
	for _, f := range fields {
		byKey[f.Key] = f
	}
	cols := []Field{}
	if raw := r.URL.Query().Get("columns"); raw != "" {
		for _, k := range strings.Split(raw, ",") {
			if f, ok := byKey[strings.TrimSpace(k)]; ok {
				cols = append(cols, f)
			}
		}
	}
	if len(cols) == 0 {
		for _, f := range fields {
			if f.Type != "files" {
				cols = append(cols, f)
			}
		}
	}
	name := fmt.Sprintf("%s-%s.csv", spec.Key, time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("\xEF\xBB\xBF")) // BOM so Excel reads UTF-8
	cw := csv.NewWriter(w)
	header := []string{"Record ID", "Name"}
	for _, f := range cols {
		if f.Key != "code" {
			header = append(header, f.Label)
		}
	}
	_ = cw.Write(header)
	written := 0
	for offset := 0; offset < maxExportRows; offset += 500 {
		p.Limit, p.Offset = 500, offset
		rows, _, err := h.list(r.Context(), ws, spec, p)
		if err != nil {
			break
		}
		for _, row := range rows {
			line := []string{row.Code, row.Title}
			for _, f := range cols {
				if f.Key != "code" {
					line = append(line, exportValue(row, f))
				}
			}
			_ = cw.Write(line)
			written++
		}
		cw.Flush()
		if len(rows) < 500 {
			break
		}
	}
	cw.Flush()
	sc := scopeFrom(r.Context())
	a := actorFromRequest(r, "export")
	_ = shared.WriteAudit(r.Context(), h.store.Pool, a.audit(sc.WS, "records.exported", entityName(spec), nil, nil, map[string]any{"rows": written, "columns": len(cols)}))
}

// ---- import ----

type importInput struct {
	Rows       [][]string `json:"rows"`
	Mapping    []string   `json:"mapping"`    // per column: a field key, or "" to skip it
	Mode       string     `json:"mode"`       // create | upsert | update
	MatchField string     `json:"matchField"` // field used to find existing records (code, email…)
	DryRun     bool       `json:"dryRun"`
}

type importRowError struct {
	Row    int               `json:"row"` // 1-based, not counting the header
	Errors map[string]string `json:"errors"`
}

type importResult struct {
	Rows     int              `json:"rows"`
	Created  int              `json:"created"`
	Updated  int              `json:"updated"`
	Skipped  int              `json:"skipped"`
	Failed   int              `json:"failed"`
	Errors   []importRowError `json:"errors"`
	DryRun   bool             `json:"dryRun"`
	Duration int64            `json:"durationMs"`
}

var truthy = map[string]bool{"yes": true, "y": true, "true": true, "1": true, "on": true, "✓": true}
var falsy = map[string]bool{"no": true, "n": true, "false": true, "0": true, "off": true}

// importDateLayouts: ISO first, then day-first (India), then month-first.
var importDateLayouts = []string{"2006-01-02", "02/01/2006", "2/1/2006", "02-01-2006", "02.01.2006", "01/02/2006", "Jan 2, 2006", "2 Jan 2006", "02-Jan-2006"}

// cellValue turns a spreadsheet cell into what coerce expects for the field.
func (h *Handler) cellValue(ctx context.Context, q querier, ws uuid.UUID, f Field, cell string, cache map[string]string) (any, string) {
	s := strings.TrimSpace(cell)
	if s == "" {
		return nil, ""
	}
	switch f.Type {
	case "boolean":
		l := strings.ToLower(s)
		if truthy[l] {
			return true, ""
		}
		if falsy[l] {
			return false, ""
		}
		return nil, "Use yes or no."
	case "select", "multiselect":
		pick := func(v string) (string, bool) {
			v = strings.TrimSpace(v)
			for _, o := range f.Options {
				if strings.EqualFold(o.Value, v) || strings.EqualFold(o.Label, v) {
					return o.Value, true
				}
			}
			return v, len(f.Options) == 0
		}
		if f.Type == "select" {
			v, ok := pick(s)
			if !ok {
				return nil, fmt.Sprintf("%q isn't one of the options.", s)
			}
			return v, ""
		}
		out := []any{}
		for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ';' || r == ',' || r == '|' }) {
			v, ok := pick(part)
			if !ok {
				return nil, fmt.Sprintf("%q isn't one of the options.", strings.TrimSpace(part))
			}
			out = append(out, v)
		}
		return out, ""
	case "date":
		for _, l := range importDateLayouts {
			if t, err := time.Parse(l, s); err == nil {
				return t.Format("2006-01-02"), ""
			}
		}
		return nil, "Use a date like 2026-09-30 or 30/09/2026."
	case "datetime":
		for _, l := range []string{time.RFC3339, "2006-01-02 15:04", "2006-01-02T15:04", "02/01/2006 15:04"} {
			if t, err := time.ParseInLocation(l, s, istLocation); err == nil {
				return t.UTC().Format(time.RFC3339), ""
			}
		}
		for _, l := range importDateLayouts {
			if t, err := time.ParseInLocation(l, s, istLocation); err == nil {
				return t.UTC().Format(time.RFC3339), ""
			}
		}
		return nil, "Use a date and time like 2026-09-30 14:30."
	case "number", "currency", "percent", "rating":
		clean := strings.NewReplacer(",", "", "₹", "", "$", "", "%", "", " ", "").Replace(s)
		if _, err := strconv.ParseFloat(clean, 64); err != nil {
			return nil, "Enter a number."
		}
		return clean, ""
	case "address":
		if strings.HasPrefix(s, "{") {
			var m map[string]any
			if json.Unmarshal([]byte(s), &m) == nil {
				return m, ""
			}
		}
		parts := strings.Split(s, ",")
		m := map[string]any{}
		keys := []string{"street", "city", "state", "postalCode", "country"}
		if len(parts) == 1 {
			m["street"] = strings.TrimSpace(parts[0])
			return m, ""
		}
		// Last parts are the most regular: country, postal code, state, city; the rest is the street.
		for i := len(parts) - 1; i >= 0 && len(keys) > 1; i-- {
			m[keys[len(keys)-1]] = strings.TrimSpace(parts[i])
			keys = keys[:len(keys)-1]
			parts = parts[:i]
		}
		if len(parts) > 0 {
			m["street"] = strings.TrimSpace(strings.Join(parts, ","))
		}
		return m, ""
	case "fullName":
		first, last, _ := strings.Cut(s, " ")
		return map[string]any{"firstName": first, "lastName": strings.TrimSpace(last)}, ""
	case "emails", "phones", "links":
		out := []any{}
		for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ';' || r == ',' || r == '|' || r == '\n' }) {
			out = append(out, strings.TrimSpace(part))
		}
		return out, ""
	case "json":
		var v any
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			return nil, "Enter valid JSON."
		}
		return v, ""
	case "files":
		return nil, "Files can't be imported; upload them on the record."
	case "lookup", "relations":
		resolve := func(name string) (string, string) {
			name = strings.TrimSpace(name)
			if _, err := uuid.Parse(name); err == nil {
				return name, ""
			}
			key := f.Lookup + "|" + strings.ToLower(name)
			if id, ok := cache[key]; ok {
				return id, ""
			}
			var id string
			var err error
			switch f.Lookup {
			case "users":
				err = q.QueryRow(ctx, `SELECT i.id::text FROM crm.identities i
					LEFT JOIN crm.verified_identifiers e ON e.identity_id = i.id AND e.kind = 'email' AND e.namespace = 'global'
					JOIN crm.memberships m ON m.identity_id = i.id AND m.workspace_id = $2 AND m.status = 'active'
					WHERE lower(i.display_name) = lower($1) OR lower(e.value_normalized) = lower($1) LIMIT 1`, name, ws).Scan(&id)
			default:
				target := specFor(f.Lookup)
				if target == nil {
					return "", "Unknown link."
				}
				err = q.QueryRow(ctx, `SELECT t.id::text FROM crm.`+target.Table+` t WHERE t.workspace_id = $2 AND t.deleted_at IS NULL
					AND (lower(t.code) = lower($1) OR lower(`+target.TitleSQL+`) = lower($1)) ORDER BY t.created_at LIMIT 1`, name, ws).Scan(&id)
			}
			if err != nil {
				return "", fmt.Sprintf("No %s called %q.", strings.ToLower(strings.TrimSuffix(lookupLabel(f.Lookup), "s")), name)
			}
			cache[key] = id
			return id, ""
		}
		if f.Type == "lookup" {
			id, msg := resolve(s)
			if msg != "" {
				return nil, msg
			}
			return id, ""
		}
		out := []any{}
		for _, part := range strings.Split(s, ";") {
			if strings.TrimSpace(part) == "" {
				continue
			}
			id, msg := resolve(part)
			if msg != "" {
				return nil, msg
			}
			out = append(out, id)
		}
		return out, ""
	}
	return s, ""
}

func lookupLabel(target string) string {
	if target == "users" {
		return "users"
	}
	if s := specFor(target); s != nil {
		return s.Plural
	}
	return target
}

// POST /crm/{object}/import
func (h *Handler) handleImport(w http.ResponseWriter, r *http.Request) {
	spec, ws, err := h.scope(r, "import")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	sc := scopeFrom(r.Context())
	var in importInput
	if err := shared.DecodeJSONLimit(w, r, &in, 16<<20); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	switch in.Mode {
	case "", "create":
		in.Mode = "create"
		if !sc.Can(spec.Key, "create") {
			shared.WriteError(w, r, shared.Forbidden("forbidden", "Importing new records needs permission to create them."))
			return
		}
	case "upsert", "update":
		if !sc.Can(spec.Key, "update") || (in.Mode == "upsert" && !sc.Can(spec.Key, "create")) {
			shared.WriteError(w, r, shared.Forbidden("forbidden", "Updating records from a file needs permission to edit them."))
			return
		}
	default:
		shared.WriteError(w, r, shared.Validation(map[string]string{"mode": "Pick whether to add new records or update existing ones."}))
		return
	}
	if len(in.Rows) == 0 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"rows": "The file has no rows."}))
		return
	}
	if len(in.Rows) > maxImportRows {
		shared.WriteError(w, r, shared.Validation(map[string]string{"rows": fmt.Sprintf("Import at most %d rows at a time.", maxImportRows)}))
		return
	}
	fields, err := allFields(r.Context(), h.store.Pool, ws, spec)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	byKey := map[string]Field{}
	for _, f := range fields {
		byKey[f.Key] = f
	}
	mapped := 0
	seenMap := map[string]bool{}
	for i, k := range in.Mapping {
		if k == "" {
			continue
		}
		if f, ok := byKey[k]; !ok || (f.ReadOnly && k != "code") || f.system {
			shared.WriteError(w, r, shared.Validation(map[string]string{fmt.Sprintf("mapping.%d", i): "This column can't be imported into that field."}))
			return
		}
		if seenMap[k] {
			shared.WriteError(w, r, shared.Validation(map[string]string{fmt.Sprintf("mapping.%d", i): "Two columns are mapped to the same field."}))
			return
		}
		seenMap[k] = true
		mapped++
	}
	if mapped == 0 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"mapping": "Map at least one column to a field."}))
		return
	}
	var match Field
	if in.Mode != "create" {
		var ok bool
		match, ok = byKey[in.MatchField]
		if !ok || (!seenMap[in.MatchField]) {
			shared.WriteError(w, r, shared.Validation(map[string]string{"matchField": "Pick a mapped column that identifies existing records (e.g. Record ID or email)."}))
			return
		}
	}
	started := time.Now()
	res := importResult{Rows: len(in.Rows), Errors: []importRowError{}, DryRun: in.DryRun}
	a := actorFromRequest(r, "import")
	owners := ownerFilter(r, spec)
	cache := map[string]string{}

	process := func(tx pgx.Tx, rowIdx int, cells []string) error {
		values := map[string]any{}
		fe := map[string]string{}
		var matchValue string
		for i, key := range in.Mapping {
			if key == "" || i >= len(cells) {
				continue
			}
			if key == "code" {
				matchValue = strings.TrimSpace(cells[i])
				continue
			}
			v, msg := h.cellValue(r.Context(), tx, ws, byKey[key], cells[i], cache)
			if msg != "" {
				fe[key] = msg
				continue
			}
			if key == in.MatchField {
				matchValue = strings.TrimSpace(cells[i])
			}
			if v != nil {
				values[key] = v
			}
		}
		if len(fe) > 0 {
			return shared.Validation(fe)
		}
		var existing uuid.UUID
		if in.Mode != "create" && matchValue != "" {
			var expr string
			if match.Key == "code" {
				expr = "lower(t.code) = lower($3)"
			} else if match.inColumn() {
				expr = "lower(t." + match.column + "::text) = lower($3)"
			} else {
				expr = "lower(t.custom->>'" + match.Key + "') = lower($3)"
			}
			_ = tx.QueryRow(r.Context(), "SELECT t.id FROM crm."+spec.Table+" t WHERE t.workspace_id = $1 AND t.deleted_at IS NULL AND ($2::uuid[] IS NULL OR t.owner_id = ANY($2)) AND "+
				expr+" ORDER BY t.created_at LIMIT 1", ws, owners, matchValue).Scan(&existing)
		}
		switch {
		case existing != uuid.Nil:
			if _, err := h.updateValues(r.Context(), tx, ws, spec, existing, a, values, nil, owners); err != nil {
				return err
			}
			res.Updated++
		case in.Mode == "update":
			res.Skipped++
		default:
			if _, err := h.createRecord(r.Context(), tx, ws, spec, a, values); err != nil {
				return err
			}
			res.Created++
		}
		return nil
	}

	for start := 0; start < len(in.Rows); start += 250 {
		end := start + 250
		if end > len(in.Rows) {
			end = len(in.Rows)
		}
		err := h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
			for i := start; i < end; i++ {
				sp, err := tx.Begin(r.Context())
				if err != nil {
					return err
				}
				if err := process(sp, i, in.Rows[i]); err != nil {
					_ = sp.Rollback(r.Context())
					res.Failed++
					if len(res.Errors) < 500 {
						e := importRowError{Row: i + 1, Errors: map[string]string{}}
						if se, ok := err.(*shared.Error); ok && len(se.FieldErrors) > 0 {
							e.Errors = se.FieldErrors
						} else {
							e.Errors["_"] = errMessage(err)
						}
						res.Errors = append(res.Errors, e)
					}
					continue
				}
				if err := sp.Commit(r.Context()); err != nil {
					return err
				}
			}
			if in.DryRun {
				return errDryRun
			}
			return nil
		})
		if err != nil && err != errDryRun {
			shared.WriteError(w, r, err)
			return
		}
	}
	res.Duration = time.Since(started).Milliseconds()
	sort.Slice(res.Errors, func(i, j int) bool { return res.Errors[i].Row < res.Errors[j].Row })
	if !in.DryRun {
		_ = shared.WriteAudit(r.Context(), h.store.Pool, a.audit(ws, "records.imported", entityName(spec), nil, nil,
			map[string]any{"rows": res.Rows, "created": res.Created, "updated": res.Updated, "failed": res.Failed}))
		h.bus.Kick()
		if res.Rows > 50 {
			h.notify(r.Context(), h.store.Pool, ws, actor(r), "import.finished", fmt.Sprintf("Import of %s finished", strings.ToLower(spec.Plural)),
				fmt.Sprintf("%d added, %d updated, %d failed.", res.Created, res.Updated, res.Failed), "/crm/w/"+sc.Code+"/"+spec.Key, nil)
		}
	}
	shared.WriteJSON(w, http.StatusOK, res)
}

var errDryRun = shared.NewError(http.StatusOK, "dry_run", "dry run")
