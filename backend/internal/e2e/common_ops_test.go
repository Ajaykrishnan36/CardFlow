package e2e

import (
	"strings"
	"testing"
	"time"
)

func dayOff(n int) string { return time.Now().AddDate(0, 0, n).Format("2006-01-02") }

// The Upcoming cycle every business asks for (D-151).
func TestUpcomingScheduleCallBookNextCycle(t *testing.T) {
	boss := signIn(t, freshPhone(), "Owner")
	ws := newBusiness(t, boss, "Care Co")
	base := crmAPI + "/w/" + ws
	staff, staffID := staffOf(t, boss, ws, "Caller One")
	staff2, _ := staffOf(t, boss, ws, "Caller Two")
	if !strings.Contains(call(t, "GET", base+"/context", staff, nil).Raw, `"/crm/w/`+ws+`/upcoming"`) {
		t.Fatal("staff see Upcoming in the menu")
	}
	cust := create(t, staff, ws, "contacts", map[string]any{"firstName": "Ravi", "lastName": "Kumar", "mobile": "+91 98000 12345"})
	cust2 := create(t, staff, ws, "contacts", map[string]any{"firstName": "Lata", "lastName": "S", "mobile": "+91 98000 67890"})
	post := func(tok, object string, v map[string]any) resp {
		return call(t, "POST", base+"/crm/"+object, tok, map[string]any{"values": v})
	}
	mustRefuse(t, "a schedule for nobody", post(staff, "care_schedules", map[string]any{"kind": "service", "intervalValue": 3, "intervalUnit": "months", "startDate": dayOff(0)}), "contactId")
	mustRefuse(t, "a zero interval", post(staff, "care_schedules", map[string]any{"contactId": cust, "kind": "service", "intervalValue": 0, "intervalUnit": "months", "startDate": dayOff(0)}), "intervalValue")
	sch := create(t, staff, ws, "care_schedules", map[string]any{"contactId": cust, "kind": "service", "intervalValue": 3, "intervalUnit": "months", "startDate": dayOff(0)})
	create(t, staff, ws, "care_schedules", map[string]any{"contactId": cust, "kind": "renewal", "intervalValue": 1, "intervalUnit": "years", "startDate": dayOff(5)})
	create(t, staff, ws, "care_schedules", map[string]any{"contactId": cust2, "kind": "follow_up", "intervalValue": 2, "intervalUnit": "weeks", "startDate": dayOff(-2)})
	if g := get(t, staff, ws, "care_schedules", sch); g["nextDueDate"] != dayOff(0) || g["anchor"] != "completion" || !strings.Contains(g["name"].(string), "every 3 months") {
		t.Fatalf("a schedule names itself and knows its next date: %v", g)
	}
	// Tiles: events and distinct customers, the same query as the list.
	sum := call(t, "GET", base+"/upcoming/summary", staff, nil)
	want(t, sum, 200, "summary")
	tile := map[string]map[string]any{}
	for _, x := range sum.list("tiles") {
		m := x.(map[string]any)
		tile[m["key"].(string)] = m
	}
	if num2(tile["overdue"]["items"]) != 1 || num2(tile["today"]["items"]) != 1 || num2(tile["next10"]["items"]) != 2 || num2(tile["next10"]["customers"]) != 1 {
		t.Fatalf("tiles: overdue 1, today 1, next 10 days 2 items for 1 customer — got %v", tile)
	}
	for _, k := range []string{"overdue", "today", "next10", "next30"} {
		l := call(t, "GET", base+"/upcoming/list?window="+k, staff, nil)
		if num2(l.at("total")) != num2(tile[k]["items"]) || len(l.list("data")) != int(num2(tile[k]["items"])) {
			t.Fatalf("list for %s has %v rows, tile says %v", k, l.at("total"), tile[k]["items"])
		}
	}
	row := call(t, "GET", base+"/upcoming/list?window=today", staff, nil).list("data")[0].(map[string]any)
	if row["customer"] != "Ravi Kumar" || row["phone"] != "+91 98000 12345" || num2(row["daysLeft"]) != 0 {
		t.Fatalf("a row shows customer, phone and days left: %v", row)
	}
	due := row["id"].(string)
	// Claim: one caller at a time.
	want(t, call(t, "POST", base+"/upcoming/"+due+"/claim", staff, nil), 200, "claim")
	// Someone else who can see the customer (the owner) is told who is calling...
	if c := call(t, "POST", base+"/upcoming/"+due+"/claim", boss, nil); c.Status != 409 || !strings.Contains(c.Raw, "Caller One") {
		t.Fatalf("a second caller is told who is calling: %d %s", c.Status, truncate(c.Raw, 200))
	}
	// ...and staff who only see their own customers can't even find it.
	want(t, call(t, "POST", base+"/upcoming/"+due+"/claim", staff2, nil), 404, "claiming another staff member's customer")
	// Calls: no answer → try again tomorrow; a call never marks the work done.
	want(t, post(staff, "call_logs", map[string]any{"dueItemId": due, "outcome": "no_answer"}), 201, "no answer")
	d := get(t, staff, ws, "due_items", due)
	if d["status"] != "contacted" || d["nextActionAt"] != dayOff(1) || d["lastOutcome"] != "no_answer" || d["claimedBy"] != nil {
		t.Fatalf("no answer: contacted, call again tomorrow, claim released — %v", d)
	}
	if c := call(t, "GET", base+"/upcoming/list?window=callback", staff, nil); len(c.list("data")) != 0 {
		t.Fatal("tomorrow's callback is not due today")
	}
	mustRefuse(t, "connected without a next step", post(staff, "call_logs", map[string]any{"dueItemId": due, "outcome": "connected"}), "nextActionAt")
	want(t, post(staff, "call_logs", map[string]any{"dueItemId": due, "outcome": "booked", "nextActionAt": dayOff(2), "notes": "Service on Friday"}), 201, "booked")
	if get(t, staff, ws, "due_items", due)["status"] != "booked" {
		t.Fatal("a booked call marks the item Booked, not Done")
	}
	// Done: the next occurrence, 3 calendar months after the day it was done, exactly once.
	want(t, edit(t, staff, ws, "due_items", due, map[string]any{"status": "done", "doneOn": dayOff(1)}), 200, "service done")
	dueAfter, _ := time.Parse("2006-01-02", dayOff(1))
	wantNext := dueAfter.AddDate(0, 3, 0)
	if wantNext.Day() != dueAfter.Day() { // month-end fallback
		wantNext = time.Date(wantNext.Year(), wantNext.Month(), 0, 0, 0, 0, 0, time.UTC)
	}
	items := rowsWhere(t, staff, ws, "due_items", "scheduleId", sch)
	open := 0
	for _, x := range items {
		if x["status"] == "open" {
			open++
			if x["dueDate"] != wantNext.Format("2006-01-02") || num2(x["cycle"]) != 2 {
				t.Fatalf("next service due %s (cycle 2), got %v", wantNext.Format("2006-01-02"), x)
			}
		}
	}
	if open != 1 || len(items) != 2 {
		t.Fatalf("one finished and one next item expected, got %d items (%d open)", len(items), open)
	}
	if g := get(t, staff, ws, "care_schedules", sch); num2(g["cycles"]) != 1 || g["lastDoneOn"] != dayOff(1) {
		t.Fatalf("the schedule counts what was done: %v", g)
	}
	mustRefuse(t, "reopening a done item", edit(t, staff, ws, "due_items", due, map[string]any{"status": "open"}), "status")
	// Skipped needs a reason and still plans the next one; declining on a call skips it.
	nextID := ""
	for _, x := range rowsWhere(t, staff, ws, "due_items", "scheduleId", sch) {
		if x["status"] == "open" {
			nextID = x["_id"].(string)
		}
	}
	mustRefuse(t, "skipping without a reason", edit(t, staff, ws, "due_items", nextID, map[string]any{"status": "skipped"}), "skipReason")
	want(t, post(staff, "call_logs", map[string]any{"dueItemId": nextID, "outcome": "declined", "notes": "Sold the vehicle"}), 201, "declined")
	if len(rowsWhere(t, staff, ws, "due_items", "scheduleId", sch)) != 3 {
		t.Fatal("a declined occurrence plans the following one")
	}
	// Pausing a schedule stops the calls.
	want(t, edit(t, staff, ws, "care_schedules", sch, map[string]any{"status": "paused"}), 200, "pause")
	for _, x := range rowsWhere(t, staff, ws, "due_items", "scheduleId", sch) {
		if x["status"] == "open" || x["status"] == "contacted" {
			t.Fatalf("a paused schedule leaves nothing to call: %v", x)
		}
	}
	// Staff who see only their own records don't see another person's items.
	other := call(t, "GET", base+"/upcoming/list?window=all", staff2, nil)
	if len(other.list("data")) != 0 {
		t.Fatalf("staff see only their own due items: %s", truncate(other.Raw, 200))
	}
	_ = staffID
}

