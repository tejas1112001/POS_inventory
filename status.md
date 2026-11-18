# Industry Phase 3 (pharmacy) build status

Branch `industry/phase3` (from main `2ac28f2`), local only: no push or merge without the owner's OK. Design: `docs/INDUSTRY_PHASE3_PHARMACY_DESIGN.md`, approved 2026-11-18 (all of §14; Q13 refuse, Q14 last, Q15 yes, Q16 0, Q17 later). Last updated: 2026-11-18.

| Item | What | State |
|---|---|---|
| 1 | Batches and expiry: module, batch records, stock per batch, switching, reconciliation | Built (tenant migration 00025, platform 00015, A23–A25, P7/P8). Done; owner OK on the copy-run report 2026-11-18 |
| 2 | Receiving with batch and expiry | Done (tenant migration 00026: DB30 + DB36 settings; A26; labels print batch/expiry); committed and deployed |
| 3 | Selling by batch (FEFO, chosen batch, unknown last with warning, void per batch, edit refused) | Done (tenant migration 00027: DB31; A27); committed and deployed |
| 4 | Returns per batch, quarantine (A28, A29) | Done (tenant migration 00028: DB32, DB33); committed and deployed |
| 5 | Write-offs, recall, trace (A30, A31; P9, P10) | Done (tenant migration 00029: DB34); committed and deployed |
| 6 | Stock counts per batch (A32) | Done (tenant migration 00030: DB35); committed and deployed |
| 7–8 | Expiry reports/dashboard/digest, pharmacy import | 7 drafted; 8 not started |

## Phase 3 notes
- Item 1: every stock change already goes through `inventory.Move`/`Set`/`Apply`; they now keep tracked products' batches in step (increase → unknown batch, decrease → earliest expiry first, unknown last). `ApplyBatched` takes named batches for items 2–8.
- Copy run: the dev database has no shops, so the copy shows only that the migrations apply (platform 15, tenants 25). Switch-on, reconciliation and totals come from a scratch two-store pharmacy shop in `TestBatchTrackingSwitchAndReconcile`.

---

# Industry Phase 1 build status (merged)

Branch `industry/phase1`, merged into `main` on 2026-11-18 (PR #20, merge commit `2ac28f2`; CI green on main). Design: `docs/INDUSTRY_PHASE1_DESIGN.md`.
How to check each item: `docs/UI_CHECKLIST.md`. Last updated: 2026-11-18.

| Item | What | State |
|---|---|---|
| 1 (a) | Business type, module registry, entitlements | Done, committed (`7016f97`), deployed to the dev stack |
| 2 (b) | Payments labelled "recorded" | Done, committed, deployed to the dev stack (tenant migration 00018) |
| 3 (c) | Barcode scanning (checkout, returns, purchases, scanner settings) | Done, committed, deployed to the dev stack |
| 4 (d) | Product attributes, variant matrix | Done, committed, deployed to the dev stack (tenant migration 00019) |
| 5 (e) | Barcode generation and label PDF | Done, committed, deployed to the dev stack (tenant migration 00020). The scan check on a real scanner and phone is for the owner (checklist step 1) |
| 6 (f) | Import templates | Done, committed, deployed to the dev stack (no migration) |
| 7 (g) | Stock counts | Done, committed, deployed to the dev stack (tenant migration 00021) |
| 8 (h) | Partial purchase receiving | Done (migration 00022 + backfill, A16 receipts endpoint, A17 old path, Receive goods screen with scanning and labels, report/dashboard); committed (`a6efa50`) and deployed |
| 9 (i) | Returns policy, exchanges, store credit | Done (migration 00024, A18 policy settings, A19 policy on returns, A20 exchanges, A21/A22 store credit ledger and checkout method, P5/P6 permissions); committed (`2bb68b2`) and deployed |
| 10 (j) | Thermal receipt, HSN on lines | Done (migration 00023, settings receiptPaper, HSN snapshot on sale lines, receipts and PDF, thermal CSS); committed and deployed |

## Notes
- Item 9, DB20: `sales.exchange_return_id` has no foreign key (a key would make a cycle with `sale_returns.sale_id`, which tenant export/restore refuses). Accepted by the owner on 2026-11-18. The column is UNIQUE (that is its index), and `linkExchange` links only a return of the same store and customer, never twice.
- Pushing: from 2026-11-18 commits stay local; push only when the owner asks.
- Test database: `max_connections` raised to 200 in `internal/testdb` (the isolation suite ran out of connections at the default 100 once items 4 and 5 added test shops). Test setup only.
- Dev stack: running; platform migrations at 15, tenant migrations at 30 (Phase 3 items 1-6 deployed). The dev database has no shops (create one in the console to check in the browser).
- govulncheck: clean after the Go 1.27.4 bump (GO-2026-6859 fixed).
- Temporary worktree `D:/pos_baseline` removed.

## Next step
Phase 1: all ten items built and merged (PR #20). Go is now 1.27.4 (GO-2026-6859). `TestLimitWaitsDoNotUseAttempts` flake fixed.

Phase 3 (pharmacy): design note `docs/INDUSTRY_PHASE3_PHARMACY_DESIGN.md` drafted 2026-11-18 with the owner's answers Q8–Q12. Waiting for approval of DB25–DB37, P7–P10, A23–A35, E3 and Q13–Q17 before any code.

## Decisions to confirm
- Item 7: every stock-count route is in the `stock_counts` module (the item 1 route registry), so with the module off the count pages are hidden rather than read-only. Their stock movements stay in the stock history. Design §1 says completed counts stay readable; making the two GET routes readable while the module is off needs a small change to the item 1 route check.
- Item 5: the label PDF is drawn with gofpdf directly (the library maroto wraps) instead of maroto's row layout. maroto starts new pages through gofpdf's automatic page break, and a full label sheet that ends on the bottom margin sometimes added a blank page. Barcodes still come from boombuler/barcode; no new dependency.
