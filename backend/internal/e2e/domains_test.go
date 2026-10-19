package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// A tenant's own domain is connected on the hosting by the server; the page says which
// DNS record to add and when it is live (D-157).
func TestCustomDomainConnectsOnTheHosting(t *testing.T) {
	var mu sync.Mutex
	domains := map[string]bool{} // domain → DNS added
	removed := []string{}
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(403)
			return
		}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		switch {
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/projects/prj_1/domains"):
			var in struct{ Name string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			domains[in.Name] = domains[in.Name]
			io := map[string]any{"name": in.Name, "verified": true}
			_ = json.NewEncoder(w).Encode(io)
		case r.Method == "DELETE":
			d := parts[len(parts)-1]
			delete(domains, d)
			removed = append(removed, d)
			w.Write([]byte(`{}`))
		case r.Method == "GET" && parts[0] == "v9":
			d := parts[len(parts)-1]
			if _, ok := domains[d]; !ok {
				w.WriteHeader(404)
				w.Write([]byte(`{"error":{"code":"not_found"}}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"name": d, "verified": true})
		case r.Method == "GET" && parts[0] == "v6":
			_ = json.NewEncoder(w).Encode(map[string]any{"misconfigured": !domains[parts[2]]})
		default:
			w.WriteHeader(404)
		}
	}))
	defer stub.Close()

	owner := signIn(t, freshPhone(), "Domain Owner")
	code := newBusiness(t, owner, "Domain Traders")
	base := crmAPI + "/w/" + code

	// Without hosting settings: the record to add, and that the platform owner connects it.
	want(t, call(t, "PATCH", base+"/business", owner, map[string]any{"profile": map[string]any{"customDomain": "crm.domaintraders.in"}}), 200, "save domain")
	st := call(t, "GET", base+"/business/domain", owner, nil)
	recs := st.list("records")
	if st.Body["automatic"] != false || len(recs) != 1 || recs[0].(map[string]any)["type"] != "CNAME" || recs[0].(map[string]any)["name"] != "crm" || recs[0].(map[string]any)["value"] != "cname.vercel-dns.com" {
		t.Fatalf("the record to add for a subdomain: %s", st.Raw)
	}

	for k, v := range map[string]string{"VERCEL_API_TOKEN": "test-token", "VERCEL_PROJECT_ID": "prj_1", "VERCEL_API_URL": stub.URL} {
		os.Setenv(k, v)
		defer os.Unsetenv(k)
	}
	// Checking adds a domain the hosting doesn't know yet.
	st = call(t, "POST", base+"/business/domain/check", owner, nil)
	if st.Body["automatic"] != true || st.Body["added"] != true || st.Body["live"] != false || !strings.Contains(st.str("message"), "Waiting") {
		t.Fatalf("connected, waiting for DNS: %s", st.Raw)
	}
	mu.Lock()
	domains["crm.domaintraders.in"] = true // the owner added the CNAME
	mu.Unlock()
	st = call(t, "POST", base+"/business/domain/check", owner, nil)
	if st.Body["live"] != true {
		t.Fatalf("live once DNS points here: %s", st.Raw)
	}
	// Changing the domain disconnects the old one and connects the new (apex: an A record).
	want(t, call(t, "PATCH", base+"/business", owner, map[string]any{"profile": map[string]any{"customDomain": "domaintraders.in"}}), 200, "new domain")
	deadline := time.Now().Add(10 * time.Second)
	for {
		mu.Lock()
		_, added := domains["domaintraders.in"]
		gone := len(removed) == 1 && removed[0] == "crm.domaintraders.in"
		mu.Unlock()
		if added && gone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("old domain removed and new one added on the hosting: added=%v removed=%v", added, removed)
		}
		time.Sleep(200 * time.Millisecond)
	}
	st = call(t, "GET", base+"/business/domain", owner, nil)
	if r := st.list("records")[0].(map[string]any); r["type"] != "A" || r["name"] != "@" || r["value"] != "76.76.21.21" {
		t.Fatalf("an apex domain uses an A record: %s", st.Raw)
	}
	staff, _ := staffOf(t, owner, code, "Desk Staff")
	if x := call(t, "POST", base+"/business/domain/check", staff, nil); x.Status != 403 {
		t.Fatalf("only people who manage the business check the domain: %d", x.Status)
	}
}
