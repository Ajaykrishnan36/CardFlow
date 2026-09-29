package records

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type LookupValue struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Object string `json:"object,omitempty"`
}

type WorkspaceRef struct {
	ID         string `json:"id"`
	Code       string `json:"code"`
	Name       string `json:"name"`
	IsPlatform bool   `json:"isPlatform"`
}

type Row struct {
	Workspace   *WorkspaceRef `json:"workspace,omitempty"`
	workspaceID string
	ID          string                 `json:"id"`
	Code        string                 `json:"code"`
	Title       string                 `json:"title"`
	Values      map[string]any         `json:"values"`
	Lookups     map[string]LookupValue `json:"lookups"`
	Version     int                    `json:"version"`
	CreatedAt   string                 `json:"createdAt"`
	UpdatedAt   string                 `json:"updatedAt"`
}

// selectSQL builds a query returning one JSON document per record: every standard
// field under its API key, plus id/version/custom/title. Column names come only from
// the static specs above, never from input.
func selectSQL(spec *objectSpec) string {
	cols := []string{"t.id", "t.version", "t.custom", "t.workspace_id AS \"_ws\"", spec.TitleSQL + ` AS "_title"`}
	for _, f := range spec.Fields {
		if f.inColumn() {
			cols = append(cols, "t."+f.column+` AS "`+f.Key+`"`)
		}
	}
	return "SELECT " + strings.Join(cols, ", ") + " FROM crm." + spec.Table + " t"
}

func decodeRow(raw []byte, spec *objectSpec, fields []Field) (Row, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return Row{}, err
	}
	r := Row{Values: map[string]any{}, Lookups: map[string]LookupValue{}}
	r.ID, _ = m["id"].(string)
	r.workspaceID, _ = m["_ws"].(string)
	r.Title, _ = m["_title"].(string)
	if v, ok := m["version"].(float64); ok {
		r.Version = int(v)
	}
	custom, _ := m["custom"].(map[string]any)
	for _, f := range fields {
		if f.inColumn() {
			r.Values[f.Key] = m[f.Key]
		} else {
			r.Values[f.Key] = custom[f.Key]
		}
	}
	r.Code, _ = m["code"].(string)
	r.CreatedAt, _ = m["createdAt"].(string)
	r.UpdatedAt, _ = m["updatedAt"].(string)
	return r, nil
}

// resolveLookups fills Row.Lookups with display labels for every lookup value.
func (h *Handler) resolveLookups(ctx context.Context, wsID uuid.UUID, rows []Row, fields []Field) error {
	byTarget := map[string]map[string]bool{}
	for _, f := range fields {
		if f.Type != "lookup" {
			continue
		}
		for _, r := range rows {
			if id, ok := r.Values[f.Key].(string); ok && id != "" {
				if byTarget[f.Lookup] == nil {
					byTarget[f.Lookup] = map[string]bool{}
				}
				byTarget[f.Lookup][id] = true
			}
		}
	}
	labels := map[string]map[string]string{}
	for target, set := range byTarget {
		ids := make([]string, 0, len(set))
		for id := range set {
			ids = append(ids, id)
		}
		var sql string
		switch target {
		case "users":
			sql = `SELECT id::text, display_name FROM crm.identities WHERE id = ANY($1::uuid[])`
		case "products":
			sql = `SELECT id::text, name FROM crm.products WHERE id = ANY($1::uuid[])`
		case "workspaces":
			sql = `SELECT id::text, name FROM crm.workspaces WHERE id = ANY($1::uuid[])`
		default:
			s := specFor(target)
			if s == nil {
				continue
			}
			sql = `SELECT t.id::text, ` + s.TitleSQL + ` FROM crm.` + s.Table + ` t WHERE t.id = ANY($1::uuid[]) AND ($2::uuid = '00000000-0000-0000-0000-000000000000' OR t.workspace_id = $2)`
		}
		var qrows pgx.Rows
		var err error
		if strings.Contains(sql, "$2") {
			qrows, err = h.store.Pool.Query(ctx, sql, ids, wsID)
		} else {
			qrows, err = h.store.Pool.Query(ctx, sql, ids)
		}
		if err != nil {
			return err
		}
		labels[target] = map[string]string{}
		for qrows.Next() {
			var id, label string
			if err := qrows.Scan(&id, &label); err != nil {
				qrows.Close()
				return err
			}
			labels[target][id] = label
		}
		qrows.Close()
	}
	for i := range rows {
		for _, f := range fields {
			if f.Type != "lookup" {
				continue
			}
			if id, ok := rows[i].Values[f.Key].(string); ok && id != "" {
				label := labels[f.Lookup][id]
				if label == "" {
					label = "Unavailable"
				}
				rows[i].Lookups[f.Key] = LookupValue{ID: id, Label: label, Object: f.Lookup}
			}
		}
	}
	return nil
}

