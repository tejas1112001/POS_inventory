# Library Decision Table

| | |
|---|---|
| Checked on | 2026-10-14, live, not from memory: latest versions from `proxy.golang.org/<module>/@latest`, Go release from `go.dev/dl`, vulnerabilities from the OSV API (`api.osv.dev/v1/query`) for the exact version, repo activity via the GitHub API where relevant |
| Rules applied | Stdlib first · no package without a release in 12 months unless justified · no heavy frameworks · exact pins in `go.mod`/`go.sum` · `govulncheck` in CI (fails on reachable vulns) |
| Not yet done | `govulncheck` itself needs Go installed (not on this machine yet, Q-9). It runs on the first Phase 2 commit; the OSV check below covers the same database at module level |

## 1. Toolchain & platform

| Need | Choice | Version | Notes |
|---|---|---|---|
| Language | Go | **1.27.2** (`go 1.27`, `toolchain go1.27.2`) | Current stable per go.dev. The brief's 1.23 is out of security support |
| Database | PostgreSQL | **18.x** (latest minor at deploy) | Need ≥ 16 for `GRANT … WITH INHERIT FALSE, SET TRUE`. Extensions: `citext`, `pg_trgm`, `pg_stat_statements` |
| Reverse proxy / TLS | Caddy 2 (+ DNS provider module) | latest 2.x at build | Automatic ACME incl. DNS-01 wildcard and on-demand TLS (custom domains later). Alternatives: Traefik (fine, label-heavy config), Nginx + certbot (manual renewal glue) |
| Container base | `gcr.io/distroless/static-debian12:nonroot` | pinned by digest | No shell, nonroot, CGO-free binary |

## 2. Go modules

OSV status = known vulnerabilities affecting **that exact version** at check time.

