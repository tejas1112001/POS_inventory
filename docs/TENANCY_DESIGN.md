# Tenancy Design — Schema-per-Tenant

| | |
|---|---|
| Status | Phase 1 design, revised 2026-10-15 after review (decisions applied); awaiting approval |
| Model | One PostgreSQL database (per cluster/shard), one `platform` schema, one `t_<id>` schema per tenant, one shared pgx pool |
| Top property | Tenant A can never read, write or infer tenant B's data (S-req 17) |

---

## 1. Should we use schema-per-tenant? (challenge)

| Model | Isolation | Ops cost | Scale ceiling (one DB) | Per-tenant export/delete/move | Cross-tenant analytics |
|---|---|---|---|---|---|
| **Shared tables + `tenant_id` + RLS** | Logical. One missing predicate or `BYPASSRLS` role leaks. RLS mitigates, but RLS + pooling + `SECURITY DEFINER` are easy to get wrong. | Lowest: one migration, one catalog | 100k+ tenants | Hard (`DELETE … WHERE tenant_id`), slow, error-prone | Easy |
| **Schema per tenant** (chosen) | Strong by construction: wrong schema = no rows, plus per-tenant DB roles = permission denied | Medium: migrations × N schemas, catalog grows | ~1,000 comfortable, ~5,000 with tuning (see §1.1) | Easy: dump/copy one schema, `DROP SCHEMA`, move to another DB | Needs fan-out queries (platform only) |
| **Database per tenant** | Strongest (separate catalogs, can be separate clusters) | Highest: pools/connections per DB, N backups | Bounded by connections (~100s per cluster) | Easiest | Hard |

**Recommendation: proceed with schema-per-tenant**, with these escape hatches designed in from day one:
1. `tenants.db_cluster` + a **connection resolver**: a big tenant can move to its own database (or a shard database holding ≤1,000 schemas) with no handler changes.
2. No `tenant_id` columns are required, but every cache key, lock key, file key and log line already carries `tenant_id`, so a later move to shared+RLS for *small* tenants stays possible.

**What would change my recommendation**
- **Shared + RLS** if the go-to-market expects many thousands of tiny/free tenants (e.g. >5,000 within 2 years), or if cross-tenant analytics becomes a core feature.
- **DB-per-tenant** for enterprise tenants with contractual isolation, data residency, customer-managed keys or very large volume. The resolver already covers this as a per-tenant option.

### 1.1 Tenant ceiling (estimate, to be measured in Phase 3)

| Item | Per tenant | 500 tenants | 1,000 | 5,000 |
|---|---|---|---|---|
| Tables (incl. ledger, outbox, audit) | ~36 | 18k | 36k | 180k |
| `pg_class` entries (tables + indexes + sequences + TOAST) | ~260 | 130k | 260k | 1.3M |
| Tenant migration (1 small DDL step, parallelism 8) | ~0.3–1 s | ~1 min | ~2 min | ~10 min |
| Whole-DB `pg_dump` locks (`AccessShareLock` per table) | ~36 | 18k | 36k | 180k |

Consequences and mitigations:
- **`max_locks_per_transaction` must be raised** (default 64 × max_connections is ≈ 6.4k locks: a whole-DB `pg_dump` fails beyond ~150 tenants). Set it to 1024+; prefer physical backups (WAL-based PITR) for the whole DB, logical dumps per tenant (OPERATIONS §6).
- **Backend relcache/catcache memory** grows with the number of distinct relations a connection touches. Mitigate with `MaxConnLifetime=30m`, `MaxConnIdleTime=5m` on the pool, and track `pg_backend_memory_contexts` in the load test.
- **Planner/autovacuum**: many small tables are fine; autovacuum naptime may need tuning at 5k.
- **Trigger to move a tenant to a dedicated DB**: >20 % of DB CPU or I/O for a week, >50 GB of data, >5 M order lines per year, p95 latency SLO misses caused by that tenant, or a contractual isolation/residency requirement.
- **Trigger to shard** (new DB per ~1,000 schemas): catalog >300k relations, migration run >10 min, or nightly per-tenant exports not finishing in their window.

---

## 2. Platform vs tenant table split (every table)

### 2.1 `platform` schema (shared, owned by the platform; never reachable from a tenant transaction)

| Table | Purpose |
|---|---|
| `tenants` | Registry: id, slug, schema_name, status, plan, owner, db_cluster, schema_version, lifecycle timestamps |
| `tenant_slug_history` | Old slugs → tenant (redirects, rare audited renames) |
| `reserved_slugs` | DB extension of the code-level reserved list |
| `tenant_domains` | **Designed, not built**: custom domains (CNAME + verification) |
| `provisioning_runs` | Provisioning state machine (step, attempts, error) |
| `plans` | Plan catalog: price, Razorpay plan id, limits, features |
| `subscriptions` | Tenant ↔ plan ↔ Razorpay subscription, status, periods, trial/grace ends |
| `invoices` | SaaS invoices mirrored from Razorpay |
| `billing_events` | Raw webhook log, unique by event id |
| `platform_users` | Super admin & platform staff (never tenant users) |
| `platform_roles`, `platform_user_roles` | `super_admin`, `billing_admin`, `support` |
| `platform_recovery_codes` | Hashed MFA recovery codes |
| `platform_refresh_tokens` | Platform sessions (hashed, rotated, family) |
| `platform_login_challenges` | Short-lived MFA challenges / step-up records |
| `impersonation_sessions` | Time-limited, reasoned, revocable impersonations + one-time exchange codes (hashed) |
| `permission_catalog` | Global catalog of tenant permissions (source of truth is code; synced at deploy) |
| `platform_audit_log` | Append-only log of every platform action |
| `platform_notifications` | Announcements (one / group / all), channels, schedule |
| `notification_log` | Dedup of scheduled notifications `(tenant_id, kind, period_key)` unique |
| `email_outbox` | Platform mail (welcome, verify, reset, billing, announcements) |
| `mail_dispatch` | Index of tenants that have pending tenant mail (cross-schema worker) |
| `tenant_exports` | Per-tenant export/restore-test records (location, manifest hash, result) |
| `migration_runs`, `migration_run_items` | Tenant migration runs, per-tenant result, durations |
| `goose_db_version` | Platform migration track |

