# ReactronPOS — Industry Fit & Expansion Plan
## Universal multi-industry POS platform | Initial tenant cohort: clothing, footwear and pharmacy

**Document status:** Product strategy and engineering implementation plan  
**Repository baseline:** `docs/OUTDATED_INDUSTRY_FIT_PLAN.md` in `tejas1112001/POS_inventory`  
**Revision date:** 16 November 2026  
**Initial tenant cohort:** Clothing store, footwear store, medical store / pharmacy  
**Long-term product scope:** A configurable POS and inventory platform serving multiple retail and service industries.

---

# Current state vs this plan

Checked against the code on `main` at commit `4432bba` (17 November 2026): Go backend (`backend/`), tenant migrations 00001–00016, platform migrations 00001–00013 and the React app (`frontend/`). Paths are relative to the repository root.

Legend: ✅ exists · ◐ partly there · ❌ missing.

Owner decisions taken on 17 November 2026 are marked **Decided (D1–D8)** where they change the plan below:

| # | Decision | Applied in |
|---|---|---|
| D1 | Barcode scanning at checkout and barcode label printing are in Phase 1. | §4.6, §4.8, Phase 1, §13 |
| D2 | The three initial shops (clothing, footwear, pharmacy) are real, confirmed customers. | §1, §12, §13 |
| D3 | Shop payments stay manual and are labelled **"recorded, not verified"**. No payment provider in the first release. | §4.6, §6, §7.1, Phase 1, §13, §14 |
| D4 | Printer models are unknown, so Phase 0 includes a per-shop hardware checklist. | §4.8, Phase 0, §13 |
| D5 | Each customer has one store. Multi-store features, store-scoped users and stock transfers move to a later phase. | §2.2, §4.5, §7.1, §9, Phases 1, 3, 4, §10.1, §13 |
| D6 | Pharmacy day-one scope: batch and expiry tracking, expiry-first (FEFO) selling with expired-stock blocking, expiry alerts and reports. MRP control, prescriptions and scheduled-drug records stay out until a pharmacist and a lawyer review them. | §5.4, Phase 3, §12, §13, §14 |
| D7 | Return handling is configurable per tenant: exchange, store credit, refund, time limit, condition rules. | §4.6, Phase 1, §13 |
| D8 | Catalogue and opening stock come from spreadsheet import with industry templates, a preview and an error report. | §9, Phases 1, 2, 3, §13 |

## Shared foundations

| Foundation (plan §) | Status | What exists today | What is missing | Code references |
|---|---|---|---|---|
| Business type and module registry (§3.1, §3.3) | ❌ | Nothing. Every tenant has the same screens. | `business_type`, module list, module registry, module-off API checks | `settings` table: `backend/db/migrations/tenant/00001_identity_settings_core.sql` |
| Entitlements, activation, permissions (§3.1, §8) | ◐ | Plan `limits` and `features` JSON; per-tenant overrides for a fixed key list (`max_stores`, `max_users`, `max_products`, `max_emails_per_day`, `max_storage_mb`, `public_shop`, `csv_import`, `new_checkout`); server-side feature check `RequireFeatureFor`; server-side RBAC with owner / admin / biller and custom roles | Module entitlement keys, owner module activation layer | `platform/00001_platform_core.sql`, `platform/00009_tenant_overrides_usage.sql`, `backend/internal/plans/`, `backend/internal/rbac/catalog.go` |
| Product catalogue and variants (§4.1) | ◐ | Products `single` / `variable`; variants with SKU and barcode, both unique per tenant; brand, category, sub-category, unit, HSN, tax rate/type; status Active / Inactive; sale lines snapshot name, SKU, price, catalogue price, tax | Typed, validated attribute dimensions (attributes are free JSON); size-colour matrix generation; department / collection / season; attribute and unit snapshots on sale lines | `tenant/00003_products_inventory.sql`, `tenant/00004_sales.sql` (`order_items`), `backend/internal/products/` |
| Inventory and stock ledger (§4.2) | ◐ | Whole-number `qty ≥ 0` per (store, variant); append-only `stock_movements` with reason, source (`ref_type`, `ref_id`), user and time; stock adjustments with a reason; advisory POS locks | Batch / serial / other tracking detail; unit on movements; physical stock counts and reconciliation; reservations | `tenant/00003_products_inventory.sql`, `tenant/00009_stock_adjustments.sql`, `backend/internal/inventory/`, `backend/internal/adjustments/` |
| Units of measure (§4.3) | ◐ | Units master (name, short name) linked to products | Conversions, sale / purchase units, fractional quantities, rounding, conversion snapshots | `tenant/00002_files_master_data.sql` (`units`) |
| Procurement and suppliers (§4.4) | ◐ | Supplier master; one purchase document with status `Ordered` / `Pending` / `Received` / `Cancelled`; stock goes in once on `Received`; supplier invoice reference unique per supplier; paid / due; `purchases:approve` permission | Separate PO and goods receipt, partial receipt and backorders, supplier returns / debit notes, supplier payment ledger, supplier price history | `tenant/00008_purchases.sql`, `backend/internal/purchases/` |
| Multi-store (§4.5) | ◐ | Stores; stock, purchase lines, held bills and the public catalogue page per store | Store-scoped users (users have one role, no stores), stock transfers, store-level prices. A `warehouses` master table exists but holds no stock. **Deferred by D5.** | `tenant/00001_identity_settings_core.sql` (`users`, `stores`), `backend/internal/catalog/stores.go` |
| Checkout and payments (§4.6) | ◐ | Server price check (`POST /api/sales/quote`); split and part payment (`sale_payments`: cash, upi, card, credit, cheque, deposit, points); on-account sales with credit limits, dues, collections and reversals; held bills (7 days, per store); `Idempotency-Key` on `POST /api/sales`; sale `version` column; `sales:discount` and `sales:price_override` permissions plus `max_discount_pct_without_override`; receipt print and email resend | Barcode scanning at checkout (D1); a "recorded, not verified" label on payments (D3); exchanges; store credit | `tenant/00011_new_checkout.sql`, `tenant/00012_payments_dues.sql`, `tenant/00015_held_carts.sql`, `backend/internal/sales/`, `frontend/src/pages/checkout/` |
| Returns (§4.6) | ◐ | Sales returns linked to sold lines with a cumulative quantity check; refund paid / due ("refund rule A") | Configurable return policy (D7): exchange, store credit, time limit, condition. Today every returned item goes straight back into stock, and a return may have no linked sale (`sale_id` can be NULL) | `tenant/00007_returns.sql`, `backend/internal/sales/` |
| Tax, invoices and receipts (§4.7) | ◐ | One GST setting per shop (`none` / `cgst_sgst` / `igst`); per-line CGST / SGST / IGST; HSN on the product; gapless counters for sale, sale return and purchase; A4 PDF receipt; narrow on-screen receipt layout; versioned receipts | Customer GSTIN and place of supply per sale, HSN on receipt lines, financial-year numbering, credit / debit notes, e-invoice, 80 mm / 58 mm thermal layout | `tenant/00001_identity_settings_core.sql` (`settings.tax_split`), `tenant/00006_receipts.sql`, `backend/internal/receipts/pdf.go`, `frontend/src/components/receipt.css` |
| Barcode, hardware and printing (§4.8) | ◐ | Barcode field on variants; `GET /api/products/barcode/{barcode}` lookup in the backend; product list search matches barcodes; browser printing (`window.print()`) | Checkout does not use barcodes (see mismatches below); label printing; thermal printing; tested device list | `backend/internal/products/http.go` (`barcode`, route at line 562), `frontend/src/pages/checkout/Checkout.jsx`, `frontend/src/components/Receipt.jsx` |
| Reports and exports (§3.2 item 9) | ◐ | Dashboard, sales, purchases, customers, customer dues, payments; Excel and PDF export | Vertical reports (size, batch, expiry) | `backend/internal/reports/` |
| Imports (§9) | ◐ | CSV import with a downloadable template; "Check file" dry run; all-or-nothing import with a row error list (up to 200); rows with the same name become variants; opening quantity per store | Industry templates (D8), attribute columns (size, colour), batch / expiry columns, `.xlsx` import, a preview of the changes (today the dry run returns counts and errors only) | `backend/internal/products/importcsv.go`, `frontend/src/components/ImportProduct.jsx` |
| Notifications (§3.2 item 10) | ◐ | Email outbox; receipts by email; daily low-stock email | Expiry alerts, SMS / WhatsApp | `tenant/00001_identity_settings_core.sql` (`email_outbox`), `tenant/00014_low_stock_email.sql`, `backend/internal/outbox/` |
| Security, audit, backups (§4.9) | ✅ (store isolation ❌, deferred by D5) | One database schema per tenant; server-side RBAC; `audit_log` written by adjustments, purchases, products, dues, stores, receipts and billing; tenant exports; backup and restore runbook | Store isolation; audit of module changes (no modules yet) | `backend/internal/audit/audit.go`, `backend/internal/tenancy/`, `docs/OPERATIONS.md` §6 |
| Onboarding (§9) | ◐ | Shops are created by the platform team; onboarding checklist on the dashboard | Business-type step, presets, hardware step | `tenant/00016_onboarding.sql`, `frontend/src/components/dashboard/OnboardingCard.jsx`, `backend/internal/provisioning/` |

