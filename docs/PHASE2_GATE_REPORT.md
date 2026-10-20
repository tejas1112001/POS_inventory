# Phase 2 — Gate report after step 8

| | |
|---|---|
| Date | 2026-10-20 |
| Branch | `go-backend/phase2` (local, not pushed) |
| Commits | `16d1e9d` files/images/plan limits · `6dd8030` step 6 · `dc7fbae` money + ledger · `ba8bab4` step 7 · `1ca0ad3` pricing · `656aac0` step 8 |
| Status | **Stopped after step 8** as asked. Nothing from step 9 on is built |

## 1. What was built

| Step | Outputs |
|---|---|
| 6 Master data | Tenant migration 00002; stores (+ showcase admin), categories, sub-categories, brands, units, suppliers, customers, warehouses; files: FS (`os.Root`) and S3 drivers, image pipeline (sniff, size/pixel caps, re-encode), upload/attach/serve; plan limits (stores, users, products, storage) |
| 7 Products/inventory | Tenant migration 00003; products + variants (unique SKU/barcode), per-store publication, POS list, barcode lookup, atomic POS locks, inventory set, append-only stock ledger (S1) with reconcile, CSV import (dry run, all or nothing) |
| 8 Sales/POS | Tenant migration 00004; pricing engine (pro-rata discount before tax, inclusive/exclusive tax, CGST/SGST/IGST, half-up per line, currency exponents); checkout with ordered row locks; `409 PRICE_CHANGED` / `INSUFFICIENT_STOCK` with details; price override and discount cap by permission; POS aliases and ignore list; gapless references from `invoice_prefix`; Idempotency-Key (S2); edit keeps the biller; void restocks; own-only sales and invoices (C-10) |

## 2. Checks

- **Gate commands, all green:** gofmt, `go mod tidy -diff`, vet, `constsql`, golangci-lint (both build modes), `make lint`, govulncheck, unit and integration tests. **244 test cases pass, 0 fail, 0 skipped.** govulncheck reports one advisory in `golang.org/x/crypto/openpgp` (no fix exists); our code doesn't call it.
- **Isolation:** every authenticated tenant route (**73**) rejects another tenant's token on 2 foreign hosts. Each new module has ID-based cross-tenant tests (read, edit, delete and use of another tenant's IDs give 404/400 and leave the data untouched).
- **Stock safety:** a 20-way race on 5 units sells exactly 5, and their references are consecutive. 24 concurrent carts with opposite line order produce no deadlock. 6 concurrent submits with one Idempotency-Key create one sale. The ledger reconciles after every stock test.
- **Dev stack smoke:** `migrate tenants up` moved `demo-shop` from version 2 to 4, and drift reports a match. Over HTTPS through Caddy: login, product, POS sale (server totals, GST split), price override (audited), oversell 409, void and restock all worked.
- **Not shown:** CI has never run (nothing pushed). The race detector can't run on this machine (no C compiler).

## 3. Differences from the design (please confirm)

| Topic | Built | Why |
|---|---|---|
| Old-form extras | `createdBy` (all forms) and the showcase form's name/address are accepted and ignored | Strict decoding without breaking today's frontend |
| Image purposes | Added category/brand/supplier images; private (served from `/api/files/…`) | The old forms upload them |
| Product images | Public URL only while published in some store; otherwise `/api/files/…` (signed-in) | No public access to unpublished products |
| Product delete | Refused (`409 INVALID_STATE`) while any stock remains | Stock never disappears silently |
| Product edit | `productType` cannot change | Avoids orphaning variants and stock |
| Plan limits | Users, stores, products and storage are enforced now (step 16 adds billing) | The limits table already exists; the isolation suite raises the trial `max_users` |
| Sale reference | Client `reference` ignored; the server issues `<invoice_prefix>-000001` from a gapless counter | GST needs consecutive numbers. The counter row serialises the end of each checkout per tenant (a few ms) |
| `paid` | Missing means paid in full (POS sends none); `0` is kept; above the total it is capped | POS compatibility, C-04 |
| Line discount | A non-zero line `discount` is refused (400), not ignored | Silently dropping a discount would change the charged price |
| Missing `variantId` | Allowed for single-variant products | `AddSales.jsx` sends only `productId` |
| Sale edit | A price already on the sale is not an override, even if the catalog changed since | Editing old sales must not need override rights |
| Sales filters | `startDate`/`endDate` filter the sale `date` as whole days in the tenant's time zone (end inclusive) | `createdAt` and UTC midnight cut off the last day |
| Void | Optional `?reason=`; voiding twice returns 204 | Idempotent DELETE |
| Pending sales | Still take stock, as before | Unchanged behaviour |
| Receipts | `receiptEmail` is accepted and stored; `receiptUrl` arrives with step 9 | Receipts are step 9 |
| Error body | Optional `details` (current prices, stock shortfalls) | The `409` responses need data |
| Idempotency keys | Expire after 24 h when the key is reused; no cleanup job yet | No worker until step 9 |

