package e2e

import (
	"context"
	"os"
	"testing"
)

// Runs last (file name): it erases the test database.
//
// A fresh start keeps the platform owner, one person (98765 43211) and one business of
// theirs ("Ajay traders") with its records and listing — and nothing else (D-108).
func TestFreshStartKeepsOnlyTheOwnerAndOneBusiness(t *testing.T) {
	ctx := context.Background()
	count := func(sql string, args ...any) int {
		var n int
		if err := testDB.Pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}
	defer os.Unsetenv("CRM_FRESH_START")

	// The person who stays: two businesses, records and cards in both.
	ajay := signIn(t, "9876543211", "Ajay")
	traders := newBusiness(t, ajay, "Ajay traders")
	other := newBusiness(t, ajay, "Ajay side project")
	keptLead := create(t, ajay, traders, "leads", map[string]any{"lastName": "Kept Lead", "phone": "9840012345"})
	create(t, ajay, other, "leads", map[string]any{"lastName": "Side Lead"})
	want(t, call(t, "POST", crmAPI+"/w/"+traders+"/cards", ajay, map[string]any{"action": "contact", "card": card("Kept Card", "Kept Co", "9333300010", "")}), 201, "a card in the kept business")
	// A teammate in the kept business, and their record.
	matePhone := freshPhone()
	want(t, call(t, "POST", crmAPI+"/w/"+traders+"/admin/members", ajay, map[string]any{"displayName": "Team Mate", "phone": matePhone, "roleKey": "ADMIN"}), 201, "add a teammate")
	mate := signIn(t, matePhone, "Team Mate")
	mateLead := create(t, mate, traders, "leads", map[string]any{"lastName": "Mate Lead"})
	// Somebody else entirely.
	stranger := signIn(t, freshPhone(), "Soon Gone")
	strangerWS := newBusiness(t, stranger, "Soon Gone Traders")
	create(t, stranger, strangerWS, "leads", map[string]any{"lastName": "Doomed Lead"})
	want(t, call(t, "POST", crmAPI+"/w/"+strangerWS+"/cards", stranger, map[string]any{"action": "card_only", "card": card("Doomed Card", "", "9333300001", "")}), 201, "a stranger's card")

	// A value that doesn't look like a deliberate request does nothing.
	os.Setenv("CRM_FRESH_START", "yes")
	crmMod.FreshStart(ctx)
	want(t, call(t, "GET", crmAPI+"/w/"+strangerWS+"/crm/leads", stranger, nil), 200, "data survives a malformed request")

	os.Setenv("CRM_FRESH_START", "erase-everything-e2e-1")
	crmMod.FreshStart(ctx)

	// Everyone else is gone: session, business, records, cards.
	for who, tok := range map[string]string{"stranger": stranger, "teammate": mate} {
		if r := call(t, "GET", crmAPI+"/businesses", tok, nil); r.Status != 401 {
			t.Fatalf("the erased %s's session still works: %d %s", who, r.Status, truncate(r.Raw, 200))
		}
	}
	if n := count(`SELECT count(*) FROM crm.identities WHERE NOT is_platform_owner`); n != 1 {
		t.Fatalf("people after a fresh start = %d, want 1", n)
	}
	if n := count(`SELECT count(*) FROM crm.workspaces WHERE origin = 'self_serve'`); n != 1 {
		t.Fatalf("customer businesses = %d, want only Ajay traders", n)
	}
	if n := count(`SELECT count(*) FROM public.saved_cards`); n != 1 {
		t.Fatalf("saved cards = %d, want only the kept person's card", n)
	}
	if n := count(`SELECT count(*) FROM public.users`); n != 1 {
		t.Fatalf("app profiles = %d, want 1", n)
	}
	if n := count(`SELECT count(*) FROM crm.audit_events WHERE after::text LIKE '%Doomed%' OR after::text LIKE '%Side Lead%'`); n != 0 {
		t.Fatalf("audit entries of erased records remain")
	}
	ownerSignIn(t)
	// Test setups and objects the owner made are gone; the standard ones stay.
	if n := count(`SELECT count(*) FROM crm.products WHERE key NOT IN ('standard_crm', 'business_card_snap')`); n != 0 {
		t.Fatalf("unused setups left behind = %d", n)
	}
	if n := count(`SELECT count(*) FROM crm.products WHERE key = 'standard_crm'`); n != 1 {
		t.Fatalf("the standard setup must stay")
	}
	if n := count(`SELECT count(*) FROM crm.object_definitions WHERE NOT is_standard`); n != 0 {
		t.Fatalf("test objects left behind = %d", n)
	}
	// Built-in relationship types are part of the product, not customer data.
	if n := count(`SELECT count(*) FROM crm.relationship_types WHERE workspace_id IS NULL`); n != 25 {
		t.Fatalf("built-in relationship types after a fresh start = %d, want 25", n)
	}
	if n := count(`SELECT count(*) FROM crm.currencies`); n < 10 {
		t.Fatalf("the currency list must survive a fresh start, has %d", n)
	}
	if n := count(`SELECT count(*) FROM crm.record_relationships r WHERE NOT EXISTS (SELECT 1 FROM crm.workspaces w WHERE w.id = r.workspace_id)`); n != 0 {
		t.Fatalf("relationships of erased businesses left behind = %d", n)
	}

	// The kept person is still signed in and finds exactly their one business, intact.
	list := call(t, "GET", crmAPI+"/businesses", ajay, nil)
	want(t, list, 200, "the kept person's session survives")
	if len(list.list("data")) != 1 || list.list("data")[0].(map[string]any)["code"] != traders {
		t.Fatalf("only Ajay traders should remain: %s", truncate(list.Raw, 300))
	}
	want(t, call(t, "GET", crmAPI+"/w/"+traders+"/crm/leads/"+keptLead, ajay, nil), 200, "the kept business keeps its records")
	if n := total(t, ajay, traders, "contacts", ""); n != 1 {
		t.Fatalf("the contact made from the kept card should remain, contacts = %d", n)
	}
	if got := len(call(t, "GET", crmAPI+"/w/"+traders+"/cards", ajay, nil).list("items")); got != 1 {
		t.Fatalf("the kept business's card should remain, cards = %d", got)
	}
	// The erased teammate's record stays in the business and passes to its owner.
	ml := call(t, "GET", crmAPI+"/w/"+traders+"/crm/leads/"+mateLead, ajay, nil)
	want(t, ml, 200, "a record of an erased teammate stays in the kept business")
	if ml.str("record", "lookups", "ownerId", "label") != "Ajay" {
		t.Fatalf("the teammate's record should pass to the business owner: %s", truncate(ml.Raw, 400))
	}
	want(t, call(t, "GET", crmAPI+"/w/"+other+"/crm/leads", ajay, nil), 403, "the kept person's other business is gone")

	// Its public listing remains, and a newcomer finds it in the directory but not its CRM.
	if n := count(`SELECT count(*) FROM public.businesses WHERE deleted_at IS NULL`); n != 1 {
		t.Fatalf("public listings = %d, want 1", n)
	}
	visitor := signIn(t, "9876543222", "New Visitor")
	search := call(t, "GET", appAPI+"/businesses/search", visitor, nil)
	want(t, search, 200, "directory search")
	if got := search.list("data", "businesses"); len(got) != 1 || got[0].(map[string]any)["name"] != "Ajay traders" {
		t.Fatalf("the directory should list only Ajay traders: %s", truncate(search.Raw, 300))
	}
	want(t, call(t, "GET", crmAPI+"/w/"+traders+"/crm/leads", visitor, nil), 403, "a stranger opening Ajay traders")

	// The same value never erases twice.
	crmMod.FreshStart(ctx)
	want(t, call(t, "GET", crmAPI+"/businesses", visitor, nil), 200, "a second start with the same value erases nothing")

	// With nobody to keep, a fresh start creates the account: the person and the business.
	if _, err := testDB.Pool.Exec(ctx, `UPDATE crm.verified_identifiers SET value_normalized = '+919000099999' WHERE kind = 'phone' AND value_normalized = '+919876543211'`); err != nil {
		t.Fatal(err)
	}
	// While the app connector is on, its own workspace stays (emptied); switched off, it goes too.
	if n := count(`SELECT count(*) FROM crm.workspaces WHERE code = 'business-card-snap'`); n != 1 {
		t.Fatalf("the connector's workspace should still exist while the connector is on, found %d", n)
	}
	os.Setenv("CRM_CARDFLOW_SYNC", "false")
	defer os.Setenv("CRM_CARDFLOW_SYNC", "true")
	os.Setenv("CRM_FRESH_START", "erase-everything-e2e-2")
	crmMod.FreshStart(ctx)
	if n := count(`SELECT count(*) FROM crm.workspaces WHERE NOT is_platform`); n != 1 {
		t.Fatalf("with the connector off only the kept business remains, found %d workspaces", n)
	}
	if n := count(`SELECT count(*) FROM crm.products WHERE key <> 'standard_crm'`); n != 0 {
		t.Fatalf("setups nobody uses should be gone, found %d", n)
	}
	if n := count(`SELECT count(*) FROM crm.identities WHERE NOT is_platform_owner`); n != 1 {
		t.Fatalf("people after the second fresh start = %d, want 1", n)
	}
	fresh := signIn(t, "9876543211", "")
	list = call(t, "GET", crmAPI+"/businesses", fresh, nil)
	if len(list.list("data")) != 1 || list.list("data")[0].(map[string]any)["name"] != "Ajay traders" {
		t.Fatalf("a created account should have Ajay traders: %s", truncate(list.Raw, 300))
	}
	if n := total(t, fresh, list.list("data")[0].(map[string]any)["code"].(string), "leads", ""); n != 0 {
		t.Fatalf("a created business starts empty, leads = %d", n)
	}
	if n := count(`SELECT count(*) FROM public.businesses WHERE workspace_id IS NOT NULL AND deleted_at IS NULL`); n != 1 {
		t.Fatalf("the created business should have its listing, listings = %d", n)
	}
}
