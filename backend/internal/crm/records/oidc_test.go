package records

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestVerifyIDToken(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	d := &oidcDiscovery{Issuer: "https://idp.example.com", keys: map[string]any{"k1": &key.PublicKey}, keysAt: time.Now()}
	p := &OIDCProvider{ClientID: "crm", Discovery: d}
	h := &Handler{}
	mk := func(k *rsa.PrivateKey, mod func(jwt.MapClaims)) string {
		c := jwt.MapClaims{"iss": d.Issuer, "aud": "crm", "sub": "u1", "email": "A@Example.com", "email_verified": true, "nonce": "n1",
			"exp": time.Now().Add(time.Minute).Unix()}
		if mod != nil {
			mod(c)
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodRS256, c)
		tok.Header["kid"] = "k1"
		s, _ := tok.SignedString(k)
		return s
	}
	got, err := h.verifyIDToken(context.Background(), p, mk(key, nil), "n1")
	if err != nil || got.Email != "a@example.com" || got.Subject != "u1" {
		t.Fatalf("valid token: %+v %v", got, err)
	}
	for name, raw := range map[string]string{
		"wrong key":      mk(other, nil),
		"wrong audience": mk(key, func(c jwt.MapClaims) { c["aud"] = "someone-else" }),
		"wrong issuer":   mk(key, func(c jwt.MapClaims) { c["iss"] = "https://evil.example.com" }),
		"expired":        mk(key, func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() }),
		"unverified":     mk(key, func(c jwt.MapClaims) { c["email_verified"] = false }),
	} {
		if _, err := h.verifyIDToken(context.Background(), p, raw, "n1"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := h.verifyIDToken(context.Background(), p, mk(key, nil), "other-nonce"); err == nil {
		t.Error("wrong nonce: accepted")
	}
}
