package e2e

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// Creating a business asks for the person, their address, the business and its address;
// each business gets its own login address and can add its own domain (D-155).
func TestOnboardingDetailsTenantLoginAndCardImages(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/641001") {
			io.WriteString(w, `[{"Status":"Success","PostOffice":[{"Name":"Coimbatore","District":"Coimbatore","Block":"Coimbatore South","State":"Tamil Nadu","Country":"India"},{"Name":"Gandhipuram","District":"Coimbatore","State":"Tamil Nadu","Country":"India"}]}]`)
			return
		}
		io.WriteString(w, `[{"Status":"Error","PostOffice":null}]`)
	}))
	defer stub.Close()
	os.Setenv("CRM_PINCODE_API", stub.URL)
	defer os.Unsetenv("CRM_PINCODE_API")

	phone := freshPhone()
	me := signIn(t, phone, "")
	p := call(t, "GET", crmAPI+"/geo/pincode/641001", me, nil)
	want(t, p, 200, "pincode")
	if p.str("state") != "Tamil Nadu" || p.str("city") != "Coimbatore" || len(p.list("areas")) != 2 {
		t.Fatalf("pincode fills state, city and areas: %s", p.Raw)
	}
	mustRefuse(t, "a pincode with 5 digits", call(t, "GET", crmAPI+"/geo/pincode/64100", me, nil), "postalCode")
	mustRefuse(t, "an unknown pincode", call(t, "GET", crmAPI+"/geo/pincode/999999", me, nil), "postalCode")
	if s := call(t, "GET", crmAPI+"/geo/states", me, nil); len(s.list("data")) != 36 {
		t.Fatalf("36 states and union territories: %d", len(s.list("data")))
	}

	addr := map[string]any{"addressLine": "12, Lakshmi Nagar", "landmark": "Opp. bus stand", "area": "Gandhipuram", "city": "Coimbatore", "state": "tamilnadu", "postalCode": "641001", "country": "India"}
	bad := map[string]any{"name": "Ajay Traders", "displayName": "Ajay Krishnan", "dateOfBirth": "2020-01-01", "personalEmail": "not-an-email",
		"personalAddress": map[string]any{"postalCode": "12", "state": "Narnia", "country": "India"}, "gstin": "33AAPFU0939F1ZV", "address": addr, "whatsapp": "123"}
	r := call(t, "POST", crmAPI+"/businesses", me, bad)
	for _, k := range []string{"dateOfBirth", "personalEmail", "personal.postalCode", "personal.state", "gstin", "whatsapp"} {
		if !strings.Contains(r.Raw, `"`+k+`"`) {
			t.Fatalf("%s is checked: %d %s", k, r.Status, truncate(r.Raw, 500))
		}
	}
	good := map[string]any{"name": "Ajay Traders", "industry": "Retail", "displayName": "Ajay Krishnan", "dateOfBirth": "1990-03-06", "personalEmail": "Ajay.K@Example.com",
		"personalAddress": addr, "phone": phone, "whatsapp": phone, "email": "sales@ajaytraders.in", "website": "ajaytraders.in", "gstin": "33aaaca1234a1zf", "address": addr}
	// 33AAACA1234A1Z? — find the right check character so the test uses a valid GSTIN.
	good["gstin"] = validGSTIN("33AAACA1234A1Z")
	r = call(t, "POST", crmAPI+"/businesses", me, good)
	want(t, r, 201, "create with every detail")
	code := r.str("business", "code")
	b := call(t, "GET", crmAPI+"/w/"+code+"/business", me, nil)
	pr := b.Body["profile"].(map[string]any)
	if pr["state"] != "Tamil Nadu" || pr["postalCode"] != "641001" || pr["landmark"] != "Opp. bus stand" || pr["website"] != "https://ajaytraders.in" ||
		pr["gstin"] != good["gstin"] || !strings.Contains(pr["address"].(string), "Near Opp. bus stand") || pr["whatsapp"] != "+91"+phone[len(phone)-10:] {
		t.Fatalf("the business profile has the tidied details: %v", pr)
	}
	if b.str("loginPath") != "/"+code+"/login" {
		t.Fatalf("each business has its own login address: %s", b.str("loginPath"))
	}
	d := call(t, "GET", crmAPI+"/me/details", me, nil)
	if d.str("displayName") != "Ajay Krishnan" || d.str("dateOfBirth") != "1990-03-06" || d.str("personalEmail") != "ajay.k@example.com" || d.str("address", "city") != "Coimbatore" || d.str("phone") == "" {
		t.Fatalf("the person's details are kept for next time: %s", d.Raw)
	}
	// The login page of the business finds it without signing in; others don't leak.
	pub := call(t, "GET", crmAPI+"/public/tenants/"+code, "", nil)
	if pub.Status != 200 || pub.str("name") != "Ajay Traders" || pub.str("home") != "/crm/w/"+code+"/home" {
		t.Fatalf("public tenant lookup: %d %s", pub.Status, pub.Raw)
	}
	want(t, call(t, "GET", crmAPI+"/public/tenants/no-such-business-x", "", nil), 404, "unknown business")
	// Own domain: unique, and found by host.
	want(t, call(t, "PATCH", crmAPI+"/w/"+code+"/business", me, map[string]any{"profile": map[string]any{"customDomain": "https://CRM.AjayTraders.in/login"}}), 200, "domain")
	if h := call(t, "GET", crmAPI+"/public/tenants/by-host?host=crm.ajaytraders.in:443", "", nil); h.str("code") != code {
		t.Fatalf("found by its domain: %s", h.Raw)
	}
	other := signIn(t, freshPhone(), "Someone Else")
	oc := newBusiness(t, other, "Other Traders")
	mustRefuse(t, "a domain another business uses", call(t, "PATCH", crmAPI+"/w/"+oc+"/business", other, map[string]any{"profile": map[string]any{"customDomain": "crm.ajaytraders.in"}}), "profile.customDomain")
	mustRefuse(t, "an invalid GSTIN on edit", call(t, "PATCH", crmAPI+"/w/"+code+"/business", me, map[string]any{"profile": map[string]any{"gstin": "33AAACA1234A1Z0"}}), "profile.gstin")
	// Business card images: owner uploads, members see, others can't.
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00\x90wS\xde\x00\x00\x00\x0cIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")
	if st := upload(t, crmAPI+"/w/"+code+"/business/card-images/front", me, png); st != 200 {
		t.Fatalf("upload the front: %d", st)
	}
	if st := upload(t, crmAPI+"/w/"+code+"/business/card-images/front", me, []byte("plain text, not an image")); st != 422 {
		t.Fatalf("only images: %d", st)
	}
	if b := call(t, "GET", crmAPI+"/w/"+code+"/business", me, nil); b.Body["cardImages"].(map[string]any)["front"] != true || b.Body["cardImages"].(map[string]any)["back"] != false {
		t.Fatalf("the business knows which sides it has: %v", b.Body["cardImages"])
	}
	if g := call(t, "GET", crmAPI+"/w/"+code+"/business/card-images/front", other, nil); g.Status != 403 && g.Status != 404 {
		t.Fatalf("another business's owner can't see the card: %d", g.Status)
	}
	// A second business offers the same personal details (the form pre-fills from /me/details).
	r = call(t, "POST", crmAPI+"/businesses", me, map[string]any{"name": "Ajay Traders Branch"})
	want(t, r, 201, "second business with just a name")
}

func validGSTIN(first14 string) string {
	const chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	sum := 0
	for i := 0; i < 14; i++ {
		v := strings.IndexByte(chars, first14[i]) * (1 + i%2)
		sum += v/36 + v%36
	}
	return first14 + string(chars[(36-sum%36)%36])
}

func upload(t *testing.T, url, token string, data []byte) int {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "card.png")
	fw.Write(data)
	mw.Close()
	req, _ := http.NewRequest("PUT", baseURL+url, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Session-Transport", "bearer")
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}
