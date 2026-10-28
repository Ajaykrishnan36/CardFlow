package e2e

import "testing"

// The Owner / Assigned to pickers list only the business's own people — also when the
// platform owner is looking in — never someone from another business or the owner console.
func TestPeoplePickerListsOnlyThisBusiness(t *testing.T) {
	a := signIn(t, freshPhone(), "Picker Alpha Boss")
	wsA := newBusiness(t, a, "Picker Alpha")
	b := signIn(t, freshPhone(), "Picker Beta Boss")
	newBusiness(t, b, "Picker Beta")

	names := func(token string) map[string]bool {
		r := call(t, "GET", crmAPI+"/w/"+wsA+"/lookup/users?q=Picker", token, nil)
		want(t, r, 200, "people lookup")
		out := map[string]bool{}
		for _, v := range r.list("data") {
			out[v.(map[string]any)["label"].(string)] = true
		}
		return out
	}

	got := names(a)
	if !got["Picker Alpha Boss"] || got["Picker Beta Boss"] {
		t.Fatalf("member lookup: want only this business's people, got %v", got)
	}
	got = names(ownerSignIn(t))
	if !got["Picker Alpha Boss"] || got["Picker Beta Boss"] {
		t.Fatalf("owner looking in: want only this business's people, got %v", got)
	}
}