## Initial verticals

| Need | Clothing | Footwear | Pharmacy |
|---|---|---|---|
| Variants with unique SKU / barcode and stock per store | ✅ | ✅ | ✅ |
| Size / colour as typed dimensions, matrix creation | ◐ free JSON attributes only | ◐ free JSON attributes only | — |
| Size system (UK / US / EU / India) | — | ❌ | — |
| Brand, category, sub-category | ✅ | ✅ | ✅ |
| Department, collection / season | ❌ | ❌ | — |
| Barcode scanning at checkout (D1) | ❌ | ❌ | ❌ |
| Barcode label printing (D1) | ❌ | ❌ | ❌ |
| Exchange against original invoice (D7) | ❌ | ❌ | — |
| Return condition (resellable, damaged, defective) (D7) | ❌ | ❌ | ❌ |
| Batch / lot stock | — | — | ❌ (one `expiry_date` and `manufactured_date` per variant) |
| FEFO selling and expired-stock block (D6) | — | — | ❌ |
| Expiry alerts and reports (D6) | — | — | ❌ (the low-stock email job is a pattern to reuse) |
| MRP, prescriptions, scheduled drugs (out of day one, D6) | — | — | ❌ |
| Vertical reports | ❌ | ❌ | ❌ |
| Industry import template (D8) | ❌ | ❌ | ❌ |

## Statements in this plan that do not match the code

1. **§14 "Preserve … payment verification".** The shop app has no payment verification. `sale_payments` stores the method and amount the cashier chooses. Razorpay (`backend/internal/billing/`) is used only for the platform's own subscription billing. D3 now applies.
2. **§4.6 payment states "initiated, pending, verified, failed, refunded".** In the code, `sales.payment_status` is `Paid` / `Partial` / `Unpaid` / `Overdue` (`tenant/00004_sales.sql`) and returns use `Paid` / `Partial` / `Unpaid`.
3. **§7.1 "Payment status depends on verified evidence", §6 "verified status" and the §4.6 UPI rule.** A UPI payment counts as paid as soon as the cashier records it. Under D3 this stays the case, with a "recorded, not verified" label.
4. **§4.6 "Fast scan", §5.1 and §6 "Scan / search".** Checkout matches only product name or SKU (`frontend/src/pages/checkout/Checkout.jsx` lines 111–115 and 142; placeholder "Search by name or SKU" at line 504). The backend barcode lookup (`GET /api/products/barcode/{barcode}`) is not called by checkout. A scanner works only when the barcode equals the SKU.
5. **§4.8 "Browser and Electron print behaviour".** The product is web only (`docs/PHASE1_DESIGN.md` Q-1). `frontend/electron.js` and `frontend/preload.js` are leftovers that load the removed Node backend (`../backend/src/index.js`). `frontend/package.json` has no Electron dependency or script. The root `README.md` still describes the old Node / MongoDB stack and Electron scripts.
6. **§2.2 Procurement "Supplier, PO, goods receipt and returns" for the initial cohort.** There is one purchase document with statuses. There is no separate goods receipt, no partial receipt and no supplier return.
7. **§4.6 "Original-sale-linked returns".** Returns can be linked to sale lines, but a return without a sale is allowed (`sale_returns.sale_id` can be NULL). Returned goods always go back into stock, with no condition check.
8. **§3.1 permission example `inventory:batches:adjust`.** Permissions in the code have two parts, `resource:action` (for example `stock_adjustments:create`, `sales:price_override`).
9. **§7.1 "Store scope checked server-side" and §9 "store assignments".** Users are not tied to stores, so there is no store scope to check. D5 defers this.
10. **§7.3 "Existing retail tenants default to `business_type = 'retail'`".** No `business_type` column exists yet. The migration that adds it must set this default.
11. **§11 Phase 1 exit "existing retail regression suite passes".** Go unit and integration tests run in CI (`.github/workflows/go-backend.yml`). The browser tests (`frontend/e2e/`) run only locally against the Docker stack, not in CI.
12. **§9 "Imports must preview changes".** The import "Check file" step validates and returns counts and errors. It does not show a row-by-row preview of what will change.
13. **§1 "the original Industry Fit Plan".** That document is now `docs/OUTDATED_INDUSTRY_FIT_PLAN.md`.

---

# 1. Executive direction

ReactronPOS should be designed as a **multi-industry POS platform**, not as software limited to clothing, footwear and pharmacy.

