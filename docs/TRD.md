# ReactronPOS — Technical Requirements Document (TRD)

| | |
|---|---|
| **Scope** | Architecture, technology stack, database, API contract, security, and operations, as built |
| **Source of truth** | Code on `main` (commit `94aeeb8`) |
| **Companion doc** | [PRD.md](./PRD.md): features and business rules |
| **Date** | 2026-10-14 |

---

## 1. System Architecture

```
┌───────────────────────────────┐        ┌──────────────────────────────┐
│  Browser / Electron renderer  │  HTTPS │   Express 5 API (Node, ESM)  │
│  React 19 SPA (HashRouter)    │ ─────▶ │   /api/*   /health           │
│  Vite 7 build, Tailwind 4     │  JSON  │   JWT auth + RBAC middleware │
│  Bearer JWT in local/session  │ ◀───── │   Sequelize 6 ORM            │
│  storage                      │        └──────────────┬───────────────┘
└───────────────────────────────┘                       │ pg (pool 5–25)
        ▲                                               ▼
        │ public, no auth                   ┌──────────────────────────┐
┌───────┴──────────────┐                    │      PostgreSQL          │
│ Public shop visitor  │──/api/public/shop─▶│  schema via sync(alter)  │
└──────────────────────┘                    └──────────────────────────┘

Electron desktop mode: electron.js imports backend/src/index.js and calls
startServer() in the main process, then loads dist/index.html (packaged) or
http://localhost:5173 (dev) in a BrowserWindow (contextIsolation on, nodeIntegration off).
```

### 1.1 Repository layout
```
/                       npm workspaces root (frontend, backend); concurrently runs both
├─ backend/
│  ├─ src/index.js      Express app factory: startServer()
│  ├─ src/config/db.js  Sequelize instance + connectDB() (authenticate + sync alter)
│  ├─ src/models/       24 Sequelize models + associations (models/index.js)
│  ├─ src/routes/       26 route modules (≈3.3k LOC); seed.js is a CLI script
│  ├─ src/middleware/   auth.js (JWT), permission.js (RBAC), admin.js (legacy)
│  ├─ src/services/     permissionSeed, permissionService (cache), concurrencycontrol (unused)
│  ├─ src/utils/        ExpressError, wrapAsync, toObjectWithId shims
│  └─ reset_passwords.js  dev utility (hard-coded demo credentials)
├─ frontend/
│  ├─ electron.js, preload.js   desktop shell
│  ├─ src/pages/        34 route pages
│  ├─ src/components/   layout, lists, forms, ui (shadcn-style Radix wrappers), shared
│  ├─ src/context/      PosContext (auth), PermissionContext, ThemeContext, ErrorContext
│  ├─ src/services/api.js  fetch wrapper with token refresh
│  └─ src/lib/          authStorage, format (INR), utils (cn)
├─ scripts/             e2e, audit, benchmark, concurrency test scripts; fix-deps postinstall
├─ index.js             legacy server entry (stale, not used)
└─ *.md                 README, USER_GUIDE, PERMISSIONS, SAAS_ARCHITECTURE, FLOW_TEST
```

---

## 2. Technology Stack

| Layer | Technology | Version (package.json) | Notes |
|---|---|---|---|
| Runtime | Node.js | ESM (`"type": "module"`) | |
| Web framework | Express | ^5.2.1 | Async errors propagate to the error middleware |
| ORM | Sequelize | ^6.37.7 | `pg` ^8.13, `pg-hstore` |
| Database | PostgreSQL | (any supported) | JSONB used for variant attributes |
| Auth | jsonwebtoken ^9, bcrypt ^6 | | HS256 by default |
| Config | dotenv ^16 | | |
| Dev | nodemon, ESLint 9 | | |
| UI framework | React | ^19.1 | |
| Routing | react-router-dom | ^7.12 | **HashRouter** (works from `file://` in Electron) |
| Build | Vite | ^7.0 | `base: './'`, alias `@ → src` |
| Styling | Tailwind CSS 4 (`@tailwindcss/vite`), tw-animate-css, CVA, clsx, tailwind-merge | | Theme tokens via CSS variables |
| Components | Radix UI (dialog, select, popover, dropdown, checkbox, avatar, scroll-area, label, slot), Headless UI, MUI 7 + Emotion (legacy parts) | | |
| Icons | lucide-react, @heroicons/react, @mui/icons-material | | |
| Tables | @tanstack/react-table ^8 | | |
| Charts | react-chartjs-2 ^5 (+ chart.js peer) | | `chart.js` is not declared directly (installed transitively) |
| Export | jspdf 4 + jspdf-autotable 5, write-excel-file 2 | | |
| Desktop | Electron | **not declared** in any package.json | See G18 in PRD |

