package e2e

import (
	"strings"
	"testing"
)

// The owner's leads (D-180): their form is the sign-up form's — nothing from any trade — and
// converting one is approving it on a plan: the business is made from the lead, opens, and
// the lead becomes a contact.
func TestOwnerLeadIsApprovedOnAPlan(t *testing.T) {
	owner := ownerToken(t)
	meta := call(t, "GET", crmAPI+"/platform/crm/meta/leads", owner, nil)
	want(t, meta, 200, "owner lead form")
	for _, k := range []string{"taxId", "plan", "teamSize", "businessType", "organization", "mobile"} {
		if !strings.Contains(meta.Raw, `"key":"`+k+`"`) {
			t.Fatalf("the owner's lead form has %s: %s", k, truncate(meta.Raw, 300))
		}
	}
	for _, k := range []string{"vehicleType", "insuranceType", "legalServiceType", "annualRevenue"} {
		if strings.Contains(meta.Raw, `"key":"`+k+`"`) {
			t.Fatalf("the owner's lead form has no trade field %s", k)
		}
	}
	boss, ws := packBusiness(t, "Trade Form Motors", "Automotive")
	if m := call(t, "GET", crmAPI+"/w/"+ws+"/crm/meta/leads", boss, nil); strings.Contains(m.Raw, `"key":"taxId"`) {
		t.Fatal("a business's lead form never shows the owner's sign-up fields")
	}

	// Required: the business name, mobile, industry and tax ID.
	bad := call(t, "POST", crmAPI+"/platform/crm/leads", owner, map[string]any{"values": map[string]any{"lastName": "Only"}})
	if bad.Status != 422 || !strings.Contains(bad.Raw, "organization") || !strings.Contains(bad.Raw, "taxId") {
		t.Fatalf("the owner's lead needs the sign-up form's starred fields: %d %s", bad.Status, truncate(bad.Raw, 300))
	}
	r := call(t, "POST", crmAPI+"/platform/crm/leads", owner, map[string]any{"values": map[string]any{
		"firstName": "Meena", "lastName": "Raj", "mobile": "+919876501234", "email": "meena@example.com",
		"organization": "Meena Fashions", "industry": "Retail", "businessType": "Clothing", "teamSize": "2-10",
		"taxIdType": "pan", "taxId": "AAAPZ1234C", "plan": "3m", "source": "phone",
	}})
	if r.Status != 201 && r.Status != 200 {
		t.Fatalf("owner adds a lead: %d %s", r.Status, truncate(r.Raw, 400))
	}
	leadID := r.str("id")

	want(t, call(t, "POST", crmAPI+"/platform/leads/"+leadID+"/approve", owner, map[string]any{}), 422, "approving needs a plan")
	ok := call(t, "POST", crmAPI+"/platform/leads/"+leadID+"/approve", owner, map[string]any{"plan": "3m", "payment": "offline", "method": "cash"})
	want(t, ok, 200, "owner approves the lead on 3 months, paid in cash")
	want(t, call(t, "POST", crmAPI+"/platform/leads/"+leadID+"/approve", owner, map[string]any{"plan": "3m"}), 409, "approve twice")

	lead := call(t, "GET", crmAPI+"/platform/crm/leads/"+leadID, owner, nil)
	if lead.str("record", "values", "status") != "converted" || lead.str("record", "values", "convertedContactId") == "" {
		t.Fatalf("approving converts the lead into a contact: %s", truncate(lead.Raw, 500))
	}
	apps := call(t, "GET", crmAPI+"/platform/applications?leadId="+leadID, owner, nil)
	if len(apps.list("data")) != 1 || !strings.Contains(apps.Raw, `"status":"active"`) || !strings.Contains(apps.Raw, "Meena Fashions") {
		t.Fatalf("the lead's business is made and open: %s", truncate(apps.Raw, 400))
	}
	if !strings.Contains(apps.Raw, `"plan":"3m"`) || !strings.Contains(apps.Raw, "paidUntil") {
		t.Fatalf("the business is on the plan, paid: %s", truncate(apps.Raw, 400))
	}
	d := call(t, "GET", crmAPI+"/platform/crm/leads/"+leadID, owner, nil)
	if d.Status == 200 && (strings.Contains(d.Raw, `"object":"test_rides"`) || strings.Contains(d.Raw, `"object":"cards"`) || strings.Contains(d.Raw, `"object":"consents"`)) {
		t.Fatalf("the owner's lead shows no trade lists: %s", truncate(d.Raw, 300))
	}
}
