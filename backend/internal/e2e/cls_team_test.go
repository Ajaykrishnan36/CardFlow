package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// A business sets up its own sales team (Cloudlight QA run, D-182): roles it made itself can
// be given to people, an invited number signs in on the business's own address, and an admin
// can't attach a number of their choosing to somebody else's login.
func TestBusinessTeamWithCustomRolesAndPhoneInvites(t *testing.T) {
	ctx := context.Background()
	boss := signIn(t, freshPhone(), "Team Boss")
	ws := newBusiness(t, boss, "Team Roles Traders")
	admin := crmAPI + "/w/" + ws + "/admin"

	opts := call(t, "GET", admin+"/options", boss, nil)
	want(t, opts, 200, "admin options")
	var adminRole, salesSet string
	for _, r := range opts.Body["roles"].([]any) {
		if m := r.(map[string]any); m["key"] == "ADMIN" {
			adminRole = m["id"].(string)
		}
	}
	for _, p := range opts.Body["permissionSets"].([]any) {
		if m := p.(map[string]any); m["name"] == "Staff access" {
			salesSet = m["id"].(string)
		}
	}
	mgr := call(t, "POST", admin+"/roles", boss, map[string]any{"name": "Global Sales Manager", "parentRoleId": adminRole})
	want(t, mgr, 201, "create a manager role")
	exec := call(t, "POST", admin+"/roles", boss, map[string]any{"name": "Sales Exec Europe", "parentRoleId": mgr.str("id")})
	want(t, exec, 201, "create a role under the manager")

	invite := func(name, email, phone, role string) resp {
		return call(t, "POST", admin+"/members", boss, map[string]any{"displayName": name, "email": email, "phone": phone,
			"roleKey": role, "method": "invite", "userType": "team_member", "permissionSetIds": []string{salesSet}})
	}
	mail := func(s string) string { return fmt.Sprintf("%s-%s@example.com", s, strings.ToLower(ws)) }

	// 1. Custom roles can be given to a Team member; a built-in role the type doesn't allow can't.
	execPhone := freshPhone()
	want(t, invite("Eva Exec", mail("eva"), execPhone, exec.str("key")), 201, "invite with a custom role two levels down")
	want(t, invite("Sid Sales", mail("sid"), freshPhone(), "SALES_STAFF"), 201, "a Team member can be Sales staff")
	want(t, invite("Sue Service", mail("sue"), freshPhone(), "SERVICE_STAFF"), 201, "a Team member can be Service staff")
	if r := invite("End User", mail("end"), "", "END_USER"); r.Status != 422 {
		t.Fatalf("a Team member still can't be an End user: %d %s", r.Status, truncate(r.Raw, 200))
	}

	// 2. The invited number signs in on the business's own address and the invitation is accepted.
	in := signInAt(t, execPhone, ws)
	want(t, in, 200, "invited person signs in at the business URL")
	want(t, call(t, "GET", crmAPI+"/w/"+ws+"/crm/leads", in.str("token"), nil), 200, "the invited exec opens the business")

	// 3. Someone who already signs in elsewhere keeps only their own number.
	otherBoss := signIn(t, freshPhone(), "Other Boss")
	other := newBusiness(t, otherBoss, "Other Traders")
	victimPhone := freshPhone()
	want(t, call(t, "POST", crmAPI+"/w/"+other+"/admin/members", otherBoss, map[string]any{"displayName": "Vic Tim", "email": mail("vic"),
		"phone": victimPhone, "roleKey": "STAFF", "method": "invite", "userType": "team_member"}), 201, "invite the victim elsewhere")
	want(t, signInAt(t, victimPhone, other), 200, "victim signs in to their own business")

	attacker := freshPhone()
	want(t, invite("Vic Tim", mail("vic"), attacker, "STAFF"), 201, "invite the victim's email with another number")
	var n int
	if err := testDB.Pool.QueryRow(ctx, `
		SELECT count(*) FROM crm.verified_identifiers p JOIN crm.verified_identifiers e ON e.identity_id = p.identity_id
		WHERE e.kind = 'email' AND e.value_normalized = $1 AND p.kind = 'phone' AND p.value_normalized LIKE '%' || $2`, mail("vic"), attacker).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("an admin must not add their own number to someone else's login")
	}
}