type listParams struct {
	Workspaces []uuid.UUID // owner console: several workspaces at once (nil = the scope's workspace)
	Owners     []uuid.UUID // own-scope members: records of these owners only (themselves + roles below)
	Q          string
	Status     string
	Sort       string
	Desc       bool
	Limit      int
	Offset     int
}

func (h *Handler) list(ctx context.Context, wsID uuid.UUID, spec *objectSpec, p listParams) ([]Row, int, error) {
	fields, err := allFields(ctx, h.store.Pool, wsID, spec)
	if err != nil {
		return nil, 0, err
	}
	where := ` WHERE t.workspace_id = $1 AND t.deleted_at IS NULL`
	args := []any{wsID}
	lookupWS := wsID
	if p.Workspaces != nil {
		where = ` WHERE t.workspace_id = ANY($1::uuid[]) AND t.deleted_at IS NULL`
		args = []any{p.Workspaces}
		lookupWS = uuid.Nil
	}
	if p.Q != "" {
		args = append(args, "%"+strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(p.Q)+"%")
		parts := []string{}
		for _, c := range spec.SearchSQL {
			if hiddenSearch(c, fields, spec) {
				continue // never match on a field the requester can't see
			}
			parts = append(parts, c+" ILIKE $"+strconv.Itoa(len(args)))
		}
		if len(parts) == 0 {
			parts = append(parts, "t.code ILIKE $"+strconv.Itoa(len(args)))
		}
		where += " AND (" + strings.Join(parts, " OR ") + ")"
	}
	if p.Owners != nil {
		args = append(args, p.Owners)
		where += " AND t.owner_id = ANY($" + strconv.Itoa(len(args)) + "::uuid[])"
	}
	if p.Status != "" && spec.StatusField != "" {
		f, _ := spec.field(spec.StatusField)
		args = append(args, p.Status)
		where += " AND t." + f.column + " = $" + strconv.Itoa(len(args))
	}

	var total int
	if err := h.store.Pool.QueryRow(ctx, "SELECT count(*) FROM crm."+spec.Table+" t"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	order := "t.created_at"
	if p.Sort == "title" {
		order = spec.TitleSQL
	} else if f, ok := findField(fields, p.Sort); ok {
		if f.inColumn() {
			order = "t." + f.column
		} else if f.Type == "number" || f.Type == "currency" || f.Type == "percent" {
			order = "(t.custom->>'" + f.Key + "')::numeric"
		} else {
			order = "t.custom->>'" + f.Key + "'"
		}
	}
	dir := " ASC NULLS LAST"
	if p.Desc {
		dir = " DESC NULLS LAST"
	}
	args = append(args, p.Limit, p.Offset)
	sql := "SELECT to_jsonb(r) FROM (" + selectSQL(spec) + where + " ORDER BY " + order + dir + ", t.id" +
		" LIMIT $" + strconv.Itoa(len(args)-1) + " OFFSET $" + strconv.Itoa(len(args)) + ") r"
	qrows, err := h.store.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, err
	}
	out := []Row{}
	for qrows.Next() {
		var raw []byte
		if err := qrows.Scan(&raw); err != nil {
			qrows.Close()
			return nil, 0, err
		}
		r, err := decodeRow(raw, spec, fields)
		if err != nil {
			qrows.Close()
			return nil, 0, err
		}
		out = append(out, r)
	}
	qrows.Close()
	if err := qrows.Err(); err != nil {
		return nil, 0, err
	}
	if p.Workspaces != nil {
		if err := h.attachWorkspaces(ctx, out); err != nil {
			return nil, 0, err
		}
	}
	return out, total, h.resolveLookups(ctx, lookupWS, out, fields)
}

func (h *Handler) attachWorkspaces(ctx context.Context, rows []Row) error {
	refs, err := h.workspaceRefs(ctx)
	if err != nil {
		return err
	}
	for i := range rows {
		if ref, ok := refs[rows[i].workspaceID]; ok {
			r := ref
			rows[i].Workspace = &r
		}
	}
	return nil
}

