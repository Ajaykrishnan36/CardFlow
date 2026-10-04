package auth

import (
	"context"
	"errors"

	"cardflow-backend/internal/config"
	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/shared"
	"cardflow-backend/internal/database"
	"cardflow-backend/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type AuthService struct {
	cfg   *config.Config
	db    *database.DB
	jwt   *JWTService
	ident *identity.Service
}

func NewAuthService(db *database.DB, redis *database.RedisClient, jwt *JWTService, cfg *config.Config) *AuthService {
	return &AuthService{cfg: cfg, db: db, jwt: jwt}
}

// SetIdentity connects the app's sign-in to the unified identity service (D-93). The app
// no longer keeps its own one-time codes: a code is created, sent, limited and checked in
// one place, and the same person is the same identity in the app and in the CRM.
func (s *AuthService) SetIdentity(i *identity.Service) { s.ident = i }

var errSignInUnavailable = shared.ServiceUnavailable("sign_in_unavailable", "Sign-in is temporarily unavailable. Please try again in a few minutes.")

// SendOTP asks the identity service to send a sign-in code. The code itself is returned
// (as otp_preview) only by the preview sender, which exists only when switched on explicitly.
func (s *AuthService) SendOTP(ctx context.Context, rawPhone string, meta identity.RequestMeta) (map[string]interface{}, error) {
	if s.ident == nil {
		return nil, errSignInUnavailable
	}
	res, err := s.ident.RequestPhoneOTP(ctx, identity.PhoneRequestInput{Phone: rawPhone}, meta)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{"success": true, "message": "OTP sent successfully", "expires_in": res.ExpiresIn}
	if res.DevCode != "" {
		out["otp_preview"] = res.DevCode
	}
	return out, nil
}

// VerifyOTP checks the code with the identity service, loads the person's app profile and
// issues the app's JWT (for older app builds) together with a unified session token.
func (s *AuthService) VerifyOTP(ctx context.Context, rawPhone, otpCode, deviceID, platform, pushToken string, meta identity.RequestMeta) (*TokenPair, error) {
	if s.ident == nil {
		return nil, errSignInUnavailable
	}
	if s.db == nil || s.db.Pool == nil {
		return nil, errors.New("database unavailable — cannot sign in without PostgreSQL")
	}
	proof, err := s.ident.ProvePhone(ctx, identity.PhoneVerifyInput{Phone: rawPhone, Code: otpCode}, meta)
	if err != nil {
		return nil, err
	}
	user, err := s.userForIdentity(ctx, proof.IdentityID)
	if err != nil {
		return nil, err
	}

	if deviceID != "" {
		_, _ = s.db.Pool.Exec(ctx, `
			INSERT INTO devices (user_id, platform, device_id, push_token, last_seen)
			VALUES ($1, $2, $3, $4, NOW())
			ON CONFLICT (user_id, device_id)
			DO UPDATE SET push_token = COALESCE(EXCLUDED.push_token, devices.push_token), last_seen = NOW()
		`, user.ID, platform, deviceID, pushToken)
	}

	tokenPair, err := s.jwt.GenerateTokenPair(user, deviceID)
	if err != nil {
		return nil, err
	}
	isNewProfile, _ := proof.App["isNewProfile"].(bool)
	tokenPair.IsNewUser = isNewProfile || user.Name == "CardFlow User" || user.Name == ""
	if claimed, ok := proof.App["claimedBusinesses"].([]map[string]string); ok {
		for _, b := range claimed {
			tokenPair.ClaimedBusinesses = append(tokenPair.ClaimedBusinesses, ClaimedBusiness{ID: b["id"], Name: b["name"], ContactName: b["contact_name"]})
		}
	}
	if tokenPair.IsNewUser {
		if n, ok := proof.App["suggestedName"].(string); ok {
			tokenPair.SuggestedName = n
		}
	}
	token, expires, hasBusiness, err := s.ident.StartBearerSession(ctx, proof, meta)
	if err != nil {
		return nil, err
	}
	tokenPair.SessionToken, tokenPair.SessionExpiresAt, tokenPair.HasBusiness = token, &expires, hasBusiness
	return tokenPair, nil
}

const userColumns = `id, phone, COALESCE(name, ''), email, photo_url, COALESCE(city, ''), COALESCE(state, ''), country,
	role::text, plan::text, free_scans_remaining, free_scans_reset_at, status::text, created_at, updated_at,
	is_subscribed, subscription_plan_id, subscription_expires_at`

func scanUser(row pgx.Row) (*domain.User, error) {
	var u domain.User
	var roleStr, planStr string
	if err := row.Scan(&u.ID, &u.Phone, &u.Name, &u.Email, &u.PhotoURL, &u.City, &u.State, &u.Country,
		&roleStr, &planStr, &u.FreeScansRemaining, &u.FreeScansResetAt, &u.Status, &u.CreatedAt, &u.UpdatedAt,
		&u.IsSubscribed, &u.SubscriptionPlanID, &u.SubscriptionExpiresAt); err != nil {
		return nil, err
	}
	u.Role = domain.UserRole(roleStr)
	u.Plan = domain.SubscriptionPlan(planStr)
	return &u, nil
}

// userForIdentity loads the app profile linked to an identity (the identity service
// creates or links it during sign-in).
func (s *AuthService) userForIdentity(ctx context.Context, identityID uuid.UUID) (*domain.User, error) {
	u, err := scanUser(s.db.Pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE identity_id = $1 AND deleted_at IS NULL`, identityID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("your profile could not be loaded. Please try again")
	}
	if err != nil {
		return nil, err
	}
	var kycStatus string
	_ = s.db.Pool.QueryRow(ctx, `SELECT aadhaar_status::text FROM user_kyc WHERE user_id = $1`, u.ID).Scan(&kycStatus)
	u.IsIDVerified = kycStatus == "verified"
	return u, nil
}

// UserForIdentity is userForIdentity for the request middleware.
func (s *AuthService) UserForIdentity(ctx context.Context, identityID uuid.UUID) (*domain.User, error) {
	return s.userForIdentity(ctx, identityID)
}
