# ReactronPOS — Product Requirements Document (PRD)

| | |
|---|---|
| **Product** | ReactronPOS — Point of Sale & Inventory Management |
| **Document type** | PRD, as-built (describes the current behavior of the code on `main`) |
| **Source of truth** | `backend/src/**`, `frontend/src/**` (commit `94aeeb8`) |
| **Companion doc** | [TRD.md](./TRD.md): architecture, database, APIs, stack |
| **Date** | 2026-10-14 |

> This document describes what the product **does today**. Where behavior in the code differs from the intent in existing docs (README, USER_GUIDE, PERMISSIONS), the code wins. Those cases are listed under §9 *Known Gaps*.

---

## 1. Product Overview

ReactronPOS is a single-business retail Point-of-Sale and inventory system. It covers:

- **Selling:** a touch-friendly POS checkout, back-office sales entry, invoices, and sales returns
- **Stock:** products with variants, per-store inventory, purchases from suppliers, and manual stock adjustments
- **Master data:** categories, sub-categories, brands, units, suppliers, customers, and stores
- **Insight:** a dashboard plus sales, purchase, customer, and customer-due reports with PDF and Excel export
- **Access control:** users with database-driven roles and permissions
- **Online showcase:** a read-only public storefront for each store, with no login

It runs as a **web app** (React SPA + Express API) and can also be wrapped as an **Electron desktop app** that starts the API in-process.

### 1.1 Problem statement
Small and mid-size retailers with one or more physical stores need one tool to ring up sales quickly, keep per-store stock accurate, record supplier purchases, handle returns, and see revenue. They also want a simple public product catalogue for each store.

### 1.2 Goals
1. Fast checkout at the counter, with stock validated before a sale is committed.
2. Accurate per-store, per-variant inventory that every stock-moving transaction keeps up to date.
3. Configurable, least-privilege access for counter staff (billers) and administrators.
4. Basic operational reporting with exports.
5. A no-login public showcase for each store.

### 1.3 Out of scope (today)
- Multi-tenancy / SaaS. This is planned only; see `SAAS_ARCHITECTURE.md`.
- Online ordering, cart, or payment on the public shop. It is catalogue-only.
- Payment gateway integration. Payment method is recorded, not processed.
- Accounting ledgers, expense tracking, payroll.
- Stock transfer between stores, and warehouse-level stock. The warehouse entity exists, but stock is tracked per store.
- Offline mode and sync.

---

## 2. Personas & Roles

| Persona | System role | Primary jobs |
|---|---|---|
| **Owner / Administrator** | `admin` (system role, all permissions) | Set up master data and stores, manage products and stock, approve purchases, manage users and roles, view all sales and reports |
| **Cashier / Biller** | `biller` (system role, limited) | Run POS checkout, add walk-in customers, view own sales, view products and inventory, view invoices and reports |
| **Custom role** (e.g. Store Manager, Stock Clerk) | Admin-defined role | Any combination of `module:action` permissions |
| **Public visitor** | none (unauthenticated) | Browse a store's published products at `/#/shop/:slug` |

### 2.1 Default permission sets (seeded on every server start)

| Module | Admin | Biller |
|---|---|---|
| dashboard | view | view |
| pos | view, create | view, create |
| products | view, create, edit, delete | view |
| sales | view, create, edit, delete | view, create (**own sales only**) |
| customers | view, create, edit, delete | view, create |
| inventory | view, create, edit | view |
| reports | view | view |
| invoices | view | view |
| settings | view | view |
| categories, brands, units, suppliers | view, create, edit, delete | none |
| purchases | view, create, edit, approve | none |
| sales_returns | view, create | none |
| stock_adjustments | view, create | none |
| users / billers | view, create, edit, delete | none |
| roles | view, create, edit, delete | none |
| stores | **not seeded** (see §9) | none |

Admin receives every permission row in the database. System roles (`admin`, `biller`) cannot be deleted, and their permission sets are **reset to the defaults on every backend restart**.

---

## 3. Feature Inventory

