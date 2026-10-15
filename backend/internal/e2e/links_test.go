package e2e

import (
	"strings"
	"testing"
	"time"
)

// A quote made for an opportunity starts with its products; a contract made for an order
// starts with what was sold. Revisions and conversions keep their own lines (D-154).
func TestDocumentsInheritWhatTheyWereMadeFrom(t *testing.T) {
	boss := signIn(t, freshPhone(), "Owner")
	ws := newBusiness(t, boss, "Links Co")
	base := crmAPI + "/w/" + ws
	chair := create(t, boss, ws, "catalog_items", map[string]any{"name": "Chair", "unitPrice": 1000, "taxRate": 18})
	desk := create(t, boss, ws, "catalog_items", map[string]any{"name": "Desk", "unitPrice": 5000, "taxRate": 18})
	acc := create(t, boss, ws, "accounts", map[string]any{"name": "Buyer"})
	opp := create(t, boss, ws, "opportunities", map[string]any{"name": "Fit-out", "accountId": acc, "closeDate": dayOff(30)})
	want(t, call(t, "PUT", base+"/opportunities/"+opp+"/lines", boss, map[string]any{"lines": []map[string]any{{"itemId": chair, "quantity": 4}, {"itemId": desk, "quantity": 1}}}), 200, "deal lines")
	amount := num2(get(t, boss, ws, "opportunities", opp)["amount"])
	if amount != 10620 {
		t.Fatalf("deal amount 4×1000 + 5000 with 18%% tax = 10620, got %v", amount)
	}
	q := create(t, boss, ws, "quotes", map[string]any{"name": "Quote — Fit-out", "opportunityId": opp, "accountId": acc})
	qv := get(t, boss, ws, "quotes", q)
	if n := len(call(t, "GET", base+"/quotes/"+q+"/lines", boss, nil).list("pricing", "lines")); n != 2 || num2(qv["total"]) != 10620 || num2(qv["subtotal"]) != 9000 || num2(qv["tax"]) != 1620 {
		t.Fatalf("the quote starts with the deal's 2 lines and totals: %d lines, %v", n, qv)
	}
	// A quote with its own amount, or with no deal, starts as it was given.
	empty := create(t, boss, ws, "quotes", map[string]any{"name": "Blank", "accountId": acc})
	if n := len(call(t, "GET", base+"/quotes/"+empty+"/lines", boss, nil).list("pricing", "lines")); n != 0 {
		t.Fatalf("a quote for no deal has no lines, got %d", n)
	}
	// Revising keeps exactly the old quote's lines (no second copy from the deal).
	want(t, edit(t, boss, ws, "quotes", q, map[string]any{"status": "sent"}), 200, "sent")
	rev := call(t, "POST", base+"/quotes/"+q+"/revise", boss, nil)
	want(t, rev, 201, "revise")
	if n := len(call(t, "GET", base+"/quotes/"+rev.str("id")+"/lines", boss, nil).list("pricing", "lines")); n != 2 {
		t.Fatalf("a revision has the quote's 2 lines, got %d", n)
	}
	want(t, edit(t, boss, ws, "quotes", q, map[string]any{"status": "accepted"}), 200, "accepted")
	so := call(t, "POST", base+"/quotes/"+q+"/convert", boss, nil)
	want(t, so, 201, "order")
	if n := len(call(t, "GET", base+"/sales_orders/"+so.str("id")+"/lines", boss, nil).list("pricing", "lines")); n != 2 {
		t.Fatalf("the order has 2 lines, got %d", n)
	}
	ct := create(t, boss, ws, "contracts", map[string]any{"name": "Supply", "accountId": acc, "orderId": so.str("id"), "startDate": dayOff(0), "endDate": dayOff(364)})
	if c := get(t, boss, ws, "contracts", ct); num2(c["contractValue"]) != 10620 || len(call(t, "GET", base+"/contracts/"+ct+"/lines", boss, nil).list("pricing", "lines")) != 2 {
		t.Fatalf("the contract carries the order's value and lines: %v", c)
	}
	// A contract given its own value keeps it.
	own := create(t, boss, ws, "contracts", map[string]any{"name": "Own value", "accountId": acc, "orderId": so.str("id"), "contractValue": 500, "startDate": dayOff(0), "endDate": dayOff(30)})
	if c := get(t, boss, ws, "contracts", own); num2(c["contractValue"]) != 500 {
		t.Fatalf("a contract's own value wins: %v", c["contractValue"])
	}
}

