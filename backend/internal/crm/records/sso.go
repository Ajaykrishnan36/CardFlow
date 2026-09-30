package records

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/shared"
	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Single sign-on with SAML 2.0 (D-64): a product connects its company identity provider
// (Okta, Microsoft Entra, Google Workspace…) by pasting the provider's metadata XML. This
// CRM is the service provider; its metadata, entity ID and ACS URL are shown to copy into
// the identity provider. People sign in with their company account; with "create
// accounts on first sign-in" on, new people from the listed email domains get an account
// with the chosen role.

type SSOSettings struct {
	Kind           string    `json:"kind"` // saml | oidc (D-82)
	Enabled        bool      `json:"enabled"`
	Name           string    `json:"name"`
	IDPMetadataXML string    `json:"idpMetadataXml,omitempty"`
	IDPEntityID    string    `json:"idpEntityId,omitempty"`
	Domains        []string  `json:"domains"`
	JIT            bool      `json:"jitProvisioning"`
	DefaultRoleKey string    `json:"defaultRoleKey"`
	SPEntityID     string    `json:"spEntityId"`
	SPACSURL       string    `json:"spAcsUrl"`
	SPMetadataURL  string    `json:"spMetadataUrl"`
	SignInURL      string    `json:"signInUrl"`
	Configured     bool      `json:"configured"`
	UpdatedAt      time.Time `json:"updatedAt,omitempty"`
	LoginMethodOn  bool      `json:"loginMethodOn"`
	OIDCIssuer     string    `json:"oidcIssuer"`
	OIDCClientID   string    `json:"oidcClientId"`
	OIDCSecretSet  bool      `json:"oidcSecretSet"`
	OIDCRedirect   string    `json:"oidcRedirectUrl"`
}

func spURLs(base, code string) (entity, acs, meta, start string) {
	b := strings.TrimRight(base, "/") + "/api/crm/v1/auth/saml/" + code
	return b + "/metadata", b + "/acs", b + "/metadata", b + "/start"
}

func newSPKey() (*rsa.PrivateKey, string, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, "", err
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "CRM SAML service provider"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(10, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, "", err
	}
	return key, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), nil
}

// SAMLProvider is a product's SAML set-up, ready to sign people in.
type SAMLProvider struct {
	SP          *saml.ServiceProvider
	WorkspaceID uuid.UUID
	Code        string
	Domains     []string
	JIT         bool
	RoleKey     string
}

// LoadSAML builds the service provider for a product (nil when SSO isn't set up).
func (h *Handler) LoadSAML(ctx context.Context, code string) (*SAMLProvider, error) {
	var p SAMLProvider
	var metaXML, certPEM string
	var keyEnc []byte
	var status string
	err := h.store.Pool.QueryRow(ctx, `SELECT w.id, w.code, s.idp_metadata_xml, s.sp_key_enc, COALESCE(s.sp_cert_pem, ''), s.domains, s.jit_provisioning, s.default_role_key, s.status
		FROM crm.sso_providers s JOIN crm.workspaces w ON w.id = s.workspace_id WHERE w.code = $1 AND w.status = 'active' AND s.kind = 'saml'`, code).
		Scan(&p.WorkspaceID, &p.Code, &metaXML, &keyEnc, &certPEM, &p.Domains, &p.JIT, &p.RoleKey, &status)
	if errors.Is(err, pgx.ErrNoRows) || status != "active" {
		return nil, shared.NotFound("sso_not_configured")
	}
	if err != nil {
		return nil, err
	}
	idp, err := samlsp.ParseMetadata([]byte(metaXML))
	if err != nil {
		return nil, errors.New("the identity provider's metadata can't be read")
	}
	rawKey, err := shared.Decrypt(h.cfg.EncryptionKey, keyEnc)
	if err != nil {
		return nil, err
	}
	key, err := x509.ParsePKCS1PrivateKey(rawKey)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return nil, errors.New("missing certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	entity, acs, meta, _ := spURLs(h.cfg.BaseURL, code)
	mu, _ := url.Parse(meta)
	au, _ := url.Parse(acs)
	p.SP = &saml.ServiceProvider{EntityID: entity, Key: key, Certificate: cert, MetadataURL: *mu, AcsURL: *au, IDPMetadata: idp, AllowIDPInitiated: true}
	return &p, nil
}

func (h *Handler) requireAccessAdmin(w http.ResponseWriter, r *http.Request) (*Scope, bool) {
	sc := scopeFrom(r.Context())
	if apiKeyFrom(r.Context()) != nil || (!sc.Owner && !sc.Eff.HasCapability(access.CapAccessManage)) {
		shared.WriteError(w, r, shared.Forbidden("forbidden", "You need the “Manage roles & permission sets” permission."))
		return nil, false
	}
	return sc, true
}

func (h *Handler) ssoSettings(ctx context.Context, sc *Scope) (*SSOSettings, error) {
	s := &SSOSettings{Kind: "saml", Domains: []string{}, DefaultRoleKey: "STAFF", Name: "Company sign-in"}
	s.SPEntityID, s.SPACSURL, s.SPMetadataURL, _ = spURLs(h.cfg.BaseURL, sc.Code)
	s.SignInURL = strings.TrimRight(h.cfg.BaseURL, "/") + "/api/crm/v1/auth/sso/" + sc.Code + "/start"
	s.OIDCRedirect = oidcRedirectURL(h.cfg.BaseURL, sc.Code)
	var status string
	err := h.store.Pool.QueryRow(ctx, `SELECT kind, name, status, idp_metadata_xml, domains, jit_provisioning, default_role_key, updated_at, oidc_issuer, oidc_client_id,
		oidc_secret_enc IS NOT NULL FROM crm.sso_providers WHERE workspace_id = $1`, sc.WS).
		Scan(&s.Kind, &s.Name, &status, &s.IDPMetadataXML, &s.Domains, &s.JIT, &s.DefaultRoleKey, &s.UpdatedAt, &s.OIDCIssuer, &s.OIDCClientID, &s.OIDCSecretSet)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		s.Configured, s.Enabled = true, status == "active"
		if idp, err := samlsp.ParseMetadata([]byte(s.IDPMetadataXML)); s.Kind == "saml" && err == nil {
			s.IDPEntityID = idp.EntityID
		}
	}
	list, _ := identityAllowed(ctx, sc.Code)
	s.LoginMethodOn = contains(list, "sso")
	return s, nil
}

