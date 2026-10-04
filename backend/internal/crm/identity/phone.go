package identity

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
	"cardflow-backend/internal/crm/sms"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Phone sign-in (D-93). A customer signs in with a mobile number and a 6-digit code sent
// by SMS: 10 minutes, 5 attempts, single use, stored hashed. The first successful code for
// a number creates the identity, so signing up and signing in are one flow. The code is
// never logged and never returned, except by the preview sender, which exists only when
// it is switched on explicitly (see sms.LoadConfig).

// AppProfile keeps the app's own profile row (public.users) in step with the identity. It
// is installed by the module at start-up, so identity doesn't depend on the app's tables.
type AppProfile interface {
	// SignedIn runs inside the sign-in transaction. It links (or creates) the profile row
	// and returns anything the app should hear about, e.g. businesses claimed from cards.
	SignedIn(ctx context.Context, tx pgx.Tx, identityID uuid.UUID, phone, name string, created bool) (map[string]any, error)
	PhoneChanged(ctx context.Context, tx pgx.Tx, identityID uuid.UUID, phone string) error
	Renamed(ctx context.Context, tx pgx.Tx, identityID uuid.UUID, name string) error
}

// SetAppProfile installs the app profile link.
func (s *Service) SetAppProfile(p AppProfile) { s.appProfile = p }

const (
	purposePhoneLogin  = "login"
	purposePhoneChange = "change_phone"
	newUserName        = "New user"
)

type PhoneRequestInput struct {
	Phone string `json:"phone"`
}

type PhoneVerifyInput struct {
	Phone string `json:"phone"`
	Code  string `json:"code"`
	Name  string `json:"name"`
}

var (
	errSMSUnavailable = shared.ServiceUnavailable("sms_unavailable", "We can't send text messages right now, so sign-in by phone isn't available. Please try again later.")
	errSMSFailed      = shared.NewError(http.StatusBadGateway, "sms_failed", "We couldn't send the code to that number. Check the number and try again in a minute.")
	errBadPhone       = shared.Validation(map[string]string{"phone": "Enter a valid mobile number, with the country code if it isn't an Indian number."})
)

func phoneHash(phone, purpose, code string) []byte {
	return shared.HashToken(phone + ":" + purpose + ":" + code)
}

func maskPhone(phone string) string {
	if len(phone) <= 4 {
		return "••••"
	}
	return strings.Repeat("•", len(phone)-4) + phone[len(phone)-4:]
}

// sendPhoneCode creates a challenge for the number and sends it. One live code per number
// and purpose: asking again cancels the previous one.
func (s *Service) sendPhoneCode(ctx context.Context, phone, purpose string, actor *uuid.UUID, meta RequestMeta) (*OTPRequestResult, error) {
	if s.sms.Mode() == "none" {
		return nil, errSMSUnavailable
	}
	ipKey := "phone-ip:" + meta.IP
	if blocked, wait := s.phoneIPLimiter.blocked(ipKey); blocked {
		return nil, shared.TooManyAttempts(int(wait.Seconds()) + 1)
	}
	for _, l := range []*limiter{s.phoneLimiter, s.phoneDayLimiter} {
		if blocked, wait := l.blocked("phone:" + phone); blocked {
			return nil, shared.TooManyAttempts(int(wait.Seconds()) + 1)
		}
	}
	s.phoneIPLimiter.hit(ipKey)
	s.phoneLimiter.hit("phone:" + phone)
	s.phoneDayLimiter.hit("phone:" + phone)

	code, err := newOTP()
	if err != nil {
		return nil, err
	}
	if _, err := s.store.Pool.Exec(ctx, `
		UPDATE crm.otp_challenges SET consumed_at = now() WHERE destination = $1 AND purpose = $2 AND consumed_at IS NULL`, phone, purpose); err != nil {
		return nil, err
	}
	var challengeID uuid.UUID
	if err := s.store.Pool.QueryRow(ctx, `
		INSERT INTO crm.otp_challenges (channel, destination, purpose, code_hash, expires_at) VALUES ('sms', $1, $2, $3, $4) RETURNING id`,
		phone, purpose, phoneHash(phone, purpose, code), time.Now().Add(otpTTL)).Scan(&challengeID); err != nil {
		return nil, err
	}
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if err := s.sms.SendCode(sendCtx, phone, code); err != nil {
		// Never pretend it was sent: cancel the code and say so.
		_, _ = s.store.Pool.Exec(ctx, `UPDATE crm.otp_challenges SET consumed_at = now() WHERE id = $1`, challengeID)
		slog.Error("crm: sign-in code not sent", "provider", s.sms.Mode(), "phone", maskPhone(phone), "error", err)
		if errors.Is(err, sms.ErrUnavailable) {
			return nil, errSMSUnavailable
		}
		return nil, errSMSFailed
	}
	_ = shared.WriteAudit(ctx, s.store.Pool, shared.AuditEvent{ActorID: actor, Action: "auth.phone_code.requested", EntityType: "phone",
		IP: meta.IP, RequestID: meta.RequestID, After: map[string]any{"phone": maskPhone(phone), "purpose": purpose, "provider": s.sms.Mode()}})
	res := &OTPRequestResult{Sent: true, ExpiresIn: int(otpTTL.Seconds()), Channel: "sms"}
	if sms.IsPreview(s.sms) {
		res.DevCode = code // preview sender only: shown on screen because nothing is sent
	}
	return res, nil
}