Job queue tables live in their own schema `river` (River's own migrations), reachable only by the jobs role.

### 2.2 `t_<id>` schema (one per tenant; all business data)

| Group | Tables |
|---|---|
| Identity | `users`, `roles`, `permissions` (tenant copy of the catalog, keeps integer `permissionIds` contract), `role_permissions`, `refresh_tokens`, `auth_tokens` (invite / reset, hashed) |
| Settings | `settings` (singleton), `tenant_email_settings` (singleton, encrypted secret), `receipt_templates` |
| Master data | `stores`, `warehouses`, `categories`, `sub_categories`, `brands`, `units`, `suppliers`, `customers` |
| Catalogue | `products`, `product_variants`, `store_products` |
| Stock | `inventories`, `stock_movements` (ledger, new), `stock_adjustments`, `pos_locks` |
| Selling | `sales`, `order_items`, `sale_returns`, `return_items`, `receipts`, `idempotency_keys` |
| Buying | `purchases`, `purchase_items` |
| Files | `files` (metadata; bytes in object storage under the tenant prefix) |
| Messaging | `email_outbox`, `banner_dismissals` |
| Audit | `audit_log` (append-only for the tenant role) |
| Ops | `goose_db_version` (tenant migration track), trigger function `mark_outbox_pending()` (owned by `pos_owner`, see EMAIL_AND_RECEIPTS §4.2) |

The new system starts with an **empty database** (the old data is demo data; there is no legacy import). The old `reports` table has no equivalent.

Shared schema `extensions` (owned by `pos_owner`, `CREATE` revoked from `PUBLIC`) holds `citext`, `pg_trgm` and `pg_stat_statements`. See §5.4.

DDL for the tenant schema: PHASE1_DESIGN §5. DDL for `platform`: §3 below.

---

## 3. Platform schema DDL (key tables)

```sql
CREATE SCHEMA IF NOT EXISTS extensions AUTHORIZATION pos_owner;   -- see §5.4
REVOKE CREATE ON SCHEMA extensions FROM PUBLIC;
CREATE EXTENSION IF NOT EXISTS citext  WITH SCHEMA extensions;
CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA extensions;
CREATE EXTENSION IF NOT EXISTS pg_stat_statements WITH SCHEMA extensions;  -- needs shared_preload_libraries
CREATE SCHEMA platform AUTHORIZATION pos_owner;
-- platform migrations run with search_path = platform, extensions (connection parameter, §8)

CREATE TABLE platform.tenants (
  id                 text PRIMARY KEY CHECK (id ~ '^[a-z0-9]{10}$'),           -- server-generated, crypto/rand base32
  schema_name        text NOT NULL UNIQUE CHECK (schema_name ~ '^t_[a-z0-9]{6,20}$'),
  db_role            text NOT NULL UNIQUE CHECK (db_role ~ '^t_[a-z0-9]{6,20}_rw$'),
  slug               citext NOT NULL UNIQUE
                     CHECK (slug ~ '^[a-z0-9](?:[a-z0-9-]{1,28}[a-z0-9])$' AND slug !~ '--'),
  name               text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
  status             text NOT NULL CHECK (status IN
                       ('provisioning','failed','trialing','active','past_due','grace',
                        'suspended','read_only','cancelled','pending_deletion','deleted')),
  plan_id            bigint REFERENCES platform.plans(id),
  owner_email        citext NOT NULL,
  billing_email      citext,
  db_cluster         text NOT NULL DEFAULT 'default',   -- key into DB_CLUSTERS env/secret map, never a DSN with a password
  schema_version     bigint NOT NULL DEFAULT 0,
  last_activity_at   timestamptz,
  storage_bytes      bigint NOT NULL DEFAULT 0,
  suspended_at       timestamptz, suspend_reason text,
  deletion_requested_at timestamptz, purge_after timestamptz,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  CHECK (schema_name = 't_' || id),
  CHECK (db_role = 't_' || id || '_rw')
);

CREATE TABLE platform.tenant_slug_history (
  old_slug   citext PRIMARY KEY,
  tenant_id  text NOT NULL REFERENCES platform.tenants(id),
  changed_at timestamptz NOT NULL DEFAULT now(),
  changed_by bigint NOT NULL REFERENCES platform.platform_users(id),
  redirect_until timestamptz NOT NULL        -- old slug cannot be re-registered until then (default 1 year)
);

CREATE TABLE platform.mail_dispatch (
  tenant_id       text PRIMARY KEY REFERENCES platform.tenants(id),
  pending_since   timestamptz NOT NULL DEFAULT now(),
  next_attempt_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE platform.impersonation_sessions (
  id            uuid PRIMARY KEY,
  platform_user_id bigint NOT NULL REFERENCES platform.platform_users(id),
  tenant_id     text NOT NULL REFERENCES platform.tenants(id),
  reason        text NOT NULL CHECK (length(reason) BETWEEN 15 AND 500),
  mode          text NOT NULL CHECK (mode IN ('read_only','read_write')),
  code_hash     bytea UNIQUE,              -- one-time exchange code, SHA-256, NULL after use
  code_expires_at timestamptz,
  started_at    timestamptz NOT NULL DEFAULT now(),
  expires_at    timestamptz NOT NULL,      -- started_at + 15 min
  revoked_at    timestamptz, revoked_by bigint
);
```

`plans`, `subscriptions`, `invoices`, `billing_events`: BILLING_DESIGN §2. `email_outbox`: EMAIL_AND_RECEIPTS §4. `platform_users` etc.: PHASE1_DESIGN §7.

---

## 4. Schema naming and validation (P0)

- **Tenant ID**: 10 chars from `crypto/rand`, lowercase base32 alphabet `[a-z2-7]` (≈ 50 bits). Generated only by the provisioning service.
- **Schema** `t_<id>`, **DB role** `t_<id>_rw`. Both derived from the ID, both re-validated by `^t_[a-z0-9]{6,20}$` at **every** use (`tenancy.SchemaName` is a Go type with an unexported field, constructible only through `tenancy.ParseSchemaName`, which validates).
- Identifiers are always quoted with `pgx.Identifier{name}.Sanitize()`. Where Postgres accepts a parameter we use one (`set_config('search_path', $1, true)`); DDL that cannot be parameterized (`CREATE SCHEMA`, `SET LOCAL ROLE`, `GRANT`) uses the validated, quoted identifier only, and is built only in `internal/db/ident.go` (the single exemption from the `constsql` lint rule).
- **Never** derived from slug, company name, Host header, JWT claim text or request body. The Host gives a *slug*; the slug is looked up in `platform.tenants`; only the **stored** `schema_name` is used.
- Tests (Phase 3): fuzz `ParseSchemaName`; injection strings in slug/Host/claims (`t_abc"; DROP SCHEMA`, unicode confusables, `public`, `platform`, `pg_catalog`, `information_schema`) never reach SQL.

---

## 5. Per-request session flow

```
Request ─▶ [1 TrustedHost] ─▶ [2 TenantResolve] ─▶ [3 Auth] ─▶ [4 AuthState] ─▶ [5 TenantGate]
        ─▶ [6 RBAC] ─▶ handler ─▶ service ─▶ tenancy.InTenantTx(ctx, fn) ─▶ repo (sqlc) ─▶ commit/rollback
```

1. **TrustedHost**: if `RemoteAddr` ∈ `TRUSTED_PROXY_CIDRS`, read the first `X-Forwarded-Host`/`X-Forwarded-Proto`; otherwise use `r.Host`. Lowercase, strip port, must be `admin.<BASE_DOMAIN>`, `<label>.<BASE_DOMAIN>` (one label), or `<BASE_DOMAIN>` itself. Anything else → `421 Misdirected Request` with the generic body. Never echo the host.
2. **TenantResolve**: `admin` → platform router only (tenant routes not mounted there, and vice versa). `<label>` → slug lookup in a TTL cache (30 s, positive and **negative** entries, key `slug:<slug>`) backed by `platform.tenants`, with invalidation via `NOTIFY tenant_changed` (§5.5). Unknown/deleted/failed/provisioning → same generic 404 (same body, same code path, negative cache keeps timing flat). Old slug → `308` to the new host for non-API GETs; `404` for API calls.
3. **Auth**: verify JWT (HS256, `kid`-selected key, `iss = <host>`, `aud = tenant`, `exp`, `nbf`, max 15 min). Claim `tid` must equal the resolved tenant ID, else `401` (logged as a security event: forged/mismatched tenant).
4. **AuthState**: load `{status, role_id, token_version}` for `(tid, sub)` from a cache (key `auth:<tid>:<uid>`, TTL 30 s, invalidated via `NOTIFY auth_changed`, §5.5). Inactive, deleted, or `token_version` newer than the token's `tv` → `401`. So deactivation and role change take effect within seconds, not at token expiry. Permissions are resolved from the **current** `role_id`, never from a claim.
5. **TenantGate** (one rule, the same table as BILLING_DESIGN §3):

   | Tenant status | Login / refresh | Reads | Writes |
   |---|---|---|---|
   | `trialing`, `active`, `past_due`, `grace` | ✔ | ✔ | ✔ |
   | `cancelled` (paid period still running) | ✔ | ✔ | ✔ until `current_period_end`; then the lifecycle job sets `read_only` |
   | `read_only` | ✔ | ✔ | ✘ `403 TENANT_READ_ONLY`, **except billing routes** (below) |
   | `suspended`, `pending_deletion` | ✘ `403 TENANT_SUSPENDED` (refresh tokens rejected too) | ✘ | ✘ |

   **Billing exemption.** Routes marked `gate.AllowReadOnly` pass the `read_only` gate so a tenant can pay to get back: `GET /api/billing/subscription`, `POST /api/billing/checkout`, plus the session plumbing `POST /api/auth/refresh`, `POST /api/auth/logout`, `GET /api/auth/me`, `GET /api/notifications/banners`, `POST /api/notifications/:id/dismiss`. The marker is an allow-list in code; a router-walk test asserts that no other write route has it. The exemption never applies to `suspended`/`pending_deletion` (those tenants pay through the emailed link, Q-11) or to impersonation sessions.
   Impersonation `read_only` → writes `403 IMPERSONATION_READ_ONLY`, billing routes included.
6. **RBAC**: route declares `rbac.Require("sales","create")`; deny by default (a route without a declaration fails a startup self-check and a unit test that walks the router).
7. **InTenantTx** (the only way to get a tenant DB handle):

```go
// internal/tenancy/session.go (sketch)
func (s *Sessions) InTenantTx(ctx context.Context, t Tenant, mode TxMode, fn func(TenantTx) error) error {
    pool := s.resolver.Pool(t.DBCluster)                 // shared pool for "default"
    tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: mode.access()})
    if err != nil { return err }
    defer tx.Rollback(ctx)                                // no-op after commit
    // All LOCAL (transaction-scoped). SET ROLE takes no bind parameter, so it is built only inside
    // internal/db/ident.go from the validated RoleName (the single constsql exemption, THREAT_MODEL §3.2).
    if err = db.SetLocalRole(ctx, tx, t.DBRole); err != nil { return err }
    if _, err = tx.Exec(ctx,
        `SELECT set_config('search_path', $1, true),
                set_config('app.tenant_id', $2, true),
                set_config('statement_timeout', $3, true),
                set_config('lock_timeout', '3s', true)`,
        t.Schema.SearchPath(), // `"t_ab12cd34ef", extensions` (§5.4)
        t.ID, mode.statementTimeout()); err != nil { return err }
    if err = fn(TenantTx{tx: tx, tenant: t}); err != nil { return err }
    // Post-condition (cheap tripwire, THREAT_MODEL §4): the session must still be what we set.
    var role, path string
    if err = tx.QueryRow(ctx, `SELECT current_user, current_setting('search_path')`).Scan(&role, &path); err != nil { return err }
    if role != t.DBRole.String() || path != t.Schema.SearchPath() {
        return security.ErrSessionTampered // rollback via defer, 500, event=security.session_tampered
    }
    return tx.Commit(ctx)
}
```

- **Request path: `SET LOCAL` / `set_config(…, true)` only.** The migration and restore path uses connection startup parameters and no `SET` statement at all (§8). CI rule (a `go/analysis` check plus a grep over `*.go` and `*.sql`, including migration files): no `SET search_path`, `SET ROLE`, `SET SESSION`, `RESET`, or `set_config('search_path'|'role', …, false)` anywhere. `RuntimeParams` keys `search_path` and `role` may appear **only** in `internal/migrate/conn.go` (the single carve-out, built from a validated `SchemaName`), which the export/restore code also uses.
- `search_path` is the tenant schema **plus `extensions`** (+ `pg_catalog` implicitly). §5.4 explains why `extensions` must be on the path. **Challenge to the brief**: the brief proposes `"t_xxx", platform`. I recommend *not* putting `platform` on the tenant path. An unqualified name that is missing in the tenant schema would silently resolve to a platform table (e.g. `email_outbox`, `audit_log` exist in both), and the tenant role would need platform privileges. The data a tenant request needs from `platform` (status, plan limits, features) is read before the tenant transaction through the platform repository (cached).
- `TenantTx` and `PlatformTx` are **distinct Go types**. Tenant repositories accept only `TenantTx`; platform repositories accept only `PlatformTx`. Mixing them is a compile error.
- `InPlatformTx` does the same with `SET LOCAL ROLE pos_platform` and `search_path = extensions` (no `platform` on the path, so an unqualified table name in a platform query fails instead of resolving somewhere else).
- GET handlers use `READ ONLY` transactions (cheap, safer, enables read-replica routing later).

### 5.1 Pool and "context leak impossible" (S-req 19)

- One shared `pgxpool` for tenant + platform traffic (size `DB_POOL_MAX`, default `4 × GOMAXPROCS`, ≤ 40), a second small pool for the job runner (`pos_jobs` role), and dedicated short-lived connections for migrations.
- The login role's **default `search_path` is empty** (`ALTER ROLE pos_app SET search_path = ''`) and it has **no table privileges at all**. Any query that escapes `InTenantTx`/`InPlatformTx` fails loudly (`relation does not exist` / `permission denied`) instead of hitting a wrong schema.
- `SET LOCAL` state ends with the transaction, on commit **and** rollback. A pool `AfterRelease` hook destroys any connection that is returned while still in a transaction (`conn.PgConn().TxStatus() != 'I'`).
- `pgx` is configured with `DefaultQueryExecMode = QueryExecModeExec` (see §5.2), so no per-connection prepared-statement state can survive between tenants.
- Tests (step 2, re-run at scale in Phase 3): pool size 1 and 2; 3+ tenants hammering the same queries concurrently; after every request assert `SHOW search_path` = `""` and `current_user = pos_app` on the released connection; panic and error paths; context cancellation mid-query.

### 5.2 The pgx statement-cache decision

**Problem.** pgx's default mode `QueryExecModeCacheStatement` prepares and caches statements per connection keyed by SQL text. The pgx v5.11 source says so directly: *"If the database schema is modified or the search_path is changed after a statement is cached then the first execution of a previously cached query may fail."* Postgres itself re-parses a prepared statement when `search_path` changes, but pgx caches the **description** (parameter/result OIDs and formats). With schema-per-tenant, the same SQL text runs under different `search_path`s on the same connection. During a rolling migration the tenants can also be at different schema versions (different columns/types), and any per-schema type (enum/domain) has a different OID per tenant.

**Options considered**

| Option | Verdict |
|---|---|
| Schema-qualify every query (`"t_x".sales`) | Rejected. sqlc generates static SQL, so names would be templated at runtime; every tenant then has distinct SQL text, and the statement cache explodes (statements × tenants × connections) |
| `QueryExecModeCacheDescribe` | Rejected: same stale-description problem |
| `QueryExecModeDescribeExec` | Safe, but 2 round trips per query |
| Clear the statement cache on every acquire | Fragile; loses the benefit anyway |
| **`QueryExecModeExec`** (chosen) | Unnamed statement, one round trip, parameter types inferred from Go types, text format. No cache, so nothing can bind to the wrong schema. Also compatible with PgBouncer transaction pooling |
| `QueryExecModeSimpleProtocol` | **Rejected, and banned by a test that inspects the pool config.** Client-side interpolation, and the simple protocol accepts several statements in one string, so an injection could append `; SET ROLE …`. The extended protocol used by `Exec` mode allows exactly one statement per call |

**Rules that make `Exec` mode robust**
- **No per-schema custom types** in tenant schemas: `text` + `CHECK` instead of `ENUM`, no domains, no composite types. All OIDs are built-ins, so they are identical across tenants.
- `numeric` ↔ `shopspring/decimal.Decimal` via its `sql.Scanner`/`driver.Valuer` (works in text mode; sqlc `overrides` map `numeric` to it). `jsonb` args passed as `[]byte`/typed structs. `uuid` via `pgtype.UUID`/`google/uuid`.
- sqlc expands `SELECT *` into explicit columns at generation time, and we forbid `*` in hand-written SQL anyway.

**How sqlc fits.** sqlc reads `db/migrations/tenant/*.sql` as the schema (unqualified names) and generates `tenantdb.Queries` from `db/queries/tenant/*.sql`. The generated code calls `DBTX.Exec/Query/QueryRow(ctx, sql, args...)`. We pass `TenantTx.Q()` (a `tenantdb.Queries` bound to the `pgx.Tx` with `search_path` already set), so unqualified names resolve to the tenant schema. Platform queries live in `db/queries/platform/*.sql`, are **always schema-qualified** (`platform.tenants`), and generate a separate package `platformdb`. A CI test fails if a tenant query mentions `platform.` or `river.`, or if a platform query has an unqualified table.

**Cost.** No plan caching: each query is parsed and planned. For the short OLTP queries here that is ~0.05–0.2 ms each. Phase 3 benchmarks compare `Exec` against `DescribeExec` and `CacheStatement` (single tenant) on the hot paths. If the overhead is ever material, the fallback is per-connection statement names salted with the schema version (`stmt_<hash(sql)>_<schema>`). We are not starting with it.

**Tests.** (a) Same connection, two tenants at different migration versions, same SQL text: each returns its own rows and columns. (b) A deliberately reproduced failure with `CacheStatement` mode in a test, documenting why we do not use it. (c) Concurrent cross-tenant requests on a pool of 1.

### 5.3 Database roles (defense in depth)

| Role | Login | Privileges |
|---|---|---|
| `pos_owner` | no | Owns `platform`, `river`, every `t_*` schema and object. Never used by request traffic |
| `pos_migrator` | yes | `CREATEROLE`, member of `pos_owner` (`SET TRUE`, `INHERIT FALSE`). Creates the tenant roles, so on PG ≥ 16 it automatically holds `ADMIN OPTION` on them and can grant them to `pos_app`/`pos_jobs`. `createrole_self_grant` stays empty, so it does **not** inherit tenant privileges. Used only by `posctl migrate`, provisioning DDL and restore |
| `pos_app` | yes | **No direct privileges**, empty default `search_path`. `GRANT pos_platform TO pos_app WITH INHERIT FALSE, SET TRUE`, and the same for each `t_<id>_rw` (PG ≥ 16 syntax) |
| `pos_platform` | no | DML on `platform.*`, `USAGE` on `extensions`, nothing on `t_*` |
| `t_<id>_rw` | no | `USAGE` on `t_<id>` and `extensions`, DML on its tables (`INSERT, SELECT` only on `audit_log`). Nothing on `platform` (the outbox trigger function runs as its owner, EMAIL_AND_RECEIPTS §4.2). Nothing else |
| `pos_jobs` | yes | `river` schema + `SET` membership like `pos_app` (workers open tenant sessions the same way) |
| `pos_readonly_ops` | yes | Optional, for humans: `pg_read_all_stats`, no data |

Effect: even if application code ran a tenant query with the wrong `search_path` or forgot the session, Postgres answers **permission denied**. It does **not** stop SQL injection that switches role itself (`set_config('role', …)` fits inside one statement, and `pos_app` may `SET` to every tenant role). Injection is prevented by parameterized sqlc queries and the constant-SQL lint rule (THREAT_MODEL §3.2); the post-condition check in `InTenantTx` is a tripwire, not a guarantee. This is listed as a residual risk (THREAT_MODEL §4).

**Grants on later tables.** Provisioning runs `ALTER DEFAULT PRIVILEGES FOR ROLE pos_owner IN SCHEMA "t_<id>" GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO "t_<id>_rw"` (and `USAGE, SELECT ON SEQUENCES`), so tables added by later migrations are usable without a grant in every migration file. After each tenant migration the runner re-applies the grant set idempotently (`GRANT … ON ALL TABLES IN SCHEMA`, then `REVOKE UPDATE, DELETE ON audit_log`). A test compares `has_table_privilege` for every tenant table with the expected matrix.

Role count = tenant count. Postgres has no hard role limit; managed providers are checked by the preflight (OPERATIONS §8). `pg_dump -n` uses `--no-owner --no-privileges` for portability.

### 5.4 `search_path` and the `extensions` schema (decision)

**Decision:** `citext` and `pg_trgm` are installed once, in the shared schema `extensions`, and the tenant `search_path` is exactly `"t_<id>", extensions`. The platform path is `extensions`, and migration connections use `"t_<id>", extensions` or `platform, extensions`.

Why `extensions` must be on the path: Postgres resolves **operators and operator classes** through `search_path` just like tables. Without `extensions` on the path, `email = $1` on a `citext` column does not find the `citext = citext` operator and silently falls back to `text = text` through the implicit cast, so the comparison becomes **case-sensitive** with no error. Trigram search (`%`, `similarity()`, `gin_trgm_ops`) would need `OPERATOR(extensions.%)` in every query. Installing the extensions per tenant schema is also wrong: each tenant would get its own `citext` type OID, which breaks the "no per-schema custom types" rule that `Exec` mode relies on (§5.2).

Why it is safe: the tenant schema comes first, so tenant objects always win. `extensions` holds only extension objects owned by `pos_owner`, and no application role can create anything in it, so it cannot be used to shadow a name. A table missing from the tenant schema still fails, because `extensions` contains no tables.

**Tests (step 2):**
1. Inside `InTenantTx`, `current_setting('search_path')` equals `t.Schema.SearchPath()` exactly, and on the released connection it is `""` again.
2. `SELECT 'A@x.io'::citext = 'a@x.io'::citext` is true, and inserting `A@x.io` then `a@x.io` into `users` hits `users_email_uq` (the case-insensitive rule really applies).
3. `EXPLAIN` of the product name search uses `products_name_trgm`.
4. A tenant role cannot `CREATE` in `extensions` or `public`, and cannot see `platform`.
5. With `extensions` left off the path (negative test), test 2 fails, which documents why the rule exists.

### 5.5 Change notifications (`LISTEN/NOTIFY`)

Channels: `tenant_changed`, `auth_changed`, `role_changed`, `impersonation_changed`, `mail_dispatch`. Payloads carry IDs only and are treated as **hints**: a listener only invalidates cache entries or wakes a worker, so a spoofed `NOTIFY` (any role can send one) costs at most an extra DB read.

- **Dedicated direct connection.** Each process holds one `pgx.Conn` for `LISTEN`, opened with `LISTEN_DATABASE_URL` (defaults to `DATABASE_URL`). It is not taken from `pgxpool`, and it must never go through PgBouncer in transaction mode, where `LISTEN` does not work. When a pooler is added later, this URL points at the direct port.
- **Reconnect.** Backoff 1 s → 30 s. After every (re)connect: `LISTEN` all channels, then **flush every cache** (tenant/slug, auth state, permissions, impersonation), because notifications sent while disconnected are lost. Only then is the listener marked healthy.
- **TTL stays as the fallback.** Caches keep their TTL (30 s, 60 s for permissions) whether or not the listener is up, so staleness is bounded even if a notification is lost. While the listener is down, TTLs drop to 5 s.
- Observability: `pos_listener_connected` gauge, `pos_listener_reconnects_total`, alert `ListenerDown` after 2 min (OPERATIONS §4). `/readyz` does **not** fail on it (the TTLs keep the app correct).
- Test (step 2): kill the listener's backend with `pg_terminate_backend`, change a user's status while it is down, and assert the change is enforced within the reduced TTL and immediately after reconnect.

---

## 6. Provisioning (idempotent, resumable)

`provisioning_runs(id, tenant_id, step, status, attempts, last_error, started_at, finished_at)`. Each step checks "already done?" before acting, so re-running is safe.

| # | Step | Idempotency check | Compensation on failure |
|---|---|---|---|
| 1 | Validate slug (regex, no `--`, reserved list, `tenant_slug_history`, uniqueness), plan, owner email | — | — |
| 2 | Insert `platform.tenants` (status `provisioning`, new ID) + `provisioning_runs` | Row exists | Mark `failed` |
| 3 | `CREATE ROLE t_<id>_rw NOLOGIN`; `CREATE SCHEMA t_<id> AUTHORIZATION pos_owner` | `pg_namespace`/`pg_roles` lookup | `DROP SCHEMA … CASCADE`, `DROP ROLE` |
| 4 | Run tenant migrations to latest (advisory lock `pg_advisory_lock(hashtextextended('migrate:'||id,0))`) | `goose_db_version` | Drop schema |
| 5 | Grants (`USAGE` on `t_<id>` and `extensions`, DML, `audit_log` insert-only), default privileges (§5.3), tenant-bound trigger function `mark_outbox_pending()` with the validated tenant ID as a literal (EMAIL_AND_RECEIPTS §4.2), `GRANT t_<id>_rw TO pos_app, pos_jobs WITH INHERIT FALSE, SET TRUE` | `has_schema_privilege`, `pg_proc` lookup | Drop schema/role |
| 6 | Seed roles, permissions and settings: permission catalog copy, roles `owner/admin/biller`, `settings` (currency/number format/timezone from the request or defaults), plus two functional defaults (O-2): a "Walk-in Customer" (set as `settings.default_customer_id`) and one default store. No demo data | Unique keys + `ON CONFLICT DO NOTHING` | Drop schema |
| 7 | Create owner user (`status=Active`, no password, `owner` role) + invite token (hashed) | Unique email | Drop schema |
| 8 | Create trial subscription row (`trialing`, `trial_end = now + plan.trial_days`) on the hidden internal `trial` plan (O-3: `is_public=false`, price 0, 14 days, seeded by a platform migration; the owner's real plans replace it later) | Unique `tenant_id` | Delete row |
| 9 | Status → `trialing`; invalidate slug cache | Status check | — |
| 10 | Enqueue welcome + set-password email (platform SMTP), with the tenant URL `https://<slug>.<BASE_DOMAIN>` | Idempotency key `tenant:<id>:welcome` | Email failure never fails provisioning |

- Steps 3–8 run as `pos_migrator`. Failure in 3–8 runs compensation in reverse, then sets `failed` with the error (sanitized) for the Super Admin to retry (`/provisioning/retry`) or discard. A periodic reaper finds runs stuck in `provisioning` for more than 15 minutes.
- The **owner role is granted only here** (and by an audited ownership-transfer endpoint later). No client field can choose it.
- New tenants start at the latest migration version.
- **Public self-signup is not built (Q-5).** Only the Super Admin creates tenants (`POST /api/platform/tenants`). If it is ever added, it would be an apex-host form → email verification + CAPTCHA + per-IP rate limit → the same orchestrator (Turnstile deferred).
- Phase 3 test: inject a failure at every step; assert no orphan schema, role or tenant row in `provisioning`.

---

## 7. Subdomains, DNS, TLS, reverse proxy, cookies, CORS

### 7.1 Hosts

| Host | Serves |
|---|---|
| `<slug>.<BASE_DOMAIN>` | Tenant SPA (static) + `/api/*` tenant API + `/api/public/tenant-info` + `/api/public/shop/*` + `/files/t/*` |
| `admin.<BASE_DOMAIN>` | Admin console SPA + `/api/platform/*` only (incl. Razorpay webhook). Strict CSP (OPERATIONS §7) |
| `<BASE_DOMAIN>` (apex) | Landing site, server-rendered HTML + CSS from `internal/landing` (no JavaScript, strict CSP): pricing from public plans, "Find my shop", contact/demo form, staff sign-in redirect. No self-signup (Q-5). API_CONTRACT §5.4 |
| anything else | `421` |

The Go binary serves the static SPA builds (from `STATIC_DIR` or embedded with `go:embed`). That keeps everything same-origin, lets the app do slug redirects, and leaves the proxy with only TLS and forwarding to do.

### 7.2 DNS & TLS
- DNS: `A/AAAA <BASE_DOMAIN>` and `A/AAAA *.<BASE_DOMAIN>` → load balancer / VM. `CAA` record restricting issuers to Let's Encrypt. New tenants need **no DNS change**.
- TLS terminated at **Caddy** (recommended: automatic certificates, built-in DNS-01 plugins, on-demand TLS for later custom domains). A wildcard cert via **DNS-01** covers exactly one label (`tenant.pos.example.com`, not `a.b.pos.example.com`), which matches the slug rules.
- HSTS (`max-age=31536000; includeSubDomains`) only after go-live checklist sign-off. Preload is optional and very hard to undo.

```caddyfile
# deploy/Caddyfile (production sketch; BASE_DOMAIN and DNS provider from env)
{
    email {$ACME_EMAIL}
}
{$BASE_DOMAIN}, *.{$BASE_DOMAIN} {
    tls {
        dns {$DNS_PROVIDER} {$DNS_API_TOKEN}
    }
    request_body { max_size 10MB }
    reverse_proxy app:8080 {
        header_up X-Forwarded-Host {host}
        header_up X-Forwarded-Proto {scheme}
        header_up X-Request-Id {http.request.uuid}
    }
}
# Custom domains later (designed, not built):
# https:// { tls { on_demand } reverse_proxy app:8080 }  +  global on_demand_tls { ask http://app:9090/internal/tls/ask }
```

### 7.3 Local development
- `BASE_DOMAIN=localhost`. Hosts: `tenant1.localhost`, `admin.localhost`. Browsers resolve `*.localhost` to loopback per RFC 6761. On Windows, CLI tools may need hosts-file entries (documented in the README).
- docker-compose runs Postgres 18, Caddy (`tls internal`, local CA), the Go app, MinIO and Mailpit (SMTP catcher). Vite dev server runs behind Caddy (`/api` → Go, everything else → Vite) so the browser still sees one origin. Vite `server.allowedHosts: ['.localhost']`.

### 7.4 Cookies
- Refresh cookie: `__Host-rt` (the `__Host-` prefix *forces* `Secure`, `Path=/` and **no `Domain`**, so it is bound to the exact host and never sent to another tenant), `HttpOnly`, `SameSite=Strict`. Never `Domain=.<BASE_DOMAIN>`.
- Platform cookie: `__Host-prt` on `admin.<BASE_DOMAIN>` only.
- CSRF: the only cookie-authenticated endpoints are refresh/logout. They require `SameSite=Strict`, an `Origin` header equal to the request origin, and a custom header (`X-Requested-With: fetch`). Everything else uses Bearer tokens (not ambient).

### 7.5 CORS
Because the SPA is same-origin, the default is **no CORS headers at all**. If a cross-origin caller must be supported (e.g. Vite without the proxy in dev), `CORS_DEV_ORIGINS` allows exact origins in development only. Production allows `Origin` only if it is exactly `https://<resolved-tenant-host>` (i.e. same origin). That is stricter than the brief's "tenant origin plus admin origin": the admin console never calls tenant APIs, because impersonation uses a one-time code exchange on the tenant host (§9). No wildcard, no reflected origins, `Vary: Origin`.

---

## 8. Migration runner (two tracks)

Tool: **goose v3** (Provider API) with plain SQL files (challenge to golang-migrate, see LIBRARIES). Layout `db/migrations/platform/NNNN_*.sql` and `db/migrations/tenant/NNNN_*.sql`; version table `goose_db_version` in each schema.

`posctl migrate platform up` → single run on `platform` (advisory lock `migrate:platform`).

`posctl migrate tenants up [--dry-run] [--parallelism 8] [--tenants id,..] [--resume RUN_ID]`:
1. Create `migration_runs` row (target version = latest file).
2. List tenants with status not in (`deleted`,`failed`), ordered by size ascending (small first, so failures show up cheaply).
3. Worker pool (bounded parallelism). Per tenant:
   - **Lock connection**: a separate `pgx.Conn` takes `pg_try_advisory_lock(hashtextextended('migrate:'||tenant_id,0))` (skip if locked, so an overlapping run can't double-apply) and holds it for the whole tenant run. If it drops, the tenant run aborts and is marked failed.
   - **Migration connection**: built by `migrate.DedicatedConn(schema)` in `internal/migrate/conn.go`, the only place allowed to do this (§5 CI rule). It is a `*sql.DB` over pgx `stdlib.OpenDB` with `SetMaxOpenConns(1)`, logging in as `pos_migrator`, and the session settings are **connection startup parameters** in `ConnConfig.RuntimeParams`, not `SET` statements: `search_path = "t_<id>", extensions`, `role = pos_owner` (so objects are owned by `pos_owner`), `lock_timeout = 5s` (don't queue behind POS traffic), `statement_timeout = 0`, `application_name = posctl-migrate`. The server applies them before the first query, so they are simply the session defaults.
   - goose `Up` on that `*sql.DB`. Each migration runs in its own transaction unless marked `-- +goose NO TRANSACTION` (e.g. `CREATE INDEX CONCURRENTLY`).
   - **Why this works with `NO TRANSACTION`**: a `SET LOCAL` would vanish outside a transaction, and a plain `SET` would be lost on reconnect (and is banned). A startup parameter is part of every physical connection the `*sql.DB` opens, so a `NO TRANSACTION` statement, or a connection that goose re-dials after a network error, still sees the tenant path. Step 3 tests this: a `NO TRANSACTION` `CREATE INDEX CONCURRENTLY` lands in `t_<id>`; `pg_terminate_backend` between two migrations, then the next migration still lands in `t_<id>`; afterwards no new object exists in `public`, `platform`, `extensions` or another tenant schema. Fallback if a provider rejects `role` as a startup parameter: `ALTER ROLE pos_migrator SET role = 'pos_owner'` (a role default, still not a `SET` statement in code).
   - Post-step: re-apply grants (§5.3) and `CREATE OR REPLACE` the tenant-bound `mark_outbox_pending()`.
   - Update `tenants.schema_version`, record the per-tenant item (from, to, ms, error), close both connections.
4. Failure of one tenant does not stop the others; the run ends `partial`. `--resume` reprocesses only failed/pending items.
5. `--dry-run`: reports pending versions per tenant and runs the SQL inside a transaction that is **rolled back** (for transactional migrations). It never touches `NO TRANSACTION` migrations.
6. **Drift report** (`posctl migrate drift` and `/api/platform/migrations/drift`): per tenant, the version vs latest, plus a structural fingerprint (hash of `information_schema.columns` + indexes + constraints for that schema) compared with the reference fingerprint of a freshly migrated scratch schema. Any difference is listed.

**Expand/contract rule** (old and new app versions run side by side during rollout):
- *Expand* release: add nullable columns / new tables / new indexes (`CONCURRENTLY`), backfill in batches, app writes both.
- *Contract* release (later, after all tenants are migrated and old app instances are gone): drop old columns, add `NOT NULL`.
- Never rename or retype a column in one step. The app tolerates schema versions N and N+1 (`Exec` mode makes that safe; sqlc queries only touch columns that exist in both).
- Deployment order: run platform migrations, run tenant migrations (expand), roll out the app, then contract later.

Phase 3: 50+ schemas with seeded data, fault injection mid-run, resume, and the drift report.

---

## 9. Impersonation hand-off (cookie-safe)

Cookies are host-scoped, so the admin console cannot log into a tenant host directly. Flow:
1. Support user (MFA + step-up) calls `POST /api/platform/tenants/:id/impersonate {reason, mode}` and gets a one-time `code` (32 random bytes, SHA-256 stored, 60 s TTL).
2. Admin console opens `https://<slug>.<BASE_DOMAIN>/#/impersonate?code=…`. The code is in the URL **fragment**, so it never reaches server logs or Referer headers.
3. Tenant SPA posts the code to `/api/auth/impersonation/exchange`. The server burns the code and returns a 15-minute access token with `imp=<session id>`, `sub=0` (no tenant user), `mode`. No refresh cookie.
4. Every request re-checks the impersonation session (cache with `NOTIFY` invalidation) → revocation is immediate. Read-only mode = `READ ONLY` transactions + write routes blocked.
5. Tenant `audit_log` rows get `actor_type='platform_impersonation'`, `impersonation_id`. The start and end of every impersonation are written to the tenant audit log (visible to the tenant) and to the platform audit log.

---

## 10. Dedicated-DB hybrid

- `tenants.db_cluster` names a cluster; `DB_CLUSTERS` (env/secret manager) maps names → DSN. **The DSN (with password) is never stored in the database** (challenge to the brief's `tenants.db_dsn`; same intent, but the secret stays in the secret store).
- `tenancy.Resolver` keeps one pool per cluster (lazily created, small). Handlers are unchanged.
- The target cluster has its own `platform`-free layout: only `t_<id>` schemas + roles. The control plane (`platform`) stays in the main DB.
- Move procedure (Phase 3+ runbook): put the tenant in `read_only` → export (§ OPERATIONS 6) → restore into the target cluster → verify counts/checksums → flip `db_cluster` → invalidate caches → back to `active` → keep the old schema renamed for the retention window, then drop.

---

## 11. Starting state (no legacy import)

The old database holds demo data only, so there is **no legacy import** (former step 17 is dropped). The Go backend starts with an empty database:
1. `posctl migrate platform up` creates `extensions`, `platform` and `river`, and seeds the permission catalog and platform roles.
2. `posctl bootstrap-superadmin` creates the first Super Admin (PHASE1_DESIGN §7.3).
3. Each tenant is provisioned from the console (§6) and gets only roles, permissions and settings.

Consequences: no bcrypt verify path (every password is argon2id from day one), no `legacy_inferred` columns, no slug backfill, and `OVERRIDING SYSTEM VALUE` is used only by per-tenant restore.
