package e2e

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// A form on the business's website becomes a lead, with the customer's consent on record (D-146).
func TestWebFormMakesLeadsAndConsent(t *testing.T) {
	boss := signIn(t, freshPhone(), "Boss")
	ws := newBusiness(t, boss, "Form Co")
	base := crmAPI + "/w/" + ws
	staff, staffID := staffOf(t, boss, ws, "Lead Desk")

	want(t, call(t, "GET", base+"/web-forms", staff, nil), 403, "staff managing web forms")
	want(t, call(t, "POST", base+"/web-forms", boss, map[string]any{"name": ""}), 422, "a form with no name")
	want(t, call(t, "POST", base+"/web-forms", boss, map[string]any{"name": "X", "fields": []string{"ownerId"}}), 422, "a field that isn't offered")
	made := call(t, "POST", base+"/web-forms", boss, map[string]any{"name": "Website enquiry", "heading": "Book a test ride", "fields": []string{"email", "city", "interest", "message"},
		"ownerId": staffID, "consentText": "I agree to be contacted about my enquiry.", "thankYou": "Thanks! Our team will call you today."})
	want(t, made, 200, "create the form")
	form := made.list("data")[0].(map[string]any)
	link := form["url"].(string)
	if !strings.Contains(link, "/public/forms/wf_") || !strings.Contains(form["embed"].(string), "<iframe") {
		t.Fatalf("a form has a link and an embed snippet: %v", form)
	}
	path := link[strings.Index(link, "/api/crm/v1"):]
	send := func(v url.Values) (int, string) {
		t.Helper()
		res, err := http.Post(baseURL+path, "application/x-www-form-urlencoded", strings.NewReader(v.Encode()))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}

	// The page anyone can open: the business's heading, the ticked fields, the consent box.
	res, err := http.Get(baseURL + path)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(res.Body)
	res.Body.Close()
	for _, s := range []string{"Book a test ride", `name="phone"`, `name="city"`, `name="consent"`, "Form Co"} {
		if !strings.Contains(string(page), s) {
			t.Fatalf("the public form shows %q", s)
		}
	}
	if strings.Contains(string(page), `name="company"`) {
		t.Fatal("a field the business didn't tick isn't asked for")
	}

	// What must be there.
	if code, body := send(url.Values{"name": {"Arun Kumar"}, "phone": {"98"}}); code != 422 || !strings.Contains(body, "mobile number") {
		t.Fatalf("a submission with no usable number: %d", code)
	}
	if code, body := send(url.Values{"name": {"Arun Kumar"}, "phone": {"+91 98401 55667"}}); code != 422 || !strings.Contains(body, "tick the box") {
		t.Fatalf("a submission without consent: %d", code)
	}
	// A bot that fills the hidden field is thanked and ignored.
	if code, _ := send(url.Values{"name": {"Bot"}, "phone": {"9999999999"}, "consent": {"yes"}, "website_url": {"http://spam"}}); code != 200 {
		t.Fatalf("the hidden-field submission: %d", code)
	}
	if n := total(t, boss, ws, "leads", ""); n != 0 {
		t.Fatalf("a bot's submission made %d leads", n)
	}
	// A real one. The visitor tries to choose the owner and a field that isn't on the form: both ignored.
	code, body := send(url.Values{"name": {"Arun Kumar"}, "phone": {"+91 98401 55667"}, "email": {"arun@example.com"}, "city": {"Coimbatore"},
		"interest": {"Activa 6G"}, "message": {"Evening test ride please"}, "consent": {"yes"}, "ownerId": {"00000000-0000-0000-0000-000000000001"}, "company": {"Sneaky Ltd"}})
	if code != 200 || !strings.Contains(body, "Our team will call you today") {
		t.Fatalf("a good submission: %d %s", code, truncate(body, 200))
	}
	leads := call(t, "GET", base+"/crm/leads", staff, nil) // the form's owner sees it as their own
	if l := leads.list("data"); len(l) != 1 {
		t.Fatalf("the lead desk gets the lead: %s", truncate(leads.Raw, 300))
	}
	lead := leads.list("data")[0].(map[string]any)
	v := lead["values"].(map[string]any)
	if v["firstName"] != "Arun" || v["lastName"] != "Kumar" || v["source"] != "web" || v["city"] != "Coimbatore" || v["ownerId"] != staffID || v["company"] != nil {
		t.Fatalf("the lead carries what the form allows and nothing else: %v", v)
	}
	if d, _ := v["description"].(string); !strings.Contains(d, "Activa 6G") || !strings.Contains(d, "Evening test ride") {
		t.Fatalf("interest and message are kept: %q", d)
	}
	cons := call(t, "GET", base+"/crm/consents", boss, nil)
	if l := cons.list("data"); len(l) != 1 {
		t.Fatalf("the consent is on record: %s", truncate(cons.Raw, 300))
	}
	c := cons.list("data")[0].(map[string]any)
	cv := c["values"].(map[string]any)
	if cv["status"] != "given" || cv["leadId"] != lead["id"] || cv["noticeText"] != "I agree to be contacted about my enquiry." || cv["givenOn"] == nil {
		t.Fatalf("the consent says who, what wording and when: %v", cv)
	}
	// The same mobile again: no second lead, the enquiry is added to the first.
	if code, _ := send(url.Values{"name": {"Arun K"}, "phone": {"9840155667"}, "message": {"Is the blue one in stock?"}, "consent": {"yes"}}); code != 200 {
		t.Fatalf("the second enquiry: %d", code)
	}
	if n := total(t, boss, ws, "leads", ""); n != 1 {
		t.Fatalf("a returning enquirer made a duplicate lead: %d", n)
	}
	if tl := call(t, "GET", base+"/crm/leads/"+lead["id"].(string)+"/timeline", boss, nil); !strings.Contains(tl.Raw, "Enquired again") {
		t.Fatalf("the return visit shows on the lead: %s", truncate(tl.Raw, 300))
	}
	// A site that posts JSON gets JSON back.
	js := call(t, "POST", path[len(""):], "", map[string]any{"name": "Divya R", "phone": "+91 90030 11223", "consent": "yes"})
	if js.Status != 201 || js.Body["ok"] != true {
		t.Fatalf("a JSON submission: %d %s", js.Status, truncate(js.Raw, 200))
	}
	if n := form["submissions"]; n == nil {
		t.Fatal("submissions are counted")
	}
	if l := call(t, "GET", base+"/web-forms", boss, nil).list("data"); num2(l[0].(map[string]any)["submissions"]) != 3 {
		t.Fatalf("three submissions were taken: %v", l[0].(map[string]any)["submissions"])
	}

	// Consent rules: withdrawn is dated and final; the wording can't be rewritten.
	cid := c["id"].(string)
	refusedOn := func(r resp, field string) {
		t.Helper()
		if r.Status != 422 || !strings.Contains(r.Raw, field) {
			t.Fatalf("want refused on %s: %d %s", field, r.Status, truncate(r.Raw, 200))
		}
	}
	refusedOn(edit(t, boss, ws, "consents", cid, map[string]any{"noticeText": "I agree to everything."}), "noticeText")
	want(t, edit(t, boss, ws, "consents", cid, map[string]any{"status": "withdrawn"}), 200, "withdraw")
	if get(t, boss, ws, "consents", cid)["withdrawnOn"] == nil {
		t.Fatal("a withdrawal is dated")
	}
	refusedOn(edit(t, boss, ws, "consents", cid, map[string]any{"status": "given"}), "status")
	refusedOn(call(t, "POST", base+"/crm/consents", boss, map[string]any{"values": map[string]any{"name": "Loose consent", "status": "given"}}), "contactId")

	// Switched off, the form stops taking submissions.
	want(t, call(t, "PATCH", base+"/web-forms/"+form["id"].(string), boss, map[string]any{"active": false}), 200, "switch the form off")
	if code, _ := send(url.Values{"name": {"Late"}, "phone": {"9000012345"}, "consent": {"yes"}}); code != 404 {
		t.Fatalf("a switched-off form: %d", code)
	}
}
