package identity

import (
	"sync"

	"github.com/alexedwards/argon2id"
)

// PRD §0.1: argon2id (m=64 MB, t=3, p=2).
var argonParams = &argon2id.Params{
	Memory:      64 * 1024,
	Iterations:  3,
	Parallelism: 2,
	SaltLength:  16,
	KeyLength:   32,
}

// Each hash uses 64 MB; cap concurrent hashing so a login burst can't exhaust
// the (shared with CardFlow) process memory.
var hashSlots = make(chan struct{}, 4)

func HashPassword(password string) (string, error) {
	hashSlots <- struct{}{}
	defer func() { <-hashSlots }()
	return argon2id.CreateHash(password, argonParams)
}

func verifyPassword(password, hash string) bool {
	hashSlots <- struct{}{}
	defer func() { <-hashSlots }()
	ok, err := argon2id.ComparePasswordAndHash(password, hash)
	return err == nil && ok
}

var (
	dummyHashOnce sync.Once
	dummyHash     string
)

// burnPasswordCheck spends the same time as a real verification so unknown
// identifiers can't be detected by response timing.
func burnPasswordCheck(password string) {
	dummyHashOnce.Do(func() {
		dummyHash, _ = HashPassword("crm-timing-equaliser-not-a-real-password")
	})
	if dummyHash != "" {
		_ = verifyPassword(password, dummyHash)
	}
}

const (
	minPasswordLen = 8
	maxPasswordLen = 128
)

// validateNewPassword returns a field error message, or "" when acceptable.
func validateNewPassword(password string) string {
	switch {
	case len(password) < minPasswordLen:
		return "Use at least 8 characters."
	case len(password) > maxPasswordLen:
		return "Use at most 128 characters."
	}
	return ""
}
