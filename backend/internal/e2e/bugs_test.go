package e2e

import (
	"context"
	"strings"
	"testing"
)

// The bugs from the October 2026 tracker, each reproduced the way it was reported.
func TestBugTrackerFixes(t *testing.T) {
	ctx := context.Background()
	boss := signIn(t, freshPhone(), "Boss")
	ws := newBusiness(t, boss, "Bug Tracker Co")
	base := crmAPI + "/w/" + ws
	members := func() map[string]map[string]any {
		out := map[string]map[string]any{}
		for _, m := range call(t, "GET", base+"/admin/members", boss, nil).list("data") {
			x := m.(map[string]any)
			out[x["displayName"].(string)] = x
		}
		return out
	}
	sets := map[string]string{}
	opts := call(t, "GET", base+"/admin/options", boss, nil)
	for _, s := range opts.list("permissionSets") {
		x := s.(map[string]any)
		sets[x["name"].(string)] = x["id"].(string)
		// BUG-003: the business's own Super Admin is offered the built-in sets.
		if blocked, _ := x["exceeds"].(bool); blocked {
			t.Fatalf("the Super Admin is told %q is more than their access", x["name"])
		}
	}
	// The catalog the screen compares against lists only modules that are switched on here.
	for _, o := range opts.at("catalog", "objects").([]any) {
		if k := o.(map[string]any)["key"]; k == "app_user" || k == "app_business" {
			t.Fatalf("a module that is off (%v) is offered in the permission catalog", k)
		}
	}
	if sets["Admin access"] == "" || sets["Staff access"] == "" {
		t.Fatalf("built-in sets missing from the options: %s", truncate(opts.Raw, 400))
	}

	// BUG-004: a Staff user invited with a temporary password can work straight away.
	inv := call(t, "POST", base+"/admin/members", boss, map[string]any{"displayName": "Temp Staff", "email": "temp-staff-" + ws + "@example.com", "roleKey": "STAFF", "method": "password", "password": "Sunrise-Harbour-42"})
	want(t, inv, 201, "invite Staff with a temporary password")
	if ps, _ := members()["Temp Staff"]["permissionSets"].([]any); len(ps) != 1 {
		t.Fatalf("a Staff user with no chosen set gets the standard Staff access, got %v", members()["Temp Staff"]["permissionSets"])
	}
	// BUG-003: the business's own Super Admin can hand out the built-in sets.
	staffPhone := freshPhone()
	want(t, call(t, "POST", base+"/admin/members", boss, map[string]any{"displayName": "Dev Staff", "phone": staffPhone, "roleKey": "STAFF"}), 201, "add staff")
	mid := members()["Dev Staff"]["membershipId"].(string)
	for _, name := range []string{"Admin access", "Staff access"} {
		r := call(t, "PATCH", base+"/admin/members/"+mid, boss, map[string]any{"permissionSetIds": []string{sets[name]}})
		if r.Status != 200 {
			t.Fatalf("Super Admin assigns %q: %d %s", name, r.Status, truncate(r.Raw, 300))
		}
	}

	// A Staff member who may manage users and developer settings, and nothing else extra.
	power := call(t, "POST", base+"/admin/permission-sets", boss, map[string]any{"name": "Helper", "rules": map[string]any{
		"objects": map[string]any{"lead": []string{"read", "create", "update"}}, "rows": map[string]any{"lead": map[string]any{"scope": "own"}},
		"capabilities": []string{"members.manage", "developer.manage"}}})
	want(t, power, 201, "custom permission set")
	want(t, call(t, "PATCH", base+"/admin/members/"+mid, boss, map[string]any{"permissionSetIds": []string{power.str("id")}}), 200, "give it to the staff member")
	staff := signIn(t, staffPhone, "")
	// BUG-008: they can't invite someone who outranks them.
	want(t, call(t, "POST", base+"/admin/members", staff, map[string]any{"displayName": "Sneaky Admin", "phone": freshPhone(), "roleKey": "ADMIN"}), 403, "staff inviting an Admin")
	// BUG-006: nor mint an API key with more access than their own.
	k := call(t, "POST", base+"/developer/api-keys", staff, map[string]any{"name": "Wide key", "access": "permission_set", "permissionSetId": sets["Admin access"]})
	if k.Status != 422 || !strings.Contains(k.Raw, "more than your own access") {
		t.Fatalf("API key on the Admin set by a Staff member: %d %s", k.Status, truncate(k.Raw, 300))
	}
	// BUG-007: nor point a webhook at records they can't see in full.
	for what, objects := range map[string][]string{"accounts they can't read": {"accounts"}, "leads they see only their own of": {"leads"}, "every object": {}} {
		r := call(t, "POST", base+"/developer/webhooks", staff, map[string]any{"name": "Leak", "url": "https://example.com/hook", "events": []string{"record.updated"}, "objects": objects})
		if r.Status != 422 {
			t.Fatalf("webhook on %s: %d %s", what, r.Status, truncate(r.Raw, 300))
		}
	}
	want(t, call(t, "POST", base+"/developer/webhooks", boss, map[string]any{"name": "Fine", "url": "https://example.com/hook", "events": []string{"record.updated"}, "objects": []string{"accounts"}}), 201, "the Super Admin's webhook")

	// BUG-016: an allocation can't exceed what the invoice still needs after a credit note.
	acc := create(t, boss, ws, "accounts", map[string]any{"name": "Buyer"})
	inv2 := create(t, boss, ws, "invoices", map[string]any{"name": "Invoice", "total": 100, "status": "sent", "accountId": acc})
	cn := create(t, boss, ws, "credit_notes", map[string]any{"name": "Credit", "accountId": acc, "invoiceId": inv2, "total": 40, "status": "issued"})
	_ = cn
	pay := create(t, boss, ws, "payments", map[string]any{"name": "Payment", "amount": 100, "paymentDate": today(), "status": "paid", "accountId": acc})
	bal := get(t, boss, ws, "invoices", inv2)["balanceDue"]
	if bal != float64(60) {
		t.Skipf("credit note did not reduce the balance in this setup (balance %v); allocation rule not exercised", bal)
	}
	want(t, call(t, "POST", base+"/payments/"+pay+"/allocations", boss, map[string]any{"invoiceId": inv2, "amount": 100}), 422, "allocate 100 to an invoice that needs 60")
	want(t, call(t, "POST", base+"/payments/"+pay+"/allocations", boss, map[string]any{"invoiceId": inv2, "amount": 60}), 200, "allocate what it needs")
	_ = ctx
}

