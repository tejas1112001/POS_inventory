# UI checklist

What to check by hand after each slice (no browser tests in S6–S9 by request). Sign in at https://demo-shop.localhost:8443 as the owner, and as the test cashier where a step says so.

## S6 – New checkout

**Turn it on:** Settings → Shop → "Checkout" → tick "Use the new checkout" → Save. Every till of the shop switches. Untick it to go back to the old screen.

How to check:
1. Open **Checkout** (`/pos`). The search box has the cursor (desktop). The top shows the store, "Cashier: …" and a "?" button.
2. Type part of a product name: the grid filters. Type an exact SKU and press **Enter**: one unit goes into the cart and the line flashes. Press Enter on a search with one match: it is added (or "Choose option" opens for a product with options).
3. Watch the total: "checking" shows briefly, then Subtotal, Tax and Total come from the server. **PAY** stays disabled while the total is being checked.
4. Change the quantity: **+ / −** buttons; tap the number and type 3, then Enter. Typing more than the stock gives "Only N … in stock here." and sets the maximum.
5. Keyboard (search box empty, no window open): **↑ / ↓** move the highlighted line, **+ / −** change it, **Delete** removes it, **F4** opens the customer picker, **F8** jumps to Discount, **F10** or **Ctrl+Enter** opens payment, **?** shows the list, **Esc** clears the search.
6. Discount: type 10 in Discount (F8). The total is re-checked. As the cashier, a discount above the shop's limit shows the server's reason under the total and PAY stays disabled until it is fixed.
7. Favourites: tap the star on two tiles. A "Favourites" pill appears; it shows only those products. Reload: the stars are kept (in this browser only).
8. Tiles: a product in the cart shows "N in cart"; a product you hold is still tappable; a product held by another till shows "In use".
9. **F4** customer picker: search by name or phone, Enter picks the first. "Add customer" adds one and selects it.
10. **PAY** → "Take payment": the amount is the checked total. Cash: "Complete sale" with an empty box says "Enter the cash received"; less than the total says how much is short; "Exact" and the note buttons fill the box; "Change to give" updates. On a tablet (touch, tall screen) a keypad shows. Card/Cheque/Deposit/Points need no cash.
11. Stop the server (or go offline) and press "Complete sale": the sheet stays open with "No answer from the server… it will not be charged twice". Start it again and press once more: one sale only (check Orders).
12. After a sale: "Sale complete" shows the reference and the change in large type. **Print** prints the receipt (paper size from the receipt window). **Receipt** opens it (PDF, email). **New sale** empties the cart and sets the walk-in customer. With "Start a new sale automatically" ticked, it counts down 5 s; touching the window pauses it. Untick it: the choice is kept.
13. Prices: change a product's price in another tab while it is in the cart (as the cashier). The "Prices have changed" window appears before payment; "Use current prices" updates the cart.
14. Store change: add items, switch to a store where one has no stock. The cart stays, the item is named in the warning and marked on its line.
15. Phone (360 px): products first, a bar "N items · ₹… Review" opens the cart as a sheet; PAY opens payment as a sheet.
16. Leave the checkout with items in the cart (or close the tab): on another till as another user, those products are free again (at most 2 minutes later if the tab was killed).
17. Turn the setting off: `/pos` shows the old screen again; it still works as after S4b/S5.

Known problems:
- Favourites are per browser, not per shop or per user.
- Long-press on + / − (plan) is not done; tap the number to type a quantity.
- A custom role without "settings: view" always gets the old screen (it cannot read the switch).
- Hold/recall, split payments and UPI are S10 items; WhatsApp share and barcode scanning stay deferred.
- Dev only: `make migrate-dev` with the default parallelism hit "tuple concurrently updated" in the post-step on 2 of 8 shops; `--resume 11 --parallelism 1` finished them. To be looked at in Phase 3.
- Running every database test suite at once made the provisioning tests time out once; they pass on their own.

## S7 – Dashboard

