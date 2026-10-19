// Package config reads every setting from the environment and fails fast on
// missing or weak values. Error messages name the variable, never its value.
package config

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"reflect"
	"runtime"
	"strings"
	"time"

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

	StorageDriver string `env:"STORAGE_DRIVER" envDefault:"fs"`

	MaxUnpagedRows int `env:"MAX_UNPAGED_ROWS" envDefault:"5000"`

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
	case "fs", "s3":
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
