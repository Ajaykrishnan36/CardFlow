package identity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/mail"
	"cardflow-backend/internal/crm/shared"
	"cardflow-backend/internal/crm/sms"
	"cardflow-backend/internal/crm/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	lockAfterFailures = 5                // PRD AUTH-02
	lockDuration      = 15 * time.Minute // PRD AUTH-02
	resetTokenTTL     = 30 * time.Minute // PRD §4 password reset
)

type Service struct {
	store  *store.Store
	cfg    shared.Config
	mailer mail.Mailer

	ipLimiter         *limiter // any login attempt, per IP
	identLimiter      *limiter // failed logins, per normalised identifier (D-14)
	resetIPLimiter    *limiter
	resetIdentLimiter *limiter
	otpLimiter        *limiter // sign-in code requests, per email

	// Phone sign-in (D-93).
	sms             sms.Sender
	phoneLimiter    *limiter // code requests per phone, short window
	phoneDayLimiter *limiter // code requests per phone, per day (SMS cost and abuse)
	phoneIPLimiter  *limiter // code requests per IP
	appProfile      AppProfile
}

func NewService(st *store.Store, cfg shared.Config, mailer mail.Mailer) *Service {
	return &Service{
		store:             st,
		cfg:               cfg,
		mailer:            mailer,
		ipLimiter:         newLimiter(30, 5*time.Minute),
		identLimiter:      newLimiter(lockAfterFailures, lockDuration),
		resetIPLimiter:    newLimiter(5, 15*time.Minute),
		resetIdentLimiter: newLimiter(3, time.Hour),
		otpLimiter:        newLimiter(5, 15*time.Minute),
		sms:               sms.New(sms.Config{}),
		phoneLimiter:      newLimiter(3, 10*time.Minute),
		phoneDayLimiter:   newLimiter(10, 24*time.Hour),
		phoneIPLimiter:    newLimiter(20, time.Hour),
	}
}

// SetSMS installs the text-message sender for phone sign-in codes.
func (s *Service) SetSMS(sender sms.Sender) { s.sms = sender }

// SMSMode names the configured text-message provider ("none" when codes can't be sent).
func (s *Service) SMSMode() string { return s.sms.Mode() }

type RequestMeta struct {
	IP        string
	UserAgent string
	RequestID string
	// Transport is "bearer" when the caller is a native app that keeps the session token
	// itself (X-Session-Transport: bearer); otherwise the session travels in a cookie.
	Transport string
}

type LoginInput struct {
	Identifier    string `json:"identifier"`
	Password      string `json:"password"`
	Audience      string `json:"audience"`
	WorkspaceCode string `json:"workspaceCode"`
}

