package cardflow

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/records"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The app's own data, read and edited in place (D-44): app users with their premium
// access, business listings and saved cards. The app's tables stay the source of truth —
// the CRM writes the same columns the app's admin console and billing flow use, so a
// change here shows in the app immediately. Catalog objects: app_user, app_business.

// premiumPlans mirrors the app's billing plans (internal/billing). Months 0 = lifetime.
var premiumPlans = map[string]struct {
	Name   string
	Months int
}{
	"3m": {"3 Months", 3}, "6m": {"6 Months", 6}, "12m": {"12 Months", 12}, "lifetime": {"Lifetime", 0},
}

const premiumExpr = `(u.is_subscribed AND (u.subscription_expires_at IS NULL OR u.subscription_expires_at > now()))`

// appScope checks the request is for the connected workspace and the object action.
func (c *Connector) appScope(w http.ResponseWriter, r *http.Request, object, action string) (*records.Scope, bool) {
	sc := records.ScopeFrom(r.Context())
	if sc == nil || sc.WS != c.WorkspaceID() {
		shared.WriteError(w, r, shared.NotFound("app_not_connected"))
		return nil, false
	}
	if object == "app_business" && !c.hasBiz {
		shared.WriteError(w, r, shared.NotFound("app_not_connected"))
		return nil, false
	}
	if !sc.Owner && !sc.Eff.Can(object, action) {
		shared.WriteError(w, r, shared.Forbidden("forbidden", "You don't have permission to do that."))
		return nil, false
	}
	return sc, true
}

func likePattern(q string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
}

// ---- app users ----

type AppUser struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	Phone         string       `json:"phone"`
	Email         string       `json:"email"`
	City          string       `json:"city"`
	State         string       `json:"state"`
	Role          string       `json:"role"`
	Status        string       `json:"status"`
	Premium       bool         `json:"premium"`
	PlanID        string       `json:"planId,omitempty"`
	PlanName      string       `json:"planName,omitempty"`
	ExpiresAt     *time.Time   `json:"expiresAt,omitempty"`
	FreeScansLeft int          `json:"freeScansLeft"`
	CreatedAt     time.Time    `json:"createdAt"`
	LastLoginAt   *time.Time   `json:"lastLoginAt,omitempty"`
	Businesses    int          `json:"businesses"`
	Cards         int          `json:"cards"`
	OpenTickets   int          `json:"openTickets"`
	Account       *LookupValue `json:"account,omitempty"`
	Contact       *LookupValue `json:"contact,omitempty"`
}

func (c *Connector) userSelect() string {
	tickets := `0`
	if c.hasTickets {
		tickets = `(SELECT count(*) FROM public.support_tickets t WHERE t.user_id = u.id AND t.status <> 'resolved')`
	}
	biz, cards := `0`, `0`
	if c.hasBiz {
		biz = `(SELECT count(*) FROM public.businesses b WHERE b.owner_user_id = u.id AND b.deleted_at IS NULL)`
	}
	if c.hasCards {
		cards = `(SELECT count(*) FROM public.saved_cards s WHERE s.user_id = u.id AND s.deleted_at IS NULL)`
	}
	return `
	SELECT u.id::text, COALESCE(u.name, ''), u.phone, COALESCE(u.email, ''), COALESCE(u.city, ''), COALESCE(u.state, ''),
	       u.role::text, u.status::text, ` + premiumExpr + `, COALESCE(u.subscription_plan_id, ''), u.subscription_expires_at,
	       u.free_scans_remaining, u.created_at, u.last_login_at, ` + biz + `, ` + cards + `, ` + tickets + `,
	       a.id::text, a.name || ' · ' || a.code, ct.id::text,
	       COALESCE(NULLIF(trim(concat_ws(' ', ct.first_name, ct.last_name)), ''), ct.code) || ' · ' || ct.code
	FROM public.users u
	LEFT JOIN crm.external_links l ON l.system = 'cardflow' AND l.external_type = 'user' AND l.external_id = u.id::text
	LEFT JOIN crm.accounts a ON a.id = l.account_id AND a.deleted_at IS NULL
	LEFT JOIN crm.contacts ct ON ct.id = l.contact_id AND ct.deleted_at IS NULL`
}

func scanAppUser(row pgx.Row) (AppUser, error) {
	var u AppUser
	var accID, accLabel, conID, conLabel *string
	var scans int16
	err := row.Scan(&u.ID, &u.Name, &u.Phone, &u.Email, &u.City, &u.State, &u.Role, &u.Status, &u.Premium, &u.PlanID, &u.ExpiresAt,
		&scans, &u.CreatedAt, &u.LastLoginAt, &u.Businesses, &u.Cards, &u.OpenTickets, &accID, &accLabel, &conID, &conLabel)
	if err != nil {
		return u, err
	}
	u.FreeScansLeft = int(scans)
	if p, ok := premiumPlans[u.PlanID]; ok {
		u.PlanName = p.Name
	}
	if accID != nil {
		u.Account = &LookupValue{ID: *accID, Label: *accLabel, Object: "accounts"}
	}
	if conID != nil {
		u.Contact = &LookupValue{ID: *conID, Label: *conLabel, Object: "contacts"}
	}
	return u, nil
}