// A business adds its own values to a built-in dropdown; built-in values stay (D-154).
func TestBuiltInDropdownTakesTheBusinessValues(t *testing.T) {
	boss := signIn(t, freshPhone(), "Owner")
	ws := newBusiness(t, boss, "Dropdown Co")
	base := crmAPI + "/w/" + ws
	staff, _ := staffOf(t, boss, ws, "Desk")
	field := func() map[string]any {
		for _, f := range call(t, "GET", base+"/crm/meta/cases", boss, nil).list("fields") {
			if m := f.(map[string]any); m["key"] == "category" {
				return m
			}
		}
		t.Fatal("cases have a category")
		return nil
	}
	var builtIn []map[string]any
	for _, o := range field()["options"].([]any) {
		m := o.(map[string]any)
		builtIn = append(builtIn, map[string]any{"value": m["value"], "label": m["label"]})
	}
	with := func(extra ...map[string]any) []map[string]any {
		return append(append([]map[string]any{}, builtIn...), extra...)
	}
	svc := map[string]any{"value": "bike_service", "label": "Bike service"}
	path := base + "/crm/meta/cases/fields/category"
	mustRefuse(t, "a case in a category that doesn't exist yet", call(t, "POST", base+"/crm/cases", boss, map[string]any{"values": map[string]any{"name": "Noise", "category": "bike_service"}}), "category")
	if r := call(t, "PATCH", path, staff, map[string]any{"options": with(svc)}); r.Status != 403 {
		t.Fatalf("staff can't change dropdowns: %d", r.Status)
	}
	mustRefuse(t, "removing a built-in value", call(t, "PATCH", path, boss, map[string]any{"options": builtIn[1:]}), "options")
	want(t, call(t, "PATCH", path, boss, map[string]any{"options": with(svc)}), 200, "add Bike service")
	f := field()
	opts := f["options"].([]any)
	if last := opts[len(opts)-1].(map[string]any); last["value"] != "bike_service" || len(f["addedOptions"].([]any)) != 1 {
		t.Fatalf("the added value comes after the built-in ones and is marked: %v", f)
	}
	c := create(t, boss, ws, "cases", map[string]any{"name": "Noise", "category": "bike_service"})
	mustRefuse(t, "removing a value a case uses", call(t, "PATCH", path, boss, map[string]any{"options": builtIn}), "options")
	want(t, edit(t, boss, ws, "cases", c, map[string]any{"category": "problem"}), 200, "move the case off it")
	want(t, call(t, "PATCH", path, boss, map[string]any{"options": builtIn}), 200, "remove Bike service")
	if _, ok := field()["addedOptions"]; ok {
		t.Fatal("no added values left")
	}
	mustRefuse(t, "statuses aren't extended here", call(t, "PATCH", base+"/crm/meta/cases/fields/status", boss, map[string]any{"options": []map[string]any{{"value": "x", "label": "X"}}}), "options")
	// Another business doesn't see this business's values.
	other := newBusiness(t, boss, "Other Dropdown Co")
	want(t, call(t, "PATCH", path, boss, map[string]any{"options": with(svc)}), 200, "add again")
	if strings.Contains(call(t, "GET", crmAPI+"/w/"+other+"/crm/meta/cases", boss, nil).Raw, "bike_service") {
		t.Fatal("added values belong to one business")
	}
}

