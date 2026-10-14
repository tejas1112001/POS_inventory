# Operations Design

| | |
|---|---|
| Status | Phase 1 design, awaiting approval |
| Runtime | One Go binary `posd` (API + workers), Caddy in front, PostgreSQL 18 (managed, PITR), S3-compatible object storage |

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

Phase 3 proves at least HighErrorRate, OutboxBacklog and WebhookSignatureFailures fire (promtool unit tests + an induced fault in the load test).

## 5. Graceful shutdown

On `SIGTERM`/`SIGINT`:
1. Mark draining: `/readyz` → 503 (load balancer stops sending new traffic).
2. Wait `SHUTDOWN_DRAIN_DELAY` (5 s) for deregistration.
3. `http.Server.Shutdown(ctx)` with `SHUTDOWN_TIMEOUT` (25 s): stops accepting, finishes in-flight requests (each request also has its own `WriteTimeout`/handler timeout of 30 s, reports 60 s).
4. River `StopAndCancel` after soft stop: `Stop(ctx 20 s)` lets running jobs finish; then remaining jobs are cancelled (they are retried later, since every job is idempotent).
5. Stop LISTEN connections, flush metrics/traces.
6. `pool.Close()` (app, jobs).
7. Exit 0 (non-zero if the timeout was hit).

Container `stopGracePeriod`/`terminationGracePeriodSeconds` = 60 s. Phase 3 tests: in-flight checkout during SIGTERM completes; new requests get connection refused / 503; no job is lost.

Server hardening: `ReadHeaderTimeout 5s`, `ReadTimeout 30s`, `WriteTimeout 60s`, `IdleTimeout 120s`, `MaxHeaderBytes 32 KiB`, `http.MaxBytesReader` per route.

## 6. Backups & restore

### 6.1 Whole database (disaster recovery)
- Managed Postgres PITR (or self-hosted pgBackRest/WAL-G): continuous WAL archiving + daily base backup, **encrypted at rest**, retention 30 days PITR + monthly snapshots kept 12 months, stored in a different region/account.
- Restore drill quarterly into a staging instance (documented runbook; timed RTO/RPO). Targets: RPO ≤ 5 min, RTO ≤ 2 h.
- Logical whole-DB `pg_dump` is **not** the primary backup (lock count grows with tenants: TENANCY §1.1). If used, set `max_locks_per_transaction ≥ 1024`.

### 6.2 Per-tenant export (built in Phase 2, tested in Phase 3)

`posctl tenant export <tenant_id>` and `POST /api/platform/tenants/:id/exports` (job):

1. Platform record `tenant_exports(status='running')`.
2. Open a `REPEATABLE READ, READ ONLY` transaction as the tenant role (consistent snapshot, cannot read other schemas).
3. For each table in the **known tenant table list** (from the migration manifest, dependency-ordered): `COPY (SELECT * FROM "<table>" ORDER BY id) TO STDOUT WITH (FORMAT csv, HEADER)` streamed into a tar entry. Compute per-table row count and SHA-256 of the stream.
4. Write `manifest.json`: tenant id, slug, schema version, export time, app version, tables `{name, rows, sha256}`, file object keys + hashes.
5. Optionally include tenant files (images/receipts) by copying object keys under `tenants/<tenant_id>/`.
6. Compress (zstd) and encrypt (SUGGESTION: `age` with a platform export public key) → object storage `exports/<tenant_id>/<ts>.tar.zst.age`. Record size and the manifest hash.
7. Also available for DBAs: `pg_dump --schema='"t_<id>"' --format=custom --no-owner --no-privileges` (same snapshot semantics). The app-level format is primary because it is version-aware and restorable into **any** schema name, while `pg_restore` cannot rename schemas.

Retention: nightly export per tenant (staggered), keep 7 daily + 4 weekly; final export kept 1 year after tenant deletion (Q-14).

### 6.3 Per-tenant restore

`posctl tenant restore <export> --into-scratch` (default) or `--replace` (step-up + super_admin, tenant must be `read_only`):

1. Verify the archive hash and manifest signature (HMAC with a platform key).
2. Create a scratch schema `r_<id>_<yyyymmddhhmm>` with its own role `r_<id>_<ts>_rw`. Scratch names use a separate validated pattern (`^r_[a-z0-9]{6,20}_[0-9]{12}$`) that the tenant resolver never accepts, so a scratch schema can never be served as a live tenant.
3. Run tenant migrations up to the manifest's `schema_version` (not latest) on the scratch schema.
4. `COPY … FROM STDIN` per table into the scratch schema (dedicated connection, `search_path` = scratch only, role = scratch role, so it **cannot write to any other schema**), `OVERRIDING SYSTEM VALUE`, then reset identity sequences.
5. Migrate scratch from `schema_version` to latest.
6. Verify: per-table row count and per-table checksum (`SELECT md5(string_agg(t::text, '' ORDER BY id)) FROM "<table>" t`, computed identically at export) must match the manifest. A mismatch fails the restore.
7. `--into-scratch`: report and keep for N days (restore test), then drop. `--replace`: in one transaction `ALTER SCHEMA "t_<id>" RENAME TO "t_<id>_old_<ts>"`, `ALTER SCHEMA scratch RENAME TO "t_<id>"`, re-grant to `t_<id>_rw`, invalidate caches; old schema dropped after the retention window.

