package e2e

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"cardflow-backend/internal/crm/platform"
)

var codeShape = regexp.MustCompile(`^[0-9]{8}[0-9a-z]{4}[0-9]{6}$`)

// Every business has its own generated address, and only its own people sign in there
// (D-156, one business per address since D-158).
func TestEachBusinessItsOwnAddressAndOnlyItsPeopleSignIn(t *testing.T) {
	ownerPhone := freshPhone()
	owner := signIn(t, ownerPhone, "PSG Owner")
	r := call(t, "POST", crmAPI+"/businesses", owner, map[string]any{"name": "PSG School", "mode": "first", "taxIdType": "pan", "taxId": "abcde1234f", "teamSize": "11-50"})
	want(t, r, 201, "first business")
	school := r.str("business", "tenantSlug")
	day := time.Now().In(time.FixedZone("IST", 5*3600+1800))
	_, week := day.ISOWeek()
	if !codeShape.MatchString(school) || school[:6] != day.Format("020106") || school[6:8] != two(week) || school[8:12] != "234f" ||
		r.str("business", "code") != school || r.str("next") != "/"+school+"/home" {
		t.Fatalf("the address is date + week + last 4 of the PAN + a running number: %s", r.Raw)
	}
	if strings.Contains(school, "psg") {
		t.Fatal("the business name is never in the address")
	}
	// Add business (from inside): a new address, its own team; same PAN is fine for the same owner.
	r = call(t, "POST", crmAPI+"/businesses", owner, map[string]any{"name": "PSG College", "mode": "add", "taxIdType": "pan", "taxId": "ABCDE1234F", "tenant": school})
	want(t, r, 201, "second business")
	college := r.str("business", "tenantSlug")
	if !codeShape.MatchString(college) || college == school {
		t.Fatalf("every business gets its own address, even with the old tenant field: %s", r.Raw)
	}
	// The first-time form is refused to someone who is already a Super Admin; it lists their addresses.
	x := call(t, "POST", crmAPI+"/businesses", owner, map[string]any{"name": "Third", "mode": "first", "taxIdType": "pan", "taxId": "ABCDE1234F"})
	if x.Status != 409 || x.str("code") != "already_registered" || !strings.Contains(x.Raw, school) || !strings.Contains(x.Raw, college) {
		t.Fatalf("already registered: %d %s", x.Status, x.Raw)
	}
	stranger := signIn(t, freshPhone(), "Stranger")
	mustRefuse(t, "someone else's PAN", call(t, "POST", crmAPI+"/businesses", stranger, map[string]any{"name": "Sneaky", "taxIdType": "pan", "taxId": "ABCDE1234F"}), "taxId")
	mustRefuse(t, "no tax ID", call(t, "POST", crmAPI+"/businesses", stranger, map[string]any{"name": "No ID", "taxId": ""}), "taxId")
	mustRefuse(t, "a bad PAN", call(t, "POST", crmAPI+"/businesses", stranger, map[string]any{"name": "Bad ID", "taxIdType": "pan", "taxId": "ABCD1234F"}), "taxId")
	mustRefuse(t, "a bad TAN", call(t, "POST", crmAPI+"/businesses", stranger, map[string]any{"name": "Bad TAN", "taxIdType": "tan", "taxId": "ABCDE1234F"}), "taxId")
	tan := call(t, "POST", crmAPI+"/businesses", stranger, map[string]any{"name": "TAN Traders", "taxIdType": "tan", "taxId": "MUMA12345B", "mode": "first"})
	want(t, tan, 201, "a TAN works, and staff-only or new people may use the first-time form")
	if s := tan.str("business", "tenantSlug"); s[8:12] != "345b" {
		t.Fatalf("last 4 of the TAN: %s", s)
	}

	// Staff of the college only; Super Admin adds another Super Admin; new roles exist.
	staffPhone := freshPhone()
	m := call(t, "POST", crmAPI+"/w/"+college+"/admin/members", owner, map[string]any{"displayName": "College Staff", "phone": staffPhone, "roleKey": "SALES_STAFF"})
	want(t, m, 201, "sales staff")
	if m.str("loginUrl") != "" && !strings.HasSuffix(m.str("loginUrl"), "/"+college+"/login") {
		t.Fatalf("adding a member gives the business's address to share: %s", m.Raw)
	}
	if m.str("loginUrl") == "" {
		t.Fatalf("no address to share: %s", m.Raw)
	}
	want(t, call(t, "POST", crmAPI+"/w/"+college+"/admin/members", owner, map[string]any{"displayName": "Service Person", "phone": freshPhone(), "roleKey": "SERVICE_STAFF"}), 201, "service staff")
	coPhone := freshPhone()
	want(t, call(t, "POST", crmAPI+"/w/"+school+"/admin/members", owner, map[string]any{"displayName": "Co Owner", "phone": coPhone, "roleKey": "SUPER_ADMIN"}), 201, "a Super Admin adds another")
	mail := call(t, "POST", crmAPI+"/w/"+school+"/admin/members", owner, map[string]any{"displayName": "Mail Person", "email": "mail.person@example.com", "roleKey": "ADMIN", "method": "invite"})
	want(t, mail, 201, "member by email")
	if !strings.HasSuffix(mail.str("loginUrl"), "/"+school+"/login") {
		t.Fatalf("address for an emailed member: %s", mail.Raw)
	}

	ask := func(phone, addr string) resp {
		return call(t, "POST", crmAPI+"/auth/phone/request", "", map[string]any{"phone": phone, "tenant": addr})
	}
	want(t, ask(staffPhone, college), 200, "the college's staff signs in at the college's address")
	want(t, ask(staffPhone, strings.ToUpper(college[:6]+"-"+college[6:8]+"-"+college[8:12]+"-"+college[12:])), 200, "typed grouped and in capitals")
	if x := ask(staffPhone, school); x.Status != 404 || x.str("code") != "account_not_found" || !strings.Contains(x.Raw, "contact your Super Admin") {
		t.Fatalf("the college's staff can't sign in at the school's address: %d %s", x.Status, x.Raw)
	}
	want(t, ask(ownerPhone, school), 200, "the owner signs in at each address")
	want(t, ask(ownerPhone, college), 200, "the owner signs in at each address")
	newcomer := freshPhone()
	if x := ask(newcomer, college); x.Status != 404 || x.str("code") != "account_not_found" {
		t.Fatalf("a new number gets no code at a business's address: %d %s", x.Status, x.Raw)
	}
	if x := call(t, "POST", crmAPI+"/auth/phone/verify", "", map[string]any{"phone": newcomer, "code": "123456", "tenant": college}); x.Status != 404 {
		t.Fatalf("verify is refused too: %d", x.Status)
	}
	if x := ask(staffPhone, "999999999999000000"); x.Status != 404 || x.str("code") != "business_not_found" || !strings.Contains(x.Raw, "invalid or unavailable") {
		t.Fatalf("an unknown address: %d %s", x.Status, x.Raw)
	}
	want(t, ask(newcomer, ""), 200, "the main page signs anyone in")
	if x := call(t, "POST", crmAPI+"/auth/login", "", map[string]any{"identifier": staffPhone, "password": "whatever-123", "tenant": school}); x.Status != 404 {
		t.Fatalf("password sign-in is checked as well: %d", x.Status)
	}
	// Renaming never moves the address; the public lookup finds it by grouped code too.
	want(t, call(t, "PATCH", crmAPI+"/w/"+school+"/business", owner, map[string]any{"name": "PSG Renamed School"}), 200, "rename")
	pub := call(t, "GET", crmAPI+"/public/tenants/"+strings.ToUpper(school), "", nil)
	if pub.str("slug") != school || pub.str("home") != "/"+school+"/home" || pub.str("name") != "PSG Renamed School" {
		t.Fatalf("the address stays: %s", pub.Raw)
	}
	b := call(t, "GET", crmAPI+"/w/"+school+"/business", owner, nil)
	if pr, _ := b.Body["profile"].(map[string]any); pr["taxId"] != "ABCDE1234F" || pr["taxIdType"] != "pan" || pr["pan"] != "ABCDE1234F" || pr["teamSize"] != "11-50" {
		t.Fatalf("the profile keeps the tax ID and team size: %v", b.Body["profile"])
	}
	mustRefuse(t, "the tax ID the address was made from", call(t, "PATCH", crmAPI+"/w/"+school+"/business", owner, map[string]any{"profile": map[string]any{"taxId": "X"}}), "profile.taxId")
}

