# Operations Design

| | |
|---|---|
| Status | Phase 1 design, revised 2026-10-15 after review; awaiting approval |
| Product | **Web application only** (Q-1). No desktop/Electron build |
| Runtime (Q-15) | One VM running docker-compose: Caddy + the Go binary `posd` (API + workers; Gotenberg only if the PDF spike needs it). Managed PostgreSQL 18 with point-in-time recovery. S3-compatible object storage |

---

## 1. Health checks

| Endpoint | Auth | Checks | Response |
|---|---|---|---|
| `GET /healthz` (+ legacy `/health`) | none | Process alive, event loop responsive | `200 {"status":"ok"}`. No dependencies, no tenant data, no version details |
| `GET /readyz` | none | `pool.Ping` (1 s timeout), jobs pool ping, River client started, platform schema at expected version, not draining | `200 {"status":"ready"}` or `503 {"status":"not_ready","checks":{"db":"fail"}}` (check names only, no error text) |
| `GET /metrics` | internal listener only (`METRICS_ADDR`, default `127.0.0.1:9090`) | — | Prometheus exposition |

Health endpoints answer on any host, before tenant resolution and before auth.

## 2. Logging
- `log/slog` JSON handler to stdout. Every request log line: `ts, level, msg, request_id, tenant_id, user_id (or platform_user_id), impersonation_id, method, route (pattern, not raw path), status, duration_ms, bytes, remote_ip (from trusted proxy)`.
- **Never logged**: passwords, password hashes, tokens (access/refresh/reset/invite/impersonation codes), `Authorization`/`Cookie` headers, SMTP credentials, Razorpay secrets/signatures, KEKs/DEKs, full email bodies, PDF contents, request bodies (except on validation errors: field *names* only).
- Enforced with a `slog.Handler` wrapper that redacts known keys (`password`, `token`, `secret`, `authorization`, `cookie`, `smtp_password`, …) and types (`secrets.Secret` implements `LogValue()` returning `"[REDACTED]"`). Tested.
- Levels: `info` default; `debug` never in production. Security events (login failure, lockout, refresh reuse, tenant mismatch, signature failure, impersonation) get `event=security.*` for easy alerting.

## 3. Metrics (Prometheus)

| Metric | Type | Labels |
|---|---|---|
| `pos_http_requests_total` | counter | `route`, `method`, `status_class`, `host_kind` (tenant/platform/apex) |
| `pos_http_request_duration_seconds` | histogram | `route`, `method` |
| `pos_db_pool_acquired_conns`, `_idle_conns`, `_total_conns`, `_max_conns`, `pos_db_pool_acquire_wait_seconds` | gauge/histogram | `pool` (app/jobs) |
| `pos_db_tx_duration_seconds` | histogram | `kind` (tenant/platform), `access` (ro/rw) |
| `pos_email_outbox_depth` | gauge | `scope` (platform/tenant) — summed over tenants |
| `pos_email_sent_total`, `pos_email_failed_total`, `pos_email_dead_total` | counter | `via`, `kind` / `class` |
| `pos_webhook_events_total` | counter | `provider`, `outcome` (applied/duplicate/stale/invalid_signature/error) |
| `pos_webhook_signature_failures_total` | counter | `provider` |
| `pos_tenant_migration_pending_tenants` | gauge | — |
| `pos_tenant_migration_runs_total` | counter | `status` |
| `pos_tenant_schema_drift_tenants` | gauge | — |
| `pos_jobs_*` | River metrics (queue depth, failures) | `queue`, `kind` |
| `pos_login_attempts_total` | counter | `scope` (tenant/platform), `result` |
| `pos_rate_limited_total` | counter | `limiter` |
| `pos_tenants` | gauge | `status` |
| `pos_listener_connected`, `pos_listener_reconnects_total` | gauge/counter | — (TENANCY §5.5) |
| `pos_session_tampered_total` | counter | — (post-condition check in `InTenantTx`) |
| `pos_backup_last_success_timestamp_seconds` | gauge | `kind` (pitr/tenant_export) |
| `pos_restore_test_last_success_timestamp_seconds` | gauge | — |

