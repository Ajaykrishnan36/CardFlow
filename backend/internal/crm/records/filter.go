package records

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// Filters (D-54). One engine compiles filter trees into SQL for lists, saved views,
// boards, exports, bulk actions, reports, workflow conditions and campaign audiences,
// so a filter means the same thing everywhere. Field names never reach SQL: every
// expression comes from the object's field definitions; values are bind parameters.

// FilterNode is either a condition (Field set) or a group of nodes joined by Op
// ("and" / "or"). Groups nest.
type FilterNode struct {
	Op      string       `json:"op,omitempty"`
	Field   string       `json:"field,omitempty"`
	Value   any          `json:"value,omitempty"`
	Filters []FilterNode `json:"filters,omitempty"`
}

// SortSpec is one sort level.
type SortSpec struct {
	Field string `json:"field"`
	Dir   string `json:"dir,omitempty"` // asc | desc
}

// filterEnv is what relative conditions need: who "me" is and who is on my teams.
type filterEnv struct {
	Me   uuid.UUID
	Team []uuid.UUID
	TZ   string
}

const maxFilterDepth = 5
const maxFilterConditions = 50

// sqlBuilder collects bind arguments while SQL is assembled.
type sqlBuilder struct {
	args []any
}

func (b *sqlBuilder) arg(v any) string {
	b.args = append(b.args, v)
	return "$" + strconv.Itoa(len(b.args))
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// valueKind is how a field compares: num, date, time, bool, ids (many links), multi,
// lookup, select or text.
func valueKind(f Field) string {
	switch f.Type {
	case "number", "currency", "percent", "rating":
		return "num"
	case "date":
		return "date"
	case "datetime":
		return "time"
	case "boolean":
		return "bool"
	case "relations":
		return "ids"
	case "multiselect":
		return "multi"
	case "lookup":
		return "lookup"
	case "select":
		return "select"
	}
	return "text"
}

// filterExpr is a field's SQL expression for comparisons (table alias t).
func filterExpr(f Field) string {
	if f.inColumn() {
		return "t." + f.column
	}
	raw := "NULLIF(t.custom->>'" + f.Key + "', '')"
	switch valueKind(f) {
	case "num":
		return "(" + raw + ")::numeric"
	case "date":
		return "(" + raw + ")::date"
	case "time":
		return "(" + raw + ")::timestamptz"
	case "bool":
		return "(" + raw + ")::boolean"
	case "ids", "multi":
		return "t.custom->'" + f.Key + "'"
	}
	return raw
}

func (n FilterNode) isGroup() bool { return n.Field == "" }

func countConditions(n FilterNode) int {
	if !n.isGroup() {
		return 1
	}
	c := 0
	for _, x := range n.Filters {
		c += countConditions(x)
	}
	return c
}

// compileFilter turns a filter tree into a SQL boolean expression ("" = no filter).
// Field errors are keyed by the condition's path ("filters.0.filters.2").
func compileFilter(n *FilterNode, fields []Field, env filterEnv, b *sqlBuilder) (string, map[string]string) {
	fe := map[string]string{}
	if n == nil {
		return "", fe
	}
	if countConditions(*n) > maxFilterConditions {
		fe["filter"] = fmt.Sprintf("Use at most %d conditions.", maxFilterConditions)
		return "", fe
	}
	byKey := map[string]Field{}
	for _, f := range fields {
		byKey[f.Key] = f
	}
	sql := compileNode(*n, "filter", 0, byKey, env, b, fe)
	return sql, fe
}

func compileNode(n FilterNode, path string, depth int, byKey map[string]Field, env filterEnv, b *sqlBuilder, fe map[string]string) string {
	if n.isGroup() {
		if depth >= maxFilterDepth {
			fe[path] = "Groups can nest at most 5 levels."
			return ""
		}
		join := " AND "
		if strings.EqualFold(n.Op, "or") {
			join = " OR "
		}
		parts := []string{}
		for i, c := range n.Filters {
			if s := compileNode(c, fmt.Sprintf("%s.filters.%d", path, i), depth+1, byKey, env, b, fe); s != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) == 0 {
			return ""
		}
		return "(" + strings.Join(parts, join) + ")"
	}
	f, ok := byKey[n.Field]
	if !ok {
		fe[path] = "Pick a field you can see."
		return ""
	}
	s, msg := compileCondition(f, n.Op, n.Value, env, b)
	if msg != "" {
		fe[path] = f.Label + ": " + msg
		return ""
	}
	return s
}

func asString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func asStrings(v any) []string {
	switch x := v.(type) {
	case []any:
		out := []string{}
		for _, i := range x {
			if s := asString(i); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return x
	case string:
		if strings.TrimSpace(x) == "" {
			return nil
		}
		return []string{strings.TrimSpace(x)}
	}
	return nil
}

func asNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	}
	n, err := strconv.ParseFloat(strings.ReplaceAll(asString(v), ",", ""), 64)
	return n, err == nil
}

