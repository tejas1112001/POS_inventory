# Industry Phase 3 (pharmacy) — Design note

Status: **draft 2026-11-18, not approved.** No code until the owner approves the changes listed in §14 (DB, P, A, E) and the open questions in §13.

Based on:
- `docs/INDUSTRY_FIT_PLAN.md` §5.4 (pharmacy) and Phase 3, and owner decision D6 (day one: batch and expiry, FEFO, expired stock blocked, alerts and reports; MRP control, prescriptions and scheduled drugs out until a pharmacist and a lawyer review them).
- The code on branch `industry/phase1` at `628d7a6` (Phase 1 items 1–10 built).
- Owner answers given on 2026-11-18 (Q8–Q12, §13).

Numbering continues from Phase 1 so nothing clashes: **DB25…**, **P7…**, **A23…**, **E3**, **Q8…**.

## Rules for every item (same as Phase 1)

- **Other shops keep working unchanged.**
  - Everything is behind a new module, `batch_expiry`, and a per-product switch. Both are off unless a shop turns them on.
  - Every new column has a default that reproduces today's behaviour.
  - The existing Go, isolation and browser tests must pass unchanged.
- **Server checks first.** The UI only hides what the server refuses. Every change writes an audit row.
- **Tests for every new endpoint:**
  - tenant isolation (generic `404` across shops, added to the isolation and security matrices);
  - permission (refused without it);
  - module (`403 MODULE_OFF` when off).
- **Every new table** joins `tenantio.Tables` or `tenantio.Excluded`, and the export and restore tests cover it.
  - Batch ledgers are append-only (`db.AppendOnlyTables`).
  - No foreign-key cycles (export/restore refuses them; see Phase 1 DB20).
- **Migrations**: expand-only, tenant 00025 onwards and platform 00015 onwards. No backfill job is needed (§1.3).
- **After each item**:
  - Backend: lint and builds, backend and isolation tests.
  - Rebuild the dev stack.
  - A "How to check" list in `docs/UI_CHECKLIST.md`, and the API in `docs/API_CONTRACT.md`.
  - Local commits only. **No push or merge without the owner's OK.**

## 0. What changes for whom

| Shop | What it notices |
|---|---|
| Retail, clothing, footwear (module off) | Nothing. |
| New pharmacy shop | `batch_expiry` is switched on with the pharmacy preset. Each product chooses whether it tracks batches; new products in a pharmacy default to tracking. |
| Pharmacy created during Phase 1 | Nothing until the owner turns the module on (Settings → Business). |

**Units (Q8): whole packs only in Phase 3 (3a).**
- Stock stays in whole units of the pack the shop buys and sells, as today.
- A product sold both as a strip and as a box is two products until Phase 3b adds pack conversion.

---

## 1. Batches and expiry: module, batch records, stock per batch

**What**
- A product can **track batches**. Each batch has:
  - batch number;
  - expiry date;
  - manufacturing date (optional);
  - MRP (record only, Q9);
  - cost (optional).
- Stock of a tracked product is held per store **and per batch**.
- Untracked products work exactly as today.

**How**

*1.1 Module*
- `batch_expiry` ("Batches and expiry") joins the module registry (Phase 1 item 1).
  - Recommended for pharmacy.
  - Entitled in every plan (E3).
  - Can be switched off per shop by a console override.
- All batch routes are in the module.
- With the module off:
  - tracked products behave as untracked;
  - batch fields in sale and purchase bodies are refused with `403 MODULE_OFF`;
  - nothing is deleted.

*1.2 Expiry is a date, and "expired" is worked out, never stored*
- Expiry is entered as `YYYY-MM-DD` or `MM/YYYY`. `MM/YYYY` means the last day of that month, as printed on Indian packs ("EXP 03/2027" = 31 March 2027).
- A batch is **expired** when its expiry date is before today in the shop's time zone.
- Because nothing is stored, there is no nightly job that could run late and let an expired pack be sold.
- A stored **status** covers what is not a date:
  - `available`;
  - `recalled` (§7).

*1.3 Switching tracking on and off (Q10: sell with a warning)*
- **Product switched on (or the module turned on) while it has stock:**
  - Its current stock in each store goes into one **unknown batch** (`UNBATCHED`, no expiry). This happens in the same transaction, so no migration backfill is needed.
  - The unknown batch **can be sold, with a warning at checkout** ("No batch or expiry recorded for this stock — check the pack").
  - The dashboard flags products with unbatched stock until a batch stock count (§8) splits it into real batches.
