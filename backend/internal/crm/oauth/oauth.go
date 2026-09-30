// Package oauth is sign-in with Google, Microsoft and LinkedIn (D-64), used both to log
// in (when a product's setup allows it) and to connect a Gmail / Outlook mailbox and
// calendar. Client IDs come from CRM_GOOGLE_CLIENT_ID / _SECRET, CRM_MICROSOFT_CLIENT_ID
// / _SECRET (+ CRM_MICROSOFT_TENANT, default "common") and CRM_LINKEDIN_CLIENT_ID /
// _SECRET; a provider without them is simply not offered. The redirect URI to register
// with each provider is <CRM_BASE_URL>/api/crm/v1/oauth/<provider>/callback.
package oauth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

type Provider struct {
	Name     string // google | microsoft | linkedin
	Label    string
	Config   oauth2.Config
	UserInfo string
}

// Scopes for signing in only.
var loginScopes = map[string][]string{
	"google":    {"openid", "email", "profile"},
	"microsoft": {"openid", "email", "profile", "User.Read"},
	"linkedin":  {"openid", "email", "profile"},
}

// Scopes for connecting a mailbox and calendar.
var mailboxScopes = map[string][]string{
	"google": {"openid", "email", "profile", "https://www.googleapis.com/auth/gmail.readonly", "https://www.googleapis.com/auth/gmail.send",
		"https://www.googleapis.com/auth/calendar.events"},
	"microsoft": {"openid", "email", "profile", "offline_access", "User.Read", "Mail.Read", "Mail.Send", "Calendars.ReadWrite"},
}

// Get returns a configured provider, or nil when its client ID isn't set.
func Get(name, baseURL string) *Provider {
	env := strings.ToUpper(name)
	id, secret := os.Getenv("CRM_"+env+"_CLIENT_ID"), os.Getenv("CRM_"+env+"_CLIENT_SECRET")
	if id == "" || secret == "" {
		return nil
	}
	redirect := strings.TrimRight(baseURL, "/") + "/api/crm/v1/oauth/" + name + "/callback"
	p := &Provider{Name: name, Config: oauth2.Config{ClientID: id, ClientSecret: secret, RedirectURL: redirect}}
	switch name {
	case "google":
		p.Label = "Google"
		p.Config.Endpoint = oauth2.Endpoint{AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token"}
		p.UserInfo = "https://openidconnect.googleapis.com/v1/userinfo"
	case "microsoft":
		tenant := os.Getenv("CRM_MICROSOFT_TENANT")
		if tenant == "" {
			tenant = "common"
		}
		p.Label = "Microsoft"
		p.Config.Endpoint = oauth2.Endpoint{AuthURL: "https://login.microsoftonline.com/" + tenant + "/oauth2/v2.0/authorize",
			TokenURL: "https://login.microsoftonline.com/" + tenant + "/oauth2/v2.0/token"}
		p.UserInfo = "https://graph.microsoft.com/oidc/userinfo"
	case "linkedin":
		p.Label = "LinkedIn"
		p.Config.Endpoint = oauth2.Endpoint{AuthURL: "https://www.linkedin.com/oauth/v2/authorization", TokenURL: "https://www.linkedin.com/oauth/v2/accessToken",
			AuthStyle: oauth2.AuthStyleInParams}
		p.UserInfo = "https://api.linkedin.com/v2/userinfo"
	default:
		return nil
	}
	return p
}

// Configured lists the providers that can be used.
func Configured(baseURL string) map[string]bool {
	out := map[string]bool{}
	for _, n := range []string{"google", "microsoft", "linkedin"} {
		out[n] = Get(n, baseURL) != nil
	}
	return out
}

// AuthURL starts the flow. purpose is "login" or "mailbox".
func (p *Provider) AuthURL(state, purpose string) string {
	scopes := loginScopes[p.Name]
	opts := []oauth2.AuthCodeOption{}
	if purpose == "mailbox" {
		scopes = mailboxScopes[p.Name]
		if p.Name == "google" {
			opts = append(opts, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"))
		}
	} else {
		opts = append(opts, oauth2.SetAuthURLParam("prompt", "select_account"))
	}
	c := p.Config
	c.Scopes = scopes
	return c.AuthCodeURL(state, opts...)
}

type UserInfo struct {
	Subject       string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified any    `json:"email_verified"`
	Name          string `json:"name"`
}

// Verified reports whether the provider vouches for the email address.
func (u UserInfo) Verified() bool {
	switch v := u.EmailVerified.(type) {
	case bool:
		return v
	case string:
		return v == "true"
	}
	return false
}

func (p *Provider) Exchange(ctx context.Context, code string) (*oauth2.Token, error) {
	return p.Config.Exchange(ctx, code)
}

// Client is an HTTP client that refreshes the token as needed.
func (p *Provider) Client(ctx context.Context, tok *oauth2.Token) *http.Client {
	return p.Config.Client(ctx, tok)
}

func (p *Provider) TokenSource(ctx context.Context, tok *oauth2.Token) oauth2.TokenSource {
	return p.Config.TokenSource(ctx, tok)
}

func (p *Provider) FetchUser(ctx context.Context, tok *oauth2.Token) (*UserInfo, error) {
	c := p.Config.Client(ctx, tok)
	c.Timeout = 10 * time.Second
	res, err := c.Get(p.UserInfo)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s user info: %s", p.Label, res.Status)
	}
	var u UserInfo
	if err := json.NewDecoder(res.Body).Decode(&u); err != nil {
		return nil, err
	}
	// Microsoft accounts: work accounts are verified by the tenant.
	if p.Name == "microsoft" && u.EmailVerified == nil && u.Email != "" {
		u.EmailVerified = true
	}
	u.Email = strings.ToLower(strings.TrimSpace(u.Email))
	if u.Email == "" || u.Subject == "" {
		return nil, errors.New("the provider didn't share an email address")
	}
	return &u, nil
}

// ---- signed state ----

// State travels through the provider and back; it's signed so it can't be forged.
type State struct {
	Purpose   string `json:"p"`           // login | mailbox
	Provider  string `json:"v"`           // google | microsoft | linkedin
	Workspace string `json:"w,omitempty"` // product code (login into a product; mailbox)
	Identity  string `json:"i,omitempty"` // mailbox: who connects it
	Nonce     string `json:"n"`           // also in a cookie, so the flow must finish in the same browser
	Return    string `json:"r,omitempty"` // path to go back to
	Expires   int64  `json:"e"`
}

func sign(key []byte, data []byte) string {
	m := hmac.New(sha256.New, key)
	m.Write(data)
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func EncodeState(key []byte, s State) string {
	raw, _ := json.Marshal(s)
	body := base64.RawURLEncoding.EncodeToString(raw)
	return body + "." + sign(key, []byte(body))
}

func DecodeState(key []byte, v string) (*State, error) {
	body, sig, ok := strings.Cut(v, ".")
	if !ok || !hmac.Equal([]byte(sig), []byte(sign(key, []byte(body)))) {
		return nil, errors.New("invalid state")
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	if time.Now().Unix() > s.Expires {
		return nil, errors.New("expired")
	}
	return &s, nil
}
