package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Owner approval of new businesses (D-161). Someone's first business waits for the platform
// owner: it is created 'pending' with its URL already reserved, and a lead for it appears in
// the owner console. Approving opens the URL and sends it by email, SMS and WhatsApp. A
// person who already owns an approved business adds more without waiting. The owner can
// switch reviews off (self-serve settings, skipReview).

// needsReview: the person doesn't own an approved business yet.
func needsReview(ctx context.Context, tx pgx.Tx, me uuid.UUID, s SelfServeSettings) (bool, error) {
	if s.SkipReview {
		return false, nil
	}
	var owns bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.workspaces WHERE created_by_identity = $1 AND origin = 'self_serve' AND status = 'active')`, me).Scan(&owns)
	return !owns, err
}

// holdForReview marks a just-created business pending and files its application as a lead
// in the platform CRM.
func holdForReview(ctx context.Context, tx pgx.Tx, wsID, creator uuid.UUID, code string, in CreateBusinessInput) error {
	if _, err := tx.Exec(ctx, `UPDATE crm.workspaces SET status = 'pending' WHERE id = $1`, wsID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE crm.tenants SET status = 'pending' WHERE id = (SELECT tenant_id FROM crm.workspaces WHERE id = $1)`, wsID); err != nil {
		return err
	}
	var platformWS uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM crm.workspaces WHERE is_platform ORDER BY created_at LIMIT 1`).Scan(&platformWS); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // no platform CRM: the business still waits in Apps
		}
		return err
	}
	var owner *uuid.UUID
	_ = tx.QueryRow(ctx, `SELECT id FROM crm.identities WHERE is_platform_owner AND status = 'active' ORDER BY created_at LIMIT 1`).Scan(&owner)
	var mobile, myEmail string
	_ = tx.QueryRow(ctx, `
		SELECT COALESCE((SELECT value_normalized FROM crm.verified_identifiers WHERE identity_id = $1 AND kind = 'phone' ORDER BY verified_at DESC NULLS LAST LIMIT 1), ''),
		       COALESCE((SELECT value_normalized FROM crm.verified_identifiers WHERE identity_id = $1 AND kind = 'email' ORDER BY verified_at DESC NULLS LAST LIMIT 1), '')`,
		creator).Scan(&mobile, &myEmail)
	var n int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO crm.code_counters (workspace_id, prefix, next_value) VALUES ($1, 'L', 2)
		ON CONFLICT (workspace_id, prefix) DO UPDATE SET next_value = crm.code_counters.next_value + 1
		RETURNING next_value - 1`, platformWS).Scan(&n); err != nil {
		return err
	}
	name := strings.TrimSpace(in.DisplayName)
	if name == "" {
		_ = tx.QueryRow(ctx, `SELECT display_name FROM crm.identities WHERE id = $1`, creator).Scan(&name)
	}
	first, last := "", name
	if i := strings.LastIndex(name, " "); i > 0 {
		first, last = name[:i], name[i+1:]
	}
	if last == "" || last == "New user" {
		last = in.Name
	}
	taxType := strings.ToUpper(firstNonEmpty(in.TaxIDType, "ID"))
	desc := fmt.Sprintf("Applied on Create your own CRM for %s (%s %s, team %s, %s). Business URL /%s/login is reserved and opens when you approve it.",
		in.Name, taxType, in.TaxID, firstNonEmpty(in.TeamSize, "not given"), firstNonEmpty(in.Industry, "industry not given"), code)
	custom, _ := json.Marshal(map[string]any{"signup_workspace_id": wsID, "business_code": code, "tax_id": in.TaxID, "tax_id_type": in.TaxIDType, "team_size": in.TeamSize})
	var leadID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO crm.leads (workspace_id, code, first_name, last_name, organization, email, mobile, phone, source, status, description,
		                       industry, user_type, owner_id, created_by, updated_by, custom)
		VALUES ($1, $2, NULLIF($3, ''), $4, $5, NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), 'web', 'new', $9,
		        NULLIF($10, ''), 'business', COALESCE($11::uuid, $12::uuid), $12::uuid, $12::uuid, $13)
		RETURNING id`, platformWS, fmt.Sprintf("L-%06d", n), first, last, in.Name, firstNonEmpty(in.PersonalEmail, myEmail, in.Email),
		mobile, in.Phone, desc, in.Industry, owner, creator, custom).Scan(&leadID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE crm.workspaces SET application_lead_id = $2 WHERE id = $1`, wsID, leadID); err != nil {
		return err
	}
	return shared.WriteAudit(ctx, tx, shared.AuditEvent{WorkspaceID: &wsID, ActorID: &creator, Action: "business.applied",
		EntityType: "workspace", EntityID: &wsID, After: map[string]any{"code": code, "name": in.Name, "leadId": leadID}})
}

