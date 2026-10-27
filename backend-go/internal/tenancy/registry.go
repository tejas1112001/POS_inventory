package tenancy

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/singleflight"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
)

// ReservedSlugs can never be tenant slugs. platform.reserved_slugs extends
// this list in the database (checked at provisioning).
var ReservedSlugs = map[string]bool{
	"admin": true, "www": true, "api": true, "app": true, "apps": true, "mail": true,
	"email": true, "smtp": true, "imap": true, "pop": true, "mx": true, "ns1": true, "ns2": true,
	"static": true, "assets": true, "cdn": true, "files": true, "media": true, "img": true,
	"status": true, "docs": true, "help": true, "support": true, "billing": true, "pay": true,
	"blog": true, "dev": true, "staging": true, "stage": true, "test": true, "demo": true,
	"root": true, "system": true, "platform": true, "dashboard": true, "console": true,
	"login": true, "auth": true, "sso": true, "account": true, "accounts": true, "secure": true,
	"ftp": true, "webmail": true, "portal": true, "internal": true, "metrics": true, "health": true,
}

// servedStatuses are the statuses a host can resolve to. The tenant gate
// (step 5) then decides what each status may do. Everything else
// (provisioning, failed, deleted) is indistinguishable from "no such tenant".
var servedStatuses = map[string]bool{
	"trialing": true, "active": true, "past_due": true, "grace": true, "cancelled": true,
	"read_only": true, "suspended": true, "pending_deletion": true,
}

// Cache TTLs (TENANCY §5 step 2, §5.5).
const (
	RegistryTTL         = 30 * time.Second
	RegistryTTLDegraded = 5 * time.Second
)

type regEntry struct {
	t     Tenant
	found bool
}

// Registry resolves tenants by slug or ID with a TTL cache that also stores
// misses, so unknown and deleted slugs take the same code path and time.
type Registry struct {
	sessions *Sessions
	cache    *TTLCache[string, regEntry]
	sf       singleflight.Group
	ttl      func() time.Duration
}

// NewRegistry creates a registry. ttl may be nil (RegistryTTL).
func NewRegistry(s *Sessions, ttl func() time.Duration) *Registry {
	if ttl == nil {
		ttl = func() time.Duration { return RegistryTTL }
	}
	return &Registry{sessions: s, cache: NewTTLCache[string, regEntry](), ttl: ttl}
}

// BySlug resolves a host label. Unknown, reserved, malformed, deleted,
// failed and provisioning tenants all return the same apperr.NotFound.
func (r *Registry) BySlug(ctx context.Context, slug string) (Tenant, error) {
	if ReservedSlugs[slug] || !validSlug(slug) {
		return Tenant{}, apperr.NotFound()
	}
	return r.lookup(ctx, "slug:"+slug, bySlug, slug)
}

const (
	qTenantBySlug = `SELECT id, slug::text, name, status, schema_name, db_role, db_cluster, coalesce(plan_id, 0)
		FROM platform.tenants WHERE slug = $1`
	qTenantByID = `SELECT id, slug::text, name, status, schema_name, db_role, db_cluster, coalesce(plan_id, 0)
		FROM platform.tenants WHERE id = $1`
)

type lookupKind int

const (
	bySlug lookupKind = iota
	byID
)

// ByID resolves a tenant ID (token claims, jobs).
func (r *Registry) ByID(ctx context.Context, id string) (Tenant, error) {
	if !ValidTenantID(id) {
		return Tenant{}, apperr.NotFound()
	}
	return r.lookup(ctx, "id:"+id, byID, id)
}

func (r *Registry) lookup(ctx context.Context, key string, kind lookupKind, arg string) (Tenant, error) {
	if e, ok := r.cache.Get(key); ok {
		return entryResult(e)
	}
	v, err, _ := r.sf.Do(key, func() (any, error) {
		e, err := r.load(ctx, kind, arg)
		if err != nil {
			return regEntry{}, err
		}
		r.cache.Set(key, e, r.ttl())
		return e, nil
	})
	if err != nil {
		return Tenant{}, err
	}
	return entryResult(v.(regEntry))
}

func entryResult(e regEntry) (Tenant, error) {
	if !e.found {
		return Tenant{}, apperr.NotFound()
	}
	return e.t, nil
}

func (r *Registry) load(ctx context.Context, kind lookupKind, arg string) (regEntry, error) {
	var (
		t                    Tenant
		schema, role, status string
	)
	err := r.sessions.InPlatformTx(ctx, TxOptions{Access: ReadOnly}, func(tx PlatformTx) error {
		var row pgx.Row
		if kind == byID {
			row = tx.QueryRow(ctx, qTenantByID, arg)
		} else {
			row = tx.QueryRow(ctx, qTenantBySlug, arg)
		}
		return row.Scan(&t.ID, &t.Slug, &t.Name, &status, &schema, &role, &t.DBCluster, &t.PlanID)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return regEntry{}, nil
	}
	if err != nil {
		return regEntry{}, err
	}
	if !servedStatuses[status] {
		return regEntry{}, nil
	}
	// Re-validate what the database returned; never trust it blindly.
	if t.Schema, err = ParseSchemaName(schema); err != nil {
		return regEntry{}, err
	}
	if t.Role, err = ParseRoleName(role); err != nil {
		return regEntry{}, err
	}
	if t.Schema.String() != "t_"+t.ID || t.Role.String() != "t_"+t.ID+"_rw" {
		return regEntry{}, ErrInvalidName
	}
	t.Status = status
	return regEntry{t: t, found: true}, nil
}

const qSlugRedirect = `SELECT t.slug::text FROM platform.tenant_slug_history h
	JOIN platform.tenants t ON t.id = h.tenant_id
	WHERE h.old_slug = $1 AND h.redirect_until > now()
	  AND t.status IN ('trialing','active','past_due','grace','cancelled','read_only','suspended','pending_deletion')`

// RedirectSlug returns the current slug for a renamed tenant's old slug.
// Misses are cached like tenant lookups.
func (r *Registry) RedirectSlug(ctx context.Context, old string) (string, bool) {
	if !validSlug(old) {
		return "", false
	}
	key := "redirect:" + old
	if e, ok := r.cache.Get(key); ok {
		return e.t.Slug, e.found
	}
	var e regEntry
	err := r.sessions.InPlatformTx(ctx, TxOptions{Access: ReadOnly}, func(tx PlatformTx) error {
		return tx.QueryRow(ctx, qSlugRedirect, old).Scan(&e.t.Slug)
	})
	switch {
	case err == nil:
		e.found = true
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return "", false // transient error: no redirect, not cached
	}
	r.cache.Set(key, e, r.ttl())
	return e.t.Slug, e.found
}

// Flush drops every cached entry (on tenant_changed and after a listener
// reconnect).
func (r *Registry) Flush() { r.cache.Flush() }

func validSlug(s string) bool {
	if len(s) < 3 || len(s) > 30 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '-' && i > 0 && i < len(s)-1 && s[i-1] != '-':
		default:
			return false
		}
	}
	return true
}