---

## 3. Backend Design

### 3.1 Startup sequence (`startServer`)
1. Build the Express app: disable `x-powered-by`, CORS, JSON/urlencoded parsers (10 MB limit), `/health`.
2. Mount the routers (§5).
3. Add a 404 handler → `ExpressError('Route not found', 404)`.
4. Add the global error handler → `{ error: message }` with `err.status`, 400 for `CastError`, else 500.
5. `connectDB()`: `sequelize.authenticate()`, then **`sequelize.sync({ alter: true })`**. The process exits if this fails.
6. `seedPermissions()`: idempotent upsert of permissions and system roles (non-fatal).
7. `app.listen(PORT||3000, HOST||'0.0.0.0')`.

The server starts automatically only when `NODE_ENV=development` or `RUN_SERVER=true`. Otherwise the module exports `startServer` for Electron.

### 3.2 CORS
- Allowed: requests with no `Origin` header (curl, Electron `file://`), and any origin listed in `CORS_ORIGIN` (comma-separated).
- In development, any `http(s)://localhost|127.0.0.1:*` origin is also allowed.
- `credentials: true`.

### 3.3 Request pipeline (protected routes)
```
authenticateToken ──▶ requirePermission(module, action) ──▶ handler
  • Bearer token required (401)          • roleId → permission Set from cache (60 s TTL)
  • jwt.verify (403 on invalid/expired)  • users ⇄ billers treated as aliases
  • legacy token w/o roleId → DB lookup  • no roleId: role=admin → allow all;
  • req.user = {userId, role, roleId}      role=biller → static whitelist
                                         • else 403
```
`isAdmin` (legacy) checks `req.user.role === 'admin'`. It is used only by sub-categories POST and warehouses POST.

### 3.4 Permission service
- `getPermissionsForRole(roleId)` loads the role with its permissions and caches a `Set<"module:action">` in process memory for 60 s.
- `invalidateRoleCache(roleId)` is called after role create/update/delete and user role assignment.
- The cache is per process. Horizontal scaling gives up to 60 s of staleness on other nodes.

### 3.5 Transactions
Unmanaged Sequelize transactions (`sequelize.transaction()` + commit/rollback) wrap:
product create, update, and delete · sale create, update, and delete · purchase create and update · sales-return create · stock-adjustment create · role create and update.
The isolation level is PostgreSQL's default **READ COMMITTED**. No row-level locks are taken in the live routes (see §8).

`services/concurrencycontrol.js` has lock-aware alternatives (`lock: true`, optimistic `version`), but **no route uses it**. It also refers to `Product.version`, which is not defined in the model.

### 3.6 Error-handling conventions
- Most handlers catch errors locally and return `500 { error: err.message }`. Validation failures thrown as plain `Error` therefore surface as **500**, not 400.
- `ExpressError(msg, status)` is used in products for 400/404.
- Responses use `{ error }`, but a few use `{ message }` (product lock, barcode, pos).

---

## 4. Database Design

### 4.1 Conventions
- PostgreSQL, integer auto-increment PKs, `createdAt` / `updatedAt` timestamps (camelCase columns) on all tables except `permissions` and `role_permissions`.
- Foreign-key columns are snake_case (`category_id`, `store_id`). Sequelize creates them from `belongsTo` associations.
- Some model attributes are camelCase mapped to snake_case columns through `field:` (e.g. `sellingType → selling_type`).
- Money columns are **FLOAT** (double precision), not NUMERIC.
- Schema management is `sync({ alter: true })` at boot. There are **no migrations**.
- There is no tenant column. The system is single-tenant.

