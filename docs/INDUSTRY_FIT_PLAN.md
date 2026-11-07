# Industry Fit Plan — medical stores, salons, mobile & electronics, restaurants & cafes

Status: **plan only, nothing built.** Written 2026-11-07 from the code on `main` (c4d98a4): Go backend (`backend-go/`), React app (`frontend/`), tenant migrations 00001–00016, platform migrations 00001–00013, and the docs in `docs/`.

Effort scale (one developer, including tests): **S** ≤ 3 days · **M** 1–2 weeks · **L** 2–4 weeks · **XL** > 1 month.

Legal and tax notes below are **questions to check with an accountant or lawyer**, not facts.

---

## 1. What we have today (baseline from the code)

| Area | What exists | Where |
|---|---|---|
| Items | Products (`single`/`variable`), variants with SKU, barcode, price, cost, free `attributes` JSON, **one** `expiry_date` and `manufactured_date` per variant, `hsn_code`, tax rate/type, free-text `warranties` | tenant 00003, `internal/products` |
| Stock | Whole-number `qty` per (store, variant); alert level; append-only stock ledger with reasons; manual adjustments. Every sale line **must** have stock (`INSUFFICIENT_STOCK`). No batches, no serial numbers, no transfers between stores | tenant 00003/00009, `internal/inventory` |
| Selling | New checkout with server price check (`POST /api/sales/quote`), held bills (7 days, per store), split / part payment, on-account sales with credit limits, dues and collections, returns (refund rule A), void, edit | tenant 00004/00012/00015, `internal/sales`, `frontend/src/pages/checkout` |
| Tax | One GST setting per shop: `none` / `cgst_sgst` / `igst`; per-line tax; prices with or without tax | `settings.tax_split`, `internal/pricing` |
| Receipts | A4 PDF per sale version (`internal/receipts/pdf.go`); on-screen receipt with a narrow layout (`receipt.css .rc-narrow`); shows GSTIN of the shop, tax summary, payments, due date. **No** HSN, customer GSTIN, place of supply, MRP, batch, IMEI | `internal/receipts` |
| Customers | Name, phone, email, address, photo, marketing consent, credit limit | tenant 00001/00012 |
| Staff | Users with one role; the biller is stored on the sale (`sales.user_id`). Users are **not** tied to stores | tenant 00001 |
| Permissions | Code catalog (`internal/rbac/catalog.go`), synced to every tenant; owner / admin / biller system roles + custom roles | `internal/rbac` |
| Reports | Dashboard, sales, purchases, customers, customer dues, payments; Excel/PDF export | `internal/reports` |
| Purchases | Per-line variant + store; stock goes in once on "Received". No batch/expiry/serial per line | tenant 00008 |
| Settings | One `settings` row per tenant: currency, locale, tax, invoice prefix, receipt header/footer, home page, new checkout, low-stock email | tenant 00001+ |
| Plans | `platform.plans.limits` / `features` JSON, per-tenant overrides (fixed key list in a CHECK), landing pricing labels in `internal/landing/plans.go` | platform 00001/00009 |
| Other | Public catalogue per store (no ordering), daily low-stock email, onboarding checklist, Super Admin console, web only (no offline, PHASE1_DESIGN Q-1) | — |

**Main gaps that matter to all four types:** services (items without stock), staff on a sale line, customer GSTIN / place of supply per sale, HSN/SAC on receipts, thermal (80 mm) receipts, and a way to switch screens per business type.

---

## 2. Comparison table (feature vs business type)

✅ have · 🆕 must build · ◐ optional / later · — not needed

