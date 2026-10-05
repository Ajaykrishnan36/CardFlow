package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func num(t *testing.T, r resp, path ...string) float64 {
	t.Helper()
	v, _ := r.at(path...).(float64)
	return v
}

func get(t *testing.T, token, ws, object, id string) map[string]any {
	t.Helper()
	r := call(t, "GET", crmAPI+"/w/"+ws+"/crm/"+object+"/"+id, token, nil)
	want(t, r, 200, "open "+object)
	return r.at("record", "values").(map[string]any)
}

// edit changes fields of a record at its current version.
func edit(t *testing.T, token, ws, object, id string, values map[string]any) resp {
	t.Helper()
	cur := call(t, "GET", crmAPI+"/w/"+ws+"/crm/"+object+"/"+id, token, nil)
	want(t, cur, 200, "open "+object)
	return call(t, "PATCH", crmAPI+"/w/"+ws+"/crm/"+object+"/"+id, token, map[string]any{"values": values, "expectedVersion": cur.at("record", "version")})
}

// One invoice, several payments: the invoice's paid amount, balance and status come from
// the payment records (D-113).
func TestInvoiceIsPaidByPaymentRecords(t *testing.T) {
	token := signIn(t, freshPhone(), "Cashier")
	ws := newBusiness(t, token, "Ledger And Sons")
	base := crmAPI + "/w/" + ws
	acc := create(t, token, ws, "accounts", map[string]any{"name": "Paying Customer"})
	inv := create(t, token, ws, "invoices", map[string]any{"name": "Invoice 1", "total": 100000, "status": "sent", "accountId": acc})

	v := get(t, token, ws, "invoices", inv)
	if v["balanceDue"] != float64(100000) || v["status"] != "sent" {
		t.Fatalf("a new invoice owes its total: %v", v)
	}
	pay := func(amount float64, status string) string {
		return create(t, token, ws, "payments", map[string]any{"name": fmt.Sprintf("Payment %.0f", amount), "amount": amount, "paymentDate": "2026-10-05", "status": status, "invoiceId": inv})
	}
	p1 := pay(30000, "paid")
	v = get(t, token, ws, "invoices", inv)
	if v["amountPaid"] != float64(30000) || v["balanceDue"] != float64(70000) || v["status"] != "partially_paid" {
		t.Fatalf("after 30,000: %v", v)
	}
	// The payment took the invoice's account, and knows it is fully applied.
	pv := get(t, token, ws, "payments", p1)
	if pv["accountId"] != acc || pv["allocatedAmount"] != float64(30000) || pv["unappliedAmount"] != float64(0) {
		t.Fatalf("payment should inherit the account and be fully applied: %v", pv)
	}
	pay(20000, "paid")
	pending := pay(10000, "pending") // not received yet: doesn't count
	v = get(t, token, ws, "invoices", inv)
	if v["amountPaid"] != float64(50000) || v["balanceDue"] != float64(50000) {
		t.Fatalf("a pending payment must not count: %v", v)
	}
	// It arrives.
	want(t, edit(t, token, ws, "payments", pending, map[string]any{"status": "paid"}), 200, "payment received")
	p4 := pay(60000, "paid") // more than the invoice still needs: 40,000 is applied, 20,000 stays unapplied
	v = get(t, token, ws, "invoices", inv)
	if v["amountPaid"] != float64(100000) || v["balanceDue"] != float64(0) || v["status"] != "paid" {
		t.Fatalf("fully paid: %v", v)
	}
	pv = get(t, token, ws, "payments", p4)
	if pv["allocatedAmount"] != float64(40000) || pv["unappliedAmount"] != float64(20000) {
		t.Fatalf("overpayment should stay unapplied on the payment: %v", pv)
	}
	// Typing a paid amount on the invoice doesn't stick: payments are the source.
	want(t, edit(t, token, ws, "invoices", inv, map[string]any{"amountPaid": 5}), 200, "manual edit")
	if v = get(t, token, ws, "invoices", inv); v["amountPaid"] != float64(100000) {
		t.Fatalf("amountPaid must follow the payments, got %v", v["amountPaid"])
	}

	// The unapplied 20,000 pays part of a second invoice.
	inv2 := create(t, token, ws, "invoices", map[string]any{"name": "Invoice 2", "total": 50000, "status": "sent", "accountId": acc})
	al := call(t, "POST", base+"/payments/"+p4+"/allocations", token, map[string]any{"invoiceId": inv2, "amount": 20000})
	want(t, al, 200, "apply the rest to another invoice")
	if len(al.list("data")) != 2 {
		t.Fatalf("the payment should now pay two invoices: %s", truncate(al.Raw, 300))
	}
	if v = get(t, token, ws, "invoices", inv2); v["amountPaid"] != float64(20000) || v["status"] != "partially_paid" {
		t.Fatalf("second invoice: %v", v)
	}
	want(t, call(t, "POST", base+"/payments/"+p4+"/allocations", token, map[string]any{"invoiceId": inv2, "amount": 25000}), 422, "more than the payment has")

	// Refund half of the first payment: the invoice loses that share.
	rf := call(t, "POST", base+"/payments/"+p1+"/refund", token, map[string]any{"amount": 15000, "reason": "Returned goods"})
	want(t, rf, 200, "partial refund")
	if rf.at("values", "status") != "partially_refunded" || rf.at("values", "refundedAmount") != float64(15000) {
		t.Fatalf("refund state: %s", truncate(rf.Raw, 300))
	}
	if v = get(t, token, ws, "invoices", inv); v["amountPaid"] != float64(85000) || v["balanceDue"] != float64(15000) || v["status"] != "partially_paid" {
		t.Fatalf("after a 15,000 refund: %v", v)
	}
	want(t, call(t, "POST", base+"/payments/"+p1+"/refund", token, map[string]any{"amount": 99999}), 422, "refund more than was paid")
	want(t, call(t, "POST", base+"/payments/"+p1+"/refund", token, map[string]any{"amount": 15000}), 200, "refund the rest")
	if v = get(t, token, ws, "invoices", inv); v["amountPaid"] != float64(70000) {
		t.Fatalf("a fully refunded payment counts for nothing: %v", v)
	}
	// Deleting a payment takes its money off the invoice; restoring brings it back.
	want(t, call(t, "DELETE", base+"/crm/payments/"+pending, token, nil), 204, "delete a payment")
	if v = get(t, token, ws, "invoices", inv); v["amountPaid"] != float64(60000) {
		t.Fatalf("a deleted payment must not count: %v", v)
	}

	ledger := call(t, "GET", base+"/invoices/"+inv+"/payments", token, nil)
	want(t, ledger, 200, "invoice ledger")
	if num(t, ledger, "paid") != 60000 || num(t, ledger, "balance") != 40000 || len(ledger.list("payments")) != 3 {
		t.Fatalf("ledger: %s", truncate(ledger.Raw, 500))
	}
	rec := call(t, "GET", base+"/finance/receivables", token, nil)
	want(t, rec, 200, "receivables")
	if num(t, rec, "outstanding") != 70000 || num(t, rec, "invoiced") != 150000 {
		t.Fatalf("receivables: %s", truncate(rec.Raw, 400))
	}

	// Another business sees none of it and can't touch it.
	other := signIn(t, freshPhone(), "Outsider")
	otherWS := newBusiness(t, other, "Other Ledger")
	want(t, call(t, "GET", base+"/invoices/"+inv+"/payments", other, nil), 403, "outsider reading the ledger")
	want(t, call(t, "GET", crmAPI+"/w/"+otherWS+"/invoices/"+inv+"/payments", other, nil), 404, "an invoice id from another business")
	otherPay := create(t, other, otherWS, "payments", map[string]any{"name": "Theirs", "amount": 500, "paymentDate": "2026-10-05", "status": "paid"})
	want(t, call(t, "POST", crmAPI+"/w/"+otherWS+"/payments/"+otherPay+"/allocations", other, map[string]any{"invoiceId": inv, "amount": 100}), 422, "paying another business's invoice")
	r := call(t, "POST", crmAPI+"/w/"+otherWS+"/crm/payments", other, map[string]any{"values": map[string]any{"name": "Sneaky", "amount": 10, "paymentDate": "2026-10-05", "invoiceId": inv}})
	if r.Status == 200 || r.Status == 201 {
		t.Fatalf("a payment must not name an invoice of another business: %s", truncate(r.Raw, 200))
	}
}