**Cardinality rule**: no `tenant_id`, `user_id`, `slug`, raw path or email labels. Per-tenant numbers (usage, last activity, mail backlog) live in platform tables and the Super Admin console, not in Prometheus.

Tracing (optional): OpenTelemetry with `otelchi` + `otelpgx`, enabled when `OTEL_EXPORTER_OTLP_ENDPOINT` is set; `tenant_id` as a span attribute is fine (traces are sampled, not aggregated).

## 4. Alerts (Prometheus rules in `deploy/prometheus/alerts.yml`)

| Alert | Condition (initial thresholds) | Severity |
|---|---|---|
| HighErrorRate | 5xx > 2 % of requests over 5 min (and > 10 req) | page |
| HighLatency | p95 `pos_http_request_duration_seconds` > 1 s for 10 min on `/api/sales` POST or POS routes | page |
| DBPoolExhausted | acquired/max > 0.9 for 5 min **or** acquire wait p95 > 200 ms | page |
| DBDown | `/readyz` failing on all instances 2 min | page |
| OutboxBacklog | `pos_email_outbox_depth{scope="tenant"}` > 500 for 15 min, or platform > 100 for 15 min | warn |
| DeadLetterGrowth | `increase(pos_email_dead_total[1h]) > 20` | warn |
| WebhookSignatureFailures | `increase(pos_webhook_signature_failures_total[15m]) > 5` | page (possible attack/secret mismatch) |
| WebhookErrors | `outcome="error"` > 0 for 30 min | warn |
| BackupFailed | `time() - pos_backup_last_success_timestamp_seconds{kind="pitr"} > 26h` or tenant exports > 26 h | page |
| RestoreTestStale | last successful restore test > 8 days | warn |
| CertificateExpiry | blackbox exporter `probe_ssl_earliest_cert_expiry - time() < 14d` | warn (< 3 d page) |
| TenantMigrationDrift | `pos_tenant_schema_drift_tenants > 0` for 1 h, or pending tenants > 0 for 2 h after a deploy | warn |
| LoginAttackSuspected | `rate(pos_login_attempts_total{result="failure"}[5m]) > 5/s` | warn |
| JobQueueStuck | River jobs available but none completed for 15 min | warn |
| ListenerDown | `pos_listener_connected == 0` for 2 min | warn |
| SessionTampered | `increase(pos_session_tampered_total[5m]) > 0` | page (possible SQL injection) |

Phase 3 proves at least HighErrorRate, OutboxBacklog and WebhookSignatureFailures fire (promtool unit tests + an induced fault in the load test).

## 5. Graceful shutdown

