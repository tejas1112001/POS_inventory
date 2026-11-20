// Package secrets holds secret values and (from step 3 on) envelope encryption.
package secrets

import "log/slog"

// Redacted is what every secret prints as.
const Redacted = "[REDACTED]"

// Secret is a string that never shows up in logs, fmt output or JSON.
// Call Reveal only where the raw value is needed (a DB driver, a signer).
type Secret string

// Reveal returns the raw value.
func (s Secret) Reveal() string { return string(s) }

// IsZero reports whether the secret is empty.
func (s Secret) IsZero() bool { return s == "" }

// String hides the value from fmt.
func (s Secret) String() string { return Redacted }

// GoString hides the value from %#v.
func (s Secret) GoString() string { return Redacted }

// LogValue hides the value from slog.
func (s Secret) LogValue() slog.Value { return slog.StringValue(Redacted) }

// MarshalJSON hides the value from encoding/json.
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + Redacted + `"`), nil }

// MarshalText hides the value from text encoders.
func (s Secret) MarshalText() ([]byte, error) { return []byte(Redacted), nil }
