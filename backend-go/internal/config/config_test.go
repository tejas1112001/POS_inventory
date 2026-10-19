package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func randKey(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(b)
}

func validVars(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		"BASE_DOMAIN":       "localhost",
		"PLATFORM_NAME":     "Test POS",
		"APP_ENV":           "development",
		"PUBLIC_SCHEME":     "http",
		"DATABASE_URL":      "postgres://pos_app:devpass@localhost:5432/pos?sslmode=disable",
		"DATABASE_JOBS_URL": "postgres://pos_jobs:devpass@localhost:5432/pos?sslmode=disable",
		"JWT_KEYS":          "k1:" + randKey(t, 32),
		"PLATFORM_JWT_KEYS": "p1:" + randKey(t, 32),
		"MASTER_KEYS":       "m1:" + randKey(t, 32),
	}
}

func TestLoadValid(t *testing.T) {
	c, err := LoadFrom(validVars(t))
	require.NoError(t, err)
	require.Equal(t, "localhost", c.BaseDomain)
	require.Equal(t, "k1", c.JWTKeys.Current().ID)
	require.Len(t, c.JWTKeys.Current().Bytes(), 32)
	require.Equal(t, c.DatabaseURL, c.ListenDatabaseURL, "LISTEN_DATABASE_URL defaults to DATABASE_URL")
	require.Positive(t, c.DBPoolMax)
}

func TestMissingRequiredNamesVariableOnly(t *testing.T) {
	for _, name := range []string{"BASE_DOMAIN", "PLATFORM_NAME", "APP_ENV", "DATABASE_URL", "DATABASE_JOBS_URL", "JWT_KEYS", "PLATFORM_JWT_KEYS", "MASTER_KEYS"} {
		t.Run(name, func(t *testing.T) {
			v := validVars(t)
			delete(v, name)
			_, err := LoadFrom(v)
			require.Error(t, err)
			require.Contains(t, err.Error(), name)
		})
	}
}

func TestErrorsNeverContainValues(t *testing.T) {
	v := validVars(t)
	short := randKey(t, 16)
	v["JWT_KEYS"] = "k1:" + short
	v["DATABASE_URL"] = "mysql://user:hunter2-very-secret@db/x"
	_, err := LoadFrom(v)
	require.Error(t, err)
	require.NotContains(t, err.Error(), short)
	require.NotContains(t, err.Error(), "hunter2-very-secret")
	require.Contains(t, err.Error(), "at least 32 bytes")
}

func TestWeakKeysRejected(t *testing.T) {
	cases := map[string]string{
		"too short":       "k1:" + randKey(t, 31),
		"placeholder":     "k1:your_jwt_secret_here_your_jwt_secret_here",
		"changeme":        "k1:" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("changeme", 5))),
		"repeated bytes":  "k1:" + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 40))),
		"not base64":      "k1:!!!!",
		"missing kid":     randKey(t, 32),
		"bad kid":         "k 1:" + randKey(t, 32),
		"duplicate kid":   "k1:" + randKey(t, 32) + ",k1:" + randKey(t, 32),
		"weak second key": "k1:" + randKey(t, 32) + ",k2:" + randKey(t, 8),
	}
	for name, val := range cases {
		t.Run(name, func(t *testing.T) {
			v := validVars(t)
			v["JWT_KEYS"] = val
			_, err := LoadFrom(v)
			require.Error(t, err)
			require.Contains(t, err.Error(), "JWT_KEYS")
		})
	}
}

func TestMasterKeysMustBeExactly32Bytes(t *testing.T) {
	v := validVars(t)
	v["MASTER_KEYS"] = "m1:" + randKey(t, 48)
	_, err := LoadFrom(v)
	require.ErrorContains(t, err, "exactly 32 bytes")
}

func TestPlatformKeysMustDifferFromTenantKeys(t *testing.T) {
	v := validVars(t)
	k := randKey(t, 32)
	v["JWT_KEYS"] = "k1:" + k
	v["PLATFORM_JWT_KEYS"] = "p1:" + k
	_, err := LoadFrom(v)
	require.ErrorContains(t, err, "PLATFORM_JWT_KEYS must not reuse")
}

