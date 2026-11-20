package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
)

// KEK is one key-encryption key from MASTER_KEYS.
type KEK struct {
	ID  string
	Key []byte // exactly 32 bytes
}

// Sealed is an envelope-encrypted value as stored in the database.
type Sealed struct {
	Ciphertext []byte // nonce || AES-256-GCM(plaintext, DEK, aad)
	WrappedDEK []byte // nonce || AES-256-GCM(DEK, KEK, aad)
	KeyID      string
}

// ErrDecrypt hides why decryption failed (wrong key, wrong binding, tamper).
var ErrDecrypt = errors.New("secrets: cannot decrypt")

// Envelope encrypts small secrets (TOTP seeds, SMTP passwords) with a
// fresh DEK each, wrapped by the current KEK. The AAD binds a ciphertext to
// its owner and purpose (for example "platform_user:7|totp"), so a value
// copied into another row fails to decrypt (EMAIL_AND_RECEIPTS §3).
type Envelope struct {
	keys []KEK // first encrypts, all decrypt
}

// NewEnvelope creates an envelope from the KEK ring.
func NewEnvelope(keys []KEK) (*Envelope, error) {
	if len(keys) == 0 {
		return nil, errors.New("secrets: no master keys")
	}
	for _, k := range keys {
		if len(k.Key) != 32 || k.ID == "" {
			return nil, errors.New("secrets: master keys must be 32 bytes with an id")
		}
	}
	return &Envelope{keys: keys}, nil
}

// Seal encrypts plaintext bound to binding.
func (e *Envelope) Seal(plaintext []byte, binding string) (Sealed, error) {
	kek := e.keys[0]
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		return Sealed{}, err
	}
	aad := []byte(binding + "|" + kek.ID)
	ct, err := gcmSeal(dek, plaintext, aad)
	if err != nil {
		return Sealed{}, err
	}
	wrapped, err := gcmSeal(kek.Key, dek, aad)
	if err != nil {
		return Sealed{}, err
	}
	clear(dek)
	return Sealed{Ciphertext: ct, WrappedDEK: wrapped, KeyID: kek.ID}, nil
}

// Open decrypts a sealed value; binding must match the one used to seal.
func (e *Envelope) Open(s Sealed, binding string) ([]byte, error) {
	var kek *KEK
	for i := range e.keys {
		if e.keys[i].ID == s.KeyID {
			kek = &e.keys[i]
			break
		}
	}
	if kek == nil {
		return nil, ErrDecrypt
	}
	aad := []byte(binding + "|" + kek.ID)
	dek, err := gcmOpen(kek.Key, s.WrappedDEK, aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	defer clear(dek)
	pt, err := gcmOpen(dek, s.Ciphertext, aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}

// NeedsRewrap reports whether s was sealed with an older KEK.
func (e *Envelope) NeedsRewrap(s Sealed) bool { return s.KeyID != e.keys[0].ID }

func gcmSeal(key, plaintext, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return g.Seal(nonce, nonce, plaintext, aad), nil
}

func gcmOpen(key, data, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(data) < g.NonceSize() {
		return nil, ErrDecrypt
	}
	return g.Open(nil, data[:g.NonceSize()], data[g.NonceSize():], aad)
}