func (h *Handler) workspaceRefs(ctx context.Context) (map[string]WorkspaceRef, error) {
	rows, err := h.store.Pool.Query(ctx, `SELECT id::text, code, name, is_platform FROM crm.workspaces`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]WorkspaceRef{}
	for rows.Next() {
		var w WorkspaceRef
		if err := rows.Scan(&w.ID, &w.Code, &w.Name, &w.IsPlatform); err != nil {
			return nil, err
		}
		if w.IsPlatform {
			w.Name = "Platform CRM"
		}
		out[w.ID] = w
	}
	return out, rows.Err()
}

func findField(fields []Field, key string) (Field, bool) {
	for _, f := range fields {
		if f.Key == key {
			return f, true
		}
	}
	return Field{}, false
}

func (h *Handler) getRow(ctx context.Context, q querier, wsID uuid.UUID, spec *objectSpec, id uuid.UUID, owners []uuid.UUID) (*Row, []Field, error) {
	fields, err := allFields(ctx, q, wsID, spec)
	if err != nil {
		return nil, nil, err
	}
	var raw []byte
	err = q.QueryRow(ctx, "SELECT to_jsonb(r) FROM ("+selectSQL(spec)+" WHERE t.id = $1 AND t.workspace_id = $2 AND t.deleted_at IS NULL AND ($3::uuid[] IS NULL OR t.owner_id = ANY($3))) r", id, wsID, owners).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, shared.NotFound("record_not_found")
	}
	if err != nil {
		return nil, nil, err
	}
	r, err := decodeRow(raw, spec, fields)
	if err != nil {
		return nil, nil, err
	}
	rows := []Row{r}
	if err := h.resolveLookups(ctx, wsID, rows, fields); err != nil {
		return nil, nil, err
	}
	return &rows[0], fields, nil
}

// ---- writes ----

var istLocation = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		return time.UTC
	}
	return loc
}()