### 4.2 Entity-relationship overview
```
roles ─< role_permissions >─ permissions
  │
  └─< users ──(created_by)──▶ most master/transactional tables

categories ─< sub_categories
categories ─< products >─ brands, units, suppliers, sub_categories
products ─< product_variants ─< inventories >─ stores
products ─< store_products >─ stores               (showcase publish flags)
products ── locked_by ──▶ users

customers ─< sales >─ stores, users(user_id)
sales ─< order_items >─ products, product_variants

suppliers ─< purchases ─< purchase_items >─ products      (no variant link)

customers ─< sale_returns >─ sales (optional), stores
sale_returns ─< return_items >─ products, product_variants

stock_adjustments >─ product_variants, stores, users
warehouses (standalone)      reports (standalone, unused)
```

### 4.3 Table dictionary

**Identity & access**

| Table | Columns | Constraints / indexes |
|---|---|---|
| `users` | id, name*, email* (unique), role ENUM(admin,biller) def biller, status ENUM(Active,Inactive), password* (bcrypt), role_id → roles | unique(email) |
| `roles` | id, name* (unique, ≤60), description, color def `#6366f1`, is_system bool | unique(name) |
| `permissions` | id, module* (≤60), action* (≤30), label | unique(module, action); no timestamps |
| `role_permissions` | role_id PK, permission_id PK | composite PK; no timestamps |

**Master data**

| Table | Columns | Indexes |
|---|---|---|
| `categories` | id, name*, status, image TEXT, created_by | |
| `sub_categories` | id, name*, status, category_id, created_by | |
| `brands` | id, name*, status, image TEXT, created_by | |
| `units` | id, name*, short_name, status, created_by | |
| `suppliers` | id, name*, email, phone, address, image, status, created_by | |
| `customers` | id, name*, email, phone, address, city, country, image, status, created_by | name, phone, email |
| `stores` | id, name*, slug (unique), address, status, logo, banner, description, phone, opening_hours, is_public_enabled def true, created_by | unique(slug), status |
| `warehouses` | id, name*, address, status, created_by | |

**Catalogue & stock**

| Table | Columns | Indexes |
|---|---|---|
| `products` | id, name*, description, status, image TEXT, slug, selling_type ENUM(Online,POS), product_type* ENUM(single,variable), tax_type ENUM(Exclusive,Inclusive), tax FLOAT, discount_type ENUM(Percentage,Fixed), discount_value, warranties, barcode_symbology, is_locked, locked_at, locked_by, is_published, is_featured, category_id, sub_category_id, brand_id, unit_id, supplier_id, created_by | name, status, is_published, is_featured, category_id, brand_id, unit_id |
| `product_variants` | id, product_id, sku, item_barcode, price, cost def 0, weight def 0, attributes JSONB def {}, expiry_date, manufactured_date, created_by | sku, item_barcode, product_id (**SKU is not unique**) |
| `inventories` | id, variant_id, store_id, qty INT def 0, quantity_alert INT def 0 | **unique(variant_id, store_id)**, store_id |
| `store_products` | id, store_id*, product_id*, is_published, is_featured | unique(store_id, product_id); (store_id, is_published); (store_id, is_featured) |
| `stock_adjustments` | id, variant_id, store_id, type* ENUM(Addition,Subtraction), quantity* INT, reason def 'Manual Correction', note, created_by | variant_id, store_id, created_by |

**Transactions**

| Table | Columns | Indexes |
|---|---|---|
| `sales` | id, reference, date, status ENUM(Completed,Pending), payment_status ENUM(Paid,Unpaid,Overdue), payment_method, subtotal, discount, tax, shipping, total, paid, due, note, customer_id, store_id, user_id | reference, date, customer_id, store_id, user_id |
| `order_items` | id, sale_id, product_id, variant_id, quantity* INT, unit_price*, discount def 0, subtotal* | sale_id, product_id, variant_id |
| `purchases` | id, reference, date, status STRING, payment_status ENUM(Paid,Unpaid,Overdue), total, paid, due, supplier_id, created_by | reference, status, date, supplier_id |
| `purchase_items` | id, purchase_id, product_id, quantity* INT, unit_price*, discount def 0, subtotal* | |
| `sale_returns` | id, reference, date, status ENUM(Pending,Completed) def Pending, total*, paid def 0, due, payment_status ENUM(Paid,Unpaid,Partial) def Unpaid, sale_id, customer_id, store_id, created_by | |
| `return_items` | id, sale_return_id, product_id, variant_id, quantity* INT, unit_price*, reason, subtotal* | |
| `reports` | id, type, reference, date, amount, payment_method, payment_status, customer_name, customer_image, product_name, product_image, category | (unused by any route) |

