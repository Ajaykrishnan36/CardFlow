package e2e

import (
	"strings"
	"testing"
	"time"
)

// The faults and gaps found in the October 2026 test round (D-134).
func TestQARoundFixes(t *testing.T) {
	boss := signIn(t, freshPhone(), "QA Boss")
	ws := newBusiness(t, boss, "QA Fixes Co")
	base := crmAPI + "/w/" + ws
	post := func(object string, values map[string]any) resp {
		return call(t, "POST", base+"/crm/"+object, boss, map[string]any{"values": values})
	}
	future := time.Now().AddDate(0, 0, 30).Format("2006-01-02")

	// Contract dates.
	want(t, post("contracts", map[string]any{"name": "Backwards", "startDate": "2026-10-20", "endDate": "2026-10-11"}), 422, "a contract ending before it starts")
	con := create(t, boss, ws, "contracts", map[string]any{"name": "Fine", "startDate": "2026-10-01", "endDate": "2027-09-30"})
	want(t, edit(t, boss, ws, "contracts", con, map[string]any{"endDate": "2026-09-01"}), 422, "moving the end before the start")

	// Account and contact trees can't loop.
	group := create(t, boss, ws, "accounts", map[string]any{"name": "Group", "type": "prospect"})
	child := create(t, boss, ws, "accounts", map[string]any{"name": "Child", "parentAccountId": group})
	grand := create(t, boss, ws, "accounts", map[string]any{"name": "Grandchild", "parentAccountId": child})
	want(t, edit(t, boss, ws, "accounts", group, map[string]any{"parentAccountId": grand}), 422, "an account under its own grandchild")
	want(t, edit(t, boss, ws, "accounts", group, map[string]any{"parentAccountId": group}), 422, "an account under itself")
	head := create(t, boss, ws, "contacts", map[string]any{"lastName": "Head", "accountId": group})
	staff := create(t, boss, ws, "contacts", map[string]any{"lastName": "Staff", "accountId": group, "reportsToId": head})
	if get(t, boss, ws, "contacts", staff)["reportsToId"] != head {
		t.Fatalf("a contact reports to another contact")
	}
	want(t, edit(t, boss, ws, "contacts", head, map[string]any{"reportsToId": staff}), 422, "two contacts reporting to each other")

	// A lost deal says why; a deal won today counts today and makes its account a customer.
	deal := create(t, boss, ws, "opportunities", map[string]any{"name": "Deal", "amount": 5000, "closeDate": future, "accountId": group, "contactId": head})
	want(t, edit(t, boss, ws, "opportunities", deal, map[string]any{"status": "closed_lost"}), 422, "lost with no reason")
	want(t, edit(t, boss, ws, "opportunities", deal, map[string]any{"status": "closed_won"}), 200, "won")
	if d := get(t, boss, ws, "opportunities", deal)["closeDate"]; d != today() {
		t.Fatalf("a deal won today closes today, got %v", d)
	}
	old := create(t, boss, ws, "opportunities", map[string]any{"name": "Old win", "amount": 1, "closeDate": "2025-03-01", "status": "closed_won"})
	if d := get(t, boss, ws, "opportunities", old)["closeDate"]; d != "2025-03-01" {
		t.Fatalf("a past close date is kept, got %v", d)
	}
	if a := get(t, boss, ws, "accounts", group); a["lifecycle"] != "active" || a["type"] != "customer" {
		t.Fatalf("a won deal makes the account an active customer: %v / %v", a["lifecycle"], a["type"])
	}
	sum := call(t, "GET", base+"/dashboard/summary?range=month", boss, nil)
	if sum.at("metrics", "wonDeals") != float64(1) || sum.at("metrics", "wonValue") != float64(5000) {
		t.Fatalf("the win shows this month: %v", truncate(sum.Raw, 300))
	}

	// Payments received are income on the dashboard, less refunds.
	inv := create(t, boss, ws, "invoices", map[string]any{"name": "Invoice", "total": 1000, "status": "sent", "accountId": group})
	pay := create(t, boss, ws, "payments", map[string]any{"name": "Paid", "amount": 600, "paymentDate": today(), "status": "paid", "invoiceId": inv})
	create(t, boss, ws, "payments", map[string]any{"name": "Promised", "amount": 300, "paymentDate": today(), "status": "pending", "invoiceId": inv})
	create(t, boss, ws, "income", map[string]any{"name": "Scrap sale", "amount": 50, "date": today()})
	want(t, call(t, "POST", base+"/payments/"+pay+"/refund", boss, map[string]any{"amount": 100}), 200, "refund")
	sum = call(t, "GET", base+"/dashboard/summary?range=month", boss, nil)
	if sum.at("finance", "income") != float64(550) {
		t.Fatalf("income = 600 paid − 100 refunded + 50 other, got %v", sum.at("finance", "income"))
	}

	// A renewal or the next version of a quote is not a duplicate; documents are named for what they are.
	quote := create(t, boss, ws, "quotes", map[string]any{"name": "Quote — Group", "accountId": group, "status": "draft"})
	item := create(t, boss, ws, "catalog_items", map[string]any{"name": "Thing", "unitPrice": 10})
	want(t, call(t, "PUT", base+"/quotes/"+quote+"/lines", boss, map[string]any{"lines": []map[string]any{{"itemId": item, "quantity": 1}}}), 200, "lines")
	rev := call(t, "POST", base+"/quotes/"+quote+"/revise", boss, nil)
	if rev.Status != 200 && rev.Status != 201 {
		t.Fatalf("revise: %d %s", rev.Status, rev.Raw)
	}
	if d := call(t, "GET", base+"/crm/quotes/"+rev.str("id")+"/duplicates", boss, nil); len(d.list("data")) != 0 {
		t.Fatalf("a quote's earlier version is not a duplicate: %s", d.Raw)
	}
	order := call(t, "POST", base+"/quotes/"+rev.str("id")+"/convert", boss, nil)
	if order.Status != 200 && order.Status != 201 {
		t.Fatalf("order: %d %s", order.Status, order.Raw)
	}
	if n := get(t, boss, ws, "sales_orders", order.str("id"))["name"]; n != "Order — Group" {
		t.Fatalf("the order is named as an order, got %v", n)
	}
	invoice := call(t, "POST", base+"/sales_orders/"+order.str("id")+"/invoice", boss, nil)
	if invoice.Status != 200 && invoice.Status != 201 {
		t.Fatalf("invoice: %d %s", invoice.Status, invoice.Raw)
	}
	if n := get(t, boss, ws, "invoices", invoice.str("id"))["name"]; n != "Invoice — Group" {
		t.Fatalf("the invoice is named as an invoice, got %v", n)
	}

	// A notify step needs someone to notify.
	wf := call(t, "POST", base+"/workflows", boss, map[string]any{"name": "Silent", "draft": map[string]any{
		"trigger": map[string]any{"type": "record.created", "object": "cases"},
		"steps":   []map[string]any{{"id": "s1", "type": "notify", "config": map[string]any{"title": "Hi"}}}}})
	want(t, wf, 201, "draft")
	if p := call(t, "POST", base+"/workflows/"+wf.str("id")+"/publish", boss, nil); p.Status != 422 || !strings.Contains(p.Raw, "Choose who") {
		t.Fatalf("a notify step with nobody to notify can't be published: %d %s", p.Status, truncate(p.Raw, 200))
	}

	// Campaigns follow a lead to its contact and show on the deal.
	camp := call(t, "POST", base+"/campaigns", boss, map[string]any{"name": "Expo", "subject": "Hi", "bodyHtml": "<p>Hi</p>", "object": "leads"})
	lead := create(t, boss, ws, "leads", map[string]any{"lastName": "Visitor", "organization": "Visitor Ltd"})
	want(t, call(t, "POST", base+"/campaigns/"+camp.str("id")+"/members", boss, map[string]any{"leadId": lead}), 200, "member")
	cv := call(t, "POST", base+"/crm/leads/"+lead+"/convert", boss, map[string]any{"account": map[string]any{"mode": "new", "name": "Visitor Ltd", "kind": "business"},
		"createContact": true, "opportunity": map[string]any{"create": true, "name": "Visitor deal", "amount": 100, "closeDate": future}})
	want(t, cv, 200, "convert")
	for object, id := range map[string]string{"contacts": cv.str("contactId"), "opportunities": cv.str("opportunityId")} {
		if c := call(t, "GET", base+"/crm/"+object+"/"+id+"/campaigns", boss, nil); len(c.list("data")) != 1 || !strings.Contains(c.Raw, "Expo") {
			t.Fatalf("%s shows the lead's campaign: %s", object, truncate(c.Raw, 200))
		}
	}
	if c := call(t, "GET", base+"/crm/opportunities/"+deal+"/campaigns", boss, nil); len(c.list("data")) != 0 {
		t.Fatalf("an unrelated deal shows no campaign: %s", c.Raw)
	}

	// A finished case says what was done; setup records start Active; the deal follows its accepted quote;
	// someone added by mobile number shows that number.
	cs := create(t, boss, ws, "cases", map[string]any{"name": "Broken"})
	want(t, edit(t, boss, ws, "cases", cs, map[string]any{"status": "resolved"}), 422, "resolved with no resolution")
	want(t, edit(t, boss, ws, "cases", cs, map[string]any{"status": "resolved", "resolution": "Replaced it"}), 200, "resolved")
	want(t, edit(t, boss, ws, "cases", cs, map[string]any{"status": "closed"}), 200, "closed after it was resolved")
	res := create(t, boss, ws, "service_resources", map[string]any{"name": "Tech"})
	if get(t, boss, ws, "service_resources", res)["status"] != "active" {
		t.Fatalf("a new service resource is active")
	}
	d2 := create(t, boss, ws, "opportunities", map[string]any{"name": "Quoted deal", "amount": 999, "closeDate": future, "accountId": group})
	q2 := create(t, boss, ws, "quotes", map[string]any{"name": "Quote — deal", "accountId": group, "opportunityId": d2, "status": "draft"})
	want(t, call(t, "PUT", base+"/quotes/"+q2+"/lines", boss, map[string]any{"lines": []map[string]any{{"itemId": item, "quantity": 7}}}), 200, "lines")
	if o := call(t, "POST", base+"/quotes/"+q2+"/convert", boss, nil); o.Status != 200 && o.Status != 201 {
		t.Fatalf("convert: %d %s", o.Status, o.Raw)
	}
	if a := get(t, boss, ws, "opportunities", d2)["amount"]; a != float64(70) {
		t.Fatalf("the deal's amount follows the accepted quote (70), got %v", a)
	}
	phone := freshPhone()
	want(t, call(t, "POST", base+"/admin/members", boss, map[string]any{"displayName": "By Phone", "phone": phone, "roleKey": "STAFF"}), 201, "add by phone")
	if m := call(t, "GET", base+"/admin/members", boss, nil); !strings.Contains(m.Raw, `"phone":"+91`+phone+`"`) {
		t.Fatalf("the members list carries the mobile number: %s", truncate(m.Raw, 400))
	}

	// Partners have named roles, on an account and on a deal.
	partner := create(t, boss, ws, "accounts", map[string]any{"name": "Partner Co", "type": "partner"})
	want(t, call(t, "POST", base+"/crm/accounts/"+partner+"/relationships", boss, map[string]any{"type": "reseller_on", "targetObject": "opportunities", "targetId": deal}), 201, "reseller on a deal")
	want(t, call(t, "POST", base+"/crm/accounts/"+partner+"/relationships", boss, map[string]any{"type": "distributor_of", "targetObject": "accounts", "targetId": group}), 201, "distributor of an account")
	if r := call(t, "GET", base+"/crm/opportunities/"+deal+"/relationships", boss, nil); !strings.Contains(r.Raw, "Reseller") {
		t.Fatalf("the deal lists its reseller: %s", truncate(r.Raw, 300))
	}
}
