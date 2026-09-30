package crm

import (
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/shared"
	"github.com/crewjam/saml"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SAML sign-in for a product (D-64): /auth/saml/<code>/metadata (for the identity
// provider), /start (redirects there) and /acs (where the provider posts back).

func (m *Module) samlRoutes(r chi.Router) {
	r.Get("/auth/saml/{code}/metadata", m.samlMetadata)
	r.Get("/auth/saml/{code}/start", m.samlStart)
	r.Post("/auth/saml/{code}/acs", m.samlACS)
}

func (m *Module) samlMetadata(w http.ResponseWriter, r *http.Request) {
	p, err := m.records.LoadSAML(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	raw, _ := xml.MarshalIndent(p.SP.Metadata(), "", "  ")
	w.Header().Set("Content-Type", "application/samlmetadata+xml")
	_, _ = w.Write(raw)
}

func samlFail(w http.ResponseWriter, r *http.Request, code, msg string) {
	http.Redirect(w, r, "/crm/login?product="+url.QueryEscape(code)+"&oauthError="+url.QueryEscape(msg), http.StatusFound)
}

func (m *Module) samlStart(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	if err := identity.CheckMethod(r.Context(), code, "sso"); err != nil {
		samlFail(w, r, code, "Single sign-on isn't turned on for this product.")
		return
	}
	p, err := m.records.LoadSAML(r.Context(), code)
	if err != nil {
		samlFail(w, r, code, "Single sign-on isn't set up for this product yet.")
		return
	}
	req, err := p.SP.MakeAuthenticationRequest(p.SP.GetSSOBindingLocation(saml.HTTPRedirectBinding), saml.HTTPRedirectBinding, saml.HTTPPostBinding)
	if err != nil {
		samlFail(w, r, code, "Your identity provider's settings don't allow a sign-in request.")
		return
	}
	u, err := req.Redirect(code, p.SP)
	if err != nil {
		samlFail(w, r, code, "Couldn't start single sign-on.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "crm_saml", Value: req.ID, Path: "/api/crm/v1/auth/saml", MaxAge: 600, HttpOnly: true, Secure: m.cfg.CookieSecure,
		SameSite: http.SameSiteNoneMode})
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func assertionEmail(a *saml.Assertion) (string, string) {
	email, name := "", ""
	for _, st := range a.AttributeStatements {
		for _, at := range st.Attributes {
			n := strings.ToLower(at.Name + " " + at.FriendlyName)
			if len(at.Values) == 0 {
				continue
			}
			switch {
			case email == "" && (strings.Contains(n, "emailaddress") || strings.Contains(n, "email") || strings.HasSuffix(n, "mail")):
				email = at.Values[0].Value
			case name == "" && (strings.Contains(n, "displayname") || strings.HasSuffix(n, "/name") || strings.Contains(n, "fullname")):
				name = at.Values[0].Value
			}
		}
	}
	if email == "" && a.Subject != nil && a.Subject.NameID != nil && strings.Contains(a.Subject.NameID.Value, "@") {
		email = a.Subject.NameID.Value
	}
	return strings.ToLower(strings.TrimSpace(email)), strings.TrimSpace(name)
}

func (m *Module) samlACS(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	p, err := m.records.LoadSAML(r.Context(), code)
	if err != nil {
		samlFail(w, r, code, "Single sign-on isn't set up for this product.")
		return
	}
	var ids []string
	if c, err := r.Cookie("crm_saml"); err == nil {
		ids = append(ids, c.Value)
	}
	if err := r.ParseForm(); err != nil {
		samlFail(w, r, code, "The sign-in response couldn't be read.")
		return
	}
	a, err := p.SP.ParseResponse(r, ids)
	if err != nil {
		samlFail(w, r, code, "Your identity provider's answer couldn't be verified. Check the SAML settings.")
		return
	}
	email, name := assertionEmail(a)
	if email == "" {
		samlFail(w, r, code, "Your identity provider didn't send an email address.")
		return
	}
	subject := email
	if a.Subject != nil && a.Subject.NameID != nil && a.Subject.NameID.Value != "" {
		subject = a.Subject.NameID.Value
	}
	identityID, err := m.samlIdentity(r.Context(), p.WorkspaceID, code, subject, email, name, p.JIT, p.Domains, p.RoleKey)
	if err != nil {
		samlFail(w, r, code, errText(err))
		return
	}
	next, err := m.identity.SignInExternal(w, r, identityID, "sso", "workspace", code)
	if err != nil {
		samlFail(w, r, code, errText(err))
		return
	}
	http.Redirect(w, r, next, http.StatusFound)
}

// samlIdentity finds (or, with just-in-time provisioning, creates) the person.
func (m *Module) samlIdentity(ctx context.Context, ws uuid.UUID, code, subject, email, name string, jit bool, domains []string, roleKey string) (uuid.UUID, error) {
	provider := "saml:" + code
	var id uuid.UUID
	err := m.platform.Store().Pool.QueryRow(ctx, `SELECT identity_id FROM crm.sso_links WHERE provider = $1 AND subject = $2`, provider, subject).Scan(&id)
	if err == nil {
		return id, nil
	}
	pool := m.platform.Store().Pool
	err = pool.QueryRow(ctx, `SELECT i.id FROM crm.verified_identifiers v JOIN crm.identities i ON i.id = v.identity_id
		WHERE v.kind = 'email' AND v.namespace = 'global' AND v.value_normalized = $1 AND i.status = 'active'`, email).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		domain := email[strings.LastIndex(email, "@")+1:]
		allowed := false
		for _, d := range domains {
			allowed = allowed || d == domain
		}
		if !jit || !allowed {
			return uuid.Nil, shared.Forbidden("account_not_found", "No account uses "+email+". Ask your administrator for an invitation.")
		}
		if name == "" {
			name = email[:strings.Index(email, "@")]
		}
		err = m.platform.Store().WithTx(ctx, func(tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `INSERT INTO crm.identities (display_name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO crm.verified_identifiers (identity_id, kind, value_normalized, namespace, verified_at) VALUES ($1, 'email', $2, 'global', now())`, id, email); err != nil {
				return err
			}
			var mid, roleID, owner uuid.UUID
			if err := tx.QueryRow(ctx, `INSERT INTO crm.memberships (workspace_id, identity_id, status) VALUES ($1, $2, 'active') RETURNING id`, ws, id).Scan(&mid); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `SELECT id FROM crm.roles WHERE workspace_id = $1 AND key = $2`, ws, roleKey).Scan(&roleID); err != nil {
				return errors.New("the role for new people doesn't exist in this product")
			}
			_ = tx.QueryRow(ctx, `SELECT id FROM crm.identities WHERE is_platform_owner ORDER BY created_at LIMIT 1`).Scan(&owner)
			if _, err := tx.Exec(ctx, `INSERT INTO crm.role_assignments (workspace_id, membership_id, role_id, product_ids, granted_by) VALUES ($1, $2, $3, '{}', $4)`,
				ws, mid, roleID, owner); err != nil {
				return err
			}
			return shared.WriteAudit(ctx, tx, shared.AuditEvent{WorkspaceID: &ws, ActorKind: "system", Action: "sso.user_provisioned", EntityType: "identity", EntityID: &id,
				After: map[string]any{"email": email, "role": roleKey}})
		})
		if err != nil {
			return uuid.Nil, err
		}
	} else if err != nil {
		return uuid.Nil, err
	}
	_, _ = pool.Exec(ctx, `INSERT INTO crm.sso_links (provider, subject, identity_id, email_at_link) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`, provider, subject, id, email)
	return id, nil
}
