package identity

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"cardflow-backend/internal/crm/mail"
	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Email one-time codes (PRD §4 AUTH-02, OTP sign-in). A 6-digit code valid for 10
// minutes, 5 attempts, single use. Unknown addresses get a clear "no account" error
// (owner's decision, D-42); rate limits cap how fast addresses can be tried. The code is a first factor: MFA, lockout and workspace checks still apply.

const (
	otpTTL         = 10 * time.Minute
	otpMaxAttempts = 5
)

type OTPRequestInput struct {
	Identifier string `json:"identifier"`
	Audience   string `json:"audience"`
}

type OTPRequestResult struct {
	Sent      bool   `json:"sent"`
	ExpiresIn int    `json:"expiresIn"`
	Channel   string `json:"channel"`
	// DevCode is returned only in local/dev when email goes to the log, so the code can
	// be typed in (same idea as CardFlow's on-screen OTP preview).
	DevCode string `json:"devCode,omitempty"`
}

func newOTP() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func (s *Service) RequestOTP(ctx context.Context, in OTPRequestInput, meta RequestMeta) (*OTPRequestResult, error) {
	res := &OTPRequestResult{Sent: true, ExpiresIn: int(otpTTL.Seconds()), Channel: "email"}
	email, ok := NormalizeEmail(in.Identifier)
	if !ok {
		return nil, shared.Validation(map[string]string{"identifier": "Enter the email address on your account."})
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

	identityID, name, err := s.accountFor(ctx, "email", email)
	if err != nil {
		return nil, err
	}
	code, err := newOTP()
	if err != nil {
		return nil, err
	}
	if _, err := s.store.Pool.Exec(ctx, `
		UPDATE crm.otp_challenges SET consumed_at = now() WHERE destination = $1 AND purpose = 'login' AND consumed_at IS NULL`, email); err != nil {
		return nil, err
	}
	if _, err := s.store.Pool.Exec(ctx, `
		INSERT INTO crm.otp_challenges (channel, destination, purpose, code_hash, expires_at) VALUES ('email', $1, 'login', $2, $3)`,
		email, shared.HashToken(email+":"+code), time.Now().Add(otpTTL)); err != nil {
		return nil, err
	}
	_ = shared.WriteAudit(ctx, s.store.Pool, shared.AuditEvent{ActorID: &identityID, Action: "auth.otp.requested", EntityType: "identity",
		EntityID: &identityID, IP: meta.IP, RequestID: meta.RequestID})
	// Magic link: the same one-time code, pre-filled on the sign-in page.
	loginPath := "/crm/login"
	if strings.EqualFold(strings.TrimSpace(in.Audience), "owner") {
		loginPath = "/crm/owner/login"
	}
	magic := s.cfg.BaseURL + loginPath + "?" + url.Values{"method": {"code"}, "email": {email}, "code": {code}}.Encode()
	if err := s.sendNow(ctx, mail.Message{
		To:      email,
		Subject: code + " is your " + s.cfg.AppName + " sign-in code",
		Heading: "Your sign-in code",
		Lines:   []string{"Hi " + name + ", tap the button to sign in to " + s.cfg.AppName + ", or type this code on the sign-in page."},
		Code:    code,
		Button:  &mail.Button{Label: "Sign in to " + s.cfg.AppName, URL: magic},
		Footer:  "The code expires in 10 minutes and works once. If you didn't try to sign in, you can ignore this email — someone may have typed your address by mistake.",
	}); err != nil {
		return nil, err
	}
	if s.cfg.IsLocalOrDev() && s.mailer.Mode() == "console" {
		res.DevCode = code
	}
	return res, nil
}

type OTPVerifyInput struct {
	Identifier    string `json:"identifier"`
	Code          string `json:"code"`
	Audience      string `json:"audience"`
	WorkspaceCode string `json:"workspaceCode"`
}

var errBadCode = shared.NewError(http.StatusUnauthorized, "invalid_code", "That code isn't right or has expired. Request a new one.")

func (s *Service) VerifyOTP(ctx context.Context, in OTPVerifyInput, meta RequestMeta) (*loginOutcome, error) {
	audience := strings.ToLower(strings.TrimSpace(in.Audience))
	if audience == "" {
		audience = "workspace"
	}
	if audience != "owner" && audience != "workspace" {
		return nil, shared.Validation(map[string]string{"audience": "Unknown sign-in audience."})
	}
	email, ok := NormalizeEmail(in.Identifier)
	code := strings.TrimSpace(in.Code)
	if !ok || len(code) != 6 {
		return nil, shared.Validation(map[string]string{"code": "Enter the 6-digit code from the email."})
	}
	identKey := "email:global:" + email
	if blocked, wait := s.identLimiter.blocked(identKey); blocked {
		return nil, shared.TooManyAttempts(int(wait.Seconds()) + 1)
	}

	var challengeID uuid.UUID
	var hash []byte
	var attempts int
	err := s.store.Pool.QueryRow(ctx, `
		SELECT id, code_hash, attempts FROM crm.otp_challenges
		WHERE destination = $1 AND purpose = 'login' AND consumed_at IS NULL AND expires_at > now()
		ORDER BY created_at DESC LIMIT 1`, email).Scan(&challengeID, &hash, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		s.identLimiter.hit(identKey)
		return nil, errBadCode
	}
	if err != nil {
		return nil, err
	}
	if attempts >= otpMaxAttempts || !shared.ConstantTimeEqual(string(hash), string(shared.HashToken(email+":"+code))) {
		_, _ = s.store.Pool.Exec(ctx, `UPDATE crm.otp_challenges SET attempts = attempts + 1,
			consumed_at = CASE WHEN attempts + 1 >= $2 THEN now() ELSE consumed_at END WHERE id = $1`, challengeID, otpMaxAttempts)
		if s.identLimiter.hit(identKey) {
			return nil, shared.TooManyAttempts(int(lockDuration.Seconds()))
		}
		return nil, errBadCode
	}
	// Single use: consume before signing in.
	tag, err := s.store.Pool.Exec(ctx, `UPDATE crm.otp_challenges SET consumed_at = now() WHERE id = $1 AND consumed_at IS NULL`, challengeID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, errBadCode
	}

	var row credentialRow
	err = s.store.Pool.QueryRow(ctx, `
		SELECT i.id, i.display_name, i.is_platform_owner, i.status, pc.hash, COALESCE(pc.must_change, false), 0, pc.locked_until
		FROM crm.verified_identifiers vi JOIN crm.identities i ON i.id = vi.identity_id
		LEFT JOIN crm.password_credentials pc ON pc.identity_id = i.id
		WHERE vi.kind = 'email' AND vi.namespace = 'global' AND vi.value_normalized = $1 AND i.status <> 'deleted'`, email).Scan(
		&row.identityID, &row.displayName, &row.isOwner, &row.status, &row.hash, &row.mustChange, &row.failedAttempts, &row.lockedUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errBadCode
	}
	if err != nil {
		return nil, err
	}
	if row.lockedUntil != nil && row.lockedUntil.After(time.Now()) {
		return nil, shared.TooManyAttempts(int(time.Until(*row.lockedUntil).Seconds()) + 1)
	}
	// A code proves the mailbox, not a password: nothing to change on first sign-in here.
	row.mustChange = row.mustChange && row.hash != nil
	s.identLimiter.clear(identKey)
	return s.finishSignIn(ctx, row, audience, in.WorkspaceCode, identKey, "email_code", meta)
}

func (s *Service) handleOTPRequest(w http.ResponseWriter, r *http.Request) {
	var in OTPRequestInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	res, err := s.RequestOTP(r.Context(), in, Meta(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, res)
}

func (s *Service) handleOTPVerify(w http.ResponseWriter, r *http.Request) {
	var in OTPVerifyInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	out, err := s.VerifyOTP(r.Context(), in, Meta(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	s.setSessionCookie(w, out.token, out.expires)
	shared.WriteJSON(w, http.StatusOK, out.step)
}
