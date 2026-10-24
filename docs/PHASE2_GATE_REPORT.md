# Phase 2 — Gate report after step 16

| | |
|---|---|
| Date | 2026-10-24 |
| Branch | `go-backend/phase2` (local, not pushed) |
| Commits | `c223643` schema · `147e2b2` lifecycle · `879c289` providers · `67f69f6` webhook · `db3a187` jobs and notices · `36f347d` tenant API · `8395ccc` Super Admin API · `03356ff` limit tests · `0da0593` test-mode checklist · `ed52a63` gitleaks allowlist · `046c905` fake IDs · `64acbb0` env example |
| Status | **Stopped after step 16** as asked. Built against the fake provider: no Razorpay keys yet, and the test-mode checks are a checklist (`docs/spikes/RAZORPAY_TEST_MODE.md`). Step 15 report confirmed 2026-10-24 |

## 1. What was built

| Part | Outputs |
|---|---|
| Schema | Platform migration 00005: `invoices` (paise), `billing_events` (one row per verified event ID), `notification_log` (one row per tenant, kind and period), `tenants.suspension_source` (admin or billing) |
| Lifecycle | `billing.Apply`: one table maps each event to the next status. Older events never undo newer ones. Grace once per incident. `halted` after the grace end suspends. Hourly job: an unpaid trial becomes read-only, grace that has run out suspends, a cancelled subscription becomes read-only at period end. Daily reminders at 7, 3 and 1 days |
| Providers | `Provider` interface; a Razorpay client (customer, subscription, fetch, cancel, webhook check) and a fake that sends signed events shaped like Razorpay's. The fake is refused in production |
| Webhook | `POST /api/platform/webhooks/razorpay` on the admin host, outside the IP allowlist: constant-time signature check, 256 KiB cap, 600 a minute per IP, dedupe by event ID, events older than 72 h ignored, every event stored with its outcome. Changes only its own tenant, never lifts an admin suspension, audited as `system:razorpay` |
| Notices | Emails (trial ending, subscription ending, payment failed, grace ending, suspended, reactivated, renewal) to the billing email, else the owner. Sent once each, even with several workers; payment emails carry a pay link |
| Tenant API | `GET /api/billing/subscription` (plan, dates, usage against limits, own invoices, plans you can buy), `POST /api/billing/checkout` (returns the pay link and never activates anything), `GET /api/notifications/banners` |
| Super Admin API | Plans (create, edit, list); per tenant: assign plan, extend trial, grace, cancel (step-up); lists of expiring, past-due and invoices; billing detail per tenant. Every change needs a reason and is audited with before and after |

## 2. Checks

- **Gate commands, all green:** gofmt, `go mod tidy -diff`, vet, `constsql`, golangci-lint (both build modes), govulncheck (no reachable vulnerabilities), gitleaks (56 branch commits), unit and integration tests. **350 test cases pass in the integration build (202 in the unit build), 0 fail, 0 skipped**, fresh run (integration one package at a time, see below).
- **Isolation:** every authenticated tenant route (**97**, was 94) rejects another tenant's token on 2 foreign hosts. A tenant sees only its own subscription and invoices; a webhook changes only the tenant it names; console actions leave another tenant's subscription and tenant row unchanged; tenant tokens get 401 on console billing routes.
- **Webhook:** missing, malformed and wrong signatures, and a body changed after signing: 400, nothing applied. Duplicates: 10 concurrent copies of one event apply once (one audit row). Out-of-order and stale events don't undo newer state. An unknown subscription is stored but changes nothing.
- **Lifecycle:** every move in the transition table has a unit test. Integration covers trial → active → past due → grace → suspended → reactivated, cancel, and the hourly job (unpaid trial, grace run out, period end).
- **Notices:** 4 reminder runs in parallel send each email once.
- **Checkout:** returns the link and leaves the status alone until the webhook activates it, on the plan the provider names. A second checkout after that gets 409. Unknown or hidden plans get 400. A provider failure gets 503 with nothing stored. Billers get 403. A read-only tenant can still sign in and pay, but cannot write.
- **Plan limits:** 8 parallel store creates on a 2-store plan: exactly one succeeds. The same holds for users and products. A downgrade blocks new records and keeps existing ones.
- **Mutation checks:** each of these breaks a test: dropping the event lock, the staleness check, the admin-suspension guard, the plan-limit lock, step-up on cancel, the limit merge, or the live-provider check.
- **Dev stack smoke, HTTPS through Caddy:** platform migrated 4 → 5; tenants at 9; drift matches. Subscription view and banners for `demo-shop`. Checkout returned the fake pay link with the status still trialing. A badly signed webhook got 400 (stored as rejected). The signed `subscription.activated` made the tenant active on the new plan with the period end set. A duplicate got 200 and changed nothing. A second checkout got 409. The demo tenant was then put back as it was. Run again after the ID fix, with the same results.
- **Found while testing:** the fake numbered its IDs from 1 in every process, so a dev checkout after a restart could reuse a stored ID and fail (fixed in `046c905`). gitleaks flagged the isolation suite's webhook test secret; an allowlist entry now covers that one file and value. In two full runs with two packages at a time, the test database of one or two packages (isolation, then platform and tenancy) timed out while starting, because Docker Desktop answered slowly; no test failed. The full integration run one package at a time passed.
- **Not shown:** nothing has talked to Razorpay. CI has never run (nothing pushed). The race detector can't run on this machine (no C compiler).

## 3. Differences from the design (please confirm)

