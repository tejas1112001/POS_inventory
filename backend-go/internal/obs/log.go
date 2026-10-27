// Package obs sets up logging, metrics and (optionally) tracing.
package obs

import (
	"io"
	"log/slog"
	"regexp"
	"strings"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
)

// sensitiveKeyParts: an attribute whose key contains one of these is never
// logged. Matching is case-insensitive.
var sensitiveKeyParts = []string{
	"password", "passwd", "pwd", "secret", "token", "authorization", "cookie",
	"api_key", "apikey", "private_key", "signature", "otp", "totp", "recovery_code",
	"dsn", "database_url", "credential", "session_id", "kek", "dek",
}

// urlCredentials matches the user:password@ part of a URL.
var urlCredentials = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://[^:/@\s]*:)[^@\s]+@`)

// bearerToken matches "Bearer <token>" anywhere in a string.
var bearerToken = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/=-]+`)

// NewLogger builds the process logger. format is "json" or "text".
func NewLogger(w io.Writer, level, format string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl, ReplaceAttr: redactAttr}
	var h slog.Handler
	if format == "text" {
		h = slog.NewTextHandler(w, opts)
	} else {
		h = slog.NewJSONHandler(w, opts)
	}
	return slog.New(h)
}

// redactAttr hides sensitive attributes. It runs for every attribute,
// including ones added with Logger.With and ones inside groups.
func redactAttr(_ []string, a slog.Attr) slog.Attr {
	if isSensitiveKey(a.Key) {
		return slog.String(a.Key, secrets.Redacted)
	}
	switch a.Value.Kind() {
	case slog.KindString:
		if s := a.Value.String(); strings.Contains(s, "://") || strings.Contains(strings.ToLower(s), "bearer") {
			return slog.String(a.Key, RedactString(s))
		}
	case slog.KindAny:
		if err, ok := a.Value.Any().(error); ok {
			return slog.String(a.Key, RedactString(err.Error()))
		}
	}
	return a
}

// RedactString removes URL credentials and bearer tokens from free text.
func RedactString(s string) string {
	s = urlCredentials.ReplaceAllString(s, "${1}"+secrets.Redacted+"@")
	return bearerToken.ReplaceAllString(s, "${1}"+secrets.Redacted)
}

func isSensitiveKey(k string) bool {
	l := strings.ToLower(k)
	for _, p := range sensitiveKeyParts {
		if strings.Contains(l, p) {
			return true
		}
	}
	return false
}