## 4. Deferred

- No worker yet (step 9), so these jobs don't run: nightly ledger reconcile (`inventory.Reconcile` exists), the expired POS lock sweep (expired locks are ignored, not deleted) and idempotency key cleanup.
- `impersonation_id` on sales and stock movements stays empty until impersonation is built.
- A sale that contains a since-deleted product can be voided but not edited.
- Frontend changes (step 19): `AddSales.jsx` must send `storeId`; POS should handle `409 PRICE_CHANGED` and send an Idempotency-Key.

## 5. Bugs found and fixed while building

`FOR UPDATE` inside read-only transactions; pgx rows not scanned; a `citext` parameter compared as text; `decimal.Decimal` used as a map key (it never matched); `[]byte` sent to a `jsonb` column; a planned ledger `UPDATE` that the append-only grant would have refused (caught in review, before it ran).

## 6. Open items for the owner

1. Push and CI still wait on the history rewrite (`20cdb92`).
2. Confirm the differences in §3.
3. Say "approved" for step 9 (receipts and email, PDF spike first) when ready.
4. Dev only: for the smoke test I set the `owner@demo.test` password on the local stack to `smoke-test-pass-2026`. The demo shop also now has a "Smoke Tea" product and 3 sales (one void).

---

# Phase 2 — Gate report after step 5

| | |
|---|---|
| Date | 2026-10-19 |
| Branch | `go-backend/phase2` (local, not pushed) |
| Commits | `9dc196a` .gitattributes · `cf75fb2` step 1 · `645c81d` step 2 · `0ef07b2` step 3 · `f85ffe2` step 4 · `f4ccf20` step 5 |
| Size | 98 files, ~16.8k lines in `backend-go/` |
| Status | **Stopped for review** (PHASE1_DESIGN §8). Nothing from step 6 on is built |

## 1. What was built

| Step | Outputs |
|---|---|
| 1 Foundation | Go 1.27.2 module, `posd` + `posctl`; env-only config that fails fast on missing, weak, placeholder or leaked secrets (SHA-256 deny list, values never in code); redacting slog; typed errors → `{error, code, fields}`; host classifier (tenant/admin/apex, else 421) with trusted-proxy handling; request IDs, access log, metrics, recover, secure headers incl. strict admin CSP, body caps, timeouts, strict JSON, dev-only CORS; `/healthz`, `/readyz`; graceful shutdown; `constsql` analyzer; distroless Dockerfile; Makefile; golangci-lint; CI + Dependabot; dev compose (Postgres 18.6, Caddy, S3 gateway, Mailpit). `frontend/.env` untracked (O-8) |
| 2 Platform schema + registry | `posctl db bootstrap` (roles, schemas, extensions; SCRAM verifiers, never clear passwords); goose platform track; `InTenantTx`/`InPlatformTx` (SET LOCAL role + `set_config(…, true)`, tamper tripwire, distinct Go types); pool destroys connections returned inside a transaction; tenant registry with negative cache; dedicated LISTEN connection (flush on connect and disconnect, degraded TTL); `posctl db preflight` (M1–M14) |
| 3 Runner + provisioning | Tenant migration 00001; per-tenant advisory lock, parallel runner, dry run, resume, drift fingerprint; post-step (grants, tenant-bound `mark_outbox_pending()`, permission sync); provisioning steps 1–10 with compensation, retry, reaper |
| 4 Super Admin | Platform users (argon2id), mandatory TOTP (RFC 6238 vectors, replay guard), recovery codes, rotating `__Host-prt` with reuse detection, step-up, lockout, tenant admin (list/get/create/patch/slug/suspend/reactivate/delete/restore/retry), append-only audit, `posctl bootstrap-superadmin`, old-slug 308 redirect |
| 5 Tenant auth + RBAC | Login/lockout/rate limits, rotating `__Host-rt`, logout, me, password change/forgot/reset, invite accept; auth state + permissions per request (cached, NOTIFY-invalidated); tenant status gate; users + roles with anti-escalation (C-07); `/api/public/tenant-info`; tenant audit log; route self-check; owner invite link from the console |

## 2. Exit criteria

