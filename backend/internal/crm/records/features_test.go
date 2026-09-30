package records

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func testFields() []Field {
	return []Field{
		{Key: "status", Label: "Status", Type: "select", Standard: true, column: "status", Options: []Option{{"new", "New"}, {"won", "Won"}}},
		{Key: "amount", Label: "Amount", Type: "currency"},
		{Key: "closeDate", Label: "Close date", Type: "date"},
		{Key: "createdAt", Label: "Created", Type: "datetime", Standard: true, column: "created_at"},
		{Key: "ownerId", Label: "Owner", Type: "lookup", Lookup: "users", Standard: true, column: "owner_id"},
		{Key: "tags", Label: "Tags", Type: "multiselect"},
		{Key: "email", Label: "Email", Type: "email", Standard: true, column: "email"},
	}
}

func TestCompileFilterNestedGroups(t *testing.T) {
	b := &sqlBuilder{}
	n := &FilterNode{Op: "or", Filters: []FilterNode{
		{Field: "status", Op: "in", Value: []any{"new", "won"}},
		{Op: "and", Filters: []FilterNode{{Field: "amount", Op: "gte", Value: 1000.0}, {Field: "closeDate", Op: "thisMonth"}}},
		{Field: "ownerId", Op: "isMe"},
		{Field: "tags", Op: "contains", Value: []any{"vip"}},
		{Field: "email", Op: "contains", Value: "acme"},
	}}
	sql, fe := compileFilter(n, testFields(), filterEnv{Me: uuid.New(), TZ: "Asia/Kolkata"}, b)
	if len(fe) > 0 {
		t.Fatalf("unexpected errors: %v", fe)
	}
	for _, want := range []string{" OR ", " AND ", "?|", "ILIKE", "date_trunc('month'"} {
		if !strings.Contains(sql, want) {
			t.Errorf("sql %q lacks %q", sql, want)
		}
	}
	// Every placeholder in the SQL has an argument and every argument is used.
	for i := range b.args {
		if !strings.Contains(sql, "$"+itoa(i+1)) {
			t.Errorf("argument $%d is never used in %s", i+1, sql)
		}
	}
}

func itoa(i int) string { return strconv.Itoa(i) }

func TestCompileFilterRejectsUnknownFieldsAndBadValues(t *testing.T) {
	_, fe := compileFilter(&FilterNode{Op: "and", Filters: []FilterNode{{Field: "nope", Op: "eq", Value: "x"}, {Field: "amount", Op: "gt", Value: "abc"}}},
		testFields(), filterEnv{}, &sqlBuilder{})
	if len(fe) != 2 {
		t.Fatalf("want 2 errors, got %v", fe)
	}
}

func TestDateConditionsUseOnlyTheirArguments(t *testing.T) {
	for _, op := range []string{"today", "overdue", "lastDays", "nextDays", "thisWeek", "lastMonth", "empty"} {
		b := &sqlBuilder{}
		sql, fe := compileFilter(&FilterNode{Field: "closeDate", Op: op, Value: 7.0}, testFields(), filterEnv{TZ: "UTC"}, b)
		if len(fe) > 0 {
			t.Fatalf("%s: %v", op, fe)
		}
		for i := range b.args {
			if !strings.Contains(sql, "$"+itoa(i+1)) {
				t.Errorf("%s: unused argument $%d in %s", op, i+1, sql)
			}
		}
	}
}

func TestCronNext(t *testing.T) {
	c, err := parseCron("30 9 * * 1-5")
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC) // Friday after 09:30
	next := c.next(from)
	if next.Weekday() != time.Monday || next.Hour() != 9 || next.Minute() != 30 {
		t.Fatalf("next = %v", next)
	}
	if _, err := parseCron("61 * * * *"); err == nil {
		t.Fatal("minute 61 accepted")
	}
}

func TestRenderTemplates(t *testing.T) {
	vars := map[string]any{"trigger": map[string]any{"record": map[string]any{"displayName": "Asha", "amount": 1200.0}}, "steps": map[string]any{}}
	if got := render("Hi {{trigger.record.displayName}}", vars); got != "Hi Asha" {
		t.Fatalf("got %v", got)
	}
	if got := render("{{trigger.record.amount}}", vars); got != 1200.0 {
		t.Fatalf("a lone placeholder keeps its type, got %#v", got)
	}
	if got := render(map[string]any{"a": "{{missing.value}}"}, vars).(map[string]any)["a"]; got != nil {
		t.Fatalf("missing value = %#v", got)
	}
}

func TestCompareValues(t *testing.T) {
	cases := []struct {
		l  any
		op string
		r  any
		ok bool
	}{{"10", "gt", "5", true}, {"abc", "contains", "B", true}, {"", "empty", nil, true}, {[]any{"a", "b"}, "contains", "b", true}, {"2026-01-02", "lt", "2026-02-01", true}, {"x", "in", "a, x", true}}
	for _, c := range cases {
		if got := compareValues(c.l, c.op, c.r); got != c.ok {
			t.Errorf("%v %s %v = %v", c.l, c.op, c.r, got)
		}
	}
}

func TestRunCodeSandbox(t *testing.T) {
	out, err := runCode(`return {n: input.x * 2}`, map[string]any{"x": 21.0})
	if err != nil || out.(map[string]any)["n"] != 42.0 {
		t.Fatalf("out=%v err=%v", out, err)
	}
	if _, err := runCode(`while(true){}`, map[string]any{}); err == nil {
		t.Fatal("endless loop wasn't stopped")
	}
}

func TestImportCellValues(t *testing.T) {
	h := &Handler{}
	ctx := context.Background()
	cache := map[string]string{}
	if v, msg := h.cellValue(ctx, nil, uuid.Nil, Field{Type: "date"}, "30/09/2026", cache); msg != "" || v != "2026-09-30" {
		t.Fatalf("date: %v %s", v, msg)
	}
	if v, _ := h.cellValue(ctx, nil, uuid.Nil, Field{Type: "boolean"}, "Yes", cache); v != true {
		t.Fatal("yes should be true")
	}
	sel := Field{Type: "select", Options: []Option{{"hot", "Hot"}}}
	if v, _ := h.cellValue(ctx, nil, uuid.Nil, sel, "HOT", cache); v != "hot" {
		t.Fatalf("select by label: %v", v)
	}
	if _, msg := h.cellValue(ctx, nil, uuid.Nil, sel, "Lukewarm", cache); msg == "" {
		t.Fatal("unknown option accepted")
	}
	if v, _ := h.cellValue(ctx, nil, uuid.Nil, Field{Type: "currency"}, "₹1,20,000", cache); v != "120000" {
		t.Fatalf("currency: %v", v)
	}
}

func TestWebhookSignature(t *testing.T) {
	a := signPayload([]byte("secret"), "1700000000", []byte(`{"a":1}`))
	b := signPayload([]byte("secret"), "1700000000", []byte(`{"a":1}`))
	c := signPayload([]byte("other"), "1700000000", []byte(`{"a":1}`))
	if a != b || a == c || !strings.HasPrefix(a, "sha256=") {
		t.Fatalf("signatures %s %s %s", a, b, c)
	}
}

func TestMergeFields(t *testing.T) {
	row := Row{Title: "Asha Rao", Values: map[string]any{"firstName": "Asha <b>"}, Lookups: map[string]LookupValue{}}
	if got := mergeFields("Hi {{firstName}} ({{name}})", row, true); got != "Hi Asha &lt;b&gt; (Asha Rao)" {
		t.Fatalf("got %q", got)
	}
}