The first three tenants will be:
1. Clothing store.
2. Footwear store.
3. Medical store / pharmacy.

**Decided (D2):** these three shops are real, confirmed customers.

These three tenants are the **initial launch and validation cohort**, not the full product scope. Their real workflows should validate the shared platform foundations: configurable product variants, stock tracking, purchasing, checkout, returns, receipts, permissions, reporting and industry modules.

The platform should subsequently support the other verticals identified in the original Industry Fit Plan and additional adjacent retail categories through reusable modules.

## Product principles

- One multi-tenant platform, one shared checkout foundation and one core inventory / stock ledger.
- Business type selects a starting configuration; it must not hard-code the tenant into one workflow.
- Industry capabilities are modular and can be combined for mixed businesses.
- Subscription entitlements, module activation and employee permissions are separate.
- All access controls are enforced by the Go backend, not only by frontend visibility.
- Existing retail tenants and existing workflows remain backward compatible.
- Industry-specific data must not be forced into unrelated industries.
- Enabling or disabling a module must never silently delete historical data.
- Build reusable foundations once, then implement vertical workflows in deliberate releases.
- The first three tenants validate the platform, but architecture and product strategy must not exclude future industries.

---

# 2. Industry coverage strategy

## 2.1 Core supported industry families

The long-term product should cover the following industry families. The order below is a proposed product sequence, not a promise that all features ship in the first release.

| Industry | Core operating model | Distinctive capabilities | Suggested priority |
|---|---|---|---|
| General retail / convenience | Product and variant sales | Catalogue, barcode, stock, purchasing, discounts, returns | Foundation |
| Clothing / apparel | Style and variant retail | Size-colour matrix, collections, exchanges, size sell-through | Initial tenant |
| Footwear | Variant retail | Size systems, size runs, model/colour/size stock, exchanges | Initial tenant |
| Pharmacy / medical store | Regulated batch-tracked retail | Batch, expiry, FEFO, MRP, prescription workflows | Initial tenant |
| Mobile / electronics | High-value tracked goods and services | IMEI/serial, warranty, repair jobs, parts | Next vertical |
| Salon / spa | Services, staff and appointments | Service duration, stylist, calendar, commission, packages | Later vertical |
| Restaurant / cafe | Table / counter orders and kitchen flow | Tables, open orders, modifiers, KOT, kitchen display | Later vertical |
| Grocery / supermarket | High-volume and weighted retail | Weighing, PLU, fractional units, expiry, wastage | Later vertical |
| Wholesale / distribution | B2B bulk sales and procurement | Customer price lists, credit, bulk orders, dispatch | Later vertical |
| Bakery / food production | Retail plus production | Recipes, batches, production, wastage, shelf life | Later vertical |
| Hardware / building materials | Quantity and measurement-based retail | Length/area/weight units, cut lengths, contractor pricing | Future candidate |
| Books / stationery / gifts | Catalogue and barcode retail | ISBN/catalogue data, bundles, school lists, gift services | Future candidate |
| Automotive parts / accessories | Compatibility-driven inventory | Vehicle fitment, part cross-reference, serial/warranty where needed | Future candidate |

The platform should be extensible beyond this list. New industry support should be added through a documented product and architecture process rather than creating a bespoke code branch for every customer.

## 2.2 Initial cohort versus long-term scope

| Area | Initial cohort | Long-term platform |
|---|---|---|
| Tenant onboarding | Clothing, footwear, pharmacy presets | Any registered supported business type |
| Product model | Variants and batch-aware inventory | Variants, batches, serials, services, recipes and measured goods |
| Checkout | Shared checkout with three vertical extensions | Shared checkout with configurable vertical workflows |
| Procurement | Supplier, PO, goods receipt and returns | Advanced procurement, approvals, replenishment and distribution |
| Multi-store | One store per customer (**Decided, D5**); multi-store features, store-scoped users and stock transfers in a later phase | Full branch / warehouse management |
| Reporting | Shared reports plus three vertical dashboards | Industry-specific analytics |
| Other industries | Not part of first customer acceptance | Staged vertical releases |

Do not mistake “not in the initial cohort” for “not supported by the architecture.”

---

# 3. Product architecture: shared core plus industry modules

## 3.1 Four independent configuration layers

Keep these concepts distinct:

| Layer | Meaning | Example |
|---|---|---|
| Business type | Onboarding preset and default experience | `clothing`, `pharmacy`, `restaurant` |
| Module entitlement | What the subscription permits | `batches` included in plan |
| Module activation | What the tenant owner has enabled | `batches` enabled |
| User permission | What an employee can do | `inventory:batches:adjust` |

A tenant may operate a mixed business. For example:
- Clothing store with accessories and tailoring services.
- Pharmacy with cosmetics and personal-care retail.
- Electronics store with repair services.
- Cafe selling packaged retail products.

Business type should recommend modules and defaults, not block legitimate mixed workflows.

## 3.2 Shared platform domains

The platform should be organised around reusable capabilities:

1. Tenant provisioning and business configuration.
2. Users, roles, permissions and store access.
3. Product catalogue, variants, categories and attributes.
4. Inventory, units, stock ledger and tracking detail.
5. Suppliers, purchasing, receipts and supplier returns.
6. Customers, credit, loyalty and customer balances.
7. Checkout, sales, payments, refunds and exchanges.
8. Invoices, receipts, tax and document numbering.
9. Reports, exports and dashboards.
10. Notifications and messaging.
11. Subscription plans, feature entitlements and usage metering.
12. Audit, security, backups and operations.
13. Hardware integration and printing.

Industry modules extend these domains; they should not duplicate them.

## 3.3 Module registry

Create a central, typed module registry. Every module should declare:
- Stable key, display name and description.
- Supported business types.
- Dependencies and incompatible combinations.
- Required permissions.
- Plan entitlement key.
- Required backend capability / migration.
- API enforcement rules.
- Navigation and UI configuration.
- Enable / disable validation.
- Data-retention and historical-record behaviour.
- Tests required for release.

Candidate module catalogue:

