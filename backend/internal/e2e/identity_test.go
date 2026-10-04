package e2e

import (
	"strings"
	"testing"
)

// Phone sign-in (D-93): a correct code signs in and creates the account; a wrong or a
// used code never does; logging out ends the session for both APIs.
func TestPhoneSignIn(t *testing.T) {
	phone := freshPhone()
	r := call(t, "POST", crmAPI+"/auth/phone/request", "", map[string]any{"phone": phone})
	want(t, r, 200, "request code")
	code := r.str("devCode")

	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	bad := call(t, "POST", crmAPI+"/auth/phone/verify", "", map[string]any{"phone": phone, "code": wrong})
	want(t, bad, 401, "wrong code")
	if bad.str("token") != "" {
		t.Fatal("a wrong code returned a session")
	}

	ok := call(t, "POST", crmAPI+"/auth/phone/verify", "", map[string]any{"phone": phone, "code": code})
	want(t, ok, 200, "correct code")
	token := ok.str("token")
	if ok.at("isNewUser") != true || ok.at("hasBusiness") != false {
		t.Fatalf("a new number should be a new user with no business: %s", ok.Raw)
	}

	again := call(t, "POST", crmAPI+"/auth/phone/verify", "", map[string]any{"phone": phone, "code": code})
	want(t, again, 401, "a code is single use")

	me := call(t, "GET", crmAPI+"/me", token, nil)
	want(t, me, 200, "/me")
	if got := me.str("identity", "phone"); got != "+91"+phone {
		t.Fatalf("identity phone = %q", got)
	}

	// The same session works on the app's own API.
	want(t, call(t, "GET", appAPI+"/cards", token, nil), 200, "app API with the unified session")

	want(t, call(t, "POST", crmAPI+"/auth/logout", token, nil), 204, "logout")
	want(t, call(t, "GET", crmAPI+"/me", token, nil), 401, "CRM API after logout")
	want(t, call(t, "GET", appAPI+"/cards", token, nil), 401, "app API after logout")
}

// A second sign-in with the same number is the same identity, not a new one.
func TestPhoneSignInIsOneIdentity(t *testing.T) {
	phone := freshPhone()
	a := call(t, "GET", crmAPI+"/me", signIn(t, phone, "First Person"), nil)
	b := call(t, "GET", crmAPI+"/me", signIn(t, phone, ""), nil)
	if a.str("identity", "id") == "" || a.str("identity", "id") != b.str("identity", "id") {
		t.Fatalf("two sign-ins gave two identities: %s vs %s", a.str("identity", "id"), b.str("identity", "id"))
	}
	if b.str("identity", "displayName") != "First Person" {
		t.Fatalf("name was lost: %q", b.str("identity", "displayName"))
	}
}

// Code requests are limited per number: the fourth request inside ten minutes is refused.
func TestPhoneCodeRequestsAreRateLimited(t *testing.T) {
	phone := freshPhone()
	for i := 0; i < 3; i++ {
		want(t, call(t, "POST", crmAPI+"/auth/phone/request", "", map[string]any{"phone": phone}), 200, "request within the limit")
	}
	r := call(t, "POST", crmAPI+"/auth/phone/request", "", map[string]any{"phone": phone})
	want(t, r, 429, "request over the limit")
}

// Five wrong codes burn the challenge: the right code no longer works afterwards.
func TestWrongCodesLockTheChallenge(t *testing.T) {
	phone := freshPhone()
	r := call(t, "POST", crmAPI+"/auth/phone/request", "", map[string]any{"phone": phone})
	code := r.str("devCode")
	wrong := "123123"
	if code == wrong {
		wrong = "321321"
	}
	for i := 0; i < 5; i++ {
		call(t, "POST", crmAPI+"/auth/phone/verify", "", map[string]any{"phone": phone, "code": wrong})
	}
	after := call(t, "POST", crmAPI+"/auth/phone/verify", "", map[string]any{"phone": phone, "code": code})
	if after.Status == 200 {
		t.Fatal("the correct code worked after five wrong attempts")
	}
}

// A person with no business can sign in, but can't reach anyone's CRM data.
func TestNoBusinessMeansNoCRMAccess(t *testing.T) {
	token := signIn(t, freshPhone(), "Nobody Yet")
	list := call(t, "GET", crmAPI+"/businesses", token, nil)
	want(t, list, 200, "list businesses")
	if len(list.list("data")) != 0 || list.at("canCreate") != true {
		t.Fatalf("expected no businesses and permission to create one: %s", list.Raw)
	}
	for _, ws := range []string{"platform", "business-card-snap"} {
		r := call(t, "GET", crmAPI+"/w/"+ws+"/crm/leads", token, nil)
		if r.Status != 403 {
			t.Fatalf("workspace %s answered %d to a non-member: %s", ws, r.Status, truncate(r.Raw, 200))
		}
	}
	if r := call(t, "GET", crmAPI+"/platform/workspaces", token, nil); r.Status != 403 {
		t.Fatalf("owner API answered %d to a customer", r.Status)
	}
	if strings.Contains(list.Raw, "platform") {
		t.Fatal("a customer can see the platform workspace")
	}
}
