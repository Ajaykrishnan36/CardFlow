package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"cardflow-backend/internal/config"
	"cardflow-backend/internal/database"
	"cardflow-backend/internal/domain"
	"cardflow-backend/internal/middleware"
	"cardflow-backend/pkg/response"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// BillingHandler serves CardFlow Premium through RevenueCat. Purchases happen
// in the RevenueCat SDKs (App Store / Play Store / Web Billing); this server
// only receives RevenueCat's webhooks, re-reads customers with the secret key,
// and stores the result in PostgreSQL. Premium APIs decide access from the
// stored state (domain.User.IsPremiumActive), never from a client flag.
type BillingHandler struct {
	db          *database.DB
	cfg         *config.Config
	rc          *RESTClient // nil when REVENUECAT_SECRET_API_KEY isn't set
	entitlement string
	now         func() time.Time

	syncMu   sync.Mutex
	lastSync map[uuid.UUID]time.Time
}

func NewBillingHandler(db *database.DB, cfg *config.Config) *BillingHandler {
	h := &BillingHandler{
		db:          db,
		cfg:         cfg,
		entitlement: cfg.RevenueCatEntitlementID,
		now:         time.Now,
		lastSync:    map[uuid.UUID]time.Time{},
	}
	if h.entitlement == "" {
		h.entitlement = DefaultEntitlementID
	}
	if cfg.RevenueCatSecretAPIKey != "" {
		h.rc = &RESTClient{BaseURL: cfg.RevenueCatAPIBaseURL, SecretKey: cfg.RevenueCatSecretAPIKey}
	}
	if cfg.RevenueCatWebhookAuth == "" {
		slog.Warn("REVENUECAT_WEBHOOK_AUTH is not set — RevenueCat webhooks will be rejected")
	}
	if h.rc == nil {
		slog.Warn("REVENUECAT_SECRET_API_KEY is not set — subscriptions update from webhook events only")
	}
	return h
}

func (h *BillingHandler) dbReady(w http.ResponseWriter) bool {
	if h.db == nil || h.db.Pool == nil {
		response.InternalServerError(w, "database not connected")
		return false
	}
	return true
}

func currentUser(w http.ResponseWriter, r *http.Request) (*domain.User, bool) {
	user, ok := r.Context().Value(middleware.UserContextKey).(*domain.User)
	if !ok || user == nil {
		response.Unauthorized(w, "authentication required")
		return nil, false
	}
	return user, true
}

// ---------------------------------------------------------------------------
// App endpoints
// ---------------------------------------------------------------------------

type statusResponse struct {
	AppUserID     string     `json:"app_user_id"` // the RevenueCat App User ID the SDKs must log in with
	Status        string     `json:"status"`
	IsPremium     bool       `json:"is_premium"`
	Entitlement   string     `json:"entitlement"`
	ProductID     *string    `json:"product_id"`
	ExpiresAt     *time.Time `json:"expires_at"`
	WillRenew     bool       `json:"will_renew"`
	Store         *string    `json:"store"`
	Source        *string    `json:"source"`
	ManagementURL *string    `json:"management_url"`
	UpdatedAt     *time.Time `json:"updated_at"`
	Synced        bool       `json:"synced"`
}

func (h *BillingHandler) readStatus(ctx context.Context, userID uuid.UUID) (statusResponse, error) {
	var out statusResponse
	var stored string
	err := h.db.Pool.QueryRow(ctx, `
		SELECT is_subscribed, subscription_plan_id, subscription_expires_at, subscription_status,
		       subscription_will_renew, subscription_store, subscription_source,
		       subscription_management_url, subscription_updated_at
		FROM users WHERE id = $1 AND deleted_at IS NULL`, userID).
		Scan(&out.IsPremium, &out.ProductID, &out.ExpiresAt, &stored, &out.WillRenew,
			&out.Store, &out.Source, &out.ManagementURL, &out.UpdatedAt)
	if err != nil {
		return out, err
	}
	out.Status, out.IsPremium = EffectiveStatus(stored, out.IsPremium, out.ExpiresAt, h.now())
	out.WillRenew = out.WillRenew && out.IsPremium
	out.Entitlement = h.entitlement
	out.AppUserID = userID.String()
	return out, nil
}

