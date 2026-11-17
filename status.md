# Industry Phase 1 build status

Branch `industry/phase1` (local only, not pushed). Design: `docs/INDUSTRY_PHASE1_DESIGN.md`.
How to check each item: `docs/UI_CHECKLIST.md`. Last updated: 2026-11-17.

| Item | What | State |
|---|---|---|
| 1 (a) | Business type, module registry, entitlements | Done, committed (`7016f97`), deployed to the dev stack |
| 2 (b) | Payments labelled "recorded" | Done, committed, deployed to the dev stack (tenant migration 00018) |
| 3 (c) | Barcode scanning (checkout, returns, purchases, scanner settings) | Done, committed, deployed to the dev stack |
| 4 (d) | Product attributes, variant matrix | In progress |
| 5 (e) | Barcode generation and label PDF | Not started |
| 6 (f) | Import templates | Not started |
| 7 (g) | Stock counts | Not started |
| 8 (h) | Partial purchase receiving | Waiting: stop here and show the backfill report from the copy run |
| 9, 10 (i, j) | After the owner's OK on item 8 | Not started |

## Notes
- Dev stack: running; platform migrations at 14. The dev database has no shops (create one in the console to check in the browser).
- govulncheck: 1 Go standard library finding (GO-2026-6859); the same on `main`, not caused by this work.
- Temporary worktree `D:/pos_baseline` (detached `main`, used for baseline test runs) to be removed at the end.

## Next step
Item 4: migration 00019 (attribute_defs, product attributes, variant dims, article no, sale line variant label), attributes API (A6), product changes (A7), matrix (A8), permissions P2, attribute presets, UI.
