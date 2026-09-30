package records

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// OpenID Connect sign-in for a product (D-82), next to SAML (D-64): the product enters
// its provider's issuer URL, client ID and secret (Okta, Entra ID, Google Workspace,
// Auth0, Keycloak…). Sign-in is the authorization-code flow with PKCE, state and nonce;
// the ID token's signature is checked against the provider's published keys.

type OIDCProvider struct {
	WorkspaceID  uuid.UUID
	Code         string
	Issuer       string
	ClientID     string
	ClientSecret string
	Domains      []string
	JIT          bool
	RoleKey      string
	RedirectURL  string
	Discovery    *oidcDiscovery
}

type oidcDiscovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
	fetched               time.Time
	keys                  map[string]any
	keysAt                time.Time
}

var (
	oidcCacheMu sync.Mutex
	oidcCache   = map[string]*oidcDiscovery{}
)

func oidcRedirectURL(base, code string) string {
	return strings.TrimRight(base, "/") + "/api/crm/v1/auth/oidc/" + code + "/callback"
}

func (h *Handler) getJSON(ctx context.Context, u string, out any) error {
	safe, msg := validOutboundURL(u, h.cfg.AppEnv == "local")
	if msg != "" {
		return errors.New(msg)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, safe, nil)
	req.Header.Set("Accept", "application/json")
	res, err := h.outboundClient(10 * time.Second).Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("%s answered %s", u, res.Status)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
}

// discoverOIDC reads (and caches for an hour) the provider's /.well-known/openid-configuration.
func (h *Handler) discoverOIDC(ctx context.Context, issuer string) (*oidcDiscovery, error) {
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	oidcCacheMu.Lock()
	d := oidcCache[issuer]
	oidcCacheMu.Unlock()
	if d != nil && time.Since(d.fetched) < time.Hour {
		return d, nil
	}
	d = &oidcDiscovery{}
	if err := h.getJSON(ctx, issuer+"/.well-known/openid-configuration", d); err != nil {
		return nil, errors.New("couldn't read the provider's OpenID configuration")
	}
	if strings.TrimRight(d.Issuer, "/") != issuer || d.AuthorizationEndpoint == "" || d.TokenEndpoint == "" || d.JWKSURI == "" {
		return nil, errors.New("the provider's OpenID configuration doesn't match this issuer")
	}
	d.fetched = time.Now()
	oidcCacheMu.Lock()
	oidcCache[issuer] = d
	oidcCacheMu.Unlock()
	return d, nil
}

// LoadOIDC returns a product's OpenID Connect set-up (not found when it has none).
func (h *Handler) LoadOIDC(ctx context.Context, code string) (*OIDCProvider, error) {
	p := OIDCProvider{}
	var status string
	var secretEnc []byte
	err := h.store.Pool.QueryRow(ctx, `SELECT w.id, w.code, s.oidc_issuer, s.oidc_client_id, s.oidc_secret_enc, s.domains, s.jit_provisioning, s.default_role_key, s.status
		FROM crm.sso_providers s JOIN crm.workspaces w ON w.id = s.workspace_id WHERE w.code = $1 AND w.status = 'active' AND s.kind = 'oidc'`, code).
		Scan(&p.WorkspaceID, &p.Code, &p.Issuer, &p.ClientID, &secretEnc, &p.Domains, &p.JIT, &p.RoleKey, &status)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && status != "active") {
		return nil, shared.NotFound("sso_not_configured")
	}
	if err != nil {
		return nil, err
	}
	if len(secretEnc) > 0 {
		raw, err := shared.Decrypt(h.cfg.EncryptionKey, secretEnc)
		if err != nil {
			return nil, err
		}
		p.ClientSecret = string(raw)
	}
	p.RedirectURL = oidcRedirectURL(h.cfg.BaseURL, code)
	if p.Discovery, err = h.discoverOIDC(ctx, p.Issuer); err != nil {
		return nil, err
	}
	return &p, nil
}

// SSOKind is "saml", "oidc" or "" for a product.
func (h *Handler) SSOKind(ctx context.Context, code string) string {
	var kind string
	_ = h.store.Pool.QueryRow(ctx, `SELECT s.kind FROM crm.sso_providers s JOIN crm.workspaces w ON w.id = s.workspace_id WHERE w.code = $1`, code).Scan(&kind)
	return kind
}

// OIDCPKCE returns a code verifier and its S256 challenge.
func OIDCPKCE() (string, string) {
	v, _ := shared.RandomToken(32)
	sum := sha256.Sum256([]byte(v))
	return v, base64.RawURLEncoding.EncodeToString(sum[:])
}

// AuthURL is where the person signs in at the provider.
func (p *OIDCProvider) AuthURL(state, nonce, challenge string) string {
	q := url.Values{"response_type": {"code"}, "client_id": {p.ClientID}, "redirect_uri": {p.RedirectURL}, "scope": {"openid email profile"},
		"state": {state}, "nonce": {nonce}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}}
	sep := "?"
	if strings.Contains(p.Discovery.AuthorizationEndpoint, "?") {
		sep = "&"
	}
	return p.Discovery.AuthorizationEndpoint + sep + q.Encode()
}

