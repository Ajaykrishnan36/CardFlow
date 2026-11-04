package e2e

import (
	"testing"
	"time"
)

// One plan at a time (D-175): a second active showroom price book for the same days is
// refused until the running one is stopped; a customer's own book doesn't clash with the
// showroom's; a product lists only today's prices as current.
func TestOnePricePlanAtATime(t *testing.T) {
	boss, ws := packBusiness(t, "Plan Switch Motors", "Automotive")
	base := crmAPI + "/w/" + ws
	today := time.Now().Format("2006-01-02")
	bike := create(t, boss, ws, "catalog_items", map[string]any{"name": "Commuter 110", "unitPrice": 80000, "taxRate": 28})
	show := create(t, boss, ws, "price_books", map[string]any{"name": "Showroom", "status": "active", "isDefault": true, "validFrom": "2026-01-01"})
	want(t, call(t, "POST", base+"/price-book-entries", boss, map[string]any{"priceBookId": show, "itemId": bike, "unitPrice": 80000, "listPrice": 80000, "validFrom": "2026-01-01"}), 200, "showroom price")
	mustRefuse(t, "a second showroom plan for the same days", call(t, "POST", base+"/crm/price_books", boss, map[string]any{"values": map[string]any{"name": "Offer", "status": "active", "isDefault": true, "validFrom": today}}), "status")
	acc := create(t, boss, ws, "accounts", map[string]any{"name": "Fleet Co"})
	create(t, boss, ws, "price_books", map[string]any{"name": "Fleet Co price", "status": "active", "accountId": acc, "validFrom": today})
	mustRefuse(t, "two books for one customer at once", call(t, "POST", base+"/crm/price_books", boss, map[string]any{"values": map[string]any{"name": "Fleet Co again", "status": "active", "accountId": acc}}), "status")

	offer := create(t, boss, ws, "price_books", map[string]any{"name": "New Year offer", "status": "inactive", "isDefault": true, "validFrom": today})
	want(t, call(t, "POST", base+"/price-book-entries", boss, map[string]any{"priceBookId": offer, "itemId": bike, "unitPrice": 72000, "listPrice": 80000, "validFrom": today}), 200, "offer price")
	mustRefuse(t, "starting the offer while the showroom plan runs", edit(t, boss, ws, "price_books", offer, map[string]any{"status": "active"}), "status")
	want(t, edit(t, boss, ws, "price_books", show, map[string]any{"status": "inactive"}), 200, "stop the showroom plan")
	want(t, edit(t, boss, ws, "price_books", offer, map[string]any{"status": "active"}), 200, "start the offer")

	current := 0
	for _, x := range call(t, "GET", base+"/price-book-entries?itemId="+bike, boss, nil).list("data") {
		e := x.(map[string]any)
		if e["current"] == true {
			current++
			if e["priceBookId"] != offer || e["bookDefault"] != true {
				t.Fatalf("only the offer is today's price: %v", e)
			}
		}
	}
	if current != 1 {
		t.Fatalf("one current price on the bike, got %d", current)
	}
	pv := call(t, "POST", base+"/pricing/preview", boss, map[string]any{"lines": []any{map[string]any{"itemId": bike, "quantity": 1}}})
	if pv.str("priceBookId") != offer {
		t.Fatalf("a quote prices from the offer: %s", truncate(pv.Raw, 300))
	}
}
