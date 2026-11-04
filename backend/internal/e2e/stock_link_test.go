package e2e

import "testing"

// Vehicles in the common stock (D-178): a vehicle received, allocated and delivered writes
// its movements on the model's "Vehicles" stock line, and the line's count follows.
func TestVehiclesInCommonStock(t *testing.T) {
	boss, ws := packBusiness(t, "Stock Link Motors", "Automotive")
	base := crmAPI + "/w/" + ws
	bike := create(t, boss, ws, "catalog_items", map[string]any{"name": "Link Bike 110", "unitPrice": 70000, "taxRate": 28})
	v1 := create(t, boss, ws, "vehicle_stock", map[string]any{"itemId": bike, "colour": "Red", "vin": "MBLLINK000000001"})
	create(t, boss, ws, "vehicle_stock", map[string]any{"itemId": bike, "colour": "Blue", "vin": "MBLLINK000000002"})
	lots := call(t, "GET", base+"/crm/stock_lots", boss, nil).list("data")
	if len(lots) != 1 {
		t.Fatalf("one stock line for the model: %d", len(lots))
	}
	lot := lots[0].(map[string]any)["values"].(map[string]any)
	if num2(lot["quantity"]) != 2 || lot["batchNumber"] != "Vehicles" {
		t.Fatalf("two vehicles on hand: %v", lot)
	}
	contact := create(t, boss, ws, "contacts", map[string]any{"firstName": "Stock", "lastName": "Buyer"})
	bk := create(t, boss, ws, "vehicle_bookings", map[string]any{"contactId": contact, "itemId": bike, "paymentPlan": "cash"})
	want(t, edit(t, boss, ws, "vehicle_bookings", bk, map[string]any{"vehicleId": v1}), 200, "allocate")
	want(t, edit(t, boss, ws, "vehicle_bookings", bk, map[string]any{"status": "delivered", "pdiPassed": true, "documentsHandedOver": true, "customerSignedOff": true, "deliveryOdometer": 2}), 200, "deliver")
	kinds := map[string]int{}
	for _, x := range call(t, "GET", base+"/crm/stock_moves?limit=50", boss, nil).list("data") {
		v := x.(map[string]any)["values"].(map[string]any)
		kinds[v["moveType"].(string)]++
	}
	if kinds["receive"] != 2 || kinds["reserve"] != 1 || kinds["release"] != 1 || kinds["issue"] != 1 {
		t.Fatalf("received twice, reserved, released and issued once: %v", kinds)
	}
	lot = call(t, "GET", base+"/crm/stock_lots", boss, nil).list("data")[0].(map[string]any)["values"].(map[string]any)
	if num2(lot["quantity"]) != 1 || num2(lot["reserved"]) != 0 {
		t.Fatalf("one vehicle left, none promised: %v", lot)
	}
	s := call(t, "GET", base+"/stock/summary", boss, nil)
	want(t, s, 200, "the stock summary")
	if len(s.list("weeks")) != 8 || s.at("onHand").(float64) != 1 {
		t.Fatalf("the summary counts what is on hand: %s", truncate(s.Raw, 300))
	}
}