| Feature | Retail (today) | Medical | Salon | Mobile & electronics | Restaurant & cafe |
|---|---|---|---|---|---|
| Business type + module switches | 🆕 | 🆕 | 🆕 | 🆕 | 🆕 |
| Stock-tracked products, purchases, ledger | ✅ | ✅ | ✅ (retail products) | ✅ | ◐ (packaged items) |
| Services / non-stock items | ◐ | ◐ | 🆕 | 🆕 (repairs, installs) | 🆕 (menu items) |
| Batch + expiry per stock lot | — | 🆕 | — | — | ◐ (ingredients) |
| Serial / IMEI per unit | — | — | — | 🆕 | — |
| Pack ↔ loose unit (strip ↔ tablet) | — | 🆕 | — | — | ◐ |
| MRP on item and receipt | ◐ | 🆕 | — | 🆕 | — |
| Staff per line + commission report | — | — | 🆕 | ◐ | ◐ (waiter) |
| Appointments | — | — | 🆕 | ◐ (repair pickup) | ◐ (reservations) |
| Open orders / tabs / tables | ✅ held bills (basic) | — | ◐ | — | 🆕 |
| Kitchen ticket (KOT) / kitchen screen | — | — | — | — | 🆕 |
| Repair job cards | — | — | — | 🆕 | — |
| Prescription / doctor details | — | 🆕 | — | — | — |
| Customer GSTIN + place of supply per sale | 🆕 | 🆕 | 🆕 | 🆕 | 🆕 |
| HSN/SAC on receipt | 🆕 | 🆕 | 🆕 | 🆕 | 🆕 |
| 80 mm thermal receipt | ◐ | 🆕 | 🆕 | 🆕 | 🆕 |
| Service charge, tips, parcel charge | — | — | ◐ (tips) | — | 🆕 |
| Packages / memberships / prepaid | — | — | 🆕 | — | ◐ |
| Warranty tracking from sale date | — | — | — | 🆕 | — |
| Expiry / near-expiry report | — | 🆕 | ◐ | — | ◐ |
| Split, part payment, dues, returns | ✅ | ✅ | ✅ | ✅ | ✅ (split bill 🆕) |
| Offline billing | — | ◐ | — | — | ◐ (risk) |

---

## 3. Shared "business type" setting per tenant

**Idea:** a business type is a **preset** that switches on a set of **modules**. Screens and fields read the modules, not the type, so mixed shops work (a mobile shop with repairs, a pharmacy that also sells cosmetics, a cafe with retail packs).

| Part | Proposal |
|---|---|
| Data (tenant) | `settings.business_type text NOT NULL DEFAULT 'retail' CHECK (IN ('retail','pharmacy','salon','electronics','restaurant'))` and `settings.modules text[] NOT NULL DEFAULT '{}'`, values checked against a code list (`services`, `batches`, `pack_units`, `serials`, `staff_lines`, `appointments`, `tables`, `kot`, `repairs`, `prescriptions`, `memberships`, `service_charge`, `thermal_receipt`). New migration, expand-only. |
| Data (platform) | Copy the type into the usage snapshot the worker already writes (platform 00009), so the Super Admin console can filter by type without reading tenant schemas. |
| Signup | One extra question on the signup form ("What kind of business?"). Provisioning stores it and applies the preset. |
| Presets on provisioning | Seed units, categories, roles and onboarding checklist per type (e.g. pharmacy: units Strip/Tablet/Bottle, role Pharmacist; salon: role Stylist, Receptionist; restaurant: roles Waiter, Kitchen). Seeds only on a new tenant, never overwritten. |
| API | `GET /api/settings` and `/api/auth/me` return `businessType` and `modules`. Writes to a module's endpoints return 403 `MODULE_OFF` when the module is off. |
| Frontend | One `useModules()` hook; the sidebar, product form, checkout and receipt show fields by module. |
| Changing type | Owner can change type or modules in Settings (needs `settings:edit`, audited). Turning a module off **hides** it; data is never deleted. |

Effort: **M**. Shared by all four types; build it first.

---

## 4. Shared building blocks

| # | Block | What changes | Effort | Used by |
|---|---|---|---|---|
| F1 | Business type + modules | §3 | M | All |
| F2 | Services (non-stock items) | `products.item_kind ('goods','service')`; sale/quote/return skip stock and ledger for services; SAC code in the same field as HSN (rename label only); product form + checkout tile | M | Salon, electronics, restaurant, medical (consultations) |
| F3 | Tax on the invoice | `customers.gstin`, `sales.customer_gstin` + `sales.place_of_supply` snapshot; pick CGST+SGST or IGST **per sale** (today one shop-wide setting); HSN/SAC on receipt lines; HSN summary report | M | All |
| F4 | Thermal receipt | 80 mm (and 58 mm) PDF + print layout next to A4; setting per store; per-type extra lines via a small block list | M | All |
| F5 | Staff per line | `order_items.staff_id` (nullable), picker at checkout, "sales by staff" + commission report (commission % per staff or per item, simple rules) | M | Salon, restaurant (waiter), electronics |
| F6 | Open orders | Long-lived open order (table / chair / job) that can be added to many times and then billed; builds on held bills but does not expire in 7 days and has a status | L | Restaurant, salon |
| F7 | Tracked stock units | Pattern for "stock rows below the variant": batches (medical) and serials (electronics). Purchase line captures them, sale line picks them, ledger keeps the total qty in step | L each (B1, E1) | Medical, electronics |
| F8 | Store-scoped users | Users tied to one or more stores (PRD roadmap item 6) | M | All multi-outlet chains |
| F9 | Messaging | Appointment reminders, warranty / refill reminders. Email now; SMS/WhatsApp later (cost per message) | M (+ provider) | Salon, medical, electronics |

