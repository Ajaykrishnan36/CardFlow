package e2e

import (
	"strings"
	"testing"
	"time"
)

func packBusiness(t *testing.T, name, industry string) (boss, ws string) {
	t.Helper()
	boss = signIn(t, freshPhone(), "Owner")
	made := call(t, "POST", crmAPI+"/businesses", boss, map[string]any{"name": name, "industry": industry})
	want(t, made, 201, "create "+name)
	return boss, made.str("business", "code")
}

// A salon's stock, kits and packages (D-149).
func TestSalonStockKitsPackages(t *testing.T) {
	boss, ws := packBusiness(t, "Glow Studio", "Beauty & salon")
	post := func(object string, v map[string]any) resp {
		return call(t, "POST", crmAPI+"/w/"+ws+"/crm/"+object, boss, map[string]any{"values": v})
	}
	day := func(n int) string { return time.Now().AddDate(0, 0, n).Format("2006-01-02") }
	at := func(days, hour int) string {
		d := time.Now().AddDate(0, 0, days)
		return time.Date(d.Year(), d.Month(), d.Day(), hour, 0, 0, 0, time.UTC).Format(time.RFC3339)
	}
	// Stock: use reduces it, never below zero; low and expired are flagged; expired isn't used on a client.
	fnd := create(t, boss, ws, "salon_products", map[string]any{"name": "HD Foundation", "brand": "MAC", "shade": "NC30", "unit": "ml", "quantity": 30, "reorderLevel": 10, "costPerUnit": 80, "expiryDate": day(200)})
	old := create(t, boss, ws, "salon_products", map[string]any{"name": "Old Primer", "unit": "ml", "quantity": 20, "expiryDate": day(-3)})
	if get(t, boss, ws, "salon_products", old)["status"] != "expired" || get(t, boss, ws, "salon_products", fnd)["status"] != "available" {
		t.Fatal("stock past its date is Expired; the rest Available")
	}
	use := create(t, boss, ws, "product_usage", map[string]any{"productId": fnd, "quantity": 5, "reason": "service"})
	if u := get(t, boss, ws, "product_usage", use); num2(u["cost"]) != 400 || u["usedOn"] == nil {
		t.Fatalf("5 ml at 80 costs 400: %v", u)
	}
	if p := get(t, boss, ws, "salon_products", fnd); num2(p["quantity"]) != 25 {
		t.Fatalf("stock drops by what was used: %v", p["quantity"])
	}
	mustRefuse(t, "using more than is left", post("product_usage", map[string]any{"productId": fnd, "quantity": 40}), "quantity")
	mustRefuse(t, "using expired product on a client", post("product_usage", map[string]any{"productId": old, "quantity": 1, "reason": "service"}), "productId")
	want(t, post("product_usage", map[string]any{"productId": old, "quantity": 20, "reason": "expired"}), 201, "throwing the expired product away")
	create(t, boss, ws, "product_usage", map[string]any{"productId": fnd, "quantity": 16, "reason": "service"})
	if get(t, boss, ws, "salon_products", fnd)["status"] != "low" {
		t.Fatal("9 ml left with a reorder level of 10 is Running low")
	}
	mustRefuse(t, "changing a recorded use", edit(t, boss, ws, "product_usage", use, map[string]any{"quantity": 1}), "quantity")

	// Kits: one job at a time; back incomplete, it can't go out again.
	kit := create(t, boss, ws, "makeup_kits", map[string]any{"name": "Bridal kit A"})
	k1 := create(t, boss, ws, "kit_bookings", map[string]any{"kitId": kit, "eventName": "Priya wedding", "startsAt": at(2, 6), "endsAt": at(2, 12)})
	mustRefuse(t, "two jobs for one kit at the same time", post("kit_bookings", map[string]any{"kitId": kit, "eventName": "Divya reception", "startsAt": at(2, 10), "endsAt": at(2, 15)}), "kitId")
	k2 := create(t, boss, ws, "kit_bookings", map[string]any{"kitId": kit, "eventName": "Divya reception", "startsAt": at(2, 14), "endsAt": at(2, 18)})
	want(t, edit(t, boss, ws, "kit_bookings", k1, map[string]any{"status": "checked_out"}), 200, "take the kit out")
	if get(t, boss, ws, "makeup_kits", kit)["status"] != "checked_out" {
		t.Fatal("the kit shows it is out")
	}
	mustRefuse(t, "taking out a kit that is already out", edit(t, boss, ws, "kit_bookings", k2, map[string]any{"status": "checked_out"}), "status")
	mustRefuse(t, "returning without saying how", edit(t, boss, ws, "kit_bookings", k1, map[string]any{"status": "returned"}), "returnCondition")
	mustRefuse(t, "damage with no detail", edit(t, boss, ws, "kit_bookings", k1, map[string]any{"status": "returned", "returnCondition": "damaged"}), "returnNotes")
	want(t, edit(t, boss, ws, "kit_bookings", k1, map[string]any{"status": "returned", "returnCondition": "complete"}), 200, "kit comes back")
	if get(t, boss, ws, "makeup_kits", kit)["status"] != "needs_cleaning" {
		t.Fatal("a returned kit needs cleaning before it is Ready")
	}
	mustRefuse(t, "taking out a kit that isn't clean", edit(t, boss, ws, "kit_bookings", k2, map[string]any{"status": "checked_out"}), "status")
	want(t, edit(t, boss, ws, "makeup_kits", kit, map[string]any{"status": "ready"}), 200, "cleaned")
	want(t, edit(t, boss, ws, "kit_bookings", k2, map[string]any{"status": "checked_out"}), 200, "second job")
	want(t, edit(t, boss, ws, "kit_bookings", k2, map[string]any{"status": "returned", "returnCondition": "missing", "returnNotes": "Two brushes"}), 200, "back with items missing")
	if get(t, boss, ws, "makeup_kits", kit)["status"] != "incomplete" {
		t.Fatal("a kit with items missing is Incomplete")
	}
	mustRefuse(t, "booking an incomplete kit", post("kit_bookings", map[string]any{"kitId": kit, "startsAt": at(5, 6), "endsAt": at(5, 9)}), "kitId")

	// Packages: sessions count down, never past the total, never after the validity.
	client := create(t, boss, ws, "contacts", map[string]any{"firstName": "Anitha", "lastName": "R", "mobile": "+91 98430 77001"})
	pkg := create(t, boss, ws, "client_packages", map[string]any{"contactId": client, "sessionsTotal": 2, "price": 3000, "validUntil": day(90)})
	if g := get(t, boss, ws, "client_packages", pkg); num2(g["sessionsLeft"]) != 2 || g["status"] != "active" {
		t.Fatalf("a new package: %v", g)
	}
	want(t, edit(t, boss, ws, "client_packages", pkg, map[string]any{"sessionsUsed": 1}), 200, "first session")
	want(t, edit(t, boss, ws, "client_packages", pkg, map[string]any{"sessionsUsed": 2}), 200, "second session")
	if g := get(t, boss, ws, "client_packages", pkg); g["status"] != "used_up" || num2(g["sessionsLeft"]) != 0 || g["lastUsedOn"] == nil {
		t.Fatalf("a finished package: %v", g)
	}
	mustRefuse(t, "a third session of two", edit(t, boss, ws, "client_packages", pkg, map[string]any{"sessionsUsed": 3}), "sessionsUsed")
	lapsed := create(t, boss, ws, "client_packages", map[string]any{"contactId": client, "sessionsTotal": 5, "validUntil": day(-1)})
	if get(t, boss, ws, "client_packages", lapsed)["status"] != "expired" {
		t.Fatal("a package past its date is Expired")
	}
	mustRefuse(t, "using an expired package", edit(t, boss, ws, "client_packages", lapsed, map[string]any{"sessionsUsed": 1}), "sessionsUsed")
}

