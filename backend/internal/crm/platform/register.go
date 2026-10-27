package platform

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// "Create your own CRM" (D-160). The site's own page registers; it never signs anyone in,
// not a member, not a Super Admin, not the platform owner. Two calls load it: send a code
// and check the code. A correct code returns a short-lived registration token (no session,
// no cookie) and nothing about the person (D-161). With the token they send the business
// form; everyone sees "we will review". A new person's business waits for the owner; someone
// who already has a CRM gets their URLs by SMS, WhatsApp and email instead. Signing in
// happens only at a business's own URL.

const registerTTL = 30 * time.Minute

var errRegisterExpired = shared.NewError(http.StatusUnauthorized, "registration_expired", "Your code check has expired. Enter your mobile number again.")

// registerToken: "reg_" + base64(identity.expiry) + "." + HMAC of both. Nothing is stored.
func (h *Handler) registerToken(id uuid.UUID, exp time.Time) string {
	payload := id.String() + "." + strconv.FormatInt(exp.Unix(), 10)
	return "reg_" + base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(h.registerMAC(payload))
}

func (h *Handler) registerMAC(payload string) []byte {
	m := hmac.New(sha256.New, append([]byte("crm-register:"), h.cfg.EncryptionKey...))
	m.Write([]byte(payload))
	return m.Sum(nil)
}

// registeredIdentity checks a registration token and returns the active identity it names.
func (h *Handler) registeredIdentity(ctx context.Context, token string) (uuid.UUID, error) {
	body, sig, ok := strings.Cut(strings.TrimPrefix(strings.TrimSpace(token), "reg_"), ".")
	raw, err1 := base64.RawURLEncoding.DecodeString(body)
	mac, err2 := base64.RawURLEncoding.DecodeString(sig)
	if !ok || err1 != nil || err2 != nil || !hmac.Equal(mac, h.registerMAC(string(raw))) {
		return uuid.Nil, errRegisterExpired
	}
	idPart, expPart, _ := strings.Cut(string(raw), ".")
	exp, err := strconv.ParseInt(expPart, 10, 64)
	id, err3 := uuid.Parse(idPart)
	if err != nil || err3 != nil || time.Now().Unix() > exp {
		return uuid.Nil, errRegisterExpired
	}
	var status string
	if err := h.store.Pool.QueryRow(ctx, `SELECT status FROM crm.identities WHERE id = $1`, id).Scan(&status); err != nil || status != "active" {
		return uuid.Nil, errRegisterExpired
	}
	return id, nil
}

// RegisteredBusiness is one business shown to a person who already has an account.
type RegisteredBusiness struct {
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Display   string `json:"display"`
	Role      string `json:"role"`
	RoleKey   string `json:"roleKey"`
	City      string `json:"city"`
	Members   int    `json:"members"`
	LoginPath string `json:"loginPath"`
	LoginURL  string `json:"loginUrl"`
}

type registeredPerson struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
}

func (h *Handler) registeredBusinesses(ctx context.Context, id uuid.UUID) ([]RegisteredBusiness, error) {
	list, err := ListBusinesses(ctx, h.store.Pool, id)
	if err != nil {
		return nil, err
	}
	out := []RegisteredBusiness{}
	for _, b := range list {
		if b.IsPlatform || b.TenantSlug == "" {
			continue
		}
		city, _ := b.Profile["city"].(string)
		out = append(out, RegisteredBusiness{Name: b.Name, Slug: b.TenantSlug, Display: DisplayCode(b.TenantSlug), Role: b.RoleName, RoleKey: b.RoleKey,
			City: city, Members: b.Members, LoginPath: "/" + b.TenantSlug + "/login", LoginURL: strings.TrimRight(h.cfg.BaseURL, "/") + "/" + b.TenantSlug + "/login"})
	}
	return out, nil
}

func (h *Handler) registeredPerson(ctx context.Context, id uuid.UUID) (registeredPerson, error) {
	var p registeredPerson
	err := h.store.Pool.QueryRow(ctx, `
		SELECT i.display_name,
		       COALESCE((SELECT value_normalized FROM crm.verified_identifiers WHERE identity_id = i.id AND kind = 'email' AND namespace = 'global' ORDER BY verified_at DESC NULLS LAST LIMIT 1),
		                (SELECT personal_email FROM crm.person_details WHERE identity_id = i.id), ''),
		       COALESCE((SELECT value_normalized FROM crm.verified_identifiers WHERE identity_id = i.id AND kind = 'phone' ORDER BY verified_at DESC NULLS LAST LIMIT 1), '')
		FROM crm.identities i WHERE i.id = $1`, id).Scan(&p.Name, &p.Email, &p.Phone)
	if p.Name == "New user" {
		p.Name = ""
	}
	return p, err
}

