# Phase 1 — Analysis & Design: Go Backend Rewrite (Multi-Tenant SaaS)

| | |
|---|---|
| Status | **Revised 2026-10-15 after review: decisions Q-1…Q-16, extras and doc fixes applied (§12). Awaiting "approved".** No application code written. |
| Baseline | Commit `94aeeb8` "Update POS inventory system". Every file in `backend/src/**` was read; frontend auth, API client and every `api.*`/`fetch` call site were read. (Commit hashes change if the history rewrite in Q-7 runs; the subjects stay.) |
| Companion docs | [API_CONTRACT](./API_CONTRACT.md) · [TENANCY_DESIGN](./TENANCY_DESIGN.md) · [THREAT_MODEL](./THREAT_MODEL.md) · [EMAIL_AND_RECEIPTS](./EMAIL_AND_RECEIPTS.md) · [BILLING_DESIGN](./BILLING_DESIGN.md) · [OPERATIONS](./OPERATIONS.md) · [LIBRARIES](./LIBRARIES.md) |

---

## 1. Summary

- Rewrite **only the backend** in Go. Keep the React SPA, PostgreSQL, and the existing routes and JSON shapes. 21 deliberate contract changes are listed in API_CONTRACT §4 (all approved); most need no frontend change.
- **Web application only**, **English only** (UI, API messages, emails, receipts; currency and number format from tenant settings). The new system starts with an **empty database**; there is no legacy import.
- **Schema-per-tenant** in one database, one shared pgx pool, every request in a transaction with `SET LOCAL ROLE t_<id>_rw` + `set_config('search_path', '"t_<id>", extensions', true)`, and pgx in **`QueryExecModeExec`** so no cached statement can bind to the wrong schema. Per-tenant DB roles make a wrong-schema bug a *permission error*, not a leak.
- Tenant from the **subdomain**, which must match the token's tenant claim. Same-origin SPA+API, so `__Host-` cookies and no CORS in production.
- Platform **Super Admin** on `admin.<BASE_DOMAIN>`: separate identity, mandatory TOTP, step-up, audited, time-limited impersonation.
- Email via a **transactional outbox** written in the sale transaction. Workers fan out across tenant schemas safely. Receipts as PDF (maroto) + HTML email. Tenant SMTP (encrypted, SSRF-hardened) or platform fallback.
- **Razorpay** subscriptions behind a `BillingProvider`: verified, idempotent webhooks; lifecycle trial → active → past_due → grace → suspended → read_only → deleted.
- Found **10 P0 issues** in the old backend (an 11th, desktop LAN exposure, no longer applies because desktop is dropped), including two the brief did not list: **client-controlled prices** (a biller can sell anything for ₹0) and a **committed JWT secret + DB password in the public git history**.

### 1.1 Assumptions (explicit)

| # | Assumption | If wrong |
|---|---|---|
| A1 | Production Postgres is **≥ 16** (we target 18) on a managed provider that allows `CREATEROLE`, thousands of roles and PG16 grant syntax | Confirmed per provider by the preflight (OPERATIONS §8) before a managed instance is used. On PG 14/15 roles still work with `NOINHERIT` on `pos_app` |
| A2 | Early scale is ≤ 1,000 tenants per DB | See TENANCY §1 for ceiling and triggers |
| A3 | The Go backend lives in `backend-go/` next to the old `backend/` until cutover | **Decided** (Q-8) |
| A4 | Quantities stay integers (contract) | Weight-based selling needs `numeric` qty later (expand migration) |
| A5 | One currency per tenant; GST-style tax config by default but country-agnostic | Multi-currency per tenant is out of scope |
| A7 | Go 1.27.2 (current stable, checked on go.dev) instead of the "1.23+" floor | 1.23 is out of security support; we pin `go 1.27` + `toolchain go1.27.2` and follow patch releases |
| A8 | Hosting: one VM with docker-compose, managed Postgres with PITR, S3-compatible storage | **Decided** (Q-15) |
| A9 | Web application only; English only | **Decided** (Q-1, language decision). No desktop build, no i18n |
| A10 | Old data is demo data; the new system starts empty and seeds only roles, permissions and settings | **Decided**. No legacy import, no bcrypt path |

### 1.2 Challenges to the brief and to `SAAS_ARCHITECTURE.md`

| Topic | Brief / existing doc | Recommendation | Why |
|---|---|---|---|
| Tenancy | SAAS_ARCHITECTURE: shared tables + `tenant_id` + Sequelize global scope | Schema-per-tenant (brief) ✔ | A forgotten `WHERE` in a global scope is the classic leak. Per-schema + per-role isolation fails closed |
| `search_path` | `"t_xxx", platform` | `"t_xxx"` only | Name collisions (`email_outbox`, `audit_log` exist in both) would silently resolve to platform tables, and the tenant role would need platform privileges |
| `tenants.db_dsn` | DSN column | `db_cluster` key → DSN in the secret store | DSNs contain passwords. Secrets don't belong in DB rows |
| Migrations | golang-migrate | **goose v3** (Provider API) | Per-migration transactions, `NO TRANSACTION` annotation, Go migrations for backfills, no global "dirty" flag that blocks every later run, easy per-schema table. SQL files stay portable (LIBRARIES) |
| TOTP lib | pquerna/otp | stdlib RFC 6238 implementation (~80 lines + RFC test vectors) | pquerna/otp: last release Dec 2024 (> 12 months), fails the library rule |
| CORS | own tenant origin + admin origin | Same-origin, no CORS in prod | Admin console never calls tenant APIs (code-exchange impersonation) |
| Suspended tenants | SAAS_ARCHITECTURE: "allow login + billing page"; brief: "blocked at login" | Blocked from all logins, payment by emailed Razorpay link | **Decided** (Q-11) |
| `search_path` vs extensions | (not covered) | `"t_xxx", extensions` | Operators (`citext =`, trigram `%`) resolve through `search_path`; without `extensions` the `citext` comparison silently becomes case-sensitive (TENANCY §5.4) |
| Token lifetime | SAAS_ARCHITECTURE: 1 h access; old code: 30 d | 10 min access + rotated refresh with reuse detection | S-req 26 |
| Billing | SAAS_ARCHITECTURE: Stripe | Razorpay (brief) behind `BillingProvider` | INR/UPI market |
| POS lock | Brief: "in-memory POS lock" | **Correction:** the old lock is DB columns on `products` (`is_locked/locked_at/locked_by`), but it is racy (read-then-write), global across stores, and not enforced at checkout | Replace with `pos_locks` (store, product) + row locks at checkout |

---

## 2. Gap list (code-verified, ranked)

Severity: **P0** = exploitable or loses money/stock/data now · **P1** = serious security/correctness flaw · **P2** = bug, debt or doc mismatch. "PRD" = also in PRD §9.

### 2.1 P0

| # | Area | Finding (file:line) | Fix in Go backend |
|---|---|---|---|
| P0-1 | Secrets | **Real `backend/.env` committed** in "Initial commit", "first commit" and "changes in UI" (deleted in "Update POS inventory system", still in history, **pushed to `github.com/tejas1112001/POS_inventory`**): a real `JWT_SECRET` (64-byte hex) and a weak dictionary-word `DB_PASSWORD`. Values deliberately not repeated here. Anyone can forge admin JWTs for any deployment that used it. | **Owner is rotating both** (Q-7). New secrets via env only, fail-fast on weak values, SHA-256 of the leaked secret on a startup deny list. History rewrite prepared (exact commands handed over; runs only after the owner confirms). gitleaks in CI. |
| P0-2 | Auth | Public `/api/auth/signup` accepts `role`/`role_id` → anyone registers as admin; `...rest` mass-assigned into `User.create` (`auth.js:72-100`). PRD G1. | Endpoint removed on tenant hosts; owner only via provisioning; `POST /api/users` guarded. |
| P0-3 | Sales | **Client-controlled prices/totals**: `unitPrice`, line `total`, `discount`, `tax`, `shipping` taken from the request (`sales.js:96-110,155`). A biller can sell any item for 0 or give unlimited discount. | Server price/tax engine; `409 PRICE_CHANGED` unless the user has `sales:price_override` (Q-3, audited); discount permission + cap. |
| P0-4 | Stock | Oversell race: stock read without `FOR UPDATE`, unconditional `decrement` (`sales.js:114-124,161-167`); same in sale edit and stock-adjustment subtraction (`stockAdjustments.js:72-84`); no `CHECK (qty >= 0)`. PRD G4. | `SELECT … FOR UPDATE` in `variant_id` order + `UPDATE … WHERE qty >= n`; DB `CHECK`; concurrency tests. |
| P0-5 | Purchases | Status toggling **re-stocks every time**: `Received → Pending → Received` adds stock again (`purchases.js:118`). Later receive uses first variant + **store 1** default (`:119-128`). Per-line variant/store from creation are never stored (`purchase_items` lacks the columns). PRD G6. | `Received` terminal; `purchase_items.variant_id/store_id NOT NULL`; `purchases:approve`. |
| P0-6 | Returns | Return qty only checked vs this return (repeat returns unlimited); **negative qty accepted** → stock *decremented* and negative refund; refund `unitPrice` from client (can exceed sold price); store/customer not checked vs sale (`salesReturns.js:96-143`). PRD G7 (partially). | Cumulative check under row lock, qty > 0, sold price, matching store/customer. |
| P0-7 | Sessions | Inactive/deleted users can log in; `/refresh` re-issues 30-day tokens from any valid token **without a status check** → endless sessions (`auth.js:47-134`). Permissions come from the token's `roleId`, so **demoting an admin has no effect until their token expires** (up to 30 days) (`middleware/auth.js:27-39`). PRD G8 (partially). | Short access tokens, rotated refresh tokens, per-request auth-state check, `token_version`. |
| P0-8 | Mass assignment | `Sale.create({...saleData})`, `originalSale.update({...saleData})`, `SaleReturn.create({...saleReturnData})`, `User.create({...rest})`: client can set `total`, `paid`, `due`, `status`, `createdAt`, `id`… (`sales.js:127,247`, `salesReturns.js:120`). | Explicit DTOs, strict decoding, allow-listed fields only. |
| P0-9 | Products | `PUT /api/products/:id` updates `findByPk(v.id)` **without checking `product_id`** → any variant (with its stock) can be moved into another product; omitted variants are hard-deleted **with their stock** and no record (`products.js:344-391`). | Ownership check, soft delete, refuse when stock > 0, ledger. |
| P0-10 | Escalation | Any `users:edit` holder can assign the admin role to themselves (`billers.js:61`, `roles.js:171-194`); any `roles:edit` holder can grant their own role every permission and edit system roles (`roles.js:104-143`). | Subset rule, no self-change, immutable owner/admin, audit. |

