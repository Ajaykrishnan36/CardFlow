package e2e

import (
	"strings"
	"testing"
)

// A person creates their own business and is its Super Admin (D-94).
func TestCreateBusinessMakesCreatorSuperAdmin(t *testing.T) {
	token := signIn(t, freshPhone(), "Owner One")
	r := call(t, "POST", crmAPI+"/businesses", token, map[string]any{"name": "Kovai Tools & Dies", "industry": "manufacturing", "city": "Coimbatore"})
	want(t, r, 201, "create business")
	if r.str("business", "roleKey") != "SUPER_ADMIN" {
		t.Fatalf("creator should be Super Admin: %s", r.Raw)
	}
	code := r.str("business", "code")
	if code != "kovai-tools-dies" {
		t.Fatalf("code from the name = %q", code)
	}
	ctx := call(t, "GET", crmAPI+"/w/"+code+"/context", token, nil)
	want(t, ctx, 200, "open the new business")

	// The same name again gets its own code; nothing collides.
	if second := newBusiness(t, token, "Kovai Tools & Dies"); second == code {
		t.Fatal("two businesses share one code")
	}
	list := call(t, "GET", crmAPI+"/businesses", token, nil)
	if len(list.list("data")) != 2 {
		t.Fatalf("expected 2 businesses, got %s", list.Raw)
	}
	// A name is required.
	want(t, call(t, "POST", crmAPI+"/businesses", token, map[string]any{"name": " "}), 422, "empty name")
}

// The per-person limit is enforced on the server.
func TestBusinessLimit(t *testing.T) {
	token := signIn(t, freshPhone(), "Many Shops")
	for i := 0; i < 5; i++ {
		newBusiness(t, token, "Shop "+string(rune('A'+i)))
	}
	r := call(t, "POST", crmAPI+"/businesses", token, map[string]any{"name": "One Too Many"})
	want(t, r, 403, "sixth business")
	if r.str("code") != "business_limit" {
		t.Fatalf("expected business_limit, got %s", r.Raw)
	}
}

