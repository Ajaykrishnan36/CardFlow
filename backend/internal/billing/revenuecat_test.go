package billing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cardflow-backend/internal/config"
)

var now = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

func ms(t time.Time) *int64     { v := t.UnixMilli(); return &v }
func tp(t time.Time) *time.Time { return &t }

func TestStateFromEvent(t *testing.T) {
	future := now.Add(30 * 24 * time.Hour)
	past := now.Add(-time.Hour)
	cases := []struct {
		name      string
		ev        Event
		status    string
		premium   bool
		willRenew bool
		product   string
	}{
		{"initial purchase", Event{Type: "INITIAL_PURCHASE", ProductID: "monthly", ExpirationAtMs: ms(future)}, StatusActive, true, true, "monthly"},
		{"renewal", Event{Type: "RENEWAL", ProductID: "monthly", ExpirationAtMs: ms(future)}, StatusActive, true, true, "monthly"},
		{"cancellation keeps access until expiry", Event{Type: "CANCELLATION", ProductID: "monthly", ExpirationAtMs: ms(future)}, StatusCancelled, true, false, "monthly"},
		{"refund (cancellation already expired)", Event{Type: "CANCELLATION", ProductID: "monthly", ExpirationAtMs: ms(past), CancelReason: "CUSTOMER_SUPPORT"}, StatusExpired, false, false, "monthly"},
		{"uncancellation", Event{Type: "UNCANCELLATION", ProductID: "monthly", ExpirationAtMs: ms(future)}, StatusActive, true, true, "monthly"},
		{"expiration", Event{Type: "EXPIRATION", ProductID: "monthly", ExpirationAtMs: ms(past)}, StatusExpired, false, false, "monthly"},
		{"billing issue in grace period", Event{Type: "BILLING_ISSUE", ExpirationAtMs: ms(past), GracePeriodExpirationMs: ms(future)}, StatusBillingIssue, true, false, ""},
		{"billing issue without grace", Event{Type: "BILLING_ISSUE", ExpirationAtMs: ms(past)}, StatusBillingIssue, false, false, ""},
		{"product change", Event{Type: "PRODUCT_CHANGE", ProductID: "monthly", NewProductID: "annual", ExpirationAtMs: ms(future)}, StatusActive, true, true, "annual"},
		{"lifetime non-renewing", Event{Type: "NON_RENEWING_PURCHASE", ProductID: "lifetime"}, StatusActive, true, false, "lifetime"},
		{"paused", Event{Type: "SUBSCRIPTION_PAUSED", ExpirationAtMs: ms(future)}, StatusCancelled, true, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, ok := StateFromEvent(c.ev, now)
			if !ok {
				t.Fatal("expected a state")
			}
			if st.Status != c.status || st.IsPremium != c.premium || st.WillRenew != c.willRenew || st.ProductID != c.product {
				t.Fatalf("got %+v, want status=%s premium=%v willRenew=%v product=%s", st, c.status, c.premium, c.willRenew, c.product)
			}
		})
	}
	if _, ok := StateFromEvent(Event{Type: "TEST"}, now); ok {
		t.Fatal("TEST events carry no state")
	}
}

func TestEntitlementFilterAndUserIDs(t *testing.T) {
	premium := "premium"
	if !(Event{EntitlementIDs: []string{"premium"}}).TouchesEntitlement(premium) {
		t.Fatal("premium entitlement should match")
	}
	if (Event{EntitlementIDs: []string{"other"}}).TouchesEntitlement(premium) {
		t.Fatal("other entitlement should not match")
	}
	if !(Event{}).TouchesEntitlement(premium) {
		t.Fatal("events without entitlement ids are relevant")
	}
	legacy := "premium"
	if !(Event{EntitlementID: &legacy}).TouchesEntitlement(premium) {
		t.Fatal("legacy entitlement_id should match")
	}
	ids := Event{AppUserID: "a", OriginalAppUserID: "a", Aliases: []string{"b", "a", ""}}.CandidateUserIDs()
	if strings.Join(ids, ",") != "a,b" {
		t.Fatalf("got %v", ids)
	}
	if IsAccessEvent("INVOICE_ISSUANCE") || !IsAccessEvent("EXPIRATION") {
		t.Fatal("access event table wrong")
	}
}

