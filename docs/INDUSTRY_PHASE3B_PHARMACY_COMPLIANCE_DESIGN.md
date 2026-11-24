# Pharmacy compliance (Phase 3b): implementation plan

Status: **approved by the owner 2026-11-21** (second version: the owner's five clarifications are §6.2, §6.9, §6.14, §6.15 and §12; Q-M, Q-N, Q-O approved as recommended). Step 1 (strict MRP) in progress.
Follows `docs/INDUSTRY_PHASE3_PHARMACY_DESIGN.md` §11 (questions) and §9 (deferred topics), with the owner's answers of 2026-11-21 (§1 below).

> **Legal note.** This plan describes software behaviour. The rules it follows (Drugs and Cosmetics Act 1940 and Rules 1945, the Pharmacy Act 1948, Legal Metrology rules on MRP, GST and Income-tax record keeping, the Digital Personal Data Protection Act 2023) are as understood on 2026-11-21. Every item marked **(LAW)** must be confirmed by the shop's registered pharmacist and lawyer before shops rely on it. The software records and enforces what the shop decides; it does not make a shop compliant on its own.

---

## 1. Owner decisions (2026-11-21)

| Q | Decision |
|---|---|
| Q1 | Loose tablets are sold. A medicine is kept in tablets (base unit) with strips and boxes as its units (H3, built). |
| Q2 | Shops buy in boxes; pack sizes are set per medicine (product units, built). |
| Q3 | One price per base unit; strip and box prices follow (built). |
| Q4 | 2D (GS1 DataMatrix) pack codes later; 1D barcodes and typed batch/expiry now. |
| Q5 | **Strictly prevent selling above MRP.** |
| Q6 | **Separate Schedule H1 and Schedule X workflows**, each per its legal requirements. |
| Q7 | Prescription uploads optional where the law permits; **required retention** for the medicines that need it. |
| Q8 | **A registered pharmacist role**; pharmacist approval recorded for the medicine sales that need it. |
| Q9 | The legally required drug licence and invoice details on invoices; **prescription-register information kept separately** from the invoice. |
| Q10 | **8 years** default retention, **category-specific** retention rules, and **legal holds**. |

Already built and reused: batches and expiry with earliest-expiry-first selling (P3 items 1–8), units (H3), MRP per variant and per batch, GST per store (M3), credit notes, audit log, private file storage, the module and permission system.

---

## 2. Scope

**In this phase**
1. Strict MRP (no sale above MRP).
2. Medicine schedule on each product: none, **H**, **H1**, **X**, plus a **narcotic/psychotropic** flag that blocks the sale.
3. Prescriptions captured at the sale (patient, prescriber, prescription number and date, optional image).
4. A **registered pharmacist** record and role; pharmacist approval of scheduled sales.
5. **Schedule H1 register** and **Schedule X register**, each its own workflow, view and export; kept apart from invoices.
6. Invoice details: drug licence numbers per store, pharmacist, patient and prescriber of scheduled lines.
7. Retention policies per record category (default 8 years) and **legal holds**; the platform's shop deletion respects both.
8. Returns, voids and exchanges of scheduled medicines keep the registers correct (reversal entries, never deletion).
9. Supplier drug licence numbers (purchase records come from licensed suppliers).

**Not in this phase** (later or out)
- Narcotic and psychotropic substances (NDPS Act): flagged and **blocked**, not sold through the app.
- Reading 2D pack codes (Q4).
- E-pharmacy / online orders of medicines (the public shop shows a catalogue only; §6.10).
- Supplier returns with debit notes, batch transfers between stores (already deferred, §9 of the Phase 3 note).
- Filing or printing government forms beyond the registers and invoices described here.

---

## 3. Concepts

| Concept | Meaning |
|---|---|
| **Schedule** | A product's legal class: `none`, `H`, `H1`, `X`. Set on the product (per product, all its variants). `narcotic` is a separate flag. |
| **Scheduled line** | A sale line of a product whose schedule is H, H1 or X. |
| **Prescription** | The prescription a scheduled sale is made against: patient (name, age, address), prescriber (name, registration number, clinic/hospital address), prescription number and date, optional image. One prescription can cover several lines of one sale. |
| **Registered pharmacist** | A user linked to a pharmacist record: name, registration number, State Pharmacy Council, valid until, the stores they work at. Only an active, valid record can approve. |
| **Pharmacist approval** | A pharmacist's confirmation of a sale's scheduled lines, by their own sign-in or PIN at the till. Recorded with the sale (who, when, which lines, which store). |
| **Register entry** | An append-only row in the **H1 register** or the **X register** for each scheduled line sold (and a reversal row for each return or void). Never edited or deleted; a correction is a new entry pointing at the old one. |
| **Retention policy** | How long each category of record must be kept (default 8 years, never less than the legal minimum). |
| **Legal hold** | A flag that stops any deletion of a shop's data (and of the platform's archive of it) until released. |

