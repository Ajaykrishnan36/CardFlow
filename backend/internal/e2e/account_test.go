package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"testing"
)

// A person who signed up by phone adds an email (verified by a code), sets a password,
// and can then sign in either way to the same account (D-103).
func TestAddEmailAndPasswordToAPhoneAccount(t *testing.T) {
	phone := freshPhone()
	token := signIn(t, phone, "Email Adder")
	ws := newBusiness(t, token, "Email Traders")
	me := call(t, "GET", crmAPI+"/me", token, nil)
	if me.at("identity", "phoneVerified") != true || me.at("identity", "emailVerified") != false || me.at("identity", "hasPassword") != false {
		t.Fatalf("a phone sign-up starts with a verified phone only: %s", truncate(me.Raw, 300))
	}
	email := "adder" + phone + "@example.com"
	want(t, call(t, "POST", crmAPI+"/me/email/request", token, map[string]any{"email": "not-an-email"}), 422, "bad email")
	req := call(t, "POST", crmAPI+"/me/email/request", token, map[string]any{"email": email})
	want(t, req, 200, "request email code")
	code := req.str("devCode")
	if len(code) != 6 {
		t.Fatalf("local mail should return the code: %s", req.Raw)
	}
	want(t, call(t, "POST", crmAPI+"/me/email/verify", token, map[string]any{"email": email, "code": "000000"}), 401, "wrong email code")
	// Not verified yet: the address isn't on the account.
	if call(t, "GET", crmAPI+"/me", token, nil).at("identity", "emailVerified") != false {
		t.Fatalf("an unconfirmed address must not count as verified")
	}
	want(t, call(t, "POST", crmAPI+"/me/email/verify", token, map[string]any{"email": email, "code": code}), 200, "verify email")
	want(t, call(t, "POST", crmAPI+"/me/email/verify", token, map[string]any{"email": email, "code": code}), 401, "a code works once")
	me = call(t, "GET", crmAPI+"/me", token, nil)
	if me.str("identity", "email") != email || me.at("identity", "emailVerified") != true {
		t.Fatalf("email should be verified on the account: %s", truncate(me.Raw, 300))
	}
	// Someone else can't take the same address.
	other := signIn(t, freshPhone(), "Other Person")
	want(t, call(t, "POST", crmAPI+"/me/email/request", other, map[string]any{"email": email}), 422, "an address in use")

	want(t, call(t, "POST", crmAPI+"/me/password", token, map[string]any{"newPassword": "short"}), 422, "weak password")
	want(t, call(t, "POST", crmAPI+"/me/password", token, map[string]any{"newPassword": "Sunrise-Harbour-42"}), 200, "set a password")
	// Changing it now needs the current one.
	want(t, call(t, "POST", crmAPI+"/me/password", token, map[string]any{"newPassword": "Another-Harbour-43"}), 422, "change without the current password")

	// Email + password signs in to the same account and the same business.
	login := call(t, "POST", crmAPI+"/auth/login", "", map[string]any{"identifier": email, "password": "Sunrise-Harbour-42", "audience": "workspace"})
	want(t, login, 200, "sign in with email and password")
	byPassword := login.str("token")
	list := call(t, "GET", crmAPI+"/businesses", byPassword, nil)
	want(t, list, 200, "businesses after password sign-in")
	if len(list.list("data")) != 1 || list.list("data")[0].(map[string]any)["code"] != ws {
		t.Fatalf("password sign-in should reach the same business: %s", truncate(list.Raw, 300))
	}
	// And the app API recognises that session as the same person.
	if r := call(t, "GET", appAPI+"/users/me", byPassword, nil); r.Status != 200 {
		t.Fatalf("app profile with a password session: %d %s", r.Status, truncate(r.Raw, 200))
	}
}