### 3.1 Authentication & Session
| ID | Requirement (as built) |
|---|---|
| AUTH-1 | Users sign in with email and password. Passwords are stored as bcrypt hashes. |
| AUTH-2 | A "Remember me" option issues a 30-day token kept in `localStorage`. Without it, a 12-hour token is kept in `sessionStorage`. |
| AUTH-3 | The client refreshes the token silently about 5 minutes before expiry, and again on any 401. |
| AUTH-4 | Self-service sign-up exists at `/signup`. New users default to the **biller** role. |
| AUTH-5 | Admins create users through the Users screen, which calls the same sign-up endpoint with a chosen role. |
| AUTH-6 | The client re-polls permissions every 30 s, so role changes take effect without logging out. |
| AUTH-7 | Inactive users (`status = Inactive`) are stored, but **login does not block them** (§9). |

### 3.2 Dashboard
- KPI tiles: total revenue (sum of all sales totals), total orders, total customers, total products.
- Payment summary: total amount, total paid (Paid sales), total unpaid (due on Unpaid sales), total overdue.
- Monthly revenue and order-count bar chart, built from the earliest 100 sales by date.
- Pie chart of the top 5 products by revenue (sum of order-item subtotals).
- Figures are global. They are not filtered by store, date range, or the biller's own sales.

### 3.3 POS Checkout (`/pos`)
The core counter workflow:

1. **Select store.** Defaults to the first active store. The product grid shows only active products that have an inventory record in that store.
2. **Select customer.** Defaults to a customer named exactly `Walk-in Customer`, or the first customer if there is none. Cashiers can add a new customer inline.
3. **Find products** by category pill or name search. Pressing Enter in search adds the first unlocked match.
4. **Add to cart.** Each product is added once, using its **first variant**. Quantity is changed with the +/- buttons and capped at the available store quantity.
5. **Product soft-lock.** Adding a product to the cart locks it for that user. Other registers see it as *Locked* and greyed out for up to 2 minutes, or until it is removed from the cart. Removing it or leaving the page unlocks it.
6. **Totals.**
   - Subtotal = Σ price × qty
   - Tax is calculated per item from the product's `tax` % and `taxType`. Exclusive tax: subtotal × rate. Inclusive tax: the tax portion inside the subtotal.
   - Discount is a manual flat amount for the whole cart.
   - Total = subtotal + tax − discount (never below 0)
7. **Pay.** The payment dialog captures payment type (cash and others), received amount, change due with quick-cash buttons, a payment note, and a sale note.
8. **Commit.** The server re-checks stock for each line in a database transaction, creates the sale and its line items, decrements store inventory, and returns the sale with a generated reference `POS-INV-<timestamp>-<rand>`.
9. **Receipt.** A printable receipt is shown (browser print). Closing it clears the cart.

**Business rules**
- A sale needs a customer, a store, and at least one line with quantity > 0.
- Stock must be ≥ requested quantity for each variant in the chosen store, or the whole sale is rejected.
- If `paid` is not sent, the sale is treated as fully paid.
- Currency is displayed as **INR (₹)** with the `en-IN` locale. This is hard-coded.

### 3.4 Sales Management (`/sales`, `/add-sale`)
- List sales with customer, biller, and line items. The API supports pagination, reference search, date range, store, and customer filters.
- **Billers see only the sales they created.**
- Back-office sale entry (`/add-sale`) uses the same create rules as POS.
- **Edit sale:** inventory from the original lines is restored, the new lines are validated and applied, and the editing user becomes the sale owner.
- **Delete sale:** inventory is restored, then the line items and the sale are removed (hard delete).

### 3.5 Invoices (`/invoices`)
- Invoices are a read-only **view over sales**. There is no separate invoice entity.
- Columns: invoice number (sale reference), customer, date, amount, paid, due, status (payment status).

### 3.6 Sales Returns (`/sales-return`, `/add-sales-return`)
- A return records customer, store, an optional original sale, line items (product, variant, qty, unit price, reason), and totals, status, and payment.
- If an original sale is given, each returned product must be on that sale, and the returned qty must not exceed the sold qty **for that return**. Earlier returns are not counted (§9).
- Returned quantities are **added back** to store inventory. If no variant is given, the product's first variant is used. A missing inventory row is created.
- Return status values: Pending / Completed. Payment status values: Paid / Unpaid / Partial.

### 3.7 Product Management (`/products`, `/add-product`)
- **Product types**
  - *Single:* one variant (SKU, barcode, price, cost, weight) plus initial quantity and alert level in one store.
  - *Variable:* many variants. Each has attributes (free-form key/value, e.g. `{size: "L", color: "Red"}`) and its own per-store quantities.
