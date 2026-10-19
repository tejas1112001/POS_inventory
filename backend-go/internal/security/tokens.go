package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

// NewToken returns a URL-safe random token with n bytes of entropy.
func NewToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// HashToken returns SHA-256 of a token. Tokens are stored only as hashes.
func HashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// recoveryAlphabet avoids look-alike characters.
const recoveryAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// NewRecoveryCode returns a code like "k7m2-p9qx-3hzt" (about 60 bits).
func NewRecoveryCode() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	var sb strings.Builder
	for i, c := range b {
		if i > 0 && i%4 == 0 {
			sb.WriteByte('-')
		}
		sb.WriteByte(recoveryAlphabet[int(c)%len(recoveryAlphabet)])
	}
	return sb.String()
}

// NormalizeRecoveryCode lowercases and strips spaces so users can type
// codes loosely; hashing is done on the normalized form.
func NormalizeRecoveryCode(c string) string {
	c = strings.ToLower(strings.TrimSpace(c))
	c = strings.ReplaceAll(c, " ", "")
	if len(c) == 12 && !strings.Contains(c, "-") {
		c = c[0:4] + "-" + c[4:8] + "-" + c[8:12]
	}
	return c
}
