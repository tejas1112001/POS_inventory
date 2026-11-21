// Package gst holds the Indian GST facts the app checks against
// (docs/M3_GST_DESIGN.md): the state code list and GSTIN validation. Like
// the other lists since M1, it lives in code, not in the schema.
package gst

import (
	"regexp"
	"strings"
)

// State is one GST state or union territory code.
type State struct {
	Code string `json:"code"` // two digits, as in the GSTIN
	Name string `json:"name"`
	// UT: a union territory without a legislature, which levies UTGST in
	// place of SGST on supplies within it.
	UT bool `json:"ut"`
}

// States lists the GST state codes in code order. 25 and 28 are kept for
// GSTINs issued before Daman and Diu merged into 26 and before Andhra
// Pradesh moved to 37.
var States = []State{
	{"01", "Jammu and Kashmir", false},
	{"02", "Himachal Pradesh", false},
	{"03", "Punjab", false},
	{"04", "Chandigarh", true},
	{"05", "Uttarakhand", false},
	{"06", "Haryana", false},
	{"07", "Delhi", false},
	{"08", "Rajasthan", false},
	{"09", "Uttar Pradesh", false},
	{"10", "Bihar", false},
	{"11", "Sikkim", false},
	{"12", "Arunachal Pradesh", false},
	{"13", "Nagaland", false},
	{"14", "Manipur", false},
	{"15", "Mizoram", false},
	{"16", "Tripura", false},
	{"17", "Meghalaya", false},
	{"18", "Assam", false},
	{"19", "West Bengal", false},
	{"20", "Jharkhand", false},
	{"21", "Odisha", false},
	{"22", "Chhattisgarh", false},
	{"23", "Madhya Pradesh", false},
	{"24", "Gujarat", false},
	{"25", "Daman and Diu (before 2020)", true},
	{"26", "Dadra and Nagar Haveli and Daman and Diu", true},
	{"27", "Maharashtra", false},
	{"28", "Andhra Pradesh (before 2019)", false},
	{"29", "Karnataka", false},
	{"30", "Goa", false},
	{"31", "Lakshadweep", true},
	{"32", "Kerala", false},
	{"33", "Tamil Nadu", false},
	{"34", "Puducherry", false},
	{"35", "Andaman and Nicobar Islands", true},
	{"36", "Telangana", false},
	{"37", "Andhra Pradesh", false},
	{"38", "Ladakh", true},
	{"97", "Other Territory", false},
}

var byCode = func() map[string]State {
	m := make(map[string]State, len(States))
	for _, s := range States {
		m[s.Code] = s
	}
	return m
}()

// StateOf returns the state with this code.
func StateOf(code string) (State, bool) {
	s, ok := byCode[code]
	return s, ok
}

// NormState reads a state code as typed ("27", " 7 " is not one: codes
// are two digits).
func NormState(s string) (string, bool) {
	s = strings.TrimSpace(s)
	_, ok := byCode[s]
	return s, ok
}

// A GSTIN is 15 characters: the state code, the holder's PAN (5 letters,
// 4 digits, a letter), the entity number (1-9 or A-Z), Z, and a check
// character.
var gstinPattern = regexp.MustCompile(`^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z][1-9A-Z]Z[0-9A-Z]$`)

const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"

// checkChar is the GSTIN check character of its first 14 characters
// (a mod-36 weighted sum, weights 1 and 2 alternating).
func checkChar(first14 string) byte {
	sum := 0
	for i := 0; i < 14; i++ {
		v := strings.IndexByte(alphabet, first14[i])
		f := 1
		if i%2 == 1 {
			f = 2
		}
		p := v * f
		sum += p/36 + p%36
	}
	return alphabet[(36-sum%36)%36]
}

// NormGSTIN upper-cases and trims a typed GSTIN.
func NormGSTIN(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// CheckGSTIN returns "" for a valid GSTIN, else what is wrong with it.
// state, when not "", is the state it is entered for: the GSTIN's first
// two digits must be that state (M3 Q6).
func CheckGSTIN(g, state string) string {
	switch {
	case !gstinPattern.MatchString(g):
		return "A GSTIN has 15 characters: the state code, the PAN, an entity number, Z and a check character."
	case checkChar(g[:14]) != g[14]:
		return "This GSTIN's check character does not match. Check it for a typing mistake."
	}
	if _, ok := byCode[g[:2]]; !ok {
		return "This GSTIN starts with an unknown state code."
	}
	if state != "" && g[:2] != state {
		s, _ := StateOf(state)
		return "This GSTIN is registered in another state (code " + g[:2] + "), not " + s.Name + " (" + state + ")."
	}
	return ""
}

// Party checks a GSTIN and a state code entered together, as typed ("" for
// none). With a GSTIN and no state, the state is the GSTIN's; a GSTIN of
// another state is refused (M3 Q6). Problems are keyed "gstin" and
// "stateCode".
func Party(gstin, state string) (string, string, map[string]string) {
	f := map[string]string{}
	g := NormGSTIN(gstin)
	s := strings.TrimSpace(state)
	if s != "" {
		if _, ok := byCode[s]; !ok {
			f["stateCode"] = "Choose a GST state code (two digits, such as 27 for Maharashtra)."
			return g, s, f
		}
	}
	if g != "" {
		if msg := CheckGSTIN(g, s); msg != "" {
			f["gstin"] = msg
			return g, s, f
		}
		s = g[:2]
	}
	return g, s, f
}
