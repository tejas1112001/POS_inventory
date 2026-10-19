package authtoken

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/config"
)

func ring(t *testing.T, ids ...string) config.KeyRing {
	t.Helper()
	vars := map[string]string{
		"BASE_DOMAIN": "pos.test", "PLATFORM_NAME": "T", "APP_ENV": "test", "PUBLIC_SCHEME": "http",
		"DATABASE_URL": "postgres://a:b@h/d", "DATABASE_JOBS_URL": "postgres://a:b@h/d",
		"PLATFORM_JWT_KEYS": "p:" + rk(t), "MASTER_KEYS": "m:" + rk(t),
	}
	val := ""
	for i, id := range ids {
		if i > 0 {
			val += ","
		}
		val += id + ":" + rk(t)
	}
	vars["JWT_KEYS"] = val
	c, err := config.LoadFrom(vars)
	require.NoError(t, err)
	return c.JWTKeys
}

func rk(t *testing.T) string {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(b)
}

func TestSignVerify(t *testing.T) {
	s, err := NewSigner(ring(t, "k1"), AudTenant, 10*time.Minute)
	require.NoError(t, err)
	tok, exp, err := s.Sign("acme.pos.test", "42", Claims{TenantID: "abcdefghij", TokenVersion: 3, SessionID: "s1"})
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(10*time.Minute), exp, time.Second)

	c, err := s.Verify(tok, "acme.pos.test")
	require.NoError(t, err)
	require.Equal(t, "42", c.Subject)
	require.Equal(t, "abcdefghij", c.TenantID)
	require.Equal(t, 3, c.TokenVersion)

	_, err = s.Verify(tok, "other.pos.test")
	require.ErrorIs(t, err, ErrInvalid, "issuer is the exact host")
}

func TestAudienceAndKeySeparation(t *testing.T) {
	tenantRing, platformRing := ring(t, "k1"), ring(t, "p1")
	tenant, _ := NewSigner(tenantRing, AudTenant, 10*time.Minute)
	platform, _ := NewSigner(platformRing, AudPlatform, 5*time.Minute)
	tok, _, err := tenant.Sign("admin.pos.test", "1", Claims{})
	require.NoError(t, err)
	_, err = platform.Verify(tok, "admin.pos.test")
	require.ErrorIs(t, err, ErrInvalid, "tenant token rejected by the platform signer")

	sameKeysOtherAud, _ := NewSigner(tenantRing, AudPlatform, 5*time.Minute)
	_, err = sameKeysOtherAud.Verify(tok, "admin.pos.test")
	require.ErrorIs(t, err, ErrInvalid, "audience checked even with the same key")
}

func TestRotationExpiryAndAlgConfusion(t *testing.T) {
	r := ring(t, "new", "old")
	oldOnly := config.KeyRing{r[1]}
	oldSigner, _ := NewSigner(oldOnly, AudTenant, time.Minute)
	tok, _, err := oldSigner.Sign("h", "1", Claims{})
	require.NoError(t, err)
	s, _ := NewSigner(r, AudTenant, time.Minute)
	_, err = s.Verify(tok, "h")
	require.NoError(t, err, "old kid still verifies")

	s.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	_, err = s.Verify(tok, "h")
	require.ErrorIs(t, err, ErrExpired)

	// alg=none and unknown kid are rejected.
	none := jwt.NewWithClaims(jwt.SigningMethodNone, Claims{RegisteredClaims: jwt.RegisteredClaims{
		Issuer: "h", Subject: "1", Audience: jwt.ClaimStrings{AudTenant},
		IssuedAt: jwt.NewNumericDate(time.Now()), ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
	}})
	none.Header["kid"] = "new"
	str, err := none.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)
	s.now = time.Now
	_, err = s.Verify(str, "h")
	require.ErrorIs(t, err, ErrInvalid)

	// A validly signed token with a lifetime over 15 minutes is refused.
	long := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{RegisteredClaims: jwt.RegisteredClaims{
		Issuer: "h", Subject: "1", Audience: jwt.ClaimStrings{AudTenant},
		IssuedAt: jwt.NewNumericDate(time.Now()), ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
	}})
	long.Header["kid"] = "new"
	str, err = long.SignedString(r[0].Bytes())
	require.NoError(t, err)
	_, err = s.Verify(str, "h")
	require.ErrorIs(t, err, ErrInvalid)

	_, err = NewSigner(r, AudTenant, time.Hour)
	require.Error(t, err)
}
