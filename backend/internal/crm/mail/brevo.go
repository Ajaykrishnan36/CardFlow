package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	netmail "net/mail"
	"time"

	"cardflow-backend/internal/crm/shared"
)

// brevoMailer sends through Brevo's HTTPS API (port 443). Hosts that block outbound SMTP,
// such as Render's free plan (ports 25/465/587), can still send this way. The sender
// address must be verified in Brevo (a Gmail address works as a single sender).
type brevoMailer struct {
	cfg    shared.Config
	client *http.Client
}

const brevoEndpoint = "https://api.brevo.com/v3/smtp/email"

func (brevoMailer) Mode() string   { return "brevo" }
func (b brevoMailer) From() string { return b.cfg.SMTPFrom }

type brevoAddress struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

func (b brevoMailer) Send(ctx context.Context, m Message) error {
	from, err := netmail.ParseAddress(b.cfg.SMTPFrom)
	if err != nil {
		from = &netmail.Address{Address: extractAddress(b.cfg.SMTPFrom)}
	}
	name := from.Name
	if m.FromName != "" {
		name = m.FromName
	}
	body := map[string]any{
		"sender":      brevoAddress{Email: from.Address, Name: name},
		"to":          []brevoAddress{{Email: m.To}},
		"subject":     m.Subject,
		"textContent": plainText(m),
	}
	if m.ReplyTo != "" {
		body["replyTo"] = brevoAddress{Email: m.ReplyTo}
	}
	if len(m.Headers) > 0 {
		body["headers"] = m.Headers
	}
	if m.HTML != "" {
		body["htmlContent"] = m.HTML
	} else if m.Heading != "" {
		html, err := renderHTML(m, b.cfg.AppName)
		if err != nil {
			return err
		}
		body["htmlContent"] = html
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, brevoEndpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("api-key", b.cfg.BrevoAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return fmt.Errorf("brevo: %s: %s", res.Status, bytes.TrimSpace(detail))
	}
	return nil
}

func newBrevo(cfg shared.Config) brevoMailer {
	return brevoMailer{cfg: cfg, client: &http.Client{Timeout: 15 * time.Second}}
}
