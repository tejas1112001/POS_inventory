# API Contract — Go Backend (Phase 1)

| | |
|---|---|
| Status | Phase 1 design, revised 2026-10-15 after review. Contract changes C-01…C-21 **approved** (Q-6) |
| Baseline | Node/Express backend at commit `94aeeb8` "Update POS inventory system" (code read in full; the code wins over PRD/TRD) |
| Goal | Keep routes and JSON shapes so the React SPA keeps working. Every deliberate change is listed in §4 with a reason. |

---

## 1. Conventions (new backend)

| Topic | Rule |
|---|---|
| Origin | SPA and API are served from the **same origin**: `https://<slug>.<BASE_DOMAIN>` (tenant) or `https://admin.<BASE_DOMAIN>` (platform). The frontend uses relative `/api/...` URLs. CORS is only needed in edge cases (see TENANCY_DESIGN §7). |
| Tenant | Comes from the **Host** header (validated), never from body/query/header fields. A token for tenant A on tenant B's host is rejected with `401`. |
| Auth | `Authorization: Bearer <access token>` (10 min, held in memory by the SPA). Refresh token in an `HttpOnly; Secure; SameSite=Strict` cookie named `__Host-rt` (host-only, no `Domain`). |
| Body | JSON only (plus `multipart/form-data` on upload endpoints). Max 1 MiB by default, 8 MiB on uploads. Unknown fields → `400` (see §4 C-04 for the POS payload exception list). Trailing garbage → `400`. A NUL character anywhere in the path, query, a JSON string, a form value or an imported CSV file → `400 VALIDATION_FAILED` (Postgres cannot store it). |
| IDs | Integer IDs as today (`BIGINT` identity per tenant schema). IDs are only meaningful inside one tenant. |
| Money | JSON **numbers** (as today), produced from `NUMERIC` / `decimal.Decimal`, rounded to the tenant currency's minor units. Never computed with floats on the server. |
| Dates | RFC 3339 timestamps in UTC. Date-only inputs (`YYYY-MM-DD`) are read in the tenant's timezone. |
| Language | **English only.** Every `error` message, email and receipt is English. No `Accept-Language` handling, no language setting, no translation files. Currency and number format come from tenant settings (`currency`, `locale` = an English number-format tag such as `en-IN`) |
| Lists | Existing list endpoints keep returning a **bare array** when `page`/`limit` are absent (contract, Q-4 decided), capped at `MAX_UNPAGED_ROWS` (5,000) with `X-Total-Count` and `X-Truncated: true` headers when capped. `page`+`limit` (limit ≤ 200) returns `{data, pagination{total,page,limit,totalPages}}` as today. |
| Errors | Always `{"error": "<human message>", "code": "<MACHINE_CODE>"}`; validation adds `"fields": {"field": "message"}`. No stack traces, SQL text or internal IDs. |
| Request ID | Every response carries `X-Request-Id` (also accepted inbound from the trusted proxy only). |

### 1.1 Error codes

| HTTP | `code` | When |
|---|---|---|
| 400 | `INVALID_JSON`, `VALIDATION_FAILED` | Malformed body, unknown fields, field rule violations |
| 401 | `UNAUTHENTICATED`, `INVALID_CREDENTIALS`, `TOKEN_EXPIRED`, `MFA_REQUIRED` | Missing/invalid token, bad login, expired access token (**was 403 before**, see C-03) |
| 403 | `FORBIDDEN`, `STEP_UP_REQUIRED`, `TENANT_SUSPENDED`, `TENANT_READ_ONLY`, `PLAN_LIMIT_REACHED`, `FEATURE_NOT_IN_PLAN`, `IMPERSONATION_READ_ONLY` | Permission or state denies the action |
| 404 | `NOT_FOUND` | Unknown route, unknown ID **in this tenant**, unknown tenant host (same body for all) |
| 409 | `CONFLICT`, `INSUFFICIENT_STOCK`, `PRICE_CHANGED`, `PRODUCT_LOCKED`, `INVALID_STATE`, `RETURN_EXCEEDS_SOLD` | Uniqueness, stock, stale price, POS lock, illegal state change, return above the quantity sold (C-13) |
| 413 | `BODY_TOO_LARGE` | Over the body cap |
| 415 | `UNSUPPORTED_MEDIA_TYPE` | Non-JSON body on a JSON endpoint, bad upload type |
| 429 | `RATE_LIMITED`, `ACCOUNT_LOCKED` | Rate limit or brute-force lockout (with `Retry-After`) |
| 500 | `INTERNAL` | Bug. Logged with the request ID; generic message to the client |
| 503 | `UNAVAILABLE` | Draining / dependency down |

---

## 2. Existing route inventory (as built, `backend/src/**`)

Legend: 🔓 public · 🔑 any valid token · `mod:act` = permission required by the **old** code. "New guard" is what the Go backend enforces. Status: **U** unchanged contract · **C** changed (see §4) · **F** behavior fix only (same request/response shape) · **N** new route.

### 2.1 Auth — `routes/auth.js`

| Method | Path | Old guard | Old behavior (from code) | New guard | Status |
|---|---|---|---|---|---|
| POST | `/api/auth/login` | 🔓 | `{email,password,remember}` → `{user,token,remember,permissions}`. 404 unknown email, 401 bad password. Inactive users **allowed**. 30 d / 12 h JWT. | 🔓 + rate limit + lockout | C-01, C-02, C-03 |
| POST | `/api/auth/signup` | 🔓 | Creates a user with **client-chosen `role`/`role_id`** (anyone can become admin), spreads `...rest` into `User.create`. Used by SignUp page **and** by AddBillerForm. | Removed from tenant hosts (`404`). Tenant user creation → `POST /api/users`. Business signup → platform flow, off by default. | C-05 |
| POST | `/api/auth/refresh` | 🔑 | Re-issues a fresh 30 d token from any still-valid token, **no status check** → endless session even for Inactive/demoted users. | Cookie-based rotation with reuse detection | C-02 |
| GET | `/api/auth/permissions` | 🔑 | `{permissions:[...]}`; `["*"]` for legacy admin without role_id | 🔑 | C-06 |

