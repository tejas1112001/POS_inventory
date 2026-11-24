# Teal & Cream redesign (2026-11-24)

Branch `ui/teal-theme` (local, not pushed). Shop app and platform console restyled to the moodboards in the repo root (deep teal #123B45, teal #20B6A4, ivory #F4F2E9, amber #F2B544, Manrope; light and dark).

| Area | What |
|---|---|
| Tokens | `frontend/shared/tokens.css`, `frontend/src/index.css`: teal/cream tokens, brand gradients, Tailwind blue/sky/indigo/slate/gray remapped to the teal ramps, `.dark` overrides for older screens |
| State | Redux Toolkit: shop `src/store` (mode, accent, sidebar, phone nav), console `admin-console/src/store.js` (mode, menu) |
| Kit | Reusable `components/kit`: IconTile, StatCard, MetricCard, ActionCard, Panel, SegmentedControl, QtyStepper, ThemeToggle, CartHeader, TotalCard, ChoiceButton, QuickButton; shared `shared/brand/BrandMark` |
| Shop | Sidebar, top bar (page search Ctrl+K, theme toggle), dashboard (KPI cards, revenue trend, top products, recent sales, low stock), POS/checkout, payment, sign-in, settings appearance, lists, dialogs, sales report cards |
| Console | Teal sidebar layout, top bar, sign-in, page headers, tenant stat cards, tables, badges, modals, inline filters, dark mode |
| Checks | eslint 0 errors; both vite builds; unit tests 32/32; screenshots light/dark, desktop/phone (mocked API, dev servers on 5173/5174 only) |

Follow-up 2026-11-24: tighter spacing (shell, topbar, cards, console); page buttons moved onto the FilterBar line on every list page (`FilterBar actions`); Storefront page rebuilt on PageHeader/Panel/FormField/FilterBar/DataTable with a new `TogglePill`; double page padding removed (Settings, Billing, Stock adjustments, Write-offs, form pages).

Follow-up 2: page intro lines removed (PageHeader, ListPageLayout, record configs, console headers, Roles/Add role headings); reusable `TabBar` (Settings, Reports, Adjustments; tabs no longer collapse on phones); dashboard panels no longer overflow on phones (Panel min-w-0, phone list for recent sales); data tables scroll instead of squeezing on phones; old black buttons/overlays themed; purchase report total as a StatCard.

Follow-up 3: one geometry for every page: top bar 56 px (sidebar brand row too) with the page search in a fixed middle column; content starts 16 px under it, 20 px from the side on all pages, Checkout included (both checkout screens: controls on one row with no bar, cart as an inset card like the Orders table, customer shown in the cart header instead of its own strip). Shared `SearchBox` (FilterBar, both checkouts). New filter bar: short search left, then `components/ui/filter-toolbar.jsx` ("Add filter" grows into a field → value menu; applied filters as chips; Clear all), built into `FilterBar` so every list page has it with no page changes; date boxes stay inline (phones: behind a calendar button). Uses `motion` (new dependency, LazyMotion + domAnimation, about 41 kB gzip); its CSS module uses the app's theme tokens (the generic `:root` block from the component's source was not added: it would overwrite the teal theme).

Follow-up 4: date filters are one range button (`components/ui/dropdown-range-date-picker.jsx`: year and month dropdowns over a range calendar, Clear/Apply; new `components/ui/calendar.jsx` on react-day-picker 10 + date-fns) on Orders, Returns, the four reports, payment methods, customer statement and the dashboard's custom range; `DateBox` removed. Dialogs: the sticky header/footer now stick to the dialog edges (fields no longer show above the title or below the buttons), the close button sits in the sticky header, phone footers put the buttons side by side. Older custom modals (supplier, customer, store, warehouse, subcategory, import, supplier picker, shop page) no longer cut off their top on short screens.

Follow-up 5: filter line controls smaller (search, Add filter, date button and the page buttons on that line: 36 px on desktop, 40 px on phones; search 224–256 px wide). Checkout keeps its 40 px search.

Not done: the landing site (`landing/`) is still the old blue theme.

# Rename to MintoBill (2026-11-23)

Domain is now mintobill.com. Branch `chore/rename-mintobill` (local, not pushed): every BilMitra/BillPiko name and bilmitra.com reference changed to MintoBill/mintobill.com (landing site, deploy files renamed, docs). Server not moved yet: run `backend/deploy/vps/migrate-to-mintobill.sh` after DNS for mintobill.com points at the VPS, then `deploy.sh` (docs/DEPLOY_VPS.md §8). bilmitra.com becomes a redirect.

