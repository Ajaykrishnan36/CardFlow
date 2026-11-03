package e2e

import (
	"strings"
	"testing"
)

// One customer per record (D-173): a vehicle decides who a case or service is for, a contact
// decides the account, and a mix of three different customers is refused with a plain reason.
func TestOneCustomerPerRecord(t *testing.T) {
	boss := signIn(t, freshPhone(), "Link Owner")
	made := call(t, "POST", crmAPI+"/businesses", boss, map[string]any{"name": "Link Motors", "industry": "Automotive"})
	want(t, made, 201, "create the dealer")
	ws := made.str("business", "code")
	base := crmAPI + "/w/" + ws
	balaji := create(t, boss, ws, "accounts", map[string]any{"name": "Balaji Auto Finance"})
	ravi := create(t, boss, ws, "contacts", map[string]any{"firstName": "Ravi", "lastName": "B", "accountId": balaji})
	senthil := create(t, boss, ws, "contacts", map[string]any{"firstName": "Senthil", "lastName": "Kumar"})
	revathi := create(t, boss, ws, "contacts", map[string]any{"firstName": "Revathi", "lastName": "S"})
	bike := create(t, boss, ws, "assets", map[string]any{"name": "Splendor TN 37 DK 4521", "contactId": senthil})

	post := func(object string, v map[string]any) resp {
		return call(t, "POST", base+"/crm/"+object, boss, map[string]any{"values": v})
	}
	// A dealer's case needs the vehicle.
	if r := post("cases", map[string]any{"name": "Noise", "contactId": senthil}); r.Status != 422 || !strings.Contains(r.Raw, `"assetId"`) {
		t.Fatalf("a dealer's case names the vehicle: %d %s", r.Status, truncate(r.Raw, 200))
	}
	// Vehicle first: the customer comes with it.
	c := create(t, boss, ws, "cases", map[string]any{"name": "Noise", "assetId": bike})
	if get(t, boss, ws, "cases", c)["contactId"] != senthil {
		t.Fatal("the case is filled with the vehicle's customer")
	}
	// Someone else's vehicle, or an account they aren't part of: refused.
	if r := post("cases", map[string]any{"name": "Mixed", "assetId": bike, "contactId": revathi}); r.Status != 422 || !strings.Contains(r.Raw, "Senthil Kumar") {
		t.Fatalf("another customer on Senthil's bike: %d %s", r.Status, truncate(r.Raw, 300))
	}
	if r := post("cases", map[string]any{"name": "Mixed", "assetId": bike, "accountId": balaji}); r.Status != 422 || !strings.Contains(r.Raw, `"accountId"`) {
		t.Fatalf("an account Senthil isn't part of: %d %s", r.Status, truncate(r.Raw, 300))
	}
	if r := post("service_dues", map[string]any{"assetId": bike, "contactId": revathi, "dueDate": "2026-12-01"}); r.Status != 422 {
		t.Fatalf("a service of Senthil's bike for Revathi: %d %s", r.Status, truncate(r.Raw, 300))
	}
	// Changing the case to another customer later is refused too.
	if r := edit(t, boss, ws, "cases", c, map[string]any{"contactId": revathi}); r.Status != 422 {
		t.Fatalf("editing the case to another customer: %d", r.Status)
	}
	// No vehicle: a contact brings their account; a contact of another account is refused.
	q := create(t, boss, ws, "quotes", map[string]any{"contactId": ravi})
	if get(t, boss, ws, "quotes", q)["accountId"] != balaji {
		t.Fatal("the quote takes Ravi's account")
	}
	other := create(t, boss, ws, "accounts", map[string]any{"name": "Swift Couriers"})
	if r := post("quotes", map[string]any{"contactId": ravi, "accountId": other}); r.Status != 422 || !strings.Contains(r.Raw, "Balaji Auto Finance") {
		t.Fatalf("Ravi on Swift's quote: %d %s", r.Status, truncate(r.Raw, 300))
	}
}
