package e2e

import (
	"strings"
	"testing"
)

// An Admin adding a Super Admin is told plainly why not, not handed a list of permissions.
func TestAdminIsToldWhyItCannotMakeASuperAdmin(t *testing.T) {
	boss := signIn(t, freshPhone(), "Rank Boss")
	ws := newBusiness(t, boss, "Rank Motors")
	adminPhone := freshPhone()
	want(t, call(t, "POST", crmAPI+"/w/"+ws+"/admin/members", boss, map[string]any{"displayName": "Anu", "phone": adminPhone, "roleKey": "ADMIN"}), 201, "add the Admin")
	admin := signIn(t, adminPhone, "Anu")
	r := call(t, "POST", crmAPI+"/w/"+ws+"/admin/members", admin, map[string]any{"displayName": "Too High", "phone": freshPhone(), "roleKey": "SUPER_ADMIN"})
	want(t, r, 403, "an Admin making a Super Admin")
	if r.str("message") != "You can't give a role that ranks above your own." {
		t.Fatalf("the refusal says why: %s", truncate(r.Raw, 300))
	}
	want(t, call(t, "POST", crmAPI+"/w/"+ws+"/admin/members", admin, map[string]any{"displayName": "Staff", "phone": freshPhone(), "roleKey": "SALES_STAFF"}), 201, "an Admin adds sales staff")
	if strings.Contains(r.Raw, "Delete permanently") {
		t.Fatal("no permission list for a role refusal")
	}
}
