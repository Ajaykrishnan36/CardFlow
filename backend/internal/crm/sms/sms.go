// Package sms sends text messages for sign-in codes. The provider is chosen by
// configuration, so the rest of the CRM never knows which company delivers the message.
package sms

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Sender delivers one sign-in code to one phone number (E.164).
type Sender interface {
	// SendCode delivers the code. Implementations must not log it.
	SendCode(ctx context.Context, phone, code string) error
	// Mode names the provider for status pages: "msg91", "twilio", "preview" or "none".
	Mode() string
}

// ErrUnavailable means no provider is configured, so a code can't be delivered.
var ErrUnavailable = errors.New("sms: no provider configured")

// Preview is implemented by the development sender: the caller shows the code to the
// person instead of sending it. It is never selected unless configured explicitly.
type Preview interface{ IsPreview() bool }

// IsPreview reports whether codes are shown on screen instead of being sent.
func IsPreview(s Sender) bool {
	p, ok := s.(Preview)
	return ok && p.IsPreview()
}

// Config is read from the environment (names only; values live in the host's settings).
type Config struct {
	Provider   string // SMS_PROVIDER: msg91 | twilio | preview | "" (none)
	AuthKey    string // SMS_AUTH_KEY (MSG91 auth key)
	SenderID   string // SMS_SENDER_ID
	TemplateID string // SMS_OTP_TEMPLATE_ID (MSG91 DLT template / flow id)
	TwilioSID  string // TWILIO_ACCOUNT_SID
	TwilioAuth string // TWILIO_AUTH_TOKEN
	TwilioFrom string // TWILIO_FROM (number or messaging service SID)
	AppName    string
	// PreviewAllowed: codes may be shown on screen. True only when asked for explicitly:
	// SMS_PROVIDER=preview, DEV_MOCK_SMS=true outside production, or
	// OTP_PREVIEW_INSECURE=true anywhere.
	PreviewAllowed bool
}

// LoadConfig reads the SMS settings. production says whether this is a production server.
func LoadConfig(appName string, production bool) Config {
	c := Config{
		Provider:   strings.ToLower(strings.TrimSpace(os.Getenv("SMS_PROVIDER"))),
		AuthKey:    strings.TrimSpace(os.Getenv("SMS_AUTH_KEY")),
		SenderID:   strings.TrimSpace(os.Getenv("SMS_SENDER_ID")),
		TemplateID: strings.TrimSpace(os.Getenv("SMS_OTP_TEMPLATE_ID")),
		TwilioSID:  strings.TrimSpace(os.Getenv("TWILIO_ACCOUNT_SID")),
		TwilioAuth: strings.TrimSpace(os.Getenv("TWILIO_AUTH_TOKEN")),
		TwilioFrom: strings.TrimSpace(os.Getenv("TWILIO_FROM")),
		AppName:    appName,
	}
	explicit := func(name string) bool { return strings.EqualFold(strings.TrimSpace(os.Getenv(name)), "true") }
	switch {
	case explicit("OTP_PREVIEW_INSECURE"):
		c.PreviewAllowed = true
	case !production && (c.Provider == "preview" || explicit("DEV_MOCK_SMS")):
		c.PreviewAllowed = true
	}
	if c.Provider == "mock" { // the old name for "no real provider"
		c.Provider = ""
	}
	return c
}

// New picks the sender. A real provider wins; preview is used only when explicitly allowed;
// otherwise codes can't be delivered and sign-in by phone reports that honestly.
func New(c Config) Sender {
	client := &http.Client{Timeout: 12 * time.Second}
	switch c.Provider {
	case "msg91":
		if c.AuthKey != "" && c.TemplateID != "" {
			return &msg91{cfg: c, http: client}
		}
	case "twilio":
		if c.TwilioSID != "" && c.TwilioAuth != "" && c.TwilioFrom != "" {
			return &twilio{cfg: c, http: client}
		}
	}
	if c.PreviewAllowed {
		return preview{}
	}
	return none{}
}

type none struct{}

func (none) SendCode(context.Context, string, string) error { return ErrUnavailable }
func (none) Mode() string                                   { return "none" }

type preview struct{}

func (preview) SendCode(context.Context, string, string) error { return nil }
func (preview) Mode() string                                   { return "preview" }
func (preview) IsPreview() bool                                { return true }

// msg91 sends through MSG91's OTP API, which fills the DLT-approved template with the code.
type msg91 struct {
	cfg  Config
	http *http.Client
}

func (m *msg91) Mode() string { return "msg91" }

func (m *msg91) SendCode(ctx context.Context, phone, code string) error {
	q := url.Values{
		"template_id": {m.cfg.TemplateID},
		"mobile":      {strings.TrimPrefix(phone, "+")},
		"otp":         {code},
	}
	if m.cfg.SenderID != "" {
		q.Set("sender", m.cfg.SenderID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://control.msg91.com/api/v5/otp?"+q.Encode(), bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	req.Header.Set("authkey", m.cfg.AuthKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := m.http.Do(req)
	if err != nil {
		return fmt.Errorf("sms: msg91 unreachable: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
	var out struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &out)
	if res.StatusCode >= 300 || strings.EqualFold(out.Type, "error") {
		return fmt.Errorf("sms: msg91 refused the message (status %d: %s)", res.StatusCode, out.Message)
	}
	return nil
}

// twilio sends a plain text message through Twilio's Messages API.
type twilio struct {
	cfg  Config
	http *http.Client
}

func (t *twilio) Mode() string { return "twilio" }

func (t *twilio) SendCode(ctx context.Context, phone, code string) error {
	form := url.Values{"To": {phone}, "Body": {code + " is your " + t.cfg.AppName + " sign-in code. It expires in 10 minutes. Don't share it."}}
	if strings.HasPrefix(t.cfg.TwilioFrom, "MG") {
		form.Set("MessagingServiceSid", t.cfg.TwilioFrom)
	} else {
		form.Set("From", t.cfg.TwilioFrom)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.twilio.com/2010-04-01/Accounts/"+url.PathEscape(t.cfg.TwilioSID)+"/Messages.json", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.SetBasicAuth(t.cfg.TwilioSID, t.cfg.TwilioAuth)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := t.http.Do(req)
	if err != nil {
		return fmt.Errorf("sms: twilio unreachable: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		var out struct {
			Message string `json:"message"`
		}
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		_ = json.Unmarshal(body, &out)
		return fmt.Errorf("sms: twilio refused the message (status %d: %s)", res.StatusCode, out.Message)
	}
	return nil
}
