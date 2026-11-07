// Package security holds password hashing, TOTP, token hashing, the password
// policy and the leaked-secret deny list.
package security

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

// leakedSecretHashes are SHA-256 hashes of secrets that were committed to the
// public git history (P0-1). Only the hashes live in code, never the values.
//
// The old JWT secret is covered in every form an operator might paste it:
// as hex text, as the bytes that hex decodes to, and as the bytes that a
// base64 decoder makes of the hex text.
//
// The old DB password is deliberately not listed: it is a short dictionary
// word, so its hash could be reversed in seconds. The production DB password
// rules cover it instead.
var leakedSecretHashes = map[[sha256.Size]byte]struct{}{
	mustHash("b4da871de54ed0ef67c7403aefb9bb9d3722628a898880247c2f23d20d5abbfc"): {}, // old JWT_SECRET, hex text
	mustHash("4da4f5f1223834280aa19f73891168e8b6fc494cf7a7b2001a45d4a8eac1ba55"): {}, // old JWT_SECRET, hex-decoded bytes
	mustHash("04b2cde7210a607c10af4cebfe531dd7551409ba854f526755b1c934e95f3442"): {}, // old JWT_SECRET, base64-decoded bytes
}

func mustHash(h string) [sha256.Size]byte {
	b, err := hex.DecodeString(h)
	if err != nil || len(b) != sha256.Size {
		panic("security: bad deny-list hash " + h)
	}
	var out [sha256.Size]byte
	copy(out[:], b)
	return out
}

// IsLeakedSecret reports whether the value, or any decoding of it, is a known
// leaked secret. It checks the text itself, its hex decoding and its base64
// decodings.
func IsLeakedSecret(value string) bool {
	v := strings.TrimSpace(value)
	if v == "" {
		return false
	}
	if isLeakedBytes([]byte(v)) {
		return true
	}
	if b, err := hex.DecodeString(v); err == nil && isLeakedBytes(b) {
		return true
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(v); err == nil && isLeakedBytes(b) {
			return true
		}
	}
	return false
}

// IsLeakedKey reports whether raw key bytes are a known leaked secret.
func IsLeakedKey(key []byte) bool { return isLeakedBytes(key) }

func isLeakedBytes(b []byte) bool {
	_, found := leakedSecretHashes[sha256.Sum256(b)]
	return found
}
