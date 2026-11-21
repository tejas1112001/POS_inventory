# Next steps after industry Phase 3

Last updated: 2026-11-21. Phase 3 (pharmacy, items 1–8) is merged into `main` (`aea9af7`). The `industry/phase1` and `industry/phase3` branches are deleted.

## 1. Checks for the owner

- [ ] **Browser check of Phase 3.** The dev database has 9 shops; create a pharmacy shop in the console (or switch one to pharmacy), then follow the "How to check" steps for Industry P3 items 1–8 in `docs/UI_CHECKLIST.md`.
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

- [x] **Stock counts (P3 item 6):** fixed 2026-11-21. A scan of a product counted by batch is refused with 409 `BATCH_REQUIRED` (`details{variantId, productName}`) instead of landing on "No batch"; the page opens "Count a batch" for it.
- [x] **Expiry report (P3 item 7):** fixed 2026-11-21. CSV too (`?format=csv`: UTF-8 with BOM, formula-safe, no totals row), besides Excel and PDF; the other reports stay Excel and PDF.
- [x] **Stock counts (P1 item 7):** fixed 2026-11-21. With the module off, the list and a count stay readable (`modules.Module.ReadsWhenOff` names the two GET routes; every change still returns `MODULE_OFF`); the pages show a read-only notice. The nav item stays hidden while the module is off.

## 4. Housekeeping

- [x] **Dependabot and `go-backend/phase2`:** branches deleted on 2026-11-18. Dependabot opens fresh pull requests on its next run; merge them after a full test run.
- [x] **Landing branches:** the marketing site was merged into `main` (`d7ab936`); it is not deployed yet. The `landing_dev` / `landing_main` branches can be deleted on the owner's word.
