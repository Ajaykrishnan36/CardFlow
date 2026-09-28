package identity

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"cardflow-backend/internal/crm/mail"
	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// NormalizeEmail returns the canonical (lower-case) form of a valid email address.
func NormalizeEmail(raw string) (string, bool) {
	id, ok := normalizeIdentifier(raw, "")
	if !ok || id.kind != "email" {
		return "", false
	}
	return id.value, true
}

// NormalizePhone returns the E.164 form of a phone number (India default).
func NormalizePhone(raw string) (string, bool) {
	return normalizePhone(raw)
}

// IssuePasswordReset creates a single-use reset link for identityID on behalf of an
// administrator (owner "Send password reset link") and emails it. The link is returned
// so local/dev can show it in the UI; production callers must not expose it.
func (s *Service) IssuePasswordReset(ctx context.Context, identityID, actorID uuid.UUID, meta RequestMeta) (string, error) {
	var name string
	if err := s.store.Pool.QueryRow(ctx, `SELECT display_name FROM crm.identities WHERE id = $1 AND status <> 'deleted'`, identityID).Scan(&name); err != nil {
		if err == pgx.ErrNoRows {
			return "", shared.NotFound("user_not_found")
		}
		return "", err
	}
	email := s.primaryEmail(ctx, identityID)
	if email == "" {
		return "", shared.NewError(422, "no_email", "This user has no verified email address to send a reset link to.")
	}
	token, err := shared.RandomToken(32)
	if err != nil {
		return "", err
	}
	err = s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE crm.password_resets SET used_at = now() WHERE identity_id = $1 AND used_at IS NULL`, identityID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO crm.password_resets (token_hash, identity_id, expires_at) VALUES ($1, $2, $3)`,
			shared.HashToken(token), identityID, time.Now().Add(resetTokenTTL)); err != nil {
			return err
		}
		return shared.WriteAudit(ctx, tx, shared.AuditEvent{
			ActorID: &actorID, Action: "identity.password_reset_sent", EntityType: "identity", EntityID: &identityID,
			IP: meta.IP, RequestID: meta.RequestID,
		})
	})
	if err != nil {
		return "", err
	}
	link := s.cfg.BaseURL + "/crm/reset-password?token=" + token
	if err := s.sendNow(ctx, mail.Message{
		To:      email,
		Subject: "Set a new " + s.cfg.AppName + " password",
		Heading: "Set a new password",
		Lines:   []string{"Hi " + name + ", your administrator sent you a link to set a new password for " + s.cfg.AppName + "."},
		Button:  &mail.Button{Label: "Set my password", URL: link},
		Footer:  "This link works once and expires in 30 minutes.",
	}); err != nil {
		return "", err
	}
	return link, nil
}

// sendNow sends while the request waits, so the person is told when an email couldn't go
// out instead of waiting for a code that never arrives. The provider error is logged.
func (s *Service) sendNow(ctx context.Context, m mail.Message) error {
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancel()
	if err := s.mailer.Send(sendCtx, m); err != nil {
		slog.Error("crm: email not sent", "mode", s.mailer.Mode(), "subject", m.Subject, "error", err)
		return shared.NewError(http.StatusBadGateway, "email_failed", "We couldn't send the email right now. Try again in a minute, or sign in with your password.")
	}
	return nil
}

// ValidateNewPassword returns a user-facing problem with a new password, or "".
func ValidateNewPassword(password string) string { return validateNewPassword(password) }

func istLocation() *time.Location {
	if loc, err := time.LoadLocation("Asia/Kolkata"); err == nil {
		return loc
	}
	return time.UTC
}

// shortAgent turns a user agent into "Chrome on macOS"-style text for emails.
func shortAgent(ua string) string {
	browser := "Browser"
	switch {
	case strings.Contains(ua, "Edg/"):
		browser = "Edge"
	case strings.Contains(ua, "Chrome/"):
		browser = "Chrome"
	case strings.Contains(ua, "Firefox/"):
		browser = "Firefox"
	case strings.Contains(ua, "Safari/"):
		browser = "Safari"
	case strings.Contains(strings.ToLower(ua), "curl"):
		browser = "curl"
	}
	for _, os := range []struct{ key, name string }{{"iPhone", "iOS"}, {"iPad", "iPadOS"}, {"Android", "Android"}, {"Mac OS X", "macOS"}, {"Windows", "Windows"}, {"Linux", "Linux"}} {
		if strings.Contains(ua, os.key) {
			return browser + " on " + os.name
		}
	}
	return browser
}