| P0-11 | Desktop/LAN | **Not applicable: web only (Q-1).** | — |

### 2.2 P1

| # | Area | Finding | Fix |
|---|---|---|---|
| P1-1 | RBAC | `stores:*` not seeded → admins can't manage stores/settings/showcase (PRD G2) | Seed `stores`, `settings` |
| P1-2 | RBAC | Unguarded: `GET /api/stores[/:id]`, `GET /:id/showcase` (and it **writes** on GET), `POST /api/product-variants`, `GET /api/sub-categories` (public); legacy `isAdmin` on sub-categories POST (PRD G10) | Guards per API_CONTRACT §3 |
| P1-3 | Object-level | "Own sales only" keyed on legacy `role === 'biller'` → every custom role is restricted, and `/api/invoices`, `/api/sales/report`, `/api/dashboard` show **all** sales/revenue to billers; sale PUT/DELETE have no ownership check | `sales:view_all` + scoped queries everywhere |
| P1-4 | Data | `payment_status 'Partial'` violates the enum → 500 (PRD G3); `paid: 0` coerced to total (`sales.js:109`) → unpaid sales recorded as Paid | Check constraint incl. Partial; respect 0 |
| P1-5 | Data | POS sends `method`, not `payment_method`, and no `date` → **every POS sale has NULL `date` and NULL `payment_method`**; reports (filter on `date`) and the dashboard chart (skips NULL) **omit all POS sales**; invoices show empty due date | Server sets `date`; alias mapping (C-04) |
| P1-6 | Tax | Inclusive tax added on top (customer overcharged); label "Tax (8%)" hard-coded (PRD G5) | Server tax engine |
| P1-7 | Integrity | Hard deletes of sales, products, customers, users, stores; FKs `SET NULL` orphan history (PRD G9) | Soft delete, void, `RESTRICT` FKs |
| P1-8 | Schema | `sync({alter:true})` at boot; no migrations; system role permissions **reset on every boot** | goose migrations; seed once |
| P1-9 | Money | `FLOAT` for every money column | `NUMERIC(18,4)` + decimal |
| P1-10 | Abuse | No rate limiting; login user enumeration (404 vs 401); bcrypt cost 10; no password policy | Rate limits, lockout, argon2id, policy |
| P1-11 | Errors | `err.message` returned to clients (Sequelize/SQL details); validation as 500; `{message}` vs `{error}` mix | Error mapper, codes |
| P1-12 | DoS | `limit` unbounded on every paginated route incl. **public** shop; most lists unpaginated; report builds `IN (all sale ids)`; 10 MB JSON bodies; base64 images in every list payload | Caps, SQL joins, file storage |
| P1-13 | Images | Base64/URL strings stored in TEXT, no type/size/content validation (S-req 14) | Upload pipeline |
| P1-14 | POS lock | Read-then-write race; global across stores; blocks back-office edits; expiry checked client-side; never enforced at checkout | `pos_locks` + checkout row locks |
| P1-15 | Purchases | `purchases:approve` defined, never enforced; totals client-supplied | Enforce, recompute |
| P1-16 | Public shop | `NODE_ENV=development` exposes unpublished products; numeric-id lookup enables store enumeration; per-page price sort; global `is_published` side effect (PRD G16) | C-18 |
| P1-17 | Tokens | 30-day bearer JWT in `localStorage` (XSS → full account takeover), no `iss/aud`, invalid token → 403 (FE refresh logic never fires) | C-02, C-03 |
| P1-18 | Audit | No audit log anywhere (role changes, price changes, stock edits, deletes) | Tenant audit log |
| P1-19 | Uniqueness | SKU/barcode not unique → barcode scan returns an arbitrary product; sale reference not unique | Partial unique indexes per tenant |
| P1-20 | Ops | No security headers, request timeouts, graceful shutdown, structured logs, health check of DB | OPERATIONS |
| P1-21 | Frontend | `AddSales.jsx` never sends `storeId` → **back-office sale entry always fails** (500) | FE fix (C-04) |
| P1-22 | Credentials | Demo credentials hard-coded: `reset_passwords.js` (`admin123`, `123456`), all four `scripts/*.mjs` (`admin@pos.com / admin123`), FLOW_TEST.md | Removed; tests use generated creds |

### 2.3 P2

| # | Finding |
|---|---|
| P2-1 | `/api/warehouses` not mounted; WarehouseModal → 404 (PRD G11). **Mounted in step 6** (Q-12) |
| P2-2 | `ImportProduct` posts to `/products`; no backend (PRD G12). **CSV import built in step 7** (Q-12, API_CONTRACT §5.1.1) |
| P2-3 | Stock adjustments have no UI (PRD G13) |
| P2-4 | `PublicShopPage` hard-codes `http://localhost:3000` (PRD G14) |
| P2-5 | Dashboard chart: earliest 100 sales, month names merge years (PRD G15); `totalUnpaid` ignores Partial |
| P2-6 | Docs mismatch: README says MongoDB; PERMISSIONS.md lists unseeded actions (PRD G17); `utils/mongoose.js` shim |
| P2-7 | Electron scripts/dependency missing; preload IPC channels without handlers; `executeScript` interpolates `err.message` into JS (PRD G18). Web only (Q-1): the Electron files are deleted at cutover |
| P2-8 | Root `index.js` is stale dead code with `isDev = true ||` and imports of missing files; `services/concurrencycontrol.js` unused and references a missing `version` column |
| P2-9 | `PUT /api/users/:id` without `email` sets email to `''`; `PUT /api/stores/:id` writes `undefined` for missing fields |
| P2-10 | Reports `stockQty` always 0; invoice `dueDate` is sale date formatted `en-GB` server-side |
| P2-11 | Store slug cleaning differs between auto-generate and edit (`\w` allows `_`) |
| P2-12 | Sequelize `Op.iLike || Op.like` dead fallback; `%`/`_` in search not escaped (wildcard injection, low impact) |
| P2-13 | `seed.js` lives in `routes/` and seeds a store named `islamabad-main` with qty 100 for all variants |
| P2-14 | CORS rejection throws an `Error` → 500 instead of a clean CORS denial |

---

## 3. Architecture

```
                 ┌──────────────── Caddy (TLS: *.BASE_DOMAIN via DNS-01) ───────────────┐
Browser ──HTTPS──▶  <slug>.BASE_DOMAIN   admin.BASE_DOMAIN   BASE_DOMAIN                   │
                 └───────────────────────────────┬──────────────────────────────────────┘
                                                 ▼ X-Forwarded-Host/Proto (trusted CIDR)
┌────────────────────────────── Go binary `posd` (cmd/api) ──────────────────────────────┐
│ chi router                                                                              │
│  ├─ host classifier ─┬─ tenant router: SPA static + /api/* + /api/public/shop/* + /files │
│  │                   ├─ platform router: admin SPA + /api/platform/*                     │
│  │                   └─ apex router: static landing page only (no self-signup, Q-5)      │
│  ├─ middleware: request-id → recover → slog → secure headers → body limit → timeout     │
│  │              → rate limit → host/tenant resolve → auth → auth-state → gate → RBAC    │
│  ├─ modules: handler → service → repository (sqlc)                                      │
│  ├─ tenancy.Sessions (InTenantTx / InPlatformTx)  ── pgxpool (Exec mode) ──┐            │
│  ├─ change listener (LISTEN, own direct pgx.Conn, cache flush on reconnect) ┤            │
│  ├─ River client (jobs: outbox fan-out, receipts, notifications, billing, migrations,  │
│  │                 exports, provisioning)  ── jobs pool ──────────────────┤            │
│  └─ metrics listener :9090 (/metrics, /internal/*)                         │            │
└─────────────────────────────────────────────────────────────────────────────┼───────────┘
                                                                              ▼
   PostgreSQL 18: extensions │ platform │ river │ t_ab12cd34ef │ t_…            Object storage (S3/MinIO/FS)
                                                                              SMTP (platform + per-tenant)
                                                                              Razorpay API + webhooks
```