`*` = NOT NULL.

### 4.4 Seed data
- **On every boot** (`permissionSeed.js`): 57 permission rows across 19 modules. Roles `admin` (all permissions; links are deleted and re-inserted) and `biller` (12 permissions). Users with `role_id IS NULL` get their role linked.
- **Manual** `npm run seed --workspace backend`: creates store `islamabad-main` and an inventory row (qty 100, alert 10) for every variant without one.

---

## 5. API Specification

Base URL: `${VITE_API_URL}` (default `http://localhost:3000`). JSON in and out. Auth header: `Authorization: Bearer <JWT>`.
Legend: 🔓 public · 🔑 token only · `mod:act` = required permission.

### 5.1 Auth — `/api/auth`
| Method | Path | Guard | Body / Query | Response |
|---|---|---|---|---|
| POST | `/login` | 🔓 | `{email, password, remember=true}` | `{user, token, remember, permissions[]}`. 404 unknown user, 401 bad password |
| POST | `/signup` | 🔓 | `{email, password, name, role?, role_id?, remember?}` | 201 same shape as login |
| POST | `/refresh` | 🔑 | `{remember?}` | New token + user + permissions |
| GET | `/permissions` | 🔑 | n/a | `{permissions: ["module:action", …]}` (`["*"]` for legacy admin) |

JWT payload: `{ userId, role, roleId, iat, exp }`. Expiry is 30 d (remember) or 12 h.

### 5.2 Catalogue
| Method | Path | Guard | Notes |
|---|---|---|---|
| GET | `/api/products` | products:view | Query `page, limit, search, categoryId, brandId, status`. Paginated → `{data, pagination{total,page,limit,totalPages}}`, else an array. Each product is enriched with `category, brand, unit, price (first variant), qty (all stores), ProductVariants[].Inventories[]` |
| GET | `/api/products/pos?storeId=` | pos:view | Active products that have inventory in the store (400 without storeId) |
| GET | `/api/products/barcode/:barcode?storeId=` | pos:view | Matches `item_barcode` or `sku`. 404 `{message}` |
| POST | `/api/products` | products:create | Single: `{name, productType:'single', sku, price, quantity, storeId, itemBarcode?, cost?, weight?, quantityAlert?, …attrs}`. Variable: `{…, productType:'variable', variants:[{sku, price, cost, weight, itemBarcode, attributes{}, inventories:[{storeId, qty, quantityAlert}]}]}` |
| PUT | `/api/products/:id` | products:edit | Full payload with `variants[]` (with `id` to keep). 409 if locked by another user |
| DELETE | `/api/products/:id` | products:delete | 204. Cascades variants and inventory in code |
| PUT | `/api/products/:id/lock` | pos:create | `{lock: bool, storeId}`. 409 if locked by another user within 2 min. 403 to unlock someone else's lock. Returns the enriched product |
| POST | `/api/product-variants` | 🔑 | `{productId, sku, itemBarcode, price, cost, weight, attributes, expiryDate, manufacturedDate}` |
| GET/POST/PUT/DELETE | `/api/categories[/:id]` | categories:* | `{name, status, image}` |
| GET | `/api/sub-categories` | 🔓 | Includes parent category name |
| POST | `/api/sub-categories` | 🔑 + isAdmin | `{name, status, categoryId}` |
| GET/POST/PUT/DELETE | `/api/brands[/:id]` | brands:* | `{name, status, image}` |
| GET/POST/PUT/DELETE | `/api/units[/:id]` | units:* | `{name, short_name, status}` |
| GET/POST/PUT/DELETE | `/api/suppliers[/:id]` | suppliers:* | `{name, email, phone, address, image, status}` |

