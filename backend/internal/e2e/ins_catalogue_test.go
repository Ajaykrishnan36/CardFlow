package e2e

import (
	"strings"
	"testing"
	"time"
)

// The rest of the Insurance Agent catalogue (D-184): the policy-year table, the premium
// calculator, the annual plan with the catalogue's worked example, insurer statements and
// commission invoices, licences, consent requests, an existing-book import, a quote shared
// with the customer and the insurance report.
func TestInsuranceCatalogueEndToEnd(t *testing.T) {
	boss, ws := packBusiness(t, "Catalogue Insurance", "Insurance")
	base := crmAPI + "/w/" + ws
	day := func(n int) string { return time.Now().AddDate(0, 0, n).Format("2006-01-02") }
	lic := create(t, boss, ws, "accounts", map[string]any{"name": "LIC of India", "kind": "business", "type": "partner"})
	for _, r := range []map[string]any{{"yearFrom": 1, "yearTo": 1, "rate": 25}, {"yearFrom": 2, "yearTo": 3, "rate": 7.5}, {"yearFrom": 4, "yearTo": 99, "rate": 5}} {
		r["insurerId"], r["policyClass"] = lic, "life"
		create(t, boss, ws, "commission_rules", r)
	}
	cust := create(t, boss, ws, "contacts", map[string]any{"firstName": "Arun", "lastName": "Prakash", "mobile": "+91 98111 70001"})

	// J8: a 10-year term, 5-pay policy builds ten year rows from the rate card.
	pol := create(t, boss, ws, "insurance_policies", map[string]any{"contactId": cust, "insurerId": lic, "policyClass": "life", "premium": 15000,
		"premiumType": "fixed", "policyTerm": 10, "premiumPayingTerm": 5, "status": "proposal"})
	want(t, edit(t, boss, ws, "insurance_policies", pol, map[string]any{"status": "in_force", "policyNumber": "CAT-1", "startDate": day(0)}), 200, "issue")
	rows := call(t, "GET", base+"/crm/policy_years?limit=50", boss, nil).list("data")
	got := map[int][2]float64{}
	for _, x := range rows {
		v := x.(map[string]any)["values"].(map[string]any)
		got[int(num2(v["policyYear"]))] = [2]float64{num2(v["premium"]), num2(v["expectedCommission"])}
	}
	if len(got) != 10 || got[1] != [2]float64{15000, 3750} || got[2] != [2]float64{15000, 1125} || got[4] != [2]float64{15000, 750} || got[6] != [2]float64{0, 0} {
		t.Fatalf("year table: %v", got)
	}

	// B6/J5/J6: the calculator, with commission.
	item := create(t, boss, ws, "catalog_items", map[string]any{"name": "Tech Term", "category": "Life", "unitPrice": 15000})
	book := create(t, boss, ws, "price_books", map[string]any{"name": "LIC rates", "accountId": lic, "status": "active"})
	want(t, call(t, "POST", base+"/price-book-entries", boss, map[string]any{"priceBookId": book, "itemId": item, "unitPrice": 15000, "listPrice": 15000, "validFrom": "2026-01-01"}), 200, "entry")
	create(t, boss, ws, "premium_rates", map[string]any{"itemId": item, "ageFrom": 36, "ageTo": 45, "ratePerThousand": 1.8})
	calc := get(t, boss, ws, "premium_calcs", create(t, boss, ws, "premium_calcs", map[string]any{"itemId": item, "age": 40, "sumAssured": 10000000, "frequency": "monthly"}))
	if num2(calc["yearlyPremium"]) != 18000 || num2(calc["instalment"]) != 1575 || num2(calc["commissionYear1"]) != 4500 || num2(calc["commissionRenewal"]) != 1350 {
		t.Fatalf("calculator: %v", calc)
	}

	// J17/J18: the catalogue's plan of 100 customers.
	plan := get(t, boss, ws, "sales_plans", create(t, boss, ws, "sales_plans", map[string]any{"planStart": day(-30), "targetCustomers": 100}))
	if num2(plan["year1Commission"]) != 298800 || num2(plan["year2Commission"]) != 153900 || num2(plan["quotesNeeded"]) != 400 || num2(plan["leadsNeeded"]) != 800 {
		t.Fatalf("plan: %v %v %v %v", plan["year1Commission"], plan["year2Commission"], plan["quotesNeeded"], plan["leadsNeeded"])
	}

	// C7: a statement line pays the commission; C9: an invoice bills what is still due, once.
	line := get(t, boss, ws, "statement_lines", create(t, boss, ws, "statement_lines", map[string]any{"insurerId": lic, "policyNumber": "cat-1", "amount": 3000, "tds": 150}))
	if line["status"] != "short" {
		t.Fatalf("3,150 against 3,750 is short paid: %v %v", line["status"], line["message"])
	}
	cust2 := create(t, boss, ws, "contacts", map[string]any{"lastName": "Second", "mobile": "+91 98111 70002"})
	p2 := create(t, boss, ws, "insurance_policies", map[string]any{"contactId": cust2, "insurerId": lic, "policyClass": "life", "premium": 10000, "status": "proposal"})
	want(t, edit(t, boss, ws, "insurance_policies", p2, map[string]any{"status": "in_force", "policyNumber": "CAT-2", "startDate": day(0)}), 200, "issue 2")
	inv := get(t, boss, ws, "commission_invoices", create(t, boss, ws, "commission_invoices", map[string]any{"insurerId": lic}))
	if inv["status"] != "made" || num2(inv["commissionTotal"]) != 6250 || num2(inv["gst"]) != 1125 {
		t.Fatalf("invoice for 3,750 + 2,500 with 18%% GST: %v %v %v", inv["status"], inv["commissionTotal"], inv["gst"])
	}
	if again := get(t, boss, ws, "commission_invoices", create(t, boss, ws, "commission_invoices", map[string]any{"insurerId": lic})); again["status"] != "nothing" {
		t.Fatal("nothing is billed twice")
	}

	// F7: an expired licence blocks the sale.
	me := call(t, "GET", crmAPI+"/me", boss, nil).str("identity", "id")
	create(t, boss, ws, "agent_licences", map[string]any{"holderId": me, "validUntil": day(-1)})
	p3 := create(t, boss, ws, "insurance_policies", map[string]any{"contactId": cust2, "insurerId": lic, "policyClass": "life", "premium": 5000, "status": "proposal"})
	mustRefuse(t, "a sale on an expired licence", edit(t, boss, ws, "insurance_policies", p3, map[string]any{"status": "in_force", "policyNumber": "CAT-3", "startDate": day(0)}), "status")

	// I2: two attempts at most; a wrong number blocks further requests.
	for i := 0; i < 2; i++ {
		r := create(t, boss, ws, "consent_requests", map[string]any{"contactId": cust, "channel": "whatsapp", "purpose": "marketing"})
		want(t, edit(t, boss, ws, "consent_requests", r, map[string]any{"outcome": "no_reply"}), 200, "no reply")
	}
	mustRefuse(t, "a third request", call(t, "POST", base+"/crm/consent_requests", boss, map[string]any{"values": map[string]any{"contactId": cust, "channel": "whatsapp", "purpose": "marketing"}}), "contactId")

	// H: an existing book — checked first (nothing written), then imported.
	batch := create(t, boss, ws, "book_batches", map[string]any{"name": "Old book", "status": "draft"})
	imp := call(t, "POST", base+"/crm/book_rows/import", boss, map[string]any{"mode": "create",
		"mapping": []string{"batchId", "rowNumber", "customerName", "mobile", "insurer", "policyClass", "policyNumber", "startDate", "endDate", "premium", "notes"},
		"rows": [][]string{{batch, "1", "Lata S", "9876543210", "LIC of India", "life", "IMP-1", day(-100), day(265), "24000", "diabetic"},
			{batch, "2", "Bad Mobile", "123", "LIC of India", "life", "IMP-2", day(-10), day(355), "9000", ""},
			{batch, "3", "Unknown Insurer", "9876543212", "Nobody Insurance", "life", "IMP-3", day(-10), day(355), "9000", ""}}})
	want(t, imp, 200, "load rows")
	want(t, edit(t, boss, ws, "book_batches", batch, map[string]any{"status": "checked"}), 200, "check")
	b := get(t, boss, ws, "book_batches", batch)
	if num2(b["rowsRejected"]) != 1 || num2(b["rowsHeld"]) != 1 || num2(b["rowsWarning"]) != 1 {
		t.Fatalf("dry run: %v %v %v — %v", b["rowsWarning"], b["rowsHeld"], b["rowsRejected"], b["report"])
	}
	want(t, edit(t, boss, ws, "book_batches", batch, map[string]any{"status": "imported"}), 200, "import")
	if b = get(t, boss, ws, "book_batches", batch); num2(b["policiesCreated"]) != 1 {
		t.Fatalf("one good row imported: %v", b["policiesCreated"])
	}

	// B7: a shared quote shows the customer no commission; the report opens for the agent.
	q := create(t, boss, ws, "quotes", map[string]any{"name": "Arun term", "contactId": cust, "priceBookId": book})
	want(t, call(t, "PUT", base+"/quotes/"+q+"/lines", boss, map[string]any{"lines": []map[string]any{{"itemId": item, "quantity": 1}}}), 200, "price")
	if qq := get(t, boss, ws, "quotes", q); num2(qq["commissionYear1"]) != 3750 {
		t.Fatalf("the agent's quote shows commission: %v", qq["commissionYear1"])
	}
	sh := call(t, "POST", base+"/quotes/"+q+"/share", boss, map[string]any{})
	want(t, sh, 201, "share")
	pub := call(t, "GET", crmAPI+"/public/shared/"+sh.str("token"), "", nil)
	want(t, pub, 200, "customer opens")
	if strings.Contains(strings.ToLower(pub.Raw), "commission") {
		t.Fatalf("the customer's copy never shows commission: %s", truncate(pub.Raw, 300))
	}
	rep := call(t, "GET", base+"/insurance/report", boss, nil)
	want(t, rep, 200, "report")
	if rep.Body["draft"] != true || len(rep.list("years")) != 5 {
		t.Fatalf("report: draft while short paid, five years: %v", truncate(rep.Raw, 300))
	}
}
