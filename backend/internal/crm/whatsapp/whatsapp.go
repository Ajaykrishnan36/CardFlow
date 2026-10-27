// Package whatsapp sends notices through the WhatsApp Business Cloud API (D-161). A message
// the business starts must use a template Meta has approved, so the notice goes in as the
// template's parameters: {{1}} the person's name, {{2}} the text.
package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ErrUnavailable means no WhatsApp account is configured.
var ErrUnavailable = errors.New("whatsapp: not configured")

// Config is read from the environment (names only; values live in the host's settings).
type Config struct {
	Token         string // WHATSAPP_TOKEN: a permanent system-user access token
	PhoneNumberID string // WHATSAPP_PHONE_NUMBER_ID: the sending number's id
	Template      string // WHATSAPP_TEMPLATE: approved template with two body parameters
	Language      string // WHATSAPP_TEMPLATE_LANG, default "en"
}

func LoadConfig() Config {
	c := Config{
		Token:         strings.TrimSpace(os.Getenv("WHATSAPP_TOKEN")),
		PhoneNumberID: strings.TrimSpace(os.Getenv("WHATSAPP_PHONE_NUMBER_ID")),
		Template:      strings.TrimSpace(os.Getenv("WHATSAPP_TEMPLATE")),
		Language:      strings.TrimSpace(os.Getenv("WHATSAPP_TEMPLATE_LANG")),
	}
	if c.Language == "" {
		c.Language = "en"
	}
	return c
}

type Sender struct {
	cfg  Config
	http *http.Client
}

func New(c Config) *Sender { return &Sender{cfg: c, http: &http.Client{Timeout: 12 * time.Second}} }

// Ready: an account and a template are set.
func (s *Sender) Ready() bool {
	return s != nil && s.cfg.Token != "" && s.cfg.PhoneNumberID != "" && s.cfg.Template != ""
}

// Send delivers one notice to a number in E.164 form.
func (s *Sender) Send(ctx context.Context, phone, name, text string) error {
	if !s.Ready() {
		return ErrUnavailable
	}
	param := func(v string) map[string]string {
		v = strings.Join(strings.Fields(v), " ") // templates refuse new lines and tabs
		if len(v) > 1000 {
			v = v[:1000]
		}
		return map[string]string{"type": "text", "text": v}
	}
	if strings.TrimSpace(name) == "" {
		name = "there"
	}
	payload, _ := json.Marshal(map[string]any{
		"messaging_product": "whatsapp",
		"to":                strings.TrimPrefix(phone, "+"),
		"type":              "template",
		"template": map[string]any{
			"name":     s.cfg.Template,
			"language": map[string]string{"code": s.cfg.Language},
			"components": []any{map[string]any{
				"type":       "body",
				"parameters": []any{param(name), param(text)},
			}},
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://graph.facebook.com/v21.0/"+s.cfg.PhoneNumberID+"/messages", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	res, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("whatsapp: unreachable: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		var out struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		_ = json.Unmarshal(body, &out)
		return fmt.Errorf("whatsapp: refused (status %d: %s)", res.StatusCode, out.Error.Message)
	}
	return nil
}
