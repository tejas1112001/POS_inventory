# Next steps after industry Phase 3

Last updated: 2026-11-18. Phase 3 (pharmacy, items 1–8) is merged into `main` (`aea9af7`). The `industry/phase1` and `industry/phase3` branches are deleted.

## 1. Checks for the owner

- [ ] **Browser check of Phase 3.** The dev database has no shops. Create a pharmacy shop in the console, then follow the "How to check" steps for Industry P3 items 1–8 in `docs/UI_CHECKLIST.md`.
- [ ] **Questions for the pharmacy.** Answer design §11 in `docs/INDUSTRY_PHASE3_PHARMACY_DESIGN.md`. Several deferred topics below wait on it.

## 2. Deferred in the design

Each needs the owner's go-ahead or answers from outside. Source: design §9.

| Topic | Waiting on |
|---|---|
| Pack / strip / tablet conversion | Phase 3b (Q8), after the pharmacy answers §11 |
| MRP control (block sales above MRP, discount rules) | A legal review. MRP is recorded per batch (Phase 3, Q9) and per variant (migration 00032); selling above it is only warned about |
| Prescriptions, scheduled drugs (H / H1 / X), registers, pharmacist sign-off on dispensing | Answers from the pharmacist and the lawyer (§11, D6) |
| Drug licence number on the invoice | The same review. A free-text receipt footer works meanwhile |
| Supplier returns with debit notes | Later (Phase 1 Q2). For now, write off with reason "returned to supplier" |
| Transfers of batches between stores | Later (D5) |
| Reading batch and expiry from 2D (GS1 DataMatrix) pack codes | Q17, answered "later" |

## 3. Small known issues

- [ ] **Stock counts (P3 item 6):** for a product counted by batch, scanning adds to its "No batch" line. To count a batch, use "Count a batch".
- [ ] **Expiry report (P3 item 7):** exports are Excel and PDF like the other reports; the design said CSV and Excel.
- [ ] **Stock counts (P1 item 7):** with the stock counts module off, completed counts are hidden. The design says they should stay readable. This needs a small change to the route check from P1 item 1.

## 4. Housekeeping

- [x] **Dependabot and `go-backend/phase2`:** branches deleted on 2026-11-18. Dependabot opens fresh pull requests on its next run; merge them after a full test run.
- [ ] **Landing branches:** `landing_dev` holds the unmerged marketing site; `landing_main` only the empty skeleton. Decide whether to merge or keep them.