// Stock, reservations, memberships, checklists, document versions, payment plans,
// payouts and credit limits (D-152).
func TestSharedOperationsModules(t *testing.T) {
	boss := signIn(t, freshPhone(), "Owner")
	ws := newBusiness(t, boss, "Ops Co")
	base := crmAPI + "/w/" + ws
	post := func(object string, v map[string]any) resp {
		return call(t, "POST", base+"/crm/"+object, boss, map[string]any{"values": v})
	}
	staff, _ := staffOf(t, boss, ws, "Store Staff")
	item := create(t, boss, ws, "catalog_items", map[string]any{"name": "Cement 53 grade", "unitPrice": 400, "unit": "bag"})
	cust := create(t, boss, ws, "contacts", map[string]any{"firstName": "Kumar", "lastName": "B", "mobile": "+91 98000 11111"})

	// --- stock ---
	lot := create(t, boss, ws, "stock_lots", map[string]any{"itemId": item, "batchNumber": "B-07", "location": "Godown 1", "unit": "bag", "quantity": 10, "reorderLevel": 3})
	if g := get(t, boss, ws, "stock_lots", lot); num2(g["available"]) != 10 || g["status"] != "available" || !strings.Contains(g["name"].(string), "Cement") {
		t.Fatalf("new stock: %v", g)
	}
	mustRefuse(t, "editing stock by hand", edit(t, boss, ws, "stock_lots", lot, map[string]any{"quantity": 50}), "quantity")
	want(t, post("stock_moves", map[string]any{"lotId": lot, "moveType": "issue", "quantity": 4, "contactId": cust}), 201, "issue 4")
	mustRefuse(t, "issuing more than available", post("stock_moves", map[string]any{"lotId": lot, "moveType": "issue", "quantity": 7}), "quantity")
	want(t, post("stock_moves", map[string]any{"lotId": lot, "moveType": "reserve", "quantity": 5}), 201, "reserve 5")
	if g := get(t, boss, ws, "stock_lots", lot); num2(g["quantity"]) != 6 || num2(g["available"]) != 1 || g["status"] != "low" {
		t.Fatalf("6 on hand, 5 reserved, 1 available, running low: %v %v %v", g["quantity"], g["available"], g["status"])
	}
	mustRefuse(t, "issuing reserved stock", post("stock_moves", map[string]any{"lotId": lot, "moveType": "issue", "quantity": 2}), "quantity")
	mustRefuse(t, "damage with no reason", post("stock_moves", map[string]any{"lotId": lot, "moveType": "damage", "quantity": 1}), "reason")
	want(t, post("stock_moves", map[string]any{"lotId": lot, "moveType": "release", "quantity": 5}), 201, "release")
	want(t, post("stock_moves", map[string]any{"lotId": lot, "moveType": "transfer", "quantity": 2, "toLocation": "Shop front"}), 201, "transfer")
	if g := get(t, boss, ws, "stock_lots", lot); num2(g["quantity"]) != 4 {
		t.Fatalf("transfer takes 2 out: %v", g["quantity"])
	}
	dest := rowsWhere(t, boss, ws, "stock_lots", "location", "Shop front")
	if len(dest) != 1 || num2(dest[0]["quantity"]) != 2 || dest[0]["batchNumber"] != "B-07" {
		t.Fatalf("the destination gains the same batch: %v", dest)
	}
	old := create(t, boss, ws, "stock_lots", map[string]any{"itemId": item, "batchNumber": "OLD", "quantity": 5, "expiryDate": dayOff(-1)})
	if get(t, boss, ws, "stock_lots", old)["status"] != "expired" {
		t.Fatal("stock past its date is Expired")
	}
	mustRefuse(t, "issuing expired stock", post("stock_moves", map[string]any{"lotId": old, "moveType": "issue", "quantity": 1}), "lotId")
	mustRefuse(t, "a count lower than reserved", post("stock_moves", map[string]any{"lotId": lot, "moveType": "count", "quantity": -1, "reason": "x"}), "quantity")
	if c := call(t, "POST", base+"/crm/stock_lots", staff, map[string]any{"values": map[string]any{"itemId": item, "quantity": 1}}); c.Status != 403 {
		t.Fatalf("staff can't add stock: %d", c.Status)
	}

	// --- reservations ---
	hall := create(t, boss, ws, "assets", map[string]any{"name": "Party hall A"})
	at := func(h int) string {
		return time.Now().AddDate(0, 0, 3).Truncate(24 * time.Hour).Add(time.Duration(h) * time.Hour).UTC().Format(time.RFC3339)
	}
	r1 := create(t, boss, ws, "item_reservations", map[string]any{"assetId": hall, "contactId": cust, "startsAt": at(10), "endsAt": at(12), "status": "confirmed", "bufferMinutes": 60, "deposit": 5000})
	mustRefuse(t, "an overlapping booking", post("item_reservations", map[string]any{"assetId": hall, "startsAt": at(11), "endsAt": at(13), "status": "confirmed"}), "assetId")
	mustRefuse(t, "inside the cleaning gap", post("item_reservations", map[string]any{"assetId": hall, "startsAt": at(12)[:13] + ":30:00Z", "endsAt": at(14), "status": "confirmed"}), "assetId")
	create(t, boss, ws, "item_reservations", map[string]any{"assetId": hall, "startsAt": at(14), "endsAt": at(16), "status": "confirmed"})
	mustRefuse(t, "returned before handed over", edit(t, boss, ws, "item_reservations", r1, map[string]any{"status": "returned", "returnCondition": "ok"}), "status")
	want(t, edit(t, boss, ws, "item_reservations", r1, map[string]any{"status": "checked_out"}), 200, "hand over")
	mustRefuse(t, "damaged with no detail", edit(t, boss, ws, "item_reservations", r1, map[string]any{"status": "returned", "returnCondition": "damaged"}), "returnNotes")
	mustRefuse(t, "returning more deposit than taken", edit(t, boss, ws, "item_reservations", r1, map[string]any{"depositReturned": 6000}), "depositReturned")
	want(t, edit(t, boss, ws, "item_reservations", r1, map[string]any{"status": "returned", "returnCondition": "damaged", "returnNotes": "Two chairs broken", "depositReturned": 4000}), 200, "returned")
	hold := create(t, boss, ws, "item_reservations", map[string]any{"assetId": hall, "startsAt": at(30), "endsAt": at(32)})
	if g := get(t, boss, ws, "item_reservations", hold); g["status"] != "hold" || g["holdUntil"] == nil {
		t.Fatalf("a new reservation is a hold with an expiry: %v", g)
	}

	// --- memberships ---
	mustRefuse(t, "ending before starting", post("memberships", map[string]any{"contactId": cust, "startDate": dayOff(0), "endDate": dayOff(-1)}), "endDate")
	m := create(t, boss, ws, "memberships", map[string]any{"contactId": cust, "itemId": item, "startDate": dayOff(-1), "endDate": dayOff(29), "sessionsTotal": 2})
	mm := get(t, boss, ws, "memberships", m)
	if mm["status"] != "active" || num2(mm["sessionsLeft"]) != 2 || mm["renewalDueId"] == nil {
		t.Fatalf("a new membership: %v", mm)
	}
	if g := get(t, boss, ws, "due_items", mm["renewalDueId"].(string)); g["dueDate"] != dayOff(29) || g["kind"] != "renewal" {
		t.Fatalf("its renewal call is on the Upcoming list at the end date: %v", g)
	}
	want(t, edit(t, boss, ws, "memberships", m, map[string]any{"sessionsUsed": 1}), 200, "use a session")
	want(t, edit(t, boss, ws, "memberships", m, map[string]any{"frozenFrom": dayOff(0), "frozenUntil": dayOff(9)}), 200, "freeze 10 days")
	mm = get(t, boss, ws, "memberships", m)
	if mm["endDate"] != dayOff(39) || mm["status"] != "frozen" || num2(mm["freezeDays"]) != 10 {
		t.Fatalf("a freeze moves the end by 10 days: %v %v %v", mm["endDate"], mm["status"], mm["freezeDays"])
	}
	if g := get(t, boss, ws, "due_items", mm["renewalDueId"].(string)); g["dueDate"] != dayOff(39) {
		t.Fatalf("the renewal call moves with it: %v", g["dueDate"])
	}
	mustRefuse(t, "using a frozen membership", edit(t, boss, ws, "memberships", m, map[string]any{"sessionsUsed": 2}), "sessionsUsed")
	want(t, edit(t, boss, ws, "memberships", m, map[string]any{"frozenFrom": nil, "frozenUntil": nil}), 200, "unfreeze")
	mustRefuse(t, "more sessions than bought", edit(t, boss, ws, "memberships", m, map[string]any{"sessionsUsed": 3}), "sessionsUsed")

	// --- checklists: steps that must be done before a stage ---
	mustRefuse(t, "a checklist for an unknown record type", post("checklist_templates", map[string]any{"name": "X", "objectKey": "nothing", "stage": "closed_won", "items": "a"}), "objectKey")
	create(t, boss, ws, "checklist_templates", map[string]any{"name": "Before winning a deal", "objectKey": "opportunities", "stage": "closed_won", "allRequired": true,
		"items": "Customer signed the quote\nAdvance received\nDelivery date agreed"})
	opp := create(t, staff, ws, "opportunities", map[string]any{"name": "Kumar — 200 bags", "amount": 80000, "closeDate": dayOff(5), "contactId": cust})
	r := edit(t, staff, ws, "opportunities", opp, map[string]any{"status": "closed_won"})
	if r.Status != 422 || !strings.Contains(r.Raw, "Advance received") {
		t.Fatalf("winning before the checklist: %d %s", r.Status, truncate(r.Raw, 300))
	}
	cl := call(t, "GET", base+"/checklists/opportunities/"+opp, staff, nil)
	want(t, cl, 200, "the deal's checklist")
	steps := cl.list("data")[0].(map[string]any)["steps"].([]any)
	if len(steps) != 3 {
		t.Fatalf("three steps: %s", truncate(cl.Raw, 300))
	}
	for _, s := range steps[:2] {
		want(t, edit(t, staff, ws, "checklist_items", s.(map[string]any)["id"].(string), map[string]any{"done": true}), 200, "tick a step")
	}
	r = edit(t, staff, ws, "opportunities", opp, map[string]any{"status": "closed_won"})
	if r.Status != 422 || !strings.Contains(r.Raw, "Delivery date agreed") || strings.Contains(r.Raw, "Advance received") {
		t.Fatalf("only the open step is named: %s", truncate(r.Raw, 300))
	}
	want(t, edit(t, staff, ws, "checklist_items", steps[2].(map[string]any)["id"].(string), map[string]any{"done": true}), 200, "last step")
	want(t, edit(t, staff, ws, "opportunities", opp, map[string]any{"status": "closed_won"}), 200, "win the deal")
	if s := call(t, "GET", base+"/checklists/opportunities/"+opp, boss, nil).list("data")[0].(map[string]any)["steps"].([]any)[0].(map[string]any); s["doneBy"] != "Store Staff" {
		t.Fatalf("a step records who did it: %v", s)
	}

	// --- document versions ---
	d1 := create(t, boss, ws, "doc_revisions", map[string]any{"name": "Kitchen layout", "objectKey": "opportunities", "recordId": opp, "summary": "First draft", "status": "sent"})
	mustRefuse(t, "approved with no name", edit(t, boss, ws, "doc_revisions", d1, map[string]any{"status": "approved"}), "approvedByName")
	want(t, edit(t, boss, ws, "doc_revisions", d1, map[string]any{"status": "approved", "approvedByName": "Kumar B"}), 200, "customer approves v1")
	mustRefuse(t, "changing an approved version", edit(t, boss, ws, "doc_revisions", d1, map[string]any{"summary": "edited"}), "status")
	d2 := create(t, boss, ws, "doc_revisions", map[string]any{"name": "Kitchen layout", "objectKey": "opportunities", "recordId": opp, "summary": "Moved the sink"})
	if num2(get(t, boss, ws, "doc_revisions", d2)["version"]) != 2 {
		t.Fatal("the next version is numbered 2")
	}
	want(t, edit(t, boss, ws, "doc_revisions", d2, map[string]any{"status": "approved", "approvedByName": "Kumar B"}), 200, "customer approves v2")
	if get(t, boss, ws, "doc_revisions", d1)["status"] != "superseded" {
		t.Fatal("approving v2 replaces v1")
	}

	// --- payment plans ---
	i1 := create(t, boss, ws, "instalments", map[string]any{"contactId": cust, "opportunityId": opp, "milestone": "Advance", "dueDate": dayOff(3), "amount": 20000})
	ii := get(t, boss, ws, "instalments", i1)
	if ii["dueItemId"] == nil || ii["status"] != "due" {
		t.Fatalf("an instalment goes on the Upcoming list: %v", ii)
	}
	mustRefuse(t, "paying more than due", edit(t, boss, ws, "instalments", i1, map[string]any{"paidAmount": 25000}), "paidAmount")
	want(t, edit(t, boss, ws, "instalments", i1, map[string]any{"paidAmount": 5000}), 200, "part payment")
	if get(t, boss, ws, "instalments", i1)["status"] != "part_paid" {
		t.Fatal("part payment is Part paid")
	}
	want(t, edit(t, boss, ws, "instalments", i1, map[string]any{"paidAmount": 20000}), 200, "paid in full")
	if g := get(t, boss, ws, "due_items", ii["dueItemId"].(string)); g["status"] != "done" {
		t.Fatalf("the follow-up closes when paid: %v", g["status"])
	}
	late := create(t, boss, ws, "instalments", map[string]any{"contactId": cust, "dueDate": dayOff(-2), "amount": 1000})
	if get(t, boss, ws, "instalments", late)["status"] != "overdue" {
		t.Fatal("an instalment past its date is Overdue")
	}

	// --- commissions & payouts ---
	me := call(t, "GET", crmAPI+"/me", boss, nil).str("identity", "id")
	p := create(t, boss, ws, "payout_entries", map[string]any{"payeeUserId": me, "opportunityId": opp, "kind": "commission", "basisAmount": 80000, "percent": 2.5})
	if num2(get(t, boss, ws, "payout_entries", p)["amount"]) != 2000 {
		t.Fatal("2.5% of 80,000 is 2,000")
	}
	mustRefuse(t, "approving your own payout", edit(t, boss, ws, "payout_entries", p, map[string]any{"status": "approved"}), "status")
	mustRefuse(t, "paying before approval", edit(t, boss, ws, "payout_entries", p, map[string]any{"status": "paid"}), "status")

	// --- credit limit ---
	acc := create(t, boss, ws, "accounts", map[string]any{"name": "Kumar Builders", "creditLimit": 100000})
	create(t, boss, ws, "invoices", map[string]any{"name": "Inv 1", "accountId": acc, "total": 80000, "status": "sent"})
	r = post("invoices", map[string]any{"name": "Inv 2", "accountId": acc, "total": 30000, "status": "sent"})
	if r.Status != 422 || !strings.Contains(r.Raw, "credit limit") {
		t.Fatalf("an invoice past the credit limit: %d %s", r.Status, truncate(r.Raw, 300))
	}
	want(t, post("invoices", map[string]any{"name": "Inv 2", "accountId": acc, "total": 30000, "status": "sent", "creditExceptionReason": "Owner approved — cheque in hand"}), 201, "with a written reason")
	want(t, post("invoices", map[string]any{"name": "Draft", "accountId": acc, "total": 500000, "status": "draft"}), 201, "drafts are not checked")

	// --- the standard Service desk set exists for every business ---
	found := false
	for _, s := range call(t, "GET", base+"/admin/options", boss, nil).list("permissionSets") {
		if s.(map[string]any)["name"] == "Service desk" {
			found = true
		}
	}
	if !found {
		t.Fatal("every business has a ready-made Service desk permission set")
	}
}