| Need | Chosen package | Version (released) | License | OSV | Alternatives considered | Why chosen |
|---|---|---|---|---|---|---|
| HTTP router | `github.com/go-chi/chi/v5` | v5.5.0 (2026-10-14) | MIT | none | stdlib `net/http` ServeMux (1.22 patterns), gin, echo, fiber | `net/http`-compatible, tiny, middleware composition and route groups per host kind. Stdlib mux lacks sub-routers/middleware stacks, so we'd rebuild chi. gin/echo/fiber add their own context types (fiber isn't even `net/http`) |
| Postgres driver | `github.com/jackc/pgx/v5` (+ `pgxpool`) | v5.11.0 (2026-09-07) | MIT | none | `lib/pq` (maintenance mode), `database/sql` + pgx stdlib | Fastest native driver, `QueryExecModeExec`, `LISTEN/NOTIFY`, `COPY`, pool hooks we need for tenancy |
| SQL codegen | `github.com/sqlc-dev/sqlc` (CLI, build-time only) | v1.31.1 (2026-04-22) | MIT | none | GORM/ent/bun (ORMs), squirrel/bob (builders), hand-written | Type-safe, plain SQL, zero runtime dependency, works with `search_path` because queries are unqualified. ORMs hide queries and make the tenancy invariants harder to audit |
| Migrations | `github.com/pressly/goose/v3` | v3.29.0 (2026-10-11) | MIT | none | **golang-migrate v4.20.1** (brief's pick), atlas, tern | **Challenge**: goose wraps each migration in a transaction (opt-out per file), supports Go migrations for batched backfills, its Provider API takes our dedicated per-schema connection and table name, and it has no global "dirty" state that blocks every later run after one failure. golang-migrate is fine, but has ~490 open issues and needs `force` after failures; both use plain SQL files, so switching is cheap |
| JWT | `github.com/golang-jwt/jwt/v5` | v5.3.1 (2026-01-28) | MIT | none | `lestrrat-go/jwx`, hand-rolled | De-facto standard, explicit `WithValidMethods` to pin HS256 (no `alg` confusion), small |
| Password hashing | `golang.org/x/crypto/argon2` (+ `bcrypt` for legacy verify) | x/crypto v0.58.0 (2026-10-09) | BSD-3 | GO-2026-5932, **not applicable** (affects only the deprecated `x/crypto/openpgp` packages, which we don't import; govulncheck's symbol analysis will confirm) | `alexedwards/argon2id` (last release 2023) | Official Go sub-repo; the wrapper adds nothing we can't do in ~40 lines (PHC encode/decode) |
| TOTP (Super Admin MFA) | **stdlib implementation** (`crypto/hmac`, `crypto/sha1`, `encoding/base32`) | — | — | — | `pquerna/otp` v1.5.0 (**last release 2024-12-31**, last commit 2025-08) | **Challenge**: pquerna/otp fails the 12-month rule. RFC 6238 TOTP is ~80 lines; we test against the RFC 6238 Appendix B vectors and add a replay guard. QR rendering stays in the frontend (`otpauth://` URI) |
| Validation | `github.com/go-playground/validator/v10` | v10.30.5 (2026-09-20) | MIT | none | ozzo-validation, hand-written | Mature, struct tags, custom rules (slug, currency, decimal ranges). Business rules stay in services |
| Money | `github.com/shopspring/decimal` | v1.5.0 (2026-10-05) | MIT | none | integer minor units (`int64`), `cockroachdb/apd`, `ericlagergren/decimal` | Maps 1:1 to `NUMERIC`; implements `sql.Scanner`/`driver.Valuer`, so it works with pgx Exec (text) mode without an adapter (`jackc/pgx-shopspring-decimal` is from 2022, not needed). Integer minor units are used in platform billing (paise from Razorpay) |
| Config | `github.com/caarlos0/env/v11` | v11.4.1 (2026-05-01) | MIT | none | `kelseyhightower/envconfig` (**last release 2019**, rejected), `knadh/koanf/v2` v2.3.8, `sethvargo/go-envconfig` v1.4.3 | Env-only requirement → koanf's multi-source power is unnecessary. caarlos0/env: struct tags, `required`, `notEmpty`, `unset`, typed durations, tiny. Secret strength validation is our own code |
| Logging | `log/slog` (stdlib) | Go 1.27 | BSD-3 | — | zap, zerolog | Stdlib, structured JSON, `LogValuer` for redaction; performance is sufficient |
| Rate limiting | `github.com/go-chi/httprate` | v0.16.1 (2026-10-08) | MIT | none | `golang.org/x/time/rate` (v0.16.0) custom, `ulule/limiter`, Redis-backed limiter | chi-native sliding window, key functions (IP, IP+email, tenant). Single node to start; `httprate-redis` drop-in when we scale out. Brute-force lockout state lives in Postgres (global, durable) |
| Job queue | `github.com/riverqueue/river` + `riverdriver/riverpgxv5` | v0.49.0 (2026-10-05) | MPL-2.0 | none | `hibiken/asynq` v0.26.0 (needs Redis), gocraft/work (dead), hand-rolled | Postgres-backed (no new infrastructure), transactional insert (`InsertTx`), unique jobs, periodic jobs, retries/backoff, custom `Schema` (we use `river`). MPL-2.0 is file-level copyleft, fine for use as a dependency |
| SMTP | `github.com/wneessen/go-mail` | v0.8.1 (2026-07-09) | MIT | none | stdlib `net/smtp` (frozen, no STARTTLS policy, no MIME builder), `jordan-wright/email` (unmaintained) | Active, MIME/attachments, mandatory-TLS policy, custom dialer hook (needed for SSRF-safe dialing), timeouts |
| PDF receipts | `github.com/johnfercher/maroto/v2` | v2.4.3 (2026-10-04) | MIT | none | `go-pdf/fpdf` (last release 2023, archived lineage of gofpdf), `jung-kurt/gofpdf` (archived 2019), `signintech/gopdf` v0.38.2 (low-level), headless Chrome via `chromedp` v0.21.0 | **Comparison:** maroto: pure Go, grid layout fits receipts, UTF-8 fonts, ~ms per page, no external process. Headless Chrome: pixel-perfect HTML reuse, but ~300 MB Chromium in the image (no distroless), 200–500 ms/render, sandbox and SSRF concerns when rendering tenant content. gopdf: we'd hand-place every element. **maroto chosen**; HTML email and PDF share one view-model, not one template |
| Excel export | `github.com/xuri/excelize/v2` | v2.11.0 (2026-07-06) | BSD-3 | none | `tealeg/xlsx` (deprecated), CSV only | De-facto standard, streaming writer for large reports |
| Email templates | `html/template`, `text/template` (stdlib) | — | — | — | — | Contextual auto-escaping |
| Currency/locale formatting | `golang.org/x/text` (`currency`, `message`, `language`) | v0.43.0 | BSD-3 | none | hand-written formatting | Correct grouping (e.g. Indian 1,00,000) and symbols per locale |
| Image validation | `github.com/gabriel-vasile/mimetype` + stdlib `image/png`, `image/jpeg` + `golang.org/x/image/webp` | v1.4.15 (2026-07-23) / x/image v0.47.0 | MIT / BSD-3 | none | `http.DetectContentType` only (too coarse), libvips bindings (CGO) | Content sniffing + decode-config dimension checks + re-encode, all pure Go |
| Object storage | `github.com/minio/minio-go/v7` (S3 API) + stdlib fs driver (`os.Root`) | v7.4.1 (2026-10-12) | Apache-2.0 | none | `aws-sdk-go-v2/service/s3` v1.114.3 | Works with S3, R2, MinIO and others; much smaller dependency tree than aws-sdk-go-v2. Behind our `files.Storage` interface |
| UUIDs | `github.com/google/uuid` | v1.6.0 (2024-01-23) | BSD-3 | none | `gofrs/uuid`, `oklog/ulid/v2` v2.1.2 | **Exception to the 12-month rule, justified**: feature-complete and stable (RFC 9562 incl. v7), maintained under the Google org, very widely used, and stdlib has no UUID package. Fallback: generate v4/v7 ourselves from `crypto/rand` (~30 lines) if it ever becomes a problem |
| Metrics | `github.com/prometheus/client_golang` | v1.25.0 (2026-10-08) | Apache-2.0 | none | OTel metrics SDK | Standard Prometheus exposition; pgx pool stats via a custom collector |
| Tracing (optional) | `go.opentelemetry.io/otel` + `github.com/exaring/otelpgx` + `github.com/riandyrn/otelchi` | v1.47.0 / v0.13.0 / v0.12.3 | Apache-2.0 | none | none | Off unless `OTEL_EXPORTER_OTLP_ENDPOINT` is set |
| CORS / secure headers | **own middleware** (~80 lines) | — | — | — | `rs/cors` v1.11.1 (**2024-08**, > 12 months), `go-chi/cors` v1.2.2 (2025-07, > 12 months), `unrolled/secure` v1.17.0 (2024-10) | Our CORS rule is dynamic (per resolved tenant host) and usually "same-origin only"; generic libs don't fit, and all three fail the 12-month rule |
| In-memory caches | stdlib (`sync.Map`/mutex map with TTL + singleflight from `golang.org/x/sync`) | — | — | — | `dgraph-io/ristretto/v2` v2.4.2, `maypok86/otter/v2` v2.3.0 | Caches are small (tenants, roles, auth state); invalidated by NOTIFY. Add otter later if profiling shows need |
| Testing | stdlib `testing` + `github.com/stretchr/testify` | v1.12.1 (2026-08-17) | MIT | none | `matryer/is`, `go-cmp` | Assertions/require; `go-cmp` for diffs where needed |
| Integration DB | `github.com/testcontainers/testcontainers-go` (+ postgres module) | v0.44.0 (2026-08-07) | MIT | none | `ory/dockertest` v3.12.0 (2025-03), external DB | Real Postgres 18 per test package; **requires Docker** (Q-9) |
| Export encryption (SUGGESTION) | `filippo.io/age` | v1.3.2 | BSD-3 | none | GPG (rejected: x/crypto/openpgp is unsafe, GO-2026-5932) | Modern, small, audited format |
| Razorpay | **own thin client** (`net/http`) | — | — | — | `razorpay/razorpay-go` v1.4.1 | ~6 endpoints; full control over timeouts/logging/redaction |

## 3. Dev/CI tools (pinned in `tools.go` / Makefile)

| Tool | Version | Purpose |
|---|---|---|
| `golang.org/x/vuln/cmd/govulncheck` | v1.9.0 (2026-10-11) | Reachability-based vuln scan; CI gate |
| `github.com/securego/gosec/v2` | v2.29.0 (2026-08-25) | Security lint (SQL string building, weak crypto, file perms) |
| `honnef.co/go/tools` (staticcheck) | v0.8.2 = 2026.2.2 (2026-10-12) | Static analysis |
| `github.com/golangci/golangci-lint/v2` | v2.14.0 (2026-09-24) | Lint runner (staticcheck, errcheck, gosec, revive, sqlclosecheck…) |
| `github.com/zricethezav/gitleaks/v8` | v8.30.1 (2026-02-21) | Secret scanning, full history + pre-commit |
| sqlc | v1.31.1 | Codegen check in CI (`sqlc diff`, `sqlc vet`) |
| k6 | latest | Load tests (Phase 3) |
| Trivy or Grype | latest | Image scan |
| promtool | with Prometheus | Alert rule unit tests |

## 4. Proactive suggestions (tools not in the brief)

| SUGGESTION | Benefit | Trade-off |
|---|---|---|
| **Caddy** over Nginx | Wildcard DNS-01 + on-demand TLS built in, tiny config | Custom build with DNS plugin (xcaddy) |
| **Mailpit** in docker-compose | Inspect every outgoing email in dev/tests via its API (Phase 3 email tests) | None |
| **MinIO** in docker-compose | Same S3 code path in dev as prod | Extra container |
| **pg_stat_statements + auto_explain** | Find slow queries per tenant schema in the load test | Small overhead |
| **PgBouncer (transaction mode)** when > 2 app nodes | Connection fan-in; compatible with `SET LOCAL` + `Exec` mode | Another component |
| **Renovate/Dependabot** | Automated pinned upgrades + govulncheck on PRs | PR noise |
| **`age`** for tenant exports | Exports encrypted at rest outside the DB | Key management |
| **Cloudflare Turnstile** (if self-signup) | Privacy-friendly CAPTCHA, simple server verification | Third party |
| **`goleak`** (`go.uber.org/goleak`) in tests | Catches goroutine leaks in workers/shutdown tests | Test-only dep (check version at add time) |