// GetStatus returns the caller's premium state as stored by the server.
// GET /api/v1/billing/status
func (h *BillingHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !h.dbReady(w) {
		return
	}
	st, err := h.readStatus(r.Context(), user.ID)
	if err != nil {
		response.InternalServerError(w, "failed to load subscription status")
		return
	}
	response.JSON(w, http.StatusOK, st)
}

// Sync asks RevenueCat (server-to-server, secret key) for the caller's current
// entitlements and stores them. The app calls this right after a purchase or
// restore so premium unlocks without waiting for the webhook. The client's
// own claim is never used — only RevenueCat's answer.
// POST /api/v1/billing/sync
func (h *BillingHandler) Sync(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !h.dbReady(w) {
		return
	}
	synced := false
	if h.rc != nil && h.allowSync(user.ID) {
		if err := h.syncUser(r.Context(), user.ID, "app_sync"); err != nil {
			slog.Warn("RevenueCat sync failed", "user_id", user.ID, "error", err)
			response.Error(w, http.StatusBadGateway, "SYNC_FAILED", "Could not reach the subscription service. Please try again.", nil)
			return
		}
		synced = true
	}
	st, err := h.readStatus(r.Context(), user.ID)
	if err != nil {
		response.InternalServerError(w, "failed to load subscription status")
		return
	}
	st.Synced = synced
	response.JSON(w, http.StatusOK, st)
}

// allowSync limits each user to one RevenueCat lookup every few seconds.
func (h *BillingHandler) allowSync(id uuid.UUID) bool {
	h.syncMu.Lock()
	defer h.syncMu.Unlock()
	now := h.now()
	if last, ok := h.lastSync[id]; ok && now.Sub(last) < 3*time.Second {
		return false
	}
	h.lastSync[id] = now
	if len(h.lastSync) > 10000 {
		h.lastSync = map[uuid.UUID]time.Time{id: now}
	}
	return true
}

// GetTransactions lists the caller's subscription history: RevenueCat
// purchases/renewals plus payments made before the migration (kept as history).
// GET /api/v1/billing/transactions
func (h *BillingHandler) GetTransactions(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok || !h.dbReady(w) {
		return
	}
	rows, err := h.db.Pool.Query(r.Context(), `
		SELECT event_id, event_type, COALESCE(product_id, ''), COALESCE(store, ''), COALESCE(environment, ''),
		       price, COALESCE(currency, ''), COALESCE(event_at, received_at), expires_at, 'revenuecat'
		FROM revenuecat_events
		WHERE user_id = $1 AND status IN ('processed', 'ignored') AND event_type IN
		      ('INITIAL_PURCHASE', 'RENEWAL', 'NON_RENEWING_PURCHASE', 'PRODUCT_CHANGE', 'CANCELLATION', 'EXPIRATION', 'BILLING_ISSUE', 'UNCANCELLATION')
		UNION ALL
		SELECT id::text, CASE status WHEN 'paid' THEN 'LEGACY_PAYMENT' ELSE 'LEGACY_' || upper(status) END,
		       plan_id, 'legacy', '', amount_paise / 100.0, 'INR', COALESCE(paid_at, created_at), NULL, 'legacy'
		FROM subscription_payments
		WHERE user_id = $1
		ORDER BY 8 DESC
		LIMIT 100`, user.ID)
	if err != nil {
		response.InternalServerError(w, "failed to load transactions")
		return
	}
	defer rows.Close()
	list := []map[string]interface{}{}
	for rows.Next() {
		var id, typ, product, store, env, currency, source string
		var price *float64
		var at time.Time
		var expires *time.Time
		if err := rows.Scan(&id, &typ, &product, &store, &env, &price, &currency, &at, &expires, &source); err != nil {
			response.InternalServerError(w, "failed to read transaction")
			return
		}
		list = append(list, map[string]interface{}{
			"id": id, "type": typ, "product_id": product, "store": store, "environment": env,
			"price": price, "currency": currency, "at": at, "expires_at": expires, "source": source,
		})
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"transactions": list})
}

// ---------------------------------------------------------------------------
// Webhook
// ---------------------------------------------------------------------------