// Any two records of one business can be linked; never across businesses (D-111).
func TestRelationshipsStayInsideTheBusiness(t *testing.T) {
	token := signIn(t, freshPhone(), "Linker")
	a := newBusiness(t, token, "Links A")
	b := newBusiness(t, token, "Links B") // the same person owns both
	base := crmAPI + "/w/" + a

	acc1 := create(t, token, a, "accounts", map[string]any{"name": "Main Employer"})
	acc2 := create(t, token, a, "accounts", map[string]any{"name": "Second Employer"})
	con := create(t, token, a, "contacts", map[string]any{"lastName": "Consultant", "accountId": acc1})
	opp := create(t, token, a, "opportunities", map[string]any{"name": "Big Deal", "closeDate": "2026-12-01", "accountId": acc1})
	accB := create(t, token, b, "accounts", map[string]any{"name": "B's Account"})

	types := call(t, "GET", base+"/relationship-types", token, nil)
	want(t, types, 200, "relationship types")
	if len(types.list("data")) < 8 {
		t.Fatalf("built-in relationship types missing: %s", truncate(types.Raw, 200))
	}
	// A contact works for a second account and is the decision maker on a deal.
	want(t, call(t, "POST", base+"/crm/contacts/"+con+"/relationships", token, map[string]any{"type": "works_for", "targetObject": "accounts", "targetId": acc2, "note": "Board member"}), 201, "second account")
	r := call(t, "POST", base+"/crm/contacts/"+con+"/relationships", token, map[string]any{"type": "decision_maker_for", "targetObject": "opportunities", "targetId": opp})
	want(t, r, 201, "decision maker")
	if len(r.list("data")) != 2 {
		t.Fatalf("contact should have two links: %s", truncate(r.Raw, 300))
	}
	want(t, call(t, "POST", base+"/crm/contacts/"+con+"/relationships", token, map[string]any{"type": "works_for", "targetObject": "accounts", "targetId": acc2}), 422, "the same link twice")
	want(t, call(t, "POST", base+"/crm/contacts/"+con+"/relationships", token, map[string]any{"type": "works_for", "targetObject": "opportunities", "targetId": opp}), 422, "a type used on the wrong object")
	want(t, call(t, "POST", base+"/crm/contacts/"+con+"/relationships", token, map[string]any{"type": "related_to", "targetObject": "contacts", "targetId": con}), 422, "a record linked to itself")

	// Seen from the other end, with the inverse wording, and on the record page.
	fromOpp := call(t, "GET", base+"/crm/opportunities/"+opp+"/relationships", token, nil)
	want(t, fromOpp, 200, "links of the deal")
	first := fromOpp.list("data")[0].(map[string]any)
	if first["direction"] != "in" || first["label"] != "Decision maker" || first["recordId"] != con {
		t.Fatalf("inverse link wrong: %v", first)
	}
	page := call(t, "GET", base+"/crm/accounts/"+acc2, token, nil)
	found := false
	for _, l := range page.list("related") {
		m := l.(map[string]any)
		if m["label"] == "Has contact" && len(m["rows"].([]any)) == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("the second account's page should list the contact: %s", truncate(page.Raw, 700))
	}

	// Across businesses: blocked both ways, even for the person who owns both.
	want(t, call(t, "POST", base+"/crm/contacts/"+con+"/relationships", token, map[string]any{"type": "works_for", "targetObject": "accounts", "targetId": accB}), 404, "link to a record of another business")
	want(t, call(t, "POST", crmAPI+"/w/"+b+"/crm/accounts/"+accB+"/relationships", token, map[string]any{"type": "related_to", "targetObject": "contacts", "targetId": con}), 404, "link from another business")
	want(t, call(t, "GET", crmAPI+"/w/"+b+"/crm/contacts/"+con+"/relationships", token, nil), 404, "reading links through another business")
	var crossed int
	if err := testDB.Pool.QueryRow(context.Background(), `
		SELECT count(*) FROM crm.record_relationships r JOIN crm.contacts c ON c.id = r.source_id WHERE c.workspace_id <> r.workspace_id`).Scan(&crossed); err != nil || crossed != 0 {
		t.Fatalf("a relationship crosses businesses: %d %v", crossed, err)
	}

	// One-to-many: an asset has one owner.
	asset := create(t, token, a, "assets", map[string]any{"name": "Forklift", "accountId": acc1})
	want(t, call(t, "POST", base+"/crm/accounts/"+acc1+"/relationships", token, map[string]any{"type": "owns", "targetObject": "assets", "targetId": asset}), 201, "owns")
	want(t, call(t, "POST", base+"/crm/accounts/"+acc2+"/relationships", token, map[string]any{"type": "owns", "targetObject": "assets", "targetId": asset}), 422, "a second owner")

	// A business's own relationship type.
	want(t, call(t, "POST", base+"/relationship-types", token, map[string]any{"key": "referred_by", "label": "Referred by", "inverseLabel": "Referred", "sourceObject": "leads", "targetObject": "contacts"}), 201, "custom type")
	lead := create(t, token, a, "leads", map[string]any{"lastName": "Referred Lead"})
	want(t, call(t, "POST", base+"/crm/leads/"+lead+"/relationships", token, map[string]any{"type": "referred_by", "targetObject": "contacts", "targetId": con}), 201, "use the custom type")
	if tb := call(t, "GET", crmAPI+"/w/"+b+"/relationship-types", token, nil); len(tb.list("data")) != len(types.list("data")) {
		t.Fatalf("a business's own relationship type leaked into another business")
	}

	// Removing a link.
	list := call(t, "GET", base+"/crm/contacts/"+con+"/relationships", token, nil)
	relID := list.list("data")[0].(map[string]any)["id"].(string)
	want(t, call(t, "DELETE", crmAPI+"/w/"+b+"/relationships/"+relID, token, nil), 404, "deleting through another business")
	want(t, call(t, "DELETE", base+"/relationships/"+relID, token, nil), 204, "delete a link")
}