| Module | Purpose / likely users |
|---|---|
| `product_variants` | Size, colour, model, style and other variants |
| `barcode_labels` | Barcode generation and label printing |
| `unit_conversions` | Box, carton, pack, strip, piece, kg, metre, litre |
| `purchase_orders` | Supplier purchase orders |
| `purchase_returns` | Supplier returns and credit notes |
| `stock_transfers` | Inter-store / warehouse transfers |
| `stock_counts` | Physical stock counts and reconciliation |
| `multi_store` | Store-scoped users, stock and reports |
| `exchange_store_credit` | Exchanges and store-credit ledger |
| `batches` | Batch / lot-level inventory |
| `expiry_management` | Expiry alerts, blocking and write-off |
| `mrp_controls` | MRP capture and applicable price ceiling |
| `prescriptions` | Prescription capture and review |
| `scheduled_medicines` | Verified scheduled-drug controls |
| `serial_tracking` | IMEI / serial-level inventory |
| `warranty_tracking` | Warranty start, expiry and claims |
| `repair_jobs` | Repair intake, estimates, parts and delivery |
| `services` | Non-stock service catalogue |
| `staff_lines` | Staff assigned to sale lines |
| `appointments` | Booking calendar and availability |
| `memberships_packages` | Prepaid packages and usage ledger |
| `tables_open_orders` | Tables, tabs and long-lived open orders |
| `modifiers` | Menu modifiers and add-ons |
| `kot_kitchen` | Kitchen tickets and display |
| `recipes_ingredients` | Recipe / ingredient consumption |
| `weighing_plu` | Scale integration and price lookup |
| `production_batches` | Production and finished goods |
| `thermal_receipts` | 80 mm / 58 mm receipt layouts |
| `messaging_reminders` | Email, SMS and WhatsApp through approved providers |
| `offline_pos` | Local transactions and synchronisation, only after separate design |

Do not enable every module for every plan or tenant by default. Dependencies and plan entitlements must be explicit.

---

# 4. Shared foundations required across industries

## 4.1 Product catalogue and variant engine

The catalogue must support:
- Products and sellable variants.
- Structured, validated attributes.
- Unique SKU and barcode rules within the tenant.
- Brand, category, tax classification and unit.
- Variant-specific cost and selling price where needed.
- Product status and discontinued items.
- Bulk import, export and validation.
- Historical sale snapshots.

Examples:
- Clothing: style + size + colour.
- Footwear: model + size system + size + colour.
- Electronics: model + storage + colour + serial tracking.
- Grocery: product + pack size + weight / PLU.
- Hardware: product + length / diameter / material.

Do not require every industry to use the same attribute dimensions. Provide validated industry templates and a controlled extension mechanism.

## 4.2 Inventory and stock tracking

Conceptual layers:
1. Product identity.
2. Sellable variant.
3. Store stock balance.
4. Optional tracking detail: batch, serial, lot, recipe ingredient or other supported tracking type.
5. Append-only stock ledger.
6. Reservations for supported open orders.
7. Unit conversion and stock status.

Suggested tracking modes:
- `quantity` — ordinary quantity stock.
- `variant` — size / colour / model variants.
- `batch` — lot / batch tracked.
- `serial` — individually identified units.
- `recipe` — ingredient consumption.
- `measured` — fractional or measured quantity.

One authoritative stock-posting path is required. Aggregate quantities must reconcile with detailed tracking rows. Avoid independently editable totals.

Stock movements must capture tenant, store, item/variant, quantity, unit, reason, source document, actor and timestamp. The stock ledger remains append-only.

## 4.3 Units of measure

Support:
- Base unit and sale / purchase units.
- Conversion factors.
- Integer and fractional quantity policies.
- Unit-specific rounding.
- Historical conversion snapshots on transactions.

Examples:
- Pharmacy: box → strip → tablet, where permitted.
- Grocery: kg → gram, litre → ml, pieces.
- Hardware: bundle → piece, metre, square foot, kg.
- Wholesale: carton → pack → piece.

Conversion must be configured and validated per product. Do not assume every unit conversion is exact or that fractional sale is permitted.

## 4.4 Procurement and suppliers

Common lifecycle:

`Purchase Request (optional) → Purchase Order → Goods Receipt → Supplier Invoice → Supplier Payment → Purchase Return / Debit Note`

Required shared features:
- Supplier master and tax/contact details.
- Purchase order lifecycle and approvals where required.
- Partial receipt and backorders.
- Goods receipt posts stock exactly once.
- Supplier invoice reference and purchase costs.
- Supplier returns linked to original receipt.
- Supplier outstanding and payment history.
- Supplier price history.
- Audit trail for cost and quantity changes.

Industry extensions:
- Pharmacy: batch, expiry, MRP and bonus/free stock.
- Clothing / footwear: variant matrices and size-run receiving.
- Electronics: serial / IMEI capture.
- Grocery / bakery: expiry, lot and measured quantity.
- Wholesale: bulk rates, dispatch and delivery.

## 4.5 Multi-store and warehouse management

Design for:
- Users assigned to one or more stores.
- Store-scoped roles and permissions.
- Stock transfers with dispatch, in-transit and receive states.
- Linked ledger entries at both ends.
- Store-specific prices and stock thresholds where needed.
- Central catalogue and branch-specific assortment.
- Branch cash registers and shift close.
- Consolidated and store-level reporting.

Implement according to customer demand, but do not make the architecture store-agnostic in a way that prevents future branch support.

**Decided (D5):** each of the three initial customers has one store. Multi-store features, store-scoped users and stock transfers move to a later phase (see Phase 4). Keep the existing per-store data model (stock, purchases and held bills already carry a store) so this can be added later.

## 4.6 Checkout, payments and returns

Shared checkout:
- Fast scan and product search. **Decided (D1):** barcode scanning at checkout is in Phase 1.
- Server-side price and tax validation.
- Discounts and overrides controlled by permissions.
- Split and multiple payments.
- Payment states: initiated, pending, verified, failed, refunded. **Decided (D3):** not in the first release; see below.
- Held carts and recoverable interrupted checkout.
- Original-sale-linked returns and exchanges.
- Receipt preview, print and reprint.
- Idempotent transaction posting and clear error recovery.

UPI must not be marked paid merely because a cashier selected UPI or displayed a QR. Payment status must reflect verified evidence from the supported payment flow.

**Decided (D3):** shop payments stay manual in the first release, with no payment provider. The cashier records each payment (cash, UPI, card and so on), and the app labels it **"recorded, not verified"** on screen, on receipts and in reports. Verified payment states come only when a payment provider is added in a later release.

Returns must preserve the original sale relationship and correctly handle stock condition, refund, store credit, tax and audit.

**Decided (D7):** return handling is configurable per tenant: whether exchanges are allowed, whether store credit is given, whether refunds are allowed, the time limit after the sale, and condition rules (for example resellable goes back to stock, damaged does not).

## 4.7 Tax, invoice and receipt foundation

Review:
- Customer GSTIN and address / place-of-supply snapshots.
- HSN/SAC and line-level tax treatment as applicable.
- Invoice numbering and financial-year policy.
- Credit / debit notes.
- E-invoice / e-way bill applicability and integrations.
- A4 and thermal receipts.
- Historical invoice snapshots.
- Store-specific invoice details.

Tax rates and legal requirements must be confirmed with qualified accountants / advisers. This plan is not legal or tax advice.

## 4.8 Barcode, hardware and printing

Build a tested compatibility matrix for:
- USB / Bluetooth barcode scanners.
- Barcode and shelf-label printers.
- 80 mm / 58 mm thermal receipt printers.
- Cash drawers and customer displays where supported.
- Weighing scales for relevant future verticals.
- Kitchen printers / displays for restaurants.
- Browser and Electron print behaviour.