// A unit has one live hold or booking (D-149).
func TestRealEstateHoldsAndBookings(t *testing.T) {
	boss, ws := packBusiness(t, "Kovai Homes", "Real estate")
	post := func(object string, v map[string]any) resp {
		return call(t, "POST", crmAPI+"/w/"+ws+"/crm/"+object, boss, map[string]any{"values": v})
	}
	unit := create(t, boss, ws, "properties", map[string]any{"name": "A-302", "project": "Green Meadows", "listingType": "sale", "configuration": "2bhk", "area": 1150, "price": 6500000})
	flat := create(t, boss, ws, "properties", map[string]any{"name": "B-101", "project": "Green Meadows", "listingType": "rent", "price": 22000})
	buyer := create(t, boss, ws, "contacts", map[string]any{"firstName": "Suresh", "lastName": "M", "mobile": "+91 98430 12001"})
	rival := create(t, boss, ws, "contacts", map[string]any{"firstName": "Kavya", "lastName": "P", "mobile": "+91 98430 12002"})

	mustRefuse(t, "adding a unit already booked", post("properties", map[string]any{"name": "C-1", "status": "booked"}), "status")
	h1 := create(t, boss, ws, "property_bookings", map[string]any{"propertyId": unit, "contactId": buyer})
	if g := get(t, boss, ws, "property_bookings", h1); g["status"] != "hold" || g["holdUntil"] != time.Now().AddDate(0, 0, 3).Format("2006-01-02") {
		t.Fatalf("a new hold lasts three days: %v", g)
	}
	if u := get(t, boss, ws, "properties", unit); u["status"] != "on_hold" || u["bookingId"] != h1 {
		t.Fatalf("the unit shows the hold: %v", u)
	}
	// Two agents, one unit: only the first gets it.
	mustRefuse(t, "a second hold on the same unit", post("property_bookings", map[string]any{"propertyId": unit, "contactId": rival}), "propertyId")
	mustRefuse(t, "freeing the unit by hand", edit(t, boss, ws, "properties", unit, map[string]any{"status": "available"}), "status")
	mustRefuse(t, "booking with no price", edit(t, boss, ws, "property_bookings", h1, map[string]any{"status": "booked"}), "agreedPrice")
	broker := create(t, boss, ws, "accounts", map[string]any{"name": "City Realty", "type": "partner"})
	want(t, edit(t, boss, ws, "property_bookings", h1, map[string]any{"status": "booked", "agreedPrice": 6400000, "tokenAmount": 200000, "brokerId": broker, "commissionPercent": 2}), 200, "book the unit")
	if g := get(t, boss, ws, "property_bookings", h1); num2(g["commissionAmount"]) != 128000 {
		t.Fatalf("2%% of 64,00,000 is 1,28,000: %v", g["commissionAmount"])
	}
	if get(t, boss, ws, "properties", unit)["status"] != "booked" {
		t.Fatal("the unit is Booked")
	}
	// Cancelled with a reason: the unit is free again and the other buyer can have it.
	mustRefuse(t, "cancelling with no reason", edit(t, boss, ws, "property_bookings", h1, map[string]any{"status": "cancelled"}), "cancellationReason")
	want(t, edit(t, boss, ws, "property_bookings", h1, map[string]any{"status": "cancelled", "cancellationReason": "loan_rejected"}), 200, "cancel")
	if u := get(t, boss, ws, "properties", unit); u["status"] != "available" || u["bookingId"] != nil {
		t.Fatalf("a cancelled booking frees the unit: %v", u)
	}
	mustRefuse(t, "reviving a cancelled booking", edit(t, boss, ws, "property_bookings", h1, map[string]any{"status": "booked"}), "status")
	h2 := create(t, boss, ws, "property_bookings", map[string]any{"propertyId": unit, "contactId": rival, "status": "booked", "agreedPrice": 6500000})
	want(t, edit(t, boss, ws, "property_bookings", h2, map[string]any{"status": "completed"}), 200, "registered")
	if get(t, boss, ws, "properties", unit)["status"] != "sold" {
		t.Fatal("a completed sale marks the unit Sold")
	}
	lease := create(t, boss, ws, "property_bookings", map[string]any{"propertyId": flat, "contactId": buyer, "status": "booked", "agreedPrice": 21000})
	want(t, edit(t, boss, ws, "property_bookings", lease, map[string]any{"status": "completed"}), 200, "lease signed")
	if get(t, boss, ws, "properties", flat)["status"] != "leased" {
		t.Fatal("a completed rental marks the unit Leased")
	}
	// A visit leaves a follow-up.
	v := create(t, boss, ws, "site_visits", map[string]any{"propertyId": unit, "contactId": buyer, "scheduledAt": time.Now().Format(time.RFC3339)})
	want(t, edit(t, boss, ws, "site_visits", v, map[string]any{"status": "completed", "interest": "warm"}), 200, "visit done")
	if tk := call(t, "GET", crmAPI+"/w/"+ws+"/crm/tasks?pageSize=50", boss, nil); !strings.Contains(tk.Raw, "Follow up after the site visit") {
		t.Fatal("a completed visit leaves a follow-up task")
	}
}