### 2.2 Catalogue — `products.js`, `productVariants.js`, `categories.js`, `subcategories.js`, `brands.js`, `units.js`, `suppliers.js`

| Method | Path | Old guard | Old behavior | New guard | Status |
|---|---|---|---|---|---|
| GET | `/api/products` | products:view | Array, or paginated with `page/limit` (limit unbounded). Filters `search,categoryId,brandId,status`. Enriched with first-variant `price`, total `qty`, nested `ProductVariants[].Inventories[]`, images inline. | products:view | F (caps, no deleted rows) |
| GET | `/api/products/pos?storeId=` | pos:view | Active products with inventory in the store; 400 `{message}` without storeId | pos:view | F (error shape) |
| GET | `/api/products/barcode/:barcode?storeId=` | pos:view | Match `item_barcode` **or** `sku` (non-unique → arbitrary match); 404 `{message}` | pos:view | F (unique barcode/SKU per tenant) |
| POST | `/api/products` | products:create | Single or variable product; validation errors thrown as plain `Error` → **500** | products:create + plan limit | F (400 on validation) |
| PUT | `/api/products/:id` | products:edit | Full replace. Variants absent from payload are **hard-deleted with their stock**. `findByPk(v.id)` does **not** check `product_id` → a variant of product B can be rewritten into product A (IDOR). Inventory rows replaced without audit. 409 `{message}` if POS-locked by another user. | products:edit | C-14 |
| DELETE | `/api/products/:id` | products:delete | Hard delete product, variants, inventory (order history loses links) | products:delete | C-12 (soft delete) |
| PUT | `/api/products/:id/lock` | pos:create | `{lock,storeId}`; read-then-write lock on the **product row** (global across stores, racy); 409/403 `{message}` | pos:create | C-15 |
| POST | `/api/product-variants` | 🔑 **(no permission)** | Any logged-in user can add a variant with any price to any product | products:edit | C-08 |
| GET/POST/PUT/DELETE | `/api/categories[/:id]` | categories:* | CRUD `{name,status,image}`; hard delete | categories:* | F, C-12 |
| GET | `/api/sub-categories` | 🔓 **(fully public)** | All sub-categories + parent name | categories:view | C-08 |
| POST | `/api/sub-categories` | 🔑 + legacy `isAdmin` (`role==='admin'`) | No validation | categories:create | C-08 |
| GET/POST/PUT/DELETE | `/api/brands[/:id]` | brands:* | CRUD | brands:* | F, C-12 |
| GET/POST/PUT/DELETE | `/api/units[/:id]` | units:* | CRUD `{name,short_name,status}` | units:* | F, C-12 |
| GET/POST/PUT/DELETE | `/api/suppliers[/:id]` | suppliers:* | CRUD | suppliers:* | F, C-12 |

### 2.3 Customers — `customers.js`

| Method | Path | Old guard | Old behavior | New guard | Status |
|---|---|---|---|---|---|
| GET | `/api/customers` | customers:view | All customers, unpaginated, with `createdBy` | customers:view | F |
| POST | `/api/customers` | customers:create | `{name,email,phone,address,city,country,image,status}` | customers:create | F (email format validated) |
| PUT | `/api/customers/:id` | customers:edit | Same body | customers:edit | F |
| DELETE | `/api/customers/:id` | customers:delete | Hard delete (sales keep a dangling `customer_id`) | customers:delete | C-12 |

### 2.4 Inventory — `inventory.js`, `stockAdjustments.js`

| Method | Path | Old guard | Old behavior | New guard | Status |
|---|---|---|---|---|---|
| GET | `/api/inventory` | inventory:view | Flat rows per variant×store, unpaginated | inventory:view | F |
| POST | `/api/inventory` | inventory:edit | Upsert absolute `qty` (negative allowed, no validation, no audit) | inventory:edit | F (qty ≥ 0, ledger + audit) |
| GET | `/api/stock-adjustments` | stock_adjustments:view | Optional pagination | stock_adjustments:view | U |
| POST | `/api/stock-adjustments` | stock_adjustments:create | Addition/Subtraction; subtraction check is **not locked** (race → negative stock) | stock_adjustments:create | F (atomic) |

### 2.5 Sales, returns, invoices — `sales.js`, `salesReturns.js`, `invoices.js`