func (c *Connector) handleListAppUsers(w http.ResponseWriter, r *http.Request) {
	c.SyncOnDemand()
	if _, ok := c.appScope(w, r, "app_user", "read"); !ok {
		return
	}
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	filter := map[string]string{
		"premium": premiumExpr, "free": "NOT " + premiumExpr, "admin": "u.role::text = 'admin'", "suspended": "u.status::text = 'suspended'",
	}[r.URL.Query().Get("filter")]
	if filter == "" {
		filter = "true"
	}
	rows, err := c.store.Pool.Query(ctx, c.userSelect()+`
		WHERE u.deleted_at IS NULL AND `+filter+`
		  AND ($1 = '' OR u.name ILIKE $2 OR u.phone ILIKE $2 OR u.email ILIKE $2 OR u.city ILIKE $2)
		ORDER BY u.created_at DESC LIMIT 500`, q, likePattern(q))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	list := []AppUser{}
	for rows.Next() {
		u, err := scanAppUser(rows)
		if err != nil {
			rows.Close()
			shared.WriteError(w, r, err)
			return
		}
		list = append(list, u)
	}
	rows.Close()
	var counts struct {
		All       int `json:"all"`
		Premium   int `json:"premium"`
		Free      int `json:"free"`
		Admin     int `json:"admin"`
		Suspended int `json:"suspended"`
	}
	if err := c.store.Pool.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE `+premiumExpr+`), count(*) FILTER (WHERE NOT `+premiumExpr+`),
		       count(*) FILTER (WHERE u.role::text = 'admin'), count(*) FILTER (WHERE u.status::text = 'suspended')
		FROM public.users u WHERE u.deleted_at IS NULL`).Scan(&counts.All, &counts.Premium, &counts.Free, &counts.Admin, &counts.Suspended); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": list, "counts": counts})
}

type SavedCard struct {
	ID          string    `json:"id"`
	PersonName  string    `json:"personName"`
	Designation string    `json:"designation"`
	Company     string    `json:"company"`
	Website     string    `json:"website"`
	Type        string    `json:"type"`
	GSTIN       string    `json:"gstin"`
	Notes       string    `json:"notes"`
	Phones      []string  `json:"phones"`
	Emails      []string  `json:"emails"`
	Address     string    `json:"address"`
	Source      string    `json:"source"`
	CreatedAt   time.Time `json:"createdAt"`
	Business    *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"business,omitempty"`
}

