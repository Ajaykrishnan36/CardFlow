package e2e

import (
	"math"
	"strings"
	"testing"
	"time"
)

// The customer happy flow (D-170): nothing asks for a name it can work out; converting a
// lead picks products (priced from the price book) instead of typing an amount; the quote
// starts with the opportunity's products; money is taken against the quote, a part payment
// leaves a "collect the balance" task, and full payment makes the invoice, pays it and wins
// the deal; delivering the bike opens the free-service ticket.
func TestCustomerHappyFlow(t *testing.T) {
	boss := signIn(t, freshPhone(), "Flow Owner")
	made := call(t, "POST", crmAPI+"/businesses", boss, map[string]any{"name": "Flow Hero Motors", "industry": "Automotive"})
	want(t, made, 201, "create the dealer")
	ws := made.str("business", "code")
	base := crmAPI + "/w/" + ws
	today := time.Now().Format("2006-01-02")

	bike := create(t, boss, ws, "catalog_items", map[string]any{"name": "Hero Splendor+ Drum", "unitPrice": 76000, "taxRate": 28, "freeServices": 4, "firstServiceDays": 30, "firstServiceKm": 1000})
	helmet := create(t, boss, ws, "catalog_items", map[string]any{"name": "ISI Helmet", "unitPrice": 1200, "taxRate": 18})
	jacket := create(t, boss, ws, "catalog_items", map[string]any{"name": "Racing jacket", "unitPrice": 3500, "taxRate": 18})
	book := create(t, boss, ws, "price_books", map[string]any{"name": "Showroom 2026", "isDefault": true, "status": "active"})
	want(t, call(t, "POST", base+"/price-book-entries", boss, map[string]any{"priceBookId": book, "itemId": bike, "unitPrice": 75000, "listPrice": 75000, "validFrom": "2026-01-01"}), 200, "showroom price")

	// No name to invent.
	meta := call(t, "GET", base+"/crm/meta/assets", boss, nil)
	for _, f := range meta.list("fields") {
		if m := f.(map[string]any); m["key"] == "name" && m["required"] == true {
			t.Fatalf("an asset's name is filled in, not asked: %v", m)
		}
	}

	// Convert with a product: the price comes from the price book.
	lead := create(t, boss, ws, "leads", map[string]any{"firstName": "Senthil", "lastName": "Kumar", "mobile": "9000077001", "source": "phone"})
	cv := call(t, "POST", base+"/crm/leads/"+lead+"/convert", boss, map[string]any{
		"account": map[string]any{"mode": "none"}, "createContact": true,
		"opportunity": map[string]any{"create": true, "items": []any{map[string]any{"itemId": bike, "quantity": 1}}}})
	want(t, cv, 200, "convert with a product")
	contact, opp := cv.str("contactId"), cv.str("opportunityId")
	o := get(t, boss, ws, "opportunities", opp)
	if !strings.Contains(o["name"].(string), "Hero Splendor+ Drum") || o["amount"].(float64) != 96000 {
		t.Fatalf("the opportunity is named from the product and priced from the price book (75,000 + 28%%): %v / %v", o["name"], o["amount"])
	}

	// More products over the follow-ups.
	want(t, call(t, "PUT", base+"/opportunities/"+opp+"/lines", boss, map[string]any{"lines": []any{
		map[string]any{"itemId": bike, "quantity": 1}, map[string]any{"itemId": helmet, "quantity": 1}, map[string]any{"itemId": jacket, "quantity": 1}}}), 200, "add helmet and jacket")
	// 75,000×1.28 + 1,200×1.18 + 3,500×1.18 = 96,000 + 1,416 + 4,130 = 1,01,546
	grand := 101546.0

	// The quote starts with them, and names itself.
	q := create(t, boss, ws, "quotes", map[string]any{"contactId": contact, "opportunityId": opp})
	qv := get(t, boss, ws, "quotes", q)
	if qv["total"].(float64) != grand || !strings.Contains(qv["name"].(string), "Senthil Kumar") {
		t.Fatalf("the quote copies the opportunity's three products: %v / %v", qv["total"], qv["name"])
	}

	// Part payment: accepted, balance task, no invoice yet.
	create(t, boss, ws, "payments", map[string]any{"amount": 50000, "paymentDate": today, "status": "paid", "paymentMethod": "cash", "quoteId": q})
	if s := get(t, boss, ws, "quotes", q)["status"]; s != "accepted" {
		t.Fatalf("a part-paid quote is accepted: %v", s)
	}
	if total(t, boss, ws, "invoices", "") != 0 {
		t.Fatal("no invoice before the quote is paid in full")
	}
	tasks := call(t, "GET", base+"/crm/tasks?pageSize=50", boss, nil)
	found := false
	for _, x := range tasks.list("data") {
		v := x.(map[string]any)["values"].(map[string]any)
		if strings.Contains(v["name"].(string), "Collect the balance ₹51,546") && v["status"] == "not_started" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a task to collect the balance: %s", truncate(tasks.Raw, 600))
	}

	// The rest: invoice made, paid, deal won.
	create(t, boss, ws, "payments", map[string]any{"amount": grand - 50000, "paymentDate": today, "status": "paid", "paymentMethod": "upi", "quoteId": q})
	inv := call(t, "GET", base+"/crm/invoices?pageSize=10", boss, nil)
	if len(inv.list("data")) != 1 {
		t.Fatalf("one invoice once paid in full: %s", truncate(inv.Raw, 300))
	}
	iv := inv.list("data")[0].(map[string]any)["values"].(map[string]any)
	if iv["status"] != "paid" || iv["total"].(float64) != grand || math.Abs(iv["balanceDue"].(float64)) > 0.001 || iv["quoteId"] != q {
		t.Fatalf("the invoice is made from the quote and paid: %v", iv)
	}
	o = get(t, boss, ws, "opportunities", opp)
	if o["status"] != "closed_won" || o["amount"].(float64) != grand {
		t.Fatalf("the deal is won for what was invoiced: %v / %v", o["status"], o["amount"])
	}
	for _, x := range call(t, "GET", base+"/crm/tasks?pageSize=50", boss, nil).list("data") {
		v := x.(map[string]any)["values"].(map[string]any)
		if strings.Contains(v["name"].(string), "Collect the balance") && v["status"] != "completed" {
			t.Fatalf("the balance task is done once paid: %v", v)
		}
	}

	// Delivery opens the free-service ticket on the customer's bike.
	vin := create(t, boss, ws, "vehicle_stock", map[string]any{"itemId": bike, "colour": "Black", "vin": "MBLHAW12XTGF00001"})
	bk := create(t, boss, ws, "vehicle_bookings", map[string]any{"contactId": contact, "itemId": bike, "vehicleId": vin, "opportunityId": opp, "paymentPlan": "cash"})
	want(t, edit(t, boss, ws, "vehicle_bookings", bk, map[string]any{"status": "delivered", "pdiPassed": true, "documentsHandedOver": true, "customerSignedOff": true, "deliveryOdometer": 5}), 200, "deliver")
	asset := get(t, boss, ws, "vehicle_bookings", bk)["assetId"]
	found = false
	for _, x := range call(t, "GET", base+"/crm/cases?pageSize=20", boss, nil).list("data") {
		v := x.(map[string]any)["values"].(map[string]any)
		if strings.HasPrefix(v["name"].(string), "Free service 1") && v["assetId"] == asset && v["contactId"] == contact && strings.Contains(v["description"].(string), "1005 km") {
			found = true
		}
	}
	if !found {
		t.Fatal("delivery opens the free-service ticket on the bike")
	}

	// The owner's view counts it all: a completed test ride that ended in a booking.
	create(t, boss, ws, "test_rides", map[string]any{"contactId": contact, "itemId": bike, "status": "completed", "licenceSeen": true, "indemnitySigned": true, "scheduledAt": time.Now().Format(time.RFC3339)})
	off := call(t, "GET", base+"/dashboard/office", boss, nil)
	want(t, off, 200, "the office view")
	rides := off.list("testRidesByModel")
	if len(rides) != 1 || rides[0].(map[string]any)["bought"].(float64) != 1 || off.at("testRideToPurchase").(float64) != 100 {
		t.Fatalf("test ride → purchase by model: %s", truncate(off.Raw, 600))
	}
	tiles := map[string]float64{}
	for _, x := range off.list("tiles") {
		m := x.(map[string]any)
		tiles[m["key"].(string)] = m["value"].(float64)
	}
	if tiles["revenueWon"] != grand {
		t.Fatalf("revenue won this month is the invoice: %v", tiles)
	}
}