| Method | Path | Old guard | Old behavior | New guard | Status |
|---|---|---|---|---|---|
| GET | `/api/sales` | sales:view | Filters `page,limit,search,startDate,endDate,storeId,customerId`; "billers" (`role==='biller'`, i.e. **every non-admin role**) see only own sales | sales:view; own-only unless `sales:view_all` | C-10 |
| POST | `/api/sales` | sales:create | **Prices, tax, discount, shipping, line totals all trusted from the client.** Stock checked without row locks (oversell). `...saleData` mass-assigned into `Sale.create`. `paid: 0` coerced to `total`. `Partial` status violates the enum → 500. POS sends `method` (not `payment_method`) and no `date` → POS sales have **NULL `payment_method` and NULL `date`**. | sales:create | C-04 |
| PUT | `/api/sales/:id` | sales:edit | Restores old stock, applies new; client values for totals mass-assigned without recompute; editor overwrites `user_id` (original biller lost); no ownership check | sales:edit (+ own-only rule) | C-04, C-10 |
| DELETE | `/api/sales/:id` | sales:delete | Restores stock, **hard-deletes** sale and items | sales:delete | C-11 (void) |
| GET | `/api/sales/returns` | sales_returns:view | Flattened rows, unpaginated | sales_returns:view | U |
| POST | `/api/sales/returns` | sales_returns:create | Qty check only vs **this** return (repeat returns exceed sold qty); negative qty accepted (decrements stock); refund `unitPrice` from client (can exceed sold price); store/customer not checked vs original sale | sales_returns:create | C-13 |
| GET | `/api/invoices` | invoices:view | View over **all** sales (no biller scoping), unpaginated; `dueDate` formatted `en-GB` string | invoices:view; own-only unless `sales:view_all` | C-10 |

### 2.6 Purchasing — `purchases.js`, `purchaseRoutes.js`

| Method | Path | Old guard | Old behavior | New guard | Status |
|---|---|---|---|---|---|
| GET | `/api/purchases` | purchases:view | With supplier + items | purchases:view | U |
| POST | `/api/purchases` | purchases:create | Totals trusted from client; per-line `variantId/storeId` used for stock-in **but not stored** (`purchase_items` has no such columns); `purchases:approve` never checked | purchases:create; `purchases:approve` to create as `Received` | C-09 |
| PUT | `/api/purchases/:id` | purchases:edit | `{status,payment_status,paid,due,storeId}`. Moving to `Received` stocks the product's **first variant** into `storeId` **defaulting to store 1**. Received → Pending → Received **stocks again each time**. | purchases:edit; `purchases:approve` for → Received | C-09 |
| GET | `/api/purchase/report` | reports:view | Rows per purchase item; loads all matching IDs into an `IN (...)` list | reports:view | F |

### 2.7 Reports & dashboard — `salesReports.js`, `dashboard.js`

| Method | Path | Old guard | Old behavior | New guard | Status |
|---|---|---|---|---|---|
| GET | `/api/sales/report` | reports:view | Rows per order item; filters on `sales.date` (NULL for POS sales → POS sales missing); biller sees **all** | reports:view; own-only unless `sales:view_all` | C-10 |
| GET | `/api/dashboard` | dashboard:view | Global totals; chart from the **earliest** 100 sales by month name (years merged); unpaid total ignores `Partial` | dashboard:view | C-16 |

### 2.8 Users, roles — `billers.js` (mounted twice), `roles.js`

| Method | Path | Old guard | Old behavior | New guard | Status |
|---|---|---|---|---|---|
| GET | `/api/users` and `/api/billers` | users:view (alias billers:view) | Query `q,status`; password excluded | users:view | U |
| PUT | `/api/users/:id`, `/api/billers/:id` | users:edit | Can set **any** `role_id` (incl. admin, incl. self) → privilege escalation; missing `email` blanks the email | users:edit + anti-escalation rules | C-07 |
| DELETE | `/api/users/:id`, `/api/billers/:id` | users:delete | Hard delete; can delete self / last admin | users:delete | C-07, C-12 |
| GET | `/api/roles` | roles:view | Roles + userCount + permissions + permissionIds | roles:view | U |
| GET | `/api/roles/permissions` | roles:view | All permission rows | roles:view | U |
| POST | `/api/roles` | roles:create | `{name,description,permissionIds}` | roles:create + anti-escalation | C-07 |
| PUT | `/api/roles/:id` | roles:edit | Can rename/edit **system** roles (edits silently reset on next boot) and grant any permission | roles:edit + anti-escalation | C-07 |
| DELETE | `/api/roles/:id` | roles:delete | Blocks system roles and roles with users | roles:delete | U |
| POST | `/api/roles/assign-user` | users:edit | Assign any role to any user incl. self; legacy `role` set to admin/biller | users:edit + anti-escalation | C-07 |

### 2.9 Stores & showcase — `stores.js`, `showcaseAdmin.js`

| Method | Path | Old guard | Old behavior | New guard | Status |
|---|---|---|---|---|---|
| GET | `/api/stores` | 🔑 **(no permission)** | Active stores | stores:view (seeded for biller too) | C-08 |
| GET | `/api/stores/:id` | 🔑 | Any store | stores:view | C-08 |
| POST | `/api/stores` | stores:create (**never seeded** → admins blocked) | `{name,address,status}` | stores:create + plan limit | F |
| PUT | `/api/stores/:id` | stores:edit | Missing fields overwrite with `undefined` | stores:edit | F |
| DELETE | `/api/stores/:id` | stores:delete | Hard delete → `{message}` | stores:delete | C-12 |
| GET | `/api/stores/:id/showcase` | 🔑 | **Mutates** (auto-creates slug) on GET | stores:view (every store gets a slug at creation). Also `isDefault` (this is the default public store) and `homePage` | C-08 |
| PUT | `/api/stores/:id/showcase` | stores:edit | `{slug,logo,banner,description,phone,openingHours,isPublicEnabled}` | stores:edit | F (slug rules, images C-17) |
| POST | `/api/stores/:id/showcase/toggle-product` | products:edit | Also writes global `products.is_published` (cross-store side effect) | products:edit | F |
| POST | `/api/stores/:id/showcase/batch-publish` | products:edit | `{publishAll}` or `{productIds,action}`; N+1 upserts; global flag side effect | products:edit | F |
| POST | `/api/stores/:id/showcase/default` | — | — | stores:edit. Makes the store the default public store (`settings.shop_store_id`) → `{isDefault:true,storeId}`. Only an active store with its showcase on (else 400); audited as `settings.update` | N |

