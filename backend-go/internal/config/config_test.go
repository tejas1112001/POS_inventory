package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
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
	v["S3_ENDPOINT"] = "https://s3.ap-south-1.amazonaws.com"
	v["S3_BUCKET"] = "pos-files"
	v["S3_ACCESS_KEY"] = "test-access-key"
	v["S3_SECRET_KEY"] = "s3-secret-access-key-value-long-enough"
	v["DATABASE_URL"] = "postgres://pos_app:" + pw + "@db:5432/pos?sslmode=verify-full"
	v["DATABASE_JOBS_URL"] = "postgres://pos_jobs:" + pw + "@db:5432/pos?sslmode=verify-full"
	_, err = LoadFrom(v)
	require.ErrorContains(t, err, "SMTP_HOST")
	v["SMTP_HOST"] = "email-smtp.ap-south-1.amazonaws.com"
	v["SMTP_TLS"] = "none"
	_, err = LoadFrom(v)
	require.ErrorContains(t, err, "SMTP_TLS=none")
	v["SMTP_TLS"] = "starttls"
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

func TestS3SettingsRequired(t *testing.T) {
	v := validVars(t)
	v["STORAGE_DRIVER"] = "s3"
	_, err := LoadFrom(v)
	require.ErrorContains(t, err, "S3_ENDPOINT, S3_BUCKET")
}

func TestMailSettings(t *testing.T) {
	c, err := LoadFrom(validVars(t))
	require.NoError(t, err)
	require.Equal(t, "no-reply@localhost", c.MailFromAddress, "default sender")
	require.Equal(t, "localhost", c.MailDomain())
	require.Equal(t, 587, c.SMTPPort)

	for name, kv := range map[string][2]string{
		"tls mode":   {"SMTP_TLS", "ssl"},
		"port":       {"SMTP_PORT", "70000"},
		"from":       {"MAIL_FROM_ADDRESS", "POS <no-reply@pos.test>"},
		"from crlf":  {"MAIL_FROM_ADDRESS", "a@b.test\r\nBcc: x@y.test"},
		"deny cidrs": {"SMTP_DENY_CIDRS", "10.0.0.0/33"},
		"host":       {"SMTP_HOST", "smtp host"},
		"user only":  {"SMTP_USERNAME", "apikey"},
	} {
		v := validVars(t)
		v[kv[0]] = kv[1]
		_, err := LoadFrom(v)
		require.ErrorContains(t, err, kv[0], name)
	}
	v := validVars(t)
	v["SMTP_HOST"], v["SMTP_TLS"], v["SMTP_PORT"] = "mailpit", "none", "1025"
	v["SMTP_DENY_CIDRS"] = "10.20.0.0/16, 172.31.0.0/16"
	c, err = LoadFrom(v)
	require.NoError(t, err, "plain relay is fine in development")
	require.Len(t, c.SMTPDenyCIDRs, 2)
}

func TestBillingConfig(t *testing.T) {
	c, err := LoadFrom(validVars(t))
	require.NoError(t, err)
	require.Equal(t, BillingFake, c.BillingProvider, "fake by default outside production")
	require.Equal(t, 7, c.BillingGraceDays)
	require.Equal(t, 72*time.Hour, c.WebhookMaxAge)

	bad := map[string]map[string]string{
		"unknown provider": {"BILLING_PROVIDER": "stripe"},
		"razorpay keys":    {"BILLING_PROVIDER": "razorpay", "RAZORPAY_KEY_ID": "rzp_test_x"},
		"weak secret": {"BILLING_PROVIDER": "razorpay", "RAZORPAY_KEY_ID": "rzp_test_x", "RAZORPAY_KEY_SECRET": "abcdefghijklmnop",
			"RAZORPAY_WEBHOOK_SECRET": "short"},
		"placeholder secret": {"RAZORPAY_WEBHOOK_SECRET": "changeme-changeme"},
		"grace":              {"BILLING_GRACE_DAYS": "0"},
		"max age":            {"WEBHOOK_MAX_AGE": "10m"},
	}
	for name, extra := range bad {
		v := validVars(t)
		for k, val := range extra {
			v[k] = val
		}
		_, err := LoadFrom(v)
		require.Error(t, err, name)
		require.NotContains(t, err.Error(), "abcdefghijklmnop", "secrets never appear in errors")
	}

	v := validVars(t)
	v["BILLING_PROVIDER"] = "razorpay"
	v["RAZORPAY_KEY_ID"] = "rzp_test_x"
	v["RAZORPAY_KEY_SECRET"] = "abcdefghijklmnop"
	v["RAZORPAY_WEBHOOK_SECRET"] = "Wh7-" + strings.Repeat("k3Zq", 5)
	c, err = LoadFrom(v)
	require.NoError(t, err)
	require.Equal(t, BillingRazorpay, c.BillingProvider)
}

func TestBillingFakeRefusedInProduction(t *testing.T) {
	v := validVars(t)
	v["APP_ENV"] = "production"
	v["BILLING_PROVIDER"] = "fake"
	_, err := LoadFrom(v)
	require.ErrorContains(t, err, "BILLING_PROVIDER=fake is not allowed in production")
	delete(v, "BILLING_PROVIDER")
	_, err = LoadFrom(v)
	require.NotContains(t, err.Error(), "BILLING_PROVIDER", "production defaults to none")
}

func TestExportConfig(t *testing.T) {
	c, err := LoadFrom(validVars(t))
	require.NoError(t, err)
	require.False(t, c.ExportsEnabled(), "off unless configured")
	require.Equal(t, 30, c.ReadOnlyDays)
	require.Equal(t, 90, c.RetentionDays)

	id, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	key := "Mk-" + strings.Repeat("r8Vq", 9)
	v := validVars(t)
	v["EXPORT_AGE_RECIPIENT"] = id.Recipient().String()
	v["EXPORT_MANIFEST_KEY"] = key
	c, err = LoadFrom(v)
	require.NoError(t, err)
	require.True(t, c.ExportsEnabled())

	for name, extra := range map[string]map[string]string{
		"identity in posd":   {"EXPORT_AGE_IDENTITY": id.String()},
		"recipient only":     {"EXPORT_MANIFEST_KEY": ""},
		"key only":           {"EXPORT_AGE_RECIPIENT": ""},
		"bad recipient":      {"EXPORT_AGE_RECIPIENT": "age1notakey"},
		"two recipients":     {"EXPORT_AGE_RECIPIENT": id.Recipient().String() + "\n" + id.Recipient().String()},
		"short key":          {"EXPORT_MANIFEST_KEY": "short"},
		"read-only days":     {"READ_ONLY_DAYS": "3"},
		"retention too soon": {"RETENTION_DAYS": "30"},
	} {
		bad := map[string]string{}
		for k, val := range v {
			bad[k] = val
		}
		for k, val := range extra {
			if val == "" {
				delete(bad, k)
			} else {
				bad[k] = val
			}
		}
		_, err := LoadFrom(bad)
		require.Error(t, err, name)
		require.NotContains(t, err.Error(), key, "%s: secrets never appear in errors", name)
		require.NotContains(t, err.Error(), id.String(), name)
	}
}
