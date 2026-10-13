package e2e

import (
	"strings"
	"testing"
	"time"
)

func mustRefuse(t *testing.T, what string, r resp, field string) {
	t.Helper()
	if r.Status != 422 || !strings.Contains(r.Raw, `"`+field+`"`) {
		t.Fatalf("%s: want it refused on %s, got %d %s", what, field, r.Status, truncate(r.Raw, 300))
	}
}

func rowsWhere(t *testing.T, token, ws, object, field, value string) []map[string]any {
	t.Helper()
	r := call(t, "GET", crmAPI+"/w/"+ws+"/crm/"+object+"?pageSize=100", token, nil)
	want(t, r, 200, "list "+object)
	out := []map[string]any{}
	for _, x := range r.list("data") {
		v := x.(map[string]any)["values"].(map[string]any)
		if v[field] == value {
			v["_id"] = x.(map[string]any)["id"]
			out = append(out, v)
		}
	}
	return out
}

// The dealer's on-road price, exchange vehicle, loan applications and stock transfer (D-147).
func TestDealershipPriceExchangeFinanceTransfer(t *testing.T) {
	boss := signIn(t, freshPhone(), "Principal")
	made := call(t, "POST", crmAPI+"/businesses", boss, map[string]any{"name": "Kovai Motors", "industry": "Automotive"})
	want(t, made, 201, "create the dealership")
	ws := made.str("business", "code")
	post := func(object string, v map[string]any) resp {
		return call(t, "POST", crmAPI+"/w/"+ws+"/crm/"+object, boss, map[string]any{"values": v})
	}
	model := create(t, boss, ws, "catalog_items", map[string]any{"name": "Honda SP 160", "unitPrice": 118000})
	veh := create(t, boss, ws, "vehicle_stock", map[string]any{"itemId": model, "colour": "Grey", "vin": "ME4KC40XXPT000301", "location": "Main showroom"})
	cust := create(t, boss, ws, "contacts", map[string]any{"firstName": "Priya", "lastName": "N", "mobile": "+91 98420 11221"})

	// On-road price is the sum of its parts, less discount; the balance is that less the advance.
	b := create(t, boss, ws, "vehicle_bookings", map[string]any{"contactId": cust, "itemId": model, "bookingAmount": 10000,
		"exShowroom": 118000, "roadTax": 11800, "insurancePremium": 6200, "handlingCharges": 1500, "hsrpCharges": 600, "accessoriesAmount": 2400, "discountAmount": 2000})
	bk := get(t, boss, ws, "vehicle_bookings", b)
	if num2(bk["onRoadPrice"]) != 138500 || num2(bk["balanceToPay"]) != 128500 {
		t.Fatalf("on-road price and balance: %v / %v", bk["onRoadPrice"], bk["balanceToPay"])
	}

	// Exchange: an offer is needed, lasts seven days, and an accepted one comes off the price.
	mustRefuse(t, "evaluated with no offer", post("vehicle_exchanges", map[string]any{"bookingId": b, "makeModel": "Hero Splendor", "status": "evaluated"}), "offeredPrice")
	ex := create(t, boss, ws, "vehicle_exchanges", map[string]any{"bookingId": b, "contactId": cust, "makeModel": "Hero Splendor", "registrationNumber": "TN 37 AB 1234",
		"expectedPrice": 30000, "offeredPrice": 25000, "status": "evaluated", "loanOutstanding": true})
	if get(t, boss, ws, "vehicle_exchanges", ex)["validUntil"] != time.Now().AddDate(0, 0, 7).Format("2006-01-02") {
		t.Fatal("an offer is valid for seven days")
	}
	want(t, edit(t, boss, ws, "vehicle_exchanges", ex, map[string]any{"status": "accepted"}), 200, "customer accepts the offer")
	bk = get(t, boss, ws, "vehicle_bookings", b)
	if num2(bk["exchangeValue"]) != 25000 || num2(bk["onRoadPrice"]) != 113500 || num2(bk["balanceToPay"]) != 103500 || !strings.Contains(bk["exchangeVehicle"].(string), "Splendor") {
		t.Fatalf("the accepted exchange comes off the booking: %v %v %v", bk["exchangeValue"], bk["onRoadPrice"], bk["balanceToPay"])
	}
	mustRefuse(t, "taking in a vehicle with a loan on it", edit(t, boss, ws, "vehicle_exchanges", ex, map[string]any{"status": "taken_in"}), "loanOutstanding")
	want(t, edit(t, boss, ws, "vehicle_exchanges", ex, map[string]any{"status": "declined"}), 200, "customer changes their mind")
	if bk = get(t, boss, ws, "vehicle_bookings", b); bk["exchangeValue"] != nil || num2(bk["onRoadPrice"]) != 138500 {
		t.Fatalf("a declined exchange goes back on the price: %v %v", bk["exchangeValue"], bk["onRoadPrice"])
	}

	// Loans: EMI worked out; one approval closes the others and names the financier.
	hdfc := create(t, boss, ws, "accounts", map[string]any{"name": "HDFC Two Wheeler Loans", "type": "partner"})
	city := create(t, boss, ws, "accounts", map[string]any{"name": "City Finance", "type": "partner"})
	l1 := create(t, boss, ws, "finance_applications", map[string]any{"bookingId": b, "contactId": cust, "lenderId": hdfc, "loanAmount": 100000, "interestRate": 12, "tenureMonths": 24, "status": "submitted"})
	l2 := create(t, boss, ws, "finance_applications", map[string]any{"bookingId": b, "contactId": cust, "lenderId": city, "loanAmount": 100000, "interestRate": 14, "tenureMonths": 24, "status": "submitted"})
	if e := num2(get(t, boss, ws, "finance_applications", l1)["emi"]); e != 4707.35 {
		t.Fatalf("EMI of 1,00,000 at 12%% over 24 months is 4,707.35, got %v", e)
	}
	mustRefuse(t, "a rejection with no reason", edit(t, boss, ws, "finance_applications", l2, map[string]any{"status": "rejected"}), "rejectionReason")
	mustRefuse(t, "disbursing before approval", edit(t, boss, ws, "finance_applications", l2, map[string]any{"status": "disbursed"}), "status")
	want(t, edit(t, boss, ws, "finance_applications", l1, map[string]any{"status": "approved"}), 200, "HDFC approves")
	if get(t, boss, ws, "finance_applications", l2)["status"] != "closed" {
		t.Fatal("the other lender's application is closed")
	}
	if bk = get(t, boss, ws, "vehicle_bookings", b); bk["financier"] != "HDFC Two Wheeler Loans" || bk["paymentPlan"] != "loan" {
		t.Fatalf("the booking shows the financier: %v %v", bk["financier"], bk["paymentPlan"])
	}

	// Transfer: the vehicle is on the way, can't be allocated meanwhile, and arrives.
	tr := create(t, boss, ws, "stock_transfers", map[string]any{"vehicleId": veh, "toLocation": "Pollachi branch"})
	if g := get(t, boss, ws, "stock_transfers", tr); g["fromLocation"] != "Main showroom" || g["status"] != "in_transit" {
		t.Fatalf("a transfer knows where it started: %v", g)
	}
	mustRefuse(t, "allocating a vehicle that is on the way", edit(t, boss, ws, "vehicle_bookings", b, map[string]any{"vehicleId": veh}), "vehicleId")
	mustRefuse(t, "a second transfer of the same vehicle", post("stock_transfers", map[string]any{"vehicleId": veh, "toLocation": "Udumalpet"}), "vehicleId")
	want(t, edit(t, boss, ws, "stock_transfers", tr, map[string]any{"status": "received"}), 200, "the branch confirms")
	if v := get(t, boss, ws, "vehicle_stock", veh); v["status"] != "in_stock" || v["location"] != "Pollachi branch" {
		t.Fatalf("the vehicle is in stock at the new place: %v %v", v["status"], v["location"])
	}
	want(t, edit(t, boss, ws, "vehicle_bookings", b, map[string]any{"vehicleId": veh}), 200, "now it can be allocated")
}

