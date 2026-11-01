# UI build log (step 2)

One entry per slice: what changed, how it was tested, the result. Screenshots of every page at 360, 768 and 1280 px for the owner and the UI-AUDIT Cashier are in `slices/<slice>/`. Tests run with `frontend/e2e/run-in-docker.sh` against the dev stack.

## S0 – Safety net and CI

**Changed**
- Add Product no longer crashes (missing `usePermission` import; `isAdmin` replaced by the `products:create` permission).
- `ErrorBoundary` around the whole app (`Root.jsx`) and around the page area (`AppLayout`, reset on navigation). A crash now shows "Something went wrong on this page" with Try again / Go to Dashboard / Copy details; a stale code chunk after a deploy shows "A new version is available". Same safety net in the Super Admin console (CSS classes only).
- Forbidden pages show "You don't have access to this page" inside the app instead of silently redirecting; unknown addresses show "Page not found" (signed out: sign-in first).
- Lint: 37 problems → 0 errors (18 warnings remain: hook dependency and hot-reload hints). Build output and Node files are no longer linted as browser code.
- New CI workflow `.github/workflows/frontend.yml`: install, lint (errors fail), build the shop app and the console.
- New browser test suite `frontend/e2e/` (Playwright 1.56.1, axe for later slices), run in the official Playwright container.
- `chart.js` declared; `react-hook-form`, `@playwright/test`, `@axe-core/playwright` added (dev tools only for the last two).

**Tested**: `e2e/smoke.spec.js`, owner and cashier × 360/768/1280 (6 runs, 34 routes each): no page errors, no blank pages, no 5xx, no sideways scrolling, "Page not found" on unknown routes, "no access" on every route the Biller may not open. **6 passed.**

Note: the test navigates inside the app (like clicking links). Full page reloads restore the session each time and hit the server's per-address rate limit on session refresh.

## S1 – Broken-form fixes

**Changed**
- Multi-step forms (Add Product, Add Customer, Add Supplier) keep every step on the page (hidden when not current), read values from the whole form, and on a problem open the step with the first invalid field (`lib/wizard.js`). Empty picture fields are no longer sent (also Add Brand).
- Customer and supplier forms: only the name is required (as on the server).
- Add Product: tax is any rate from 0 to 100 (was 8/5/4 % only); prices accept paise (`step` 0.01; "99.5" used to be refused by the browser); the example variant rows are hints, not values; the −/+ buttons no longer submit the form; Sub Category, Brand and Unit no longer look required.
- Edit Product: Category, Brand and Unit not required; number boxes accept decimals; labels linked to their inputs.
- Add Purchase: sizes/variants are found (text vs number ID); line totals and the total update as you type.
- POS: the store picker shows the selected store.
- Product Showcase: changing the web address asks first.
- Sales and Purchase reports show the product SKU.
- Private pictures (categories, brands, products) load with the signed-in session; products without a picture show coloured initials.
- New sales, returns and purchases default to today in the shop's time zone.
- Stock history: "Set by hand", "Product edited".

**Tested**
- Route smoke (`smoke.spec.js`) on the S1 build: owner and cashier × 3 widths, **6 passed**.
- `s1-forms.spec.js`, owner × 3 widths: customer (empty name goes back to step 1; name only saves), supplier, brand without picture, single product with 18 % tax and a ₹99.50 price saved from the last step, edit of a product without a brand, purchase of a product with sizes (sizes listed, line total 500.00), POS store shown, web-address change asks first, report SKUs, no 401 for pictures. **3 passed** (first run found the price-step bug, fixed above).

## S2 – Tokens and one message system