| Topic | Built | Why |
|---|---|---|
| Test-mode checks | Not run. Checklist with setup, steps and "what to change" per outcome (R1–R6 in full, R7–R11 listed) | No keys; you asked for a checklist |
| R1 (halted revival) | The **revivable** branch: the suspended email links the newest unpaid invoice, else the checkout link; `activated` or `charged` reactivates. The not-revivable branch (new subscription) is not built | Waits for R1; about a day of work if needed |
| Sign-in on read-only | Password change, forgot, reset and invite accept now pass the read-only gate (step 5 refused forgot-password there) | An owner who never set or forgot a password must be able to sign in and pay (C-21) |
| Checkout | Refused (409) once a provider subscription is live; refused when suspended (pay from the email); 10 a minute per user; a running trial keeps going and the first charge is at its end | No plan changes yet (R7) |
| Plan changes | None while the provider bills: the console's assign-plan is refused then, since the next event would set the plan back | Needs R7 (`ChangePlan` or cancel plus new subscription) |
| `authenticated` | Never changes the status; read-only 24 h after the trial end if no charge arrives | R4 |
| Grace | 7 days from the first `pending` (or `halted`), once per incident; `payment.failed` alone changes nothing | BILLING_DESIGN §3; R3 may change the length |
| Suspensions | Billing never lifts an admin suspension; admin reactivate clears both | Kept apart by `suspension_source` |
| Plans | Every limit must be set on create (0 = no limit); a PATCH merges limits and features; a provider-linked plan keeps its price and interval; the provider plan ID is set once | A missing limit means unlimited |
| Console actions | Reason 5–500 characters; extend trial 1–90 days (also revives a read-only tenant whose trial ended); grace 1–60 days (lifts a billing suspension); cancel cancels at the provider first | The design gave no ranges |
| Banners | Billing banner only (trial ≤ 7 days, payment failed, ending, read-only); the button only for billing managers; no dismiss endpoint or announcements | Deferred |
| Ordering | Only subscription events move `last_event_at`; invoice and payment events don't | Their timestamps describe other objects |
| Stored payload | The parsed event, not the raw body; bad signatures store no payload | Smaller, and nothing unverified is kept |
| Limit edits | A tenant moved to another plan gets its limits at once; editing a plan's limits takes up to a minute (cache) | Plan cache from step 6 |
| Dates in emails | UTC | Tenant time zones in emails come later |

## 4. Deferred

- Reconcile (a periodic fetch of every live subscription) and an alert when webhooks go quiet. Needed before go-live if R6 shows Razorpay disables failing webhooks.
- Plan changes and proration (R7); clean-up of unpaid `created` provider subscriptions.
- The not-revivable branch, if R1 needs it.
- Pending deletion after `READ_ONLY_DAYS` (comes with step 18 exports).
- Banner dismissal, announcements; console reminders and reconcile buttons.
- The billing page and console screens (step 19).

## 5. Open items for the owner

1. Push and CI still wait on the history rewrite (`20cdb92`; the leaked `backend/.env` is in `63b1c9b` and `9cff503`).
2. Confirm the differences in §3, especially sign-in flows open on read-only and no plan changes while the provider bills.
3. When you have Razorpay test keys: run `docs/spikes/RAZORPAY_TEST_MODE.md` (or give me the keys and I run it). R1 decides whether the not-revivable branch is needed; R6 decides how urgent reconcile is.
4. Next is step 18 (per-tenant export/restore); the design's step list has no step 17. Say "approved" when ready.
5. Dev only: `deploy/.env` (git-ignored) now has a random `RAZORPAY_WEBHOOK_SECRET` so test webhooks can be signed. The dev DB is at platform 5 / tenant 9. A hidden, inactive `smoke_pro` plan and the smoke events stay in it.

---

# Phase 2 — Gate report after step 15

| | |
|---|---|
| Date | 2026-10-24 |
| Branch | `go-backend/phase2` (local, not pushed) |
| Commits | `ba04896` PDF fonts · `01a9fb1` reports · `70c9096` exports · `e445333` step 14 · `50464a6` step 15 · `0476995` full-suite test fixes |
| Status | **Stopped after step 15** as asked. Nothing from step 16 on is built. Step 12 differences and the Excel writer confirmed 2026-10-24 |

## 1. What was built

| Step | Outputs |
|---|---|
| 13 Reports and exports | `GET /api/sales/report` and `/api/purchase/report`: one row per line, old fields kept, newest first, date/store/product/customer/supplier filters. Sales rows are own-only without `sales:view_all`, leave out voided sales and show returns netted per line; `stockQty` is real stock (was 0). `GET /api/sales/report/export` and `/api/purchase/report/export` with `?format=xlsx\|pdf`: `reports:export` and the plan's `reports_export` feature, same scope and filters, totals row, formula-safe text, audited, never truncated |
| 14 Dashboard | `GET /api/dashboard`, same keys (C-16): store, date and user filters, own-only without `sales:view_all`, calendar months in the tenant's time zone ("Mon YYYY", zero-filled, latest 12), top 5 products, payment cards; adds `totalReturns` and `netRevenue` |
| 15 Public shop | `GET /api/public/shop/{slug}` plus `/products`, `/products/{id or slug}`, `/categories`, `/featured` (C-18). No sign-in; tenant from the host, store by slug only; published-in-this-store products only; public allow-list; `limit` ≤ 48 with sorting in SQL; per-IP and per-tenant rate limits; `Cache-Control` + ETag/304 |

## 2. Checks