Do not claim universal compatibility. Record exact models, operating systems, connection type and known limitations.

**Decided (D1):** barcode scanning at checkout and barcode label printing are in Phase 1.  
**Decided (D4):** the shops' printer models are unknown today, so Phase 0 includes a per-shop hardware checklist.

## 4.9 Security, privacy and audit

- Server-side tenant and store isolation.
- Server-side module, entitlement and RBAC checks.
- Audit stock adjustments, price overrides, refunds, exchanges, purchase returns and module changes.
- Secure file storage and access controls.
- Least privilege for sensitive customer / prescription information.
- Data retention, export and deletion policies.
- Secrets and payment credentials never exposed in frontend or logs.
- Backup, restore and incident-response procedures.

---

# 5. Industry capability specifications

## 5.1 General retail and convenience

**Core needs**
- Product catalogue, variants, barcode and stock.
- Purchase, goods receipt and supplier return.
- Checkout, discounts, returns and receipts.
- Customer management and basic loyalty.
- Stock count, low-stock alerts and sales reports.

**Later enhancements**
- Promotions and bundles.
- Gift cards and store credit.
- Price labels and shelf labels.
- Multi-store transfers.
- Replenishment recommendations.

## 5.2 Clothing / apparel

**Core needs**
- Parent style with size-colour variants.
- Brand, category, department, collection / season.
- Unique SKU / barcode per sellable variant.
- Bulk size-colour matrix creation and import.
- Variant-level stock by store.
- Barcode label printing.
- Exchange against original invoice.
- Return condition: resellable, damaged, defective or quarantine.

**Reports**
- Sales by style, category, brand, size and colour.
- Sell-through by collection / season.
- Size gaps, slow-moving and dead stock.
- Margin, discount and exchange analysis.

**Acceptance**
- Sale deducts only selected variant.
- Exchange restores original variant and deducts replacement exactly once.
- Historical invoice remains unchanged after product edits.
- Labels scan correctly on customer hardware.

## 5.3 Footwear

**Core needs**
- Brand, model / article, category and department.
- Explicit size system (UK, US, EU, India) and displayed size label.
- Colour and size variants with unique SKU / barcode.
- Size-run creation and carton receiving if customer uses it.
- Variant-level stock and exchange.
- Defective / display pair stock status if required.
- Accessories in shared catalogue.

**Reports**
- Sales by brand, model, size and colour.
- Size availability and missing-size report.
- Size-run purchase versus sales.
- Aged stock, defective stock and exchange analysis.

**Acceptance**
- Different size systems are never conflated.
- Every size-colour variant has independent stock.
- Size exchange posts both movements and retains original invoice reference.

## 5.4 Pharmacy / medical store

This is a higher-risk vertical because batch, expiry, MRP, prescription and scheduled-medicine workflows affect inventory accuracy and legal responsibilities.

**Decided (D6): pharmacy day-one scope**
- In: batch and expiry tracking; expiry-first (FEFO) selling with blocking of expired stock; expiry alerts and reports.
- Out until a pharmacist and a lawyer review them: MRP control, prescriptions and scheduled-drug records.
- The other core needs below follow after day one.

**Core needs**
- Batch / lot number, expiry and manufacturing date where supplied.
- MRP and purchase cost per batch where applicable.
- Supplier and purchase receipt reference.
- Batch-level available, quarantined, expired, recalled and written-off states.
- FEFO allocation suggestion.
- Expired / recalled / quarantined stock sale block.
- Batch-aware returns, supplier returns and write-offs.
- Pack / strip / tablet conversion where permitted.
- MRP validation server-side.
- Near-expiry reports and alerts.

**Conditional / compliance-gated**
- Prescription capture and secure attachment storage.
- Product schedule classification.
- Pharmacist review and dispense permissions.
- Required registers and retention rules.
- Drug licence details on receipts.
- Public catalogue restrictions.

Do not implement clinical advice, automated drug interaction diagnosis or substitution recommendations in the initial POS. Verify applicable rules with qualified pharmacy professionals and legal advisers.

**Acceptance**
- Multiple batches for one medicine remain separately traceable.
- Sale records exact batch and quantity.
- FEFO selects earliest eligible expiry by default.
- Expired / recalled / quarantined stock cannot be sold.
- Returns cannot silently restore unsafe stock.
- Batch and aggregate quantities reconcile after every stock movement.
- Prescription access and dispense permissions are server-side and audited.

## 5.5 Mobile and electronics

**Core needs**
- Model, storage, colour and other product variants.
- IMEI / serial capture at receiving and sale.
- Unique serial / IMEI validation within tenant.
- Serial status: in stock, sold, returned, defective, quarantine.
- Warranty start and end date.
- IMEI / serial lookup and customer purchase history.
- Repair job cards with customer, device, issue, estimate, status and technician.
- Parts consumed by repair must post stock correctly.
- Repair labour as a service line.
- Finance partner recorded as payment reference initially; avoid partner API integration in v1.

**Reports**
- Serial-wise stock and ageing.
- Sold units by IMEI.
- Warranty expiry.
- Repairs by status and technician.
- Parts usage and finance references.

**Later**
- Used-device buyback after legal and operational review.
- Manufacturer warranty integrations.

## 5.6 Salon and spa

**Core needs**
- Service catalogue with duration and price.
- Staff / stylist assignment per service line.
- Walk-in billing.
- Appointment calendar by staff and store.
- Availability, leave and double-booking prevention.
- Appointment-to-bill conversion.
- Retail product sales in the same checkout.
- Tips and commission rules.
- Customer visit history and notes with access controls.
- Packages, memberships and prepaid balance ledger.
- Cancellation, deposit and no-show policy.

**Reports**
- Sales by stylist and service.
- Commission.
- Service versus product mix.
- Appointment no-shows and utilisation.
- Unused package liability.

## 5.7 Restaurant and cafe

**Core needs**
- Menu and menu categories.
- Dine-in, takeaway and delivery order types.
- Tables, areas, seats and table status.
- Long-lived open orders and concurrent edit protection.
- Modifiers and add-ons.
- KOT creation, cancellation reason, reprint and kitchen display.
- Table move / merge.
- Split bill by items or amount.
- Waiter assignment.
- Service / parcel charge configuration.
- 80 mm bill and KOT printing.
- Day-end / shift cash reconciliation.

**Later**
- Recipe and ingredient inventory.
- QR ordering.
- Delivery aggregator integration.
- Advanced reservations and kitchen analytics.

Ingredient inventory needs fractional units and recipe consumption. It should be a deliberate inventory extension, not forced into ordinary whole-number product stock.

## 5.8 Grocery and supermarket

**Core needs**
- High-volume barcode checkout.
- Weighed goods and PLU.
- Fractional units such as kg / gram and litre / ml.
- Price-per-unit and weighed-item labels.
- Batch / expiry for relevant products.
- Wastage, spoilage and write-off.
- Promotions and multi-buy pricing.
- Fast cashier workflow and cash reconciliation.

