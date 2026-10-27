package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// register goes through "Create your own CRM" (D-160): send a code, check it. It returns the
// verify answer, which carries the registration token and never a session.
func register(t *testing.T, phone string) resp {
	t.Helper()
	phoneSeq++
	clientIP = fmt.Sprintf("10.8.%d.%d", phoneSeq/250, phoneSeq%250+1)
	defer func() { clientIP = "" }()
	r := call(t, "POST", crmAPI+"/public/register/request", "", map[string]any{"phone": phone})
	want(t, r, 200, "register: send code to "+phone)
	v := call(t, "POST", crmAPI+"/public/register/verify", "", map[string]any{"phone": phone, "code": r.str("devCode")})
	want(t, v, 200, "register: check code for "+phone)
	if v.str("token") != "" || strings.Contains(strings.Join(v.Header.Values("Set-Cookie"), ";"), "crm_sid=") {
		t.Fatalf("registering must not sign in: %s / %v", truncate(v.Raw, 200), v.Header.Get("Set-Cookie"))
	}
	if !strings.HasPrefix(v.str("registrationToken"), "reg_") {
		t.Fatalf("no registration token: %s", truncate(v.Raw, 300))
	}
	return v
}

// signInAt signs a phone in at one business's own URL and returns the bearer token.
func signInAt(t *testing.T, phone, tenant string) resp {
	t.Helper()
	phoneSeq++
	clientIP = fmt.Sprintf("10.7.%d.%d", phoneSeq/250, phoneSeq%250+1)
	defer func() { clientIP = "" }()
	r := call(t, "POST", crmAPI+"/auth/phone/request", "", map[string]any{"phone": phone, "tenant": tenant})
	if r.Status != 200 {
		return r
	}
	return call(t, "POST", crmAPI+"/auth/phone/verify", "", map[string]any{"phone": phone, "code": r.str("devCode"), "tenant": tenant})
}

func registerBusiness(t *testing.T, token string, body map[string]any) resp {
	t.Helper()
	body["registrationToken"] = token
	if _, ok := body["taxId"]; !ok {
		body["taxIdType"], body["taxId"] = "pan", testPAN()
	}
	r := call(t, "POST", crmAPI+"/public/register/business", "", body)
	if r.str("token") != "" {
		t.Fatalf("registering a business never signs in: %s", truncate(r.Raw, 300))
	}
	// A first business waits for the owner (202, no URL yet, D-161); more businesses of
	// someone who owns an approved one open at once (201, with the URL).
	if r.Status == 202 {
		if r.Body["pending"] != true || r.str("loginPath") != "" {
			t.Fatalf("a business in review has no sign-in URL yet: %s", truncate(r.Raw, 300))
		}
		return r
	}
	want(t, r, 201, "register business "+fmt.Sprint(body["name"]))
	if !strings.HasSuffix(r.str("loginPath"), "/login") {
		t.Fatalf("a new business answers with its sign-in URL: %s", truncate(r.Raw, 300))
	}
	return r
}