// coerce validates one input value for a field. It returns the value to store (Go
// types for standard columns, JSON-friendly values for custom fields) or a message.
func coerce(ctx context.Context, q querier, wsID uuid.UUID, f Field, v any) (any, string) {
	if v == nil {
		if f.Type == "multiselect" && f.inColumn() {
			return []string{}, ""
		}
		return nil, ""
	}
	str := func() (string, bool) {
		s, ok := v.(string)
		return strings.TrimSpace(s), ok
	}
	switch f.Type {
	case "text", "textarea", "email", "phone", "url", "select":
		s, ok := str()
		if !ok {
			if n, isNum := v.(float64); isNum && f.Type != "select" {
				s = strconv.FormatFloat(n, 'f', -1, 64)
			} else {
				return nil, "Enter text."
			}
		}
		if s == "" {
			return nil, ""
		}
		max := 255
		if f.Type == "textarea" {
			max = 32000
		} else if f.Type == "url" {
			max = 2000
		}
		if len(s) > max {
			return nil, fmt.Sprintf("Use at most %d characters.", max)
		}
		switch f.Type {
		case "email":
			addr, err := mail.ParseAddress(s)
			if err != nil || addr.Address != s || !strings.Contains(s[strings.LastIndex(s, "@"):], ".") {
				return nil, "Enter a valid email address."
			}
			s = strings.ToLower(s)
		case "phone":
			digits := 0
			for _, r := range s {
				switch {
				case r >= '0' && r <= '9':
					digits++
				case strings.ContainsRune("+-() .", r):
				default:
					return nil, "Enter a valid phone number."
				}
			}
			if digits < 5 || digits > 15 {
				return nil, "Enter a valid phone number."
			}
		case "url":
			if !strings.Contains(s, "://") {
				s = "https://" + s
			}
			u, err := url.Parse(s)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || !strings.Contains(u.Host, ".") {
				return nil, "Enter a valid web address."
			}
		case "select":
			if len(f.Options) > 0 {
				found := false
				for _, o := range f.Options {
					if o.Value == s {
						found = true
					}
				}
				if !found {
					return nil, "Pick one of the options."
				}
			}
		}
		return s, ""
	case "number", "currency", "percent":
		var n float64
		switch x := v.(type) {
		case float64:
			n = x
		case string:
			s := strings.ReplaceAll(strings.TrimSpace(x), ",", "")
			if s == "" {
				return nil, ""
			}
			p, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return nil, "Enter a number."
			}
			n = p
		default:
			return nil, "Enter a number."
		}
		if math.IsNaN(n) || math.IsInf(n, 0) || math.Abs(n) >= 1e15 {
			return nil, "Enter a smaller number."
		}
		if f.Type == "percent" && (n < 0 || n > 100) {
			return nil, "Enter a percentage between 0 and 100."
		}
		if f.isInt {
			if n < 0 || n > 2_000_000_000 {
				return nil, "Enter a whole number between 0 and 2,000,000,000."
			}
			return int64(math.Round(n)), ""
		}
		return n, ""
	case "date":
		s, ok := str()
		if !ok {
			return nil, "Enter a date."
		}
		if s == "" {
			return nil, ""
		}
		if len(s) > 10 {
			s = s[:10]
		}
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			return nil, "Enter a valid date."
		}
		if f.inColumn() {
			return t, ""
		}
		return s, ""
	case "datetime":
		s, ok := str()
		if !ok {
			return nil, "Enter a date and time."
		}
		if s == "" {
			return nil, ""
		}
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			// <input type="datetime-local"> sends no zone; read it as Indian time.
			for _, layout := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05"} {
				if t, err = time.ParseInLocation(layout, s, istLocation); err == nil {
					break
				}
			}
		}
		if err != nil {
			return nil, "Enter a valid date and time."
		}
		if f.inColumn() {
			return t, ""
		}
		return t.UTC().Format(time.RFC3339), ""
	case "boolean":
		b, ok := v.(bool)
		if !ok {
			return nil, "Choose yes or no."
		}
		return b, ""
	case "multiselect":
		arr, ok := v.([]any)
		if !ok {
			return nil, "Enter a list."
		}
		out := []string{}
		seen := map[string]bool{}
		for _, item := range arr {
			s, ok := item.(string)
			s = strings.TrimSpace(s)
			if !ok || s == "" || seen[s] {
				continue
			}
			if len(s) > 80 {
				return nil, "Each value can be at most 80 characters."
			}
			if len(f.Options) > 0 {
				found := false
				for _, o := range f.Options {
					if o.Value == s {
						found = true
					}
				}
				if !found {
					return nil, "Pick from the options."
				}
			}
			seen[s] = true
			out = append(out, s)
		}
		if len(out) > 50 {
			return nil, "Use at most 50 values."
		}
		if len(out) == 0 && !f.inColumn() {
			return nil, ""
		}
		return out, ""
	case "lookup":
		s, ok := str()
		if !ok {
			return nil, "Pick a record."
		}
		if s == "" {
			return nil, ""
		}
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, "Pick a record."
		}
		var sql string
		switch f.Lookup {
		case "users":
			sql = `SELECT EXISTS (SELECT 1 FROM crm.identities WHERE id = $1 AND status <> 'deleted')`
		case "products":
			sql = `SELECT EXISTS (SELECT 1 FROM crm.products WHERE id = $1)`
		case "workspaces":
			sql = `SELECT EXISTS (SELECT 1 FROM crm.workspaces WHERE id = $1 AND NOT is_platform)`
		default:
			s := specFor(f.Lookup)
			if s == nil {
				return nil, "That record doesn't exist."
			}
			sql = `SELECT EXISTS (SELECT 1 FROM crm.` + s.Table + ` WHERE id = $1 AND workspace_id = '` + wsID.String() + `' AND deleted_at IS NULL)`
		}
		var exists bool
		if err := q.QueryRow(ctx, sql, id).Scan(&exists); err != nil || !exists {
			return nil, "That record doesn't exist."
		}
		return id, ""
	}
	return nil, "Unsupported field."
}

func isEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case []string:
		return len(x) == 0
	}
	return false
}

type changeSet struct {
	columns map[string]any // column → value
	custom  map[string]any // custom key → value (nil = remove)
	after   map[string]any // API key → value (for audit)
}