- **Gate commands, all green:** gofmt, `go mod tidy -diff`, vet, `constsql`, golangci-lint (both build modes), govulncheck (no reachable vulnerabilities), gitleaks (43 branch commits), unit and integration tests. **320 test cases pass in the integration build (184 in the unit build), 0 fail, 0 skipped**, fresh run.
- **Isolation:** every authenticated tenant route (**94**, was 89) rejects another tenant's token on 2 foreign hosts. Reports, exports and the dashboard on tenant C never contain tenant A's rows. The public shop routes take no token; C's showcase slug, product IDs and product slugs give 404 on A's host, A's lists never show C's products, and the two hosts never share an ETag.
- **Reports and exports:** own-only lines for billers (and in a clerk's export), returns netted, voided and cancelled rows left out by default, filters and paging, bad dates 400; the purchase report and its export need `purchases:view`; billers cannot export; a plan without `reports_export` gets `403 FEATURE_NOT_IN_PLAN`; every export writes an audit row. Excel files round-trip through our reader and were also opened with excelize outside the repo (PyPI is unreachable here, so not with openpyxl or Excel).
- **Dashboard:** figures checked against sales dated in 2001 (a year no other test uses): totals, voided left out, returns, paid and unpaid (Partial counted), months zero-filled from the start date's month, top products; a biller's `userId` cannot widen the scope.
- **Public shop:** only published, active products of that store; costs, barcodes, weights and stock counts never appear; discounts and stock status; sorting across the whole result; `%` in search is literal; numeric, unknown and malformed slugs look the same; a disabled showcase shows only its header; suspended tenants and plans without `public_shop` get 404, read-only tenants keep the shop; one IP gets exactly 120 requests a minute, then 429 with `Retry-After`. Mutations (unpublished listed, suspended shown) each fail a test. The first version of the test missed the first mutation, because the hidden product had no showcase row at all; it now has a featured-but-unpublished row.
- **Found in the full run:** tenant A had already used all three stores, so the shop test fixture moved to tenant C. The suite also signs in more than 100 times a minute and hit the per-tenant login limit; the isolation app now raises it through a test-only option (`app.WithTenantLoginLimit`), and `posd` keeps 100 a minute.
- **Dev stack smoke, HTTPS through Caddy:** image rebuilt, no new migrations (tenant 9), drift matches. The sales report, purchase report, Excel and PDF exports (correct headers; the Excel file read back by excelize: ₹85.05 sold, ₹42.00 returned, ₹43.05 net), and the dashboard (the same totals, 12 months, "Oct 2026") all agree. After publishing Smoke Tea, the public shop served it without a token, with `Cache-Control: public, max-age=60`, an ETag and 304 on a conditional request; a numeric store ID got 404.
- **Not shown:** CI has never run (nothing pushed). The race detector can't run on this machine (no C compiler).

## 3. Differences from the design (please confirm)

| Topic | Built | Why |
|---|---|---|
| Export endpoints | `GET /api/sales/report/export` and `/api/purchase/report/export`, `?format=xlsx\|pdf` | The design named Excel/PDF export but no routes |
| Export size | Refused (400, asks for a shorter range) above 50,000 rows for Excel or 5,000 for PDF | An export never silently drops rows |
| Purchase report | Also needs `purchases:view` (report and export) | Billers have `reports:view` by default; supplier costs stay hidden from them |
| Report rows | Old fields kept; `sku` is still the old `SO-…`/`PO-…` label, the product SKU is in `itemSku`; `stockQty` is current stock; adds `returnedQty`, `returnedAmount`, `netAmount`, customer, store, status | Compatible with today's pages; the customer report page can use `customerName` instead of the brand |
| Report defaults | Voided sales and cancelled purchases left out unless `includeVoided` / `includeCancelled` | C-11 for sales; cancelled orders are not purchases |
| Returns without a sale | Not in the sales report (no sold line); counted in the dashboard's `totalReturns` | They refund money without a sale to attach to |
| Dashboard payments | `totalPaid` sums every payment (was only fully paid sales); `totalUnpaid` includes Partial | C-16 called the old figures wrong |
| Dashboard counts | Customer and product counts are master data, not filtered by user, store or date | Billers already see both lists |
| Dashboard extras | `totalReturns`, `netRevenue` and the filters used are added | Same keys otherwise |
| Shop visibility | Suspended and pending-deletion tenants, and plans without `public_shop`, show no shop (404); read-only and grace tenants keep it | BILLING_DESIGN §3 status rules |
| Shop data | No store ID, exact stock counts, barcodes or weights (old fields `qty`, `totalQty`, `itemBarcode`, `weight` dropped); `inStock`/`stockStatus` kept | THREAT_MODEL §3.10; `PublicShopPage.jsx` does not use them |
| Shop prices | `price` is the first live variant's price, `finalPrice` applies the product discount rounded to the currency; products without a live variant are not shown | As before, sorting now in SQL |
| Shop rate limits | 120 requests a minute per IP, 3,000 per tenant | One page view makes about 4 requests; the design gave no numbers |
| Shop caching | `Cache-Control: public, max-age=60` and a strong ETag (304) on every success | CDN-friendly, short enough for stock changes |
| Featured and categories | Featured needs published; categories only with published products in that store | C-18 |

## 4. Deferred

- Frontend (step 19): `PublicShopPage.jsx` relative URLs (C-18); report pages may switch to the server exports; customer report from `customerName`; dashboard month labels now include the year.
- Server exports of the customer and customer-due reports (the pages build them from other endpoints).
- A CDN in front of the public shop.

## 5. Open items for the owner

1. Push and CI still wait on the history rewrite (`20cdb92`; the leaked `backend/.env` is in `63b1c9b` and `9cff503`).
2. Confirm the differences in §3, especially the purchase report needing `purchases:view` and the shop rate limits.
3. Step 16 (billing) comes next and starts with the Razorpay test-mode checks (BILLING_DESIGN §10, R1–R6), which need Razorpay test-mode keys from you. Say "approved" with those when ready.
4. Dev only: Smoke Tea is now published in the demo shop's Main Store, so `https://demo-shop.localhost:8443/api/public/shop/main` shows it. The dev DB is still at tenant version 9.

---

# Phase 2 — Gate report after step 12

| | |
|---|---|
| Date | 2026-10-24 |
| Branch | `go-backend/phase2` (local, not pushed) |
| Commits | `9eb438f` shared helpers · `ace0d45` purchases schema · `4179671` step 11 · `b2a9afb` adjustments schema · `20ffb86` step 12 · `b3652c9` test SKUs |
| Status | **Stopped after step 12** as asked. Nothing from step 13 on is built. Step 10 differences confirmed 2026-10-23 |

## 1. What was built

| Step | Outputs |
|---|---|
| Shared helpers | Idempotency-Key handling, stock row locking and document dates moved out of sales into `idempotency`, `inventory` and `docdate`, unchanged, so purchases and adjustments use the same code |
| 11 Purchases | Tenant migration 00008. `GET/POST /api/purchases`, `GET/PUT /api/purchases/{id}`. Every line stores its variant and store (C-09). Stock goes in once, when the purchase becomes Received, to each line's own variant and store, through the ledger, under the purchase's row lock. Receiving needs `purchases:approve`. Received and Cancelled are final (`409 INVALID_STATE`); payment and note stay editable. Totals computed here; reference unique per supplier, else a gapless `PUR-000001`; Idempotency-Key; audited (create, update, receive) |
| 12 Stock adjustments | Tenant migration 00009 (append-only). `GET/POST /api/stock-adjustments`. The inventory row is locked; a subtraction below zero gets `409 INSUFFICIENT_STOCK`; the record, the stock change and the ledger row commit together; the stock left is recorded. Idempotency-Key; audited |

## 2. Checks

- **Gate commands, all green:** gofmt, `go mod tidy -diff`, vet, `constsql`, golangci-lint (both build modes), govulncheck (no reachable vulnerabilities), gitleaks (36 branch commits), unit and integration tests. **300 test cases pass in the integration build (175 in the unit build), 0 fail, 0 skipped**, fresh run.
- **Isolation:** every authenticated tenant route (**89**, was 83) rejects another tenant's token on 2 foreign hosts. On tenant C, tenant A's purchase ID gives 404 for read and receive (A's purchase stays Pending, A's stock unchanged); A's supplier and variant IDs are refused (400); C's lists never show A's purchases or adjustments. A's IDs were moved to ranges C does not have.
- **Purchases:** lines stock their own variant and store, not "the first variant in store 1"; `storeId` in the PUT body is ignored; received again changes nothing, back to Pending is refused; **8 concurrent receives stock once** (one `purchase.receive` audit row); approve needed at creation and later; 14 validation cases; Cancelled final; Overdue kept while due. Mutations (no row lock, Received not final, no approve check) each fail a test.
- **Adjustments:** ledger and audit rows with every change; a refused subtraction records nothing; **12 concurrent subtractions of 5 units: exactly 5 succeed**, recording 4, 3, 2, 1, 0 left; 11 validation cases; the tenant role cannot update or delete adjustments.
- **Dev stack smoke, HTTPS through Caddy:** `demo-shop` migrated from 7 to 9; drift matches. A purchase in the exact `AddPurchase.jsx` shape (`STW-1001`, 24 × ₹30) was created Pending (no stock change) and received by PUT: stock 1 → 25, paid ₹300 of ₹720 shown as Partial although the client sent "Paid". Back to Pending and a duplicate reference both got 409. An adjustment of −3 left 22; −500 got `409 INSUFFICIENT_STOCK`; the ledger matches stock.
- **Found while testing:** a new test reused SKUs from the products test in the same tenant and failed only in a full run; fixed in `b3652c9`.
- **Not shown:** CI has never run (nothing pushed). The race detector can't run on this machine (no C compiler).

## 3. Differences from the design (please confirm)

| Topic | Built | Why |
|---|---|---|
| Purchase status | Only `Ordered`, `Pending`, `Received`, `Cancelled` (was free text); missing means `Pending` | Design DDL; no stock-in without an explicit Received |
| Cancelled | Final, like Received | A cancelled order should not come back to life; enter a new one |
| Reference | Unique per supplier (`409`); empty gets a gapless `PUR-000001` | The same supplier invoice cannot be stocked twice; the form's field is optional |
| `paid` on purchases | Above the total is refused (400), not capped as on sales; missing means 0 | Purchases are typed by hand; the old code also defaulted to 0 |
| Payment status | Paid/Partial/Unpaid follow from `paid`; `Overdue` is kept only while something is due | Totals recomputed (C-09) |
| Amounts | Optional per-line discount (up to the line amount); no purchase-level discount and no tax on purchases | The form sends neither; input tax credit is out of scope |
| Checks | Line stores must be active; supplier and variants must not be deleted (inactive products can be bought) | Same rules as sales |
| After creation | Status, payment and note can change; lines and totals cannot | PRD §3.10 |
| New route | `GET /api/purchases/{id}` | Detail view |
| Purchase response | Adds the capitalised `Supplier` and `paymentStatus` | `Purchases.jsx` reads `Supplier`, so today it shows "Unknown Supplier" |
| Adjustment store | Must exist; an inactive store is allowed | Counting stock in a closed store is still a correction |
| Adjustment records | Append-only; store the stock left and the product | Corrections are new adjustments |
| Adjustment Idempotency-Key | Supported (S2 named sales, returns and purchases) | A double click would adjust twice |

## 4. Deferred

- Purchase report (step 13).
- Editing a purchase's lines before it is received; deleting purchases (no endpoint, as before); returns to suppliers; stock transfers between stores.
- Frontend (step 19): stock adjustment screen (PRD G13); hide Received without `purchases:approve` (C-09).

## 5. Open items for the owner

1. Push and CI still wait on the history rewrite (`20cdb92`; the leaked `backend/.env` is in `63b1c9b` and `9cff503`).
2. Confirm the differences in §3, especially Cancelled being final and `paid` above the total being refused.
3. Say "approved" for step 13 (reports and exports) when ready.
4. Dev only: the demo shop now also has supplier "Smoke Tea Wholesale", purchase `STW-1001` (received, 24 units, ₹300 paid) and one −3 adjustment; the dev DB is at tenant version 9.

---

# Phase 2 — Gate report after step 10

| | |
|---|---|
| Date | 2026-10-23 |
| Branch | `go-backend/phase2` (local, not pushed) |
| Commits | `d926e83` held-mail fix · `7f63b17` returns schema · `f7b5d87` refund shares · `3bef53b` step 10 returns |
| Status | **Stopped after step 10** as asked. Nothing from step 11 on is built. Step 9 differences confirmed 2026-10-23 |

## 1. What was built

| Area | Outputs |
|---|---|
| Held mail (step 9 follow-up) | A wait for a daily limit (the plan's platform limit, or the tenant mailbox cap with fallback off) reschedules the row and gives back its attempt, so held mail never goes dead while it waits. A tenant mailbox that is not ready still uses attempts |
| Schema | Tenant migration 00007: `sale_returns`, `return_items` (link to the sold line, snapshots, sold unit price, discount/tax/CGST/SGST/IGST shares, refund), a `sale_return` counter. `return_items` is append-only for the tenant role |
| Returns | `POST /api/sales/returns`, `GET /api/sales/returns` (bare array unless paged, `?saleId=`). Linked returns lock the sale row, then check each sold line cumulatively; `409 RETURN_EXCEEDS_SOLD` with sold/returned/requested. Quantity > 0; store and customer from the sale; refund = the sold amount, client prices ignored; voided sales refused. Returns without a sale need `sales:price_override`. Gapless `RET-000001`; stock back through the ledger; audited; optional Idempotency-Key |
| Sales | A sale with returns can no longer be edited or voided (`409 INVALID_STATE`); sale lines show `returnedQty` |

## 2. Checks

- **Gate commands, all green:** gofmt, `go mod tidy -diff`, vet, `constsql`, golangci-lint (both build modes), govulncheck (no reachable vulnerabilities), gitleaks (29 branch commits), unit and integration tests. **292 test cases pass in the integration build (175 in the unit build), 0 fail, 0 skipped**, fresh run.
- **Isolation:** every authenticated tenant route (**83**, was 81) rejects another tenant's token on 2 foreign hosts. On tenant C, tenant A's sale ID, sold line ID and variant ID are refused (400) and A's stock and returns are untouched; C's list never shows A's returns. A's IDs were moved to a range C does not have, so the check is not a coincidence.
- **Returns rules:** a line returned in parts (1 then 2 of 3, with a sale discount and 18 % GST) refunds exactly the line amount, tax and CGST/SGST included; a 4th unit gets 409; negative, zero and fractional quantities, a product not on the sale, another store or customer, another sale's line, an unknown or voided sale are refused; a clerk without `sales:view_all` returns only own sales, and a return without a sale needs the price override. **12 concurrent returns of 5 sold units: exactly 5 succeed**, with consecutive numbers; the ledger reconciles after each test.
- **Mutation check:** dropping the cumulative sum, the sale row lock, or the edit/void refusal each makes a test fail (without the lock, all 12 concurrent returns went through).
- **Held mail:** both limits are held three times over with 0 attempts used, then the mail goes out with 1 attempt; the test fails on the old code.
- **Dev stack smoke, HTTPS through Caddy:** `migrate tenants up` moved `demo-shop` from 6 to 7; drift matches. A return in the exact shape `AddSalesReturn.jsx` posts (string IDs, `RET482913`, unit price 999) became `RET-000001` for ₹42.00 (₹40 + CGST ₹1 + SGST ₹1); stock went 0 → 1; a second return got `409 RETURN_EXCEEDS_SOLD`, editing the sale `409`; the list has `customer.name`, `product.name` and `paymentStatus`; the ledger matches stock.
- **Not shown:** CI has never run (nothing pushed). The race detector can't run on this machine (no C compiler).

## 3. Differences from the design (please confirm)

| Topic | Built | Why |
|---|---|---|
| Edit or void after a return | Refused (`409 INVALID_STATE`) | An edit replaces the lines returns point to; a void would restock goods that already came back |
| Refund amount | The line's share of what was charged: after its share of the sale discount, with tax, split into CGST/SGST/IGST. Cut on the running quantity, so parts add up exactly. Shipping is not refunded | "Sold price" (C-13) made precise for discounts and GST credit notes |
| Partly paid sales | A return refunds the full line share; the sale's own paid/due do not change | Settling a refund against an unpaid balance is not designed yet |
| Status | `Pending`/`Completed` stored as sent (default `Pending`, as before); stock goes back at once either way; no way to change it later | Unchanged behaviour (PRD §3.6) |
| Reference | Server issues `RET-000001` from its own gapless counter; client `reference` ignored; prefix fixed | Same rule as sale numbers |
| Matching lines | By `productId`/`variantId`, as the form sends; a product sold in several variants needs `variantId`; the same variant on two lines is taken from them in order. New optional `orderItemId` names the line exactly | The form does not send line IDs |
| Own-only | Without `sales:view_all`, only own sales can be returned ("Sale not found"). The returns list itself is not own-only (`sales_returns:view`, unchanged) | C-10 for the sale; list contract unchanged |
| Without a sale | Client unit price × quantity, no tax split out; store must be active; customer defaults to walk-in | C-13 requires the price override |
| Deleted products | A sold line of a since-deleted product can still be returned; the goods go back to that variant's stock | Same as void |
| `paid` | Missing = refunded in full; `0` kept; above the total capped; payment status computed | Same as sales (C-04); the old code turned `0` into the total |
| List response | Old flat fields plus nested `customer`/`product`, `paymentStatus`, `tax`, `currency`, `saleReference`, `storeName`, per-line tax shares; images are signed-in URLs | `SalesReturnList.jsx` reads the nested fields (it fails on today's backend) |
| New error code | `409 RETURN_EXCEEDS_SOLD` with details (added to API_CONTRACT §1) | The form needs to show what is left |

## 4. Deferred

- Return receipt / credit note PDF and email.
- Changing a return's status or refund payment after creation (no `PUT`).
- Reports (step 13) and the dashboard (step 14) must net returns out of sales.
- Frontend (step 19): `AddSalesReturn.jsx` should send `orderItemId`, show what is left (`returnedQty`), handle `409 RETURN_EXCEEDS_SOLD`, and stop sending a reference and unit prices for linked returns.

## 5. Open items for the owner

1. Push and CI still wait on the history rewrite (`20cdb92`; the leaked `backend/.env` is in `63b1c9b` and `9cff503`).
2. Confirm the differences in §3, especially refusing edit/void after a return and the partly-paid case.
3. Say "approved" for step 11 (purchases) when ready.
4. Dev only: the demo shop now also has sale `POS-INV-000005` and return `RET-000001`; the dev DB is at tenant version 7.

---

# Phase 2 — Gate report after step 9

| | |
|---|---|
| Date | 2026-10-23 |
| Branch | `go-backend/phase2` (local, not pushed) |
| Commits | `dd3ca30` mailer · `99833d4` settings + SMTP · `ae8b1b7` receipts · `12c096c` templates · `bb0aea9` worker · `3235328` Go 1.27.3 · `098fdac` gitleaks config |
| Status | **Stopped after step 9** as asked. Nothing from step 10 on is built |

## 1. What was built

| Area | Outputs |
|---|---|
| Mailer | go-mail v0.8.1. Control characters refused in every address, name, subject and header; allow-listed headers. SSRF guard: resolve once, refuse internal ranges (incl. IPv4-mapped and `SMTP_DENY_CIDRS`), dial the checked IP, TLS still checks the host name. Tenant ports 465/587 only, TLS 1.2+. Classified errors |
| Settings | Tenant migration 00005. `GET/PUT /api/settings` (business profile, currency, number format, time zone, tax, discount cap, invoice prefix, receipt header/footer). `GET/PUT/DELETE /api/settings/email`: password write-only, envelope-encrypted, bound to the tenant ID. `POST /api/settings/email/test` |
| Receipts | Tenant migration 00006. One receipt per sale version; A4 PDF (maroto v2.4.3, Noto Sans) with CGST/SGST or IGST summary and a VOID banner. `GET /api/sales/{id}/receipt` (renders on demand, 5 s cap), `POST …/receipt/resend` (202, 5 per address per day). Sale responses carry `receiptUrl` |
| Templates | English-only receipt, password reset, staff invite and welcome emails, HTML and text. Tenant text is data, never template source |
| Worker | River v0.49.0 in schema `river` (platform migration 00004). Per-tenant outbox delivery (lease, `SKIP LOCKED`, `leased_by` guard, no SMTP inside a transaction); tenant SMTP with fallback to "<Business> via <Platform>"; backoff and dead letter; platform outbox; receipt pre-render. Nightly ledger check, POS lock sweep every 5 min, idempotency key cleanup hourly. `/readyz` reports `jobs` |

## 2. Checks

- **Gate commands, all green on Go 1.27.3:** gofmt, `go mod tidy -diff`, vet, `constsql`, golangci-lint (both build modes), govulncheck, gitleaks (branch commits), unit and integration tests. **280 test cases pass in the integration build (171 in the unit build), 0 fail, 0 skipped**, fresh run (`-count=1`). govulncheck still lists the unreachable `x/crypto/openpgp` advisory from step 8.
- **Isolation:** every authenticated tenant route (**81**, was 73) rejects another tenant's token on 2 foreign hosts. Another tenant's or another biller's receipt gives 404 for download and resend. An SMTP password ciphertext copied into another tenant does not decrypt.
- **SMTP safety:** hosts that resolve to cloud metadata, private, IPv4-mapped loopback or mixed public/private addresses are refused before connecting. CR/LF in recipients, names and subjects is refused. Test-send limits return 429.
- **Delivery:** end to end through an in-process STARTTLS tenant server and a platform relay: PDF attached, From/Reply-To, fallback on 535, dead letter on 550, retry on 451, working reset and invite links, welcome emails, leases respected, an expired lease recovered.
- **Dev stack smoke, HTTPS through Caddy:** image rebuilt on the Go 1.27.3 base. `migrate platform up` added River's tables (platform version 4); `migrate tenants up` moved `demo-shop` from 4 to 6; drift matches. `/readyz` said `jobs: fail` before the migration and `ready` after. A POS sale with a receipt email returned `receiptUrl`; the PDF downloaded (₹, CGST/SGST, India time; 401 without a token) and the email reached Mailpit from "Demo Shop via POS Dev" with the PDF. A resend to a new address followed at once by a void gave a version 2 PDF with the VOID banner. The resend's first attempt saw the newer version and retried; about 80 s later it went out with the VOID PDF.
- **Not shown:** CI has never run (nothing pushed). The race detector can't run on this machine (no C compiler). Devanagari text was not tested (see §3).

## 3. Differences from the design (please confirm)

| Topic | Built | Why |
|---|---|---|
| PDF spike, fonts | No spike; Noto Sans only. **Names typed in Devanagari will not render correctly in PDFs** | Your 2026-10-20 decision (English only). Later fix: a Devanagari font plus shaping, or Gotenberg (§5.6) |
| When tenant SMTP is used | Only with status `ok`, set by a passing test send. Saving changed settings resets it to `unverified`; an auth failure sets `failing` until the next passing test | One clear rule |
| Tenant daily cap reached | The rest goes through the platform if fallback is on (within the plan's daily email limit), otherwise it waits | Receipts still go out |
| **Mail held by a limit** | Held mail (plan limit reached, or fallback off and tenant SMTP not usable) retries hourly and **each retry uses one of the 8 attempts**, so it goes dead after about 8 hours. Limits are a rolling 24 h, so after a burst, held receipts can die before room frees up | Not intended. Suggested fix: waits for a limit don't count as attempts. Say if you want it |
| Test-send limits | 20/hour in total, 5/hour to other addresses | Design limited only other addresses |
| Per-tenant burst (30/min) | Not built; daily caps only. Pace is bounded by one job per tenant and 20 rows per batch | Enough at current volume |
| SMTP auth | PLAIN only (no LOGIN) | Gmail, Workspace, SES and Brevo accept PLAIN |
| From alias | Not built: when the username is an address, From must equal it | Alias verification deferred |
| Receipt pre-render | River job queued right after commit; no sweep of old `pending` receipts. A receipt whose job was lost renders on first download or email | On-demand rendering covers it |
| Receipt email without PDF | After 3 failed renders it says "ask the store for a copy", no download link | Download needs sign-in; a customer link needs a signed token (not built) |
| Receipt email content | Always the sale's current version at send time | An edit or void before sending changes what the customer should get |
| Message-ID | `<tenant>.<idempotency key>@MAIL_DOMAIN` | Keys like `sale:4:receipt` repeat across tenants |
| River tables | Own schema `river`; `pos_jobs` gets DML only; River's migrations run as `pos_owner` in `migrate platform up` | Keeps River out of `platform` and tenant schemas |
| Ledger check | Daily at 21:00 UTC (02:30 India) | Quiet hours |
| Dev mail | `SMTP_TLS=none` for Mailpit in dev compose; production still requires TLS | Mailpit has no TLS |

## 4. Deferred

- Dead-letter list and manual retry (Settings ▸ Email ▸ failed messages): no endpoint yet. Status and last error are in `GET /api/settings/email`.
- Platform billing emails (trial/subscription ending, payment failed, grace, suspended, reactivated) → step 16. No email verification flow or announcements yet.
- Marketing mail, consent and `List-Unsubscribe`: no marketing mail exists yet.
- Two-worker crash-point duplicate test: Phase 3, as planned.
- Frontend (step 19): receipt email field in POS, download/resend buttons, settings pages, SMTP status display.

## 5. Fixed during this gate

- **Go 1.27.2 → 1.27.3** (`3235328`): govulncheck found GO-2026-6809 (crypto/x509 trusted compromised intermediate certificates), reachable from TLS certificate checks including the tenant SMTP client. `go.mod` toolchain, Dockerfile base (pinned by digest), LIBRARIES.md and README updated, per A7 (follow patch releases).
- **gitleaks** (`098fdac`): the branch scan failed on two test `Idempotency-Key` values in `sales_test.go`, already there at step 8 (gitleaks was not in the step 8 check list). `.gitleaks.toml` allows only that file and value pattern. The default rules also **do not detect the values in the leaked `backend/.env`**, so `make gitleaks-history` had been failing only because of those test values. A new rule flags any committed backend `.env` file, so the full-history scan fails on `backend/.env` until the rewrite, as the README says.

## 6. Open items for the owner

1. Push and CI still wait on the history rewrite (`20cdb92`; the leaked `backend/.env` is in `63b1c9b` and `9cff503`).
2. Confirm the differences in §3, especially Devanagari names in PDFs and the held-mail attempts.
3. Say "approved" for step 10 (returns) when ready.
4. Dev only: the demo shop now also has sale `POS-INV-000004` (voided), and Mailpit holds 3 emails. The dev DB is at platform 4 / tenant 6. The step 8 `owner@demo.test` dev password is unchanged.

---

# Phase 2 — Gate report after step 8

| | |
|---|---|
| Date | 2026-10-20 |
| Branch | `go-backend/phase2` (local, not pushed) |
| Commits | `16d1e9d` files/images/plan limits · `6dd8030` step 6 · `dc7fbae` money + ledger · `ba8bab4` step 7 · `1ca0ad3` pricing · `656aac0` step 8 |
| Status | **Stopped after step 8** as asked. Nothing from step 9 on is built |

## 1. What was built

| Step | Outputs |
|---|---|
| 6 Master data | Tenant migration 00002; stores (+ showcase admin), categories, sub-categories, brands, units, suppliers, customers, warehouses; files: FS (`os.Root`) and S3 drivers, image pipeline (sniff, size/pixel caps, re-encode), upload/attach/serve; plan limits (stores, users, products, storage) |
| 7 Products/inventory | Tenant migration 00003; products + variants (unique SKU/barcode), per-store publication, POS list, barcode lookup, atomic POS locks, inventory set, append-only stock ledger (S1) with reconcile, CSV import (dry run, all or nothing) |
| 8 Sales/POS | Tenant migration 00004; pricing engine (pro-rata discount before tax, inclusive/exclusive tax, CGST/SGST/IGST, half-up per line, currency exponents); checkout with ordered row locks; `409 PRICE_CHANGED` / `INSUFFICIENT_STOCK` with details; price override and discount cap by permission; POS aliases and ignore list; gapless references from `invoice_prefix`; Idempotency-Key (S2); edit keeps the biller; void restocks; own-only sales and invoices (C-10) |

## 2. Checks

- **Gate commands, all green:** gofmt, `go mod tidy -diff`, vet, `constsql`, golangci-lint (both build modes), `make lint`, govulncheck, unit and integration tests. **244 test cases pass, 0 fail, 0 skipped.** govulncheck reports one advisory in `golang.org/x/crypto/openpgp` (no fix exists); our code doesn't call it.
- **Isolation:** every authenticated tenant route (**73**) rejects another tenant's token on 2 foreign hosts. Each new module has ID-based cross-tenant tests (read, edit, delete and use of another tenant's IDs give 404/400 and leave the data untouched).
- **Stock safety:** a 20-way race on 5 units sells exactly 5, and their references are consecutive. 24 concurrent carts with opposite line order produce no deadlock. 6 concurrent submits with one Idempotency-Key create one sale. The ledger reconciles after every stock test.
- **Dev stack smoke:** `migrate tenants up` moved `demo-shop` from version 2 to 4, and drift reports a match. Over HTTPS through Caddy: login, product, POS sale (server totals, GST split), price override (audited), oversell 409, void and restock all worked.
- **Not shown:** CI has never run (nothing pushed). The race detector can't run on this machine (no C compiler).

## 3. Differences from the design (please confirm)

| Topic | Built | Why |
|---|---|---|
| Old-form extras | `createdBy` (all forms) and the showcase form's name/address are accepted and ignored | Strict decoding without breaking today's frontend |
| Image purposes | Added category/brand/supplier images; private (served from `/api/files/…`) | The old forms upload them |
| Product images | Public URL only while published in some store; otherwise `/api/files/…` (signed-in) | No public access to unpublished products |
| Product delete | Refused (`409 INVALID_STATE`) while any stock remains | Stock never disappears silently |
| Product edit | `productType` cannot change | Avoids orphaning variants and stock |
| Plan limits | Users, stores, products and storage are enforced now (step 16 adds billing) | The limits table already exists; the isolation suite raises the trial `max_users` |
| Sale reference | Client `reference` ignored; the server issues `<invoice_prefix>-000001` from a gapless counter | GST needs consecutive numbers. The counter row serialises the end of each checkout per tenant (a few ms) |
| `paid` | Missing means paid in full (POS sends none); `0` is kept; above the total it is capped | POS compatibility, C-04 |
| Line discount | A non-zero line `discount` is refused (400), not ignored | Silently dropping a discount would change the charged price |
| Missing `variantId` | Allowed for single-variant products | `AddSales.jsx` sends only `productId` |
| Sale edit | A price already on the sale is not an override, even if the catalog changed since | Editing old sales must not need override rights |
| Sales filters | `startDate`/`endDate` filter the sale `date` as whole days in the tenant's time zone (end inclusive) | `createdAt` and UTC midnight cut off the last day |
| Void | Optional `?reason=`; voiding twice returns 204 | Idempotent DELETE |
| Pending sales | Still take stock, as before | Unchanged behaviour |
| Receipts | `receiptEmail` is accepted and stored; `receiptUrl` arrives with step 9 | Receipts are step 9 |
| Error body | Optional `details` (current prices, stock shortfalls) | The `409` responses need data |
| Idempotency keys | Expire after 24 h when the key is reused; no cleanup job yet | No worker until step 9 |

## 4. Deferred

- No worker yet (step 9), so these jobs don't run: nightly ledger reconcile (`inventory.Reconcile` exists), the expired POS lock sweep (expired locks are ignored, not deleted) and idempotency key cleanup.
- `impersonation_id` on sales and stock movements stays empty until impersonation is built.
- A sale that contains a since-deleted product can be voided but not edited.
- Frontend changes (step 19): `AddSales.jsx` must send `storeId`; POS should handle `409 PRICE_CHANGED` and send an Idempotency-Key.

## 5. Bugs found and fixed while building

`FOR UPDATE` inside read-only transactions; pgx rows not scanned; a `citext` parameter compared as text; `decimal.Decimal` used as a map key (it never matched); `[]byte` sent to a `jsonb` column; a planned ledger `UPDATE` that the append-only grant would have refused (caught in review, before it ran).

## 6. Open items for the owner

1. Push and CI still wait on the history rewrite (`20cdb92`).
2. Confirm the differences in §3.
3. Say "approved" for step 9 (receipts and email, PDF spike first) when ready.
4. Dev only: for the smoke test I set the `owner@demo.test` password on the local stack to `smoke-test-pass-2026`. The demo shop also now has a "Smoke Tea" product and 3 sales (one void).

---

# Phase 2 — Gate report after step 5

| | |
|---|---|
| Date | 2026-10-19 |
| Branch | `go-backend/phase2` (local, not pushed) |
| Commits | `9dc196a` .gitattributes · `cf75fb2` step 1 · `645c81d` step 2 · `0ef07b2` step 3 · `f85ffe2` step 4 · `f4ccf20` step 5 |
| Size | 98 files, ~16.8k lines in `backend-go/` |
| Status | **Stopped for review** (PHASE1_DESIGN §8). Nothing from step 6 on is built |

## 1. What was built

| Step | Outputs |
|---|---|
| 1 Foundation | Go 1.27.2 module, `posd` + `posctl`; env-only config that fails fast on missing, weak, placeholder or leaked secrets (SHA-256 deny list, values never in code); redacting slog; typed errors → `{error, code, fields}`; host classifier (tenant/admin/apex, else 421) with trusted-proxy handling; request IDs, access log, metrics, recover, secure headers incl. strict admin CSP, body caps, timeouts, strict JSON, dev-only CORS; `/healthz`, `/readyz`; graceful shutdown; `constsql` analyzer; distroless Dockerfile; Makefile; golangci-lint; CI + Dependabot; dev compose (Postgres 18.6, Caddy, S3 gateway, Mailpit). `frontend/.env` untracked (O-8) |
| 2 Platform schema + registry | `posctl db bootstrap` (roles, schemas, extensions; SCRAM verifiers, never clear passwords); goose platform track; `InTenantTx`/`InPlatformTx` (SET LOCAL role + `set_config(…, true)`, tamper tripwire, distinct Go types); pool destroys connections returned inside a transaction; tenant registry with negative cache; dedicated LISTEN connection (flush on connect and disconnect, degraded TTL); `posctl db preflight` (M1–M14) |
| 3 Runner + provisioning | Tenant migration 00001; per-tenant advisory lock, parallel runner, dry run, resume, drift fingerprint; post-step (grants, tenant-bound `mark_outbox_pending()`, permission sync); provisioning steps 1–10 with compensation, retry, reaper |
| 4 Super Admin | Platform users (argon2id), mandatory TOTP (RFC 6238 vectors, replay guard), recovery codes, rotating `__Host-prt` with reuse detection, step-up, lockout, tenant admin (list/get/create/patch/slug/suspend/reactivate/delete/restore/retry), append-only audit, `posctl bootstrap-superadmin`, old-slug 308 redirect |
| 5 Tenant auth + RBAC | Login/lockout/rate limits, rotating `__Host-rt`, logout, me, password change/forgot/reset, invite accept; auth state + permissions per request (cached, NOTIFY-invalidated); tenant status gate; users + roles with anti-escalation (C-07); `/api/public/tenant-info`; tenant audit log; route self-check; owner invite link from the console |

## 2. Exit criteria

| # | Criterion | Result |
|---|---|---|
| 1 | CI green on Linux runners | **Not yet shown.** The workflow exists but has never run, because nothing is pushed. Locally every gate is green: gofmt, `go mod tidy`, `go vet`, `constsql`, golangci-lint (incl. gosec, staticcheck, errcheck, revive) in both build modes, govulncheck (no vulnerabilities), unit and integration tests. Race detector **not run locally** (no C compiler on this machine); CI runs it |
| 2 | search_path suite | **Pass.** TENANCY §5.1 (pool 1 and 2, concurrent tenants, released connections, error/panic/cancel/timeout), §5.2 (version skew on one connection; the documented `CacheStatement` failure), §5.4 tests 1–5, §8 (`NO TRANSACTION` index and reconnect stay in the tenant schema) |
| 3 | Tenant-isolation suite (3+ tenants) | **Pass.** Every authenticated tenant route (17) rejects another tenant's token on 2 foreign hosts; privilege matrix across 3 tenant roles; no tenant role sees `platform` or another schema; forged `X-Forwarded-Host`; identical 404 for unknown/deleted/provisioning hosts incl. `/api/public/tenant-info` |
| 4 | Provisioning fault injection | **Pass.** A failure injected after each of steps 2–8 leaves no orphan schema, role or subscription and nothing in `provisioning`; retry then succeeds with the original options |
| 5 | This report | Done |

**Tests:** 66 unit-test functions and 61 integration-test functions. A full run executes 197 test cases including subtests: 197 pass, 0 skipped. Integration tests run on testcontainers PostgreSQL 18.6.

## 3. Differences from the design (please confirm)

| Topic | Design | Built | Why |
|---|---|---|---|
| Dev S3 | MinIO | Versity S3 Gateway (Apache-2.0, v1.9.0) | MinIO's images are no longer publicly pullable. The app talks plain S3, so the code doesn't change |
| Dev ports | 443/80/5432/8025 | 8443 / 55432 / 8026 (`HTTPS_PORT`, `POSTGRES_HOST_PORT`) | Those ports are taken on this machine |
| `constsql` | Runs inside golangci-lint as a module plugin | Runs as `go vet -vettool` (`make constsql`, CI step) | Simpler; same analyzer. It exempts `_test.go`, `internal/testdb/` and pass-through wrappers |
| gosec | Standalone binary | Through golangci-lint | gosec v2.29.0 can't read Go 1.27 export data |
| New env vars | — | `DATABASE_ADMIN_URL` (bootstrap/preflight only), `HSTS_ENABLED`, `REQUEST_TIMEOUT`, `SHUTDOWN_*` | Needed by the design's own steps |
| Production DB passwords | Not specified | DSN passwords must be ≥ 16 chars in production | Also covers the old weak password, whose hash is deliberately not in code |
| Platform roles | `platform_roles` + `platform_user_roles` tables | One `role` column on `platform_users` | One role per staff member is enough for the §7.1 matrix |
| Owner invite | Token created in provisioning step 7 | Created when sent: by the console's `owner-invite` action (link shown once) or the email worker | Avoids storing a raw token anywhere |
| Reset/invite emails | Outbox data holds "IDs and small fields only" | Raw token is envelope-encrypted inside the outbox row | The link needs the token; it is never stored in clear |
| Remember-me | 30 d sliding | 30 d sliding, capped at 90 d absolute | Bounds a stolen-but-refreshed token |
| Tenant restore from `pending_deletion` | "Cancel pending deletion" | Lands in `suspended`; reactivation is a separate step | Deliberate two-step return |
| Slug CHECKs | Regex on `citext` | Regex on `slug::text` | On `citext` the regex was case-insensitive and accepted uppercase (bug found by a test) |
| Password blocklist | "Embedded list of common/breached passwords" | ~150 most common passwords + project demo passwords | No network download during the build. A larger list can drop in later |
| Runner eligibility | Status not in (`deleted`,`failed`) | Also skips `provisioning` (the orchestrator migrates those) | Avoids racing provisioning |

## 4. Not built yet (by design or deferred)

- Impersonation (TENANCY §9), plans, billing, notifications, exports, platform staff management, and the migrations endpoints on the platform API (steps 9–18). `/api/auth/impersonation/exchange` is not mounted.
- Email sending: reset/invite/welcome emails are queued in the outbox only. The worker arrives in step 9.
- The frontend still uses the old API (step 19).

## 5. Open items for the owner

1. **Push and CI.** Push only after the history rewrite: commit `20cdb92` on this branch's ancestry contains the old DB password. Rotation is done (Q-7), so what's left is cleaning history.
2. Confirm the differences in §3.
3. Say "approved" for steps 6+ when ready.
4. One isolation-suite run failed once (61 s, during a deliberate mutation experiment) and could not be reproduced: three clean runs followed. If CI ever shows it again, it is worth investigating.