**Hardware**
- Weighing scale integration must be tested against specific models and protocols.

## 5.9 Wholesale and distribution

**Core needs**
- B2B customer accounts and tax details.
- Customer-specific price lists and quantity breaks.
- Bulk orders and repeat orders.
- Credit limits, dues and party ledger.
- Salesperson assignment and commission where needed.
- Purchase orders, dispatch and delivery tracking.
- Partial fulfilment and backorders.
- Stock across warehouses and branches.

**Reports**
- Customer outstanding and ageing.
- Sales by customer / salesperson.
- Gross margin by customer and product.
- Dispatch and fulfilment performance.

## 5.10 Bakery and food production

**Core needs**
- Retail products and made-to-order items.
- Recipe / bill of materials.
- Ingredient consumption and fractional units.
- Production batches and finished goods.
- Shelf-life and expiry.
- Wastage, spoilage and production variance.
- Pre-orders and custom orders.
- Production planning and daily output.

Recipe inventory and production must use auditable stock movements.

## 5.11 Hardware and building materials

Potential capabilities:
- Products sold by length, area, weight, volume or piece.
- Cut-length and remnant tracking where relevant.
- Contractor / trade customer pricing.
- Bulk quotation and conversion to sale.
- Delivery / dispatch and partial fulfilment.
- Product compatibility and technical attributes.

This vertical should be validated with real merchants before defining exact units and workflows.

## 5.12 Books, stationery and gifts

Potential capabilities:
- ISBN / publisher catalogue import where available.
- Barcode-based product lookup.
- School / institution lists and bulk orders.
- Bundles and kits.
- Gift wrap and gift cards.
- Special orders and supplier backorders.

## 5.13 Automotive parts and accessories

Potential capabilities:
- Make / model / year compatibility.
- Part number and cross-reference.
- Fitment notes.
- Serial / warranty tracking for selected products.
- Workshop / installation service lines.
- Supplier catalogues and special orders.

This is a future candidate, subject to customer discovery and product-data availability.

---

# 6. Shared checkout and role experiences

Use one checkout engine with industry-specific extensions.

| Shared checkout | Industry-specific extension |
|---|---|
| Scan / search and cart | Variant matrix, batch selector, serial selector, service selector, table order |
| Price and tax validation | MRP / batch price, staff commission, modifiers |
| Discounts and permissions | Industry-specific override rules |
| Split payment and verified status (**Decided, D3:** "recorded, not verified" in the first release) | Deposits, tips, service charges as applicable |
| Hold / resume | Long-lived table / appointment / repair orders |
| Returns and refunds | Variant exchange, batch return, serial return |
| Receipt and print | IMEI, batch, expiry, KOT or service details |

Keep distinct experiences for:
1. Platform Super Admin.
2. Shop Admin / Owner.
3. Cashier / Biller.
4. Industry-specific roles such as pharmacist, stylist, waiter, kitchen staff or repair technician.

Platform administration must not be merged with tenant administration. Permissions and module access must be enforced by backend APIs.

---

# 7. Platform, API and migration requirements

Before implementation, inspect actual Go code, migrations, API contracts and frontend workflows on the current branch.

## 7.1 Required invariants

- Tenant isolation for every new table, query and background job.
- Store scope checked server-side (once store-scoped users exist; **Decided, D5:** later phase).
- Stock ledger remains append-only.
- Every movement has source document, reason, actor and store.
- No sale, return, receipt, transfer or write-off posts stock twice.
- Aggregate stock reconciles with variant / batch / serial detail.
- Historical sale lines preserve product, variant, price, tax, unit and tracking snapshots.
- Payment status depends on verified evidence. **Decided (D3):** with no payment provider in the first release, every shop payment is shown as "recorded, not verified"; nothing is shown as verified without evidence.
- Module-off APIs reject prohibited writes server-side.
- Plan entitlement and user permission are both enforced.
- Sensitive data is access-controlled and audited.
- Retryable operations are idempotent.
- Concurrent updates use transactions, constraints or version checks.

## 7.2 API contract

For every new or changed endpoint:
- Update `docs/API_CONTRACT.md`.
- Define request and response schemas.
- Define permission, module, tenant and store requirements.
- Define validation errors and stable error codes.
- Define idempotency and retry behaviour.
- Add integration tests.
- Preserve existing contracts unless a versioned change is approved.

Do not invent endpoints in frontend code. Backend contracts are authoritative.

## 7.3 Migration policy

- Prefer expand-only migrations.
- Existing retail tenants default to `business_type = 'retail'`.
- Existing tenants retain current behaviour unless explicitly migrated.
- Backfills are deterministic, tested, resumable and observable.
- Add reconciliation queries before and after migrations.
- Module disablement never deletes historical data.
- Document rollback limitations honestly.

---

# 8. Plans, entitlements and commercial packaging

Do not create a separate subscription plan for every industry. Prefer a common plan structure plus explicit module entitlements and usage limits.

Potential entitlement dimensions:
- Number of stores.
- Number of users.
- Product / variant count.
- Monthly transactions or other fair usage measure.
- Storage.
- Prescription / attachment storage.
- Messaging credits.
- Advanced reports.
- Multi-store and warehouse features.
- Appointments, KOT, repairs or other premium workflows.

Commercial rules:
- A business type does not automatically imply every module is paid or free.
- Plan entitlement controls whether a tenant may activate a module.
- Owner activation controls whether the tenant uses the module.
- User permissions control who can perform actions.
- Trial tenants may receive selected modules for evaluation, subject to cost and risk controls.
- Usage and paid add-ons must be transparent in the Super Admin console.

---

# 9. Onboarding, imports and customer migration

Onboarding flow:
1. Select business type.
2. Apply recommended preset.
3. Display available modules and plan entitlements.
4. Collect store and invoice details.
5. Import catalogue with industry-specific template.
6. Validate duplicate SKUs/barcodes and required fields.
7. Configure users, roles and store assignments (store assignments in a later phase, **Decided, D5**).
8. Configure printers and scanners.
9. Import and reconcile opening stock.
10. Run first purchase and first sale.
11. Confirm reports and closing balance.

Templates:
- Clothing: style, brand, category, colour, size, SKU, barcode, cost, price, opening stock.
- Footwear: brand, model, category, colour, size system, size, SKU, barcode, cost, price, opening stock.
- Pharmacy: product, manufacturer, composition if maintained, pack size, base unit, batch, expiry, MRP, cost, supplier, opening quantity.
- Electronics: brand, model, storage, colour, SKU, serial / IMEI, cost, price.
- Grocery: product, PLU, barcode, unit, weight / price rule, expiry if applicable.
- Wholesale: product, unit, price list, customer group and opening stock.

