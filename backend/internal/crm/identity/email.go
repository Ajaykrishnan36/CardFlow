package identity

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"cardflow-backend/internal/crm/mail"
	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Adding an email address and a password to an account that signed up by phone (D-103).
// The address counts only after a code sent to it is typed back; a password can be set
// once the account has a verified email or phone to sign in with.
//
//	POST /me/email/request  {email}          → sends a 6-digit code to the new address
//	POST /me/email/verify   {email, code}    → the address is now verified on the account
//	POST /me/password       {newPassword, currentPassword?}

const purposeEmailVerify = "verify_email"

func emailHash(email, code string) []byte {
	return shared.HashToken(email + ":" + purposeEmailVerify + ":" + code)
}

// RequestEmailVerify sends a code to an address the signed-in person wants on their account.
func (s *Service) RequestEmailVerify(ctx context.Context, identityID uuid.UUID, rawEmail string, meta RequestMeta) (*OTPRequestResult, error) {
	email, ok := NormalizeEmail(rawEmail)
	if !ok {
		return nil, shared.Validation(map[string]string{"email": "Enter a valid email address."})
	}
	var owner uuid.UUID
	var verified *time.Time
	err := s.store.Pool.QueryRow(ctx, `SELECT identity_id, verified_at FROM crm.verified_identifiers WHERE kind = 'email' AND namespace = 'global' AND value_normalized = $1`,
		email).Scan(&owner, &verified)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if err == nil && owner != identityID {
		return nil, shared.Validation(map[string]string{"email": "Another account already uses this email address."})
	}
	if err == nil && verified != nil {
		return nil, shared.Validation(map[string]string{"email": "This email address is already verified on your account."})
	}
	ipKey := "otp-ip:" + meta.IP
	if blocked, wait := s.resetIPLimiter.blocked(ipKey); blocked {
		return nil, shared.TooManyAttempts(int(wait.Seconds()) + 1)
	}
	s.resetIPLimiter.hit(ipKey)
	identKey := "otp:" + email
	if blocked, wait := s.otpLimiter.blocked(identKey); blocked {
		return nil, shared.TooManyAttempts(int(wait.Seconds()) + 1)
	}
	s.otpLimiter.hit(identKey)

	code, err := newOTP()
	if err != nil {
		return nil, err
	}
	if _, err := s.store.Pool.Exec(ctx, `UPDATE crm.otp_challenges SET consumed_at = now() WHERE destination = $1 AND purpose = $2 AND consumed_at IS NULL`,
		email, purposeEmailVerify); err != nil {
		return nil, err
	}
	var challengeID uuid.UUID
	if err := s.store.Pool.QueryRow(ctx, `
		INSERT INTO crm.otp_challenges (channel, destination, purpose, code_hash, expires_at) VALUES ('email', $1, $2, $3, $4) RETURNING id`,
		email, purposeEmailVerify, emailHash(email, code), time.Now().Add(otpTTL)).Scan(&challengeID); err != nil {
		return nil, err
	}
	if err := s.sendNow(ctx, mail.Message{
		To:      email,
		Subject: code + " is your " + s.cfg.AppName + " verification code",
		Heading: "Verify your email address",
		Lines:   []string{"Type this code in " + s.cfg.AppName + " to add this email address to your account."},
		Code:    code,
		Footer:  "The code expires in 10 minutes and works once. If you didn't ask for it, you can ignore this email.",
	}); err != nil {
		_, _ = s.store.Pool.Exec(ctx, `UPDATE crm.otp_challenges SET consumed_at = now() WHERE id = $1`, challengeID)
		return nil, err
	}
	_ = shared.WriteAudit(ctx, s.store.Pool, shared.AuditEvent{ActorID: &identityID, Action: "auth.email_verify.requested", EntityType: "identity",
		EntityID: &identityID, IP: meta.IP, RequestID: meta.RequestID})
	res := &OTPRequestResult{Sent: true, ExpiresIn: int(otpTTL.Seconds()), Channel: "email"}
	if s.cfg.IsLocalOrDev() && s.mailer.Mode() == "console" {
		res.DevCode = code
	}
	return res, nil
}

