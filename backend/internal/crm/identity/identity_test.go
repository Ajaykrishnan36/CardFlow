package identity

import (
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestAUTH02_NormalizeIdentifier(t *testing.T) {
	cases := []struct {
		in, ws      string
		kind, value string
		namespace   string
		ok          bool
	}{
		{in: "Ajay@Gmail.com", kind: "email", value: "ajay@gmail.com", namespace: "global", ok: true},
		{in: "  ajay@gmail.com ", kind: "email", value: "ajay@gmail.com", namespace: "global", ok: true},
		{in: "Name <a@b.co>", ok: false},
		{in: "a@b", ok: false},
		{in: "98765 43210", kind: "phone", value: "+919876543210", namespace: "global", ok: true},
		{in: "919876543210", kind: "phone", value: "+919876543210", namespace: "global", ok: true},
		{in: "+1 (415) 555-0100", kind: "phone", value: "+14155550100", namespace: "global", ok: true},
		{in: "Ravi.K", ws: "Acme-Demo", kind: "login_id", value: "ravi.k", namespace: "acme-demo", ok: true},
		{in: "drop table;", ok: false},
		{in: "", ok: false},
	}
	for _, c := range cases {
		got, ok := normalizeIdentifier(c.in, c.ws)
		if ok != c.ok {
			t.Fatalf("%q: ok=%v want %v", c.in, ok, c.ok)
		}
		if !ok {
			continue
		}
		if got.kind != c.kind || got.value != c.value || got.namespace != c.namespace {
			t.Errorf("%q: got %+v", c.in, got)
		}
	}
}

func TestAUTH02_LimiterLocksAtThreshold(t *testing.T) {
	l := newLimiter(5, time.Minute)
	for i := 1; i <= 4; i++ {
		if l.hit("k") {
			t.Fatalf("locked early at hit %d", i)
		}
	}
	if !l.hit("k") {
		t.Fatal("expected lock on 5th hit")
	}
	if blocked, wait := l.blocked("k"); !blocked || wait <= 0 {
		t.Fatalf("blocked=%v wait=%v", blocked, wait)
	}
	l.clear("k")
	if blocked, _ := l.blocked("k"); blocked {
		t.Fatal("clear should unblock")
	}
	if blocked, _ := l.blocked("other"); blocked {
		t.Fatal("keys must be independent")
	}
}

func TestAUTH02_PasswordHashRoundTrip(t *testing.T) {
	h, err := HashPassword("Ajay1234")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("unexpected params in %q", h)
	}
	if !verifyPassword("Ajay1234", h) || verifyPassword("ajay1234", h) {
		t.Fatal("verification mismatch")
	}
}

func TestAUTH02_NewPasswordPolicy(t *testing.T) {
	if validateNewPassword("short") == "" {
		t.Error("7 chars must be rejected")
	}
	if validateNewPassword("longenough") != "" {
		t.Error("8+ chars must be accepted")
	}
	if validateNewPassword(strings.Repeat("a", 129)) == "" {
		t.Error("129 chars must be rejected")
	}
}

func TestAUTH02_TOTPMatchesAdjacentStepsOnly(t *testing.T) {
	_, enr, err := generateTOTP("Test", "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, offset := range []time.Duration{0, -30 * time.Second, 30 * time.Second} {
		code, _ := totp.GenerateCodeCustom(enr.Secret, now.Add(offset), totpOpts)
		if _, ok := matchTOTP(enr.Secret, code, now); !ok {
			t.Errorf("offset %v should match", offset)
		}
	}
	old, _ := totp.GenerateCodeCustom(enr.Secret, now.Add(-2*time.Minute), totpOpts)
	if _, ok := matchTOTP(enr.Secret, old, now); ok {
		t.Error("a code from 2 minutes ago must not match")
	}
	if _, ok := matchTOTP(enr.Secret, "12345", now); ok {
		t.Error("5-digit code must not match")
	}
	if !strings.HasPrefix(enr.QRDataURL, "data:image/png;base64,") {
		t.Error("QR must be a PNG data URL")
	}
}

func TestAUTH02_RecoveryCodes(t *testing.T) {
	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 10 || len(hashes) != 10 {
		t.Fatalf("want 10 codes, got %d/%d", len(codes), len(hashes))
	}
	seen := map[string]bool{}
	for i, c := range codes {
		if len(c) != 9 || c[4] != '-' {
			t.Errorf("bad format %q", c)
		}
		if seen[c] {
			t.Errorf("duplicate code %q", c)
		}
		seen[c] = true
		if hashRecoveryCode(strings.ToUpper(strings.ReplaceAll(c, "-", " "))) != hashes[i] {
			t.Errorf("code %q must match regardless of case/separators", c)
		}
	}
}

func TestAUTH03_NextPathOrdering(t *testing.T) {
	if got := nextPath(true, true, true, true); got != "/crm/mfa/setup" {
		t.Errorf("enrolment first, got %s", got)
	}
	if got := nextPath(true, true, false, true); got != "/crm/mfa/verify" {
		t.Errorf("verify before password change, got %s", got)
	}
	if got := nextPath(false, false, false, true); got != "/crm/change-password" {
		t.Errorf("got %s", got)
	}
	if got := nextPath(true, false, false, false); got != "/crm/owner/dashboard" {
		t.Errorf("got %s", got)
	}
	if got := nextPath(false, false, false, false); got != "/crm/home" {
		t.Errorf("got %s", got)
	}
}

func TestAUTH03_SessionStages(t *testing.T) {
	s := Session{MFARequired: true}
	if s.MFAComplete() || s.Ready() {
		t.Fatal("pending MFA must not be ready")
	}
	s.MFAPassed = true
	if !s.Ready() {
		t.Fatal("MFA passed should be ready")
	}
	s.MustChangePassword = true
	if s.Ready() || !s.MFAComplete() {
		t.Fatal("must-change blocks readiness but not MFA completion")
	}
	if idleFor(true) != 30*time.Minute || idleFor(false) != 7*24*time.Hour {
		t.Fatal("idle timeouts must follow PRD AUTH-03")
	}
}