# Status, 2026-11-21

Everything below is on GitHub `main` (pushed 2026-11-21, CI green at `9e1583a`). What is left: `REMAINING.md` (architecture review follow-up) and `NEXT_STEPS.md` (owner checks, pharmacy questions).

Built 2026-11-21 (each with tests; details in `REMAINING.md` and the design notes):

| Item | What | Design |
|---|---|---|
| H3 | Units (sell, buy, return by the box), decimal quantities everywhere, CSV import `allow_fraction` | ARCHITECTURE_REVIEW H3 |
| M3 | GST per store and B2B: GSTIN/state on shop, stores, customers, suppliers; split per sale; copies on sales and credit notes; receipts; printable credit note; invoice numbers per store and year (opt-in) | `docs/M3_GST_DESIGN.md` |
| — | Price per store; several barcodes per variant (case codes); scale barcodes | REMAINING "also decided" |
| — | GST returns: GSTR-1 workbook and GSTR-3B summary per GSTIN and month or quarter | `docs/GST_REPORTS_DESIGN.md` |
| — | Known issues: stock counts readable with the module off; batch scans in counts; expiry report CSV | `NEXT_STEPS.md` §3 |
| CI | gitleaks allowlists match the old `backend-go/` paths (full-history job had failed since 2026-11-20) | — |

# Architecture review fixes (ARCHITECTURE_REVIEW.md)

2026-11-20, on request ("do the required items immediately"). Pushed to `main` 2026-11-20/21.