type AuthStep struct {
	MFARequired           bool   `json:"mfaRequired"`
	MFAEnrollmentRequired bool   `json:"mfaEnrollmentRequired"`
	MustChangePassword    bool   `json:"mustChangePassword"`
	Next                  string `json:"next"`
	// Token and ExpiresAt are set only for a bearer session (native app).
	Token     string     `json:"token,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	// IsNewUser: this sign-in created the account (phone sign-up).
	IsNewUser bool `json:"isNewUser,omitempty"`
	// HasBusiness: the person belongs to at least one business; false sends them to create one.
	HasBusiness bool `json:"hasBusiness"`
	// App carries what the app's profile link reports (e.g. businesses claimed from scanned cards).
	App map[string]any `json:"app,omitempty"`
}

type loginOutcome struct {
	token   string
	expires time.Time
	step    AuthStep
}

func nextPath(isOwner, mfaPending, enrollRequired, mustChange bool) string {
	switch {
	case enrollRequired:
		return "/crm/mfa/setup"
	case mfaPending:
		return "/crm/mfa/verify"
	case mustChange:
		return "/crm/change-password"
	case isOwner:
		return "/crm/owner/dashboard"
	default:
		return "/crm/home"
	}
}

type credentialRow struct {
	identityID     uuid.UUID
	displayName    string
	isOwner        bool
	status         string
	hash           *string
	mustChange     bool
	failedAttempts int
	lockedUntil    *time.Time
}

func (s *Service) Login(ctx context.Context, in LoginInput, meta RequestMeta) (*loginOutcome, error) {
	audience := strings.ToLower(strings.TrimSpace(in.Audience))
	if audience == "" {
		audience = "workspace"
	}
	if audience != "owner" && audience != "workspace" {
		return nil, shared.Validation(map[string]string{"audience": "Unknown sign-in audience."})
	}

	fields := map[string]string{}
	if strings.TrimSpace(in.Identifier) == "" {
		fields["identifier"] = "Enter your email or phone number."
	}
	if in.Password == "" {
		fields["password"] = "Enter your password."
	}
	if len(fields) > 0 {
		return nil, shared.Validation(fields)
	}

	ipKey := "ip:" + meta.IP
	if blocked, wait := s.ipLimiter.blocked(ipKey); blocked {
		return nil, shared.TooManyAttempts(int(wait.Seconds()) + 1)
	}
	s.ipLimiter.hit(ipKey)

	id, ok := normalizeIdentifier(in.Identifier, in.WorkspaceCode)
	if !ok {
		return nil, shared.Validation(map[string]string{"identifier": "Enter a valid email or phone number."})
	}
	identKey := id.kind + ":" + id.namespace + ":" + id.value
	if blocked, wait := s.identLimiter.blocked(identKey); blocked {
		return nil, shared.TooManyAttempts(int(wait.Seconds()) + 1)
	}

	var row credentialRow
	err := s.store.Pool.QueryRow(ctx, `
		SELECT i.id, i.display_name, i.is_platform_owner, i.status,
		       pc.hash, COALESCE(pc.must_change, false), COALESCE(pc.failed_attempts, 0), pc.locked_until
		FROM crm.verified_identifiers vi
		JOIN crm.identities i ON i.id = vi.identity_id
		LEFT JOIN crm.password_credentials pc ON pc.identity_id = i.id
		WHERE vi.kind = $1 AND vi.namespace = $2 AND vi.value_normalized = $3
		  AND vi.verified_at IS NOT NULL AND i.status <> 'deleted'`,
		id.kind, id.namespace, id.value).Scan(
		&row.identityID, &row.displayName, &row.isOwner, &row.status,
		&row.hash, &row.mustChange, &row.failedAttempts, &row.lockedUntil)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.hash == nil) {
		burnPasswordCheck(in.Password)
		return nil, s.failLogin(ctx, identKey, nil, meta, "unknown_identifier")
	}
	if err != nil {
		return nil, err
	}

	if row.lockedUntil != nil && row.lockedUntil.After(time.Now()) {
		return nil, shared.TooManyAttempts(int(time.Until(*row.lockedUntil).Seconds()) + 1)
	}

	if !verifyPassword(in.Password, *row.hash) {
		_, _ = s.store.Pool.Exec(ctx, `
			UPDATE crm.password_credentials
			SET failed_attempts = CASE WHEN failed_attempts + 1 >= $2 THEN 0 ELSE failed_attempts + 1 END,
			    locked_until    = CASE WHEN failed_attempts + 1 >= $2 THEN now() + $3::interval ELSE locked_until END,
			    updated_at = now()
			WHERE identity_id = $1`, row.identityID, lockAfterFailures, fmt.Sprintf("%d seconds", int(lockDuration.Seconds())))
		return nil, s.failLogin(ctx, identKey, &row.identityID, meta, "bad_password")
	}

	return s.finishSignIn(ctx, row, audience, in.WorkspaceCode, identKey, "password", meta)
}

// failLogin records the failure and returns the generic error — or 429 once the
// identifier is locked — so every failure path looks identical to the caller.
func (s *Service) failLogin(ctx context.Context, identKey string, identityID *uuid.UUID, meta RequestMeta, reason string) error {
	locked := s.identLimiter.hit(identKey)
	_ = shared.WriteAudit(ctx, s.store.Pool, shared.AuditEvent{
		ActorID: identityID, Action: "auth.login.failed", EntityType: "identity", EntityID: identityID,
		After: map[string]any{"reason": reason}, IP: meta.IP, RequestID: meta.RequestID,
	})
	if locked {
		return shared.TooManyAttempts(int(lockDuration.Seconds()))
	}
	return shared.InvalidCredentials()
}

func (s *Service) primaryEmail(ctx context.Context, identityID uuid.UUID) string {
	var email string
	_ = s.store.Pool.QueryRow(ctx, `
		SELECT value_normalized FROM crm.verified_identifiers
		WHERE identity_id = $1 AND kind = 'email' AND verified_at IS NOT NULL
		ORDER BY verified_at LIMIT 1`, identityID).Scan(&email)
	return email
}

func (s *Service) sendNewDeviceAlert(identityID uuid.UUID, name string, meta RequestMeta) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		email := s.primaryEmail(ctx, identityID)
		if email == "" {
			return
		}
		when := time.Now().In(istLocation()).Format("2 Jan 2006, 3:04 PM MST")
		err := s.mailer.Send(ctx, mail.Message{
			To:      email,
			Subject: "New sign-in to " + s.cfg.AppName,
			Heading: "New sign-in to your account",
			Lines: []string{
				"Hi " + name + ", your account was just used to sign in from a device we haven't seen before.",
				"When: " + when + " · IP address: " + meta.IP + " · Device: " + shortAgent(meta.UserAgent),
				"If this was you, there's nothing to do. If it wasn't, reset your password now.",
			},
			Button: &mail.Button{Label: "Reset my password", URL: s.cfg.BaseURL + "/crm/forgot-password"},
			Footer: "You get this email every time your account signs in from a new device.",
		})
		if err != nil {
			slog.Warn("crm: new-device alert not sent", "error", err)
		}
	}()
}

// ---- MFA ----

type mfaResult struct {
	token string
	Next  string `json:"next"`
}

func (s *Service) VerifyMFA(ctx context.Context, sess *Session, code string, meta RequestMeta) (*mfaResult, error) {
	if !sess.MFARequired || sess.MFAPassed {
		return nil, shared.NewError(400, "mfa_not_pending", "Two-step verification is not pending for this session.")
	}
	if sess.MFAAttempts >= maxMFAAttempts {
		_, _ = s.store.Pool.Exec(ctx, `UPDATE crm.sessions SET revoked_at = now() WHERE id = $1`, sess.ID)
		return nil, shared.TooManyAttempts(0)
	}

	var methodID uuid.UUID
	var secretEnc []byte
	var recovery []string
	var lastStep int64
	err := s.store.Pool.QueryRow(ctx, `
		SELECT id, secret_enc, recovery_codes_hash, last_used_step FROM crm.mfa_methods
		WHERE identity_id = $1 AND confirmed_at IS NOT NULL ORDER BY confirmed_at DESC LIMIT 1`,
		sess.IdentityID).Scan(&methodID, &secretEnc, &recovery, &lastStep)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NewError(400, "mfa_not_enrolled", "Set up two-step verification first.")
	}
	if err != nil {
		return nil, err
	}
	secret, err := shared.Decrypt(s.cfg.EncryptionKey, secretEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt totp secret: %w", err)
	}

	usedRecovery := false
	step, ok := matchTOTP(string(secret), code, time.Now())
	if ok && step <= lastStep {
		ok = false // replayed code
	}
	if !ok && looksLikeRecoveryCode(code) {
		h := hashRecoveryCode(code)
		for i, stored := range recovery {
			if shared.ConstantTimeEqual(stored, h) {
				recovery = append(recovery[:i], recovery[i+1:]...)
				ok, usedRecovery = true, true
				break
			}
		}
	}
	if !ok {
		_, _ = s.store.Pool.Exec(ctx, `UPDATE crm.sessions SET mfa_attempts = mfa_attempts + 1 WHERE id = $1`, sess.ID)
		_ = shared.WriteAudit(ctx, s.store.Pool, shared.AuditEvent{
			ActorID: &sess.IdentityID, Action: "auth.mfa.failed", EntityType: "identity", EntityID: &sess.IdentityID,
			IP: meta.IP, RequestID: meta.RequestID,
		})
		return nil, shared.Validation(map[string]string{"code": "That code didn't work. Check your authenticator app and try again."})
	}

	var out mfaResult
	err = s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if usedRecovery {
			if _, err := tx.Exec(ctx, `UPDATE crm.mfa_methods SET recovery_codes_hash = $2 WHERE id = $1`, methodID, recovery); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `UPDATE crm.mfa_methods SET last_used_step = $2 WHERE id = $1`, methodID, step); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE crm.sessions SET mfa_passed = true, mfa_attempts = 0, recent_auth_at = now() WHERE id = $1`, sess.ID); err != nil {
			return err
		}
		token, err := rotateToken(ctx, tx, sess.ID)
		if err != nil {
			return err
		}
		out.token = token
		return shared.WriteAudit(ctx, tx, shared.AuditEvent{
			ActorID: &sess.IdentityID, Action: "auth.mfa.verified", EntityType: "identity", EntityID: &sess.IdentityID,
			After: map[string]any{"recoveryCode": usedRecovery, "recoveryCodesLeft": len(recovery)},
			IP:    meta.IP, RequestID: meta.RequestID,
		})
	})
	if err != nil {
		return nil, err
	}
	out.Next = nextPath(sess.IsPlatformOwner, false, false, sess.MustChangePassword)
	return &out, nil
}