// Ajay's plan: three businesses from "Create your own CRM", each with its own URL; a partner
// who is Super Admin of two signs in at those two only; staff only at their own business.
func TestCreateYourOwnCRMRegistersAndOnlyBusinessURLsSignIn(t *testing.T) {
	setReview(t, true)
	defer setReview(t, false)
	ajay := freshPhone()
	owner := ownerToken(t)

	// 1. A new number: no account yet, nothing about anyone else.
	first := register(t, ajay)
	if first.Body["hasAccount"] != false || len(first.list("businesses")) != 0 {
		t.Fatalf("a new number has no account: %s", truncate(first.Raw, 300))
	}
	// A "Create your own CRM" code never signs in, and a sign-in code never registers.
	// (Another number: each number may ask for 3 codes in 10 minutes, as in real use.)
	other := freshPhone()
	reg := call(t, "POST", crmAPI+"/public/register/request", "", map[string]any{"phone": other})
	want(t, reg, 200, "register code")
	if x := call(t, "POST", crmAPI+"/auth/phone/verify", "", map[string]any{"phone": other, "code": reg.str("devCode")}); x.Status == 200 {
		t.Fatalf("a register code signed in: %s", truncate(x.Raw, 200))
	}
	// A made-up or altered token is refused.
	for _, bad := range []string{"", "reg_abc.def", first.str("registrationToken") + "x"} {
		x := call(t, "POST", crmAPI+"/public/register/business", "", map[string]any{"registrationToken": bad, "name": "Fake", "taxIdType": "pan", "taxId": testPAN()})
		want(t, x, 401, "register with token "+truncate(bad, 12))
	}

	traders := registerBusiness(t, first.str("registrationToken"), map[string]any{
		"mode": "first", "name": "Ajay Traders", "displayName": "Ajay Krishnan", "personalEmail": "ajay-" + ajay + "@example.com", "teamSize": "2-10", "industry": "Retail",
	})
	// Step 3–5 (D-161): "we will review your application"; nobody can sign in at its URL
	// yet; the application is a lead in the owner console, which the owner approves.
	if traders.Status != 202 {
		t.Fatalf("a first business waits for approval: %d %s", traders.Status, truncate(traders.Raw, 300))
	}
	tradersSlug := traders.str("business", "tenantSlug")
	if x := signInAt(t, ajay, tradersSlug); x.str("code") != "business_pending" {
		t.Fatalf("sign-in at a business in review: %d %s", x.Status, truncate(x.Raw, 200))
	}
	if x := call(t, "GET", crmAPI+"/public/tenants/"+tradersSlug, "", nil); x.str("code") != "business_pending" {
		t.Fatalf("its URL says it waits for approval: %d %s", x.Status, truncate(x.Raw, 200))
	}
	waiting := register(t, ajay)
	crmMod.Identity().ForgetCodeLimits("+91" + ajay) // ten minutes later
	if waiting.Body["hasAccount"] != false || len(waiting.list("pending")) != 1 {
		t.Fatalf("coming back while in review shows the application: %s", truncate(waiting.Raw, 300))
	}
	apps := call(t, "GET", crmAPI+"/platform/applications?status=pending", owner, nil)
	want(t, apps, 200, "owner lists applications")
	var leadID string
	for _, a := range apps.list("data") {
		if m := a.(map[string]any); m["code"] == tradersSlug {
			leadID, _ = m["leadId"].(string)
		}
	}
	if leadID == "" {
		t.Fatalf("the application is a lead in the owner console: %s", truncate(apps.Raw, 400))
	}
	lead := call(t, "GET", crmAPI+"/platform/crm/leads/"+leadID, owner, nil)
	want(t, lead, 200, "owner opens the application lead")
	if !strings.Contains(lead.Raw, "Ajay Traders") || !strings.Contains(lead.Raw, ajay) {
		t.Fatalf("the lead carries the business and the mobile number: %s", truncate(lead.Raw, 400))
	}
	if x := call(t, "GET", crmAPI+"/platform/applications?leadId="+leadID, owner, nil); len(x.list("data")) != 1 {
		t.Fatalf("the lead finds its business: %s", truncate(x.Raw, 300))
	}
	want(t, call(t, "POST", crmAPI+"/platform/applications/"+workspaceID(t, tradersSlug)+"/approve", "", map[string]any{}), 401, "approve without the owner")
	approve(t, owner, tradersSlug)
	want(t, call(t, "POST", crmAPI+"/platform/applications/"+workspaceID(t, tradersSlug)+"/approve", owner, map[string]any{}), 409, "approve twice")
	// The owner converts the lead into a contact (and account).
	want(t, call(t, "POST", crmAPI+"/platform/crm/leads/"+leadID+"/convert", owner, map[string]any{"account": map[string]any{"mode": "new", "name": "Ajay Traders", "kind": "business"}, "createContact": true}), 200, "owner converts the application lead")
	traders.Body["loginPath"] = "/" + tradersSlug + "/login"

	// 2. Coming back to the page: the code shows who they are and their URLs first.
	again := register(t, ajay)
	if again.Body["hasAccount"] != true || again.str("person", "name") != "Ajay Krishnan" || again.str("person", "email") != "ajay-"+ajay+"@example.com" {
		t.Fatalf("a returning Super Admin is told they have an account: %s", truncate(again.Raw, 400))
	}
	bs := again.list("businesses")
	b0, _ := bs[0].(map[string]any)
	if len(bs) != 1 || b0["name"] != "Ajay Traders" || b0["roleKey"] != "SUPER_ADMIN" || b0["loginPath"] != traders.str("loginPath") {
		t.Fatalf("their business and its URL: %v", bs)
	}
	// Step 4a: they are told on every channel; the page says where.
	if again.str("notify", "email") == "" {
		t.Fatalf("an existing account is told where its URLs are sent: %s", truncate(again.Raw, 400))
	}
	// "Request my URL" emails them (console mail in tests).
	sent := call(t, "POST", crmAPI+"/public/register/send-urls", "", map[string]any{"registrationToken": again.str("registrationToken")})
	want(t, sent, 200, "request my URL")

	// 3. They can still create more businesses; each is linked to them, and (step 6) opens
	// at once because Ajay already owns an approved one.
	tech := registerBusiness(t, again.str("registrationToken"), map[string]any{"mode": "add", "name": "Ajay Tech"})
	bikes := registerBusiness(t, again.str("registrationToken"), map[string]any{"mode": "add", "name": "Ajay Bikes"})
	if tech.Status != 201 || bikes.Status != 201 {
		t.Fatalf("more businesses of an approved owner open at once: %d %d", tech.Status, bikes.Status)
	}
	slugs := map[string]string{}
	for k, r := range map[string]resp{"traders": traders, "tech": tech, "bikes": bikes} {
		slugs[k] = r.str("business", "tenantSlug")
	}
	if slugs["traders"] == slugs["tech"] || slugs["tech"] == slugs["bikes"] {
		t.Fatalf("each business has its own URL: %v", slugs)
	}
	var mine int
	if err := testDB.Pool.QueryRow(context.Background(), `SELECT count(*) FROM crm.workspaces w JOIN crm.tenants t ON t.id = w.tenant_id
		WHERE t.slug = ANY($1) AND w.created_by_identity = t.owner_identity
		  AND w.created_by_identity = (SELECT identity_id FROM crm.verified_identifiers WHERE kind = 'phone' AND value_normalized = '+91' || $2)`,
		[]string{slugs["traders"], slugs["tech"], slugs["bikes"]}, ajay).Scan(&mine); err != nil {
		t.Fatal(err)
	}
	if mine != 3 {
		t.Fatalf("all three businesses are Ajay's: %d", mine)
	}

	// Ajay signs in at each business's own URL, then adds the team.
	session := signInAt(t, ajay, slugs["traders"])
	want(t, session, 200, "Ajay signs in at Ajay Traders' URL")
	boss := session.str("token")
	want(t, call(t, "PUT", crmAPI+"/platform/plans/team20", owner, map[string]any{"name": "Team 20", "isActive": true, "position": 8, "limits": map[string]any{"members": 20}}), 200, "a plan with room")
	for _, k := range []string{"traders", "tech", "bikes"} {
		var wsID string
		if err := testDB.Pool.QueryRow(context.Background(), `SELECT id::text FROM crm.workspaces WHERE code = $1`, slugs[k]).Scan(&wsID); err != nil {
			t.Fatal(err)
		}
		want(t, call(t, "PUT", crmAPI+"/platform/subscriptions/"+wsID, owner, map[string]any{"planKey": "team20", "status": "active"}), 200, "plan for "+k)
	}
	add := func(biz, name, phone, role string) {
		t.Helper()
		want(t, call(t, "POST", crmAPI+"/w/"+slugs[biz]+"/admin/members", boss, map[string]any{"displayName": name, "phone": phone, "roleKey": role}), 201, "add "+name+" to "+biz)
	}
	ram := freshPhone()
	team := map[string][]string{}
	for _, biz := range []string{"traders", "tech"} {
		add(biz, "Ram", ram, "SUPER_ADMIN")
		admin := freshPhone()
		add(biz, "Admin of "+biz, admin, "ADMIN")
		team[biz] = append(team[biz], admin)
		staff := 2
		if biz == "tech" {
			staff = 5
		}
		for i := 0; i < staff; i++ {
			p := freshPhone()
			add(biz, fmt.Sprintf("Staff %d", i), p, "STAFF")
			team[biz] = append(team[biz], p)
		}
	}
	for i, role := range []string{"ADMIN", "SALES_STAFF", "SALES_STAFF", "SERVICE_STAFF", "SERVICE_STAFF", "STAFF", "STAFF", "STAFF", "STAFF"} {
		p := freshPhone()
		add("bikes", fmt.Sprintf("Bikes %d", i), p, role)
		team["bikes"] = append(team["bikes"], p)
	}
	counts := map[string]int{"traders": 5, "tech": 8, "bikes": 10}
	for biz, n := range counts {
		var got int
		if err := testDB.Pool.QueryRow(context.Background(), `SELECT count(*) FROM crm.memberships m JOIN crm.workspaces w ON w.id = m.workspace_id WHERE w.code = $1 AND m.status = 'active'`, slugs[biz]).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != n {
			t.Fatalf("%s has %d people, want %d", biz, got, n)
		}
	}

	// Ram, Super Admin of two: those two URLs only.
	want(t, signInAt(t, ram, slugs["traders"]), 200, "Ram at Ajay Traders")
	want(t, signInAt(t, ram, slugs["tech"]), 200, "Ram at Ajay Tech")
	if x := signInAt(t, ram, slugs["bikes"]); x.str("code") != "account_not_found" {
		t.Fatalf("Ram is not in Ajay Bikes: %d %s", x.Status, truncate(x.Raw, 200))
	}
	// Each person only at their own business.
	want(t, signInAt(t, team["bikes"][1], slugs["bikes"]), 200, "sales staff at Ajay Bikes")
	for _, other := range []string{"traders", "tech"} {
		if x := signInAt(t, team["bikes"][1], slugs[other]); x.str("code") != "account_not_found" {
			t.Fatalf("bikes staff signed in at %s: %d", other, x.Status)
		}
	}
	if x := signInAt(t, team["traders"][0], slugs["tech"]); x.str("code") != "account_not_found" {
		t.Fatalf("Ajay Traders' admin signed in at Ajay Tech: %d", x.Status)
	}

	// Ram on "Create your own CRM": told he has an account with his two businesses, and he
	// may create his own, which is linked to him.
	ramReg := register(t, ram)
	if ramReg.Body["hasAccount"] != true || len(ramReg.list("businesses")) != 2 {
		t.Fatalf("Ram sees his two businesses: %s", truncate(ramReg.Raw, 400))
	}
	own := registerBusiness(t, ramReg.str("registrationToken"), map[string]any{"mode": "add", "name": "Ram Motors"})
	// Ram owns no business himself: his first one waits for approval like any new one.
	if own.Status != 202 {
		t.Fatalf("Ram's first own business waits for approval: %d", own.Status)
	}
	approve(t, owner, own.str("business", "tenantSlug"))
	// Ram signs in once and switches between his businesses with the same session.
	crmMod.Identity().ForgetCodeLimits("+91" + ram) // ten minutes later
	ramSession := signInAt(t, ram, own.str("business", "tenantSlug"))
	want(t, ramSession, 200, "Ram at Ram Motors")
	for _, k := range []string{"traders", "tech"} {
		want(t, call(t, "GET", crmAPI+"/w/"+slugs[k]+"/crm/leads", ramSession.str("token"), nil), 200, "Ram switches to "+k)
	}
	if x := call(t, "GET", crmAPI+"/w/"+slugs["bikes"]+"/crm/leads", ramSession.str("token"), nil); x.Status == 200 {
		t.Fatal("Ram's session reads Ajay Bikes")
	}
	// Owner console: Products are accounts with their app counts.
	cust := call(t, "GET", crmAPI+"/platform/customers", owner, nil)
	want(t, cust, 200, "owner lists products")
	appsOf := map[string]float64{}
	for _, c := range cust.list("data") {
		m := c.(map[string]any)
		appsOf[fmt.Sprint(m["phone"])] = m["apps"].(float64)
	}
	if appsOf["+91"+ajay] != 3 || appsOf["+91"+ram] != 1 {
		t.Fatalf("Ajay runs 3 apps and Ram 1: %v", appsOf)
	}
	var ramIsSuperAdmin bool
	if err := testDB.Pool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM crm.memberships m JOIN crm.workspaces w ON w.id = m.workspace_id
		JOIN crm.role_assignments ra ON ra.membership_id = m.id JOIN crm.roles r ON r.id = ra.role_id AND r.key = 'SUPER_ADMIN'
		JOIN crm.verified_identifiers v ON v.identity_id = m.identity_id AND v.kind = 'phone' AND v.value_normalized = '+91' || $2
		WHERE w.code = $1)`, own.str("business", "tenantSlug"), ram).Scan(&ramIsSuperAdmin); err != nil {
		t.Fatal(err)
	}
	if !ramIsSuperAdmin {
		t.Fatal("Ram's own business is linked to him as Super Admin")
	}
	if x := signInAt(t, ajay, own.str("business", "tenantSlug")); x.str("code") != "account_not_found" {
		t.Fatalf("Ajay is not in Ram's business: %d", x.Status)
	}

	// Another customer (Group 25): Krishnan registers Krish insurance — him, 1 admin, 2 sales
	// staff. It is fully separate from Ajay's businesses ("Ak world").
	krishnan := freshPhone()
	kr := register(t, krishnan)
	if kr.Body["hasAccount"] != false {
		t.Fatalf("Krishnan is new: %s", truncate(kr.Raw, 200))
	}
	krish := registerBusiness(t, kr.str("registrationToken"), map[string]any{"mode": "first", "name": "Krish insurance", "displayName": "Krishnan", "industry": "Insurance"}).str("business", "tenantSlug")
	approve(t, owner, krish)
	ks := signInAt(t, krishnan, krish)
	want(t, ks, 200, "Krishnan at Krish insurance")
	// The Free plan allows 3 people; Krish insurance needs 4, so the owner moves it to a bigger plan.
	var krishID string
	if err := testDB.Pool.QueryRow(context.Background(), `SELECT id::text FROM crm.workspaces WHERE code = $1`, krish).Scan(&krishID); err != nil {
		t.Fatal(err)
	}
	want(t, call(t, "PUT", crmAPI+"/platform/subscriptions/"+krishID, owner, map[string]any{"planKey": "team20", "status": "active"}), 200, "plan for Krish insurance")
	krishTeam := []string{}
	for i, role := range []string{"ADMIN", "SALES_STAFF", "SALES_STAFF"} {
		p := freshPhone()
		want(t, call(t, "POST", crmAPI+"/w/"+krish+"/admin/members", ks.str("token"), map[string]any{"displayName": fmt.Sprintf("Krish %d", i), "phone": p, "roleKey": role}), 201, "add to Krish insurance")
		krishTeam = append(krishTeam, p)
	}
	var krishPeople int
	if err := testDB.Pool.QueryRow(context.Background(), `SELECT count(*) FROM crm.memberships m JOIN crm.workspaces w ON w.id = m.workspace_id WHERE w.code = $1 AND m.status = 'active'`, krish).Scan(&krishPeople); err != nil {
		t.Fatal(err)
	}
	if krishPeople != 4 {
		t.Fatalf("Krish insurance has %d people, want 4", krishPeople)
	}
	want(t, signInAt(t, krishTeam[1], krish), 200, "Krish sales staff at Krish insurance")
	for _, other := range []string{slugs["traders"], slugs["tech"], slugs["bikes"]} {
		if x := signInAt(t, krishTeam[0], other); x.str("code") != "account_not_found" {
			t.Fatalf("Krish insurance's admin got into one of Ajay's businesses: %d", x.Status)
		}
		if x := signInAt(t, krishnan, other); x.str("code") != "account_not_found" {
			t.Fatalf("Krishnan got into one of Ajay's businesses: %d", x.Status)
		}
	}
	if x := signInAt(t, ram, krish); x.str("code") != "account_not_found" {
		t.Fatalf("Ram got into Krish insurance: %d", x.Status)
	}
	if x := signInAt(t, team["bikes"][0], krish); x.str("code") != "account_not_found" {
		t.Fatalf("Ajay Bikes' admin got into Krish insurance: %d", x.Status)
	}
	// Krishnan's own data: the record lists of Ajay's businesses refuse his session.
	if x := call(t, "GET", crmAPI+"/w/"+slugs["traders"]+"/crm/leads", ks.str("token"), nil); x.Status == 200 {
		t.Fatalf("Krishnan's session read Ajay Traders' leads: %d", x.Status)
	}
}