- **Attributes:** name, description, status, image (stored inline as a base64/URL string), slug, category, sub-category, brand, unit, supplier, selling type (Online/POS), tax type (Exclusive/Inclusive), tax %, discount type (Percentage/Fixed) and value, warranty, barcode symbology.
- **Edit:** a full replace of variants. Variants left out of the payload are deleted along with their inventory. A variant's inventory rows are replaced when new inventory is supplied.
- **Edit protection:** editing fails with *409 Locked* while another user holds the POS lock.
- **Delete:** a hard delete of the product, its variants, and their inventory.
- **Barcode lookup:** a POS-side API looks up a product by variant barcode or SKU.
- **List:** paginated, with search by name and filters for category, brand, and status. Each row shows first-variant price and total quantity across all stores.

### 3.8 Inventory (`/inventory`)
- Each row is one *variant × store*: SKU, attributes, price, product, store, qty, alert level.
- Stock status is derived: **Out of Stock** (qty ≤ 0), **Low Stock** (qty ≤ alert), **In Stock**.
- Users with inventory edit rights can directly set qty and alert level (upsert). No audit record is written.

### 3.9 Stock Adjustments (API only)
- Record an **Addition** or **Subtraction** for a variant in a store, with reason and note. The record is auditable, with the creator stored.
- A subtraction cannot take stock below zero.
- **No UI screen exists yet.** The feature is reachable through `/api/stock-adjustments` only.

### 3.10 Purchasing (`/purchases`, `/add-purchase`)
- A purchase records reference, date, supplier, status (free text; the UI uses `Received`, `Pending`, `Ordered`), payment status (Paid/Unpaid/Overdue), total, paid, and due. Line items record product, qty, unit price, discount, and subtotal.
- **Stock-in rule:** inventory goes up only when status is `Received`, either at creation or on a later status change to `Received`.
  - At creation, stock goes to each line's chosen variant and store.
  - On a later status change, stock goes to the product's **first variant** in `storeId` from the request, **defaulting to store 1** (§9).
- Purchases can be edited for status, payment status, paid, and due only. Purchases cannot be deleted.

### 3.11 Master Data
| Entity | Fields | Operations |
|---|---|---|
| Category | name, status, image | CRUD |
| Sub-category | name, status, parent category | List (public), create (admin only) |
| Brand | name, status, image | CRUD |
| Unit | name, short name, status | CRUD |
| Supplier | name, email, phone, address, image, status | CRUD |
| Customer | name, email, phone, address, city, country, image, status | CRUD; inline create from POS |
| Store | name, address, status, plus showcase profile fields | List active, get, create, update, delete |
| Warehouse | name, address, status | Model and route exist but are **not mounted** (§9) |

Deletes are hard deletes and do not check for references.

### 3.12 Users & Roles (`/users`, `/roles`)
- **Users:** list with search (name/email) and status filter, create, edit (name, email unique, status, role), and delete.
- **Roles:** list with user counts and permission strings, create custom roles, edit name, description, and permissions, delete custom roles. Deleting is blocked for system roles and for roles that still have users.
- The permission matrix editor groups permissions by module.
- Assigning any role whose name is not `admin` sets the legacy `role` field to `biller`. Effective access still comes from the role's permissions.

### 3.13 Reports (`/sales-report`, `/purchase-report`, `/customer-report`, `/customer-due-report`)
| Report | Source | Filters (API) |
|---|---|---|
| Sales report | One row per order item: date, product, brand, category, sold qty, sold amount | start/end date, product, customer |
| Purchase report | One row per purchase item: reference, date, product, category, qty, amount | start/end date, supplier |
| Customer report | Built from the sales report data | n/a |
| Customer due report | Built from invoices: amount, paid, due per sale | n/a |

- All list screens support **PDF** export (jsPDF + autotable) and **Excel** export (write-excel-file).
- `stockQty` in reports is always 0 (placeholder).

### 3.14 Public Product Showcase
**Admin side (`/showcase-settings`):**
- Pick a store, then edit its public profile: URL slug (unique, auto-generated if missing), logo, banner, description, phone, opening hours, and an enable/disable switch.
- Per product: toggle **Published** and **Featured** for that store. Publish or unpublish all, or a selected batch.