On `SIGTERM`/`SIGINT`:
1. Mark draining: `/readyz` → 503 (Caddy's active health check on `/readyz` stops sending new traffic; matters when two app containers run side by side during a deploy).
2. Wait `SHUTDOWN_DRAIN_DELAY` (5 s) for the health check to notice.
3. `http.Server.Shutdown(ctx)` with `SHUTDOWN_TIMEOUT` (25 s): stops accepting, finishes in-flight requests (each request also has its own `WriteTimeout`/handler timeout of 30 s, reports 60 s).
4. River `StopAndCancel` after soft stop: `Stop(ctx 20 s)` lets running jobs finish; then remaining jobs are cancelled (they are retried later, since every job is idempotent).
5. Stop LISTEN connections, flush metrics/traces.
6. `pool.Close()` (app, jobs).
7. Exit 0 (non-zero if the timeout was hit).

docker-compose `stop_grace_period: 60s`. Phase 3 tests: in-flight checkout during SIGTERM completes; new requests get connection refused / 503; no job is lost.

Server hardening: `ReadHeaderTimeout 5s`, `ReadTimeout 30s`, `WriteTimeout 60s`, `IdleTimeout 120s`, `MaxHeaderBytes 32 KiB`, `http.MaxBytesReader` per route.

## 6. Backups & restore

### 6.1 Whole database (disaster recovery)
- Managed Postgres PITR (or self-hosted pgBackRest/WAL-G): continuous WAL archiving + daily base backup, **encrypted at rest**, retention 30 days PITR + monthly snapshots kept 12 months, stored in a different region/account.
- Restore drill quarterly into a staging instance (documented runbook; timed RTO/RPO). Targets: RPO ≤ 5 min, RTO ≤ 2 h.
- Logical whole-DB `pg_dump` is **not** the primary backup (lock count grows with tenants: TENANCY §1.1). If used, set `max_locks_per_transaction ≥ 1024`.

### 6.2 Per-tenant export (built in Phase 2, tested in Phase 3)

`posctl tenant export <tenant_id>` and `POST /api/platform/tenants/:id/exports` (job):

1. Platform record `tenant_exports(status='running')`.
2. Open a `REPEATABLE READ, READ ONLY` transaction as the tenant role (consistent snapshot, cannot read other schemas). The connection uses fixed output settings as startup parameters (`TimeZone=UTC`, `DateStyle=ISO`, `IntervalStyle=iso_8601`, `extra_float_digits=3`, `bytea_output=hex`), so the same data always produces the same bytes.
3. For each table in the **known tenant table list** (from the migration manifest, dependency-ordered), **except excluded tables**: `COPY (SELECT <explicit column list> FROM "<table>" ORDER BY <primary key>) TO STDOUT WITH (FORMAT csv, HEADER)` streamed into a tar entry. Row count and SHA-256 are computed **on the stream** as it passes (an `io.MultiWriter` into the hasher), so memory use is constant whatever the table size. The column list and key order are recorded in the manifest.
   - **Excluded: `refresh_tokens`, `auth_tokens`** (live credentials: sessions, invite and reset tokens), plus the transient tables `pos_locks` and `idempotency_keys`. They are never written to an export.
   - `tenant_email_settings` is included; its password stays envelope-encrypted, bound to the tenant ID (EMAIL_AND_RECEIPTS §3), so it only decrypts in the same tenant.
4. Write `manifest.json`: tenant id, slug, schema version, export time, app version, tables `{name, columns, order_by, rows, sha256}`, excluded tables, file object keys + hashes. The manifest is HMAC-signed with `EXPORT_MANIFEST_KEY`.
5. Optionally include tenant files (images/receipts) by copying object keys under `tenants/<tenant_id>/`.
6. Compress (zstd) and encrypt with `age` (S7, included) to the platform export public key `EXPORT_AGE_RECIPIENT` → object storage `exports/<tenant_id>/<ts>.tar.zst.age`. Record size and the manifest hash. The private key (`EXPORT_AGE_IDENTITY`) is given only to `posctl` for restore, never to the running API.
7. Also available for DBAs: `pg_dump --schema='"t_<id>"' --format=custom --no-owner --no-privileges` (same snapshot semantics). The app-level format is primary because it is version-aware and restorable into **any** schema name, while `pg_restore` cannot rename schemas.

Retention: nightly export per tenant (staggered), keep 7 daily + 4 weekly; final export kept 1 year after tenant deletion (Q-14).

### 6.3 Per-tenant restore

`posctl tenant restore <export> --into-scratch` (default) or `--replace` (step-up + super_admin, tenant must be `read_only`):

1. Verify the archive hash and manifest signature (HMAC with a platform key).
2. Create a scratch schema `r_<id>_<yyyymmddhhmm>` with its own role `r_<id>_<ts>_rw`. Scratch names use a separate validated pattern (`^r_[a-z0-9]{6,20}_[0-9]{12}$`) that the tenant resolver never accepts, so a scratch schema can never be served as a live tenant.
3. Run tenant migrations up to the manifest's `schema_version` (not latest) on the scratch schema.
4. `COPY … FROM STDIN` per table into the scratch schema (dedicated connection from `migrate.DedicatedConn`, `search_path` = scratch + `extensions` as startup parameters, role = scratch role, so it **cannot write to any other schema**), `OVERRIDING SYSTEM VALUE`, then reset identity sequences.
5. **Verify before migrating further**: re-run the exact export query from the manifest (same column list, same `ORDER BY`, same output settings) against the scratch schema and stream it into SHA-256, as at export. Row count and hash must equal the manifest for every table. This replaces the earlier `md5(string_agg(…))`, which built the whole table as one string in memory. Verifying at the manifest's `schema_version` matters: after step 6 the columns may differ, and the hashes would no longer be comparable. A mismatch fails the restore.
6. Migrate scratch from `schema_version` to latest.
7. `--into-scratch`: report and keep for N days (restore test), then drop. `--replace`: in one transaction `ALTER SCHEMA "t_<id>" RENAME TO "t_<id>_old_<ts>"`, `ALTER SCHEMA scratch RENAME TO "t_<id>"`, re-grant to `t_<id>_rw`, re-create the tenant-bound trigger function.
8. **Revoke sessions after a `--replace` restore**: the restored schema has empty `refresh_tokens` and `auth_tokens` (they are never exported), so every refresh session and every pending invite/reset link is gone. In the same transaction `UPDATE users SET token_version = token_version + 1` kills outstanding access tokens, and every impersonation session for the tenant is revoked. After commit: `NOTIFY auth_changed` and `tenant_changed`, flush that tenant's caches, write a tenant and a platform audit entry, and email the owner that everyone must sign in again (invites must be re-sent). The old schema is dropped after the retention window.

**Weekly automated restore test** (job): pick a random active tenant + the oldest-tested one, restore into scratch, compare, record `pos_restore_test_last_success_timestamp_seconds`, drop scratch. Phase 3 test: export tenant A, restore into scratch, counts + checksums equal, and **no other schema's** `pg_stat_user_tables` write counters changed (proves isolation).

### 6.4 Deletion
**No automatic schema drop in v1** (decided O-4). At `RETENTION_DAYS` (90) after the restriction started (BILLING_DESIGN §3 timeline), the tenant only becomes *eligible* for deletion:
1. Warning emails to the owner and billing contact before the eligibility date (14, 7 and 1 days before; deduplicated via `notification_log`).
2. A final export is taken and **verified** (restore into scratch, counts and checksums match, §6.3). Without a verified final export, deletion is refused.
3. A Super Admin deletes from the console with **step-up** and an explicit confirmation (typed slug). Only then: `DROP SCHEMA "t_<id>" CASCADE`, `DROP ROLE t_<id>_rw`, delete object-storage prefix `tenants/<id>/` (except the final export), status `deleted`, slug reserved for 1 year. Platform audit row. Billing records are kept (legal).

The "Super Admin → eligible for deletion" list shows these tenants; nothing is dropped by a job.

## 7. Deployment & security posture
- Image: multi-stage build → `gcr.io/distroless/static-debian12:nonroot`, `CGO_ENABLED=0`, `-trimpath -ldflags "-s -w"`, read-only root FS, no shell. Version via `-ldflags -X`. The image is scanned (Trivy/Grype) in CI.
- Postgres connection TLS `verify-full` in production; app roles per TENANCY §5.3; `pg_stat_statements` enabled; `log_min_duration_statement = 500ms`.
- Secrets via env injected from the secret manager (Docker secrets / cloud SM). `.env.example` only in the repo; gitleaks pre-commit + CI. Dependency updates: **Dependabot** (native on GitHub) for `gomod`, `npm`, `docker` and `github-actions`, weekly, grouped; every PR runs the full CI including `govulncheck`.
- Security headers (all hosts): `Strict-Transport-Security` (after go-live), `X-Content-Type-Options: nosniff`, `Referrer-Policy: strict-origin-when-cross-origin`, `X-Frame-Options: DENY`, `Permissions-Policy` minimal, `Cross-Origin-Opener-Policy: same-origin`, `Cache-Control: no-store` on authenticated API responses. API JSON responses: `Content-Security-Policy: default-src 'none'; frame-ancestors 'none'`.
- **CSP, admin host (strict now, S13):** `default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; manifest-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'`. No `'unsafe-inline'`, no `'unsafe-eval'`, no nonces needed: the admin console must ship only static JS and CSS files (no inline `<script>`/`<style>`, no runtime CSS-in-JS that injects `<style>` tags). A CI check loads the built admin console in a headless browser and fails on any CSP violation report. `img-src data:` is for the TOTP QR code.
- **CSP, tenant host (unchanged for now):** `default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'` because MUI/Emotion inject styles at run time. Moving to nonces is deferred (S13, tenant part).
- HTTPS assumption: TLS terminates at Caddy. The app trusts `X-Forwarded-Proto` only from `TRUSTED_PROXY_CIDRS`. Cookies are always `Secure`. App-to-proxy traffic stays on a private network.

## 8. Managed Postgres preflight (before step 2 targets a real instance)

Steps 1–5 are developed and tested against Postgres 18 in Docker (testcontainers), where every requirement below holds. Before any managed instance is used (staging or production), these requirements must be confirmed on **that** provider. `posctl db preflight` (built in step 2) checks them automatically against a scratch database and prints pass/fail per line; the SQL below does the same by hand.

| # | Requirement | Why | Check |
|---|---|---|---|
| M1 | PostgreSQL **≥ 16**. Preflight target (decided O-7): **AWS RDS for PostgreSQL 18 in ap-south-1**; PG 17 acceptable if 18 is unavailable there | `GRANT … WITH INHERIT FALSE, SET TRUE`, PG16 `CREATEROLE` semantics | `SHOW server_version_num` ≥ `160000` |
| M2 | The admin login can `CREATE ROLE … CREATEROLE` (no superuser needed) | Creates `pos_owner`, `pos_migrator` (with `CREATEROLE`), `pos_app`, `pos_platform`, `pos_jobs` | Create them in the scratch DB |
| M3 | `pos_migrator` can create a role and then `GRANT` it to `pos_app WITH INHERIT FALSE, SET TRUE` | Provisioning step 3/5; on PG16 the creator gets `ADMIN OPTION` automatically | Create `t_preflight0_rw`, grant it, `SET ROLE` to it as `pos_app` |
| M4 | `createrole_self_grant` is empty (or settable to empty) | Otherwise `pos_migrator` would inherit every tenant role | `SHOW createrole_self_grant` |
| M5 | **Many roles and schemas**: 2,000 roles + 2,000 schemas can be created and dropped, with no provider limit or slowdown | One role and one schema per tenant | Timed loop in the scratch DB, then dropped |
| M6 | `pos_migrator` can `SET ROLE pos_owner` and `CREATE SCHEMA … AUTHORIZATION pos_owner`; `role` and `search_path` are accepted as **connection startup parameters** | Migration runner (TENANCY §8) | Connect with `RuntimeParams{role, search_path}`, check `current_user`/`current_setting` |
| M7 | `CREATE EXTENSION citext, pg_trgm WITH SCHEMA extensions` works for the admin login (trusted extensions); `pg_stat_statements` is in `shared_preload_libraries` | TENANCY §5.4 | `CREATE EXTENSION`, `SHOW shared_preload_libraries` |
| M8 | Parameters can be changed: `max_locks_per_transaction ≥ 1024`, `log_min_duration_statement`, `shared_preload_libraries`, `max_connections` | TENANCY §1.1 | Provider parameter group; `SHOW` after reboot |
| M9 | `ALTER ROLE pos_app SET search_path = ''` is allowed | Empty default path (TENANCY §5.1) | Run it, reconnect, `SHOW search_path` |
| M10 | `SECURITY DEFINER` functions owned by `pos_owner` can write to `platform` | Outbox trigger (EMAIL_AND_RECEIPTS §4.2) | Create and fire a test trigger |
| M11 | A **direct** (non-pooled) endpoint exists for `LISTEN` | TENANCY §5.5 | `LISTEN x` on one connection, `NOTIFY x` on another |
| M12 | TLS with `sslmode=verify-full` and the provider CA bundle | Prod config rule | Connect with `verify-full` |
| M13 | PITR enabled, retention ≥ 7 days; `pg_dump -n` works without superuser | Backups (§6) | Provider console; `pg_dump --schema=…` on the scratch DB |
| M14 | `pg_read_all_stats` can be granted to `pos_readonly_ops` | Ops role | `GRANT pg_read_all_stats TO …` |

Manual version (run as the provider's admin login in a **new, empty scratch database**; it creates and drops its own objects):

```sql
-- psql script. First: \set pf_pw '<a random password>'
SHOW server_version_num;                                   -- M1: expect >= 160000
SHOW createrole_self_grant;                                -- M4: expect ''
SHOW shared_preload_libraries;                             -- M7: expect pg_stat_statements listed
SHOW max_locks_per_transaction;                            -- M8: expect >= 1024 after tuning
CREATE ROLE pf_owner NOLOGIN;
CREATE ROLE pf_migrator LOGIN CREATEROLE PASSWORD :'pf_pw'; -- M2
CREATE ROLE pf_app LOGIN PASSWORD :'pf_pw';
GRANT pf_owner TO pf_migrator WITH INHERIT FALSE, SET TRUE; -- M1/M6 syntax
GRANT CREATE ON DATABASE :"DBNAME" TO pf_owner;             -- psql fills in the current DB name
CREATE SCHEMA IF NOT EXISTS extensions;
CREATE EXTENSION IF NOT EXISTS citext  WITH SCHEMA extensions;  -- M7
CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA extensions;
ALTER ROLE pf_app SET search_path = '';                     -- M9
-- then reconnect as pf_migrator and run:
--   CREATE ROLE t_preflight0_rw NOLOGIN;                                  -- M3
--   GRANT t_preflight0_rw TO pf_app WITH INHERIT FALSE, SET TRUE;         -- M3 (needs ADMIN, automatic on PG16)
--   SET ROLE pf_owner; CREATE SCHEMA t_preflight0 AUTHORIZATION pf_owner; RESET ROLE;  -- M6 (manual check only)
--   DO $$ BEGIN FOR i IN 1..2000 LOOP EXECUTE format('CREATE ROLE %I NOLOGIN', 'pf_r'||i);
--        EXECUTE format('CREATE SCHEMA %I', 'pf_s'||i); END LOOP; END $$;  -- M5 (time it), then drop them
-- and as pf_app:  SET ROLE t_preflight0_rw; SELECT current_user;          -- expect t_preflight0_rw
-- Cleanup: DROP the pf_* schemas/roles, t_preflight0*, and the scratch database.
```

(The `SET ROLE`/`RESET` lines are a manual DBA check, not application code; the CI rule in TENANCY §5 covers `*.go` and the migration files only.)

**Provider notes** (to be confirmed by the preflight, not assumed): AWS RDS and Aurora give the master user `rds_superuser` with `CREATEROLE`, trusted extensions, and parameter groups for M7/M8. Providers with a built-in transaction pooler (Supabase, Neon) need the direct endpoint for M11. Anything that manages roles only through its own API and blocks `CREATE ROLE` in SQL fails M2/M3 and is not usable for this design.

## 9. Go-live checklist (README will carry it)
1. Choose `BASE_DOMAIN` and `PLATFORM_NAME`; set them in env for the app, the frontend build (`VITE_*` only if not same-origin), Caddy, and email templates (all read env; nothing hard-coded).
2. DNS: `A/AAAA` for apex and `*`; `CAA` → letsencrypt.org; MX/SPF/DKIM/DMARC for the platform mail domain. SES: verify the domain, enable Easy DKIM, request production access (leave the sandbox) in ap-south-1; or Brevo: authenticate the domain.
3. Managed Postgres preflight (§8) green on the production instance.
4. Caddy DNS-01 provider token (least privilege: DNS edit for that zone only); verify wildcard cert issuance on staging ACME first.
5. Rotate every secret (JWT keys, platform JWT keys, master keys, export keys, DB passwords, SMTP, Razorpay); confirm the app refuses the leaked old JWT secret (its SHA-256 is on the startup deny list; the value itself is not in the code).
6. `TRUSTED_PROXY_CIDRS` set; `APP_ENV=production`; DB TLS `verify-full`.
7. Razorpay: results of the test-mode checks (BILLING_DESIGN §10) applied; live keys, webhook URL on `admin.<BASE_DOMAIN>`, webhook secret, plans created by the owner and IDs copied into `plans`.
8. Backups: PITR on, first tenant export + restore test green; alerts wired (pager).
9. `posctl bootstrap-superadmin`; log in, change password, enrol TOTP, store recovery codes offline.
10. Enable HSTS (without preload first).
11. Run the Phase 3 smoke suite against production with a test tenant, then delete it.