// The critical multi-business test (spec §72). The phone 6382124970 is a lead in
// Business A, a lead in Business B and a contact in Business C. Each business sees only
// its own record through lists, direct ids, search and exports, and deleting A's lead
// leaves B and C untouched.
func TestSamePersonInThreeBusinessesStaysSeparate(t *testing.T) {
	const person = "+91 63821 24970"
	ownerA := signIn(t, freshPhone(), "Owner A")
	ownerBC := signIn(t, freshPhone(), "Owner BC") // one person can own several businesses
	a := newBusiness(t, ownerA, "Business A")
	b := newBusiness(t, ownerBC, "Business B")
	c := newBusiness(t, ownerBC, "Business C")

	leadA := create(t, ownerA, a, "leads", map[string]any{"firstName": "John", "lastName": "Mathew", "organization": "ABC Company", "phone": person, "description": "A-only private note: budget 5,00,000"})
	leadB := create(t, ownerBC, b, "leads", map[string]any{"firstName": "John", "lastName": "Mathew", "organization": "ABC Company", "phone": person})
	contactC := create(t, ownerBC, c, "contacts", map[string]any{"firstName": "John", "lastName": "Mathew", "phone": person})

	// Each business lists exactly its own record.
	for _, tc := range []struct {
		token, ws, object string
		n                 int
	}{{ownerA, a, "leads", 1}, {ownerA, a, "contacts", 0}, {ownerBC, b, "leads", 1}, {ownerBC, b, "contacts", 0}, {ownerBC, c, "leads", 0}, {ownerBC, c, "contacts", 1}} {
		if got := total(t, tc.token, tc.ws, tc.object, ""); got != tc.n {
			t.Fatalf("%s in %s: %d records, want %d", tc.object, tc.ws, got, tc.n)
		}
	}

	// A member of A can't enter B or C at all: 403, not an empty list.
	for _, ws := range []string{b, c} {
		for _, path := range []string{"/crm/leads", "/crm/contacts", "/search?q=6382124970", "/dashboard", "/context", "/crm/leads/export"} {
			if r := call(t, "GET", crmAPI+"/w/"+ws+path, ownerA, nil); r.Status != 403 {
				t.Fatalf("owner A got %d from %s%s: %s", r.Status, ws, path, truncate(r.Raw, 200))
			}
		}
	}

	// Another business's record id never resolves through your own business.
	for _, tc := range []struct{ token, ws, object, id string }{
		{ownerA, a, "leads", leadB}, {ownerA, a, "contacts", contactC}, {ownerBC, b, "leads", leadA}, {ownerBC, c, "leads", leadA},
	} {
		if r := call(t, "GET", crmAPI+"/w/"+tc.ws+"/crm/"+tc.object+"/"+tc.id, tc.token, nil); r.Status != 404 {
			t.Fatalf("foreign %s %s opened through %s: %d", tc.object, tc.id, tc.ws, r.Status)
		}
		if r := call(t, "PATCH", crmAPI+"/w/"+tc.ws+"/crm/"+tc.object+"/"+tc.id, tc.token, map[string]any{"values": map[string]any{"lastName": "Hacked"}, "expectedVersion": 1}); r.Status != 404 {
			t.Fatalf("foreign %s %s edited through %s: %d", tc.object, tc.id, tc.ws, r.Status)
		}
		if r := call(t, "DELETE", crmAPI+"/w/"+tc.ws+"/crm/"+tc.object+"/"+tc.id, tc.token, nil); r.Status != 404 {
			t.Fatalf("foreign %s %s deleted through %s: %d", tc.object, tc.id, tc.ws, r.Status)
		}
	}

	// Search by the phone number finds only the business's own record.
	for _, tc := range []struct{ token, ws, wantID, notID string }{{ownerA, a, leadA, leadB}, {ownerBC, b, leadB, leadA}, {ownerBC, c, contactC, leadA}} {
		r := call(t, "GET", crmAPI+"/w/"+tc.ws+"/search?q=63821", tc.token, nil)
		want(t, r, 200, "search in "+tc.ws)
		if !strings.Contains(r.Raw, tc.wantID) {
			t.Fatalf("search in %s didn't find its own record: %s", tc.ws, truncate(r.Raw, 300))
		}
		if strings.Contains(r.Raw, tc.notID) || strings.Contains(r.Raw, "budget") {
			t.Fatalf("search in %s leaked another business: %s", tc.ws, truncate(r.Raw, 300))
		}
	}

	// The export of B's leads carries nothing from A.
	exp := call(t, "GET", crmAPI+"/w/"+b+"/crm/leads/export", ownerBC, nil)
	want(t, exp, 200, "export B leads")
	if strings.Contains(exp.Raw, "budget 5,00,000") {
		t.Fatal("B's export contains A's private note")
	}

	// Business A deletes its lead. B and C keep theirs.
	want(t, call(t, "DELETE", crmAPI+"/w/"+a+"/crm/leads/"+leadA, ownerA, nil), 204, "delete A's lead")
	if got := total(t, ownerA, a, "leads", ""); got != 0 {
		t.Fatalf("A still lists %d leads", got)
	}
	want(t, call(t, "GET", crmAPI+"/w/"+b+"/crm/leads/"+leadB, ownerBC, nil), 200, "B's lead after A deleted")
	want(t, call(t, "GET", crmAPI+"/w/"+c+"/crm/contacts/"+contactC, ownerBC, nil), 200, "C's contact after A deleted")
	if total(t, ownerBC, b, "leads", "") != 1 || total(t, ownerBC, c, "contacts", "") != 1 {
		t.Fatal("deleting A's lead changed B or C")
	}
}

// Switching business switches the data: the same person sees different records in each
// of their businesses, and nothing carries over.
func TestSwitchingBusinessSwitchesData(t *testing.T) {
	token := signIn(t, freshPhone(), "Two Shops")
	one := newBusiness(t, token, "First Shop")
	two := newBusiness(t, token, "Second Shop")
	create(t, token, one, "leads", map[string]any{"lastName": "Only In One"})
	create(t, token, two, "accounts", map[string]any{"name": "Only In Two"})
	if total(t, token, one, "leads", "") != 1 || total(t, token, one, "accounts", "") != 0 {
		t.Fatal("first shop shows the wrong records")
	}
	if total(t, token, two, "leads", "") != 0 || total(t, token, two, "accounts", "") != 1 {
		t.Fatal("second shop shows the wrong records")
	}
}
