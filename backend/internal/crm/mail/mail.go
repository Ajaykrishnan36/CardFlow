package mail

import (
	"context"
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
)

type Message struct {
	To      string
	Subject string
	Text    string
}

type Mailer interface {
	Send(ctx context.Context, m Message) error
}

// New returns the SMTP mailer when CRM_SMTP_HOST is set, otherwise the console
// mailer, which logs the whole message so reset links are usable in local dev (D-16).
func New(cfg shared.Config) Mailer {
	if cfg.SMTPHost == "" {
		return consoleMailer{}
	}
	return smtpMailer{cfg: cfg}
}

type consoleMailer struct{}

func (consoleMailer) Send(_ context.Context, m Message) error {
	slog.Info("CRM email (console mailer)", "to", m.To, "subject", m.Subject, "body", m.Text)
	return nil
}

type smtpMailer struct{ cfg shared.Config }

func (s smtpMailer) Send(ctx context.Context, m Message) error {
	addr := fmt.Sprintf("%s:%d", s.cfg.SMTPHost, s.cfg.SMTPPort)
	var auth smtp.Auth
	if s.cfg.SMTPUser != "" {
		auth = smtp.PlainAuth("", s.cfg.SMTPUser, s.cfg.SMTPPass, s.cfg.SMTPHost)
	}
	body := strings.Join([]string{
		"From: " + s.cfg.SMTPFrom,
		"To: " + m.To,
		"Subject: " + m.Subject,
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		m.Text,
	}, "\r\n")

	done := make(chan error, 1)
	go func() {
		done <- smtp.SendMail(addr, auth, extractAddress(s.cfg.SMTPFrom), []string{m.To}, []byte(body))
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// extractAddress turns `Name <a@b.c>` into `a@b.c`.
func extractAddress(from string) string {
	if i := strings.LastIndex(from, "<"); i >= 0 {
		if j := strings.LastIndex(from, ">"); j > i {
			return from[i+1 : j]
		}
	}
	return from
}
