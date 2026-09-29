package billing

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CardFlow Premium is a single RevenueCat entitlement. Access is decided by
// this entitlement only — never by product ids, which change per store/plan.
const DefaultEntitlementID = "premium"

// Subscription states shown to the app and stored in users.subscription_status.
const (
	StatusFree         = "FREE"
	StatusActive       = "ACTIVE"
	StatusCancelled    = "CANCELLED" // will not renew, access continues until expiry
	StatusExpired      = "EXPIRED"
	StatusBillingIssue = "BILLING_ISSUE"
)

// Who granted the current access (users.subscription_source).
const (
	SourceRevenueCat = "revenuecat"
	SourceCRMGrant   = "crm_grant"
	SourceLegacy     = "legacy" // paid through the old checkout before the migration
)

// State is the premium state for one user, derived from RevenueCat.
type State struct {
	Status        string     `json:"status"`
	IsPremium     bool       `json:"is_premium"`
	ProductID     string     `json:"product_id,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"` // nil with IsPremium = lifetime
	WillRenew     bool       `json:"will_renew"`
	Store         string     `json:"store,omitempty"`
	Source        string     `json:"source,omitempty"`
	ManagementURL string     `json:"management_url,omitempty"`
}

// ---------------------------------------------------------------------------
// Webhook events
// ---------------------------------------------------------------------------

type webhookBody struct {
	APIVersion string `json:"api_version"`
	Event      Event  `json:"event"`
}

// Event is the subset of a RevenueCat webhook event CardFlow uses; the full
// payload is stored as-is in revenuecat_events.payload.
type Event struct {
	ID                       string   `json:"id"`
	Type                     string   `json:"type"`
	AppUserID                string   `json:"app_user_id"`
	OriginalAppUserID        string   `json:"original_app_user_id"`
	Aliases                  []string `json:"aliases"`
	ProductID                string   `json:"product_id"`
	NewProductID             string   `json:"new_product_id"`
	EntitlementID            *string  `json:"entitlement_id"`
	EntitlementIDs           []string `json:"entitlement_ids"`
	PeriodType               string   `json:"period_type"`
	Store                    string   `json:"store"`
	Environment              string   `json:"environment"`
	TransactionID            string   `json:"transaction_id"`
	Price                    *float64 `json:"price"`
	PriceInPurchasedCurrency *float64 `json:"price_in_purchased_currency"`
	Currency                 string   `json:"currency"`
	CancelReason             string   `json:"cancel_reason"`
	ExpirationReason         string   `json:"expiration_reason"`
	PurchasedAtMs            *int64   `json:"purchased_at_ms"`
	ExpirationAtMs           *int64   `json:"expiration_at_ms"`
	GracePeriodExpirationMs  *int64   `json:"grace_period_expiration_at_ms"`
	EventTimestampMs         *int64   `json:"event_timestamp_ms"`
	TransferredFrom          []string `json:"transferred_from"`
	TransferredTo            []string `json:"transferred_to"`
}

func msTime(ms *int64) *time.Time {
	if ms == nil || *ms <= 0 {
		return nil
	}
	t := time.UnixMilli(*ms).UTC()
	return &t
}

// EventAt is when RevenueCat produced the event (used to drop stale,
// out-of-order deliveries).
func (e Event) EventAt() *time.Time { return msTime(e.EventTimestampMs) }