### 5.3 Inventory
| Method | Path | Guard | Notes |
|---|---|---|---|
| GET | `/api/inventory` | inventory:view | Flat rows `{id, qty, quantityAlert, variantId, sku, attributes, price, productId, productName, productImage, storeId, storeName, status}` |
| POST | `/api/inventory` | inventory:edit | Upsert `{variantId, storeId, qty, quantityAlert}` |
| GET | `/api/stock-adjustments` | stock_adjustments:view | Optional `page, limit` |
| POST | `/api/stock-adjustments` | stock_adjustments:create | `{variantId, storeId, type: Addition\|Subtraction, quantity>0, reason?, note?}` |

### 5.4 Sales & returns
| Method | Path | Guard | Notes |
|---|---|---|---|
| GET | `/api/sales` | sales:view | Query `page, limit, search (reference), startDate, endDate (createdAt), storeId, customerId`. Billers are forced to `user_id = self` |
| POST | `/api/sales` | sales:create | `{customerId, storeId, orderItems:[{productId, variantId, quantity, unitPrice, total?, discount?}], discount?, tax?, shipping?, paid?, payment_status?, payment_method?, status?, note?, reference?}` → 201 sale with `customer, user, items[] (+qty, unitPrice, price aliases)` |
| PUT | `/api/sales/:id` | sales:edit | Same body. Restores old stock, applies new |
| DELETE | `/api/sales/:id` | sales:delete | 204. Restores stock |
| GET | `/api/sales/returns` | sales_returns:view | Flattened `{…, customerName, productName (first item), itemsCount, items}` |
| POST | `/api/sales/returns` | sales_returns:create | `{customerId, storeId, saleId?, returnItems:[{productId, variantId?, quantity, unitPrice, reason?, total?}], reference?, date?, status?, payment_status?, total?, paid?}` |
| GET | `/api/invoices` | invoices:view | Derived from sales `{id, invoiceNo, customer{name, avatar}, dueDate (en-GB string), amount, paid, amountDue, status}` |

> Mount order: `/api/sales` is registered before `/api/sales/returns` and `/api/sales/report`. GET requests reach the sub-routers because the sales router only defines `GET /`. A `PUT` or `DELETE` to `/api/sales/returns` would be caught by `/api/sales/:id`.

### 5.5 Purchasing
| Method | Path | Guard | Notes |
|---|---|---|---|
| GET | `/api/purchases` | purchases:view | Includes supplier and items with product name |
| POST | `/api/purchases` | purchases:create | `{reference, date, status, payment_status, total, paid, due, supplierId, purchaseItems:[{productId, variantId, storeId, quantity, unitPrice, discount?, subtotal}]}`. Stock is added if `status==='Received'` |
| PUT | `/api/purchases/:id` | purchases:edit | `{status?, payment_status?, paid?, due?, storeId?}`. A move to `Received` adds stock |

### 5.6 Reports & dashboard
| Method | Path | Guard | Notes |
|---|---|---|---|
| GET | `/api/dashboard` | dashboard:view | `{stats{totalRevenue,totalOrders,totalCustomers,totalProducts}, revenueData[{month,revenue,orders}], salesDistribution[{category,value,color}], summaryCards{totalAmount,totalPaid,totalUnpaid,totalOverdue}}` |
| GET | `/api/sales/report` | reports:view | Query `startDate&endDate (both), productId, customerId` → order-item rows |
| GET | `/api/purchase/report` | reports:view | Query `startDate&endDate, supplierId` → purchase-item rows |