// VerifyEmail puts the address on the account once the code is confirmed. It becomes the
// account's email: an older address of the same account is replaced.
func (s *Service) VerifyEmail(ctx context.Context, identityID uuid.UUID, rawEmail, code string, meta RequestMeta) (string, error) {
	email, ok := NormalizeEmail(rawEmail)
	code = strings.TrimSpace(code)
	if !ok || len(code) != 6 {
		return "", shared.Validation(map[string]string{"code": "Enter the 6-digit code from the email."})
	}
	identKey := "email:global:" + email
	if blocked, wait := s.identLimiter.blocked(identKey); blocked {
		return "", shared.TooManyAttempts(int(wait.Seconds()) + 1)
	}
	var challengeID uuid.UUID
	var hash []byte
	var attempts int
	err := s.store.Pool.QueryRow(ctx, `
		SELECT id, code_hash, attempts FROM crm.otp_challenges
		WHERE destination = $1 AND purpose = $2 AND channel = 'email' AND consumed_at IS NULL AND expires_at > now()
		ORDER BY created_at DESC LIMIT 1`, email, purposeEmailVerify).Scan(&challengeID, &hash, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		s.identLimiter.hit(identKey)
		return "", errBadCode
	}
	if err != nil {
		return "", err
	}
	if attempts >= otpMaxAttempts || !shared.ConstantTimeEqual(string(hash), string(emailHash(email, code))) {
		_, _ = s.store.Pool.Exec(ctx, `UPDATE crm.otp_challenges SET attempts = attempts + 1,
			consumed_at = CASE WHEN attempts + 1 >= $2 THEN now() ELSE consumed_at END WHERE id = $1`, challengeID, otpMaxAttempts)
		if s.identLimiter.hit(identKey) {
			return "", shared.TooManyAttempts(int(lockDuration.Seconds()))
		}
		return "", errBadCode
	}
	err = s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := consumeChallenge(ctx, tx, challengeID); err != nil {
			return err
		}
		var owner uuid.UUID
		err := tx.QueryRow(ctx, `SELECT identity_id FROM crm.verified_identifiers WHERE kind = 'email' AND namespace = 'global' AND value_normalized = $1 FOR UPDATE`,
			email).Scan(&owner)
		if err == nil && owner != identityID {
			return shared.Validation(map[string]string{"email": "Another account already uses this email address."})
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM crm.verified_identifiers WHERE identity_id = $1 AND kind = 'email' AND namespace = 'global' AND value_normalized <> $2`,
			identityID, email); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.verified_identifiers (identity_id, kind, value_normalized, namespace, verified_at) VALUES ($1, 'email', $2, 'global', now())
			ON CONFLICT (kind, namespace, value_normalized) DO UPDATE SET verified_at = now()`, identityID, email); err != nil {
			return err
		}
		return shared.WriteAudit(ctx, tx, shared.AuditEvent{ActorID: &identityID, Action: "auth.email.verified", EntityType: "identity", EntityID: &identityID,
			After: map[string]any{"email": maskEmail(email)}, IP: meta.IP, RequestID: meta.RequestID})
	})
	if err != nil {
		return "", err
	}
	s.identLimiter.clear(identKey)
	return email, nil
}

// SetPassword gives the account a password. An account that already has one must send
// the current password; one that never had a password (signed up by phone) may set it
// directly — the person is signed in with a code sent to their own number or address.
func (s *Service) SetPassword(ctx context.Context, sess *Session, current, next string, meta RequestMeta) error {
	if msg := validateNewPassword(next); msg != "" {
		return shared.Validation(map[string]string{"newPassword": msg})
	}
	var hash string
	err := s.store.Pool.QueryRow(ctx, `SELECT hash FROM crm.password_credentials WHERE identity_id = $1`, sess.IdentityID).Scan(&hash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	had := err == nil
	if had {
		if current == "" {
			return shared.Validation(map[string]string{"currentPassword": "Enter your current password."})
		}
		if !verifyPassword(current, hash) {
			return shared.Validation(map[string]string{"currentPassword": "That isn't your current password."})
		}
		if next == current {
			return shared.Validation(map[string]string{"newPassword": "Choose a password different from your current one."})
		}
	}
	newHash, err := HashPassword(next)
	if err != nil {
		return err
	}
	return s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.password_credentials (identity_id, hash, must_change) VALUES ($1, $2, false)
			ON CONFLICT (identity_id) DO UPDATE SET hash = EXCLUDED.hash, must_change = false, updated_at = now()`, sess.IdentityID, newHash); err != nil {
			return err
		}
		// Every other session ends: a changed password signs other devices out.
		if _, err := tx.Exec(ctx, `UPDATE crm.sessions SET revoked_at = now() WHERE identity_id = $1 AND id <> $2 AND revoked_at IS NULL`,
			sess.IdentityID, sess.ID); err != nil {
			return err
		}
		action := "auth.password.set"
		if had {
			action = "auth.password.changed"
		}
		return shared.WriteAudit(ctx, tx, shared.AuditEvent{ActorID: &sess.IdentityID, Action: action, EntityType: "identity", EntityID: &sess.IdentityID,
			IP: meta.IP, RequestID: meta.RequestID})
	})
}

func (s *Service) handleEmailVerifyRequest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	res, err := s.RequestEmailVerify(r.Context(), SessionFrom(r.Context()).IdentityID, in.Email, Meta(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, res)
}

func (s *Service) handleEmailVerify(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	email, err := s.VerifyEmail(r.Context(), SessionFrom(r.Context()).IdentityID, in.Email, in.Code, Meta(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"email": email, "verified": true})
}

func (s *Service) handleSetPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if err := s.SetPassword(r.Context(), SessionFrom(r.Context()), in.CurrentPassword, in.NewPassword, Meta(r)); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// CookieSession resolves the browser session of a request to another API on this server
// (the app API). A request that changes something must carry the CSRF token, exactly as
// CRM requests do. nil = no valid session.
func (s *Service) CookieSession(r *http.Request) *Session {
	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	if !isSafeMethod(r.Method) {
		csrf, err := r.Cookie(CSRFCookie)
		header := r.Header.Get(CSRFHeader)
		if err != nil || len(csrf.Value) != 43 || header == "" || !shared.ConstantTimeEqual(header, csrf.Value) {
			return nil
		}
	}
	sess, err := s.lookupSession(r.Context(), c.Value)
	if err != nil || sess == nil || sess.Transport == "bearer" || !sess.Ready() {
		return nil
	}
	s.touchSession(r.Context(), sess)
	return sess
}
