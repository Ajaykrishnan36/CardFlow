package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"cardflow-backend/internal/config"
	"cardflow-backend/internal/domain"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type Claims struct {
	UserID string `json:"sub"`
	Phone  string `json:"phone"`
	Role   string `json:"role"`
	Plan   string `json:"plan"`
	jwt.RegisteredClaims
}

type TokenPair struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	User         *domain.User `json:"user"`
	IsNewUser    bool         `json:"is_new_user"`
	// ClaimedBusinesses are card-created businesses that became this user's
	// at this sign-in because the phone on the card is theirs.
	ClaimedBusinesses []ClaimedBusiness `json:"claimed_businesses,omitempty"`
	// SuggestedName pre-fills onboarding for a new user (the name on their card).
	SuggestedName string `json:"suggested_name,omitempty"`
	// SessionToken is the unified session (D-93): the same sign-in works for the CRM API
	// and, as "Authorization: Bearer <token>", for this API. It can be revoked; the JWT
	// above is kept only for app builds from before the identities were unified.
	SessionToken     string     `json:"session_token,omitempty"`
	SessionExpiresAt *time.Time `json:"session_expires_at,omitempty"`
	// HasBusiness: the person already belongs to a business; false sends them to create one.
	HasBusiness bool `json:"has_business"`
}

type ClaimedBusiness struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContactName string `json:"contact_name"`
}

type JWTService struct {
	cfg *config.Config
}

// devJWTSecret is the built-in key from config. It is public (it is in the source), so a
// production server must never sign with it.
const devJWTSecret = "cardflow-dev-secret-key-ed25519-placeholder-for-dev"

func NewJWTService(cfg *config.Config) *JWTService {
	if cfg.IsProduction() && (cfg.JWTPrivateKey == devJWTSecret || len(cfg.JWTPrivateKey) < 32) {
		// Refuse the known or a short key: sign with a random one for this run. Tokens stop
		// working at the next restart, which is the safe failure. Set JWT_PRIVATE_KEY to a
		// long random value to fix it.
		buf := make([]byte, 48)
		if _, err := rand.Read(buf); err == nil {
			cfg.JWTPrivateKey = hex.EncodeToString(buf)
			slog.Error("JWT_PRIVATE_KEY is missing, the built-in default or shorter than 32 characters; using a random key for this run. Set a long random JWT_PRIVATE_KEY")
		}
	}
	return &JWTService{cfg: cfg}
}

func (j *JWTService) GenerateTokenPair(user *domain.User, deviceID string) (*TokenPair, error) {
	now := time.Now()
	accessExpiry := now.Add(time.Duration(j.cfg.JWTAccessExpiryMin) * time.Minute)

	tokenID := uuid.New().String()
	claims := Claims{
		UserID: user.ID.String(),
		Phone:  user.Phone,
		Role:   string(user.Role),
		Plan:   string(user.Plan),
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        tokenID,
			Issuer:    j.cfg.JWTIssuer,
			Subject:   user.ID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(accessExpiry),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	accessToken, err := token.SignedString([]byte(j.cfg.JWTPrivateKey))
	if err != nil {
		return nil, fmt.Errorf("failed signing access token: %w", err)
	}

	// Generate opaque refresh token
	refreshToken := fmt.Sprintf("cf_refr_%s_%s", user.ID.String()[:8], uuid.New().String())

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		User:         user,
	}, nil
}

func (j *JWTService) ValidateAccessToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(j.cfg.JWTPrivateKey), nil
	})

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token claims")
	}

	return claims, nil
}
