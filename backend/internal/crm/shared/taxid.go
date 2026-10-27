package shared

import (
	"regexp"
	"strings"
)

var (
	panShape = regexp.MustCompile(`^[A-Z]{5}[0-9]{4}[A-Z]$`)
	tanShape = regexp.MustCompile(`^[A-Z]{4}[0-9]{5}[A-Z]$`)
)

// CheckTaxID normalises a GSTIN, PAN or TAN and says what is wrong with it ("" when it is
// valid, D-158). kind is "gstin", "pan" or "tan"; state is used only for a GSTIN.
func CheckTaxID(kind, raw, state string) (string, string) {
	v := strings.ToUpper(strings.Join(strings.Fields(raw), ""))
	switch strings.ToLower(kind) {
	case "gstin":
		return CheckGSTIN(v, state)
	case "pan":
		if !panShape.MatchString(v) {
			return v, "Enter a 10-character PAN, like ABCDE1234F."
		}
		return v, ""
	case "tan":
		if !tanShape.MatchString(v) {
			return v, "Enter a 10-character TAN, like ABCD12345E."
		}
		return v, ""
	}
	return v, "Choose GSTIN, PAN or TAN."
}
