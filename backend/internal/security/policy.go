package security

import (
	"strings"
	"unicode/utf8"
)

// commonPasswords is a small embedded blocklist of the most common and
// breached passwords (and patterns used in this project's old demo data).
// Matching is case-insensitive on the whole value. NIST 800-63B: no
// composition rules, only length plus a blocklist.
var commonPasswords = map[string]bool{}

func init() {
	for _, p := range strings.Fields(`
123456 123456789 12345678 1234567890 12345 1234567 qwerty password password1 password123
111111 123123 000000 abc123 iloveyou admin admin123 admin1234 welcome welcome1 welcome123
letmein monkey dragon football baseball sunshine princess qwerty123 qwertyuiop 1q2w3e4r
1q2w3e4r5t zaq12wsx passw0rd p@ssw0rd p@ssword changeme secret trustno1 master superman
batman shadow michael jennifer hunter2 starwars whatever freedom login abcd1234 asdfghjkl
asdfasdf zxcvbnm zxcvbnm123 qazwsx 654321 666666 777777 888888 987654321 121212 112233
aa123456 1qaz2wsx 123qwe 123abc pos123456 admin@123 india123 india@123 password@123
qwerty@123 welcome@123 test1234 testtest guest guest123 default root toor administrator
pos@admin.com admin@pos.com demo1234 demodemo samsung iphone loveyou lovely ashley
charlie donald harley hello hello123 summer winter spring autumn mustang access flower
`) {
		commonPasswords[p] = true
	}
}

// PolicyError explains why a password was rejected (safe to show).
type PolicyError struct{ Reason string }

func (e *PolicyError) Error() string { return e.Reason }

// CheckPassword applies the policy: length between min and 128 characters,
// not on the blocklist, not the email or a business/user name, not one
// repeated character.
func CheckPassword(pw string, minLen int, personal ...string) error {
	n := utf8.RuneCountInString(pw)
	if n < minLen {
		return &PolicyError{Reason: "must be at least " + itoa(minLen) + " characters"}
	}
	if n > 128 {
		return &PolicyError{Reason: "must be at most 128 characters"}
	}
	lower := strings.ToLower(pw)
	if commonPasswords[lower] || IsLeakedSecret(pw) {
		return &PolicyError{Reason: "is too common; choose another"}
	}
	for _, p := range personal {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if lower == p || (strings.Contains(p, "@") && lower == strings.SplitN(p, "@", 2)[0]) {
			return &PolicyError{Reason: "must not be your email or name"}
		}
	}
	first, _ := utf8.DecodeRuneInString(pw)
	if strings.Count(pw, string(first)) == n {
		return &PolicyError{Reason: "must not be one repeated character"}
	}
	return nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
