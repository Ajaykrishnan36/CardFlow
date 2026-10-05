package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// The owner can add a phone number when inviting an admin; it is optional, checked, and
// never taken from somebody else.
func TestOwnerInviteCarriesPhone(t *testing.T) {
	ctx := context.Background()
	taken := freshPhone()
	token := signIn(t, taken, "Invite Owner")
	ws := newBusiness(t, token, "Invite Traders")
	owner := ownerSignIn(t)
	var wsID string
	if err := testDB.Pool.QueryRow(ctx, `SELECT id::text FROM crm.workspaces WHERE code = $1`, ws).Scan(&wsID); err != nil {
		t.Fatal(err)
	}
	invite := func(email, phone string) resp {
		body := map[string]any{"name": "Asha Admin", "email": email, "roleKey": "ADMIN"}
		if phone != "" {
			body["phone"] = phone
		}
		return call(t, "POST", crmAPI+"/platform/workspaces/"+wsID+"/invitations", owner, body)
	}
	mail := func(n int) string { return fmt.Sprintf("invite-phone-%d-%s@example.com", n, strings.ToLower(ws)) }

	if r := invite(mail(1), "12"); r.Status != 422 && r.Status != 400 {
		t.Fatalf("a bad phone number is refused: %d %s", r.Status, truncate(r.Raw, 200))
	}
	if r := invite(mail(1), taken); (r.Status != 422 && r.Status != 400) || !strings.Contains(r.Raw, "phone") {
		t.Fatalf("somebody else's number is refused: %d %s", r.Status, truncate(r.Raw, 200))
	}
	var left int
	_ = testDB.Pool.QueryRow(ctx, `SELECT count(*) FROM crm.verified_identifiers WHERE value_normalized = $1`, mail(1)).Scan(&left)
	if left != 0 {
		t.Fatalf("a refused invitation leaves nothing behind")
	}

	phone := freshPhone()
	want(t, invite(mail(1), phone), 201, "invite with a phone number")
	var n int
	if err := testDB.Pool.QueryRow(ctx, `
		SELECT count(*) FROM crm.verified_identifiers e
		JOIN crm.verified_identifiers p ON p.identity_id = e.identity_id AND p.kind = 'phone'
		WHERE e.kind = 'email' AND e.value_normalized = $1 AND p.value_normalized LIKE '%' || $2`, mail(1), phone).Scan(&n); err != nil || n != 1 {
		t.Fatalf("the phone number is saved on the invited person: n=%d err=%v", n, err)
	}
	// Inviting again with their own number is fine; without a number still works.
	want(t, invite(mail(1), phone), 201, "re-invite with the same number")
	want(t, invite(mail(2), ""), 201, "invite without a phone number")

	// Signing in with a code sent to that number accepts the invitation: the business opens.
	status := func() (m, inv string) {
		_ = testDB.Pool.QueryRow(ctx, `
			SELECT m.status, (SELECT i.status FROM crm.invitations i WHERE i.membership_id = m.id ORDER BY i.created_at DESC LIMIT 1)
			FROM crm.memberships m JOIN crm.verified_identifiers e ON e.identity_id = m.identity_id
			WHERE m.workspace_id = $1::uuid AND e.kind = 'email' AND e.value_normalized = $2`, wsID, mail(1)).Scan(&m, &inv)
		return
	}
	if m, _ := status(); m != "invited" {
		t.Fatalf("before signing in the person is only invited, got %q", m)
	}
	invited := signIn(t, phone, "")
	if m, inv := status(); m != "active" || inv != "accepted" {
		t.Fatalf("phone sign-in accepts the invitation: membership %q, invitation %q", m, inv)
	}
	want(t, call(t, "GET", crmAPI+"/w/"+ws+"/crm/leads", invited, nil), 200, "the invited admin opens the business")
	var role string
	_ = testDB.Pool.QueryRow(ctx, `
		SELECT r.key FROM crm.role_assignments ra JOIN crm.roles r ON r.id = ra.role_id
		JOIN crm.memberships m ON m.id = ra.membership_id JOIN crm.verified_identifiers e ON e.identity_id = m.identity_id
		WHERE m.workspace_id = $1::uuid AND e.value_normalized = $2`, wsID, mail(1)).Scan(&role)
	if role != "ADMIN" {
		t.Fatalf("they get the invited role, got %q", role)
	}
	// Someone invited by email only is not let in by an unrelated phone sign-in.
	var other string
	_ = testDB.Pool.QueryRow(ctx, `
		SELECT m.status FROM crm.memberships m JOIN crm.verified_identifiers e ON e.identity_id = m.identity_id
		WHERE m.workspace_id = $1::uuid AND e.value_normalized = $2`, wsID, mail(2)).Scan(&other)
	if other != "invited" {
		t.Fatalf("the email-only invitation is still waiting, got %q", other)
	}
}
