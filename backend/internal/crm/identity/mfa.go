package identity

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"image/png"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const (
	totpPeriod        = 30
	recoveryCodeCount = 10
)

var totpOpts = totp.ValidateOpts{Period: totpPeriod, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1}

type enrollment struct {
	Secret     string `json:"secret"`
	OtpauthURL string `json:"otpauthUrl"`
	QRDataURL  string `json:"qrDataUrl"`
}

func generateTOTP(issuer, account string) (*otp.Key, enrollment, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: account,
		Period:      totpPeriod,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
		SecretSize:  20,
	})
	if err != nil {
		return nil, enrollment{}, err
	}
	img, err := key.Image(240, 240)
	if err != nil {
		return nil, enrollment{}, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, enrollment{}, err
	}
	return key, enrollment{
		Secret:     key.Secret(),
		OtpauthURL: key.URL(),
		QRDataURL:  "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()),
	}, nil
}

// matchTOTP checks code against the previous, current and next 30-second steps and
// returns the matching step so callers can reject a replay of an already-used code.
func matchTOTP(secret, code string, now time.Time) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return 0, false
	}
	for _, skew := range []int64{0, -1, 1} {
		t := now.Add(time.Duration(skew*totpPeriod) * time.Second)
		expected, err := totp.GenerateCodeCustom(secret, t, totpOpts)
		if err != nil {
			return 0, false
		}
		if shared.ConstantTimeEqual(expected, code) {
			return t.Unix() / totpPeriod, true
		}
	}
	return 0, false
}

// newRecoveryCodes returns display codes ("abcd-efgh") and their stored hashes.
func newRecoveryCodes() (codes []string, hashes []string, err error) {
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	for i := 0; i < recoveryCodeCount; i++ {
		raw := make([]byte, 5)
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, err
		}
		s := strings.ToLower(enc.EncodeToString(raw)) // 8 chars
		code := s[:4] + "-" + s[4:]
		codes = append(codes, code)
		hashes = append(hashes, hashRecoveryCode(code))
	}
	return codes, hashes, nil
}

func normalizeRecoveryCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, " ", "")
	return strings.ReplaceAll(code, "-", "")
}

func hashRecoveryCode(code string) string {
	sum := sha256.Sum256([]byte(normalizeRecoveryCode(code)))
	return hex.EncodeToString(sum[:])
}

func looksLikeRecoveryCode(code string) bool {
	return len(normalizeRecoveryCode(code)) == 8
}
