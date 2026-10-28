// Package billing: every member of a business is paid for, with Razorpay (D-164).
//
// A member is paid up to a date (crm.memberships.paid_until). Until then — or once that
// date has passed — the member is locked out of the business: every /w/{code} call answers
// 402 payment_required except the context and billing calls, so the screen can show the
// payment page. Someone may pay for themselves and for members ranked below them; a Super
// Admin for everyone, several people in one payment. The platform's own workspaces and the
// platform owner are never charged. Prices per member are set by the owner for 1, 3, 6 and
// 12 months. Charging is on only while Razorpay's keys are set and the owner hasn't switched
// it off.
package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// connectorCode is the app connector's own workspace (Business Card Snap): never charged.
const connectorCode = "business-card-snap"

type period struct {
	Key    string `json:"key"`
	Months int    `json:"months"`
}

var periods = []period{{"1m", 1}, {"3m", 3}, {"6m", 6}, {"12m", 12}}

func monthsOf(key string) int {
	for _, p := range periods {
		if p.Key == key {
			return p.Months
		}
	}
	return 0
}

// Settings are the owner's: whether members are charged, and the price (rupees, per member)
// for each period.
type Settings struct {
	Enabled bool           `json:"enabled"`
	Prices  map[string]int `json:"prices"`
}

func defaultSettings() Settings {
	return Settings{Enabled: true, Prices: map[string]int{"1m": 499, "3m": 1399, "6m": 2599, "12m": 4799}}
}

type Service struct {
	pool *pgxpool.Pool
	cfg  Config

	mu       sync.Mutex
	cached   Settings
	cachedAt time.Time
}

func New(pool *pgxpool.Pool, cfg Config) *Service {
	return &Service{pool: pool, cfg: cfg}
}

func (s *Service) Config() Config { return s.cfg }

// Settings, read at most every 15 seconds.
func (s *Service) Settings(ctx context.Context) Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.cachedAt.IsZero() && time.Since(s.cachedAt) < 15*time.Second {
		return s.cached
	}
	st := defaultSettings()
	var raw []byte
	if err := s.pool.QueryRow(ctx, `SELECT value FROM crm.platform_settings WHERE key = 'billing'`).Scan(&raw); err == nil {
		var stored Settings
		if json.Unmarshal(raw, &stored) == nil {
			st.Enabled = stored.Enabled
			for k, v := range stored.Prices {
				if monthsOf(k) > 0 && v > 0 {
					st.Prices[k] = v
				}
			}
		}
	}
	s.cached, s.cachedAt = st, time.Now()
	return st
}

func (s *Service) forget() {
	s.mu.Lock()
	s.cachedAt = time.Time{}
	s.mu.Unlock()
}

// Enforced: members are charged right now.
func (s *Service) Enforced(ctx context.Context) bool {
	return s != nil && s.cfg.Ready() && s.Settings(ctx).Enabled
}

// Exempt: the platform's own workspaces are never charged.
func Exempt(isPlatform bool, code string) bool { return isPlatform || code == connectorCode }

// Locked: the member hasn't paid, or their time has run out.
func (s *Service) Locked(ctx context.Context, membershipID uuid.UUID) (bool, *time.Time, error) {
	var until *time.Time
	if err := s.pool.QueryRow(ctx, `SELECT paid_until FROM crm.memberships WHERE id = $1`, membershipID).Scan(&until); err != nil {
		return false, nil, err
	}
	return until == nil || !until.After(time.Now()), until, nil
}

// PaymentRequired is the answer every locked call gets.
func PaymentRequired() error {
	return shared.NewError(http.StatusPaymentRequired, "payment_required", "Your access to this business has not been paid for, or has run out. Pay to continue.")
}

// AllowedWhileLocked: what a locked member may still call — the context (so the screen can
// show the payment page) and billing itself.
func AllowedWhileLocked(path, code string) bool {
	base := "/w/" + code
	i := strings.Index(path, base)
	if i < 0 {
		return false
	}
	rest := path[i+len(base):]
	return rest == "/context" || rest == "/billing" || strings.HasPrefix(rest, "/billing/")
}

// Scope is the caller inside one business.
type Scope struct {
	WS           uuid.UUID
	Code         string
	Name         string
	IsPlatform   bool
	MembershipID uuid.UUID // uuid.Nil for the platform owner's visits
	Owner        bool      // the platform owner
}