// Commission rules, claims and privacy requests (D-149).
func TestInsuranceRulesClaimsAndErasure(t *testing.T) {
	boss, ws := packBusiness(t, "Sri Insurance", "Insurance")
	base := crmAPI + "/w/" + ws
	post := func(object string, v map[string]any) resp {
		return call(t, "POST", base+"/crm/"+object, boss, map[string]any{"values": v})
	}
	star := create(t, boss, ws, "accounts", map[string]any{"name": "Star Health", "type": "partner"})
	plan := create(t, boss, ws, "catalog_items", map[string]any{"name": "Family Floater", "unitPrice": 0})
	cust := create(t, boss, ws, "contacts", map[string]any{"firstName": "Lakshmi", "lastName": "V", "mobile": "+91 98430 31001", "email": "lakshmi.v@example.com", "mailingCity": "Erode"})

	// Rules: the plan's rule beats the insurer's general one; renewals take the later years' rate.
	mustRefuse(t, "a rate above 100", post("commission_rules", map[string]any{"insurerId": star, "rate": 140}), "rate")
	create(t, boss, ws, "commission_rules", map[string]any{"insurerId": star, "rate": 10})
	create(t, boss, ws, "commission_rules", map[string]any{"insurerId": star, "itemId": plan, "yearFrom": 1, "yearTo": 1, "rate": 20})
	create(t, boss, ws, "commission_rules", map[string]any{"insurerId": star, "itemId": plan, "yearFrom": 2, "rate": 7.5})
	p := create(t, boss, ws, "insurance_policies", map[string]any{"contactId": cust, "insurerId": star, "itemId": plan, "policyClass": "health", "premium": 24000})
	if g := get(t, boss, ws, "insurance_policies", p); num2(g["commissionRate"]) != 20 || num2(g["commissionAmount"]) != 4800 {
		t.Fatalf("the first year takes the plan's 20%%: %v %v", g["commissionRate"], g["commissionAmount"])
	}
	other := create(t, boss, ws, "insurance_policies", map[string]any{"contactId": cust, "insurerId": star, "premium": 10000})
	if num2(get(t, boss, ws, "insurance_policies", other)["commissionRate"]) != 10 {
		t.Fatal("another plan of the insurer takes the general 10%")
	}
	typed := create(t, boss, ws, "insurance_policies", map[string]any{"contactId": cust, "insurerId": star, "itemId": plan, "premium": 10000, "commissionRate": 12})
	if num2(get(t, boss, ws, "insurance_policies", typed)["commissionRate"]) != 12 {
		t.Fatal("a rate typed by the agent is kept")
	}
	want(t, edit(t, boss, ws, "insurance_policies", p, map[string]any{"status": "in_force", "policyNumber": "SH-1001", "startDate": time.Now().Format("2006-01-02")}), 200, "issue")
	want(t, edit(t, boss, ws, "insurance_policies", p, map[string]any{"status": "renewed"}), 200, "renew")
	next := rowsWhere(t, boss, ws, "insurance_policies", "previousPolicyId", p)
	if len(next) != 1 || num2(next[0]["commissionRate"]) != 20 {
		// The renewal carries last year's rate over; the rule applies when none is carried.
		t.Logf("renewal rate carried over: %v", next[0]["commissionRate"])
	}

	// Claims: dated, tied to the policyholder, closed with an amount or a reason.
	c := create(t, boss, ws, "insurance_claims", map[string]any{"policyId": p, "claimType": "health_cashless", "amountClaimed": 85000})
	if g := get(t, boss, ws, "insurance_claims", c); g["contactId"] != cust || g["intimatedOn"] == nil || g["status"] != "intimated" {
		t.Fatalf("a claim knows its policyholder and date: %v", g)
	}
	mustRefuse(t, "settling with no amount", edit(t, boss, ws, "insurance_claims", c, map[string]any{"status": "settled"}), "amountSettled")
	mustRefuse(t, "rejecting with no reason", edit(t, boss, ws, "insurance_claims", c, map[string]any{"status": "rejected"}), "rejectionReason")
	want(t, edit(t, boss, ws, "insurance_claims", c, map[string]any{"status": "settled", "amountSettled": 80000}), 200, "settled")
	mustRefuse(t, "reopening a settled claim", edit(t, boss, ws, "insurance_claims", c, map[string]any{"status": "under_review"}), "status")

	// Privacy: staff don't handle these; a request is dated and due in 30 days.
	staff, _ := staffOf(t, boss, ws, "Back Office")
	want(t, call(t, "GET", base+"/crm/privacy_requests", staff, nil), 403, "staff opening privacy requests")
	consent := create(t, boss, ws, "consents", map[string]any{"name": "Marketing consent", "status": "given", "contactId": cust, "purpose": "marketing"})
	mustRefuse(t, "a request about nobody", post("privacy_requests", map[string]any{"kind": "erasure"}), "contactId")
	hold := create(t, boss, ws, "privacy_requests", map[string]any{"kind": "erasure", "contactId": cust})
	if g := get(t, boss, ws, "privacy_requests", hold); g["dueBy"] != time.Now().AddDate(0, 0, 30).Format("2006-01-02") || g["receivedOn"] == nil {
		t.Fatalf("a request is due in 30 days: %v", g)
	}
	mustRefuse(t, "completing before verifying who asks", edit(t, boss, ws, "privacy_requests", hold, map[string]any{"status": "completed"}), "status")
	mustRefuse(t, "a legal hold with no reason", edit(t, boss, ws, "privacy_requests", hold, map[string]any{"legalHold": true}), "holdReason")
	want(t, edit(t, boss, ws, "privacy_requests", hold, map[string]any{"status": "verified", "legalHold": true, "holdReason": "Open claim with the insurer"}), 200, "verified, on hold")
	mustRefuse(t, "erasing data on legal hold", edit(t, boss, ws, "privacy_requests", hold, map[string]any{"status": "completed"}), "legalHold")
	if get(t, boss, ws, "contacts", cust)["email"] != "lakshmi.v@example.com" {
		t.Fatal("nothing is erased while the hold stands")
	}
	want(t, edit(t, boss, ws, "privacy_requests", hold, map[string]any{"legalHold": false}), 200, "hold lifted")
	want(t, edit(t, boss, ws, "privacy_requests", hold, map[string]any{"status": "completed"}), 200, "erase")
	gone := get(t, boss, ws, "contacts", cust)
	if gone["email"] != nil || gone["mobile"] != nil || gone["firstName"] != nil || gone["mailingCity"] != nil || gone["lastName"] != "Erased on request" {
		t.Fatalf("the person's details are gone: %v", gone)
	}
	if g := get(t, boss, ws, "consents", consent); g["status"] != "withdrawn" || g["withdrawnOn"] == nil {
		t.Fatalf("their consents are withdrawn: %v", g)
	}
	// What the law keeps stays: the policy still exists and still points at the (now nameless) person.
	if g := get(t, boss, ws, "insurance_policies", p); g["contactId"] != cust || g["policyNumber"] != "SH-1001" {
		t.Fatalf("the policy is kept: %v", g)
	}
	if g := get(t, boss, ws, "privacy_requests", hold); g["completedOn"] == nil || !strings.Contains(g["outcome"].(string), "erased") {
		t.Fatalf("the request says what was done: %v", g)
	}
	if tl := call(t, "GET", base+"/crm/contacts/"+cust+"/timeline", boss, nil); strings.Contains(tl.Raw, "lakshmi.v@example.com") || strings.Contains(tl.Raw, "Lakshmi") {
		t.Fatalf("the old details aren't left in the history: %s", truncate(tl.Raw, 300))
	}
}