### 2.10 Public shop — `publicShop.js` (no auth)

| Method | Path | Old behavior | New | Status |
|---|---|---|---|---|
| GET | `/api/public/shop/:slug` | Slug **or numeric id** (enumeration). Disabled shop → `{isPublicEnabled:false,message}` | Tenant from host, store by slug in that tenant; rate limited; cacheable | C-18 |
| GET | `/api/public/shop/:slug/products` | `page,limit(unbounded),search,categoryId,brandId,sort,featured`; price sort per page only | limit ≤ 48; price sort in SQL | C-18 |
| GET | `/api/public/shop/:slug/products/:productId` | Id or slug; **unpublished visible when `NODE_ENV=development`** | Published only | C-18 |
| GET | `/api/public/shop/:slug/categories` | All categories (not store-filtered) | Categories that have published products in the store | F |
| GET | `/api/public/shop/:slug/featured` | Up to 8 | Same | U |
| GET | `/api/public/home` | — | What the main address shows: `{homePage:"shop",slug}` or `{homePage:"login"}` (home page set to login, tenant suspended, plan without `public_shop`, or no active public store). The slug is the default store: the chosen one (`shopStoreId`), else slug `main`, else the oldest; each must be active and public, so a store turned off falls back to the next. `Cache-Control: no-cache` + ETag; same rate limits | N |
| GET | `/api/public/shops` | — | Store switcher: `{stores:[{name,slug}]}`, the active public stores by name (at most 50). 404 for suspended tenants and plans without `public_shop`. `Cache-Control: no-cache` + ETag; same rate limits | N |

### 2.11 Misc

| Method | Path | Old | New | Status |
|---|---|---|---|---|
| GET | `/health` | `OK` text | Kept as alias of `/healthz` | U |
| — | `/api/warehouses` (GET public, POST isAdmin) | **Not mounted**; frontend `WarehouseModal` gets 404 | Mounted with `stores:*` permissions (Q-12 decided: build) | C-19 |
| — | `POST /products` (frontend `ImportProduct`) | No such route (404) | `POST /api/products/import` (CSV), built in step 7 (Q-12 decided: build) | C-19 |

Count: **78** mounted method+path pairs in the old backend (incl. the `/api/billers` aliases), plus 2 dead frontend calls.

---

## 3. Endpoint guards: default permission seed (new)

New permissions are added to the catalog; seeded system roles: **owner** (all, immutable, exactly one or more owners, assigned only by provisioning/ownership transfer), **admin** (all except `billing:*`, `tenant:*`, immutable set), **biller** (editable, seeded once — no longer reset on each boot).

| Module | Actions | Biller default |
|---|---|---|
| dashboard | view | view (own data only) |
| pos | view, create | view, create |
| products | view, create, edit, delete, import | view |
| categories, brands, units, suppliers | view, create, edit, delete | — |
| customers | view, create, edit, delete | view, create |
| inventory | view, edit | view |
| stock_adjustments | view, create | — |
| sales | view, **view_all**, create, edit, delete, **price_override**, **discount** | view, create, discount (cap set in settings) |
| sales_returns | view, create | — |
| purchases | view, create, edit, approve | — |
| invoices | view | view |
| reports | view, export | view (own data only) |
| users | view, create, edit, delete | — |
| roles | view, create, edit, delete | — |
| **stores** | view, create, edit, delete | **view** |
| **settings** | view, edit | view |
| **email_settings** | view, edit | — |
| **receipts** | view, resend | view, resend (own sales) |
| **audit** | view | — |
| **billing** | view, manage | — (owner only) |

`billers:*` permissions are merged into `users:*` (the alias logic in `permission.js` becomes data, not code).

---

## 4. Deliberate contract changes

All approved (Q-6, 2026-10-15). "FE" = frontend change needed.

