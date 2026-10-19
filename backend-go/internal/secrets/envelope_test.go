package secrets

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func kek(t *testing.T, id string) KEK {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return KEK{ID: id, Key: b}
}

func TestEnvelopeRoundTripAndBinding(t *testing.T) {
	k1 := kek(t, "k1")
	e, err := NewEnvelope([]KEK{k1})
	require.NoError(t, err)
	s, err := e.Seal([]byte("totp-seed"), "platform_user:7|totp")
	require.NoError(t, err)
	require.False(t, bytes.Contains(s.Ciphertext, []byte("totp-seed")))

	pt, err := e.Open(s, "platform_user:7|totp")
	require.NoError(t, err)
	require.Equal(t, "totp-seed", string(pt))

	_, err = e.Open(s, "platform_user:8|totp")
	require.ErrorIs(t, err, ErrDecrypt, "copied to another user")
	_, err = e.Open(s, "platform_user:7|smtp_password")
	require.ErrorIs(t, err, ErrDecrypt, "other purpose")

	tampered := s
	tampered.Ciphertext = append([]byte{}, s.Ciphertext...)
	tampered.Ciphertext[len(tampered.Ciphertext)-1] ^= 1
	_, err = e.Open(tampered, "platform_user:7|totp")
	require.ErrorIs(t, err, ErrDecrypt)
}

func TestEnvelopeRotation(t *testing.T) {
	old, cur := kek(t, "old"), kek(t, "new")
	eOld, _ := NewEnvelope([]KEK{old})
	s, err := eOld.Seal([]byte("v"), "b")
	require.NoError(t, err)

	eNew, _ := NewEnvelope([]KEK{cur, old})
	pt, err := eNew.Open(s, "b")
	require.NoError(t, err, "old key still decrypts")
	require.Equal(t, "v", string(pt))
	require.True(t, eNew.NeedsRewrap(s))

	eOnlyNew, _ := NewEnvelope([]KEK{cur})
	_, err = eOnlyNew.Open(s, "b")
	require.ErrorIs(t, err, ErrDecrypt)

	_, err = NewEnvelope(nil)
	require.Error(t, err)
	_, err = NewEnvelope([]KEK{{ID: "x", Key: []byte("short")}})
	require.Error(t, err)
}
