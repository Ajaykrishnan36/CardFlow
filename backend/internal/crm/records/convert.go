package records

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"cardflow-backend/internal/crm/platform"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Lead conversion (PRD §6.2, CRM-01/CRM-02): one transaction creates the account (and
// contact), marks the lead converted and — when the owner gives the lead CRM access —
// provisions the customer workspace and invites the lead as its Super Admin. Saving or
// converting a lead never grants access by itself; only the invitation does.

type convertInput struct {
	Account struct {
		Mode      string    `json:"mode"`
		Name      string    `json:"name"`
		Kind      string    `json:"kind"`
		AccountID uuid.UUID `json:"accountId"`
	} `json:"account"`
	CreateContact bool                     `json:"createContact"`
	Access        *platform.GiveLoginInput `json:"access,omitempty"`
	Provision     *struct {
		WorkspaceName string      `json:"workspaceName"`
		WorkspaceCode string      `json:"workspaceCode"`
		ProductIDs    []uuid.UUID `json:"productIds"`
		Timezone      string      `json:"timezone"`
		Currency      string      `json:"currency"`
	} `json:"provision,omitempty"`
}

type ConvertResult struct {
	AccountID     uuid.UUID            `json:"accountId"`
	ContactID     *uuid.UUID           `json:"contactId,omitempty"`
	WorkspaceID   *uuid.UUID           `json:"workspaceId,omitempty"`
	IdentityID    *uuid.UUID           `json:"identityId,omitempty"`
	Invitation    *platform.Invitation `json:"invitation,omitempty"`
	ExistingLogin bool                 `json:"existingLogin,omitempty"`
}

type leadRow struct {
	status, salutation, firstName, lastName, title, organization, email, phone, mobile, website string
	industry, source, rating, street, city, state, postalCode, country, description             string
	annualRevenue                                                                               *float64
	employees                                                                                   *int
	custom                                                                                      map[string]any
}

func (h *Handler) handleConvert(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	ws := sc.WS
	leadID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("record_not_found"))
		return
	}
	var in convertInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	// Members convert within their workspace only: no logins or tenants (PRD §2: a
	// conversion inside a customer workspace never creates a new tenant).
	if !sc.Owner {
		switch {
		case !sc.Can("leads", "convert"):
			shared.WriteError(w, r, errForbidden)
			return
		case in.Account.Mode != "existing" && !sc.Can("accounts", "create"), in.CreateContact && !sc.Can("contacts", "create"):
			shared.WriteError(w, r, shared.Forbidden("forbidden", "Converting needs permission to create accounts and contacts."))
			return
		case in.Access != nil || in.Provision != nil:
			shared.WriteError(w, r, shared.Forbidden("forbidden", "Only the platform owner can give logins."))
			return
		}
	}
	var passwordHash string
	if in.Access != nil {
		var p platform.Person
		if err := h.store.Pool.QueryRow(r.Context(), `
			SELECT trim(concat_ws(' ', first_name, last_name)), COALESCE(email, ''), COALESCE(phone, mobile, '')
			FROM crm.leads WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL`, leadID, ws).Scan(&p.Name, &p.Email, &p.Phone); err != nil {
			shared.WriteError(w, r, shared.NotFound("record_not_found"))
			return
		}
		if passwordHash, err = platform.PrepareGiveLogin(in.Access, p, "access."); err != nil {
			shared.WriteError(w, r, err)
			return
		}
	}
	status, resp, err := shared.Idempotent(r.Context(), h.store.Pool, actor(r), r.Header.Get("Idempotency-Key"),
		map[string]any{"lead": leadID, "body": in}, func() (int, any, error) {
			res, sent, err := h.convert(r, ws, leadID, in, passwordHash)
			if err != nil {
				return 0, nil, renameProvisionErrors(err)
			}
			if sent != nil {
				res.Invitation = h.platform.DeliverInvitation(r.Context(), sent)
			}
			return http.StatusOK, res, nil
		})
	respond(w, r, status, resp, err)
}

