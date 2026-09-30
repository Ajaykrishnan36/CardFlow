package crm

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"cardflow-backend/internal/crm/records"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
)

// OpenID Connect sign-in for a product (D-82): /auth/sso/<code>/start sends the person to
// the provider, which returns to /auth/oidc/<code>/callback. State, nonce and the PKCE
// verifier travel in a short-lived encrypted cookie.

const oidcCookie = "crm_oidc"

type oidcFlow struct {
	Code     string `json:"c"`
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	Expires  int64  `json:"e"`
}

func (m *Module) oidcStart(w http.ResponseWriter, r *http.Request, code string) {
	p, err := m.records.LoadOIDC(r.Context(), code)
	if err != nil {
		samlFail(w, r, code, "Single sign-on isn't set up for this product yet.")
		return
	}
	state, _ := shared.RandomToken(16)
	nonce, _ := shared.RandomToken(16)
	verifier, challenge := records.OIDCPKCE()
	raw, _ := json.Marshal(oidcFlow{Code: code, State: state, Nonce: nonce, Verifier: verifier, Expires: time.Now().Add(10 * time.Minute).Unix()})
	sealed, err := shared.Encrypt(m.cfg.EncryptionKey, raw)
	if err != nil {
		samlFail(w, r, code, "Couldn't start single sign-on.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: oidcCookie, Value: base64.RawURLEncoding.EncodeToString(sealed), Path: "/api/crm/v1/auth/oidc", MaxAge: 600,
		HttpOnly: true, Secure: m.cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, p.AuthURL(state, nonce, challenge), http.StatusFound)
}

func (m *Module) oidcCallback(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	http.SetCookie(w, &http.Cookie{Name: oidcCookie, Value: "", Path: "/api/crm/v1/auth/oidc", MaxAge: -1, HttpOnly: true, Secure: m.cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		samlFail(w, r, code, "Your identity provider stopped the sign-in ("+e+").")
		return
	}
	var flow oidcFlow
	c, err := r.Cookie(oidcCookie)
	if err == nil {
		var sealed, raw []byte
		if sealed, err = base64.RawURLEncoding.DecodeString(c.Value); err == nil {
			if raw, err = shared.Decrypt(m.cfg.EncryptionKey, sealed); err == nil {
				err = json.Unmarshal(raw, &flow)
			}
		}
	}
	if err != nil || flow.Code != code || flow.State == "" || flow.State != q.Get("state") || time.Now().Unix() > flow.Expires {
		samlFail(w, r, code, "The sign-in expired or was started in another browser. Try again.")
		return
	}
	p, err := m.records.LoadOIDC(r.Context(), code)
	if err != nil {
		samlFail(w, r, code, "Single sign-on isn't set up for this product.")
		return
	}
	claims, err := m.records.OIDCExchange(r.Context(), p, q.Get("code"), flow.Verifier, flow.Nonce)
	if err != nil {
		samlFail(w, r, code, errText(err))
		return
	}
	identityID, err := m.ssoIdentity(r.Context(), "oidc:"+code, p.WorkspaceID, claims.Subject, claims.Email, claims.Name, p.JIT, p.Domains, p.RoleKey)
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