func TestKeyRingRotation(t *testing.T) {
	v := validVars(t)
	v["JWT_KEYS"] = "new:" + randKey(t, 32) + ", old:" + randKey(t, 64)
	c, err := LoadFrom(v)
	require.NoError(t, err)
	require.Equal(t, "new", c.JWTKeys.Current().ID)
	_, ok := c.JWTKeys.Lookup("old")
	require.True(t, ok)
	_, ok = c.JWTKeys.Lookup("missing")
	require.False(t, ok)
}

func TestKeyStringHidesMaterial(t *testing.T) {
	c, err := LoadFrom(validVars(t))
	require.NoError(t, err)
	k := c.JWTKeys.Current()
	require.NotContains(t, k.String(), hex.EncodeToString(k.Bytes()))
	require.Contains(t, k.String(), "REDACTED")
}

func TestHTTPSchemeOnlyInDev(t *testing.T) {
	v := validVars(t)
	v["APP_ENV"] = "production"
	_, err := LoadFrom(v)
	require.ErrorContains(t, err, "PUBLIC_SCHEME=http is only allowed")
}

func TestProductionRules(t *testing.T) {
	v := validVars(t)
	v["APP_ENV"] = "production"
	v["PUBLIC_SCHEME"] = "https"
	_, err := LoadFrom(v)
	require.Error(t, err)
	msg := err.Error()
	for _, want := range []string{"TRUSTED_PROXY_CIDRS", "STORAGE_DRIVER=s3", "sslmode=verify-full", "real BASE_DOMAIN", "at least 16 characters"} {
		require.Contains(t, msg, want)
	}

	pw := "Zq7w" + strings.Repeat("x9Lk", 5)
	v["BASE_DOMAIN"] = "pos.example.com"
	v["TRUSTED_PROXY_CIDRS"] = "10.0.0.0/8, 172.16.0.1"
	v["STORAGE_DRIVER"] = "s3"
	v["DATABASE_URL"] = "postgres://pos_app:" + pw + "@db:5432/pos?sslmode=verify-full"
	v["DATABASE_JOBS_URL"] = "postgres://pos_jobs:" + pw + "@db:5432/pos?sslmode=verify-full"
	c, err := LoadFrom(v)
	require.NoError(t, err)
	require.Len(t, c.TrustedProxies, 2)
	require.Equal(t, "172.16.0.1/32", c.TrustedProxies[1].String())
}

func TestInvalidValues(t *testing.T) {
	cases := map[string][2]string{
		"env":         {"APP_ENV", "staging"},
		"mode":        {"POSD_MODE", "desktop"},
		"domain":      {"BASE_DOMAIN", "https://pos.example.com"},
		"cidr":        {"TRUSTED_PROXY_CIDRS", "10.0.0.0/33"},
		"duration":    {"ACCESS_TOKEN_TTL", "soon"},
		"long ttl":    {"ACCESS_TOKEN_TTL", "2h"},
		"storage":     {"STORAGE_DRIVER", "ftp"},
		"log level":   {"LOG_LEVEL", "trace"},
		"cors any":    {"CORS_DEV_ORIGINS", "*"},
		"pool":        {"DB_POOL_MIN", "500"},
		"unpaged cap": {"MAX_UNPAGED_ROWS", "0"},
	}
	for name, kv := range cases {
		t.Run(name, func(t *testing.T) {
			v := validVars(t)
			v[kv[0]] = kv[1]
			_, err := LoadFrom(v)
			require.Error(t, err)
			require.Contains(t, err.Error(), kv[0])
		})
	}
}

func TestCORSDevOriginsOnlyInDevelopment(t *testing.T) {
	v := validVars(t)
	v["CORS_DEV_ORIGINS"] = "http://localhost:5173"
	c, err := LoadFrom(v)
	require.NoError(t, err)
	require.Equal(t, []string{"http://localhost:5173"}, c.CORSDevOrigins)

	v["APP_ENV"] = "test"
	_, err = LoadFrom(v)
	require.ErrorContains(t, err, "CORS_DEV_ORIGINS is only allowed")
}