The same binary runs API + workers (`POSD_MODE=all|api|worker`) so it can be split later without code changes. There is no desktop mode. `posctl` is the operator CLI.

### 3.1 Folder structure

```
backend-go/
├─ cmd/
│  ├─ posd/            # HTTP API + workers (mode flag)
│  └─ posctl/          # migrate, db preflight, bootstrap-superadmin, provision, export, restore, drift
├─ internal/
│  ├─ app/             # wiring, lifecycle, graceful shutdown
│  ├─ config/          # env parsing + validation (fail fast on missing/weak secrets)
│  ├─ httpx/           # router, middleware, JSON decode/encode, error mapping, pagination
│  ├─ obs/             # slog setup, Prometheus metrics, OTel (optional)
│  ├─ db/              # pool config (Exec mode, hooks), tx helpers, pg error mapping, ident.go (only non-constant SQL)
│  ├─ tenancy/         # SchemaName type, host resolver, registry cache, Sessions, cluster resolver, change listener
│  ├─ migrate/         # two-track runner, dry run, drift fingerprint, conn.go (startup-param connections)
│  ├─ tenantio/        # per-tenant export / restore (streamed checksums, age)
│  ├─ provisioning/    # orchestrator + steps + compensation
│  ├─ platform/        # super admin auth/MFA, tenants admin, impersonation, announcements, platform audit
│  ├─ auth/            # tenant login, refresh rotation, password reset/invite, lockout
│  ├─ rbac/            # permission catalog, middleware, cached resolver, anti-escalation
│  ├─ audit/           # tenant audit writer
│  ├─ settings/        # tenant settings (currency, number format, timezone, tax, receipt template)
│  ├─ catalog/         # categories, sub-categories, brands, units, suppliers, warehouses
│  ├─ customers/
│  ├─ stores/          # stores + showcase admin
│  ├─ products/        # products, variants, barcode, CSV import
│  ├─ inventory/       # inventory, adjustments, stock ledger, POS locks
│  ├─ pricing/         # tax/discount/rounding engine (pure functions, heavily tested)
│  ├─ sales/           # sales, invoices view
│  ├─ returns/
│  ├─ purchases/
│  ├─ reports/         # reports, dashboard, Excel/PDF export
│  ├─ publicshop/
│  ├─ receipts/        # receipt model, PDF render, storage
│  ├─ mailer/          # EmailSender, SMTP impl, safe dialer, templates, outbox, workers
│  ├─ billing/         # BillingProvider, Razorpay, webhook, lifecycle, limits, notifier
│  ├─ payments/        # PaymentProvider for POS (manual/cash default)
│  ├─ files/           # Storage interface (fs, s3), image validation/re-encode
│  ├─ jobs/            # River setup, periodic jobs
│  ├─ secrets/         # envelope encryption, key ring
│  ├─ security/        # argon2id, TOTP, token hashing, password policy, leaked-secret deny list (hashes)
│  └─ money/           # decimal helpers, currency exponents, rounding
├─ db/
│  ├─ migrations/{platform,tenant}/NNNN_name.sql
│  ├─ queries/{platform,tenant}/*.sql
│  └─ sqlc.yaml        # two packages: internal/db/platformdb, internal/db/tenantdb
├─ web/                # (optional) embedded SPA builds
├─ test/               # integration, isolation, search_path, migration, e2e helpers, k6 scripts
├─ tools/analyzers/    # constsql (go/analysis): SQL arguments must be constants
├─ spikes/pdfshaping/  # throwaway Devanagari PDF spike, start of step 9 (EMAIL_AND_RECEIPTS §5.6)
├─ deploy/             # Dockerfile, docker-compose.yml (Caddy, Postgres 18, MinIO, Mailpit), Caddyfile(s), prometheus rules
├─ .github/            # CI workflow, dependabot.yml (repo root)
├─ Makefile  .env.example  README.md  go.mod
└─ pkg/                # intentionally empty: nothing is meant for external import yet
```

### 3.2 Layers

| Layer | Responsibility | May not |
|---|---|---|
| Handler | Decode + validate DTO (strict), call one service method, map result/error to JSON | Touch the DB, contain business rules |
| Service | Business rules, permissions beyond route-level (ownership, anti-escalation), transactions via `tenancy.InTenantTx`, audit, outbox | Know HTTP |
| Repository | sqlc queries + small hand-written SQL where sqlc can't (dynamic filters use sqlc `sqlc.narg` patterns) | Contain rules |
| Domain packages (`pricing`, `money`) | Pure functions | I/O |

Errors flow as typed domain errors (`apperr.Validation{Fields}`, `apperr.NotFound`, `apperr.Conflict{Code}`, …) mapped once in `httpx`.

---

## 4. Auth, RBAC, money, stock (key designs)

### 4.1 Tenant authentication
- **Passwords**: argon2id (`m=46 MiB, t=1, p=1`, OWASP), PHC string format, constant-time compare, global semaphore limiting concurrent hashes (memory-DoS guard). No bcrypt path (empty start, no legacy hashes). Policy: ≥ 12 chars, ≤ 128, blocklist of common/breached passwords (embedded list), not equal to email/business name; no composition rules (NIST 800-63B).
- **Login**: `POST /api/auth/login` on the tenant host. Rate limits: 5/min per (IP, email), 20/min per IP, 100/min per tenant. Progressive lockout: after 5 failures `locked_until = now + 2^n min` (max 1 h). Dummy hash on unknown email (timing). Generic `401`.
- **Tokens**: access JWT HS256, 10 min, claims `iss=<host>`, `aud=tenant`, `tid`, `sub`, `tv` (token_version), `sid` (refresh family), `imp?`. Keys `JWT_KEYS="kid:base64,..."`, first signs, all verify (rotation). Refresh: 32 random bytes, SHA-256 stored in `refresh_tokens` with `family_id`, rotated on every use; reuse of a rotated token → revoke the whole family + audit event. Remember-me 30 d sliding, else 12 h absolute.
- **Revocation**: logout (family), password change/reset (all families + `token_version++`), role/status change (`token_version++` + all families), admin "sign out everywhere".

### 4.2 RBAC
- Permission catalog in code (`rbac.Catalog`) → synced to `platform.permission_catalog` and each tenant's `permissions` table by migration/seed (keeps integer `permissionIds` contract).
- Route declarations; startup self-check that every tenant route has a requirement or an explicit `rbac.Public`/`rbac.Authenticated` marker.
- Resolver: `perm:<tenant>:<role_id>` cache, TTL 60 s, invalidated via `NOTIFY role_changed` on the dedicated listener connection, all caches flushed on reconnect (TENANCY §5.5; multi-node safe; fixes the per-process cache).
- Object-level rules live in services: own-sales scope, store scope (future `user_stores`), receipts, returns.
- Anti-escalation: grantable set ⊆ actor's set; `owner`/`admin` immutable; no self role/status change; last owner protected.

### 4.3 Money & tax
- DB: `NUMERIC(18,4)` for unit prices/costs, `NUMERIC(18,2)`-equivalent rounding for line and document totals (stored as `NUMERIC(18,4)` rounded to the currency exponent, so 3-decimal currencies work). Go: `decimal.Decimal`; JSON as numbers.
- Currency exponent from ISO 4217 table (`INR`=2, `JPY`=0, `KWD`=3), tenant `settings.currency`.
- `pricing.Quote(lines, cartDiscount, settings)`: per line `gross = price × qty`; cart discount allocated pro-rata to lines (largest-remainder to the minor unit) **before tax** (Q-2, decided); price override only with `sales:price_override` (Q-3, decided), audited; exclusive tax `tax = round(net × rate)`; inclusive tax `tax = net − net / (1 + rate)`, not added again; per-line rounding half-up; document totals = sums. GST split (CGST/SGST vs IGST) stored per line for receipts.
- Shipping not taxed by default (setting).