// CandidateUserIDs lists every app user id the event can belong to, most
// specific first. CardFlow logs in to RevenueCat with its own user UUID.
func (e Event) CandidateUserIDs() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, id := range append([]string{e.AppUserID, e.OriginalAppUserID}, e.Aliases...) {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// TouchesEntitlement reports whether the event concerns the premium
// entitlement. Events that carry no entitlement list (older payloads, some
// event types) are treated as relevant; the REST sync then decides.
func (e Event) TouchesEntitlement(entitlement string) bool {
	ids := e.EntitlementIDs
	if len(ids) == 0 && e.EntitlementID != nil {
		ids = []string{*e.EntitlementID}
	}
	if len(ids) == 0 {
		return true
	}
	for _, id := range ids {
		if id == entitlement {
			return true
		}
	}
	return false
}

// Event types CardFlow reacts to. Everything else is logged and ignored.
var accessEvents = map[string]bool{
	"INITIAL_PURCHASE":            true,
	"RENEWAL":                     true,
	"CANCELLATION":                true,
	"UNCANCELLATION":              true,
	"NON_RENEWING_PURCHASE":       true,
	"EXPIRATION":                  true,
	"BILLING_ISSUE":               true,
	"PRODUCT_CHANGE":              true,
	"SUBSCRIPTION_PAUSED":         true,
	"SUBSCRIPTION_EXTENDED":       true,
	"TEMPORARY_ENTITLEMENT_GRANT": true,
	"REFUND_REVERSED":             true,
	"TRANSFER":                    true,
}

// IsAccessEvent reports whether the event type can change premium access.
func IsAccessEvent(eventType string) bool { return accessEvents[eventType] }

// StateFromEvent derives the new state from a single webhook event. It is the
// fallback used when REVENUECAT_SECRET_API_KEY isn't set; with the key the
// handler re-reads the subscriber from RevenueCat instead (safer against
// missed or out-of-order events). ok=false means the event carries no state.
func StateFromEvent(e Event, now time.Time) (State, bool) {
	expires := msTime(e.ExpirationAtMs)
	product := e.ProductID
	st := State{ProductID: product, ExpiresAt: expires, Store: e.Store, Source: SourceRevenueCat}
	activeUntil := func(t *time.Time) bool { return t == nil || t.After(now) }

	switch e.Type {
	case "INITIAL_PURCHASE", "RENEWAL", "UNCANCELLATION", "SUBSCRIPTION_EXTENDED",
		"TEMPORARY_ENTITLEMENT_GRANT", "REFUND_REVERSED":
		st.IsPremium = activeUntil(expires)
		st.WillRenew = expires != nil && st.IsPremium
	case "NON_RENEWING_PURCHASE":
		st.IsPremium = activeUntil(expires)
	case "PRODUCT_CHANGE":
		if e.NewProductID != "" {
			st.ProductID = e.NewProductID
		}
		st.IsPremium = activeUntil(expires)
		st.WillRenew = st.IsPremium && expires != nil
	case "CANCELLATION", "SUBSCRIPTION_PAUSED":
		// Access continues until the paid period ends (a refund arrives as a
		// CANCELLATION whose expiration is already in the past).
		st.IsPremium = expires != nil && expires.After(now)
		if st.IsPremium {
			st.Status = StatusCancelled
		} else {
			st.Status = StatusExpired
		}
		return st, true
	case "BILLING_ISSUE":
		// Keep access through the store's grace period, if any.
		until := expires
		if g := msTime(e.GracePeriodExpirationMs); g != nil && (until == nil || g.After(*until)) {
			until = g
		}
		st.ExpiresAt = until
		st.IsPremium = until != nil && until.After(now)
		st.Status = StatusBillingIssue
		return st, true
	case "EXPIRATION":
		st.IsPremium = false
		st.Status = StatusExpired
		return st, true
	default:
		return State{}, false
	}
	if st.IsPremium {
		st.Status = StatusActive
	} else {
		st.Status = StatusExpired
	}
	return st, true
}

// ---------------------------------------------------------------------------
// REST API (server-side only — uses the secret key)
// ---------------------------------------------------------------------------

type rcEntitlement struct {
	ExpiresDate            *time.Time `json:"expires_date"`
	GracePeriodExpiresDate *time.Time `json:"grace_period_expires_date"`
	ProductIdentifier      string     `json:"product_identifier"`
	PurchaseDate           *time.Time `json:"purchase_date"`
}

type rcSubscription struct {
	ExpiresDate             *time.Time `json:"expires_date"`
	UnsubscribeDetectedAt   *time.Time `json:"unsubscribe_detected_at"`
	BillingIssuesDetectedAt *time.Time `json:"billing_issues_detected_at"`
	RefundedAt              *time.Time `json:"refunded_at"`
	Store                   string     `json:"store"`
	IsSandbox               bool       `json:"is_sandbox"`
}

type rcNonSubscription struct {
	Store string `json:"store"`
}

// Subscriber is RevenueCat's GET /v1/subscribers/{id} "subscriber" object.
type Subscriber struct {
	Entitlements     map[string]rcEntitlement       `json:"entitlements"`
	Subscriptions    map[string]rcSubscription      `json:"subscriptions"`
	NonSubscriptions map[string][]rcNonSubscription `json:"non_subscriptions"`
	ManagementURL    *string                        `json:"management_url"`
}

// StateFromSubscriber derives the premium state from RevenueCat's current
// view of the customer — the source of truth.
func StateFromSubscriber(s Subscriber, entitlement string, now time.Time) State {
	st := State{Source: SourceRevenueCat}
	if s.ManagementURL != nil {
		st.ManagementURL = *s.ManagementURL
	}
	ent, ok := s.Entitlements[entitlement]
	if !ok {
		st.Status = StatusFree
		return st
	}
	st.ProductID = ent.ProductIdentifier
	until := ent.ExpiresDate
	if g := ent.GracePeriodExpiresDate; g != nil && until != nil && g.After(*until) {
		until = g
	}
	st.ExpiresAt = until
	st.IsPremium = until == nil || until.After(now)

	sub, isSub := s.Subscriptions[ent.ProductIdentifier]
	if isSub {
		st.Store = sub.Store
	} else if ns := s.NonSubscriptions[ent.ProductIdentifier]; len(ns) > 0 {
		st.Store = ns[len(ns)-1].Store
	}
	switch {
	case isSub && sub.BillingIssuesDetectedAt != nil && (sub.UnsubscribeDetectedAt == nil || sub.BillingIssuesDetectedAt.After(*sub.UnsubscribeDetectedAt)):
		st.Status = StatusBillingIssue
	case !st.IsPremium:
		st.Status = StatusExpired
	case isSub && sub.UnsubscribeDetectedAt != nil:
		st.Status = StatusCancelled
	default:
		st.Status = StatusActive
		st.WillRenew = isSub && until != nil
	}
	return st
}

// RESTClient calls RevenueCat's REST API with the secret key.
type RESTClient struct {
	BaseURL   string
	SecretKey string
	HTTP      *http.Client
}

// GetSubscriber fetches the current customer info for an app user id.
func (c *RESTClient) GetSubscriber(ctx context.Context, appUserID string) (Subscriber, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(c.BaseURL, "/")+"/v1/subscribers/"+url.PathEscape(appUserID), nil)
	if err != nil {
		return Subscriber{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.SecretKey)
	req.Header.Set("Accept", "application/json")
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return Subscriber{}, fmt.Errorf("revenuecat request failed: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	// 201 = RevenueCat created the customer on this first lookup.
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		return Subscriber{}, fmt.Errorf("revenuecat returned %d", res.StatusCode)
	}
	var out struct {
		Subscriber Subscriber `json:"subscriber"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return Subscriber{}, fmt.Errorf("revenuecat response unreadable: %w", err)
	}
	return out.Subscriber, nil
}

// ---------------------------------------------------------------------------
// Rules
// ---------------------------------------------------------------------------

// ValidWebhookAuth checks the Authorization header RevenueCat sends with every
// webhook against REVENUECAT_WEBHOOK_AUTH (constant-time). The dashboard value
// may be configured with or without a "Bearer " prefix. An empty secret
// rejects everything (fail closed).
func ValidWebhookAuth(header, secret string) bool {
	if secret == "" || header == "" {
		return false
	}
	eq := func(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
	return eq(header, secret) || eq(header, "Bearer "+secret) || eq("Bearer "+header, secret)
}

// Current is what's stored for a user before applying a new state.
type Current struct {
	IsSubscribed bool
	ExpiresAt    *time.Time
	Source       string
	EventAt      *time.Time
}

func (c Current) premium(now time.Time) bool {
	return c.IsSubscribed && (c.ExpiresAt == nil || c.ExpiresAt.After(now))
}

// ShouldApply decides whether a new RevenueCat state may overwrite what's
// stored. eventAt is nil for a live REST sync (always current).
//   - Stale webhook deliveries (older than the last applied event) are dropped.
//   - Access granted outside RevenueCat (CRM grant, pre-migration purchase)
//     is never revoked by RevenueCat saying "no entitlement", and a lifetime
//     grant is never replaced by an expiring subscription.
func ShouldApply(cur Current, next State, eventAt *time.Time, now time.Time) (bool, string) {
	if eventAt != nil && cur.EventAt != nil && eventAt.Before(*cur.EventAt) {
		return false, "stale event (a newer one was already applied)"
	}
	if cur.Source != "" && cur.Source != SourceRevenueCat && cur.premium(now) {
		if !next.IsPremium {
			return false, "access granted outside RevenueCat is kept"
		}
		if cur.ExpiresAt == nil && next.ExpiresAt != nil {
			return false, "lifetime access granted outside RevenueCat is kept"
		}
	}
	return true, ""
}

// EffectiveStatus turns the stored columns into the status the app shows,
// accounting for time passing since the last event (an ACTIVE/CANCELLED row
// whose expiry is past is EXPIRED).
func EffectiveStatus(stored string, isSubscribed bool, expiresAt *time.Time, now time.Time) (string, bool) {
	premium := isSubscribed && (expiresAt == nil || expiresAt.After(now))
	if premium {
		switch stored {
		case StatusCancelled, StatusBillingIssue:
			return stored, true
		}
		return StatusActive, true
	}
	switch stored {
	case StatusBillingIssue:
		// Stays until RevenueCat reports a recovery (RENEWAL) or EXPIRATION.
		return StatusBillingIssue, false
	case StatusActive, StatusCancelled, StatusExpired:
		return StatusExpired, false
	}
	return StatusFree, false
}