**Public side (`/#/shop/:slug`, `/#/shop/:slug/product/:productId`), no login:**
- A store is resolved by slug or numeric id, and must be Active and public-enabled.
- Shows the store profile, categories, up to 8 featured products, and a paginated product grid (24 per page) with search, category and brand filters, and sorting (newest, name, price ascending/descending).
- Each product shows its discounted *final price* (percentage or fixed), stock status for that store, and variants.
- Internal fields such as cost, supplier, and lock owner are never exposed.

### 3.15 Settings (`/settings`)
- Theme accent colour: Sky Blue, Emerald Green, Violet, Rose, or Amber. Saved per browser.
- Light and dark theme support.

---

## 4. Core Business Logic Summary

### 4.1 Inventory movement matrix
| Event | Effect on `inventories.qty` (variant × store) | Validation |
|---|---|---|
| Create product (single) | Insert initial qty | sku, price, quantity, and storeId required |
| Create product (variable) | Insert qty for each variant and store | ≥ 1 variant |
| Edit product | Replace inventory rows for variants that sent inventories. Delete rows of removed variants. | Not locked by another user |
| POS / back-office sale | − qty | qty ≤ stock, all-or-nothing |
| Edit sale | + old qty, then − new qty | New qty ≤ stock |
| Delete sale | + qty | n/a |
| Sales return | + qty (row created if missing) | ≤ sold qty when linked to a sale |
| Purchase `Received` | + qty (row created if missing) | n/a |
| Stock adjustment | ± qty | Result ≥ 0 |
| Inventory direct edit | Set qty to an absolute value | n/a |

### 4.2 Sale financials (server-side)
```
subtotal  = Σ (line.quantity × line.unitPrice)          // recalculated on the server
total     = max(0, subtotal − discount + tax + shipping) // discount, tax, and shipping come from the client
paid      = request.paid || total
due       = max(0, total − paid)
payment_status = request.payment_status || (paid ≥ total ? 'Paid' : paid > 0 ? 'Partial' : 'Unpaid')
```

### 4.3 Concurrency
- **Soft lock** at cart level (2-minute expiry) stops two registers from selling the same product at the same time.
- **Database transaction** around each sale, purchase, return, and adjustment means one failed line rolls back the whole document.

### 4.4 Data visibility
- Billers: sales list filtered to `user_id = me`. Other lists (customers, products, inventory, invoices, reports, dashboard) are **not** filtered by user or store.

---

## 5. User Journeys

1. **First-time setup (admin):** sign up → create store(s) → categories, brands, units → suppliers → products with opening stock → create billers and assign roles → (optional) publish the showcase.
2. **Stock-in:** Purchases → Add → supplier, store, and variant lines → status Received → inventory goes up.
3. **Counter sale (biller):** POS → store → customer → scan or search and add → adjust qty → Pay → print receipt.
4. **Return:** Sales Returns → Add → original sale (optional) → items and reasons → inventory goes back up.
5. **Owner review:** Dashboard → Reports → export PDF or Excel.

---

## 6. Non-Functional Requirements (current state)

| Area | Current behavior |
|---|---|
| Platforms | Modern browsers (SPA with hash routing); Electron desktop shell (Windows/macOS/Linux) |
| Performance | DB connection pool 5–25. Indexes on hot columns (sales date/store/user, inventory variant+store, etc.). Benchmark and concurrency scripts are in `scripts/`. |
| Security | JWT bearer auth. RBAC is enforced on the server for most endpoints. CORS allow-list. bcrypt passwords. |
| Reliability | Transactions for every stock-moving operation. The permission seed is idempotent. |
| Localisation | English UI only. INR currency hard-coded. |
| Accessibility | Radix UI primitives give keyboard and ARIA support for dialogs, selects, and menus. |
| Responsiveness | POS and lists adapt to mobile, tablet, and desktop widths. |

---

## 7. Success Metrics (proposed)
- Checkout time from first item to receipt under 30 s for 5 items.
- Zero negative-stock incidents, and zero oversell under concurrent checkout (see `scripts/concurrent-checkout-test.mjs`).
- 100 % of API endpoints protected by permission checks.
- Inventory agrees with transaction history in a daily reconciliation.