**Weekly automated restore test** (job): pick a random active tenant + the oldest-tested one, restore into scratch, compare, record `pos_restore_test_last_success_timestamp_seconds`, drop scratch. Phase 3 test: export tenant A, restore into scratch, counts + checksums equal, and **no other schema's** `pg_stat_user_tables` write counters changed (proves isolation).

### 6.4 Deletion
`pending_deletion` → after `RETENTION_DAYS`: final export → `DROP SCHEMA "t_<id>" CASCADE`, `DROP ROLE t_<id>_rw`, delete object-storage prefix `tenants/<id>/` (except the final export), status `deleted`, slug reserved for 1 year. Platform audit row. Billing records are kept (legal).

## 7. Deployment & security posture
- Image: multi-stage build → `gcr.io/distroless/static-debian12:nonroot`, `CGO_ENABLED=0`, `-trimpath -ldflags "-s -w"`, read-only root FS, no shell. Version via `-ldflags -X`. The image is scanned (Trivy/Grype) in CI.
- Postgres connection TLS `verify-full` in production; app roles per TENANCY §5.3; `pg_stat_statements` enabled; `log_min_duration_statement = 500ms`.
- Secrets via env injected from the secret manager (Docker secrets / cloud SM). `.env.example` only in the repo; gitleaks pre-commit + CI.
- Security headers (all hosts): `Strict-Transport-Security` (after go-live), `Content-Security-Policy` (SPA: `default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'` initially because of MUI/Emotion, tightened later; API JSON responses: `default-src 'none'; frame-ancestors 'none'`), `X-Content-Type-Options: nosniff`, `Referrer-Policy: strict-origin-when-cross-origin`, `X-Frame-Options: DENY`, `Permissions-Policy` minimal, `Cross-Origin-Opener-Policy: same-origin`, `Cache-Control: no-store` on authenticated API responses.
- HTTPS assumption: TLS terminates at Caddy. The app trusts `X-Forwarded-Proto` only from `TRUSTED_PROXY_CIDRS`. Cookies are always `Secure`. App-to-proxy traffic stays on a private network.

## 8. Electron / desktop mode
- Electron main process spawns the `posd` sidecar binary with `POSD_MODE=desktop`, `HTTP_ADDR=127.0.0.1:0`, `DESKTOP_TOKEN=<32 random bytes, per launch>` (env, not argv). `posd` prints the chosen port on stdout as JSON; Electron reads it and loads `http://127.0.0.1:<port>/`.
- Desktop mode: a single fixed local tenant (`t_local…`), no subdomain logic (Host must be `127.0.0.1:<port>`), platform API, billing and public shop disabled, every request must carry the desktop token (injected by the preload as a header via `contextBridge` → `api.js`), binds loopback only.
- Database per Q-1 (embedded Postgres recommended). Sidecar exits when Electron exits (parent-death watch + stdin EOF).

## 9. Go-live checklist (README will carry it)
1. Choose `BASE_DOMAIN` and `PLATFORM_NAME`; set them in env for the app, the frontend build (`VITE_*` only if not same-origin), Caddy, and email templates (all read env; nothing hard-coded).
2. DNS: `A/AAAA` for apex and `*`; `CAA` → letsencrypt.org; MX/SPF/DKIM/DMARC for the platform mail domain.
3. Caddy DNS-01 provider token (least privilege: DNS edit for that zone only); verify wildcard cert issuance on staging ACME first.
4. Rotate every secret (JWT keys, master keys, DB passwords, SMTP, Razorpay); confirm the app refuses the leaked old JWT secret.
5. `TRUSTED_PROXY_CIDRS` set; `APP_ENV=production`; DB TLS `verify-full`.
6. Razorpay: live keys, webhook URL on `admin.<BASE_DOMAIN>`, webhook secret, plans created and IDs copied into `plans`.
7. Backups: PITR on, first tenant export + restore test green; alerts wired (pager).
8. `posctl bootstrap-superadmin`; log in, change password, enrol TOTP, store recovery codes offline.
9. Enable HSTS (without preload first).
10. Run the Phase 3 smoke suite against production with a test tenant, then delete it.
