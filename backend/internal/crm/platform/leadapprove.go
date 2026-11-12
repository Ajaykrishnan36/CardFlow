package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Converting one of the owner's leads is approving it (D-180): the lead is a person who
// wants a CRM. A lead from "Create your own CRM" already has its business waiting; a lead
// the owner typed in gets its business made now from the lead's details, with the person
// (their mobile number) as its Super Admin. Either way the owner picks the plan, the
// business opens, its URL is sent, and the lead becomes a contact.

// POST /platform/leads/{id}/approve
func (h *Handler) handleApproveLead(w http.ResponseWriter, r *http.Request) {
	leadID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("lead_not_found"))
		return
	}
	var in approveInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if !Plans[in.Plan] {
		shared.WriteError(w, r, shared.Validation(map[string]string{"plan": "Choose the plan."}))
		return
	}
	ctx := r.Context()
	wsID, err := h.businessForLead(ctx, leadID)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	res, err := h.approveBusiness(r, wsID, in)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, res)
}

// businessForLead returns the business waiting on this lead, making it from the lead's
// details when there is none yet.
func (h *Handler) businessForLead(ctx context.Context, leadID uuid.UUID) (uuid.UUID, error) {
	var wsID uuid.UUID
	var status string
	err := h.store.Pool.QueryRow(ctx, `SELECT id, status FROM crm.workspaces WHERE application_lead_id = $1 AND status <> 'deleted' ORDER BY created_at DESC LIMIT 1`, leadID).Scan(&wsID, &status)
	switch {
	case err == nil && status == "pending":
		return wsID, nil
	case err == nil:
		return uuid.Nil, shared.NewError(http.StatusConflict, "already_approved", "This lead's business is already approved.")
	case !errors.Is(err, pgx.ErrNoRows):
		return uuid.Nil, err
	}

	var (
		first, last, org, email, mobile, phone, industry *string
		custom                                           []byte
		converted                                        bool
	)
	err = h.store.Pool.QueryRow(ctx, `
		SELECT l.first_name, l.last_name, l.organization, l.email, l.mobile, l.phone, l.industry, COALESCE(l.custom, '{}'), l.converted_at IS NOT NULL
		FROM crm.leads l JOIN crm.workspaces w ON w.id = l.workspace_id AND w.is_platform
		WHERE l.id = $1 AND l.deleted_at IS NULL`, leadID).Scan(&first, &last, &org, &email, &mobile, &phone, &industry, &custom, &converted)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, shared.NotFound("lead_not_found")
	}
	if err != nil {
		return uuid.Nil, err
	}
	if converted {
		return uuid.Nil, shared.NewError(http.StatusConflict, "already_converted", "This lead is already approved.")
	}
	var c map[string]any
	_ = json.Unmarshal(custom, &c)
	str := func(k string) string { v, _ := c[k].(string); return strings.TrimSpace(v) }

	fe := map[string]string{}
	name := strings.TrimSpace(deref(first) + " " + deref(last))
	ph, ok := identity.NormalizePhone(deref(mobile))
	if !ok {
		fe["mobile"] = "Add the person's mobile number to the lead first: they sign in with it."
	}
	if strings.TrimSpace(deref(org)) == "" {
		fe["organization"] = "Add the business / brand name to the lead first."
	}
	if strings.TrimSpace(deref(industry)) == "" {
		fe["industry"] = "Add the industry to the lead first."
	}
	if str("taxId") == "" {
		fe["taxId"] = "Add the GSTIN, PAN or TAN to the lead first."
	}
	if len(fe) > 0 {
		return uuid.Nil, shared.Validation(fe)
	}
	in := CreateBusinessInput{
		Name: deref(org), Industry: deref(industry), Phone: deref(phone), Email: str("businessEmail"),
		TaxIDType: firstNonEmpty(str("taxIdType"), "gstin"), TaxID: str("taxId"), TeamSize: str("teamSize"), Plan: str("plan"),
		DisplayName: name, PersonalEmail: deref(email), Mode: "add",
	}
	// The lead's business type is typed freely; the list's own name is used when it matches.
	bt := str("businessType")
	if ind := access.FindIndustry(in.Industry); ind != nil {
		switch {
		case ind.Label == access.OtherIndustry:
			in.IndustryOther = firstNonEmpty(bt, in.Name)
		case bt == "":
		case ind.FindType(bt) != nil:
			in.BusinessType = bt
		case ind.FindType("other") != nil:
			in.BusinessType, in.BusinessTypeOther = "other", bt
		}
	}
	setupID, err := EnsureStandardSetup(ctx, h.store.Pool)
	if err != nil {
		return uuid.Nil, err
	}
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		person, err := identityForPhone(ctx, tx, ph, firstNonEmpty(name, deref(org)))
		if err != nil {
			return err
		}
		id, _, err := CreateBusinessTx(ctx, tx, person, setupID, in)
		if err != nil {
			return renameLeadFields(err)
		}
		wsID = id
		if _, err := tx.Exec(ctx, `UPDATE crm.workspaces SET status = 'pending', application_lead_id = $2 WHERE id = $1`, id, leadID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE crm.tenants SET status = 'pending' WHERE id = (SELECT tenant_id FROM crm.workspaces WHERE id = $1)`, id); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE crm.leads SET identity_id = COALESCE(identity_id, $2) WHERE id = $1`, leadID, person)
		return err
	})
	return wsID, err
}