func TestStateFromSubscriber(t *testing.T) {
	future := now.Add(24 * time.Hour)
	past := now.Add(-24 * time.Hour)
	mgmt := "https://apps.apple.com/account/subscriptions"
	sub := func(ent *rcEntitlement, s *rcSubscription) Subscriber {
		out := Subscriber{Entitlements: map[string]rcEntitlement{}, Subscriptions: map[string]rcSubscription{}, ManagementURL: &mgmt}
		if ent != nil {
			out.Entitlements["premium"] = *ent
		}
		if s != nil {
			out.Subscriptions["monthly"] = *s
		}
		return out
	}
	cases := []struct {
		name    string
		s       Subscriber
		status  string
		premium bool
	}{
		{"never subscribed", sub(nil, nil), StatusFree, false},
		{"active", sub(&rcEntitlement{ExpiresDate: &future, ProductIdentifier: "monthly"}, &rcSubscription{ExpiresDate: &future, Store: "app_store"}), StatusActive, true},
		{"cancelled", sub(&rcEntitlement{ExpiresDate: &future, ProductIdentifier: "monthly"}, &rcSubscription{UnsubscribeDetectedAt: &past}), StatusCancelled, true},
		{"expired", sub(&rcEntitlement{ExpiresDate: &past, ProductIdentifier: "monthly"}, &rcSubscription{}), StatusExpired, false},
		{"billing issue", sub(&rcEntitlement{ExpiresDate: &past, ProductIdentifier: "monthly"}, &rcSubscription{BillingIssuesDetectedAt: &past}), StatusBillingIssue, false},
		{"grace period", sub(&rcEntitlement{ExpiresDate: &past, GracePeriodExpiresDate: &future, ProductIdentifier: "monthly"}, &rcSubscription{BillingIssuesDetectedAt: &past}), StatusBillingIssue, true},
		{"lifetime", sub(&rcEntitlement{ProductIdentifier: "lifetime"}, nil), StatusActive, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := StateFromSubscriber(c.s, "premium", now)
			if st.Status != c.status || st.IsPremium != c.premium {
				t.Fatalf("got %+v, want %s premium=%v", st, c.status, c.premium)
			}
			if st.ManagementURL != mgmt {
				t.Fatal("management url lost")
			}
		})
	}
	// Access is decided by the entitlement, not by product ids.
	other := Subscriber{Entitlements: map[string]rcEntitlement{"gold": {ExpiresDate: &future}}}
	if StateFromSubscriber(other, "premium", now).IsPremium {
		t.Fatal("a different entitlement must not grant premium")
	}
}

func TestShouldApply(t *testing.T) {
	future := now.Add(24 * time.Hour)
	later := now.Add(time.Minute)
	if ok, _ := ShouldApply(Current{EventAt: &later}, State{IsPremium: true}, tp(now), now); ok {
		t.Fatal("stale event applied")
	}
	if ok, _ := ShouldApply(Current{EventAt: &now}, State{IsPremium: false}, &later, now); !ok {
		t.Fatal("newer event rejected")
	}
	if ok, _ := ShouldApply(Current{EventAt: &later}, State{}, nil, now); !ok {
		t.Fatal("live sync must always apply")
	}
	crm := Current{IsSubscribed: true, ExpiresAt: &future, Source: SourceCRMGrant}
	if ok, _ := ShouldApply(crm, State{IsPremium: false, Status: StatusFree}, nil, now); ok {
		t.Fatal("RevenueCat 'no entitlement' must not revoke a CRM grant")
	}
	if ok, _ := ShouldApply(crm, State{IsPremium: true, ExpiresAt: &future}, nil, now); !ok {
		t.Fatal("a real purchase should take over a CRM grant")
	}
	lifetime := Current{IsSubscribed: true, Source: SourceLegacy}
	if ok, _ := ShouldApply(lifetime, State{IsPremium: true, ExpiresAt: &future}, nil, now); ok {
		t.Fatal("lifetime legacy access must not be replaced by an expiring one")
	}
	expiredLegacy := Current{IsSubscribed: true, ExpiresAt: tp(now.Add(-time.Hour)), Source: SourceLegacy}
	if ok, _ := ShouldApply(expiredLegacy, State{Status: StatusFree}, nil, now); !ok {
		t.Fatal("expired legacy access can be overwritten")
	}
}

