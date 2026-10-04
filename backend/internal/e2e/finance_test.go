package e2e

import (
	"testing"
	"time"
)

func metric(t *testing.T, r resp, key string) float64 {
	t.Helper()
	v, _ := r.at("metrics", key).(float64)
	return v
}

// Income, expenses and net income are real records on the record engine, summed for the
// chosen date range and for this business only (D-96).
func TestFinanceAndDashboardSummary(t *testing.T) {
	token := signIn(t, freshPhone(), "Finance Owner")
	ws := newBusiness(t, token, "Ledger Traders")
	other := newBusiness(t, token, "Other Books")
	today := time.Now().Format("2006-01-02")
	lastYear := time.Now().AddDate(-1, 0, 0).Format("2006-01-02")

	create(t, token, ws, "income", map[string]any{"name": "Invoice 101 paid", "amount": 5000, "date": today, "category": "sales", "status": "received"})
	create(t, token, ws, "income", map[string]any{"name": "Old sale", "amount": 1000, "date": lastYear, "status": "received"})
	create(t, token, ws, "income", map[string]any{"name": "Cancelled sale", "amount": 9999, "date": today, "status": "cancelled"})
	create(t, token, ws, "expenses", map[string]any{"name": "Shop rent", "amount": 2000, "date": today, "category": "rent", "status": "paid"})
	create(t, token, other, "income", map[string]any{"name": "Another business", "amount": 777777, "date": today})

	create(t, token, ws, "leads", map[string]any{"lastName": "Open Lead"})
	create(t, token, ws, "opportunities", map[string]any{"name": "Big deal", "amount": 100000, "probability": 50, "closeDate": today, "status": "proposal"})
	create(t, token, ws, "tasks", map[string]any{"name": "Call back", "dueDate": today, "status": "not_started"})
	create(t, token, ws, "cases", map[string]any{"name": "Printer broken", "status": "new"})

	r := call(t, "GET", crmAPI+"/w/"+ws+"/dashboard/summary?range=today", token, nil)
	want(t, r, 200, "summary today")
	if got := r.at("finance", "income"); got != float64(5000) {
		t.Fatalf("income today = %v, want 5000 (cancelled and other-business income must not count): %s", got, truncate(r.Raw, 400))
	}
	if r.at("finance", "expenses") != float64(2000) || r.at("finance", "net") != float64(3000) {
		t.Fatalf("expenses/net wrong: %v", r.at("finance"))
	}
	for key, wantV := range map[string]float64{"activeLeads": 1, "newLeads": 1, "openOpportunities": 1, "pipelineValue": 100000, "weightedPipeline": 50000,
		"tasksDueToday": 1, "openCases": 1} {
		if got := metric(t, r, key); got != wantV {
			t.Fatalf("%s = %v, want %v", key, got, wantV)
		}
	}

	all := call(t, "GET", crmAPI+"/w/"+ws+"/dashboard/summary?range=all", token, nil)
	if all.at("finance", "income") != float64(6000) {
		t.Fatalf("all-time income = %v, want 6000", all.at("finance", "income"))
	}
	custom := call(t, "GET", crmAPI+"/w/"+ws+"/dashboard/summary?range=custom&from="+lastYear+"&to="+lastYear, token, nil)
	want(t, custom, 200, "custom range")
	if custom.at("finance", "income") != float64(1000) {
		t.Fatalf("custom-range income = %v, want 1000", custom.at("finance", "income"))
	}
	want(t, call(t, "GET", crmAPI+"/w/"+ws+"/dashboard/summary?range=custom&from=bad", token, nil), 422, "bad custom range")

	// Winning the deal moves it out of the pipeline and into won deals.
	list := call(t, "GET", crmAPI+"/w/"+ws+"/crm/opportunities", token, nil)
	opp, _ := list.list("data")[0].(map[string]any)
	id, _ := opp["id"].(string)
	version, _ := opp["version"].(float64)
	want(t, call(t, "PATCH", crmAPI+"/w/"+ws+"/crm/opportunities/"+id, token,
		map[string]any{"values": map[string]any{"status": "closed_won"}, "expectedVersion": version}), 200, "mark won")
	won := call(t, "GET", crmAPI+"/w/"+ws+"/dashboard/summary?range=today", token, nil)
	if metric(t, won, "wonDeals") != 1 || metric(t, won, "wonValue") != 100000 || metric(t, won, "openOpportunities") != 0 {
		t.Fatalf("won deal not reflected: %v", won.at("metrics"))
	}
}

// The commerce and service objects exist as ordinary records in a new business.
func TestCommerceObjectsWork(t *testing.T) {
	token := signIn(t, freshPhone(), "Commerce Owner")
	ws := newBusiness(t, token, "Quote House")
	acct := create(t, token, ws, "accounts", map[string]any{"name": "Buyer Ltd"})
	item := create(t, token, ws, "catalog_items", map[string]any{"name": "Widget", "sku": "W-1", "unitPrice": 250, "cost": 100})
	quote := create(t, token, ws, "quotes", map[string]any{"name": "Quote for Buyer", "accountId": acct, "total": 500, "status": "draft"})
	create(t, token, ws, "line_items", map[string]any{"name": "Widget x2", "itemId": item, "quantity": 2, "unitPrice": 250, "total": 500, "quoteId": quote})
	order := create(t, token, ws, "sales_orders", map[string]any{"name": "Order 1", "accountId": acct, "quoteId": quote, "total": 500})
	create(t, token, ws, "invoices", map[string]any{"name": "Invoice 1", "accountId": acct, "orderId": order, "total": 500, "status": "sent"})
	create(t, token, ws, "price_books", map[string]any{"name": "Retail"})
	create(t, token, ws, "purchase_orders", map[string]any{"name": "Restock", "vendor": "Supplier Co", "total": 1000})
	create(t, token, ws, "solutions", map[string]any{"name": "How to reset", "body": "<p>Hold the button.</p>", "status": "published"})

	// The quote page lists its line item as a related record.
	q := call(t, "GET", crmAPI+"/w/"+ws+"/crm/quotes/"+quote, token, nil)
	want(t, q, 200, "open quote")
	found := false
	for _, l := range q.list("related") {
		m, _ := l.(map[string]any)
		if rows, _ := m["rows"].([]any); len(rows) == 1 && m["object"] == "line_items" {
			found = true
		}
	}
	if !found {
		t.Fatalf("quote has no line item related list: %s", truncate(q.Raw, 600))
	}
}