---

## 4. Data (tenant migration unless marked platform)

| ID | Change |
|---|---|
| DB1 | `products.drug_schedule text` (`H`, `H1`, `X`; null = none) and `products.narcotic boolean NOT NULL DEFAULT false`. Format checks only (list in code, as M1). |
| DB2 | `pharmacists`: id, user_id (unique), name, registration_no, council (state), valid_until, active, created_at/by. `pharmacist_stores` (pharmacist_id, store_id). |
| DB3 | `pharmacist_pins`: user_id, PIN hash (Argon2id, as passwords), failed attempts, locked_until. Optional: approval by full sign-in works without a PIN. |
| DB4 | `prescriptions`: id, store_id, patient_name, patient_age, patient_address, prescriber_name, prescriber_reg_no, prescriber_address, rx_number, rx_date, image_file_id (private file, purpose `prescription`), created_by, created_at. |
| DB5 | `sale_prescriptions` (sale_id, prescription_id) and on `order_items`: `drug_schedule` (copy at sale time) and `prescription_id`. |
| DB6 | `pharmacist_approvals`: id, sale_id (or return_id), pharmacist_id, user_id, method (`signin` / `pin`), approved_at, store_id, line ids. |
| DB7 | `register_entries`: id, register (`H1` / `X`), store_id, entry_no (gapless per register, store and financial year), entry_date, sale_id / return_id, order_item_id, product name, batch number, expiry, quantity (base units and as sold), patient, prescriber, rx number/date, pharmacist_id, kind (`sale` / `return` / `void` / `correction`), refers_to (entry id), created_at. **Append-only**: the app has no update or delete path; the table refuses updates by trigger. |
| DB8 | `stores.drug_licences jsonb`: the licence numbers of the premises (e.g. retail licence forms for general and for scheduled drugs; Schedule X licence), with valid-until dates. Shop-level fallback in settings. **(LAW: which forms)** |
| DB9 | `suppliers.drug_licence text`, `suppliers.drug_licence_valid_until date`. |
| DB10 | `retention_policies`: category, years (≥ the legal minimum in code), updated_by/at. Defaults seeded (§6.9). |
| DB11 | Platform: `legal_holds` (tenant_id, reason, placed_by, placed_at, released_by, released_at); `tenant_archives` (tenant_id, export file, created_at, retain_until) for shops deleted while records are under retention. |
| DB13 | MRP with its pack size: `product_variants.mrp_pack` and `batches.mrp_pack` (base units the MRP was printed for; default 1, so today's MRPs keep their meaning). |
| DB14 | `order_items.approval_id` (FK `pharmacist_approvals`), the CHECKs and the schedule-copy trigger of §6.14. |
| DB15 | `register_entries.prev_hash`, `entry_hash`; grants and triggers making `register_entries` and `pharmacist_approvals` append-only (§6.15). |
| DB16 | Platform: `tenant_exports.retain_until`, `second_copy_key`, `legal_hold` check; `archive_access_requests` (tenant, requested_by, approved_by, reason, opened_at) for §6.9. |
| DB17 | `prescriptions.repeats_allowed`, `uses`, `paper_copy_filed` (§6.5). |
| DB12 | Settings: `pharmacy` module settings: require PIN or sign-in for approval; whether H needs a prescription record (default yes, prescriber name at least); Schedule X image required (default **yes**, see Q-B). |

All new tenant tables join the tenant export (`tenantio.Tables`). Register entries, approvals and prescriptions are restored exactly (ids and numbers kept).

---

## 5. Permissions, roles and module

- **Module** `pharmacy_compliance` (depends on `batch_expiry`), recommended for the pharmacy business type; entitlement through plans as other modules. With the module off, existing records stay **readable** (as stock counts now do) and scheduled medicines **cannot be sold**.
- **New permissions**: `pharmacy:approve` (approve scheduled lines), `pharmacy:pharmacists` (manage pharmacist records), `registers:view`, `registers:export`, `prescriptions:view` (open patient details and images), `retention:manage` (owner).
- **New default role `pharmacist`**: POS selling, returns, `pharmacy:approve`, `registers:view`, `prescriptions:view`. Having the permission is **not enough** to approve: the user must also have an **active pharmacist record valid today for that store**.
- **Owner**: everything above plus `registers:export`, `pharmacy:pharmacists`, `retention:manage`. Cashiers (biller): sell, but cannot approve or open prescriptions after the sale.
- **Platform (Super Admin)**: place and release legal holds; see that a shop is under hold or retention; never read registers or prescriptions (no tenant data access, as today).

---

## 6. Workflows and business rules

### 6.1 Set-up (owner)
1. Turn on **Pharmacy compliance** (Settings → Modules).
2. Add each **store's drug licence numbers** (Stores page). A store without a valid general licence cannot sell scheduled medicines; without a valid Schedule X licence it cannot sell Schedule X **(LAW)**.
3. Add **pharmacists**: link a user, enter registration number, council, valid-until, stores; the pharmacist sets their PIN.
4. Mark **medicine schedules**: on the product form, or in bulk with the import's new `schedule` column (H, H1, X) and `narcotic` (yes/no). A product list filter shows unclassified medicines.
5. Check **retention** (Settings → Records): defaults shown, can only be raised above legal minimums.

### 6.2 Strict MRP (Q5) — every shop, not only pharmacies

**Rule.** No sale line may cost the customer more than its MRP. "Cost" is the line's amount **with tax and after every discount** (line discount, its share of the bill discount, coupons); "MRP" is the MRP of the exact stock sold.

**Where the MRP comes from**
- A medicine's MRP is printed **per pack** (a strip of 10, 15 or 30 tablets, a bottle). Each MRP is therefore stored **with the pack size it was printed for** (DB13): "₹35.00 per 10 tablets", not a rounded per-tablet price. This avoids rounding errors such as ₹25.00 for 15 tablets = ₹1.6666… a tablet.
- A batch's MRP (entered at receiving) wins over the variant's. Receiving a medicine batch **without an MRP is refused** in a pharmacy shop (other shops: optional, as today).
- A line drawn from several batches (earliest expiry first) gets the **sum of each batch's MRP for the quantity taken from it**.

**The check, exactly**
- Line MRP = Σ over batches of (base quantity from the batch × pack MRP ÷ pack size), computed exactly (no rounding), then **rounded down** to the paisa.
- Refuse when the line's amount (in paise) is greater than the line MRP. Rounding down means the customer can never pay even a fraction of a paisa above MRP.
- **Bill round-off** (to the rupee): it may never take the bill above the sum of its lines' MRPs; when rounding up would, the bill is rounded down instead.
- A medicine with **no MRP at all** (no batch MRP, no variant MRP) cannot be sold in a pharmacy shop; other shops sell it without the check, as today.

**Examples** (tablet is the base unit; strip MRP ₹35.00 per 10 tablets; box of 10 strips)

| Sold | Line MRP | Allowed | Refused |
|---|---|---|---|
| 1 tablet | ₹3.50 | ₹3.50 | ₹3.51 |
| 3 tablets | ₹10.50 | ₹10.00 | ₹10.51 |
| 1 strip | ₹35.00 | ₹35.00 | ₹35.01 |
| 1 box (100 tablets) | ₹350.00 | ₹330.00 (discount) | ₹350.01 |
| 1 tablet of a 15-tablet strip at ₹25.00 | ₹1.66 (₹1.6666… rounded down) | ₹1.66 | ₹1.67 |
| 1 strip: 6 tablets from batch A (₹35.00/10) + 4 from batch B (₹38.00/10) | ₹21.00 + ₹15.20 = ₹36.20 | ₹36.20 | ₹36.21 |
| Price ₹33.00 + 12% GST added on top (tax-exclusive) | ₹35.00 | — | ₹36.96 |
| Strip at ₹40.00, then 15% bill discount | ₹35.00 | ₹34.00 | — |

**Every path is checked on the server**, in the one shared pricing step (`price`/`resolve` in `sales/checkout.go`), so no screen can skip it: the new checkout, the old POS screen (it calls the same `POST /api/sales`), quotes (refusal shown before Pay), edits of a sale, exchanges' new lines, recalled held carts (re-priced at recall), and price overrides. Saving a **store price** or a variant price above MRP is refused (today: a warning).

**Before the rule turns on:** a one-off report lists every variant, batch and store price above its MRP; the owner fixes them (the migration changes no prices). The error is 409 `ABOVE_MRP` with each line's amount and MRP.

### 6.3 Selling a Schedule H medicine
- At **Pay**, the till asks for the prescription: prescriber name (required), registration number, patient name, prescription number/date (optional), image (optional).
- **Pharmacist approval** required (§6.6).
- No H1/X register entry. The sale still appears in the **prescription register** report (§6.8), which lists every prescription sale (H, H1, X) with prescriber, patient, medicine, quantity and pharmacist, because Rule 65 asks for prescription supplies to be recorded (in a register or on the cash memo) **(LAW: §12)**.
- An H prescription is dispensed once unless the prescriber wrote repeats (as X).

### 6.4 Selling a Schedule H1 medicine (its own workflow)
- Prescription **required**: patient name and address, prescriber name and address/registration number, prescription number or date, the medicine and quantity (from the line). Image optional (Q7).
- **Pharmacist approval** required.
- On payment, in the same transaction as the sale: one **H1 register entry** per H1 line (entry number gapless per store and year; product, batch, expiry, quantity, patient, prescriber, prescription, pharmacist).
- The H1 register is kept **separately** from invoices (Q9): its own pages, numbers, export; the invoice only marks the line "Schedule H1".
- Retention: **at least 3 years** for the register (Drugs and Cosmetics (Fourth Amendment) Rules 2013, GSR 588(E); §12), default 8 (Q10).

### 6.5 Selling a Schedule X medicine (its own workflow, stricter)
- Only at a store with a **valid Schedule X licence** **(LAW)**.
- Prescription **required**, with the image **required** by default (Q-B): the shop must keep the prescription; a stored copy proves it.
- **Pharmacist approval** required, by **full sign-in**, not PIN (Q-C).
- One **X register entry** per X line, separate numbering from H1.
- Quantity limit per prescription: not more than prescribed (the prescribed quantity is entered and checked) **(LAW)**.
- The prescription is written **in duplicate**; the shop keeps one paper copy (Rule 65(9)(a)). The till asks the pharmacist to confirm "paper copy filed"; the image is the backup.
- A Schedule X prescription is dispensed **once**, unless the prescriber wrote a number of repeats: the prescription records the repeats allowed and the system refuses a use beyond them.
- Retention: **at least 2 years** for the kept prescription copy (Rule 65(9)(a); §12), default 8. The X register follows the same minimum until the lawyer confirms otherwise.

### 6.6 Pharmacist approval (Q8)
- Shown at **Pay** when the cart has scheduled lines: "Pharmacist approval needed".
- If the signed-in user is a pharmacist valid for this store: one tap "I approve" (re-enter PIN or password for X).
- Otherwise a pharmacist approves **on the same till** with their PIN (H, H1) or a full sign-in (X); the cashier's session continues. The approval is tied to this sale only.
- Failed PINs lock the pharmacist's PIN after 5 attempts for 15 minutes (as sign-in throttling).
- The server checks: the approver has `pharmacy:approve`, an active pharmacist record valid today for this store, and approved these exact lines (a changed cart needs approval again).
- Recorded: `pharmacist_approvals` + the sale's audit entry. The invoice prints the pharmacist's name and registration number.

### 6.7 Returns, voids and exchanges of scheduled lines
- A return of an H1/X line writes a **reversal register entry** (kind `return`) referring to the sale's entry; a void writes kind `void`. Entries are never removed.
- Returns of scheduled lines need **pharmacist approval** (the returned stock goes to quarantine unless the pharmacist marks it resellable; P3 item 4 already quarantines unsafe returns).
- An exchange's new scheduled lines go through §6.3–6.5 again (new prescription unless the same prescription covers them, H1 only; never for X).
- A sale with scheduled lines **cannot be edited** (only returned or voided), so register entries always match the invoice.
- The full trail of every one of these actions is §6.15.

### 6.8 Registers (H1, X)
- **Reports → Registers**: the H1 register, the X register and the prescription register (all prescription sales, read from the sales; not a separate table); choose register, store, period; list entries with entry number, date, medicine, batch, expiry, quantity, patient, prescriber, prescription, pharmacist, sale/return reference.
- **Export** PDF (inspection-ready, page-numbered, totals) and Excel. Audited.
- **Correction**: an owner or pharmacist adds a correction entry (reason required) that refers to the wrong entry; the wrong entry stays visible with a "corrected by" mark.
- Prescription images open from an entry only with `prescriptions:view`; each opening is audited.

### 6.9 Retention, archives and legal holds (Q10, clarification 2)

| Category | Legal minimum in code (§12) | Default |
|---|---|---|
| Sales, invoices, credit notes, payments | 6 years (GST: 72 months from the annual return's due date) | **8 years** |
| Purchases, supplier invoices | 6 years | 8 years |
| Accounts in general (Income-tax) | 6 years from the end of the assessment year (≈ 8 years) | 8 years |
| H1 register, H1 prescription copies | 3 years | 8 years |
| Schedule X prescription copies, X register | 2 years | 8 years |
| Prescription register, other records under the Drugs Rules | 2 years **(LAW)** | 8 years |
| Batch, stock and audit records | — | 8 years |

- A policy can be raised, never set below its legal minimum. Changes are audited (§6.15).
- In a live shop nothing is ever deleted (as today); the policy only decides how long **archives** of deleted shops are kept.

**What happens today when a shop is deleted** (checked in the code): the Super Admin can finalise a deletion only after the shop's **final export** succeeded and its **restore test** passed. The final export holds every table and every stored file (prescription images included), and is encrypted with `age` to a key whose private half only the `posctl` tool holds (`EXPORT_AGE_IDENTITY`); the web server cannot decrypt it, and the API never serves archives. About 30 days later the purge drops the shop's schema, its database role and its live files; **the final export is kept, with no end date**.

**What this phase adds**
1. **Retain-until.** Each final export gets `retain_until` = its date + the longest retention policy of the shop (default 8 years). A daily job deletes archives past their date, **unless a legal hold is on** (DB11). Keeping archives for ever would breach the data-protection rule of not keeping personal data longer than needed.
2. **Two copies.** The archive is copied to a second storage location (another bucket/region) with **write-once object lock** until `retain_until`, so it cannot be deleted or changed early even with storage credentials; the restore test runs on both copies yearly.
3. **Tenant isolation.**
   - One archive holds exactly one shop (`exports/<tenant-id>/`); archives are never merged.
   - Opening one means **restoring it into a new, temporary database** (never into the live database or another shop's schema), by `posctl` on the operator's machine.
   - The restored copy is dropped after use; what leaves it is only that shop's own export file, given to that shop's owner.
4. **Strict access.**
   - Opening an archive needs a written request from the shop's owner (or a court or regulator's order), a Super Admin with MFA step-up, a **second Super Admin's approval**, and the `posctl` key.
   - Each step is written to the platform audit log (who, when, why, which archive).
   - Platform staff never browse registers or prescriptions in the console (as today: no tenant data access).
5. **Key safety.** The `age` private key is backed up offline in two places; a lost key would make every archive unreadable. Rotating the key re-encrypts archives under retention.
6. **Legal holds.**
   - Placed by the shop's owner (Settings → Records) or by a Super Admin (console), with a reason.
   - While on, it blocks deleting the shop, finalising a deletion, the purge and the archive's expiry.
   - Released only by whoever placed it or a Super Admin. Every change is audited.

- **Personal data (DPDP Act):** in a live shop, after a record's retention period an owner action (with a yearly reminder) **anonymises** patient and prescriber details (names and addresses blanked) while keeping the register entry's numbers, medicines and quantities **(LAW)**. Records under a legal hold are never anonymised.

### 6.10 Online shop
- Products with schedule H1 or X are **not shown** in the public shop; H products are shown with "Prescription required, buy at the store" **(LAW: Q-F)**. Narcotic-flagged products are never shown.

### 6.11 Old POS screen
- The old POS screen cannot capture prescriptions or approvals. A sale with a scheduled line made from it is **refused** with "Use the checkout to sell prescription medicines". The new checkout is required for pharmacies (it can be turned on per shop already).

### 6.12 Invoice (Q9)
- Header: the store's **drug licence numbers** (and the shop's GSTIN, already).
- For each scheduled line: "Sch. H/H1/X", **batch and expiry** (already printed), manufacturer **(LAW: required? the product has a brand field; a manufacturer field may be needed, Q-D)**.
- Footer: **pharmacist** name and registration number; for scheduled sales, **patient and prescriber names** **(LAW)**.
- Register details (addresses, prescription numbers) are **not** on the invoice (Q9: kept separately).

### 6.13 Purchases
- A supplier's drug licence number and validity on the supplier record; receiving from a supplier with no or an expired licence shows a warning **(LAW: refuse?)**.

### 6.14 No way round the pharmacist (clarification 1)

Every route that sells, changes or returns stock was checked in the code (2026-11-21). The approval rule lives in **one server function** used by all of them, and the database refuses a scheduled sale line without an approval, so a new screen or an API caller cannot skip it.

| Path | Today | With this phase |
|---|---|---|
| New checkout → `POST /api/sales` | sells | prescription + approval required for H/H1/X lines |
| Old POS screen → the same `POST /api/sales` | sells | same check (the request has no prescription, so it is refused with "Use the checkout to sell prescription medicines") |
| Quote `POST /api/sales/quote` | prices only | reports what is needed; never approves |
| Edit `PUT /api/sales/{id}` | replaces lines | **refused** if the sale has, or would get, a scheduled line |
| Void `DELETE /api/sales/{id}` | keeps sale, restocks | pharmacist approval required; X/H1 reversal entries (§6.7) |
| Exchange `POST /api/sales/exchanges` | return + new lines | new scheduled lines need prescription + approval |
| Return `POST /api/sales/returns` | credit note | pharmacist approval for scheduled lines |
| Held carts (hold, recall) | saves a cart | a held cart never carries an approval; approval happens at Pay after recall |
| Idempotent retry of the same sale | returns the stored result | unchanged: cannot create a second sale |
| Online shop | catalogue only (no orders) | H1/X hidden, H labelled (§6.10) |
| Stock adjustment, write-off, recall, stock count of a scheduled medicine | changes stock | reason + pharmacist approval; H1/X write-offs written to the register as kind `writeoff` (stock cannot leave without a trace) |
| Module turned off | — | records readable; scheduled lines **cannot be sold** (turning it off is no way round) |
| Changing a product's schedule (X → none) | — | owner only, reason required, audited; the sale line copies the schedule **at sale time** from the product row by a database trigger, not from the request |
| Tenant import / restore | inserts rows | restores approved sales exactly; does not create sales |

**The approval itself**
- A short-lived (5 minutes), **single-use** approval token.
- Bound to the store, the pharmacist, and a hash of the exact scheduled lines (variant, quantity, unit, batch). Any change to those lines voids it.
- The server re-checks at Pay: `pharmacy:approve`, an active pharmacist record valid today for that store, and the store's licence for the schedule. A cashier's own permissions never count as approval.

**In the database (defence in depth)**
- On `order_items`: `CHECK (drug_schedule IS NULL OR approval_id IS NOT NULL)`, and for H1/X also `prescription_id IS NOT NULL`.
- A `BEFORE INSERT` trigger fills `drug_schedule` from the product, so a bug that leaves it out cannot skip the check.
- An integration test calls every route above as a cashier without approval and expects a refusal.

### 6.15 Audit trail for returns, exchanges, voids and corrections (clarification 5)

Three layers. Nothing in any of them is updated or deleted by the app.
1. **Business records stay.**
   - A void keeps the sale (status Void).
   - A return adds a sale return and a credit note.
   - An exchange links its return and its new sale.
   - Register entries are append-only. The tenant role may only insert into `register_entries`, `pharmacist_approvals` and `audit_log`; it has no update or delete grant, and a trigger refuses updates and deletes even from the owner role.
2. **Register entries.**
   - Every scheduled-line event writes one entry: `sale`, `return`, `void`, `exchange_return`, `exchange_sale`, `writeoff`, `correction`.
   - Each entry refers to the entry it reverses or corrects.
   - The register view shows the chain: sale → return → correction.
   - Each entry stores a **hash of the previous entry** (per register and store), so a gap or change made outside the app is detected. Reports → Registers shows "chain intact"; export and restore keep the chain.
3. **Audit log** (existing, append-only): who did what, when, from where, with the reason.

| Event | Who may | Reason required | Pharmacist approval | Register entry | Audit log |
|---|---|---|---|---|---|
| Sale with scheduled lines | cashier + pharmacist | — | yes | H1/X lines | yes |
| Return of scheduled lines | `sales_returns:create` + pharmacist | yes | yes | reversal | yes |
| Exchange | as return + sale | yes | yes (both sides) | reversal + new | yes |
| Void | `sales:delete` + pharmacist | yes (required for scheduled sales) | yes | reversal per line | yes |
| Register correction | owner or pharmacist | yes | — | correction entry | yes |
| Write-off / adjustment of scheduled stock | `stock_adjustments:create` + pharmacist | yes | yes | `writeoff` (H1/X) | yes |
| Schedule change on a product | owner | yes | — | — | yes (old → new) |
| Pharmacist record change, PIN reset | owner | — | — | — | yes |
| Prescription opened, register exported | `prescriptions:view` / `registers:export` | — | — | — | yes |
| Retention change, legal hold placed or released | owner / Super Admin | yes | — | — | yes (tenant or platform) |

---

## 7. API (summary)

| ID | Change |
|---|---|
| A1 | Products: `drugSchedule`, `narcotic` (form, import column, list filter). |
| A2 | `GET/POST/PUT /api/pharmacists`, `PUT /api/pharmacists/{id}/pin` (self). |
| A3 | Quote: per line `schedule`; `needs: {prescription: H/H1/X, approval: true}`; MRP check (409 `ABOVE_MRP`). |
| A4 | `POST /api/sales` (and exchanges): `prescriptions[{patient…, prescriber…, rxNumber, rxDate, imageFileId?, lines[]}]`, `approval{method, pin? | approvalToken}`. Missing → 409 `PRESCRIPTION_REQUIRED` / `PHARMACIST_APPROVAL_REQUIRED`. |
| A5 | `POST /api/pharmacy/approvals` (pharmacist sign-in on the till): returns a short-lived approval token for the cart's exact lines. |
| A6 | `GET /api/registers/{h1|x}?storeId=&from=&to=`, `/export?format=pdf|xlsx`, `POST /api/registers/{h1|x}/corrections`. |
| A7 | `GET /api/prescriptions/{id}` (audited), files purpose `prescription` (private). |
| A8 | Stores: `drugLicences`; suppliers: `drugLicence`, `drugLicenceValidUntil`. |
| A9 | `GET/PUT /api/settings/retention`; `POST/DELETE /api/settings/legal-hold`. Platform console: tenant legal hold, archive status. |

---

## 8. Screens

- **Settings → Modules**: Pharmacy compliance. **Settings → Pharmacists** (list, add, validity, stores). **Settings → Records** (retention, legal hold).
- **Stores**: drug licences. **Suppliers**: drug licence.
- **Product form / list**: schedule and narcotic; filter "Medicines without a schedule".
- **Checkout**: Rx badges on scheduled lines; at Pay a **Prescription** step (per prescription, lines it covers, image upload from camera/file) and a **Pharmacist approval** step; clear refusals (store licence, X without image, narcotic).
- **Returns**: pharmacist approval for scheduled lines.
- **Reports → Registers**: H1 and X, with export and corrections.
- **Receipt / PDF**: licence numbers, pharmacist, Rx details (§6.12).
- **Super Admin console**: legal hold and archive per shop.

---

## 9. Missing requirements and open questions

These are gaps the answers so far do not settle. Each has a recommendation; items marked **(LAW)** need the pharmacist or lawyer.

| # | Question | Recommendation |
|---|---|---|
| Q-A | Exact columns of the H1 and X registers in your state (some states publish formats). **(LAW)** | Use §6.4/6.5's columns; adjust to the state format once known. |
| Q-B | Schedule X: the law asks for the paper copy (Rule 65(9)(a)); is an image also expected? **(LAW)** | Keep the paper (recorded as filed) and require the image by default as a backup; setting to relax. |
| Q-C | Approval method: PIN for H/H1 and full sign-in for X? | Yes. |
| Q-D | Must invoices show the **manufacturer**? (The product has a brand, not a manufacturer.) **(LAW)** | Add a manufacturer field to products; print it on scheduled lines. |
| Q-E | Anonymise patient details after retention, automatically or by owner action? **(LAW, DPDP)** | Owner action with a yearly reminder; no silent automatic deletion. |
| Q-F | Online shop: hide H1/X and label H? **(LAW)** | Yes, as §6.10. |
| Q-G | Supplier with no valid drug licence: warn or refuse receiving? **(LAW)** | Warn now; refuse once suppliers' licences are entered. |
| Q-H | Is a pharmacist required to be **on shift** for every sale (not only scheduled)? **(LAW)** | No system rule; approvals cover scheduled lines. |
| Q-I | Strict MRP: also for non-medicine shops (it is the general law)? | Yes, every shop (§6.2). |
| Q-J | Who may place and release a **legal hold**: owner, Super Admin, both? | Both may place; only who placed it (or the Super Admin) releases; all audited. |
| Q-K | Archive of a deleted shop: kept up to its retain-until date (default 8 years), in two locations with object lock. Storage cost acceptable? | Yes (exports are small). |
| Q-M | Opening an archive needs two Super Admins. Is there a second Super Admin today? | **Approved:** create one before the first pharmacy shop goes live; until then the owner of the platform plus written request. |
| Q-N | Does the X register also need **receipts** (a stock register: opening, received, sold, closing per batch)? **(LAW)** | **Approved:** build the X register as a stock register (receipts from purchases included). |
| Q-O | Must the prescription be **stamped** as dispensed (shop name, date)? **(LAW)** | **Approved:** the system records it and prints a "dispensed" slip for the paper copy. |
| Q-L | Patient consent text at the till (DPDP Act): show a notice? **(LAW)** | A short notice on the prescription step and on the invoice footer. |

---

## 10. Build order (each step: code, tests, docs; stop for the owner's OK)

1. **Strict MRP** (§6.2, DB13) with the report of prices above MRP. Useful for every shop.
   - **Built 2026-11-22 (not committed; waiting for the owner's OK).**
     - Tenant migration 00046: `mrp_pack` on `product_variants` and `batches` (default 1) and on the sale copies (`order_items`, `order_item_batches`).
     - `pricing.LineMRP`: exact sum, rounded down to the paisa. `pricing.AboveMRP` checks the whole pack and one loose unit.
     - Every sale line goes through `sales.insertItems`, so sales, edits and exchanges are all checked by `checkMRP`: 409 `ABOVE_MRP`, or 409 `MRP_REQUIRED` for a pharmacy's batch-tracked product with no MRP. The quote reports `aboveMrp`/`mrpRequired` and the checkout blocks Pay.
     - Saving a variant price, a store price, or a lower MRP that would put a price above MRP is refused. The CSV import's warning became a row error, and nothing is imported.
     - In a pharmacy shop each received or imported batch needs its MRP.
     - Report: `GET /api/products/above-mrp` (Products → Prices above MRP).
     - Screens: an "MRP is for how many units" field on the product forms and on receiving; MRP shown per pack ("₹35.00 for 10") on checkout, the old POS, the product page and receipts.
     - **Bill round-off:** the checkout has no bill-level round-off today (every line is rounded to the paisa), so there is nothing for it to push above MRP. The rule stays for when one is added.
     - **Limits of this step:**
       - Imported MRPs are for one base unit (no `mrp_pack` import column).
       - Only single products and the first variant get the pack field in the forms; other variants keep pack 1.
       - Exchange and edit share the sale check, but only sale, edit and quote are tested by API.
2. **Schedules and pharmacists**: DB1–DB3, DB8, DB9, permissions, role, module, settings and forms.
3. **Prescriptions and approval at the till**: DB4–DB6, A3–A5, checkout and old-POS refusal, Schedule H flow.
4. **H1 register** (§6.4) with its workflow, report and export.
5. **X register** (§6.5) with the licence check, image rule, single-use prescriptions.
6. **Returns, voids, exchanges** of scheduled lines (§6.7).
7. **Invoice details** (§6.12) and the online-shop rules (§6.10).
8. **Retention, archives and legal holds** (§6.9): retain-until, second copy with object lock, two-person archive access, holds.

## 11. Tests

Per step, integration tests on a scratch pharmacy shop with two stores (one without a Schedule X licence), a pharmacist valid for one store only, an expired pharmacist, a cashier: above-MRP refusals (batch, unit, store price, discount share); each schedule's required fields; approval by PIN and sign-in, wrong store, expired record, changed cart; register numbering per store and year, append-only (update refused), reversal on return and void; X single-use prescription and quantity limit; old POS refusal; module off (records readable, selling refused); export/restore keeps registers exact; shop deletion with records in retention → archive, with a legal hold → blocked.

Added for the clarifications:
- **§6.14:** each route in its table, called as a cashier without approval, is refused; a direct SQL insert of a scheduled line without approval is refused by the CHECK; an approval token reused, expired, for another store or for a changed cart is refused; module off → refused.
- **§6.2:** every row of the examples table; bill round-off at the MRP limit; batches with different MRPs and pack sizes; store price and override above MRP refused; medicine without MRP refused in a pharmacy shop.
- **§6.9:** archive deleted after retain-until, kept under a hold; restore only into a scratch database; opening without the second approval refused; each step audited.
- **§6.15:** every event in its table writes its register entry and audit row; update or delete of a register entry refused; a removed entry breaks the hash chain and the report says so.


## 12. Legal requirements as checked (clarification 3)

Checked on 2026-11-21 against published texts of the Drugs and Cosmetics Rules 1945 (Rule 65 and its 2013 amendment) and their summaries by public bodies. **This is a check of the published rules, not legal advice:** state Drugs Control departments add their own forms and conditions, so the shop's lawyer and pharmacist must still sign off the items marked **(LAW)**.

| Requirement | What the rules say | Source | Status | In the plan |
|---|---|---|---|---|
| H1 register | Separate register at the time of supply: prescriber's name and address, patient's name, drug, quantity | Rule 65 as amended by GSR 588(E), 30 Aug 2013 | Confirmed | §6.4 (adds batch, pharmacist; Q-A for state formats) |
| H1 retention | Register kept 3 years, open to inspection | GSR 588(E); NHSRC, NTEP summaries | Confirmed | 3-year minimum |
| H1 prescription copy | Copy kept 3 years (stated by NTEP for H1 drugs) | NTEP | Likely; **(LAW)** | Image optional, paper with the shop |
| X prescription | Only on a registered medical practitioner's prescription, in duplicate, one copy kept 2 years | Rule 65(9)(a) | Confirmed | §6.5, DB17 |
| X repeat | Not dispensed more than once unless the prescriber directs repeats | Rule 65(11); Telangana Pharmacy Council guideline | Confirmed | `repeats_allowed`, `uses` |
| X licence | Retail sale of Schedule X needs a licence in Form 20F | Rule 61(3), Form 20F | Confirmed | store licence check |
| General retail licences | Forms 20 and 21 (and restricted 20A/21A) | Rules 59–61 | Confirmed (form numbers) | `stores.drug_licences` |
| H dispensing | Prescription only; once unless repeats directed | Rule 65; Telangana guideline | Confirmed | §6.3 |
| Prescription supplies recorded | Recorded at supply (register, or the cash memo for some cases) | Rule 65(3) | **(LAW)**: exact form | prescription register report |
| General record retention | Records kept 2 years from the entry | Rule 65 (per Telangana guideline) | Unconfirmed; **(LAW)** | 2-year minimum, 8 default |
| Pharmacist | Dispensing on prescription only by or under a registered pharmacist | Pharmacy Act 1948 s.42; Rule 65 | Confirmed (principle) | §6.6 |
| Institutional orders | Signed orders for H/H1 to hospitals kept 2 years | Rule 65(9)(b) | Confirmed | Not in scope (B2B hospital sales later) |
| MRP | Not sold above the printed retail price | Legal Metrology (Packaged Commodities) Rules 2011 | Confirmed (principle) | §6.2 |
| GST records | Kept 72 months from the annual return's due date | CGST Act s.36 | Confirmed | 6-year minimum |
| Narcotics | NDPS Act licences and forms | NDPS Act 1985 | Out of scope | blocked |

Sources consulted: Indian Kanoon text of Rule 65 (https://future.indiankanoon.org/doc/196956471/, https://future.indiankanoon.org/doc/147665881/); CDSCO Drugs Rules (https://cdsco.gov.in/opencms/en/Acts-and-rules/Drugs-Rules/); NHSRC Schedule H1 register (https://www.nhsrcindia.org/sites/default/files/2021-07/Attachment%20A%2C%20Schedule%20H1%20register%20and%20NDPS%20Forms.pdf); NTEP (https://ntep.in/node/938); Telangana Pharmacy Council guidelines (https://pharmacycouncil.telangana.gov.in/pdf/Regulations/Guidelines.pdf); Form 20F (https://www.meghealth.gov.in/forms/Form_20F_Retail_Licence.pdf).