// checkPhoneCode validates the newest live code for the number without consuming it.
func (s *Service) checkPhoneCode(ctx context.Context, phone, purpose, code string) (uuid.UUID, error) {
	identKey := "phone:global:" + phone
	if blocked, wait := s.identLimiter.blocked(identKey); blocked {
		return uuid.Nil, shared.TooManyAttempts(int(wait.Seconds()) + 1)
	}
	var challengeID uuid.UUID
	var hash []byte
	var attempts int
	err := s.store.Pool.QueryRow(ctx, `
		SELECT id, code_hash, attempts FROM crm.otp_challenges
		WHERE destination = $1 AND purpose = $2 AND channel = 'sms' AND consumed_at IS NULL AND expires_at > now()
		ORDER BY created_at DESC LIMIT 1`, phone, purpose).Scan(&challengeID, &hash, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		s.identLimiter.hit(identKey)
		return uuid.Nil, errBadCode
	}
	if err != nil {
		return uuid.Nil, err
	}
	if attempts >= otpMaxAttempts || !shared.ConstantTimeEqual(string(hash), string(phoneHash(phone, purpose, code))) {
		_, _ = s.store.Pool.Exec(ctx, `UPDATE crm.otp_challenges SET attempts = attempts + 1,
			consumed_at = CASE WHEN attempts + 1 >= $2 THEN now() ELSE consumed_at END WHERE id = $1`, challengeID, otpMaxAttempts)
		if s.identLimiter.hit(identKey) {
			return uuid.Nil, shared.TooManyAttempts(int(lockDuration.Seconds()))
		}
		return uuid.Nil, errBadCode
	}
	return challengeID, nil
}

