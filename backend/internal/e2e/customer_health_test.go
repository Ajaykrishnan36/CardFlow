package e2e

import (
	"testing"
	"time"
)

// Customer health (D-176): each account with its open pipeline, a worked-out score, 12 weeks
// of engagement and the last touch; one account's heatmap and key contacts.
func TestCustomerHealth(t *testing.T) {
	boss, ws := packBusiness(t, "Health Traders", "Retail")
	base := crmAPI + "/w/" + ws
	acc := create(t, boss, ws, "accounts", map[string]any{"name": "Busy Buyer Pvt Ltd"})
	quiet := create(t, boss, ws, "accounts", map[string]any{"name": "Quiet Buyer"})
	create(t, boss, ws, "contacts", map[string]any{"firstName": "Meera", "lastName": "Nair", "accountId": acc, "title": "Purchase head"})
	create(t, boss, ws, "opportunities", map[string]any{"name": "Busy order", "accountId": acc, "amount": 120000, "closeDate": time.Now().AddDate(0, 0, 20).Format("2006-01-02"), "status": "proposal"})
	r := call(t, "GET", base+"/customers/health", boss, nil)
	want(t, r, 200, "customer health")
	got := map[string]map[string]any{}
	for _, x := range r.list("data") {
		m := x.(map[string]any)
		got[m["id"].(string)] = m
	}
	busy, still := got[acc], got[quiet]
	if busy == nil || still == nil || busy["openValue"].(float64) != 120000 || busy["openDeals"].(float64) != 1 || len(busy["engagement"].([]any)) != 12 {
		t.Fatalf("the busy account's pipeline and engagement: %v", busy)
	}
	if busy["touches"].(float64) < 3 || busy["score"].(float64) <= still["score"].(float64) || busy["lastTouch"] == nil {
		t.Fatalf("activity on the account, its contact and its deal raise its score: busy %v quiet %v", busy, still)
	}
	d := call(t, "GET", base+"/customers/health?account="+acc, boss, nil)
	want(t, d, 200, "one account")
	if len(d.list("detail", "heatmap")) != 18*7 || len(d.list("detail", "contacts")) != 1 {
		t.Fatalf("the heatmap and the key contacts: %s", truncate(d.Raw, 400))
	}
}