// In a browser one sign-in (a cookie) serves the CRM and the app API; anything that
// changes data must carry the CSRF token (D-104).
func TestBrowserSessionWorksOnTheAppAPI(t *testing.T) {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	base, _ := url.Parse(baseURL)
	csrf := func() string {
		for _, c := range jar.Cookies(base) {
			if c.Name == "crm_csrf" {
				return c.Value
			}
		}
		return ""
	}
	do := func(method, path string, body any, withCSRF bool) (int, map[string]any) {
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, _ := http.NewRequest(method, baseURL+path, rd)
		req.Header.Set("Content-Type", "application/json")
		phoneSeq++
		req.Header.Set("X-Forwarded-For", "10.8.0.9")
		if withCSRF {
			req.Header.Set("X-CSRF-Token", csrf())
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		out := map[string]any{}
		_ = json.Unmarshal(raw, &out)
		return res.StatusCode, out
	}
	if s, _ := do("GET", crmAPI+"/auth/csrf", nil, false); s != 200 && s != 204 {
		t.Fatalf("csrf: %d", s)
	}
	phone := freshPhone()
	s, r := do("POST", crmAPI+"/auth/phone/request", map[string]any{"phone": phone}, true)
	if s != 200 {
		t.Fatalf("request code: %d %v", s, r)
	}
	s, r = do("POST", crmAPI+"/auth/phone/verify", map[string]any{"phone": phone, "code": r["devCode"], "name": "Browser Person"}, true)
	if s != 200 {
		t.Fatalf("verify: %d %v", s, r)
	}
	if _, has := r["token"]; has {
		t.Fatalf("a browser sign-in must not hand the session token to JavaScript: %v", r)
	}
	// The same cookie is the session on the app API.
	if s, r := do("GET", appAPI+"/users/me", nil, false); s != 200 {
		t.Fatalf("app profile by cookie: %d %v", s, r)
	}
	// A write without the CSRF token is refused; with it, it works.
	cardBody := map[string]any{"person_name": "Cookie Card", "company": "Cookie Co", "phones": []map[string]any{{"raw": "9444400001"}}}
	if s, _ := do("POST", appAPI+"/cards", cardBody, false); s != 401 {
		t.Fatalf("a write by cookie without the CSRF token: got %d, want 401", s)
	}
	if s, r := do("POST", appAPI+"/cards", cardBody, true); s != 200 && s != 201 {
		t.Fatalf("a write by cookie with the CSRF token: %d %v", s, r)
	}
	// Signing out ends it for both.
	if s, _ := do("POST", crmAPI+"/auth/logout", nil, true); s != 200 && s != 204 {
		t.Fatalf("logout: %d", s)
	}
	if s, _ := do("GET", appAPI+"/users/me", nil, false); s != 401 {
		t.Fatalf("the app API still accepts a signed-out cookie: %d", s)
	}
}

// New businesses get a "getting started" list whose ticks come from real data (D-109).
func TestGettingStartedReflectsRealProgress(t *testing.T) {
	token := signIn(t, freshPhone(), "Starter")
	ws := newBusiness(t, token, "Starter Traders")
	step := func() map[string]bool {
		r := call(t, "GET", crmAPI+"/w/"+ws+"/getting-started", token, nil)
		want(t, r, 200, "getting started")
		out := map[string]bool{}
		for _, s := range r.list("steps") {
			m := s.(map[string]any)
			out[m["key"].(string)] = m["done"].(bool)
		}
		return out
	}
	before := step()
	for _, k := range []string{"lead", "scan", "followup", "business", "team", "email"} {
		if done, has := before[k]; !has || done {
			t.Fatalf("a new business should list %q as not done: %v", k, before)
		}
	}
	create(t, token, ws, "leads", map[string]any{"lastName": "First Lead"})
	create(t, token, ws, "tasks", map[string]any{"name": "Call back"})
	want(t, call(t, "POST", crmAPI+"/w/"+ws+"/cards", token, map[string]any{"action": "card_only", "card": card("A Card", "", "9555500011", "")}), 201, "scan a card")
	after := step()
	if !after["lead"] || !after["followup"] || !after["scan"] || after["team"] || after["email"] {
		t.Fatalf("ticks should follow what was done: %v", after)
	}
	// Another business of the same person starts from zero.
	other := newBusiness(t, token, "Second Starter")
	r := call(t, "GET", crmAPI+"/w/"+other+"/getting-started", token, nil)
	if r.at("done") != float64(0) {
		t.Fatalf("progress leaked between businesses: %s", truncate(r.Raw, 200))
	}
}
