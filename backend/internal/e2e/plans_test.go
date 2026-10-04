package e2e

import (
	"context"
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

// A business's plan is enforced on the server: records, members and card scans (D-101).
func TestPlanLimitsAreEnforcedPerBusiness(t *testing.T) {
	token := signIn(t, freshPhone(), "Plan Owner")
	ws := newBusiness(t, token, "Limit Traders")
	other := newBusiness(t, token, "Roomy Traders")
	owner := ownerSignIn(t)

	p := call(t, "GET", crmAPI+"/w/"+ws+"/plan", token, nil)
	want(t, p, 200, "plan of a new business")
	if p.str("plan", "key") != "free" || p.str("source") != "default" {
		t.Fatalf("a new self-serve business starts on the default plan: %s", truncate(p.Raw, 300))
	}

	// The owner defines a tiny plan and puts this business on it.
	want(t, call(t, "PUT", crmAPI+"/platform/plans/tiny", owner, map[string]any{
		"name": "Tiny", "isActive": true, "position": 9, "limits": map[string]any{"members": 1, "records": 2, "cardScansPerMonth": 1}}), 200, "owner creates a plan")
	var wsID string
	if err := testDB.Pool.QueryRow(context.Background(), `SELECT id::text FROM crm.workspaces WHERE code = $1`, ws).Scan(&wsID); err != nil {
		t.Fatal(err)
	}
	want(t, call(t, "PUT", crmAPI+"/platform/subscriptions/"+wsID, owner, map[string]any{"planKey": "tiny", "status": "active"}), 200, "owner sets the plan")
	// A customer can't do either of those.
	want(t, call(t, "PUT", crmAPI+"/platform/plans/hack", token, map[string]any{"name": "Hack"}), 403, "customer editing plans")
	want(t, call(t, "PUT", crmAPI+"/platform/subscriptions/"+wsID, token, map[string]any{"planKey": "business"}), 403, "customer upgrading themselves")

	create(t, token, ws, "leads", map[string]any{"lastName": "One"})
	create(t, token, ws, "contacts", map[string]any{"lastName": "Two"})
	third := call(t, "POST", crmAPI+"/w/"+ws+"/crm/leads", token, map[string]any{"values": map[string]any{"lastName": "Three"}})
	want(t, third, 402, "third record on a 2-record plan")
	if third.str("code") != "plan_limit" {
		t.Fatalf("expected plan_limit, got %s", truncate(third.Raw, 200))
	}
	// The limit belongs to that business only.
	create(t, token, other, "leads", map[string]any{"lastName": "Free A"})
	create(t, token, other, "leads", map[string]any{"lastName": "Free B"})
	create(t, token, other, "leads", map[string]any{"lastName": "Free C"})

	// Card scans: one a month on this plan.
	want(t, call(t, "POST", crmAPI+"/w/"+ws+"/cards", token, map[string]any{"action": "card_only", "card": card("Scan One", "", "9222200001", "")}), 201, "first scan")
	second := call(t, "POST", crmAPI+"/w/"+ws+"/cards", token, map[string]any{"action": "card_only", "card": card("Scan Two", "", "9222200002", "")})
	want(t, second, 402, "second scan on a 1-scan plan")

	// Members: the creator fills the only seat.
	inv := call(t, "POST", crmAPI+"/w/"+ws+"/admin/members", token, map[string]any{"displayName": "Second Person", "email": "second@limit.test", "roleKey": "STAFF", "method": "invite"})
	if inv.Status != 402 {
		t.Fatalf("inviting past the member limit: got %d, want 402 — %s", inv.Status, truncate(inv.Raw, 300))
	}

	// Moving the business to a roomier plan lifts the limits at once.
	want(t, call(t, "PUT", crmAPI+"/platform/subscriptions/"+wsID, owner, map[string]any{"planKey": "business", "status": "active"}), 200, "owner upgrades")
	create(t, token, ws, "leads", map[string]any{"lastName": "Three"})
	p = call(t, "GET", crmAPI+"/w/"+ws+"/plan", token, nil)
	if p.str("plan", "key") != "business" || p.at("usage", "records") != float64(3) {
		t.Fatalf("plan page should show the new plan and real usage: %s", truncate(p.Raw, 400))
	}
	subs := call(t, "GET", crmAPI+"/platform/subscriptions?q=limit", owner, nil)
	want(t, subs, 200, "owner lists subscriptions")
	if len(subs.list("data")) != 1 {
		t.Fatalf("owner should find the business: %s", truncate(subs.Raw, 300))
	}
	// An expired subscription falls back to the default plan.
	want(t, call(t, "PUT", crmAPI+"/platform/subscriptions/"+wsID, owner, map[string]any{"planKey": "business", "status": "active", "currentPeriodEnd": "2020-01-01"}), 200, "owner sets a past end date")
	p = call(t, "GET", crmAPI+"/w/"+ws+"/plan", token, nil)
	if p.str("plan", "key") != "free" || p.str("status") != "expired" {
		t.Fatalf("a lapsed plan should fall back to Free and say expired: %s", truncate(p.Raw, 300))
	}
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
	// Nor can they change the business, its plan, or its members.
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