// consumeChallenge makes a code single use; a second use fails.
func consumeChallenge(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	tag, err := tx.Exec(ctx, `UPDATE crm.otp_challenges SET consumed_at = now() WHERE id = $1 AND consumed_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errBadCode
	}
	return nil
}

// RequestPhoneOTP sends a sign-in code to a mobile number.
func (s *Service) RequestPhoneOTP(ctx context.Context, in PhoneRequestInput, meta RequestMeta) (*OTPRequestResult, error) {
	phone, ok := normalizePhone(strings.TrimSpace(in.Phone))
	if !ok {
		return nil, errBadPhone
	}
	return s.sendPhoneCode(ctx, phone, purposePhoneLogin, nil, meta)
}

// PhoneProof is the result of a correct code: who the person is, before any session exists.
type PhoneProof struct {
	IdentityID uuid.UUID
	Phone      string
	Created    bool           // this code created the account
	App        map[string]any // what the app profile link reported
	row        credentialRow
	identKey   string
}

// ProvePhone checks a sign-in code and returns the identity it belongs to, creating the
// identity when the number is new. It does not start a session.
func (s *Service) ProvePhone(ctx context.Context, in PhoneVerifyInput, meta RequestMeta) (*PhoneProof, error) {
	phone, ok := normalizePhone(strings.TrimSpace(in.Phone))
	code := strings.TrimSpace(in.Code)
	if !ok {
		return nil, errBadPhone
	}
	if len(code) != 6 {
		return nil, shared.Validation(map[string]string{"code": "Enter the 6-digit code we sent you."})
	}
	name := strings.TrimSpace(in.Name)
	if len(name) > 120 {
		name = name[:120]
	}
	challengeID, err := s.checkPhoneCode(ctx, phone, purposePhoneLogin, code)
	if err != nil {
		return nil, err
	}
	proof := &PhoneProof{Phone: phone, identKey: "phone:global:" + phone}
	err = s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := consumeChallenge(ctx, tx, challengeID); err != nil {
			return err
		}
		var identifierID uuid.UUID
		row := credentialRow{}
		err := tx.QueryRow(ctx, `
			SELECT i.id, i.display_name, i.is_platform_owner, i.status, vi.id
			FROM crm.verified_identifiers vi JOIN crm.identities i ON i.id = vi.identity_id
			WHERE vi.kind = 'phone' AND vi.namespace = 'global' AND vi.value_normalized = $1
			FOR UPDATE OF vi`, phone).Scan(&row.identityID, &row.displayName, &row.isOwner, &row.status, &identifierID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if name == "" {
				name = newUserName
			}
			if err := tx.QueryRow(ctx, `INSERT INTO crm.identities (display_name, source) VALUES ($1, 'phone_signup') RETURNING id`, name).Scan(&row.identityID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO crm.verified_identifiers (identity_id, kind, value_normalized, namespace, verified_at) VALUES ($1, 'phone', $2, 'global', now())`,
				row.identityID, phone); err != nil {
				return err
			}
			row.displayName, row.status, proof.Created = name, "active", true
			if err := shared.WriteAudit(ctx, tx, shared.AuditEvent{ActorID: &row.identityID, Action: "auth.signed_up", EntityType: "identity", EntityID: &row.identityID,
				IP: meta.IP, RequestID: meta.RequestID, After: map[string]any{"method": "phone", "phone": maskPhone(phone)}}); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			if row.status == "deleted" {
				return shared.Forbidden("account_deleted", "This account was deleted. Contact support to use this number again.")
			}
			// The code proves the number, whether or not it was proved before.
			if _, err := tx.Exec(ctx, `UPDATE crm.verified_identifiers SET verified_at = COALESCE(verified_at, now()) WHERE id = $1`, identifierID); err != nil {
				return err
			}
		}
		if s.appProfile != nil {
			app, err := s.appProfile.SignedIn(ctx, tx, row.identityID, phone, name, proof.Created)
			if err != nil {
				return err
			}
			proof.App = app
		}
		proof.IdentityID, proof.row = row.identityID, row
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.identLimiter.clear(proof.identKey)
	return proof, nil
}

// VerifyPhoneOTP checks the code and signs the person in.
func (s *Service) VerifyPhoneOTP(ctx context.Context, in PhoneVerifyInput, meta RequestMeta) (*loginOutcome, error) {
	proof, err := s.ProvePhone(ctx, in, meta)
	if err != nil {
		return nil, err
	}
	out, err := s.finishSignIn(ctx, proof.row, "workspace", "", proof.identKey, "phone", meta)
	if err != nil {
		return nil, err
	}
	out.step.IsNewUser = proof.Created || proof.row.displayName == newUserName
	out.step.App = proof.App
	return out, nil
}

