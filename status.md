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
| 6 (f) | Import templates | In progress: import screen written; backend drafted (xlsx reader, templates, preview, error report) |
| 7 (g) | Stock counts | Not started |
| 8 (h) | Partial purchase receiving | Waiting: stop here and show the backfill report from the copy run |
| 9, 10 (i, j) | After the owner's OK on item 8 | Not started |

## Notes
- Test database: `max_connections` raised to 200 in `internal/testdb` (the isolation suite ran out of connections at the default 100 once items 4 and 5 added test shops). Test setup only.
- Dev stack: running; platform migrations at 14. The dev database has no shops (create one in the console to check in the browser).
- govulncheck: 1 Go standard library finding (GO-2026-6859); the same on `main`, not caused by this work.
- Temporary worktree `D:/pos_baseline` (detached `main`, used for baseline test runs) to be removed at the end.

## Next step
Item 6: move the drafted backend in (xlsx reader and multi-sheet writer, A12 templates, A13 .xlsx import with preview, A14 error report), tests, docs, deploy.

## Decisions to confirm
- Item 5: the label PDF is drawn with gofpdf directly (the library maroto wraps) instead of maroto's row layout. maroto starts new pages through gofpdf's automatic page break, and a full label sheet that ends on the bottom margin sometimes added a blank page. Barcodes still come from boombuler/barcode; no new dependency.
