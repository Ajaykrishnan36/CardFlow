package e2e

import (
	"strings"
	"testing"
)

// Pickers find a customer by name or by any part of the phone number, show who is who,
// and say what else the form can fill in (D-143).
func TestPickersFindByPhoneAndFillRelated(t *testing.T) {
	boss := signIn(t, freshPhone(), "Boss")
	ws := newBusiness(t, boss, "Velan Bikes")
	base := crmAPI + "/w/" + ws
	find := func(target, q string) []map[string]any {
		t.Helper()
		r := call(t, "GET", base+"/lookup/"+target+"?q="+strings.ReplaceAll(q, " ", "%20"), boss, nil)
		want(t, r, 200, "search "+target+" for "+q)
		out := []map[string]any{}
		for _, x := range r.list("data") {
			out = append(out, x.(map[string]any))
		}
		return out
	}
	// Two customers with the same name, told apart by their numbers.
	acc1 := create(t, boss, ws, "accounts", map[string]any{"name": "Karthik Raja", "kind": "individual", "phone": "+91 98401 23456"})
	con1 := create(t, boss, ws, "contacts", map[string]any{"firstName": "Karthik", "lastName": "Raja", "mobile": "+91 98401 23456", "accountId": acc1})
	acc2 := create(t, boss, ws, "accounts", map[string]any{"name": "Karthik Raja", "kind": "individual", "phone": "+91 90030 77881"})
	con2 := create(t, boss, ws, "contacts", map[string]any{"firstName": "Karthik", "lastName": "Raja", "mobile": "+91 90030 77881", "accountId": acc2})
	bike := create(t, boss, ws, "assets", map[string]any{"name": "Honda Shine 125", "serialNumber": "TN38CX4521", "accountId": acc2, "contactId": con2})

	// By name: both, each with a second line that shows the number.
	byName := find("contacts", "karthik")
	if len(byName) != 2 {
		t.Fatalf("two contacts named Karthik expected, got %v", byName)
	}
	for _, c := range byName {
		if h, _ := c["hint"].(string); !strings.Contains(h, "98401") && !strings.Contains(h, "90030") {
			t.Fatalf("a contact's second line shows the phone number, got %q", h)
		}
	}
	// By the last four digits, however the number was typed in.
	for _, q := range []string{"7881", "77881", "90030 77881", "+91-90030"} {
		got := find("contacts", q)
		if len(got) != 1 || got[0]["id"] != con2 {
			t.Fatalf("contacts for %q: %v", q, got)
		}
	}
	if got := find("accounts", "3456"); len(got) != 1 || got[0]["id"] != acc1 {
		t.Fatalf("accounts by last four digits: %v", got)
	}
	// What the customer owns is found by its number, by the owner's name, or the owner's phone.
	for _, q := range []string{"TN38CX", "karthik", "7881"} {
		got := find("assets", q)
		if len(got) != 1 || got[0]["id"] != bike {
			t.Fatalf("assets for %q: %v", q, got)
		}
		if h, _ := got[0]["hint"].(string); !strings.Contains(h, "TN38CX4521") || !strings.Contains(h, "Karthik Raja") {
			t.Fatalf("an asset's second line shows its number and owner, got %q", h)
		}
	}
	if got := find("assets", "3456"); len(got) != 0 {
		t.Fatalf("the other Karthik owns no bike: %v", got)
	}
	// Words that aren't a number are not treated as one.
	if got := find("contacts", "raja 78"); len(got) != 0 {
		t.Fatalf("a mixed search matches nothing here: %v", got)
	}

	// Choosing the bike tells the form its owner; choosing a contact, the account;
	// choosing an individual's account, the one contact.
	rel := call(t, "GET", base+"/lookup/assets/"+bike+"/related", boss, nil)
	want(t, rel, 200, "what the bike links to")
	if rel.str("lookups", "accountId", "id") != acc2 || rel.str("lookups", "contactId", "id") != con2 {
		t.Fatalf("the bike's owner: %s", truncate(rel.Raw, 300))
	}
	if r := call(t, "GET", base+"/lookup/contacts/"+con1+"/related", boss, nil); r.str("lookups", "accountId", "id") != acc1 {
		t.Fatalf("the contact's account: %s", truncate(r.Raw, 300))
	}
	if r := call(t, "GET", base+"/lookup/accounts/"+acc1+"/related", boss, nil); r.str("lookups", "contactId", "id") != con1 {
		t.Fatalf("the individual account's contact: %s", truncate(r.Raw, 300))
	}
	// A vehicle that names only its owner still leads to the owner's account.
	scooter := create(t, boss, ws, "assets", map[string]any{"name": "Honda Dio", "serialNumber": "TN38DD0001", "contactId": con1})
	if r := call(t, "GET", base+"/lookup/assets/"+scooter+"/related", boss, nil); r.str("lookups", "accountId", "id") != acc1 {
		t.Fatalf("the scooter owner's account: %s", truncate(r.Raw, 300))
	}
	// With an account already on the form, its contacts come first.
	first := call(t, "GET", base+"/lookup/contacts?q=&accountId="+acc1, boss, nil)
	if l := first.list("data"); len(l) != 2 || l[0].(map[string]any)["id"] != con1 {
		t.Fatalf("the chosen account's contact comes first: %s", truncate(first.Raw, 300))
	}

	// Staff who see only their own records can't pick, or learn about, someone else's.
	staff, _ := staffOf(t, boss, ws, "Counter Staff")
	if r := call(t, "GET", base+"/lookup/contacts?q=7881", staff, nil); len(r.list("data")) != 0 {
		t.Fatalf("staff found a contact they don't own: %s", truncate(r.Raw, 300))
	}
	want(t, call(t, "GET", base+"/lookup/assets/"+bike+"/related", staff, nil), 404, "staff asking about a bike they can't see")

	// Every object's picker answers — also the ones with no phone or owner to search by.
	objects := strings.Fields(`leads accounts contacts opportunities tasks events notes communications subscriptions catalog_items cases
		income expenses price_books quotes sales_orders invoices purchase_orders solutions payments contracts assets sla_policies entitlements
		appointments territories refunds credit_notes debit_notes work_orders service_resources resource_absences users`)
	for _, key := range objects {
		for _, q := range []string{"", "a", "3210"} {
			r := call(t, "GET", base+"/lookup/"+key+"?q="+q+"&accountId="+acc1+"&contactId="+con1, boss, nil)
			if r.Status != 200 {
				t.Fatalf("picker for %s (q=%q): %d %s", key, q, r.Status, truncate(r.Raw, 200))
			}
		}
	}
}