| ID | Change | Why | FE impact |
|---|---|---|---|
| C-01 | Login: unknown email and wrong password both return `401 INVALID_CREDENTIALS` (was 404 / 401). Inactive users → `401` same body. Suspended tenant → `403 TENANT_SUSPENDED`. Lockout → `429 ACCOUNT_LOCKED`. | No user enumeration; S-req 2, 8 | None (shows `error` text) |
| C-02 | Session model: login/refresh still return `{user, token, remember, permissions}` but `token` is a **10-min** access token. Refresh token is set as `__Host-rt` cookie (30 d sliding if `remember`, else browser-session cookie with 12 h absolute cap). `POST /api/auth/refresh` reads the cookie (no Bearer needed), rotates it, returns the same body. Reuse of an old refresh token revokes the whole family. New `POST /api/auth/logout`. | S-req 26; removes 30-day bearer tokens from `localStorage` | `services/api.js`, `context/PosContext.jsx`, `lib/authStorage.js`: keep token in memory, call refresh with `credentials:'include'`, call logout |
| C-03 | Expired/invalid access token → `401 TOKEN_EXPIRED/UNAUTHENTICATED` (was **403**). | Correct semantics; the frontend's retry-on-401 logic finally works | None (fixes existing FE logic) |
| C-04 | `POST/PUT /api/sales`: server is the only source of prices, tax, discount and totals. Client `unitPrice` is compared with the catalog price: different and no `sales:price_override` → `409 PRICE_CHANGED` (with current prices) instead of silently charging the client's number. With `sales:price_override` (Q-3) the client price is used, and the line stores both prices and is audited (`sale.price_override`). Cart discount is allocated pro-rata to lines **before tax** (Q-2). Client `subtotal/tax/total/due/payment_status` are accepted but **ignored**. `discount` validated against permission/cap. `paid: 0` respected; `payment_status` computed (`Paid/Partial/Unpaid`). Accepted POS aliases: `method`→`payment_method`, `paymentNote`, `saleNote`→`note`, `receivedAmount`/`amountTendered`→`tendered`; other PayForm keys (`payingAmount`, `change`, `paymentType`, `quickCash`) are on an explicit ignore list, so strict decoding does not break POS. `date` defaults to now. Response shape unchanged (+ `receiptUrl`). PUT keeps the original `user_id`, records `updated_by`. Optional `Idempotency-Key` header. | P0 price tampering, G3, G5, NULL date bug, double-submit | `AddSales.jsx` must send `storeId` (it never did → back-office sales always failed). POS label "Tax (8%)" → "Tax". |
| C-05 | `POST /api/auth/signup` removed on tenant hosts. New `POST /api/users` (users:create) `{name,email,password?,role_id,status}`; with no password an invite email is sent. Public business signup (if enabled) is a platform flow. | G1/S1, S-req 3 | `AddBillerForm.jsx` → `/api/users`; `SignUp.jsx` route removed on tenant hosts |
| C-06 | `GET /api/auth/permissions` never returns `["*"]` | Legacy path removed | None |
| C-07 | Anti-escalation: an actor can only grant roles/permissions that are a subset of their own; nobody can change their own role or status, delete themselves, or remove the last owner; `owner` and `admin` roles are immutable; `owner` cannot be assigned via these endpoints. Role/status change revokes the target's sessions. Missing `email` on PUT no longer blanks it. | Privilege escalation found in code | Error toasts only |
| C-08 | Previously unguarded routes get permissions (`stores:view`, `categories:view`, `products:edit`, `categories:create`). `GET /api/stores/:id/showcase` no longer writes (every store gets a slug at creation). | G10 | None for seeded roles (biller gets `stores:view`) |
| C-09 | Purchases: `purchase_items` store `variant_id` + `store_id`; stock-in uses them. `Received` is **terminal** (any change away from it → `409 INVALID_STATE`; corrections via stock adjustment). Setting `Received` needs `purchases:approve`. `storeId` in PUT body ignored. Totals recomputed server-side. | G6, double stock-in bug | AddPurchase already sends `variantId/storeId` per line; status dropdown should hide `Received` without approve |
| C-10 | Object-level visibility: users without `sales:view_all` see only their own sales in `/api/sales`, `/api/invoices`, `/api/sales/report`, dashboard, receipts. Today's rule (`role === 'biller'`) restricts *every* custom role and none of the reports. | IDOR / data leak | None |
| C-11 | `DELETE /api/sales/:id` → **void** (status `Void`, stock restored, kept for audit), still `204`. Voided sales hidden from lists/reports unless `?includeVoided=true`. | Financial records must not be hard-deleted | None |
| C-12 | Master-data/user/store/product deletes → **soft delete** (`204`/`{message}` unchanged). Soft-deleted rows disappear from lists and pickers; history keeps names. Unique constraints ignore deleted rows. | G9 | None |
| C-13 | Returns: qty > 0; cumulative returned qty ≤ sold qty per order line (locked); store and customer must match the linked sale; refund price = sold price for linked sales (client `unitPrice` ignored); unlinked returns need `sales_returns:create` + `sales:price_override`. | G7, negative-qty bug, refund inflation | None |
| C-14 | `PUT /api/products/:id`: variant IDs must belong to the product (else 400); variants left out are soft-deleted, and refused (`409`) if they still hold stock; inventory changes are written to the stock ledger. | IDOR + silent stock loss | None |
| C-15 | POS lock is per (store, product) in a `pos_locks` table, atomic, 2-min TTL, `storeId` required. Product JSON keeps `isLocked/lockedAt/locked_by` (derived for the requested store). Lock never blocks back-office edits of other fields; checkout itself is protected by row locks, not by the soft lock. | Racy global lock | None |
| C-16 | Dashboard accepts `storeId,startDate,endDate,userId` and honors own-only scope; `revenueData[].month` becomes `"Mon YYYY"` grouped by calendar month in tenant timezone, latest 12 months. Same keys. | G15, global dashboard | Chart labels only |
| C-17 | Image fields (`image`, `logo`, `banner`) still accept a **data URI** (png/jpeg/webp ≤ 2 MiB) for compatibility; the server decodes, validates, re-encodes, stores it as a file and saves a tenant-scoped URL. Responses return URLs, not base64. New `POST /api/files` (multipart) for the FE to migrate to later. SVG rejected. | S-req 14, payload size | None now; later FE upload |
| C-18 | Public shop: tenant from host; numeric-id store lookup removed (every store has a slug); dev-mode unpublished bypass removed; `limit` ≤ 48; rate limited. | Enumeration, info leak, DoS | `PublicShopPage.jsx` must use relative URLs (it hard-codes `http://localhost:3000`) |
| C-19 | `/api/warehouses` mounted (`{name,address,status}`, same shape as the unmounted old route) and `POST /api/products/import` added (§5.1.1). | Dead UI (Q-12: build both) | `ImportProduct.jsx` posts to `/api/products/import` instead of `/products`; "Download Sample File" uses the template endpoint |
| C-20 | `GET /api/stores` (and selectors) unaffected, but stores beyond plan limit cannot be created (`403 PLAN_LIMIT_REACHED`); same for users and products. | Billing | Toast |
| C-21 | Login on a suspended or pending-deletion tenant blocked (`403 TENANT_SUSPENDED`); read-only tenant: all writes → `403 TENANT_READ_ONLY` **except the billing routes** (`GET /api/billing/subscription`, `POST /api/billing/checkout`) so the owner can pay. A cancelled tenant keeps full access until its paid period ends. One rule: BILLING_DESIGN §3. | Billing lifecycle | Banner (new) |