---

## 5. Medical stores (pharmacies)

**Day to day:** receive stock from distributors with batch numbers, expiry dates and MRP; sell by strip or loose tablet; check the prescription for scheduled drugs and record doctor and patient; sell the earliest-expiring batch first; return near-expiry stock to the distributor; keep prescription registers; reorder daily.

| Area | Already covered | Missing / must change |
|---|---|---|
| Data model | Variant expiry/mfg date (one per variant), HSN, tax rate, cost, barcode, suppliers, purchases | **Batches**: `stock_batches (store, variant, batch_no, expiry, mrp, cost, qty)`; purchase lines carry batch/expiry/MRP; sale and return lines carry `batch_id`. **Pack size** on variant (sell loose in the smallest unit; buy in packs). `products.schedule` (e.g. none / H / H1 / X — list to be confirmed). `drug composition` / salt name for substitute search. Prescription record (`sale_prescriptions`: doctor, reg. no., patient, image file) |
| Screens | Checkout, purchases, inventory | Batch picker (default earliest expiry); search by salt/composition and show substitutes; prescription capture when a scheduled item is in the cart; expiry dashboard; purchase return to supplier (not built at all today) |
| Permissions | Role catalog | `sales:sell_scheduled` (pharmacist only), `inventory:expiry_writeoff`, `purchase_returns:create` |
| Receipts | A4/narrow receipt, shop GSTIN | Batch, expiry, MRP per line; drug licence no. in header; doctor/patient for scheduled items; thermal layout (F4) |
| Reports | Sales, purchases, stock | Near-expiry / expired stock, scheduled-drug register (H1-style), batch-wise stock, purchase returns, MRP vs selling price |
| Settings | Tax, invoice prefix | Drug licence no(s), pharmacist name, block sale of expired batches (on), near-expiry days (e.g. 90), hide scheduled items from the public shop (on) |

**Verify with an accountant / lawyer**
- Which prescription and register rules apply to scheduled drugs (H, H1, X), what must be recorded and for how long.
- Whether the bill must show drug licence number, batch, expiry, pharmacist name.
- MRP rules: can we sell above MRP (we should block it), and how discounts below MRP are shown.
- Current GST rates and HSN codes for medicines and devices (rates have changed over time).
- Whether listing medicines on our public shop page counts as online sale/advertising under e-pharmacy or drug advertising rules.
- Rules for returning expired stock to suppliers and for selling loose tablets.

**Build plan**

| Slice | Content | Effort | Risk |
|---|---|---|---|
| M1 | Pack size + loose sale (smallest unit, whole numbers stay) | M | Wrong conversion on old purchases; keep pack size 1 as default |
| M2 | Batches: table, purchase capture, FEFO pick at checkout, batch on returns, ledger in step | XL | Biggest change to stock; two quantities (variant total and batch total) must never drift. Needs a DB check + reconciliation test |
| M3 | MRP field, block price > MRP, MRP on receipt | S | — |
| M4 | Schedules + prescription capture + register report | M | Legal wording; image storage counts toward plan storage |
| M5 | Expiry dashboard, near-expiry email (reuse low-stock email job), expired-batch block | M | — |
| M6 | Purchase returns to supplier (debit note) | M | New doc counter + stock-out reason |
| M7 | Salt/composition search + substitutes | M | Needs a drug master; start with owner-entered data, no third-party list |