// RegisterRoutes need no sign-in (under /public/, so no CSRF cookie either).
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/public/register/request", h.handleRegisterRequest)
	r.Post("/public/register/verify", h.handleRegisterVerify)
	r.Post("/public/register/business", h.handleRegisterBusiness)
	r.Post("/public/register/request-url", h.handleRequestURL)
}

func (h *Handler) handleRegisterRequest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Phone string `json:"phone"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	res, err := h.identity.RequestRegisterCode(r.Context(), in.Phone, identity.Meta(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, res)
}

func (h *Handler) handleRegisterVerify(w http.ResponseWriter, r *http.Request) {
	var in identity.PhoneVerifyInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	in.Tenant, in.Name = "", ""
	ctx := r.Context()
	proof, err := h.identity.ProveRegisterPhone(ctx, in, identity.Meta(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	// Nothing about the person is shown on this page (D-161): new or not, the answer is the
	// same and the form comes next. Whether they already have a CRM is decided on the server
	// when the form is sent.
	shared.WriteJSON(w, http.StatusOK, map[string]any{
		"registrationToken": h.registerToken(proof.IdentityID, time.Now().Add(registerTTL)),
		"expiresIn":         int(registerTTL.Seconds()),
		"canCreate":         SelfServe(ctx, h.store.Pool).Enabled,
	})
}

// underReview is the one answer "Create your own CRM" gives after the form, whatever happened
// on the server, so the page never tells anyone whether a number already has an account.
func underReview(name string) map[string]any {
	return map[string]any{"pending": true, "business": map[string]any{"name": name, "status": "pending"}}
}

func (h *Handler) handleRegisterBusiness(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CreateBusinessInput
		RegistrationToken string `json:"registrationToken"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	me, err := h.registeredIdentity(ctx, in.RegistrationToken)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if strings.TrimSpace(in.TaxID) == "" && strings.TrimSpace(in.GSTIN) == "" {
		shared.WriteError(w, r, shared.Validation(map[string]string{"taxId": "Enter your GSTIN, PAN or TAN."}))
		return
	}
	fe := in.normalizeOnboarding()
	if len(strings.TrimSpace(in.Name)) < 2 {
		fe["name"] = "Enter your business or brand name."
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	// Step 4 (D-161): someone who already created a CRM for themselves gets nothing new; they
	// are told by SMS, WhatsApp and email to use their URL (sent again in the message). Someone
	// whose application waits is told it is in review. The page shows the same "we will
	// review" either way. More businesses are added from inside (Add business).
	var owns bool
	if err := h.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.workspaces WHERE created_by_identity = $1 AND origin = 'self_serve' AND status = 'active')`, me).Scan(&owns); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	pending, err := h.pendingApplications(ctx, me)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if owns || len(pending) > 0 {
		meta := identity.Meta(r)
		go h.tellExisting(context.WithoutCancel(ctx), me, owns, pending, in.Name, meta)
		shared.WriteJSON(w, http.StatusAccepted, underReview(in.Name))
		return
	}
	// The business is linked to the person who proved the number, whoever else they are.
	status, resp, err := h.createBusiness(ctx, me, r.Header.Get("Idempotency-Key"), in.CreateBusinessInput)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if m, ok := resp.(map[string]any); ok {
		if m["pending"] == true {
			resp = underReview(in.Name) // the code stays internal until the owner approves it
		} else if b, ok := m["business"].(*Business); ok {
			m["loginPath"] = "/" + b.TenantSlug + "/login"
			m["loginUrl"] = strings.TrimRight(h.cfg.BaseURL, "/") + "/" + b.TenantSlug + "/login"
			m["display"] = DisplayCode(b.TenantSlug)
			delete(m, "next") // no session: the next step is signing in at the business's URL
		}
	}
	shared.WriteJSON(w, status, resp)
}

// POST /public/register/request-url — "Forgot my CRM URL" (D-161): after the code, the
// person's business URLs go to them by SMS, WhatsApp and email, and the owner is told. The
// answer is the same whether or not the number has a CRM.
func (h *Handler) handleRequestURL(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RegistrationToken string `json:"registrationToken"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	me, err := h.registeredIdentity(ctx, in.RegistrationToken)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	meta := identity.Meta(r)
	go func(ctx context.Context) {
		person, err := h.registeredPerson(ctx, me)
		if err != nil {
			return
		}
		list, err := h.registeredBusinesses(ctx, me)
		if err != nil || len(list) == 0 {
			_ = shared.WriteAudit(ctx, h.store.Pool, shared.AuditEvent{ActorID: &me, Action: "register.url_requested", EntityType: "identity", EntityID: &me,
				IP: meta.IP, RequestID: meta.RequestID, After: map[string]any{"businesses": 0}})
			return
		}
		sent := h.sendURLs(ctx, me, person, list, meta)
		names := []string{}
		for _, b := range list {
			names = append(names, b.Name)
		}
		h.notifyOwner(ctx, "CRM URL requested: "+firstNonEmpty(person.Name, person.Phone), []string{
			fmt.Sprintf("%s (%s) used Forgot my CRM URL.", firstNonEmpty(person.Name, "Someone"), person.Phone),
			"Their URLs were sent for: " + strings.Join(names, ", ") + ".",
			fmt.Sprintf("Email: %s, SMS: %s, WhatsApp: %s.", sent["email"], sent["sms"], sent["whatsapp"]),
		})
	}(context.WithoutCancel(ctx))
	shared.WriteJSON(w, http.StatusOK, map[string]any{"sent": true})
}

