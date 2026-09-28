package identity

import (
	"net/mail"
	"strings"
	"unicode"
)

type identifier struct {
	kind      string // email | phone | login_id
	namespace string
	value     string
}

// normalizeIdentifier turns what the user typed into the canonical form stored in
// crm.verified_identifiers: lower-case email, E.164 phone (India default), or a
// workspace-scoped login ID.
func normalizeIdentifier(raw, workspaceCode string) (identifier, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 254 {
		return identifier{}, false
	}

	if strings.Contains(raw, "@") {
		addr, err := mail.ParseAddress(raw)
		if err != nil || addr.Address != raw || !strings.Contains(raw[strings.LastIndex(raw, "@"):], ".") {
			return identifier{}, false
		}
		return identifier{kind: "email", namespace: "global", value: strings.ToLower(raw)}, true
	}

	if phone, ok := normalizePhone(raw); ok {
		return identifier{kind: "phone", namespace: "global", value: phone}, true
	}

	ns := strings.ToLower(strings.TrimSpace(workspaceCode))
	if ns == "" {
		ns = "global"
	}
	for _, r := range raw {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-') {
			return identifier{}, false
		}
	}
	return identifier{kind: "login_id", namespace: ns, value: strings.ToLower(raw)}, true
}

func normalizePhone(raw string) (string, bool) {
	hasPlus := strings.HasPrefix(raw, "+")
	var digits strings.Builder
	for _, r := range raw {
		switch {
		case unicode.IsDigit(r):
			digits.WriteRune(r)
		case r == ' ' || r == '-' || r == '(' || r == ')' || r == '+':
		default:
			return "", false
		}
	}
	d := digits.String()
	switch {
	case len(d) == 10 && !hasPlus:
		return "+91" + d, true
	case len(d) == 12 && strings.HasPrefix(d, "91") && !hasPlus:
		return "+" + d, true
	case hasPlus && len(d) >= 8 && len(d) <= 15:
		return "+" + d, true
	}
	return "", false
}