| # | Criterion | Result |
|---|---|---|
| 1 | CI green on Linux runners | **Not yet shown.** The workflow exists but has never run, because nothing is pushed. Locally every gate is green: gofmt, `go mod tidy`, `go vet`, `constsql`, golangci-lint (incl. gosec, staticcheck, errcheck, revive) in both build modes, govulncheck (no vulnerabilities), unit and integration tests. Race detector **not run locally** (no C compiler on this machine); CI runs it |
| 2 | search_path suite | **Pass.** TENANCY §5.1 (pool 1 and 2, concurrent tenants, released connections, error/panic/cancel/timeout), §5.2 (version skew on one connection; the documented `CacheStatement` failure), §5.4 tests 1–5, §8 (`NO TRANSACTION` index and reconnect stay in the tenant schema) |
| 3 | Tenant-isolation suite (3+ tenants) | **Pass.** Every authenticated tenant route (17) rejects another tenant's token on 2 foreign hosts; privilege matrix across 3 tenant roles; no tenant role sees `platform` or another schema; forged `X-Forwarded-Host`; identical 404 for unknown/deleted/provisioning hosts incl. `/api/public/tenant-info` |
| 4 | Provisioning fault injection | **Pass.** A failure injected after each of steps 2–8 leaves no orphan schema, role or subscription and nothing in `provisioning`; retry then succeeds with the original options |
| 5 | This report | Done |

**Tests:** 66 unit-test functions and 61 integration-test functions. A full run executes 197 test cases including subtests: 197 pass, 0 skipped. Integration tests run on testcontainers PostgreSQL 18.6.

## 3. Differences from the design (please confirm)

| Topic | Design | Built | Why |
|---|---|---|---|
| Dev S3 | MinIO | Versity S3 Gateway (Apache-2.0, v1.9.0) | MinIO's images are no longer publicly pullable. The app talks plain S3, so the code doesn't change |
| Dev ports | 443/80/5432/8025 | 8443 / 55432 / 8026 (`HTTPS_PORT`, `POSTGRES_HOST_PORT`) | Those ports are taken on this machine |
| `constsql` | Runs inside golangci-lint as a module plugin | Runs as `go vet -vettool` (`make constsql`, CI step) | Simpler; same analyzer. It exempts `_test.go`, `internal/testdb/` and pass-through wrappers |
| gosec | Standalone binary | Through golangci-lint | gosec v2.29.0 can't read Go 1.27 export data |
| New env vars | — | `DATABASE_ADMIN_URL` (bootstrap/preflight only), `HSTS_ENABLED`, `REQUEST_TIMEOUT`, `SHUTDOWN_*` | Needed by the design's own steps |
| Production DB passwords | Not specified | DSN passwords must be ≥ 16 chars in production | Also covers the old weak password, whose hash is deliberately not in code |
| Platform roles | `platform_roles` + `platform_user_roles` tables | One `role` column on `platform_users` | One role per staff member is enough for the §7.1 matrix |
| Owner invite | Token created in provisioning step 7 | Created when sent: by the console's `owner-invite` action (link shown once) or the email worker | Avoids storing a raw token anywhere |
| Reset/invite emails | Outbox data holds "IDs and small fields only" | Raw token is envelope-encrypted inside the outbox row | The link needs the token; it is never stored in clear |
| Remember-me | 30 d sliding | 30 d sliding, capped at 90 d absolute | Bounds a stolen-but-refreshed token |
| Tenant restore from `pending_deletion` | "Cancel pending deletion" | Lands in `suspended`; reactivation is a separate step | Deliberate two-step return |
| Slug CHECKs | Regex on `citext` | Regex on `slug::text` | On `citext` the regex was case-insensitive and accepted uppercase (bug found by a test) |
| Password blocklist | "Embedded list of common/breached passwords" | ~150 most common passwords + project demo passwords | No network download during the build. A larger list can drop in later |
| Runner eligibility | Status not in (`deleted`,`failed`) | Also skips `provisioning` (the orchestrator migrates those) | Avoids racing provisioning |

## 4. Not built yet (by design or deferred)

- Impersonation (TENANCY §9), plans, billing, notifications, exports, platform staff management, and the migrations endpoints on the platform API (steps 9–18). `/api/auth/impersonation/exchange` is not mounted.
- Email sending: reset/invite/welcome emails are queued in the outbox only. The worker arrives in step 9.
- The frontend still uses the old API (step 19).

## 5. Open items for the owner

1. **Push and CI.** Push only after the history rewrite: commit `20cdb92` on this branch's ancestry contains the old DB password. Rotation is done (Q-7), so what's left is cleaning history.
2. Confirm the differences in §3.
3. Say "approved" for steps 6+ when ready.
4. One isolation-suite run failed once (61 s, during a deliberate mutation experiment) and could not be reproduced: three clean runs followed. If CI ever shows it again, it is worth investigating.