- **Product switched off:**
  - Its batch stock is closed. Batch rows and their history stay.
  - Its stock carries on as a plain quantity.
- **Switched on again:** the stock then on hand goes into the unknown batch again.
- The same rules apply when the whole module is turned off or on. This keeps the batch totals right without trying to replay sales made while tracking was off.

*1.4 The one rule that keeps stock right*
- For every tracked product, store by store: **today's stock quantity (`inventories.qty`) = the sum of its batch quantities.**
  - Quarantined units (§6) are kept separately and are not in either side.
- Every movement changes both, in one transaction, under the same locks.
- The existing ledger reconciliation (the `stock.ledger_mismatch` check) gains this rule and reports any shop where it does not hold.

*1.5 Locking*
- Stock rows are locked as today (`inventory.Lock`: inventory rows in store, variant order).
- Then the batch stock rows they need, `FOR UPDATE`, in (store, batch id) order.
- Every path (sale, return, exchange, receipt, count, write-off) uses this one order, so two tills can never deadlock or both sell the last pack of a batch.

**Data**
- **DB25** `settings.modules` check gains `batch_expiry`. The pharmacy preset recommends it.
- **DB26** `products.tracks_batches boolean NOT NULL DEFAULT false`.
- **DB27** table `batches`:
  - `id`, `variant_id` → `product_variants`;
  - `batch_no citext` (1–40 characters);
  - `expiry_date date` (NULL only for the unknown batch);
  - `mfg_date date`, `mrp numeric(14,2)`, `cost numeric(14,2)`;
  - `is_unknown boolean`;
  - `status` (`available` | `recalled`);
  - `recalled_at`, `recall_reason`;
  - `created_by`, timestamps.
  - Constraints: `UNIQUE (variant_id, batch_no)`; at most one unknown batch per variant.
  - The same batch number arriving again with a different expiry is refused, because it is a typing error.
- **DB28** table `batch_stock`:
  - `store_id`, `batch_id`;
  - `qty integer >= 0` (sellable side, counted in `inventories.qty`);
  - `quarantined_qty integer >= 0`;
  - `UNIQUE (store_id, batch_id)`.
- **DB29** table `batch_movements`, **append-only**:
  - `store_id`, `batch_id`;
  - `bucket` (`stock` | `quarantine`);
  - `delta <> 0`, `qty_after`;
  - `reason`, `ref_type` / `ref_id`;
  - `user_id`, `impersonation_id`, `created_at`.
  - Every batch quantity change writes one row, like `stock_movements`.

**Permissions**
- **P7** `batches:view`: billers too, because checkout shows batches.
- **P8** `batches:manage`: correct batch details, switch tracking on products, release quarantine. Admin.

**API**
- **A23** `GET /api/batches?variantId&storeId&status&expiringWithinDays&includeEmpty` (`batches:view`) → batches with stock per store and an `expired` flag.
- `GET /api/batches/{id}` → the batch, stock per store, its movements.
- **A24** `PUT /api/batches/{id}` (`batches:manage`): correct the batch number, expiry, mfg date, MRP or cost, with a reason; audit `batch.edit`.
  - The receipts of sales already made keep the details they were sold with (A27 snapshot).
- **A25** `PUT /api/products/{id}` and the product form accept `tracksBatches` (`batches:manage`, module on), applying §1.3.
  - Product JSON gains `tracksBatches` and `batchStock` (sellable, quarantined, unbatched, nearest expiry).

**Risks**
- **Wrong expiry typed at receiving.** Mitigations:
  - the `MM/YYYY` shortcut;
  - a confirmation for dates more than 10 years ahead;
  - the correction endpoint, which is audited.
- **Checkout is a little slower** because of the extra row locks. Measured in the oversell and deadlock tests (§12).

**Tests**
- The rule in §1.4 after every kind of movement.
- Switching tracking on, off and on again with stock in two stores.
- Module off: tracked products sell as untracked; batch fields are refused.
- Isolation, permissions and module checks for A23–A25.

---

## 2. Receiving with batch and expiry (Q11: refuse expired, warn near)

**What**: the Receive goods screen (Phase 1 item 8) asks for batch number, expiry, mfg date (optional), MRP (optional) and quantity for each line of a tracked product. One line can be split into several batches.