// Webhook receives RevenueCat's server-to-server events.
// POST /api/webhooks/revenuecat
//
// Authenticated by the Authorization header configured in the RevenueCat
// dashboard (REVENUECAT_WEBHOOK_AUTH). Every event is stored once by id, so a
// retried delivery is recognised and not applied twice. Returns 5xx only when
// processing failed and RevenueCat should retry.
func (h *BillingHandler) Webhook(w http.ResponseWriter, r *http.Request) {
	if !ValidWebhookAuth(r.Header.Get("Authorization"), h.cfg.RevenueCatWebhookAuth) {
		slog.Warn("RevenueCat webhook rejected: bad or missing Authorization header", "remote", r.RemoteAddr)
		response.Unauthorized(w, "invalid webhook authorization")
		return
	}
	if !h.dbReady(w) {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		response.BadRequest(w, "could not read body", nil)
		return
	}
	var body webhookBody
	if err := json.Unmarshal(raw, &body); err != nil || body.Event.ID == "" || body.Event.Type == "" {
		response.BadRequest(w, "invalid RevenueCat webhook payload", nil)
		return
	}
	ev := body.Event
	ctx := r.Context()

	claimed, prior, err := h.claimEvent(ctx, ev, raw)
	if err != nil {
		slog.Error("RevenueCat webhook: could not record event", "event_id", ev.ID, "error", err)
		response.InternalServerError(w, "could not record event")
		return
	}
	if !claimed {
		slog.Info("RevenueCat webhook duplicate", "event_id", ev.ID, "type", ev.Type, "status", prior)
		response.JSON(w, http.StatusOK, map[string]interface{}{"status": prior, "duplicate": true})
		return
	}

	status, userID, note, perr := h.processEvent(ctx, ev)
	if perr != nil {
		h.finishEvent(ctx, ev.ID, "failed", userID, perr.Error())
		slog.Error("RevenueCat webhook failed", "event_id", ev.ID, "type", ev.Type, "error", perr)
		response.InternalServerError(w, "event processing failed")
		return
	}
	h.finishEvent(ctx, ev.ID, status, userID, note)
	slog.Info("RevenueCat webhook", "event_id", ev.ID, "type", ev.Type, "environment", ev.Environment,
		"status", status, "user_id", userID, "note", note)
	response.JSON(w, http.StatusOK, map[string]interface{}{"status": status})
}

// claimEvent stores the event (first delivery) and marks it as being
// processed. It returns claimed=false with the prior status when the event is
// already processed/ignored or is being processed by a concurrent delivery.
func (h *BillingHandler) claimEvent(ctx context.Context, ev Event, raw []byte) (bool, string, error) {
	var payload json.RawMessage
	var env struct {
		Event json.RawMessage `json:"event"`
	}
	if json.Unmarshal(raw, &env) == nil && len(env.Event) > 0 {
		payload = env.Event
	} else {
		payload = raw
	}
	price := ev.PriceInPurchasedCurrency
	currency := ev.Currency
	if price == nil && ev.Price != nil {
		price, currency = ev.Price, "USD"
	}
	product := ev.ProductID
	if ev.Type == "PRODUCT_CHANGE" && ev.NewProductID != "" {
		product = ev.NewProductID
	}
	_, err := h.db.Pool.Exec(ctx, `
		INSERT INTO revenuecat_events
		    (event_id, event_type, app_user_id, product_id, entitlement_ids, store, environment,
		     price, currency, transaction_id, event_at, expires_at, payload)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5, NULLIF($6, ''), NULLIF($7, ''),
		        $8, NULLIF($9, ''), NULLIF($10, ''), $11, $12, $13)
		ON CONFLICT (event_id) DO NOTHING`,
		ev.ID, ev.Type, ev.AppUserID, product, ev.EntitlementIDs, ev.Store, ev.Environment,
		price, currency, ev.TransactionID, ev.EventAt(), msTime(ev.ExpirationAtMs), []byte(payload))
	if err != nil {
		return false, "", err
	}
	// Claim it: new, previously failed, or stuck "processing" for > 2 minutes.
	var claimed string
	err = h.db.Pool.QueryRow(ctx, `
		UPDATE revenuecat_events
		SET status = 'processing', attempts = attempts + 1, updated_at = NOW()
		WHERE event_id = $1 AND (status IN ('received', 'failed')
		      OR (status = 'processing' AND updated_at < NOW() - INTERVAL '2 minutes'))
		RETURNING event_id`, ev.ID).Scan(&claimed)
	if errors.Is(err, pgx.ErrNoRows) {
		var prior string
		if err := h.db.Pool.QueryRow(ctx, `SELECT status FROM revenuecat_events WHERE event_id = $1`, ev.ID).Scan(&prior); err != nil {
			return false, "", err
		}
		return false, prior, nil
	}
	if err != nil {
		return false, "", err
	}
	return true, "", nil
}