// Application is a self-serve business as the owner sees it.
type Application struct {
	WorkspaceID string     `json:"workspaceId"`
	Name        string     `json:"name"`
	Code        string     `json:"code"`
	Display     string     `json:"display"`
	Status      string     `json:"status"`
	LeadID      *string    `json:"leadId"`
	CreatedAt   time.Time  `json:"createdAt"`
	ApprovedAt  *time.Time `json:"approvedAt"`
	Members     int        `json:"members"`
	Industry    string     `json:"industry"`
	TeamSize    string     `json:"teamSize"`
	TaxID       string     `json:"taxId"`
	LoginURL    string     `json:"loginUrl"`
	OwnerID     *string    `json:"ownerId"`
	OwnerName   string     `json:"ownerName"`
	OwnerPhone  string     `json:"ownerPhone"`
}

// The card app's own workspace (business-card-snap) holds synced data, not a customer.
const applicationSelect = `
	SELECT w.id::text, w.name, COALESCE(t.slug, w.code), w.status, w.application_lead_id::text, w.created_at, w.approved_at,
	       (SELECT count(*) FROM crm.memberships m WHERE m.workspace_id = w.id AND m.status = 'active'),
	       COALESCE(w.profile->>'industry', ''), COALESCE(w.profile->>'teamSize', ''), COALESCE(w.profile->>'taxId', w.profile->>'gstin', ''),
	       w.created_by_identity::text, COALESCE(i.display_name, ''),
	       COALESCE((SELECT value_normalized FROM crm.verified_identifiers v WHERE v.identity_id = i.id AND v.kind = 'phone' ORDER BY verified_at DESC NULLS LAST LIMIT 1), '')
	FROM crm.workspaces w
	LEFT JOIN crm.tenants t ON t.id = w.tenant_id
	LEFT JOIN crm.identities i ON i.id = w.created_by_identity
	WHERE NOT w.is_platform AND w.origin <> 'system' AND w.code <> 'business-card-snap' AND w.status <> 'deleted'`