// The forecast of a period is rolled up from opportunities by owner and team, against
// targets, without counting anything twice (D-112).
func TestForecastRollsUpWithoutDoubleCounting(t *testing.T) {
	boss := signIn(t, freshPhone(), "Sales Head")
	ws := newBusiness(t, boss, "Forecast Co")
	base := crmAPI + "/w/" + ws
	repPhone := freshPhone()
	want(t, call(t, "POST", base+"/admin/members", boss, map[string]any{"displayName": "Rep One", "phone": repPhone, "roleKey": "STAFF"}), 201, "add a rep")
	rep := signIn(t, repPhone, "Rep One")
	me := call(t, "GET", crmAPI+"/me", rep, nil).str("identity", "id")

	periods := call(t, "GET", base+"/forecast/periods", boss, nil)
	want(t, periods, 200, "periods")
	quarter := periods.str("current", "quarter")
	// A close date inside the current quarter, and one far outside it.
	in := time.Now().Format("2006-01-02")
	out := time.Now().AddDate(1, 2, 0).Format("2006-01-02")
	deal := func(token, name string, amount float64, status, category, close string) string {
		v := map[string]any{"name": name, "amount": amount, "closeDate": close, "status": status}
		if category != "" {
			v["forecastCategory"] = category
		}
		return create(t, token, ws, "opportunities", v)
	}
	deal(boss, "Boss won", 100000, "closed_won", "", in)
	deal(boss, "Boss commit", 50000, "negotiation", "commit", in)
	deal(boss, "Boss lost", 70000, "closed_lost", "", in)
	deal(boss, "Boss next year", 999999, "proposal", "commit", out)
	deal(rep, "Rep won", 20000, "closed_won", "", in)
	deal(rep, "Rep best", 40000, "proposal", "best_case", in)
	deal(rep, "Rep pipe", 30000, "prospecting", "", in)
	deal(rep, "Rep omitted", 5000, "prospecting", "omitted", in)

	want(t, call(t, "PUT", base+"/forecast/quotas", rep, map[string]any{"periodKey": quarter, "scope": "user", "ownerId": me, "amount": 1}), 403, "a rep setting a target")
	want(t, call(t, "PUT", base+"/forecast/quotas", boss, map[string]any{"periodKey": quarter, "scope": "user", "ownerId": me, "amount": 100000}), 200, "target for the rep")
	want(t, call(t, "PUT", base+"/forecast/quotas", boss, map[string]any{"periodKey": quarter, "scope": "company", "amount": 400000}), 200, "company target")

	f := call(t, "GET", base+"/forecast?period="+quarter, boss, nil)
	want(t, f, 200, "company forecast")
	for key, wantV := range map[string]float64{"closed": 120000, "commit": 50000, "bestCase": 40000, "pipeline": 30000, "omitted": 5000, "lost": 70000,
		"forecast": 170000, "bestCaseTotal": 210000, "openPipeline": 120000, "quota": 400000, "gap": 230000, "attainment": 30} {
		if got := num(t, f, "totals", key); got != wantV {
			t.Fatalf("company %s = %v, want %v — %s", key, got, wantV, truncate(f.Raw, 500))
		}
	}
	var sumClosed float64
	for _, row := range f.list("rows") {
		sumClosed += row.(map[string]any)["closed"].(float64)
	}
	if sumClosed != 120000 || len(f.list("rows")) != 2 {
		t.Fatalf("rows must add up to the total exactly once: closed %v in %d rows", sumClosed, len(f.list("rows")))
	}
	// The rep (own records only) sees just their own numbers.
	mine := call(t, "GET", base+"/forecast?period="+quarter, rep, nil)
	want(t, mine, 200, "rep forecast")
	if num(t, mine, "totals", "closed") != 20000 || num(t, mine, "totals", "bestCase") != 40000 || num(t, mine, "totals", "quota") != 100000 ||
		num(t, mine, "totals", "attainment") != 20 || len(mine.list("rows")) != 1 || mine.str("scope") != "team" {
		t.Fatalf("a rep must see only their own forecast: %s", truncate(mine.Raw, 500))
	}
	// By team: a person in two teams shows in both, the total stays the same.
	for _, name := range []string{"North", "Key accounts"} {
		team := call(t, "POST", base+"/teams", boss, map[string]any{"name": name, "members": []string{me}})
		if team.Status != 200 && team.Status != 201 {
			t.Fatalf("create team: %d %s", team.Status, truncate(team.Raw, 200))
		}
	}
	byTeam := call(t, "GET", base+"/forecast?period="+quarter+"&groupBy=team", boss, nil)
	want(t, byTeam, 200, "forecast by team")
	if num(t, byTeam, "totals", "closed") != 120000 {
		t.Fatalf("grouping by team must not change the total: %s", truncate(byTeam.Raw, 400))
	}
	inTeams := 0
	for _, row := range byTeam.list("rows") {
		if m := row.(map[string]any); m["closed"] == float64(20000) {
			inTeams++
		}
	}
	if inTeams != 2 {
		t.Fatalf("the rep should appear in both of their teams, appeared in %d: %s", inTeams, truncate(byTeam.Raw, 600))
	}

	// Submit, then the manager approves with their own number.
	sub := call(t, "POST", base+"/forecast/submit", rep, map[string]any{"periodKey": quarter, "comment": "Confident"})
	want(t, sub, 200, "submit")
	if num(t, sub, "forecastAmount") != 20000 {
		t.Fatalf("a rep's forecast is closed + commit = 20,000: %s", sub.Raw)
	}
	want(t, call(t, "POST", base+"/forecast/submissions/"+sub.str("id")+"/review", rep, map[string]any{"status": "approved"}), 403, "a rep approving")
	want(t, call(t, "POST", base+"/forecast/submissions/"+sub.str("id")+"/review", boss, map[string]any{"status": "approved", "overrideAmount": 35000, "comment": "Include the best case"}), 200, "manager approves")
	f = call(t, "GET", base+"/forecast?period="+quarter, boss, nil)
	okSub := false
	for _, row := range f.list("rows") {
		m := row.(map[string]any)
		if s, has := m["submission"].(map[string]any); has && s["status"] == "approved" && s["overrideAmount"] == float64(35000) {
			okSub = true
		}
	}
	if !okSub {
		t.Fatalf("the approved submission should show on the rep's row: %s", truncate(f.Raw, 700))
	}

	// Another business: nothing of this one, and its ids don't work there.
	other := signIn(t, freshPhone(), "Rival")
	otherWS := newBusiness(t, other, "Rival Co")
	of := call(t, "GET", crmAPI+"/w/"+otherWS+"/forecast?period="+quarter, other, nil)
	want(t, of, 200, "another business's forecast")
	if num(t, of, "totals", "closed") != 0 || num(t, of, "totals", "quota") != 0 {
		t.Fatalf("forecast leaked across businesses: %s", truncate(of.Raw, 300))
	}
	want(t, call(t, "GET", base+"/forecast?period="+quarter, other, nil), 403, "outsider reading the forecast")
	want(t, call(t, "POST", crmAPI+"/w/"+otherWS+"/forecast/submissions/"+sub.str("id")+"/review", other, map[string]any{"status": "approved"}), 404, "reviewing another business's submission")
	want(t, call(t, "PUT", crmAPI+"/w/"+otherWS+"/forecast/quotas", other, map[string]any{"periodKey": quarter, "scope": "user", "ownerId": me, "amount": 5}), 422, "a target for someone who isn't a member")
	want(t, call(t, "GET", base+"/forecast?period=nonsense", boss, nil), 422, "bad period")
}