// OIDCClaims are what the CRM uses from the ID token.
type OIDCClaims struct {
	Subject, Email, Name string
}

// Exchange trades the code for tokens and verifies the ID token.
func (h *Handler) OIDCExchange(ctx context.Context, p *OIDCProvider, code, verifier, nonce string) (*OIDCClaims, error) {
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {p.RedirectURL}, "client_id": {p.ClientID}, "code_verifier": {verifier}}
	if p.ClientSecret != "" {
		form.Set("client_secret", p.ClientSecret)
	}
	safe, msg := validOutboundURL(p.Discovery.TokenEndpoint, h.cfg.AppEnv == "local")
	if msg != "" {
		return nil, errors.New(msg)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, safe, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := h.outboundClient(10 * time.Second).Do(req)
	if err != nil {
		return nil, errors.New("couldn't reach your identity provider")
	}
	defer res.Body.Close()
	var tok struct {
		IDToken string `json:"id_token"`
		Error   string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&tok)
	if res.StatusCode >= 300 || tok.IDToken == "" {
		return nil, fmt.Errorf("your identity provider refused the sign-in (%s)", strings.TrimSpace(tok.Error+" "+res.Status))
	}
	return h.verifyIDToken(ctx, p, tok.IDToken, nonce)
}

func (h *Handler) verifyIDToken(ctx context.Context, p *OIDCProvider, raw, nonce string) (*OIDCClaims, error) {
	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		return h.oidcKey(ctx, p.Discovery, kid)
	}, jwt.WithValidMethods([]string{"RS256", "RS384", "RS512", "PS256", "ES256", "ES384"}), jwt.WithIssuer(p.Discovery.Issuer),
		jwt.WithAudience(p.ClientID), jwt.WithExpirationRequired(), jwt.WithLeeway(time.Minute))
	if err != nil {
		return nil, errors.New("your identity provider's sign-in couldn't be verified")
	}
	if n, _ := claims["nonce"].(string); n == "" || n != nonce {
		return nil, errors.New("the sign-in expired; try again")
	}
	out := &OIDCClaims{}
	out.Subject, _ = claims["sub"].(string)
	out.Email, _ = claims["email"].(string)
	out.Name, _ = claims["name"].(string)
	if out.Email == "" {
		out.Email, _ = claims["preferred_username"].(string) // Entra ID without the email claim
	}
	if v, ok := claims["email_verified"].(bool); ok && !v {
		return nil, errors.New("your identity provider hasn't verified that email address")
	}
	out.Email = strings.ToLower(strings.TrimSpace(out.Email))
	if out.Subject == "" || !strings.Contains(out.Email, "@") {
		return nil, errors.New("your identity provider didn't send an email address")
	}
	return out, nil
}

// oidcKey finds the signing key, refreshing the provider's key set when the id is new.
func (h *Handler) oidcKey(ctx context.Context, d *oidcDiscovery, kid string) (any, error) {
	oidcCacheMu.Lock()
	k, ok := d.keys[kid]
	stale := time.Since(d.keysAt) > 10*time.Minute
	oidcCacheMu.Unlock()
	if ok {
		return k, nil
	}
	if !stale && d.keys != nil {
		return nil, errors.New("unknown signing key")
	}
	var set struct {
		Keys []struct {
			Kty, Kid, Use, N, E, Crv, X, Y string
		} `json:"keys"`
	}
	if err := h.getJSON(ctx, d.JWKSURI, &set); err != nil {
		return nil, err
	}
	keys := map[string]any{}
	for _, j := range set.Keys {
		if j.Use != "" && j.Use != "sig" {
			continue
		}
		switch j.Kty {
		case "RSA":
			n, err1 := base64.RawURLEncoding.DecodeString(j.N)
			e, err2 := base64.RawURLEncoding.DecodeString(j.E)
			if err1 == nil && err2 == nil {
				keys[j.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
			}
		case "EC":
			var curve elliptic.Curve
			switch j.Crv {
			case "P-256":
				curve = elliptic.P256()
			case "P-384":
				curve = elliptic.P384()
			default:
				continue
			}
			x, err1 := base64.RawURLEncoding.DecodeString(j.X)
			y, err2 := base64.RawURLEncoding.DecodeString(j.Y)
			if err1 == nil && err2 == nil {
				keys[j.Kid] = &ecdsa.PublicKey{Curve: curve, X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
			}
		}
	}
	oidcCacheMu.Lock()
	d.keys, d.keysAt = keys, time.Now()
	oidcCacheMu.Unlock()
	if k, ok := keys[kid]; ok {
		return k, nil
	}
	if kid == "" && len(keys) == 1 {
		for _, k := range keys {
			return k, nil
		}
	}
	return nil, errors.New("unknown signing key")
}