Imports must preview changes, validate rows, report failures and avoid silent partial imports. Opening stock must create traceable stock entries. Batch / serial-tracked products require their tracking details before they can be sold under those controls.

**Decided (D8):** for the three initial shops, the catalogue and opening stock come from spreadsheet import, using an industry template (clothing, footwear, pharmacy), with a preview of what will change and an error report per row before anything is saved.

---

# 10. Testing and release gates

## 10.1 Automated tests

**Unit tests**
- Variant matrix generation and validation.
- Unit conversion and rounding.
- Price / tax calculations.
- MRP validation.
- FEFO batch selection.
- Serial uniqueness and status transitions.
- Exchange and store-credit calculations.
- Recipe quantity calculation when implemented.

**Integration tests**
- Purchase order → receipt → stock ledger.
- Sale → payment → receipt.
- Return / exchange → refund or store credit → stock.
- Pharmacy purchase → batch receipt → FEFO sale → return / write-off.
- Serial receive → sale → return.
- Multi-store transfer → dispatch → receive (later phase, **Decided, D5**).
- Module disabled → API rejection.
- Plan not entitled → activation rejected.
- Permission missing → operation rejected.
- Tenant A cannot access tenant B data.

**End-to-end tests**
- Clothing: style, variants, receiving, labels, sale and size exchange.
- Footwear: size run, receiving, scan, sale and exchange.
- Pharmacy: multiple batches, FEFO, expired-stock block, safe return and write-off.
- Electronics: serial receiving, sale, warranty lookup and repair.
- Salon: appointment, service, staff assignment and package redemption.
- Restaurant: table, open order, KOT, split bill and close.
- Grocery: weighed item and fractional stock movement.
- Wholesale: price list, credit sale and dispatch.

Tests for future verticals should be added before those verticals are declared production-ready.

## 10.2 Production release gates

A vertical is not production-ready until:
- P0 workflows pass.
- No unresolved critical tenant-isolation or stock-integrity defects remain.
- Opening stock reconciles.
- Returns and refunds reconcile.
- Tax / invoice outputs are reviewed by customer and accountant where applicable.
- Actual hardware is tested.
- Real user roles and permissions are tested.
- Industry-specific legal review is signed off where needed.
- Customer acceptance checklist is approved.
- Backup, monitoring, support and rollback procedures are documented.

---

# 11. Phased implementation roadmap

The roadmap covers the entire industry strategy, while the first three tenants determine the first production cohort. Do not build all verticals simultaneously.

## Phase 0 — Repository and customer discovery

- Re-read current migrations, services, API contracts and frontend workflows.
- Verify which statements in the original plan still match `main`.
- Map current product, variant, stock, purchase, sale, return and receipt models.
- Review RBAC, subscription feature checks and tenant/store boundaries.
- Conduct discovery with the first three tenants.
- Identify hardware models and import requirements.
- **Decided (D4):** complete a hardware checklist for each shop: receipt printer, label printer and barcode scanner (make, model, connection type, paper / label size), plus the computer, operating system and browser at the counter.
- Produce an implementation gap matrix and approve it.

**Exit:** approved architecture map, customer requirements, API impact list, P0 scope and acceptance tests.

## Phase 1 — Shared retail platform foundation

- Business type and module registry.
- Entitlement / activation / permission separation.
- Structured product and variant engine.
- Barcode and label printing. **Decided (D1):** barcode scanning at checkout and barcode label printing.
- Unit-of-measure foundation.
- Supplier and purchase lifecycle.
- Stock count, adjustment and reconciliation.
- Exchange / store-credit ledger. **Decided (D7):** with a return policy configurable per tenant (exchange, store credit, refund, time limit, condition rules).
- Spreadsheet import of catalogue and opening stock with industry templates, preview and error report (**Decided, D8**).
- Tax / invoice / thermal receipt improvements.
- Shared checkout and payment-state clarity. **Decided (D3):** payments labelled "recorded, not verified"; no payment provider.
- ~~Store-scoping architecture and implementation where required.~~ Moved to a later phase (**Decided, D5**).

**Exit:** existing retail regression suite passes and shared workflows are stable.

## Phase 2 — Initial tenant cohort: clothing and footwear

- Clothing style and size-colour matrix.
- Footwear model, explicit size systems and size-run support if needed.
- Bulk import and barcode labels (clothing and footwear import templates, **Decided, D8**).
- Variant-level purchasing and stock.
- Original-sale-linked exchange workflow.
- Vertical reports.
- Hardware validation and customer pilots.

**Exit:** both customers complete their acceptance checklists using real catalogues and hardware.

## Phase 3 — Initial tenant cohort: pharmacy

- Pharmacy module and role configuration.
- Batch-level stock model and reconciliation.
- Batch-aware purchase receiving.
- FEFO allocation and expiry blocking.
- ~~MRP capture and server-side validation.~~ Out until a pharmacist and a lawyer review it (**Decided, D6**).
- Batch-aware returns and write-offs (transfers in a later phase, **Decided, D5**).
- Expiry dashboard, alerts and reports.
- Prescription / scheduled medicine workflow only after compliance sign-off (out of day one until a pharmacist and a lawyer review it, **Decided, D6**).
- Pharmacy import template with batch and expiry columns (**Decided, D8**).
- Pharmacy receipt and record review.
- Customer-supervised opening-stock import and pilot.

**Exit:** batch integrity, expiry controls, permissions, compliance review and customer acceptance pass.

## Phase 4 — Stabilisation and platform hardening

- Monitor live transaction performance and failures.
- Resolve pilot issues before expanding.
- Improve onboarding and imports.
- Confirm backup, restore, observability and incident response.
- Validate multi-store requirements. **Decided (D5):** multi-store features, store-scoped users and stock transfers are planned here, after the first cohort is stable, and built when a customer needs a second store.
- Design offline mode only after documenting transaction and conflict guarantees.

**Exit:** first cohort operates reliably and support processes are proven.

## Phase 5 — Mobile and electronics

- Serial / IMEI tracking.
- Warranty tracking.
- Repair job cards.
- Parts consumption and labour services.
- Finance partner payment references.
- Repair and serial reports.

**Exit:** serial quantity reconciles with aggregate stock and repair workflows pass.

## Phase 6 — Salon and spa

- Services and duration.
- Staff per line and commissions.
- Appointments and availability.
- Walk-ins and appointment conversion.
- Packages, memberships and prepaid ledger.
- Reminders and customer visit history.

**Exit:** double-booking, commission and prepaid liability tests pass.

## Phase 7 — Restaurant and cafe

- Menu, modifiers and order types.
- Tables and long-lived open orders.
- KOT and kitchen display.
- Table move / merge and bill split.
- Thermal bill and day close.
- Recipe / ingredient stock as a separate extension.

**Exit:** concurrent order, KOT reliability and cash-close tests pass.

## Phase 8 — Grocery and supermarket