### 5.7 Users, roles, stores
| Method | Path | Guard | Notes |
|---|---|---|---|
| GET | `/api/users` = `/api/billers` | users:view | Query `q, status`. Password excluded, includes Role |
| PUT | `/api/users/:id` | users:edit | `{name, email, status, role?, role_id?}`. 409 on duplicate email |
| DELETE | `/api/users/:id` | users:delete | 204 |
| GET | `/api/roles` | roles:view | `[{id, name, description, is_system, userCount, permissions[], permissionIds[]}]` |
| GET | `/api/roles/permissions` | roles:view | All permission rows |
| POST | `/api/roles` | roles:create | `{name, description, permissionIds[]}`. 409 on duplicate |
| PUT | `/api/roles/:id` | roles:edit | Replaces permission links |
| DELETE | `/api/roles/:id` | roles:delete | 400 for system roles or roles that still have users |
| POST | `/api/roles/assign-user` | users:edit | `{userId, roleId}` |
| GET | `/api/stores`, `/api/stores/:id` | 🔑 | List is active stores only |
| POST/PUT | `/api/stores[/:id]` | stores:create / stores:edit | `{name, address, status}` |
| DELETE | `/api/stores/:id` | stores:delete | `{message}` |
| GET | `/api/stores/:id/showcase` | 🔑 | `{store, products[{id,name,category,brand,price,image,isPublished,isFeatured}]}`. Auto-creates the slug |
| PUT | `/api/stores/:id/showcase` | stores:edit | `{slug, logo, banner, description, phone, openingHours, isPublicEnabled}`. 400 if the slug is taken |
| POST | `/api/stores/:id/showcase/toggle-product` | products:edit | `{productId, field: isPublished\|isFeatured, value}` |
| POST | `/api/stores/:id/showcase/batch-publish` | products:edit | `{publishAll: bool}` or `{productIds[], action: publish\|unpublish}` |

### 5.8 Public shop — `/api/public/shop` (🔓)
| Method | Path | Notes |
|---|---|---|
| GET | `/:slug` | `:slug` is a slug or numeric id. Store must be Active. If public is disabled → `{isPublicEnabled:false, message}` |
| GET | `/:slug/products` | `page, limit(24), search, categoryId, brandId, sort (newest\|name_asc\|price_asc\|price_desc), featured=true`. Price sort is applied in memory **per page only** |
| GET | `/:slug/products/:productId` | Id or slug. 404 if unpublished (except when `NODE_ENV=development`) |
| GET | `/:slug/categories` | All categories (not filtered by store) |
| GET | `/:slug/featured` | Up to 8 |

### 5.9 Misc
| GET | `/health` | 🔓 | `OK` |
|---|---|---|---|

**Not mounted:** `routes/warehouses.js` (`/api/warehouses`), `routes/productLock.js` (empty).

---

## 6. Frontend Architecture

### 6.1 Composition
```
<ThemeProvider>            accent preset + light/dark, persisted in localStorage
  <PosProvider>            auth state: currentUser, login, logout, scheduled refresh
    <PermissionProvider>   permissions[], hasPermission, canAccessMenu, 30 s poll
      <GlobalErrorHandler/> listens for window 'apiError' events → toast
      <HashRouter> <AppRoutes/>
```
`ErrorProvider` (from `ErrorContext`) wraps all of the above in `main.jsx` and supplies `showError`.

### 6.2 Routing & guards
- `ProtectedLayout` = `ProtectedRoute(module, action)` + `AppLayout` (Sidebar + Topbar).
- `ProtectedRoute`: shows a loading state while auth or permissions load. Without a user it redirects to `/signin` and keeps `from`. Without the permission it redirects to `/`.
- The Sidebar filters items with `canAccessMenu(menuKey)`.
- 34 routes (see `App.jsx`). Public routes: `/signin`, `/signup`, `/shop/:slug`, `/shop/:slug/product/:productId`.

### 6.3 API client (`services/api.js`)
- `api.get | post | put | delete | postFormData`. These add `Authorization` and JSON headers.
- Pre-emptive refresh happens when the token is within 120 s of expiry. A 401 triggers one refresh, and the request is retried once. Concurrent refreshes are de-duplicated with a shared promise.
- Errors dispatch `apiError` (toast) and, on 401, an `unauthorized` event, which makes PosContext refresh or log out.

### 6.4 Client storage
| Key | Store | Content |
|---|---|---|
| `token`, `currentUser`, `userPermissions` | localStorage (remember) or sessionStorage | Auth session |
| `rememberSession`, `savedEmail` | localStorage | Preferences |
| theme keys | localStorage | Accent / mode |

### 6.5 Reusable list framework
`components/ListComponents/*` holds a header, filters, search, a TanStack table, pagination, a date-range picker, and PDF/Excel export. All list pages (Products, Sales, Purchases, Inventory, Reports, etc.) use it. Paging and filtering are mostly done **on the client** over full result sets.