// buildChanges validates input values against the object's fields.
func buildChanges(ctx context.Context, q querier, wsID uuid.UUID, fields []Field, values map[string]any, creating bool) (*changeSet, map[string]string) {
	cs := &changeSet{columns: map[string]any{}, custom: map[string]any{}, after: map[string]any{}}
	fe := map[string]string{}
	byKey := map[string]Field{}
	for _, f := range fields {
		byKey[f.Key] = f
	}
	for key, raw := range values {
		f, ok := byKey[key]
		if !ok {
			fe[key] = "Unknown field."
			continue
		}
		if f.ReadOnly || f.system {
			fe[key] = "This field can't be edited."
			continue
		}
		v, msg := coerce(ctx, q, wsID, f, raw)
		if msg != "" {
			fe[key] = msg
			continue
		}
		if f.Required && isEmpty(v) {
			fe[key] = f.Label + " is required."
			continue
		}
		if f.inColumn() {
			cs.columns[f.column] = v
		} else {
			cs.custom[f.Key] = v
		}
		cs.after[key] = raw
	}
	if creating {
		for _, f := range fields {
			if f.Required && !f.ReadOnly {
				if _, given := values[f.Key]; !given && fe[f.Key] == "" {
					fe[f.Key] = f.Label + " is required."
				}
			}
		}
	}
	return cs, fe
}

// NextCode allocates the next human code (L-000001…) for a workspace.
func NextCode(ctx context.Context, tx pgx.Tx, wsID uuid.UUID, prefix string) (string, error) {
	return nextCode(ctx, tx, wsID, prefix)
}

func nextCode(ctx context.Context, tx pgx.Tx, wsID uuid.UUID, prefix string) (string, error) {
	var n int64
	err := tx.QueryRow(ctx, `
		INSERT INTO crm.code_counters (workspace_id, prefix, next_value) VALUES ($1, $2, 2)
		ON CONFLICT (workspace_id, prefix) DO UPDATE SET next_value = crm.code_counters.next_value + 1
		RETURNING next_value - 1`, wsID, prefix).Scan(&n)
	return fmt.Sprintf("%s-%06d", prefix, n), err
}

// insertRecord inserts a row with the given column values (columns come from specs).
func insertRecord(ctx context.Context, tx pgx.Tx, wsID uuid.UUID, spec *objectSpec, actor uuid.UUID, cs *changeSet) (uuid.UUID, error) {
	code, err := nextCode(ctx, tx, wsID, spec.Prefix)
	if err != nil {
		return uuid.Nil, err
	}
	custom := map[string]any{}
	for k, v := range cs.custom {
		if v != nil {
			custom[k] = v
		}
	}
	customJSON, _ := json.Marshal(custom)
	cols := []string{"workspace_id", "code", "owner_id", "created_by", "updated_by", "custom"}
	args := []any{wsID, code, actor, actor, actor, customJSON}
	for col, v := range cs.columns {
		if v == nil {
			continue // let column defaults apply
		}
		cols = append(cols, col)
		args = append(args, v)
	}
	ph := make([]string, len(args))
	for i := range args {
		ph[i] = "$" + strconv.Itoa(i+1)
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, "INSERT INTO crm."+spec.Table+" ("+strings.Join(cols, ", ")+") VALUES ("+strings.Join(ph, ", ")+") RETURNING id", args...).Scan(&id)
	return id, err
}

// updateRecord applies a change set with optimistic concurrency (PRD §14: expectedVersion).
func updateRecord(ctx context.Context, tx pgx.Tx, wsID uuid.UUID, spec *objectSpec, id uuid.UUID, actor uuid.UUID, expectedVersion int, cs *changeSet) error {
	sets := []string{"version = version + 1", "updated_at = now()", "updated_by = $4"}
	args := []any{id, wsID, expectedVersion, actor}
	for col, v := range cs.columns {
		args = append(args, v)
		sets = append(sets, col+" = $"+strconv.Itoa(len(args)))
	}
	if len(cs.custom) > 0 {
		setJSON := map[string]any{}
		remove := []string{}
		for k, v := range cs.custom {
			if v == nil {
				remove = append(remove, k)
			} else {
				setJSON[k] = v
			}
		}
		raw, _ := json.Marshal(setJSON)
		args = append(args, raw, remove)
		sets = append(sets, "custom = (custom || $"+strconv.Itoa(len(args)-1)+"::jsonb) - $"+strconv.Itoa(len(args))+"::text[]")
	}
	tag, err := tx.Exec(ctx, "UPDATE crm."+spec.Table+" SET "+strings.Join(sets, ", ")+
		" WHERE id = $1 AND workspace_id = $2 AND version = $3 AND deleted_at IS NULL", args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM crm."+spec.Table+" WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL)", id, wsID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return shared.NotFound("record_not_found")
		}
		return shared.NewError(409, "version_conflict", "Someone else changed this record. Reload to see the latest version.")
	}
	return nil
}
