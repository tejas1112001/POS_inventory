# Industry Phase 1 build status

Branch `industry/phase1` (local only, not pushed). Design: `docs/INDUSTRY_PHASE1_DESIGN.md`.
How to check each item: `docs/UI_CHECKLIST.md`. Last updated: 2026-11-17.

| Item | What | State |
|---|---|---|
| 1 (a) | Business type, module registry, entitlements | Done, committed (`7016f97`), deployed to the dev stack |
| 2 (b) | Payments labelled "recorded" | Done, committed, deployed to the dev stack (tenant migration 00018) |
| 3 (c) | Barcode scanning (checkout, returns, purchases, scanner settings) | Done, committed, deployed to the dev stack |
| 4 (d) | Product attributes, variant matrix | Done, committed, deployed to the dev stack (tenant migration 00019) |
| 5 (e) | Barcode generation and label PDF | Done, committed, deployed to the dev stack (tenant migration 00020). The scan check on a real scanner and phone is for the owner (checklist step 1) |
| 6 (f) | Import templates | Done, committed, deployed to the dev stack (no migration) |
| 7 (g) | Stock counts | Done, committed, deployed to the dev stack (tenant migration 00021) |
| 8 (h) | Partial purchase receiving | Done (migration 00022 + backfill, A16 receipts endpoint, A17 old path, Receive goods screen with scanning and labels, report/dashboard); committed and deployed |
| 9 (i) | Returns policy, exchanges, store credit | Not started (L; next session) |
| 10 (j) | Thermal receipt, HSN on lines | Done (migration 00023, settings receiptPaper, HSN snapshot on sale lines, receipts and PDF, thermal CSS); committed and deployed |

## Notes
- Test database: `max_connections` raised to 200 in `internal/testdb` (the isolation suite ran out of connections at the default 100 once items 4 and 5 added test shops). Test setup only.
- Dev stack: running; platform migrations at 14. The dev database has no shops (create one in the console to check in the browser).
- govulncheck: 1 Go standard library finding (GO-2026-6859); the same on `main`, not caused by this work.
- Temporary worktree `D:/pos_baseline` (detached `main`, used for baseline test runs) to be removed at the end.

## Next step
Item 8 rest (A16 receipts endpoint, A17, purchase screen, tests, docs), then item 9 (returns policy, exchanges, store credit; L) in a new session. Branch industry/phase1 is pushed to origin.

## Decisions to confirm
- Item 7: every stock-count route is in the `stock_counts` module (the item 1 route registry), so with the module off the count pages are hidden rather than read-only. Their stock movements stay in the stock history. Design §1 says completed counts stay readable; making the two GET routes readable while the module is off needs a small change to the item 1 route check.
- Item 5: the label PDF is drawn with gofpdf directly (the library maroto wraps) instead of maroto's row layout. maroto starts new pages through gofpdf's automatic page break, and a full label sheet that ends on the bottom margin sometimes added a blank page. Barcodes still come from boombuler/barcode; no new dependency.
