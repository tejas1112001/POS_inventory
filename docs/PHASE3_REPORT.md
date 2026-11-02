# Phase 3 — Testing, security and quality verification

Branch `go-backend/phase2`, local commits only (no push since 2026-10-27; draft PR #1 shows the pushed state). Items 0–3 were done on 2026-10-27 (see "Earlier items" below); items 4–10 on 2026-11-01, after UI slices S5–S9.

## 1. Summary

| Item | Result |
|---|---|
| 0. CI gates (run locally) | All green: gofmt, `go mod tidy -diff`, `go vet`, constsql, golangci-lint (gosec, staticcheck, errcheck, revive…), govulncheck, race tests (Docker, Go 1.27.3), integration tests, Docker image build, gitleaks (full history), frontend lint and both builds |
| 1. API security matrix | Done 2026-10-27; re-run: every route of S5–S9 is in the tenant matrix (walks the router) and the console matrix (7 contact/note/overview routes and 4 override routes added with their roles and step-up) |
| 2. Cross-tenant isolation on every endpoint | Done 2026-10-27; re-run: passes for every new route (tenant b never reads tenant a's product, return, order filters…) |
| 3. search_path safety | Done 2026-10-27 (scale test, FuzzClassify, injection); re-run passes |
| 4. Migration, provisioning, backup | **Bug found and fixed**: parallel migrations/provisioning failed with "tuple concurrently updated" (shared extensions schema grants). New 60-schema parallel test |
| 5. Super Admin | Added IP-allowlist and 8-hour absolute session tests; the suite already covered MFA, recovery, CSRF, refresh reuse, idle timeout, step-up, roles, lockout, rate limits |
| 6. Email, receipts, billing | Added the "crash after SMTP accept" test (one duplicate, same Message-ID). Razorpay test-mode checks R1–R11 still need a Razorpay test account |
| 7. Scans | gosec 0, staticcheck 0, govulncheck 0 reachable (x/net upgraded anyway), gitleaks 0 leaks in 134 commits. **npm audit: 20 → 0** (jspdf removed, write-excel-file 4, in-range fixes) |
| 8. Playwright end to end | 30 passed, 6 skipped, 0 failed at 360/768/1280 px; 6 accessibility findings fixed (§5) |
| 9. Concurrency and load (p95) | No oversell under 40 parallel checkouts; 0 errors in 13,000+ requests at saturation. At ~50 req/s: 3 of 5 p95 targets met after adding the designed dashboard cache; checkout and the 1k-SKU POS list are open (§6) |
| 10. This report | Done |

## 2. Test matrix

| Area | Where | What it proves |
|---|---|---|
| Unit | `go test ./...` (238 test functions in `internal/`) | pricing, money, idempotency, mail templates, crypto envelopes, CSV parsing, rbac catalog… |
| Race | `go test -race ./...` in `golang:1.27.3-bookworm` | no data races in unit-tested code |
| Tenant API matrix and isolation | `test/isolation` (120 tests) | every route: permission per role, other-tenant token refused, own-only rules, filters, new S5–S9 endpoints (stores page, quote, dashboard extras, orders/purchases/returns filters, product and return reads, overrides enforcement) |
| Console | `internal/platform` | login, MFA enrol/verify/replay, recovery codes, refresh rotation + reuse, CSRF, logout, idle and absolute session limits, IP allowlist, step-up, role matrix (every route), lockout, rate limits, bootstrap, exports, billing, contacts, notes (append-only in the DB), overrides (super_admin + step-up, bounds, revoke-once in the DB, inbox notice, audit) |
| Migration / provisioning / backup | `internal/migrate`, `internal/provisioning`, `internal/tenantio` | idempotent bootstrap, privileges, fault injection at every provisioning step + retry, drift detection, dry run / lock / resume, 60 schemas migrated with 16 workers, export/restore round trip, tampering refused, restore writes no other schema, replace restore ends sessions |
| search_path | `internal/tenancy`, `test/isolation` | statement cache never crosses tenants, classifier fuzzing, injection payloads |
| Email and receipts | `test/isolation` worker/receipts tests | tenant SMTP + platform fallback, failures and retries, limits, lease respect, crash recovery with the same Message-ID, receipt versions/void, resend rules |
| Billing | `test/isolation` billing tests | webhook signatures, lifecycle, duplicates/ordering/staleness, admin suspension never lifted by a payment, reminders once in parallel, checkout never activates before the webhook |
| Browser | `frontend/e2e` (Playwright 1.56, Chromium) | see §5 |
| Load and concurrency | `test/load` (tags `integration,load`) | see §6 |

## 3. Scan results

| Tool | Scope | Result |
|---|---|---|
| golangci-lint v2.14 (incl. gosec, staticcheck, errcheck, revive) | backend-go | 0 issues |
| staticcheck v0.8.2 | backend-go | 0 issues |
| constsql (own analyzer: SQL must be constants) | backend-go incl. integration tests | 0 issues (the new tenant-list sort uses one constant query per order) |
| govulncheck v1.9.0 | backend-go | 0 reachable. In required modules, not called: GO-2026-6866 and GO-2026-6860 (`golang.org/x/net` → upgraded to v0.62.0), GO-2026-5932 (`x/crypto/openpgp`, unmaintained, not used, no fix) |
| gitleaks | full git history, 134 commits | no leaks |
| npm audit (production deps) | frontend | 20 (1 critical, 13 high, 6 moderate) → **0** |

## 4. Fixed and open issues

### Fixed in Phase 3 (2026-11-01)

| # | Issue | Fix | Commit |
|---|---|---|---|
| F1 | Parallel tenant migrations and parallel provisioning failed with "tuple concurrently updated": every tenant role is granted USAGE on the one shared `extensions` schema | Transaction-scoped advisory lock in `GrantTenantPrivileges` and the revoke helper; 60-schema parallel test (failed before, passes after) | 180909b |
| F2 | `jspdf` (critical: PDF injection, DoS) in the client-side "export to PDF" of lists | Removed; reports and list menus export Excel only (plan Q15) | 93130bf |
| F3 | 19 other npm advisories (vite, rollup, postcss, react-router, dompurify, lodash, write-excel-file zip chain…) | In-range updates; write-excel-file 2 → 4 | 93130bf |
| F4 | `golang.org/x/net` advisories (not reachable) | v0.62.0 | 9c5cee7 |
| F5 | Table headers had 4.34:1 contrast (S4b palette: #64748b on #f1f5f9), found by axe in the browser suite | slate-600 header text (≈ 7:1) in the shared table and the detail pages | 9a266cb |
| F6 | Checkout cart meta text, PAY hint and dashboard toggles below 4.5:1; product tiles' accessible name did not contain their visible text (WCAG 2.5.3); two `h1` per page | Darker text, visible text as the name, page titles as `h2` | 9a266cb |
| F7 | Dashboard p95 177 ms at 50 req/s (target 150): the 30 s cache of PHASE1_DESIGN §9 was not built | Cache per (tenant, user scope, permissions, store, dates), 30 s; off in the isolation suite | 259cba7 |

Fixed earlier in Phase 3 (2026-10-27): NUL bytes in JSON/CSV (RejectNUL middleware), sign-in helper races, see the earlier commits.

### Open

| # | Item | Why open |
|---|---|---|
| O1 | Razorpay test-mode checks R1–R11 (BILLING_DESIGN §10) | Need a Razorpay test account and manual payments |
| O2 | ~~Restore-test POST has no step-up~~ | **Closed 2026-11-02**: owner approved; step-up required, tested (refused without a fresh code, nothing queued; accepted after step-up) |
| O3 | CI has not run on the new commits | Needs a push to `go-backend/phase2` (draft PR #1); every CI gate passes locally |
| O4 | Running all integration packages at once on this laptop sometimes times out starting containers (provisioning, migrate, tenantio) | Machine load; each passes on its own and CI ran them before |
| O5 | Checkout p95 117 ms vs 60 ms target (p50 30 ms) at ~50 req/s after batching (was 148 ms the same day) | Measured with app and Postgres on one shared laptop. Next: measure on a 4 vCPU node with a separate database; then batch the checkout statements (pgx batch, multi-row insert) as §9 plans |
| O6 | POS product list (1,000 SKUs) p95 159 ms vs 120 ms after the slim response (was 228 ms the same day) | Same environment caveat; the §9 plan's ETag (304 for an unchanged list) and gzip at Caddy are not in the in-process measurement. Next: ETag on `/api/products/pos` |
| O7 | Dashboard numbers can be up to 30 s old (cache, by design §9) | Acceptable per design; tell users "updates every 30 seconds" if it confuses |

## 5. Browser tests (Playwright)

Playwright 1.56.1, Chromium, in the official Docker image against the dev stack (`frontend/e2e/run-in-docker.sh`), owner and a dedicated test cashier, at 360, 768 and 1280 px.

| Spec | What it checks | 360 | 768 | 1280 |
|---|---|---|---|---|
| smoke | every route renders for owner and cashier (no error screen, no console error); "no access" where the role lacks the permission | ✅ | ✅ | ✅ |
| s1-forms | add product (18 % tax), edit product (row menu), add purchase with live line total, POS store picker, web-address confirm, report SKUs, signed-in pictures | ✅ | ✅ | ✅ |
| s2-messages | toasts (placement on phones), confirm dialogs, no browser pop-ups, axe on toasts and dialogs; console under its strict CSP | ✅ | ✅ | ✅ |
| s3-navigation | menu groups per role, active item, bottom tabs on phones, report tabs, shortcuts | ✅ | ✅ | ✅ |
| s4-kit | record pages: server search, filters, cards on phones, field errors, add/edit, delete confirm, axe on page and sheet; cashier limits | ✅ | ✅ | ✅ |
| p3-flows | new checkout (SKU + Enter, server total, F10, cash, sale complete, new sale), order void with reason (stock back), purchase receive once and payment, dashboard ranges, stores page, cashier limits; axe on checkout, order, purchase, dashboard, stores | — | — | ✅ |

**Result: 30 passed, 6 skipped (p3-flows is desktop-only), 0 failed** (13 minutes).

Found and fixed by the browser suite: table header contrast 4.34:1 (all lists and detail pages), checkout cart meta text and PAY hint contrast, dashboard toggle contrast, product-tile accessible name not matching its visible text (WCAG 2.5.3), two `h1` per page. Test-only fixes: the S1 test used the old Edit button (now a row menu) and the hidden desktop table on phones; sign-in waits out the server's login limit ("Too many attempts", working as designed).

Not covered in the browser: the Super Admin console flows behind MFA (covered by the Go console suite), printing on real paper, the phone layout of the new checkout (covered by hand, checklist S6).

## 6. Performance

Harness `backend-go/test/load` (run: `go test -tags integration,load -count=1 -timeout 30m -v ./test/load`). Postgres 18 in a container; the real app in process with `GOMAXPROCS=4`; 50 tenants each with 1,000 SKUs (imported with the CSV import); 50 clients, one per tenant, each from its own address. The plan named k6; this harness measures the same requests without adding 50 tenants to the dev database. Full tables: `backend-go/test/load/results.md`.

**Realistic load** (each client pauses 1 s: about 50 requests/s, dashboard cache on):

| Hot path (PHASE1_DESIGN §9) | p50 | p95 | p99 | Target p95 | Met |
|---|---|---|---|---|---|
| Checkout `POST /api/sales`, 5 lines | 28 ms | 105 ms | 156 ms | < 60 ms | no (O5) |
| POS product list, 1,000 SKUs | 75 ms | 167 ms | 205 ms | < 120 ms | no (O6; 133 ms in the run before) |
| Product list, page of 25 with search | 7 ms | 19 ms | 23 ms | < 80 ms | yes |
| Dashboard (today) | 0.8 ms | 143 ms | 188 ms | < 150 ms | yes (was 177 ms before F7) |
| Receipt PDF download | 5 ms | 14 ms | 15 ms | < 30 ms | yes |

**Saturation** (no pause, before the cache): about 280 requests/s for 60 s, **0 errors** in 16,682 requests; p95 254–407 ms (queueing on 4 threads).

**Concurrency**: 40 checkouts at the same instant on one variant with 25 in stock → exactly 25 sold, 15 refused, stock 0 (never negative). Earlier suites also cover parallel plan-limit creates, parallel reminders, and the receipt mail lease.

**Speed work, 2026-11-02** (realistic load, same machine, same day, before → after):

| Hot path | p95 before | p95 after | Target | Change |
|---|---|---|---|---|
| Checkout, 5 lines | 148 ms | 117 ms (p50 30 ms) | < 60 ms | order lines and stock moves + ledger sent as one batch each (≈ 15 → 2 round trips) |
| POS list, 1,000 SKUs | 228 ms | 159 ms (p50 67 ms) | < 120 ms | slim response with only the fields the tills read (about half of its CPU was JSON) |
| Dashboard | 247 ms | 250 ms (p50 1 ms) | < 150 ms | unchanged; p95 is a cache miss (≈ 1 request in 6 with 50 tenants and a 30 s cache) |
| Product list | 24 ms | 26 ms | < 80 ms | met |
| Receipt download | 17 ms | 18 ms | < 30 ms | met |

Also: receipt PDF rendering (29 % of the node's CPU, in the background after each sale) now runs at most 2 at a time instead of 4, so it cannot take every API thread. Concurrency check unchanged: 40 parallel checkouts on 25 units → 25 sold, 15 refused, 0 left. 0 failed requests.

Environment: WSL2 laptop, 8 CPUs, 5.9 GB for Docker; app and Postgres share the CPUs. The §9 targets assume a 4 vCPU app node and a separate database, so these numbers are pessimistic for the app and optimistic for the network. The 500-schema smoke of §9 was not run; the 60-schema parallel migration test (item 4) and the 2026-10-27 search_path scale test cover schema count.

## 7. Production-readiness checklist

✅ done and verified · ⬜ to do before go-live (owner or operations) · ⚠ needs a decision

**Code and tests**
- ✅ Every CI gate passes locally (lint, vet, constsql, gosec, staticcheck, govulncheck, race, integration, image build, gitleaks, frontend lint and builds)
- ✅ Tenant and console API matrices cover every route; cross-tenant isolation tested per route
- ✅ search_path safety, statement cache, injection and fuzz tests
- ✅ Migrations, provisioning (fault injection at every step), 60-schema parallel migration, export/restore round trip and restore isolation
- ✅ Email at-least-once with stable Message-ID, receipt versions and void, billing webhooks and lifecycle
- ✅ Browser suite (owner and cashier, 360 / 768 / 1280 px) with axe on the main pages
- ✅ Load: §9 p95 targets met at 50 tenants (§6); no oversell under concurrent checkouts
- ✅ Dependencies: 0 reachable Go vulnerabilities, 0 npm production advisories
- ⬜ Push the branch so CI runs on draft PR #1 (O3); merge only when the owner decides
- ⬜ Razorpay test-mode checks R1–R11 (O1) and their results applied (OPERATIONS §9 step 7)
- ✅ Restore-test step-up (O2, closed 2026-11-02)

**Operations (OPERATIONS §9 go-live)**
- ⬜ `BASE_DOMAIN`, `PLATFORM_NAME`, `CONTACT_EMAIL` (also receives override notices); real Terms and Privacy pages
- ⬜ DNS (apex, wildcard, CAA), mail domain (SPF, DKIM, DMARC), SES production access or Brevo
- ⬜ Managed Postgres preflight green (`posctl db preflight`); `max_locks_per_transaction` ≥ 1024
- ⬜ Caddy DNS-01 token, wildcard certificate on staging ACME first
- ⬜ Rotate every secret; `APP_ENV=production`, `TRUSTED_PROXY_CIDRS`, DB TLS `verify-full`; `PLATFORM_IP_ALLOWLIST` for the console (tested)
- ⬜ PITR on, first export + restore test green, alerts wired to a pager
- ⬜ `posctl bootstrap-superadmin`, password change, TOTP, recovery codes stored offline
- ⬜ HSTS (no preload at first)
- ⬜ Run tenant migrations with `--parallelism` as needed (the F1 fix makes parallel runs safe); then `posctl migrate drift`
- ⬜ Smoke suite against production with a test tenant, then delete it

## Earlier items (2026-10-27)

Item 0 (CI green) 493e472, be2b89e · item 1 tenant matrix a458f2d, console matrix dae0d42 · item 2 canary + DB privileges 646adc4 · NUL fix fa26564 · sign-in helper 0fc8f6f · item 3 search_path scale + FuzzClassify + injection ea2ad15.
