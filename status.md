# Industry Phase 1 build status

Branch `industry/phase1` (local only, not pushed). Design: `docs/INDUSTRY_PHASE1_DESIGN.md`.
How to check each item: `docs/UI_CHECKLIST.md`. Last updated: 2026-11-17.

| Item | What | State |
|---|---|---|
| 1 (a) | Business type, module registry, entitlements | Done, committed (`7016f97`), deployed to the dev stack |
| 2 (b) | Payments labelled "recorded" | Done, committed, deployed to the dev stack (tenant migration 00018) |
| 3 (c) | Barcode scanning (checkout, returns, purchases, scanner settings) | Done, committed, deployed to the dev stack |
| 4 (d) | Product attributes, variant matrix | Done, committed, deployed to the dev stack (tenant migration 00019) |
| 5 (e) | Barcode generation and label PDF | In progress: EAN-13 codes, layouts and label PDF written and unit-tested; next: migration 00020, A9-A11 endpoints, P3, UI |
| 6 (f) | Import templates | Not started |
| 7 (g) | Stock counts | Not started |
| 8 (h) | Partial purchase receiving | Waiting: stop here and show the backfill report from the copy run |
| 9, 10 (i, j) | After the owner's OK on item 8 | Not started |

## Notes
- Dev stack: running; platform migrations at 14. The dev database has no shops (create one in the console to check in the browser).
- govulncheck: 1 Go standard library finding (GO-2026-6859); the same on `main`, not caused by this work.
- Temporary worktree `D:/pos_baseline` (detached `main`, used for baseline test runs) to be removed at the end.

## Next step
Item 5: migration 00020 (barcode counter, label layouts setting), A9 generate codes, A10 label PDF, A11 layouts, P3 `labels:print`, matrix "generate barcodes", product-page print UI, isolation tests, docs. Label PDF is drawn with gofpdf (the library maroto wraps) for exact sheet positions.