// Businesses from before D-158 get their own generated address; the old one still leads there.
func TestOldAddressesMoveToGeneratedCodes(t *testing.T) {
	owner := signIn(t, freshPhone(), "Old Brand Owner")
	a := newBusiness(t, owner, "Old Brand One")
	bcode := newBusiness(t, owner, "Old Brand Two")
	ctx := context.Background()
	// Make them look like D-156: name-based codes, one brand tenant "old-brand-x" over both.
	if _, err := testDB.Pool.Exec(ctx, `UPDATE crm.workspaces SET code = CASE WHEN code = $1 THEN 'old-brand-one-x' ELSE 'old-brand-two-x' END, tenant_id = NULL WHERE code IN ($1, $2)`, a, bcode); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB.Pool.Exec(ctx, `DELETE FROM crm.tenants WHERE slug IN ($1, $2)`, a, bcode); err != nil {
		t.Fatal(err)
	}
	a, bcode = "old-brand-one-x", "old-brand-two-x"
	var brand string
	if err := testDB.Pool.QueryRow(ctx, `INSERT INTO crm.tenants (slug, name, custom_domain) VALUES ('old-brand-x', 'Old Brand', 'crm.oldbrand-test.in') RETURNING id`).Scan(&brand); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB.Pool.Exec(ctx, `UPDATE crm.workspaces SET tenant_id = $1 WHERE code IN ($2, $3)`, brand, a, bcode); err != nil {
		t.Fatal(err)
	}
	if err := crmMigrateAddresses(ctx); err != nil {
		t.Fatal(err)
	}
	var slugA, slugB, status, domain string
	_ = testDB.Pool.QueryRow(ctx, `SELECT t.slug FROM crm.workspaces w JOIN crm.tenants t ON t.id = w.tenant_id WHERE w.code = $1`, a).Scan(&slugA)
	_ = testDB.Pool.QueryRow(ctx, `SELECT t.slug FROM crm.workspaces w JOIN crm.tenants t ON t.id = w.tenant_id WHERE w.code = $1`, bcode).Scan(&slugB)
	_ = testDB.Pool.QueryRow(ctx, `SELECT status, COALESCE(custom_domain, '') FROM crm.tenants WHERE id = $1`, brand).Scan(&status, &domain)
	if !codeShape.MatchString(slugA) || !codeShape.MatchString(slugB) || slugA == slugB || status != "redirected" || domain != "" {
		t.Fatalf("each business its own code, the brand address redirected: %s %s %s %q", slugA, slugB, status, domain)
	}
	if pub := call(t, "GET", crmAPI+"/public/tenants/old-brand-x", "", nil); pub.str("slug") != slugA {
		t.Fatalf("the old address leads to the first business: %s", pub.Raw)
	}
	if h := call(t, "GET", crmAPI+"/public/tenants/by-host?host=crm.oldbrand-test.in", "", nil); h.str("slug") != slugA {
		t.Fatalf("the brand's domain moved with it: %s", h.Raw)
	}
	if pub := call(t, "GET", crmAPI+"/public/tenants/"+bcode, "", nil); pub.str("slug") != slugB {
		t.Fatalf("an old business code leads to that business: %s", pub.Raw)
	}
	if err := crmMigrateAddresses(ctx); err != nil {
		t.Fatal(err)
	}
	var again string
	_ = testDB.Pool.QueryRow(ctx, `SELECT t.slug FROM crm.workspaces w JOIN crm.tenants t ON t.id = w.tenant_id WHERE w.code = $1`, a).Scan(&again)
	if again != slugA {
		t.Fatalf("running again changes nothing: %s → %s", slugA, again)
	}
}

func two(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

func crmMigrateAddresses(ctx context.Context) error {
	return platform.MigrateBusinessAddresses(ctx, testDB.Pool)
}