func (s *Service) handlePhoneRequest(w http.ResponseWriter, r *http.Request) {
	var in PhoneRequestInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	res, err := s.RequestPhoneOTP(r.Context(), in, Meta(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, res)
}

func (s *Service) handlePhoneVerify(w http.ResponseWriter, r *http.Request) {
	var in PhoneVerifyInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	out, err := s.VerifyPhoneOTP(r.Context(), in, Meta(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	s.deliverSession(w, r, out.token, out.expires, &out.step)
	shared.WriteJSON(w, http.StatusOK, out.step)
}

// ---- changing the number on an account ----

// RequestPhoneChange sends a code to the new number of a signed-in person.
func (s *Service) RequestPhoneChange(ctx context.Context, identityID uuid.UUID, rawPhone string, meta RequestMeta) (*OTPRequestResult, error) {
	phone, ok := normalizePhone(strings.TrimSpace(rawPhone))
	if !ok {
		return nil, errBadPhone
	}
	var owner uuid.UUID
	err := s.store.Pool.QueryRow(ctx, `SELECT identity_id FROM crm.verified_identifiers WHERE kind = 'phone' AND namespace = 'global' AND value_normalized = $1`, phone).Scan(&owner)
	if err == nil && owner != identityID {
		return nil, shared.Validation(map[string]string{"phone": "Another account already uses this number."})
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	return s.sendPhoneCode(ctx, phone, purposePhoneChange, &identityID, meta)
}

// ChangePhone moves the account to a new number once the code sent there is confirmed.
func (s *Service) ChangePhone(ctx context.Context, identityID uuid.UUID, rawPhone, code string, meta RequestMeta) (string, error) {
	phone, ok := normalizePhone(strings.TrimSpace(rawPhone))
	if !ok {
		return "", errBadPhone
	}
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return "", shared.Validation(map[string]string{"code": "Enter the 6-digit code we sent you."})
	}
	challengeID, err := s.checkPhoneCode(ctx, phone, purposePhoneChange, code)
	if errors.Is(err, errBadCode) {
		// Older app builds ask for the code through the sign-in request.
		challengeID, err = s.checkPhoneCode(ctx, phone, purposePhoneLogin, code)
	}
	if err != nil {
		return "", err
	}
	err = s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := consumeChallenge(ctx, tx, challengeID); err != nil {
			return err
		}
		var owner uuid.UUID
		err := tx.QueryRow(ctx, `SELECT identity_id FROM crm.verified_identifiers WHERE kind = 'phone' AND namespace = 'global' AND value_normalized = $1`, phone).Scan(&owner)
		if err == nil && owner != identityID {
			return shared.Validation(map[string]string{"phone": "Another account already uses this number."})
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM crm.verified_identifiers WHERE identity_id = $1 AND kind = 'phone' AND namespace = 'global' AND value_normalized <> $2`, identityID, phone); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.verified_identifiers (identity_id, kind, value_normalized, namespace, verified_at) VALUES ($1, 'phone', $2, 'global', now())
			ON CONFLICT (kind, namespace, value_normalized) DO UPDATE SET verified_at = COALESCE(crm.verified_identifiers.verified_at, now())`, identityID, phone); err != nil {
			return err
		}
		if s.appProfile != nil {
			if err := s.appProfile.PhoneChanged(ctx, tx, identityID, phone); err != nil {
				return err
			}
		}
		return shared.WriteAudit(ctx, tx, shared.AuditEvent{ActorID: &identityID, Action: "identity.phone_changed", EntityType: "identity", EntityID: &identityID,
			IP: meta.IP, RequestID: meta.RequestID, After: map[string]any{"phone": maskPhone(phone)}})
	})
	if err != nil {
		return "", err
	}
	s.identLimiter.clear("phone:global:" + phone)
	return phone, nil
}

func (s *Service) handleChangePhoneRequest(w http.ResponseWriter, r *http.Request) {
	var in PhoneRequestInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	res, err := s.RequestPhoneChange(r.Context(), SessionFrom(r.Context()).IdentityID, in.Phone, Meta(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, res)
}

func (s *Service) handleChangePhoneVerify(w http.ResponseWriter, r *http.Request) {
	var in PhoneVerifyInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	phone, err := s.ChangePhone(r.Context(), SessionFrom(r.Context()).IdentityID, in.Phone, in.Code, Meta(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]string{"phone": phone})
}

// ---- profile and session upkeep ----

// UpdateMeInput changes the signed-in person's own profile. Empty fields are left alone.
type UpdateMeInput struct {
	DisplayName *string `json:"displayName"`
	Locale      *string `json:"locale"`
	Timezone    *string `json:"timezone"`
}

func (s *Service) UpdateMe(ctx context.Context, sess *Session, in UpdateMeInput, meta RequestMeta) error {
	fe := map[string]string{}
	name := ""
	if in.DisplayName != nil {
		name = strings.TrimSpace(*in.DisplayName)
		if name == "" || len(name) > 120 {
			fe["displayName"] = "Enter your name (up to 120 characters)."
		}
	}
	if in.Timezone != nil {
		if _, err := time.LoadLocation(strings.TrimSpace(*in.Timezone)); err != nil || strings.TrimSpace(*in.Timezone) == "" {
			fe["timezone"] = "Choose a time zone from the list."
		}
	}
	if in.Locale != nil && (len(*in.Locale) < 2 || len(*in.Locale) > 10) {
		fe["locale"] = "Choose a language from the list."
	}
	if len(fe) > 0 {
		return shared.Validation(fe)
	}
	return s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE crm.identities SET display_name = COALESCE($2, display_name), locale = COALESCE($3, locale),
			       timezone = COALESCE($4, timezone), updated_at = now() WHERE id = $1`,
			sess.IdentityID, nullable(in.DisplayName, name), trimPtr(in.Locale), trimPtr(in.Timezone)); err != nil {
			return err
		}
		if in.DisplayName != nil && s.appProfile != nil {
			if err := s.appProfile.Renamed(ctx, tx, sess.IdentityID, name); err != nil {
				return err
			}
		}
		return shared.WriteAudit(ctx, tx, shared.AuditEvent{ActorID: &sess.IdentityID, Action: "identity.profile_updated", EntityType: "identity",
			EntityID: &sess.IdentityID, IP: meta.IP, RequestID: meta.RequestID})
	})
}

func nullable(p *string, v string) *string {
	if p == nil {
		return nil
	}
	return &v
}

func trimPtr(p *string) *string {
	if p == nil {
		return nil
	}
	v := strings.TrimSpace(*p)
	return &v
}

func (s *Service) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	var in UpdateMeInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	sess := SessionFrom(r.Context())
	if err := s.UpdateMe(r.Context(), sess, in, Meta(r)); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	out, err := s.Me(r.Context(), sess)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, out)
}

// POST /auth/session/renew — rotates the token of a live session, so a token that leaked
// stops working at the app's next renewal. The session's hard limit doesn't move.
func (s *Service) handleRenewSession(w http.ResponseWriter, r *http.Request) {
	sess := SessionFrom(r.Context())
	if sess.Audience == "api" {
		shared.WriteError(w, r, shared.Forbidden("forbidden", "API keys can't be renewed here."))
		return
	}
	token, err := rotateToken(r.Context(), s.store.Pool, sess.ID)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	step := AuthStep{Next: "", HasBusiness: true}
	s.deliverSession(w, r, token, sess.AbsoluteExpiresAt, &step)
	shared.WriteJSON(w, http.StatusOK, map[string]any{"token": step.Token, "expiresAt": sess.AbsoluteExpiresAt})
}

// LookupBearer resolves a raw "crms_…" token to its live session (nil when it isn't one).
// The app's own API uses it so one sign-in works for both APIs.
func (s *Service) LookupBearer(ctx context.Context, raw string) (*Session, error) {
	if !strings.HasPrefix(raw, BearerPrefix) {
		return nil, nil
	}
	sess, err := s.lookupSession(ctx, strings.TrimPrefix(raw, BearerPrefix))
	if err != nil || sess == nil || sess.Transport != "bearer" {
		return nil, err
	}
	s.touchSession(ctx, sess)
	return sess, nil
}

// StartBearerSession signs a proved phone in and returns a bearer token (with its prefix).
func (s *Service) StartBearerSession(ctx context.Context, proof *PhoneProof, meta RequestMeta) (string, time.Time, bool, error) {
	meta.Transport = "bearer"
	out, err := s.finishSignIn(ctx, proof.row, "workspace", "", proof.identKey, "phone", meta)
	if err != nil {
		return "", time.Time{}, false, err
	}
	return BearerPrefix + out.token, out.expires, out.step.HasBusiness, nil
}
