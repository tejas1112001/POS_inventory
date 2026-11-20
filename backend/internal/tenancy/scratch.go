package tenancy

import (
	"regexp"
	"strings"
	"time"
)

// Restore schemas (OPERATIONS §6.3). Their names never match the tenant
// pattern ^t_[a-z0-9]{6,20}$, and the resolver only accepts SchemaName, so a
// scratch or old schema can never be served as a live tenant.
var (
	scratchPattern = regexp.MustCompile(`^r_([a-z0-9]{6,20})_[0-9]{12}$`)
	oldPattern     = regexp.MustCompile(`^t_([a-z0-9]{6,20})_old_[0-9]{12}$`)
)

// stamp is the minute a restore schema was made (UTC).
func stamp(at time.Time) string { return at.UTC().Format("200601021504") }

// ScratchSchema is a validated restore scratch schema name
// r_<tenant id>_<yyyymmddhhmm>. Its role is <name>_rw.
type ScratchSchema struct{ name string }

// ParseScratchSchema validates s against ^r_[a-z0-9]{6,20}_[0-9]{12}$.
func ParseScratchSchema(s string) (ScratchSchema, error) {
	if !scratchPattern.MatchString(s) {
		return ScratchSchema{}, ErrInvalidName
	}
	return ScratchSchema{name: s}, nil
}

// ScratchFor names the scratch schema for restoring a tenant at a time.
func ScratchFor(tenantID string, at time.Time) (ScratchSchema, error) {
	if !ValidTenantID(tenantID) {
		return ScratchSchema{}, ErrInvalidName
	}
	return ParseScratchSchema("r_" + tenantID + "_" + stamp(at))
}

// String returns the bare schema name.
func (s ScratchSchema) String() string { return s.name }

// IsZero reports whether s was never set.
func (s ScratchSchema) IsZero() bool { return s.name == "" }

// Role is the scratch schema's own role, which holds privileges on the
// scratch schema only.
func (s ScratchSchema) Role() string { return s.name + "_rw" }

// TenantID is the tenant the scratch schema restores.
func (s ScratchSchema) TenantID() string {
	if m := scratchPattern.FindStringSubmatch(s.name); m != nil {
		return m[1]
	}
	return ""
}

// SearchPath is the scratch schema, then the shared extensions schema.
func (s ScratchSchema) SearchPath() string { return `"` + s.name + `", extensions` }

// OldSchema is a validated name t_<tenant id>_old_<yyyymmddhhmm>: the live
// schema a replace restore set aside.
type OldSchema struct{ name string }

// ParseOldSchema validates s against ^t_[a-z0-9]{6,20}_old_[0-9]{12}$.
func ParseOldSchema(s string) (OldSchema, error) {
	if !oldPattern.MatchString(s) {
		return OldSchema{}, ErrInvalidName
	}
	return OldSchema{name: s}, nil
}

// OldSchemaFor names the set-aside schema of a tenant at a time.
func OldSchemaFor(tenantID string, at time.Time) (OldSchema, error) {
	if !ValidTenantID(tenantID) {
		return OldSchema{}, ErrInvalidName
	}
	return ParseOldSchema("t_" + tenantID + "_old_" + stamp(at))
}

// String returns the bare schema name.
func (o OldSchema) String() string { return o.name }

// IsZero reports whether o was never set.
func (o OldSchema) IsZero() bool { return o.name == "" }

// TenantID is the tenant the old schema belonged to.
func (o OldSchema) TenantID() string {
	return strings.TrimPrefix(strings.SplitN(o.name, "_old_", 2)[0], "t_")
}