type AppPayment struct {
	PlanID    string     `json:"planId"`
	PlanName  string     `json:"planName"`
	AmountINR float64    `json:"amountInr"`
	Currency  string     `json:"currency,omitempty"` // empty = INR (legacy payments)
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"createdAt"`
	PaidAt    *time.Time `json:"paidAt,omitempty"`
}

type AppUserDetail struct {
	AppUser
	Businesses []AppBusiness `json:"businessList"`
	Cards      []SavedCard   `json:"cardList"`
	Tickets    []Ticket      `json:"ticketList"`
	Payments   []AppPayment  `json:"payments"`
}

func (c *Connector) loadAppUser(ctx context.Context, id string) (*AppUserDetail, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, shared.NotFound("app_user_not_found")
	}
	u, err := scanAppUser(c.store.Pool.QueryRow(ctx, c.userSelect()+` WHERE u.id = $1 AND u.deleted_at IS NULL`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NotFound("app_user_not_found")
	}
	if err != nil {
		return nil, err
	}
	d := &AppUserDetail{AppUser: u, Businesses: []AppBusiness{}, Cards: []SavedCard{}, Tickets: []Ticket{}, Payments: []AppPayment{}}
	if c.hasBiz {
		if d.Businesses, _, err = c.queryBusinesses(ctx, "b.owner_user_id = $1", id); err != nil {
			return nil, err
		}
	}
	if c.hasCards {
		if d.Cards, err = c.queryCards(ctx, id); err != nil {
			return nil, err
		}
	}
	if c.hasTickets {
		rows, err := c.store.Pool.Query(ctx, ticketSelect+` WHERE t.user_id = $1 ORDER BY t.created_at DESC LIMIT 100`, id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			t, err := scanTicket(rows)
			if err != nil {
				rows.Close()
				return nil, err
			}
			d.Tickets = append(d.Tickets, t)
		}
		rows.Close()
	}
	if tableExists(ctx, c.store.Pool, "subscription_payments") {
		rows, err := c.store.Pool.Query(ctx, `
			SELECT plan_id, amount_paise, status, created_at, paid_at FROM public.subscription_payments
			WHERE user_id = $1 ORDER BY created_at DESC LIMIT 50`, id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var p AppPayment
			var paise int
			if err := rows.Scan(&p.PlanID, &paise, &p.Status, &p.CreatedAt, &p.PaidAt); err != nil {
				rows.Close()
				return nil, err
			}
			p.AmountINR = float64(paise) / 100
			p.PlanName = p.PlanID
			if pl, ok := premiumPlans[p.PlanID]; ok {
				p.PlanName = pl.Name
			}
			d.Payments = append(d.Payments, p)
		}
		rows.Close()
	}
	// RevenueCat purchase history (CardFlow Premium after the RevenueCat migration).
	if tableExists(ctx, c.store.Pool, "revenuecat_events") {
		rows, err := c.store.Pool.Query(ctx, `
			SELECT COALESCE(product_id, ''), event_type, COALESCE(price, 0)::float8, COALESCE(currency, ''),
			       COALESCE(event_at, received_at)
			FROM public.revenuecat_events
			WHERE user_id = $1 AND status IN ('processed', 'ignored') AND event_type IN ('INITIAL_PURCHASE', 'RENEWAL', 'NON_RENEWING_PURCHASE', 'PRODUCT_CHANGE', 'CANCELLATION', 'EXPIRATION', 'BILLING_ISSUE')
			ORDER BY 5 DESC LIMIT 50`, id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var p AppPayment
			var typ string
			if err := rows.Scan(&p.PlanID, &typ, &p.AmountINR, &p.Currency, &p.CreatedAt); err != nil {
				rows.Close()
				return nil, err
			}
			p.PlanName = p.PlanID
			p.Status = strings.ToLower(typ)
			switch typ {
			case "INITIAL_PURCHASE", "RENEWAL", "NON_RENEWING_PURCHASE":
				p.Status = "paid"
				at := p.CreatedAt
				p.PaidAt = &at
			}
			d.Payments = append(d.Payments, p)
		}
		rows.Close()
		sort.SliceStable(d.Payments, func(i, j int) bool { return d.Payments[i].CreatedAt.After(d.Payments[j].CreatedAt) })
	}
	return d, nil
}

func (c *Connector) queryCards(ctx context.Context, userID string) ([]SavedCard, error) {
	rows, err := c.store.Pool.Query(ctx, `
		SELECT s.id::text, COALESCE(s.person_name, ''), COALESCE(s.designation, ''), COALESCE(s.company, ''), COALESCE(s.website, ''),
		       s.contact_type::text, COALESCE(s.gstin, ''), COALESCE(s.notes, ''), COALESCE(s.source, ''), s.created_at,
		       COALESCE((SELECT array_agg(COALESCE(p.phone_e164, p.raw_phone) ORDER BY p.created_at) FROM public.saved_card_phones p WHERE p.saved_card_id = s.id), '{}'),
		       COALESCE((SELECT array_agg(e.email ORDER BY e.created_at) FROM public.saved_card_emails e WHERE e.saved_card_id = s.id), '{}'),
		       COALESCE((SELECT a.raw_address FROM public.saved_card_addresses a WHERE a.saved_card_id = s.id LIMIT 1), ''),
		       b.id::text, b.name
		FROM public.saved_cards s
		LEFT JOIN public.businesses b ON b.id = s.linked_business_id AND b.deleted_at IS NULL
		WHERE s.user_id = $1 AND s.deleted_at IS NULL
		ORDER BY s.created_at DESC LIMIT 500`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SavedCard{}
	for rows.Next() {
		var s SavedCard
		var bizID, bizName *string
		if err := rows.Scan(&s.ID, &s.PersonName, &s.Designation, &s.Company, &s.Website, &s.Type, &s.GSTIN, &s.Notes, &s.Source, &s.CreatedAt,
			&s.Phones, &s.Emails, &s.Address, &bizID, &bizName); err != nil {
			return nil, err
		}
		s.GSTIN = strings.TrimSpace(s.GSTIN)
		if bizID != nil {
			s.Business = &struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}{*bizID, *bizName}
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (c *Connector) handleGetAppUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := c.appScope(w, r, "app_user", "read"); !ok {
		return
	}
	d, err := c.loadAppUser(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, d)
}

type appUserPatch struct {
	Name   *string `json:"name"`
	Email  *string `json:"email"`
	City   *string `json:"city"`
	Role   *string `json:"role"`   // user | admin (the app's admin console)
	Status *string `json:"status"` // active | suspended
	Access *struct {
		Action string `json:"action"` // grant | revoke
		PlanID string `json:"planId"` // 3m | 6m | 12m | lifetime
	} `json:"access"`
}

var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

func (c *Connector) handleUpdateAppUser(w http.ResponseWriter, r *http.Request) {
	sc, ok := c.appScope(w, r, "app_user", "update")
	if !ok {
		return
	}
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	var in appUserPatch
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	fields := map[string]string{}
	sets := []string{}
	args := []any{id}
	add := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, col+" = $"+itoa(len(args)))
	}
	changes := map[string]any{}
	if in.Name != nil {
		v := strings.TrimSpace(*in.Name)
		if v == "" || len(v) > 100 {
			fields["name"] = "Enter a name up to 100 characters."
		} else {
			add("name", v)
			changes["name"] = v
		}
	}
	if in.Email != nil {
		v := strings.ToLower(strings.TrimSpace(*in.Email))
		if v != "" && (len(v) > 255 || !emailRe.MatchString(v)) {
			fields["email"] = "Enter a valid email address."
		} else {
			add("email", nullIfEmpty(v))
			changes["email"] = v
		}
	}
	if in.City != nil {
		v := strings.TrimSpace(*in.City)
		if len(v) > 100 {
			fields["city"] = "Use at most 100 characters."
		} else {
			add("city", nullIfEmpty(v))
			changes["city"] = v
		}
	}
	if in.Role != nil {
		if *in.Role != "user" && *in.Role != "admin" {
			fields["role"] = "Pick user or admin."
		} else {
			args = append(args, *in.Role)
			sets = append(sets, "role = $"+itoa(len(args))+"::user_role")
			changes["role"] = *in.Role
		}
	}
	if in.Status != nil {
		if *in.Status != "active" && *in.Status != "suspended" {
			fields["status"] = "Pick active or suspended."
		} else {
			args = append(args, *in.Status)
			sets = append(sets, "status = $"+itoa(len(args))+"::user_status")
			changes["status"] = *in.Status
		}
	}
	activity := ""
	if in.Access != nil {
		switch in.Access.Action {
		case "grant":
			plan, ok := premiumPlans[in.Access.PlanID]
			if !ok {
				fields["planId"] = "Pick 3 months, 6 months, 12 months or lifetime."
				break
			}
			var expires *time.Time
			if plan.Months > 0 {
				t := time.Now().AddDate(0, plan.Months, 0)
				expires = &t
			}
			sets = append(sets, "is_subscribed = true", "subscription_status = 'ACTIVE'",
				"subscription_source = 'crm_grant'", "subscription_will_renew = false", "subscription_updated_at = now()")
			add("subscription_plan_id", in.Access.PlanID)
			add("subscription_expires_at", expires)
			changes["access"] = map[string]any{"granted": in.Access.PlanID, "expiresAt": expires}
			activity = "Premium access granted: " + plan.Name
		case "revoke":
			// Store-bought (RevenueCat) access can only be cancelled by the
			// customer in their store; revoking here removes CRM-granted access.
			sets = append(sets, "is_subscribed = false", "subscription_status = 'FREE'",
				"subscription_source = NULL", "subscription_will_renew = false", "subscription_updated_at = now()")
			changes["access"] = "revoked"
			activity = "Premium access revoked"
		default:
			fields["access"] = "Choose grant or revoke."
		}
	}
	if len(fields) > 0 {
		shared.WriteError(w, r, shared.Validation(fields))
		return
	}
	if len(sets) == 0 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"name": "Nothing to change."}))
		return
	}
	sess := identity.SessionFrom(ctx)
	actor := sess.IdentityID
	err := c.store.WithTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE public.users SET `+strings.Join(sets, ", ")+`, updated_at = now() WHERE id = $1 AND deleted_at IS NULL`, args...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return shared.NotFound("app_user_not_found")
		}
		if activity == "" {
			if role, ok := changes["role"]; ok {
				activity = "App role changed to " + role.(string)
			} else if st, ok := changes["status"]; ok {
				activity = "App account " + st.(string)
			}
		}
		if activity != "" {
			var l link
			if err := tx.QueryRow(ctx, `SELECT lead_id, account_id, contact_id FROM crm.external_links
				WHERE system = $1 AND external_type = 'user' AND external_id = $2`, System, id).Scan(&l.leadID, &l.accountID, &l.contactID); err == nil {
				if err := addActivity(ctx, tx, sc.WS, l, "app.access", activity+" (by "+sess.DisplayName+")", time.Now(),
					"cardflow:access:"+id+":"+time.Now().Format(time.RFC3339Nano), changes); err != nil {
					return err
				}
			}
		}
		meta := identity.Meta(r)
		ws := sc.WS
		return shared.WriteAudit(ctx, tx, shared.AuditEvent{WorkspaceID: &ws, ActorID: &actor, Action: "app_user.updated", EntityType: "app_user",
			After: map[string]any{"appUserId": id, "changes": changes}, IP: meta.IP, RequestID: meta.RequestID})
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	d, err := c.loadAppUser(ctx, id)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, d)
}

