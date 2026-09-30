package identity

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"cardflow-backend/internal/crm/oauth"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Sign in with Google, Microsoft or LinkedIn (D-64). The provider must vouch for the
// email address, and that address must already belong to an account (people are still
// invited first); the provider account is then linked (crm.sso_links) so later
// sign-ins match on the provider's stable id even if the email changes.

// GET /auth/oauth/{provider}/start?product=<code>&audience=workspace|owner
func (s *Service) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "provider")
	p := oauth.Get(name, s.cfg.BaseURL)
	back := "/crm/login"
	audience := r.URL.Query().Get("audience")
	if audience == "owner" {
		back = "/crm/owner/login"
	}
	code := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("product")))
	if p == nil {
		http.Redirect(w, r, back+"?oauthError="+urlEscape(p0(name)+" sign-in isn't set up on this server yet."), http.StatusFound)
		return
	}
	if code != "" {
		if err := CheckMethod(r.Context(), code, name); err != nil {
			http.Redirect(w, r, back+"?product="+code+"&oauthError="+urlEscape(err.(*shared.Error).Message), http.StatusFound)
			return
		}
	}
	nonce, _ := shared.RandomToken(16)
	st := oauth.State{Purpose: "login", Provider: name, Workspace: code, Nonce: nonce, Return: back, Expires: time.Now().Add(10 * time.Minute).Unix()}
	if audience == "owner" {
		st.Identity = "owner"
	}
	http.SetCookie(w, &http.Cookie{Name: "crm_oauth", Value: nonce, Path: "/api/crm/v1/oauth", MaxAge: 600, HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, p.AuthURL(oauth.EncodeState(s.cfg.EncryptionKey, st), "login"), http.StatusFound)
}

func p0(name string) string {
	switch name {
	case "google":
		return "Google"
	case "microsoft":
		return "Microsoft"
	case "linkedin":
		return "LinkedIn"
	}
	return name
}

func urlEscape(s string) string { return url.QueryEscape(s) }

// CompleteOAuthLogin finishes sign-in and sets the session cookie; it returns where to go.
func (s *Service) CompleteOAuthLogin(w http.ResponseWriter, r *http.Request, st *oauth.State, code string) (string, error) {
	ctx := r.Context()
	p := oauth.Get(st.Provider, s.cfg.BaseURL)
	if p == nil {
		return "", errors.New("this sign-in isn't set up on the server")
	}
	tok, err := p.Exchange(ctx, code)
	if err != nil {
		return "", errors.New(p.Label + " didn't accept the sign-in. Try again.")
	}
	u, err := p.FetchUser(ctx, tok)
	if err != nil {
		return "", err
	}
	identityID, err := s.identityForProvider(ctx, st.Provider, u)
	if err != nil {
		return "", err
	}
	audience := "workspace"
	if st.Identity == "owner" {
		audience = "owner"
	}
	return s.SignInExternal(w, r, identityID, st.Provider, audience, st.Workspace)
}

// identityForProvider finds the account for a provider identity, linking it on first use.
func (s *Service) identityForProvider(ctx context.Context, provider string, u *oauth.UserInfo) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.store.Pool.QueryRow(ctx, `SELECT identity_id FROM crm.sso_links WHERE provider = $1 AND subject = $2`, provider, u.Subject).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, err
	}
	if !u.Verified() {
		return uuid.Nil, shared.Forbidden("email_not_verified", p0(provider)+" hasn't verified "+u.Email+", so it can't be used to sign in.")
	}
	id, _, err = s.accountFor(ctx, "email", u.Email)
	if err != nil {
		return uuid.Nil, shared.Forbidden("account_not_found", "No account uses "+u.Email+". Ask your administrator for an invitation first.")
	}
	if _, err := s.store.Pool.Exec(ctx, `INSERT INTO crm.sso_links (provider, subject, identity_id, email_at_link) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
		provider, u.Subject, id, u.Email); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// SignInExternal signs an identity in after an outside provider (OAuth, SAML) proved who
// they are, with the same checks as a password sign-in, and sets the cookie.
func (s *Service) SignInExternal(w http.ResponseWriter, r *http.Request, identityID uuid.UUID, method, audience, workspaceCode string) (string, error) {
	ctx := r.Context()
	var row credentialRow
	err := s.store.Pool.QueryRow(ctx, `SELECT i.id, i.display_name, i.is_platform_owner, i.status, COALESCE(pc.must_change, false)
		FROM crm.identities i LEFT JOIN crm.password_credentials pc ON pc.identity_id = i.id WHERE i.id = $1 AND i.status <> 'deleted'`, identityID).
		Scan(&row.identityID, &row.displayName, &row.isOwner, &row.status, &row.mustChange)
	if err != nil {
		return "", shared.Forbidden("account_not_found", "That account no longer exists.")
	}
	row.mustChange = false // external sign-in doesn't use the password
	out, err := s.finishSignIn(ctx, row, audience, workspaceCode, "external:"+identityID.String(), method, Meta(r))
	if err != nil {
		return "", err
	}
	s.setSessionCookie(w, out.token, out.expires)
	next := out.step.Next
	if workspaceCode != "" && next == "/crm/home" {
		next = "/crm/w/" + workspaceCode + "/home"
	}
	return next, nil
}