Shared: F1, F3, F4, batches pattern (F7) for restaurant ingredients later, purchase returns for every type, expiry report for salon/cafe products.

---

## 6. Salons (and spas)

**Day to day:** book appointments by stylist and time; handle walk-ins; record which stylist did which service; sell retail products (shampoo etc.); take tips; sell packages and memberships (e.g. 5 haircuts, prepaid wallet); pay stylists commission; remind clients of the next visit.

| Area | Already covered | Missing / must change |
|---|---|---|
| Data model | Customers with phone, consent; products for retail items; split payment; dues | Services (F2) with duration; `order_items.staff_id` (F5); appointments (`appointments`: customer, staff, service(s), start, end, status, store); packages/memberships with balance (`customer_packages`, usage rows); tips on the sale; customer notes (allergies, preferences), birthday |
| Screens | Checkout, customers | Appointment calendar by stylist (day/week); convert appointment → bill; staff picker per line; package sale and redeem at checkout; customer visit history |
| Permissions | Roles | `appointments:view/create/edit/delete`; `packages:sell`, `packages:redeem`; stylist role sees only own appointments (`appointments:view_all` for reception) |
| Receipts | Payments, totals | Stylist per line, SAC per service, package balance left, tip line |
| Reports | Sales, payments, customers | Sales by stylist, commission, service vs product mix, appointment no-shows, package liability (unused prepaid balance) |
| Settings | — | Opening hours per store (field exists on `stores`), slot length, commission rules, reminder on/off |

**Verify with an accountant / lawyer**
- GST rate and SAC code for salon services; registration threshold for service businesses vs goods.
- GST treatment of prepaid packages/memberships and gift vouchers (when tax is due: at sale or at use).
- Whether tips are part of taxable value and how they are recorded.
- Rules on storing customer phone numbers and sending reminders (data protection law, consent — we already store marketing consent).

**Build plan**

| Slice | Content | Effort | Risk |
|---|---|---|---|
| S1 | Services (F2) + staff per line (F5) + tips | M | Services must never touch stock or ledger; returns of services need a rule (refund only) |
| S2 | Commission rules + staff report | M | Rules get complicated; start with % per staff and % per service |
| S3 | Appointments calendar + convert to bill | L | Double-booking under concurrency (use a DB exclusion constraint on staff + time range) |
| S4 | Packages / memberships / prepaid wallet | L | It is money held for the customer: needs an append-only ledger like customer payments |
| S5 | Reminders (email first) | M | Message cost if SMS/WhatsApp is added |

Shared: services and staff lines (restaurant, electronics repairs), appointments (repair pickups, table reservations), prepaid wallet (cafes).

---

## 7. Mobile and electronics sellers

**Day to day:** buy phones/devices with an IMEI or serial number; sell a specific unit and write its IMEI on the bill; give warranty from the sale date; take devices for repair and track the job; sell accessories in bulk; offer EMI through finance partners; sometimes buy back old phones.

| Area | Already covered | Missing / must change |
|---|---|---|
| Data model | Variants with attributes (colour, storage), barcode, free-text warranty, purchases, returns | **Serial units** (`serial_units`: variant, store, serial/IMEI 1/IMEI 2, status in_stock / sold / returned / defective, purchase_item_id, order_item_id); `products.track_serials`; warranty months on the product; repair jobs (`repair_jobs`: customer, device, IMEI, problem, estimate, status, technician, parts used, labour as a service) |
| Screens | Checkout, purchases | Scan or type IMEI at purchase receive (bulk paste) and at sale; IMEI lookup ("where is this unit, who bought it"); warranty check by IMEI or phone; repair job board (received → diagnosing → waiting parts → ready → delivered) |
| Permissions | Roles | `serials:view/edit`, `repairs:view/create/edit`, `repairs:deliver` |
| Receipts | Totals, GSTIN | IMEI/serial per line, warranty end date, terms; repair job slip and delivery receipt |
| Reports | Sales, stock | Serial-wise stock and ageing, sold units by IMEI, warranty expiring, repairs by status and technician, finance/EMI sales by partner |
| Settings | — | Warranty terms text, require IMEI at sale (on), finance partners list (payment reference only) |

