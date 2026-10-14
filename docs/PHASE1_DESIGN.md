# Phase 1 — Analysis & Design: Go Backend Rewrite (Multi-Tenant SaaS)

| | |
|---|---|
| Status | **Awaiting approval.** No application code written. |
| Baseline | Commit `94aeeb8`. Every file in `backend/src/**` was read; frontend auth, API client and every `api.*`/`fetch` call site were read. |
| Companion docs | [API_CONTRACT](./API_CONTRACT.md) · [TENANCY_DESIGN](./TENANCY_DESIGN.md) · [THREAT_MODEL](./THREAT_MODEL.md) · [EMAIL_AND_RECEIPTS](./EMAIL_AND_RECEIPTS.md) · [BILLING_DESIGN](./BILLING_DESIGN.md) · [OPERATIONS](./OPERATIONS.md) · [LIBRARIES](./LIBRARIES.md) |

---

## 1. Summary

- Rewrite **only the backend** in Go. Keep the React SPA, PostgreSQL, and the existing routes and JSON shapes. 21 deliberate contract changes are listed in API_CONTRACT §4; most need no frontend change.
- **Schema-per-tenant** in one database, one shared pgx pool, every request in a transaction with `SET LOCAL ROLE t_<id>_rw` + `set_config('search_path', '"t_<id>"', true)`, and pgx in **`QueryExecModeExec`** so no cached statement can bind to the wrong schema. Per-tenant DB roles make a wrong-schema bug a *permission error*, not a leak.
- Tenant from the **subdomain**, which must match the token's tenant claim. Same-origin SPA+API, so `__Host-` cookies and no CORS in production.
- Platform **Super Admin** on `admin.<BASE_DOMAIN>`: separate identity, mandatory TOTP, step-up, audited, time-limited impersonation.
- Email via a **transactional outbox** written in the sale transaction. Workers fan out across tenant schemas safely. Receipts as PDF (maroto) + HTML email. Tenant SMTP (encrypted, SSRF-hardened) or platform fallback.
- **Razorpay** subscriptions behind a `BillingProvider`: verified, idempotent webhooks; lifecycle trial → active → past_due → grace → suspended → read_only → deleted.
- Found **11 P0 issues** in the old backend, including two the brief did not list: **client-controlled prices** (a biller can sell anything for ₹0) and a **committed JWT secret + DB password in the public git history**.

### 1.1 Assumptions (explicit)

| # | Assumption | If wrong |
|---|---|---|
| A1 | Production Postgres is **≥ 16** (we target 18) | Per-tenant role design needs `GRANT … WITH INHERIT FALSE, SET TRUE` (PG16). On PG 14/15 roles still work with `NOINHERIT` on `pos_app` |
| A2 | Early scale is ≤ 1,000 tenants per DB | See TENANCY §1 for ceiling and triggers |
| A3 | The Go backend lives in a new folder `backend-go/` next to the old `backend/` until cutover | Q-8 |
| A4 | Quantities stay integers (contract) | Weight-based selling needs `numeric` qty later (expand migration) |
| A5 | One currency per tenant; GST-style tax config by default but country-agnostic | Multi-currency per tenant is out of scope |
| A6 | Desktop (Electron) is a single local tenant and needs a local Postgres | Q-1 |
| A7 | Go 1.27.2 (current stable, checked on go.dev) instead of the "1.23+" floor | 1.23 is out of security support; we pin `go 1.27` + `toolchain go1.27.2` and follow patch releases |
| A8 | Hosting: one VM / small cluster with docker-compose, managed Postgres with PITR | Q-15 |

### 1.2 Challenges to the brief and to `SAAS_ARCHITECTURE.md`

