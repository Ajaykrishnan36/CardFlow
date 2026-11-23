package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The Lawyer CRM use case (D-185): the acceptance scenarios, end to end.
func TestLawyerCRMAcceptance(t *testing.T) {
	boss := signIn(t, freshPhone(), "Main Advocate")
	made := call(t, "POST", crmAPI+"/businesses", boss, map[string]any{"name": "Lex Chambers", "industry": "Legal services", "businessType": "Law firm"})
	want(t, made, 201, "create the firm")
	ws := made.str("business", "code")
	base := crmAPI + "/w/" + ws
	day := func(n int) string { return time.Now().AddDate(0, 0, n).Format("2006-01-02") }
	at := func(n int) string { return time.Now().AddDate(0, 0, n).UTC().Format(time.RFC3339) }
	member := func(name, role string) (string, string) {
		phone := freshPhone()
		want(t, call(t, "POST", base+"/admin/members", boss, map[string]any{"displayName": name, "email": fmt.Sprintf("%s-%s@example.com", strings.ReplaceAll(strings.ToLower(name), " ", ""), strings.ToLower(ws)),
			"phone": phone, "roleKey": role, "method": "invite", "userType": "team_member"}), 201, "invite "+name)
		in := signInAt(t, phone, ws)
		want(t, in, 200, name+" signs in")
		tok := in.str("token")
		return tok, call(t, "GET", crmAPI+"/me", tok, nil).str("identity", "id")
	}
	seniorA, _ := member("Senior A", "SENIOR_ADVOCATE")
	juniorA1, juniorA1ID := member("Junior A1", "JUNIOR_ADVOCATE")
	juniorA2, juniorA2ID := member("Junior A2", "JUNIOR_ADVOCATE")
	juniorB1, _ := member("Junior B1", "JUNIOR_ADVOCATE")
	clerk, _ := member("Clerk C", "LEGAL_ASSISTANT")
	books, _ := member("Accounts D", "LEGAL_ACCOUNTS")

	// Catalogue: scope, documents, price.
	book := create(t, boss, ws, "price_books", map[string]any{"name": "Standard fees", "isDefault": true, "status": "active"})
	service := func(name string, fee float64, docs ...string) string {
		id := create(t, boss, ws, "catalog_items", map[string]any{"name": name, "unitPrice": fee, "itemType": "service"})
		want(t, entry(t, boss, ws, book, id, fee, nil), 200, "price "+name)
		create(t, boss, ws, "service_scopes", map[string]any{"name": "Drafting and filing", "serviceId": id, "inclusion": "included", "status": "active"})
		for _, d := range docs {
			create(t, boss, ws, "service_requirements", map[string]any{"name": d, "serviceId": id, "need": "required", "status": "active"})
		}
		return id
	}
	accident := service("Accident Compensation (MACT)", 45000, "FIR", "Medical records", "Insurance copy")
	land := service("Property Land Dispute", 50000, "ID proof", "Sale deed", "EC")
	want(t, call(t, "PUT", base+"/approvals/rules", boss, map[string]any{"kind": "discount", "rules": []map[string]any{{"minValue": 15, "approverRole": "ADMIN", "label": "Main advocate"}}}), 200, "discount limit")

	// 1, 2: a lead stays a lead; duplicates warn; advertising is refused.
	kumarPhone := "+9198" + fmt.Sprintf("%08d", time.Now().UnixNano()%100000000)
	lead := create(t, boss, ws, "leads", map[string]any{"firstName": "Ravi", "lastName": "Kumar", "phone": kumarPhone, "source": "phone", "status": "new"})
	task := create(t, boss, ws, "tasks", map[string]any{"name": "Call Kumar", "leadId": lead, "dueDate": day(1), "status": "not_started"})
	if task == "" || get(t, boss, ws, "leads", lead)["status"] == "converted" {
		t.Fatal("1: a follow-up leaves the lead unconverted")
	}
	dup := call(t, "POST", base+"/crm/leads", boss, map[string]any{"values": map[string]any{"lastName": "Kumar Two", "phone": kumarPhone, "source": "phone"}})
	want(t, dup, 201, "2: a second lead")
	if d := call(t, "GET", base+"/crm/leads/"+dup.str("id")+"/duplicates", boss, nil); len(d.list("data")) != 1 {
		t.Fatalf("2: same mobile must be flagged: %s", truncate(d.Raw, 300))
	}
	if r := call(t, "POST", base+"/crm/leads", boss, map[string]any{"values": map[string]any{"lastName": "Ad Lead", "phone": "+919811100001", "source": "advertisement"}}); r.Status < 400 {
		t.Fatalf("Rule 36: advertisement must be refused: %d", r.Status)
	}

	// 3: intake shows scope, documents and price.
	intake := create(t, boss, ws, "case_intakes", map[string]any{"name": "Road accident claim", "leadId": lead, "serviceId": accident, "opponents": "Zed Transport"})
	svc := call(t, "GET", base+"/legal/services/"+accident, boss, nil)
	want(t, svc, 200, "3: service panel")
	if num2(svc.at("price")) != 45000 || len(svc.list("documents")) != 3 || len(svc.list("scope")) != 1 {
		t.Fatalf("3: scope, documents, ₹45,000: %s", truncate(svc.Raw, 400))
	}
	if get(t, boss, ws, "case_intakes", intake)["conflictStatus"] != "clear" {
		t.Fatalf("a clean name clears: %v", get(t, boss, ws, "case_intakes", intake)["conflictStatus"])
	}

	// 4: convert as an individual, no duplicates; the intake moves.
	cv := call(t, "POST", base+"/crm/leads/"+lead+"/convert", boss, map[string]any{"account": map[string]any{"mode": "new", "name": "Ravi Kumar", "kind": "individual"}, "createContact": true,
		"opportunity": map[string]any{"create": true, "name": "x", "closeDate": day(30)}})
	want(t, cv, 200, "4: convert")
	i := get(t, boss, ws, "case_intakes", intake)
	opp, _ := i["opportunityId"].(string)
	acct, _ := i["accountId"].(string)
	contact, _ := i["contactId"].(string)
	if opp == "" || acct == "" || contact == "" {
		t.Fatalf("4: intake moves to the client: %v", i)
	}
	if o := get(t, boss, ws, "opportunities", opp); !strings.Contains(fmt.Sprint(o["name"]), "Ravi Kumar – Accident") {
		t.Fatalf("4: deal is Client – Service: %v", o["name"])
	}

	// 5: a business with two contacts.
	abc := create(t, boss, ws, "accounts", map[string]any{"name": "ABC Traders", "kind": "business", "clientTaxCategory": "gst_business", "gstin": "33ABCDE1234F1Z5"})
	abc1 := create(t, boss, ws, "contacts", map[string]any{"lastName": "Mani", "accountId": abc})
	create(t, boss, ws, "contacts", map[string]any{"lastName": "Latha", "accountId": abc})

	// 6, 7: revise, discount approval, accept.
	q1 := create(t, boss, ws, "quotes", map[string]any{"name": "Fee quote", "opportunityId": opp, "accountId": acct, "contactId": contact, "status": "draft"})
	want(t, call(t, "PUT", base+"/quotes/"+q1+"/lines", boss, map[string]any{"lines": []map[string]any{{"itemId": accident, "quantity": 1}}}), 200, "6: price v1")
	rv := call(t, "POST", base+"/quotes/"+q1+"/revise", boss, nil)
	want(t, rv, 201, "6: revise")
	q2 := rv.str("id")
	if get(t, boss, ws, "quotes", q1)["status"] != "superseded" {
		t.Fatalf("6: v1 is Superseded: %v", get(t, boss, ws, "quotes", q1)["status"])
	}
	want(t, call(t, "PUT", base+"/quotes/"+q2+"/lines", boss, map[string]any{"lines": []map[string]any{{"itemId": accident, "quantity": 1, "discountPercent": 20}}}), 200, "7: over the limit")
	if r := edit(t, boss, ws, "quotes", q2, map[string]any{"status": "sent"}); r.Status != 422 {
		t.Fatalf("7: can't send before approval: %d", r.Status)
	}
	want(t, call(t, "POST", base+"/approvals/"+call(t, "GET", base+"/approvals", boss, nil).list("data")[0].(map[string]any)["id"].(string)+"/decide", boss, map[string]any{"status": "rejected"}), 200, "7: refuse")
	want(t, call(t, "PUT", base+"/quotes/"+q2+"/lines", boss, map[string]any{"lines": []map[string]any{{"itemId": accident, "quantity": 1, "discountPercent": 10}}}), 200, "6: ₹40,500")
	want(t, edit(t, boss, ws, "quotes", q2, map[string]any{"status": "sent", "approvalStatus": "not_required"}), 200, "6: send")
	mustRefuse(t, "accept needs the date", edit(t, boss, ws, "quotes", q2, map[string]any{"status": "accepted"}), "acceptedOn")
	mustRefuse(t, "accept needs the advance", edit(t, boss, ws, "quotes", q2, map[string]any{"status": "accepted", "acceptedOn": day(0), "acceptanceMethod": "signed", "engagementSignedOn": day(0), "paymentTerms": "advance_balance"}), "advanceAmount")
	want(t, edit(t, boss, ws, "quotes", q2, map[string]any{"status": "accepted", "acceptedOn": day(0), "acceptanceMethod": "signed", "engagementSignedOn": day(0), "paymentTerms": "advance_balance", "advanceAmount": 10000}), 200, "6: accept")
	want(t, edit(t, boss, ws, "opportunities", opp, map[string]any{"status": "closed_won"}), 200, "6: Closed Won")

	// 8: Closed Won makes the matter, its detail and checklist.
	list := func(tok, object, q string) []any {
		r := call(t, "GET", base+"/crm/"+object+"?pageSize=100"+q, tok, nil)
		want(t, r, 200, "list "+object)
		return r.list("data")
	}
	var matter string
	for _, m := range list(boss, "legal_matters", "") {
		if x := m.(map[string]any); x["values"].(map[string]any)["opportunityId"] == opp {
			matter = x["id"].(string)
		}
	}
	m := get(t, boss, ws, "legal_matters", matter)
	if m["caseKind"] != "accident" || num2(m["feeAgreed"]) != 40500 || num2(m["outstanding"]) != 40500 {
		t.Fatalf("8: matter from the accepted quote: %v %v %v", m["caseKind"], m["feeAgreed"], m["outstanding"])
	}
	if len(list(boss, "accident_details", "")) != 1 || len(list(boss, "client_requirements", "")) != 3 {
		t.Fatal("8: accident detail and three checklist items")
	}

	if ms := list(boss, "payment_milestones", ""); len(ms) != 1 || num2(ms[0].(map[string]any)["values"].(map[string]any)["amount"]) != 10000 {
		t.Fatalf("the advance becomes a milestone: %v", ms)
	}
	if m["acceptanceMethod"] != "signed" {
		t.Fatalf("acceptance carried to the matter: %v", m["acceptanceMethod"])
	}

	// An approved discount can go out; the queue; a minor needs a guardian.
	qa := create(t, boss, ws, "quotes", map[string]any{"name": "Approved discount", "accountId": abc, "contactId": abc1, "status": "draft"})
	want(t, call(t, "PUT", base+"/quotes/"+qa+"/lines", boss, map[string]any{"lines": []map[string]any{{"itemId": land, "quantity": 1, "discountPercent": 20}}}), 200, "7: over the limit again")
	if r := edit(t, boss, ws, "quotes", qa, map[string]any{"status": "sent"}); r.Status != 422 {
		t.Fatalf("7: waits for approval: %d", r.Status)
	}
	pending := call(t, "GET", base+"/approvals", boss, nil).list("data")
	var reqID string
	for _, p := range pending {
		if x := p.(map[string]any); x["status"] == "pending" {
			reqID = x["id"].(string)
		}
	}
	want(t, call(t, "POST", base+"/approvals/"+reqID+"/decide", boss, map[string]any{"status": "approved"}), 200, "7: main advocate approves")
	want(t, edit(t, boss, ws, "quotes", qa, map[string]any{"status": "sent"}), 200, "7: sent after approval")
	ql := create(t, boss, ws, "leads", map[string]any{"lastName": "Queue Lead", "phone": "+919811100088", "source": "web", "sendToQueue": true})
	if v := get(t, boss, ws, "leads", ql); v["ownerId"] != nil {
		t.Fatalf("queue lead has no owner: %v", v["ownerId"])
	}
	if num2(call(t, "GET", base+"/legal/diary", boss, nil).at("tiles", "unassigned")) != 1 {
		t.Fatal("the queue is counted on the diary")
	}
	want(t, edit(t, boss, ws, "leads", ql, map[string]any{"ownerId": juniorA2ID}), 200, "assign from the queue")
	if v := get(t, boss, ws, "leads", ql); v["sendToQueue"] != false || v["ownerId"] != juniorA2ID {
		t.Fatalf("assigned leaves the queue: %v %v", v["sendToQueue"], v["ownerId"])
	}
	det := list(boss, "accident_details", "")[0].(map[string]any)["id"].(string)
	mustRefuse(t, "a minor needs a guardian", edit(t, boss, ws, "accident_details", det, map[string]any{"involvesMinor": true}), "guardianConsent")
	want(t, edit(t, boss, ws, "accident_details", det, map[string]any{"involvesMinor": true, "guardianContactId": contact, "guardianConsent": true}), 200, "guardian recorded")

	// 9: Flow B, second case for the same client stays separate.
	i2 := create(t, boss, ws, "case_intakes", map[string]any{"name": "Land dispute", "accountId": acct, "contactId": contact, "serviceId": land, "opponents": "Zed Transport"})
	_ = i2
	opp2 := create(t, boss, ws, "opportunities", map[string]any{"name": "Ravi Kumar – Land", "accountId": acct, "contactId": contact, "status": "qualification", "amount": 50000, "closeDate": day(30)})
	want(t, edit(t, boss, ws, "case_intakes", i2, map[string]any{"opportunityId": opp2}), 200, "9: link")
	want(t, edit(t, boss, ws, "opportunities", opp2, map[string]any{"status": "closed_won"}), 200, "9: won")
	if len(list(boss, "legal_matters", "")) != 2 {
		t.Fatal("9: a second, separate matter")
	}

	// Conflict with a known opposing party needs a decision before conversion.
	create(t, boss, ws, "case_parties", map[string]any{"name": "Vikram Rao", "matterId": matter, "partyRole": "opponent"})
	l3 := create(t, boss, ws, "leads", map[string]any{"firstName": "Vikram", "lastName": "Rao", "phone": "+919811100077", "source": "walk_in"})
	i3 := create(t, boss, ws, "case_intakes", map[string]any{"name": "Rao matter", "leadId": l3, "serviceId": land})
	if get(t, boss, ws, "case_intakes", i3)["conflictStatus"] != "review_required" {
		t.Fatalf("conflict flagged: %v", get(t, boss, ws, "case_intakes", i3)["conflictStatus"])
	}
	if r := call(t, "POST", base+"/crm/leads/"+l3+"/convert", boss, map[string]any{"account": map[string]any{"mode": "none"}, "createContact": true}); r.Status < 400 {
		t.Fatalf("conflict blocks conversion: %d", r.Status)
	}

	// 13: assignment decides access.
	asg := create(t, boss, ws, "advocate_assignments", map[string]any{"name": "A1 primary", "matterId": matter, "advocateId": juniorA1ID, "assignmentRole": "primary"})
	_ = asg
	fir := create(t, boss, ws, "legal_documents", map[string]any{"name": "FIR copy", "matterId": matter, "category": "accident", "status": "received"})
	fileID := legalUpload(t, boss, base+"/crm/legal_documents/"+fir+"/files")
	want(t, call(t, "GET", base+"/files/"+fileID, juniorA1, nil), 200, "13: A1 opens the file")
	if r := call(t, "GET", base+"/files/"+fileID, juniorB1, nil); r.Status == 200 {
		t.Fatal("13: B1 must not open the file")
	}
	want(t, call(t, "GET", base+"/crm/legal_matters/"+matter, juniorA1, nil), 200, "13: A1 sees it")
	if len(list(juniorA1, "legal_documents", "")) != 1 {
		t.Fatal("13: A1 sees its files")
	}
	want(t, call(t, "GET", base+"/crm/legal_matters/"+matter, seniorA, nil), 200, "13: Senior A sees their junior's matter")
	if r := call(t, "GET", base+"/crm/legal_matters/"+matter, juniorB1, nil); r.Status == 200 {
		t.Fatal("13: B1 must not see it")
	}
	if len(list(juniorB1, "legal_documents", "")) != 0 {
		t.Fatal("13: B1 sees no files")
	}

	// 10, 11: hearings.
	h1 := create(t, boss, ws, "court_hearings", map[string]any{"name": "First hearing", "matterId": matter, "hearingAt": at(-1), "status": "scheduled", "assignedAdvocateId": juniorA1ID, "courtName": "MACT Chennai"})
	d := call(t, "GET", base+"/legal/diary", boss, nil)
	want(t, d, 200, "11: diary")
	if num2(d.at("tiles", "notUpdated")) != 1 {
		t.Fatalf("11: passed hearing is not updated: %s", truncate(d.Raw, 300))
	}
	next := at(7)
	want(t, edit(t, boss, ws, "court_hearings", h1, map[string]any{"status": "adjourned", "nextHearingAt": next, "nextPurpose": "evidence", "judgeRemarks": "Produce records", "followUp": "Collect bills"}), 200, "10: update")
	if o := get(t, boss, ws, "court_hearings", h1); o["status"] != "adjourned" || o["judgeRemarks"] != "Produce records" {
		t.Fatalf("10: kept: %v", o["status"])
	}
	if len(list(boss, "court_hearings", "")) != 2 {
		t.Fatal("10: next hearing created")
	}
	if !strings.HasPrefix(fmt.Sprint(get(t, boss, ws, "legal_matters", matter)["nextHearingAt"]), next[:10]) {
		t.Fatalf("10: matter's next date: %v", get(t, boss, ws, "legal_matters", matter)["nextHearingAt"])
	}

	// 12: filters.
	if len(call(t, "GET", base+"/legal/diary?caseType=accident&from="+day(0)+"&to="+day(8), boss, nil).list("rows")) != 1 || len(call(t, "GET", base+"/legal/diary?caseType=family&from="+day(0)+"&to="+day(8), boss, nil).list("rows")) != 0 {
		t.Fatal("12: case-type filter")
	}
	if len(call(t, "GET", base+"/legal/diary", juniorB1, nil).list("rows")) != 0 {
		t.Fatal("13: B1's diary is empty")
	}

	// 14: reassign.
	mustRefuse(t, "14: a reason is needed", call(t, "POST", base+"/crm/advocate_assignments", boss, map[string]any{"values": map[string]any{"name": "A2", "matterId": matter, "advocateId": juniorA2ID, "assignmentRole": "primary"}}), "reassignmentReason")
	create(t, boss, ws, "advocate_assignments", map[string]any{"name": "A2 primary", "matterId": matter, "advocateId": juniorA2ID, "assignmentRole": "primary", "reassignmentReason": "A1 on leave"})
	if r := call(t, "GET", base+"/crm/legal_matters/"+matter, juniorA1, nil); r.Status == 200 {
		t.Fatal("14: A1 loses access")
	}
	want(t, call(t, "GET", base+"/crm/legal_matters/"+matter, juniorA2, nil), 200, "14: A2 has it")
	want(t, call(t, "GET", base+"/files/"+fileID, juniorA2, nil), 200, "14: A2 opens the file")
	if r := call(t, "GET", base+"/files/"+fileID, juniorA1, nil); r.Status == 200 {
		t.Fatal("14: A1 loses the file")
	}
	if get(t, boss, ws, "advocate_assignments", asg)["status"] != "ended" {
		t.Fatal("14: history kept")
	}

	// Roles: clerk has no fees; accounts has no case documents.
	if c := call(t, "GET", base+"/crm/legal_invoices", clerk, nil); c.Status == 200 {
		t.Fatal("clerk must not see invoices")
	}
	if c := call(t, "GET", base+"/crm/legal_documents", books, nil); c.Status == 200 {
		t.Fatal("accounts must not see documents")
	}

	// 15 and RCM + TDS: nothing falsely outstanding.
	inv := create(t, boss, ws, "legal_invoices", map[string]any{"name": "Fee invoice 1", "matterId": matter, "grossFee": 40500})
	create(t, boss, ws, "legal_payments", map[string]any{"name": "Advance", "matterId": matter, "invoiceId": inv, "amount": 20000})
	if x := get(t, boss, ws, "legal_matters", matter); num2(x["feeReceived"]) != 20000 || num2(x["outstanding"]) != 20500 {
		t.Fatalf("15: %v %v", x["feeReceived"], x["outstanding"])
	}

	// 16: closing needs the final order and no balance.
	closing := map[string]any{"status": "closed", "closureReason": "judgment", "outcome": "Award passed"}
	mustRefuse(t, "16: balance", edit(t, boss, ws, "legal_matters", matter, closing), "outstanding")
	create(t, boss, ws, "legal_payments", map[string]any{"name": "Balance", "matterId": matter, "invoiceId": inv, "amount": 18475, "tds": 2025})
	if x := get(t, boss, ws, "legal_invoices", inv); num2(x["balance"]) != 0 || x["status"] != "paid" {
		t.Fatalf("TDS is not outstanding: %v %v", x["balance"], x["status"])
	}
	for _, h := range list(boss, "court_hearings", "") {
		if x := h.(map[string]any); x["values"].(map[string]any)["status"] == "scheduled" {
			want(t, edit(t, boss, ws, "court_hearings", x["id"].(string), map[string]any{"status": "disposed"}), 200, "dispose")
		}
	}
	mustRefuse(t, "16: final order", edit(t, boss, ws, "legal_matters", matter, closing), "status")
	create(t, boss, ws, "legal_documents", map[string]any{"name": "Award", "matterId": matter, "category": "court_order", "status": "verified"})
	want(t, edit(t, boss, ws, "legal_matters", matter, closing), 200, "16: closes")
	if get(t, boss, ws, "opportunities", opp)["status"] != "closed_won" {
		t.Fatal("16: the deal is untouched")
	}

	// Retention and legal hold.
	if x := get(t, boss, ws, "legal_matters", matter); !strings.HasPrefix(fmt.Sprint(x["retentionUntil"]), time.Now().AddDate(8, 0, 0).Format("2006")) {
		t.Fatalf("closing sets the retention date: %v", x["retentionUntil"])
	}
	if r := call(t, "DELETE", base+"/crm/legal_matters/"+matter, boss, nil); r.Status != 422 {
		t.Fatalf("a file in retention can't be deleted: %d", r.Status)
	}
	held := create(t, boss, ws, "legal_matters", map[string]any{"name": "Held matter", "contactId": abc1, "caseKind": "other", "status": "new"})
	heldDoc := create(t, boss, ws, "legal_documents", map[string]any{"name": "Held evidence", "matterId": held, "category": "evidence"})
	heldFile := legalUpload(t, boss, base+"/crm/legal_documents/"+heldDoc+"/files")
	want(t, edit(t, boss, ws, "legal_matters", held, map[string]any{"legalHold": true}), 200, "hold")
	if r := call(t, "DELETE", base+"/files/"+heldFile, boss, nil); r.Status != 422 {
		t.Fatalf("legal hold keeps the attachment: %d", r.Status)
	}
	for _, d := range []string{"/crm/legal_matters/" + held, "/crm/legal_documents/" + heldDoc, "/crm/contacts/" + abc1} {
		if r := call(t, "DELETE", base+d, boss, nil); r.Status != 422 {
			t.Fatalf("legal hold blocks deleting %s: %d", d, r.Status)
		}
	}
	want(t, edit(t, boss, ws, "legal_matters", held, map[string]any{"legalHold": false}), 200, "lift the hold")
	want(t, call(t, "DELETE", base+"/crm/legal_documents/"+heldDoc, boss, nil), 204, "deletable once lifted")

	// Reminders: once each.
	soon := create(t, boss, ws, "legal_matters", map[string]any{"name": "Reminder matter", "contactId": abc1, "caseKind": "other", "status": "new"})
	create(t, boss, ws, "court_hearings", map[string]any{"name": "Week hearing", "matterId": soon, "hearingAt": time.Now().AddDate(0, 0, 7).UTC().Format(time.RFC3339), "status": "scheduled"})
	create(t, boss, ws, "court_hearings", map[string]any{"name": "Missed hearing", "matterId": soon, "hearingAt": time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339), "status": "scheduled"})
	create(t, boss, ws, "legal_deadlines", map[string]any{"name": "Filing due", "matterId": soon, "rule": "custom", "triggerDate": day(0), "dueDate": day(7)})
	crmMod.Sweep(context.Background())
	crmMod.Sweep(context.Background())
	notes := call(t, "GET", base+"/notifications?limit=200", boss, nil).Raw
	for _, w := range []string{"Hearing in 7 days", "Hearing not updated", "Deadline in 7 days"} {
		if strings.Count(notes, w) != 1 {
			t.Fatalf("reminder %q exactly once, got %d", w, strings.Count(notes, w))
		}
	}

	// Corporate invoice: RCM 0% GST.
	m2 := create(t, boss, ws, "legal_matters", map[string]any{"name": "ABC contract matter", "accountId": abc, "contactId": abc1, "caseKind": "contract", "feeAgreed": 100000, "status": "new"})
	corp := create(t, boss, ws, "legal_invoices", map[string]any{"name": "Corp", "matterId": m2, "grossFee": 100000})
	create(t, boss, ws, "legal_payments", map[string]any{"name": "Net", "matterId": m2, "invoiceId": corp, "amount": 90000, "tds": 10000})
	if x := get(t, boss, ws, "legal_invoices", corp); x["taxRegime"] != "rcm" || num2(x["gstAmount"]) != 0 || num2(x["balance"]) != 0 {
		t.Fatalf("RCM + TDS: %v %v %v", x["taxRegime"], x["gstAmount"], x["balance"])
	}

	// S.138: Day 16 rule; caveat never on a criminal matter.
	notice := day(-20)
	dl := create(t, boss, ws, "legal_deadlines", map[string]any{"name": "S.138 complaint", "matterId": m2, "rule": "ni138_complaint", "triggerDate": notice})
	if x := get(t, boss, ws, "legal_deadlines", dl); x["earliestDate"] != day(-4) {
		t.Fatalf("S.138 earliest is Day 16: %v", x["earliestDate"])
	}
	mustRefuse(t, "S.138 before Day 16", edit(t, boss, ws, "legal_deadlines", dl, map[string]any{"doneOn": day(-10)}), "doneOn")
	crim := create(t, boss, ws, "legal_matters", map[string]any{"name": "Bail matter", "contactId": abc1, "caseKind": "criminal", "status": "new"})
	mustRefuse(t, "no caveat on a criminal matter", call(t, "POST", base+"/crm/legal_deadlines", boss, map[string]any{"values": map[string]any{"name": "Caveat", "matterId": crim, "rule": "caveat", "triggerDate": day(0)}}), "rule")
}

// legalUpload attaches a small file to a record and returns its id.
func legalUpload(t *testing.T, token, url string) string {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, _ := w.CreateFormFile("file", "order.pdf")
	_, _ = part.Write([]byte("%PDF-1.4 test order"))
	_ = w.Close()
	req, _ := http.NewRequest("POST", baseURL+url, &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("X-Session-Transport", "bearer")
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		t.Fatalf("upload: %d %s", res.StatusCode, truncate(string(raw), 200))
	}
	var out struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &out)
	if out.ID == "" {
		t.Fatalf("upload gave no id: %s", truncate(string(raw), 200))
	}
	return out.ID
}