**Verify with an accountant / lawyer**
- Whether IMEI must be recorded on the invoice and kept for a period; any police / state rules for buying and selling **used** phones.
- GST rate and HSN for phones and accessories; whether a margin scheme applies to second-hand goods.
- How EMI / finance-partner sales and subvention charges are invoiced.
- E-waste rules that may apply to sellers or repairers.
- Warranty wording (consumer protection law).

**Build plan**

| Slice | Content | Effort | Risk |
|---|---|---|---|
| E1 | Serial units: purchase capture, sale pick, return back to stock, stock = count of in-stock serials | L | Must keep qty and serial count equal; unique IMEI per tenant |
| E2 | IMEI on receipt + warranty months + warranty lookup | S | — |
| E3 | Repair job cards + job slip + labour as service (needs F2) | L | Parts used must move stock correctly; job may never be billed |
| E4 | Finance partner as a payment reference | S | Do not integrate partners' APIs in v1 |
| E5 | Buy-back of used phones (purchase from a customer) | M | Legal checks above; keep out of v1 |

Shared: serial pattern (F7) reused by any high-value item; repair jobs reuse open-order status flow (F6); staff per line for sales incentives.

---

## 8. Restaurants and cafes

**Day to day:** take orders per table or counter or takeaway/delivery; send items to the kitchen (KOT); add more items during the meal; move or merge tables; print the bill, split it, apply service charge and parcel charges; settle by cash/UPI/card; close the day; track raw-material stock loosely; speed matters a lot at peak hours.

| Area | Already covered | Missing / must change |
|---|---|---|
| Data model | Products (packaged items), held bills (7-day carts), split payment, store per outlet | Menu items as services or recipe items (F2); **open orders** (F6) with `order_type` (dine-in / takeaway / delivery), `table_id`, covers, waiter; `tables` (store, name, area, seats, status); `kots` and KOT lines with status (new / preparing / ready / served / cancelled); item modifiers and add-ons (extra cheese, less sugar); service charge and parcel charge on the sale; optional recipes (ingredients consumed per menu item) |
| Screens | Checkout | Table map; order screen with fast menu grid and modifiers; KOT print / kitchen display; move/merge table; split bill by items or equally; day close (shift cash count) |
| Permissions | Roles | `tables:view/manage`, `kot:view` (kitchen), `orders:cancel_item` (after KOT, with reason), `sales:waive_service_charge` |
| Receipts | A4/narrow | 80 mm bill and KOT (F4); table, covers, waiter; service charge line; FSSAI no. if required |
| Reports | Sales, payments | Item-wise sales, hourly sales, sales by order type, cancelled KOT items, waiter sales, day-end (Z) report, ingredient usage if recipes on |
| Settings | Tax | Order types on/off, service charge % (optional, waivable), parcel charge, FSSAI no., KOT printer per kitchen section |

**Verify with an accountant / lawyer**
- GST rate for restaurant service, whether input tax credit is allowed, and the composition scheme option.
- Whether a service charge may be added by default, and how it must be shown (consumer guidelines exist on this).
- FSSAI licence number on bills; packaging/parcel charge tax treatment.
- Alcohol sales (outside GST; state excise) — recommend out of scope.
- Tax on delivery-app orders (aggregators may collect and pay the tax).

**Build plan**

| Slice | Content | Effort | Risk |
|---|---|---|---|
| R1 | Menu items (F2) + modifiers + fast order grid | L | Peak-hour speed; checkout today calls the server for a quote on each change |
| R2 | Tables + open orders (F6) + move/merge | L | Concurrency when two waiters edit one table (use row version like `sales.version`) |
| R3 | KOT print + kitchen display (polling first, push later) | L | Printing from a browser is unreliable; KOT must never be lost |
| R4 | Bill split, service charge, parcel charge, 80 mm bill | M | Rounding across split bills |
| R5 | Day close / shift report | M | — |
| R6 | Recipes + ingredient stock | XL | Fractional quantities (grams, ml) clash with whole-number stock; keep out of v1 |
| R7 | Offline billing | XL | Conflicts with web-only decision (Q-1); keep out of v1 |

Shared: open orders (salon chairs, repair jobs), staff per line (waiter), thermal receipt, service charge pattern for salons.

---

## 9. Recommended build order