// Status is what the screen needs to know about the caller.
type Status struct {
	Enforced  bool       `json:"enforced"`
	Locked    bool       `json:"locked"`
	PaidUntil *time.Time `json:"paidUntil,omitempty"`
	// CanPayOthers: the caller may pay for other members too (shows the Billing page).
	CanPayOthers bool `json:"canPayOthers"`
}

func (s *Service) StatusFor(ctx context.Context, sc Scope) Status {
	if !s.Enforced(ctx) || Exempt(sc.IsPlatform, sc.Code) {
		return Status{}
	}
	st := Status{Enforced: true}
	if sc.Owner || sc.MembershipID == uuid.Nil {
		st.CanPayOthers = true
		return st
	}
	locked, until, err := s.Locked(ctx, sc.MembershipID)
	if err != nil {
		return st
	}
	st.Locked, st.PaidUntil = locked, until
	var others bool
	_ = s.pool.QueryRow(ctx, `
		WITH me AS (SELECT `+rankOf("$1")+` AS rank)
		SELECT EXISTS (SELECT 1 FROM crm.memberships m, me WHERE m.workspace_id = $2 AND m.id <> $1 AND m.status IN ('active', 'invited')
		               AND (me.rank >= $3 OR `+rankOf("m.id")+` < me.rank))`, sc.MembershipID, sc.WS, access.RankSuperAdmin).Scan(&others)
	st.CanPayOthers = others
	return st
}

// rankOf is the SQL for a membership's highest role rank (0 with no role).
func rankOf(membership string) string {
	return `COALESCE((SELECT max(r.rank) FROM crm.role_assignments ra JOIN crm.roles r ON r.id = ra.role_id
	                  WHERE ra.membership_id = ` + membership + ` AND (ra.expires_at IS NULL OR ra.expires_at > now())), 0)`
}

// Member is one person on the Billing page.
type Member struct {
	MembershipID uuid.UUID  `json:"membershipId"`
	IdentityID   uuid.UUID  `json:"identityId"`
	Name         string     `json:"name"`
	Phone        string     `json:"phone"`
	RoleName     string     `json:"roleName"`
	Rank         int        `json:"-"`
	PaidUntil    *time.Time `json:"paidUntil,omitempty"`
	Locked       bool       `json:"locked"`
	Me           bool       `json:"me"`
	CanPay       bool       `json:"canPay"`
}