// Deleting works like the app's admin console: soft delete + suspend; admins are kept.
func (c *Connector) handleDeleteAppUser(w http.ResponseWriter, r *http.Request) {
	sc, ok := c.appScope(w, r, "app_user", "delete")
	if !ok {
		return
	}
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if _, err := uuid.Parse(id); err != nil {
		shared.WriteError(w, r, shared.NotFound("app_user_not_found"))
		return
	}
	actor := identity.SessionFrom(ctx).IdentityID
	err := c.store.WithTx(ctx, func(tx pgx.Tx) error {
		var role string
		err := tx.QueryRow(ctx, `SELECT role::text FROM public.users WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&role)
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.NotFound("app_user_not_found")
		}
		if err != nil {
			return err
		}
		if role == "admin" {
			return shared.NewError(http.StatusConflict, "app_admin", "App admins can't be deleted. Change their role to user first.")
		}
		if _, err := tx.Exec(ctx, `UPDATE public.users SET deleted_at = now(), status = 'suspended', updated_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		meta := identity.Meta(r)
		ws := sc.WS
		return shared.WriteAudit(ctx, tx, shared.AuditEvent{WorkspaceID: &ws, ActorID: &actor, Action: "app_user.deleted", EntityType: "app_user",
			After: map[string]any{"appUserId": id}, IP: meta.IP, RequestID: meta.RequestID})
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- business listings ----

type AppBusiness struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Slug          string    `json:"slug"`
	Description   string    `json:"description"`
	CategoryID    string    `json:"categoryId"`
	Category      string    `json:"category"`
	Website       string    `json:"website"`
	Email         string    `json:"email"`
	AddressLine1  string    `json:"addressLine1"`
	AddressLine2  string    `json:"addressLine2"`
	Locality      string    `json:"locality"`
	City          string    `json:"city"`
	District      string    `json:"district"`
	State         string    `json:"state"`
	Pincode       string    `json:"pincode"`
	GSTIN         string    `json:"gstin"`
	Status        string    `json:"status"`
	Verification  string    `json:"verification"`
	Listing       string    `json:"listing"`
	PhoneVerified bool      `json:"phoneVerified"`
	Completeness  int       `json:"completeness"`
	Services      []string  `json:"services"`
	Phones        []string  `json:"phones"`
	SavedBy       int       `json:"savedBy"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
	// Owner is empty (ID "") while a card-created business is unclaimed.
	Owner struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Phone string `json:"phone"`
	} `json:"owner"`
	// Source is "owner" (registered in the app) or "card" (created from a scanned card).
	Source string `json:"source"`
	// LeadStatus: "lead" (card business, owner not on the app yet),
	// "converted" (card business claimed by its owner), "owner" (registered by the owner).
	LeadStatus         string     `json:"leadStatus"`
	LeadID             string     `json:"leadId"`
	ContactName        string     `json:"contactName"`
	ContactDesignation string     `json:"contactDesignation"`
	ContactPhone       string     `json:"contactPhone"`
	ClaimedAt          *time.Time `json:"claimedAt,omitempty"`
	HasFrontImage      bool       `json:"hasFrontImage"`
	HasBackImage       bool       `json:"hasBackImage"`
}

const businessSelect = `
	SELECT b.id::text, b.name, b.slug, COALESCE(b.description, ''), b.primary_category_id::text, COALESCE(cat.name, ''),
	       COALESCE(b.website, ''), COALESCE(b.email, ''), b.address_line1, COALESCE(b.address_line2, ''), COALESCE(b.locality, ''),
	       b.city, COALESCE(b.district, ''), b.state, b.pincode, COALESCE(b.gstin, ''), b.status::text, b.verification::text,
	       b.listing::text, b.phone_verified, b.completeness,
	       COALESCE((SELECT array_agg(s.name ORDER BY s.created_at) FROM public.business_services s WHERE s.business_id = b.id), '{}'),
	       COALESCE((SELECT array_agg(p.phone ORDER BY p.created_at) FROM public.business_phones p WHERE p.business_id = b.id), '{}'),
	       %s, b.created_at, b.updated_at, COALESCE(u.id::text, ''), COALESCE(NULLIF(u.name, 'CardFlow User'), ''), COALESCE(u.phone, ''),
	       COALESCE(b.source, 'owner'), COALESCE(b.contact_name, ''), COALESCE(b.contact_designation, ''), COALESCE(b.contact_phone, ''),
	       b.claimed_at,
	       EXISTS (SELECT 1 FROM public.business_card_images i WHERE i.business_id = b.id AND i.side = 'front' AND length(i.image_data) > 0),
	       EXISTS (SELECT 1 FROM public.business_card_images i WHERE i.business_id = b.id AND i.side = 'back' AND length(i.image_data) > 0),
	       COALESCE((SELECT el.lead_id::text FROM crm.external_links el
	                 WHERE el.system = 'cardflow' AND el.external_type = 'business' AND el.external_id = b.id::text), '')
	FROM public.businesses b
	LEFT JOIN public.users u ON u.id = b.owner_user_id
	LEFT JOIN public.categories cat ON cat.id = b.primary_category_id`

func (c *Connector) businessSelect() string {
	saved := `0`
	if c.hasCards {
		saved = `(SELECT count(DISTINCT sc.user_id) FROM public.saved_cards sc WHERE sc.linked_business_id = b.id AND sc.deleted_at IS NULL)`
	}
	return strings.Replace(businessSelect, "%s", saved, 1)
}

func scanBusiness(row pgx.Row) (AppBusiness, error) {
	var b AppBusiness
	var completeness int16
	err := row.Scan(&b.ID, &b.Name, &b.Slug, &b.Description, &b.CategoryID, &b.Category, &b.Website, &b.Email, &b.AddressLine1,
		&b.AddressLine2, &b.Locality, &b.City, &b.District, &b.State, &b.Pincode, &b.GSTIN, &b.Status, &b.Verification, &b.Listing,
		&b.PhoneVerified, &completeness, &b.Services, &b.Phones, &b.SavedBy, &b.CreatedAt, &b.UpdatedAt, &b.Owner.ID, &b.Owner.Name, &b.Owner.Phone,
		&b.Source, &b.ContactName, &b.ContactDesignation, &b.ContactPhone, &b.ClaimedAt, &b.HasFrontImage, &b.HasBackImage, &b.LeadID)
	b.GSTIN = strings.TrimSpace(b.GSTIN)
	switch {
	case b.Source == "card" && b.Owner.ID == "":
		b.LeadStatus = "lead"
	case b.Source == "card":
		b.LeadStatus = "converted"
	default:
		b.LeadStatus = "owner"
	}
	b.Completeness = int(completeness)
	return b, err
}

// queryBusinesses lists live (not deleted) listings matching where (with args from $1).
func (c *Connector) queryBusinesses(ctx context.Context, where string, args ...any) ([]AppBusiness, int, error) {
	rows, err := c.store.Pool.Query(ctx, c.businessSelect()+` WHERE b.deleted_at IS NULL AND `+where+` ORDER BY b.updated_at DESC LIMIT 500`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []AppBusiness{}
	for rows.Next() {
		b, err := scanBusiness(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, b)
	}
	return out, len(out), rows.Err()
}

func (c *Connector) handleListBusinesses(w http.ResponseWriter, r *http.Request) {
	c.SyncOnDemand()
	if _, ok := c.appScope(w, r, "app_business", "read"); !ok {
		return
	}
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	filter := map[string]string{
		"listed": "b.listing::text = 'listed'", "hidden": "b.listing::text = 'unlisted'",
		"verified": "b.verification::text NOT IN ('pending', 'failed')", "unverified": "b.verification::text IN ('pending', 'failed')",
		"live": "b.status::text = 'live'", "review": "b.status::text IN ('draft', 'pending_verification', 'under_review')",
		"suspended": "b.status::text IN ('suspended', 'removed')",
		"leads":     "b.owner_user_id IS NULL",
		"converted": "COALESCE(b.source, 'owner') = 'card' AND b.owner_user_id IS NOT NULL",
		"owners":    "COALESCE(b.source, 'owner') = 'owner'",
	}[r.URL.Query().Get("filter")]
	if filter == "" {
		filter = "true"
	}
	list, _, err := c.queryBusinesses(ctx, filter+` AND ($1 = '' OR b.name ILIKE $2 OR b.city ILIKE $2 OR b.pincode ILIKE $2
		OR COALESCE(cat.name, '') ILIKE $2 OR COALESCE(u.name, '') ILIKE $2 OR COALESCE(u.phone, '') ILIKE $2 OR COALESCE(b.gstin, '') ILIKE $2
		OR COALESCE(b.contact_name, '') ILIKE $2 OR COALESCE(b.contact_phone, '') ILIKE $2)`, q, likePattern(q))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var counts struct {
		All       int `json:"all"`
		Listed    int `json:"listed"`
		Hidden    int `json:"hidden"`
		Verified  int `json:"verified"`
		Review    int `json:"review"`
		Leads     int `json:"leads"`
		Converted int `json:"converted"`
	}
	if err := c.store.Pool.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE listing::text = 'listed'), count(*) FILTER (WHERE listing::text = 'unlisted'),
		       count(*) FILTER (WHERE verification::text NOT IN ('pending', 'failed')),
		       count(*) FILTER (WHERE status::text IN ('draft', 'pending_verification', 'under_review')),
		       count(*) FILTER (WHERE owner_user_id IS NULL),
		       count(*) FILTER (WHERE COALESCE(source, 'owner') = 'card' AND owner_user_id IS NOT NULL)
		FROM public.businesses WHERE deleted_at IS NULL`).Scan(&counts.All, &counts.Listed, &counts.Hidden, &counts.Verified, &counts.Review,
		&counts.Leads, &counts.Converted); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": list, "counts": counts})
}

func (c *Connector) loadBusiness(ctx context.Context, id string) (*AppBusiness, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, shared.NotFound("business_not_found")
	}
	b, err := scanBusiness(c.store.Pool.QueryRow(ctx, c.businessSelect()+` WHERE b.id = $1 AND b.deleted_at IS NULL`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NotFound("business_not_found")
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (c *Connector) handleGetBusiness(w http.ResponseWriter, r *http.Request) {
	if _, ok := c.appScope(w, r, "app_business", "read"); !ok {
		return
	}
	b, err := c.loadBusiness(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, b)
}

type businessPatch struct {
	Name         *string   `json:"name"`
	Description  *string   `json:"description"`
	CategoryID   *string   `json:"categoryId"`
	Website      *string   `json:"website"`
	Email        *string   `json:"email"`
	AddressLine1 *string   `json:"addressLine1"`
	AddressLine2 *string   `json:"addressLine2"`
	Locality     *string   `json:"locality"`
	City         *string   `json:"city"`
	District     *string   `json:"district"`
	State        *string   `json:"state"`
	Pincode      *string   `json:"pincode"`
	GSTIN        *string   `json:"gstin"`
	Status       *string   `json:"status"`
	Verification *string   `json:"verification"`
	Listing      *string   `json:"listing"`
	Services     *[]string `json:"services"`
	OwnerPhone   *string   `json:"ownerPhone"`
}

var (
	pincodeRe = regexp.MustCompile(`^[0-9]{6}$`)
	gstinRe   = regexp.MustCompile(`^[0-9]{2}[A-Z0-9]{13}$`)
	enumSets  = map[string][]string{
		"status":       {"draft", "pending_verification", "live", "under_review", "suspended", "removed"},
		"verification": {"pending", "gst", "pan", "tan", "manual", "failed"},
		"listing":      {"unlisted", "listed"},
	}
	enumTypes = map[string]string{"status": "business_status", "verification": "verification_type", "listing": "listing_visibility"}
)

func inSet(v string, set []string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

func (c *Connector) handleUpdateBusiness(w http.ResponseWriter, r *http.Request) {
	sc, ok := c.appScope(w, r, "app_business", "update")
	if !ok {
		return
	}
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if _, err := uuid.Parse(id); err != nil {
		shared.WriteError(w, r, shared.NotFound("business_not_found"))
		return
	}
	var in businessPatch
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	fields := map[string]string{}
	sets := []string{}
	args := []any{id}
	changes := map[string]any{}
	set := func(col, key string, v any, cast string) {
		args = append(args, v)
		sets = append(sets, col+" = $"+itoa(len(args))+cast)
		changes[key] = v
	}
	text := func(p *string, col, key string, required bool, max int) {
		if p == nil {
			return
		}
		v := strings.TrimSpace(*p)
		switch {
		case required && v == "":
			fields[key] = "This field is required."
		case len(v) > max:
			fields[key] = "Use at most " + itoa(max) + " characters."
		case required:
			set(col, key, v, "")
		default:
			set(col, key, nullIfEmpty(v), "")
		}
	}
	text(in.Name, "name", "name", true, 200)
	text(in.Description, "description", "description", false, 4000)
	text(in.Website, "website", "website", false, 255)
	text(in.AddressLine1, "address_line1", "addressLine1", true, 255)
	text(in.AddressLine2, "address_line2", "addressLine2", false, 255)
	text(in.Locality, "locality", "locality", false, 150)
	text(in.City, "city", "city", true, 100)
	text(in.District, "district", "district", false, 100)
	text(in.State, "state", "state", true, 100)
	if in.Email != nil {
		v := strings.ToLower(strings.TrimSpace(*in.Email))
		if v != "" && (len(v) > 255 || !emailRe.MatchString(v)) {
			fields["email"] = "Enter a valid email address."
		} else {
			set("email", "email", nullIfEmpty(v), "")
		}
	}
	if in.Pincode != nil {
		v := strings.TrimSpace(*in.Pincode)
		if !pincodeRe.MatchString(v) {
			fields["pincode"] = "Enter a 6-digit pincode."
		} else {
			set("pincode", "pincode", v, "")
		}
	}
	if in.GSTIN != nil {
		v := strings.ToUpper(strings.TrimSpace(*in.GSTIN))
		if v != "" && !gstinRe.MatchString(v) {
			fields["gstin"] = "A GSTIN is 15 characters, starting with the 2-digit state code."
		} else {
			set("gstin", "gstin", nullIfEmpty(v), "")
		}
	}
	for key, p := range map[string]*string{"status": in.Status, "verification": in.Verification, "listing": in.Listing} {
		if p == nil {
			continue
		}
		if !inSet(*p, enumSets[key]) {
			fields[key] = "Pick one of: " + strings.Join(enumSets[key], ", ") + "."
			continue
		}
		set(key, key, *p, "::"+enumTypes[key])
	}
	if in.CategoryID != nil {
		var ok bool
		_ = c.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM public.categories WHERE id::text = $1)`, *in.CategoryID).Scan(&ok)
		if !ok {
			fields["categoryId"] = "Pick a category."
		} else {
			set("primary_category_id", "categoryId", *in.CategoryID, "::uuid")
		}
	}
	if in.OwnerPhone != nil {
		phone, okPhone := normalizePhone(*in.OwnerPhone)
		var ownerID string
		if !okPhone || c.store.Pool.QueryRow(ctx, `SELECT id::text FROM public.users WHERE phone = $1 AND deleted_at IS NULL`, phone).Scan(&ownerID) != nil {
			fields["ownerPhone"] = "No app user has this phone number. The owner must sign up in the app first."
		} else {
			set("owner_user_id", "ownerId", ownerID, "::uuid")
		}
	}
	var services []string
	if in.Services != nil {
		seen := map[string]bool{}
		for _, s := range *in.Services {
			s = strings.TrimSpace(s)
			if s == "" || seen[strings.ToLower(s)] {
				continue
			}
			if len(s) > 150 {
				fields["services"] = "Keep each service under 150 characters."
				break
			}
			seen[strings.ToLower(s)] = true
			services = append(services, s)
		}
		if len(services) > 30 {
			fields["services"] = "Add at most 30 services."
		}
		changes["services"] = services
	}
	if len(fields) > 0 {
		shared.WriteError(w, r, shared.Validation(fields))
		return
	}
	if len(sets) == 0 && in.Services == nil {
		shared.WriteError(w, r, shared.Validation(map[string]string{"name": "Nothing to change."}))
		return
	}
	actor := identity.SessionFrom(ctx).IdentityID
	err := c.store.WithTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE public.businesses SET `+strings.Join(append(sets, "updated_at = now()"), ", ")+` WHERE id = $1 AND deleted_at IS NULL`, args...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return shared.NotFound("business_not_found")
		}
		if in.Services != nil {
			if _, err := tx.Exec(ctx, `DELETE FROM public.business_services WHERE business_id = $1`, id); err != nil {
				return err
			}
			for _, s := range services {
				if _, err := tx.Exec(ctx, `INSERT INTO public.business_services (business_id, name) VALUES ($1, $2)`, id, s); err != nil {
					return err
				}
			}
		}
		meta := identity.Meta(r)
		ws := sc.WS
		return shared.WriteAudit(ctx, tx, shared.AuditEvent{WorkspaceID: &ws, ActorID: &actor, Action: "app_business.updated", EntityType: "app_business",
			After: map[string]any{"businessId": id, "changes": changes}, IP: meta.IP, RequestID: meta.RequestID})
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	b, err := c.loadBusiness(ctx, id)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, b)
}

// Deleting a listing removes it from the app (soft delete, hidden from search); saved
// cards that point at it keep their own copy of the details.
func (c *Connector) handleDeleteBusiness(w http.ResponseWriter, r *http.Request) {
	sc, ok := c.appScope(w, r, "app_business", "delete")
	if !ok {
		return
	}
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	if _, err := uuid.Parse(id); err != nil {
		shared.WriteError(w, r, shared.NotFound("business_not_found"))
		return
	}
	actor := identity.SessionFrom(ctx).IdentityID
	err := c.store.WithTx(ctx, func(tx pgx.Tx) error {
		var name string
		err := tx.QueryRow(ctx, `UPDATE public.businesses SET deleted_at = now(), status = 'removed', listing = 'unlisted', updated_at = now()
			WHERE id = $1 AND deleted_at IS NULL RETURNING name`, id).Scan(&name)
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.NotFound("business_not_found")
		}
		if err != nil {
			return err
		}
		meta := identity.Meta(r)
		ws := sc.WS
		return shared.WriteAudit(ctx, tx, shared.AuditEvent{WorkspaceID: &ws, ActorID: &actor, Action: "app_business.deleted", EntityType: "app_business",
			After: map[string]any{"businessId": id, "name": name}, IP: meta.IP, RequestID: meta.RequestID})
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c *Connector) handleCategories(w http.ResponseWriter, r *http.Request) {
	if _, ok := c.appScope(w, r, "app_business", "read"); !ok {
		return
	}
	rows, err := c.store.Pool.Query(r.Context(), `
		SELECT id::text, name FROM public.categories WHERE is_active ORDER BY sort_order, name`)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	type cat struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	out := []cat{}
	for rows.Next() {
		var c cat
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		out = append(out, c)
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}

// appRoutes are mounted under /w/{code}.
func (c *Connector) appRoutes(r chi.Router) {
	r.Get("/app/users", c.handleListAppUsers)
	r.Get("/app/users/{id}", c.handleGetAppUser)
	r.Patch("/app/users/{id}", c.handleUpdateAppUser)
	r.Delete("/app/users/{id}", c.handleDeleteAppUser)
	r.Get("/app/businesses", c.handleListBusinesses)
	r.Get("/app/businesses/{id}", c.handleGetBusiness)
	r.Patch("/app/businesses/{id}", c.handleUpdateBusiness)
	r.Delete("/app/businesses/{id}", c.handleDeleteBusiness)
	r.Get("/app/businesses/{id}/savers", c.handleBusinessSavers)
	r.Get("/app/businesses/{id}/card-image", c.handleBusinessCardImage)
	r.Put("/app/businesses/{id}/card-image", c.handleUploadBusinessCardImage)
	r.Get("/app/categories", c.handleCategories)
}

// appRelated adds the connected app's data to CRM records (D-90): a contact (the person)
// shows their app profile and saved cards; an account (a business, D-52) shows that business
// listing, the owner's app profile and, when the owner's contact sits under another of their
// businesses, a link back to that contact. Businesses are never listed on the contact.
func (c *Connector) appRelated(ctx context.Context, sc *records.Scope, object, recordID string) ([]records.RelatedList, error) {
	col := map[string]string{"accounts": "account_id", "contacts": "contact_id"}[object]
	if col == "" {
		return nil, nil
	}
	var userID string
	bizIDs := map[string]bool{}
	var ownerContact *string
	if object == "accounts" && c.hasBiz {
		rows, err := c.store.Pool.Query(ctx, `
			SELECT el.external_id, COALESCE(b.owner_user_id::text, ''), el.contact_id::text
			FROM crm.external_links el JOIN public.businesses b ON b.id::text = el.external_id
			WHERE el.system = $1 AND el.external_type = 'business' AND el.account_id = $2`, System, recordID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, owner string
			var contact *string
			if err := rows.Scan(&id, &owner, &contact); err != nil {
				rows.Close()
				return nil, err
			}
			bizIDs[id] = true
			if userID == "" && owner != "" {
				userID, ownerContact = owner, contact
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	if userID == "" {
		err := c.store.Pool.QueryRow(ctx, `SELECT external_id FROM crm.external_links WHERE system = $1 AND external_type = 'user' AND `+col+` = $2`,
			System, recordID).Scan(&userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
	}
	d, err := c.loadAppUser(ctx, userID)
	if err != nil {
		var se *shared.Error
		if errors.As(err, &se) {
			return nil, nil // deleted in the app
		}
		return nil, err
	}
	access := "Free"
	if d.Premium {
		access = "Premium · " + d.PlanName
		if d.ExpiresAt != nil {
			access += " until " + d.ExpiresAt.In(ist()).Format("2 Jan 2006")
		}
	}
	out := []records.RelatedList{{Key: "app-profile", Label: AppName + " profile", Object: "app-users", Rows: []records.RelatedRow{{
		ID: d.ID, Title: strings.TrimSpace(d.Name + " · " + d.Phone), Subtitle: access + " · " + itoa(len(d.Businesses)) + " businesses · " + itoa(len(d.Cards)) + " saved cards",
		Status: map[bool]string{true: "premium", false: "free"}[d.Premium],
	}}}}
	if object == "accounts" {
		// The owner's contact, when it isn't already this account's contact (a person keeps one primary account).
		if ownerContact != nil {
			var name, primary string
			if err := c.store.Pool.QueryRow(ctx, `SELECT trim(concat_ws(' ', first_name, last_name)), COALESCE(account_id::text, '') FROM crm.contacts
				WHERE id = $1 AND deleted_at IS NULL`, *ownerContact).Scan(&name, &primary); err == nil && primary != recordID {
				out = append(out, records.RelatedList{Key: "app-owner", Label: "Business owner", Object: "contacts", Rows: []records.RelatedRow{{
					ID: *ownerContact, Title: name, Subtitle: "Contact · owns this business in " + AppName}}})
			}
		}
		if c.hasBiz {
			l := records.RelatedList{Key: "app-businesses", Label: "Business listing", Object: "app-businesses", Rows: []records.RelatedRow{}}
			for _, b := range d.Businesses {
				// A business account shows its own listing; an older personal account shows them all.
				if len(bizIDs) > 0 && !bizIDs[b.ID] {
					continue
				}
				l.Rows = append(l.Rows, records.RelatedRow{ID: b.ID, Title: b.Name, Subtitle: strings.Trim(b.Category+" · "+b.City, " ·"), Status: b.Listing})
			}
			if len(bizIDs) == 0 {
				l.Label = "Businesses"
			}
			out = append(out, l)
		}
		return out, nil
	}
	if c.hasCards {
		l := records.RelatedList{Key: "app-cards", Label: "Saved cards", Object: "app-cards", Rows: []records.RelatedRow{}}
		for _, s := range d.Cards {
			title := s.PersonName
			if title == "" {
				title = s.Company
			}
			l.Rows = append(l.Rows, records.RelatedRow{ID: d.ID + "#" + s.ID, Title: title, Subtitle: strings.Trim(s.Designation+" · "+s.Company, " ·")})
		}
		out = append(out, l)
	}
	return out, nil
}

func ist() *time.Location {
	if loc, err := time.LoadLocation("Asia/Kolkata"); err == nil {
		return loc
	}
	return time.UTC
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// upgradeAppAccess (idempotent, every start): the product gets the app modules, and the
// workspace's customized Super Admin role gets the app objects once (D-44).
func upgradeAppAccess(ctx context.Context, tx pgx.Tx, wsID, productID uuid.UUID) error {
	mods := []string{"app_users", "directory"}
	if _, err := tx.Exec(ctx, `
		UPDATE crm.product_versions SET config = jsonb_set(config, '{modules}',
		       COALESCE(config->'modules', '[]'::jsonb) || to_jsonb(ARRAY(SELECT m FROM unnest($2::text[]) m WHERE NOT COALESCE(config->'modules', '[]'::jsonb) ? m)))
		WHERE product_id = $1 AND NOT (COALESCE(config->'modules', '[]'::jsonb) ?& $2::text[])`, productID, mods); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE crm.products SET draft_config = jsonb_set(draft_config, '{modules}',
		       COALESCE(draft_config->'modules', '[]'::jsonb) || to_jsonb(ARRAY(SELECT m FROM unnest($2::text[]) m WHERE NOT COALESCE(draft_config->'modules', '[]'::jsonb) ? m)))
		WHERE id = $1 AND draft_config ? 'modules' AND NOT (draft_config->'modules' ?& $2::text[])`, productID, mods); err != nil {
		return err
	}
	const marker = "cardflow:app-objects-v1"
	var done bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.connector_state WHERE key = $1)`, marker).Scan(&done); err != nil || done {
		return err
	}
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT base_rules FROM crm.roles WHERE workspace_id = $1 AND key = 'SUPER_ADMIN' AND customized`, wsID).Scan(&raw)
	if err == nil {
		var rules access.Rules
		if err := json.Unmarshal(raw, &rules); err != nil {
			return err
		}
		if rules.Objects == nil {
			rules.Objects = map[string][]string{}
		}
		if rules.Rows == nil {
			rules.Rows = map[string]map[string]string{}
		}
		for _, o := range access.CatalogObjects() {
			if !o.App {
				continue
			}
			if _, has := rules.Objects[o.Key]; !has {
				rules.Objects[o.Key] = append([]string{}, o.Actions...)
				rules.Rows[o.Key] = map[string]string{"scope": "workspace"}
			}
		}
		out, _ := json.Marshal(rules)
		if _, err := tx.Exec(ctx, `UPDATE crm.roles SET base_rules = $2, updated_at = now() WHERE workspace_id = $1 AND key = 'SUPER_ADMIN'`, wsID, out); err != nil {
			return err
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO crm.connector_state (key, value) VALUES ($1, '{"done":true}') ON CONFLICT (key) DO NOTHING`, marker)
	return err
}

func itoa(n int) string { return strconv.Itoa(n) }
