package e2e

import (
	"strings"
	"testing"
	"time"
)

// A catalog entry is always of a product (D-171), and a quote's balance can be split into a
// payment plan that the money taken against the quote pays off in date order; the last
// instalment paid makes the invoice.
func TestCatalogAndPaymentPlan(t *testing.T) {
	boss := signIn(t, freshPhone(), "Plan Owner")
	made := call(t, "POST", crmAPI+"/businesses", boss, map[string]any{"name": "Plan Motors", "industry": "Automotive"})
	want(t, made, 201, "create the dealer")
	ws := made.str("business", "code")
	base := crmAPI + "/w/" + ws
	bike := create(t, boss, ws, "catalog_items", map[string]any{"name": "Hero Xtreme 160R", "unitPrice": 100000, "taxRate": 28})

	// Catalog: the product first, then the details.
	r := call(t, "POST", base+"/crm/catalog_entries", boss, map[string]any{"values": map[string]any{"variant": "Stealth", "colours": "Matt Black"}})
	if r.Status != 422 || !strings.Contains(r.Raw, `"itemId"`) {
		t.Fatalf("a catalog entry needs its product: %d %s", r.Status, truncate(r.Raw, 300))
	}
	ce := create(t, boss, ws, "catalog_entries", map[string]any{"itemId": bike, "variant": "Stealth", "colours": "Matt Black, Red", "engine": "163 cc", "weight": "139 kg", "features": "LED lamps, digital console"})
	if n := get(t, boss, ws, "catalog_entries", ce)["name"]; n != "Hero Xtreme 160R · Stealth" {
		t.Fatalf("the entry is named from its product and variant: %v", n)
	}

	// Sales staff look the catalog up; changing it is for admins.
	phone := freshPhone()
	want(t, call(t, "POST", base+"/admin/members", boss, map[string]any{"displayName": "Catalog Sales", "phone": phone, "roleKey": "SALES_STAFF"}), 201, "add sales staff")
	sales := signIn(t, phone, "Catalog Sales")
	if r := call(t, "GET", base+"/crm/catalog_entries", sales, nil); r.Status != 200 || r.at("total").(float64) != 1 {
		t.Fatalf("sales staff see the catalog: %d %s", r.Status, truncate(r.Raw, 200))
	}
	want(t, call(t, "POST", base+"/crm/catalog_entries", sales, map[string]any{"values": map[string]any{"itemId": bike}}), 403, "sales staff adding to the catalog")

	// A quote of ₹1,28,000, split into 4.
	contact := create(t, boss, ws, "contacts", map[string]any{"firstName": "Divya", "lastName": "N", "mobile": "9000077101"})
	q := create(t, boss, ws, "quotes", map[string]any{"contactId": contact, "status": "draft"})
	want(t, call(t, "PUT", base+"/quotes/"+q+"/lines", boss, map[string]any{"lines": []any{map[string]any{"itemId": bike, "quantity": 1}}}), 200, "quote line")
	first := time.Now().AddDate(0, 0, 30).Format("2006-01-02")
	want(t, call(t, "POST", base+"/quotes/"+q+"/payment-plan", boss, map[string]any{"instalments": 4, "firstDue": first, "everyDays": 30}), 201, "payment plan")
	want(t, call(t, "POST", base+"/quotes/"+q+"/payment-plan", boss, map[string]any{"instalments": 4, "firstDue": first}), 409, "a second plan")
	plan := func() []map[string]any {
		out := []map[string]any{}
		for _, x := range call(t, "GET", base+"/crm/instalments?pageSize=20&sort=dueDate", boss, nil).list("data") {
			out = append(out, x.(map[string]any)["values"].(map[string]any))
		}
		return out
	}
	p := plan()
	if len(p) != 4 || p[0]["amount"].(float64) != 32000 || p[0]["quoteId"] != q {
		t.Fatalf("four instalments of ₹32,000: %v", p)
	}
	// The first instalment paid.
	today := time.Now().Format("2006-01-02")
	create(t, boss, ws, "payments", map[string]any{"amount": 32000, "paymentDate": today, "status": "paid", "paymentMethod": "upi", "quoteId": q})
	paid := 0
	for _, x := range plan() {
		if x["status"] == "paid" {
			paid++
		}
	}
	if paid != 1 || total(t, boss, ws, "invoices", "") != 0 {
		t.Fatalf("one instalment paid, no invoice yet: %d paid", paid)
	}
	// The rest: all paid, invoice made.
	create(t, boss, ws, "payments", map[string]any{"amount": 96000, "paymentDate": today, "status": "paid", "paymentMethod": "cash", "quoteId": q})
	for _, x := range plan() {
		if x["status"] != "paid" {
			t.Fatalf("every instalment paid: %v", x)
		}
	}
	if total(t, boss, ws, "invoices", "") != 1 {
		t.Fatal("the invoice is made once the plan is paid off")
	}
}
