# Industry Phase 0 and Phase 1 — Design note

Status: **approved 2026-11-17 by the owner**: all of DB1–DB24, P1–P6 and E1–E2, with the answers to Q1–Q7 below. Build order: items 1–7 (a–g), then a stop before item 8 (h) to show the backfill report from the copy run, then items 9–10 (i–j). Go tests run in a Go Docker container with `-p 2`. Based on `docs/INDUSTRY_FIT_PLAN.md` (its "Current state vs this plan" section and owner decisions D1–D8) and the code on `main` at `59e2c64`.

This note covers:
- **Phase 0**: the deliverables (hardware checklist, gap matrix, API impact list).
- **Phase 1**: every change to stored data (**DB**), permissions (**P**), API contracts (**A**) and plan entitlements (**E**), with the risks and the tests required for each item.

The last section lists every change for a yes/no.

Owner decisions are written D1–D8, as in the plan. Changes in this note are numbered DB1…, P1…, A1… and E1… so they don't clash with D1–D8.

## Rules for every item (same as S10)

- **Existing retail tenants keep working unchanged.**
  - Every new table and column has a default that reproduces today's behaviour.
  - Every new behaviour is off until a shop turns it on, except the few additive changes listed in §0.4.
  - The existing Go, isolation and browser tests must pass unchanged.
- **Server checks first.** The UI only hides what the server refuses. Every change writes an audit row.
- **Tests for every new endpoint:**
  - Tenant isolation: a cross-tenant read and write is refused with the generic `404`, and the endpoint is added to `test/isolation/isolation_matrix_test.go` and the security matrix.
  - Permission: refused without the permission.
  - Module: refused with `403 MODULE_OFF` when the module is off.
- **Every new table** joins `tenantio.Tables` (exports) or `tenantio.Excluded`. The export and restore tests cover it.
- **Migrations**: expand-only, tenant 00017 onwards and platform 00014 onwards. Any backfill runs on a copy first and its report goes into this note, as in S10 §5.2.
- **After each item**:
  - Backend: `make check` (vet, constsql, lint, vuln, test) and `make test-integration`. Go is not installed on this machine, so these run in a `golang` Docker container against the dev stack (Docker Desktop is present).
  - Frontend: `npm run lint` and both builds (`build`, `build:admin`).
  - A **"How to check"** list in `docs/UI_CHECKLIST.md` ("Industry P1 – Item N").
  - Small local commits.
- **No push or merge without the owner's OK.**

---

## 0. Phase 0 deliverables

### 0.1 Hardware checklist (D4), one per shop

This is filled in with each shop before items 3, 5 and 10 are tested. Until then, everything is tested on browser print preview, on a USB scanner set up as a keyboard, and on PDF output.

| Device | Clothing shop | Footwear shop | Pharmacy |
|---|---|---|---|
| Counter computer: OS and version, browser and version | | | |
| Barcode scanner: make, model, USB or Bluetooth, keyboard mode, suffix (Enter or Tab), symbologies enabled | | | |
| Receipt printer: make, model, connection, paper width (80 / 58 mm), driver installed, prints from browser? | | | |
| Label printer: make, model, connection, label size (mm), labels per row, gap, driver, prints a PDF at 100 %? | | | |
| Existing barcodes on goods: manufacturer EAN / UPC, own labels, none | | | |
| Cash drawer (through the printer?) | | | |
| Network: fixed line or mobile; does billing need to continue when offline? | | | |

Each answer is checked by a test print or test scan at the shop, and the result is recorded here (date, pass or fail, notes). No device is called "supported" without a passing test.

### 0.2 Gap matrix (Phase 1 scope)

Status as in INDUSTRY_FIT_PLAN "Current state vs this plan".

| Phase 1 need (plan / decision) | Today | Item in this note |
|---|---|---|
| Business type and module registry; entitlement, activation and permission kept separate (§3) | ❌ / ◐ | 1 |
| Payment-state clarity: "recorded, not verified" (D3) | ❌ | 2 |
| Barcode scanning at checkout and other screens (D1) | ◐ backend lookup only | 3 |
| Structured product and variant engine: typed attributes, size-colour matrix, size system (§4.1, §5.2, §5.3) | ◐ free JSON | 4 |
| Barcode generation and label printing (D1) | ❌ | 5 |
| Spreadsheet import with industry templates, preview and error report (D8) | ◐ CSV, one template, dry-run counts | 6 |
| Stock count and reconciliation, opening stock (§4.2, D8) | ❌ (adjustments only) | 7 |
| Supplier and purchase lifecycle: partial receiving (§4.4) | ◐ one document, all-or-nothing receive | 8 |
| Configurable returns, exchanges and store credit (D7) | ◐ returns only | 9 |
| Thermal receipt, HSN on receipt lines (§4.7) | ❌ | 10 |
| Unit-of-measure conversions (§4.3) | ◐ units master only | **Proposed: not in Phase 1** (Q1) |
| Supplier returns, debit notes, supplier payment ledger (§4.4) | ❌ | **Proposed: not in Phase 1** (Q2) |
| Customer GSTIN and place of supply per sale (§4.7) | ❌ | **Proposed: not in Phase 1** (Q3) |
| Store-scoped users, transfers (§4.5) | ❌ | Not in Phase 1 (D5) |

