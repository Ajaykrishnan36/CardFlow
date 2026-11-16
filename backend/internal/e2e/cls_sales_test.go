package e2e

import (
	"strings"
	"testing"
)

// The Cloudlight QA run (D-182): a business sets its own deal stages and lead statuses,
// reports never add different currencies together, a sales person's billing page shows only
// their own seat, lead custom values reach the new deal, and a new object belongs to Admins.
func TestBusinessStagesCurrenciesAndAccess(t *testing.T) {
	boss := signIn(t, freshPhone(), "Stage Boss")
	ws := newBusiness(t, boss, "Stage Staffing")
	base := crmAPI + "/w/" + ws
	salesPhone, adminPhone := freshPhone(), freshPhone()
	want(t, call(t, "POST", base+"/admin/members", boss, map[string]any{"displayName": "Sam Sales", "phone": salesPhone, "roleKey": "SALES_STAFF"}), 201, "add sales staff")
	want(t, call(t, "POST", base+"/admin/members", boss, map[string]any{"displayName": "Ada Admin", "phone": adminPhone, "roleKey": "ADMIN"}), 201, "add an admin")
	sales, admin := signIn(t, salesPhone, "Sam Sales"), signIn(t, adminPhone, "Ada Admin")

	// 1. Deal stages.
	stages := []map[string]any{{"value": "qualification", "label": "Qualification", "probability": 10}, {"value": "discovery", "label": "Discovery", "probability": 20},
		{"value": "profiles_submitted", "label": "Profiles Submitted", "probability": 45}, {"value": "contract", "label": "Contract", "probability": 90},
		{"value": "closed_won", "label": "Closed Won", "probability": 100}, {"value": "closed_lost", "label": "Closed Lost", "probability": 0}}
	if r := call(t, "PUT", base+"/crm/meta/opportunities/stages", sales, map[string]any{"stages": stages}); r.Status != 403 {
		t.Fatalf("sales staff can't change stages: %d", r.Status)
	}
	if r := call(t, "PUT", base+"/crm/meta/opportunities/stages", boss, map[string]any{"stages": stages[:3]}); r.Status != 422 {
		t.Fatalf("the closing stages must stay: %d %s", r.Status, truncate(r.Raw, 200))
	}
	deal := create(t, boss, ws, "opportunities", map[string]any{"name": "Old stage deal", "closeDate": "2027-01-31", "status": "prospecting", "amount": 1000.0})
	if r := call(t, "PUT", base+"/crm/meta/opportunities/stages", boss, map[string]any{"stages": stages}); r.Status != 422 || !strings.Contains(r.Raw, "prospecting") {
		t.Fatalf("a stage deals are at can't disappear: %d %s", r.Status, truncate(r.Raw, 200))
	}
	upd := func(token, object, id string, values map[string]any) resp {
		cur := call(t, "GET", base+"/crm/"+object+"/"+id, token, nil)
		var v float64
		if rec, ok := cur.Body["record"].(map[string]any); ok {
			v, _ = rec["version"].(float64)
		}
		return call(t, "PATCH", base+"/crm/"+object+"/"+id, token, map[string]any{"values": values, "expectedVersion": v})
	}
	want(t, upd(boss, "opportunities", deal, map[string]any{"status": "qualification"}), 200, "move the deal to a stage both lists have")
	want(t, call(t, "PUT", base+"/crm/meta/opportunities/stages", boss, map[string]any{"stages": stages}), 200, "save the business's own stages")
	r := upd(boss, "opportunities", deal, map[string]any{"status": "profiles_submitted"})
	want(t, r, 200, "use a new stage")
	if p, _ := r.Body["values"].(map[string]any)["probability"].(float64); p != 45 {
		t.Fatalf("the stage's probability applies, got %v", p)
	}
	want(t, call(t, "PUT", base+"/crm/meta/leads/stages", boss, map[string]any{"stages": []map[string]any{{"value": "new", "label": "New"},
		{"value": "unqualified", "label": "Unqualified"}, {"value": "converted", "label": "Converted"}, {"value": "lost", "label": "Closed lost"}}}), 200, "lead statuses")
	lead := create(t, boss, ws, "leads", map[string]any{"lastName": "Status Lead", "status": "unqualified"})
	if lead == "" {
		t.Fatal("a lead can use the new status")
	}

	// 2. Money: a USD and a GBP deal are added up in the base currency, or split by currency.
	for _, cur := range []string{"USD", "GBP"} {
		want(t, call(t, "PUT", base+"/exchange-rates", boss, map[string]any{"fromCurrency": cur, "rate": map[string]float64{"USD": 80, "GBP": 100}[cur]}), 200, "rate "+cur)
	}
	create(t, boss, ws, "opportunities", map[string]any{"name": "USD deal", "closeDate": "2027-01-31", "status": "contract", "amount": 100.0, "currency": "USD"})
	create(t, boss, ws, "opportunities", map[string]any{"name": "GBP deal", "closeDate": "2027-01-31", "status": "contract", "amount": 100.0, "currency": "GBP"})
	run := func(group string) resp {
		return call(t, "POST", base+"/reports/run", boss, map[string]any{"object": "opportunities", "definition": map[string]any{
			"filters": []map[string]any{{"field": "status", "op": "eq", "value": "contract"}}, "groupBy": group, "measure": map[string]any{"fn": "sum", "field": "amount"}, "chart": "table"}})
	}
	if r := run(""); r.Body["total"] != 18000.0 {
		t.Fatalf("100 USD + 100 GBP is 18,000 in the base currency, got %v", r.Body["total"])
	}
	if r := run("currency"); r.Body["byCurrency"] != true {
		t.Fatalf("split by currency, the rows stay in their own currency: %s", truncate(r.Raw, 300))
	}

	// 3. Billing shows a sales person only their own seat.
	bill := call(t, "GET", base+"/billing", sales, nil)
	want(t, bill, 200, "sales staff billing page")
	if ms, _ := bill.Body["members"].([]any); len(ms) != 1 {
		t.Fatalf("sales staff see only their own seat, got %d", len(ms))
	}

	// 4. A lead's custom value reaches the deal made from it.
	want(t, call(t, "POST", base+"/crm/meta/leads/fields", boss, map[string]any{"label": "Region", "key": "region", "type": "text"}), 201, "lead region")
	want(t, call(t, "POST", base+"/crm/meta/opportunities/fields", boss, map[string]any{"label": "Region", "key": "region", "type": "text"}), 201, "deal region")
	l := create(t, boss, ws, "leads", map[string]any{"lastName": "Carry Lead", "organization": "Carry Co", "region": "Europe"})
	cv := call(t, "POST", base+"/crm/leads/"+l+"/convert", boss, map[string]any{"account": map[string]any{"mode": "new", "name": "Carry Co", "kind": "business"},
		"createContact": true, "opportunity": map[string]any{"create": true, "name": "Carry deal"}})
	want(t, cv, 200, "convert")
	opp := call(t, "GET", base+"/crm/opportunities/"+cv.str("opportunityId"), boss, nil)
	if !strings.Contains(opp.Raw, `"region":"Europe"`) {
		t.Fatalf("the deal keeps the lead's region: %s", truncate(opp.Raw, 300))
	}

	// 5. A business's own object is open to Admins at once.
	obj := map[string]any{"key": "staffing_needs", "singular": "Staffing Need", "plural": "Staffing Needs", "prefix": "SN", "nameLabel": "Need", "icon": "users",
		"fields": []map[string]any{{"key": "resources", "label": "Resources", "type": "number"}}}
	want(t, call(t, "POST", base+"/objects", boss, obj), 201, "create an object")
	want(t, call(t, "POST", base+"/crm/staffing_needs", admin, map[string]any{"values": map[string]any{"name": "2 developers", "resources": 2}}), 201, "an Admin uses the new object")
	if r := call(t, "GET", base+"/crm/staffing_needs", sales, nil); r.Status != 403 {
		t.Fatalf("other sets get it only when an admin adds it: %d", r.Status)
	}
}