**Changed**
- `frontend/shared/tokens.css`: status colours (success, info, warning, danger with background and text pairs), surface/text/border, focus, shadows, type scale, 4 px spacing, radius, motion (respects reduced motion), touch sizes; light and dark. Used by both apps; mapped into Tailwind (`bg-danger-bg`, `text-success`, …) in the shop app.
- Self-hosted Inter font (`@fontsource-variable/inter`) in both apps.
- Shared toast (`frontend/shared/toast/`): plain JavaScript store + React component + static CSS, no run-time styles. Success 4 s, info 5 s, warning 7 s, error 8 s (or until closed when marked sticky); pause on hover/focus; × button (44 px) and Esc; at most 3; identical messages merged (×2); polite live region for success/info, assertive for errors. Desktop: top right under the top bar; phones: top, full width (moved from the bottom during testing: there it covered the forms' Save buttons).
- The old red corner box is gone; every API error goes through the toast. Success toasts on every add, edit and delete (products, customers, suppliers, categories, brands, units, purchases, sales, returns, roles, users, settings, storefront, receipt email).
- `ConfirmDialog` (`useConfirm()`): app dialog, bottom sheet on phones, optional type-to-confirm; replaces every `window.confirm` and `window.alert` (roles, storefront, email settings, public shop).
- `InlineAlert` and `FieldError` components; the POS error bar uses `InlineAlert` (announced to screen readers).
- Dialogs can opt into a phone bottom sheet (`sheet` prop).
- Console: shared tokens, font and toasts; success notices became toasts; errors stay inline. The new `shared/` folder is under the console's strict lint rules.

**Tested**
- `s2-messages.spec.js` × 3 widths: success toast inside the screen (top on phones), error toast for a duplicate and closing it, delete confirm in an app dialog (cancelled; nothing deleted), web-address confirm in an app dialog, **no browser pop-ups**, axe on the toast area and the dialog (no serious/critical issues); console: **no CSP violations, no `<style>` elements, no inline styles**, tokens and Inter loaded. **6 passed** (earlier runs found a list-semantics axe issue in the toast and the bottom-toast overlap; both fixed).

## S3 – Shell and navigation

**Changed**
- New grouped menu (`layout/navigation.js`): Overview · Sell (Checkout, Orders, Returns, Invoices & dues) · Inventory (Products, Stock, Adjustments & history, Purchases) · Contacts (Customers, Suppliers) · Catalogue setup (Categories, Brands, Units) · Reports · Online shop (Storefront) · Team (Users, Roles & permissions) · Settings (Settings, Plan & billing). Items show by permission; empty groups are hidden; the open page is marked (`aria-current="page"`) including its sub-pages. Old addresses keep working.
- Page titles follow the new names (top bar). `routeTitles.js` removed.
- Phones (< 768 px): bottom tab bar — owners/admins Dashboard, Checkout, Orders, More; cashiers Checkout, Orders, Customers, More ("More" opens the full menu). Not shown on the checkout screen. Page content gets room so nothing hides behind it.
- Reports: one menu entry, one tab bar on all four reports (Sales, Purchases, Customers, Customer dues); `/reports` opens Sales. The Purchases tab and page need the purchases view permission (cashiers now see "no access" instead of a 403 error).
- Storefront (Product Showcase) is shown to roles that can edit stores (cashiers no longer see a read-only page whose switches fail).
- Settings tabs scroll on phones and include "Plan & billing".
- Trial/billing banner: smaller, at most two lines on phones.
- Keyboard: `g` then `d`/`p`/`o` → Dashboard / Checkout / Orders; `/` focuses the page's search box.

These change only what the menu shows and which page shows "no access"; the server's permission checks are unchanged.

**Tested**
- `s3-navigation.spec.js`, owner and cashier × 3 widths: menu groups per role, active item, no Storefront/Purchases for cashiers, tab bar only under 768 px with the right tabs and a working More, content padding, Reports redirect and tabs per permission, `g o` shortcut. **6 passed.**
- Route smoke (now also `/reports`; Storefront and Purchase report counted as "no access" for cashiers): **6 passed.**

## S4 – Component kit and pilot pages

**Changed**
- Kit (`src/components/kit/`): `PageHeader`; `DataTable` (server pages, a real table from 640 px, cards on phones, skeleton loading, empty and error states with Retry, "1–25 of N", rows per page, 44 px paging buttons); `FilterBar` (search, select filters, removable chips, Clear filters; on phones the filters open in a bottom sheet); `RowActions` (one "…" menu per row, only actions the role may take); `StatusBadge` (one colour map for every status); form kit on **react-hook-form** (`FormSheet`, `TextField`, `SelectField`, `TextAreaField`, `ImageField`): labels linked to inputs, `aria-invalid` and `aria-describedby`, field errors under the field, server field errors mapped to fields with a summary at the top; `RecordsPage` (list + search + status filter + add/edit sheet + delete confirm). `usePagedList` keeps page, size, search and filters in the address.
- Pilot pages rebuilt on the kit: **Categories, Brands, Units, Customers, Suppliers, Stock**. Add/edit open in a sheet (the old add pages and their 2-step forms are gone; `/add-…` addresses open the sheet). Stock has counter tiles (All / In stock / Low stock / Out of stock, clickable) and "Set stock" in a sheet.
- Server (read-only queries): `?search=` and `?status=` on categories, brands, units, suppliers, customers; `?search=` (product, SKU, store) and `?stockStatus=` on stock. Integration test `test/isolation/list_filters_test.go`; the whole isolation suite passes.
- Accessibility found by axe and fixed app-wide: the primary colour (all five theme presets) gave white button text 3.0–3.7:1; now ≥ 4.7:1. Hover states darken instead of lightening (they dropped to 4.0:1).
- Libraries: **MUI, Emotion and Headless UI removed** (66 packages); the old list modal uses the Radix dialog (bottom sheet on phones). TanStack Table stays only in the old list kit until S8; the new `DataTable` does not need it. `ProductPicker` moves to S8, where its forms are rebuilt.
- Fixed while testing: quick filter changes could undo each other (address updates now start from the current address); on phones a long form's Save/Cancel sat below the screen edge (the sheet's buttons now stay fixed at its bottom while the fields scroll); the picture-less initials (S1) had 2.9:1 contrast (now full primary).

**Tested** — `s4-kit.spec.js` × 3 widths:
- Owner, each of the five record pages: server search (kept in the address), status filter (filters sheet on phones, chip, clear), cards instead of a table on phones, add with an empty name (field marked invalid, "… is required." under it), add (toast), edit (toast), delete asks (cancelled: nothing deleted), `/add-…` opens the sheet and Cancel returns to the list; axe on the page area and the sheet (no serious/critical issues).
- Owner, Stock: tiles filter, search by SKU, "Set stock" refuses −1 ("Must be 0 or more.").
- Cashier: may add customers but has no edit/delete menu; Categories says "no access"; Stock has no "Set stock".
- Regression: all suites together (smoke, S1, S2, S3, S4) × 3 widths. **27 passed** (the S1 and S2 tests were updated for the new forms and the in-app confirm dialog).