func (h *Handler) queryApplications(ctx context.Context, where string, args ...any) ([]Application, error) {
	rows, err := h.store.Pool.Query(ctx, applicationSelect+where+` ORDER BY (w.status = 'pending') DESC, w.created_at DESC LIMIT 500`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Application{}
	for rows.Next() {
		var a Application
		if err := rows.Scan(&a.WorkspaceID, &a.Name, &a.Code, &a.Status, &a.LeadID, &a.CreatedAt, &a.ApprovedAt, &a.Members,
			&a.Industry, &a.TeamSize, &a.TaxID, &a.OwnerID, &a.OwnerName, &a.OwnerPhone); err != nil {
			return nil, err
		}
		a.Display = DisplayCode(a.Code)
		a.LoginURL = strings.TrimRight(h.cfg.BaseURL, "/") + "/" + a.Code + "/login"
		out = append(out, a)
	}
	return out, rows.Err()
}

// ApprovalRoutes are the owner's (identity.RequireOwner is applied by the caller).
func (h *Handler) ApprovalRoutes(r chi.Router) {
	// GET /platform/applications?status=pending&leadId=… — businesses with their applicant.
	r.Get("/platform/applications", func(w http.ResponseWriter, r *http.Request) {
		where, args := "", []any{}
		if s := r.URL.Query().Get("status"); s != "" {
			args = append(args, s)
			where += fmt.Sprintf(" AND w.status = $%d", len(args))
		}
		if l, err := uuid.Parse(r.URL.Query().Get("leadId")); err == nil {
			args = append(args, l)
			where += fmt.Sprintf(" AND w.application_lead_id = $%d", len(args))
		}
		list, err := h.queryApplications(r.Context(), where, args...)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		shared.WriteJSON(w, http.StatusOK, map[string]any{"data": list})
	})
	r.Post("/platform/applications/{id}/approve", h.handleApprove)
	r.Get("/platform/customers", h.handleCustomers)
	r.Get("/platform/customers/{id}", h.handleCustomer)
	r.Get("/platform/records/{object}/{id}/businesses", h.handleRecordBusinesses)
}

// GET /platform/records/{object}/{id}/businesses — on a platform-CRM lead, contact or account
// (D-162): the person behind it (by the application it came from, else by mobile number or
// email) and their businesses — the ones they created (any status) and the ones they belong to.
func (h *Handler) handleRecordBusinesses(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("record_not_found"))
		return
	}
	// Contact details of the record (for an account: its contacts and converted leads).
	var detailSQL string
	switch chi.URLParam(r, "object") {
	case "leads":
		detailSQL = `SELECT l.email, l.mobile, l.phone, (SELECT w.created_by_identity FROM crm.workspaces w WHERE w.application_lead_id = l.id LIMIT 1)
			FROM crm.leads l WHERE l.id = $1`
	case "contacts":
		detailSQL = `SELECT c.email, c.mobile, c.phone, (SELECT w.created_by_identity FROM crm.workspaces w JOIN crm.leads l ON l.id = w.application_lead_id
			WHERE l.converted_contact_id = c.id LIMIT 1) FROM crm.contacts c WHERE c.id = $1`
	case "accounts":
		detailSQL = `SELECT c.email, c.mobile, c.phone, (SELECT w.created_by_identity FROM crm.workspaces w JOIN crm.leads l ON l.id = w.application_lead_id
			WHERE l.converted_account_id = $1 LIMIT 1) FROM crm.contacts c WHERE c.account_id = $1
			UNION ALL SELECT a.email, NULL, a.phone, NULL FROM crm.accounts a WHERE a.id = $1`
	default:
		shared.WriteError(w, r, shared.NotFound("record_not_found"))
		return
	}
	ctx := r.Context()
	rows, err := h.store.Pool.Query(ctx, detailSQL, id)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var emails, phones []string
	var person *uuid.UUID
	for rows.Next() {
		var email, mobile, phone *string
		var applicant *uuid.UUID
		if err := rows.Scan(&email, &mobile, &phone, &applicant); err != nil {
			rows.Close()
			shared.WriteError(w, r, err)
			return
		}
		if applicant != nil && person == nil {
			person = applicant
		}
		if email != nil && strings.TrimSpace(*email) != "" {
			emails = append(emails, strings.ToLower(strings.TrimSpace(*email)))
		}
		for _, p := range []*string{mobile, phone} {
			if p != nil {
				if d := lastDigits(*p); len(d) == 10 {
					phones = append(phones, d)
				}
			}
		}
	}
	rows.Close()
	if person == nil && (len(emails) > 0 || len(phones) > 0) {
		var found uuid.UUID
		err := h.store.Pool.QueryRow(ctx, `
			SELECT v.identity_id FROM crm.verified_identifiers v JOIN crm.identities i ON i.id = v.identity_id AND i.status <> 'deleted' AND NOT i.is_platform_owner
			WHERE (v.kind = 'phone' AND right(regexp_replace(v.value_normalized, '\D', '', 'g'), 10) = ANY($1))
			   OR (v.kind = 'email' AND v.value_normalized = ANY($2))
			ORDER BY (v.kind = 'phone') DESC LIMIT 1`, phones, emails).Scan(&found)
		if err == nil {
			person = &found
		} else if !errors.Is(err, pgx.ErrNoRows) {
			shared.WriteError(w, r, err)
			return
		}
	}
	if person == nil {
		shared.WriteJSON(w, http.StatusOK, map[string]any{"person": nil, "apps": []Application{}, "memberOf": []RegisteredBusiness{}})
		return
	}
	p, err := h.registeredPerson(ctx, *person)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	apps, err := h.queryApplications(ctx, " AND w.created_by_identity = $1", *person)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	member, err := h.registeredBusinesses(ctx, *person)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	own := map[string]bool{}
	for _, a := range apps {
		own[a.Code] = true
	}
	others := []RegisteredBusiness{}
	for _, b := range member {
		if !own[b.Slug] {
			others = append(others, b)
		}
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"person": map[string]any{"id": *person, "name": p.Name, "phone": p.Phone, "email": p.Email},
		"apps": apps, "memberOf": others})
}

func lastDigits(s string) string {
	d := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
	if len(d) > 10 {
		d = d[len(d)-10:]
	}
	return d
}