---

## 5. New endpoints

### 5.1 Tenant host (`<slug>.<BASE_DOMAIN>`)

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/api/public/tenant-info` | 🔓 rate-limited (30/min per IP, 300/min per tenant) | Sign-in page branding. Returns **only** `{"name": "<business name>", "logoUrl": "/files/t/<key>" \| null}`. Unknown, deleted, failed or provisioning host → the **same generic `404`** (same body, same code path, negative-cached) as any unknown tenant host, so it adds no enumeration signal. Answers for `suspended`/`read_only` tenants too (the name is shown on the sign-in page anyway). `Cache-Control: public, max-age=300` + `ETag`. No status, plan, slug history or contact data |
| POST | `/api/auth/logout` | cookie | Revoke refresh family, clear cookie |
| GET | `/api/auth/me` | 🔑 | User, permissions, tenant (name, currency, number-format locale), impersonation flag, banners count |
| POST | `/api/auth/password/forgot` | 🔓 rate-limited | Always `202`; emails a reset link if the account exists (platform SMTP) |
| POST | `/api/auth/password/reset` | 🔓 token | `{token,password}`; revokes all sessions |
| POST | `/api/auth/invite/accept` | 🔓 token | Owner/user sets first password |
| POST | `/api/auth/password/change` | 🔑 | `{currentPassword,newPassword}`; revokes other sessions |
| POST | `/api/auth/impersonation/exchange` | 🔓 one-time code | Platform impersonation hand-off (TENANCY §9) |
| POST | `/api/users` | users:create | Replaces signup-as-create |
| GET/PUT | `/api/settings` | settings:view / settings:edit | Business profile, currency, number format (`locale`, English tags only, `^en-[A-Z]{2}$`), timezone, tax config, receipt template. No language field; `homePage` (`login` default, or `shop`) and `shopStoreId` (the default store, shown at `/`; null: slug `main`, else the oldest active public store; a chosen store that is off or inactive falls back the same way) |
| GET/PUT/DELETE | `/api/settings/email` | email_settings:* | Tenant SMTP (password write-only; GET returns `passwordSet: true`) |
| POST | `/api/settings/email/test` | email_settings:edit | Send test email to `{to}` |
| GET | `/api/sales/:id` | sales:view (+ own-only) | Single sale (receipt page reload) |
| GET | `/api/sales/:id/receipt` | receipts:view (+ own-only) | PDF download |
| POST | `/api/sales/:id/receipt/resend` | receipts:resend | `{email?}`; rate-limited; enqueues email |
| GET | `/api/stock-movements` | inventory:view | Stock ledger (paginated) |
| POST | `/api/files` | any create/edit permission on the target entity | Image upload |
| GET | `/files/t/<key>` | 🔓 allow-list only | **Public route, narrow allow-list** (decided round 2). Serves only: the tenant logo, store logos and banners, and images of **published** products (published in at least one store, not deleted). Decided per request from `files.purpose` plus the owning row; the host must match the file's tenant; anything else → the generic `404`. **Customer and user images** are served only behind login (`GET /api/files/:id`, 🔑 + permission on the owning entity). **Receipts and exports are never served on `/files/t/`** (receipts: `/api/sales/:id/receipt`; exports: platform only) |
| GET | `/api/audit-log` | audit:view | Tenant audit log (paginated, filters) |
| GET | `/api/notifications/banners` | 🔑 | Active platform banners + billing banner |
| POST | `/api/notifications/:id/dismiss` | 🔑 | Per-user dismissal |
| GET | `/api/billing/subscription` | billing:view | Plan, status, usage vs limits, invoices |
| POST | `/api/billing/checkout` | billing:manage | Creates/returns Razorpay subscription link (`short_url`) |
| POST | `/api/products/import` | products:import (+ plan limit) | CSV import, see §5.1.1 |
| GET | `/api/products/import/template` | products:import | Sample CSV (header row + one example row) |
| GET | `/api/warehouses`, POST, PUT/:id, DELETE/:id | stores:view / create / edit / delete | `{name,address,status}`; DELETE is a soft delete (C-12) |

#### 5.1.1 Product CSV import (`POST /api/products/import`)

- `multipart/form-data`, file field **`csvFile`** (what `ImportProduct.jsx` already sends; its extra `product` field is ignored). UTF-8 (BOM allowed), comma-separated, ≤ 5 MiB, ≤ 5,000 data rows, header row required. Query `?dryRun=true` validates everything and writes nothing.
- Columns (header names, case-insensitive): `name`*, `sku`*, `price`*, `cost`, `barcode`, `category`, `sub_category`, `brand`, `unit`, `tax_type` (`Exclusive`/`Inclusive`), `tax_rate`, `hsn_code`, `store`, `opening_qty`, `quantity_alert`, `status`. `*` = required. One row = one single-variant product; rows sharing a `name` with different `sku`s become variants of one variable product.
- Lookups by name (category, brand, unit, store slug) must already exist; unknown → row error (no silent creation).
- **Create-only**: an existing SKU or barcode is a row error (no updates in v1).
- **All or nothing**: one tenant transaction; any row error → nothing is written. Response `200 {"created": n, "variants": n, "errors": []}` or `400 VALIDATION_FAILED` with `{"errors": [{"row": 12, "field": "price", "message": "must be ≥ 0"}]}` (max 200 errors listed).
- Opening stock writes `stock_movements` rows with reason `import`. Audited (`products.import`, counts only).
- Cells starting with `=`, `+`, `-`, `@` are stored as text and prefixed with `'` on any later spreadsheet export (CSV/formula injection).

