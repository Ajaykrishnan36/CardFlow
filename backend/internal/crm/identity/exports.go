package identity

import (
	"context"
	"fmt"
	"log/slog"
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
	s.sendAsync(mail.Message{
		To:      email,
		Subject: "Reset your " + s.cfg.AppName + " password",
		Text: fmt.Sprintf("Hi %s,\n\nAn administrator sent you a link to set a new password. It works once and expires in 30 minutes:\n\n%s\n",
			name, link),
	})
	return link, nil
}

func (s *Service) sendAsync(m mail.Message) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := s.mailer.Send(ctx, m); err != nil {
			slog.Warn("crm: email not sent", "subject", m.Subject, "error", err)
		}
	}()
}

// ValidateNewPassword returns a user-facing problem with a new password, or "".
func ValidateNewPassword(password string) string { return validateNewPassword(password) }
