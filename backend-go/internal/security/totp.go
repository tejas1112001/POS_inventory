package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" // #nosec G505 -- RFC 6238 TOTP uses HMAC-SHA1; authenticator apps expect it
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP parameters (PHASE1_DESIGN §7.2): SHA-1, 6 digits, 30 s, ±1 step.
const (
	TOTPDigits = 6
	TOTPPeriod = 30
	TOTPSkew   = 1
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns 20 random bytes (160 bits, RFC 4226 recommendation).
func NewTOTPSecret() []byte {
	s := make([]byte, 20)
	_, _ = rand.Read(s)
	return s
}

// TOTPSecretBase32 encodes a secret for manual entry.
func TOTPSecretBase32(secret []byte) string { return b32.EncodeToString(secret) }

// TOTPURI builds the otpauth:// URI the console renders as a QR code.
func TOTPURI(issuer, account string, secret []byte) string {
	v := url.Values{}
	v.Set("secret", TOTPSecretBase32(secret))
	v.Set("issuer", issuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", fmt.Sprint(TOTPDigits))
	v.Set("period", fmt.Sprint(TOTPPeriod))
	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
	return "otpauth://totp/" + label + "?" + v.Encode()
}

// hotp is RFC 4226 with a configurable digit count.
func hotp(secret []byte, counter uint64, digits int) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	code := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	mod := uint32(1)
	for range digits {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, code%mod)
}

// TOTPCode returns the code for time t.
func TOTPCode(secret []byte, t time.Time) string {
	return hotp(secret, uint64(t.Unix())/TOTPPeriod, TOTPDigits) // #nosec G115 -- Unix time is positive
}

// VerifyTOTP checks code within ±TOTPSkew steps. It returns the matched
// step so the caller can store it and refuse any step <= lastStep (replay
// guard). ok is false for a wrong or replayed code.
func VerifyTOTP(secret []byte, code string, t time.Time, lastStep int64) (step int64, ok bool) {
	code = strings.TrimSpace(code)
	if len(code) != TOTPDigits {
		return 0, false
	}
	now := t.Unix() / TOTPPeriod
	matched := int64(-1)
	for d := int64(-TOTPSkew); d <= TOTPSkew; d++ {
		s := now + d
		if s < 0 {
			continue
		}
		// Check every candidate (constant work) and keep the latest match.
		if subtle.ConstantTimeCompare([]byte(hotp(secret, uint64(s), TOTPDigits)), []byte(code)) == 1 {
			matched = s
		}
	}
	if matched < 0 || matched <= lastStep {
		return 0, false
	}
	return matched, true
}
