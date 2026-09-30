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

// Self sign-up (D-69). A product whose setup allows "Let people sign up themselves"
// accepts new accounts from its sign-in page: the person proves their email with a
// one-time code, then joins the product as an End user. Everyone else still joins by
// invitation. Existing accounts are pointed to sign-in instead.

// SignupTarget is the product a sign-up joins.
type SignupTarget struct {
	WorkspaceID uuid.UUID
	Name        string
	Allowed     bool
}

// SignupPolicy looks up a product by its code and whether it allows self sign-up.
type SignupPolicy func(ctx context.Context, code string) (SignupTarget, error)

var signupPolicy SignupPolicy

func SetSignupPolicy(p SignupPolicy) { signupPolicy = p }

// SignupAllowed reports whether a product code accepts self sign-up.
func SignupAllowed(ctx context.Context, code string) bool {
	if signupPolicy == nil || code == "" {
		return false
	}
	t, err := signupPolicy(ctx, code)
	return err == nil && t.Allowed
}

type SignupInput struct {
	Product  string `json:"product"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Code     string `json:"code"`
	Password string `json:"password"`
}

var errSignupOff = shared.Forbidden("signup_closed", "This product only lets people join by invitation. Ask its administrator to invite you.")

func (s *Service) signupTarget(ctx context.Context, code string) (SignupTarget, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	if signupPolicy == nil || code == "" {
		return SignupTarget{}, errSignupOff
	}
	t, err := signupPolicy(ctx, code)
	if err != nil {
		return SignupTarget{}, err
	}
	if !t.Allowed {
		return SignupTarget{}, errSignupOff
	}
	return t, nil
}

func (s *Service) emailTaken(ctx context.Context, email string) (bool, error) {
	var taken bool
	err := s.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.verified_identifiers v JOIN crm.identities i ON i.id = v.identity_id
		WHERE v.kind = 'email' AND v.namespace = 'global' AND v.value_normalized = $1 AND i.status <> 'deleted')`, email).Scan(&taken)
	return taken, err
}

var errAccountExists = shared.NewError(http.StatusConflict, "account_exists", "You already have an account with this email. Sign in instead — use “Email code” if you don’t have a password.")

// RequestSignup emails a one-time code that proves the address.
func (s *Service) RequestSignup(ctx context.Context, in SignupInput, meta RequestMeta) (*OTPRequestResult, error) {
	t, err := s.signupTarget(ctx, in.Product)
	if err != nil {
		return nil, err
	}
	email, ok := NormalizeEmail(in.Email)
	name := strings.TrimSpace(in.Name)
	fe := map[string]string{}
	if !ok {
		fe["email"] = "Enter a valid email address."
	}
	if name == "" || len(name) > 120 {
		fe["name"] = "Enter your name."
	}
	if len(fe) > 0 {
		return nil, shared.Validation(fe)
	}
	ipKey := "otp-ip:" + meta.IP
	if blocked, wait := s.resetIPLimiter.blocked(ipKey); blocked {
		return nil, shared.TooManyAttempts(int(wait.Seconds()) + 1)
	}
	s.resetIPLimiter.hit(ipKey)
	identKey := "signup:" + email
	if blocked, wait := s.otpLimiter.blocked(identKey); blocked {
		return nil, shared.TooManyAttempts(int(wait.Seconds()) + 1)
	}
	s.otpLimiter.hit(identKey)
	if taken, err := s.emailTaken(ctx, email); err != nil {
		return nil, err
	} else if taken {
		return nil, errAccountExists
	}
	code, err := newOTP()
	if err != nil {
		return nil, err
	}
	if _, err := s.store.Pool.Exec(ctx, `UPDATE crm.otp_challenges SET consumed_at = now() WHERE destination = $1 AND purpose = 'signup' AND consumed_at IS NULL`, email); err != nil {
		return nil, err
	}
	if _, err := s.store.Pool.Exec(ctx, `INSERT INTO crm.otp_challenges (channel, destination, purpose, code_hash, expires_at) VALUES ('email', $1, 'signup', $2, $3)`,
		email, shared.HashToken(email+":signup:"+code), time.Now().Add(otpTTL)); err != nil {
		return nil, err
	}
	if err := s.sendNow(ctx, mail.Message{
		To:      email,
		Subject: code + " is your code to join " + t.Name,
		Heading: "Confirm your email",
		Lines:   []string{"Hi " + name + ", type this code on the sign-up page to create your " + t.Name + " account."},
		Code:    code,
		Footer:  "The code expires in 10 minutes and works once. If you didn't sign up, you can ignore this email.",
	}); err != nil {
		return nil, err
	}
	res := &OTPRequestResult{Sent: true, ExpiresIn: int(otpTTL.Seconds()), Channel: "email"}
	if s.cfg.IsLocalOrDev() && s.mailer.Mode() == "console" {
		res.DevCode = code
	}
	return res, nil
}