func (h *Handler) convert(r *http.Request, ws, leadID uuid.UUID, in convertInput, passwordHash string) (*ConvertResult, *platform.SentInvitation, error) {
	ctx := r.Context()
	me := actor(r)
	res := &ConvertResult{}
	var sent *platform.SentInvitation

	err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		var l leadRow
		var customRaw []byte
		err := tx.QueryRow(ctx, `
			SELECT status, COALESCE(salutation, ''), COALESCE(first_name, ''), COALESCE(last_name, ''), COALESCE(title, ''),
			       COALESCE(organization, ''), COALESCE(email, ''), COALESCE(phone, ''), COALESCE(mobile, ''), COALESCE(website, ''),
			       COALESCE(industry, ''), source, COALESCE(rating, ''), COALESCE(street, ''), COALESCE(city, ''), COALESCE(state, ''),
			       COALESCE(postal_code, ''), COALESCE(country, ''), COALESCE(description, ''), annual_revenue::float8, employees, custom
			FROM crm.leads WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL FOR UPDATE`, leadID, ws).Scan(
			&l.status, &l.salutation, &l.firstName, &l.lastName, &l.title, &l.organization, &l.email, &l.phone, &l.mobile, &l.website,
			&l.industry, &l.source, &l.rating, &l.street, &l.city, &l.state, &l.postalCode, &l.country, &l.description,
			&l.annualRevenue, &l.employees, &customRaw)
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.NotFound("record_not_found")
		}
		if err != nil {
			return err
		}
		_ = json.Unmarshal(customRaw, &l.custom)
		if own := ownerFilter(r, &leadSpec); own != nil {
			var mine bool
			if err := tx.QueryRow(ctx, `SELECT owner_id = $2 FROM crm.leads WHERE id = $1`, leadID, *own).Scan(&mine); err != nil || !mine {
				return shared.NotFound("record_not_found")
			}
		}
		if l.status == "converted" {
			return shared.NewError(http.StatusUnprocessableEntity, "already_converted", "This lead has already been converted.")
		}
		fullName := strings.TrimSpace(l.firstName + " " + l.lastName)

		// Custom values carry over to the new records where a custom field with the same key exists.
		carry := func(object string) map[string]any {
			out := map[string]any{}
			defs, err := customFields(ctx, tx, ws, object)
			if err != nil {
				return out
			}
			for _, d := range defs {
				if v, ok := l.custom[d.Key]; ok && v != nil {
					out[d.Key] = v
				}
			}
			return out
		}

		// 1. Account
		switch in.Account.Mode {
		case "existing":
			var ok bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.accounts WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL)`,
				in.Account.AccountID, ws).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return shared.Validation(map[string]string{"account": "Pick an existing account."})
			}
			res.AccountID = in.Account.AccountID
		case "new", "":
			name := strings.TrimSpace(in.Account.Name)
			if name == "" {
				name = l.organization
			}
			if name == "" {
				name = fullName
			}
			if name == "" || len(name) > 255 {
				return shared.Validation(map[string]string{"account": "Enter an account name."})
			}
			kind := in.Account.Kind
			if kind != "individual" {
				kind = "business"
			}
			lifecycle := "prospect"
			if in.Provision != nil {
				lifecycle = "onboarding"
			}
			code, err := nextCode(ctx, tx, ws, "A")
			if err != nil {
				return err
			}
			var email *string
			if kind == "individual" && l.email != "" {
				email = &l.email
			}
			customJSON, _ := json.Marshal(carry("accounts"))
			if err := tx.QueryRow(ctx, `
				INSERT INTO crm.accounts (workspace_id, code, kind, name, type, lifecycle, industry, rating, website, email, phone,
				                          annual_revenue, employees, billing_street, billing_city, billing_state, billing_postal_code,
				                          billing_country, description, owner_id, created_by, updated_by, custom)
				VALUES ($1, $2, $3, $4, 'customer', $5, NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), $9, NULLIF($10, ''),
				        $11, $12, NULLIF($13, ''), NULLIF($14, ''), NULLIF($15, ''), NULLIF($16, ''), NULLIF($17, ''), NULLIF($18, ''),
				        $19, $19, $19, $20)
				RETURNING id`,
				ws, code, kind, name, lifecycle, l.industry, l.rating, l.website, email, firstNonEmpty(l.phone, l.mobile),
				l.annualRevenue, l.employees, l.street, l.city, l.state, l.postalCode, l.country, l.description, me, customJSON).Scan(&res.AccountID); err != nil {
				return err
			}
		default:
			return shared.Validation(map[string]string{"account": "Choose to create a new account or pick an existing one."})
		}

		// 2. Contact
		if in.CreateContact {
			if l.lastName == "" && l.firstName == "" {
				return shared.Validation(map[string]string{"contact": "The lead needs a name to create a contact."})
			}
			code, err := nextCode(ctx, tx, ws, "C")
			if err != nil {
				return err
			}
			lastName := l.lastName
			if lastName == "" {
				lastName = l.firstName
			}
			customJSON, _ := json.Marshal(carry("contacts"))
			var cid uuid.UUID
			if err := tx.QueryRow(ctx, `
				INSERT INTO crm.contacts (workspace_id, code, account_id, salutation, first_name, last_name, title, email, phone, mobile,
				                          lead_source, mailing_street, mailing_city, mailing_state, mailing_postal_code, mailing_country,
				                          owner_id, created_by, updated_by, custom)
				VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6, NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''), NULLIF($10, ''),
				        NULLIF($11, ''), NULLIF($12, ''), NULLIF($13, ''), NULLIF($14, ''), NULLIF($15, ''), NULLIF($16, ''), $17, $17, $17, $18)
				RETURNING id`,
				ws, code, res.AccountID, l.salutation, nullIfSame(l.firstName, lastName), lastName, l.title, l.email, l.phone, l.mobile,
				l.source, l.street, l.city, l.state, l.postalCode, l.country, me, customJSON).Scan(&cid); err != nil {
				return err
			}
			res.ContactID = &cid
		}

		// 3a. CRM access: any workspace, role, permission sets; invitation or temporary password.
		if in.Access != nil {
			res2, s2, err := h.platform.GiveLoginTx(ctx, tx, me, platform.Person{Name: fullName, Email: l.email, Phone: firstNonEmpty(l.phone, l.mobile)},
				*in.Access, passwordHash, "access.")
			if err != nil {
				return err
			}
			sent = s2
			res.IdentityID, res.ExistingLogin = &res2.IdentityID, res2.ExistingLogin
			var isPlatform bool
			if err := tx.QueryRow(ctx, `SELECT is_platform FROM crm.workspaces WHERE id = $1`, res2.WorkspaceID).Scan(&isPlatform); err != nil {
				return err
			}
			if !isPlatform {
				wsID := res2.WorkspaceID
				res.WorkspaceID = &wsID
				if _, err := tx.Exec(ctx, `UPDATE crm.accounts SET customer_workspace_id = COALESCE(customer_workspace_id, $2), lifecycle = 'onboarding'
					WHERE id = $1`, res.AccountID, wsID); err != nil {
					return err
				}
			}
			if err := linkIdentity(ctx, tx, res2.IdentityID, leadID, res.AccountID, res.ContactID); err != nil {
				return err
			}
		}

		// 3b. Legacy CRM access: customer workspace + Super Admin invitation.
		if p := in.Provision; p != nil && in.Access == nil {
			if l.email == "" {
				return shared.Validation(map[string]string{"provision": "Add an email to the lead before giving them a login."})
			}
			pin := platform.ProvisionInput{
				Name: p.WorkspaceName, Code: p.WorkspaceCode, Timezone: p.Timezone, Currency: p.Currency, ProductIDs: p.ProductIDs,
			}
			if fe := platform.ValidateProvision(&pin, "provision."); len(fe) > 0 {
				return shared.Validation(fe)
			}
			wsID, err := platform.ProvisionTx(ctx, tx, me, pin, "provision.")
			if err != nil {
				return err
			}
			res.WorkspaceID = &wsID
			name := fullName
			if name == "" {
				name = l.email
			}
			if sent, err = h.platform.InviteTx(ctx, tx, me, wsID, platform.InviteInput{Name: name, Email: l.email, RoleKey: "SUPER_ADMIN"}, "provision."); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE crm.accounts SET customer_workspace_id = $2, identity_id = COALESCE(identity_id, $3),
				lifecycle = 'onboarding', updated_at = now() WHERE id = $1`, res.AccountID, wsID, sent.IdentityID); err != nil {
				return err
			}
			if res.ContactID != nil {
				if _, err := tx.Exec(ctx, `UPDATE crm.contacts SET identity_id = $2 WHERE id = $1`, *res.ContactID, sent.IdentityID); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE crm.leads SET identity_id = $2 WHERE id = $1`, leadID, sent.IdentityID); err != nil {
				return err
			}
		}

		// 4. Lead → converted, with the conversion record.
		if _, err := tx.Exec(ctx, `
			UPDATE crm.leads SET status = 'converted', converted_at = now(), converted_account_id = $2, converted_contact_id = $3,
			       version = version + 1, updated_at = now(), updated_by = $4
			WHERE id = $1`, leadID, res.AccountID, res.ContactID, me); err != nil {
			return err
		}
		var invID *uuid.UUID
		if sent != nil {
			invID = &sent.ID
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.lead_conversions (workspace_id, lead_id, account_id, contact_id, workspace_provisioned_id, invitation_id, converted_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, ws, leadID, res.AccountID, res.ContactID, res.WorkspaceID, invID, me); err != nil {
			return err
		}
		return shared.WriteAudit(ctx, tx, audit(r, ws, "lead.converted", "lead", &leadID, nil, map[string]any{
			"accountId": res.AccountID, "contactId": res.ContactID, "workspaceId": res.WorkspaceID,
		}))
	})
	return res, sent, err
}

// linkIdentity records who the login belongs to on the lead, account and contact.
func linkIdentity(ctx context.Context, tx pgx.Tx, identityID, leadID, accountID uuid.UUID, contactID *uuid.UUID) error {
	if _, err := tx.Exec(ctx, `UPDATE crm.leads SET identity_id = $2 WHERE id = $1`, leadID, identityID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE crm.accounts SET identity_id = COALESCE(identity_id, $2) WHERE id = $1`, accountID, identityID); err != nil {
		return err
	}
	if contactID != nil {
		if _, err := tx.Exec(ctx, `UPDATE crm.contacts SET identity_id = $2 WHERE id = $1`, *contactID, identityID); err != nil {
			return err
		}
	}
	return nil
}