// A case gets its SLA clocks from the policy of its priority or its entitlement; they
// pause while it waits for the customer, stop when it is answered and resolved, and a
// clock that runs out marks the case as breached and escalates it (D-115).
func TestCaseSLAClocks(t *testing.T) {
	ctx := context.Background()
	token := signIn(t, freshPhone(), "Support Lead")
	ws := newBusiness(t, token, "Support Desk")
	base := crmAPI + "/w/" + ws
	create(t, token, ws, "sla_policies", map[string]any{"name": "Critical", "priority": "critical", "firstResponseMinutes": 30, "resolutionMinutes": 240, "autoEscalate": true})
	create(t, token, ws, "sla_policies", map[string]any{"name": "Standard", "priority": "any", "firstResponseMinutes": 120, "resolutionMinutes": 480, "isDefault": true})
	premium := create(t, token, ws, "sla_policies", map[string]any{"name": "Premium support", "priority": "any", "firstResponseMinutes": 15, "resolutionMinutes": 60})

	c := create(t, token, ws, "cases", map[string]any{"name": "Server down", "priority": "critical"})
	sla := call(t, "GET", base+"/cases/"+c+"/sla", token, nil)
	want(t, sla, 200, "sla of the case")
	if sla.str("policy", "label") != "Critical" || len(sla.list("timers")) != 2 {
		t.Fatalf("a critical case takes the critical policy with two clocks: %s", truncate(sla.Raw, 400))
	}
	timers := map[string]map[string]any{}
	for _, x := range sla.list("timers") {
		m := x.(map[string]any)
		timers[m["milestone"].(string)] = m
	}
	if timers["first_response"]["targetMinutes"] != float64(30) || timers["resolution"]["targetMinutes"] != float64(240) || timers["resolution"]["state"] != "running" {
		t.Fatalf("clock targets wrong: %v", timers)
	}
	v := get(t, token, ws, "cases", c)
	if v["slaDueAt"] == nil || v["firstResponseDueAt"] == nil {
		t.Fatalf("the case should carry its due times: %v", v)
	}

	// Picking it up answers the first response.
	patch := func(id string, values map[string]any) {
		t.Helper()
		want(t, edit(t, token, ws, "cases", id, values), 200, "update case")
	}
	patch(c, map[string]any{"status": "assigned"})
	if v = get(t, token, ws, "cases", c); v["firstRespondedAt"] == nil {
		t.Fatalf("leaving New records the first response: %v", v)
	}
	// Waiting for the customer pauses the clock; the due time moves by the wait.
	patch(c, map[string]any{"status": "pending"})
	if _, err := testDB.Pool.Exec(ctx, `UPDATE crm.sla_timers SET paused_at = paused_at - interval '45 minutes' WHERE case_id = $1::uuid AND paused_at IS NOT NULL`, c); err != nil {
		t.Fatal(err)
	}
	before := get(t, token, ws, "cases", c)["slaDueAt"].(string)
	sla = call(t, "GET", base+"/cases/"+c+"/sla", token, nil)
	for _, x := range sla.list("timers") {
		if m := x.(map[string]any); m["milestone"] == "resolution" && m["state"] != "paused" {
			t.Fatalf("the resolution clock should be paused: %v", m)
		}
	}
	patch(c, map[string]any{"status": "working"})
	after := get(t, token, ws, "cases", c)["slaDueAt"].(string)
	tb, _ := time.Parse(time.RFC3339, before)
	ta, _ := time.Parse(time.RFC3339, after)
	if moved := ta.Sub(tb).Minutes(); moved < 44 || moved > 46 {
		t.Fatalf("resuming should push the due time by the 45 minutes waited, moved %.1f", moved)
	}

	// The clock runs out: breached, flagged, escalated.
	if _, err := testDB.Pool.Exec(ctx, `UPDATE crm.sla_timers SET due_at = now() - interval '1 minute' WHERE case_id = $1::uuid AND milestone = 'resolution'`, c); err != nil {
		t.Fatal(err)
	}
	crmMod.Sweep(ctx)
	v = get(t, token, ws, "cases", c)
	if v["slaBreached"] != true || v["status"] != "escalated" || v["escalatedAt"] == nil {
		t.Fatalf("a breached critical case is flagged and escalated: %v", v)
	}
	crmMod.Sweep(ctx) // a second pass does nothing more
	var breaches int
	_ = testDB.Pool.QueryRow(ctx, `SELECT count(*) FROM crm.activities WHERE record_id = $1::uuid AND kind = 'sla.breached'`, c).Scan(&breaches)
	if breaches != 1 {
		t.Fatalf("a breach is recorded once, got %d", breaches)
	}
	patch(c, map[string]any{"status": "resolved"})
	sla = call(t, "GET", base+"/cases/"+c+"/sla", token, nil)
	for _, x := range sla.list("timers") {
		if m := x.(map[string]any); m["milestone"] == "resolution" && m["state"] != "missed" {
			t.Fatalf("resolved after the due time = missed: %v", m)
		}
	}

	// No priority match: the default policy. An entitlement on the account wins over both.
	plain := create(t, token, ws, "cases", map[string]any{"name": "Question", "priority": "low"})
	if p := call(t, "GET", base+"/cases/"+plain+"/sla", token, nil); p.str("policy", "label") != "Standard" {
		t.Fatalf("a case without a matching priority takes the default policy: %s", truncate(p.Raw, 300))
	}
	acc := create(t, token, ws, "accounts", map[string]any{"name": "Premium Customer"})
	contract := create(t, token, ws, "contracts", map[string]any{"name": "Support contract", "accountId": acc, "startDate": "2026-01-01", "endDate": "2027-12-31", "status": "active"})
	ent := create(t, token, ws, "entitlements", map[string]any{"name": "Premium support", "accountId": acc, "contractId": contract, "slaPolicyId": premium, "supportLevel": "premium", "status": "active"})
	entitled := create(t, token, ws, "cases", map[string]any{"name": "Entitled case", "priority": "low", "accountId": acc})
	p := call(t, "GET", base+"/cases/"+entitled+"/sla", token, nil)
	if p.str("policy", "label") != "Premium support" {
		t.Fatalf("the account's entitlement decides the SLA: %s", truncate(p.Raw, 300))
	}
	if v = get(t, token, ws, "cases", entitled); v["entitlementId"] != ent {
		t.Fatalf("the case should be linked to the entitlement it used: %v", v)
	}
	// Raising the priority of a case without an entitlement retargets its clocks.
	patch(plain, map[string]any{"priority": "critical"})
	if p := call(t, "GET", base+"/cases/"+plain+"/sla", token, nil); p.str("policy", "label") != "Critical" {
		t.Fatalf("a new priority brings its policy: %s", truncate(p.Raw, 300))
	}

	// Another business can't read these clocks.
	other := signIn(t, freshPhone(), "Nosy")
	otherWS := newBusiness(t, other, "Nosy Desk")
	want(t, call(t, "GET", base+"/cases/"+c+"/sla", other, nil), 403, "outsider reading SLA")
	want(t, call(t, "GET", crmAPI+"/w/"+otherWS+"/cases/"+c+"/sla", other, nil), 404, "a case id from another business")
	// …and a case there can't use this business's policy or entitlement.
	r := call(t, "POST", crmAPI+"/w/"+otherWS+"/crm/cases", other, map[string]any{"values": map[string]any{"name": "Borrowed SLA", "slaPolicyId": premium}})
	if r.Status == 200 || r.Status == 201 {
		t.Fatalf("a case must not use another business's SLA policy: %s", truncate(r.Raw, 200))
	}
}