### 4.4 Stock safety
- Checkout: merge duplicate variant lines → `SELECT id, qty FROM inventories WHERE store_id=$1 AND variant_id = ANY($2) ORDER BY variant_id FOR UPDATE` (deterministic lock order → no deadlocks) → validate → `UPDATE … SET qty = qty - d.n FROM unnest(...) d WHERE … AND qty >= d.n` → rows-affected check → `stock_movements` rows. `CHECK (qty >= 0)` as the last line of defense.
- Sale edit = void old lines (restock) + apply new lines in the same transaction, locks taken over the union of variants in sorted order.
- `pos_locks(store_id, product_id, user_id, expires_at)`: `INSERT … ON CONFLICT (store_id, product_id) DO UPDATE … WHERE pos_locks.expires_at < now() OR pos_locks.user_id = EXCLUDED.user_id RETURNING …`. Atomic, per store, TTL 2 min, swept by a job. Advisory only (UX). Correctness comes from the row locks.

---

## 5. Tenant schema (DDL sketch)

Conventions: `bigint GENERATED ALWAYS AS IDENTITY` PKs (`OVERRIDING SYSTEM VALUE` only in per-tenant restore), `timestamptz`, `text` + `CHECK` (no ENUMs, see TENANCY §5.2), `citext`/`pg_trgm` from the shared schema `extensions`, which is on the tenant `search_path` (TENANCY §5.4), `deleted_at` soft delete, FKs `ON DELETE RESTRICT`. Columns used by JSON keep their old names so the contract mapping stays trivial (`createdAt` is mapped in SQL `AS "createdAt"`; DB columns are snake_case).