| Order | Work | Why |
|---|---|---|
| 1 | **Foundations:** F1 business type, F2 services, F3 tax on invoice, F4 thermal receipt | Needed by every type; F3 also helps today's retail shops (B2B bills) |
| 2 | **Mobile & electronics** (E1–E4) | Closest to today's retail flow; serials prove the "tracked units" pattern on a smaller scope than batches; low legal risk |
| 3 | **Medical** (M1–M7) | Large market and fits the product well, but batches are the riskiest stock change and legal rules need sign-off first; reuses the pattern from step 2 |
| 4 | **Salon** (F5, S1–S5) | Mostly new screens (calendar, packages) on top of F2/F5; little stock risk |
| 5 | **Restaurant** (R1–R5) | Most different workflow (tables, KOT, speed, printers); reuses open orders, staff lines and thermal receipts built earlier |

Rough total: foundations ~6–8 weeks; each type 1–3 months; restaurant the largest.

---

## 10. What stays unchanged

- Multi-tenancy (schema per tenant), provisioning, migrations runner, backups, exports.
- Auth, sessions, step-up, rate limits; the RBAC mechanism (new permissions are only **appended** to the catalog).
- Billing and Razorpay flows; plan limits code (new feature keys only).
- Payments, part payment, dues, credit limits, collections; refund rule A.
- Stock ledger and its append-only rule (new reasons only, e.g. `batch_writeoff`, `serial_receive`).
- Gapless document counters (new counter names only).
- Email outbox, receipts versioning, audit log, Super Admin console (small additions).
- Retail tenants: default `business_type = 'retail'` with no modules on, so nothing changes for them.

---

## 11. Impact on plans and pricing

| Topic | Proposal |
|---|---|
| Plan per type? | **No.** Keep the same plans for every type; the type only picks screens. Avoids a plans × types matrix. |
| Module gating | Most modules in all paid plans. Heavier ones (`appointments` reminders, `kot` kitchen display, `repairs`, multi-outlet) can be plan features. Needs new keys in `platform.plans.features`, in the `tenant_overrides.key` CHECK (platform migration), and in `featureLabels` on the landing page. |
| Limits | `max_products` may be low for pharmacies (thousands of SKUs); review limits per plan. Prescription images count toward `max_storage_mb`. |
| Paid extras | SMS/WhatsApp reminders billed per message or as an add-on (we pay per message). |
| Landing page | One landing section or page per type (SEO); signup pre-selects the type from the page. |
| Trial | Trial plan (hidden, `trial`) gets all modules so people can try them. |

---

## 12. General legal / tax points to verify (all types)

- Invoice numbering: whether a new series is needed each financial year (our counter is one gapless series per tenant).
- Mandatory B2B invoice fields (customer GSTIN, place of supply, HSN/SAC digits by turnover).
- E-invoicing (IRN/QR) and e-way bill thresholds — whether any of our target customers cross them.
- Data protection rules for customer data (phone, prescriptions, health data) and how long to keep it.

---

## 13. Questions for you (with my recommended default)

| # | Question | Recommended default |
|---|---|---|
| 1 | Is the build order in §9 right? | Yes: foundations → electronics → medical → salon → restaurant |
| 2 | Can the owner change the business type after signup? | Yes, in Settings; turning modules off only hides data |
| 3 | Medical: sell loose tablets as whole units of the smallest pack unit (no decimal quantities)? | Yes; decimals stay out of v1 |
| 4 | Medical: hard-block sale of expired batches, with no override? | Yes, hard block |
| 5 | Medical: hide scheduled drugs from the public shop page? | Yes, hidden by default |
| 6 | Salon: ship walk-in billing with stylists first, appointments second? | Yes (S1–S2 before S3) |
| 7 | Electronics: buy-back of used phones in v1? | No, later and only after legal check |
| 8 | Restaurant: recipes / ingredient stock and offline mode in v1? | No, both later |
| 9 | Receipts: browser print at 80 mm, or direct ESC/POS printer support? | Browser print first; ESC/POS later if customers ask |
| 10 | SMS/WhatsApp reminders? | Email only in v1; pick a provider later |
| 11 | Same plans for all types? | Yes, with a few module features as plan flags |
| 12 | Accountant/lawyer sign-off before each type launches? | Yes, a gate for each type |
| 13 | Store-scoped users (F8) in the foundations? | Yes, many chains need it; M effort |