---

## 8. Glossary
| Term | Meaning |
|---|---|
| Biller | Cashier user who operates the POS |
| Variant | A sellable SKU of a product (size, colour, etc.). Every product has ≥ 1. |
| Store | Physical selling location. Inventory is kept per store. |
| Walk-in Customer | Default anonymous customer record used at POS |
| Showcase | Public read-only catalogue for a store |
| Soft lock | Temporary per-product reservation while it is in a POS cart |

---

## 9. Known Gaps, Defects & Risks (found during analysis)

Severity: 🔴 high · 🟠 medium · 🟡 low

| # | Sev | Area | Finding |
|---|---|---|---|
| G1 | 🔴 | Security | `POST /api/auth/signup` is public and accepts `role_id` and `role`. Anyone can register as **admin**. |
| G2 | 🔴 | Security | `stores:*` permissions are not in the seed list, so admins get no `stores` permission. Store create/edit/delete, showcase profile update, and the **Settings** and **Showcase** menus are blocked for seeded admins. They work only through the legacy "admin with no role_id" path. |
| G3 | 🔴 | Sales | The server can compute `payment_status = 'Partial'`, but the `sales.payment_status` enum is `Paid/Unpaid/Overdue`. A partial-payment sale fails with a DB error. |
| G4 | 🔴 | Concurrency | Sale creation reads stock **without row locks** (`SELECT … FOR UPDATE`). Two concurrent sales can both pass validation and oversell. Only the UI soft lock prevents this. |
| G5 | 🟠 | POS tax | For **Inclusive** tax, the tax portion is added on top of a subtotal that already includes it, so the customer is over-charged. The label says "Tax (8%)" whatever the real rates are. |
| G6 | 🟠 | Purchases | `purchase_items` has no `variant_id`. Receiving later always stocks the **first variant**, in store 1 by default. |
| G7 | 🟠 | Returns | The return qty check ignores earlier returns against the same sale, so repeated returns can exceed the quantity sold. |
| G8 | 🟠 | Auth | Inactive users can still log in. Deleted users keep working until their token expires (up to 30 days). |
| G9 | 🟠 | Data | Deletes are hard and do not check references (customers, products, stores with sales), so history can be orphaned. |
| G10 | 🟠 | Unprotected endpoints | `GET /api/stores`, `GET /api/stores/:id`, `GET /api/stores/:id/showcase`, `POST /api/product-variants` need only a valid token (no permission check). `GET /api/sub-categories` is fully public. |
| G11 | 🟡 | Warehouses | The warehouse route is not mounted, so the UI's *Add Warehouse* modal gets 404. |
| G12 | 🟡 | Import | `ImportProduct` posts to `/products` instead of `/api/products`. The backend has no bulk import endpoint. |
| G13 | 🟡 | Stock adjustments | Backend is complete, but there is no frontend screen. |
| G14 | 🟡 | Public shop | The page calls `http://localhost:3000` directly instead of `VITE_API_URL`, so it breaks in any non-local deployment. |
| G15 | 🟡 | Dashboard | The monthly chart uses only the *earliest* 100 sales and groups by month name only, so different years are merged. |
| G16 | 🟡 | Showcase | Per-store publish toggles also write the global `products.is_published` flag, so one store's change affects other stores' fallback. |
| G17 | 🟡 | Docs | README says MongoDB/Mongoose; the code uses PostgreSQL/Sequelize. PERMISSIONS.md lists actions that are not seeded (e.g. `inventory:delete`). |
| G18 | 🟡 | Desktop | Root scripts call `dist` / `dev:electron` in the frontend workspace, but these scripts and the `electron` dependency are missing from `frontend/package.json`. |

---

## 10. Roadmap Candidates
1. Fix G1–G4 first (security and data integrity).
2. Stock adjustment UI, stock-transfer between stores, and a stock movement ledger (audit trail for every qty change).
3. Configurable business settings: currency, tax defaults, receipt header/footer, invoice prefix.
4. Variant picker at POS and real barcode-scanner input.
5. Soft delete and archival for master data.
6. Store-scoped users: billers restricted to assigned stores.
7. Multi-tenant SaaS conversion as planned in `SAAS_ARCHITECTURE.md`.
