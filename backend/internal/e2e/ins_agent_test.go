package e2e

import (
	"testing"
	"time"
)

// Insurance Agent catalogue QA run (D-183): a converted lead's deal starts from the expected
// premium, issuing the policy wins the deal on the issue date (B14), and a renewal takes the
// rate card's rate for the next policy year (J9), not the first year's.
func TestInsuranceAgentDealWonAndYearTwoRate(t *testing.T) {
	boss, ws := packBusiness(t, "Year Rate Insurance", "Insurance")
	today := time.Now().Format("2006-01-02")
	insurer := create(t, boss, ws, "accounts", map[string]any{"name": "Life Insurer", "kind": "business", "type": "partner"})
	create(t, boss, ws, "commission_rules", map[string]any{"insurerId": insurer, "policyClass": "life", "yearFrom": 1, "yearTo": 1, "rate": 25})
	create(t, boss, ws, "commission_rules", map[string]any{"insurerId": insurer, "policyClass": "life", "yearFrom": 2, "yearTo": 99, "rate": 7.5})

	lead := create(t, boss, ws, "leads", map[string]any{"lastName": "Prakash", "mobile": "+91 98111 00001", "insuranceType": "life", "expectedPremium": 12000, "status": "qualified"})
	cv := call(t, "POST", crmAPI+"/w/"+ws+"/crm/leads/"+lead+"/convert", boss, map[string]any{"account": map[string]any{"mode": "new", "name": "Prakash household", "kind": "individual"},
		"createContact": true, "opportunity": map[string]any{"create": true}})
	want(t, cv, 200, "convert")
	opp := get(t, boss, ws, "opportunities", cv.str("opportunityId"))
	if num2(opp["amount"]) != 12000 || opp["name"] != "Prakash — Life cover" {
		t.Fatalf("the deal starts from the expected premium and the cover type: %v %v", opp["amount"], opp["name"])
	}

	pol := create(t, boss, ws, "insurance_policies", map[string]any{"contactId": cv.str("contactId"), "insurerId": insurer, "opportunityId": cv.str("opportunityId"),
		"policyClass": "life", "premium": 12000, "status": "proposal"})
	want(t, edit(t, boss, ws, "insurance_policies", pol, map[string]any{"status": "in_force", "policyNumber": "YR-1", "startDate": today}), 200, "issue")
	if p := get(t, boss, ws, "insurance_policies", pol); num2(p["commissionRate"]) != 25 {
		t.Fatalf("year 1 takes 25%%, got %v", p["commissionRate"])
	}
	if o := get(t, boss, ws, "opportunities", cv.str("opportunityId")); o["status"] != "closed_won" || o["closeDate"] != today {
		t.Fatalf("issuing the policy wins the deal today: %v %v", o["status"], o["closeDate"])
	}

	want(t, edit(t, boss, ws, "insurance_policies", pol, map[string]any{"status": "renewed"}), 200, "renew")
	list := call(t, "GET", crmAPI+"/w/"+ws+"/crm/insurance_policies?limit=10", boss, nil).list("data")
	for _, x := range list {
		v := x.(map[string]any)["values"].(map[string]any)
		if v["previousPolicyId"] == pol {
			if num2(v["commissionRate"]) != 7.5 || num2(v["commissionAmount"]) != 900 {
				t.Fatalf("year 2 takes the year-2 rate 7.5%% (₹900): %v %v", v["commissionRate"], v["commissionAmount"])
			}
			return
		}
	}
	t.Fatal("the renewal term was not created")
}

// A new insurance business's sales team doesn't see commission (F13), yet can quote with any
// insurer; the CRM still works out the commission the agent sees. Remittance is refused when
// the customer pays the insurer, a task's unchanged link isn't re-checked, and the dashboard
// shows commission received, due and clawed back (C20).
func TestInsuranceTeamCommissionAndRemittance(t *testing.T) {
	boss, ws := packBusiness(t, "Team Rate Insurance", "Insurance")
	base := crmAPI + "/w/" + ws
	today := time.Now().Format("2006-01-02")
	pospPhone := freshPhone()
	want(t, call(t, "POST", base+"/admin/members", boss, map[string]any{"displayName": "Pos P", "phone": pospPhone, "roleKey": "SALES_STAFF"}), 201, "add a POSP")
	posp := signIn(t, pospPhone, "Pos P")

	insurer := create(t, boss, ws, "accounts", map[string]any{"name": "Health Insurer", "kind": "business", "type": "partner"})
	create(t, boss, ws, "commission_rules", map[string]any{"insurerId": insurer, "policyClass": "health", "rate": 15})
	if r := call(t, "GET", base+"/crm/commissions", posp, nil); r.Status != 403 {
		t.Fatalf("a new insurance business's sales staff don't see commission: %d", r.Status)
	}
	cust := create(t, posp, ws, "contacts", map[string]any{"lastName": "Posp Customer", "mobile": "+91 98111 55555"})
	pol := create(t, posp, ws, "insurance_policies", map[string]any{"contactId": cust, "insurerId": insurer, "policyClass": "health", "premium": 20000, "collectionMode": "direct", "status": "proposal"})
	if p := get(t, posp, ws, "insurance_policies", pol); p["commissionAmount"] != nil {
		t.Fatalf("the POSP doesn't see the commission: %v", p["commissionAmount"])
	}
	if p := get(t, boss, ws, "insurance_policies", pol); num2(p["commissionAmount"]) != 3000 {
		t.Fatalf("the agent sees 15%% = 3000: %v", p["commissionAmount"])
	}
	want(t, edit(t, boss, ws, "insurance_policies", pol, map[string]any{"status": "in_force", "policyNumber": "TR-1", "startDate": today}), 200, "issue")
	dues := call(t, "GET", base+"/crm/premium_dues?limit=5", boss, nil).list("data")
	due := dues[0].(map[string]any)["id"].(string)
	want(t, edit(t, boss, ws, "premium_dues", due, map[string]any{"status": "paid"}), 200, "paid")
	mustRefuse(t, "remitting a premium the customer paid to the insurer", edit(t, boss, ws, "premium_dues", due, map[string]any{"remittedOn": today}), "remittedOn")

	comm := call(t, "GET", base+"/crm/commissions?limit=5", boss, nil).list("data")[0].(map[string]any)["id"].(string)
	want(t, edit(t, boss, ws, "commissions", comm, map[string]any{"receivedAmount": 2000}), 200, "part of the commission arrives")
	s := call(t, "GET", base+"/dashboard/summary?range=today", boss, nil)
	m := s.Body["metrics"].(map[string]any)
	if num2(m["commissionReceived"]) != 2000 || num2(m["commissionReceivable"]) != 1000 {
		t.Fatalf("dashboard: received 2000, still due 1000 — got %v %v", m["commissionReceived"], m["commissionReceivable"])
	}

	// A task the agent puts on their own lead for the POSP can be completed by the POSP.
	lead := create(t, boss, ws, "leads", map[string]any{"lastName": "Agent Lead"})
	me := call(t, "GET", crmAPI+"/me", posp, nil).str("identity", "id")
	task := create(t, boss, ws, "tasks", map[string]any{"name": "Call the agent's lead", "leadId": lead, "ownerId": me})
	want(t, edit(t, posp, ws, "tasks", task, map[string]any{"status": "completed", "leadId": lead}), 200, "complete with the link sent back unchanged")
	mustRefuse(t, "linking a new task to the agent's lead", call(t, "POST", base+"/crm/tasks", posp, map[string]any{"values": map[string]any{"name": "x", "leadId": lead}}), "leadId")
}