| Topic | Brief / existing doc | Recommendation | Why |
|---|---|---|---|
| Tenancy | SAAS_ARCHITECTURE: shared tables + `tenant_id` + Sequelize global scope | Schema-per-tenant (brief) ✔ | A forgotten `WHERE` in a global scope is the classic leak. Per-schema + per-role isolation fails closed |
| `search_path` | `"t_xxx", platform` | `"t_xxx"` only | Name collisions (`email_outbox`, `audit_log` exist in both) would silently resolve to platform tables, and the tenant role would need platform privileges |
| `tenants.db_dsn` | DSN column | `db_cluster` key → DSN in the secret store | DSNs contain passwords. Secrets don't belong in DB rows |
| Migrations | golang-migrate | **goose v3** (Provider API) | Per-migration transactions, `NO TRANSACTION` annotation, Go migrations for backfills, no global "dirty" flag that blocks every later run, easy per-schema table. SQL files stay portable (LIBRARIES) |
| TOTP lib | pquerna/otp | stdlib RFC 6238 implementation (~80 lines + RFC test vectors) | pquerna/otp: last release Dec 2024 (> 12 months), fails the library rule |
| CORS | own tenant origin + admin origin | Same-origin, no CORS in prod | Admin console never calls tenant APIs (code-exchange impersonation) |
| Suspended tenants | SAAS_ARCHITECTURE: "allow login + billing page"; brief: "blocked at login" | Blocked at login (brief), with payment by emailed Razorpay link | Q-11 |
| Desktop | SAAS_ARCHITECTURE: Electron = thin client of SaaS | Brief: sidecar + single local tenant | Q-1. Both are possible; offline POS favours the sidecar |
| Token lifetime | SAAS_ARCHITECTURE: 1 h access; old code: 30 d | 10 min access + rotated refresh with reuse detection | S-req 26 |
| Billing | SAAS_ARCHITECTURE: Stripe | Razorpay (brief) behind `BillingProvider` | INR/UPI market |
| POS lock | Brief: "in-memory POS lock" | **Correction:** the old lock is DB columns on `products` (`is_locked/locked_at/locked_by`), but it is racy (read-then-write), global across stores, and not enforced at checkout | Replace with `pos_locks` (store, product) + row locks at checkout |

---

## 2. Gap list (code-verified, ranked)

Severity: **P0** = exploitable or loses money/stock/data now · **P1** = serious security/correctness flaw · **P2** = bug, debt or doc mismatch. "PRD" = also in PRD §9.

### 2.1 P0

