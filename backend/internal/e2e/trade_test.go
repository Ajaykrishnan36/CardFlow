package e2e

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// One CRM for every trade (D-174): the industry and business type are checked against the
// list, they decide the pack, the lead fields and a few words, and the rest is the same CRM.
func TestIndustryAndBusinessType(t *testing.T) {
	// The list on the sign-up form.
	list := call(t, "GET", crmAPI+"/public/industries", "", nil)
	want(t, list, 200, "the industries")
	for _, must := range []string{`"Legal services"`, `"Law firm"`, `"Advocate"`, `"Both bike `, `"Insurance agent"`, `"Other"`} {
		if !strings.Contains(list.Raw, must) {
			t.Fatalf("the list has %s: %s", must, truncate(list.Raw, 300))
		}
	}

	// The sign-up form must say which trade.
	reg := register(t, freshPhone())
	tok := reg.str("registrationToken")
	apply := func(extra map[string]any) resp {
		body := map[string]any{"registrationToken": tok, "mode": "first", "name": "Trade Check", "displayName": "Kavin", "teamSize": "2-10", "taxIdType": "pan", "taxId": testPAN()}
		for k, v := range extra {
			body[k] = v
		}
		return call(t, "POST", crmAPI+"/public/register/business", "", body)
	}
	mustRefuse(t, "no industry", apply(nil), "industry")
	mustRefuse(t, "an industry not on the list", apply(map[string]any{"industry": "Space travel"}), "industry")
	mustRefuse(t, "a law firm without its type", apply(map[string]any{"industry": "Legal services"}), "businessType")
	mustRefuse(t, "a type from another industry", apply(map[string]any{"industry": "Legal services", "businessType": "Bike"}), "businessType")
	mustRefuse(t, "Other without the trade", apply(map[string]any{"industry": "Other"}), "industryOther")
	mustRefuse(t, "type Other without the words", apply(map[string]any{"industry": "Retail", "businessType": "Other"}), "businessTypeOther")

	// Inside: a law firm.
	boss := signIn(t, freshPhone(), "Advocate Meena")
	made := call(t, "POST", crmAPI+"/businesses", boss, map[string]any{"name": "Meena & Associates", "mode": "first", "industry": "legal services", "businessType": "law firm"})
	want(t, made, 201, "a law firm")
	ws := made.str("business", "code")
	base := crmAPI + "/w/" + ws
	biz := call(t, "GET", base+"/business", boss, nil)
	if biz.str("profile", "industry") != "Legal services" || biz.str("profile", "businessType") != "Law firm" {
		t.Fatalf("stored as the list writes them: %s", truncate(biz.Raw, 400))
	}
	ctx := call(t, "GET", base+"/context", boss, nil)
	if nav := navText(ctx); !strings.Contains(nav, `"/crm/w/`+ws+`/legal_matters"`) || !strings.Contains(nav, `"label":"Clients"`) || strings.Contains(nav, "vehicle_stock") {
		t.Fatalf("a law firm gets matters, calls contacts Clients and has no vehicles: %s", truncate(nav, 900))
	}
	if ctx.str("workspace", "trade", "industryKey") != "legal" || ctx.str("workspace", "terms", "contacts", "plural") != "Clients" {
		t.Fatalf("the trade and its words are in the context: %s", truncate(ctx.Raw, 300))
	}
	leadMeta := call(t, "GET", base+"/crm/meta/leads", boss, nil).Raw
	if !strings.Contains(leadMeta, `"matterCategory"`) || strings.Contains(leadMeta, `"preferredItemId"`) || strings.Contains(leadMeta, `"insuranceType"`) {
		t.Fatal("a law firm's lead asks about the matter, not about a bike or a policy")
	}
	assetMeta := call(t, "GET", base+"/crm/meta/assets", boss, nil).Raw
	if strings.Contains(assetMeta, `"vin"`) || strings.Contains(assetMeta, `"odometer"`) {
		t.Fatal("no VIN or odometer on a law firm's assets")
	}
	if m := call(t, "GET", base+"/crm/meta/contacts", boss, nil); m.str("labelPlural") != "Clients" {
		t.Fatalf("contacts are Clients: %s", m.str("labelPlural"))
	}
	if m := call(t, "GET", base+"/crm/meta/cases", boss, nil); m.str("labelPlural") != "Support tickets" {
		t.Fatalf("a support case is not a legal matter: %s", m.str("labelPlural"))
	}

	// A matter and its hearings.
	client := create(t, boss, ws, "contacts", map[string]any{"firstName": "Ravi", "lastName": "Shankar", "mobile": "9840011223"})
	lead := create(t, boss, ws, "leads", map[string]any{"lastName": "Prakash", "legalServiceType": "litigation", "matterCategory": "property"})
	if l := get(t, boss, ws, "leads", lead); l["matterCategory"] != "property" {
		t.Fatalf("the lead keeps the matter category: %v", l)
	}
	matter := create(t, boss, ws, "legal_matters", map[string]any{"contactId": client, "matterCategory": "property", "courtCaseNumber": "OS 412/2026", "courtName": "City Civil Court, Chennai"})
	m := get(t, boss, ws, "legal_matters", matter)
	if m["status"] != "new" || !strings.Contains(asText(m["name"]), "Ravi Shankar") || !strings.Contains(asText(m["name"]), "OS 412/2026") {
		t.Fatalf("a new matter names itself from the client and case number: %v", m)
	}
	when := time.Now().Add(20 * time.Hour).UTC().Truncate(time.Minute)
	h1 := create(t, boss, ws, "court_hearings", map[string]any{"matterId": matter, "hearingAt": when.Format(time.RFC3339), "purpose": "evidence"})
	hr := get(t, boss, ws, "court_hearings", h1)
	if hr["contactId"] != client {
		t.Fatalf("the hearing's client comes from the matter: %v", hr)
	}
	m = get(t, boss, ws, "legal_matters", matter)
	if m["status"] != "active" || m["nextHearingAt"] == nil {
		t.Fatalf("a hearing makes the matter active and shows the next date: %v", m)
	}
	other := create(t, boss, ws, "contacts", map[string]any{"firstName": "Someone", "lastName": "Else"})
	mustRefuse(t, "a hearing for another client", call(t, "POST", base+"/crm/court_hearings", boss, map[string]any{"values": map[string]any{"matterId": matter, "contactId": other, "hearingAt": when.Format(time.RFC3339)}}), "contactId")
	mustRefuse(t, "adjourned without the next date", edit(t, boss, ws, "court_hearings", h1, map[string]any{"status": "adjourned"}), "nextHearingAt")
	next := when.Add(21 * 24 * time.Hour)
	want(t, edit(t, boss, ws, "court_hearings", h1, map[string]any{"status": "adjourned", "outcome": "Witness absent", "nextHearingAt": next.Format(time.RFC3339)}), 200, "adjourned to a date")
	if n := total(t, boss, ws, "court_hearings", ""); n != 2 {
		t.Fatalf("the next date is booked as a hearing: %d", n)
	}
	m = get(t, boss, ws, "legal_matters", matter)
	if at, err := time.Parse(time.RFC3339, asText(m["nextHearingAt"])); err != nil || !at.Equal(next) {
		t.Fatalf("the matter's next hearing moves to the new date: %v", m["nextHearingAt"])
	}
	want(t, edit(t, boss, ws, "legal_matters", matter, map[string]any{"status": "settled", "outcome": "Compromise memo filed"}), 200, "settled")
	if m = get(t, boss, ws, "legal_matters", matter); m["closedOn"] == nil {
		t.Fatalf("a settled matter has its closing date: %v", m)
	}
	mustRefuse(t, "a hearing on a closed matter", call(t, "POST", base+"/crm/court_hearings", boss, map[string]any{"values": map[string]any{"matterId": matter, "hearingAt": next.Add(48 * time.Hour).Format(time.RFC3339)}}), "matterId")
	off := call(t, "GET", base+"/dashboard/office", boss, nil)
	if !strings.Contains(off.Raw, `"activeMatters"`) || !strings.Contains(off.Raw, `"hearingsWeek"`) || strings.Contains(off.Raw, `"testRides"`) {
		t.Fatalf("a law firm's dashboard counts matters and hearings: %s", truncate(off.Raw, 500))
	}

	// A car dealer: test drives and car fields; a bike dealer keeps test rides and no car fields.
	dealer := func(name, typ string) (string, string) {
		who := signIn(t, freshPhone(), "Owner")
		r := call(t, "POST", crmAPI+"/businesses", who, map[string]any{"name": name, "industry": "Automotive", "businessType": typ})
		want(t, r, 201, "a "+typ+" dealer")
		return who, r.str("business", "code")
	}
	carBoss, carWS := dealer("Arun Cars", "Car")
	if c := navText(call(t, "GET", crmAPI+"/w/"+carWS+"/context", carBoss, nil)); !strings.Contains(c, `"label":"Test drives"`) || strings.Contains(c, "legal_matters") {
		t.Fatalf("a car dealer books test drives: %s", truncate(c, 500))
	}
	carLead := call(t, "GET", crmAPI+"/w/"+carWS+"/crm/meta/leads", carBoss, nil).Raw
	if !strings.Contains(carLead, `"transmission"`) || !strings.Contains(carLead, `"preferredItemId"`) || strings.Contains(carLead, `"matterCategory"`) {
		t.Fatal("a car dealer's lead asks for the model and the transmission")
	}
	if c := call(t, "GET", crmAPI+"/w/"+carWS+"/crm/meta/catalog_entries", carBoss, nil).Raw; !strings.Contains(c, `"carBody"`) || strings.Contains(c, `"bikeCategory"`) {
		t.Fatal("a car's catalog entry has the body type, not a bike category")
	}
	bikeBoss, bikeWS := dealer("Kumar Bikes", "Bike")
	if c := navText(call(t, "GET", crmAPI+"/w/"+bikeWS+"/context", bikeBoss, nil)); !strings.Contains(c, `"label":"Test rides"`) {
		t.Fatalf("a bike dealer books test rides: %s", truncate(c, 400))
	}
	if c := call(t, "GET", crmAPI+"/w/"+bikeWS+"/crm/meta/catalog_entries", bikeBoss, nil).Raw; strings.Contains(c, `"carBody"`) || !strings.Contains(c, `"bikeCategory"`) {
		t.Fatal("a bike's catalog entry has a bike category, not a car body")
	}
	if c := call(t, "GET", crmAPI+"/w/"+bikeWS+"/crm/meta/leads", bikeBoss, nil).Raw; strings.Contains(c, `"transmission"`) {
		t.Fatal("no transmission on a bike dealer's lead")
	}

	// The trade can change later, on Business profile; it is checked the same way.
	mustRefuse(t, "a type from another industry", call(t, "PATCH", crmAPI+"/w/"+bikeWS+"/business", bikeBoss, map[string]any{"profile": map[string]any{"businessType": "Law firm"}}), "profile.businessType")
	want(t, call(t, "PATCH", crmAPI+"/w/"+bikeWS+"/business", bikeBoss, map[string]any{"profile": map[string]any{"industry": "Insurance", "businessType": "Insurance agent"}}), 200, "now an insurance agent")
	if c := navText(call(t, "GET", crmAPI+"/w/"+bikeWS+"/context", bikeBoss, nil)); !strings.Contains(c, "insurance_policies") || strings.Contains(c, "vehicle_stock") {
		t.Fatalf("an insurance agent gets policies and no vehicles: %s", truncate(c, 500))
	}
}

// navText is the sidebar of a context as JSON.
func navText(r resp) string {
	b, _ := json.Marshal(r.at("navigation"))
	return string(b)
}

func asText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
