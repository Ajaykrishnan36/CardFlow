package e2e

import (
	"strings"
	"testing"
)

// A tenant is the brand: one permanent address for all its businesses, and sign-in there
// only for its own people (D-156).
func TestTenantOneAddressAndOnlyItsPeopleSignIn(t *testing.T) {
	owner := signIn(t, freshPhone(), "PSG Owner")
	r := call(t, "POST", crmAPI+"/businesses", owner, map[string]any{"name": "PSG Tenant Test"})
	want(t, r, 201, "first business makes the tenant")
	slug := r.str("business", "tenantSlug")
	school := r.str("business", "code")
	if slug == "" || slug != school || !strings.HasPrefix(r.str("next"), "/"+slug+"/home") {
		t.Fatalf("the tenant is addressed by the first business's code: %s", r.Raw)
	}
	r = call(t, "POST", crmAPI+"/businesses", owner, map[string]any{"name": "PSG College Test", "tenant": slug})
	want(t, r, 201, "second business in the same tenant")
	college := r.str("business", "code")
	if r.str("business", "tenantSlug") != slug || college == school {
		t.Fatalf("a second business keeps the tenant's address: %s", r.Raw)
	}
	stranger := signIn(t, freshPhone(), "Stranger")
	if x := call(t, "POST", crmAPI+"/businesses", stranger, map[string]any{"name": "Sneaky", "tenant": slug}); x.Status != 403 {
		t.Fatalf("only the tenant's owner or Super Admin adds businesses to it: %d", x.Status)
	}
	// Staff of the college, and someone of another tenant.
	staffPhone := freshPhone()
	want(t, call(t, "POST", crmAPI+"/w/"+college+"/admin/members", owner, map[string]any{"displayName": "College Staff", "phone": staffPhone, "roleKey": "STAFF"}), 201, "staff")
	otherPhone := freshPhone()
	other := signIn(t, otherPhone, "AK Owner")
	newBusiness(t, other, "AK Finance Tenant Test")

	ask := func(phone, tenant string) resp {
		return call(t, "POST", crmAPI+"/auth/phone/request", "", map[string]any{"phone": phone, "tenant": tenant})
	}
	want(t, ask(staffPhone, slug), 200, "the college's staff signs in on the tenant's address")
	if x := ask(otherPhone, slug); x.Status != 404 || x.str("code") != "account_not_found" {
		t.Fatalf("someone of another tenant: account not found — %d %s", x.Status, x.Raw)
	}
	newcomer := freshPhone()
	if x := ask(newcomer, slug); x.Status != 404 || x.str("code") != "account_not_found" {
		t.Fatalf("a new number gets no code and no account on a tenant's address: %d %s", x.Status, x.Raw)
	}
	if x := call(t, "POST", crmAPI+"/auth/phone/verify", "", map[string]any{"phone": newcomer, "code": "123456", "tenant": slug}); x.Status != 404 {
		t.Fatalf("verify is refused too: %d", x.Status)
	}
	if x := ask(staffPhone, "no-such-tenant-xyz"); x.Status != 404 || x.str("code") != "business_not_found" {
		t.Fatalf("an unknown tenant: %d %s", x.Status, x.Raw)
	}
	want(t, ask(otherPhone, ""), 200, "the main page signs anyone in")
	if x := call(t, "POST", crmAPI+"/auth/login", "", map[string]any{"identifier": otherPhone, "password": "whatever-123", "tenant": slug}); x.Status != 404 {
		t.Fatalf("password sign-in is checked as well: %d", x.Status)
	}
	// /me lists each membership with its tenant; renaming a business never moves the address.
	staff := signIn(t, staffPhone, "College Staff")
	me := call(t, "GET", crmAPI+"/me", staff, nil)
	if !strings.Contains(me.Raw, `"tenantSlug":"`+slug+`"`) {
		t.Fatalf("memberships carry the tenant: %s", truncate(me.Raw, 600))
	}
	want(t, call(t, "PATCH", crmAPI+"/w/"+school+"/business", owner, map[string]any{"name": "PSG Renamed School"}), 200, "rename")
	pub := call(t, "GET", crmAPI+"/public/tenants/"+slug, "", nil)
	if pub.str("slug") != slug || pub.str("home") != "/"+slug+"/home" {
		t.Fatalf("the address stays: %s", pub.Raw)
	}
	if ctx := call(t, "GET", crmAPI+"/w/"+college+"/context", owner, nil); ctx.str("workspace", "tenantSlug") != slug {
		t.Fatalf("the business context names its tenant: %s", truncate(ctx.Raw, 300))
	}
	// A name that is one of the site's own pages never becomes an address.
	if x := call(t, "POST", crmAPI+"/businesses", stranger, map[string]any{"name": "Home"}); x.str("business", "tenantSlug") == "home" {
		t.Fatal("reserved addresses are skipped")
	}
}