// Contracts: renewing makes the next term; one that runs out is flagged and then expires (D-116).
func TestContractRenewalAndExpiry(t *testing.T) {
	ctx := context.Background()
	token := signIn(t, freshPhone(), "Contract Owner")
	ws := newBusiness(t, token, "Contracts Co")
	base := crmAPI + "/w/" + ws
	acc := create(t, token, ws, "accounts", map[string]any{"name": "Signed Customer"})
	asset := create(t, token, ws, "assets", map[string]any{"name": "Generator", "accountId": acc, "serialNumber": "GEN-1", "status": "installed"})
	child := create(t, token, ws, "assets", map[string]any{"name": "Battery", "accountId": acc, "parentAssetId": asset})
	if get(t, token, ws, "assets", child)["parentAssetId"] != asset {
		t.Fatalf("asset hierarchy not kept")
	}
	con := create(t, token, ws, "contracts", map[string]any{"name": "AMC 2026", "accountId": acc, "startDate": "2026-01-01", "endDate": "2026-12-31", "contractValue": 120000, "status": "active"})
	want(t, call(t, "POST", base+"/crm/contracts/"+con+"/relationships", token, map[string]any{"type": "covers", "targetObject": "assets", "targetId": asset}), 201, "contract covers the asset")

	rn := call(t, "POST", base+"/contracts/"+con+"/renew", token, map[string]any{"contractValue": 132000})
	want(t, rn, 201, "renew")
	next := rn.str("id")
	nv := get(t, token, ws, "contracts", next)
	if nv["startDate"] != "2027-01-01" || nv["endDate"] != "2027-12-31" || nv["contractValue"] != float64(132000) || nv["status"] != "active" || nv["accountId"] != acc {
		t.Fatalf("the renewal should be the next term: %v", nv)
	}
	if get(t, token, ws, "contracts", con)["status"] != "renewed" {
		t.Fatalf("the old contract should be marked renewed")
	}
	want(t, call(t, "POST", base+"/contracts/"+con+"/renew", token, map[string]any{}), 422, "renewing twice")
	links := call(t, "GET", base+"/crm/contracts/"+next+"/relationships", token, nil)
	labels := map[string]bool{}
	for _, l := range links.list("data") {
		labels[l.(map[string]any)["label"].(string)] = true
	}
	if !labels["Renewal of"] || !labels["Covers"] {
		t.Fatalf("the renewal should point at the old contract and cover the same asset: %s", truncate(links.Raw, 400))
	}

	// Running out: inside the notice period → Expiring + a renewal task; past the end → Expired.
	soon := time.Now().AddDate(0, 0, 10).Format("2006-01-02")
	past := time.Now().AddDate(0, 0, -2).Format("2006-01-02")
	ending := create(t, token, ws, "contracts", map[string]any{"name": "Ending soon", "accountId": acc, "startDate": "2026-01-01", "endDate": soon, "status": "active", "renewalNoticeDays": 30})
	ended := create(t, token, ws, "contracts", map[string]any{"name": "Ended", "accountId": acc, "startDate": "2025-01-01", "endDate": past, "status": "active"})
	far := create(t, token, ws, "contracts", map[string]any{"name": "Long", "accountId": acc, "startDate": "2026-01-01", "endDate": "2030-01-01", "status": "active"})
	inv := create(t, token, ws, "invoices", map[string]any{"name": "Late invoice", "total": 500, "status": "sent", "dueDate": past})
	crmMod.Sweep(ctx)
	if s := get(t, token, ws, "contracts", ending)["status"]; s != "expiring" {
		t.Fatalf("a contract inside its notice period should be Expiring, is %v", s)
	}
	if s := get(t, token, ws, "contracts", ended)["status"]; s != "expired" {
		t.Fatalf("a contract past its end should be Expired, is %v", s)
	}
	if s := get(t, token, ws, "contracts", far)["status"]; s != "active" {
		t.Fatalf("a contract far from its end stays Active, is %v", s)
	}
	if s := get(t, token, ws, "invoices", inv)["status"]; s != "overdue" {
		t.Fatalf("an unpaid invoice past its due date should be Overdue, is %v", s)
	}
	if n := total(t, token, ws, "tasks", "?q=Renew"); n != 1 {
		t.Fatalf("one renewal task should be created, got %d", n)
	}
	crmMod.Sweep(ctx)
	if n := total(t, token, ws, "tasks", "?q=Renew"); n != 1 {
		t.Fatalf("the renewal task must not be created twice, got %d", n)
	}

	// Services share the catalog with products; appointments book a service.
	service := create(t, token, ws, "catalog_items", map[string]any{"name": "Annual maintenance", "itemType": "service", "unitPrice": 5000, "billingFrequency": "yearly", "durationMinutes": 120})
	create(t, token, ws, "catalog_items", map[string]any{"name": "Spare battery", "itemType": "product", "unitPrice": 900})
	appt := create(t, token, ws, "appointments", map[string]any{"name": "Maintenance visit", "accountId": acc, "itemId": service,
		"startsAt": time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339), "status": "scheduled"})
	want(t, edit(t, token, ws, "appointments", appt, map[string]any{"status": "cancelled", "cancellationReason": "Customer travelling"}), 200, "cancel the appointment")
	// A vendor is an account; its purchase orders and expenses hang off it.
	vendor := create(t, token, ws, "accounts", map[string]any{"name": "Battery Supplier", "type": "supplier"})
	po := create(t, token, ws, "purchase_orders", map[string]any{"name": "PO for batteries", "accountId": vendor, "total": 9000})
	create(t, token, ws, "expenses", map[string]any{"name": "Batteries", "amount": 9000, "date": "2026-10-05", "accountId": vendor, "purchaseOrderId": po})
	page := call(t, "GET", base+"/crm/accounts/"+vendor, token, nil)
	seen := map[string]bool{}
	for _, l := range page.list("related") {
		m := l.(map[string]any)
		if len(m["rows"].([]any)) > 0 {
			seen[m["object"].(string)] = true
		}
	}
	if !seen["purchase_orders"] || !seen["expenses"] {
		t.Fatalf("a vendor's page should list its purchase orders and expenses: %v", seen)
	}
	// The customer's page shows what it owns and has signed.
	page = call(t, "GET", base+"/crm/accounts/"+acc, token, nil)
	seen = map[string]bool{}
	for _, l := range page.list("related") {
		m := l.(map[string]any)
		if len(m["rows"].([]any)) > 0 {
			seen[m["object"].(string)] = true
		}
	}
	for _, object := range []string{"contracts", "assets", "appointments"} {
		if !seen[object] {
			t.Fatalf("the account page should list its %s: %v", object, seen)
		}
	}
}

