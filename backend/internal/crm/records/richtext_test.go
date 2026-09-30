package records

import (
	"strings"
	"testing"
)

func TestSanitizeRich(t *testing.T) {
	id := "11111111-2222-3333-4444-555555555555"
	in := `<h2>Call notes</h2><p onclick="x()">Hi <strong>there</strong> <a href="javascript:alert(1)">bad</a> <a href="https://example.com">ok</a></p>` +
		`<script>alert(1)</script><img src="x" onerror="alert(1)"><img src="/api/crm/v1/w/acme/files/abc?inline=1" alt="shot">` +
		`<ul data-type="taskList"><li data-type="taskItem" data-checked="true"><label><input type="checkbox" checked></label><div><p>Send quote</p></div></li></ul>` +
		`<p><span data-type="mention" data-id="` + id + `" data-label="Priya">@Priya</span></p>`
	out := sanitizeRich(in)
	for _, bad := range []string{"<script", "onclick", "onerror", "javascript:", `src="x"`} {
		if strings.Contains(out, bad) {
			t.Fatalf("kept %q: %s", bad, out)
		}
	}
	for _, good := range []string{"<h2>Call notes</h2>", "<strong>there</strong>", `href="https://example.com"`, `/api/crm/v1/w/acme/files/abc?inline=1`,
		`data-type="taskList"`, `data-checked="true"`, `data-id="` + id + `"`} {
		if !strings.Contains(out, good) {
			t.Fatalf("lost %q: %s", good, out)
		}
	}
	if ids := mentionIDs(out); len(ids) != 1 || ids[0] != id {
		t.Fatalf("mentions: %v", ids)
	}
	if got := sanitizeRich("plain <3 text"); got != "plain <3 text" {
		t.Fatalf("plain text changed: %q", got)
	}
	if got := sanitizeRich("<p></p><p> </p>"); got != "" {
		t.Fatalf("empty editor should be empty: %q", got)
	}
	if got := richPlain("<h2>Title</h2><p>One &amp; two</p><ul><li>a</li></ul>"); got != "Title\nOne & two\na" {
		t.Fatalf("plain: %q", got)
	}
}

func TestCurrencyCode(t *testing.T) {
	if c, msg := currencyCode("usd"); msg != "" || c != "USD" {
		t.Fatalf("usd: %v %q", c, msg)
	}
	if _, msg := currencyCode("XYZ"); msg == "" {
		t.Fatal("unknown code accepted")
	}
	if c, msg := currencyCode(""); msg != "" || c != nil {
		t.Fatalf("empty should clear: %v %q", c, msg)
	}
}
