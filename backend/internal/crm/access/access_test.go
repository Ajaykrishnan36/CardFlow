package access

import (
	"reflect"
	"testing"
)

func TestPERM01_NormalizeImpliesReadAndDropsUnknown(t *testing.T) {
	got := Normalize(Rules{
		Objects:      map[string][]string{"lead": {"update", "fly"}, "spaceship": {"read"}, "account": {}},
		Rows:         map[string]map[string]string{"lead": {"scope": "galaxy"}},
		Capabilities: []string{CapDashboard, "root.everything", CapDashboard},
	})
	if want := []string{"read", "update"}; !reflect.DeepEqual(got.Objects["lead"], want) {
		t.Fatalf("lead actions = %v, want %v", got.Objects["lead"], want)
	}
	if _, ok := got.Objects["spaceship"]; ok {
		t.Fatal("unknown object kept")
	}
	if _, ok := got.Objects["account"]; ok {
		t.Fatal("object with no actions kept")
	}
	if got.Rows["lead"]["scope"] != "own" {
		t.Fatalf("unknown scope should fall back to own, got %q", got.Rows["lead"]["scope"])
	}
	if !reflect.DeepEqual(got.Capabilities, []string{CapDashboard}) {
		t.Fatalf("capabilities = %v", got.Capabilities)
	}
}

func TestPERM01_EndUserPlusPermissionSet(t *testing.T) {
	endUser, _ := FindSystemRole("END_USER")
	set := Rules{Objects: map[string][]string{"lead": {"read"}}, Rows: map[string]map[string]string{"lead": {"scope": "workspace"}},
		Capabilities: []string{CapDashboard}}
	all := map[string]bool{"leads": true, "accounts": true, "contacts": true}
	e := Combine([]grantSource{{"Role: End user", endUser.Rules}, {"Permission set: Leads viewer", set}}, all, nil)

	if !e.Can("lead", "read") || e.Can("lead", "create") || e.Can("account", "read") {
		t.Fatalf("unexpected grants: %+v", e.Objects)
	}
	if e.OwnOnly("lead") {
		t.Fatal("lead scope should be workspace")
	}
	if !e.HasCapability(CapDashboard) || e.HasCapability(CapMetadata) {
		t.Fatalf("capabilities = %+v", e.Capabilities)
	}
	if got := e.Objects["lead"].Sources; !reflect.DeepEqual(got, []string{"Permission set: Leads viewer"}) {
		t.Fatalf("sources = %v", got)
	}
}

func TestPERM01_ModulesGateGrants(t *testing.T) {
	sa, _ := FindSystemRole("SUPER_ADMIN")
	e := Combine([]grantSource{{"Role: Super Admin", sa.Rules}}, map[string]bool{"leads": true}, nil)
	if !e.Can("lead", "delete") {
		t.Fatal("super admin should delete leads")
	}
	if e.Can("account", "read") {
		t.Fatal("accounts module is off: no access expected")
	}
}

func TestPERM01_UnionTakesBroadestScope(t *testing.T) {
	staff, _ := FindSystemRole("STAFF")
	wide := Rules{Objects: map[string][]string{"account": {"read"}}, Rows: map[string]map[string]string{"account": {"scope": "workspace"}}}
	all := map[string]bool{"leads": true, "accounts": true, "contacts": true}
	e := Combine([]grantSource{{"Role: Staff", staff.Rules}, {"Permission set: All accounts", wide}}, all, nil)
	if e.OwnOnly("account") {
		t.Fatal("permission set should widen accounts to workspace scope")
	}
	if !e.OwnOnly("lead") {
		t.Fatal("staff leads stay own-scope")
	}
	if !e.Can("account", "update") {
		t.Fatal("staff update on accounts should remain")
	}
}

func TestPERM03_ExceedsNamesEveryGrantBeyondTheLimit(t *testing.T) {
	limitRules := Rules{Objects: map[string][]string{"lead": {"read", "create"}, "account": {"read"}},
		Rows: map[string]map[string]string{"lead": {"scope": "workspace"}, "account": {"scope": "own"}}, Capabilities: []string{CapAccessManage}}
	all := map[string]bool{"leads": true, "accounts": true, "contacts": true}
	limit := Combine([]grantSource{{"Role: Delegate", limitRules}}, all, nil)

	ok := Rules{Objects: map[string][]string{"lead": {"create"}}, Rows: map[string]map[string]string{"lead": {"scope": "workspace"}}}
	if got := Exceeds(ok, limit); len(got) != 0 {
		t.Fatalf("within limit, got %v", got)
	}
	tooMuch := Rules{
		Objects:      map[string][]string{"lead": {"delete"}, "account": {"read"}, "contact": {"read"}},
		Rows:         map[string]map[string]string{"account": {"scope": "workspace"}},
		Capabilities: []string{CapMembersManage},
	}
	want := []string{"Leads: Delete", "Accounts: all records", "Contacts: View", "Manage users"}
	if got := Exceeds(tooMuch, limit); !reflect.DeepEqual(got, want) {
		t.Fatalf("exceeds = %v, want %v", got, want)
	}
	if Exceeds(tooMuch, nil) != nil {
		t.Fatal("the owner (nil limit) can grant anything")
	}
}

func TestPERM03_AsRulesRoundTripsWithinItself(t *testing.T) {
	sa, _ := FindSystemRole("SUPER_ADMIN")
	e := Combine([]grantSource{{"Role: Super Admin", sa.Rules}}, map[string]bool{"leads": true, "accounts": true, "contacts": true}, nil)
	if got := Exceeds(e.AsRules(), e); len(got) != 0 {
		t.Fatalf("a holder can always grant their own access, got %v", got)
	}
}