// POST /platform/applications/{id}/approve — open a pending business and send its URL.
func (h *Handler) handleApprove(w http.ResponseWriter, r *http.Request) {
	wsID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("business_not_found"))
		return
	}
	ctx := r.Context()
	me := actorID(r)
	var creator uuid.UUID
	var name, slug string
	var profile []byte
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		var status string
		var by *uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT w.status, w.created_by_identity, w.name, COALESCE(t.slug, w.code), w.profile
			FROM crm.workspaces w LEFT JOIN crm.tenants t ON t.id = w.tenant_id
			WHERE w.id = $1 AND NOT w.is_platform FOR UPDATE OF w`, wsID).Scan(&status, &by, &name, &slug, &profile); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return shared.NotFound("business_not_found")
			}
			return err
		}
		if status != "pending" {
			return shared.NewError(http.StatusConflict, "not_pending", "This business isn't waiting for approval.")
		}
		if by == nil {
			return shared.NewError(http.StatusConflict, "no_applicant", "This business has no applicant to send its URL to.")
		}
		creator = *by
		if _, err := tx.Exec(ctx, `UPDATE crm.workspaces SET status = 'active', approved_at = now(), approved_by = $2, updated_at = now() WHERE id = $1`, wsID, me); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE crm.tenants SET status = 'active', updated_at = now() WHERE id = (SELECT tenant_id FROM crm.workspaces WHERE id = $1) AND status = 'pending'`, wsID); err != nil {
			return err
		}
		// The lead moves on; converting it into a contact stays the owner's choice.
		if _, err := tx.Exec(ctx, `
			UPDATE crm.leads SET status = 'qualified', updated_at = now(), updated_by = $2
			WHERE id = (SELECT application_lead_id FROM crm.workspaces WHERE id = $1) AND converted_at IS NULL AND status IN ('new', 'contacted')`, wsID, me); err != nil {
			return err
		}
		return shared.WriteAudit(ctx, tx, auditEvent(r, "business.approved", "workspace", &wsID, &wsID, map[string]any{"status": "pending"}, map[string]any{"status": "active", "code": slug}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	// Its public listing (directory, card, QR) is made now that it is open.
	if AfterBusinessCreated != nil {
		var p map[string]string
		_ = json.Unmarshal(profile, &p)
		AfterBusinessCreated(ctx, creator, wsID, CreateBusinessInput{Name: name, Phone: p["phone"], Email: p["email"], Website: p["website"],
			City: p["city"], State: p["state"], Industry: p["industry"]})
	}
	url := strings.TrimRight(h.cfg.BaseURL, "/") + "/" + slug + "/login"
	sent := h.notifyPerson(ctx, creator, notice{
		Subject: name + " is approved: your CRM URL",
		Heading: name + " is ready",
		Lines: []string{
			fmt.Sprintf("Your business %s is approved on %s.", name, h.cfg.AppName),
			"Sign in with your mobile number at your business URL: " + url,
			"Then add your team (admins, staff) from Users inside.",
		},
		Footer: "Only the people you add can sign in at this URL.",
	})
	list, err := h.queryApplications(ctx, " AND w.id = $1", wsID)
	if err != nil || len(list) == 0 {
		shared.WriteJSON(w, http.StatusOK, map[string]any{"approved": true, "notified": sent})
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"approved": true, "application": list[0], "notified": sent})
}

// Customer (shown as a Product in the owner console): one person, one mobile number, who
// created businesses on Create your own CRM, and how many apps (businesses) they run.
type Customer struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Phone     string    `json:"phone"`
	Email     string    `json:"email"`
	Apps      int       `json:"apps"`
	Active    int       `json:"active"`
	Pending   int       `json:"pending"`
	Members   int       `json:"members"`
	CreatedAt time.Time `json:"createdAt"`
}

const customerSelect = `
	SELECT i.id::text, i.display_name,
	       COALESCE((SELECT value_normalized FROM crm.verified_identifiers v WHERE v.identity_id = i.id AND v.kind = 'phone' ORDER BY verified_at DESC NULLS LAST LIMIT 1), ''),
	       COALESCE((SELECT value_normalized FROM crm.verified_identifiers v WHERE v.identity_id = i.id AND v.kind = 'email' ORDER BY verified_at DESC NULLS LAST LIMIT 1),
	                (SELECT personal_email FROM crm.person_details d WHERE d.identity_id = i.id), ''),
	       count(w.id), count(w.id) FILTER (WHERE w.status = 'active'), count(w.id) FILTER (WHERE w.status = 'pending'),
	       COALESCE(sum((SELECT count(*) FROM crm.memberships m WHERE m.workspace_id = w.id AND m.status = 'active')), 0),
	       min(w.created_at)
	FROM crm.identities i
	JOIN crm.workspaces w ON w.created_by_identity = i.id AND w.origin = 'self_serve' AND NOT w.is_platform AND w.status <> 'deleted'`

func scanCustomer(row pgx.Row) (Customer, error) {
	var c Customer
	err := row.Scan(&c.ID, &c.Name, &c.Phone, &c.Email, &c.Apps, &c.Active, &c.Pending, &c.Members, &c.CreatedAt)
	return c, err
}

func (h *Handler) handleCustomers(w http.ResponseWriter, r *http.Request) {
	rows, err := h.store.Pool.Query(r.Context(), customerSelect+` GROUP BY i.id ORDER BY min(w.created_at) DESC LIMIT 1000`)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	out := []Customer{}
	for rows.Next() {
		c, err := scanCustomer(rows)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (h *Handler) handleCustomer(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("product_not_found"))
		return
	}
	c, err := scanCustomer(h.store.Pool.QueryRow(r.Context(), customerSelect+` WHERE i.id = $1 GROUP BY i.id`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		shared.WriteError(w, r, shared.NotFound("product_not_found"))
		return
	}
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	apps, err := h.queryApplications(r.Context(), " AND w.origin = 'self_serve' AND w.created_by_identity = $1", id)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"customer": c, "apps": apps})
}