```sql
-- identity -------------------------------------------------------------------
CREATE TABLE roles (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name citext NOT NULL, description text, color text NOT NULL DEFAULT '#6366f1',
  is_system boolean NOT NULL DEFAULT false,
  system_key text UNIQUE CHECK (system_key IN ('owner','admin','biller')),
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE UNIQUE INDEX roles_name_uq ON roles (name) WHERE deleted_at IS NULL;

CREATE TABLE permissions (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  module text NOT NULL, action text NOT NULL, label text,
  UNIQUE (module, action)
);
CREATE TABLE role_permissions (
  role_id bigint NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  permission_id bigint NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
  PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE users (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
  email citext NOT NULL CHECK (length(email) <= 254),
  password_hash text,                                   -- NULL until invite accepted
  status text NOT NULL DEFAULT 'Active' CHECK (status IN ('Active','Inactive')),
  role_id bigint NOT NULL REFERENCES roles(id),
  token_version int NOT NULL DEFAULT 1,
  failed_logins int NOT NULL DEFAULT 0, locked_until timestamptz,
  password_changed_at timestamptz, last_login_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE UNIQUE INDEX users_email_uq ON users (email) WHERE deleted_at IS NULL;

CREATE TABLE refresh_tokens (
  id uuid PRIMARY KEY, user_id bigint NOT NULL REFERENCES users(id),
  family_id uuid NOT NULL, token_hash bytea NOT NULL UNIQUE,
  remember boolean NOT NULL, issued_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL, absolute_expires_at timestamptz NOT NULL,
  rotated_at timestamptz, revoked_at timestamptz, revoke_reason text,
  ip inet, user_agent text
);
CREATE INDEX ON refresh_tokens (user_id) WHERE revoked_at IS NULL;
CREATE INDEX ON refresh_tokens (family_id);

CREATE TABLE auth_tokens (                               -- invite / password reset
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id bigint NOT NULL REFERENCES users(id),
  purpose text NOT NULL CHECK (purpose IN ('invite','reset')),
  token_hash bytea NOT NULL UNIQUE, expires_at timestamptz NOT NULL, used_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- settings -------------------------------------------------------------------
CREATE TABLE settings (
  id smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  business_name text NOT NULL, legal_name text, address text, phone text, email citext,
  logo_file_id uuid,
  currency char(3) NOT NULL DEFAULT 'INR' CHECK (currency ~ '^[A-Z]{3}$'),
  locale text NOT NULL DEFAULT 'en-IN' CHECK (locale ~ '^en-[A-Z]{2}$'),  -- number/date format only; all text is English
  timezone text NOT NULL DEFAULT 'Asia/Kolkata',
  tax_label text NOT NULL DEFAULT 'GST', tax_registration_no text,   -- e.g. GSTIN
  tax_split text NOT NULL DEFAULT 'cgst_sgst' CHECK (tax_split IN ('none','cgst_sgst','igst')),
  prices_include_tax_default boolean NOT NULL DEFAULT false,
  discount_mode text NOT NULL DEFAULT 'prorata_pre_tax',
  max_discount_pct_without_override numeric(5,2) NOT NULL DEFAULT 10,
  invoice_prefix text NOT NULL DEFAULT 'POS-INV',
  receipt_header text, receipt_footer text,
  default_customer_id bigint,
  updated_at timestamptz NOT NULL DEFAULT now()
);
-- tenant_email_settings: EMAIL_AND_RECEIPTS §3

-- master data (same pattern for categories, brands, units, suppliers, warehouses) ----
CREATE TABLE stores (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name text NOT NULL, slug citext NOT NULL CHECK (slug ~ '^[a-z0-9](?:[a-z0-9-]{0,58}[a-z0-9])?$'),
  address text, phone text, status text NOT NULL DEFAULT 'Active' CHECK (status IN ('Active','Inactive')),
  logo_file_id uuid, banner_file_id uuid, description text, opening_hours text,
  is_public_enabled boolean NOT NULL DEFAULT true,
  created_by bigint REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE UNIQUE INDEX stores_slug_uq ON stores (slug) WHERE deleted_at IS NULL;

CREATE TABLE customers (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name text NOT NULL, email citext, phone text, address text, city text, country text,
  image_file_id uuid, status text NOT NULL DEFAULT 'Active' CHECK (status IN ('Active','Inactive')),
  marketing_consent boolean NOT NULL DEFAULT false, marketing_consent_at timestamptz,
  created_by bigint REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE INDEX customers_name_trgm ON customers USING gin (name gin_trgm_ops);  -- pg_trgm in "extensions"
CREATE INDEX ON customers (phone); CREATE INDEX ON customers (email);

-- catalogue ------------------------------------------------------------------
CREATE TABLE products (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name text NOT NULL, description text, slug citext,
  status text NOT NULL DEFAULT 'Active' CHECK (status IN ('Active','Inactive')),
  image_file_id uuid,
  selling_type text CHECK (selling_type IN ('Online','POS')),
  product_type text NOT NULL CHECK (product_type IN ('single','variable')),
  tax_type text CHECK (tax_type IN ('Exclusive','Inclusive')),
  tax_rate numeric(6,3) CHECK (tax_rate BETWEEN 0 AND 100),
  hsn_code text,                                          -- S4 (GST)
  discount_type text CHECK (discount_type IN ('Percentage','Fixed')),
  discount_value numeric(18,4) CHECK (discount_value >= 0),
  warranties text, barcode_symbology text,
  is_published boolean NOT NULL DEFAULT false, is_featured boolean NOT NULL DEFAULT false,
  category_id bigint REFERENCES categories(id), sub_category_id bigint REFERENCES sub_categories(id),
  brand_id bigint REFERENCES brands(id), unit_id bigint REFERENCES units(id),
  supplier_id bigint REFERENCES suppliers(id),
  created_by bigint REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE INDEX products_active_idx ON products (status, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX products_name_trgm ON products USING gin (name gin_trgm_ops);
CREATE INDEX ON products (category_id); CREATE INDEX ON products (brand_id);

CREATE TABLE product_variants (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  product_id bigint NOT NULL REFERENCES products(id),
  sku citext NOT NULL, item_barcode text,
  price numeric(18,4) NOT NULL CHECK (price >= 0), cost numeric(18,4) NOT NULL DEFAULT 0 CHECK (cost >= 0),
  weight numeric(12,3) NOT NULL DEFAULT 0, attributes jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(attributes) = 'object'),
  expiry_date date, manufactured_date date,
  created_by bigint REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE UNIQUE INDEX variants_sku_uq ON product_variants (sku) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX variants_barcode_uq ON product_variants (item_barcode) WHERE deleted_at IS NULL AND item_barcode IS NOT NULL;
CREATE INDEX ON product_variants (product_id);

CREATE TABLE store_products (
  store_id bigint NOT NULL REFERENCES stores(id), product_id bigint NOT NULL REFERENCES products(id),
  is_published boolean NOT NULL DEFAULT false, is_featured boolean NOT NULL DEFAULT false,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (store_id, product_id)
);
CREATE INDEX ON store_products (store_id) WHERE is_published;

-- stock ----------------------------------------------------------------------
CREATE TABLE inventories (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  variant_id bigint NOT NULL REFERENCES product_variants(id),
  store_id bigint NOT NULL REFERENCES stores(id),
  qty integer NOT NULL DEFAULT 0 CHECK (qty >= 0),
  quantity_alert integer NOT NULL DEFAULT 0 CHECK (quantity_alert >= 0),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (store_id, variant_id)
);
CREATE INDEX ON inventories (variant_id);

CREATE TABLE stock_movements (                            -- append-only ledger (new)
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  store_id bigint NOT NULL REFERENCES stores(id), variant_id bigint NOT NULL REFERENCES product_variants(id),
  delta integer NOT NULL CHECK (delta <> 0), qty_after integer NOT NULL,
  reason text NOT NULL CHECK (reason IN ('opening','sale','sale_edit','sale_void','return',
          'purchase_receive','adjustment','manual_set','product_edit','import')),
  ref_type text, ref_id bigint, user_id bigint REFERENCES users(id), impersonation_id uuid,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ON stock_movements (store_id, variant_id, created_at DESC);
CREATE INDEX ON stock_movements (ref_type, ref_id);

CREATE TABLE pos_locks (
  store_id bigint NOT NULL REFERENCES stores(id), product_id bigint NOT NULL REFERENCES products(id),
  user_id bigint NOT NULL REFERENCES users(id), locked_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  PRIMARY KEY (store_id, product_id)
);
-- stock_adjustments: as today + product_id, store/variant FKs, quantity > 0 CHECK

-- selling --------------------------------------------------------------------
CREATE TABLE sales (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  reference text NOT NULL UNIQUE,
  date timestamptz NOT NULL DEFAULT now(),
  status text NOT NULL DEFAULT 'Completed' CHECK (status IN ('Completed','Pending','Void')),
  payment_status text NOT NULL CHECK (payment_status IN ('Paid','Partial','Unpaid','Overdue')),
  payment_method text, payment_note text, note text,
  subtotal numeric(18,4) NOT NULL, discount numeric(18,4) NOT NULL DEFAULT 0,
  tax numeric(18,4) NOT NULL DEFAULT 0, shipping numeric(18,4) NOT NULL DEFAULT 0,
  total numeric(18,4) NOT NULL CHECK (total >= 0),
  paid numeric(18,4) NOT NULL DEFAULT 0 CHECK (paid >= 0), due numeric(18,4) NOT NULL DEFAULT 0 CHECK (due >= 0),
  tendered numeric(18,4), change_due numeric(18,4),
  currency char(3) NOT NULL,                              -- snapshot
  customer_id bigint NOT NULL REFERENCES customers(id), store_id bigint NOT NULL REFERENCES stores(id),
  user_id bigint NOT NULL REFERENCES users(id),           -- biller / owner of the sale
  updated_by bigint REFERENCES users(id), impersonation_id uuid,
  receipt_email citext,
  voided_at timestamptz, voided_by bigint REFERENCES users(id), void_reason text,
  version int NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sales_date_idx ON sales (date DESC) WHERE status <> 'Void';
CREATE INDEX ON sales (user_id, date DESC); CREATE INDEX ON sales (store_id, date DESC);
CREATE INDEX ON sales (customer_id); CREATE INDEX sales_ref_trgm ON sales USING gin (reference gin_trgm_ops);

CREATE TABLE order_items (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  sale_id bigint NOT NULL REFERENCES sales(id),
  product_id bigint REFERENCES products(id), variant_id bigint REFERENCES product_variants(id),
  product_name text NOT NULL, sku text,                   -- snapshots for receipts/history
  quantity integer NOT NULL CHECK (quantity > 0),
  unit_price numeric(18,4) NOT NULL CHECK (unit_price >= 0),
  catalog_price numeric(18,4) NOT NULL,                   -- differs from unit_price only on a price override (Q-3)
  discount numeric(18,4) NOT NULL DEFAULT 0, tax_type text, tax_rate numeric(6,3) NOT NULL DEFAULT 0,
  tax_amount numeric(18,4) NOT NULL DEFAULT 0, cgst numeric(18,4), sgst numeric(18,4), igst numeric(18,4),
  subtotal numeric(18,4) NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ON order_items (sale_id); CREATE INDEX ON order_items (product_id); CREATE INDEX ON order_items (variant_id);

CREATE TABLE sale_returns (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  reference text NOT NULL UNIQUE, date timestamptz NOT NULL DEFAULT now(),
  status text NOT NULL DEFAULT 'Pending' CHECK (status IN ('Pending','Completed')),
  payment_status text NOT NULL DEFAULT 'Unpaid' CHECK (payment_status IN ('Paid','Unpaid','Partial')),
  total numeric(18,4) NOT NULL CHECK (total >= 0), paid numeric(18,4) NOT NULL DEFAULT 0, due numeric(18,4) NOT NULL DEFAULT 0,
  sale_id bigint REFERENCES sales(id), customer_id bigint NOT NULL REFERENCES customers(id),
  store_id bigint NOT NULL REFERENCES stores(id), created_by bigint NOT NULL REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE return_items (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  sale_return_id bigint NOT NULL REFERENCES sale_returns(id),
  order_item_id bigint REFERENCES order_items(id),        -- links to the sold line (cumulative check)
  product_id bigint REFERENCES products(id), variant_id bigint NOT NULL REFERENCES product_variants(id),
  quantity integer NOT NULL CHECK (quantity > 0), unit_price numeric(18,4) NOT NULL CHECK (unit_price >= 0),
  reason text, subtotal numeric(18,4) NOT NULL
);
CREATE INDEX ON return_items (order_item_id);

CREATE TABLE receipts (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  sale_id bigint NOT NULL REFERENCES sales(id), sale_version int NOT NULL,
  status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','rendered','failed')),
  file_key text, sha256 bytea, rendered_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (sale_id, sale_version)
);

CREATE TABLE idempotency_keys (                           -- S2: safe POS retries
  key text NOT NULL, user_id bigint NOT NULL, request_hash bytea NOT NULL,
  status int, response jsonb, created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, key)
);

-- buying ---------------------------------------------------------------------
CREATE TABLE purchases (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  reference text NOT NULL, date timestamptz NOT NULL DEFAULT now(),
  status text NOT NULL CHECK (status IN ('Ordered','Pending','Received','Cancelled')),
  payment_status text NOT NULL DEFAULT 'Unpaid' CHECK (payment_status IN ('Paid','Unpaid','Partial','Overdue')),
  total numeric(18,4) NOT NULL, paid numeric(18,4) NOT NULL DEFAULT 0, due numeric(18,4) NOT NULL DEFAULT 0,
  supplier_id bigint NOT NULL REFERENCES suppliers(id),
  received_at timestamptz, received_by bigint REFERENCES users(id),
  created_by bigint NOT NULL REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX purchases_ref_uq ON purchases (supplier_id, reference);
CREATE TABLE purchase_items (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  purchase_id bigint NOT NULL REFERENCES purchases(id),
  product_id bigint NOT NULL REFERENCES products(id),
  variant_id bigint NOT NULL REFERENCES product_variants(id),
  store_id bigint NOT NULL REFERENCES stores(id),
  quantity integer NOT NULL CHECK (quantity > 0), unit_price numeric(18,4) NOT NULL CHECK (unit_price >= 0),
  discount numeric(18,4) NOT NULL DEFAULT 0, subtotal numeric(18,4) NOT NULL
);

-- files, outbox, audit ---------------------------------------------------------
CREATE TABLE files (
  id uuid PRIMARY KEY, kind text NOT NULL CHECK (kind IN ('image','receipt','export')),
  purpose text NOT NULL CHECK (purpose IN ('tenant_logo','store_logo','store_banner','product_image',
                                          'customer_image','user_image','receipt','export')),
                                                          -- only the first four can ever be public (API_CONTRACT /files/t/)
  content_type text NOT NULL, size_bytes bigint NOT NULL, sha256 bytea NOT NULL,
  storage_key text NOT NULL UNIQUE, created_by bigint REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(), deleted_at timestamptz
);
-- email_outbox + tenant-bound trigger function mark_outbox_pending(): EMAIL_AND_RECEIPTS §4

CREATE TABLE audit_log (                                  -- tenant role: INSERT + SELECT only
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  at timestamptz NOT NULL DEFAULT now(),
  actor_type text NOT NULL CHECK (actor_type IN ('user','platform_impersonation','system')),
  actor_id bigint, impersonation_id uuid,
  action text NOT NULL, entity text NOT NULL, entity_id text,
  before jsonb, after jsonb,                              -- secrets/passwords never included
  ip inet, user_agent text, request_id text
);
CREATE INDEX ON audit_log (at DESC); CREATE INDEX ON audit_log (entity, entity_id);
```

Audited actions (S-req 12): login success/failure/lockout, logout-all, password change/reset, user create/edit/delete/status, role create/edit/delete/assign, settings change, SMTP settings change/test, price change (variant price diff), stock change (manual set, adjustment), sale edit/void, return, purchase receive, showcase publish, export request, impersonation start/end.

---

## 6. Configuration (env only)