### 6.6 Electron bridge (`preload.js`)
`window.electronAPI = { onBackendError, onBackendStarted, printReceipt, saveReport }`. Only `onBackendError` has a main-process sender. `print-receipt` and `save-report` have **no IPC handlers**.

---

## 7. Configuration

### 7.1 Backend `.env`
| Variable | Required | Default | Purpose |
|---|---|---|---|
| `DB_HOST`, `DB_NAME`, `DB_USER`, `DB_PASSWORD` | ✔ | n/a | Postgres connection (the process exits if missing) |
| `DB_PORT` | | 5432 | |
| `DB_SSL` | | false | `true` → SSL with `rejectUnauthorized:false` |
| `DB_POOL_MAX/MIN/ACQUIRE/IDLE` | | 25 / 5 / 30000 / 10000 | Pool tuning |
| `JWT_SECRET` | ✔ | n/a | HMAC secret |
| `PORT`, `HOST` | | 3000, 0.0.0.0 | Listener |
| `NODE_ENV` | | n/a | `development` enables auto-start, SQL logging, and localhost CORS |
| `RUN_SERVER` | | n/a | `true` auto-starts outside development |
| `CORS_ORIGIN` | | n/a | Comma-separated allow-list |

### 7.2 Frontend `.env`
| `VITE_API_URL` | API base, no trailing slash. Defaults to `http://localhost:3000` in dev |
|---|---|

### 7.3 Scripts
| Command | Effect |
|---|---|
| `npm run dev` (root) | `concurrently` backend (nodemon) + frontend (`vite --host`) |
| `npm run build` | Vite production build → `frontend/dist` |
| `npm run seed --workspace backend` | Default store + inventory seed |
| `npm run lint --workspace <ws>` | ESLint |
| `node scripts/e2e-flow-test.mjs` | Master data → product → purchase → sale → stock verification |
| `node scripts/audit-e2e-flow.mjs` | Extended flow audit |
| `node scripts/concurrent-checkout-test.mjs` | 10 parallel checkouts on one SKU (oversell test) |
| `node scripts/benchmark-pos-perf.mjs` | Latency benchmark of POS endpoints |
| `postinstall` → `scripts/fix-deps.cjs` | Writes ESM shim files into specific node_modules packages |

---

## 8. Security & Integrity Review

### 8.1 Controls in place
- bcrypt (cost 10) password hashing. The password is excluded from user payloads.
- JWT with a server secret. The server refuses to run auth without `JWT_SECRET`.
- Database-driven RBAC on most endpoints, with the cache invalidated on role changes.
- CORS allow-list. `x-powered-by` disabled.
- The public shop payload strips cost, supplier, lock, and creator fields.
- Parameterised queries throughout (Sequelize). The raw SQL in the dashboard has no user input.

### 8.2 Gaps (priority order)
| # | Issue | Recommendation |
|---|---|---|
| S1 | Public signup accepts `role`/`role_id`, so anyone can self-register as admin | Ignore role fields on public signup. Add an authenticated `POST /api/users` guarded by `users:create`. Optionally disable public signup after the first user. |
| S2 | No check of `users.status` or user existence on login or token use | Reject Inactive users at login. Check status in `authenticateToken`, or use short tokens plus refresh. |
| S3 | `stores` permissions are missing from the seed | Add `stores:view/create/edit/delete` to `ALL_PERMISSIONS`. |
| S4 | Endpoints without permission guards (§5: stores GET, showcase GET, product-variants POST, sub-categories GET) | Add `requirePermission`. |
| S5 | Oversell race: stock is checked without a lock | `Inventory.findOne({ lock: t.LOCK.UPDATE })` or a conditional `UPDATE … SET qty = qty - n WHERE qty >= n`, checking the affected row count. |
| S6 | No rate limiting or brute-force protection on login | `express-rate-limit` on `/api/auth/*`. |
| S7 | 30-day bearer token in localStorage (XSS exposure) | Shorter access token plus an httpOnly refresh cookie. |
| S8 | `sync({ alter: true })` in production can change the schema unexpectedly | Move to Sequelize migrations (`umzug` / `sequelize-cli`). |
| S9 | FLOAT for money gives rounding drift | `DECIMAL(12,2)`, and integer-minor-unit arithmetic. |
| S10 | Images stored as base64 TEXT in rows (10 MB body limit) | Object storage plus URL columns. |
| S11 | Error messages from DB exceptions go back to clients | Map to generic messages and log the details. |
| S12 | `reset_passwords.js` hard-codes demo credentials | Remove from the repo or gate it to dev. |
| S13 | `frontend/.env` is committed | Fine while it holds only a public URL. Keep secrets out. |

