package e2e

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// Runs last (file name). Ajay's plan of 2026-10-28 from end to end, on a database that
// keeps only the platform owner (fresh start with CRM_FRESH_START_PHONE=none):
//
//   - 98765 43211 (Ajay Kumar) creates Ajay Bikes on "Create your own CRM"; it waits for the
//     owner, who approves it from the lead; Ajay Traders and Ajay Tech open at once.
//   - Ajay Bikes: an admin, 2 sales staff and 2 service staff; 10 leads; buyers become an
//     account, contact and opportunity; one buys a bike; later he raises a case, the admin
//     assigns it to a service staff member, who resolves it.
//   - Traders and Tech: partner Ram is Super Admin of both, with an admin and staff; 10
//     leads each, some converted. Ram switches between the two and can't open Ajay Bikes.
//   - 98765 43210 (Priya, an insurance agent) has one business with the same kind of work.
//
// With E2E_DATABASE_URL set it fills that database instead (a dev database: it erases
// everything but the owner first). Run it alone then: go test -run TestDevData.
func TestDevDataFreshStartAndDemo(t *testing.T) {
	ctx := context.Background()
	count := func(sql string, args ...any) int {
		t.Helper()
		var n int
		if err := testDB.Pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}

	// ---- 1. Only the owner stays ----
	label := "erase-everything-owner-only-" + time.Now().Format("20060102-150405")
	if targetDB != "" {
		label = "erase-everything-2026-10-28-owner-only"
	}
	os.Setenv("CRM_FRESH_START", label)
	os.Setenv("CRM_FRESH_START_PHONE", "none")
	crmMod.FreshStart(ctx)
	os.Unsetenv("CRM_FRESH_START")
	os.Unsetenv("CRM_FRESH_START_PHONE")
	if n := count(`SELECT count(*) FROM crm.identities WHERE NOT is_platform_owner`); n != 0 {
		t.Fatalf("after an owner-only fresh start %d other people are left", n)
	}
	if n := count(`SELECT count(*) FROM crm.workspaces w WHERE NOT w.is_platform AND w.code <> 'business-card-snap'`); n != 0 {
		t.Fatalf("after an owner-only fresh start %d businesses are left", n)
	}
	if count(`SELECT count(*) FROM crm.identities WHERE is_platform_owner AND status = 'active'`) == 0 {
		t.Fatal("the owner is kept")
	}
	setReview(t, true)
	if targetDB == "" {
		defer setReview(t, false)
	}
	owner := ownerToken(t)
	want(t, call(t, "PUT", crmAPI+"/platform/plans/team20", owner, map[string]any{"name": "Team 20", "isActive": true, "position": 8, "limits": map[string]any{"members": 20}}), 200, "a plan with room for a team")
	later := func(phone string) { crmMod.Identity().ForgetCodeLimits("+91" + phone) }

	// ---- 2. Ajay: first business waits, the owner approves it from the lead ----
	ajay := "9876543211"
	reg := register(t, ajay)
	bikesApp := registerBusiness(t, reg.str("registrationToken"), map[string]any{
		"mode": "first", "name": "Ajay Bikes", "displayName": "Ajay Kumar", "personalEmail": "ajay.kumar.demo@example.com",
		"teamSize": "11-50", "industry": "Automotive", "taxIdType": "gstin", "taxId": "33ABCDE1234F1Z7", "state": "Tamil Nadu",
	})
	if bikesApp.Status != 202 {
		t.Fatalf("Ajay's first business waits for approval: %d %s", bikesApp.Status, truncate(bikesApp.Raw, 300))
	}
	bikes := slugOf(t, ajay, "Ajay Bikes")
	if !regexpCode(bikes) || !strings.HasSuffix(bikes[:12], "1z7") {
		t.Fatalf("the URL code is DDMMYY + week + last 4 of the GSTIN + running number: %s", bikes)
	}
	var leadID string
	for _, a := range call(t, "GET", crmAPI+"/platform/applications?status=pending", owner, nil).list("data") {
		if m := a.(map[string]any); m["code"] == bikes {
			leadID, _ = m["leadId"].(string)
		}
	}
	if leadID == "" {
		t.Fatal("Ajay Bikes' application is a lead in the owner console")
	}
	approve(t, owner, bikes)
	want(t, call(t, "POST", crmAPI+"/platform/crm/leads/"+leadID+"/convert", owner, map[string]any{
		"account": map[string]any{"mode": "new", "name": "Ajay Bikes", "kind": "business"}, "createContact": true}), 200, "owner converts Ajay's lead into a contact")

	// Ajay fills the form again on "/": same answer, nothing new; his URL is sent to him.
	later(ajay)
	back := register(t, ajay)
	if r := registerBusiness(t, back.str("registrationToken"), map[string]any{"mode": "first", "name": "Ajay Again"}); r.Status != 202 {
		t.Fatalf("an existing account sees the same answer: %d", r.Status)
	}
	waitAudit(t, "register.urls_sent", ajay)
	// "Forgot my CRM URL"
	want(t, call(t, "POST", crmAPI+"/public/register/request-url", "", map[string]any{"registrationToken": back.str("registrationToken")}), 200, "Ajay requests his URL")
	later(ajay)
	ajaySession := signInAt(t, ajay, bikes)
	want(t, ajaySession, 200, "Ajay signs in at Ajay Bikes")
	boss := ajaySession.str("token")
	// Step 6: Traders and Tech are added from inside and open at once.
	slug := map[string]string{"bikes": bikes}
	for k, in := range map[string]map[string]any{
		"traders": {"mode": "add", "name": "Ajay Traders", "teamSize": "2-10", "industry": "Retail", "taxIdType": "pan", "taxId": "ABCDE1234F"},
		"tech":    {"mode": "add", "name": "Ajay Tech", "teamSize": "2-10", "industry": "Technology", "taxIdType": "pan", "taxId": "ABCDE1234F"},
	} {
		r := call(t, "POST", crmAPI+"/businesses", boss, in)
		want(t, r, 201, "Ajay adds "+k)
		slug[k] = r.str("business", "tenantSlug")
	}
	for _, s := range slug {
		want(t, call(t, "PUT", crmAPI+"/platform/subscriptions/"+workspaceID(t, s), owner, map[string]any{"planKey": "team20", "status": "active"}), 200, "plan for "+s)
	}

	// ---- 3. Teams ----
	phoneOf := map[string]string{}
	add := func(biz, name, phone, role string) {
		t.Helper()
		want(t, call(t, "POST", crmAPI+"/w/"+slug[biz]+"/admin/members", boss, map[string]any{"displayName": name, "phone": phone, "roleKey": role}), 201, "add "+name+" to "+biz)
		phoneOf[name] = phone
	}
	add("bikes", "Bala Admin", "9123400001", "ADMIN")
	add("bikes", "Siva Sales", "9123400002", "SALES_STAFF")
	add("bikes", "Divya Sales", "9123400003", "SALES_STAFF")
	add("bikes", "Mani Service", "9123400004", "SERVICE_STAFF")
	add("bikes", "Ravi Service", "9123400005", "SERVICE_STAFF")
	for _, biz := range []string{"traders", "tech"} {
		add(biz, "Ram Partner", "9123400010", "SUPER_ADMIN")
	}
	add("traders", "Kavya Admin", "9123400011", "ADMIN")
	add("traders", "Arun Staff", "9123400012", "STAFF")
	add("traders", "Meena Staff", "9123400013", "STAFF")
	add("tech", "Vijay Admin", "9123400021", "ADMIN")
	add("tech", "Nila Staff", "9123400022", "STAFF")
	add("tech", "Hari Staff", "9123400023", "STAFF")

	tokens := map[string]string{}
	signAs := func(name, biz string) string {
		t.Helper()
		if tok := tokens[name]; tok != "" {
			return tok
		}
		r := signInAt(t, phoneOf[name], slug[biz])
		want(t, r, 200, name+" signs in at "+biz)
		tokens[name] = r.str("token")
		return tokens[name]
	}

	// ---- 4. Ajay Bikes: leads, buyers, a bike sold, a service case ----
	type person struct{ first, last, mobile, city string }
	bikeLeads := []person{
		{"Karthik", "Raja", "9840100001", "Chennai"}, {"Meena", "Sundar", "9840100002", "Chennai"}, {"Arjun", "Pillai", "9840100003", "Madurai"},
		{"Lakshmi", "Narayan", "9840100004", "Coimbatore"}, {"Prakash", "Iyer", "9840100005", "Salem"}, {"Fathima", "Begum", "9840100006", "Trichy"},
		{"Gokul", "Das", "9840100007", "Chennai"}, {"Revathi", "Mohan", "9840100008", "Vellore"}, {"Imran", "Khan", "9840100009", "Chennai"},
		{"Sowmya", "Ravi", "9840100010", "Erode"},
	}
	bikeLeadIDs := []string{}
	for i, p := range bikeLeads {
		by := "Siva Sales"
		if i%2 == 1 {
			by = "Divya Sales"
		}
		id := create(t, signAs(by, "bikes"), slug["bikes"], "leads", map[string]any{"firstName": p.first, "lastName": p.last, "mobile": p.mobile, "city": p.city,
			"source": "phone", "description": "Enquired about a new bike."})
		bikeLeadIDs = append(bikeLeadIDs, id)
	}
	if n := total(t, boss, slug["bikes"], "leads", ""); n != 10 {
		t.Fatalf("Ajay Bikes has %d leads, want 10", n)
	}
	// Some buy: account + contact + opportunity. The rest stay leads.
	type deal struct{ opp, account, contact string }
	deals := []deal{}
	for i, id := range bikeLeadIDs[:4] {
		p := bikeLeads[i]
		seller := "Siva Sales" // each salesperson converts their own leads
		if i%2 == 1 {
			seller = "Divya Sales"
		}
		cv := call(t, "POST", crmAPI+"/w/"+slug["bikes"]+"/crm/leads/"+id+"/convert", signAs(seller, "bikes"), map[string]any{
			"account": map[string]any{"mode": "new", "name": p.first + " " + p.last, "kind": "individual"}, "createContact": true,
			"opportunity": map[string]any{"create": true, "name": "Honda Shine 125 for " + p.first, "amount": 82000, "closeDate": time.Now().AddDate(0, 0, 14).Format("2006-01-02")}})
		want(t, cv, 200, "convert buyer "+p.first)
		if cv.str("opportunityId") == "" || cv.str("accountId") == "" || cv.str("contactId") == "" {
			t.Fatalf("a buyer becomes account, contact and opportunity: %s", truncate(cv.Raw, 300))
		}
		deals = append(deals, deal{cv.str("opportunityId"), cv.str("accountId"), cv.str("contactId")})
	}
	// Karthik buys the bike.
	want(t, edit(t, boss, slug["bikes"], "opportunities", deals[0].opp, map[string]any{"status": "closed_won"}), 200, "Karthik buys the bike")
	if get(t, boss, slug["bikes"], "opportunities", deals[0].opp)["status"] != "closed_won" {
		t.Fatal("the sale is recorded")
	}
	// After a few weeks he raises a case; the admin assigns it to Mani (service).
	admin := signAs("Bala Admin", "bikes")
	caseID := create(t, admin, slug["bikes"], "cases", map[string]any{"name": "Karthik: brake noise after first service", "accountId": deals[0].account,
		"contactId": deals[0].contact, "priority": "high", "origin": "phone", "description": "Front brake squeaks at low speed."})
	var mani string
	if err := testDB.Pool.QueryRow(ctx, `SELECT identity_id::text FROM crm.verified_identifiers WHERE kind = 'phone' AND value_normalized = '+919123400004'`).Scan(&mani); err != nil {
		t.Fatal(err)
	}
	want(t, edit(t, admin, slug["bikes"], "cases", caseID, map[string]any{"ownerId": mani, "status": "working"}), 200, "admin assigns the case to Mani")
	service := signAs("Mani Service", "bikes")
	if c := get(t, service, slug["bikes"], "cases", caseID); c["ownerId"] != mani {
		t.Fatalf("Mani sees the case assigned to him: %v", c["ownerId"])
	}
	want(t, edit(t, service, slug["bikes"], "cases", caseID, map[string]any{"status": "resolved", "resolution": "Brake pads cleaned and adjusted; test ride done."}), 200, "Mani resolves the case")
	if get(t, boss, slug["bikes"], "cases", caseID)["status"] != "resolved" {
		t.Fatal("the case is resolved")
	}

	// ---- 5. Traders and Tech: 10 leads each, 3 converted ----
	for _, biz := range []string{"traders", "tech"} {
		staff := map[string][]string{"traders": {"Arun Staff", "Meena Staff"}, "tech": {"Nila Staff", "Hari Staff"}}[biz]
		adminName := map[string]string{"traders": "Kavya Admin", "tech": "Vijay Admin"}[biz]
		ids := []string{}
		for i := 0; i < 10; i++ {
			by := staff[i%2]
			ids = append(ids, create(t, signAs(by, biz), slug[biz], "leads", map[string]any{
				"firstName": []string{"Anand", "Bharathi", "Chitra", "Deepak", "Ezhil", "Ganesh", "Hema", "Inba", "Jaya", "Kumar"}[i],
				"lastName":  strings.Title(biz) + " Lead", "organization": fmt.Sprintf("%s Customer %d", strings.Title(biz), i+1),
				"mobile": fmt.Sprintf("98%d0200%03d", map[string]int{"traders": 4, "tech": 5}[biz], i+1), "source": "referral"}))
		}
		for i, id := range ids[:3] {
			want(t, call(t, "POST", crmAPI+"/w/"+slug[biz]+"/crm/leads/"+id+"/convert", signAs(adminName, biz), map[string]any{
				"account": map[string]any{"mode": "new", "name": fmt.Sprintf("%s Customer %d", strings.Title(biz), i+1), "kind": "business"}, "createContact": true,
				"opportunity": map[string]any{"create": true, "name": fmt.Sprintf("First order %d", i+1), "amount": 25000 * (i + 1)}}), 200, "convert in "+biz)
		}
		if n := total(t, boss, slug[biz], "leads", ""); n != 10 {
			t.Fatalf("%s has %d leads, want 10", biz, n)
		}
	}

	// ---- 6. Who signs in where ----
	ram := signAs("Ram Partner", "traders")
	want(t, call(t, "GET", crmAPI+"/w/"+slug["tech"]+"/crm/leads", ram, nil), 200, "Ram switches to Ajay Tech")
	if x := call(t, "GET", crmAPI+"/w/"+slug["bikes"]+"/crm/leads", ram, nil); x.Status == 200 {
		t.Fatal("Ram's session opens Ajay Bikes")
	}
	if x := signInAt(t, phoneOf["Ram Partner"], slug["bikes"]); x.str("code") != "account_not_found" {
		t.Fatalf("Ram signs in at Ajay Bikes: %d", x.Status)
	}
	if x := signInAt(t, phoneOf["Siva Sales"], slug["traders"]); x.str("code") != "account_not_found" {
		t.Fatalf("bikes sales staff signs in at Ajay Traders: %d", x.Status)
	}
	if x := call(t, "GET", crmAPI+"/w/"+slug["tech"]+"/crm/leads", signAs("Arun Staff", "traders"), nil); x.Status == 200 {
		t.Fatal("Ajay Traders' staff reads Ajay Tech")
	}

	// ---- 7. Priya, insurance agent: one business, same work ----
	priya := "9876543210"
	pr := register(t, priya)
	insApp := registerBusiness(t, pr.str("registrationToken"), map[string]any{
		"mode": "first", "name": "Priya Insurance Services", "displayName": "Priya Sharma", "personalEmail": "priya.sharma.demo@example.com",
		"teamSize": "2-10", "industry": "Insurance", "taxIdType": "pan", "taxId": "PQRSA6789K"})
	if insApp.Status != 202 {
		t.Fatalf("Priya's business waits for approval: %d", insApp.Status)
	}
	slug["insurance"] = slugOf(t, priya, "Priya Insurance Services")
	if x := signInAt(t, priya, slug["insurance"]); x.str("code") != "business_pending" {
		t.Fatalf("Priya can't sign in before approval: %d %s", x.Status, truncate(x.Raw, 200))
	}
	approve(t, owner, slug["insurance"])
	want(t, call(t, "PUT", crmAPI+"/platform/subscriptions/"+workspaceID(t, slug["insurance"]), owner, map[string]any{"planKey": "team20", "status": "active"}), 200, "plan for Priya")
	later(priya)
	ps := signInAt(t, priya, slug["insurance"])
	want(t, ps, 200, "Priya signs in")
	boss = ps.str("token")
	add("insurance", "Nisha Admin", "9123400031", "ADMIN")
	add("insurance", "Rahul Sales", "9123400032", "SALES_STAFF")
	add("insurance", "Sneha Sales", "9123400033", "SALES_STAFF")
	add("insurance", "Tarun Service", "9123400034", "SERVICE_STAFF")
	insLeads := []string{}
	for i := 0; i < 10; i++ {
		by := []string{"Rahul Sales", "Sneha Sales"}[i%2]
		insLeads = append(insLeads, create(t, signAs(by, "insurance"), slug["insurance"], "leads", map[string]any{
			"firstName": []string{"Asha", "Bala", "Charan", "Dhivya", "Elango", "Farhan", "Geetha", "Harish", "Indu", "Joseph"}[i],
			"lastName":  "Policy Lead", "mobile": fmt.Sprintf("984030%04d", i+1), "source": "referral",
			"description": []string{"Health cover for family", "Term life 1 crore", "Bike insurance renewal", "Car insurance"}[i%4]}))
	}
	policy := ""
	var policyAccount, policyContact string
	for i, id := range insLeads[:3] {
		cv := call(t, "POST", crmAPI+"/w/"+slug["insurance"]+"/crm/leads/"+id+"/convert", signAs([]string{"Rahul Sales", "Sneha Sales"}[i%2], "insurance"), map[string]any{
			"account": map[string]any{"mode": "new", "name": fmt.Sprintf("Policy holder %d", i+1), "kind": "individual"}, "createContact": true,
			"opportunity": map[string]any{"create": true, "name": fmt.Sprintf("Family health policy %d", i+1), "amount": 18000 + 4000*i}})
		want(t, cv, 200, "convert policy buyer")
		if i == 0 {
			policy, policyAccount, policyContact = cv.str("opportunityId"), cv.str("accountId"), cv.str("contactId")
		}
	}
	want(t, edit(t, boss, slug["insurance"], "opportunities", policy, map[string]any{"status": "closed_won"}), 200, "the first policy is sold")
	claim := create(t, signAs("Nisha Admin", "insurance"), slug["insurance"], "cases", map[string]any{"name": "Claim: hospital bill reimbursement",
		"accountId": policyAccount, "contactId": policyContact, "priority": "medium", "origin": "phone"})
	var tarun string
	if err := testDB.Pool.QueryRow(ctx, `SELECT identity_id::text FROM crm.verified_identifiers WHERE kind = 'phone' AND value_normalized = '+919123400034'`).Scan(&tarun); err != nil {
		t.Fatal(err)
	}
	want(t, edit(t, signAs("Nisha Admin", "insurance"), slug["insurance"], "cases", claim, map[string]any{"ownerId": tarun, "status": "working"}), 200, "admin assigns the claim")
	want(t, edit(t, signAs("Tarun Service", "insurance"), slug["insurance"], "cases", claim, map[string]any{"status": "resolved", "resolution": "Claim approved and paid to the policy holder."}), 200, "the claim is settled")
	for _, other := range []string{"bikes", "traders", "tech"} {
		if x := call(t, "GET", crmAPI+"/w/"+slug[other]+"/crm/leads", boss, nil); x.Status == 200 {
			t.Fatalf("Priya reads %s", other)
		}
	}
	if x := call(t, "GET", crmAPI+"/w/"+slug["insurance"]+"/crm/leads", ram, nil); x.Status == 200 {
		t.Fatal("Ram reads Priya's leads")
	}

	// ---- 8. Owner console: 2 products (accounts), 4 apps ----
	cust := call(t, "GET", crmAPI+"/platform/customers", owner, nil)
	appsOf := map[string]float64{}
	for _, c := range cust.list("data") {
		m := c.(map[string]any)
		appsOf[fmt.Sprint(m["phone"])] = m["apps"].(float64)
	}
	if len(appsOf) != 2 || appsOf["+91"+ajay] != 3 || appsOf["+91"+priya] != 1 {
		t.Fatalf("Products: Ajay with 3 apps, Priya with 1: %v", appsOf)
	}
	if all := call(t, "GET", crmAPI+"/platform/applications", owner, nil); len(all.list("data")) != 4 {
		t.Fatalf("Apps lists %d businesses, want 4: %s", len(all.list("data")), truncate(all.Raw, 6000))
	}
	t.Logf("business URLs: %v", slug)
}

func regexpCode(s string) bool {
	if len(s) != 18 {
		return false
	}
	for i, r := range s {
		digit := r >= '0' && r <= '9'
		if (i < 8 || i >= 12) && !digit {
			return false
		}
	}
	return true
}