| # | Area | Finding (file:line) | Fix in Go backend |
|---|---|---|---|
| P0-1 | Secrets | **Real `backend/.env` committed** in `63b1c9b`/`9cff503` (deleted in `94aeeb8`, still in history, **pushed to `github.com/tejas1112001/POS_inventory`**): `JWT_SECRET=***REMOVED***…`, `DB_PASSWORD=***REMOVED***`. Anyone can forge admin JWTs for any deployment that used it. | **Rotate now** (independent of the rewrite). New secrets via env only, fail-fast on weak values. History rewrite needs your OK (Q-7). gitleaks in CI. |
| P0-2 | Auth | Public `/api/auth/signup` accepts `role`/`role_id` → anyone registers as admin; `...rest` mass-assigned into `User.create` (`auth.js:72-100`). PRD G1. | Endpoint removed on tenant hosts; owner only via provisioning; `POST /api/users` guarded. |
| P0-3 | Sales | **Client-controlled prices/totals**: `unitPrice`, line `total`, `discount`, `tax`, `shipping` taken from the request (`sales.js:96-110,155`). A biller can sell any item for 0 or give unlimited discount. | Server price/tax engine; `409 PRICE_CHANGED`; discount permission + cap. |
| P0-4 | Stock | Oversell race: stock read without `FOR UPDATE`, unconditional `decrement` (`sales.js:114-124,161-167`); same in sale edit and stock-adjustment subtraction (`stockAdjustments.js:72-84`); no `CHECK (qty >= 0)`. PRD G4. | `SELECT … FOR UPDATE` in `variant_id` order + `UPDATE … WHERE qty >= n`; DB `CHECK`; concurrency tests. |
| P0-5 | Purchases | Status toggling **re-stocks every time**: `Received → Pending → Received` adds stock again (`purchases.js:118`). Later receive uses first variant + **store 1** default (`:119-128`). Per-line variant/store from creation are never stored (`purchase_items` lacks the columns). PRD G6. | `Received` terminal; `purchase_items.variant_id/store_id NOT NULL`; `purchases:approve`. |
| P0-6 | Returns | Return qty only checked vs this return (repeat returns unlimited); **negative qty accepted** → stock *decremented* and negative refund; refund `unitPrice` from client (can exceed sold price); store/customer not checked vs sale (`salesReturns.js:96-143`). PRD G7 (partially). | Cumulative check under row lock, qty > 0, sold price, matching store/customer. |
| P0-7 | Sessions | Inactive/deleted users can log in; `/refresh` re-issues 30-day tokens from any valid token **without a status check** → endless sessions (`auth.js:47-134`). Permissions come from the token's `roleId`, so **demoting an admin has no effect until their token expires** (up to 30 days) (`middleware/auth.js:27-39`). PRD G8 (partially). | Short access tokens, rotated refresh tokens, per-request auth-state check, `token_version`. |
| P0-8 | Mass assignment | `Sale.create({...saleData})`, `originalSale.update({...saleData})`, `SaleReturn.create({...saleReturnData})`, `User.create({...rest})`: client can set `total`, `paid`, `due`, `status`, `createdAt`, `id`… (`sales.js:127,247`, `salesReturns.js:120`). | Explicit DTOs, strict decoding, allow-listed fields only. |
| P0-9 | Products | `PUT /api/products/:id` updates `findByPk(v.id)` **without checking `product_id`** → any variant (with its stock) can be moved into another product; omitted variants are hard-deleted **with their stock** and no record (`products.js:344-391`). | Ownership check, soft delete, refuse when stock > 0, ledger. |
| P0-10 | Escalation | Any `users:edit` holder can assign the admin role to themselves (`billers.js:61`, `roles.js:171-194`); any `roles:edit` holder can grant their own role every permission and edit system roles (`roles.js:104-143`). | Subset rule, no self-change, immutable owner/admin, audit. |
| P0-11 | Desktop/LAN | Server binds `0.0.0.0` by default (`index.js:114`) and allows requests **without `Origin`**; in Electron mode the full API (with P0-2 signup) is exposed to the local network. | Desktop sidecar binds `127.0.0.1:<random>` + per-launch token. |

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
| P2-1 | `/api/warehouses` not mounted; WarehouseModal → 404 (PRD G11) |
| P2-2 | `ImportProduct` posts to `/products`; no backend (PRD G12) |
| P2-3 | Stock adjustments have no UI (PRD G13) |
| P2-4 | `PublicShopPage` hard-codes `http://localhost:3000` (PRD G14) |
| P2-5 | Dashboard chart: earliest 100 sales, month names merge years (PRD G15); `totalUnpaid` ignores Partial |
| P2-6 | Docs mismatch: README says MongoDB; PERMISSIONS.md lists unseeded actions (PRD G17); `utils/mongoose.js` shim |
| P2-7 | Electron scripts/dependency missing; preload IPC channels without handlers; `executeScript` interpolates `err.message` into JS (PRD G18) |
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
│  │                   └─ apex router: landing/signup (optional)                           │
│  ├─ middleware: request-id → recover → slog → secure headers → body limit → timeout     │
│  │              → rate limit → host/tenant resolve → auth → auth-state → gate → RBAC    │
│  ├─ modules: handler → service → repository (sqlc)                                      │
│  ├─ tenancy.Sessions (InTenantTx / InPlatformTx)  ── pgxpool (Exec mode) ──┐            │
│  ├─ River client (jobs: outbox fan-out, receipts, notifications, billing, migrations,  │
│  │                 exports, provisioning)  ── jobs pool ──────────────────┤            │
│  └─ metrics listener :9090 (/metrics, /internal/*)                         │            │
└─────────────────────────────────────────────────────────────────────────────┼───────────┘
                                                                              ▼
   PostgreSQL 18: platform │ river │ t_ab12cd34ef │ t_… (one per tenant)      Object storage (S3/MinIO/FS)
                                                                              SMTP (platform + per-tenant)
                                                                              Razorpay API + webhooks
```

The same binary runs API + workers (`POSD_MODE=all|api|worker`) so it can be split later without code changes. `posctl` is the operator CLI.

### 3.1 Folder structure

```
backend-go/
├─ cmd/
│  ├─ posd/            # HTTP API + workers (mode flag)
│  └─ posctl/          # migrate, bootstrap-superadmin, provision, import-legacy, export, restore, drift
├─ internal/
│  ├─ app/             # wiring, lifecycle, graceful shutdown
│  ├─ config/          # env parsing + validation (fail fast on missing/weak secrets)
│  ├─ httpx/           # router, middleware, JSON decode/encode, error mapping, pagination
│  ├─ obs/             # slog setup, Prometheus metrics, OTel (optional)
│  ├─ db/              # pool config (Exec mode, hooks), tx helpers, pg error mapping
│  ├─ tenancy/         # SchemaName type, host resolver, registry cache, Sessions, cluster resolver
│  ├─ migrate/         # two-track runner, dry run, drift fingerprint
│  ├─ provisioning/    # orchestrator + steps + compensation
│  ├─ platform/        # super admin auth/MFA, tenants admin, impersonation, announcements, platform audit
│  ├─ auth/            # tenant login, refresh rotation, password reset/invite, lockout
│  ├─ rbac/            # permission catalog, middleware, cached resolver, anti-escalation
│  ├─ audit/           # tenant audit writer
│  ├─ settings/        # tenant settings (currency, locale, tax, receipt template)
│  ├─ catalog/         # categories, sub-categories, brands, units, suppliers, warehouses
│  ├─ customers/
│  ├─ stores/          # stores + showcase admin
│  ├─ products/        # products, variants, barcode, import
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
│  ├─ security/        # argon2id, bcrypt legacy verify, TOTP, token hashing, password policy
│  └─ money/           # decimal helpers, currency exponents, rounding
├─ db/
│  ├─ migrations/{platform,tenant}/NNNN_name.sql
│  ├─ queries/{platform,tenant}/*.sql
│  └─ sqlc.yaml        # two packages: internal/db/platformdb, internal/db/tenantdb
├─ web/                # (optional) embedded SPA builds
├─ test/               # integration, isolation, search_path, migration, e2e helpers, k6 scripts
├─ deploy/             # Dockerfile, docker-compose.yml, Caddyfile(s), prometheus rules, grafana
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
- **Passwords**: argon2id (`m=46 MiB, t=1, p=1`, OWASP), PHC string format, constant-time compare, global semaphore limiting concurrent hashes (memory-DoS guard). Legacy bcrypt hashes verified then rehashed on login. Policy: ≥ 12 chars, ≤ 128, blocklist of common/breached passwords (embedded list), not equal to email/business name; no composition rules (NIST 800-63B).
- **Login**: `POST /api/auth/login` on the tenant host. Rate limits: 5/min per (IP, email), 20/min per IP, 100/min per tenant. Progressive lockout: after 5 failures `locked_until = now + 2^n min` (max 1 h). Dummy hash on unknown email (timing). Generic `401`.
- **Tokens**: access JWT HS256, 10 min, claims `iss=<host>`, `aud=tenant`, `tid`, `sub`, `tv` (token_version), `sid` (refresh family), `imp?`. Keys `JWT_KEYS="kid:base64,..."`, first signs, all verify (rotation). Refresh: 32 random bytes, SHA-256 stored in `refresh_tokens` with `family_id`, rotated on every use; reuse of a rotated token → revoke the whole family + audit event. Remember-me 30 d sliding, else 12 h absolute.
- **Revocation**: logout (family), password change/reset (all families + `token_version++`), role/status change (`token_version++` + all families), admin "sign out everywhere".

### 4.2 RBAC
- Permission catalog in code (`rbac.Catalog`) → synced to `platform.permission_catalog` and each tenant's `permissions` table by migration/seed (keeps integer `permissionIds` contract).
- Route declarations; startup self-check that every tenant route has a requirement or an explicit `rbac.Public`/`rbac.Authenticated` marker.
- Resolver: `perm:<tenant>:<role_id>` cache, TTL 60 s, invalidated via `NOTIFY role_changed` (multi-node safe; fixes the per-process cache).
- Object-level rules live in services: own-sales scope, store scope (future `user_stores`), receipts, returns.
- Anti-escalation: grantable set ⊆ actor's set; `owner`/`admin` immutable; no self role/status change; last owner protected.

### 4.3 Money & tax
- DB: `NUMERIC(18,4)` for unit prices/costs, `NUMERIC(18,2)`-equivalent rounding for line and document totals (stored as `NUMERIC(18,4)` rounded to the currency exponent, so 3-decimal currencies work). Go: `decimal.Decimal`; JSON as numbers.
- Currency exponent from ISO 4217 table (`INR`=2, `JPY`=0, `KWD`=3), tenant `settings.currency`.
- `pricing.Quote(lines, cartDiscount, settings)`: per line `gross = price × qty`; cart discount allocated pro-rata to lines (largest-remainder to the minor unit) **before tax** (Q-2); exclusive tax `tax = round(net × rate)`; inclusive tax `tax = net − net / (1 + rate)`, not added again; per-line rounding half-up; document totals = sums. GST split (CGST/SGST vs IGST) stored per line for receipts.
- Shipping not taxed by default (setting).

### 4.4 Stock safety
- Checkout: merge duplicate variant lines → `SELECT id, qty FROM inventories WHERE store_id=$1 AND variant_id = ANY($2) ORDER BY variant_id FOR UPDATE` (deterministic lock order → no deadlocks) → validate → `UPDATE … SET qty = qty - d.n FROM unnest(...) d WHERE … AND qty >= d.n` → rows-affected check → `stock_movements` rows. `CHECK (qty >= 0)` as the last line of defense.
- Sale edit = void old lines (restock) + apply new lines in the same transaction, locks taken over the union of variants in sorted order.
- `pos_locks(store_id, product_id, user_id, expires_at)`: `INSERT … ON CONFLICT (store_id, product_id) DO UPDATE … WHERE pos_locks.expires_at < now() OR pos_locks.user_id = EXCLUDED.user_id RETURNING …`. Atomic, per store, TTL 2 min, swept by a job. Advisory only (UX). Correctness comes from the row locks.

---

## 5. Tenant schema (DDL sketch)

Conventions: `bigint GENERATED ALWAYS AS IDENTITY` PKs (IDs preserved on import via `OVERRIDING SYSTEM VALUE`), `timestamptz`, `text` + `CHECK` (no ENUMs, see TENANCY §5.2), `citext` from schema `extensions`, `deleted_at` soft delete, FKs `ON DELETE RESTRICT`. Columns used by JSON keep their old names so the contract mapping stays trivial (`createdAt` is mapped in SQL `AS "createdAt"`; DB columns are snake_case).

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
  locale text NOT NULL DEFAULT 'en-IN', timezone text NOT NULL DEFAULT 'Asia/Kolkata',
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
  hsn_code text,                                          -- SUGGESTION (GST)
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

CREATE TABLE idempotency_keys (                           -- SUGGESTION: safe POS retries
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
  discount numeric(18,4) NOT NULL DEFAULT 0, subtotal numeric(18,4) NOT NULL,
  legacy_inferred boolean NOT NULL DEFAULT false
);

-- files, outbox, audit ---------------------------------------------------------
CREATE TABLE files (
  id uuid PRIMARY KEY, kind text NOT NULL CHECK (kind IN ('image','receipt','export')),
  content_type text NOT NULL, size_bytes bigint NOT NULL, sha256 bytea NOT NULL,
  storage_key text NOT NULL UNIQUE, created_by bigint REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(), deleted_at timestamptz
);
-- email_outbox: EMAIL_AND_RECEIPTS §4

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
| `POSD_MODE` | | `all` | `api`, `worker`, `all`, `desktop` |
| `HTTP_ADDR` | | `:8080` | Public listener |
| `METRICS_ADDR` | | `127.0.0.1:9090` | `/metrics`, `/internal/*`, never proxied |
| `BASE_DOMAIN` | ✔ | `localhost` in dev | No domain is hard-coded anywhere |
| `PUBLIC_SCHEME` | | `https` | `http` only allowed with `APP_ENV=development` |
| `PLATFORM_NAME` | ✔ | — | Display name in emails/console (no brand in code) |
| `APP_ENV` | ✔ | — | `development`, `test`, `production` (production enforces stricter checks) |
| `TRUSTED_PROXY_CIDRS` | ✔ prod | — | Only these may set `X-Forwarded-*` |
| `DATABASE_URL` | ✔ | — | `pos_app` login (sslmode=verify-full in prod) |
| `DATABASE_MIGRATOR_URL` | ✔ for posctl | — | `pos_migrator` |
| `DATABASE_JOBS_URL` | ✔ | — | `pos_jobs` |
| `DB_CLUSTERS` | | — | `name=dsn;...` for dedicated tenant DBs (secret) |
| `DB_POOL_MAX` / `DB_POOL_MIN` | | `4×CPU` / `2` | |
| `DB_STATEMENT_TIMEOUT` | | `5s` (reports `30s`) | Per tx via `set_config` |
| `JWT_KEYS` | ✔ | — | `kid:base64(≥32 bytes)` list; startup fails if any key < 32 bytes or looks like a placeholder |
| `ACCESS_TOKEN_TTL` / `PLATFORM_ACCESS_TOKEN_TTL` | | `10m` / `5m` | |
| `REFRESH_TTL_REMEMBER` / `REFRESH_TTL_SESSION` | | `720h` / `12h` | |
| `PLATFORM_SESSION_MAX` / `PLATFORM_IDLE_TIMEOUT` | | `8h` / `30m` | |
| `MASTER_KEYS` | ✔ | — | Envelope KEKs `kid:base64(32)`; first encrypts |
| `PLATFORM_IP_ALLOWLIST` | | — | CIDRs for admin host (optional) |
| `SELF_SIGNUP_ENABLED` | | `false` | |
| `CAPTCHA_PROVIDER` / `CAPTCHA_SECRET` | if signup | — | Turnstile/hCaptcha |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_TLS_MODE`, `MAIL_FROM_ADDRESS`, `MAIL_FROM_NAME` | ✔ prod | Mailpit in dev | Platform SMTP |
| `TENANT_SMTP_ALLOW_PORTS` | | `465,587` | |
| `TENANT_MAIL_DAILY_CAP_DEFAULT` | | `300` | Plan can override |
| `STORAGE_DRIVER` | | `fs` | `fs` or `s3` |
| `STORAGE_FS_ROOT` / `S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_REGION` | per driver | — | |
| `RAZORPAY_KEY_ID`, `RAZORPAY_KEY_SECRET`, `RAZORPAY_WEBHOOK_SECRET` | ✔ when `BILLING_ENABLED` | — | Never logged/returned |
| `BILLING_ENABLED` | | `false` in dev | |
| `GRACE_DAYS`, `READ_ONLY_DAYS`, `RETENTION_DAYS` | | `7`, `30`, `90` | Q-14 |
| `MAX_UNPAGED_ROWS` | | `5000` | |
| `LOG_LEVEL`, `LOG_FORMAT` | | `info`, `json` | |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | | — | Tracing off if unset |
| `BOOTSTRAP_ADMIN_EMAIL`, `BOOTSTRAP_ADMIN_PASSWORD` | posctl only | — | One-time; refuses if a platform user exists |
| `DESKTOP_TOKEN` | desktop | — | Per-launch, from Electron |

Fail-fast rules: missing required var → exit 1 with the variable name (never its value). Weak secrets: length < 32 bytes, or a known placeholder (`your_jwt_secret_here`, `changeme`, the leaked secret from P0-1) → exit 1. `APP_ENV=production` additionally requires `PUBLIC_SCHEME=https`, non-empty `TRUSTED_PROXY_CIDRS`, DB TLS, and S3 storage.

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

Rationale: support must see and diagnose but not change money or data. Billing staff must fix subscriptions but not read tenant business data. Only super_admin can destroy data. **SUGGESTION:** a fourth `auditor` role (read-only audit log + tenant list) for external reviews.

### 7.2 Security
- Separate table `platform_users`, separate cookie, separate JWT `aud=platform`, `iss=admin.<BASE_DOMAIN>`, separate signing key (`kid` from a distinct `PLATFORM_JWT_KEYS`, **SUGGESTION**: so a tenant-key leak cannot mint platform tokens). Platform routes are mounted only on the admin host, and tenant tokens are rejected there by `aud`.
- Password ≥ 14 chars + blocklist; argon2id `m=64 MiB, t=3, p=2`.
- **MFA mandatory**: TOTP (RFC 6238, SHA-1, 6 digits, 30 s, ±1 step, replay guard: last used step stored). Secret encrypted with the envelope keys. 10 single-use recovery codes (SHA-256 hashed, shown once). **SUGGESTION**: WebAuthn/passkeys (go-webauthn) as a phishing-resistant second factor in a later phase.
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

Each step is one or more small commits with tests. Nothing merges without `go vet`, `staticcheck`, `gosec`, `govulncheck`, unit tests, and (from step 2 on) integration tests on testcontainers Postgres.

| # | Step | Key outputs |
|---|---|---|
| 1 | Foundation | config, slog, errors, httpx (strict JSON, body caps, timeouts, secure headers, request id, recover), metrics, `/healthz` `/readyz`, graceful shutdown, Dockerfile (distroless nonroot), Makefile, CI |
| 2 | Platform schema + registry | platform migrations, `tenancy` (SchemaName, host resolver, cache, Sessions, roles), search_path safety tests |
| 3 | Migration runner + provisioning | goose two tracks, dry run, resume, drift, provisioning orchestrator + compensation |
| 4 | Super Admin | platform auth, TOTP, recovery codes, step-up, tenants admin, audit, bootstrap CLI |
| 5 | Tenant auth + RBAC | login, refresh rotation, lockout, invite/reset, RBAC middleware, anti-escalation, users/roles endpoints |
| 6 | Master data | stores (+ showcase admin), categories, sub-categories, brands, units, suppliers, customers, warehouses, files/images |
| 7 | Products/inventory | products, variants, barcode, POS list, POS locks, inventory, stock ledger |
| 8 | Sales/POS | pricing engine, checkout with row locks, edit, void, invoices, idempotency keys |
| 9 | Receipts + email | settings, tenant SMTP (encrypted, SSRF-safe), outbox, dispatch, workers, templates, PDF, resend/download |
| 10 | Returns | cumulative checks |
| 11 | Purchases | variant/store lines, approve, receive |
| 12 | Stock adjustments | atomic, ledger |
| 13 | Reports/exports | sales/purchase reports, Excel/PDF export, scoped |
| 14 | Dashboard | filtered aggregates |
| 15 | Public shop | host → tenant → store, published only, rate limits, caching headers |
| 16 | Billing | plans/subscriptions tables, BillingProvider/Razorpay, webhook, lifecycle, limits, notifications |
| 17 | Legacy import | `posctl import-legacy` + verification |
| 18 | Per-tenant export/restore | export, restore into scratch, compare |
| 19 | Frontend changes | API_CONTRACT §6 list; admin console |
| 20 | Electron sidecar | desktop mode |

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

Other levers: gzip/brotli at Caddy, HTTP/2, `pgx` batch for multi-statement steps, `COPY` for bulk inserts (import, ledger backfill), `GOMEMLIMIT` set from the container limit, connection-count budget (pool max ≤ 40 per node; PgBouncer in transaction mode works with this design if more nodes are added).

---

## 10. Suggestions (not requested)

| # | SUGGESTION | Trade-off |
|---|---|---|
| S1 | **Stock ledger** (`stock_movements`) for every qty change, plus a nightly reconciliation job (sum of movements = inventory) | ~1 extra insert per line; makes stock disputes provable |
| S2 | **Idempotency-Key** on `POST /api/sales` (and returns, purchases) | Small table; prevents double sales on double-click/retry over flaky shop Wi-Fi |
| S3 | Separate platform JWT signing keys | One more secret to manage |
| S4 | `hsn_code` on products + proper GST tax-invoice fields (CGST/SGST/IGST split) | Small schema/UI work; legally relevant for Indian B2B invoices |
| S5 | Same-origin SPA served by the Go binary | Removes CORS/cookie complexity; static assets served by Go (cache headers + gzip at Caddy are enough) |
| S6 | Passkeys (WebAuthn) for Super Admin | Extra flow; phishing-resistant |
| S7 | Per-tenant export encrypted with `age` (filippo.io/age) | One dependency; exports safe at rest outside the DB |
| S8 | Store-scoped users (`user_stores`) so billers see only their store | Schema + UI; PRD roadmap item |
| S9 | Read replica for reports/dashboard later (READ ONLY tx already in place) | Replica lag on dashboards |
| S10 | "Find my store" on the apex host: email in → email out with the tenant URL(s) | Needs platform-level email→tenant index (hash of email only) |
| S11 | Have-I-Been-Pwned k-anonymity check on password set | External call (optional, fail-open) |
| S12 | Server-side pagination migration for all FE lists | FE work; removes the 5,000-row cap |
| S13 | CSP with nonces for the SPA, `Trusted Types` | Requires checking MUI/Emotion inline styles |
| S14 | Partition `audit_log` and `stock_movements` by month once they exceed ~10M rows | Not needed at launch |

---

## 11. Risks

| Risk | Impact | Mitigation |
|---|---|---|
| Leaked JWT secret/DB password (P0-1) is live in any deployment | Full takeover | Rotate immediately; new backend rejects the leaked value at startup |
| Catalog bloat beyond ~1–5k tenants | Slow dumps/migrations, memory | Ceiling monitoring, sharding via `db_cluster`, exit to RLS for small tenants |
| `Exec` mode overhead | Slightly higher latency | Benchmarks; DescribeExec/salted statement names as fallback |
| Contract drift breaking the SPA | Broken UI | Contract tests generated from recorded old-backend responses (golden JSON) for every route |
| Frontend still keeps tokens in storage during transition | XSS exposure | FE change ships with the backend (step 19) |
| No Go/Docker on the dev machine | Can't build/test Phase 2 | Q-9 |
| Gmail SMTP limits for tenants | Receipt failures | Outbox retries, platform fallback, status indicator |
| Razorpay event ordering/duplicates | Wrong subscription state | Event-id dedupe, `created_at` ordering, reconcile job |
| Electron needs a local Postgres | Desktop install friction | Q-1 |
| Data import surprises (negative stock, duplicate SKUs, NULL dates) | Migration delays | Dry-run report first; explicit flags |

---

## 12. Decisions needed (one batch, with recommended defaults)

| # | Question | Recommended default |
|---|---|---|
| Q-1 | Desktop/Electron: (a) Go sidecar + **embedded Postgres** binaries bundled (single local tenant), (b) sidecar + user-installed Postgres (as today), or (c) desktop is a thin client of the SaaS (no local backend)? | **(a)** for offline POS; (c) is cheapest if offline isn't needed |
| Q-2 | Cart-level discount: allocate pro-rata **before tax** (GST-correct) or flat after tax (today's math)? | **Pro-rata before tax** |
| Q-3 | Price override at POS/back-office: allowed with `sales:price_override`, otherwise `409 PRICE_CHANGED`? | **Yes** |
| Q-4 | Unpaginated list endpoints: keep bare arrays capped at 5,000 rows (FE unchanged) and migrate the FE to server paging later? | **Yes** |
| Q-5 | Public self-signup at launch? | **Off** (Super Admin creates tenants) |
| Q-6 | Approve contract changes C-01…C-21 (API_CONTRACT §4)? | **Approve all** |
| Q-7 | Leaked secrets: rotate now (you), and rewrite git history (`git filter-repo` + force-push, destructive, invalidates clones)? | **Rotate now: yes. History rewrite: yes, once you confirm**, since the secret is public anyway after rotation |
| Q-8 | Code location `backend-go/` beside `backend/`, module path `github.com/tejas1112001/POS_inventory/backend-go`? Old backend deleted only at cutover. | **Yes** |
| Q-9 | Install **Go 1.27.2** and **Docker Desktop** (testcontainers, Postgres 18, Caddy, MinIO, Mailpit) on this machine? Phase 2 cannot run tests without them. | **Yes, before Phase 2** |
| Q-10 | Sale edit: keep original biller as `user_id` and record `updated_by` (today the editor takes over)? | **Keep original** |
| Q-11 | Suspended tenant: block **all** logins and pay through an emailed Razorpay link, or allow an owner-only billing session? | **Block all + emailed link** (matches the brief) |
| Q-12 | Warehouses and product CSV import: build both, or hide the dead UI? | **Mount warehouses (cheap); build CSV import in Phase 2 step 7** |
| Q-13 | Production mail provider for platform SMTP (SES, Brevo, Resend, Postmark), and the platform display name? | **SES or Brevo via SMTP**; name `PLATFORM_NAME` env |
| Q-14 | Plans/prices, trial length, grace / read-only / retention windows? | **Trial 14 d, grace 7 d, read-only 30 d, deletion 90 d after cancellation**; plans TBD by you |
| Q-15 | Hosting target: single VM + docker-compose + managed Postgres (PITR), or Kubernetes? Object storage provider? | **Single VM + managed Postgres + S3-compatible (R2/S3)** |
| Q-16 | Purchase `Received` terminal (corrections only via stock adjustment / future purchase return)? | **Yes** |
