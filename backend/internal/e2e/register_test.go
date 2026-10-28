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

	// 1. After the code the page learns nothing about anyone (D-161): just a token.
	first := register(t, ajay)
	for _, k := range []string{"hasAccount", "person", "businesses", "pending"} {
		if _, ok := first.Body[k]; ok {
			t.Fatalf("the code check must not say anything about the person (%s): %s", k, truncate(first.Raw, 300))
		}
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
	// Step 3–5: "we will review your application"; the URL isn't shown and nobody can sign
	// in there yet; the application is a lead in the owner console, which the owner approves.
	if traders.Status != 202 || traders.str("business", "tenantSlug") != "" {
		t.Fatalf("a first business waits for approval, its URL unseen: %d %s", traders.Status, truncate(traders.Raw, 300))
	}
	tradersSlug := slugOf(t, ajay, "Ajay Traders")
	if x := signInAt(t, ajay, tradersSlug); x.str("code") != "business_pending" {
		t.Fatalf("sign-in at a business in review: %d %s", x.Status, truncate(x.Raw, 200))
	}
	if x := call(t, "GET", crmAPI+"/public/tenants/"+tradersSlug, "", nil); x.str("code") != "business_pending" {
		t.Fatalf("its URL says it waits for approval: %d %s", x.Status, truncate(x.Raw, 200))
	}
	// Sending the form again while in review: the same answer, nothing new is made, and
	// they are told it is being reviewed.
	waiting := register(t, ajay)
	crmMod.Identity().ForgetCodeLimits("+91" + ajay) // ten minutes later
	if r := registerBusiness(t, waiting.str("registrationToken"), map[string]any{"mode": "first", "name": "Ajay Traders Again"}); r.Status != 202 {
		t.Fatalf("a second application answers the same: %d", r.Status)
	}
	if n := ownedBy(t, ajay); n != 1 {
		t.Fatalf("a repeat application creates nothing: %d businesses", n)
	}
	waitAudit(t, "register.repeat_application", ajay)
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
	converted := call(t, "POST", crmAPI+"/platform/crm/leads/"+leadID+"/convert", owner, map[string]any{"account": map[string]any{"mode": "new", "name": "Ajay Traders", "kind": "business"}, "createContact": true})
	want(t, converted, 200, "owner converts the application lead")

	// 2. Step 4a: Ajay, who already has a CRM, fills the form again. The page says the
	// same; nothing is created; his URLs go to him by SMS, WhatsApp and email.
	again := register(t, ajay)
	if r := registerBusiness(t, again.str("registrationToken"), map[string]any{"mode": "first", "name": "Ajay Something"}); r.Status != 202 {
		t.Fatalf("an existing account sees the same answer: %d", r.Status)
	}
	waitAudit(t, "register.urls_sent", ajay)
	if n := ownedBy(t, ajay); n != 1 {
		t.Fatalf("an existing account's form creates nothing: %d businesses", n)
	}
	// "Forgot my CRM URL": after the code, the URLs are sent; the answer is the same for anyone.
	want(t, call(t, "POST", crmAPI+"/public/register/request-url", "", map[string]any{"registrationToken": again.str("registrationToken")}), 200, "request my URL")
	stranger := register(t, freshPhone())
	want(t, call(t, "POST", crmAPI+"/public/register/request-url", "", map[string]any{"registrationToken": stranger.str("registrationToken")}), 200, "request a URL for a number with no CRM")

	// 3. Step 6: more businesses are added from inside and open at once.
	crmMod.Identity().ForgetCodeLimits("+91" + ajay)
	session := signInAt(t, ajay, tradersSlug)
	want(t, session, 200, "Ajay signs in at Ajay Traders' URL")
	boss := session.str("token")
	slugs := map[string]string{"traders": tradersSlug}
	for k, name := range map[string]string{"tech": "Ajay Tech", "bikes": "Ajay Bikes"} {
		r := call(t, "POST", crmAPI+"/businesses", boss, map[string]any{"mode": "add", "name": name})
		want(t, r, 201, "Ajay adds "+name)
		slugs[k] = r.str("business", "tenantSlug")
	}
	if slugs["traders"] == slugs["tech"] || slugs["tech"] == slugs["bikes"] {
		t.Fatalf("each business has its own URL: %v", slugs)
	}
	if n := ownedBy(t, ajay); n != 3 {
		t.Fatalf("all three businesses are Ajay's: %d", n)
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

	// Ram on "Create your own CRM": he is in Ajay's businesses but hasn't created a CRM
	// himself, so he goes through the same new-business flow; it is linked to him.
	ramReg := register(t, ram)
	own := registerBusiness(t, ramReg.str("registrationToken"), map[string]any{"mode": "first", "name": "Ram Motors"})
	ramMotors := slugOf(t, ram, "Ram Motors")
	// Ram owns no business himself: his first one waits for approval like any new one.
	if own.Status != 202 {
		t.Fatalf("Ram's first own business waits for approval: %d", own.Status)
	}
	approve(t, owner, ramMotors)
	// Ram signs in once and switches between his businesses with the same session.
	crmMod.Identity().ForgetCodeLimits("+91" + ram) // ten minutes later
	ramSession := signInAt(t, ram, ramMotors)
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
	// Lead, contact and account show the person's businesses (D-162).
	for object, id := range map[string]string{"leads": leadID, "contacts": converted.str("contactId"), "accounts": converted.str("accountId")} {
		x := call(t, "GET", crmAPI+"/platform/records/"+object+"/"+id+"/businesses", owner, nil)
		want(t, x, 200, "businesses on the "+object+" record")
		if x.str("person", "phone") != "+91"+ajay || len(x.list("apps")) != 3 {
			t.Fatalf("the %s record shows Ajay and his 3 businesses: %s", object, truncate(x.Raw, 400))
		}
	}
	// Owner console Users: only the owner's own team, never customers' people (D-162).
	teamList := call(t, "GET", crmAPI+"/platform/users?scope=team&limit=200", owner, nil)
	want(t, teamList, 200, "owner's team")
	if strings.Contains(teamList.Raw, ajay) || strings.Contains(teamList.Raw, ram) || len(teamList.list("data")) == 0 {
		t.Fatalf("the team list holds the owner's team only: %s", truncate(teamList.Raw, 400))
	}
	var ramIsSuperAdmin bool
	if err := testDB.Pool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM crm.memberships m JOIN crm.workspaces w ON w.id = m.workspace_id
		JOIN crm.role_assignments ra ON ra.membership_id = m.id JOIN crm.roles r ON r.id = ra.role_id AND r.key = 'SUPER_ADMIN'
		JOIN crm.verified_identifiers v ON v.identity_id = m.identity_id AND v.kind = 'phone' AND v.value_normalized = '+91' || $2
		WHERE w.code = $1)`, ramMotors, ram).Scan(&ramIsSuperAdmin); err != nil {
		t.Fatal(err)
	}
	if !ramIsSuperAdmin {
		t.Fatal("Ram's own business is linked to him as Super Admin")
	}
	if x := signInAt(t, ajay, ramMotors); x.str("code") != "account_not_found" {
		t.Fatalf("Ajay is not in Ram's business: %d", x.Status)
	}

	// Another customer (Group 25): Krishnan registers Krish insurance — him, 1 admin, 2 sales
	// staff. It is fully separate from Ajay's businesses ("Ak world").
	krishnan := freshPhone()
	kr := register(t, krishnan)
	registerBusiness(t, kr.str("registrationToken"), map[string]any{"mode": "first", "name": "Krish insurance", "displayName": "Krishnan", "industry": "Insurance"})
	krish := slugOf(t, krishnan, "Krish insurance")
	approve(t, owner, krish)
	ks := signInAt(t, krishnan, krish)
	want(t, ks, 200, "Krishnan at Krish insurance")
	// The Free plan allows 3 people; Krish insurance needs 4, so the owner moves it to a bigger plan.
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