| Variable | Required | Default | Notes |
|---|---|---|---|
| `POSD_MODE` | | `all` | `api`, `worker`, `all` |
| `HTTP_ADDR` | | `:8080` | Listener on the private network behind Caddy |
| `STATIC_DIR` | | embedded | Tenant SPA and admin console builds (same-origin, S5) |
| `METRICS_ADDR` | | `127.0.0.1:9090` | `/metrics`, `/internal/*`, never proxied |
| `BASE_DOMAIN` | ✔ | `localhost` in dev | No domain is hard-coded anywhere |
| `PUBLIC_SCHEME` | | `https` | `http` only allowed with `APP_ENV=development` |
| `PLATFORM_NAME` | ✔ | — | Display name in emails/console (no brand in code) |
| `APP_ENV` | ✔ | — | `development`, `test`, `production` (production enforces stricter checks) |
| `TRUSTED_PROXY_CIDRS` | ✔ prod | — | Only these may set `X-Forwarded-*` |
| `DATABASE_URL` | ✔ | — | `pos_app` login (sslmode=verify-full in prod) |
| `DATABASE_MIGRATOR_URL` | ✔ for posctl | — | `pos_migrator` |
| `DATABASE_JOBS_URL` | ✔ | — | `pos_jobs` |
| `LISTEN_DATABASE_URL` | | = `DATABASE_URL` | Dedicated `LISTEN` connection; must be a **direct** endpoint, never a transaction pooler (TENANCY §5.5) |
| `DB_CLUSTERS` | | — | `name=dsn;...` for dedicated tenant DBs (secret) |
| `DB_POOL_MAX` / `DB_POOL_MIN` | | `4×CPU` / `2` | |
| `DB_STATEMENT_TIMEOUT` | | `5s` (reports `30s`) | Per tx via `set_config` |
| `JWT_KEYS` | ✔ | — | `kid:base64(≥32 bytes)` list; startup fails if any key < 32 bytes or looks like a placeholder |
| `PLATFORM_JWT_KEYS` | ✔ | — | Same format, separate keys for platform tokens (S3); startup fails if any key equals a `JWT_KEYS` key |
| `ACCESS_TOKEN_TTL` / `PLATFORM_ACCESS_TOKEN_TTL` | | `10m` / `5m` | |
| `REFRESH_TTL_REMEMBER` / `REFRESH_TTL_SESSION` | | `720h` / `12h` | |
| `PLATFORM_SESSION_MAX` / `PLATFORM_IDLE_TIMEOUT` | | `8h` / `30m` | |
| `MASTER_KEYS` | ✔ | — | Envelope KEKs `kid:base64(32)`; first encrypts |
| `PLATFORM_IP_ALLOWLIST` | | — | CIDRs for admin host (optional) |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_TLS_MODE`, `MAIL_FROM_ADDRESS`, `MAIL_FROM_NAME` | ✔ prod | Mailpit in dev | Platform SMTP: SES `email-smtp.ap-south-1.amazonaws.com:587` or Brevo `smtp-relay.brevo.com:587` (Q-13). `MAIL_FROM_NAME` defaults to `PLATFORM_NAME` |
| `TENANT_SMTP_ALLOW_PORTS` | | `465,587` | |
| `TENANT_MAIL_DAILY_CAP_DEFAULT` | | `300` | Plan can override |
| `STORAGE_DRIVER` | | `fs` | `fs` or `s3` |
| `STORAGE_FS_ROOT` / `S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_REGION` | per driver | — | |
| `RAZORPAY_KEY_ID`, `RAZORPAY_KEY_SECRET`, `RAZORPAY_WEBHOOK_SECRET` | ✔ when `BILLING_ENABLED` | — | Never logged/returned |
| `BILLING_ENABLED` | | `false` in dev | |
| `GRACE_DAYS`, `READ_ONLY_DAYS`, `RETENTION_DAYS` | | `7`, `30`, `90` | Q-14 decided (trial length 14 d is `plans.trial_days`) |
| `EXPORT_AGE_RECIPIENT` | ✔ when exports run | — | age public key for tenant exports (S7) |
| `EXPORT_AGE_IDENTITY` | posctl restore only | — | age private key; never given to the API process |
| `EXPORT_MANIFEST_KEY` | ✔ when exports run | — | HMAC key for export manifests |
| `MAX_UNPAGED_ROWS` | | `5000` | Q-4 decided |
| `LOG_LEVEL`, `LOG_FORMAT` | | `info`, `json` | |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | | — | Tracing off if unset |
| `BOOTSTRAP_ADMIN_EMAIL`, `BOOTSTRAP_ADMIN_PASSWORD` | posctl only | — | One-time; refuses if a platform user exists |

Not configurable on purpose: language (English only), self-signup (not built, Q-5), desktop mode (dropped, Q-1).

Fail-fast rules: missing required var → exit 1 with the variable name (never its value). Weak secrets: length < 32 bytes, or a known placeholder (`your_jwt_secret_here`, `changeme`), or a value whose SHA-256 is on the deny list (the leaked secret from P0-1; only its hash is in the code) → exit 1. `APP_ENV=production` additionally requires `PUBLIC_SCHEME=https`, non-empty `TRUSTED_PROXY_CIDRS`, DB TLS, and S3 storage.

---

## 7. Super Admin (platform console)

### 7.1 Roles

| Platform permission | super_admin | billing_admin | support |
|---|---|---|---|
| tenants:view (status, plan, usage, last activity, schema version, storage) | ✔ | ✔ | ✔ |
| tenants:create / edit / slug | ✔ | — | — |
| tenants:suspend / reactivate (step-up) | ✔ | ✔ (billing reasons) | — |
| tenants:delete / restore (step-up) | ✔ | — | — |
| tenants:impersonate read-only (step-up) | ✔ | — | ✔ |
| tenants:impersonate read-write (step-up) | ✔ | — | — |
| tenants:export / restore-test (step-up) | ✔ | — | — |
| plans:manage | ✔ | ✔ | — |
| billing:view / manage / reconcile | ✔ | ✔ | view |
| notifications:send | ✔ | ✔ (billing kinds) | — |
| migrations:view / run (step-up) | ✔ / ✔ | — | view |
| audit:view | ✔ | own actions | own actions |
| platform_users:manage | ✔ | — | — |

Rationale: support must see and diagnose but not change money or data. Billing staff must fix subscriptions but not read tenant business data. Only super_admin can destroy data. A fourth `auditor` role is **deferred**.

### 7.2 Security
- Separate table `platform_users`, separate cookie, separate JWT `aud=platform`, `iss=admin.<BASE_DOMAIN>`, separate signing key (`kid` from a distinct `PLATFORM_JWT_KEYS`, S3 **included**: a tenant-key leak cannot mint platform tokens). Platform routes are mounted only on the admin host, and tenant tokens are rejected there by `aud`.
- Password ≥ 14 chars + blocklist; argon2id `m=64 MiB, t=3, p=2`.
- **MFA mandatory**: TOTP (RFC 6238, SHA-1, 6 digits, 30 s, ±1 step, replay guard: last used step stored). Secret encrypted with the envelope keys. 10 single-use recovery codes (SHA-256 hashed, shown once). WebAuthn/passkeys (S6) **deferred**.
- **Strict CSP on the admin host** from step 4 (S13): no `'unsafe-inline'`, tenant text rendered as plain text only (OPERATIONS §7, THREAT_MODEL §3.6).
- Session: access 5 min, refresh 8 h absolute, 30 min idle; refresh rotated with reuse detection.
- Step-up: `POST /auth/step-up` with TOTP → session gets `stepup_until = now+5m`. Required for suspend, delete, restore, slug change, impersonate, export, migrations run, platform user changes.
- Optional per-user and global IP allowlists; rate limits 5/min/IP on login and 5 attempts per MFA challenge; lockout after 10 failures (manual unlock by another super_admin).
- Every action → `platform_audit_log(actor, action, target_tenant, before/after (redacted), ip, ua, request_id, at)`. Append-only (no UPDATE/DELETE grant).
- The console never shows tenant password hashes, SMTP passwords, Razorpay secrets or refresh tokens. The API has no field for them.

### 7.3 Bootstrap
`posctl bootstrap-superadmin` (reads `BOOTSTRAP_ADMIN_EMAIL`/`BOOTSTRAP_ADMIN_PASSWORD` from env or prompts on a TTY): refuses if any platform user exists, validates the policy, creates the user with `must_change_password = true` and `mfa_enrolled = false`, writes an audit row. First login forces password change → TOTP enrolment → recovery codes. No default password exists in code.

### 7.4 Impersonation
TENANCY §9. 15 min max, written reason, read-only default (read-write = super_admin only + step-up), visible banner in the tenant UI, tenant audit log entries, revocable instantly.

---

## 8. Delivery order (Phase 2) and commits

Each step is one or more small commits with tests. Nothing merges without `go vet`, `staticcheck`, `gosec`, `govulncheck`, the `constsql` analyzer, gitleaks, unit tests with `goleak`, and (from step 2 on) integration tests on testcontainers Postgres 18.

**Prerequisite:** Go 1.27.2 and Docker are installed on the dev machine (Q-9; not installed as of 2026-10-15).

| # | Step | Key outputs |
|---|---|---|
| 1 | Foundation | `backend-go/` module, config (fail-fast, leaked-secret hash deny list), slog + redaction, errors, httpx (strict JSON, body caps, timeouts, secure headers incl. the strict admin-host CSP, request id, recover), metrics, `/healthz` `/readyz`, graceful shutdown, Dockerfile (distroless nonroot), Makefile, CI (golangci-lint + `constsql`, gosec, govulncheck, gitleaks, goleak), Dependabot, docker-compose (Postgres 18, Caddy `tls internal`, MinIO, Mailpit). Untrack `frontend/.env` (O-8) |
| 2 | Platform schema + registry | `extensions` + platform migrations (incl. `pg_stat_statements`), `tenancy` (SchemaName, host resolver, cache, Sessions with post-condition check, roles), change listener (TENANCY §5.5), `posctl db preflight`, **search_path and extensions tests** (TENANCY §5.4) |
| 3 | Migration runner + provisioning | goose two tracks over startup-parameter connections (TENANCY §8, incl. the `NO TRANSACTION` + reconnect tests), dry run, resume, drift, default privileges + grant re-apply, tenant-bound `mark_outbox_pending()`, provisioning orchestrator + compensation |
| 4 | Super Admin | platform auth with separate `PLATFORM_JWT_KEYS` (S3), TOTP, recovery codes, step-up, tenants admin, audit, bootstrap CLI |
| 5 | Tenant auth + RBAC | login, refresh rotation, lockout, invite/reset, RBAC middleware, anti-escalation, users/roles endpoints, tenant gate (one status rule incl. the billing exemption), `GET /api/public/tenant-info`, **tenant-isolation suite** |
| — | **Stop for review** | After step 5, before step 6 (see gate below) |
| 6 | Master data | stores (+ showcase admin), categories, sub-categories, brands, units, suppliers, customers, warehouses (Q-12), files/images |
| 7 | Products/inventory | products, variants, barcode, POS list, POS locks, inventory, stock ledger (S1), **product CSV import** (Q-12) |
| 8 | Sales/POS | pricing engine (S4 GST split), checkout with row locks, edit (original biller kept, `updated_by`), void, invoices, idempotency keys (S2) |
| 9 | Receipts + email | **PDF spike first** (EMAIL_AND_RECEIPTS §5.6, O-5), then settings, tenant SMTP (encrypted, SSRF-safe), outbox, dispatch, workers, templates, PDF (maroto or the spike's fallback), resend/download |
| 10 | Returns | cumulative checks |
| 11 | Purchases | variant/store lines, approve, receive (`Received` terminal, Q-16) |
| 12 | Stock adjustments | atomic, ledger |
| 13 | Reports/exports | sales/purchase reports, Excel/PDF export, scoped |
| 14 | Dashboard | filtered aggregates |
| 15 | Public shop | host → tenant → store, published only, rate limits, caching headers |
| 16 | Billing | after the Razorpay test-mode checks (BILLING_DESIGN §10): BillingProvider/Razorpay, webhook, lifecycle, limits, notifications |
| 18 | Per-tenant export/restore | streamed checksums, `age` encryption (S7), restore into scratch, compare, session revocation |
| 19 | Frontend changes | API_CONTRACT §6 list; admin console (strict CSP) |

Steps 17 (legacy import: old data is demo data) and 20 (Electron: web only) were **dropped**. Numbers are kept stable so earlier references stay valid.

**Gate after step 5.** Phase 2 first builds only steps 1–5, then stops for review. Exit criteria:
1. CI green with every gate above, on Linux runners.
2. **search_path suite** passes: TENANCY §5.1 (pool size 1 and 2, concurrent tenants, released-connection checks, panic/cancel paths), §5.2 (version skew on one connection, the documented `CacheStatement` failure), §5.4 (extensions tests 1–5), §8 (`NO TRANSACTION` and reconnect tests).
3. **Tenant-isolation suite** passes with 3+ tenants: every step-5 endpoint with swapped host/token (`401`), `has_table_privilege` matrix for every role, a tenant role cannot read `platform` or another `t_*`, forged `X-Forwarded-Host`, generic 404 for unknown/deleted/provisioning hosts (including `/api/public/tenant-info`).
4. Provisioning fault injection at every step leaves no orphan schema, role or tenant row.
5. A short report of what was built, test counts, and anything that differed from this design.

---

## 9. Performance plan

| Hot path | Plan | Target (p95, 50 tenants, 1 node, 4 vCPU) |
|---|---|---|
| POS checkout (`POST /api/sales`, 5 lines) | 1 tx, ~7 round trips (role/search_path set in 1, lock+read 1, update 1, insert sale 1, insert items 1 via `COPY`/multi-row, ledger 1, outbox 1), deterministic lock order | < 60 ms |
| POS product list (1k SKUs) | 3 queries (products, variants, store inventory) joined in Go, no N+1, no images inline (URLs), `ETag` + gzip | < 120 ms |
| Product list (paginated) | Keyset-friendly index, `pg_trgm` search | < 80 ms |
| Dashboard | Single aggregate SQL per card group with filters, `date_trunc` in tenant TZ, 30 s cache per (tenant, filters) | < 150 ms |
| Receipt PDF | Rendered by a worker after commit, cached file; download serves the stored file | render < 150 ms, download < 30 ms |
| Public shop list | Published-only join, `limit ≤ 48`, `Cache-Control: public, max-age=60` + ETag | < 80 ms |

Measured in Phase 3 with k6 (50+ tenants, 500-schema smoke test), pprof, `pg_stat_statements`, pool-wait metrics. Before/after numbers come from the old Node backend running the same k6 scripts.

Other levers: gzip/brotli at Caddy, HTTP/2, `pgx` batch for multi-statement steps, `COPY` for bulk inserts (CSV import, restore), `GOMEMLIMIT` set from the container limit, connection-count budget (pool max ≤ 40 per node). PgBouncer is **deferred**; transaction mode works with this design if more nodes are added later, with `LISTEN` kept on its own direct connection.

---

## 10. Suggestions (not requested)

| # | SUGGESTION | Trade-off | Decision (2026-10-15) |
|---|---|---|---|
| S1 | **Stock ledger** (`stock_movements`) for every qty change, plus a nightly reconciliation job (sum of movements = inventory) | ~1 extra insert per line; makes stock disputes provable | **Phase 2** (step 7) |
| S2 | **Idempotency-Key** on `POST /api/sales` (and returns, purchases) | Small table; prevents double sales on double-click/retry over flaky shop Wi-Fi | **Phase 2** (step 8) |
| S3 | Separate platform JWT signing keys | One more secret to manage | **Phase 2** (step 4) |
| S4 | `hsn_code` on products + proper GST tax-invoice fields (CGST/SGST/IGST split) | Small schema/UI work; legally relevant for Indian B2B invoices | **Phase 2** (steps 7–9) |
| S5 | Same-origin SPA served by the Go binary | Removes CORS/cookie complexity; static assets served by Go (cache headers + gzip at Caddy are enough) | **Phase 2** (step 1) |
| S6 | Passkeys (WebAuthn) for Super Admin | Extra flow; phishing-resistant | Deferred |
| S7 | Per-tenant export encrypted with `age` (filippo.io/age) | One dependency; exports safe at rest outside the DB | **Phase 2** (step 18) |
| S8 | Store-scoped users (`user_stores`) so billers see only their store | Schema + UI; PRD roadmap item | Deferred |
| S9 | Read replica for reports/dashboard later (READ ONLY tx already in place) | Replica lag on dashboards | Deferred |
| S10 | "Find my store" on the apex host: email in → email out with the tenant URL(s) | Needs platform-level email→tenant index (hash of email only) | Deferred |
| S11 | Have-I-Been-Pwned k-anonymity check on password set | External call (optional, fail-open) | Deferred |
| S12 | Server-side pagination migration for all FE lists | FE work; removes the 5,000-row cap | Deferred |
| S13 | Strict CSP (no `'unsafe-inline'`), later nonces and Trusted Types | Requires checking MUI/Emotion inline styles | **Split:** strict CSP on the **admin host now** (step 4); nonce work for the **tenant app deferred** |
| S14 | Partition `audit_log` and `stock_movements` by month once they exceed ~10M rows | Not needed at launch | Deferred |

Tools: Caddy, Mailpit, MinIO, `pg_stat_statements`, Dependabot and goleak are **in Phase 2**; PgBouncer, Turnstile and the `auditor` platform role are **deferred** (LIBRARIES §4).

---

## 11. Risks

| Risk | Impact | Mitigation |
|---|---|---|
| Leaked JWT secret/DB password (P0-1) is live in any deployment | Full takeover | Owner rotating now (Q-7); new backend rejects the leaked value at startup; history rewrite after confirmation |
| Catalog bloat beyond ~1–5k tenants | Slow dumps/migrations, memory | Ceiling monitoring, sharding via `db_cluster`, exit to RLS for small tenants |
| `Exec` mode overhead | Slightly higher latency | Benchmarks; DescribeExec/salted statement names as fallback |
| Contract drift breaking the SPA | Broken UI | Contract tests generated from recorded old-backend responses (golden JSON) for every route |
| Frontend still keeps tokens in storage during transition | XSS exposure | FE change ships with the backend (step 19) |
| No Go/Docker on the dev machine (**confirmed 2026-10-15**) | Can't build/test Phase 2 | Install list handed over (Q-9); Phase 2 waits for it |
| Gmail SMTP limits for tenants | Receipt failures | Outbox retries, platform fallback, status indicator |
| Razorpay event ordering/duplicates | Wrong subscription state | Event-id dedupe, `created_at` ordering, reconcile job |
| A halted Razorpay subscription may not be revivable by paying a link | Suspended tenant can't pay from the email | Test-mode check R1 before step 16; both code paths designed (BILLING_DESIGN §10) |
| maroto cannot shape Devanagari | Broken customer names on receipts | Early spike; Gotenberg HTML-to-PDF fallback (EMAIL_AND_RECEIPTS §5.6) |
| Managed Postgres restricts `CREATEROLE`, role counts or startup parameters | Design doesn't run on that provider | Preflight M1–M14 before a managed instance is used (OPERATIONS §8) |
| Injected SQL switching role inside a tenant tx | Cross-tenant read | No injectable SQL (sqlc + `constsql`), `Exec` mode, commit tripwire; accepted residual risk (THREAT_MODEL §4) |

---

## 12. Decisions (resolved 2026-10-15)

| # | Decision | Applied in |
|---|---|---|
| Q-1 | **Web application only.** Desktop/Electron dropped: no step 20, no `POSD_MODE=desktop`, no `DESKTOP_TOKEN`, no desktop section in OPERATIONS, no assumption A6, no desktop security control (P0-11, THREAT_MODEL "Electron LAN exposure") | All docs |
| Q-2 | Cart discount allocated pro-rata **before tax** | §4.3, API_CONTRACT C-04 |
| Q-3 | Price override allowed with `sales:price_override` (audited, `catalog_price` kept on the line) | §4.3, §5, C-04 |
| Q-4 | Bare-array lists kept, capped at 5,000 rows | API_CONTRACT §1 |
| Q-5 | Public self-signup **off**; the Super Admin creates tenants. Not built | TENANCY §6, §7.1, §6 config |
| Q-6 | Contract changes C-01…C-21 **approved** | API_CONTRACT §4 |
| Q-7 | Owner rotates the JWT secret and DB password. History rewrite: exact commands handed over; nothing destructive runs until the owner confirms | P0-1 |
| Q-8 | Code in `backend-go/` next to `backend/` | A3, §3.1 |
| Q-9 | Checked 2026-10-15: **Go and Docker are not installed**. Install list handed over; Phase 2 waits | §8 prerequisite, LIBRARIES |
| Q-10 | Sale edit keeps the original biller; `updated_by` recorded | C-04, §5 |
| Q-11 | Suspended tenant blocked from **all** logins; pays through the Razorpay link sent by email | TENANCY §5, BILLING §3, §5, §10 |
| Q-12 | Warehouse API mounted (step 6); product CSV import built (step 7) | API_CONTRACT §2.11, §5.1.1 |
| Q-13 | Platform mail via SES (ap-south-1) or Brevo over SMTP; `PLATFORM_NAME` from env, no hard-coded name | EMAIL §1–3, §6 config |
| Q-14 | Trial 14 d, grace 7 d, read-only 30 d, deletion after 90 d; plans and prices set later by the owner | BILLING §3, §6 config |
| Q-15 | Single VM with docker-compose, managed Postgres with PITR, S3-compatible storage | OPERATIONS |
| Q-16 | Purchase `Received` is terminal | C-09, step 11 |
| Language | English only everywhere; no language setting, translation files or i18n libraries; currency and number format from tenant settings | API_CONTRACT §1, EMAIL, LIBRARIES, §5 `settings.locale` |
| Data | Old data is demo data: no legacy import (step 17 dropped); empty start, seeds only roles, permissions and settings | TENANCY §6, §11 |
| Extras | Phase 2: S1, S2, S3, S4, S5, S7, Caddy, Mailpit, MinIO, `pg_stat_statements`, Dependabot, goleak. Deferred: S6, S8–S12, S14, PgBouncer, Turnstile, `auditor` role. S13: strict CSP on the admin host now, tenant-app nonces deferred | §10, LIBRARIES §4, OPERATIONS §7 |

### 12.1 Review doc fixes (where each one landed)

| # | Fix | Where |
|---|---|---|
| 1 | One tenant-status rule; `cancelled` keeps access until period end; billing routes exempt from `read_only` | TENANCY §5 step 5, BILLING §3, C-21 |
| 2 | `GET /api/public/tenant-info` | API_CONTRACT §5.1 |
| 3 | Migration `search_path` as a connection startup parameter; CI-rule carve-out; works with `NO TRANSACTION` | TENANCY §5, §8 |
| 4 | `mark_outbox_pending()` no longer uses `current_schema()`; see open question 1 on `current_user` | EMAIL §4.2 |
| 5 | Dedicated direct `LISTEN` connection, cache flush on reconnect, TTL fallback | TENANCY §5.5 |
| 6 | `search_path` = `"t_<id>", extensions`; sketch and text agree; tests | TENANCY §5, §5.4 |
| 7 | Streamed checksums, `refresh_tokens`/`auth_tokens` excluded, sessions revoked after restore | OPERATIONS §6.2–6.3 |
| 8 | Devanagari PDF spike + HTML-to-PDF fallback | EMAIL §5.6 |
| 9 | Admin-console XSS and injected `SET ROLE` as residual risks; `constsql` lint rule | THREAT_MODEL §3.2, §3.6, §4 |
| 10 | Razorpay test-mode checklist (halted revival = R1) | BILLING §10 |
| 11 | Managed-Postgres preflight (`CREATEROLE`, many roles, PG16 grants, startup params, extensions) | OPERATIONS §8 |

### 12.2 Round-2 questions (answered, see 12.3)

| # | Question | Recommended default |
|---|---|---|
| O-1 | `mark_outbox_pending()` and `current_user`: inside a `SECURITY DEFINER` function `current_user` is the owner (`pos_owner`), so it cannot identify the tenant. Accept the per-tenant function with the validated tenant ID written in at provisioning (EMAIL §4.2)? | **Yes** |
| O-2 | "Seed only roles, permissions and settings" leaves no store and no customer, but `sales.customer_id` is required and POS needs a store. Also seed a "Walk-in Customer" and one default store (functional defaults, not demo data)? | **Yes, seed both** |
| O-3 | Provisioning (step 3) creates a trial subscription, but plans are yours to define and billing is step 16. Seed one non-public internal `trial` plan (price 0, 14 days, generous limits) that you replace later? | **Yes** |
| O-4 | Retention clock: day 0 = first day `read_only` **or** `suspended`; `pending_deletion` at day 30; schema dropped at day 90 counted from day 0 (BILLING §3)? | **Yes, as written** |
| O-5 | Run the PDF spike inside the first batch (steps 1–5), or at the start of step 9? | **First batch** (about half a day; the result may add a container) |
| O-6 | Admin console under the strict CSP: build it **without** runtime CSS-in-JS (plain React + CSS Modules), or with MUI + per-request nonces? | **Without MUI** (no nonce plumbing needed) |
| O-7 | Managed Postgres provider (needed for the preflight)? | **AWS RDS for PostgreSQL 18, ap-south-1**, VM and S3 in the same region (matches SES Mumbai) |
| O-8 | `frontend/.env` is tracked in git despite `.gitignore` (contains only `VITE_API_URL`, not secret). Untrack it in the step 1 commit (`git rm --cached`, no history rewrite)? | **Yes** |
| O-9 | "Security item 16" was taken to mean the desktop LAN control (P0-11 + THREAT_MODEL "Electron LAN exposure"), both removed. Correct? | **Yes** |

### 12.3 Round-2 answers (2026-10-15)

| # | Answer | Applied in |
|---|---|---|
| O-1 | Yes. Outbox function created inside each tenant schema, own `search_path = pg_catalog, platform`, `EXECUTE` revoked from `PUBLIC`, granted only to that tenant's role | EMAIL §4.2 |
| O-2 | Yes. Seed "Walk-in Customer" and one default store | TENANCY §6 step 6 |
| O-3 | Yes. Hidden internal `trial` plan | TENANCY §6 step 8 |
| O-4 | Yes, but **no automatic schema drop in v1**: warning emails, verified final export, Super Admin step-up confirmation | BILLING §3, OPERATIONS §6.4 |
| O-5 | PDF spike at the **start of step 9** | §8, EMAIL §5.6 |
| O-6 | Admin console built **without MUI** | API_CONTRACT §6 |
| O-7 | Preflight target AWS RDS for PostgreSQL 18, ap-south-1 (PG 17 acceptable if 18 is unavailable) | OPERATIONS §8 M1 |
| O-8 | Yes. Untrack `frontend/.env` in step 1 | §8 step 1 |
| O-9 | Yes. One-line note kept: P0-11 not applicable (web only) | §2.1 |
| Files | `/files/t/` serves only tenant logo, store logo/banner and published product images. Customer and user images stay behind login. Receipts and exports are never served there | API_CONTRACT §5.1, §5 `files.purpose`, THREAT_MODEL §3.9 |