// Workflows can address the record's owner and its linked records, and round robin
// skips people on leave (D-154).
func TestWorkflowOwnerLinkedRecordsAndLeave(t *testing.T) {
	boss := signIn(t, freshPhone(), "Owner")
	ws := newBusiness(t, boss, "Workshop Co")
	base := crmAPI + "/w/" + ws
	_, ganesh := staffOf(t, boss, ws, "Ganesh")
	_, bala := staffOf(t, boss, ws, "Bala")
	if r := call(t, "POST", base+"/teams", boss, map[string]any{"name": "Service", "members": []string{ganesh, bala}}); r.Status != 200 && r.Status != 201 {
		t.Fatalf("team: %d %s", r.Status, truncate(r.Raw, 200))
	}
	var team string
	for _, x := range call(t, "GET", base+"/teams", boss, nil).list("data") {
		if m := x.(map[string]any); m["name"] == "Service" {
			team = m["id"].(string)
		}
	}
	flow := func(name string, trigger map[string]any, steps []map[string]any) string {
		w := call(t, "POST", base+"/workflows", boss, map[string]any{"name": name, "draft": map[string]any{"trigger": trigger, "steps": steps}})
		want(t, w, 201, name)
		want(t, call(t, "POST", base+"/workflows/"+w.str("id")+"/publish", boss, nil), 200, "publish "+name)
		call(t, "POST", base+"/workflows/"+w.str("id")+"/status", boss, map[string]any{"status": "active"})
		return w.str("id")
	}
	rr := flow("Round robin", map[string]any{"type": "record.created", "object": "cases"},
		[]map[string]any{{"id": "a", "type": "assign", "config": map[string]any{"object": "cases", "recordId": "{{trigger.recordId}}", "teamId": team}}})
	mail := flow("Tell the customer", map[string]any{"type": "record.updated", "object": "cases", "fields": []string{"status"}},
		[]map[string]any{{"id": "m", "type": "send_email", "config": map[string]any{"to": "{{trigger.related.contactId.email}}", "subject": "Update for {{trigger.related.contactId.firstName}} from {{trigger.owner.name}}"}}})
	res := create(t, boss, ws, "service_resources", map[string]any{"name": "Ganesh", "userId": ganesh})
	create(t, boss, ws, "resource_absences", map[string]any{"name": "Leave", "resourceId": res,
		"startsAt": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339), "endsAt": time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)})
	cust := create(t, boss, ws, "contacts", map[string]any{"firstName": "Ravi", "lastName": "K", "email": "ravi.links@example.com"})
	runs := func(wf string, n int) []any {
		deadline := time.Now().Add(40 * time.Second)
		for {
			l := call(t, "GET", base+"/workflows/"+wf+"/runs", boss, nil).list("data")
			done := 0
			for _, x := range l {
				if s := x.(map[string]any)["status"]; s == "completed" || s == "failed" {
					done++
				}
			}
			if done >= n || time.Now().After(deadline) {
				return l
			}
			time.Sleep(time.Second)
		}
	}
	c1 := create(t, boss, ws, "cases", map[string]any{"name": "One", "contactId": cust})
	c2 := create(t, boss, ws, "cases", map[string]any{"name": "Two", "contactId": cust})
	runs(rr, 2)
	for _, c := range []string{c1, c2} {
		if o := get(t, boss, ws, "cases", c)["ownerId"]; o != bala {
			t.Fatalf("round robin skips Ganesh on leave: case owned by %v", o)
		}
	}
	want(t, edit(t, boss, ws, "cases", c1, map[string]any{"status": "working"}), 200, "status")
	l := runs(mail, 1)
	if len(l) == 0 {
		t.Fatal("the email workflow ran")
	}
	run := l[0].(map[string]any)
	if run["status"] != "completed" {
		t.Fatalf("the email went to the case's contact: %v", truncate(call(t, "GET", base+"/workflows/"+mail+"/runs", boss, nil).Raw, 600))
	}
	trig := run["trigger"].(map[string]any)
	if trig["owner"].(map[string]any)["name"] != "Bala" || trig["related"].(map[string]any)["contactId"].(map[string]any)["email"] != "ravi.links@example.com" {
		t.Fatalf("trigger carries the owner and the linked contact: %v", trig)
	}
}