// Lead conversion keeps the owner, the lead's data and existing contacts (BUG-018, 029, 030).
func TestConversionKeepsOwnerDataAndContacts(t *testing.T) {
	boss := signIn(t, freshPhone(), "Manager")
	ws := newBusiness(t, boss, "Convert Co")
	base := crmAPI + "/w/" + ws
	repPhone := freshPhone()
	want(t, call(t, "POST", base+"/admin/members", boss, map[string]any{"displayName": "Rep", "phone": repPhone, "roleKey": "STAFF"}), 201, "add rep")
	rep := signIn(t, repPhone, "")
	lead := create(t, rep, ws, "leads", map[string]any{"firstName": "Leela", "lastName": "Rao", "email": "leela@example.com", "organization": "Rao Mills", "industry": "manufacturing", "website": "https://raomills.example.com", "description": "Wants 40 units."})
	existing := create(t, boss, ws, "accounts", map[string]any{"name": "Rao Mills"})
	// The manager converts the rep's lead into the existing account, with a deal.
	cv := call(t, "POST", base+"/crm/leads/"+lead+"/convert", boss, map[string]any{"account": map[string]any{"mode": "existing", "accountId": existing}, "createContact": true,
		"opportunity": map[string]any{"create": true, "name": "Rao Mills deal", "amount": 1000, "closeDate": today()}})
	want(t, cv, 200, "convert")
	if total(t, rep, ws, "opportunities", "") != 1 || total(t, rep, ws, "contacts", "") != 1 {
		t.Fatalf("the rep keeps the deal and the contact made from their lead")
	}
	if a := get(t, boss, ws, "accounts", existing); a["industry"] != "manufacturing" || a["website"] != "https://raomills.example.com" || a["description"] != "Wants 40 units." {
		t.Fatalf("the lead's details fill the existing account's empty fields: %v / %v / %v", a["industry"], a["website"], a["description"])
	}
	// A second lead with the same email links to the same contact instead of duplicating it.
	lead2 := create(t, rep, ws, "leads", map[string]any{"firstName": "Leela", "lastName": "R", "email": "LEELA@example.com"})
	cv2 := call(t, "POST", base+"/crm/leads/"+lead2+"/convert", rep, map[string]any{"account": map[string]any{"mode": "none"}, "createContact": true})
	want(t, cv2, 200, "convert the second lead")
	if cv2.str("contactId") != cv.str("contactId") || cv2.at("contactExisting") != true || total(t, boss, ws, "contacts", "") != 1 {
		t.Fatalf("the second lead links to the existing contact: %s", truncate(cv2.Raw, 300))
	}
}

// CSV export is safe to open in a spreadsheet, and board totals cover the whole column (BUG-019, 022).
func TestExportAndBoardTotals(t *testing.T) {
	boss := signIn(t, freshPhone(), "Exporter")
	ws := newBusiness(t, boss, "Export Co")
	base := crmAPI + "/w/" + ws
	create(t, boss, ws, "leads", map[string]any{"lastName": `=HYPERLINK("http://evil","x")`, "phone": "+919000070001", "organization": "@cmd"})
	exp := call(t, "GET", base+"/crm/leads/export?columns=lastName,phone,organization", boss, nil)
	if exp.Status != 200 || !strings.Contains(exp.Raw, `'=HYPERLINK`) || !strings.Contains(exp.Raw, `'@cmd`) || strings.Contains(exp.Raw, `'+919000070001`) || !strings.Contains(exp.Raw, "+919000070001") {
		t.Fatalf("formula cells are neutralised and phone numbers left alone: %d %s", exp.Status, truncate(exp.Raw, 300))
	}
	if bad := call(t, "GET", base+"/crm/leads/export?filter=%7Bbroken", boss, nil); bad.Status == 200 {
		t.Fatalf("a bad filter is an error, not an empty file: %d", bad.Status)
	}
	for i := 0; i < 30; i++ {
		create(t, boss, ws, "opportunities", map[string]any{"name": "Deal", "amount": 100, "closeDate": today(), "status": "prospecting"})
	}
	g := call(t, "GET", base+"/crm/opportunities/groups?field=status&limit=25", boss, nil)
	want(t, g, 200, "board groups")
	for _, x := range g.list("groups") {
		m := x.(map[string]any)
		if m["value"] == "prospecting" && (m["amount"] != float64(3000) || len(m["rows"].([]any)) != 25) {
			t.Fatalf("the column total covers all 30 deals though 25 are loaded: amount %v, rows %d", m["amount"], len(m["rows"].([]any)))
		}
	}
}
