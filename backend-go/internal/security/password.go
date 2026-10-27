package security

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2Params are argon2id cost settings.
type Argon2Params struct {
	MemoryKiB uint32
	Time      uint32
	Threads   uint8
}

// Cost profiles (PHASE1_DESIGN §4.1, §7.2).
var (
	TenantArgon2   = Argon2Params{MemoryKiB: 46 * 1024, Time: 1, Threads: 1} // OWASP
	PlatformArgon2 = Argon2Params{MemoryKiB: 64 * 1024, Time: 3, Threads: 2}
)

const (
	saltLen = 16
	keyLen  = 32
)

// ErrMismatch means the password does not match the hash.
var ErrMismatch = errors.New("security: password mismatch")

// Hasher hashes and verifies passwords. A semaphore caps concurrent hashes
// so a login flood cannot exhaust memory (each hash allocates MemoryKiB).
type Hasher struct {
	params Argon2Params
	sem    chan struct{}
	dummy  string
}

// NewHasher creates a hasher allowing maxConcurrent hashes at once.
func NewHasher(p Argon2Params, maxConcurrent int) *Hasher {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	h := &Hasher{params: p, sem: make(chan struct{}, maxConcurrent)}
	// A real hash of a random value, verified on unknown users so that
	// "no such user" takes as long as "wrong password".
	var b [16]byte
	_, _ = rand.Read(b[:])
	h.dummy, _ = h.hash(base64.RawStdEncoding.EncodeToString(b[:]))
	return h
}

func (h *Hasher) acquire(ctx context.Context) error {
	select {
	case h.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Hasher) release() { <-h.sem }

// Hash returns a PHC-format argon2id hash.
func (h *Hasher) Hash(ctx context.Context, password string) (string, error) {
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	defer h.release()
	return h.hash(password)
}

func (h *Hasher) hash(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, h.params.Time, h.params.MemoryKiB, h.params.Threads, keyLen)
	b64 := base64.RawStdEncoding.EncodeToString
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.params.MemoryKiB, h.params.Time, h.params.Threads, b64(salt), b64(key)), nil
}

// Verify checks password against a PHC hash in constant time. An empty
// hash (user without a password, unknown user) is checked against a dummy
// hash and always fails.
func (h *Hasher) Verify(ctx context.Context, password, phc string) error {
	if err := h.acquire(ctx); err != nil {
		return err
	}
	defer h.release()
	if phc == "" {
		_ = verifyPHC(password, h.dummy)
		return ErrMismatch
	}
	return verifyPHC(password, phc)
}

func verifyPHC(password, phc string) error {
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return errors.New("security: unsupported hash format")
	}
	var v int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &v); err != nil || v != argon2.Version {
		return errors.New("security: unsupported argon2 version")
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil || m == 0 || t == 0 || p == 0 || m > 1<<20 || t > 20 {
		return errors.New("security: bad argon2 parameters")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return errors.New("security: bad salt")
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 || len(want) > 64 {
		return errors.New("security: bad key")
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want))) // #nosec G115 -- len checked <= 64
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrMismatch
	}
	return nil
}

// NeedsRehash reports whether a hash was made with different parameters.
func (h *Hasher) NeedsRehash(phc string) bool {
	return !strings.Contains(phc, fmt.Sprintf("$m=%d,t=%d,p=%d$", h.params.MemoryKiB, h.params.Time, h.params.Threads))
}
