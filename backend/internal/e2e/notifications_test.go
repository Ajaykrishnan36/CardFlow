package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"cardflow-backend/internal/crm/billing"
)

// inbox reads someone's bell in a business, waiting a little for the relay to deliver
// event notifications until ok says the list is as expected.
func inbox(t *testing.T, token, ws, query string, ok func(resp) bool) resp {
	t.Helper()
	var r resp
	for i := 0; i < 50; i++ {
		r = call(t, "GET", crmAPI+"/w/"+ws+"/notifications"+query, token, nil)
		want(t, r, 200, "notifications")
		if ok == nil || ok(r) {
			return r
		}
		time.Sleep(100 * time.Millisecond)
	}
	return r
}

// titled says whether the list holds a notification whose title contains s.
func titled(s string) func(resp) bool {
	return func(r resp) bool { return findNote(r, s) != nil }
}

func findNote(r resp, s string) map[string]any {
	for _, n := range r.list("data") {
		m := n.(map[string]any)
		if strings.Contains(m["title"].(string), s) {
			return m
		}
	}
	return nil
}

// The bell tells the business what happened (D-169): a member added and their first
// sign-in, a deal won, work booked for a service person, a record assigned; paid time
// running out (once per step); Today / unread counts; read and clear, one or all; and
// only this business's notifications.
func TestBellTellsTheBusiness(t *testing.T) {
	ctx := context.Background()
	boss := signIn(t, freshPhone(), "Bell Boss")
	ws := newBusiness(t, boss, "Bell Motors")
	base := crmAPI + "/w/" + ws

	adminPhone, svcPhone := freshPhone(), freshPhone()
	want(t, call(t, "POST", base+"/admin/members", boss, map[string]any{"displayName": "Bell Admin", "phone": adminPhone, "roleKey": "ADMIN"}), 201, "add the admin")
	admin := signIn(t, adminPhone, "Bell Admin")
	if r := inbox(t, admin, ws, "", titled("You were added to Bell Motors")); findNote(r, "You were added to Bell Motors") == nil {
		t.Fatalf("the new member is welcomed: %s", truncate(r.Raw, 400))
	}
	if r := inbox(t, boss, ws, "", titled("Bell Admin signed in for the first time")); findNote(r, "Bell Admin signed in for the first time") == nil {
		t.Fatalf("whoever added them hears of the first sign-in: %s", truncate(r.Raw, 400))
	}
	want(t, call(t, "POST", base+"/admin/members", boss, map[string]any{"displayName": "Bell Service", "phone": svcPhone, "roleKey": "STAFF"}), 201, "add the service person")
	if r := inbox(t, admin, ws, "", titled("Bell Boss added Bell Service")); findNote(r, "Bell Boss added Bell Service") == nil {
		t.Fatalf("the other leaders hear a member was added: %s", truncate(r.Raw, 400))
	}
	svc := signIn(t, svcPhone, "Bell Service")
	svcID := call(t, "GET", crmAPI+"/me", svc, nil).str("identity", "id")
	inbox(t, boss, ws, "", titled("Bell Service signed in for the first time"))
	if r := inbox(t, admin, ws, "", titled("Bell Service signed in for the first time")); findNote(r, "Bell Service signed in") == nil {
		t.Fatalf("leaders hear of the first sign-in: %s", truncate(r.Raw, 400))
	}
	signIn(t, svcPhone, "Bell Service") // a second sign-in is not news
	time.Sleep(300 * time.Millisecond)
	n := 0
	for _, x := range inbox(t, admin, ws, "", nil).list("data") {
		if strings.Contains(x.(map[string]any)["title"].(string), "Bell Service signed in") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("only the first sign-in is told: %d", n)
	}

	// A sale: the admin hears that the boss won a deal.
	opp := create(t, boss, ws, "opportunities", map[string]any{"name": "Senthil — Splendor+", "amount": 96722.08, "closeDate": "2026-11-05"})
	want(t, edit(t, boss, ws, "opportunities", opp, map[string]any{"status": "closed_won"}), 200, "win the deal")
	r := inbox(t, admin, ws, "", titled("Deal won: Senthil — Splendor+"))
	if d := findNote(r, "Deal won"); d == nil || !strings.Contains(d["body"].(string), "₹96,722.08") {
		t.Fatalf("leaders hear of a won deal with its amount: %s", truncate(r.Raw, 500))
	}

	// Work booked on the service person, and a case assigned to them.
	res := create(t, boss, ws, "service_resources", map[string]any{"name": "Bell Service", "resourceType": "technician", "userId": svcID, "status": "active"})
	create(t, boss, ws, "work_orders", map[string]any{"name": "Brake check TN 37 DK 4521", "resourceId": res})
	if r := inbox(t, svc, ws, "", titled("booked you for a job card")); findNote(r, "booked you for a job card") == nil {
		t.Fatalf("the service person hears of the job card: %s", truncate(r.Raw, 400))
	}
	c := create(t, boss, ws, "cases", map[string]any{"name": "Brake noise"})
	want(t, edit(t, boss, ws, "cases", c, map[string]any{"ownerId": svcID}), 200, "assign the case")
	if r := inbox(t, svc, ws, "", titled("assigned you a Case")); findNote(r, "assigned you a Case") == nil {
		t.Fatalf("the case owner is told: %s", truncate(r.Raw, 400))
	}
	sm := call(t, "GET", base+"/dashboard/summary?range=today", svc, nil)
	want(t, sm, 200, "the service person's home")
	mine := map[string]float64{}
	for _, x := range sm.list("mine") {
		m := x.(map[string]any)
		mine[m["object"].(string)] = m["count"].(float64)
	}
	if mine["cases"] != 1 || mine["work_orders"] != 1 {
		t.Fatalf("home counts the case and the job card assigned to them: %v", mine)
	}

	// Counts, read and clear.
	r = inbox(t, svc, ws, "?today=1", nil)
	if r.at("today").(float64) < 3 || r.at("unread").(float64) < 3 || len(r.list("data")) != int(r.at("today").(float64)) {
		t.Fatalf("today and unread counts: %s", truncate(r.Raw, 300))
	}
	first := findNote(r, "assigned you a Case")["id"]
	r = call(t, "POST", base+"/notifications/read", svc, map[string]any{"ids": []any{first}})
	want(t, r, 200, "mark one read")
	if findNote(r, "assigned you a Case")["readAt"] == nil {
		t.Fatalf("one notification marked read: %s", truncate(r.Raw, 300))
	}
	before := r.at("unread").(float64)
	r = call(t, "POST", base+"/notifications/read?unread=1", svc, map[string]any{"all": true})
	if r.at("unread").(float64) != 0 || len(r.list("data")) != 0 || before == 0 {
		t.Fatalf("mark all read: %s", truncate(r.Raw, 300))
	}
	total := r.at("total").(float64)
	r = call(t, "POST", base+"/notifications/clear", svc, map[string]any{"ids": []any{first}})
	if r.at("total").(float64) != total-1 || findNote(r, "assigned you a Case") != nil {
		t.Fatalf("clear one: %s", truncate(r.Raw, 300))
	}
	r = call(t, "POST", base+"/notifications/clear", svc, map[string]any{"all": true})
	if r.at("total").(float64) != 0 || len(r.list("data")) != 0 {
		t.Fatalf("clear all: %s", truncate(r.Raw, 300))
	}

	// Another business of the boss's keeps its own bell.
	other := newBusiness(t, boss, "Bell Other")
	if r := inbox(t, boss, other, "", nil); len(r.list("data")) != 0 {
		t.Fatalf("another business's bell is empty: %s", truncate(r.Raw, 300))
	}

	// Paid time running out: told 7, 5, 3 and 1 day before and when it ends, each once.
	crmMod.Billing().UseConfig(billing.Config{KeyID: "rzp_test_e2e", KeySecret: "e2e-secret"})
	defer crmMod.Billing().UseConfig(billing.Config{})
	setUntil := func(sql string) {
		if _, err := testDB.Pool.Exec(ctx, `UPDATE crm.memberships SET paid_until = `+sql+` WHERE identity_id = $1`, svcID); err != nil {
			t.Fatal(err)
		}
	}
	count := func(title string) int {
		var n int
		_ = testDB.Pool.QueryRow(ctx, `SELECT count(*) FROM crm.notifications WHERE identity_id IN (SELECT id FROM crm.identities WHERE display_name IN ('Bell Service', 'Bell Boss')) AND title = $1`, title).Scan(&n)
		return n
	}
	setUntil(`now() + interval '6 days'`)
	crmMod.Sweep(ctx)
	crmMod.Sweep(ctx) // the sweep runs hourly: nothing twice
	if count("Your access ends in 7 days") != 1 || count("Bell Service’s access ends in 7 days") != 1 {
		t.Fatalf("7 days before: the person and the Super Admin, once each: %d / %d", count("Your access ends in 7 days"), count("Bell Service’s access ends in 7 days"))
	}
	setUntil(`now() + interval '20 hours'`)
	crmMod.Sweep(ctx)
	setUntil(`now() - interval '1 hour'`)
	crmMod.Sweep(ctx)
	if count("Your access ends in 1 day") != 1 || count("Your access has ended") != 1 || count("Bell Service’s access has ended") != 1 {
		t.Fatalf("1 day before and at the end: %d / %d / %d", count("Your access ends in 1 day"), count("Your access has ended"), count("Bell Service’s access has ended"))
	}
}