// The new screens follow the same record permissions as everything else: someone who
// sees only their own records can't reach a colleague's through them.
func TestEnterpriseAPIsFollowRecordPermissions(t *testing.T) {
	boss := signIn(t, freshPhone(), "Owner")
	ws := newBusiness(t, boss, "Scoped Co")
	base := crmAPI + "/w/" + ws
	repPhone := freshPhone()
	want(t, call(t, "POST", base+"/admin/members", boss, map[string]any{"displayName": "Junior", "phone": repPhone, "roleKey": "STAFF"}), 201, "add staff")
	rep := signIn(t, repPhone, "Junior")

	acc := create(t, boss, ws, "accounts", map[string]any{"name": "Boss Account"})
	inv := create(t, boss, ws, "invoices", map[string]any{"name": "Boss invoice", "total": 1000, "status": "sent"})
	pay := create(t, boss, ws, "payments", map[string]any{"name": "Boss payment", "amount": 400, "paymentDate": "2026-10-05", "status": "paid", "invoiceId": inv})
	cs := create(t, boss, ws, "cases", map[string]any{"name": "Boss case"})
	con := create(t, boss, ws, "contracts", map[string]any{"name": "Boss contract", "startDate": "2026-01-01", "endDate": "2026-12-31", "status": "active"})

	for what, r := range map[string]resp{
		"invoice ledger": call(t, "GET", base+"/invoices/"+inv+"/payments", rep, nil),
		"case SLA":       call(t, "GET", base+"/cases/"+cs+"/sla", rep, nil),
		"renew":          call(t, "POST", base+"/contracts/"+con+"/renew", rep, map[string]any{}),
		"links":          call(t, "GET", base+"/crm/accounts/"+acc+"/relationships", rep, nil),
	} {
		if r.Status != 404 {
			t.Errorf("%s of a colleague's record: got %d, want 404 — %s", what, r.Status, truncate(r.Raw, 200))
		}
	}
	// Money is the Finance app: staff have no access to payments until a permission set grants it.
	want(t, call(t, "GET", base+"/payments/"+pay+"/allocations", rep, nil), 403, "staff reading payment allocations")
	want(t, call(t, "POST", base+"/payments/"+pay+"/refund", rep, map[string]any{"amount": 100}), 403, "staff refunding")
	want(t, call(t, "POST", base+"/crm/payments", rep, map[string]any{"values": map[string]any{"name": "Sneaky", "amount": 50, "paymentDate": "2026-10-05", "invoiceId": inv}}), 403, "staff recording a payment")
	// Their own record can't be linked to one they can't see.
	mine := create(t, rep, ws, "contacts", map[string]any{"lastName": "Mine"})
	want(t, call(t, "POST", base+"/crm/contacts/"+mine+"/relationships", rep, map[string]any{"type": "works_for", "targetObject": "accounts", "targetId": acc}), 404, "link to a record you can't see")
	if v := get(t, boss, ws, "invoices", inv); v["amountPaid"] != float64(400) {
		t.Fatalf("the invoice must be untouched: %v", v["amountPaid"])
	}
	// The receivables total counts only what they can see.
	if rec := call(t, "GET", base+"/finance/receivables", rep, nil); num(t, rec, "invoiced") != 0 {
		t.Fatalf("receivables leaked a colleague's invoices: %s", truncate(rec.Raw, 300))
	}
	// A relationship type is workspace setup: not for staff.
	want(t, call(t, "POST", base+"/relationship-types", rep, map[string]any{"key": "x_rel", "label": "X", "sourceObject": "leads", "targetObject": "leads"}), 403, "staff adding a relationship type")
	// Two people editing one record: the second save is told.
	cur := call(t, "GET", base+"/crm/payments/"+pay, boss, nil)
	v := cur.at("record", "version")
	want(t, call(t, "PATCH", base+"/crm/payments/"+pay, boss, map[string]any{"values": map[string]any{"reference": "UTR-1"}, "expectedVersion": v}), 200, "first save")
	want(t, call(t, "PATCH", base+"/crm/payments/"+pay, boss, map[string]any{"values": map[string]any{"reference": "UTR-2"}, "expectedVersion": v}), 409, "stale save")
}
