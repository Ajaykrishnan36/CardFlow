package e2e

import (
	"context"
	"strings"
	"testing"
)

// Validation rules and default values an admin sets per object (D-141).
func TestValidationRulesAndDefaults(t *testing.T) {
	boss := signIn(t, freshPhone(), "Boss")
	ws := newBusiness(t, boss, "Rules Co")
	base := crmAPI + "/w/" + ws
	staff, _ := staffOf(t, boss, ws, "Rule Staff")

	// Only someone who can customise the business writes rules.
	want(t, call(t, "GET", base+"/crm/meta/opportunities/rules", staff, nil), 403, "staff reading the rules")

	// A rule needs a name, a message and at least one condition on a real field.
	bad := call(t, "POST", base+"/crm/meta/opportunities/rules", boss, map[string]any{"name": "", "message": "", "filter": map[string]any{"op": "and", "filters": []any{}}})
	want(t, bad, 422, "empty rule")
	bad = call(t, "POST", base+"/crm/meta/opportunities/rules", boss, map[string]any{"name": "X", "message": "Y",
		"filter": map[string]any{"op": "and", "filters": []any{map[string]any{"field": "nope", "op": "eq", "value": 1}}}})
	want(t, bad, 422, "rule on an unknown field")

	// "A deal of 5 lakh or more needs a next step."
	rule := map[string]any{"name": "Big deals need a next step", "message": "Add the next step for deals of 5 lakh and above.", "field": "nextStep",
		"filter": map[string]any{"op": "and", "filters": []any{
			map[string]any{"field": "amount", "op": "gte", "value": 500000},
			map[string]any{"field": "nextStep", "op": "empty"},
		}}}
	made := call(t, "POST", base+"/crm/meta/opportunities/rules", boss, rule)
	want(t, made, 200, "create a rule")
	rules := made.list("rules")
	if len(rules) != 1 {
		t.Fatalf("one rule expected, got %s", truncate(made.Raw, 300))
	}
	ruleID := rules[0].(map[string]any)["id"].(string)
	want(t, call(t, "POST", base+"/crm/meta/opportunities/rules", boss, rule), 422, "a second rule with the same name")

	// The rule holds for the boss and for staff, on create and on edit.
	for who, tok := range map[string]string{"boss": boss, "staff": staff} {
		r := call(t, "POST", base+"/crm/opportunities", tok, map[string]any{"values": map[string]any{"name": "Fleet order " + who, "amount": 750000, "closeDate": today()}})
		if r.Status != 422 || !strings.Contains(r.Raw, "Add the next step") || !strings.Contains(r.Raw, "nextStep") {
			t.Fatalf("%s creating a big deal with no next step: %d %s", who, r.Status, truncate(r.Raw, 300))
		}
	}
	if n := total(t, boss, ws, "opportunities", ""); n != 0 {
		t.Fatalf("a refused deal was still saved: %d", n)
	}
	small := create(t, boss, ws, "opportunities", map[string]any{"name": "Small order", "amount": 40000, "closeDate": today()})
	r := edit(t, boss, ws, "opportunities", small, map[string]any{"amount": 900000})
	if r.Status != 422 {
		t.Fatalf("raising the amount past the rule: %d %s", r.Status, truncate(r.Raw, 300))
	}
	if got := get(t, boss, ws, "opportunities", small)["amount"]; num2(got) != 40000 {
		t.Fatalf("the refused edit changed the amount to %v", got)
	}
	want(t, edit(t, boss, ws, "opportunities", small, map[string]any{"amount": 900000, "nextStep": "Send the fleet quote"}), 200, "an edit that satisfies the rule")

	// Switched off, it stops applying; deleted, it is gone.
	want(t, call(t, "PATCH", base+"/crm/meta/opportunities/rules/"+ruleID, boss, map[string]any{"active": false}), 200, "switch the rule off")
	create(t, boss, ws, "opportunities", map[string]any{"name": "Big, rule off", "amount": 800000, "closeDate": today()})
	gone := call(t, "DELETE", base+"/crm/meta/opportunities/rules/"+ruleID, boss, nil)
	want(t, gone, 200, "delete the rule")
	if len(gone.list("rules")) != 0 {
		t.Fatalf("the rule is still listed: %s", truncate(gone.Raw, 300))
	}

	// Defaults: a new lead starts as a Walk-in from Chennai unless told otherwise.
	want(t, call(t, "PUT", base+"/crm/meta/leads/defaults", boss, map[string]any{"defaults": map[string]any{"nope": "x"}}), 422, "default on an unknown field")
	want(t, call(t, "PUT", base+"/crm/meta/leads/defaults", boss, map[string]any{"defaults": map[string]any{"source": "not-a-source"}}), 422, "default that isn't one of the options")
	want(t, call(t, "PUT", base+"/crm/meta/leads/defaults", staff, map[string]any{"defaults": map[string]any{"city": "Chennai"}}), 403, "staff setting defaults")
	saved := call(t, "PUT", base+"/crm/meta/leads/defaults", boss, map[string]any{"defaults": map[string]any{"city": "Chennai", "rating": "warm"}})
	want(t, saved, 200, "save defaults")
	if saved.str("defaults", "city") != "Chennai" {
		t.Fatalf("defaults not returned: %s", truncate(saved.Raw, 300))
	}
	// The form is told, so the person sees them before saving.
	if m := call(t, "GET", base+"/crm/meta/leads", staff, nil); m.str("defaults", "city") != "Chennai" {
		t.Fatalf("the lead form isn't given the defaults: %s", truncate(m.Raw, 200))
	}
	a := get(t, staff, ws, "leads", create(t, staff, ws, "leads", map[string]any{"firstName": "Arun", "lastName": "Default", "phone": freshPhone()}))
	if a["city"] != "Chennai" || a["rating"] != "warm" {
		t.Fatalf("a lead with nothing given: city %v, rating %v", a["city"], a["rating"])
	}
	b := get(t, staff, ws, "leads", create(t, staff, ws, "leads", map[string]any{"firstName": "Bala", "lastName": "Given", "phone": freshPhone(), "city": "Madurai"}))
	if b["city"] != "Madurai" || b["rating"] != "warm" {
		t.Fatalf("a lead with a city given: city %v, rating %v", b["city"], b["rating"])
	}
	// Clearing them.
	want(t, call(t, "PUT", base+"/crm/meta/leads/defaults", boss, map[string]any{"defaults": map[string]any{}}), 200, "clear defaults")
	c := get(t, staff, ws, "leads", create(t, staff, ws, "leads", map[string]any{"firstName": "Chitra", "lastName": "Plain", "phone": freshPhone()}))
	if c["city"] != nil && c["city"] != "" {
		t.Fatalf("a cleared default still applies: %v", c["city"])
	}
}

