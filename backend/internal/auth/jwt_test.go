package auth

import (
	"context"
	"testing"

	"cardflow-backend/internal/config"
	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/domain"
	"github.com/google/uuid"
)

func TestJWTGenerationAndValidation(t *testing.T) {
	cfg := &config.Config{
		JWTPrivateKey:      "test-secret-key-for-unit-testing-32-bytes",
		JWTIssuer:          "cardflow.test",
		JWTAccessExpiryMin: 15,
	}

	jwtSvc := NewJWTService(cfg)

	user := &domain.User{
		ID:    uuid.New(),
		Phone: "+919876543210",
		Role:  domain.RoleUser,
		Plan:  domain.PlanPlus,
	}

	pair, err := jwtSvc.GenerateTokenPair(user, "dev-device-123")
	if err != nil {
		t.Fatalf("Failed to generate token pair: %v", err)
	}

	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("Expected non-empty token pair")
	}

	claims, err := jwtSvc.ValidateAccessToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("Failed to validate access token: %v", err)
	}

	if claims.UserID != user.ID.String() {
		t.Errorf("Expected UserID %s, got %s", user.ID.String(), claims.UserID)
	}
	if claims.Role != string(domain.RoleUser) {
		t.Errorf("Expected Role %s, got %s", domain.RoleUser, claims.Role)
	}
}

// The app keeps no one-time codes of its own any more (D-93): without the identity
// service there is no way to request or check a code, and nothing is ever "accepted".
func TestSignInNeedsIdentityService(t *testing.T) {
	cfg := &config.Config{Env: "development", JWTPrivateKey: "secret"}
	authSvc := NewAuthService(nil, nil, NewJWTService(cfg), cfg)

	if _, err := authSvc.SendOTP(context.Background(), "9876543211", identity.RequestMeta{}); err == nil {
		t.Fatal("expected SendOTP to fail without the identity service")
	}
	for _, code := range []string{"123456", "000000"} {
		if _, err := authSvc.VerifyOTP(context.Background(), "9876543211", code, "device1", "web", "", identity.RequestMeta{}); err == nil {
			t.Fatalf("code %s must not be accepted without the identity service", code)
		}
	}
}

// A production server must not sign tokens with the key that ships in the source.
func TestProductionRefusesBuiltInJWTKey(t *testing.T) {
	cfg := &config.Config{Env: "production", JWTPrivateKey: devJWTSecret}
	NewJWTService(cfg)
	if cfg.JWTPrivateKey == devJWTSecret || len(cfg.JWTPrivateKey) < 32 {
		t.Fatal("production kept the built-in JWT key")
	}
}
