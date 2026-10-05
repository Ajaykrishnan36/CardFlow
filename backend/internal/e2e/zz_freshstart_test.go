package e2e

import (
	"context"
	"os"
	"testing"
)

// Runs last (file name): it erases the test database.
//
// A fresh start removes every customer and leaves the owner plus the baseline: one person
// with two businesses whose records stay apart (D-106).
func TestFreshStartErasesCustomersAndSeedsTheBaseline(t *testing.T) {
	ctx := context.Background()
	old := signIn(t, freshPhone(), "Soon Gone")
	oldWS := newBusiness(t, old, "Soon Gone Traders")
	create(t, old, oldWS, "leads", map[string]any{"lastName": "Doomed Lead"})
	want(t, call(t, "POST", crmAPI+"/w/"+oldWS+"/cards", old, map[string]any{"action": "card_only", "card": card("Doomed Card", "", "9333300001", "")}), 201, "a card before the erase")

	// A value that doesn't look like a deliberate request does nothing.
	os.Setenv("CRM_FRESH_START", "yes")
	crmMod.FreshStart(ctx)
	want(t, call(t, "GET", crmAPI+"/w/"+oldWS+"/crm/leads", old, nil), 200, "data survives a malformed request")

	os.Setenv("CRM_FRESH_START", "erase-everything-e2e-1")
	crmMod.FreshStart(ctx)
	defer os.Unsetenv("CRM_FRESH_START")

	// The old customer is gone: session, business, records, cards.
	if r := call(t, "GET", crmAPI+"/businesses", old, nil); r.Status != 401 {
		t.Fatalf("an erased person's session still works: %d %s", r.Status, truncate(r.Raw, 200))
	}
	count := func(sql string) int {
		var n int
		if err := testDB.Pool.QueryRow(ctx, sql).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}
	if n := count(`SELECT count(*) FROM crm.identities WHERE NOT is_platform_owner`); n != 1 {
		t.Fatalf("people after a fresh start = %d, want 1 (the baseline person)", n)
	}
	if n := count(`SELECT count(*) FROM crm.identities WHERE is_platform_owner`); n < 1 {
		t.Fatalf("the platform owner must be kept")
	}
	if n := count(`SELECT count(*) FROM crm.workspaces WHERE origin = 'self_serve'`); n != 2 {
		t.Fatalf("self-serve businesses = %d, want the 2 baseline ones", n)
	}
	if n := count(`SELECT count(*) FROM crm.workspaces WHERE code = 'soon-gone-traders'`); n != 0 {
		t.Fatalf("the erased business still exists")
	}
	if n := count(`SELECT count(*) FROM public.saved_cards`); n != 0 {
		t.Fatalf("saved cards after a fresh start = %d, want 0", n)
	}
	if n := count(`SELECT count(*) FROM crm.audit_events WHERE action = 'record.created' AND after::text LIKE '%Doomed%'`); n != 0 {
		t.Fatalf("audit entries of erased records remain")
	}
	// The owner still signs in.
	ownerSignIn(t)

	// The baseline person signs in with their number and finds two separate businesses.
	ajay := signIn(t, "9876543211", "")
	list := call(t, "GET", crmAPI+"/businesses", ajay, nil)
	want(t, list, 200, "baseline businesses")
	codes := map[string]string{}
	for _, b := range list.list("data") {
		m := b.(map[string]any)
		codes[m["name"].(string)] = m["code"].(string)
		if m["roleKey"] != "SUPER_ADMIN" {
			t.Fatalf("the baseline person must be Super Admin of %v, is %v", m["name"], m["roleKey"])
		}
	}
	tech, fin := codes["Ajay tech"], codes["Ajay finace"]
	if tech == "" || fin == "" || len(codes) != 2 {
		t.Fatalf("baseline businesses wrong: %v", codes)
	}
	for _, ws := range []string{tech, fin} {
		if n := total(t, ajay, ws, "leads", ""); n != 2 {
			t.Fatalf("%s leads = %d, want 2 (one new, one converted)", ws, n)
		}
		if n := total(t, ajay, ws, "leads", "?status=converted"); n != 1 {
			t.Fatalf("%s converted leads = %d, want 1", ws, n)
		}
		if n := total(t, ajay, ws, "accounts", ""); n != 2 {
			t.Fatalf("%s accounts = %d, want 2", ws, n)
		}
		if n := total(t, ajay, ws, "contacts", ""); n != 2 {
			t.Fatalf("%s contacts = %d, want 2", ws, n)
		}
		for _, object := range []string{"opportunities", "tasks", "events", "cases", "income", "expenses"} {
			if n := total(t, ajay, ws, object, ""); n != 1 {
				t.Fatalf("%s %s = %d, want 1", ws, object, n)
			}
		}
	}
	// The two businesses don't share a single record.
	if n := total(t, ajay, tech, "leads", "?q=Meena"); n != 0 {
		t.Fatalf("a lead of Ajay finace shows up in Ajay tech")
	}
	if n := total(t, ajay, fin, "accounts", "?q=Kovai"); n != 0 {
		t.Fatalf("an account of Ajay tech shows up in Ajay finace")
	}
	sum := call(t, "GET", crmAPI+"/w/"+tech+"/dashboard/summary?range=all", ajay, nil)
	want(t, sum, 200, "baseline summary")
	if sum.at("finance", "income") != float64(25000) || sum.at("finance", "expenses") != float64(4000) {
		t.Fatalf("Ajay tech finance wrong: %v", sum.at("finance"))
	}

	// Both businesses are in the public directory, and a brand-new person finds them there.
	if n := count(`SELECT count(*) FROM public.businesses WHERE workspace_id IS NOT NULL AND deleted_at IS NULL`); n != 2 {
		t.Fatalf("public listings = %d, want 2", n)
	}
	visitor := signIn(t, "9876543222", "New Visitor")
	search := call(t, "GET", appAPI+"/businesses/search", visitor, nil)
	want(t, search, 200, "directory search")
	found := 0
	for _, b := range search.list("data", "businesses") {
		if n, _ := b.(map[string]any)["name"].(string); n == "Ajay tech" || n == "Ajay finace" {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("the directory should list both baseline businesses, found %d: %s", found, truncate(search.Raw, 400))
	}
	// …but the visitor can't open either CRM.
	want(t, call(t, "GET", crmAPI+"/w/"+tech+"/crm/leads", visitor, nil), 403, "a stranger opening Ajay tech")
	if l := call(t, "GET", crmAPI+"/businesses", visitor, nil); len(l.list("data")) != 0 {
		t.Fatalf("a new person starts with no business: %s", truncate(l.Raw, 200))
	}

	// The same value never erases twice.
	create(t, ajay, tech, "leads", map[string]any{"lastName": "After The Reset"})
	crmMod.FreshStart(ctx)
	if n := total(t, ajay, tech, "leads", ""); n != 3 {
		t.Fatalf("a second start with the same value erased data: leads = %d, want 3", n)
	}
}
