package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestDenyListHashesAreWellFormed(t *testing.T) {
	if len(leakedSecretHashes) != 3 {
		t.Fatalf("expected 3 deny-list hashes, got %d", len(leakedSecretHashes))
	}
}

func TestIsLeakedSecretRejectsRandomValues(t *testing.T) {
	for range 100 {
		b := make([]byte, 64)
		if _, err := rand.Read(b); err != nil {
			t.Fatal(err)
		}
		for _, v := range []string{hex.EncodeToString(b), base64.StdEncoding.EncodeToString(b)} {
			if IsLeakedSecret(v) {
				t.Fatalf("random value flagged as leaked")
			}
		}
		if IsLeakedKey(b) {
			t.Fatalf("random key flagged as leaked")
		}
	}
	if IsLeakedSecret("") || IsLeakedSecret("   ") {
		t.Fatal("empty value flagged as leaked")
	}
}

// TestIsLeakedSecretMatchesEveryForm checks the decoding logic with a
// stand-in secret registered for the duration of the test. The real leaked
// value is never in the code or the tests.
func TestIsLeakedSecretMatchesEveryForm(t *testing.T) {
	raw := make([]byte, 71)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	hexText := hex.EncodeToString(raw)

	add := func(b []byte) { leakedSecretHashes[sha256.Sum256(b)] = struct{}{} }
	b64Bytes, err := base64.RawStdEncoding.DecodeString(hexText)
	if err != nil {
		t.Fatal(err)
	}
	add([]byte(hexText))
	add(raw)
	add(b64Bytes)
	t.Cleanup(func() {
		delete(leakedSecretHashes, sha256.Sum256([]byte(hexText)))
		delete(leakedSecretHashes, sha256.Sum256(raw))
		delete(leakedSecretHashes, sha256.Sum256(b64Bytes))
	})

	cases := map[string]string{
		"hex text":           hexText,
		"hex text, spaces":   "  " + hexText + "\n",
		"base64 of raw":      base64.StdEncoding.EncodeToString(raw),
		"raw base64 of raw":  base64.RawURLEncoding.EncodeToString(raw),
		"base64 of hex text": base64.StdEncoding.EncodeToString([]byte(hexText)),
	}
	for name, v := range cases {
		if !IsLeakedSecret(v) {
			t.Errorf("%s: not detected", name)
		}
	}
	if !IsLeakedKey(raw) || !IsLeakedKey(b64Bytes) {
		t.Error("IsLeakedKey missed a registered key")
	}
}
