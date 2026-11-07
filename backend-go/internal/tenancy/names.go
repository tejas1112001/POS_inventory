// Package tenancy resolves tenants from hosts and opens tenant and platform
// database sessions. InTenantTx is the only way to reach tenant data.
package tenancy

import (
	"crypto/rand"
	"errors"
	"regexp"
)

var (
	tenantIDPattern = regexp.MustCompile(`^[a-z0-9]{10}$`)
	schemaPattern   = regexp.MustCompile(`^t_[a-z0-9]{6,20}$`)
	rolePattern     = regexp.MustCompile(`^t_[a-z0-9]{6,20}_rw$`)
)

// ErrInvalidName means a tenant ID, schema or role name failed validation.
var ErrInvalidName = errors.New("tenancy: invalid name")

// SchemaName is a validated tenant schema name. The zero value is invalid;
// the only constructors are ParseSchemaName and SchemaFor.
type SchemaName struct{ name string }

// ParseSchemaName validates s against ^t_[a-z0-9]{6,20}$.
func ParseSchemaName(s string) (SchemaName, error) {
	if !schemaPattern.MatchString(s) {
		return SchemaName{}, ErrInvalidName
	}
	return SchemaName{name: s}, nil
}

// String returns the bare schema name.
func (s SchemaName) String() string { return s.name }

// IsZero reports whether s was never set.
func (s SchemaName) IsZero() bool { return s.name == "" }

// SearchPath is the exact search_path for a tenant session (TENANCY §5.4):
// the tenant schema first, then the shared extensions schema.
func (s SchemaName) SearchPath() string { return `"` + s.name + `", extensions` }

// RoleName is a validated tenant DB role name.
type RoleName struct{ name string }

// ParseRoleName validates s against ^t_[a-z0-9]{6,20}_rw$.
func ParseRoleName(s string) (RoleName, error) {
	if !rolePattern.MatchString(s) {
		return RoleName{}, ErrInvalidName
	}
	return RoleName{name: s}, nil
}

// String returns the bare role name.
func (r RoleName) String() string { return r.name }

// IsZero reports whether r was never set.
func (r RoleName) IsZero() bool { return r.name == "" }

// ValidTenantID reports whether id is a well-formed tenant ID.
func ValidTenantID(id string) bool { return tenantIDPattern.MatchString(id) }

// SchemaFor derives the schema and role of a tenant ID.
func SchemaFor(id string) (SchemaName, RoleName, error) {
	if !ValidTenantID(id) {
		return SchemaName{}, RoleName{}, ErrInvalidName
	}
	return SchemaName{name: "t_" + id}, RoleName{name: "t_" + id + "_rw"}, nil
}

// idAlphabet is lowercase base32 (RFC 4648 letters, digits 2–7).
const idAlphabet = "abcdefghijklmnopqrstuvwxyz234567"

// NewTenantID returns 10 random base32 characters (50 bits) from crypto/rand.
func NewTenantID() string {
	var b [10]byte
	_, _ = rand.Read(b[:]) // never fails on supported platforms
	for i := range b {
		b[i] = idAlphabet[b[i]&31]
	}
	return string(b[:])
}