func (s *Service) members(ctx context.Context, sc Scope) ([]Member, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.id, m.identity_id, i.display_name, COALESCE(v.value_normalized, ''), m.paid_until,
		       `+rankOf("m.id")+`,
		       COALESCE((SELECT r.name FROM crm.role_assignments ra JOIN crm.roles r ON r.id = ra.role_id
		                 WHERE ra.membership_id = m.id AND (ra.expires_at IS NULL OR ra.expires_at > now()) ORDER BY r.rank DESC LIMIT 1), '')
		FROM crm.memberships m
		JOIN crm.identities i ON i.id = m.identity_id
		LEFT JOIN LATERAL (SELECT value_normalized FROM crm.verified_identifiers
		                   WHERE identity_id = m.identity_id AND kind = 'phone' ORDER BY verified_at LIMIT 1) v ON true
		WHERE m.workspace_id = $1 AND m.status IN ('active', 'invited')`, sc.WS)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	myRank := -1
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.MembershipID, &m.IdentityID, &m.Name, &m.Phone, &m.PaidUntil, &m.Rank, &m.RoleName); err != nil {
			return nil, err
		}
		m.Locked = m.PaidUntil == nil || !m.PaidUntil.After(time.Now())
		m.Me = m.MembershipID == sc.MembershipID
		if m.Me {
			myRank = m.Rank
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		m := &out[i]
		// Yourself; everyone below you; everyone, for a Super Admin (or the platform owner).
		m.CanPay = sc.Owner || m.Me || myRank >= access.RankSuperAdmin || (myRank >= 0 && m.Rank < myRank)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Me != out[j].Me {
			return out[i].Me
		}
		if out[i].Rank != out[j].Rank {
			return out[i].Rank > out[j].Rank
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// ---- routes inside a business: /w/{code}/billing ----

func (s *Service) MemberRoutes(r chi.Router, scopeFn func(*http.Request) Scope) {
	r.Get("/billing", func(w http.ResponseWriter, r *http.Request) { s.handleOverview(w, r, scopeFn(r)) })
	r.Post("/billing/orders", func(w http.ResponseWriter, r *http.Request) { s.handleCreateOrder(w, r, scopeFn(r)) })
	r.Post("/billing/verify", func(w http.ResponseWriter, r *http.Request) { s.handleVerify(w, r, scopeFn(r)) })
}

type priceRow struct {
	Key    string `json:"key"`
	Months int    `json:"months"`
	Price  int    `json:"price"`
}

func (s *Service) priceList(ctx context.Context) []priceRow {
	st := s.Settings(ctx)
	out := make([]priceRow, 0, len(periods))
	for _, p := range periods {
		out = append(out, priceRow{p.Key, p.Months, st.Prices[p.Key]})
	}
	return out
}

func (s *Service) handleOverview(w http.ResponseWriter, r *http.Request, sc Scope) {
	ctx := r.Context()
	members, err := s.members(ctx, sc)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{
		"status":   s.StatusFor(ctx, sc),
		"exempt":   Exempt(sc.IsPlatform, sc.Code),
		"keyId":    s.cfg.KeyID,
		"currency": "INR",
		"prices":   s.priceList(ctx),
		"members":  members,
		"business": sc.Name,
	})
}

func (s *Service) handleCreateOrder(w http.ResponseWriter, r *http.Request, sc Scope) {
	ctx := r.Context()
	var in struct {
		Period        string      `json:"period"`
		MembershipIDs []uuid.UUID `json:"membershipIds"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if !s.cfg.Ready() {
		shared.WriteError(w, r, shared.ServiceUnavailable("payments_not_set_up", "Payments aren't set up on this server yet."))
		return
	}
	if Exempt(sc.IsPlatform, sc.Code) {
		shared.WriteError(w, r, shared.BadRequest("This business is never charged."))
		return
	}
	months := monthsOf(in.Period)
	price := s.Settings(ctx).Prices[in.Period]
	if months == 0 || price <= 0 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"period": "Choose 1, 3, 6 or 12 months."}))
		return
	}
	members, err := s.members(ctx, sc)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	byID := map[uuid.UUID]Member{}
	for _, m := range members {
		byID[m.MembershipID] = m
	}
	seen := map[uuid.UUID]bool{}
	ids := []uuid.UUID{}
	names := []string{}
	for _, id := range in.MembershipIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		m, ok := byID[id]
		if !ok {
			shared.WriteError(w, r, shared.Validation(map[string]string{"membershipIds": "Someone you chose isn't in this business."}))
			return
		}
		if !m.CanPay {
			shared.WriteError(w, r, shared.Forbidden("cannot_pay_for_member", "You can pay for yourself and for people below your role. "+m.Name+" isn't."))
			return
		}
		ids = append(ids, id)
		names = append(names, m.Name)
	}
	if len(ids) == 0 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"membershipIds": "Choose at least one person."}))
		return
	}
	amount := int64(price) * int64(len(ids)) * 100
	receipt := "m" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
	order, err := s.cfg.createOrder(ctx, amount, receipt, map[string]string{"business": sc.Code, "period": in.Period, "members": fmt.Sprint(len(ids))})
	if err != nil {
		slog.Warn("crm billing: order not created", "error", err)
		shared.WriteError(w, r, shared.ServiceUnavailable("payment_unavailable", "Couldn't start the payment with Razorpay. Please try again."))
		return
	}
	sess := identity.SessionFrom(ctx)
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO crm.billing_orders (workspace_id, razorpay_order_id, period, months, unit_price, amount_paise, membership_ids, paid_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, sc.WS, order.ID, in.Period, months, price, amount, ids, sess.IdentityID); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var name, email, phone string
	_ = s.pool.QueryRow(ctx, `
		SELECT i.display_name,
		       COALESCE((SELECT value_normalized FROM crm.verified_identifiers WHERE identity_id = i.id AND kind = 'email' LIMIT 1), ''),
		       COALESCE((SELECT value_normalized FROM crm.verified_identifiers WHERE identity_id = i.id AND kind = 'phone' LIMIT 1), '')
		FROM crm.identities i WHERE i.id = $1`, sess.IdentityID).Scan(&name, &email, &phone)
	what := fmt.Sprintf("%d %s × %d month", len(ids), map[bool]string{true: "member", false: "members"}[len(ids) == 1], months)
	if months > 1 {
		what += "s"
	}
	shared.WriteJSON(w, http.StatusCreated, map[string]any{
		"orderId":     order.ID,
		"amount":      amount,
		"currency":    "INR",
		"keyId":       s.cfg.KeyID,
		"business":    sc.Name,
		"description": what,
		"members":     names,
		"prefill":     map[string]string{"name": name, "email": email, "contact": phone},
	})
}