### 5.2 Platform host (`admin.<BASE_DOMAIN>`, prefix `/api/platform`) — never routed on tenant hosts

| Method | Path | Platform permission | Notes |
|---|---|---|---|
| POST | `/auth/login` | 🔓 strict rate limit | Step 1 → `{mfaRequired:true, challengeId}` or `{passwordChangeRequired:true}` |
| POST | `/auth/mfa/verify` | challenge | TOTP or recovery code → session |
| POST | `/auth/mfa/enroll`, `/auth/mfa/enroll/confirm` | enrolling session | Mandatory at first login; returns recovery codes once |
| POST | `/auth/password/change` | session | Forced at first login |
| POST | `/auth/step-up` | session | Re-verify TOTP; valid 5 min for destructive actions |
| POST | `/auth/refresh`, `/auth/logout` | cookie | Short sessions |
| GET | `/me` | session | |
| GET | `/tenants` | tenants:view | Filters: status, plan, expiring, q; usage summary |
| POST | `/tenants` | tenants:create | Provision `{name, slug, ownerEmail, ownerName, planCode, trialDays}` → `202` + provisioning id |
| GET | `/tenants/:id` | tenants:view | Status, plan, usage vs limits, last activity, schema version, storage use (no secrets) |
| PATCH | `/tenants/:id` | tenants:edit | Name, billing email, notes |
| POST | `/tenants/:id/slug` | tenants:edit + step-up | Rename slug (keeps redirect) |
| POST | `/tenants/:id/suspend`, `/reactivate` | tenants:suspend + step-up | Reason required |
| DELETE | `/tenants/:id` | tenants:delete + step-up | → `pending_deletion` (retention window) |
| POST | `/tenants/:id/restore` | tenants:delete + step-up | Cancel pending deletion |
| POST | `/tenants/:id/provisioning/retry` | tenants:create | Resume failed provisioning |
| POST | `/tenants/:id/impersonate` | tenants:impersonate + step-up | `{reason, mode:'read_only'|'read_write'}` → `{code, url, expiresAt}` |
| GET | `/impersonations`, DELETE `/impersonations/:id` | tenants:impersonate | List / revoke |
| GET/POST/PATCH | `/plans[/:id]` | plans:manage | |
| POST | `/tenants/:id/subscription` | billing:manage | Assign/change plan |
| POST | `/tenants/:id/subscription/extend-trial`, `/grace`, `/cancel` | billing:manage (+ step-up for cancel) | |
| GET | `/billing/invoices`, `/billing/expiring`, `/billing/past-due` | billing:view | |
| POST | `/billing/reconcile` | billing:manage | Pull Razorpay state, fix drift |
| POST | `/tenants/:id/reminders` | billing:manage | Manual reminder email |
| POST/GET | `/notifications` | notifications:send / view | Announcement to one / group / all (email + banner) |
| GET | `/migrations/status`, `/migrations/drift` | migrations:view | |
| POST | `/migrations/run` | migrations:run + step-up | `{dryRun, tenantIds?, parallelism}` |
| POST | `/tenants/:id/exports` | tenants:export + step-up | Per-tenant export `{includeFiles?}` → `202` + the queued export; the worker runs it. `409` while one is queued or running, or when exports are not configured |
| GET | `/tenants/:id/exports` | tenants:export | The tenant's last 50 exports (status, size, SHA-256, counts, error). Archives are never downloadable from the API |
| POST | `/tenants/:id/restore-tests` | tenants:export | Queue a restore of the latest (or `{exportId}`) succeeded export of this tenant into a scratch schema, compared and dropped → `202`. Run by `posctl tenant restore-tests` (only posctl holds the age identity). `404` without a succeeded export of this tenant |
| GET | `/tenants/:id/restore-tests` | tenants:export | The tenant's last 50 restores (any mode): status, `verified`, rows, per-table counts and checksums |
| GET | `/audit` | audit:view | Platform audit log |
| GET/POST/PATCH | `/users[/:id]` | platform_users:manage (super_admin) | Platform staff |
| POST | `/webhooks/razorpay` | 🔓 signature | Raw-body HMAC, idempotent (BILLING_DESIGN §4) |

### 5.3 Infrastructure (any host, or internal port)

| Method | Path | Notes |
|---|---|---|
| GET | `/healthz` | Liveness, no dependencies |
| GET | `/readyz` | DB + job queue + platform migration version; 503 while draining |
| GET | `/metrics` | **Internal listener only** (`METRICS_ADDR`), never routed by the proxy |
| GET | `/internal/tls/ask?domain=` | Internal only; Caddy on-demand TLS check (custom domains, later) |

### 5.4 Main address (`<BASE_DOMAIN>`, landing site)

Server-rendered HTML and one CSS file (`internal/landing`), no JavaScript, strict CSP (OPERATIONS §7). English only. Brand = `PLATFORM_NAME`, every URL from `PUBLIC_SCHEME` + `BASE_DOMAIN` (+ `PUBLIC_PORT` in development). No `/api` routes on this host (JSON `404`). No sign-up.