- Weighed goods, PLU and fractional units.
- Scale integration.
- Expiry and wastage.
- High-volume checkout and promotions.

**Exit:** scale compatibility and measured-stock reconciliation pass.

## Phase 9 — Wholesale and distribution

- B2B customer pricing.
- Bulk orders and quantity breaks.
- Credit and customer ledger.
- Dispatch, delivery and partial fulfilment.
- Warehouse and branch operations.

**Exit:** customer balances, stock and dispatch reconcile.

## Phase 10 — Bakery, hardware, books, automotive and further verticals

Prioritise based on qualified customer demand and reuse of existing modules:
- Bakery: production, recipes, batches, shelf life and wastage.
- Hardware: measured goods, cut lengths, contractor pricing and dispatch.
- Books / stationery: ISBN/catalogue lookup, bundles and institutional orders.
- Automotive: fitment compatibility, part cross-reference and workshop services.

Each vertical needs its own discovery, specification, compliance review if applicable, implementation plan and acceptance tests.

---

# 12. Initial tenant cohort: definition of done

These are the first three customers (real and confirmed, **Decided, D2**) and should be the first production acceptance cohort. They do not define the complete set of industries supported by the product.

## Clothing tenant
- [ ] Catalogue imported and reconciled.
- [ ] Style / size / colour variants work.
- [ ] SKU and barcode uniqueness validated.
- [ ] Purchase receipt updates correct variant stock.
- [ ] Labels scan on actual hardware.
- [ ] Sale and exchange workflows pass.
- [ ] Discounts, returns and refunds are permission-controlled.
- [ ] Reports reconcile.
- [ ] Receipt and printer verified.
- [ ] Owner and cashier sign off.

## Footwear tenant
- [ ] Catalogue and size system imported.
- [ ] Model / colour / size variants work.
- [ ] Size-run receiving tested if required.
- [ ] Each variant has correct SKU, barcode and stock.
- [ ] Sale and size exchange workflows pass.
- [ ] Defective / damaged stock handling works if required.
- [ ] Reports reconcile.
- [ ] Receipt and printer verified.
- [ ] Owner and cashier sign off.

## Pharmacy tenant
- [ ] Product and supplier catalogue imported.
- [ ] Opening stock is batch-level and reconciled.
- [ ] Purchase receiving captures required batch details.
- [ ] FEFO allocation tested.
- [ ] Expired / recalled stock cannot be sold.
- [ ] Expiry alerts and reports work (**Decided, D6**).
- [ ] ~~MRP validation is server-side.~~ Not in day one (**Decided, D6**).
- [ ] Returns and write-offs preserve batch traceability.
- [ ] ~~Prescription / scheduled medicine controls verified where applicable.~~ Not in day one (**Decided, D6**).
- [ ] Required receipts and reports reviewed by qualified adviser.
- [ ] Pharmacist / owner sign off.

---

# 13. Decisions and risks to resolve

## Decisions to confirm

| Decision | Proposed default | Confirm with |
|---|---|---|
| Initial business types | Clothing, footwear and pharmacy, while retaining general retail. **Decided (D2):** the three shops are real, confirmed customers | Product owner |
| Future verticals | Add through module registry and phased roadmap | Product owner |
| Variant dimensions | Typed, configurable dimensions | Clothing / footwear tenants |
| Footwear size system | Explicitly store system and label | Footwear tenant |
| Size-run receiving | Implement if supplier cartons require it | Footwear tenant |
| Exchange | **Decided (D7):** return handling configurable per tenant (exchange, store credit, refund, time limit, condition rules), linked to the original sale | Clothing / footwear tenants |
| Store credit | Separate audited liability / balance ledger | Product owner / accountant |
| Unit conversion | Explicit base unit and conversion factors | All tenants |
| Pharmacy FEFO | **Decided (D6):** in day one; suggested default; actual batch stored | Pharmacy tenant |
| Expired stock | **Decided (D6):** hard block from sale in day one | Pharmacy tenant / adviser |
| MRP | **Decided (D6):** out of day one until a pharmacist and a lawyer review it | Pharmacy tenant / adviser |
| Prescription capture | **Decided (D6):** prescriptions and scheduled-drug records out of day one until a pharmacist and a lawyer review them | Pharmacy tenant / adviser |
| Offline mode | Separate design, not assumed in first cohort | Product owner |
| Multi-store | **Decided (D5):** one store per customer; multi-store features, store-scoped users and stock transfers in a later phase | Initial tenants |
| Hardware | Test exact models. **Decided (D4):** per-shop hardware checklist in Phase 0 | Each tenant |
| Barcode | **Decided (D1):** barcode scanning and label printing in Phase 1 | Product owner |
| Shop payments | **Decided (D3):** manual, labelled "recorded, not verified"; no payment provider in the first release | Product owner |
| Catalogue and opening stock | **Decided (D8):** spreadsheet import with industry templates, preview and error report | Product owner |
| Tax | Accountant-approved per customer | Each tenant |

## Principal risks

| Risk | Mitigation |
|---|---|
| Over-generalised data model | Typed common entities plus validated industry extensions |
| Too many verticals at once | Release in phases; first three tenants are the first cohort |
| Variant stock errors | Unique SKU, authoritative posting path and reconciliation |
| Pharmacy batch drift | Batch-aware ledger, constraints and reconciliation tests |
| Unsafe returns | Quarantine and authorised restock workflow |
| Frontend-only module gating | Server-side entitlement, activation and RBAC |
| Existing retail regression | Expand-only migrations and regression tests |
| Hardware mismatch | Tested device matrix and fallback |
| Incorrect tax assumptions | Professional review |
| Sensitive prescription data exposure | Least privilege, secure storage, audit and retention |
| Offline conflicts | Dedicated idempotency and conflict-resolution design |

---

# 14. Final product recommendation

**Build ReactronPOS as a universal, modular POS platform. Launch with clothing, footwear and pharmacy as the first three tenants.**

The first three tenants should validate the shared foundations:
- Clothing and footwear validate a flexible variant engine, barcode workflows and exchanges.
- Pharmacy validates batch-aware inventory, FEFO, expiry, MRP and controlled workflows. **Decided (D6):** day one covers batch-aware inventory, FEFO and expiry; MRP and controlled workflows follow after a pharmacist and a lawyer review them.
- All three validate procurement, stock integrity, checkout, payments, receipts, reporting, permissions and onboarding.

After this cohort is stable, expand into mobile/electronics, salon/spa, restaurant/cafe, grocery, wholesale/distribution, bakery and other categories through the same module architecture.

Do not build all industries at once, but do not design the system around only the first three. Preserve the existing Go backend, tenant isolation, RBAC, stock ledger, payment verification, document counters and audit architecture. (Note: the shop app has no payment verification today; payments are recorded by the cashier. **Decided, D3:** they stay manual and are labelled "recorded, not verified" in the first release.) Reconcile every proposed change against the actual code and API contract before implementation.