// tellExisting: "You already have a CRM account — please use it" with their URLs, or "your
// application is being reviewed", by SMS, WhatsApp and email.
func (h *Handler) tellExisting(ctx context.Context, me uuid.UUID, owns bool, pending []PendingApplication, asked string, meta identity.RequestMeta) {
	person, err := h.registeredPerson(ctx, me)
	if err != nil {
		return
	}
	if owns {
		list, err := h.registeredBusinesses(ctx, me)
		if err != nil {
			return
		}
		h.sendURLs(ctx, me, person, list, meta)
		return
	}
	names := []string{}
	for _, p := range pending {
		names = append(names, p.Name)
	}
	sent := h.notifyPerson(ctx, me, notice{
		Subject: "Your CRM application is being reviewed",
		Heading: "We're reviewing your application",
		Lines: []string{
			fmt.Sprintf("Hi %s, your application for %s is already with us and being reviewed.", firstNonEmpty(person.Name, "there"), strings.Join(names, ", ")),
			"We'll send your business URL by SMS, WhatsApp and email once it's approved.",
		},
	})
	_ = shared.WriteAudit(ctx, h.store.Pool, shared.AuditEvent{ActorID: &me, Action: "register.repeat_application", EntityType: "identity", EntityID: &me,
		IP: meta.IP, RequestID: meta.RequestID, After: map[string]any{"asked": asked, "channels": sent}})
}

// sendURLs: "You already have a CRM account — use your URL to sign in", with every URL, by
// email, SMS and WhatsApp.
func (h *Handler) sendURLs(ctx context.Context, me uuid.UUID, person registeredPerson, list []RegisteredBusiness, meta identity.RequestMeta) map[string]string {
	lines := []string{fmt.Sprintf("Hi %s, you already have a %s account, so please use it. In case you missed your URL, here it is again:", firstNonEmpty(person.Name, "there"), h.cfg.AppName)}
	for _, b := range list {
		lines = append(lines, fmt.Sprintf("%s (%s): %s", b.Name, firstNonEmpty(b.Role, "Member"), b.LoginURL))
	}
	lines = append(lines, "To add another business, sign in and use Add business.")
	sent := h.notifyPerson(ctx, me, notice{
		Subject: "You already have a CRM account",
		Heading: "Your business URLs",
		Lines:   lines,
		Footer:  "Someone entered your mobile number on " + h.cfg.AppName + " and confirmed it with the code we texted. If it wasn't you, you can ignore this message.",
	})
	_ = shared.WriteAudit(ctx, h.store.Pool, shared.AuditEvent{ActorID: &me, Action: "register.urls_sent", EntityType: "identity", EntityID: &me,
		IP: meta.IP, RequestID: meta.RequestID, After: map[string]any{"businesses": len(list), "channels": sent}})
	return sent
}

// PendingApplication is a business of the person's that waits for the owner's approval.
type PendingApplication struct {
	Name      string    `json:"name"`
	Code      string    `json:"code"`
	Display   string    `json:"display"`
	CreatedAt time.Time `json:"createdAt"`
}

func (h *Handler) pendingApplications(ctx context.Context, me uuid.UUID) ([]PendingApplication, error) {
	rows, err := h.store.Pool.Query(ctx, `
		SELECT w.name, COALESCE(t.slug, w.code), w.created_at FROM crm.workspaces w LEFT JOIN crm.tenants t ON t.id = w.tenant_id
		WHERE w.created_by_identity = $1 AND w.status = 'pending' ORDER BY w.created_at`, me)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PendingApplication{}
	for rows.Next() {
		var a PendingApplication
		if err := rows.Scan(&a.Name, &a.Code, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Display = DisplayCode(a.Code)
		out = append(out, a)
	}
	return out, rows.Err()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