How to check:
1. Open **Dashboard** as the owner. The heading greets you by first name ("Good morning/afternoon/evening", by the shop's clock) and says "Sales of the whole shop."
2. Date range buttons: **Today** (default), **Yesterday**, **Last 7 days**, **This month**, **Custom** (two date boxes, "to" cannot be after today). The address keeps the choice (`?range=…`); reload keeps it.
3. With more than one store, the store picker shows "All stores" and each store; the figures follow it.
4. Row 1: Net sales (after returns), Orders, Average bill, Items sold, each with a green/red change against the period of the same length just before (for Today: yesterday). "new" when the previous period had nothing.
5. Row 2: Collected, Due from customers (click → Customer dues), Refunds, Discounts given. Compare with Reports → Sales for the same dates: the totals match.
6. Chart: Today and Yesterday show sales by hour (24 points), 7 days and This month by day; the Revenue/Orders switch changes the line. Top products is a horizontal bar chart (top 5).
7. "Needs attention" cards: Low stock and Out of stock (click → Stock filtered), Purchases to receive (ordered or pending → Purchases), Customers with dues (→ Customer dues), Plan & billing when the shop is not active (trial, past due). Cards with nothing to show are hidden.
8. With no sale today: a dashed box "No sales yet today" with **Open Checkout**.
9. As the cashier: "Your own sales."; no "Due from customers" card; no Purchases or Customers-with-dues cards; the figures are only the cashier's sales.
10. Phone (360 px): two figures per row, the range buttons scroll sideways, charts fit the width.
11. Stop the server and reload: "Could not load" with **Retry**; start it and press Retry.

Known problems:
- "Purchases to receive" counts the whole shop (purchases have no store), whatever store is picked.
- The plan card shows only the shop status (trial, past due…), not days left; the banner at the top still has the details.
- Payment-method split (donut) waits for split payments (S10).

## S8 – Remaining pages

### S8a – Orders and order page

How to check:
1. **Orders** (`/sales`): newest first, 25 per page with the shared pager. Search an order number or a customer name.
2. Filters: Status (default "Not voided"; Completed, Pending, Voided), Payment (Paid, Partial, Unpaid, Overdue), Store (with more than one store), From/To dates. Each shows as a chip; "Clear filters" resets. On a phone the filters (and dates) are in the "Filters" sheet. The address keeps them; reload keeps them.
3. Row menu (…): Open, Receipt, Return items (not on voided orders), Void… (owner/admin only). Click the order number to open it.
4. **Order page** (`/sales/<id>`): number, status and payment badges, date; customer, store, cashier, payment (method and status), receipt email and notes; items with option labels, SKU and "N returned"; totals with Received/Change and Due; the order's returns (click → return).
5. Buttons: **Receipt** (view, PDF, email), **Print** (prints at once), **Return items** (opens New return with this order already chosen; hidden when everything was returned), **Void…**.
6. Void: the reason is required (at least 3 characters). "Keep order" cancels. After voiding: a red "Voided on … : reason" box, the badge says Void, and the stock is back (check Stock). The printed receipt shows VOID with the reason.
7. As the cashier: only own orders (a colleague's order number gives "does not exist"); no Void.
8. "New order" (owner) still opens the back-office order form.

Known problems:
- Order editing is not offered (Q6: use a return and a new sale); the server still accepts edits from the old form.
- The order page shows the receipt in the window used everywhere; there is no separate "share" link.

### S8b – Products and product page

How to check:
1. **Products** (`/products`): server pages; each row shows the picture (or initials), name, first SKU with "N options", category, brand (wide screens), price (a range when options differ), units in stock over all stores, status.
2. Search a name or a SKU. Filters: Category, Brand, Status (category/brand filters only appear for roles that may see those lists). Chips and "Clear filters" as elsewhere; on phones in the Filters sheet.
3. Header: **Import CSV** (opens the import window; same-name rows become options) and **Add product** (the existing add wizard).
4. Row menu: Open, Edit (the existing edit form in a sheet; Save keeps you on the list and refreshes it), Delete (asks first).
5. **Product page** (`/products/<id>`): picture, name, status, category · brand · unit. Options table: option label (attributes or SKU), SKU, price, cost (editors only), stock per store with low stock in amber and zero in red, and a total with more than one store. Details: tax, HSN, sold at, online shop published, added by, description.
6. Buttons: **Stock** (opens Stock searched by the SKU), **Edit**, **Delete** (back to the list after deleting).
7. As the cashier: the list and the page open (read only); no Import, Add, Edit, Delete or cost column.

Known problems:
- The add-product wizard and the edit form are the existing ones (restyled in S4b, not rebuilt).
- Barcode labels stay deferred.

### S8c – Purchases and purchase page

How to check:
1. **Purchases** (`/purchases`): server pages; number (click to open), date, supplier, total, due (wide screens), status and payment badges. Search a purchase number or a supplier name. Filters: Status, Payment, Supplier.
2. Row menu: Open; **Receive into stock** (Ordered/Pending, owner/admin with "approve purchases"); **Record payment** (when something is due); **Cancel purchase** (Ordered/Pending).
3. Receive: the window says how many units go into which stores and that it happens once. After it: badge "Received", a green note with the time, and Stock shows the higher quantities. Receiving again is not offered.
4. Record payment: shows total, paid and due; "Pay the full …" fills the amount; more than the due amount is refused. After it the payment badge changes (Partial or Paid) and Due drops.
5. Cancel: asks "Keep it / Cancel purchase". A cancelled purchase cannot be received or paid.
6. **Purchase page** (`/purchases/<id>`): items with the store each line stocks, unit cost and totals; the same three buttons at the top.
7. A role without "approve purchases" sees "Not received yet. Receiving needs the approve purchases permission." instead of the Receive button.
8. "New purchase" opens the existing purchase form.

Known problems:
- Purchases have no store of their own; each line names its store (shown per line).
- Overdue is not set from here (the server keeps it when the form sends it).

### S8d – Returns and return page

How to check:
1. **Returns** (`/sales-return`): newest first; return number (click to open), date, order (click → order page, or "Without order"), customer, store (wide screens), refund and "Paid back" badge.
2. Search a return number, an order number or a customer name. Filters: Store (more than one store), From/To dates (in the Filters sheet on phones).
3. Row menu: Open, Open the order.
4. **Return page** (`/sales-return/<id>`): order link, customer, store, note; items with reason, quantity, price and total; refund box (tax included, total, paid back, still to pay back).
5. From an order page, **Return items** opens New return with that order chosen; after saving, the order page shows the return under "Returns" and "N returned" on the line.

Known problems:
- The New return form is the existing one (restyled, not rebuilt).
- Returns have no printed credit note yet (S10b).

### S8e – Users (and the pages left as they are)

How to check:
1. **Users** (`/users`; the old `/billers` address opens the same page): name (with "You" on your own row), email, role, added date (wide screens), status. Search name or email; Status filter.
2. **Add user**: name, email, password (optional), role, status. With a password of 12+ characters the user can sign in at once; with an empty password the toast says an invitation was emailed (check Mailpit at http://127.0.0.1:8026). A short password is refused under the field.
3. **Edit**: name, email, role, status. Your own row has no "Remove".
4. **Remove** asks first; the removed user can no longer sign in.
5. As the cashier: Users is not in the menu; `/users` shows "no access".

Not rebuilt in S8 (already on the shared toasts, confirm windows and pager from S2–S4b): Roles & permissions, Storefront, the four reports, Invoices & dues. They keep their current layout.

Known problems:
- The users list is not paged on the server (shops have few users); search and pages are done in the browser.

## S9 – Super Admin console (part 1: before flags and overrides)

Sign in at https://admin.localhost:8443 (owner-console@posdev.test; see the reset earlier).

How to check:
1. Header: navy bar. At phone width (under 768 px) the menu folds behind a **Menu** button; it closes after you pick a page.
2. **Tenants**: new columns Last activity and Storage. **Sort**: Newest, Oldest, Name A–Z, Last activity, Storage used.
3. **Save view**: name the current search + status + sort; it appears as a chip above the table (click to apply, × to delete). Kept in this browser.
4. At phone width every table shows each row as a card with "label: value" lines.
5. Tenant page → **Overview**: new card "Health and plan": plan, schema (up to date / behind), receipts waiting to send, contacts/notes count, storage with a bar against the plan's storage limit, plan limits, recent notices (billing emails sent).
6. Tenant page → **Contacts** tab: add (name, role, email or phone, primary), edit, remove (asks first). Only one contact is primary: marking another moves the mark. Email and phone are plain text (the console never turns shop data into links).
7. Tenant page → **Notes** tab: add a note; newest first with time and author. There is no edit or delete (by design); write a new note to correct one.
8. As a support user (read-only role): Contacts and Notes are visible, but there is no Add/Edit/Remove or "New note".
9. Audit log: contact add/edit/remove and note add appear, without the email, phone or note text.

Known problems:
- The health card shows plan limits, not the shop's own counts (users, stores, products) and not per-shop email health: reading those needs the decision in `docs/S9_SECURITY_NOTE.md`.
- Saved views are per browser.
- Feature flags and limit overrides are not built (waiting for the security note).

## S9 – Super Admin console (part 2: overrides, flags, usage)

Approved in `docs/S9_SECURITY_NOTE.md` (all five decisions, plus an "expiring soon" warning).

How to check (console, as owner-console@posdev.test):
1. Tenant page → **Overrides** tab: empty history ("this shop uses its plan as it is").
2. **Add override**: pick a setting (Stores, Users, Products, Emails a day, Storage MB, Online shop, CSV import, New checkout), a value (a whole number, or on/off), the days (default 30, 1–366) and a reason (10+ characters). It asks for a fresh MFA code. Wrong values are refused under the field (0, decimals, above the cap or 10 × the plan, a reason that is too short).
3. Adding the same setting again replaces the old one (history shows "Replaced"). **Revoke** asks for a reason and an MFA code; the row shows who and why.
4. Set an override for 3 days: a yellow "ends within 7 days" box appears on the tenant page and the Overrides tab; the tenants list shows "override ending soon" next to the status. Overrides with more time show "N overrides".
5. Tenants list: tick **Has overrides** to see only those shops; it is kept in saved views.
6. Health card: "Stores 2 used of 3" style lines and Receipt emails (sent/failed in 7 days, waiting) from the worker's snapshot (every 15 minutes; the time is shown).
7. Mailpit (http://127.0.0.1:8026): every add or revoke sends "Override set/revoked for <shop>: <setting>" to the platform inbox (if CONTACT_EMAIL is set in deploy/.env).
8. Shop side: with "CSV import: off", Products → Import CSV says the plan does not include CSV import. With "New checkout: off", Settings → Checkout is greyed out with "The platform has turned the new checkout off", and `/pos` shows the old screen. With "Stores: 1", Settings → Stores → Add store is refused with the plan-limit message.
9. A billing_admin or support user sees the Overrides tab read-only (no Add / Revoke).

Known problems:
- Another server instance sees a change up to 30 seconds later (cache); the console's own instance at once.
- Usage counts are up to 15 minutes old.

## Phase 3 changes you can see

### Item 7 – dependency scan fixes
1. **Reports → Customers** and **Reports → Customer dues**: the "Generate PDF" button is now **Export to Excel** and downloads an `.xlsx` file (the client-side PDF export used `jspdf`, which had a critical vulnerability; plan question Q15 already chose Excel only).
2. Lists that had the PDF and Excel icons (old list pages, e.g. Invoices & dues, Roles) now show only the **Excel** icon; the file opens in Excel/LibreOffice with one column per field.
3. Receipts are unchanged: their PDF comes from the server.

Known problems:
- None found; Excel files are still built in the browser (write-excel-file v4).

### Item 8 – fixes found by the browser tests
1. Table header row (every list, order/product/purchase/return pages): the column titles are a darker grey; they were below the readable contrast (4.34:1, now about 7:1).
2. Checkout: the small "₹… each" and option text in a highlighted cart line, and "Change · F4", are darker grey; the "F10" hint inside PAY is no longer faded.
3. Checkout tiles: screen readers now read the tile's visible text (name, category, price, stock badge) instead of a separate label; the star button keeps its own label.
4. Dashboard: the unselected date-range buttons and the Revenue/Orders switch are darker grey (were 4.34:1).
5. Dashboard, order, product, purchase and return pages: their big titles are now level-2 headings (the top bar holds the page's one level-1 heading); they look the same.
6. Correction: cashiers can open Settings → Stores read-only (they hold "stores: view"), with no Add store and no Edit/Turn off. My S5 reply said "no access"; that was wrong.

Known problems: none new.

### Item 9 – load test change you can see
1. **Dashboard**: the figures are cached for 30 seconds per shop, user and filter (as PHASE1_DESIGN §9 planned). Make a sale, then open the dashboard: the new sale can take up to 30 seconds to appear. Changing the date range or store shows fresh numbers at once.

Known problems:
- Checkout and the 1,000-product POS list are slower than their design targets under load on this laptop (see docs/PHASE3_REPORT.md §6, O5/O6); in normal use they feel instant (median 28 ms and 75 ms).

## S10 – Item 1: Platform users (Super Admin console)

How to check (console, signed in as a super admin):
1. Menu → **Platform users**: every console account with role, status, sign-in state ("Invite sent (until …)", "MFA set up", "MFA not set up") and last sign-in. Your own row shows "You" and has no actions.
2. **Add user**: name, email, role, reason (10+ characters). It asks for your MFA code. Then a window shows the invite link **once** with "Copy link"; closing it hides the link for good. Mailpit (http://127.0.0.1:8026) has the invite email "You are invited to the … console" with an "Accept the invite" button.
3. Open the link in a private window: "Welcome. Choose your password" (14+ characters, twice) → the authenticator QR code → the 10 recovery codes (shown once) → signed in. The address bar shows only `/invite` (the token is removed). Opening the same link again says the invite is invalid, used or expired.
4. **New invite** (only for users who have not set up MFA): earlier links stop working; a new email and a new copy-once link.
5. **Role**: pick the new role and a reason; the user is signed out and sees the new menu at the next sign-in. Email "Your console role changed" to the user and the platform inbox.
6. **Deactivate**: the user is signed out at once and cannot sign in; **Reactivate** lets them in again. Both send emails.
7. **Reset MFA** (only for users with MFA): their authenticator and recovery codes stop working; at the next sign-in they set up a new one. Email to the user ("If you did not ask for this, contact the platform owner now") and the inbox.
8. Audit log: platform_user.invite / reinvite / role / deactivate / reactivate / mfa_reset with the reason; invite_accepted when the user accepts.
9. As a billing admin or support user: no Platform users menu item; the page answers "no access".

Known problems:
- The console password hint said 12 characters while the server required 14; it now says 14.
- An invite opened in a browser where another admin is already signed in shows the console, not the invite: use a private window.

## S10 – Item 2: Plan changes (owner and console)

How to check:
1. Shop (owner) → **Plan & billing** on a shop that pays through the provider (status Active): a **Change plan** card lists the other plans. "Change to …" opens a preview: old → new price per period and when it applies.
2. A dearer plan: "Takes effect now; the payment provider charges the pro-rated difference" → **Change now**: the plan and the usage limits change at once.
3. A cheaper plan while the shop uses more than it allows (for example 2 stores, plan allows 1): the preview lists "Reduce these first: Stores: 2 in use, the plan allows 1" and has no confirm button.
4. A cheaper plan within its limits: "Takes effect on <period end>" → **Schedule for <date>**. The "Your plan" card shows "Changes to … on <date>" with **Cancel the change**; cancelling removes it.
5. A shop on trial (not subscribed yet) has no Change plan card; it uses Subscribe as before. Staff without billing rights do not see the page.
6. Console → tenant → **Billing** → **Change plan**: pick a plan → **Preview** → the next window shows the price change and when it applies (or why it is refused), asks for a reason and your MFA code. A scheduled change shows as "Scheduled change: <plan> on <date>" with **Cancel scheduled change**.
7. Console: a shop billed by the provider whose usage has not been counted yet is refused ("Usage not measured yet"); a comped/trial shop changes at once.
8. Audit: billing.plan_change / billing.plan_change_cancelled (shop audit log and console audit log); billing.assign_plan in the console log with the reason.

Known problems:
- Tested against the fake provider only. Live Razorpay checks are listed in docs/BILLING_DESIGN.md (R7, R7a) and must be done before real customers change plans.
- Changing between monthly and yearly plans is refused (contact support).

## S10 – Item 3: Razorpay reconcile (report only)

How to check:
1. Console → **Billing** → **Drift** tab: "Checked every hour against the payment provider. Nothing is fixed automatically", the time of the last webhook, and two lists (Open; Resolved in the last 7 days). On a fresh dev stack both are empty.
2. The job runs hourly (worker job `billing_reconcile`). For each shop with a provider subscription it compares the provider status, plan and period end. A difference appears under Open with "Ours" and "Provider" values; a row opens the tenant.
3. When the same difference is seen in two runs in a row, Mailpit gets one email to the platform inbox: "Billing differs from the payment provider: <shop>". It is sent once per difference.
4. When both sides agree again, the row moves to Resolved.
5. When subscriptions are live but no verified webhook came for 24 hours (BILLING_WEBHOOK_QUIET_HOURS), the inbox gets "No billing webhooks received" (at most once a day). Metrics: pos_billing_drift_open, pos_billing_webhook_age_seconds; alert rules in docs/OPERATIONS.md.
6. The job never changes a subscription: fix a difference with the tenant's billing actions (Change plan, Cancel, Grace…).

Known problems:
- With the fake provider in dev, subscriptions created before a restart are unknown to the fake and show as "Subscription at provider: provider answered 400".

## S10 – Item 4: Nightly exports, retention, final deletion

How to check:
1. Nightly exports run at 01:00 India time (worker job `nightly_exports`). Next morning, console → tenant → **Exports** shows a `nightly` export (data only) for every live shop. Pending-deletion and deleted shops get none.
2. Retention: each shop keeps its last 7 nightly exports and its last 4 Sunday ones; older nightly exports show status `expired` (their files are deleted). Manual and final exports are never expired, and the shop's newest export with a passed restore test is always kept.
3. Console → tenant → **Delete** now says the deletion date is 30 days out. The tenant page then has a **Deletion** card: requested, deletion date, warnings sent, final export, restore test.
4. Warnings: 14, 7 and 1 day(s) before the deletion date, Mailpit gets "<shop> will be deleted in N days" for the billing contact and the owner (both if different).
5. The final export is taken automatically; once it succeeds a restore test is queued (run by `posctl tenant restore-tests`).
6. **Finalise deletion** stays disabled with the reason ("The deletion date is …", "The final export has not succeeded yet", "…restore test has not passed yet") until all are done. Then it asks for a reason, the subdomain typed, and your MFA code. Afterwards: status deleted, the shop's address no longer answers, the payment provider's subscription is cancelled at once, and the audit log has tenant.finalise_deletion. The schema and files stay (OPERATIONS §6.4 step 4).

Known problems:
- In dev the restore test only runs when `posctl tenant restore-tests` is run by hand.
- Nightly exports hold data only (not uploaded images); the final export holds both.

## S10 – Item 5: Split and part payment, customer dues

Before you start: Settings stays as it is; the owner has the new permissions "Sell on credit or part payment" and "Set customer credit limits" (not given to Admin); Owner and Admin have "Collect customer payments"; Biller has none of them.

How to check:
1. Customers: the list has **Owes** and **Credit limit** ("No credit" by default) columns, and a row action **Account and dues**.
2. Account page (owner): **Change** next to Credit limit → set e.g. ₹1,000 (or "No credit for this customer"). Walk-in cannot get a limit. An admin or biller sees no Change link.
3. New checkout with that customer → Take payment: pick Cash, type 100 → **Add this payment and take another**; pick UPI, amount 50, reference; tick **Put what is left on <customer>'s account**, due date defaults to 30 days → Complete. The done screen shows "Cash + UPI + On account" and "₹X on <customer>'s account, due by <date>".
4. The order page shows each payment line (Cash, UPI (ref)), Paid, **Balance due** and **Due by**; the receipt PDF and the receipt email show the payment lines, "Balance due" and the due date.
5. Credit limit: put more on account than the limit allows → the sheet stays open with "Credit limit reached: <customer> owes ₹X of ₹Y. Take ₹Z now or ask the owner to raise the limit."
6. With Walk-in, or as a cashier/admin without the permission, there is no "on account" box and a short payment is refused.
7. Quick path unchanged: Cash → Exact → Complete (or Card → Complete) works as before.
8. Account page: **Collect payment** (amount defaults to what is owed; method, reference) → it pays the oldest sale first; more than owed is refused. The payment appears under "Payments received" with the sales it paid; **Reverse** asks for a reason, adds a reversal line and the dues come back; a payment can be reversed once.
9. A sale paid by a collection cannot be voided ("Reverse that payment first").
10. **Statement** → choose dates → opening balance, each sale, payment, collection, reversal, return and refund with the running balance, closing "Balance due" equal to "Owes now". **Print** prints only the statement (A4).
11. Return part of a sale that still has something due (Return items on the order): the due drops first and only the rest is refunded; the return shows "applied to due".
12. Orders list: Paid / Partial / Unpaid / **Overdue** (a credit sale past its due date); the Payment filter "Overdue" works. Invoices & dues show the due date.
13. Dashboard: **Collected** = payments received in the period (including dues collected); Due from customers / overdue use the due date.
14. Reports → Sales: **Sales by payment method** table (method, payments, of which dues collected, amount; "Money taken" total).
15. Back office **New order**: method + "Paid now" (empty = in full; less = rest on account with "Rest due by").
16. Customer Due report: the customer name opens the account page; dues already include returns.

Known problems:
- The old POS screen (when New checkout is off) still takes one payment; it cannot split.
- Existing sales from before this change have no due date, so they never show Overdue.
- Credit applies to completed sales; a pending order has no due date until it is completed.

## S10 – Item 6: Low-stock alerts

How to check:
1. Settings → **Notifications**: "Email the owner the low-stock list every morning" is on by default; turning it off is saved and audited like other settings.
2. Set a product's low-stock level above its quantity (Stock page). Within 5 minutes (or on reload), staff who can see stock get a yellow banner "N items are at or below their low-stock level." with **View stock**; it can be dismissed for this tab and comes back the next day or when the count changes. Users without stock access get no banner.
3. From 08:00 shop time the worker (job `low_stock_email`, hourly) sends every active owner one email "N items are low on stock at <shop>" (Mailpit), listing up to 50 items with store, quantity left and alert level, and a "See stock" button. Only once a day, never when nothing is low, never when the setting is off.
4. The Dashboard's Low stock / Out of stock cards are unchanged.

Known problems:
- The email goes out at the first hourly run after 08:00 shop time (up to an hour later).

## S10 – Item 7: Held bills

How to check (new checkout):
1. Add items, choose a customer, give a discount → **Hold** (or F6) → optional label (e.g. "Table 4") → **Hold bill**. The cart empties and the toast says "Bill "Table 4" held." Stock does not change.
2. **Recall** (or F7) on another till of the same store (another cashier is fine): the list shows the label, items, an estimate, who held it and when. **Recall** loads the cart with the customer and discount; prices are checked again (a changed price shows "Prices have changed"), quantities are cut to what is in stock now with a note.
3. Recall on two tills at once: the second gets "Someone else recalled or discarded that bill." The trash button discards a bill.
4. Recall is disabled while the cart has items ("Hold or clear it before recalling another bill").
5. Another store's held bills are not listed; held bills disappear after 7 days; one user can hold at most 20 ("You already hold 20 bills…").
6. Shortcuts help (?) lists F6 and F7.

Known problems:
- Held bills are not in tenant exports (transient, like product locks).
- The old POS screen has no Hold/Recall.
