package records

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/plans"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// The business behind a workspace (D-94): its name, money and time settings, and the
// profile people fill in. Any member can read it; changing it needs "manage access" or
// "manage members" (the Super Admin of a self-serve business has both).
//
//	GET   /w/{code}/business
//	PATCH /w/{code}/business

func (h *Handler) businessRoutes(r chi.Router) {
	r.Get("/business", h.handleGetBusiness)
	r.Patch("/business", h.handleUpdateBusiness)
	h.cardImageRoutes(r)
	r.Get("/getting-started", h.handleGettingStarted)
	// The business's plan, what it uses, and the plans on offer (any member may look).
	r.Get("/plan", func(w http.ResponseWriter, r *http.Request) {
		out, err := plans.NewHandler(h.store).Overview(r, scopeFrom(r.Context()).WS)
		respond(w, r, http.StatusOK, out, err)
	})
}

type businessProfile struct {
	Code      string            `json:"code"`
	Name      string            `json:"name"`
	Currency  string            `json:"currency"`
	Timezone  string            `json:"timezone"`
	Locale    string            `json:"locale"`
	Origin    string            `json:"origin"`
	Profile   map[string]string `json:"profile"`
	Members   int               `json:"members"`
	CreatedAt time.Time         `json:"createdAt"`
	CanEdit   bool              `json:"canEdit"`
	// CardImages: which sides of the business card are uploaded (D-155).
	CardImages map[string]bool `json:"cardImages"`
	// LoginPath is the business's own sign-in address on this site.
	LoginPath string `json:"loginPath"`
}

// businessProfileKeys are the profile fields a business can fill in.
var businessProfileKeys = []string{"industry", "phone", "email", "website", "address", "city", "state", "postalCode", "country", "gstin", "pan", "about", "packs",
	"addressLine", "landmark", "area", "district", "latitude", "longitude", "whatsapp", "customDomain"}

var domainRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,24}$`)

func canEditBusiness(sc *Scope) bool {
	return sc.Owner || sc.Eff.HasCapability(access.CapAccessManage) || sc.Eff.HasCapability(access.CapMembersManage)
}

func (h *Handler) loadBusiness(r *http.Request) (*businessProfile, error) {
	sc := scopeFrom(r.Context())
	b := &businessProfile{Profile: map[string]string{}, CanEdit: canEditBusiness(sc)}
	var raw []byte
	err := h.store.Pool.QueryRow(r.Context(), `
		SELECT w.code, w.name, trim(w.currency), w.timezone, w.locale, w.origin, w.profile || jsonb_build_object('customDomain', COALESCE(w.custom_domain, '')), w.created_at,
		       (SELECT count(*) FROM crm.memberships m WHERE m.workspace_id = w.id AND m.status = 'active')
		FROM crm.workspaces w WHERE w.id = $1`, sc.WS).
		Scan(&b.Code, &b.Name, &b.Currency, &b.Timezone, &b.Locale, &b.Origin, &raw, &b.CreatedAt, &b.Members)
	if err != nil {
		return nil, err
	}
	all := map[string]any{}
	_ = json.Unmarshal(raw, &all)
	for _, k := range businessProfileKeys {
		if s, ok := all[k].(string); ok && s != "" {
			b.Profile[k] = s
		}
	}
	b.LoginPath = "/" + b.Code + "/login"
	b.CardImages = map[string]bool{"front": false, "back": false}
	rows, err := h.store.Pool.Query(r.Context(), `SELECT side FROM crm.business_card_images WHERE workspace_id = $1`, sc.WS)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var side string
		if rows.Scan(&side) == nil {
			b.CardImages[side] = true
		}
	}
	return b, rows.Err()
}

func (h *Handler) handleGetBusiness(w http.ResponseWriter, r *http.Request) {
	b, err := h.loadBusiness(r)
	respond(w, r, http.StatusOK, b, err)
}

func (h *Handler) handleUpdateBusiness(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !canEditBusiness(sc) {
		shared.WriteError(w, r, errForbidden)
		return
	}
	var in struct {
		Name     *string            `json:"name"`
		Currency *string            `json:"currency"`
		Timezone *string            `json:"timezone"`
		Profile  map[string]*string `json:"profile"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	fe := map[string]string{}
	if in.Name != nil {
		*in.Name = strings.TrimSpace(*in.Name)
		if n := len(*in.Name); n < 2 || n > 120 {
			fe["name"] = "Enter your business name (2–120 characters)."
		}
	}
	if in.Currency != nil {
		*in.Currency = strings.ToUpper(strings.TrimSpace(*in.Currency))
		if len(*in.Currency) != 3 {
			fe["currency"] = "Use a 3-letter currency code, like INR."
		}
	}
	if in.Timezone != nil {
		*in.Timezone = strings.TrimSpace(*in.Timezone)
		if _, err := time.LoadLocation(*in.Timezone); err != nil || *in.Timezone == "" {
			fe["timezone"] = "Choose a time zone, like Asia/Kolkata."
		}
	}
	allowed := map[string]bool{}
	for _, k := range businessProfileKeys {
		allowed[k] = true
	}
	for k, v := range in.Profile {
		if !allowed[k] {
			fe["profile."+k] = "This isn't a business profile field."
		} else if v != nil && len(strings.TrimSpace(*v)) > 500 {
			fe["profile."+k] = "Use at most 500 characters."
		}
	}
	h.checkProfile(r, in.Profile, fe)
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	a := actorFromRequest(r, "ui")
	err := h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		var before []byte
		if err := tx.QueryRow(ctx, `SELECT jsonb_build_object('name', name, 'currency', trim(currency), 'timezone', timezone, 'profile', profile)
			FROM crm.workspaces WHERE id = $1 FOR UPDATE`, sc.WS).Scan(&before); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE crm.workspaces SET name = COALESCE($2, name), currency = COALESCE($3, currency),
			timezone = COALESCE($4, timezone), updated_at = now() WHERE id = $1`, sc.WS, in.Name, in.Currency, in.Timezone); err != nil {
			return err
		}
		for k, v := range in.Profile {
			val := ""
			if v != nil {
				val = strings.TrimSpace(*v)
			}
			var err error
			if k == "customDomain" { // its own unique column: no two businesses share a domain
				_, err = tx.Exec(ctx, `UPDATE crm.workspaces SET custom_domain = NULLIF(lower($2), '') WHERE id = $1`, sc.WS, val)
			} else if val == "" {
				_, err = tx.Exec(ctx, `UPDATE crm.workspaces SET profile = profile - $2 WHERE id = $1`, sc.WS, k)
			} else {
				_, err = tx.Exec(ctx, `UPDATE crm.workspaces SET profile = profile || jsonb_build_object($2::text, $3::text) WHERE id = $1`, sc.WS, k, val)
			}
			if err != nil {
				return err
			}
		}
		var prev map[string]any
		_ = json.Unmarshal(before, &prev)
		id := sc.WS
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "business.updated", "workspace", &id, prev, in))
	})
	if err != nil && strings.Contains(err.Error(), "workspaces_custom_domain_key") {
		err = shared.Validation(map[string]string{"profile.customDomain": "Another business already uses this domain."})
	}
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	b, err := h.loadBusiness(r)
	respond(w, r, http.StatusOK, b, err)
}

// checkProfile tidies and checks the profile fields that have a format (D-155).
func (h *Handler) checkProfile(r *http.Request, p map[string]*string, fe map[string]string) {
	val := func(k string) string {
		if v, ok := p[k]; ok && v != nil {
			return strings.TrimSpace(*v)
		}
		return ""
	}
	set := func(k, v string) { p[k] = &v }
	if g := val("gstin"); g != "" {
		state := val("state")
		if _, given := p["state"]; !given {
			_ = h.store.Pool.QueryRow(r.Context(), `SELECT COALESCE(profile->>'state', '') FROM crm.workspaces WHERE id = $1`, scopeFrom(r.Context()).WS).Scan(&state)
		}
		norm, msg := shared.CheckGSTIN(g, state)
		if msg != "" {
			fe["profile.gstin"] = msg
		}
		set("gstin", norm)
	}
	if d := strings.ToLower(val("customDomain")); d != "" {
		d = strings.TrimPrefix(strings.TrimPrefix(d, "https://"), "http://")
		if i := strings.IndexAny(d, "/:?#"); i >= 0 {
			d = d[:i]
		}
		switch {
		case !domainRe.MatchString(d):
			fe["profile.customDomain"] = "Enter a domain like crm.yourbusiness.com."
		case strings.HasSuffix(d, ".vercel.app") || strings.HasSuffix(d, ".onrender.com") || strings.HasPrefix(d, "localhost"):
			fe["profile.customDomain"] = "Use a domain your business owns."
		}
		set("customDomain", d)
	}
	for _, k := range []string{"phone", "whatsapp"} {
		if v := val(k); v != "" {
			if n, ok := identity.NormalizePhone(v); ok {
				set(k, n)
			} else {
				fe["profile."+k] = "Enter a valid phone number."
			}
		}
	}
	if s := val("state"); s != "" && (val("country") == "" || strings.EqualFold(val("country"), "india")) {
		if st, ok := shared.IndianStateNamed(s); ok {
			set("state", st.Name)
		}
	}
}

// Business card images (D-155): the front and back of the business's own card.
//
//	GET    /w/{code}/business/card-images/{side}
//	PUT|POST /w/{code}/business/card-images/{side} (multipart "file", up to 5 MB, JPEG/PNG/WebP)
//	DELETE /w/{code}/business/card-images/{side}
func (h *Handler) cardImageRoutes(r chi.Router) {
	side := func(r *http.Request) (string, bool) {
		s := chi.URLParam(r, "side")
		return s, s == "front" || s == "back"
	}
	r.Get("/business/card-images/{side}", func(w http.ResponseWriter, r *http.Request) {
		s, ok := side(r)
		if !ok {
			shared.WriteError(w, r, shared.NotFound("not_found"))
			return
		}
		var ct string
		var data []byte
		err := h.store.Pool.QueryRow(r.Context(), `SELECT content_type, data FROM crm.business_card_images WHERE workspace_id = $1 AND side = $2`, scopeFrom(r.Context()).WS, s).Scan(&ct, &data)
		if errors.Is(err, pgx.ErrNoRows) {
			shared.WriteError(w, r, shared.NotFound("not_found"))
			return
		}
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "private, max-age=60")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(data)
	})
	put := func(w http.ResponseWriter, r *http.Request) {
		sc := scopeFrom(r.Context())
		s, ok := side(r)
		if !ok {
			shared.WriteError(w, r, shared.NotFound("not_found"))
			return
		}
		if !canEditBusiness(sc) {
			shared.WriteError(w, r, errForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 6<<20)
		f, _, err := r.FormFile("file")
		if err != nil {
			shared.WriteError(w, r, shared.Validation(map[string]string{"file": "Choose an image up to 5 MB."}))
			return
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, 5<<20+1))
		if err != nil || len(data) == 0 || len(data) > 5<<20 {
			shared.WriteError(w, r, shared.Validation(map[string]string{"file": "Choose an image up to 5 MB."}))
			return
		}
		ct := http.DetectContentType(data)
		if ct != "image/jpeg" && ct != "image/png" && ct != "image/webp" {
			shared.WriteError(w, r, shared.Validation(map[string]string{"file": "Use a JPEG, PNG or WebP photo."}))
			return
		}
		a := actorFromRequest(r, "ui")
		err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
			if _, err := tx.Exec(r.Context(), `INSERT INTO crm.business_card_images (workspace_id, side, content_type, data, uploaded_by) VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (workspace_id, side) DO UPDATE SET content_type = EXCLUDED.content_type, data = EXCLUDED.data, uploaded_by = EXCLUDED.uploaded_by, created_at = now()`,
				sc.WS, s, ct, data, a.ID); err != nil {
				return err
			}
			id := sc.WS
			return shared.WriteAudit(r.Context(), tx, a.audit(sc.WS, "business.card_image", "workspace", &id, nil, map[string]any{"side": s, "bytes": len(data)}))
		})
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		b, err := h.loadBusiness(r)
		respond(w, r, http.StatusOK, b, err)
	}
	r.Put("/business/card-images/{side}", put)
	r.Post("/business/card-images/{side}", put)
	r.Delete("/business/card-images/{side}", func(w http.ResponseWriter, r *http.Request) {
		sc := scopeFrom(r.Context())
		s, ok := side(r)
		if !ok || !canEditBusiness(sc) {
			shared.WriteError(w, r, errForbidden)
			return
		}
		if _, err := h.store.Pool.Exec(r.Context(), `DELETE FROM crm.business_card_images WHERE workspace_id = $1 AND side = $2`, sc.WS, s); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		b, err := h.loadBusiness(r)
		respond(w, r, http.StatusOK, b, err)
	})
}

// Getting started (D-109): what a new business should do first, with what is already
// done worked out from its real data — never a canned "100% complete".
//
//	GET /w/{code}/getting-started

type startStep struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Body  string `json:"body"`
	Done  bool   `json:"done"`
	// Go names where the step is done: lead | scan | task | business | team | account.
	Go string `json:"go"`
}

func (h *Handler) handleGettingStarted(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	ctx := r.Context()
	me := actor(r)
	var hasPeople, hasFollowUp, hasTeam, hasEmail bool
	var profileFields int
	err := h.store.Pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM crm.leads WHERE workspace_id = $1 AND deleted_at IS NULL)
		    OR EXISTS (SELECT 1 FROM crm.contacts WHERE workspace_id = $1 AND deleted_at IS NULL),
		       EXISTS (SELECT 1 FROM crm.object_records WHERE workspace_id = $1 AND deleted_at IS NULL AND object_key IN ('tasks', 'events'))
		    OR EXISTS (SELECT 1 FROM crm.leads WHERE workspace_id = $1 AND deleted_at IS NULL AND next_follow_up_at IS NOT NULL),
		       (SELECT count(*) FROM crm.memberships WHERE workspace_id = $1 AND status IN ('active', 'invited')) > 1,
		       EXISTS (SELECT 1 FROM crm.verified_identifiers WHERE identity_id = $2 AND kind = 'email' AND verified_at IS NOT NULL),
		       (SELECT count(*) FROM jsonb_object_keys((SELECT profile FROM crm.workspaces WHERE id = $1)))`,
		sc.WS, me).Scan(&hasPeople, &hasFollowUp, &hasTeam, &hasEmail, &profileFields)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	hasCard := false
	if h.cardsReady(ctx) {
		_ = h.store.Pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM public.saved_cards WHERE workspace_id = $1 AND deleted_at IS NULL)
			    OR EXISTS (SELECT 1 FROM crm.card_links WHERE workspace_id = $1)`, sc.WS).Scan(&hasCard)
	}
	steps := []startStep{}
	if sc.Can("leads", "create") {
		steps = append(steps, startStep{Key: "lead", Title: "Add your first lead", Go: "lead", Done: hasPeople,
			Body: "A lead is someone who might buy from you. Add one by hand, or scan their card."})
		steps = append(steps, startStep{Key: "scan", Title: "Scan a business card", Go: "scan", Done: hasCard,
			Body: "Take a photo of a paper card. It is read for you and saved as a lead or contact."})
	}
	if sc.Can("tasks", "create") {
		steps = append(steps, startStep{Key: "followup", Title: "Plan a follow-up", Go: "task", Done: hasFollowUp,
			Body: "Add a task or a meeting so you remember to call back. It shows on your Home screen on the day."})
	}
	if canEditBusiness(sc) {
		steps = append(steps, startStep{Key: "business", Title: "Complete your business profile", Go: "business", Done: profileFields >= 3,
			Body: "Phone, address and tax details appear on your digital card and public listing."})
		steps = append(steps, startStep{Key: "team", Title: "Add a teammate", Go: "team", Done: hasTeam,
			Body: "Add people by mobile number and choose what each of them can see and do."})
	}
	steps = append(steps, startStep{Key: "email", Title: "Add your email", Go: "account", Done: hasEmail,
		Body: "Verify an email address so you can also sign in with it and get important messages."})
	done := 0
	for _, s := range steps {
		if s.Done {
			done++
		}
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"steps": steps, "done": done, "total": len(steps)})
}