// dayExpr is the expression as a calendar day in the workspace's time zone.
func dayExpr(expr, kind string, b *sqlBuilder, tz string) string {
	if kind == "time" {
		return "((" + expr + ") AT TIME ZONE " + b.arg(tz) + "::text)::date"
	}
	return "(" + expr + ")::date"
}

func compileCondition(f Field, op string, value any, env filterEnv, b *sqlBuilder) (string, string) {
	expr := filterExpr(f)
	kind := valueKind(f)
	tz := env.TZ
	if tz == "" {
		tz = "Asia/Kolkata"
	}
	isNull := func() string {
		switch kind {
		case "ids", "multi":
			return "(" + expr + " IS NULL OR jsonb_typeof(" + expr + ") <> 'array' OR jsonb_array_length(" + expr + ") = 0)"
		}
		if f.inColumn() && (f.Type == "text" || f.Type == "textarea" || f.Type == "email" || f.Type == "phone" || f.Type == "url") {
			return "NULLIF(" + expr + ", '') IS NULL"
		}
		return "(" + expr + ") IS NULL"
	}
	switch op {
	case "empty":
		return isNull(), ""
	case "notEmpty":
		return "NOT " + isNull(), ""
	}
	todayArg := ""
	todayFn := func() string {
		if todayArg == "" {
			todayArg = "(now() AT TIME ZONE " + b.arg(tz) + "::text)::date"
		}
		return todayArg
	}
	switch kind {
	case "num":
		switch op {
		case "eq", "neq", "gt", "gte", "lt", "lte":
			n, ok := asNumber(value)
			if !ok {
				return "", "enter a number."
			}
			sqlOp := map[string]string{"eq": "=", "neq": "IS DISTINCT FROM", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}[op]
			return "(" + expr + ")::numeric " + sqlOp + " " + b.arg(n) + "::numeric", ""
		case "between":
			vals := asStrings(value)
			if len(vals) != 2 {
				return "", "enter two numbers."
			}
			lo, ok1 := asNumber(vals[0])
			hi, ok2 := asNumber(vals[1])
			if !ok1 || !ok2 {
				return "", "enter two numbers."
			}
			return "(" + expr + ")::numeric BETWEEN " + b.arg(lo) + " AND " + b.arg(hi), ""
		}
	case "date", "time":
		day := dayExpr(expr, kind, b, tz)
		dateArg := func(s string) (string, string) {
			if len(s) > 10 {
				s = s[:10]
			}
			if len(s) != 10 {
				return "", "pick a date."
			}
			return b.arg(s) + "::date", ""
		}
		switch op {
		case "eq", "on":
			a, m := dateArg(asString(value))
			if m != "" {
				return "", m
			}
			return day + " = " + a, ""
		case "neq":
			a, m := dateArg(asString(value))
			if m != "" {
				return "", m
			}
			return day + " IS DISTINCT FROM " + a, ""
		case "before", "lt", "after", "gt", "gte", "lte":
			a, m := dateArg(asString(value))
			if m != "" {
				return "", m
			}
			sqlOp := map[string]string{"before": "<", "lt": "<", "after": ">", "gt": ">", "gte": ">=", "lte": "<="}[op]
			return day + " " + sqlOp + " " + a, ""
		case "between":
			vals := asStrings(value)
			if len(vals) != 2 {
				return "", "pick two dates."
			}
			a1, m1 := dateArg(vals[0])
			a2, m2 := dateArg(vals[1])
			if m1 != "" || m2 != "" {
				return "", "pick two dates."
			}
			return day + " BETWEEN " + a1 + " AND " + a2, ""
		case "lastDays", "nextDays":
			n, ok := asNumber(value)
			if !ok || n < 1 || n > 3650 {
				return "", "enter a number of days (1–3650)."
			}
			if op == "lastDays" {
				return day + " BETWEEN " + todayFn() + " - " + b.arg(int(n)) + "::int AND " + todayFn(), ""
			}
			return day + " BETWEEN " + todayFn() + " AND " + todayFn() + " + " + b.arg(int(n)) + "::int", ""
		case "today":
			return day + " = " + todayFn(), ""
		case "yesterday":
			return day + " = " + todayFn() + " - 1", ""
		case "tomorrow":
			return day + " = " + todayFn() + " + 1", ""
		case "overdue":
			return day + " < " + todayFn(), ""
		case "thisWeek", "lastWeek", "thisMonth", "lastMonth", "thisYear", "thisQuarter":
			unit := map[string]string{"thisWeek": "week", "lastWeek": "week", "thisMonth": "month", "lastMonth": "month", "thisYear": "year", "thisQuarter": "quarter"}[op]
			start := "date_trunc('" + unit + "', " + todayFn() + ")::date"
			if strings.HasPrefix(op, "last") {
				start = "(date_trunc('" + unit + "', " + todayFn() + ") - interval '1 " + unit + "')::date"
				return day + " >= " + start + " AND " + day + " < date_trunc('" + unit + "', " + todayFn() + ")::date", ""
			}
			return day + " >= " + start + " AND " + day + " < (" + start + " + interval '1 " + unit + "')::date", ""
		}
	case "bool":
		if op == "eq" || op == "is" {
			want := asString(value) == "true"
			return "COALESCE(" + expr + ", false) = " + b.arg(want) + "::boolean", ""
		}
	case "ids", "multi":
		vals := asStrings(value)
		switch op {
		case "contains", "in", "eq":
			if len(vals) == 0 {
				return "", "pick at least one value."
			}
			return "(jsonb_typeof(" + expr + ") = 'array' AND " + expr + " ?| " + b.arg(vals) + "::text[])", ""
		case "notIn", "neq", "notContains":
			if len(vals) == 0 {
				return "", "pick at least one value."
			}
			return "NOT (COALESCE(jsonb_typeof(" + expr + ") = 'array' AND " + expr + " ?| " + b.arg(vals) + "::text[], false))", ""
		case "all":
			if len(vals) == 0 {
				return "", "pick at least one value."
			}
			return "(jsonb_typeof(" + expr + ") = 'array' AND " + expr + " ?& " + b.arg(vals) + "::text[])", ""
		}
	case "lookup", "select":
		text := "(" + expr + ")::text"
		if kind == "lookup" && f.Lookup == "users" {
			switch op {
			case "isMe":
				return text + " = " + b.arg(env.Me.String()) + "::text", ""
			case "notMe":
				return text + " IS DISTINCT FROM " + b.arg(env.Me.String()) + "::text", ""
			case "myTeam":
				ids := []string{env.Me.String()}
				for _, t := range env.Team {
					ids = append(ids, t.String())
				}
				return text + " = ANY(" + b.arg(ids) + "::text[])", ""
			}
		}
		switch op {
		case "eq", "neq":
			v := asString(value)
			if v == "" {
				return "", "pick a value."
			}
			if op == "eq" {
				return text + " = " + b.arg(v) + "::text", ""
			}
			return text + " IS DISTINCT FROM " + b.arg(v) + "::text", ""
		case "in", "notIn":
			vals := asStrings(value)
			if len(vals) == 0 {
				return "", "pick at least one value."
			}
			if op == "in" {
				return text + " = ANY(" + b.arg(vals) + "::text[])", ""
			}
			return "NOT (COALESCE(" + text + " = ANY(" + b.arg(vals) + "::text[]), false))", ""
		}
	default: // text-like
		text := "(" + expr + ")::text"
		v := asString(value)
		switch op {
		case "eq":
			return "lower(" + text + ") = lower(" + b.arg(v) + "::text)", ""
		case "neq":
			return "lower(" + text + ") IS DISTINCT FROM lower(" + b.arg(v) + "::text)", ""
		case "contains":
			if v == "" {
				return "", "enter some text."
			}
			return text + " ILIKE " + b.arg("%"+likeEscape(v)+"%") + "::text", ""
		case "notContains":
			if v == "" {
				return "", "enter some text."
			}
			return "COALESCE(" + text + ", '') NOT ILIKE " + b.arg("%"+likeEscape(v)+"%") + "::text", ""
		case "startsWith":
			if v == "" {
				return "", "enter some text."
			}
			return text + " ILIKE " + b.arg(likeEscape(v)+"%") + "::text", ""
		case "endsWith":
			if v == "" {
				return "", "enter some text."
			}
			return text + " ILIKE " + b.arg("%"+likeEscape(v)) + "::text", ""
		case "in":
			vals := asStrings(value)
			if len(vals) == 0 {
				return "", "enter at least one value."
			}
			lower := make([]string, len(vals))
			for i, s := range vals {
				lower[i] = strings.ToLower(s)
			}
			return "lower(" + text + ") = ANY(" + b.arg(lower) + "::text[])", ""
		}
	}
	return "", "pick a comparison that fits this field."
}

