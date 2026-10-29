package e2e

import (
	"strings"
	"testing"
	"time"
)

// A bike dealer from stock to the second service (D-145): one vehicle per booking, the
// delivery checks, and everything a delivery sets in motion.
func TestDealershipStockBookingDeliveryService(t *testing.T) {
	boss := signIn(t, freshPhone(), "Dealer Principal")
	made := call(t, "POST", crmAPI+"/businesses", boss, map[string]any{"name": "Velan Honda", "industry": "Automotive"})
	want(t, made, 201, "create the dealership")
	ws := made.str("business", "code")
	base := crmAPI + "/w/" + ws
	refused := func(what string, r resp, field string) {
		t.Helper()
		if r.Status != 422 || !strings.Contains(r.Raw, `"`+field+`"`) {
			t.Fatalf("%s: want it refused on %s, got %d %s", what, field, r.Status, truncate(r.Raw, 300))
		}
	}
	post := func(object string, v map[string]any) resp {
		return call(t, "POST", base+"/crm/"+object, boss, map[string]any{"values": v})
	}
	listOf := func(object, filterField, value string) []map[string]any {
		t.Helper()
		r := call(t, "GET", base+"/crm/"+object+"?pageSize=100", boss, nil)
		want(t, r, 200, "list "+object)
		out := []map[string]any{}
		for _, x := range r.list("data") {
			v := x.(map[string]any)["values"].(map[string]any)
			if v[filterField] == value {
				v["_id"] = x.(map[string]any)["id"]
				v["_title"] = x.(map[string]any)["title"]
				out = append(out, v)
			}
		}
		return out
	}

	// A business that isn't a dealer has no vehicle stock.
	other := newBusiness(t, boss, "Velan Textiles")
	want(t, call(t, "GET", crmAPI+"/w/"+other+"/crm/vehicle_stock", boss, nil), 404, "vehicle stock in a textile shop")

	shine := create(t, boss, ws, "catalog_items", map[string]any{"name": "Honda Shine 125", "unitPrice": 82000, "serviceIntervalDays": 120, "serviceIntervalKm": 3000, "freeServices": 3})
	activa := create(t, boss, ws, "catalog_items", map[string]any{"name": "Honda Activa 6G", "unitPrice": 78000})
	// Receiving stock: no name to invent, and a VIN can be received only once.
	v1 := create(t, boss, ws, "vehicle_stock", map[string]any{"itemId": shine, "colour": "Black", "vin": "ME4JC85XXPT000101", "engineNumber": "JC85E000101"})
	v2 := create(t, boss, ws, "vehicle_stock", map[string]any{"itemId": shine, "colour": "Red", "vin": "ME4JC85XXPT000102"})
	v3 := create(t, boss, ws, "vehicle_stock", map[string]any{"itemId": activa, "colour": "Blue", "vin": "ME4JF50XXPT000201"})
	refused("the same VIN again", post("vehicle_stock", map[string]any{"itemId": shine, "vin": "ME4JC85XXPT000101"}), "vin")
	if n := get(t, boss, ws, "vehicle_stock", v1)["name"].(string); !strings.Contains(n, "Honda Shine 125") || !strings.Contains(n, "000101") {
		t.Fatalf("a vehicle names itself from model, colour and VIN, got %q", n)
	}
	refused("marking a vehicle allocated by hand", edit(t, boss, ws, "vehicle_stock", v2, map[string]any{"status": "allocated"}), "status")

	karthik := create(t, boss, ws, "contacts", map[string]any{"firstName": "Karthik", "lastName": "Raja", "mobile": "+91 98401 23456"})
	meena := create(t, boss, ws, "contacts", map[string]any{"firstName": "Meena", "lastName": "S", "mobile": "+91 98401 99887"})

	// Booking with a vehicle: both sides agree.
	refused("a booking with no customer", post("vehicle_bookings", map[string]any{"itemId": shine}), "contactId")
	b1 := create(t, boss, ws, "vehicle_bookings", map[string]any{"contactId": karthik, "itemId": shine, "colour": "Black", "bookingAmount": 5000, "promisedDate": today(), "vehicleId": v1, "expectedDailyKm": 30})
	if b := get(t, boss, ws, "vehicle_bookings", b1); b["status"] != "allocated" || b["bookingDate"] == nil || !strings.Contains(b["name"].(string), "Karthik") {
		t.Fatalf("a booking with a vehicle is Allocated, dated and named: %v", b)
	}
	if v := get(t, boss, ws, "vehicle_stock", v1); v["status"] != "allocated" || v["bookingId"] != b1 {
		t.Fatalf("the vehicle shows who holds it: %v %v", v["status"], v["bookingId"])
	}
	// The same vehicle can't go to a second customer; nor a different model.
	refused("a second booking for the same vehicle", post("vehicle_bookings", map[string]any{"contactId": meena, "itemId": shine, "vehicleId": v1}), "vehicleId")
	refused("a vehicle of another model", post("vehicle_bookings", map[string]any{"contactId": meena, "itemId": shine, "vehicleId": v3}), "vehicleId")
	b2 := create(t, boss, ws, "vehicle_bookings", map[string]any{"contactId": meena, "itemId": shine, "colour": "Red"})
	if get(t, boss, ws, "vehicle_bookings", b2)["status"] != "open" {
		t.Fatal("a booking with no vehicle waits as Booked")
	}
	want(t, edit(t, boss, ws, "vehicle_bookings", b2, map[string]any{"vehicleId": v2}), 200, "allocate from stock")
	// Cancelling needs a reason and frees the vehicle.
	refused("cancelling without a reason", edit(t, boss, ws, "vehicle_bookings", b2, map[string]any{"status": "cancelled"}), "cancellationReason")
	want(t, edit(t, boss, ws, "vehicle_bookings", b2, map[string]any{"status": "cancelled", "cancellationReason": "finance_rejected"}), 200, "cancel the booking")
	if v := get(t, boss, ws, "vehicle_stock", v2); v["status"] != "in_stock" || v["bookingId"] != nil {
		t.Fatalf("a cancelled booking returns its vehicle to stock: %v %v", v["status"], v["bookingId"])
	}
	refused("reviving a cancelled booking", edit(t, boss, ws, "vehicle_bookings", b2, map[string]any{"status": "open"}), "status")

	// Delivery: not before the checks.
	r := edit(t, boss, ws, "vehicle_bookings", b1, map[string]any{"status": "delivered"})
	refused("delivering without the inspection", r, "pdiPassed")
	if !strings.Contains(r.Raw, "customerSignedOff") || !strings.Contains(r.Raw, "documentsHandedOver") {
		t.Fatalf("every missing delivery check is named: %s", truncate(r.Raw, 400))
	}
	want(t, edit(t, boss, ws, "vehicle_bookings", b1, map[string]any{"status": "delivered", "pdiPassed": true, "documentsHandedOver": true,
		"customerSignedOff": true, "deliveryOdometer": 4}), 200, "deliver the bike")
	booking := get(t, boss, ws, "vehicle_bookings", b1)
	assetID, _ := booking["assetId"].(string)
	if assetID == "" || booking["deliveryDate"] == nil {
		t.Fatalf("delivery creates the customer's vehicle and dates itself: %v", booking)
	}
	bike := get(t, boss, ws, "assets", assetID)
	if bike["vin"] != "ME4JC85XXPT000101" || bike["contactId"] != karthik || bike["odometer"] == nil || bike["nextServiceDate"] == nil {
		t.Fatalf("the customer's vehicle carries VIN, owner, odometer and next service: %v", bike)
	}
	if v := get(t, boss, ws, "vehicle_stock", v1); v["status"] != "delivered" || v["assetId"] != assetID {
		t.Fatalf("the stock vehicle is Delivered and points to the customer's vehicle: %v", v)
	}
	refused("un-delivering", edit(t, boss, ws, "vehicle_bookings", b1, map[string]any{"status": "allocated"}), "status")
	plan := listOf("service_dues", "assetId", assetID)
	if len(plan) != 3 {
		t.Fatalf("three free services are planned at delivery, got %d", len(plan))
	}
	first := time.Now().AddDate(0, 0, 30).Format("2006-01-02")
	var s1, s2, s3 string
	for _, s := range plan {
		switch num2(s["serviceNumber"]) {
		case 1:
			s1 = s["_id"].(string)
			if s["dueDate"] != first || num2(s["dueKm"]) != 754 || s["kind"] != "free" || s["status"] != "planned" {
				t.Fatalf("service 1 is due 30 days and 750 km after delivery: %v", s)
			}
		case 2:
			s2 = s["_id"].(string)
		case 3:
			s3 = s["_id"].(string)
		}
	}
	if len(listOf("odometer_readings", "assetId", assetID)) != 1 {
		t.Fatal("the delivery reading is stored")
	}
	regs := listOf("vehicle_registrations", "assetId", assetID)
	if len(regs) != 1 || regs[0]["status"] != "temporary" {
		t.Fatalf("a registration is opened at delivery: %v", regs)
	}
	tasks := call(t, "GET", base+"/crm/tasks?pageSize=50", boss, nil)
	if !strings.Contains(tasks.Raw, "Thank-you and feedback call") {
		t.Fatal("a thank-you call is planned three days after delivery")
	}

	// Registration: the number reaches the vehicle, and the vehicle is found by it.
	reg := regs[0]["_id"].(string)
	refused("RC issued with no number", edit(t, boss, ws, "vehicle_registrations", reg, map[string]any{"status": "rc_issued"}), "registrationNumber")
	want(t, edit(t, boss, ws, "vehicle_registrations", reg, map[string]any{"status": "rc_issued", "registrationNumber": "tn 38 cx 4521"}), 200, "RC issued")
	if g := get(t, boss, ws, "vehicle_registrations", reg); g["registrationNumber"] != "TN 38 CX 4521" || g["rcIssuedOn"] == nil {
		t.Fatalf("the number is tidied and the stage dated: %v", g)
	}
	if get(t, boss, ws, "assets", assetID)["registrationNumber"] != "TN 38 CX 4521" {
		t.Fatal("the registration number is written to the customer's vehicle")
	}
	for _, q := range []string{"CX%204521", "3456", "000101"} { // registration, owner's phone, VIN
		found := call(t, "GET", base+"/lookup/assets?q="+q, boss, nil)
		if l := found.list("data"); len(l) != 1 || l[0].(map[string]any)["id"] != assetID {
			t.Fatalf("job card lookup for %q: %s", q, truncate(found.Raw, 300))
		}
	}

	// Service: closing needs the odometer; a job card closes its service; the cycle goes on.
	refused("closing a service with no odometer", edit(t, boss, ws, "service_dues", s1, map[string]any{"status": "done"}), "closingOdometer")
	job := create(t, boss, ws, "work_orders", map[string]any{"name": "First free service", "assetId": assetID, "contactId": karthik, "serviceDueId": s1})
	if get(t, boss, ws, "service_dues", s1)["status"] != "booked" {
		t.Fatal("a job card for a scheduled service marks it Booked")
	}
	refused("completing the job with no odometer", edit(t, boss, ws, "work_orders", job, map[string]any{"status": "completed"}), "odometer")
	want(t, edit(t, boss, ws, "work_orders", job, map[string]any{"status": "completed", "odometer": 810}), 200, "complete the first service")
	if d := get(t, boss, ws, "service_dues", s1); d["status"] != "done" || num2(d["closingOdometer"]) != 810 || d["doneOn"] == nil {
		t.Fatalf("the service is closed from the job card: %v", d)
	}
	if b := get(t, boss, ws, "assets", assetID); num2(b["odometer"]) != 810 {
		t.Fatalf("the vehicle shows the latest odometer: %v", b["odometer"])
	}
	refused("an odometer that goes backwards", edit(t, boss, ws, "service_dues", s2, map[string]any{"status": "done", "closingOdometer": 500}), "closingOdometer")
	refused("a reading typed in lower than the last", post("odometer_readings", map[string]any{"assetId": assetID, "reading": 300}), "reading")
	refused("moving a due date without saying why", edit(t, boss, ws, "service_dues", s2, map[string]any{"dueDate": time.Now().AddDate(0, 2, 0).Format("2006-01-02")}), "overrideReason")
	want(t, edit(t, boss, ws, "service_dues", s2, map[string]any{"status": "done", "closingOdometer": 3900}), 200, "second service")
	if len(listOf("service_dues", "assetId", assetID)) != 3 {
		t.Fatal("no extra service is planned while one is still waiting")
	}
	want(t, edit(t, boss, ws, "service_dues", s3, map[string]any{"status": "done", "closingOdometer": 7100}), 200, "third service")
	after := listOf("service_dues", "assetId", assetID)
	if len(after) != 4 {
		t.Fatalf("closing the last planned service plans the next: %d", len(after))
	}
	for _, s := range after {
		if num2(s["serviceNumber"]) == 4 && (s["kind"] != "paid" || num2(s["dueKm"]) != 10100 || s["status"] != "planned") {
			t.Fatalf("service 4 is a paid one, 3,000 km on: %v", s)
		}
	}
	if get(t, boss, ws, "assets", assetID)["nextServiceKm"] == nil {
		t.Fatal("the vehicle shows its next service")
	}

	// A repair job card on the vehicle (not a scheduled service) also needs the odometer, and
	// the vehicle shows it (D-167).
	repair := create(t, boss, ws, "work_orders", map[string]any{"name": "Brake noise", "assetId": assetID, "contactId": karthik})
	refused("completing a repair with no odometer", edit(t, boss, ws, "work_orders", repair, map[string]any{"status": "completed"}), "odometer")
	refused("a repair odometer lower than the last", edit(t, boss, ws, "work_orders", repair, map[string]any{"status": "completed", "odometer": 7000}), "odometer")
	want(t, edit(t, boss, ws, "work_orders", repair, map[string]any{"status": "completed", "odometer": 7250}), 200, "complete the repair")
	if b := get(t, boss, ws, "assets", assetID); num2(b["odometer"]) != 7250 {
		t.Fatalf("a completed repair brings the vehicle's odometer up to date: %v", b["odometer"])
	}

	// Test ride: the checks come first; a completed ride leaves a follow-up.
	refused("a ride on an expired licence", post("test_rides", map[string]any{"contactId": meena, "itemId": activa, "scheduledAt": time.Now().Format(time.RFC3339), "licenceValidUntil": "2020-01-01"}), "licenceValidUntil")
	ride := create(t, boss, ws, "test_rides", map[string]any{"contactId": meena, "itemId": activa, "vehicleId": v3, "scheduledAt": time.Now().Format(time.RFC3339)})
	refused("completing a ride with no checks", edit(t, boss, ws, "test_rides", ride, map[string]any{"status": "completed"}), "licenceSeen")
	want(t, edit(t, boss, ws, "test_rides", ride, map[string]any{"status": "completed", "licenceSeen": true, "indemnitySigned": true, "startOdometer": 120, "endOdometer": 126, "rating": 4}), 200, "complete the ride")
	if tk := call(t, "GET", base+"/crm/tasks?pageSize=50", boss, nil); !strings.Contains(tk.Raw, "Follow up after the test ride") {
		t.Fatal("a completed test ride leaves a follow-up task")
	}

	// Staff see the whole stock when they allocate, but don't change it.
	staff, _ := staffOf(t, boss, ws, "Sales Executive")
	if l := call(t, "GET", base+"/lookup/vehicle_stock?q=000102", staff, nil).list("data"); len(l) != 1 {
		t.Fatal("staff can pick any vehicle in stock")
	}
	want(t, call(t, "POST", base+"/crm/vehicle_stock", staff, map[string]any{"values": map[string]any{"itemId": shine, "vin": "X1"}}), 403, "staff adding stock")
	mine := call(t, "POST", base+"/crm/contacts", staff, map[string]any{"values": map[string]any{"firstName": "Walk", "lastName": "In", "mobile": "+91 90000 11223"}})
	want(t, mine, 201, "staff adds their walk-in")
	sb := call(t, "POST", base+"/crm/vehicle_bookings", staff, map[string]any{"values": map[string]any{"contactId": mine.str("id"), "itemId": shine, "vehicleId": v2}})
	if sb.Status != 201 && sb.Status != 200 {
		t.Fatalf("staff books and allocates for their own customer: %d %s", sb.Status, truncate(sb.Raw, 300))
	}
	if get(t, boss, ws, "vehicle_stock", v2)["status"] != "allocated" {
		t.Fatal("the staff member's booking holds the vehicle")
	}
}
