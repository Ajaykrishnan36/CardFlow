package e2e

import (
	"strings"
	"testing"
)

// A dealer's Super Admin sees who added each lead and how each person is doing (D-137).
func TestTeamPerformanceAndWhoAddedWhat(t *testing.T) {
	ram := signIn(t, freshPhone(), "Ram")
	ws := newBusiness(t, ram, "Tata EV Dealer")
	base := crmAPI + "/w/" + ws
	kumarPhone, kavinPhone := freshPhone(), freshPhone()
	want(t, call(t, "POST", base+"/admin/members", ram, map[string]any{"displayName": "Kumar", "phone": kumarPhone, "roleKey": "STAFF"}), 201, "add Kumar")
	want(t, call(t, "POST", base+"/admin/members", ram, map[string]any{"displayName": "Kavin", "phone": kavinPhone, "roleKey": "STAFF"}), 201, "add Kavin")
	kumar, kavin := signIn(t, kumarPhone, ""), signIn(t, kavinPhone, "")
	uma := create(t, kumar, ws, "leads", map[string]any{"lastName": "Uma", "organization": "Uma Travels"})
	create(t, kumar, ws, "leads", map[string]any{"lastName": "Vani"})
	create(t, kavin, ws, "leads", map[string]any{"lastName": "Sam"})
	want(t, call(t, "POST", base+"/crm/leads/"+uma+"/convert", kumar, map[string]any{"account": map[string]any{"mode": "new", "name": "Uma Travels", "kind": "business"}, "createContact": true}), 200, "Kumar converts Uma")

	// The Super Admin's lead list names who added each lead, and it is a default column.
	list := call(t, "GET", base+"/crm/leads", ram, nil)
	if n, _ := list.at("total").(float64); n != 3 {
		t.Fatalf("the Super Admin sees all 3 leads, got %v", list.at("total"))
	}
	for _, row := range list.list("data") {
		m := row.(map[string]any)
		v := m["values"].(map[string]any)
		if id, _ := v["createdBy"].(string); id == "" {
			t.Fatalf("lead %v has nobody as its creator", m["title"])
		}
	}
	meta := call(t, "GET", base+"/crm/meta/leads", ram, nil)
	if !strings.Contains(meta.Raw, `"createdBy"`) || !strings.Contains(meta.Raw, "Added by") {
		t.Fatalf("leads show an Added by column")
	}
	cols := meta.list("listColumns")
	found := false
	for _, c := range cols {
		found = found || c == "createdBy"
	}
	if !found {
		t.Fatalf("Added by is a default list column: %v", cols)
	}
	if n := total(t, kumar, ws, "leads", ""); n != 2 {
		t.Fatalf("Kumar sees only his own 2 leads, got %d", n)
	}

	// Team performance: per person and over time.
	team := call(t, "GET", base+"/dashboard/team?range=month", ram, nil)
	want(t, team, 200, "team performance")
	got := map[string]map[string]any{}
	for _, m := range team.list("members") {
		x := m.(map[string]any)
		got[x["name"].(string)] = x
	}
	if got["Kumar"]["leadsAdded"] != float64(2) || got["Kumar"]["leadsConverted"] != float64(1) || got["Kumar"]["accountsAdded"] != float64(1) || got["Kumar"]["contactsAdded"] != float64(1) {
		t.Fatalf("Kumar: 2 leads added, 1 converted, 1 account, 1 contact — got %v", got["Kumar"])
	}
	if got["Kavin"]["leadsAdded"] != float64(1) || got["Kavin"]["leadsConverted"] != float64(0) {
		t.Fatalf("Kavin: 1 lead added, none converted — got %v", got["Kavin"])
	}
	if _, ok := got["Ram"]; !ok || got["Ram"]["leadsAdded"] != float64(0) {
		t.Fatalf("Ram is listed with nothing added — got %v", got["Ram"])
	}
	sum := 0.0
	for _, b := range team.list("buckets") {
		sum += b.(map[string]any)["leads"].(float64)
	}
	if sum != 3 || team.str("bucket") != "day" {
		t.Fatalf("3 leads across the days of the month, got %v (%s)", sum, team.str("bucket"))
	}
	want(t, call(t, "GET", base+"/dashboard/team?range=year", ram, nil), 200, "by month for a year")
	// Staff who only see their own records don't get the team report.
	want(t, call(t, "GET", base+"/dashboard/team?range=month", kumar, nil), 403, "staff asking for the team report")

	// Bulk import: 12 leads in one go; a bad row is reported, the rest go in. Staff can import too.
	rows := [][]string{}
	for _, n := range []string{"Anu", "Bala", "Chitra", "Dev", "Esha", "Farid", "Gita", "Hari", "Indu", "Jai", "Kala", "Latha"} {
		rows = append(rows, []string{n, "Kumar-import", strings.ToLower(n) + "@example.test"})
	}
	rows = append(rows, []string{"", "", "not-an-email"})
	body := map[string]any{"rows": rows, "mapping": []string{"firstName", "lastName", "email"}, "mode": "create"}
	imp := call(t, "POST", base+"/crm/leads/import", ram, body)
	if imp.Status != 200 || imp.at("created") != float64(12) || imp.at("failed") != float64(1) {
		t.Fatalf("import 12 leads and report the bad row: %d %s", imp.Status, truncate(imp.Raw, 300))
	}
	after := call(t, "GET", base+"/dashboard/team?range=month", ram, nil)
	for _, m := range after.list("members") {
		if x := m.(map[string]any); x["name"] == "Ram" && x["leadsAdded"] != float64(12) {
			t.Fatalf("imported leads count for the person who imported them, got %v", x["leadsAdded"])
		}
	}

	one := call(t, "POST", base+"/crm/leads/import", kavin, map[string]any{"rows": [][]string{{"Staff", "Imported"}}, "mapping": []string{"firstName", "lastName"}, "mode": "create"})
	if one.Status != 200 || one.at("created") != float64(1) {
		t.Fatalf("staff can import leads: %d %s", one.Status, truncate(one.Raw, 200))
	}

	// A case handed to someone else stays readable for the person who logged it.
	cs := create(t, kumar, ws, "cases", map[string]any{"name": "Car won't start"})
	var kavinID string
	for _, m := range call(t, "GET", base+"/admin/members", ram, nil).list("data") {
		if x := m.(map[string]any); x["displayName"] == "Kavin" {
			kavinID = x["identityId"].(string)
		}
	}
	want(t, edit(t, ram, ws, "cases", cs, map[string]any{"ownerId": kavinID}), 200, "assign the case to Kavin")
	want(t, call(t, "GET", base+"/crm/cases/"+cs, kumar, nil), 200, "Kumar still opens the case he logged")
	want(t, edit(t, kumar, ws, "cases", cs, map[string]any{"priority": "low"}), 403, "but can no longer change it")
	want(t, call(t, "GET", base+"/crm/cases/"+cs, kavin, nil), 200, "Kavin opens his case")

	// A deal's items follow the quote the customer accepted.
	item := create(t, ram, ws, "catalog_items", map[string]any{"name": "Nexon", "unitPrice": 1000, "taxRate": 0})
	deal := create(t, ram, ws, "opportunities", map[string]any{"name": "Deal", "amount": 1, "closeDate": today()})
	want(t, call(t, "PUT", base+"/opportunities/"+deal+"/lines", ram, map[string]any{"lines": []map[string]any{{"itemId": item, "quantity": 3}}}), 200, "deal lines")
	quote := create(t, ram, ws, "quotes", map[string]any{"name": "Quote — deal", "opportunityId": deal, "status": "draft"})
	want(t, call(t, "PUT", base+"/quotes/"+quote+"/lines", ram, map[string]any{"lines": []map[string]any{{"itemId": item, "quantity": 2, "discountPercent": 10}}}), 200, "quote lines")
	if o := call(t, "POST", base+"/quotes/"+quote+"/convert", ram, nil); o.Status != 200 && o.Status != 201 {
		t.Fatalf("convert: %d %s", o.Status, o.Raw)
	}
	lines := call(t, "GET", base+"/opportunities/"+deal+"/lines", ram, nil)
	if lines.at("pricing", "total") != float64(1800) || get(t, ram, ws, "opportunities", deal)["amount"] != float64(1800) || len(lines.at("pricing", "lines").([]any)) != 1 {
		t.Fatalf("the deal's items and amount match the accepted quote (2 × 1000 − 10%% = 1800): %s", truncate(lines.Raw, 300))
	}

	// Export with the Added by column.
	exp := call(t, "GET", base+"/crm/leads/export?columns=lastName,createdBy", ram, nil)
	if exp.Status != 200 || !strings.Contains(exp.Raw, "Kumar") || !strings.Contains(exp.Raw, "Kavin") {
		t.Fatalf("the export names who added each lead: %d %s", exp.Status, truncate(exp.Raw, 300))
	}
}