func TestEffectiveStatus(t *testing.T) {
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)
	cases := []struct {
		stored  string
		sub     bool
		exp     *time.Time
		status  string
		premium bool
	}{
		{StatusFree, false, nil, StatusFree, false},
		{StatusActive, true, &future, StatusActive, true},
		{StatusActive, true, &past, StatusExpired, false}, // expired since the last event
		{StatusCancelled, true, &future, StatusCancelled, true},
		{StatusCancelled, true, &past, StatusExpired, false},
		{StatusBillingIssue, false, &past, StatusBillingIssue, false},
		{StatusActive, true, nil, StatusActive, true},   // lifetime
		{StatusFree, true, &future, StatusActive, true}, // granted before the status column existed
	}
	for _, c := range cases {
		s, p := EffectiveStatus(c.stored, c.sub, c.exp, now)
		if s != c.status || p != c.premium {
			t.Errorf("EffectiveStatus(%s,%v,%v) = %s,%v want %s,%v", c.stored, c.sub, c.exp, s, p, c.status, c.premium)
		}
	}
}

func TestValidWebhookAuth(t *testing.T) {
	if ValidWebhookAuth("anything", "") {
		t.Fatal("empty secret must reject (fail closed)")
	}
	if ValidWebhookAuth("", "s3cret") || ValidWebhookAuth("wrong", "s3cret") {
		t.Fatal("wrong header accepted")
	}
	for _, h := range []string{"s3cret", "Bearer s3cret"} {
		if !ValidWebhookAuth(h, "s3cret") {
			t.Fatalf("%q rejected", h)
		}
	}
	if !ValidWebhookAuth("s3cret", "Bearer s3cret") {
		t.Fatal("secret configured with Bearer prefix should match")
	}
}

func TestWebhookRejectsBadAuthBeforeTouchingDB(t *testing.T) {
	h := NewBillingHandler(nil, &config.Config{RevenueCatWebhookAuth: "s3cret"})
	for _, auth := range []string{"", "nope"} {
		req := httptest.NewRequest(http.MethodPost, "/api/webhooks/revenuecat", strings.NewReader(`{"event":{"id":"1","type":"TEST"}}`))
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		h.Webhook(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("auth %q: got %d", auth, rec.Code)
		}
	}
}

func TestRESTClientGetSubscriber(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk_test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/v1/subscribers/new-user" {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"subscriber":{"entitlements":{},"subscriptions":{}}}`))
			return
		}
		if r.URL.Path != "/v1/subscribers/user-1" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"subscriber": map[string]any{
			"entitlements":  map[string]any{"premium": map[string]any{"expires_date": "2030-01-01T00:00:00Z", "product_identifier": "annual"}},
			"subscriptions": map[string]any{"annual": map[string]any{"expires_date": "2030-01-01T00:00:00Z", "store": "play_store"}},
		}})
	}))
	defer srv.Close()
	c := &RESTClient{BaseURL: srv.URL, SecretKey: "sk_test"}
	s, err := c.GetSubscriber(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	st := StateFromSubscriber(s, "premium", now)
	if !st.IsPremium || st.Status != StatusActive || st.Store != "play_store" || st.ProductID != "annual" || !st.WillRenew {
		t.Fatalf("got %+v", st)
	}
	// First lookup of a customer RevenueCat hasn't seen returns 201.
	if s, err := c.GetSubscriber(context.Background(), "new-user"); err != nil || StateFromSubscriber(s, "premium", now).Status != StatusFree {
		t.Fatalf("201 for a new customer should read as FREE, got err=%v", err)
	}
	bad := &RESTClient{BaseURL: srv.URL, SecretKey: "wrong"}
	if _, err := bad.GetSubscriber(context.Background(), "user-1"); err == nil {
		t.Fatal("expected an error for a rejected key")
	}
}
