package security

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var fastParams = Argon2Params{MemoryKiB: 8 * 1024, Time: 1, Threads: 1}

func TestHashAndVerify(t *testing.T) {
	ctx := context.Background()
	h := NewHasher(fastParams, 2)
	phc, err := h.Hash(ctx, "correct horse battery staple")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(phc, "$argon2id$v=19$m=8192,t=1,p=1$"))
	require.NoError(t, h.Verify(ctx, "correct horse battery staple", phc))
	require.ErrorIs(t, h.Verify(ctx, "wrong", phc), ErrMismatch)
	require.ErrorIs(t, h.Verify(ctx, "anything", ""), ErrMismatch, "no hash never verifies")
	require.Error(t, h.Verify(ctx, "x", "$bcrypt$..."))
	require.Error(t, h.Verify(ctx, "x", "$argon2id$v=19$m=999999999,t=1,p=1$c2FsdA$a2V5"), "absurd params refused")

	other, err := h.Hash(ctx, "correct horse battery staple")
	require.NoError(t, err)
	require.NotEqual(t, phc, other, "random salt")
	require.False(t, h.NeedsRehash(phc))
	require.True(t, NewHasher(TenantArgon2, 1).NeedsRehash(phc))
}

func TestHasherSemaphore(t *testing.T) {
	h := NewHasher(fastParams, 1)
	h.sem <- struct{}{} // occupy the only slot
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := h.Hash(ctx, "x")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	<-h.sem

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			_, err := h.Hash(context.Background(), "parallel")
			require.NoError(t, err)
		})
	}
	wg.Wait()
}

func TestPasswordPolicy(t *testing.T) {
	ok := "glacier-violet-tram-42"
	require.NoError(t, CheckPassword(ok, 12, "owner@shop.test", "Asha"))
	for name, c := range map[string]struct {
		pw       string
		personal []string
	}{
		"short":    {"short", nil},
		"long":     {strings.Repeat("ab", 65), nil},
		"common":   {"Password123", nil},
		"demo":     {"admin123456!", nil},
		"email":    {"owner@shop.test", []string{"owner@shop.test"}},
		"local":    {"ownerownerown", []string{"ownerownerown@x.test"}},
		"name":     {"Asha Kulkarni", []string{"asha kulkarni"}},
		"repeated": {strings.Repeat("z", 20), nil},
	} {
		if name == "demo" {
			c.pw = "admin@123"
			require.Error(t, CheckPassword(c.pw, 8), name)
			continue
		}
		require.Error(t, CheckPassword(c.pw, 12, c.personal...), name)
	}
	require.Error(t, CheckPassword("thirteen-char", 14), "platform minimum is 14")
}

// RFC 6238 Appendix B, SHA-1, 8 digits, secret "12345678901234567890".
func TestTOTPRFC6238Vectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	for _, v := range []struct {
		unix int64
		code string
	}{
		{59, "94287082"}, {1111111109, "07081804"}, {1111111111, "14050471"},
		{1234567890, "89005924"}, {2000000000, "69279037"}, {20000000000, "65353130"},
	} {
		require.Equal(t, v.code, hotp(secret, uint64(v.unix/30), 8), v.unix)
	}
}

func TestVerifyTOTPWindowAndReplay(t *testing.T) {
	secret := NewTOTPSecret()
	now := time.Unix(1_800_000_000, 0)
	code := TOTPCode(secret, now)
	step, ok := VerifyTOTP(secret, code, now, 0)
	require.True(t, ok)
	require.Equal(t, now.Unix()/30, step)

	_, ok = VerifyTOTP(secret, code, now, step)
	require.False(t, ok, "same step cannot be used twice")

	_, ok = VerifyTOTP(secret, TOTPCode(secret, now.Add(-30*time.Second)), now, 0)
	require.True(t, ok, "previous step accepted")
	_, ok = VerifyTOTP(secret, TOTPCode(secret, now.Add(30*time.Second)), now, 0)
	require.True(t, ok, "next step accepted")
	_, ok = VerifyTOTP(secret, TOTPCode(secret, now.Add(-90*time.Second)), now, 0)
	require.False(t, ok, "outside the window")
	_, ok = VerifyTOTP(secret, "12345", now, 0)
	require.False(t, ok)

	uri := TOTPURI("POS Dev", "root@x.test", secret)
	require.True(t, strings.HasPrefix(uri, "otpauth://totp/POS%20Dev:root@x.test?"))
	require.Contains(t, uri, "secret="+TOTPSecretBase32(secret))
}

func TestTokensAndRecoveryCodes(t *testing.T) {
	a, b := NewToken(32), NewToken(32)
	require.NotEqual(t, a, b)
	require.Len(t, a, 43)
	require.Len(t, HashToken(a), 32)
	require.Equal(t, HashToken(a), HashToken(a))

	c := NewRecoveryCode()
	require.Regexp(t, `^[a-z2-9]{4}-[a-z2-9]{4}-[a-z2-9]{4}$`, c)
	require.Equal(t, c, NormalizeRecoveryCode(" "+strings.ToUpper(strings.ReplaceAll(c, "-", ""))+" "))
}
