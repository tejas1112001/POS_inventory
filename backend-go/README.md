# posd: Go backend (multi-tenant)

The Go rewrite of the POS backend. Design: [`docs/PHASE1_DESIGN.md`](../docs/PHASE1_DESIGN.md) and its companion docs. The old Node backend in `../backend` keeps running until cutover.

## Status

Phase 2, built in order (PHASE1_DESIGN §8).

- Step 1 (foundation): config, logging, errors, HTTP layer, health, metrics, graceful shutdown, Docker, CI.
- Step 2 (platform schema + registry): DB roles and bootstrap, platform migrations (goose), tenant registry with negative caching, `InTenantTx`/`InPlatformTx` with a tamper tripwire, LISTEN/NOTIFY cache invalidation, `posctl db preflight`.

- Step 3 (migration runner + provisioning): tenant migrations with per-tenant advisory locks, dry run, resume and a drift report; post-step grants, tenant-bound outbox trigger and permission sync; provisioning with compensation and retry.

- Step 4 (Super Admin): platform users with argon2id, mandatory TOTP (RFC 6238, replay guard), recovery codes, rotating refresh cookie with reuse detection, step-up for destructive actions, tenant administration, append-only platform audit log, `posctl bootstrap-superadmin`.

Step 5 follows, then a review stop.

## First Super Admin (dev)

```sh
docker compose -f deploy/docker-compose.yml --env-file deploy/.env run --rm --no-deps \
  -e BOOTSTRAP_ADMIN_EMAIL=you@example.com -e BOOTSTRAP_ADMIN_PASSWORD='<14+ chars>' \
  --entrypoint /usr/local/bin/posctl app bootstrap-superadmin
```

Then `POST https://admin.localhost:8443/api/platform/auth/login`. The first sign-in forces a new password and TOTP enrolment, and shows 10 recovery codes once.

## Tenants (dev)

```sh
P="docker compose -f deploy/docker-compose.yml --env-file deploy/.env run --rm --no-deps --entrypoint /usr/local/bin/posctl app"
$P tenant provision --slug demo-shop --name "Demo Shop" --owner-email owner@demo.test
$P migrate tenants up --dry-run     # then without --dry-run
$P migrate drift
```

## Requirements

- Go 1.27.2 (`go.mod` pins the toolchain)
- Docker with Compose (dev stack and integration tests)
- GNU make

## Common commands

```sh
make help          # list targets
make test          # unit tests
make check         # vet + constsql + golangci-lint + govulncheck + tests
make build         # bin/posd, bin/posctl
make up / down     # dev stack: Postgres 18, Caddy, S3 gateway, Mailpit, posd
```

## Running the dev stack

```sh
cp deploy/.env.example deploy/.env      # fill every value; generate secrets:
openssl rand -base64 32                 # one per key, never reuse values
make up
make db-init                            # roles, schemas, platform migrations
curl -k https://acme.localhost:8443/healthz
```

| URL | What |
|---|---|
| `https://<slug>.localhost:8443` | Tenant host (any valid slug for now; tenants arrive in step 2) |
| `https://admin.localhost:8443` | Super Admin host |
| `https://localhost:8443` | Apex (landing page only) |
| `http://127.0.0.1:8026` | Mailpit (captured email) |
| `127.0.0.1:55432` | Postgres (loopback only; `POSTGRES_HOST_PORT` to change) |

The ports avoid 80, 443, 5432 and 8025, which other software on the machine often uses. Change them with `HTTPS_PORT` and `POSTGRES_HOST_PORT` in `deploy/.env`.

Browsers resolve `*.localhost` to your machine. Some CLI tools on Windows don't. Use `curl --resolve acme.localhost:8443:127.0.0.1` or add hosts-file entries.

## Configuration

Environment variables only; see [`.env.example`](.env.example) and PHASE1_DESIGN §6. Startup fails, naming the variable but never its value, when a required value is missing or a secret is weak:

- Keys (`JWT_KEYS`, `PLATFORM_JWT_KEYS`, `MASTER_KEYS`) use the format `kid:base64`. The first key signs, and every key verifies, which allows rotation. Each must be at least 32 random bytes. `MASTER_KEYS` must be exactly 32 bytes.
- Placeholders, low-variety keys, and the JWT secret leaked in the old git history (checked by SHA-256 only) are refused.
- `PLATFORM_JWT_KEYS` must not reuse a `JWT_KEYS` key.
- Production also requires https, `TRUSTED_PROXY_CIDRS`, `sslmode=verify-full`, generated DB passwords (≥ 16 chars), S3 storage and a real domain.

`posctl config check` validates the environment without starting the server.

## Database roles (TENANCY §5.3)

| Role | Login | Use |
|---|---|---|
| `pos_owner` | no | Owns every schema and object |
| `pos_migrator` | yes | Migrations and provisioning (`SET ROLE pos_owner` via startup parameter) |
| `pos_app` | yes | Request traffic; no privileges of its own, empty `search_path` |
| `pos_platform` | no | DML on `platform.*`; entered with `SET LOCAL ROLE` |
| `t_<id>_rw` | no | One per tenant; DML on its own schema only |
| `pos_jobs` | yes | Background jobs |

`posctl db bootstrap` (as the admin login) creates them idempotently and sets the login passwords from `DATABASE_MIGRATOR_URL`, `DATABASE_URL` and `DATABASE_JOBS_URL`. Passwords are sent as SCRAM verifiers, never in clear. Re-run it to rotate them. `posctl db preflight` checks a managed Postgres against OPERATIONS §8 in a scratch database.

## Layout

```
cmd/posd       API + workers          cmd/posctl   operator CLI
internal/      app, config, httpx, obs, apperr, db, secrets, security, ...
tools/analyzers/constsql   lint rule: SQL arguments must be constants
deploy/        docker-compose (dev), Caddyfile
```

## Security notes

- Requests are routed by host: `<slug>.BASE_DOMAIN` gets the tenant API, `admin.BASE_DOMAIN` the platform API, and the apex only the landing page. Any other host gets `421`. `X-Forwarded-*` headers are honoured only from `TRUSTED_PROXY_CIDRS`.
- The admin and apex hosts use a strict CSP (no `unsafe-inline`). The tenant SPA keeps `style-src 'unsafe-inline'` until MUI styles move to nonces (deferred). JSON responses get `default-src 'none'`.
- Logs redact passwords, tokens, cookies, authorization headers, URL credentials and every `secrets.Secret`.
- `/metrics` is served only on `METRICS_ADDR` (loopback by default), never through Caddy.
- `make gitleaks` scans this branch's new commits. `make gitleaks-history` scans everything and fails until the history rewrite for the leaked secret (P0-1) runs.
