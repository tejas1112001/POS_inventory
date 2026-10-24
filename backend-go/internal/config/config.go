// Package config reads every setting from the environment and fails fast on
// missing or weak values. Error messages name the variable, never its value.
package config

import (
	"errors"
	"fmt"
	"net/mail"
	"net/netip"
	"net/url"
	"reflect"
	"runtime"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/caarlos0/env/v11"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/security"
)

// Environments.
const (
	EnvDevelopment = "development"
	EnvTest        = "test"
	EnvProduction  = "production"
)

// Config is the validated runtime configuration.
type Config struct {
	Mode        string `env:"POSD_MODE" envDefault:"all"`
	HTTPAddr    string `env:"HTTP_ADDR" envDefault:":8080"`
	MetricsAddr string `env:"METRICS_ADDR" envDefault:"127.0.0.1:9090"`
	StaticDir   string `env:"STATIC_DIR"`

	BaseDomain   string `env:"BASE_DOMAIN,required,notEmpty"`
	PublicScheme string `env:"PUBLIC_SCHEME" envDefault:"https"`
	PlatformName string `env:"PLATFORM_NAME,required,notEmpty"`
	AppEnv       string `env:"APP_ENV,required,notEmpty"`

	TrustedProxyCIDRsRaw string         `env:"TRUSTED_PROXY_CIDRS"`
	TrustedProxies       []netip.Prefix `env:"-"`

	DatabaseURL         secrets.Secret `env:"DATABASE_URL,required,notEmpty"`
	DatabaseMigratorURL secrets.Secret `env:"DATABASE_MIGRATOR_URL"`
	DatabaseJobsURL     secrets.Secret `env:"DATABASE_JOBS_URL,required,notEmpty"`
	ListenDatabaseURL   secrets.Secret `env:"LISTEN_DATABASE_URL"`
	DBPoolMax           int32          `env:"DB_POOL_MAX"`
	DBPoolMin           int32          `env:"DB_POOL_MIN" envDefault:"2"`
	DBStatementTimeout  time.Duration  `env:"DB_STATEMENT_TIMEOUT" envDefault:"5s"`

	JWTKeysRaw         secrets.Secret `env:"JWT_KEYS,required,notEmpty"`
	PlatformJWTKeysRaw secrets.Secret `env:"PLATFORM_JWT_KEYS,required,notEmpty"`
	MasterKeysRaw      secrets.Secret `env:"MASTER_KEYS,required,notEmpty"`
	JWTKeys            KeyRing        `env:"-"`
	PlatformJWTKeys    KeyRing        `env:"-"`
	MasterKeys         KeyRing        `env:"-"`

	AccessTokenTTL         time.Duration `env:"ACCESS_TOKEN_TTL" envDefault:"10m"`
	PlatformAccessTokenTTL time.Duration `env:"PLATFORM_ACCESS_TOKEN_TTL" envDefault:"5m"`
	RefreshTTLRemember     time.Duration `env:"REFRESH_TTL_REMEMBER" envDefault:"720h"`
	RefreshTTLSession      time.Duration `env:"REFRESH_TTL_SESSION" envDefault:"12h"`
	PlatformSessionMax     time.Duration `env:"PLATFORM_SESSION_MAX" envDefault:"8h"`
	PlatformIdleTimeout    time.Duration `env:"PLATFORM_IDLE_TIMEOUT" envDefault:"30m"`

	PlatformIPAllowlistRaw string         `env:"PLATFORM_IP_ALLOWLIST"`
	PlatformIPAllowlist    []netip.Prefix `env:"-"`

	StorageDriver string         `env:"STORAGE_DRIVER" envDefault:"fs"`
	StorageFSRoot string         `env:"STORAGE_FS_ROOT" envDefault:"data/files"`
	S3Endpoint    string         `env:"S3_ENDPOINT"`
	S3Bucket      string         `env:"S3_BUCKET"`
	S3AccessKey   secrets.Secret `env:"S3_ACCESS_KEY"`
	S3SecretKey   secrets.Secret `env:"S3_SECRET_KEY"`
	S3Region      string         `env:"S3_REGION" envDefault:"us-east-1"`

	MaxUnpagedRows int `env:"MAX_UNPAGED_ROWS" envDefault:"5000"`

	// Platform SMTP (EMAIL_AND_RECEIPTS §2, Q-13): Amazon SES ap-south-1 or
	// Brevo, or Mailpit in development. Without SMTP_HOST (allowed outside
	// production) platform mail stays queued.
	SMTPHost         string         `env:"SMTP_HOST"`
	SMTPPort         int            `env:"SMTP_PORT" envDefault:"587"`
	SMTPTLS          string         `env:"SMTP_TLS" envDefault:"starttls"`
	SMTPUsername     string         `env:"SMTP_USERNAME"`
	SMTPPassword     secrets.Secret `env:"SMTP_PASSWORD"`
	MailFromAddress  string         `env:"MAIL_FROM_ADDRESS"`
	SMTPDenyCIDRsRaw string         `env:"SMTP_DENY_CIDRS"`
	SMTPDenyCIDRs    []netip.Prefix `env:"-"`

	// Billing (BILLING_DESIGN): none, fake (development and test only) or
	// razorpay. Default: fake outside production, none in production.
	BillingProvider       string         `env:"BILLING_PROVIDER"`
	RazorpayKeyID         string         `env:"RAZORPAY_KEY_ID"`
	RazorpayKeySecret     secrets.Secret `env:"RAZORPAY_KEY_SECRET"`
	RazorpayWebhookSecret secrets.Secret `env:"RAZORPAY_WEBHOOK_SECRET"`
	BillingGraceDays      int            `env:"BILLING_GRACE_DAYS" envDefault:"7"`
	WebhookMaxAge         time.Duration  `env:"WEBHOOK_MAX_AGE" envDefault:"72h"`

	// Tenant exports (OPERATIONS §6.2, S7): off unless both are set. The
	// age identity (private key) is for posctl restore only; posd refuses
	// to start with it (it must never reach the API process).
	ExportAgeRecipientRaw string         `env:"EXPORT_AGE_RECIPIENT"`
	ExportAgeRecipient    age.Recipient  `env:"-"`
	ExportManifestKey     secrets.Secret `env:"EXPORT_MANIFEST_KEY"`
	ExportAgeIdentity     secrets.Secret `env:"EXPORT_AGE_IDENTITY"`
	// Deletion timeline (BILLING_DESIGN §3): read-only or suspended by
	// billing for ReadOnlyDays → pending_deletion (final export); eligible
	// for deletion RetentionDays after day 0. Nothing is dropped by a job.
	ReadOnlyDays  int `env:"READ_ONLY_DAYS" envDefault:"30"`
	RetentionDays int `env:"RETENTION_DAYS" envDefault:"90"`

	LogLevel  string `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat string `env:"LOG_FORMAT" envDefault:"json"`

	// HSTSEnabled turns on Strict-Transport-Security. Off until the go-live
	// checklist (OPERATIONS §9 step 10) is signed off.
	HSTSEnabled bool `env:"HSTS_ENABLED" envDefault:"false"`

	RequestTimeout       time.Duration  `env:"REQUEST_TIMEOUT" envDefault:"30s"`
	ShutdownDrainDelay   time.Duration  `env:"SHUTDOWN_DRAIN_DELAY" envDefault:"5s"`
	ShutdownTimeout      time.Duration  `env:"SHUTDOWN_TIMEOUT" envDefault:"25s"`
	CORSDevOriginsRaw    string         `env:"CORS_DEV_ORIGINS"`
	CORSDevOrigins       []string       `env:"-"`
	BootstrapAdminEmail  string         `env:"BOOTSTRAP_ADMIN_EMAIL"`
	BootstrapAdminPasswd secrets.Secret `env:"BOOTSTRAP_ADMIN_PASSWORD"`
}

// IsProduction reports whether production rules apply.
func (c *Config) IsProduction() bool { return c.AppEnv == EnvProduction }

// Load reads the process environment.
func Load() (*Config, error) { return parse(env.Options{}) }

// LoadFrom reads the given variables only (tests).
func LoadFrom(vars map[string]string) (*Config, error) {
	return parse(env.Options{Environment: vars})
}

func parse(opts env.Options) (*Config, error) {
	var c Config
	if err := env.ParseWithOptions(&c, opts); err != nil {
		return nil, scrubEnvError(err)
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// scrubEnvError keeps the variable names from env errors and drops anything
// that could echo a value.
func scrubEnvError(err error) error {
	var agg env.AggregateError
	if !errors.As(err, &agg) {
		return errors.New("config: invalid environment")
	}
	var msgs []string
	for _, e := range agg.Errors {
		var (
			notSet   env.VarIsNotSetError
			empty    env.EmptyVarError
			parseErr env.ParseError
		)
		switch {
		case errors.As(e, &notSet):
			msgs = append(msgs, notSet.Key+" is required")
		case errors.As(e, &empty):
			msgs = append(msgs, empty.Key+" must not be empty")
		case errors.As(e, &parseErr):
			msgs = append(msgs, envKeyOf(parseErr.Name)+" has an invalid value")
		default:
			msgs = append(msgs, "invalid environment variable")
		}
	}
	return errors.New("config: " + strings.Join(msgs, "; "))
}

// envKeyOf maps a Config field name to its environment variable name.
func envKeyOf(field string) string {
	f, ok := reflect.TypeFor[Config]().FieldByName(field)
	if !ok {
		return "an environment variable"
	}
	key, _, _ := strings.Cut(f.Tag.Get("env"), ",")
	return key
}

func (c *Config) validate() error {
	var errs []string
	add := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }

	switch c.AppEnv {
	case EnvDevelopment, EnvTest, EnvProduction:
	default:
		add("APP_ENV must be development, test or production")
	}
	switch c.Mode {
	case "all", "api", "worker":
	default:
		add("POSD_MODE must be all, api or worker")
	}
	switch c.PublicScheme {
	case "https":
	case "http":
		if c.AppEnv != EnvDevelopment && c.AppEnv != EnvTest {
			add("PUBLIC_SCHEME=http is only allowed with APP_ENV=development or test")
		}
	default:
		add("PUBLIC_SCHEME must be https or http")
	}

	c.BaseDomain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(c.BaseDomain), "."))
	if !validDomain(c.BaseDomain) {
		add("BASE_DOMAIN must be a bare host name such as pos.example.com")
	}

	var err error
	if c.TrustedProxies, err = parseCIDRs(c.TrustedProxyCIDRsRaw); err != nil {
		add("TRUSTED_PROXY_CIDRS: %v", err)
	}
	if c.PlatformIPAllowlist, err = parseCIDRs(c.PlatformIPAllowlistRaw); err != nil {
		add("PLATFORM_IP_ALLOWLIST: %v", err)
	}

	for name, s := range map[string]secrets.Secret{
		"DATABASE_URL": c.DatabaseURL, "DATABASE_JOBS_URL": c.DatabaseJobsURL,
		"DATABASE_MIGRATOR_URL": c.DatabaseMigratorURL, "LISTEN_DATABASE_URL": c.ListenDatabaseURL,
	} {
		if s.IsZero() {
			continue
		}
		if msg := CheckDSN(s.Reveal(), c.IsProduction()); msg != "" {
			add("%s %s", name, msg)
		}
	}
	if c.ListenDatabaseURL.IsZero() {
		c.ListenDatabaseURL = c.DatabaseURL
	}

	if c.JWTKeys, err = parseKeyRing("JWT_KEYS", c.JWTKeysRaw.Reveal(), 0); err != nil {
		add("%v", err)
	}
	if c.PlatformJWTKeys, err = parseKeyRing("PLATFORM_JWT_KEYS", c.PlatformJWTKeysRaw.Reveal(), 0); err != nil {
		add("%v", err)
	}
	if c.MasterKeys, err = parseKeyRing("MASTER_KEYS", c.MasterKeysRaw.Reveal(), 32); err != nil {
		add("%v", err)
	}
	if sharesKey(c.JWTKeys, c.PlatformJWTKeys) {
		add("PLATFORM_JWT_KEYS must not reuse any JWT_KEYS key")
	}
	if sharesKey(c.MasterKeys, c.JWTKeys) || sharesKey(c.MasterKeys, c.PlatformJWTKeys) {
		add("MASTER_KEYS must not reuse a JWT key")
	}

	for name, d := range map[string]time.Duration{
		"ACCESS_TOKEN_TTL": c.AccessTokenTTL, "PLATFORM_ACCESS_TOKEN_TTL": c.PlatformAccessTokenTTL,
		"REFRESH_TTL_REMEMBER": c.RefreshTTLRemember, "REFRESH_TTL_SESSION": c.RefreshTTLSession,
		"PLATFORM_SESSION_MAX": c.PlatformSessionMax, "PLATFORM_IDLE_TIMEOUT": c.PlatformIdleTimeout,
		"DB_STATEMENT_TIMEOUT": c.DBStatementTimeout, "REQUEST_TIMEOUT": c.RequestTimeout,
		"SHUTDOWN_TIMEOUT": c.ShutdownTimeout,
	} {
		if d <= 0 {
			add("%s must be a positive duration", name)
		}
	}
	if c.ShutdownDrainDelay < 0 {
		add("SHUTDOWN_DRAIN_DELAY must not be negative")
	}
	if c.AccessTokenTTL > time.Hour || c.PlatformAccessTokenTTL > 15*time.Minute {
		add("ACCESS_TOKEN_TTL must be <= 1h and PLATFORM_ACCESS_TOKEN_TTL <= 15m")
	}

	if c.DBPoolMax == 0 {
		c.DBPoolMax = int32(min(4*runtime.NumCPU(), 40)) // #nosec G115 -- bounded by 40
	}
	if c.DBPoolMax < 1 || c.DBPoolMin < 0 || c.DBPoolMin > c.DBPoolMax {
		add("DB_POOL_MAX must be >= 1 and DB_POOL_MIN between 0 and DB_POOL_MAX")
	}
	if c.MaxUnpagedRows < 1 || c.MaxUnpagedRows > 50000 {
		add("MAX_UNPAGED_ROWS must be between 1 and 50000")
	}

	switch c.StorageDriver {
	case "fs":
		if c.StorageFSRoot == "" {
			add("STORAGE_FS_ROOT is required with STORAGE_DRIVER=fs")
		}
	case "s3":
		if c.S3Endpoint == "" || c.S3Bucket == "" || c.S3AccessKey.IsZero() || c.S3SecretKey.IsZero() {
			add("STORAGE_DRIVER=s3 needs S3_ENDPOINT, S3_BUCKET, S3_ACCESS_KEY and S3_SECRET_KEY")
		}
		if u, err := url.Parse(c.S3Endpoint); err == nil && c.IsProduction() && u.Scheme != "https" {
			add("S3_ENDPOINT must use https in production")
		}
	default:
		add("STORAGE_DRIVER must be fs or s3")
	}
	switch strings.ToLower(c.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		add("LOG_LEVEL must be debug, info, warn or error")
	}
	switch c.LogFormat {
	case "json", "text":
	default:
		add("LOG_FORMAT must be json or text")
	}

	if c.CORSDevOriginsRaw != "" {
		if c.AppEnv != EnvDevelopment {
			add("CORS_DEV_ORIGINS is only allowed with APP_ENV=development")
		}
		for o := range strings.SplitSeq(c.CORSDevOriginsRaw, ",") {
			o = strings.TrimSpace(o)
			u, err := url.Parse(o)
			if err != nil || u.Scheme == "" || u.Host == "" || u.Path != "" || o == "*" {
				add("CORS_DEV_ORIGINS entries must be exact origins like http://localhost:5173")
				continue
			}
			c.CORSDevOrigins = append(c.CORSDevOrigins, o)
		}
	}

	c.validateMail(add)
	c.validateBilling(add)
	c.validateExports(add)

	if !c.BootstrapAdminPasswd.IsZero() && security.IsLeakedSecret(c.BootstrapAdminPasswd.Reveal()) {
		add("BOOTSTRAP_ADMIN_PASSWORD was leaked and must not be used")
	}

	if c.IsProduction() {
		if c.PublicScheme != "https" {
			add("production requires PUBLIC_SCHEME=https")
		}
		if len(c.TrustedProxies) == 0 {
			add("production requires TRUSTED_PROXY_CIDRS")
		}
		if c.StorageDriver != "s3" {
			add("production requires STORAGE_DRIVER=s3")
		}
		if strings.EqualFold(c.LogLevel, "debug") {
			add("LOG_LEVEL=debug is not allowed in production")
		}
		if c.BaseDomain == "localhost" {
			add("production requires a real BASE_DOMAIN")
		}
	}

	if len(errs) > 0 {
		return errors.New("config: " + strings.Join(errs, "; "))
	}
	return nil
}

// Billing providers.
const (
	BillingNone     = "none"
	BillingFake     = "fake"
	BillingRazorpay = "razorpay"
)

func (c *Config) validateBilling(add func(string, ...any)) {
	c.BillingProvider = strings.ToLower(strings.TrimSpace(c.BillingProvider))
	if c.BillingProvider == "" {
		c.BillingProvider = BillingFake
		if c.IsProduction() {
			c.BillingProvider = BillingNone
		}
	}
	secret := c.RazorpayWebhookSecret.Reveal()
	weak := !c.RazorpayWebhookSecret.IsZero() && (len(secret) < 16 || looksLikePlaceholder(secret))
	switch c.BillingProvider {
	case BillingNone:
	case BillingFake:
		if c.IsProduction() {
			add("BILLING_PROVIDER=fake is not allowed in production")
		}
		if weak {
			add("RAZORPAY_WEBHOOK_SECRET must be at least 16 random characters")
		}
	case BillingRazorpay:
		if c.RazorpayKeyID == "" || c.RazorpayKeySecret.IsZero() || c.RazorpayWebhookSecret.IsZero() {
			add("BILLING_PROVIDER=razorpay requires RAZORPAY_KEY_ID, RAZORPAY_KEY_SECRET and RAZORPAY_WEBHOOK_SECRET")
		} else if weak {
			add("RAZORPAY_WEBHOOK_SECRET must be at least 16 random characters")
		}
	default:
		add("BILLING_PROVIDER must be none, fake or razorpay")
	}
	if c.BillingGraceDays < 1 || c.BillingGraceDays > 60 {
		add("BILLING_GRACE_DAYS must be between 1 and 60")
	}
	if c.WebhookMaxAge < time.Hour || c.WebhookMaxAge > 30*24*time.Hour {
		add("WEBHOOK_MAX_AGE must be between 1h and 720h")
	}
}

// ExportsEnabled reports whether tenant exports can run.
func (c *Config) ExportsEnabled() bool { return c.ExportAgeRecipient != nil }

// ParseAgeRecipient parses exactly one age recipient (age1…).
func ParseAgeRecipient(s string) (age.Recipient, error) {
	rs, err := age.ParseRecipients(strings.NewReader(strings.TrimSpace(s)))
	if err != nil || len(rs) != 1 {
		return nil, errors.New("must be one age recipient (age1…)")
	}
	return rs[0], nil
}

// CheckManifestKey validates EXPORT_MANIFEST_KEY (an HMAC key).
func CheckManifestKey(k secrets.Secret) string {
	v := k.Reveal()
	if len(v) < 32 || looksLikePlaceholder(v) {
		return "must be at least 32 random characters"
	}
	return ""
}

func (c *Config) validateExports(add func(string, ...any)) {
	if !c.ExportAgeIdentity.IsZero() {
		add("EXPORT_AGE_IDENTITY must not be set for posd: only posctl restore uses it")
	}
	hasR, hasK := strings.TrimSpace(c.ExportAgeRecipientRaw) != "", !c.ExportManifestKey.IsZero()
	switch {
	case hasR != hasK:
		add("EXPORT_AGE_RECIPIENT and EXPORT_MANIFEST_KEY must be set together")
	case hasR:
		r, err := ParseAgeRecipient(c.ExportAgeRecipientRaw)
		if err != nil {
			add("EXPORT_AGE_RECIPIENT %v", err)
		}
		if msg := CheckManifestKey(c.ExportManifestKey); msg != "" {
			add("EXPORT_MANIFEST_KEY %s", msg)
		}
		c.ExportAgeRecipient = r
	}
	if c.ReadOnlyDays < 7 || c.ReadOnlyDays > 365 {
		add("READ_ONLY_DAYS must be between 7 and 365")
	}
	if c.RetentionDays <= c.ReadOnlyDays || c.RetentionDays > 3650 {
		add("RETENTION_DAYS must be more than READ_ONLY_DAYS and at most 3650")
	}
}

func (c *Config) validateMail(add func(string, ...any)) {
	switch c.SMTPTLS {
	case "starttls", "implicit":
	case "none":
		if c.IsProduction() {
			add("SMTP_TLS=none is not allowed in production")
		}
	default:
		add("SMTP_TLS must be starttls, implicit or none")
	}
	if c.SMTPPort < 1 || c.SMTPPort > 65535 {
		add("SMTP_PORT must be a TCP port")
	}
	c.SMTPHost = strings.ToLower(strings.TrimSpace(c.SMTPHost))
	if c.SMTPHost != "" && !validDomain(c.SMTPHost) {
		if _, err := netip.ParseAddr(c.SMTPHost); err != nil {
			add("SMTP_HOST must be a host name or IP address")
		}
	}
	if c.IsProduction() && c.SMTPHost == "" {
		add("production requires SMTP_HOST (platform mail)")
	}
	if (c.SMTPUsername == "") != c.SMTPPassword.IsZero() {
		add("SMTP_USERNAME and SMTP_PASSWORD must be set together")
	}
	if c.MailFromAddress == "" {
		c.MailFromAddress = "no-reply@" + c.BaseDomain
	}
	if a, err := mail.ParseAddress(c.MailFromAddress); err != nil || a.Address != c.MailFromAddress || !strings.Contains(c.MailFromAddress, "@") {
		add("MAIL_FROM_ADDRESS must be a bare email address")
	}
	var err error
	if c.SMTPDenyCIDRs, err = parseCIDRs(c.SMTPDenyCIDRsRaw); err != nil {
		add("SMTP_DENY_CIDRS: %v", err)
	}
}

// MailDomain is the domain of MAIL_FROM_ADDRESS, used for Message-IDs and
// EHLO.
func (c *Config) MailDomain() string {
	_, d, _ := strings.Cut(c.MailFromAddress, "@")
	return d
}

// CheckDSN validates a Postgres URL without ever returning its contents.
func CheckDSN(dsn string, production bool) string {
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		return "must be a postgres:// URL"
	}
	var problems []string
	if pw, ok := u.User.Password(); ok {
		if security.IsLeakedSecret(pw) {
			problems = append(problems, "uses a password that was leaked in git history")
		} else if production && (len(pw) < 16 || looksLikePlaceholder(pw)) {
			problems = append(problems, "needs a generated password of at least 16 characters in production")
		}
	}
	if production && u.Query().Get("sslmode") != "verify-full" {
		problems = append(problems, "must use sslmode=verify-full in production")
	}
	return strings.Join(problems, ", ")
}

func parseCIDRs(raw string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for part := range strings.SplitSeq(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		p, err := netip.ParsePrefix(part)
		if err != nil {
			a, aerr := netip.ParseAddr(part)
			if aerr != nil {
				return nil, fmt.Errorf("%q is not a CIDR or IP", part)
			}
			p = netip.PrefixFrom(a, a.BitLen())
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

func validDomain(d string) bool {
	if d == "" || len(d) > 253 || strings.ContainsAny(d, ":/ ") {
		return false
	}
	for label := range strings.SplitSeq(d, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}
