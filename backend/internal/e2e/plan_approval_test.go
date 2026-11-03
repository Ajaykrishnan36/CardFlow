package e2e

import (
	"context"
	"testing"
	"time"

	"cardflow-backend/internal/crm/billing"
)

// A plan on the application (D-172): the applicant picks a plan and pays nothing; the owner
// sees it, then approves it as paid offline (the Super Admin opens the business with no
// payment lock and the payment is on the owner's list) or as paid online later (the plan is
// what their payment screen starts on).
func TestApproveOnAPlan(t *testing.T) {
	setReview(t, true)
	defer setReview(t, false)
	ctx := context.Background()
	b := crmMod.Billing()
	b.UseConfig(billing.Config{KeyID: "rzp_test_e2e", KeySecret: "e2e-secret"})
	defer b.UseConfig(billing.Config{})
	owner := ownerToken(t)

	want(t, call(t, "GET", crmAPI+"/public/billing/plans", "", nil), 200, "the plans on the sign-up form")

	// Paid offline.
	phone := freshPhone()
	reg := register(t, phone)
	want(t, call(t, "POST", crmAPI+"/public/register/business", "", map[string]any{"registrationToken": reg.str("registrationToken"), "mode": "first",
		"name": "Offline Plan Motors", "displayName": "Mani", "teamSize": "2-10", "industry": "Automotive", "businessType": "Bike", "plan": "3m", "taxIdType": "pan", "taxId": testPAN()}), 202, "apply on 3 months")
	slug := slugOf(t, phone, "Offline Plan Motors")
	var app map[string]any
	for _, x := range call(t, "GET", crmAPI+"/platform/applications?status=pending", owner, nil).list("data") {
		if m := x.(map[string]any); m["code"] == slug {
			app = m
		}
	}
	if app == nil || app["plan"] != "3m" || app["paidUntil"] != nil {
		t.Fatalf("the owner sees the plan asked for, not paid: %v", app)
	}
	ws := workspaceID(t, slug)
	want(t, call(t, "POST", crmAPI+"/platform/applications/"+ws+"/approve", owner, map[string]any{"plan": "3m", "payment": "offline"}), 422, "offline without how they paid")
	r := call(t, "POST", crmAPI+"/platform/applications/"+ws+"/approve", owner, map[string]any{"plan": "3m", "payment": "offline", "method": "upi", "reference": "UPI-778812"})
	want(t, r, 200, "approve as paid offline")
	if u, err := time.Parse(time.RFC3339, r.str("application", "paidUntil")); err != nil || time.Until(u) < 85*24*time.Hour {
		t.Fatalf("the applicant is paid for 3 months: %s", truncate(r.Raw, 400))
	}
	tok := signInAt(t, phone, slug).str("token")
	want(t, call(t, "GET", crmAPI+"/w/"+slug+"/crm/leads", tok, nil), 200, "no payment lock after an offline payment")
	var method, ref string
	if err := testDB.Pool.QueryRow(ctx, `SELECT method, reference FROM crm.billing_orders WHERE workspace_id = $1 AND status = 'paid'`, ws).Scan(&method, &ref); err != nil || method != "upi" || ref != "UPI-778812" {
		t.Fatalf("the offline payment is on the owner's list: %v %s %s", err, method, ref)
	}

	// Online: approved on 6 months, the payment screen starts there.
	phone2 := freshPhone()
	reg2 := register(t, phone2)
	want(t, call(t, "POST", crmAPI+"/public/register/business", "", map[string]any{"registrationToken": reg2.str("registrationToken"), "mode": "first",
		"name": "Online Plan Motors", "displayName": "Kavya", "teamSize": "2-10", "industry": "Automotive", "businessType": "Bike", "plan": "1m", "taxIdType": "pan", "taxId": testPAN()}), 202, "apply on 1 month")
	slug2 := slugOf(t, phone2, "Online Plan Motors")
	want(t, call(t, "POST", crmAPI+"/platform/applications/"+workspaceID(t, slug2)+"/approve", owner, map[string]any{"plan": "6m", "payment": "online"}), 200, "approve to pay online")
	tok2 := signInAt(t, phone2, slug2).str("token")
	if x := call(t, "GET", crmAPI+"/w/"+slug2+"/crm/leads", tok2, nil); x.Status != 402 {
		t.Fatalf("pay-online: locked until they pay: %d", x.Status)
	}
	if ov := call(t, "GET", crmAPI+"/w/"+slug2+"/billing", tok2, nil); ov.str("suggestedPeriod") != "6m" {
		t.Fatalf("the payment screen starts on the approved plan: %s", truncate(ov.Raw, 300))
	}
}
