package auth

import (
	"errors"
	"fmt"
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
}

type ClaimedBusiness struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContactName string `json:"contact_name"`
}

type JWTService struct {
	cfg *config.Config
}

func NewJWTService(cfg *config.Config) *JWTService {
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
