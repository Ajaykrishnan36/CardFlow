package e2e

import (
	"testing"
	"time"
)

// The owner's dashboard (D-176): eight numbers against the period before, the funnel by
// stage, win rate and revenue — counted from the records.
func TestDashboardOverview(t *testing.T) {
	boss, ws := packBusiness(t, "Overview Traders", "Retail")
	base := crmAPI + "/w/" + ws
	today := time.Now().Format("2006-01-02")
	create(t, boss, ws, "leads", map[string]any{"lastName": "Lead One"})
	create(t, boss, ws, "leads", map[string]any{"lastName": "Lead Two"})
	create(t, boss, ws, "opportunities", map[string]any{"name": "Open deal", "amount": 50000, "closeDate": today, "status": "proposal"})
	won := create(t, boss, ws, "opportunities", map[string]any{"name": "Won deal", "amount": 80000, "closeDate": today, "status": "qualification"})
	want(t, edit(t, boss, ws, "opportunities", won, map[string]any{"status": "closed_won"}), 200, "win the deal")
	create(t, boss, ws, "payments", map[string]any{"amount": 30000, "paymentDate": today, "status": "paid", "paymentMethod": "cash"})

	r := call(t, "GET", base+"/dashboard/overview?range=month", boss, nil)
	want(t, r, 200, "the overview")
	kpi := map[string]float64{}
	for _, x := range r.list("kpis") {
		m := x.(map[string]any)
		kpi[m["key"].(string)] = m["value"].(float64)
		if len(m["trend"].([]any)) != 8 {
			t.Fatalf("each number has an 8-step trend: %v", m)
		}
	}
	if kpi["leads"] != 2 || kpi["openOpportunities"] != 1 || kpi["won"] != 1 || kpi["revenue"] != 30000 {
		t.Fatalf("leads, open deals, won and revenue are counted: %v", kpi)
	}
	stages := map[string]float64{}
	for _, x := range r.list("funnel") {
		m := x.(map[string]any)
		stages[m["key"].(string)] = m["count"].(float64)
	}
	if stages["leads"] != 2 || stages["proposal"] != 1 || stages["closed_won"] != 1 {
		t.Fatalf("the funnel counts each stage: %v", stages)
	}
	if r.at("winRate").(float64) != 100 || r.at("wonValue").(float64) != 80000 || r.at("openPipeline", "value").(float64) != 50000 {
		t.Fatalf("win rate, won value and the open pipeline: %s", truncate(r.Raw, 500))
	}
	if len(r.list("activity")) == 0 || len(r.list("leaderboard")) != 1 {
		t.Fatalf("recent activity and the leaderboard: %s", truncate(r.Raw, 400))
	}
	want(t, call(t, "GET", base+"/dashboard/overview?range=year", boss, nil), 200, "a year")
	if y := call(t, "GET", base+"/dashboard/overview?range=year", boss, nil); len(y.list("trendLabels")) != 12 {
		t.Fatalf("a year is shown by month: %s", truncate(y.Raw, 200))
	}
}