// orderSQL turns sort levels into an ORDER BY list (always ending with t.id for stable pages).
func orderSQL(sorts []SortSpec, fields []Field, spec *objectSpec) (string, map[string]string) {
	fe := map[string]string{}
	byKey := map[string]Field{}
	for _, f := range fields {
		byKey[f.Key] = f
	}
	parts := []string{}
	for i, s := range sorts {
		if i >= 5 {
			fe["sort"] = "Sort by at most 5 fields."
			break
		}
		dir := " ASC NULLS LAST"
		if strings.EqualFold(s.Dir, "desc") {
			dir = " DESC NULLS LAST"
		}
		if s.Field == "title" {
			parts = append(parts, spec.TitleSQL+dir)
			continue
		}
		f, ok := byKey[s.Field]
		if !ok {
			fe[fmt.Sprintf("sort.%d", i)] = "Pick a field you can see."
			continue
		}
		expr := filterExpr(f)
		switch valueKind(f) {
		case "ids", "multi":
			expr = "(" + expr + ")::text"
		case "text", "select", "lookup":
			if !f.inColumn() {
				expr = "lower(" + expr + ")"
			} else if f.Type != "lookup" {
				expr = "lower(" + expr + "::text)"
			}
		}
		parts = append(parts, expr+dir)
	}
	if len(parts) == 0 {
		parts = append(parts, "t.created_at DESC")
	}
	return strings.Join(append(parts, "t.id"), ", "), fe
}
