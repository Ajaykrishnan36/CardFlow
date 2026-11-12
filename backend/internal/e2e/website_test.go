package e2e

import (
	"strings"
	"testing"
	"time"
)

// The website (D-181): "Book a free call" and "Send an enquiry" save leads in the owner's
// Platform CRM, distinguishable by source and type, without a sign-in; the same visitor is
// not filed twice; the owner's dashboard counts them and the owner's lead form shows them.
func TestWebsiteFormsFileOwnerLeads(t *testing.T) {
	owner := ownerToken(t)
	api := crmAPI + "/public/enquiries"
	date := time.Now().AddDate(0, 0, 2).Format("2006-01-02")

	bad := call(t, "POST", api, "", map[string]any{"kind": "call", "name": "A", "phone": "12"})
	if bad.Status != 422 || !strings.Contains(bad.Raw, "preferredDate") || !strings.Contains(bad.Raw, "business") {
		t.Fatalf("a call request needs its fields: %d %s", bad.Status, truncate(bad.Raw, 300))
	}
	past := call(t, "POST", api, "", map[string]any{"kind": "call", "name": "Past Date", "business": "Past Co", "phone": "+919811100001",
		"email": "p@example.com", "industry": "Retail", "teamSize": "2-10", "preferredDate": "2020-01-01", "preferredTime": "morning"})
	want(t, past, 422, "a call can't be asked for a past date")

	c := call(t, "POST", api, "", map[string]any{"kind": "call", "name": "Call Visitor", "business": "Call Visitor Traders", "phone": "+919811100002",
		"email": "call@example.com", "industry": "Retail", "teamSize": "11-50", "currentMethod": "paper", "challenge": "Missed follow-ups",
		"preferredDate": date, "preferredTime": "afternoon", "message": "Call me"})
	want(t, c, 201, "visitor asks for a call")
	callCode := c.str("reference")
	again := call(t, "POST", api, "", map[string]any{"kind": "enquiry", "name": "Call Visitor", "business": "Call Visitor Traders", "phone": "+919811100002",
		"subject": "Price for ten", "category": "pricing", "message": "What does it cost for ten people?"})
	want(t, again, 201, "the same visitor sends an enquiry")
	if again.str("reference") != callCode {
		t.Fatalf("the same visitor is not filed twice: %s then %s", callCode, again.str("reference"))
	}
	e := call(t, "POST", api, "", map[string]any{"kind": "enquiry", "name": "Enquiry Visitor", "business": "Enquiry Visitor Co", "phone": "+919811100003",
		"email": "enq@example.com", "industry": "Education", "subject": "School demo", "category": "demo", "message": "Admissions follow-ups"})
	want(t, e, 201, "visitor sends an enquiry")
	bot := call(t, "POST", api, "", map[string]any{"kind": "enquiry", "name": "Robot", "business": "Robot", "phone": "+919811100004",
		"subject": "spam", "category": "other", "message": "spam spam", "website": "http://spam.example"})
	want(t, bot, 200, "a robot is thanked")

	// The owner sees them: the lists, with their source and type.
	calls := call(t, "GET", crmAPI+`/platform/crm/leads?filter={"op":"and","filters":[{"field":"requestType","op":"in","value":["call"]}]}`, owner, nil)
	want(t, calls, 200, "owner lists call requests")
	if !strings.Contains(calls.Raw, "Call Visitor Traders") || strings.Contains(calls.Raw, "Enquiry Visitor Co") {
		t.Fatalf("call requests list only call requests: %s", truncate(calls.Raw, 500))
	}
	enqs := call(t, "GET", crmAPI+`/platform/crm/leads?filter={"op":"and","filters":[{"field":"requestType","op":"in","value":["enquiry"]}]}`, owner, nil)
	if !strings.Contains(enqs.Raw, "Enquiry Visitor Co") || strings.Contains(enqs.Raw, "Robot") {
		t.Fatalf("website enquiries list the enquiries, and no robot: %s", truncate(enqs.Raw, 500))
	}
	if !strings.Contains(calls.Raw, "website_call") || !strings.Contains(enqs.Raw, "website_enquiry") {
		t.Fatalf("each form keeps its own source: %s / %s", truncate(calls.Raw, 300), truncate(enqs.Raw, 300))
	}
	meta := call(t, "GET", crmAPI+"/platform/crm/meta/leads", owner, nil)
	for _, k := range []string{"CRM Website - Book Free Call", "CRM Website - General Enquiry", `"key":"preferredDate"`, `"key":"callStatus"`, `"key":"enquiryCategory"`} {
		if !strings.Contains(meta.Raw, k) {
			t.Fatalf("the owner's lead form knows %s", k)
		}
	}

	// The dashboard counts them.
	dash := call(t, "GET", crmAPI+"/platform/dashboard", owner, nil)
	want(t, dash, 200, "owner dashboard")
	got := map[string]float64{}
	for _, k := range dash.list("kpis") {
		m := k.(map[string]any)
		got[m["key"].(string)] = m["value"].(float64)
	}
	if got["calls"] < 1 || got["enquiries"] < 1 {
		t.Fatalf("the dashboard counts call requests and enquiries: %v", got)
	}
	// Nobody signed in sees anything private.
	want(t, call(t, "GET", crmAPI+"/platform/crm/leads", "", nil), 401, "leads stay private")
}