// renameProvisionErrors maps shared provisioning field keys onto the convert form's names.
func renameProvisionErrors(err error) error {
	var e *shared.Error
	if !errors.As(err, &e) || len(e.FieldErrors) == 0 {
		return err
	}
	names := map[string]string{"provision.name": "provision.workspaceName", "provision.code": "provision.workspaceCode"}
	out := map[string]string{}
	for k, v := range e.FieldErrors {
		if n, ok := names[k]; ok {
			k = n
		}
		out[k] = v
	}
	e.FieldErrors = out
	return e
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// nullIfSame avoids "Priya Priya" when a lead only had one name part.
func nullIfSame(first, last string) string {
	if first == last {
		return ""
	}
	return first
}

// ---- give login from a record (owner only) ----

// handleGiveLogin gives the person behind a lead, account or contact a login, using the
// record's name and email, and links the new identity back to the record.
func (h *Handler) handleGiveLogin(w http.ResponseWriter, r *http.Request) {
	spec, ws, err := h.scope(r, "read")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("record_not_found"))
		return
	}
	var in platform.GiveLoginInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var p platform.Person
	var existing *uuid.UUID
	nameSQL := spec.TitleSQL
	if spec.Key == "accounts" {
		nameSQL = "t.name"
	}
	phoneSQL := "COALESCE(t.phone, '')"
	if spec.Key != "accounts" {
		phoneSQL = "COALESCE(t.phone, t.mobile, '')"
	}
	err = h.store.Pool.QueryRow(r.Context(), `SELECT `+nameSQL+`, COALESCE(t.email, ''), `+phoneSQL+`, t.identity_id FROM crm.`+spec.Table+
		` t WHERE t.id = $1 AND t.workspace_id = $2 AND t.deleted_at IS NULL`, id, ws).Scan(&p.Name, &p.Email, &p.Phone, &existing)
	if errors.Is(err, pgx.ErrNoRows) {
		shared.WriteError(w, r, shared.NotFound("record_not_found"))
		return
	}
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if strings.TrimSpace(p.Email) == "" {
		shared.WriteError(w, r, shared.NewError(http.StatusUnprocessableEntity, "no_email", "Add an email to this record first — it's what they sign in with."))
		return
	}
	hash, err := platform.PrepareGiveLogin(&in, p, "")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var res *platform.GiveLoginResult
	var sent *platform.SentInvitation
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		var err error
		if res, sent, err = h.platform.GiveLoginTx(r.Context(), tx, actor(r), p, in, hash, ""); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `UPDATE crm.`+spec.Table+` SET identity_id = $2, version = version + 1, updated_at = now() WHERE id = $1`,
			id, res.IdentityID); err != nil {
			return err
		}
		if spec.Key == "accounts" {
			var isPlatform bool
			if err := tx.QueryRow(r.Context(), `SELECT is_platform FROM crm.workspaces WHERE id = $1`, res.WorkspaceID).Scan(&isPlatform); err != nil {
				return err
			}
			if !isPlatform {
				if _, err := tx.Exec(r.Context(), `UPDATE crm.accounts SET customer_workspace_id = COALESCE(customer_workspace_id, $2) WHERE id = $1`,
					id, res.WorkspaceID); err != nil {
					return err
				}
			}
		}
		return shared.WriteAudit(r.Context(), tx, audit(r, ws, "record.login_granted", strings.TrimSuffix(spec.Key, "s"), &id, nil,
			map[string]any{"identityId": res.IdentityID, "workspaceId": res.WorkspaceID, "method": in.Method}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if sent != nil {
		res.Invitation = h.platform.DeliverInvitation(r.Context(), sent)
	}
	shared.WriteJSON(w, http.StatusCreated, res)
}
