package e2e

import (
	"context"
	"strings"
	"testing"
)

// The database itself refuses the mistakes the schema review found (D-136): a reference to
// the wrong kind of record, to another business's record, or to a business that isn't there.
func TestDatabaseIntegrityGuards(t *testing.T) {
	ctx := context.Background()
	a := signIn(t, freshPhone(), "Owner A")
	wsA := newBusiness(t, a, "Integrity A")
	b := signIn(t, freshPhone(), "Owner B")
	wsB := newBusiness(t, b, "Integrity B")
	id := func(code string) string {
		var s string
		if err := testDB.Pool.QueryRow(ctx, `SELECT id::text FROM crm.workspaces WHERE code = $1`, code).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	A := id(wsA)
	refused := func(what, sql string, args ...any) {
		t.Helper()
		_, err := testDB.Pool.Exec(ctx, sql, args...)
		if err == nil {
			t.Fatalf("%s: the database accepted it", what)
		}
		if !strings.Contains(err.Error(), "23503") && !strings.Contains(strings.ToLower(err.Error()), "foreign key") && !strings.Contains(err.Error(), "must reference") {
			t.Fatalf("%s: refused for another reason: %v", what, err)
		}
	}
	accA := create(t, a, wsA, "accounts", map[string]any{"name": "A's account"})
	accB := create(t, b, wsB, "accounts", map[string]any{"name": "B's account"})
	invA := create(t, a, wsA, "invoices", map[string]any{"name": "Invoice", "total": 100, "status": "sent", "accountId": accA})
	payA := create(t, a, wsA, "payments", map[string]any{"name": "Payment", "amount": 10, "paymentDate": today(), "status": "pending"})
	taskA := create(t, a, wsA, "tasks", map[string]any{"name": "A task"})
	invB := create(t, b, wsB, "invoices", map[string]any{"name": "B invoice", "total": 100, "status": "sent", "accountId": accB})

	// Typed keys: an allocation's invoice must be an invoice.
	refused("allocation pointing at a task", `INSERT INTO crm.payment_allocations (workspace_id, payment_id, invoice_id, amount) VALUES ($1, $2, $3, 1)`, A, payA, taskA)
	// Same tenant: a contact can't sit under another business's account; an allocation can't pay another business's invoice.
	refused("contact under another business's account", `INSERT INTO crm.contacts (workspace_id, last_name, account_id, code) VALUES ($1, 'X', $2, 'C-X1')`, A, accB)
	refused("allocation to another business's invoice", `INSERT INTO crm.payment_allocations (workspace_id, payment_id, invoice_id, amount) VALUES ($1, $2, $3, 1)`, A, payA, invB)
	// Money links inside the record: wrong kind, other business, nothing.
	line := `INSERT INTO crm.object_records (workspace_id, object_key, code, name, custom) VALUES ($1, 'line_items', $2, 'Line', jsonb_build_object('invoiceId', $3::text))`
	refused("line item on a task", line, A, "LI-X1", taskA)
	refused("line item on another business's invoice", line, A, "LI-X2", invB)
	refused("line item on nothing", line, A, "LI-X3", "00000000-0000-0000-0000-000000000001")
	if _, err := testDB.Pool.Exec(ctx, line, A, "LI-OK", invA); err != nil {
		t.Fatalf("a line item on its own business's invoice is fine: %v", err)
	}
	refused("payment moved to another business's invoice", `UPDATE crm.object_records SET custom = custom || jsonb_build_object('invoiceId', $2::text) WHERE id = $1`, payA, invB)
	// Tenant keys: no rows for a business that doesn't exist.
	refused("team of a missing business", `INSERT INTO crm.teams (workspace_id, name) VALUES ('00000000-0000-0000-0000-000000000002', 'Ghosts')`)

	// The books are never erased: an invoice can go to the bin but not be deleted for good.
	want(t, call(t, "DELETE", crmAPI+"/w/"+wsA+"/crm/invoices/"+invA, a, nil), 204, "invoice to the recycle bin")
	d := call(t, "POST", crmAPI+"/w/"+wsA+"/crm/invoices/bulk", a, map[string]any{"action": "destroy", "query": map[string]any{"ids": []string{invA}, "deleted": true}})
	var left int
	_ = testDB.Pool.QueryRow(ctx, `SELECT count(*) FROM crm.object_records WHERE id = $1`, invA).Scan(&left)
	if left != 1 {
		t.Fatalf("a financial record was deleted permanently: %d %s", d.Status, truncate(d.Raw, 300))
	}

	// Two businesses can each have their own "Projects" object.
	oa := call(t, "POST", crmAPI+"/w/"+wsA+"/objects", a, map[string]any{"singular": "Site visit", "plural": "Site visits"})
	ob := call(t, "POST", crmAPI+"/w/"+wsB+"/objects", b, map[string]any{"singular": "Site visit", "plural": "Site visits"})
	if oa.Status != 201 || ob.Status != 201 || oa.str("key") == ob.str("key") || oa.str("prefix") == ob.str("prefix") {
		t.Fatalf("each business gets its own object: %d %q %q / %d %q %q — %s", oa.Status, oa.str("key"), oa.str("prefix"), ob.Status, ob.str("key"), ob.str("prefix"), truncate(ob.Raw, 200))
	}
	recB := call(t, "POST", crmAPI+"/w/"+wsB+"/crm/"+ob.str("key"), b, map[string]any{"values": map[string]any{"name": "Visit 1"}})
	if recB.Status != 200 && recB.Status != 201 {
		t.Fatalf("the second business uses its object: %d %s", recB.Status, truncate(recB.Raw, 200))
	}
	if r := call(t, "GET", crmAPI+"/w/"+wsA+"/crm/"+ob.str("key"), a, nil); r.Status == 200 {
		t.Fatalf("the first business can open the second business's object")
	}
}
