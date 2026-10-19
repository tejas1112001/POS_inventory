package config

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/security"
)

// MinKeyBytes is the minimum size of every signing or encryption key.
const MinKeyBytes = 32

var kidPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// placeholderMarkers are fragments that show a value was copied from an
// example file instead of being generated. They are long enough that a
// random base64 key practically never contains one by chance.
var placeholderMarkers = []string{
	"changeme", "change_me", "change-me", "your_", "your-", "placeholder",
	"example", "secret_here", "replace", "dummy", "default",
}

// Key is one entry of a key ring such as JWT_KEYS.
type Key struct {
	ID    string
	bytes []byte
}

// Bytes returns a copy of the key material.
func (k Key) Bytes() []byte { return bytes.Clone(k.bytes) }

// String hides the key material.
func (k Key) String() string { return "Key{" + k.ID + ", [REDACTED]}" }

// KeyRing is an ordered list of keys. The first key signs or encrypts; every
// key verifies or decrypts, so keys can be rotated without downtime.
type KeyRing []Key

// Current returns the key used for new signatures or ciphertexts.
func (r KeyRing) Current() Key { return r[0] }

// Lookup finds a key by its ID.
func (r KeyRing) Lookup(kid string) (Key, bool) {
	for _, k := range r {
		if k.ID == kid {
			return k, true
		}
	}
	return Key{}, false
}

// parseKeyRing parses "kid:base64,kid:base64". exactLen > 0 requires that
// exact key size (envelope KEKs are exactly 32 bytes).
// Errors name the variable and the kid, never the key material.
func parseKeyRing(name, raw string, exactLen int) (KeyRing, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%s is required", name)
	}
	var ring KeyRing
	seen := map[string]bool{}
	for i, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		kid, enc, ok := strings.Cut(part, ":")
		if !ok {
			return nil, fmt.Errorf("%s entry %d: expected kid:base64", name, i+1)
		}
		if !kidPattern.MatchString(kid) {
			return nil, fmt.Errorf("%s entry %d: kid must match %s", name, i+1, kidPattern)
		}
		if seen[kid] {
			return nil, fmt.Errorf("%s: duplicate kid %q", name, kid)
		}
		seen[kid] = true
		if looksLikePlaceholder(enc) {
			return nil, fmt.Errorf("%s kid %q: value looks like a placeholder; generate a random key", name, kid)
		}
		if security.IsLeakedSecret(enc) {
			return nil, fmt.Errorf("%s kid %q: this value was leaked in git history and must never be used", name, kid)
		}
		b, err := decodeBase64(enc)
		if err != nil {
			return nil, fmt.Errorf("%s kid %q: value is not valid base64", name, kid)
		}
		if security.IsLeakedKey(b) {
			return nil, fmt.Errorf("%s kid %q: this value was leaked in git history and must never be used", name, kid)
		}
		switch {
		case exactLen > 0 && len(b) != exactLen:
			return nil, fmt.Errorf("%s kid %q: key must be exactly %d bytes, got %d", name, kid, exactLen, len(b))
		case len(b) < MinKeyBytes:
			return nil, fmt.Errorf("%s kid %q: key must be at least %d bytes, got %d", name, kid, MinKeyBytes, len(b))
		case lowEntropy(b):
			return nil, fmt.Errorf("%s kid %q: key has too little variety; generate it with a CSPRNG", name, kid)
		}
		ring = append(ring, Key{ID: kid, bytes: b})
	}
	return ring, nil
}

func decodeBase64(s string) ([]byte, error) {
	var lastErr error
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		b, err := enc.DecodeString(s)
		if err == nil {
			return b, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func looksLikePlaceholder(s string) bool {
	l := strings.ToLower(s)
	for _, m := range placeholderMarkers {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}

// lowEntropy catches keys like 32 × 'a' or a short repeated pattern.
// A random 32-byte key has about 28 distinct byte values on average.
func lowEntropy(b []byte) bool {
	distinct := map[byte]struct{}{}
	for _, c := range b {
		distinct[c] = struct{}{}
	}
	return len(distinct) < 12
}

// sharesKey reports whether any key appears in both rings.
func sharesKey(a, b KeyRing) bool {
	for _, x := range a {
		for _, y := range b {
			if bytes.Equal(x.bytes, y.bytes) {
				return true
			}
		}
	}
	return false
}
