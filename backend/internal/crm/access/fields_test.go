package access

import "testing"

func TestFieldAccessCombinesMostOpen(t *testing.T) {
	role := Rules{Objects: map[string][]string{"lead": {"read", "update"}},
		Fields: map[string]map[string]string{"lead": {"email": FieldHidden, "phone": FieldRead, "title": FieldHidden}}}
	set := Rules{Objects: map[string][]string{"lead": {"read"}},
		Fields: map[string]map[string]string{"lead": {"email": FieldRead, "title": FieldHidden}}}
	// A set that doesn't grant leads never opens up lead fields.
	other := Rules{Objects: map[string][]string{"account": {"read"}}}
	e := Combine([]grantSource{{"role", role}, {"set", set}, {"other", other}}, map[string]bool{"leads": true, "accounts": true}, nil)
	if got := e.FieldLevel("lead", "email"); got != FieldRead {
		t.Errorf("email = %s, want read (most open of hidden/read)", got)
	}
	if got := e.FieldLevel("lead", "phone"); got != FieldEdit {
		t.Errorf("phone = %s, want edit (the set doesn't restrict it)", got)
	}
	if got := e.FieldLevel("lead", "title"); got != FieldHidden {
		t.Errorf("title = %s, want hidden (every grant hides it)", got)
	}
	if got := e.FieldLevel("lead", "status"); got != FieldEdit {
		t.Errorf("status = %s, want edit", got)
	}
}

func TestExceedsFieldAccess(t *testing.T) {
	limit := Combine([]grantSource{{"admin", Rules{Objects: map[string][]string{"lead": {"read", "update"}},
		Fields: map[string]map[string]string{"lead": {"email": FieldHidden, "phone": FieldRead}}}}}, map[string]bool{"leads": true}, nil)
	ok := Rules{Objects: map[string][]string{"lead": {"read"}}, Fields: map[string]map[string]string{"lead": {"email": FieldHidden, "phone": FieldHidden}}}
	if out := Exceeds(ok, limit); len(out) != 0 {
		t.Fatalf("within limit, got %v", out)
	}
	tooMuch := Rules{Objects: map[string][]string{"lead": {"read"}}, Fields: map[string]map[string]string{"lead": {"phone": FieldRead}}}
	if out := Exceeds(tooMuch, limit); len(out) != 1 {
		t.Fatalf("email opened up beyond the admin's own access should be flagged once, got %v", out)
	}
}

func TestNormalizeDropsFieldsOfUngrantedObjects(t *testing.T) {
	r := Normalize(Rules{Objects: map[string][]string{"lead": {"read"}},
		Fields: map[string]map[string]string{"lead": {"email": FieldHidden, "bad key!": FieldHidden, "phone": "weird"}, "account": {"name": FieldHidden}}})
	if len(r.Fields) != 1 || len(r.Fields["lead"]) != 1 || r.Fields["lead"]["email"] != FieldHidden {
		t.Fatalf("unexpected %v", r.Fields)
	}
}
