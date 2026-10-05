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
}