**How**
- `POST /api/purchases/{id}/receipts` lines gain `batches: [{batchNo, expiry, mfgDate?, mrp?, quantity}]`. The batch quantities must add up to the line's quantity.
- An existing batch number for that product is reused only if its expiry matches; otherwise `400 lines[i].batches[j].expiry`.
- **An expiry already passed is refused** (`400`, "This batch has expired").
- **A batch expiring within the shop's warning window** (`nearExpiryDays`, default 90) is refused with `409 NEAR_EXPIRY`, with details of each batch. Sending again with `acceptNearExpiry: true` saves it. The UI shows a confirmation, and the audit records the acceptance.
- Cost per batch defaults to the purchase line's cost.
- **Scanning:**
  - the product barcode picks the line as today;
  - the batch number and expiry are typed;
  - reading batch and expiry from 2D GS1 codes on packs is Q17.
- **Receiving everything in one click** (Phase 1 A17) is refused for a purchase with tracked lines (`409`, "Enter batch and expiry for each line"), since batch details are needed.
- **Labels:** after a receipt, the label window (Phase 1 item 5) can print batch and expiry on labels.

**Data**: **DB30** table `purchase_receipt_batches` (`receipt_item_id` → `purchase_receipt_items`, `batch_id`, `quantity > 0`).

**API**: **A26** the receipts endpoint above. Receipt JSON lines gain `batches[{batchId, batchNo, expiry, mrp, quantity}]`.

**Tests**
- Split line; batch reuse; expiry mismatch.
- Expired refused; near expiry `409` then accepted; the audit row.
- Totals must add up.
- Partial receipts still update `received_qty` as in item 8.
- Concurrency, as in item 8.

---

## 3. Selling: earliest expiry first, expired blocked (Q12: any cashier may pick, logged)

**What**: at checkout each line of a tracked product is filled from its batches, earliest expiry first (FEFO). The cashier sees the batch and expiry, and may pick another sellable batch. Expired and recalled batches can never be sold.

**How**
- **Sellable batch:** status `available`, not expired, and at least `minDaysToSell` days before expiry (setting, default 0), with stock in the sale's store.
- **The server allocates** in this order: earliest expiry, then oldest batch. The unknown batch comes last (Q14) and adds a warning.
  - A line may be split across batches.
  - The quote endpoint returns the allocation and warnings, so the till shows them before payment.
- **The cashier may send `batches: [{batchId, quantity}]` on a line** to choose. The server checks each chosen batch is sellable and has the stock.
  - No permission is needed (Q12).
  - The sale line records `chosen = true`, and the batch report shows chosen versus FEFO.
- **Errors:**
  - `409 BATCH_NOT_SELLABLE` (expired, recalled, too close to expiry), with details;
  - `409 INSUFFICIENT_STOCK` when sellable batches don't cover the quantity, even if the plain total would. Details say how much is unsellable and why.
- **Void** returns each unit to the batch it came from.
- **Edit of a sale with tracked lines is refused** (`409`, "Return or void the sale instead") (Q13). Edit restocks and resells, which would have to re-allocate batches the customer already has. Returns and exchanges (Phase 1 item 9) cover the need.
- **The receipt** shows "Batch B123 · Exp 03/2027 · MRP 45.00" under the line. These details are copied onto the sale at sale time, like HSN in Phase 1 item 10.

**Data**: **DB31** table `order_item_batches`:
- `order_item_id`, `batch_id`, `quantity > 0`;
- `chosen boolean`;
- snapshot of `batch_no`, `expiry_date`, `mrp`.

**API**: **A27**
- `POST /api/sales`, the quote, and the exchange's new lines: lines gain optional `batches`.
- Sale JSON lines gain `batches[{batchId, batchNo, expiry, mrp, quantity, chosen}]`, and `warnings[]` (unbatched stock).