func (s *Service) EnrollMFA(ctx context.Context, sess *Session) (*enrollment, error) {
	var confirmed bool
	if err := s.store.Pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM crm.mfa_methods WHERE identity_id = $1 AND confirmed_at IS NOT NULL)`,
		sess.IdentityID).Scan(&confirmed); err != nil {
		return nil, err
	}
	if confirmed {
		return nil, shared.NewError(409, "mfa_already_enabled", "Two-step verification is already turned on.")
	}

	account := s.primaryEmail(ctx, sess.IdentityID)
	if account == "" {
		account = sess.DisplayName
	}
	key, enr, err := generateTOTP(s.cfg.AppName, account)
	if err != nil {
		return nil, err
	}
	sealed, err := shared.Encrypt(s.cfg.EncryptionKey, []byte(key.Secret()))
	if err != nil {
		return nil, err
	}
	err = s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM crm.mfa_methods WHERE identity_id = $1 AND confirmed_at IS NULL`, sess.IdentityID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO crm.mfa_methods (identity_id, kind, secret_enc) VALUES ($1, 'totp', $2)`, sess.IdentityID, sealed)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &enr, nil
}

type confirmResult struct {
	token         string
	RecoveryCodes []string `json:"recoveryCodes"`
	Next          string   `json:"next"`
}

func (s *Service) ConfirmMFA(ctx context.Context, sess *Session, code string, meta RequestMeta) (*confirmResult, error) {
	var methodID uuid.UUID
	var secretEnc []byte
	err := s.store.Pool.QueryRow(ctx, `
		SELECT id, secret_enc FROM crm.mfa_methods
		WHERE identity_id = $1 AND confirmed_at IS NULL ORDER BY created_at DESC LIMIT 1`,
		sess.IdentityID).Scan(&methodID, &secretEnc)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NewError(400, "mfa_enrollment_missing", "Start two-step verification setup again.")
	}
	if err != nil {
		return nil, err
	}
	secret, err := shared.Decrypt(s.cfg.EncryptionKey, secretEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt totp secret: %w", err)
	}
	step, ok := matchTOTP(string(secret), code, time.Now())
	if !ok {
		return nil, shared.Validation(map[string]string{"code": "That code didn't match. Make sure your phone's time is set automatically."})
	}
	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}

	out := confirmResult{RecoveryCodes: codes}
	err = s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE crm.mfa_methods SET confirmed_at = now(), recovery_codes_hash = $2, last_used_step = $3 WHERE id = $1`,
			methodID, hashes, step); err != nil {
			return err
		}
		if !sess.MFAComplete() {
			if _, err := tx.Exec(ctx, `
				UPDATE crm.sessions SET mfa_passed = true, mfa_attempts = 0, recent_auth_at = now() WHERE id = $1`, sess.ID); err != nil {
				return err
			}
			token, err := rotateToken(ctx, tx, sess.ID)
			if err != nil {
				return err
			}
			out.token = token
		}
		return shared.WriteAudit(ctx, tx, shared.AuditEvent{
			ActorID: &sess.IdentityID, Action: "auth.mfa.enrolled", EntityType: "identity", EntityID: &sess.IdentityID,
			IP: meta.IP, RequestID: meta.RequestID,
		})
	})
	if err != nil {
		return nil, err
	}
	out.Next = nextPath(sess.IsPlatformOwner, false, false, sess.MustChangePassword)
	return &out, nil
}