// A business limits where its people work from and how long a sign-in lasts (D-142).
func TestLoginPolicy(t *testing.T) {
	ctx := context.Background()
	boss := signIn(t, freshPhone(), "Boss")
	ws := newBusiness(t, boss, "Policy Co")
	base := crmAPI + "/w/" + ws
	staff, _ := staffOf(t, boss, ws, "Policy Staff")
	from := func(ip string, f func()) {
		clientIP = ip
		defer func() { clientIP = "" }()
		f()
	}

	want(t, call(t, "GET", base+"/login-policy", staff, nil), 403, "staff reading the login policy")
	from("203.0.113.7", func() {
		p := call(t, "GET", base+"/login-policy", boss, nil)
		want(t, p, 200, "read the policy")
		if p.str("yourIp") != "203.0.113.7" || len(p.list("ipRanges")) != 0 {
			t.Fatalf("an empty policy that shows the caller's address expected: %s", p.Raw)
		}
		want(t, call(t, "PUT", base+"/login-policy", boss, map[string]any{"ipRanges": []string{"not-an-ip"}}), 422, "a range that isn't one")
		// A list without the admin's own address would lock them out: refused.
		r := call(t, "PUT", base+"/login-policy", boss, map[string]any{"ipRanges": []string{"198.51.100.0/24"}})
		if r.Status != 422 || !strings.Contains(r.Raw, "locked out") {
			t.Fatalf("saving a list that excludes yourself: %d %s", r.Status, truncate(r.Raw, 300))
		}
		ok := call(t, "PUT", base+"/login-policy", boss, map[string]any{"ipRanges": []string{"203.0.113.0/24", "198.51.100.9"}})
		want(t, ok, 200, "save the office networks")
		if got := ok.list("ipRanges"); len(got) != 2 || got[1] != "198.51.100.9/32" {
			t.Fatalf("ranges are stored in one form: %v", got)
		}
		want(t, call(t, "GET", base+"/crm/leads", staff, nil), 200, "staff inside the office network")
	})
	from("192.0.2.44", func() {
		r := call(t, "GET", base+"/crm/leads", staff, nil)
		if r.Status != 403 || r.str("code") != "network_not_allowed" {
			t.Fatalf("staff from outside the approved networks: %d %s", r.Status, truncate(r.Raw, 300))
		}
		// Their other businesses are not affected.
		want(t, call(t, "GET", crmAPI+"/businesses", staff, nil), 200, "the list of their businesses")
	})
	from("198.51.100.9", func() {
		want(t, call(t, "GET", base+"/crm/leads", staff, nil), 200, "staff from the single approved address")
		// Clearing the list opens it up again; a sign-in then lasts 8 hours.
		want(t, call(t, "PUT", base+"/login-policy", boss, map[string]any{"ipRanges": []string{}, "sessionHours": 0}), 422, "zero hours")
		want(t, call(t, "PUT", base+"/login-policy", boss, map[string]any{"ipRanges": []string{}, "sessionHours": 8}), 200, "sign in again after 8 hours")
	})
	want(t, call(t, "GET", base+"/crm/leads", staff, nil), 200, "a fresh sign-in is fine")
	// The staff member's sign-in is now nine hours old.
	if _, err := testDB.Pool.Exec(ctx, `UPDATE crm.sessions s SET created_at = now() - interval '9 hours'
		FROM crm.identities i WHERE i.id = s.identity_id AND i.display_name = 'Policy Staff'`); err != nil {
		t.Fatal(err)
	}
	old := call(t, "GET", base+"/crm/leads", staff, nil)
	if old.Status != 401 || old.str("code") != "session_expired" {
		t.Fatalf("a sign-in older than the business allows: %d %s", old.Status, truncate(old.Raw, 300))
	}
	want(t, call(t, "GET", base+"/crm/leads", boss, nil), 200, "the boss's newer sign-in still works")
}