**Tests**
- FEFO across 3 batches and a split line.
- Expired (yesterday and today, at the shop's time-zone boundary), recalled, and `minDaysToSell`.
- Chosen batch: valid, unsellable, short.
- Unknown batch sold last, with a warning.
- Void back to the same batches.
- Edit refused.
- Receipt shows the snapshot even after A24 changes the batch.
- Oversell and deadlock tests with 10 concurrent tills on one batch, and with opposite order on two batches.

---

## 4. Returns and exchanges per batch, quarantine

**What**: returned goods go back to the batch they were sold from. Unsafe goods go to **quarantine**, which is counted but cannot be sold.

**How**
- **A linked return line** goes back to the batches recorded on the sale line (`order_item_batches`), latest-expiry batch first, never more than was sold from each.
- **Where the units go:**
  - resellable, and the batch is still sellable → batch stock (sellable);
  - otherwise (damaged or defective condition, or the batch expired or recalled) → the batch's **quarantine**.
- For tracked products the **condition is always asked**, even if the shop's return policy leaves conditions off. This only changes returns of tracked products.
- **An unlinked return** (already needs `sales:price_override`) must name the batch, or it goes to quarantine under the unknown batch.
- **Exchanges** (Phase 1 A20) use the same rules for the returned goods, and §3 for the new goods.

**Data**:
- **DB32** table `return_item_batches` (`return_item_id`, `batch_id`, `quantity`, `bucket` = `stock` | `quarantine`).
- **DB33** stock ledger reasons gain `quarantine` and `quarantine_release`.
  - Moving units between sellable and quarantine changes `inventories.qty`, so it is on the main ledger too.

**API**
- **A28** Return and exchange JSON lines gain `batches[{batchId, batchNo, expiry, quantity, bucket}]`.
- Unlinked return lines accept `batchId`.
- **A29** `POST /api/batches/{id}/quarantine` `{storeId, quantity, reason}`: found damaged on the shelf.
- **A29** `POST /api/batches/{id}/release` `{storeId, quantity, reason}`: back to sale. Refused for an expired or recalled batch.
- Both need `batches:manage`, and both are audited.

**Tests**
- Return to the original batches.
- Expired batch → quarantine.
- Damaged → quarantine.
- Release refused when expired.
- The rule in §1.4 holds throughout.
- Phase 1 return and exchange tests pass unchanged for untracked products.

---

## 5. Write-offs and recalls

**What**
- **Write off** units that leave the shop: expired, damaged, recalled, returned to supplier, or other.
- **Recall** a batch: it stops selling at once, and a report shows where it went.

**How**
- **Write-off:**
  - A document `WO-000001` with store, reason, note and lines (batch, quantity, from sellable stock or from quarantine).
  - Posting takes the units out (ledger reason `write_off`).
  - Audit `write_off.create`.
- **Returned to supplier** is only a write-off reason in Phase 3. A supplier return with a debit note is later (Phase 1 Q2).
- **Recall:**
  - Sets the batch status to `recalled` with a reason.
  - Checkout refuses it immediately, and its stock can only be written off or kept in quarantine.
  - **Lifting a recall** needs the same permission and a reason, and is audited.
- **Trace report:** for one batch, every sale with date, customer (named customers only), quantity and store, plus returns and write-offs. This is what a pharmacy needs for a supplier or regulator recall.

**Data**: **DB34** tables `write_offs` and `write_off_lines`, plus the doc counter `write_off`. Stock ledger reason `write_off`.

**Permissions**
- **P9** `batches:write_off`: admin.
- **P10** `batches:recall`: owner and admin.

**API**
- **A30** `POST /api/write-offs` (idempotent); `GET /api/write-offs`; `GET /api/write-offs/{id}`.
- **A31** `POST /api/batches/{id}/recall` and `/lift-recall` `{reason}`.
- **A31** `GET /api/batches/{id}/trace`.

**Tests**
- A write-off from stock and from quarantine; it can't take more than is there.
- Recall blocks a sale that was in flight (concurrency).
- The trace report lists only this shop's sales.
- Permissions and isolation.

---

## 6. Stock counts per batch

**What**: the stock count (Phase 1 item 7) counts tracked products **by batch**. The same screen turns unbatched stock into real batches.

**How**
- A count line for a tracked product names a batch: pick an existing one, or type batch number and expiry for a batch found on the shelf.
- **Completing the count:**
  - posts each batch's difference;
  - a counted unknown batch normally goes to 0, as its units were counted under their real batches;
  - the product's total moves by the sum of the differences, as today.
- Expired batches found on the shelf can be written off from the count result in one step (a write-off document, §5).

**Data**: **DB35** `stock_count_lines.batch_id` (nullable). The unique key becomes `(count_id, variant_id, batch_id)`, where no batch counts as one value.

**API**: **A32** count lines accept `batchId`, or `batchNo` + `expiry`. Count JSON lines gain them.

**Tests**
- Count splits the unknown batch into three real batches; §1.4 holds.
- A batch found on the shelf that the system doesn't know.
- Untracked lines unchanged.

---

## 7. Expiry reports, dashboard and alerts

**What**: the pharmacy sees what is about to expire and what already has, in time to sell it first, return it or write it off.

**How**
- **Expiry report** (Reports → Expiry), filters by store and window (30 / 60 / 90 days, or custom):
  - expired on hand;
  - expiring in the window;
  - quarantined;
  - recalled with stock.
  - Shows quantity, cost value and MRP value. Export as CSV and Excel, like the other reports.
- **Dashboard tile** (pharmacy, module on): expired on hand, expiring within the window, unbatched stock, quarantined. Each opens the report.
- **Daily email digest** (optional, off by default):
  - to the owner, through the existing mail outbox and its limits;
  - only on days with something to report.
- **Settings → Batches:**
  - `nearExpiryDays` (default 90; also the receiving warning, §2);
  - `minDaysToSell` (default 0, Q16);
  - `expiryDigest` (off).

**Data**: **DB36** `settings.near_expiry_days int NOT NULL DEFAULT 90` (1–730), `min_days_to_sell int NOT NULL DEFAULT 0` (0–365), `expiry_digest boolean NOT NULL DEFAULT false`.

**API**
- **A33** `GET /api/reports/expiry`.
- **A33** dashboard fields under `batches`.
- **A34** `GET/PUT /api/settings/batches` (`settings:view` / `settings:edit`).

**Tests**
- The report's time-zone boundary.
- The digest goes once per day, and not on empty days.
- Report totals equal the batch rows.
- Isolation.

---

## 8. Pharmacy import template (opening stock)

**What**: the spreadsheet import (Phase 1 item 6) gets a **pharmacy** template:
- name, SKU, barcode, HSN, tax, price, cost;
- **batch number, expiry, mfg date, MRP, quantity, store**.
- One row per batch; several rows with the same SKU are several batches.

**How**
- Same preview-then-save flow and error report as item 6.
- Expired rows are reported and not imported (Q11).
- Near-expiry rows are imported and listed as warnings.
- Products are created with tracking on.

**Data**: none (validation in code). **API**: **A35** template `pharmacy` in the existing import endpoints.

**Tests**
- Multi-batch product.
- Bad dates and `MM/YYYY`.
- Expired row refused.
- Totals per store equal the batch sum.

---

## 9. What Phase 3 does not do (out or later)

| Topic | Where it goes |
|---|---|
| Pack / strip / tablet conversion | **Phase 3b** (Q8), after the pharmacy answers §11 |
| MRP control (block above MRP, discount rules) | After legal review. Phase 3 only records MRP (Q9) |
| Prescriptions, scheduled drugs (H / H1 / X), registers, pharmacist dispense sign-off | After the pharmacist and lawyer answer §11 (D6) |
| Drug licence number on the invoice | Same review. A free-text receipt footer works meanwhile |
| Supplier returns with debit notes | Later (Phase 1 Q2). Phase 3 writes off with reason "returned to supplier" |
| Transfers of batches between stores | Later (D5) |
| Reading batch and expiry from 2D (GS1 DataMatrix) pack codes | Q17 |

## 10. Build order and stop points

1. Item 1 (module, batches, batch stock, switching, reconciliation).
   - **Stop:** run the reconciliation and the switch-on on a copy of the dev database and add the report here, as for Phase 1 item 8.
2. Item 2 (receiving).
3. Item 3 (selling).
4. Item 4 (returns, quarantine).
5. Item 5 (write-offs, recalls).
6. Item 6 (counts).
7. Item 7 (reports, alerts).
8. Item 8 (import).

Each item follows the routine at the top.

## 11. Questions for the pharmacy, the pharmacist and the lawyer (for Phase 3b)

**For the pharmacy (units, Q8)**
1. Do you sell loose tablets, or only whole strips and boxes?
2. Do you buy in boxes? How many strips per box and tablets per strip, typically? Does it vary by product?
3. Is the strip price always the box price divided by the strips, or set separately?
4. Do your packs carry 2D codes (GS1 DataMatrix) with batch and expiry? Does your scanner read them? (Q17)

**For the pharmacist and the lawyer**
1. MRP: must the system refuse a price above MRP? Are discounts below MRP limited?
2. Which medicine schedules must be recorded (H, H1, X, narcotics)? What does each register need (patient, prescriber, prescription number, quantity, batch)?
3. Must a prescription image be stored? For how long, and who may see it?
4. Who may dispense scheduled medicines? Must a registered pharmacist confirm each sale in the system?
5. What must the invoice show (drug licence numbers, pharmacist name, batch, expiry)?
6. How long must sales, batch and register records be kept?

## 12. Tests across the phase

- The rule in §1.4 is checked after every endpoint in the item tests and by the reconciliation job.
- **Concurrency:**
  - 10 tills selling the last units of one batch;
  - opposite lock order on two batches (no deadlock);
  - a recall during a sale;
  - receiving while selling.
- Export and restore with batch tables; append-only grants on `batch_movements`.
- Module off, permission and isolation tests for A23–A35.
- All Phase 1 tests unchanged.

## 13. Owner answers and open questions

| # | Question | Answer |
|---|---|---|
| Q8 | Pack / strip / tablet units in Phase 3? | **Whole packs only (3a); conversion in 3b.** Answered 2026-11-18. |
| Q9 | MRP? | **Record per batch, show on screens and receipts; no enforcement** until legal review. Answered 2026-11-18. This changes D6 (which left MRP out). |
| Q10 | Existing stock with no batch? | **Sell with a warning**; flagged on the dashboard until a batch count. Answered 2026-11-18. |
| Q11 | Receiving expired or near-expiry goods? | **Refuse expired; warn and confirm near expiry** (window `nearExpiryDays`, default 90). Answered 2026-11-18. |
| Q12 | Who may pick a batch other than FEFO? | **Any cashier; recorded on the sale line.** Expired, recalled and quarantined stay blocked. Answered 2026-11-18. |
| Q13 | Editing a sale with tracked lines? | Proposed: **refused**; use return, exchange or void. |
| Q14 | Where the unknown batch sits in FEFO? | Proposed: **last**, with a warning. |
| Q15 | Is `batch_expiry` in every plan (E3)? | Proposed: **yes**, like Phase 1 E1; no price change. |
| Q16 | Default "do not sell within N days of expiry"? | Proposed: **0** (only expired is blocked); the shop can raise it. |
| Q17 | 2D GS1 code scanning for batch and expiry? | Proposed: **later**, after the pharmacy's answer in §11. |

## 14. Every change for a yes/no

| # | Item | Change |
|---|---|---|
| DB25 | 1 | `settings.modules` allows `batch_expiry`; pharmacy preset recommends it |
| DB26 | 1 | `products.tracks_batches` (default false) |
| DB27 | 1 | new `batches` |
| DB28 | 1 | new `batch_stock` |
| DB29 | 1 | new `batch_movements` (append-only) |
| DB30 | 2 | new `purchase_receipt_batches` |
| DB31 | 3 | new `order_item_batches` (with snapshot) |
| DB32 | 4 | new `return_item_batches` |
| DB33 | 4 | stock ledger reasons `quarantine`, `quarantine_release` |
| DB34 | 5 | new `write_offs`, `write_off_lines`; counter `write_off`; ledger reason `write_off` |
| DB35 | 6 | `stock_count_lines.batch_id`; unique key includes it |
| DB36 | 7 | `settings.near_expiry_days`, `min_days_to_sell`, `expiry_digest` |
| DB37 | 1 | platform: console override key `batch_expiry` |
| P7 | 1 | `batches:view` (admin, billers) |
| P8 | 1 | `batches:manage` (admin) |
| P9 | 5 | `batches:write_off` (admin) |
| P10 | 5 | `batches:recall` (owner, admin) |
| A23–A25 | 1 | batch list/detail, batch correction, product `tracksBatches` |
| A26 | 2 | receipts with batches; `NEAR_EXPIRY` |
| A27 | 3 | sale/quote/exchange lines with batches; `BATCH_NOT_SELLABLE`; edit refused for tracked lines |
| A28–A29 | 4 | return lines with batches; quarantine and release |
| A30–A31 | 5 | write-offs; recall, lift recall, trace |
| A32 | 6 | count lines with batches |
| A33–A34 | 7 | expiry report, dashboard fields, batch settings |
| A35 | 8 | pharmacy import template |
| E3 | 1 | `batch_expiry` entitled in every plan and the trial; no price change |
