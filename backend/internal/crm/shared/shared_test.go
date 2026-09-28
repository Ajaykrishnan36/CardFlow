package shared

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestSEC01_EncryptRoundTripAndTamper(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	sealed, err := Encrypt(key, []byte("JBSWY3DPEHPK3PXP"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := Decrypt(key, sealed)
	if err != nil || string(plain) != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("round trip failed: %v %q", err, plain)
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := Decrypt(key, sealed); err == nil {
		t.Fatal("tampered ciphertext must fail")
	}
	other := bytes.Repeat([]byte{8}, 32)
	good, _ := Encrypt(key, []byte("x"))
	if _, err := Decrypt(other, good); err == nil {
		t.Fatal("wrong key must fail")
	}
}

func TestAUTH03_TokensAreRandomAndHashed(t *testing.T) {
	a, _ := RandomToken(32)
	b, _ := RandomToken(32)
	if a == b || len(a) != 43 {
		t.Fatalf("tokens %q %q", a, b)
	}
	if len(HashToken(a)) != 32 || bytes.Equal(HashToken(a), HashToken(b)) {
		t.Fatal("hash must be 32-byte SHA-256 and distinct")
	}
}

func TestAUTH01_ProductionRefusesUnsafeConfig(t *testing.T) {
	t.Setenv("CRM_APP_ENV", "production")
	t.Setenv("CRM_SEED_DEMO", "true")
	t.Setenv("CRM_ENCRYPTION_KEY_BASE64", "")
	cfg, problems := LoadConfig("production")
	if len(problems) != 2 {
		t.Fatalf("want demo-seed and missing-key problems, got %v", problems)
	}
	if !cfg.CookieSecure || !cfg.MFAEnforced() {
		t.Fatal("production must use secure cookies and enforce MFA")
	}
}

func TestAUTH01_LocalDefaults(t *testing.T) {
	t.Setenv("CRM_APP_ENV", "")
	t.Setenv("CRM_SEED_DEMO", "")
	t.Setenv("CRM_ENCRYPTION_KEY_BASE64", "")
	cfg, problems := LoadConfig("development")
	if len(problems) != 0 {
		t.Fatalf("local should have no problems: %v", problems)
	}
	if cfg.AppEnv != "local" || !cfg.SeedDemo || cfg.CookieSecure || cfg.MFAEnforced() || len(cfg.EncryptionKey) != 32 {
		t.Fatalf("unexpected local config %+v", cfg)
	}
}

func TestSEC01_EncryptionKeyValidation(t *testing.T) {
	t.Setenv("CRM_APP_ENV", "staging")
	t.Setenv("CRM_SEED_DEMO", "false")
	t.Setenv("CRM_ENCRYPTION_KEY_BASE64", base64.StdEncoding.EncodeToString([]byte("too-short")))
	if _, problems := LoadConfig(""); len(problems) == 0 {
		t.Fatal("short key must be rejected")
	}
	t.Setenv("CRM_ENCRYPTION_KEY_BASE64", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)))
	if _, problems := LoadConfig(""); len(problems) != 0 {
		t.Fatalf("valid key rejected: %v", problems)
	}
}