### 8.3 Data integrity issues
- `sales.payment_status` enum lacks `Partial`, which the code can produce. Add it, or change the computation.
- `purchase_items` lacks `variant_id`. Add the column, and use it when receiving later.
- Sales-return quantity validation should subtract quantities already returned.
- POS inclusive-tax double counting. The server trusts the client's `tax` and `discount`, so it should recompute them from product data.
- Hard deletes break history. Prefer soft delete (`paranoid: true`) or FK `RESTRICT`.
- `product_variants.sku` is not unique, so barcode lookups can be ambiguous.

---

## 9. Non-Functional Technical Requirements (targets)

| Area | Target |
|---|---|
| Availability | Single node. Stateless API apart from the in-memory permission cache. |
| Scalability | Add server-side pagination to all list endpoints (only products, sales, and stock-adjustments have it now). Cache in Redis for multi-node. |
| Performance | POS product load under 300 ms for 1k SKUs. Sale commit under 200 ms p95. |
| Observability | Today: `console.*` only. Recommended: structured logging (pino), request IDs, `/health` with a DB check. |
| Testing | Today: script-based E2E against a running server, no unit tests. Recommended: Vitest/Jest + Supertest on a test DB, run in CI. |
| Backup | `pg_dump` schedule (not part of the app). |

---

## 10. Deployment Topologies

1. **Web (recommended):** Postgres → `node backend/src/index.js` (with `RUN_SERVER=true`) behind a reverse proxy with TLS. Serve `frontend/dist` statically, and set `VITE_API_URL` at build time and `CORS_ORIGIN` accordingly.
2. **Desktop (Electron):** The main process imports the backend and starts it on `localhost:3000`. It still needs a reachable PostgreSQL (there is no embedded DB). The packaged app expects the backend under `resources/backend`. The packaging config, the `electron` dependency, and the `dist`/`dev:electron` scripts are currently missing.
3. **Future SaaS:** shared DB with row-level `tenant_id`, per `SAAS_ARCHITECTURE.md`.

---

## 11. Traceability (PRD → code)

| PRD feature | Backend | Frontend |
|---|---|---|
| Auth | `routes/auth.js`, `middleware/auth.js` | `pages/SignIn.jsx`, `SignUp.jsx`, `context/PosContext.jsx`, `lib/authStorage.js` |
| RBAC | `middleware/permission.js`, `services/permission*.js`, `routes/roles.js` | `context/PermissionContext.jsx`, `ProtectedRoute.jsx`, `pages/Roles.jsx`, `AddRole.jsx` |
| POS | `routes/products.js` (`/pos`, `/lock`, `/barcode`), `routes/sales.js` | `pages/POS.jsx`, `PayForm.jsx`, `Receipt.jsx`, `CustomerForm.jsx` |
| Products | `routes/products.js`, `productVariants.js` | `pages/AddProduct.jsx`, `forms/EditProductForm.jsx`, `lists/ProductList.jsx` |
| Inventory | `routes/inventory.js`, `stockAdjustments.js` | `pages/Inventory.jsx`, `lists/InventoryList.jsx` |
| Purchases | `routes/purchases.js`, `purchaseRoutes.js` | `pages/AddPurchase.jsx`, `Purchases.jsx`, `PurchaseReport.jsx` |
| Returns | `routes/salesReturns.js` | `pages/AddSalesReturn.jsx`, `SalesReturn.jsx` |
| Reports | `routes/salesReports.js`, `invoices.js`, `dashboard.js` | `pages/*Report.jsx`, `Dashboard.jsx`, `Invoices.jsx` |
| Showcase | `routes/showcaseAdmin.js`, `publicShop.js` | `pages/ShowcaseSettings.jsx`, `PublicShopPage.jsx` |
