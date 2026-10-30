package e2e

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"cardflow-backend/internal/crm/billing"
)

// fakeRazorpay answers POST /v1/orders like Razorpay does and remembers the amounts.
type fakeRazorpay struct {
	mu      sync.Mutex
	n       int
	amounts map[string]int64
}

func (f *fakeRazorpay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/orders" || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	if u, p, ok := r.BasicAuth(); !ok || u != "rzp_test_e2e" || p != "e2e-secret" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var in struct {
		Amount int64 `json:"amount"`
	}
	b, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(b, &in)
	f.mu.Lock()
	f.n++
	id := fmt.Sprintf("order_e2e%06d", f.n)
	f.amounts[id] = in.Amount
	f.mu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "amount": in.Amount, "currency": "INR", "status": "created"})
}

func sign(secret, msg string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(msg))
	return hex.EncodeToString(m.Sum(nil))
}

// Members are paid for with Razorpay (D-164): unpaid members are locked out of the business
// (402) except billing; a member pays for themselves and those below them, a Super Admin
// for anyone and several at once; the webhook settles a payment the browser didn't report;
// time running out locks again; the owner sets prices and sees payments.
func TestMembersArePaidForWithRazorpay(t *testing.T) {
	ctx := context.Background()
	fake := &fakeRazorpay{amounts: map[string]int64{}}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	b := crmMod.Billing()
	b.UseConfig(billing.Config{KeyID: "rzp_test_e2e", KeySecret: "e2e-secret", WebhookSecret: "e2e-hook", APIBase: srv.URL})
	defer b.UseConfig(billing.Config{})

	bossPhone := freshPhone()
	boss := signIn(t, bossPhone, "Billing Boss")
	ws := newBusiness(t, boss, "Paid Members Ltd")

	// Locked until paid: records answer 402, the context says so, billing still works.
	if r := call(t, "GET", crmAPI+"/w/"+ws+"/crm/leads", boss, nil); r.Status != 402 || r.str("code") != "payment_required" {
		t.Fatalf("an unpaid Super Admin is locked out: %d %s", r.Status, truncate(r.Raw, 200))
	}
	ctxR := call(t, "GET", crmAPI+"/w/"+ws+"/context", boss, nil)
	want(t, ctxR, 200, "the context answers while locked")
	if ctxR.at("billing", "locked") != true || ctxR.at("billing", "enforced") != true {
		t.Fatalf("the context shows the lock: %s", truncate(ctxR.Raw, 300))
	}
	ov := call(t, "GET", crmAPI+"/w/"+ws+"/billing", boss, nil)
	want(t, ov, 200, "billing overview while locked")
	members := ov.list("members")
	if len(members) != 1 {
		t.Fatalf("only the Super Admin so far: %s", truncate(ov.Raw, 400))
	}
	bossM := members[0].(map[string]any)["membershipId"].(string)

	// Pay for yourself, 1 month: ₹499.
	o := call(t, "POST", crmAPI+"/w/"+ws+"/billing/orders", boss, map[string]any{"period": "1m", "membershipIds": []string{bossM}})
	want(t, o, 201, "order for one month")
	order := o.str("orderId")
	if fake.amounts[order] != 49900 || o.at("amount").(float64) != 49900 || o.str("keyId") != "rzp_test_e2e" {
		t.Fatalf("one member for a month is ₹499: %s", truncate(o.Raw, 300))
	}
	if r := call(t, "POST", crmAPI+"/w/"+ws+"/billing/verify", boss, map[string]any{"razorpay_order_id": order, "razorpay_payment_id": "pay_fake", "razorpay_signature": "00"}); r.Status != 400 {
		t.Fatalf("a wrong signature is refused: %d", r.Status)
	}
	if r := call(t, "GET", crmAPI+"/w/"+ws+"/crm/leads", boss, nil); r.Status != 402 {
		t.Fatalf("still locked after a refused payment: %d", r.Status)
	}
	v := call(t, "POST", crmAPI+"/w/"+ws+"/billing/verify", boss, map[string]any{"razorpay_order_id": order, "razorpay_payment_id": "pay_e2e1", "razorpay_signature": sign("e2e-secret", order+"|pay_e2e1")})
	want(t, v, 200, "a signed payment is accepted")
	want(t, call(t, "GET", crmAPI+"/w/"+ws+"/crm/leads", boss, nil), 200, "paid: the business opens")
	if r := inbox(t, boss, ws, "", titled("Payment received: ₹499")); findNote(r, "Payment received: ₹499") == nil {
		t.Fatalf("the payer is told the payment went through (D-169): %s", truncate(r.Raw, 300))
	}
	// Reporting it twice changes nothing.
	want(t, call(t, "POST", crmAPI+"/w/"+ws+"/billing/verify", boss, map[string]any{"razorpay_order_id": order, "razorpay_payment_id": "pay_e2e1", "razorpay_signature": sign("e2e-secret", order+"|pay_e2e1")}), 200, "verify again")
	var until time.Time
	if err := testDB.Pool.QueryRow(ctx, `SELECT paid_until FROM crm.memberships WHERE id = $1`, bossM).Scan(&until); err != nil {
		t.Fatal(err)
	}
	if d := time.Until(until); d < 27*24*time.Hour || d > 32*24*time.Hour {
		t.Fatalf("one month paid (once): %v", d)
	}

	// Any number of members (no plan limit), each locked until paid.
	adminPhone, staffPhone, staff2Phone := freshPhone(), freshPhone(), freshPhone()
	for _, m := range []struct{ name, phone, role string }{{"Pay Admin", adminPhone, "ADMIN"}, {"Pay Staff", staffPhone, "STAFF"}, {"Pay Staff Two", staff2Phone, "STAFF"}} {
		want(t, call(t, "POST", crmAPI+"/w/"+ws+"/admin/members", boss, map[string]any{"displayName": m.name, "phone": m.phone, "roleKey": m.role}), 201, "add "+m.name)
	}
	staff := signIn(t, staffPhone, "Pay Staff")
	if r := call(t, "GET", crmAPI+"/w/"+ws+"/crm/leads", staff, nil); r.Status != 402 {
		t.Fatalf("a new member is locked until paid: %d", r.Status)
	}
	ids := map[string]string{}
	canPay := map[string]bool{}
	for _, m := range call(t, "GET", crmAPI+"/w/"+ws+"/billing", staff, nil).list("members") {
		mm := m.(map[string]any)
		ids[mm["name"].(string)] = mm["membershipId"].(string)
		canPay[mm["name"].(string)] = mm["canPay"].(bool)
	}
	if !canPay["Pay Staff"] || canPay["Pay Admin"] || canPay["Billing Boss"] {
		t.Fatalf("staff may pay only for themselves: %v", canPay)
	}
	if r := call(t, "POST", crmAPI+"/w/"+ws+"/billing/orders", staff, map[string]any{"period": "1m", "membershipIds": []string{ids["Pay Admin"]}}); r.Status != 403 {
		t.Fatalf("staff can't pay for an admin: %d", r.Status)
	}
	admin := signIn(t, adminPhone, "Pay Admin")
	adminView := map[string]bool{}
	for _, m := range call(t, "GET", crmAPI+"/w/"+ws+"/billing", admin, nil).list("members") {
		mm := m.(map[string]any)
		adminView[mm["name"].(string)] = mm["canPay"].(bool)
	}
	if !adminView["Pay Admin"] || !adminView["Pay Staff"] || adminView["Billing Boss"] {
		t.Fatalf("an admin pays for themselves and staff, not the Super Admin: %v", adminView)
	}

	// The Super Admin pays for three people at once, for 3 months: 3 × ₹1,399; the browser
	// never reports back, Razorpay's webhook does.
	bulk := call(t, "POST", crmAPI+"/w/"+ws+"/billing/orders", boss, map[string]any{"period": "3m", "membershipIds": []string{ids["Pay Admin"], ids["Pay Staff"], ids["Pay Staff Two"]}})
	want(t, bulk, 201, "bulk order")
	if fake.amounts[bulk.str("orderId")] != 3*139900 {
		t.Fatalf("three members for 3 months is ₹4,197: %d", fake.amounts[bulk.str("orderId")])
	}
	event, _ := json.Marshal(map[string]any{"event": "payment.captured", "payload": map[string]any{"payment": map[string]any{"entity": map[string]any{"id": "pay_e2e2", "order_id": bulk.str("orderId"), "status": "captured"}}}})
	hook := func(sig string) int {
		req, _ := http.NewRequest("POST", baseURL+crmAPI+"/public/billing/razorpay", bytes.NewReader(event))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Razorpay-Signature", sig)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if s := hook("bad"); s != 403 {
		t.Fatalf("an unsigned webhook is refused: %d", s)
	}
	if s := hook(sign("e2e-hook", string(event))); s != 200 {
		t.Fatalf("the signed webhook settles the payment: %d", s)
	}
	want(t, call(t, "GET", crmAPI+"/w/"+ws+"/crm/leads", staff, nil), 200, "staff opens the business after the bulk payment")
	want(t, call(t, "GET", crmAPI+"/w/"+ws+"/crm/leads", admin, nil), 200, "admin too")
	if r := inbox(t, staff, ws, "", titled("Your access is paid until")); findNote(r, "Your access is paid until") == nil || !strings.Contains(findNote(r, "Your access is paid until")["body"].(string), "Billing Boss paid for 3 months") {
		t.Fatalf("each person paid for hears it, with who paid (D-169): %s", truncate(r.Raw, 300))
	}
	if r := inbox(t, boss, ws, "", titled("Payment received: ₹4,197")); findNote(r, "Payment received: ₹4,197") == nil {
		t.Fatalf("the payer is told when the webhook settles it: %s", truncate(r.Raw, 300))
	}

	// Time runs out: locked again.
	if _, err := testDB.Pool.Exec(ctx, `UPDATE crm.memberships SET paid_until = now() - interval '1 minute' WHERE id = $1`, ids["Pay Staff"]); err != nil {
		t.Fatal(err)
	}
	if r := call(t, "GET", crmAPI+"/w/"+ws+"/crm/leads", staff, nil); r.Status != 402 {
		t.Fatalf("an expired member is locked again: %d", r.Status)
	}

	// The owner: prices and payments. New prices apply to new orders.
	owner := ownerSignIn(t)
	st := call(t, "GET", crmAPI+"/platform/billing", owner, nil)
	want(t, st, 200, "owner billing")
	if st.at("collectedPaise").(float64) != 49900+3*139900 || st.at("testMode") != true {
		t.Fatalf("the owner sees what was collected: %s", truncate(st.Raw, 300))
	}
	want(t, call(t, "PUT", crmAPI+"/platform/billing", owner, map[string]any{"enabled": true, "prices": map[string]int{"1m": 0, "3m": 1, "6m": 1, "12m": 1}}), 422, "a zero price is refused")
	want(t, call(t, "PUT", crmAPI+"/platform/billing", owner, map[string]any{"enabled": true, "prices": map[string]int{"1m": 599, "3m": 1499, "6m": 2699, "12m": 4999}}), 200, "owner sets prices")
	o2 := call(t, "POST", crmAPI+"/w/"+ws+"/billing/orders", staff, map[string]any{"period": "1m", "membershipIds": []string{ids["Pay Staff"]}})
	want(t, o2, 201, "staff pays for themselves")
	if fake.amounts[o2.str("orderId")] != 59900 {
		t.Fatalf("the new price applies: %d", fake.amounts[o2.str("orderId")])
	}
	// Switched off: nobody is locked.
	want(t, call(t, "PUT", crmAPI+"/platform/billing", owner, map[string]any{"enabled": false, "prices": map[string]int{"1m": 499, "3m": 1399, "6m": 2599, "12m": 4799}}), 200, "owner switches charging off")
	want(t, call(t, "GET", crmAPI+"/w/"+ws+"/crm/leads", staff, nil), 200, "charging off: nobody is locked")
	want(t, call(t, "PUT", crmAPI+"/platform/billing", owner, map[string]any{"enabled": true, "prices": map[string]int{"1m": 499, "3m": 1399, "6m": 2599, "12m": 4799}}), 200, "back on")
	// The platform's own workspace is never charged.
	want(t, call(t, "GET", crmAPI+"/platform/crm/leads", owner, nil), 200, "the owner console is never locked")
}
