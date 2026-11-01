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
| 8. Playwright end to end | See §5 |
| 9. Concurrency and load (p95) | See §6 |
| 10. This report | — |

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
| F5 | Table headers had 4.34:1 contrast (S4b palette: #64748b on #f1f5f9), found by axe in the browser suite | slate-600 header text (≈ 7:1) in the shared table and the detail pages | see §5 |

Fixed earlier in Phase 3 (2026-10-27): NUL bytes in JSON/CSV (RejectNUL middleware), sign-in helper races, see the earlier commits.

### Open

| # | Item | Why open |
|---|---|---|
| O1 | Razorpay test-mode checks R1–R11 (BILLING_DESIGN §10) | Need a Razorpay test account and manual payments |
| O2 | Restore-test POST has no step-up although PHASE1_DESIGN §7.1 lists it | Security change: waiting for the owner (a test pins today's behaviour) |
| O3 | CI has not run on the new commits | Needs a push to `go-backend/phase2` (draft PR #1); every CI gate passes locally |
| O4 | Running all integration packages at once on this laptop sometimes times out starting containers (provisioning, migrate, tenantio) | Machine load; each passes on its own and CI ran them before |

## 5. Browser tests (Playwright)

_Filled in below after the run._

## 6. Performance

_Filled in below after the run._

## 7. Production-readiness checklist

_Filled in below._

## Earlier items (2026-10-27)

Item 0 (CI green) 493e472, be2b89e · item 1 tenant matrix a458f2d, console matrix dae0d42 · item 2 canary + DB privileges 646adc4 · NUL fix fa26564 · sign-in helper 0fc8f6f · item 3 search_path scale + FuzzClassify + injection ea2ad15.