| Item | What | Commit | State |
|---|---|---|---|
| H1 | `pos_tenant` group role holds USAGE on `extensions`; migrations no longer rewrite that ACL per shop (no "tuple concurrently updated"); 55P03 lock timeouts retried | `41e1379`, `bd0d7c0` | Done, full suite green |
| H4 | Bounded LRU caches (registry, auth, permissions, modules, plans, reports, import reports); per-client limit on uncached host lookups (60/min); cache metrics | `41e99ff` | Done, full suite green |
| — | `backend-go/` renamed to `backend/` (module path too) | `0f0668c` | Done, full suite green |
| M5 | Usage snapshot repairs the console's copy of the business type from the shop's settings | `7ce9ff8` | Done |
| M2 | Sweeps work on 3 shops at a time with per-shop time limits; nightly reconcile = one River job per shop (queue `tenant_maintenance`); checkpointed reconcile (tenant migration 00033, full check weekly, re-check before reporting) | `3fa12ae`, `df53a2e` | Done |
| M1 | Lists out of the schemas: format checks instead of IN lists (tenant 00034/00035, platform 00016); override module keys from the registry; console reads `GET /platform/business-types` | `5c2b6ea` | Done, full suite green |
| M4 | Purge of deleted shops `PURGE_AFTER_DAYS` (30) after finalising: files, schema, role dropped; `purged_at` tombstone + audit (platform 00017); `Storage.DeletePrefix` | `fb20852` | Done; targeted tests green |
| H3a | Decimal quantities: every quantity column `numeric(14,3)` (tenant 00036), `products.allowFraction`; Go `qty.Q` fixed-point type through inventory, sales, returns, exchanges, purchases, receipts, counts, adjustments, batches, reports, receipts; part units refused for whole-unit products everywhere; frontend checkout/POS/product forms take part units | (this commit) | Done, full suite green |
| H3b | Units: `product_units` (tenant 00037), `units` on products; sale lines in another unit (`unitId`) with the unit copied on the line | `b3dfe53`; purchases in units `f872c28`; returns, exchanges, held carts, quote `6fe487a` (tenant 00038); PDF receipt and reports show the unit (2026-11-21) | Done: every flow takes units on the server; product forms, checkout, held bills, purchases and linked returns take them in the UI and every line screen, receipt and report shows them. Unlinked returns and exchange new lines too; the old POS stays in base units (decided) |
| H3c | Frontend units (`b003ea3`, `061586b`, `b85e9a1`, slice 4 2026-11-21) and decimal display on the remaining screens | — | Done (decimal display on the remaining screens and the import's allow_fraction column, 2026-11-21). H3 is complete |
| M3 | GSTIN and state per store (falls back to the shop's); CGST+SGST or IGST per sale from the store state and place of supply, copied on the sale; buyer and supplier GSTIN and state | — | Decided (multi-state + B2B); after H3 |
| — | Price per store, several barcodes per variant, scale (weight/price) barcodes | — | Decided; after H3 |

Notes:
- `TestRestoreWritesToNoOtherSchema` was flaky (a backend's late statistics flush); fixed by ending idle backends before the snapshot. `TestPublicShopTenantStatusPlanAndRateLimit` failed once (rate window crossing a minute) and passed in the next full run.
- Orphaned test containers from an interrupted run were removed (2026-11-20).
- `TestEveryProtectedRouteRejectsMissingAndForgedTokens` failed in long runs: the suite's Super Admin token expired. The helper now signs in again after 8 minutes (TOTP steps tracked so no code is reused).

---

# MRP on every variant

2026-11-20, on request. Tenant migration 00032; API_CONTRACT §5.7 (M1–M6).

| Part | What | State |
|---|---|---|
| 1 | `product_variants.mrp` (optional, tax included); product forms, variant matrix and product page show it | Done, committed |
| 2 | Price above MRP: warning only (product forms, product page, cart via the quote, sale lines); never refused | Done, committed |
| 3 | Shop setting "Show MRP on receipts" (off by default): line MRP and "You saved (against MRP)" on the PDF and screen receipt | Done, committed |

| 4 | MRP elsewhere: `mrp` column in the retail/clothing/footwear import templates (warning row when above), label field `mrp`, online shop, old POS screen, product list flag (API_CONTRACT M7–M9) | Done, committed |

Notes: sale lines keep MRP at sale time (`order_items.mrp`, `mrp_amount`); a batch's MRP wins over the variant's. The comparison includes tax (and, at the till, the discount share). The old POS screen compares the unit price with tax (it has no server quote).

---

# Super Admin console MFA switch (temporary)

2026-11-20: MFA for the Super Admin console is **off** on request (`PLATFORM_MFA_DISABLED=true` in `backend/deploy/.env`, git-ignored). Code default stays on. To turn it back on: set it to `false` (or remove the line) and restart the app container. Authenticators already set up are kept and work again as soon as it is on; users who signed in while it was off are asked to enrol at their next sign-in.

---

# Industry Phase 3 (pharmacy) build status (merged)

Merged into `main` on 2026-11-18 (`aea9af7`), with the owner's OK; the `industry/phase3` branch is deleted. What is left: `NEXT_STEPS.md`. Design: `docs/INDUSTRY_PHASE3_PHARMACY_DESIGN.md`, approved 2026-11-18 (all of §14; Q13 refuse, Q14 last, Q15 yes, Q16 0, Q17 later). Last updated: 2026-11-18.

| Item | What | State |
|---|---|---|
| 1 | Batches and expiry: module, batch records, stock per batch, switching, reconciliation | Built (tenant migration 00025, platform 00015, A23–A25, P7/P8). Done; owner OK on the copy-run report 2026-11-18 |
| 2 | Receiving with batch and expiry | Done (tenant migration 00026: DB30 + DB36 settings; A26; labels print batch/expiry); committed and deployed |
| 3 | Selling by batch (FEFO, chosen batch, unknown last with warning, void per batch, edit refused) | Done (tenant migration 00027: DB31; A27); committed and deployed |
| 4 | Returns per batch, quarantine (A28, A29) | Done (tenant migration 00028: DB32, DB33); committed and deployed |
| 5 | Write-offs, recall, trace (A30, A31; P9, P10) | Done (tenant migration 00029: DB34); committed and deployed |
| 6 | Stock counts per batch (A32) | Done (tenant migration 00030: DB35); committed and deployed |
| 7 | Expiry report, dashboard tile, batch settings, daily digest (A33, A34) | Done (tenant migration 00031: outbox kind); committed and deployed |
| 8 | Pharmacy import template (A35) | Done (no migration); committed and deployed |

## Phase 3 notes
- Item 1: every stock change already goes through `inventory.Move`/`Set`/`Apply`; they now keep tracked products' batches in step (increase → unknown batch, decrease → earliest expiry first, unknown last). `ApplyBatched` takes named batches for items 2–8.
- Item 7: report exports are Excel and PDF like the other reports (the design said CSV and Excel).
- Item 8: before this item, products imported into a shop with the module on did not track batches (the import skipped the item 1 default). Imports now turn tracking on in such shops: pharmacy rows go into their batches, other templates' opening stock into the unknown batch. Retail shops are unchanged.
- Item 8: expired rows still create the product (catalog), but their batch and stock are not imported.
- Copy run: the dev database has no shops, so the copy shows only that the migrations apply (platform 15, tenants 25). Switch-on, reconciliation and totals come from a scratch two-store pharmacy shop in `TestBatchTrackingSwitchAndReconcile`.

---

# Industry Phase 1 build status (merged)

Branch `industry/phase1`, merged into `main` on 2026-11-18 (PR #20, merge commit `2ac28f2`; CI green on main). Design: `docs/INDUSTRY_PHASE1_DESIGN.md`.
How to check each item: `docs/UI_CHECKLIST.md`. Last updated: 2026-11-18.

| Item | What | State |
|---|---|---|
| 1 (a) | Business type, module registry, entitlements | Done, committed (`7016f97`), deployed to the dev stack |
| 2 (b) | Payments labelled "recorded" | Done, committed, deployed to the dev stack (tenant migration 00018) |
| 3 (c) | Barcode scanning (checkout, returns, purchases, scanner settings) | Done, committed, deployed to the dev stack |
| 4 (d) | Product attributes, variant matrix | Done, committed, deployed to the dev stack (tenant migration 00019) |
| 5 (e) | Barcode generation and label PDF | Done, committed, deployed to the dev stack (tenant migration 00020). The scan check on a real scanner and phone is for the owner (checklist step 1) |
| 6 (f) | Import templates | Done, committed, deployed to the dev stack (no migration) |
| 7 (g) | Stock counts | Done, committed, deployed to the dev stack (tenant migration 00021) |
| 8 (h) | Partial purchase receiving | Done (migration 00022 + backfill, A16 receipts endpoint, A17 old path, Receive goods screen with scanning and labels, report/dashboard); committed (`a6efa50`) and deployed |
| 9 (i) | Returns policy, exchanges, store credit | Done (migration 00024, A18 policy settings, A19 policy on returns, A20 exchanges, A21/A22 store credit ledger and checkout method, P5/P6 permissions); committed (`2bb68b2`) and deployed |
| 10 (j) | Thermal receipt, HSN on lines | Done (migration 00023, settings receiptPaper, HSN snapshot on sale lines, receipts and PDF, thermal CSS); committed and deployed |

## Notes
- Item 9, DB20: `sales.exchange_return_id` has no foreign key (a key would make a cycle with `sale_returns.sale_id`, which tenant export/restore refuses). Accepted by the owner on 2026-11-18. The column is UNIQUE (that is its index), and `linkExchange` links only a return of the same store and customer, never twice.
- Pushing: from 2026-11-18 commits stay local; push only when the owner asks.
- Test database: `max_connections` raised to 200 in `internal/testdb` (the isolation suite ran out of connections at the default 100 once items 4 and 5 added test shops). Test setup only.
- Dev stack: running; platform migrations at 15, tenant migrations at 31 (Phase 3 items 1-8 deployed). The dev database has no shops (create one in the console to check in the browser).
- govulncheck: clean after the Go 1.27.4 bump (GO-2026-6859 fixed).
- Temporary worktree `D:/pos_baseline` removed.

## Next step
Phase 1: all ten items built and merged (PR #20). Go is now 1.27.4 (GO-2026-6859). `TestLimitWaitsDoNotUseAttempts` flake fixed.

Phase 3 (pharmacy): design note `docs/INDUSTRY_PHASE3_PHARMACY_DESIGN.md` drafted 2026-11-18 with the owner's answers Q8–Q12. Waiting for approval of DB25–DB37, P7–P10, A23–A35, E3 and Q13–Q17 before any code.

## Decisions to confirm
- Item 7: every stock-count route is in the `stock_counts` module (the item 1 route registry), so with the module off the count pages are hidden rather than read-only. Their stock movements stay in the stock history. Design §1 says completed counts stay readable; making the two GET routes readable while the module is off needs a small change to the item 1 route check.
- Item 5: the label PDF is drawn with gofpdf directly (the library maroto wraps) instead of maroto's row layout. maroto starts new pages through gofpdf's automatic page break, and a full label sheet that ends on the bottom margin sometimes added a blank page. Barcodes still come from boombuler/barcode; no new dependency.