// renameLeadFields points the business form's messages at the lead's fields.
func renameLeadFields(err error) error {
	var ae *shared.Error
	if !errors.As(err, &ae) || len(ae.FieldErrors) == 0 {
		return err
	}
	names := map[string]string{"name": "organization", "phone": "phone", "email": "businessEmail", "personalEmail": "email", "displayName": "lastName"}
	out := map[string]string{}
	for k, v := range ae.FieldErrors {
		if n, ok := names[k]; ok {
			k = n
		}
		out[k] = v
	}
	return shared.Validation(out)
}

// identityForPhone finds the person with this mobile number, or makes them: they sign in
// to their business with it (D-158).
func identityForPhone(ctx context.Context, tx pgx.Tx, phone, name string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT identity_id FROM crm.verified_identifiers WHERE kind = 'phone' AND namespace = 'global' AND value_normalized = $1`, phone).Scan(&id)
	if err == nil || !errors.Is(err, pgx.ErrNoRows) {
		return id, err
	}
	if err := tx.QueryRow(ctx, `INSERT INTO crm.identities (display_name, source) VALUES ($1, 'phone_signup') RETURNING id`, name).Scan(&id); err != nil {
		return id, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO crm.verified_identifiers (identity_id, kind, value_normalized, namespace, verified_at) VALUES ($1, 'phone', $2, 'global', now())`, id, phone)
	return id, err
}

// MigrateApplicationLeads gives sign-up leads from before D-180 the lead form's own keys
// (taxId, teamSize, plan …). It only adds keys; nothing is removed or overwritten.
func MigrateApplicationLeads(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		UPDATE crm.leads l SET custom = jsonb_strip_nulls(jsonb_build_object(
		         'taxId', l.custom->>'tax_id', 'taxIdType', l.custom->>'tax_id_type', 'teamSize', l.custom->>'team_size',
		         'plan', NULLIF(w.profile->>'plan', ''), 'businessType', NULLIF(w.profile->>'businessType', ''),
		         'businessEmail', NULLIF(w.profile->>'email', ''))) || l.custom
		FROM crm.workspaces w
		WHERE w.application_lead_id = l.id AND l.custom ? 'tax_id' AND NOT l.custom ? 'taxId'`)
	if err != nil {
		return err
	}
	// Where each lead came from (D-181): sign-ups are marked; the website marks its own.
	_, err = pool.Exec(ctx, `
		UPDATE crm.leads l SET custom = COALESCE(l.custom, '{}') || '{"requestType":"signup"}'
		FROM crm.workspaces w
		WHERE w.application_lead_id = l.id AND NOT COALESCE(l.custom, '{}') ? 'requestType'`)
	return err
}