// ---- Passwords ----

func (s *Service) ForgotPassword(ctx context.Context, identifierRaw string, meta RequestMeta) (string, error) {
	ipKey := "reset-ip:" + meta.IP
	if blocked, wait := s.resetIPLimiter.blocked(ipKey); blocked {
		return "", shared.TooManyAttempts(int(wait.Seconds()) + 1)
	}
	s.resetIPLimiter.hit(ipKey)

	id, ok := normalizeIdentifier(identifierRaw, "")
	if !ok {
		return "", shared.Validation(map[string]string{"identifier": "Enter a valid email or phone number."})
	}
	identKey := "reset:" + id.kind + ":" + id.value
	if blocked, wait := s.resetIdentLimiter.blocked(identKey); blocked {
		return "", shared.TooManyAttempts(int(wait.Seconds()) + 1)
	}
	s.resetIdentLimiter.hit(identKey)

	identityID, name, err := s.accountFor(ctx, id.kind, id.value)
	if err != nil {
		return "", err
	}
	email := s.primaryEmail(ctx, identityID)
	if email == "" {
		return "", shared.Validation(map[string]string{"identifier": "This account has no email address yet. Ask your administrator to add one."})
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
			ActorID: &identityID, Action: "auth.password.reset_requested", EntityType: "identity", EntityID: &identityID,
			IP: meta.IP, RequestID: meta.RequestID,
		})
	})
	if err != nil {
		return "", err
	}

	link := s.cfg.BaseURL + "/crm/reset-password?token=" + token
	if err := s.sendNow(ctx, mail.Message{
		To:      email,
		Subject: "Reset your " + s.cfg.AppName + " password",
		Heading: "Reset your password",
		Lines:   []string{"Hi " + name + ", we got a request to reset the password for your " + s.cfg.AppName + " account."},
		Button:  &mail.Button{Label: "Choose a new password", URL: link},
		Footer:  "This link works once and expires in 30 minutes. If you didn't ask for it, ignore this email — your password stays the same.",
	}); err != nil {
		return "", err
	}
	return email, nil
}