func (h *BillingHandler) finishEvent(ctx context.Context, eventID, status string, userID *uuid.UUID, note string) {
	_, err := h.db.Pool.Exec(ctx, `
		UPDATE revenuecat_events
		SET status = $2::varchar, user_id = COALESCE($3::uuid, user_id), error = NULLIF($4::text, ''), updated_at = NOW(),
		    processed_at = CASE WHEN $2::varchar IN ('processed', 'ignored') THEN NOW() ELSE processed_at END
		WHERE event_id = $1`, eventID, status, userID, note)
	if err != nil {
		slog.Error("RevenueCat webhook: could not update event status", "event_id", eventID, "error", err)
	}
}

// processEvent applies one event. Returns "processed" or "ignored" (with a
// note), or an error when it should be retried.
func (h *BillingHandler) processEvent(ctx context.Context, ev Event) (string, *uuid.UUID, string, error) {
	if ev.Type == "TEST" {
		return "ignored", nil, "test event from the RevenueCat dashboard", nil
	}
	if !IsAccessEvent(ev.Type) {
		return "ignored", nil, "event type does not affect premium access", nil
	}
	if ev.Type == "TRANSFER" {
		return h.processTransfer(ctx, ev)
	}
	if !ev.TouchesEntitlement(h.entitlement) {
		return "ignored", nil, "event is for another entitlement", nil
	}
	userID, err := h.resolveUser(ctx, ev.CandidateUserIDs())
	if err != nil {
		return "", nil, "", err
	}
	if userID == nil {
		return "ignored", nil, "no CardFlow user matches the app user id", nil
	}
	if h.rc != nil {
		if err := h.syncUser(ctx, *userID, "webhook:"+ev.Type); err != nil {
			return "", userID, "", err
		}
		return "processed", userID, "", nil
	}
	st, ok := StateFromEvent(ev, h.now())
	if !ok {
		return "ignored", userID, "event carries no subscription state", nil
	}
	applied, reason, err := h.applyState(ctx, *userID, st, ev.EventAt())
	if err != nil {
		return "", userID, "", err
	}
	if !applied {
		return "ignored", userID, reason, nil
	}
	return "processed", userID, "", nil
}

// processTransfer moves access between app users (e.g. a restore on a device
// that belonged to another account). Both sides are re-read from RevenueCat;
// without the secret key the losing side is revoked and the receiving side
// picks up its access on its next /billing/sync.
func (h *BillingHandler) processTransfer(ctx context.Context, ev Event) (string, *uuid.UUID, string, error) {
	var first *uuid.UUID
	touched := 0
	for _, side := range []struct {
		ids  []string
		lose bool
	}{{ev.TransferredFrom, true}, {ev.TransferredTo, false}} {
		for _, appID := range side.ids {
			userID, err := h.resolveUser(ctx, []string{appID})
			if err != nil {
				return "", first, "", err
			}
			if userID == nil {
				continue
			}
			if first == nil {
				first = userID
			}
			if h.rc != nil {
				if err := h.syncUser(ctx, *userID, "webhook:TRANSFER"); err != nil {
					return "", first, "", err
				}
				touched++
			} else if side.lose {
				if ok, _, err := h.applyState(ctx, *userID, State{Status: StatusExpired, Source: SourceRevenueCat}, ev.EventAt()); err != nil {
					return "", first, "", err
				} else if ok {
					touched++
				}
			}
		}
	}
	if touched == 0 {
		return "ignored", first, "transfer touched no CardFlow users", nil
	}
	return "processed", first, "", nil
}

// resolveUser maps RevenueCat app user ids to a CardFlow user. Anonymous
// RevenueCat ids ($RCAnonymousID:...) are not UUIDs and never match.
func (h *BillingHandler) resolveUser(ctx context.Context, ids []string) (*uuid.UUID, error) {
	for _, raw := range ids {
		id, err := uuid.Parse(raw)
		if err != nil {
			continue
		}
		var found uuid.UUID
		err = h.db.Pool.QueryRow(ctx, `SELECT id FROM users WHERE id = $1`, id).Scan(&found)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return &found, nil
	}
	return nil, nil
}