// An insurance agent's policy from issue to renewal (D-148).
func TestInsurancePolicyPremiumCommissionRenewal(t *testing.T) {
	boss := signIn(t, freshPhone(), "Agent")
	made := call(t, "POST", crmAPI+"/businesses", boss, map[string]any{"name": "Ganesh Insurance", "industry": "Insurance"})
	want(t, made, 201, "create the agency")
	ws := made.str("business", "code")
	base := crmAPI + "/w/" + ws
	// An agency has no vehicle stock; a dealer has no policies.
	want(t, call(t, "GET", base+"/crm/vehicle_stock", boss, nil), 404, "vehicle stock at an insurance agency")
	post := func(object string, v map[string]any) resp {
		return call(t, "POST", base+"/crm/"+object, boss, map[string]any{"values": v})
	}
	lic := create(t, boss, ws, "accounts", map[string]any{"name": "LIC of India", "type": "partner"})
	cust := create(t, boss, ws, "contacts", map[string]any{"firstName": "Ramesh", "lastName": "K", "mobile": "+91 98430 55661"})

	// The worked example of the catalogue: premium 20,000, commission 15%, costs 1,000.
	p := create(t, boss, ws, "insurance_policies", map[string]any{"contactId": cust, "insurerId": lic, "policyClass": "life", "premium": 20000,
		"commissionRate": 15, "directCosts": 1000, "frequency": "quarterly", "collectionMode": "agent"})
	pol := get(t, boss, ws, "insurance_policies", p)
	if pol["status"] != "proposal" || num2(pol["totalToCollect"]) != 20000 || num2(pol["commissionAmount"]) != 3000 || num2(pol["profit"]) != 2000 || num2(pol["amountToRemit"]) != 20000 {
		t.Fatalf("customer pays 20,000, commission 3,000, profit 2,000, remit 20,000: %v", pol)
	}
	mustRefuse(t, "issuing with no policy number", edit(t, boss, ws, "insurance_policies", p, map[string]any{"status": "in_force"}), "policyNumber")
	mustRefuse(t, "ending before starting", edit(t, boss, ws, "insurance_policies", p, map[string]any{"startDate": "2026-10-01", "endDate": "2026-09-01"}), "endDate")
	start := time.Now().Format("2006-01-02")
	want(t, edit(t, boss, ws, "insurance_policies", p, map[string]any{"status": "in_force", "policyNumber": "LIC-778812", "startDate": start}), 200, "issue the policy")
	pol = get(t, boss, ws, "insurance_policies", p)
	if pol["endDate"] != time.Now().AddDate(1, 0, -1).Format("2006-01-02") {
		t.Fatalf("a policy runs a year unless told otherwise: %v", pol["endDate"])
	}
	mustRefuse(t, "the same policy number twice", post("insurance_policies", map[string]any{"contactId": cust, "insurerId": lic, "policyNumber": "LIC-778812", "premium": 1}), "policyNumber")
	dues := rowsWhere(t, boss, ws, "premium_dues", "policyId", p)
	if len(dues) != 4 {
		t.Fatalf("a quarterly policy has four instalments, got %d", len(dues))
	}
	sum := 0.0
	var first string
	for _, d := range dues {
		sum += num2(d["amount"])
		if num2(d["instalmentNumber"]) == 1 {
			first = d["_id"].(string)
			if d["dueDate"] != start || d["status"] != "due" {
				t.Fatalf("the first instalment is due at the start: %v", d)
			}
		}
	}
	if sum != 20000 {
		t.Fatalf("the instalments add up to what the customer pays: %v", sum)
	}
	coms := rowsWhere(t, boss, ws, "commissions", "policyId", p)
	if len(coms) != 1 || num2(coms[0]["expectedAmount"]) != 3000 || coms[0]["kind"] != "first_year" || coms[0]["status"] != "expected" {
		t.Fatalf("the first-year commission is expected: %v", coms)
	}

	// Premium: part payment isn't Paid; remitting needs the customer's payment first.
	mustRefuse(t, "remitting before the customer paid", edit(t, boss, ws, "premium_dues", first, map[string]any{"remittedOn": start}), "remittedOn")
	mustRefuse(t, "marking a part payment Paid", edit(t, boss, ws, "premium_dues", first, map[string]any{"status": "paid", "paidAmount": 2000}), "paidAmount")
	want(t, edit(t, boss, ws, "premium_dues", first, map[string]any{"status": "paid", "paymentMode": "upi"}), 200, "premium paid")
	if d := get(t, boss, ws, "premium_dues", first); d["paidOn"] == nil || num2(d["paidAmount"]) != 5000 {
		t.Fatalf("a paid instalment is dated with its amount: %v", d)
	}

	// Commission: short payment is flagged with the difference.
	c := coms[0]["_id"].(string)
	want(t, edit(t, boss, ws, "commissions", c, map[string]any{"receivedAmount": 2400, "tdsDeducted": 150}), 200, "insurer pays less")
	if g := get(t, boss, ws, "commissions", c); g["status"] != "short_paid" || num2(g["variance"]) != -450 {
		t.Fatalf("2,400 + 150 TDS against 3,000 is 450 short: %v %v", g["status"], g["variance"])
	}
	want(t, edit(t, boss, ws, "commissions", c, map[string]any{"receivedAmount": 2850}), 200, "the rest arrives")
	if g := get(t, boss, ws, "commissions", c); g["status"] != "received" || num2(g["variance"]) != 0 {
		t.Fatalf("paid in full: %v %v", g["status"], g["variance"])
	}

	// Renewal: the next term starts the day after, as a proposal; only one is made.
	want(t, edit(t, boss, ws, "insurance_policies", p, map[string]any{"status": "renewed"}), 200, "renew")
	next := rowsWhere(t, boss, ws, "insurance_policies", "previousPolicyId", p)
	if len(next) != 1 || next[0]["status"] != "proposal" || next[0]["startDate"] != time.Now().AddDate(1, 0, 0).Format("2006-01-02") ||
		num2(next[0]["commissionAmount"]) != 3000 || next[0]["policyNumber"] != nil {
		t.Fatalf("the next term: %v", next)
	}
	mustRefuse(t, "reopening a renewed term", edit(t, boss, ws, "insurance_policies", p, map[string]any{"status": "in_force"}), "status")
	n := next[0]["_id"].(string)
	want(t, edit(t, boss, ws, "insurance_policies", n, map[string]any{"status": "in_force", "policyNumber": "LIC-778812-R1"}), 200, "issue the renewal")
	rc := rowsWhere(t, boss, ws, "commissions", "policyId", n)
	if len(rc) != 1 || rc[0]["kind"] != "renewal" || num2(rc[0]["policyYear"]) != 2 {
		t.Fatalf("the renewal's commission is year 2: %v", rc)
	}

	// Free-look cancellation: the schedule stops and received commission is clawed back.
	want(t, edit(t, boss, ws, "commissions", rc[0]["_id"].(string), map[string]any{"receivedAmount": 3000}), 200, "renewal commission received")
	mustRefuse(t, "cancelling with no reason", edit(t, boss, ws, "insurance_policies", n, map[string]any{"status": "cancelled"}), "cancellationReason")
	want(t, edit(t, boss, ws, "insurance_policies", n, map[string]any{"status": "cancelled", "cancellationReason": "free_look"}), 200, "free-look cancellation")
	for _, d := range rowsWhere(t, boss, ws, "premium_dues", "policyId", n) {
		if d["status"] != "cancelled" {
			t.Fatalf("an unpaid instalment of a cancelled policy is cancelled: %v", d["status"])
		}
	}
	claw := 0.0
	for _, x := range rowsWhere(t, boss, ws, "commissions", "policyId", n) {
		if x["status"] == "clawback" {
			claw = num2(x["expectedAmount"])
		}
	}
	if claw != -3000 {
		t.Fatalf("the commission already received is to be given back: %v", claw)
	}

	// A direct-pay policy holds nothing for the insurer.
	d := create(t, boss, ws, "insurance_policies", map[string]any{"contactId": cust, "insurerId": lic, "premium": 12000, "commissionAmount": 1500, "collectionMode": "direct"})
	if g := get(t, boss, ws, "insurance_policies", d); num2(g["amountToRemit"]) != 0 || num2(g["profit"]) != 1500 {
		t.Fatalf("direct mode: nothing to remit, profit is the commission: %v %v", g["amountToRemit"], g["profit"])
	}
}