func (s *Service) handleVerify(w http.ResponseWriter, r *http.Request, sc Scope) {
	ctx := r.Context()
	var in struct {
		OrderID   string `json:"razorpay_order_id"`
		PaymentID string `json:"razorpay_payment_id"`
		Signature string `json:"razorpay_signature"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if !s.cfg.paymentSignatureOK(in.OrderID, in.PaymentID, in.Signature) {
		shared.WriteError(w, r, shared.BadRequest("This payment could not be confirmed. If money was taken, it will be matched automatically or refunded."))
		return
	}
	sess := identity.SessionFrom(ctx)
	n, err := s.markPaid(ctx, in.OrderID, in.PaymentID, &sc.WS, &sess.IdentityID)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"paid": true, "members": n, "status": s.StatusFor(ctx, sc)})
}

var errOrderNotFound = shared.NotFound("order_not_found")

// markPaid counts one order as paid (once): each member's paid time grows by the period,
// from today or from the end of what they already have. Returns how many members.
func (s *Service) markPaid(ctx context.Context, orderID, paymentID string, ws *uuid.UUID, actor *uuid.UUID) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id, wsID uuid.UUID
	var status string
	var months int
	var ids []uuid.UUID
	var amount int64
	err = tx.QueryRow(ctx, `SELECT id, workspace_id, status, months, membership_ids, amount_paise FROM crm.billing_orders
		WHERE razorpay_order_id = $1 FOR UPDATE`, orderID).Scan(&id, &wsID, &status, &months, &ids, &amount)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, errOrderNotFound
	}
	if err != nil {
		return 0, err
	}
	if ws != nil && *ws != wsID {
		return 0, errOrderNotFound
	}
	if status == "paid" {
		return len(ids), nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE crm.memberships SET paid_until = GREATEST(COALESCE(paid_until, now()), now()) + make_interval(months => $3)
		WHERE workspace_id = $1 AND id = ANY($2)`, wsID, ids, months); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE crm.billing_orders SET status = 'paid', razorpay_payment_id = $2, paid_at = now(),
		paid_by = COALESCE(paid_by, $3) WHERE id = $1`, id, paymentID, actor); err != nil {
		return 0, err
	}
	kind := "identity"
	if actor == nil {
		kind = "system"
	}
	if err := shared.WriteAudit(ctx, tx, shared.AuditEvent{WorkspaceID: &wsID, ActorID: actor, ActorKind: kind, Action: "billing.paid", EntityType: "billing_order", EntityID: &id,
		After: map[string]any{"members": len(ids), "months": months, "amountPaise": amount, "paymentId": paymentID}}); err != nil {
		return 0, err
	}
	return len(ids), tx.Commit(ctx)
}

// ---- Razorpay's webhook: POST /public/billing/razorpay ----

func (s *Service) PublicRoutes(r chi.Router) {
	r.Post("/public/billing/razorpay", s.handleWebhook)
}

func (s *Service) handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := readAll(r, 1<<20)
	if err != nil {
		shared.WriteError(w, r, shared.BadRequest("unreadable body"))
		return
	}
	if !s.cfg.webhookSignatureOK(body, r.Header.Get("X-Razorpay-Signature")) {
		shared.WriteError(w, r, shared.Forbidden("bad_signature", "Signature check failed."))
		return
	}
	var ev struct {
		Event   string `json:"event"`
		Payload struct {
			Payment struct {
				Entity struct {
					ID      string `json:"id"`
					OrderID string `json:"order_id"`
					Status  string `json:"status"`
				} `json:"entity"`
			} `json:"payment"`
			Order struct {
				Entity struct {
					ID string `json:"id"`
				} `json:"entity"`
			} `json:"order"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(body, &ev); err != nil {
		shared.WriteError(w, r, shared.BadRequest("unreadable event"))
		return
	}
	orderID := ev.Payload.Payment.Entity.OrderID
	if orderID == "" {
		orderID = ev.Payload.Order.Entity.ID
	}
	switch ev.Event {
	case "payment.captured", "order.paid":
		if _, err := s.markPaid(r.Context(), orderID, ev.Payload.Payment.Entity.ID, nil, nil); err != nil && !errors.Is(err, errOrderNotFound) {
			shared.WriteError(w, r, err)
			return
		}
	case "payment.failed":
		_, _ = s.pool.Exec(r.Context(), `UPDATE crm.billing_orders SET status = 'failed' WHERE razorpay_order_id = $1 AND status = 'created'`, orderID)
	}
	shared.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---- the owner: /platform/billing ----

func (s *Service) OwnerRoutes(r chi.Router) {
	r.Get("/platform/billing", s.handleOwnerGet)
	r.Put("/platform/billing", s.handleOwnerSave)
}

type ownerOrder struct {
	ID          uuid.UUID  `json:"id"`
	Business    string     `json:"business"`
	Code        string     `json:"code"`
	PaidBy      string     `json:"paidBy"`
	Period      string     `json:"period"`
	Members     int        `json:"members"`
	AmountPaise int64      `json:"amountPaise"`
	Status      string     `json:"status"`
	OrderID     string     `json:"orderId"`
	PaymentID   string     `json:"paymentId"`
	CreatedAt   time.Time  `json:"createdAt"`
	PaidAt      *time.Time `json:"paidAt,omitempty"`
}

func (s *Service) handleOwnerGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := s.pool.Query(ctx, `
		SELECT o.id, w.name, w.code, COALESCE(i.display_name, ''), o.period, cardinality(o.membership_ids), o.amount_paise, o.status,
		       o.razorpay_order_id, COALESCE(o.razorpay_payment_id, ''), o.created_at, o.paid_at
		FROM crm.billing_orders o JOIN crm.workspaces w ON w.id = o.workspace_id
		LEFT JOIN crm.identities i ON i.id = o.paid_by
		ORDER BY o.created_at DESC LIMIT 200`)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	orders := []ownerOrder{}
	var collected int64
	for rows.Next() {
		var o ownerOrder
		if err := rows.Scan(&o.ID, &o.Business, &o.Code, &o.PaidBy, &o.Period, &o.Members, &o.AmountPaise, &o.Status, &o.OrderID, &o.PaymentID, &o.CreatedAt, &o.PaidAt); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		if o.Status == "paid" {
			collected += o.AmountPaise
		}
		orders = append(orders, o)
	}
	st := s.Settings(ctx)
	shared.WriteJSON(w, http.StatusOK, map[string]any{
		"enabled":        st.Enabled,
		"ready":          s.cfg.Ready(),
		"testMode":       strings.HasPrefix(s.cfg.KeyID, "rzp_test_"),
		"webhookReady":   s.cfg.WebhookSecret != "",
		"prices":         s.priceList(ctx),
		"orders":         orders,
		"collectedPaise": collected,
	})
}

func (s *Service) handleOwnerSave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in Settings
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	fields := map[string]string{}
	out := Settings{Enabled: in.Enabled, Prices: map[string]int{}}
	for _, p := range periods {
		v := in.Prices[p.Key]
		if v < 1 || v > 1000000 {
			fields["prices."+p.Key] = "Enter a price from ₹1 to ₹10,00,000."
			continue
		}
		out.Prices[p.Key] = v
	}
	if len(fields) > 0 {
		shared.WriteError(w, r, shared.Validation(fields))
		return
	}
	raw, _ := json.Marshal(out)
	sess := identity.SessionFrom(ctx)
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO crm.platform_settings (key, value, updated_by) VALUES ('billing', $1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = now()`, raw, sess.IdentityID); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	_ = shared.WriteAudit(ctx, s.pool, shared.AuditEvent{ActorID: &sess.IdentityID, Action: "billing.settings_changed", EntityType: "platform_setting", After: out})
	s.forget()
	s.handleOwnerGet(w, r)
}
