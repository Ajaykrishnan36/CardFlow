package e2e

import (
	"context"
	"testing"
)

// The owner's one-time clean-up (D-129): the two named test businesses and the connector's
// workspace go, with a copy kept; every other business is untouched; Ajay traders gets
// sample records. Runs last: it retires the connector for this database.
func TestRetireTestWorkspacesTouchesNothingElse(t *testing.T) {
	ctx := context.Background()
	count := func(sql string, args ...any) int {
		var n int
		if err := testDB.Pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	// Nothing to remove: nothing happens, and nothing is marked as done.
	if crmMod.RetireTestWorkspaces(ctx) {
		t.Fatal("the clean-up must not run in a database without the named test businesses")
	}
	a := signIn(t, freshPhone(), "Gova")
	gova := newBusiness(t, a, "Gova test")
	b := signIn(t, freshPhone(), "Surya")
	surya := newBusiness(t, b, "surya work space")
	k := signIn(t, freshPhone(), "Keeper")
	keeper := newBusiness(t, k, "Keeper Co")
	if gova != "gova-test" || surya != "surya-work-space" {
		t.Fatalf("business codes: %q %q", gova, surya)
	}
	create(t, a, gova, "leads", map[string]any{"lastName": "Gova lead"})
	acc := create(t, a, gova, "accounts", map[string]any{"name": "Gova account"})
	create(t, a, gova, "opportunities", map[string]any{"name": "Gova deal", "accountId": acc, "closeDate": today(), "amount": 10})
	create(t, b, surya, "contacts", map[string]any{"lastName": "Surya contact"})
	keptLead := create(t, k, keeper, "leads", map[string]any{"lastName": "Kept lead"})
	keptInv := create(t, k, keeper, "invoices", map[string]any{"name": "Kept invoice", "total": 500, "status": "sent"})
	people := count(`SELECT count(*) FROM crm.identities`)
	cards := count(`SELECT count(*) FROM public.saved_cards`)

	if !crmMod.RetireTestWorkspaces(ctx) {
		t.Fatal("the clean-up should have run")
	}
	if n := count(`SELECT count(*) FROM crm.workspaces WHERE code IN ('gova-test', 'surya-work-space', 'business-card-snap')`); n != 0 {
		t.Fatalf("%d of the removed businesses are still there", n)
	}
	for _, table := range []string{"leads", "accounts", "contacts", "object_records", "memberships", "activities", "audit_events"} {
		if n := count(`SELECT count(*) FROM crm.` + table + ` t WHERE NOT EXISTS (SELECT 1 FROM crm.workspaces w WHERE w.id = t.workspace_id) AND t.workspace_id IS NOT NULL`); n != 0 {
			t.Errorf("%d rows of removed businesses left in crm.%s", n, table)
		}
	}
	// A copy of what was removed is kept.
	if n := count(`SELECT count(*) FROM crm.deleted_workspace_archive WHERE workspace_code = 'gova-test' AND table_name IN ('crm.leads', 'crm.accounts', 'crm.object_records', 'crm.workspaces')`); n < 4 {
		t.Fatalf("the removed business's rows should be archived, found %d", n)
	}
	// Their listings left the directory.
	if n := count(`SELECT count(*) FROM public.businesses WHERE name IN ('Gova test', 'surya work space') AND deleted_at IS NULL`); n != 0 {
		t.Fatalf("%d listings of removed businesses are still public", n)
	}
	// Everyone still exists and can sign in; the people of the removed businesses simply have no business.
	if n := count(`SELECT count(*) FROM crm.identities`); n != people {
		t.Fatalf("people before %d, after %d: nobody should be removed", people, n)
	}
	if n := count(`SELECT count(*) FROM public.saved_cards`); n != cards {
		t.Fatalf("saved cards before %d, after %d", cards, n)
	}
	if r := call(t, "GET", crmAPI+"/w/"+gova+"/crm/leads", a, nil); r.Status != 403 && r.Status != 404 {
		t.Fatalf("the removed business must not open: %d", r.Status)
	}
	if list := call(t, "GET", crmAPI+"/businesses", a, nil); len(list.list("data")) != 0 {
		t.Fatalf("its owner should have no business left: %s", truncate(list.Raw, 200))
	}
	// The business that wasn't named is exactly as it was.
	want(t, call(t, "GET", crmAPI+"/w/"+keeper+"/crm/leads/"+keptLead, k, nil), 200, "the other business's lead")
	if v := get(t, k, keeper, "invoices", keptInv); v["total"] != float64(500) || v["balanceDue"] != float64(500) {
		t.Fatalf("the other business's invoice changed: %v", v)
	}
	if total(t, k, keeper, "leads", "") != 1 {
		t.Fatalf("the other business got records it didn't have")
	}
	// Ajay traders has its sample records.
	if n := count(`SELECT count(*) FROM crm.leads l JOIN crm.workspaces w ON w.id = l.workspace_id WHERE w.code = 'ajay-traders'`); n != 2 {
		t.Fatalf("Ajay traders should have 2 sample leads, has %d", n)
	}
	// Once only; and the connector's workspace is not brought back.
	if crmMod.RetireTestWorkspaces(ctx) {
		t.Fatal("the clean-up must run once")
	}
	c := signIn(t, freshPhone(), "Later")
	if code := newBusiness(t, c, "Gova test"); code == "" {
		t.Fatal("the name can be used again")
	}
	if crmMod.RetireTestWorkspaces(ctx) {
		t.Fatal("a business created later with the same name is not removed")
	}
}