// syncUser re-reads the customer from RevenueCat and stores the result.
func (h *BillingHandler) syncUser(ctx context.Context, userID uuid.UUID, reason string) error {
	sub, err := h.rc.GetSubscriber(ctx, userID.String())
	if err != nil {
		return err
	}
	st := StateFromSubscriber(sub, h.entitlement, h.now())
	applied, why, err := h.applyState(ctx, userID, st, nil)
	if err != nil {
		return err
	}
	slog.Info("RevenueCat sync", "user_id", userID, "reason", reason, "status", st.Status,
		"premium", st.IsPremium, "applied", applied, "note", why)
	return nil
}

// applyState writes a RevenueCat state to users, inside a row lock so
// concurrent webhooks for one user can't interleave. eventAt is the event
// timestamp for webhook-derived states (nil for a live REST sync).
func (h *BillingHandler) applyState(ctx context.Context, userID uuid.UUID, st State, eventAt *time.Time) (bool, string, error) {
	tx, err := h.db.Pool.Begin(ctx)
	if err != nil {
		return false, "", err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var cur Current
	var source *string
	err = tx.QueryRow(ctx, `
		SELECT is_subscribed, subscription_expires_at, subscription_source, subscription_event_at
		FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&cur.IsSubscribed, &cur.ExpiresAt, &source, &cur.EventAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, "user not found", nil
	}
	if err != nil {
		return false, "", err
	}
	if source != nil {
		cur.Source = *source
	}
	now := h.now()
	if ok, why := ShouldApply(cur, st, eventAt, now); !ok {
		return false, why, tx.Commit(ctx)
	}
	stamp := now
	if eventAt != nil {
		stamp = *eventAt
	}
	var product, store, mgmt *string
	if st.ProductID != "" {
		product = &st.ProductID
	}
	if st.Store != "" {
		store = &st.Store
	}
	if st.ManagementURL != "" {
		mgmt = &st.ManagementURL
	}
	// A customer RevenueCat has never seen stays FREE and keeps its old
	// plan/expiry columns as history.
	if st.Status == StatusFree && !cur.IsSubscribed && cur.Source == "" {
		_, err = tx.Exec(ctx, `
			UPDATE users SET subscription_updated_at = NOW(),
			       subscription_management_url = COALESCE($2, subscription_management_url)
			WHERE id = $1`, userID, mgmt)
		if err != nil {
			return false, "", err
		}
		return true, "", tx.Commit(ctx)
	}
	_, err = tx.Exec(ctx, `
		UPDATE users
		SET is_subscribed = $2,
		    subscription_plan_id = COALESCE($3, subscription_plan_id),
		    subscription_expires_at = $4,
		    subscription_status = $5,
		    subscription_will_renew = $6,
		    subscription_store = COALESCE($7, subscription_store),
		    subscription_source = $8,
		    subscription_management_url = COALESCE($9, subscription_management_url),
		    subscription_event_at = GREATEST(COALESCE(subscription_event_at, $10), $10),
		    subscription_updated_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1`,
		userID, st.IsPremium, product, st.ExpiresAt, st.Status, st.WillRenew, store,
		SourceRevenueCat, mgmt, stamp)
	if err != nil {
		return false, "", fmt.Errorf("update subscription: %w", err)
	}
	return true, "", tx.Commit(ctx)
}

// GetCredits returns the scan-credit summary (unrelated to subscriptions).
func (h *BillingHandler) GetCredits(w http.ResponseWriter, r *http.Request) {
	if _, ok := currentUser(w, r); !ok {
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"balance":              25,
		"free_scans_remaining": 30,
		"reset_date":           time.Now().AddDate(0, 1, 0),
		"history": []map[string]interface{}{
			{"id": uuid.New(), "delta": 10, "reason": "Signup Welcome Bonus", "balance_after": 10, "created_at": time.Now().AddDate(0, 0, -5)},
			{"id": uuid.New(), "delta": 15, "reason": "Mini Pack Top-up (15 Credits)", "balance_after": 25, "created_at": time.Now().AddDate(0, 0, -1)},
		},
	})
}
