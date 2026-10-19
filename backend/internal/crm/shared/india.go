package shared

import (
	"regexp"
	"strings"
)

// IndianState is a state or union territory with its GST state code (D-155).
type IndianState struct {
	Name    string `json:"name"`
	GSTCode string `json:"gstCode"`
}

// IndianStates are the 28 states and 8 union territories with their GST codes.
var IndianStates = []IndianState{
	{"Andaman and Nicobar Islands", "35"}, {"Andhra Pradesh", "37"}, {"Arunachal Pradesh", "12"}, {"Assam", "18"}, {"Bihar", "10"},
	{"Chandigarh", "04"}, {"Chhattisgarh", "22"}, {"Dadra and Nagar Haveli and Daman and Diu", "26"}, {"Delhi", "07"}, {"Goa", "30"},
	{"Gujarat", "24"}, {"Haryana", "06"}, {"Himachal Pradesh", "02"}, {"Jammu and Kashmir", "01"}, {"Jharkhand", "20"},
	{"Karnataka", "29"}, {"Kerala", "32"}, {"Ladakh", "38"}, {"Lakshadweep", "31"}, {"Madhya Pradesh", "23"},
	{"Maharashtra", "27"}, {"Manipur", "14"}, {"Meghalaya", "17"}, {"Mizoram", "15"}, {"Nagaland", "13"},
	{"Odisha", "21"}, {"Puducherry", "34"}, {"Punjab", "03"}, {"Rajasthan", "08"}, {"Sikkim", "11"},
	{"Tamil Nadu", "33"}, {"Telangana", "36"}, {"Tripura", "16"}, {"Uttar Pradesh", "09"}, {"Uttarakhand", "05"},
	{"West Bengal", "19"},
}

// stateAliases: spellings other sources use for the same state.
var stateAliases = map[string]string{"orissa": "Odisha", "pondicherry": "Puducherry", "nct of delhi": "Delhi", "new delhi": "Delhi",
	"uttaranchal": "Uttarakhand", "the dadra and nagar haveli and daman and diu": "Dadra and Nagar Haveli and Daman and Diu",
	"dadra and nagar haveli": "Dadra and Nagar Haveli and Daman and Diu", "daman and diu": "Dadra and Nagar Haveli and Daman and Diu",
	"andaman & nicobar islands": "Andaman and Nicobar Islands", "jammu & kashmir": "Jammu and Kashmir", "tamilnadu": "Tamil Nadu"}

// IndianStateNamed finds a state by any common spelling ("Tamil Nadu", "tamilnadu", "Orissa").
func IndianStateNamed(name string) (IndianState, bool) {
	n := strings.ToLower(strings.Join(strings.Fields(strings.ReplaceAll(name, "&", " and ")), " "))
	if a, ok := stateAliases[strings.ToLower(strings.TrimSpace(name))]; ok {
		n = strings.ToLower(a)
	} else if a, ok := stateAliases[n]; ok {
		n = strings.ToLower(a)
	}
	for _, s := range IndianStates {
		if strings.ToLower(s.Name) == n {
			return s, true
		}
	}
	return IndianState{}, false
}

var gstinShape = regexp.MustCompile(`^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z][1-9A-Z]Z[0-9A-Z]$`)

// CheckGSTIN normalises a GSTIN and says what is wrong with it ("" when it is valid): the
// shape (state code, PAN, entity number, Z, check character) and the check character itself.
// When state is given, the GSTIN must belong to that state.
func CheckGSTIN(raw, state string) (string, string) {
	g := strings.ToUpper(strings.Join(strings.Fields(raw), ""))
	if !gstinShape.MatchString(g) {
		return g, "Enter a 15-character GSTIN, like 33ABCDE1234F1Z5."
	}
	const chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	sum := 0
	for i := 0; i < 14; i++ {
		v := strings.IndexByte(chars, g[i]) * (1 + i%2)
		sum += v/36 + v%36
	}
	if chars[(36-sum%36)%36] != g[14] {
		return g, "This GSTIN's last character doesn't match — check for a typing mistake."
	}
	known := false
	for _, s := range IndianStates {
		known = known || s.GSTCode == g[:2]
	}
	if !known && g[:2] != "97" && g[:2] != "25" && g[:2] != "28" {
		return g, "The first two digits aren't an Indian state code."
	}
	if st, ok := IndianStateNamed(state); ok && state != "" && g[:2] != st.GSTCode && !(st.GSTCode == "26" && g[:2] == "25") && !(st.GSTCode == "37" && g[:2] == "28") {
		return g, "This GSTIN is registered in another state (code " + g[:2] + "), not " + st.Name + " (" + st.GSTCode + ")."
	}
	return g, ""
}