func (s *Service) ResetPassword(ctx context.Context, token, newPassword string, meta RequestMeta) error {
	if msg := validateNewPassword(newPassword); msg != "" {
		return shared.Validation(map[string]string{"password": msg})
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	return s.store.WithTx(ctx, func(tx pgx.Tx) error {
		var identityID uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT identity_id FROM crm.password_resets
			WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now()
			FOR UPDATE`, shared.HashToken(strings.TrimSpace(token))).Scan(&identityID)
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.NewError(400, "invalid_reset_token", "This reset link is invalid or has expired. Request a new one.")
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE crm.password_resets SET used_at = now() WHERE token_hash = $1`, shared.HashToken(strings.TrimSpace(token))); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.password_credentials (identity_id, hash) VALUES ($1, $2)
			ON CONFLICT (identity_id) DO UPDATE
			SET hash = EXCLUDED.hash, must_change = false, failed_attempts = 0, locked_until = NULL, updated_at = now()`,
			identityID, hash); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE crm.sessions SET revoked_at = now() WHERE identity_id = $1 AND revoked_at IS NULL`, identityID); err != nil {
			return err
		}
		return shared.WriteAudit(ctx, tx, shared.AuditEvent{
			ActorID: &identityID, Action: "auth.password.reset", EntityType: "identity", EntityID: &identityID,
			IP: meta.IP, RequestID: meta.RequestID,
		})
	})
}

type changeResult struct {
	token string
	Next  string `json:"next"`
}