func identityAllowed(ctx context.Context, code string) ([]string, error) {
	return allowedMethodsHook(ctx, code), nil
}

// allowedMethodsHook is identity.AllowedMethods (kept as a variable to avoid an import cycle in tests).
var allowedMethodsHook = func(ctx context.Context, code string) []string { return nil }

// SetAllowedMethodsHook wires identity's sign-in policy into the SSO page.
func SetAllowedMethodsHook(f func(ctx context.Context, code string) []string) { allowedMethodsHook = f }

func (h *Handler) handleGetSSO(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireAccessAdmin(w, r)
	if !ok {
		return
	}
	s, err := h.ssoSettings(r.Context(), sc)
	respond(w, r, http.StatusOK, s, err)
}

func (h *Handler) handleSaveSSO(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireAccessAdmin(w, r)
	if !ok {
		return
	}
	var in struct {
		Kind           string   `json:"kind"`
		OIDCIssuer     string   `json:"oidcIssuer"`
		OIDCClientID   string   `json:"oidcClientId"`
		OIDCSecret     string   `json:"oidcClientSecret"`
		Enabled        bool     `json:"enabled"`
		Name           string   `json:"name"`
		IDPMetadataXML string   `json:"idpMetadataXml"`
		IDPMetadataURL string   `json:"idpMetadataUrl"`
		Domains        []string `json:"domains"`
		JIT            bool     `json:"jitProvisioning"`
		DefaultRoleKey string   `json:"defaultRoleKey"`
	}
	if err := shared.DecodeJSONLimit(w, r, &in, 1<<20); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	fe := map[string]string{}
	if in.Kind != "oidc" {
		in.Kind = "saml"
	}
	var secretEnc []byte
	var hadSecret bool
	_ = h.store.Pool.QueryRow(r.Context(), `SELECT oidc_secret_enc IS NOT NULL FROM crm.sso_providers WHERE workspace_id = $1 AND kind = 'oidc'`, sc.WS).Scan(&hadSecret)
	if in.Kind == "oidc" {
		in.OIDCIssuer = strings.TrimRight(strings.TrimSpace(in.OIDCIssuer), "/")
		in.OIDCClientID = strings.TrimSpace(in.OIDCClientID)
		if _, msg := validOutboundURL(in.OIDCIssuer, h.cfg.AppEnv == "local"); msg != "" {
			fe["oidcIssuer"] = msg
		} else if _, err := h.discoverOIDC(r.Context(), in.OIDCIssuer); err != nil {
			fe["oidcIssuer"] = "Couldn't read " + in.OIDCIssuer + "/.well-known/openid-configuration — check the issuer URL."
		}
		if in.OIDCClientID == "" || len(in.OIDCClientID) > 500 {
			fe["oidcClientId"] = "Paste the client ID from your identity provider."
		}
		if s := strings.TrimSpace(in.OIDCSecret); s != "" {
			enc, err := shared.Encrypt(h.cfg.EncryptionKey, []byte(s))
			if err != nil {
				shared.WriteError(w, r, err)
				return
			}
			secretEnc = enc
		} else if !hadSecret {
			fe["oidcClientSecret"] = "Paste the client secret from your identity provider."
		}
		in.IDPMetadataXML, in.IDPMetadataURL = "", ""
	}
	if in.Kind == "saml" && in.IDPMetadataURL != "" && strings.TrimSpace(in.IDPMetadataXML) == "" {
		u, msg := validOutboundURL(in.IDPMetadataURL, h.cfg.AppEnv == "local")
		if msg != "" {
			fe["idpMetadataUrl"] = msg
		} else {
			res, err := h.outboundClient(10 * time.Second).Get(u)
			if err != nil {
				fe["idpMetadataUrl"] = "Couldn't download the metadata from that URL."
			} else {
				defer res.Body.Close()
				buf := make([]byte, 0, 64<<10)
				tmp := make([]byte, 32<<10)
				for len(buf) < 512<<10 {
					n, err := res.Body.Read(tmp)
					buf = append(buf, tmp[:n]...)
					if err != nil {
						break
					}
				}
				in.IDPMetadataXML = string(buf)
			}
		}
	}
	if in.Kind == "oidc" {
		// checked above
	} else if strings.TrimSpace(in.IDPMetadataXML) == "" {
		fe["idpMetadataXml"] = "Paste your identity provider's metadata XML (or its metadata URL)."
	} else if _, err := samlsp.ParseMetadata([]byte(in.IDPMetadataXML)); err != nil {
		fe["idpMetadataXml"] = "That isn't valid SAML metadata."
	}
	domains := []string{}
	for _, d := range in.Domains {
		d = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(d), "@"))
		if d != "" && strings.Contains(d, ".") && !contains(domains, d) {
			domains = append(domains, d)
		}
	}
	if in.JIT && len(domains) == 0 {
		fe["domains"] = "List the email domains that may get an account on first sign-in (e.g. acme.com)."
	}
	if in.DefaultRoleKey == "" || in.DefaultRoleKey == "SUPER_ADMIN" {
		in.DefaultRoleKey = "STAFF"
	}
	if strings.TrimSpace(in.Name) == "" {
		in.Name = "Company sign-in"
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	status := "disabled"
	if in.Enabled {
		status = "active"
	}
	err := h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM crm.sso_providers WHERE workspace_id = $1)`, sc.WS).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			key, certPEM, err := newSPKey()
			if err != nil {
				return err
			}
			enc, err := shared.Encrypt(h.cfg.EncryptionKey, x509.MarshalPKCS1PrivateKey(key))
			if err != nil {
				return err
			}
			if _, err := tx.Exec(r.Context(), `INSERT INTO crm.sso_providers (workspace_id, name, status, idp_metadata_xml, domains, jit_provisioning, default_role_key, sp_key_enc, sp_cert_pem, created_by,
				kind, oidc_issuer, oidc_client_id, oidc_secret_enc)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`, sc.WS, in.Name, status, in.IDPMetadataXML, domains, in.JIT, in.DefaultRoleKey, enc, certPEM, actor(r),
				in.Kind, in.OIDCIssuer, in.OIDCClientID, secretEnc); err != nil {
				return err
			}
		} else if _, err := tx.Exec(r.Context(), `UPDATE crm.sso_providers SET name = $2, status = $3, idp_metadata_xml = CASE WHEN $8 = 'oidc' THEN idp_metadata_xml ELSE $4 END,
			domains = $5, jit_provisioning = $6, default_role_key = $7, kind = $8,
			oidc_issuer = CASE WHEN $8 = 'oidc' THEN $9 ELSE oidc_issuer END, oidc_client_id = CASE WHEN $8 = 'oidc' THEN $10 ELSE oidc_client_id END,
			oidc_secret_enc = COALESCE($11, oidc_secret_enc), updated_at = now()
			WHERE workspace_id = $1`, sc.WS, in.Name, status, in.IDPMetadataXML, domains, in.JIT, in.DefaultRoleKey, in.Kind, in.OIDCIssuer, in.OIDCClientID, secretEnc); err != nil {
			return err
		}
		return shared.WriteAudit(r.Context(), tx, actorFromRequest(r, "ui").audit(sc.WS, "sso.saved", "sso", nil, nil, map[string]any{"kind": in.Kind, "enabled": in.Enabled, "domains": domains, "jit": in.JIT}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	s, err := h.ssoSettings(r.Context(), sc)
	respond(w, r, http.StatusOK, s, err)
}

func (h *Handler) handleDeleteSSO(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireAccessAdmin(w, r)
	if !ok {
		return
	}
	if _, err := h.store.Pool.Exec(r.Context(), `DELETE FROM crm.sso_providers WHERE workspace_id = $1`, sc.WS); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