| Method | Path | Notes |
|---|---|---|
| GET | `/` | Hero, features, how it works, pricing, FAQ, Find my shop form, contact form. Pricing reads `platform.plans` where `is_public AND is_active` (cheapest first, cached 1 min); none → "Contact us for pricing". Title, description, canonical, Open Graph, Twitter and JSON-LD (`Organization`, `SoftwareApplication` with one `Offer` per public plan, `FAQPage`). `Cache-Control: public, max-age=300` |
| GET | `/find-my-shop`, `/contact`, `/terms`, `/privacy` | Indexable pages. Terms and Privacy are marked placeholders (`internal/landing/templates/terms.html`, `privacy.html`) |
| POST | `/find-my-shop` | Form `email`. Always `303 → /find-my-shop/sent` with the same page. Each valid request queues one `find_shop` row in `platform.email_outbox` (key: hash of the address + 15-minute bucket); the worker looks up shops whose `owner_email` or `billing_email` matches and sends their `/login` addresses, or cancels the row when there are none. Staff emails are not looked up |
| POST | `/contact` | Form `topic` (`demo`\|`question`), `name`, `email`, `business?`, `phone?`, `message` (required for a question). Stored in `platform.contact_requests`; with `CONTACT_EMAIL` set, a `contact_request` email (Reply-To: the visitor) is queued in the same transaction. `303 → /contact/sent`; `422` re-renders with field errors |
| GET | `/login` | Staff sign-in helper: shop address field. `noindex`; CSP `form-action` also allows `*.<BASE_DOMAIN>` |
| GET | `/login/go?shop=` | `303` to `<slug>.<BASE_DOMAIN>/login` for any well-formed, non-reserved slug (never checks that it exists); else `422` |
| GET | `/robots.txt`, `/sitemap.xml`, `/favicon.svg`, `/assets/site-<hash>.css` | Robots allows all and names the sitemap; noindex pages say so themselves |
| GET | anything else | HTML `404`, `noindex` |

Both forms: same-origin `Origin` required (`403`), `application/x-www-form-urlencoded` only, 16 KiB body. Spam checks that answer like a success and do nothing: hidden `website` field filled, missing or forged signed time stamp `ft` (HMAC from `MASTER_KEYS`), sent under 2 s after the page; a stamp over 24 h old asks to send again. Rate limits per IP (IPv6 /64), in memory per instance: Find my shop 5/hour (`429` page), plus 3 emails per address per hour (over it: dropped quietly, same reply); contact 5/hour per IP and 100/hour in total; `/login/go` 60/minute.

Search engines: the admin host sends `X-Robots-Tag: noindex, nofollow` on every response; tenant staff pages (`/login`, back office) send `noindex`; tenant storefront pages (`/`, `/product/*`, `/shop/*`) are indexable.

---

## 6. Frontend changes (minimal set, Phase 2)

| File | Change | Reason |
|---|---|---|
| `src/services/api.js` | Relative base URL by default; access token in memory; `credentials:'include'` on refresh; handle `code` | C-02, same-origin |
| `src/context/PosContext.jsx` | Refresh via cookie; logout calls API; bootstrap with `/api/auth/refresh` on load | C-02 |
| `src/lib/authStorage.js` | Stop storing tokens in local/sessionStorage (keep `savedEmail`, `rememberSession`) | C-02 |
| `src/pages/SignIn.jsx` | Show tenant business name (from `/api/public/tenant-info`), "Forgot password" link | Subdomain login |
| `src/pages/SignUp.jsx`, `App.jsx` | Remove `/signup` route on tenant hosts | C-05 |
| new `src/pages/ResetPassword.jsx`, `AcceptInvite.jsx` | Set/reset password pages | New flows |
| `src/components/forms/AddBillerForm.jsx` | POST `/api/users` | C-05 |
| `src/pages/AddSales.jsx` | Add store selector, send `storeId` | Existing bug |
| `src/pages/PublicShopPage.jsx` | Relative URLs | G14 |
| `src/lib/format.js` | `formatCurrency` uses tenant currency and number-format locale from `/api/auth/me` (`Intl.NumberFormat`); all UI text stays English, no i18n library | INR hard-coded |
| `src/components/ImportProduct.jsx` | POST `/api/products/import`; sample file from `/api/products/import/template`; show row errors | C-19 |
| `src/pages/AddSales.jsx`, `AddSalesReturn.jsx`, `Dashboard.jsx` | Replace literal `₹` with currency symbol helper | INR hard-coded |
| `src/pages/POS.jsx` | "Tax" label; handle `409 PRICE_CHANGED`; show receipt email field | G5, C-04, receipts |
| `src/components/Receipt.jsx` | Tenant business name/logo/address from settings; "Download PDF"/"Resend" | Receipts |
| `src/pages/Settings.jsx` (+ new sub-pages) | Business profile/currency/tax, SMTP settings + test, receipt template | Settings |
| `src/pages/Sales.jsx` / `SalesList.jsx` | Resend receipt / download actions | Receipts |
| `src/components/layout/AppShell.jsx` | Banner area (billing + announcements + impersonation flag) | Billing |
| new `admin-console/` Vite entry (separate bundle) | Super Admin console, **built without MUI** (plain React + CSS Modules, decided round 2). Must work under the strict admin CSP (OPERATIONS §7): static JS/CSS only, no inline scripts or runtime-injected styles; tenant-supplied text rendered as plain text only (`react/no-danger` ESLint rule as an error, no HTML/Markdown rendering, no tenant-supplied URLs in `href`/`src`) | Platform, THREAT_MODEL §3.6 |
| Electron leftovers (`frontend/electron.js`, `frontend/preload.js`, root `package.json` script `start:electron`) | Removed at cutover | Web only (Q-1) |
| `vite.config.js` | Multi-page build (tenant app + admin console); dev proxy `/api` → Go; `server.allowedHosts: ['.localhost']` | Same-origin dev |
| `frontend/.env*` | `VITE_API_URL` optional (empty = same origin) | No hard-coded domain |