func (s *Service) ChangePassword(ctx context.Context, sess *Session, current, next string, meta RequestMeta) (*changeResult, error) {
	fields := map[string]string{}
	if current == "" {
		fields["currentPassword"] = "Enter your current password."
	}
	if msg := validateNewPassword(next); msg != "" {
		fields["newPassword"] = msg
	} else if next == current {
		fields["newPassword"] = "Choose a password different from your current one."
	}
	if len(fields) > 0 {
		return nil, shared.Validation(fields)
	}

	var hash string
	if err := s.store.Pool.QueryRow(ctx, `SELECT hash FROM crm.password_credentials WHERE identity_id = $1`, sess.IdentityID).Scan(&hash); err != nil {
		return nil, err
	}
	if !verifyPassword(current, hash) {
		return nil, shared.Validation(map[string]string{"currentPassword": "That isn't your current password."})
	}
	newHash, err := HashPassword(next)
	if err != nil {
		return nil, err
	}

	var out changeResult
	err = s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE crm.password_credentials SET hash = $2, must_change = false, updated_at = now() WHERE identity_id = $1`,
			sess.IdentityID, newHash); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE crm.sessions SET revoked_at = now() WHERE identity_id = $1 AND id <> $2 AND revoked_at IS NULL`,
			sess.IdentityID, sess.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE crm.sessions SET recent_auth_at = now() WHERE id = $1`, sess.ID); err != nil {
			return err
		}
		token, err := rotateToken(ctx, tx, sess.ID)
		if err != nil {
			return err
		}
		out.token = token
		return shared.WriteAudit(ctx, tx, shared.AuditEvent{
			ActorID: &sess.IdentityID, Action: "auth.password.changed", EntityType: "identity", EntityID: &sess.IdentityID,
			IP: meta.IP, RequestID: meta.RequestID,
		})
	})
	if err != nil {
		return nil, err
	}
	out.Next = nextPath(sess.IsPlatformOwner, false, false, false)
	return &out, nil
}

// ---- Logout ----

func (s *Service) Logout(ctx context.Context, sess *Session, all bool, meta RequestMeta) error {
	q := `UPDATE crm.sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`
	arg := sess.ID
	action := "auth.logout"
	if all {
		q = `UPDATE crm.sessions SET revoked_at = now() WHERE identity_id = $1 AND revoked_at IS NULL`
		arg = sess.IdentityID
		action = "auth.logout_all"
	}
	if _, err := s.store.Pool.Exec(ctx, q, arg); err != nil {
		return err
	}
	return shared.WriteAudit(ctx, s.store.Pool, shared.AuditEvent{
		ActorID: &sess.IdentityID, Action: action, EntityType: "identity", EntityID: &sess.IdentityID,
		IP: meta.IP, RequestID: meta.RequestID,
	})
}

// finishSignIn runs everything after the first factor (password or email code):
// account status, audience and workspace checks, MFA requirement, session creation,
// audit and the new-device alert.
func (s *Service) finishSignIn(ctx context.Context, row credentialRow, audience, workspaceCode, identKey, method string, meta RequestMeta) (*loginOutcome, error) {
	if row.status != "active" {
		return nil, shared.Forbidden("account_suspended", "This account is suspended. Contact your administrator.")
	}

	memberships, err := access.ListActiveMemberships(ctx, s.store.Pool, row.identityID)
	if err != nil {
		return nil, err
	}
	switch audience {
	case "owner":
		if !row.isOwner {
			return nil, s.failLogin(ctx, identKey, &row.identityID, meta, "not_owner")
		}
		if method != "password" && method != "otp" && method != "google" && method != "microsoft" && method != "linkedin" {
			return nil, shared.Forbidden("sign_in_method_not_allowed", "The owner console signs in with a password, an email code, Google, Microsoft or LinkedIn.")
		}
	case "workspace":
		if code := strings.ToLower(strings.TrimSpace(workspaceCode)); code != "" && !row.isOwner {
			found := false
			for _, m := range memberships {
				if m.WorkspaceCode == code {
					found = true
				}
			}
			if !found {
				return nil, s.failLogin(ctx, identKey, &row.identityID, meta, "no_membership")
			}
			// The product's setup decides how its users sign in (D-64).
			if err := CheckMethod(ctx, code, method); err != nil {
				return nil, err
			}
		}
		// Self-serve (D-94): a person with no business yet is let in to create one. With
		// self-serve switched off, joining stays by invitation only.
		if !row.isOwner && len(memberships) == 0 && !SelfServeEnabled(ctx) {
			return nil, shared.Forbidden("no_workspace_access", "Your account doesn't have access to a workspace yet. Ask your administrator for an invitation.")
		}
	}
	if method == "phone" && row.isOwner {
		return nil, shared.Forbidden("sign_in_method_not_allowed", "The owner console signs in with a password, an email code, Google, Microsoft or LinkedIn.")
	}

	var mfaEnrolled bool
	if err := s.store.Pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM crm.mfa_methods WHERE identity_id = $1 AND confirmed_at IS NOT NULL)`,
		row.identityID).Scan(&mfaEnrolled); err != nil {
		return nil, err
	}
	privileged := access.IsPrivileged(row.isOwner, memberships)
	mfaRequired := mfaEnrolled || (privileged && s.cfg.MFAEnforced())
	if method == "phone" {
		// The SMS code already proves possession of the phone (D-93): a customer who runs
		// their own business isn't forced into an authenticator app or a 30-minute idle
		// limit. Anyone who enrolled MFA is still challenged.
		mfaRequired = mfaEnrolled
		privileged = false
	}
	if row.isOwner {
		// The owner console signs in without two-step verification (D-133, Ajay's call,
		// 2026-10-09). Everyone else keeps it. Delete this block to switch it back on.
		mfaRequired = false
	}
	sessionAudience := "workspace"
	if row.isOwner {
		sessionAudience = "owner"
	}

	var newDevice bool
	var out loginOutcome
	err = s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			SELECT NOT EXISTS (SELECT 1 FROM crm.sessions WHERE identity_id = $1 AND user_agent = $2)`,
			row.identityID, truncate(meta.UserAgent, 400)).Scan(&newDevice); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE crm.password_credentials SET failed_attempts = 0, locked_until = NULL, updated_at = now()
			WHERE identity_id = $1`, row.identityID); err != nil {
			return err
		}
		token, expires, err := createSession(ctx, tx, newSession{
			identityID:  row.identityID,
			audience:    sessionAudience,
			privileged:  privileged,
			mfaRequired: mfaRequired,
			ip:          meta.IP,
			userAgent:   meta.UserAgent,
			method:      method, // a product that only allows SSO (say) checks this later
			transport:   meta.Transport,
		})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE crm.identities SET last_login_at = now() WHERE id = $1`, row.identityID); err != nil {
			return err
		}
		out = loginOutcome{token: token, expires: expires}
		return shared.WriteAudit(ctx, tx, shared.AuditEvent{
			ActorID: &row.identityID, Action: "auth.login.succeeded", EntityType: "identity", EntityID: &row.identityID,
			After: map[string]any{"method": method, "audience": audience, "mfaRequired": mfaRequired, "userAgent": meta.UserAgent},
			IP:    meta.IP, RequestID: meta.RequestID,
		})
	})
	if err != nil {
		return nil, err
	}
	s.identLimiter.clear(identKey)

	if newDevice {
		s.sendNewDeviceAlert(row.identityID, row.displayName, meta)
	}

	enrollRequired := mfaRequired && !mfaEnrolled
	out.step = AuthStep{
		MFARequired:           mfaRequired,
		MFAEnrollmentRequired: enrollRequired,
		MustChangePassword:    row.mustChange,
		Next:                  nextPath(row.isOwner, mfaRequired, enrollRequired, row.mustChange),
		HasBusiness:           len(memberships) > 0,
	}
	return &out, nil
}

// errAccountNotFound tells the person plainly that no account uses this address (owner's
// choice over enumeration-safe silence; the IP and per-address rate limits still apply).
func errAccountNotFound(kind string) error {
	what := "email"
	if kind == "phone" {
		what = "phone number"
	}
	return &shared.Error{Status: 422, Code: "account_not_found", Message: "No account found for this " + what + ".",
		FieldErrors: map[string]string{"identifier": "No account found for this " + what + ". Check it or ask your administrator for an invitation."}}
}

// accountFor finds an active account by a verified identifier, with clear errors.
func (s *Service) accountFor(ctx context.Context, kind, value string) (uuid.UUID, string, error) {
	var identityID uuid.UUID
	var name, status string
	err := s.store.Pool.QueryRow(ctx, `
		SELECT i.id, i.display_name, i.status FROM crm.verified_identifiers vi
		JOIN crm.identities i ON i.id = vi.identity_id
		WHERE vi.kind = $1 AND vi.namespace = 'global' AND vi.value_normalized = $2
		  AND vi.verified_at IS NOT NULL AND i.status <> 'deleted'`, kind, value).Scan(&identityID, &name, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", errAccountNotFound(kind)
	}
	if err != nil {
		return uuid.Nil, "", err
	}
	if status != "active" {
		return uuid.Nil, "", shared.Forbidden("account_suspended", "This account is suspended. Contact your administrator.")
	}
	return identityID, name, nil
}