### 0.3 API impact list

"New" means a new route; "Changed" means an existing contract gains optional fields only, and old requests and responses stay valid.

| # | Method and path | New / Changed | Guard | Item |
|---|---|---|---|---|
| A1 | `GET /api/auth/me`, `GET /api/settings` | Changed: add `businessType`, `modules[]`, `entitledModules[]` | as today | 1 |
| A2 | `PUT /api/settings/business` `{businessType, modules[]}` | New | `modules:manage` | 1 |
| A3 | `GET /api/modules` (registry: key, label, dependencies, entitled, enabled) | New | `settings:view` | 1 |
| A4 | Sale, receipt, payment and report responses: each payment gains `verification: "recorded"` | Changed | as today | 2 |
| A5 | `GET /api/products/barcode/{code}` | Changed: add `variantId`, so a scan picks the exact variant; still `404` when unknown | `pos:view` | 3 |
| A6 | `GET/POST/PUT/DELETE /api/attributes[/{id}]` (attribute definitions) | New | `attributes:*`, module `product_attributes` | 4 |
| A7 | `GET /api/products/{id}`, `POST/PUT /api/products` | Changed: optional `attributes` (product level) and `variantDims[]`; validated only when the product uses defined attributes | as today | 4 |
| A8 | `POST /api/products/{id}/variants/matrix?dryRun=` `{dims, skuPattern, price, cost, generateBarcodes}` | New | `products:edit`, module `product_attributes` | 4 |
| A9 | `POST /api/barcodes/generate` `{variantIds[]}` (internal EAN-13 for variants without a barcode) | New | `products:edit`, module `barcode_labels` | 5 |
| A10 | `POST /api/labels/pdf` `{items:[{variantId, copies}], layoutId}` → PDF | New | `labels:print`, module `barcode_labels` | 5 |
| A11 | `GET/PUT /api/settings/labels` (label layouts: size, columns, gaps, fields) | New | `settings:view` / `settings:edit` | 5 |
| A12 | `GET /api/products/import/template?type=` (CSV or XLSX) | Changed: `type` = retail (default, today's columns) / clothing / footwear | `products:import` | 6 |
| A13 | `POST /api/products/import?dryRun=true` | Changed: accepts `.xlsx` and `.csv`; the dry run also returns `preview[]` (planned product, variants, opening stock per row); a failed run also returns a downloadable error report | `products:import` | 6 |
| A14 | `GET /api/products/import/errors/{token}` (error report as XLSX, kept 1 hour) | New | `products:import` | 6 |
| A15 | `GET/POST /api/stock-counts`, `GET/PUT /api/stock-counts/{id}`, `POST /api/stock-counts/{id}/complete`, `POST /api/stock-counts/{id}/cancel`, `POST /api/stock-counts/{id}/import` | New | `stock_counts:*`, module `stock_counts` | 7 |
| A16 | `POST /api/purchases/{id}/receipts` `{lines:[{purchaseItemId, quantity}]}`, `GET /api/purchases/{id}/receipts` | New | `purchases:approve` | 8 |
| A17 | `PUT /api/purchases/{id}` with status `Received` | Unchanged behaviour: receives everything still outstanding | `purchases:approve` | 8 |
| A18 | `GET/PUT /api/settings/returns` (return policy) | New | `settings:view` / `settings:edit` | 9 |
| A19 | `POST /api/sales/returns` | Changed: optional `settlement` (`refund` default / `store_credit`), per-line `condition`, `policyOverrideReason`; new refusals `RETURN_WINDOW_CLOSED`, `RETURN_NOT_ALLOWED`, `SALE_REQUIRED` only when the shop's policy turns them on | `sales_returns:create` | 9 |
| A20 | `POST /api/sales/exchanges` `{saleId, returnLines, newLines, payments, settlement}` (one transaction) | New | `sales_returns:create` + `sales:create` | 9 |
| A21 | `GET /api/customers/{id}/store-credit` (balance and entries), `POST /api/customers/{id}/store-credit/adjust` | New | `customers:view` / `customers:store_credit` | 9 |
| A22 | `POST /api/sales` and the quote | Changed: payment method `store_credit` allowed | `pos:create` | 9 |
| A23 | `GET /api/sales/{id}` and the receipt | Changed: lines gain `hsnCode`; settings gain `receiptPaper` | as today | 10 |

`docs/API_CONTRACT.md` gets a new §5.5 "Industry Phase 1" with the full request and response shapes and error codes, written as each item is built.

### 0.4 What existing retail shops will notice

All of these are additions; nothing they use today changes:
- Checkout search also finds a product by **exact barcode** (today a scanned barcode finds nothing unless it equals the SKU).
- Payment lines show **"Recorded"**, with the tooltip "recorded by the cashier, not verified by a bank or payment provider" (D3).
- Settings shows a **Business type: Retail** card and a **Returns** card whose defaults match today's rules.
- Purchases can be received **in parts**. Choosing "Received" still receives everything at once, as today.
- Receipts show the HSN code per line **for new sales**, where the product has one.

Everything else (attributes, matrix, labels, stock counts, exchanges, store credit, thermal paper) stays hidden until the shop turns it on.

---

## 1. Business type, module registry, entitlements

**What**
- A shop has a business type and a set of switched-on modules.
- The business type is a **preset**: it recommends modules and seeds starting data, and never blocks anything. Screens and APIs check **modules**, not the type, so mixed shops work.

**How**
- Code registry `internal/modules`. Each module has a key, label, description, dependencies, the plan feature key, the permissions it uses, and the business types that recommend it.
- Phase 1 modules: `product_attributes`, `barcode_labels`, `stock_counts`. Returns, exchanges and store credit are settings (item 9), not a module, because their defaults must match today's rules.
- Three separate checks, all on the server:
  1. **Entitled**: the plan feature is not false, and no override turns it off.
  2. **Enabled**: listed in `settings.modules`.
  3. **Permitted**: RBAC as today.
- Middleware `RequireModule(key)` returns `403 MODULE_OFF` when a module is not entitled or not enabled.
- Changing the type or modules:
  - Done by the owner in Settings (`modules:manage`, owner-only by default), and audited.
  - The platform console can set the type at provisioning.
  - Choosing a type switches on its recommended modules (the owner can untick them) and seeds attribute definitions (item 4) **only where none exist**; nothing is ever overwritten.
- Turning a module off hides its screens and refuses its writes. **Data is never deleted**, and history stays readable (for example a completed stock count).
- Presets:
  - clothing: `product_attributes`, `barcode_labels`, `stock_counts`; attributes department, collection, season, size, colour.
  - footwear: the same modules; attributes department, size_system (UK / US / EU / India), size, colour.
  - pharmacy: `barcode_labels`, `stock_counts` (batches arrive in Phase 3).
  - retail: none.

**Data**
- **DB1** `settings.business_type text NOT NULL DEFAULT 'retail' CHECK (IN ('retail','clothing','footwear','pharmacy'))`.
- **DB2** `settings.modules text[] NOT NULL DEFAULT '{}'`, with a CHECK that each value is in the Phase 1 module list. Later phases extend the CHECK.
- **DB3** platform: `platform.tenants.business_type` (copied for console filtering, kept in step by the settings write and provisioning). `tenant_overrides.key` CHECK gains `product_attributes`, `barcode_labels`, `stock_counts`.

**Permissions**: **P1** `modules:manage` (owner only, like billing).

**Entitlements**
- **E1** Module feature keys default to **on** when a plan does not mention them. All current plans and the trial get every Phase 1 module, so no plan rows change and no price changes.
- **E2** The console can turn a module off for one shop with an override (reason and expiry, as today).

**Risks**
- A module check missed on one endpoint: a route test asserts that every route under a module's paths is wrapped.
- Cache staleness after a toggle: same 30-second TTL as overrides, and the instance that wrote the change forgets at once.

**Tests**
- Registry unit tests (dependencies, unknown key).
- `settings.modules` CHECK.
- `MODULE_OFF` for each module route (off, not entitled, overridden).
- Seed-only-when-empty.
- Audit row.
- Isolation tests for A2 and A3.
- An existing tenant migrates to `retail` with `{}`, and all existing tests are unchanged.

## 2. Payments shown as "recorded, not verified" (D3)

**What**: there is no payment provider for shop sales in the first release. Every payment the cashier records is labelled as recorded, never as verified.

**How**
- Receipts (screen, PDF and email), sale detail, customer statement and the payments report show the method followed by "(recorded)".
- A legend reads: "Payments are recorded by the cashier and are not verified by a bank or payment provider."
- **Q4**: show the label on every method, or on everything except cash? The proposed default is **every method except cash**.
- Nothing changes in amounts or in Paid / Partial / Unpaid.

**Data**: **DB4** `sale_payments.verification` and `customer_payments.verification`, `text NOT NULL DEFAULT 'recorded' CHECK (verification IN ('recorded'))`. A later provider release adds `'verified'`. Existing rows get `'recorded'` from the default, so no backfill job is needed.

**Risks**: the receipt layout changes (golden PDF test updated); none to money.

**Tests**: the receipt render test, the report export columns, and the JSON field on every payment-bearing response.

## 3. Barcode scanning (D1)

**What**: a USB or Bluetooth scanner in keyboard mode works at checkout, and also in the purchase lines, the return form, stock counts (item 7) and label printing (item 5).

**How**
- One shared input handler. A run of keystrokes faster than 30 ms apart ending in Enter or Tab is treated as a scan. Typing by hand also works, because Enter does an exact match.
- Exact match order: barcode, then SKU. The variant is added with quantity 1, and a repeat scan adds 1.
- Checkout already loads `itemBarcode` for every variant (`internal/products/model.go`), so a match is instant. The server lookup (A5) is the fallback for products not loaded in the list.
- No match: a beep (Web Audio), the toast "No product with barcode 890…", and the search keeps the code so it can be fixed.
- Out of stock: today's message.

**Data**: none. **Permissions**: none (`pos:view` as today).

**Risks**: scanners that send a Tab or no suffix, or that type slowly over Bluetooth. The hardware checklist records each model, and the gap threshold is a setting if a scanner needs it (stored per browser).

**Tests**
- Unit tests for the scan detector (fast vs slow input, suffixes).
- Browser test: a simulated scan adds the right variant at checkout and in a return.
- A5 `variantId` on a variant barcode; isolation for A5 (exists, extended).

## 4. Typed attributes and the variant matrix (clothing, footwear)

**What**
- A shop defines its attributes once, for example Size (choices XS…XXL or 6…11), Colour (choices or free text), Department, Collection, Season and Size system.
- A product uses some of them at **product level** (department, season, size system) and some as its **variant dimensions** (size × colour).
- "Create variants" builds every combination, with SKUs from a pattern and optional generated barcodes. A preview is shown first.

**How**
- **DB5** new tenant table `attribute_defs`: key (citext, unique among live rows), label, level (`product` / `variant`), kind (`choice` / `text`), choices jsonb (ordered list), sort, status, soft delete.
- **DB6** `products.attributes jsonb NOT NULL DEFAULT '{}'` (product-level values) and `products.variant_dims text[] NOT NULL DEFAULT '{}'` (ordered variant dimension keys).
- Validation only applies when `variant_dims` is not empty or `products.attributes` has keys:
  - Every variant has one value per dimension, from the choices when the kind is `choice`.
  - No two live variants of a product have the same combination.
  - Products with empty `variant_dims` keep today's free `product_variants.attributes`, unvalidated, so retail and existing products are unchanged.
- Footwear:
  - `size_system` is a product-level choice attribute, so all variants of one product share one system.
  - Sizes are shown as "UK 8".
  - Reports group by (system, size), so different size systems are never added together.
- Matrix (A8):
  - SKU pattern tokens: `{article}` (a new optional product field, see DB7), `{size}`, `{colour}` and any other dimension key, with values upper-cased and stripped of characters SKUs may not use.
  - Collisions with existing SKUs are refused with the list.
  - The dry run returns the planned variants; existing combinations are skipped.
  - New variants get stock 0 in the shop's store.
- **DB7** `products.article_no text` (style or model number, optional, max 64, indexed for search).
- Sale lines: **DB8** `order_items.variant_label text` (snapshot of "M / Blue"; `NULL` for older lines), so history stays unchanged after attribute edits.

**Permissions**: **P2** `attributes:view` / `create` / `edit` / `delete` (admin by default; billers get `attributes:view`, since checkout shows attribute labels).

**Risks**
- Strict validation breaking an existing product: avoided because validation only applies to products that opt in.
- Renaming a choice that variants use: refused while it is in use (409 with a count); a label change is allowed.
- The JSON shape on product responses grows; old clients ignore it.

**Tests**
- Validation tables (missing dimension, value not in choices, duplicate combination).
- Matrix: pattern, collision, dry run, and creation in one transaction.
- Snapshot on sale lines.
- An existing product with free attributes still saves unchanged.
- Isolation for A6 and A8; `MODULE_OFF`.

## 5. Barcode generation and label printing (D1)

**What**
- Variants without a barcode can get an internal one.
- Labels print as a PDF sized for the shop's label printer or for A4 sheets: product name, variant label, price, SKU and the barcode, with fields selectable per layout.

**How**
- Generated codes are EAN-13 in the in-store range (prefix `2`, which GS1 reserves for internal use): `2` + 11 digits from a counter + a check digit. A code that already exists is skipped. Existing barcodes are never changed.
- **DB9** `doc_counters` CHECK gains `'barcode'` (one row inserted).
- The PDF is drawn with maroto v2 (already used for receipts; its barcode component uses `boombuler/barcode`, which is already in `go.sum`).
- Symbology: EAN-13 when the code is a valid EAN-13, Code 128 otherwise.
- **Spike first**: confirm maroto's EAN-13 and Code 128 output scans on a phone and on one real scanner before the rest of the item.
- Layouts (A11) are stored in **DB10** `settings.label_layouts jsonb NOT NULL DEFAULT '[]'` (label width and height in mm, columns, gaps, margins, page size, fields). There are 3 built-in presets: A4 sheet 3×8 (65×34 mm), roll 50×25 mm, roll 38×25 mm. Final sizes come from the hardware checklist.
- Print from: product page (choose variants and copies), a received purchase (copies = received quantity, item 8) and a stock count.
- Limit: 2,000 labels per request.

**Permissions**: **P3** `labels:print` (admin; billers don't get it by default).

**Risks**
- Printer scaling ("fit to page") shrinks barcodes until they don't scan. The print dialog note and the "How to check" list say to print at 100 %, and the layout has a test-page button.
- A generated code that clashes with a manufacturer code: avoided by the in-store prefix 2 plus the unique index.

**Tests**
- Check-digit unit tests; counter skips taken codes.
- PDF smoke test (page count, size); barcode decode test on the rendered image.
- Isolation for A9, A10 and A11; `MODULE_OFF`.

## 6. Spreadsheet import with industry templates (D8)

**What**: the catalogue and opening stock come from a spreadsheet (`.xlsx` or `.csv`) using the template for the shop's type. Before saving, the owner sees what will happen, and a file with errors gives a row-by-row error report.

**How**
- Templates (A12):
  - **retail** = today's 16 columns, unchanged.
  - **clothing** = article_no, name, department, collection, season, size, colour, sku, barcode, price, cost, category, brand, tax_type, tax_rate, hsn_code, opening_qty, quantity_alert.
  - **footwear** = the same, with size_system instead of collection/season.
  - Pharmacy gets its template in Phase 3, together with batch and expiry.
  - The XLSX template has a header row, one example row and a "Help" sheet listing the columns and the allowed values for choice attributes.
- Rows with the same `article_no` (or the same name when there is no article number) become one product with variants. Dimensions come from the template.
- Reading: `internal/xlsx.Read` (already in the repo, no new dependency). The first sheet only, values as text, the same 5 MB / 5,000-row limits.
- The dry run returns `preview[]`: per product, the action (create), its variants with attributes, SKU, barcode and price, and opening stock per store. The screen shows totals plus an expandable list.
- Errors: as today (all or nothing, up to 200 shown), plus a downloadable XLSX error report (A14): the original row, then the column and the message. It is kept in memory for 1 hour per user, and no file is stored.
- Opening stock is written to the ledger with reason `import`, as today.
- Updating products that already exist by import is **not** in Phase 1 (create only, as today). Stock for existing products goes through a stock count (item 7).

**Data**: none (validation is in code).

**Risks**
- Large files in memory (the 5 MB cap stays).
- Excel turning barcodes into numbers like `8.90123E+12`: the template formats code columns as text, and the reader refuses numbers in scientific notation with a clear message.
- Duplicate SKUs within the file: refused per row.

**Tests**
- Parsing for each template (CSV and XLSX).
- Grouping into variants; preview contents.
- Error report contents; scientific-notation refusal.
- The retail template and the existing import tests unchanged.
- Isolation for A14 (one user's report cannot be fetched by another user or tenant).

## 7. Stock counts (reconciliation, opening stock)

**What**
- Count a store's stock, or part of it (by category or brand), by scanning, typing or uploading a sheet.
- Review the differences, then complete. One ledger movement per difference.

**How**
- Draft:
  - Created with a scope; lines are added by scan (+1 per scan), typed, or imported (sku/barcode, counted_qty).
  - The expected quantity is shown live; nothing changes stock until completion.
- Complete (`stock_counts:complete`):
  - Locks the inventory rows in id order.
  - For each counted line: difference = counted − current quantity. The quantity becomes the counted quantity, through the ledger, with new reason `stock_count` and ref `stock_count`.
  - Variants in scope that were not counted are **left unchanged**, and the review screen lists them.
- A sale between counting and completing shifts the difference. The review screen shows "sold since this line was counted" from the ledger, and the shop completes counts out of hours or right after counting.
- Cancelled and completed counts are read-only.
- **DB11** new tenant tables:
  - `stock_counts`: reference (counter `stock_count`), store, scope jsonb, status `Draft` / `Completed` / `Cancelled`, note, created_by, completed_by, completed_at, version.
  - `stock_count_lines`: count, variant, counted_qty, counted_at, counted_by, expected_at_completion, delta. Unique per (count, variant).
  - Doc counter `stock_count`.
- **DB12** `stock_movements.reason` CHECK gains `'stock_count'`.

**Permissions**: **P4** `stock_counts:view`, `stock_counts:create` (count), `stock_counts:complete` (post differences). Admin gets all; billers get none by default.

**Risks**
- Stock drift: the single posting path is `inventory.LockRows` / `inventory.Set` (as for adjustments), and `inventory.Reconcile` must report no mismatch after completion.
- Two counts of the same variant in parallel: refused while another draft in the same store contains that variant.

**Tests**
- Completion math; uncounted lines untouched; reason and ref on movements.
- Concurrency (two completions; a sale during completion).
- Import into a count.
- Isolation for every A15 route; `MODULE_OFF`; export and restore of the new tables.

## 8. Partial purchase receiving

**What**: receive a purchase in parts (for example 60 of 100 pieces today and the rest next week), scan items as they arrive, and print labels for what arrived.

**How**
- A receipt (A16) lists purchase lines and quantities ≤ outstanding. Stock goes in per receipt line through the ledger (`purchase_receive`, ref `purchase_receipt`).
- The purchase shows its received and outstanding quantities.
- When everything is received, the status becomes `Received` (terminal, as C-09).
- Setting status `Received` the old way (A17) creates one receipt for everything outstanding. Today's behaviour and its tests stay as they are.
- A partly received purchase cannot change its lines below what has already been received (409).
- Cancelling a partly received purchase is refused; the shop closes it with a stock adjustment, as today's rule for corrections.
- **DB13** new tenant tables `purchase_receipts` (purchase, reference from counter `goods_receipt`, received_at, received_by, note) and `purchase_receipt_items` (receipt, purchase_item, quantity > 0).
- **DB14** `purchase_items.received_qty int NOT NULL DEFAULT 0 CHECK (0 ≤ received_qty ≤ quantity)`.
- **Backfill**: every `Received` purchase gets `received_qty = quantity` and one receipt row dated `received_at`. This runs **on a copy first**, with a per-shop report (purchases, rows created, mismatches = 0), as in S10 §5.2.
- **DB15** `purchases.status` CHECK gains `'Partially received'`.

**Permissions**: existing `purchases:approve` (receiving is approving stock in).

**Risks**
- Double stock-in: receipt lines are locked against the purchase row, and the outstanding quantity is checked under the lock.
- Backfill mismatches (reported, must be 0).
- The purchase report and filters must understand the new status.

**Tests**
- Partial then full receiving; over-receiving refused; the old PUT path.
- Backfill migration test (as `TestPaymentsBackfill`).
- Isolation for A16; export and restore of the new tables.

## 9. Configurable returns, exchanges and store credit (D7)

**What**: each shop sets its return policy. A return can be refunded, given as store credit, or exchanged for other goods in one step.

**Return policy (A18)**: **DB16** `settings.return_policy jsonb NOT NULL`, with defaults equal to today's rules.

| Setting | Default (= today) | Meaning |
|---|---|---|
| `refund` | true | Cash, UPI or card refund allowed |
| `storeCredit` | false | The return value can go to the customer's store credit (named customer only) |
| `exchange` | false | Exchange screen and A20 allowed |
| `timeLimitDays` | null (no limit) | Days after the sale date (shop time zone) |
| `requireLinkedSale` | false | true: returns without a sale are refused |
| `conditions` | false | true: each returned line needs a condition (resellable / damaged / defective) |
| `restockConditions` | `["resellable"]` | Which conditions go back to stock |

**Rules**
- Refund rule A stays: the return value first reduces the sale's due; only the rest is settled by refund, store credit or exchange.
- Outside the time limit, or a settlement the policy forbids → `409 RETURN_WINDOW_CLOSED` / `RETURN_NOT_ALLOWED`. A user with **P5** `sales_returns:override_policy` can go ahead with a written reason (10+ characters, audited).
- With conditions on, only the restock conditions go back to stock. Other lines are recorded with `restocked = false` and no ledger movement. A damaged-stock or quarantine area comes later, with pharmacy.
- **Exchange (A20)**:
  - One transaction: a return (reference `RET-…`) and a new sale (reference `INV-…`), linked.
  - The return's settled value pays the new sale through payment method **`exchange`**, linked to the return.
  - If the new sale costs more, the customer pays the difference with the normal payment methods. If it costs less, the rest is refunded or goes to store credit, per the policy.
  - Returned items go back per condition, new items leave stock; each movement happens exactly once.
- **Store credit**:
  - An append-only ledger per customer: `+` from a return, `−` when used as payment method `store_credit`, `±` adjustment with a reason (**P6** `customers:store_credit`, owner only).
  - The balance can never go below 0 (checked under a row lock on the customer).
  - Shown on the customer page, at checkout when a named customer is chosen, and on receipts ("Store credit left: ₹X").
  - No expiry in Phase 1 (**Q5**).
  - Walk-in customers cannot get store credit.

**Data**
- **DB16** `settings.return_policy`.
- **DB17** `return_items.condition text NULL CHECK (IN ('resellable','damaged','defective'))` and `return_items.restocked boolean NOT NULL DEFAULT true`.
- **DB18** `sale_returns`: `settlement text NOT NULL DEFAULT 'refund' CHECK (IN ('refund','store_credit','exchange'))`, `to_store_credit` and `to_exchange` numeric, default 0, and the CHECK becomes `paid + due + applied_to_due + to_store_credit + to_exchange = total`. Existing rows satisfy it with zeros.
- **DB19** `sale_payments.method` CHECK gains `'exchange'` and `'store_credit'`; new nullable `sale_payments.sale_return_id` (set for `exchange`).
- **DB20** `sales.exchange_return_id` (nullable link).
- **DB21** new tenant table `customer_credit_entries`: customer, amount (signed), kind `return` / `sale` / `adjust` / `reversal`, sale_return_id, sale_id, reason, user, created_at. **Append-only** for the tenant role, like `customer_payments`.
- **DB22** `audit_log` gets the new actions (no schema change).

**Permissions**: P5 `sales_returns:override_policy` (owner and admin), P6 `customers:store_credit` (owner only).

**Risks**
- **Money correctness**: the server computes everything, under one transaction with row locks on the sale and the customer, with idempotency keys on A19 and A20. Tests cover every combination of refund, store credit, exchange (more, less, equal), due on the original sale, and conditions.
- Reports: sales by method gains `exchange` and `store_credit`, which are not cash taken. The Dashboard "collected" figure excludes both (**Q6**).
- Tax on exchanges and store credit (credit-note treatment): **check with the shops' accountant** before the first live exchange. Receipts show the return and the new sale separately, each with its own tax.
- Void and edit: a sale paid by an exchange or by store credit cannot be edited or voided until the exchange is reversed (same rule as sales paid by a collection).

**Tests**
- A policy table test for each setting; refund rule A with every settlement.
- Exchange money and stock math; the store credit ledger (no negative balance, concurrency).
- Override permission and audit; the void and edit guard.
- Report totals; the export and restore of `customer_credit_entries`.
- Isolation for A18–A22; the default policy reproduces every existing returns test unchanged.

## 10. Thermal receipt and HSN on receipt lines

**What**: an 80 mm or 58 mm print layout for the counter printer, and the HSN code on each receipt line.

**How**
- **DB23** `settings.receipt_paper text NOT NULL DEFAULT 'a4' CHECK (IN ('a4','80mm','58mm'))`.
- The on-screen receipt prints with `@page { size: 80mm auto }` (or 58 mm), using the existing narrow layout, tightened.
- The emailed PDF stays A4.
- **DB24** `order_items.hsn_code text NULL` (a snapshot at sale time, for new sales only). Receipts show it when present.

**Risks**: browser print margins differ by driver (hardware checklist; test page). Long product names wrap.

**Tests**: render tests for each paper size, the HSN snapshot, and old sales without HSN rendering unchanged.

---

## Proposed order and size

| Order | Item | Size (one developer, with tests) |
|---|---|---|
| 1 | Module registry, business type, entitlements | M |
| 2 | "Recorded, not verified" | S |
| 3 | Barcode scanning | S |
| 4 | Typed attributes and variant matrix | L |
| 5 | Barcode generation and labels (spike first) | M |
| 6 | Spreadsheet import templates | M |
| 7 | Stock counts | M |
| 8 | Partial receiving (backfill on a copy first) | M |
| 9 | Returns policy, exchanges, store credit | L |
| 10 | Thermal receipt, HSN on lines | S |

Rough total: 8–11 weeks. Items 4–6 together unblock the clothing and footwear catalogues (Phase 2 pilots); items 3, 5 and 10 need the hardware checklist (§0.1) for the final test.

## Questions for the owner (with my default)

| # | Question | Default | Owner (2026-11-17) |
|---|---|---|---|
| Q1 | Unit conversions (box / strip / tablet) in Phase 1? | **No.** Needed only by pharmacy, after day one (D6). Stock stays in whole units. | **No**, pending the pharmacy's answer. |
| Q2 | Supplier returns, debit notes and a supplier payment ledger in Phase 1? | **No.** Phase 3 (pharmacy expiry returns) or later. | **No.** |
| Q3 | Customer GSTIN and place of supply per sale in Phase 1? | **No**, unless one of the three shops issues B2B invoices. | **No.** |
| Q4 | "Recorded" label on cash too? | **No**, only on UPI, card, cheque, bank deposit, exchange and store credit. | **No.** Cash has no label. |
| Q5 | Does store credit expire? | **No** in Phase 1. | **No.** |
| Q6 | Does the Dashboard "collected" figure exclude exchange and store-credit payments? | **Yes.** | **Yes.** |
| Q7 | Does choosing a business type at provisioning (console) also send the owner a different onboarding checklist? | **Yes**: "Import your catalogue (clothing template)" replaces "First product". | **Yes.** |

## Changes and approvals (all approved 2026-11-17)

| # | Item | Change |
|---|---|---|
| DB1 | 1 | `settings.business_type` (default `retail`) |
| DB2 | 1 | `settings.modules` (default empty) |
| DB3 | 1 | `platform.tenants.business_type`; override keys for the 3 modules |
| DB4 | 2 | `sale_payments.verification`, `customer_payments.verification` (`recorded`) |
| DB5 | 4 | new tenant `attribute_defs` |
| DB6 | 4 | `products.attributes`, `products.variant_dims` |
| DB7 | 4 | `products.article_no` |
| DB8 | 4 | `order_items.variant_label` (snapshot) |
| DB9 | 5 | doc counter `barcode` |
| DB10 | 5 | `settings.label_layouts` |
| DB11 | 7 | new tenant `stock_counts`, `stock_count_lines`; counter `stock_count` |
| DB12 | 7 | ledger reason `stock_count` |
| DB13 | 8 | new tenant `purchase_receipts`, `purchase_receipt_items`; counter `goods_receipt` |
| DB14 | 8 | `purchase_items.received_qty` + backfill of received purchases |
| DB15 | 8 | purchase status `Partially received` |
| DB16 | 9 | `settings.return_policy` (defaults = today) |
| DB17 | 9 | `return_items.condition`, `restocked` |
| DB18 | 9 | `sale_returns.settlement`, `to_store_credit`, `to_exchange`; amount CHECK widened |
| DB19 | 9 | payment methods `exchange`, `store_credit`; `sale_payments.sale_return_id` |
| DB20 | 9 | `sales.exchange_return_id` |
| DB21 | 9 | new tenant `customer_credit_entries` (append-only) |
| DB22 | 9 | new audit actions (no schema change) |
| DB23 | 10 | `settings.receipt_paper` (default `a4`) |
| DB24 | 10 | `order_items.hsn_code` (snapshot, new sales) |
| P1 | 1 | `modules:manage` (owner only) |
| P2 | 4 | `attributes:view/create/edit/delete` (biller: view) |
| P3 | 5 | `labels:print` |
| P4 | 7 | `stock_counts:view/create/complete` |
| P5 | 9 | `sales_returns:override_policy` (owner, admin) |
| P6 | 9 | `customers:store_credit` (owner only) |
| E1 | 1 | module features default on in every plan and the trial; no price change |
| E2 | 1 | console override can turn a module off per shop |
| A1–A23 | all | API changes in §0.3; additive only, documented in API_CONTRACT §5.5 |
| Q1–Q7 | — | open questions above |
