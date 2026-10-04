package records

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"cardflow-backend/internal/crm/access"
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
}

// businessProfileKeys are the profile fields a business can fill in.
var businessProfileKeys = []string{"industry", "phone", "email", "website", "address", "city", "state", "postalCode", "country", "gstin", "pan", "about"}

func canEditBusiness(sc *Scope) bool {
	return sc.Owner || sc.Eff.HasCapability(access.CapAccessManage) || sc.Eff.HasCapability(access.CapMembersManage)
}

func (h *Handler) loadBusiness(r *http.Request) (*businessProfile, error) {
	sc := scopeFrom(r.Context())
	b := &businessProfile{Profile: map[string]string{}, CanEdit: canEditBusiness(sc)}
	var raw []byte
	err := h.store.Pool.QueryRow(r.Context(), `
		SELECT w.code, w.name, trim(w.currency), w.timezone, w.locale, w.origin, w.profile, w.created_at,
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
	return b, nil
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
			if val == "" {
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
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	b, err := h.loadBusiness(r)
	respond(w, r, http.StatusOK, b, err)
}
