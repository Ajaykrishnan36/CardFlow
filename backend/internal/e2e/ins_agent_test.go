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
