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
	"cardflow-backend/internal/crm/sms"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// "Create your own CRM" (D-160). The site's own page registers; it never signs anyone in,
// not a member, not a Super Admin, not the platform owner. Two calls load it: send a code
// and check the code. A correct code returns a short-lived registration token (no session,
// no cookie) and, for someone who already has an account, who they are and the URLs of
// their businesses. With the token they may create a business (it is linked to them) or
// have their URLs emailed. Signing in happens only at a business's own URL.

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
	r.Post("/public/register/send-urls", h.handleRegisterSendURLs)
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
	person, err := h.registeredPerson(ctx, proof.IdentityID)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	list, err := h.registeredBusinesses(ctx, proof.IdentityID)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	s := SelfServe(ctx, h.store.Pool)
	var created int
	_ = h.store.Pool.QueryRow(ctx, `SELECT count(*) FROM crm.workspaces WHERE created_by_identity = $1 AND origin = 'self_serve'`, proof.IdentityID).Scan(&created)
	pending, err := h.pendingApplications(ctx, proof.IdentityID)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	// Someone who already has an account is told on every channel too (D-161), in the
	// background so the page answers at once.
	var notify map[string]any
	if len(list) > 0 {
		notify = h.channels(person)
		go h.sendURLs(context.WithoutCancel(ctx), proof.IdentityID, person, list, identity.Meta(r))
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{
		"pending":           pending,
		"notify":            notify,
		"registrationToken": h.registerToken(proof.IdentityID, time.Now().Add(registerTTL)),
		"expiresIn":         int(registerTTL.Seconds()),
		"hasAccount":        len(list) > 0,
		"person":            person,
		"businesses":        list,
		"canCreate":         s.Enabled && created < s.MaxBusinesses,
		"limit":             s.MaxBusinesses,
		"created":           created,
	})
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
	// The business is linked to the person who proved the number, whoever else they are.
	status, resp, err := h.createBusiness(ctx, me, r.Header.Get("Idempotency-Key"), in.CreateBusinessInput)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if m, ok := resp.(map[string]any); ok {
		if b, ok := m["business"].(*Business); ok {
			m["loginPath"] = "/" + b.TenantSlug + "/login"
			m["loginUrl"] = strings.TrimRight(h.cfg.BaseURL, "/") + "/" + b.TenantSlug + "/login"
			m["display"] = DisplayCode(b.TenantSlug)
			delete(m, "next") // no session: the next step is signing in at the business's URL
		}
	}
	shared.WriteJSON(w, status, resp)
}

// POST /public/register/send-urls — "Request my URL": emails the person every business URL
// they have. SMS waits for a provider template (see SendBusinessURL).
func (h *Handler) handleRegisterSendURLs(w http.ResponseWriter, r *http.Request) {
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
	person, err := h.registeredPerson(ctx, me)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	list, err := h.registeredBusinesses(ctx, me)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if len(list) == 0 {
		shared.WriteError(w, r, shared.NewError(http.StatusNotFound, "no_business", "You don't belong to a business yet. Create one below."))
		return
	}
	sent := h.sendURLs(ctx, me, person, list, identity.Meta(r))
	if sent["email"] != "sent" && sent["sms"] != "sent" && sent["whatsapp"] != "sent" {
		if person.Email == "" && sent["sms"] != "failed" && sent["whatsapp"] != "failed" {
			shared.WriteError(w, r, shared.NewError(http.StatusUnprocessableEntity, "no_email", "There's no email on your account and text messages aren't set up yet. Ask your Super Admin for your business URL."))
			return
		}
		shared.WriteError(w, r, shared.NewError(http.StatusBadGateway, "email_failed", "We couldn't send your URLs right now. Please try again in a minute."))
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"sent": true, "email": maskEmail(person.Email), "channels": sent})
}

// sendURLs: "You already have a CRM account — use your URL to sign in", with every URL, by
// email, SMS and WhatsApp.
func (h *Handler) sendURLs(ctx context.Context, me uuid.UUID, person registeredPerson, list []RegisteredBusiness, meta identity.RequestMeta) map[string]string {
	lines := []string{fmt.Sprintf("Hi %s, you already have a %s account. Please sign in at your business URL:", firstNonEmpty(person.Name, "there"), h.cfg.AppName)}
	for _, b := range list {
		lines = append(lines, fmt.Sprintf("%s (%s): %s", b.Name, firstNonEmpty(b.Role, "Member"), b.LoginURL))
	}
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

// channels says where a notice to the person can go: the masked email, and whether SMS and
// WhatsApp are set up on this server.
func (h *Handler) channels(p registeredPerson) map[string]any {
	smsOK := false
	if h.sms != nil && p.Phone != "" {
		_, texter := h.sms.(sms.Texter)
		smsOK = texter || sms.IsPreview(h.sms)
	}
	email := ""
	if p.Email != "" {
		email = maskEmail(p.Email)
	}
	return map[string]any{"email": email, "sms": smsOK, "whatsapp": p.Phone != "" && h.wa.Ready()}
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

func maskEmail(e string) string {
	user, domain, ok := strings.Cut(e, "@")
	if !ok || len(user) == 0 {
		return e
	}
	return user[:1] + strings.Repeat("•", max(len(user)-1, 1)) + "@" + domain
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
