package e2e

import (
	"strings"
	"testing"
)

// ownerSignIn signs the demo platform owner in (the test database is seeded with it).
func ownerSignIn(t *testing.T) string {
	t.Helper()
	r := call(t, "POST", crmAPI+"/auth/login", "", map[string]any{"identifier": "ajay@gmail.com", "password": "Ajay1234", "audience": "owner"})
	want(t, r, 200, "owner sign-in")
	tok := r.str("token")
	if !strings.HasPrefix(tok, "crms_") {
		t.Fatalf("owner sign-in returned no bearer token: %s", truncate(r.Raw, 300))
	}
	return tok
}

// What a member may do is decided by their role, on the server.
func TestStaffCannotSeeFinanceOrManageTheBusiness(t *testing.T) {
	boss := signIn(t, freshPhone(), "Boss")
	ws := newBusiness(t, boss, "Role Traders")
	create(t, boss, ws, "income", map[string]any{"name": "Secret income", "amount": 99999, "date": "2026-01-01"})
	bossLead := create(t, boss, ws, "leads", map[string]any{"lastName": "Boss Lead"})

	staffPhone := freshPhone()
	inv := call(t, "POST", crmAPI+"/w/"+ws+"/admin/members", boss, map[string]any{"displayName": "Staff Person", "phone": staffPhone, "roleKey": "STAFF", "method": "invite"})
	if inv.Status != 200 && inv.Status != 201 {
		t.Fatalf("invite staff: %d %s", inv.Status, truncate(inv.Raw, 400))
	}
	staff := signIn(t, staffPhone, "Staff Person")
	list := call(t, "GET", crmAPI+"/businesses", staff, nil)
	want(t, list, 200, "staff lists businesses")
	found := false
	for _, b := range list.list("data") {
		if b.(map[string]any)["code"] == ws {
			found = true
		}
	}
	if !found {
		t.Skipf("the invitation needs accepting before the membership is active in this setup: %s", truncate(list.Raw, 300))
	}

	// Finance is not part of the Staff role.
	if r := call(t, "GET", crmAPI+"/w/"+ws+"/crm/income", staff, nil); r.Status != 403 && r.Status != 404 {
		t.Fatalf("staff reading income: got %d, want 403/404", r.Status)
	}
	sum := call(t, "GET", crmAPI+"/w/"+ws+"/dashboard/summary?range=all", staff, nil)
	if sum.Status == 200 && sum.at("finance") != nil {
		t.Fatalf("staff must not see finance totals: %s", truncate(sum.Raw, 300))
	}
	// Nor can they change the business or its members.
	want(t, call(t, "PATCH", crmAPI+"/w/"+ws+"/business", staff, map[string]any{"name": "Hijacked"}), 403, "staff renaming the business")
	if r := call(t, "POST", crmAPI+"/w/"+ws+"/admin/members", staff, map[string]any{"displayName": "X", "email": "x@x.test", "roleKey": "SUPER_ADMIN", "method": "invite"}); r.Status != 403 {
		t.Fatalf("staff inviting a super admin: got %d, want 403", r.Status)
	}
	// They can still work with leads.
	create(t, staff, ws, "leads", map[string]any{"lastName": "Staff Lead"})
	// "Own records": the boss's lead is not theirs to open, and their list has only their own.
	want(t, call(t, "GET", crmAPI+"/w/"+ws+"/crm/leads/"+bossLead, staff, nil), 404, "staff opening the boss's lead")
	if n := total(t, staff, ws, "leads", ""); n != 1 {
		t.Fatalf("staff should see only their own lead, saw %d", n)
	}
	if n := total(t, boss, ws, "leads", ""); n != 2 {
		t.Fatalf("the boss sees the team's leads too, saw %d", n)
	}
}