// CompleteSignup checks the code, creates the account and membership, and signs in.
func (s *Service) CompleteSignup(ctx context.Context, in SignupInput, meta RequestMeta) (*loginOutcome, error) {
	product := strings.ToLower(strings.TrimSpace(in.Product))
	t, err := s.signupTarget(ctx, product)
	if err != nil {
		return nil, err
	}
	email, ok := NormalizeEmail(in.Email)
	name := strings.TrimSpace(in.Name)
	code := strings.TrimSpace(in.Code)
	fe := map[string]string{}
	if !ok {
		fe["email"] = "Enter a valid email address."
	}
	if name == "" || len(name) > 120 {
		fe["name"] = "Enter your name."
	}
	if len(code) != 6 {
		fe["code"] = "Enter the 6-digit code from the email."
	}
	if in.Password != "" {
		if msg := validateNewPassword(in.Password); msg != "" {
			fe["password"] = msg
		}
	}
	if len(fe) > 0 {
		return nil, shared.Validation(fe)
	}
	identKey := "email:global:" + email
	if blocked, wait := s.identLimiter.blocked(identKey); blocked {
		return nil, shared.TooManyAttempts(int(wait.Seconds()) + 1)
	}
	var challengeID uuid.UUID
	var hash []byte
	var attempts int
	err = s.store.Pool.QueryRow(ctx, `SELECT id, code_hash, attempts FROM crm.otp_challenges
		WHERE destination = $1 AND purpose = 'signup' AND consumed_at IS NULL AND expires_at > now() ORDER BY created_at DESC LIMIT 1`, email).
		Scan(&challengeID, &hash, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		s.identLimiter.hit(identKey)
		return nil, errBadCode
	}
	if err != nil {
		return nil, err
	}
	if attempts >= otpMaxAttempts || !shared.ConstantTimeEqual(string(hash), string(shared.HashToken(email+":signup:"+code))) {
		_, _ = s.store.Pool.Exec(ctx, `UPDATE crm.otp_challenges SET attempts = attempts + 1,
			consumed_at = CASE WHEN attempts + 1 >= $2 THEN now() ELSE consumed_at END WHERE id = $1`, challengeID, otpMaxAttempts)
		s.identLimiter.hit(identKey)
		return nil, errBadCode
	}
	var pwHash string
	if in.Password != "" {
		if pwHash, err = HashPassword(in.Password); err != nil {
			return nil, err
		}
	}
	var id uuid.UUID
	err = s.store.WithTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE crm.otp_challenges SET consumed_at = now() WHERE id = $1 AND consumed_at IS NULL`, challengeID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errBadCode
		}
		var taken bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.verified_identifiers WHERE kind = 'email' AND namespace = 'global' AND value_normalized = $1)`, email).Scan(&taken); err != nil {
			return err
		}
		if taken {
			return errAccountExists
		}
		if err := tx.QueryRow(ctx, `INSERT INTO crm.identities (display_name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO crm.verified_identifiers (identity_id, kind, value_normalized, namespace, verified_at) VALUES ($1, 'email', $2, 'global', now())`, id, email); err != nil {
			return err
		}
		if pwHash != "" {
			if _, err := tx.Exec(ctx, `INSERT INTO crm.password_credentials (identity_id, hash) VALUES ($1, $2)`, id, pwHash); err != nil {
				return err
			}
		}
		var mid, roleID uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO crm.memberships (workspace_id, identity_id, status) VALUES ($1, $2, 'active') RETURNING id`, t.WorkspaceID, id).Scan(&mid); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT id FROM crm.roles WHERE workspace_id = $1 AND key = 'END_USER'`, t.WorkspaceID).Scan(&roleID); err != nil {
			return errors.New("this product has no End user role")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO crm.role_assignments (workspace_id, membership_id, role_id, product_ids, granted_by) VALUES ($1, $2, $3, '{}', $4)`,
			t.WorkspaceID, mid, roleID, id); err != nil {
			return err
		}
		ws := t.WorkspaceID
		return shared.WriteAudit(ctx, tx, shared.AuditEvent{WorkspaceID: &ws, ActorID: &id, Action: "auth.signed_up", EntityType: "identity", EntityID: &id,
			IP: meta.IP, RequestID: meta.RequestID, After: map[string]any{"email": email, "role": "END_USER"}})
	})
	if err != nil {
		return nil, err
	}
	s.identLimiter.clear(identKey)
	row := credentialRow{identityID: id, displayName: name, status: "active"}
	return s.finishSignIn(ctx, row, "workspace", product, identKey, "otp", meta)
}

func (s *Service) handleSignupRequest(w http.ResponseWriter, r *http.Request) {
	var in SignupInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	res, err := s.RequestSignup(r.Context(), in, Meta(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, res)
}

func (s *Service) handleSignupVerify(w http.ResponseWriter, r *http.Request) {
	var in SignupInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	out, err := s.CompleteSignup(r.Context(), in, Meta(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	s.setSessionCookie(w, out.token, out.expires)
	shared.WriteJSON(w, http.StatusOK, out.step)
}
