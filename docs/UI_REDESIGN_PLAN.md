# UI Redesign Plan (step 1 of 3: implementation plan)

| | |
|---|---|
| Status | Approved 31 Oct 2026 with the recommended defaults; build in progress (step 2). **Barcode: later** — barcode scanning and barcode labels are deferred (owner, 1 Nov 2026): removed from S6, S8 and S10; name/SKU search stays |
| Date | 1 November 2026 |
| Scope | Shop app (https://demo-shop.localhost:8443) and Super Admin console (https://admin.localhost:8443) |
| Evidence | `docs/ui-audit/`: about 230 screenshots (360, 768 and 1280 px), crawl and form logs (`crawl-*.json`, `forms*.json`, `*-context.json`), a sample receipt PDF, and `created-records.md` |

## 0. How the audit was done

- **Browser**: headless Chromium (Playwright 1.56.1) in the official Playwright container, attached to the dev stack's Docker network. Pages were loaded at their real addresses (`*.localhost:8443`) through Caddy, so the real CSP and cookies applied.
- **Shop app**: signed in as the owner and as a new Biller cashier ("UI-AUDIT Cashier"). Every route in `App.jsx` (35 routes, including an unknown one) was visited at 360, 768 and 1280 px for both roles. Console errors, page errors, failed calls, redirects, blank pages and horizontal scrolling were recorded.
- **Forms**: every form listed in brief G was run with valid and invalid input. 20 records were created, all prefixed UI-AUDIT (see `docs/ui-audit/created-records.md`). Nothing was deleted. One accidental change (the Main Store web address) was put back within a minute; it is listed there.
- **Super Admin console**: the sign-in page is real. Signing in needs the console owner's authenticator code, which I did not have and did not work around. The signed-in pages (tenants, tenant detail, plans, billing, audit) are the **real console code under its real CSP**, given **sample data** by the browser. Those screenshots end in `-SAMPLE`. No CSP violations were logged.
- **Code**: read alongside, to find root causes. ESLint was run on the frontend.

File naming in `docs/ui-audit/`: `owner-<width>_<route>.jpg`, `cashier-<width>_<route>.jpg`, `admin-<width>_<page>.jpg`, `forms<N>-<step>.jpg` (step order in `forms<N>.json`), `msg-360-*.jpg`, `storefront-*.jpg`.

---

## 1. Findings by severity

Severity: **Critical** = a core task cannot be done or the screen breaks; **High** = wrong data or a task blocked for many users; **Medium** = confusing, inconsistent or partly broken; **Low** = polish.

### Critical

| # | Finding | Steps to reproduce | Root cause | Evidence |
|---|---|---|---|---|
| C1 | **Add Product is a blank page** at every width | Products → Add Product | `AddProduct.jsx:34` calls `usePermission()` without importing it (`ReferenceError: usePermission is not defined`). Line 849 also uses an undefined `isAdmin`. No error boundary exists, so React unmounts the whole tree and leaves an empty `#root` | `owner-1280_add-product.jpg`, `crawl-owner.json` |
| C2 | **No way to add a store anywhere in the UI** | Look for Stores in the menu; the only "+ store" button lives inside Add Product (C1) | There is no Stores page; the store quick-add was only reachable from Add Product | `crawl-owner.json` (no route) |
| C3 | **Add Customer and Add Supplier pages cannot save** | Customers → Add Customer → fill → Next → Add Customer | Two causes. (a) Steps render with `currentStep === n &&`, so step-1 inputs are unmounted when the form submits from step 2 and `new FormData(form)` loses them. (b) An empty file input becomes `{}` in JSON → server: `image: has the wrong type` → "Validation failed" | `forms2-03.jpg`, `forms2-04.jpg` |
| C4 | **Add Brand cannot save without a picture** | Brands → Add → name only → Add Brand | Same as C3(b): `AddBrand.jsx` keeps the empty `File` (Add Category deletes it, Brand does not) | `forms1b-02.jpg` |
| C5 | **Purchases of products with sizes/variants cannot be saved; a Pending purchase can never be received** | Add Purchase → product with 2 variants → "No variants available for this product" → save → "Choose a variant for this product." Purchases list offers only View | `AddPurchase.jsx:120,132` compares the `<select>` string value with a numeric `p.id`, so the variant list is always empty. Single-variant products work only because the server fills the variant in. No receive/cancel UI exists for the server's `PUT /purchases/{id}` | `forms2-06.jpg`, `forms3-18.jpg`, `final-01.jpg` |
| C6 | **The frontend is not checked in CI** | `.github/workflows/` has only `go-backend.yml` | No lint, build or browser test runs for `frontend/`. ESLint finds 37 problems including the two `no-undef` errors behind C1 | `npx eslint src admin-console` |

### High

| # | Finding | Steps | Root cause | Evidence |
|---|---|---|---|---|
| H1 | **POS store picker is blank** (cashier cannot see which store they sell from) | Open POS at any width | Radix `Select` gets a number (`fetchedStores[0].id`) while items use `String(store.id)` | `owner-360_pos.jpg`, `owner-1280_pos.jpg` |
| H2 | **POS total on screen differs from the saved total** | POS → tax-inclusive item: PAY shows ₹136.00, receipt ₹118.00. Discount ₹12.05 on ₹120.50 at 18 %: PAY ₹130.14, receipt ₹127.97 | `POS.jsx` adds inclusive tax again and takes the discount after tax; the server does neither | `forms3-context.json`, `cashier-context.json` |
| H3 | **Edit Product silently refuses to save products without a brand** (all imported products without a brand) | Products → Edit → change price → Save Product: nothing happens | `EditProductForm` marks Brand, Unit, Category `required`; the browser blocks the submit with a native bubble inside the modal, and labels are not tied to inputs | `forms5-context.json` |
| H4 | **Product Showcase silently rewrites and saves the store web address** | Type "Bad Slug!" → Save Profile Changes → "saved successfully!" → the public address is now `/shop/badslug` | `toSlug()` strips characters while typing; save has no confirmation even though old links break | `forms4-04.jpg` (restored afterwards) |
| H5 | **Messages are inconsistent and some are invisible** (section 2) | e.g. Showcase uses `alert()`, most forms show nothing on success, many errors are only logged to the console | 6 different mechanisms; 51 `console.error`-only catches | section 2 |
| H6 | **On a phone, the error message box is cut off and covers the header** | 360 px: duplicate category | `ErrorToast` is `fixed right-5 w-full max-w-sm` → 20 px off the left edge | `msg-360-toast.jpg` |
| H7 | **POS on a phone shows almost no products** | POS at 360 px | The cart panel takes up to 45 % of the height plus the trial banner; one product tile is visible, partly | `owner-360_pos.jpg` |

### Medium

| # | Finding | Evidence / root cause |
|---|---|---|
| M1 | Sales Report "SKU" column shows an internal row code (`SO-34-34`) instead of the product SKU | `final-context.json`; the list uses `sku` (row id) instead of `itemSku` |
| M2 | Category and brand pictures in lists are broken (401) | `<img>` points to signed-in `/api/files/{id}` without a token (`crawl-owner.json`) |
| M3 | Dashboard always greets "Admin"; has no date/store filters; "Total Overdue" is always ₹0; returns not deducted; "Sales Distribution" is really top products | `owner-1280_dashboard.jpg`, `reports/dashboard.go` |
| M4 | Redirects without explanation: a cashier opening a forbidden page lands on the Dashboard with no message; unknown addresses go to `/` | `crawl-cashier.json` |
| M5 | Purchase Report is in the cashier's menu but answers 403 | `crawl-cashier.json` |
| M6 | POS product tile turns grey for the cashier's own cart items; after a crash or in a second tab the cashier's own 2-minute lock blocks them; the "already in the cart" message can never appear | `final-context.json` (`tileDisabledAfterAdd: true`) |
| M7 | Filters that never match: "Overdue" everywhere, Returns "Received", Customer Report payment method; no "Partial" filter | code, `docs/POS_FLOW.md` §19 |
| M8 | Lists load everything at once (no server paging) and say nothing when the server truncates | `Sales.jsx`, `Products.jsx` … |
| M9 | "View" buttons show raw stored fields ("View Item"), list exports use internal column names | `ActionModal.jsx`, `exportFunctions.jsx` |
| M10 | Add Sale's default date is the device's day, not the shop's (a sale made at 02:50 IST was dated 31 Oct from a UTC device) | `final-context.json` (POS-INV-000033) |
| M11 | Stock history shows `manual_set` / `product_edit` | `final-context.json` |
| M12 | Form labels are not linked to inputs (EntityForm, most legacy forms): screen readers and label clicks do not work | `forms4-context.json` (`editLabelsLinked: 0`) |
| M13 | Product Showcase is visible to cashiers (read-only) but every switch answers "You do not have permission for this action" | `cashier-06.jpg` |

### Low

| # | Finding |
|---|---|
| L1 | Login page logs a red console error (401 on the session probe) on every visit |
| L2 | Page titles repeat in the top bar and in the page ("Dashboard" twice); list pages have inconsistent subtitles ("Manage Your pos orders") |
| L3 | Trial banner takes 4 lines on a phone on every page |
| L4 | Product tiles with no picture are blank white squares |
| L5 | Admin console at 360 px: top menu wraps to two lines, the tenants table is cut off on the right |
| L6 | `chart.js` is not declared in `frontend/package.json` (resolved from the root lockfile only) |
| L7 | MUI + Emotion are installed for a single icon (`LoopIcon` in `useUI.jsx`) |

### What works well (keep)

POS sale, receipts (on screen, PDF, email with attachment, resend with 5-a-day limit), returns with cumulative checks and exact refunds, stock adjustments with live "after" preview, Inventory edit, import with dry run and row errors, permission enforcement on the server, Settings validation (field errors), Email settings test, the admin console's CSP discipline and plain-CSS tokens.

---

## 2. Messages (brief A)

### 2.1 What exists today

| # | Mechanism | Where | Code / library | How long | Close | Mobile | Accessibility | Notes |
|---|---|---|---|---|---|---|---|---|
| 1 | Red box, top right ("toast") | Every API call not marked `quiet` (81 call sites) | Custom: `context/ErrorContext.jsx` + `GlobalErrorHandler.jsx`, fed by the `apiError` window event in `services/api.js` | 5 s, auto-closes | × button | Cut off 20 px at the left on 360 px and covers the top bar (H6) | `role=alert`, `aria-live=assertive` | Errors only, one at a time (a new one replaces the old), no success style |
| 2 | POS red bar above the products | POS | Inline JSX in `POS.jsx` | Until closed or replaced | × | Pushes the product grid down | No role, not announced | Can be hidden behind the cart on phones |
| 3 | Inline form boxes | Add Sale, Add Return, Receipt, Settings, sign-in, POS customer form | Five separate implementations (`Notice` in `settings/FormKit.jsx`, `FormMessage` in `AuthLayout.jsx`, ad hoc `div`s) | Until next action | Mostly no | OK | Mixed (`role=alert` in some) | Settings is the best: summary + per-field errors |
| 4 | Browser pop-ups | Product Showcase (success and errors), Roles (delete), public shop | `window.alert`, `window.confirm` | Blocking | OK button | Native | Native | Inconsistent look; blocks the page |
| 5 | Confirmation dialogs | List delete, POS clear cart, price changed | `ActionModal.jsx` (custom, Headless UI), POS custom overlay, `PriceChangedDialog` (Radix Dialog) | Until answered | Yes | Centered modals, no bottom sheet | Radix one is accessible; the custom ones do not trap focus | Three implementations |
| 6 | Native validation bubbles | Most forms (`required`, `min`, `max`) | Browser | ~5 s | — | Small, easy to miss | OK | Silent inside modals (H3); cannot be styled |
| 7 | Nothing | Success of most create forms; 51 `catch` blocks that only `console.error` | — | — | — | — | — | The user does not learn that something worked |
| 8 | Banners | Trial / billing state | `layout/BannerArea.jsx` | Until dismissed | × | 4 lines tall on phones | `role=status` | Fine, but too tall on mobile |
| 9 | Admin console | Inline notices, action dialogs | `admin-console/src/components/ui.jsx` (`Notice`), `ActionDialog.jsx`, CSS classes | Until next action | — | OK | Good | No toasts at all; CSP-safe |

### 2.2 Recommendation: one message system, four parts

| Part | Use for | Behaviour |
|---|---|---|
| **Toast** | The result of an action: "Sale saved", "Receipt emailed", "Could not reach the server" | Transient; see spec below |
| **Inline alert** | A problem that stops a form from saving, or a page-level state (read-only shop, trial) | Stays at the top of the form/page until fixed; scrolls into view; first invalid field gets focus |
| **Field error** | One field is wrong | Red text under the field, linked with `aria-describedby`, `aria-invalid` set; forms use `noValidate` so the browser bubbles never appear |
| **Confirm dialog** | Destructive or irreversible actions (delete, void, change web address, sign out everywhere) | Modal on desktop, bottom sheet on phones; optional "type the name to confirm"; replaces every `alert`/`confirm` |

**Library decision: a small shared component, not a library.** Popular toast libraries inject styles at run time (Sonner inserts a `<style>` element; react-hot-toast uses goober, which writes CSS at run time). The admin console's CSP (`style-src 'self'`) blocks both. A ~150-line headless `Toast` (queue, timers, `aria-live` region, keyboard) in a new `frontend/shared/` folder, styled only through class names, works in both apps: Tailwind classes in the shop app, `global.css` classes in the console. Step 3 checks for CSP violations.

### 2.3 Toast specification

| Rule | Value |
|---|---|
| Kinds | success, info, warning, error |
| Position | Desktop: top right, under the top bar, 16 px from the edge. Phones (< 640 px): top, under the top bar, full width minus 16 px gutters (changed during S2: at the bottom they covered sticky Save buttons; the top keeps them clear of the bottom navigation and the PAY bar) |
| Stack | At most 3; oldest leaves first; identical messages within 3 s are merged ("×2") |
| Duration | success 4 s, info 5 s, warning 7 s, **error stays until closed** if it needs action, otherwise 8 s. Timers pause on hover and focus |
| Close | × button (44 × 44 px touch target), Esc when focused, swipe on touch |
| Action | Optional single button: "Retry", "View receipt", "Undo" |
| Accessibility | One polite live region (`role=status`) for success/info, one assertive (`role=alert`) for errors; toasts never take focus; contrast ≥ 4.5:1; icon + text, never colour alone |
| Text | Plain sentence from the server's `error` field; for field errors the toast is not used: the form shows an inline alert "Please correct the highlighted fields." |
| API wiring | `services/api.js` keeps one event; `quiet` calls stay silent so forms can show inline errors; success toasts are explicit (`toast.success("Customer added")`) |
| Network | "No connection. Check the internet and try again." with Retry; 503: "The server is busy. Try again in a moment." |

---

## 3. Blank page (brief B)

**Reproduced**: `/add-product` is blank at 360, 768 and 1280 px (`owner-*_add-product.jpg`).

**Root cause** (page error captured by the browser): `ReferenceError: usePermission is not defined` at `frontend/src/pages/AddProduct.jsx:34`. The hook is used but never imported. Line 849 has a second undefined name, `isAdmin`, which would crash too. Both are flagged by ESLint (`no-undef`), but CI never runs ESLint on the frontend (C6).

**Why no error screen appears**: the app has **no error boundary** anywhere (no `componentDidCatch`, no router `errorElement`; `App.jsx` uses `<BrowserRouter>` + `<Routes>`, which has no error UI). When a component throws while rendering, React removes the whole tree from `#root`, so the user sees an empty page, without the sidebar, with no message and no way back except the browser's back button.

**Plan: global error safety net**

1. `AppErrorBoundary` around the whole app: full-page fallback "Something went wrong" with **Reload**, **Go to Dashboard**, and **Copy details** (error message, page, time, app version). Never shows stack traces to users.
2. `RouteErrorBoundary` inside `AppLayout`, keyed on the route: only the page area is replaced, the sidebar and top bar stay, so the user can navigate away. It resets on navigation.
3. Chunk-load errors after a deploy ("Failed to fetch dynamically imported module") get their own message: "A new version is available" + Reload.
4. Errors are logged to the console with a short code; sending them to the server is a question for you (Q7).
5. Same pattern in the admin console (CSS classes only).
6. **Prevention**: CI job for the frontend (`npm ci`, `eslint` with `no-undef` as error, both builds) and a Playwright smoke test that opens every route and fails on any page error.

---

## 4. Navigation (brief C)

### 4.1 Current sidebar (in order)

| Group | Items |
|---|---|
| Overview | Dashboard |
| Sales | POS, Sales, Sales Returns, Invoices |
| Inventory | Products, Inventory, Stock Adjustments, Purchases |
| Master Data | Categories, Brands, Units, Suppliers |
| People | Customers |
| Reports | Sales Report, Purchase Report, Customer Report, Customer Due |
| System | Product Showcase, Users, Roles & Permissions, Billing, Settings |

Problems: 23 items; Customers and Suppliers are split; four separate report entries; no Stores; Billing and Settings mixed with team pages; cashiers see items that answer 403 (Purchase Report) or are read-only (Product Showcase); forbidden pages redirect silently.

### 4.2 Target navigation map

| Group | Items (target name ← current) | Visible with |
|---|---|---|
| **Overview** | Dashboard | dashboard:view |
| **Sell** | Checkout ← POS · Orders ← Sales · Returns ← Sales Returns · Invoices & dues ← Invoices | pos:view · sales:view · sales_returns:view · invoices:view |
| **Inventory** | Products · Stock ← Inventory · Adjustments & history ← Stock Adjustments · Purchases | products:view · inventory:view · stock_adjustments:view · purchases:view |
| **Contacts** | Customers · Suppliers | customers:view · suppliers:view |
| **Catalogue setup** | Categories (with sub-categories) · Brands · Units | categories/brands/units:view |
| **Reports** | One page with tabs: Sales · Purchases · Customers · Customer dues (later: GST, Day-end, Stock value) | reports:view; each tab also needs its data permission (Purchases needs purchases:view) |
| **Online shop** | Storefront ← Product Showcase · "View shop" link | stores:**edit** (not view) |
| **Team** | Users · Roles & permissions | users:view · roles:view |
| **Settings** | Business · **Stores (new)** · Tax & invoices · Receipts · Email · Home page · Appearance · Plan & billing | settings:view (+ stores:view, email_settings:view, billing:view per tab) |

Rules:
- A group with no permitted items is hidden. A tab without permission is hidden, not shown and refused.
- Opening a forbidden page shows "You don't have access to this page" with a **Back to Dashboard** button (no silent redirect). Unknown pages show a real 404 page inside the layout.
- The current page is marked with `aria-current="page"`; the sidebar can be collapsed to icons on desktop (exists today).
- Keyboard: `g` then `d`/`p`/`o` jumps (Dashboard, POS, Orders); `/` focuses search on list pages.

### 4.3 Mobile navigation

- **Phones (< 768 px)**: top bar (menu button, page title, user menu) + **bottom tab bar** with the 4 most used items for the role: Owner: Dashboard, Checkout, Orders, More. Cashier: Checkout, Orders, Customers, More. "More" opens the full menu as a bottom sheet.
- **Tablets (768–1279 px)**: collapsible icon sidebar; the POS uses the full width with the cart as a right panel.
- **POS**: a "focus mode" that hides the sidebar on all sizes (exists as `fullBleed`; keep, add an exit button).

### 4.4 New Stores page (Settings → Stores)

| Feature | Behaviour | Server support |
|---|---|---|
| List | Name, address, phone, status (Active/Inactive), online shop on/off, default store marker, units in stock | `GET /api/stores` (returns active only today: needs an `includeInactive` option, small server change) |
| Add store | Name (required), address, phone, status. Toast "Store added". Plan limit reached → inline alert with "Upgrade plan" | `POST /api/stores` (plan limit already enforced) |
| Rename / edit | Same fields; the web address is changed only on the Online shop page with a confirm dialog (fixes H4) | `PUT /api/stores/{id}` |
| Activate / deactivate | Confirm dialog. Cannot deactivate the last active store. Warns when the store has stock or is the default online store | `PUT` status; last-store rule exists for delete only → extend |
| Delete | Not offered at first (stock and history belong to stores); question Q4 | `DELETE` exists |
| Permissions | stores:view to see, stores:create/edit to change | exist |

---

## 5. Design system and components (brief D)

### 5.1 Tokens (one source, two outputs)

One token file, `frontend/shared/tokens.css`, holds CSS custom properties. The shop app maps them into Tailwind (`@theme` in Tailwind 4); the admin console imports the same file through its `global.css`. No runtime CSS anywhere.

| Group | Tokens |
|---|---|
| Colour (semantic) | `--bg`, `--surface`, `--surface-2`, `--text`, `--text-muted`, `--border`, `--primary` (+ `-hover`, `-fg`), `--success`, `--warning`, `--danger`, `--info` (each with `-bg` and `-fg`), `--focus-ring`. Light and dark sets. Every text/background pair ≥ 4.5:1, large text and icons ≥ 3:1 |
| Type | Inter (self-hosted, no CDN; question Q2) with system fallback. Scale 12 / 14 / 16 / 18 / 20 / 24 / 30; body 14 px desktop, 16 px on phones (no iOS zoom); tabular numbers for money |
| Spacing | 4 px grid: 4, 8, 12, 16, 24, 32, 48. Page gutter 16 px (phones), 24 px (tablet), 32 px (desktop) |
| Radius | 6 (inputs), 10 (cards), 16 (sheets) |
| Elevation | 3 levels (card, popover, modal) |
| Motion | 150 ms / 250 ms; respects `prefers-reduced-motion` |
| Touch | Minimum 44 × 44 px targets; 48 px for POS buttons |
| Breakpoints | 360 (base), 640, 768, 1024, 1280 |

### 5.2 Component list

Built once in `frontend/src/components/ui/` (shop app, on the existing Radix + shadcn-style pieces) with CSS-only twins in `admin-console/src/components/` where the console needs them.

| Component | Replaces / notes |
|---|---|
| `AppShell` (sidebar, top bar, bottom tabs, banner slot) | `AppLayout`, `Sidebar`, `Topbar`, `BannerArea` |
| `PageHeader` (title, description, primary action, secondary actions, breadcrumbs) | Two `PageHeader`s, `ListPageLayout` header |
| `DataTable` (TanStack Table): server paging, sorting, column visibility, sticky header, row actions menu, bulk select, **card layout under 640 px**, empty/loading/error states, CSV/XLSX export of visible columns with readable headers | `ListPageLayout`, `ListTable`, `useUI`, `exportFunctions` |
| `FilterBar` (search, select filters, date range, "Clear filters", active filter chips; collapses into a "Filters" sheet on phones) | `SearchInput`, `SelectFilters`, `DateRangePicker` |
| `Form` kit: `Form`, `FormSection`, `Field` (label linked by `id`, hint, error, required marker), `TextInput`, `NumberInput`, `MoneyInput` (₹, 2 decimals), `Select`, `Combobox` (searchable), `Switch`, `Checkbox`, `DateInput`, `Textarea`, `ImagePicker`, `FormActions` (sticky on phones) | `EntityForm`, `FormField`, `settings/FormKit`, dozens of ad hoc inputs |
| `Wizard` that **keeps every step mounted** (hidden, not removed) and collects data from React state, not `FormData` | Fixes C3 and the Add Product data loss |
| `Modal` / `Sheet`: centered dialog ≥ 640 px, **bottom sheet** below; focus trap, Esc, scroll lock, `aria-labelledby` | `ActionModal`, `ui/dialog`, the POS overlays, `CustomerForm`, `StoreModal`, `SupplierModal`, `SubcategoryModal` |
| `ConfirmDialog` (normal and destructive, optional type-to-confirm) | `window.confirm`, `window.alert`, ActionModal delete |
| `Toast`, `InlineAlert`, `FieldError` | Section 2 |
| `EmptyState`, `LoadingState` (skeletons for tables and cards), `ErrorState` (with Retry) | "Loading…", "No products found", red text |
| `StatCard`, `ChartCard` | `KpiCard`, `SectionCard` |
| `StatusBadge` (one colour map for sale, payment, purchase, stock, tenant states) | `statusColumn`, ad hoc badges |
| `Money`, `DateTime` formatters (shop currency, locale and **shop time zone**) | `lib/format.js` (time zone fix for M10) |
| `ProductPicker` (search by name or SKU, shows variant and stock; barcode: later) | Product `<select>`s in Add Sale, Add Purchase, Returns, Adjustments |
| `AppErrorBoundary`, `RouteErrorBoundary`, `NotFound`, `NoAccess` | Section 3 |

### 5.3 Forms and modals found today (brief D)

| Form / popup | Problems found | Target |
|---|---|---|
| Add Product (4 steps) | Blank page (C1); steps unmount (data loss); tax list limited to 8/5/4 %; fixed example variant rows | Single page with sections + variant table editor; any tax rate; Wizard rules |
| Edit Product (modal) | Silent native validation (H3); variants not editable | Full page "Product" with Details / Variants & prices / Stock / Online tabs |
| Add Customer, Add Supplier (2 steps) | Cannot save (C3); every field marked required though only the name is | One-page form, picture optional |
| Add Category, Brand, Unit | Brand needs a picture (C4); no success message | Small modal or sheet, toast on success |
| POS Add New customer | Name and phone only | Name, phone, email (optional), GSTIN (optional) |
| Store quick-add | Unreachable (C2) | Settings → Stores |
| Users add/edit | Inline errors OK; buttons enabled without permission | Sheet; disable by permission |
| Roles | `alert()`/`confirm()` | ConfirmDialog; permission matrix grouped by area with "Select all" per group (keep) |
| Add Purchase | Variant bug (C5); line total shows 0.00 until the total box is touched; no receive later | ProductPicker lines with live totals; Purchase detail page with Receive / Cancel / Record payment |
| Add Sale | OK; device date (M10) | Same layout as checkout cart, shop time zone |
| Add Return | OK; raw date in list | Keep flow; return detail page with credit note print |
| Stock adjustment (modal) | Good; becomes cramped at 360 | Sheet on phones |
| Inventory edit (modal) | Good | Sheet; show the movement it will create |
| Settings tabs | Good validation | Keep; move Stores here |
| Email (SMTP) | Good | Keep; add "last 20 emails" log (section 9) |
| Product Showcase | `alert()` messages; silent slug rewrite (H4); visible to cashiers (M13) | Online shop page; confirm dialog for the web address |
| Receipt (modal) | Not a real dialog (no focus trap); print uses the browser page | Dialog/sheet; thermal-width print stylesheet (section 9) |
| Price changed (Radix) | Good | Keep, restyle |

---

## 6. Shop dashboard and POS checkout (brief E)

### 6.1 Dashboard

- **Header**: "Good afternoon, {first name}" (fix M3), store switcher (All stores / one store), date range (Today, Yesterday, Last 7 days, This month, Custom). The server already accepts `storeId`, `startDate`, `endDate`.
- **Row 1 (today, big)**: Net sales (after returns), Orders, Average bill, Items sold, each with change vs. the previous period. The server already returns `totalReturns` and `netRevenue`.
- **Row 2 (money)**: Collected, Due from customers (tap → Customer dues), Refunds, Discounts given.
- **Charts**: Sales by day/hour (line) with a revenue/orders switch; Top products (horizontal bar, not a pie); Payment methods (donut, once split payments exist).
- **Action cards**: Low stock (count, tap → Stock filtered), Purchases waiting to be received, Customers with dues, Trial/billing status.
- **Cashier view**: only own sales, no money-due cards unless permitted.
- States: skeleton loading, empty ("No sales yet today – open Checkout"), error with Retry.

### 6.2 POS checkout

Layout:

| Width | Layout |
|---|---|
| ≥ 1024 px | Left: search bar (always focused), category chips, product grid. Right: cart panel (420 px) with customer, lines, totals, PAY. Top strip: store (visible, fixes H1), cashier, "Hold", "Recall" |
| 768–1023 px | Same, cart 360 px; product tiles 3 per row |
| < 768 px | Two screens: **Products** (search + grid, a floating cart bar "3 items · ₹612.00 → Review") and **Cart** (full-screen sheet with lines, totals, PAY). Fixes H7 |

Speed and input:
- **Name/SKU search first**: the search box is always focused; typing filters by name or SKU, Enter adds the single or exact SKU match with a short highlight. **Barcode: later** (deferred by the owner on 1 Nov 2026: scanner input and the `GET /products/barcode/{code}` lookup are not part of S6).
- **Keyboard**: F2 search, F4 customer, F8 discount, F9 hold, F10 or Ctrl+Enter pay, + / − quantity on the selected line, Del remove line, Esc close sheet. Shortcuts listed under a "?" button.
- **Quantity**: tap the number to type it; long-press +/−; weight items later (needs decimal quantities, J).
- **Tiles**: picture or a coloured initial (L4), name, price, stock badge ("3 left", "Out of stock" greyed). Own cart items stay tappable (adds one) instead of greying out (M6).
- **Totals**: the cart shows the **server's figures**: a "price check" call (new, read-only) or the pricing engine shared with the server, so PAY always equals the receipt (fixes H2). Question Q1.
- **Payment sheet**: big amount, payment method buttons (Cash, UPI, Card, Credit/On account; split later), cash keypad with quick-notes ₹10–₹2,000 and "Exact", change due in large type, optional email/phone for the e-receipt, **Complete sale** (one button, disabled while saving). Paid amount is sent, so part payment and dues become possible (needs a product decision, Q5).
- **After the sale**: success screen with change due, Print, Email, WhatsApp share (later), **New sale** (auto after 5 s, configurable).
- **Errors**: inline in the payment sheet (stays open on failure, unlike today), using the toast spec for network errors with Retry (same idempotency key: safe).
- **Locks**: renew the lock while the item is in the cart, release on unload; never block the same user (M6).

---

## 7. Page-by-page redesign

| Page | Today | Target |
|---|---|---|
| Sign in / reset / invite | Clean | Keep; remove the 401 console noise (L1); show shop logo |
| Dashboard | Section 6.1 | Section 6.1 |
| Checkout (POS) | Section 6.2 | Section 6.2 |
| Orders (Sales) | Client-side list, no voided, no actions | DataTable with server paging; filters: date, store, status, payment (with Partial), cashier, customer; row → **Order detail** page (lines, payments, receipt actions, returns, history; Edit and Void when built, Q6) |
| Returns | Raw dates, raw View | DataTable; **Return detail** with credit note print |
| Invoices & dues | Same data as Orders | Merge into Orders with a "Dues" view and "Record payment" (later, J) |
| Products | Client list; Add Product blank | DataTable with picture, SKU, price, stock per store, status; **Product page** (create/edit) with variants; bulk actions (activate, deactivate, category); Import keeps the current dialog |
| Stock | OK | DataTable with store filter, low-stock filter; inline "Adjust" action |
| Adjustments & history | Good | Keep; plain-word reasons (M11) |
| Purchases | Add only | List + **Purchase detail** with Receive, Cancel, Record payment; status timeline |
| Customers / Suppliers | Add pages broken | DataTable + one-page form + detail page (purchases or sales, dues) |
| Categories / Brands / Units | OK except Brand (C4) | Compact tables with sheet forms; sub-categories under categories |
| Reports | 4 pages, broken filters | One page with tabs, shared FilterBar (dates, store, product, customer), server exports, summary cards, print stylesheet |
| Online shop | Showcase | Profile, products publishing, preview; confirm for web address changes |
| Users / Roles | OK | DataTable + sheets; role matrix grouped |
| Settings | Good | Left sub-navigation: Business, Stores, Tax & invoices, Receipts, Email, Home page, Appearance, Plan & billing |
| Billing | OK | Keep content, new shell |
| 404 / No access | Silent redirect | Proper pages (section 4.2) |

---

## 8. Super Admin console (brief F)

### 8.1 What exists (from code and the sample-data screenshots)

- **Tenants** list: search (name, subdomain, owner email), status filter, 50 per page; columns Name, Subdomain, Status, Plan, Owner; **New tenant**.
- **Tenant page**: Overview (ID, owner, billing email, plan, created, last activity, schema version, storage, suspension, notes), Subscription (status, trial end, period end), Actions (Edit details, Change subdomain, Suspend / Reactivate, Delete / Restore, Owner sign-in link, Retry provisioning). Tabs: Billing (subscription actions, invoices, provider events), Exports (data export, restore tests), Audit.
- **Plans**, **Billing**, **Audit log** pages. Sign-in with password + authenticator; step-up for sensitive actions.
- CSP-clean: plain CSS with tokens in `global.css`, ESLint bans inline styles and HTML-from-strings. No violations at any width.
- **At 360 px** (`admin-360_*`): the top menu wraps to two rows, the tenants table is cut off on the right (L5), action buttons wrap unevenly.

### 8.2 What to add for control

| Addition | Why | Needs server work |
|---|---|---|
| **Usage against plan limits** on the tenant page and as a list column: products, stores, users, storage, platform emails per day (bars with % and colour at 80/100 %) | Spot tenants about to hit limits before they call support | Yes: one read endpoint aggregating counts (limits exist in `plans`) |
| **Activity**: last staff sign-in, last sale, sales in the last 7/30 days, active users | Know who is really using the product; churn risk | Yes: read-only aggregates from the tenant schema |
| **Contacts**: owner name and phone, billing contact name/email/phone, GSTIN (for platform invoices) | Today only the owner email and billing email | Yes: columns + edit |
| **Internal notes timeline** (who, when) instead of one notes field | Support history | Yes |
| **Feature flags per tenant** (e.g. new checkout, public shop) with reason | Gradual rollout of the redesign (slice S6) | Yes: flags table + read in tenant app |
| **Per-tenant limit overrides** (value, reason, expiry), step-up and audited | Sales promises, pilots | Yes |
| **Email health**: tenant SMTP status and last error, platform emails sent (24 h), outbox backlog, failed/dead emails with a "retry" button | Receipt emails are customer-facing | Partly: data exists in outbox / settings; needs a platform read endpoint |
| **Storage breakdown**: product pictures, receipts, exports | Storage is a plan limit | Yes |
| **Health**: schema version vs. latest, provisioning state, last migration run, last backup/restore test result | Operations | Partly (schema version shown; restore tests exist) |
| **Support tools**: resend owner invite (exists as sign-in link), unlock a locked staff account, force sign-out of all staff, "view as owner (read-only)" | Faster support | Yes; read-only impersonation is a **bigger decision** (security, audit, consent) – question Q9 |
| **List improvements**: sortable columns, created date, last activity, usage warning icon, saved filters (e.g. "trial ending in 7 days"), CSV export | Daily triage | Partly |

### 8.3 Responsive layout (CSP-safe)

- **< 768 px**: top bar with product name + menu button; navigation in a drawer (CSS-only toggle with a `<button>` and `aria-expanded`); tables become **stacked cards** (name, status badge, plan, owner) via CSS grid; tenant actions collapse into one "Actions" menu; tabs scroll horizontally.
- **≥ 768 px**: left sidebar (Tenants, Plans, Billing, Audit, later Health), content max width 1200 px, two-column tenant overview.
- Modals use the native `<dialog>` element (no library, no inline styles); toasts use the shared `Toast` with console CSS classes.
- Tenant text stays plain text (React escaping only); the existing ESLint rules stay and gain one more: no third-party UI kits in `admin-console/`.

---

## 9. Libraries (brief H)

| Area | In use today (files) | Decision |
|---|---|---|
| UI primitives | Radix UI (10 files) + shadcn-style wrappers (`class-variance-authority`, `clsx`, `tailwind-merge`) | **Keep** for the shop app; build the kit on them |
| | Headless UI (1: `ActionModal`) | **Remove**: replace with Radix Dialog |
| | MUI + Emotion (1 icon: `LoopIcon`) | **Remove** both packages (large; Emotion injects styles at run time) |
| Styling | Tailwind 4 (`@tailwindcss/vite`), `tw-animate-css`; console: plain CSS | Keep; add shared `tokens.css` |
| Icons | lucide-react (36 files), Heroicons (19), MUI icons (1) | **Lucide only** (consistent stroke, tree-shaken) |
| Charts | react-chartjs-2 + chart.js (chart.js **not declared**, L6) | Keep Chart.js, **declare it**; bar/line/donut only |
| Tables | TanStack Table (2) | Keep; `DataTable` on top |
| Messages | Custom `ErrorContext`, `window.alert/confirm`, Headless UI modal | One shared `Toast` + `ConfirmDialog` (section 2) |
| Forms | None (hand-written state, `FormData`) | Add **react-hook-form** (no runtime styles, small) for the shop app; console keeps plain state (question Q16) |
| Exports | jsPDF + jspdf-autotable (list PDF), write-excel-file (list XLSX); reports already export on the server | Reports: server exports (keep). Lists: Excel only with readable headers; drop client PDF (question Q15) |
| QR | qrcode-generator (console MFA) | Keep |
| Desktop | `electron.js`, `preload.js` in `frontend/` (not wired into the builds) | Leave alone; relevant only if a local print agent is chosen (Q12) |
| Testing | none for the frontend | Add `@playwright/test` and `@axe-core/playwright` (dev only) |

---

## 10. Suggestions (brief I)

Effort: **S** ≤ 2 days, **M** 3–5 days, **L** 1–2 weeks, **XL** > 2 weeks (one developer).

| Priority | Suggestion | Effort |
|---|---|---|
| Must | Frontend CI (lint, both builds, route smoke test at 3 widths) | S |
| Must | Error boundaries, 404 and No-access pages | S |
| Must | Fix the broken forms (C1, C3, C4, C5, H1, H3, H4) before redesigning them | M |
| Must | One message system (toast, inline alert, field error, confirm) | M |
| Must | Checkout totals equal the receipt (server-priced cart) | M |
| Must | Stores page | M |
| Must | Server-side paging and filters on all lists | L |
| Must | Accessibility baseline: linked labels, focus rings, keyboard paths, contrast, axe-clean pages | M (ongoing) |
| Should | Order, Purchase, Return, Customer, Supplier **detail pages** with actions (receive, cancel, record payment, void with reason) | L |
| Should | Onboarding checklist for a new shop (business details, tax, store, import products, invite a cashier, first sale) | S |
| Should | Global search / command palette (Ctrl+K): products, customers, orders by receipt number | M |
| Should | Activity log page for owners (the server has `audit-log` and the permission already) | S |
| Should | Notification centre: low stock, failed receipt emails, trial ending | M |
| Should | Print stylesheets: 80 mm and 58 mm receipts, A4 tax invoice, reports | S |
| Should | Saved list views and column choice per user | M |
| Could | Installable web app (manifest, icons; **no offline mode**) for tablets at the counter | S |
| Could | Dark mode polish (tokens make it nearly free) | S |
| Could | In-app help ("?" tooltips, short guides from `docs/POS_FLOW.md`) | M |
| Could | Visual regression screenshots in CI | M |

---

## 11. POS expert review (brief J)

### 11.1 Where we stand

Compared with Shopify POS, Square POS and Lightspeed Retail (public feature descriptions, not their assets), and with what a busy Indian counter (kirana, apparel, mobile accessories, pharmacy-adjacent, small chains) needs:

| Area | Us today | Typical in Shopify / Square / Lightspeed | Indian counter reality |
|---|---|---|---|
| Checkout speed | Tap tiles, name search, no scanner path, no shortcuts | Scan-first, quick keys, favourites grid | Scanner + keyboard, 20–60 bills an hour at peak, queue at the counter |
| Payments | One method, always "paid in full" | Split tender, card/terminal, gift cards | Cash + UPI dominate; mixed cash+UPI common; customer credit (*khata*) is normal |
| Customers | Name/phone; no dues collection | Profiles, history, loyalty | Phone number as the key, dues ledger, WhatsApp receipts |
| Returns | Good (cumulative, exact refunds) | Returns + exchanges in one flow, store credit | Exchange is more common than refund |
| Inventory | Stores, adjustments, ledger, low-stock status only | Transfers, counts, reorder points, POs | Batches/expiry for FMCG, IMEI/serial for mobiles, MRP |
| Purchasing | One-step purchase; no receive later; no supplier payments | PO → receive (partial) → bills → payments | Supplier credit and dues are central |
| Cash control | None | Shifts, cash drawer counts, Z-report | Owner wants the drawer reconciled every evening |
| Reports | Sales lines, purchases, customer totals | Day-end, by cashier, by payment, tax, margin | GST summaries for the accountant, day-end sheet |
| Roles | Good server permissions, fixed discount cap | Manager PIN overrides | Owner approves big discounts in person: PIN override |
| Receipts | Screen, A4-ish PDF, email | Thermal print, email/SMS, custom footer | 58/80 mm thermal printers, WhatsApp share, GSTIN and HSN on invoices |

### 11.2 Ideas

Effort as in section 10. "Slice" refers to section 12. Items marked ⚠ need legal or tax verification against current rules before building; items marked ★ are bigger decisions.

| Idea | Problem it solves | Who benefits | Priority | Effort | Slice |
|---|---|---|---|---|---|
| **Barcode scanning in checkout** (keyboard-wedge scanners, exact barcode match, beep, highlight) | Typing names is slow and error-prone | Cashier | Must | S | **barcode: later** (deferred) |
| **Keyboard shortcuts + always-focused search** | Mouse-free speed at peak | Cashier | Must | S | S6 |
| **Quick product grid / favourites** (owner pins 12–24 items) | Loose items without barcodes (tea, snacks) | Cashier | Should | S | S6 |
| **Hold / park bills** (several open carts, recall by number or customer) | Customer forgets an item; queue keeps moving | Cashier | Must | M | S10a |
| **Split and mixed payments** (cash + UPI + card + credit, in one sale) | Very common in India; today impossible | Cashier, owner | Must | M (server: payments table) | S10a |
| **Static UPI QR on screen** for the shop's UPI ID with the amount pre-filled; cashier confirms receipt manually | Faster UPI without typing; no gateway | Cashier, customer | Should ⚠ (QR format and merchant terms per NPCI/bank) | S | S10a |
| Dynamic UPI with automatic confirmation | No manual check | Cashier | Could ★ (needs a payment provider) | XL | later |
| **Customer credit and due collection** (sell on credit with a limit, "Collect payment" against dues, statement) | *Khata* is how many regulars pay | Owner | Must | L (server) | S10b |
| **Shift open/close with cash count** (opening float, cash in/out, expected vs counted, variance) | Cash leakage; accountability per cashier | Owner | Must | M (server) | S10c |
| **Day-end (Z) report**: sales by payment method, returns, discounts, cash expected, by cashier | Evening reconciliation | Owner, accountant | Must | M | S10c |
| **Manager PIN override** for discounts above the cap, price changes, voids | Owner nearby approves without signing in | Owner, cashier | Should | M | S10c |
| **Thermal receipt printing** via 58/80 mm print stylesheet (browser print) | Most counters use thermal printers | Cashier | Must | S | S6 |
| Direct ESC/POS printing, cash drawer kick, silent print | Faster printing; drawer opens on cash sale | Cashier | Could ★ (needs a local print agent or WebUSB; browser alone cannot) | L | later |
| **Barcode label printing** (sheets and label printers, price + MRP + barcode) | Loose and repacked goods need labels | Owner | Should | M | **barcode: later** (deferred) |
| **Returns and exchanges in one flow** (return item, add new items, pay/refund difference) | Exchanges dominate apparel/footwear | Cashier | Should | M | S10b |
| **Store credit / credit note** instead of cash refund | Keeps money in the shop | Owner | Should ⚠ (credit-note numbering and GST treatment) | M | S10b |
| **GST invoice completeness**: customer GSTIN and state, place of supply → CGST+SGST or IGST **per sale** (today a shop-wide setting), HSN on invoice lines, B2B vs B2C | Correct tax invoices for business customers | Owner, accountant | Must ⚠ | M | S10d |
| **GST summaries**: sales by tax rate and HSN, B2B/B2C split, credit notes; export for the accountant | Monthly filing preparation | Accountant | Should ⚠ (verify current return formats; e-invoicing applicability depends on turnover thresholds) | M | S10d |
| **Round-off** of bill totals to the nearest rupee with a shown "round off" line | Cash convenience; common practice | Cashier | Should ⚠ | S | S6 |
| **MRP field** and "selling below MRP" display; block selling above MRP | Customer trust; packaged-goods labelling | Owner | Should ⚠ (Legal Metrology rules) | S | S8 |
| **Low-stock alerts and reorder suggestions** (reorder point, suggested quantity from last 30 days) | Running out of fast movers | Owner | Must | M | S10e |
| **Purchase orders → receive (partial) → supplier bill → payments**, supplier dues | Today purchases are one step and cannot be received later | Owner | Must | L (server exists partly) | S8 + S10e |
| **Stock transfers between stores** (send, in transit, receive) | Multi-store chains move stock daily | Owner | Should | L (server) | S10e |
| **Stock count (cycle count)** with variance → adjustments | Year-end and spot checks | Owner | Should | M | S10e |
| **Batch and expiry tracking** (FEFO picking, expiry report) | FMCG, cosmetics, food, OTC | Owner | Should ⚠ (stricter for medicines: verify drug-sale rules) | L | later |
| **Serial / IMEI tracking** | Mobiles and electronics warranty | Owner | Could | L | later |
| **Price lists** (retail, wholesale, member) per customer group | Wholesale-retail shops | Owner | Could | M | later |
| **Coupons and promotions** (% or ₹ off, buy X get Y, date ranges) | Festive sales | Owner | Could | L | later |
| **Loyalty points** by phone number | Repeat customers | Owner, customer | Could | M | later |
| **WhatsApp / SMS e-receipts** | Customers rarely give email | Customer | Should ★ (needs a messaging provider and consent; a free "Share via WhatsApp" link is S) | S / L | S6 (share link) |
| **Decimal quantities and weighed items** (kg, g, m) | Loose grains, cloth | Cashier | Should | M (server: quantities are whole numbers) | later |
| **Customer-facing display** (second screen) | Transparency at the counter | Customer | Could | M | later |
| **Offline selling** | Internet drops | Cashier | Could ★ (major architecture decision: sync, conflicts, stock) | XL | not planned |

---

## 12. Build slices (step 2)

Each slice is shippable on its own, keeps the app working, and ends with its tests from section 13. Effort for one developer.

| # | Slice | Contents | Files likely to change | Risks | Effort |
|---|---|---|---|---|---|
| **S0** | Safety net and CI | Fix the `usePermission`/`isAdmin` crash; `AppErrorBoundary` + `RouteErrorBoundary`; 404 and No-access pages (no silent redirects); frontend CI job (lint with zero errors, both builds); Playwright route smoke test (3 widths, owner and cashier) | `App.jsx`, `pages/AddProduct.jsx`, `components/ProtectedRoute.jsx`, new `components/errors/*`, `eslint.config.js`, the 37 lint problems, new `.github/workflows/frontend.yml`, new `frontend/e2e/` | Fixing old lint errors touches many files: keep to mechanical changes | S–M (3 d) |
| **S1** | Broken-form fixes (before redesign) | Wizards keep steps mounted and use state (Add Product, Customer, Supplier); drop empty image fields (Brand, Customer, Supplier); Add Purchase variant compare and live line totals; POS store picker value; Edit Product required flags match the server; Showcase web-address confirm; Sales Report SKU column; signed-in list pictures (M2); Add Sale shop time zone; plain-word stock reasons | `pages/AddProduct.jsx`, `AddCustomer.jsx`, `AddSupplier.jsx`, `AddBrand.jsx`, `AddPurchase.jsx`, `AddSales.jsx`, `POS.jsx`, `forms/EditProductForm.jsx`, `ShowcaseSettings.jsx`, `lists/SalesReportList.jsx`, `StockAdjustments.jsx`, `lib/format.js` | Behaviour changes users may notice (good ones); covered by form tests | M (4–5 d) |
| **S2** | Tokens and messages | `shared/tokens.css` for both apps; `Toast`, `InlineAlert`, `FieldError`, `ConfirmDialog`; `api.js` wiring; replace every `alert`/`confirm`; success toasts on all saves; console uses the same toast with CSS classes | new `frontend/shared/`, `context/ErrorContext.jsx`, `GlobalErrorHandler.jsx`, `services/api.js`, `index.css`, `admin-console/src/global.css`, ~25 pages for success toasts | CSP in the console: no runtime styles (verified by test) | M (5 d) |
| **S3** | Shell and navigation | New grouped sidebar with role-based visibility; bottom tabs on phones; Reports hub with tabs; Settings sub-navigation; shorter trial banner on phones | `layout/Sidebar.jsx`, `AppLayout.jsx`, `Topbar.jsx`, `BannerArea.jsx`, `routeTitles.js`, `context/PermissionContext.jsx`, `App.jsx`, report pages, `Settings.jsx` | Route names change: keep old paths as redirects | M (5 d) |
| **S4** | Component kit + pilot pages | `PageHeader`, `DataTable` (server paging, card mode < 640 px), `FilterBar`, form kit (react-hook-form), `Modal/Sheet`, empty/loading/error states, `ProductPicker`; migrate Customers, Suppliers, Categories, Brands, Units, Stock | `components/ui/*`, new `components/data-table/*`, `components/form/*`, the 6 pilot pages and their lists | Kit churn: freeze the API after the pilot | L (2 w) |
| **S4b** | Polish and checkout fixes (added after S4) | Brand palette (navy #0F172A, slate #1E293B, blue #2563EB, light #F8FAFC, text #334155) in both apps; fixed shell (100dvh), sticky table headers, pagination pinned under tables; one `Pagination` everywhere; dialogs capped at the screen with fixed header/footer; receipt in the PDF layout, printed from a frame on A4/80 mm/58 mm; PDF drops "Receipt N · version N" and says "Received"; Finalize sale in two columns; compact POS tiles, "Choose option" for variants, cart lines per variant, lock per product, phone cart sheet; images through `ProductImage`/`AuthImage` | `shared/tokens.css`, `index.css`, `ui/dialog.jsx`, `kit/*`, `POS.jsx`, `PayForm.jsx`, `Receipt.jsx` + `receipt.css`, `receipts/pdf.go`, console `global.css` | Checkout look changes before S6 (logic and server rules unchanged) | M |
| **S5** | Stores page | Settings → Stores (list, add, edit, activate/deactivate); server: list inactive stores, deactivation rules (not the last active store; warn on default store) | new `pages/settings/Stores.jsx`, `backend-go/internal/catalog/stores.go` (+ tests) | Server change, small | M (3–4 d) |
| **S6** | New checkout | Section 6.2: layout per width, two-screen phone mode, name/SKU search (barcode: later), shortcuts, favourites, server-priced cart (new read-only "quote" endpoint reusing `pricing.Compute`), payment sheet, success screen, thermal print stylesheet, round-off (if approved), lock renewal, WhatsApp share link. Ships behind a per-shop setting; old POS kept until approved | new `pages/checkout/*`, `services/api.js`, `backend-go/internal/sales` (quote endpoint + tests), `products` (lock refresh), print CSS | Core money flow: full regression suite before switching the default | L (2 w) |
| **S7** | Dashboard | Section 6.1; server: greeting name, low-stock count, previous-period figures (filters already exist) | `pages/Dashboard.jsx`, `backend-go/internal/reports/dashboard.go` | Numbers must match reports: cross-checked in tests | M (4–5 d) |
| **S8** | Remaining pages | Orders + Order detail (receipt actions, returns, void with reason if Q6), Products list + Product page with variants (barcode labels: later), Purchases + Purchase detail (Receive, Cancel, Record payment: server exists), Returns + detail, Users/Roles, Online shop, Reports polish | most of `pages/*`, `components/lists/*` | Size: split into S8a–S8e, one area each | XL (3–4 w) |
| **S9** | Super Admin console | Responsive shell (drawer, card tables, `<dialog>`), shared toast; tenant page: usage vs limits, activity, contacts, notes timeline, email health, storage, health; list sorting and saved filters | `admin-console/src/*`, `backend-go/internal/platform` (read endpoints + tests) | Mostly server work; strict CSP | L (1.5–2 w) |
| **S10a–e** | POS expert features | a: hold bills, split payments, static UPI QR · b: customer credit/dues, exchanges, credit notes · c: shifts, cash count, day-end report, manager PIN · d: GST invoice completeness and summaries · e: low stock/reorder, PO → receive → supplier payments, transfers, stock counts | sales, purchases, reports packages (server) + new pages | Each needs its own short design and approval; ⚠ items need tax/legal verification | L each |

Recommended order: S0 → S1 → S2 → S3 → S4 → S5 → S6 → S7 → S8 → S9, with S10 items approved one by one after S6. S0 and S1 can ship within the first week and remove every Critical finding except the missing receive/cancel purchase UI (S8).

---

## 13. Test plan for step 3

All browser tests use Playwright (Chromium; WebKit for the checkout on tablets) against the dev stack, as in this audit, and run in CI after S0.

| Area | What | Pass when |
|---|---|---|
| **Route smoke** | Every route × 360/768/1280 × owner, admin-role, cashier | No page errors, no unexpected console errors, no 5xx, no horizontal scroll, page title and main landmark present, forbidden routes show No-access |
| **Forms** | Every form in brief G: valid save, each required field missing, server validation error, network failure (request aborted), double submit | Saved record appears; correct toast/inline/field messages from section 2.3; no duplicate records |
| **Accessibility** | axe-core on every page at 360 and 1280; keyboard-only path through sign-in, checkout, add product, add return; visible focus; reduced motion | Zero serious/critical axe issues; every input has a linked label; all actions reachable by keyboard; contrast ≥ 4.5:1 |
| **CSP (console)** | Every console page under its real CSP with sample data, listening for `securitypolicyviolation` and "Refused to" console messages; check no `<style>` elements are added at run time | Zero violations |
| **POS regression** | Cash sale with change; exact UPI/card; discount at cap (allowed) and above cap (refused) as cashier; tax-inclusive and tax-exclusive items; **screen total = receipt total** for 20 generated carts; price changed while in cart; out of stock; name/SKU search adds the exact SKU on Enter; hold/recall (when built); connection lost then retry (one sale only); receipt print, PDF, email, resend limit; return partial, full, too many; purchase received raises stock; adjustment; Add Sale with part payment | All expected figures equal; stock ledger reconciles (reuse the server's reconcile check) |
| **Visual review** | Screenshots of every page at three widths attached to each slice's PR | Reviewed by you (automatic pixel diffs only if Q11 = yes) |
| **Data** | Tests create their own records with a `E2E-` prefix in a dedicated test shop | No test writes into demo-shop except on request |

---

## 14. Questions (one batch)

Each has my recommended default; answer only where you disagree.

1. **Checkout totals**: add a small read-only "quote" endpoint so the cart always shows the server's figures? *Default: yes.*
2. **Font**: self-host Inter? *Default: yes (no external CDN).*
3. **Brand colour**: keep today's blue as primary and build the palette around it? *Default: yes.*
4. **Stores**: offer delete at all? *Default: no; only activate/deactivate.*
5. **Part payment and customer credit at the till**: allow selling on credit (needs a new permission and a credit limit)? *Default: yes, in S10b, owner-only by default.*
6. **Order edit and void screens**: build "Void with reason" (manager) and skip "Edit" (use return + new sale)? *Default: yes.*
7. **Client error reports**: keep errors in the browser console with "Copy details", no server logging yet? *Default: yes.*
8. **Checkout rollout**: new checkout behind a per-shop setting, old POS kept for one release? *Default: yes.*
9. **"View as owner (read-only)" in the console**: postpone? *Default: postpone; needs a separate security design.*
10. **Phones**: every page usable at 360 px, with the checkout optimised for tablets (768+) but fully usable on phones? *Default: yes.*
11. **Visual regression diffs in CI**: *Default: no; screenshots reviewed by you per slice.*
12. **Printing**: thermal printing through the browser print dialog only (no local print agent, no cash drawer kick) for now? *Default: yes.*
13. **Fix first**: ship S0 and S1 (bug fixes) before any redesign work? *Default: yes.*
14. **UPI QR**: static QR from the shop's UPI ID with manual confirmation? *Default: yes (S10a), after checking the bank/NPCI terms.*
15. **List exports**: drop the client-side PDF export of lists, keep Excel with readable headers? *Default: yes.*
16. **Form library**: adopt react-hook-form in the shop app? *Default: yes.*
17. **Audit records**: keep the UI-AUDIT records in demo-shop? *Default: keep; delete only on your request.*

### 14.1 Decisions after S4 (2026-11-01)

- S5 store rules: yes (list inactive stores, keep one store active, warn on the default online store).
- S6: the new checkout is switched per shop (a setting stored on the server), not per browser.
- S8 detail pages: actions on existing endpoints (purchase receive/cancel/payment, void with reason): yes.
- S9 console: read-only parts plus contacts and notes now; flags and limit overrides only after a security note.
- Checkout store change (S4b follow-up): keep the cart and warn about items without stock in the new store (do not empty it).
