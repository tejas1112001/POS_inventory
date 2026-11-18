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

## S10 – Item 8: Onboarding checklist

How to check (owner, or anyone who can edit settings):
1. Dashboard of a new shop: **Get your shop ready**, "N of M done" with a progress bar and the steps: Business profile and tax number, Logo, Store address, First product, First staff user, Email sending, First sale, and Online shop (only when the plan includes the online shop). Each step opens its page.
2. Ticks come from the shop's data, never from clicking: e.g. add a product → "First product" is ticked on the next dashboard load; sell something → "First sale"; fill address, phone and tax number in Settings → "Business profile and tax number". "Email sending" ticks when the shop's own email server is verified or any email has been sent.
3. **Hide checklist** → confirm → it disappears for everyone in the shop, for good (audit: settings.onboarding_hidden). When every step is done it disappears by itself.
4. Cashiers (no settings:edit) never see it.

## Industry P1 – Item 1: Business type and modules

How to check:
1. An existing shop (owner): Settings → Shop shows a **Business type** card at the top with **Retail** chosen and no module ticked. Every other screen is unchanged (no new menu items).
2. Choose **Clothing**: Sizes, colours and attributes, Barcode labels and Stock counts get ticked. Untick one, **Save business type** → "Business type saved." Reload: the choice is kept. Audit log: `settings.business_update` with before and after.
3. Choose **Retail** again → every module unticked; save. Data from a module is never deleted when it goes off.
4. As an admin or a cashier the card is read-only: "Only the owner can change the business type and modules." Roles & permissions lists **modules: Change business type and modules**; the Admin role does not have it, the Owner does.
5. Super Admin console → Tenants: a **Type** column and an **All business types** filter. **New tenant** has a **Business type** field (default Retail). Create a Clothing shop: its owner's onboarding checklist shows "Import your catalogue (clothing template)" instead of "First product", and its Settings show Clothing with the three modules on.
6. Console → a tenant → Overrides: the keys list has "Module: sizes, colours and attributes", "Module: barcode labels" and "Module: stock counts". Add one with value **off**: within 30 seconds the shop's Settings card shows that module as **Not in your plan**, and it can no longer be ticked (an already ticked one stays ticked but is not usable).
7. API: `GET /api/auth/me` → `tenant.businessType`, `tenant.modules`, `tenant.entitledModules`.

Known problems:
- The modules themselves arrive with items 4 (attributes), 5 (labels) and 7 (stock counts); until then switching them on shows no new screen.
- The onboarding "Import your catalogue" step opens the Products page; the template import itself arrives with item 6.
- Existing shops' Biller role does not get new permissions automatically (roles are never reset); item 4 gives new shops' billers `attributes:view`.

## Industry P1 – Item 2: Payments are "recorded"

How to check:
1. Checkout: sell one item paid part **Cash**, part **UPI** (with a reference). Sale done screen: "UPI (recorded)"; Cash has no label.
2. Orders → that sale: the payments list shows "UPI (recorded)" and, under it, "Payments are recorded by the cashier and are not verified by a bank or payment provider."
3. Print the receipt and download the PDF: the UPI line reads "UPI (recorded)" and the same sentence is printed above the footer. A cash-only sale shows neither the label nor the sentence.
4. Email the receipt (customer with an email): the email shows the same label and sentence.
5. Customer account: collect a dues payment by **Card** → the history shows "Card (recorded)" (hover for the hint). The statement shows "Payment received (card, recorded)".
6. Reports → Sales by payment method: non-cash rows carry "(recorded)" and the legend is shown under the table.
7. Old sales from before this change show the same labels (every existing payment is "recorded").

Known problems:
- Nothing can be marked "verified" in this release; that needs a payment provider (a later release).

## Industry P1 – Item 3: Barcode scanning

Use a USB or Bluetooth scanner in keyboard mode (it types the code and presses Enter). Without one, type the code fast and press Enter, or type it into the search box and press Enter.

How to check:
1. Settings → Appearance → **Barcode scanner**: click the test box and scan → "Scan recognised: <code> (n characters)". If a slow scanner is not recognised, raise "Time between scanner keystrokes" (stored in this browser only).
2. Checkout: scan a product's barcode with the cursor anywhere (not in a field) → the exact variant is added with quantity 1. Scan again → quantity 2. Scanning while the search box has focus does the same and clears the box.
3. Checkout: scan a variant's SKU → the same as a barcode.
4. Checkout: scan an unknown code → a low beep, the toast "No product with barcode <code>", and the code stays in the search box so it can be fixed.
5. Checkout: type a barcode into the search box and press Enter → that variant is added. Typing a product name and pressing Enter still works as before.
6. Checkout: a product that is in use at another till → "… is in use at another till." and nothing is added.
7. Checkout with the payment dialog open: scanning does nothing (no item added behind the dialog).
8. Returns → New return, step Items with a sale chosen: scanning an item of that sale adds 1 to "Return now" (never more than is left); an item not on the sale → beep and "No item still returnable on this sale with barcode …". Without a sale (unlinked return): a scan adds a line, or 1 more on the same variant's line.
9. Purchases → Add purchase, step Items: a scan adds a line (quantity 1, the variant's cost as the unit price), or 1 more on the same variant's line.
10. A shop with no barcodes on its products: nothing changes; typing in the search box works as before.

Known problems:
- A scanner set to send Tab instead of Enter works; one set to send nothing after the code is not recognised (set the scanner to send Enter).
- Scans are not recognised inside other text fields (on purpose, so typing a note is never taken as a scan).

## Industry P1 – Item 4: Sizes, colours and attributes

Set the shop to **Clothing** (Settings → Shop → Business type) first; a retail shop sees none of this.

How to check:
1. Menu → Catalogue setup → **Attributes** lists Department, Collection, Season, Size (XS…3XL) and Colour (free text). A Footwear shop gets Department, Size system, Size and Colour instead.
2. **New attribute** "Fit", Variant, Choose from a list, choices Slim / Regular → saved. A second "fit" → "An attribute with this key already exists."
3. Products → Add product → **Variable**, one variant (SKU TSH-01-S-RED). Open it: the **Sizes, colours and attributes** card. Enter article number TSH-01, Department Men, Season Spring/Summer; tick **Size** and **Colour**; give the variant Size S, Colour Red → **Save attributes**. The option now reads "S / Red".
4. **Create variants** → tick S, M; Colour "Red, Blue" → **Preview**: 3 new variants (S / Red skipped) with SKUs TSH-01-S-BLUE, TSH-01-M-RED, TSH-01-M-BLUE → **Create 3 variants**. The product shows 4 options with stock 0.
5. Create variants again with the same values → Preview says 0 new, 4 skipped. A pattern that repeats a SKU (e.g. "TSH-{colour}" with two sizes) → "The SKU pattern gives the same SKU to several variants".
6. Products → search "tsh-01" finds the product by its article number.
7. Checkout: the variant picker shows "S / Red", "M / Blue"…; sell S / Red (the only one with stock); Orders → that order shows "Crew tee (S / Red)", and it still does after the attribute is edited.
8. Attributes → edit Size and remove "M" → refused: "M is used by 1 variants or products…". Adding "XXS" works. Delete Colour → refused while products use it; delete Fit → done.
9. A plain product (no dimensions) still saves with any free attribute, as before.
10. Settings → untick **Sizes, colours and attributes** → the Attributes menu and the product card disappear; products keep their labels; the Attributes page says the module is off.
11. Roles: Admin has the four Attributes permissions; a new shop's Biller has "View product attributes".

Known problems:
- The variant grid creates variants with stock 0; receive stock with a purchase or a stock count (item 7).
- Existing shops' Biller role does not get "View product attributes" automatically (roles are never reset); the owner can add it.

## Industry P1 – Item 5: Barcodes and labels

Set the shop to **Clothing** (Barcode labels is on by default) and have a product with 2–3 options, one without a barcode.

How to check:
1. **Scan check first** (design §5 spike): Settings → **Labels** → A4 sheet → **Test page**. Print it at **Actual size / 100 %** (not "Fit to page"). Scan two labels with a phone barcode app and with the shop's scanner (Settings → Appearance → Test scan): each reads 13 digits starting with 2, e.g. 2000000000015. Do the same on the 50 × 25 roll if the shop has a label printer.
2. Product page → **Labels**: each option with its barcode (or "None: prints the SKU"), price and copies 1. **Create barcodes** → the missing ones get a code starting with 2; options that had a barcode keep it.
3. Set copies 3 / 0 / 2, A4 sheet → the line under the table says "5 labels on 1 sheet" → **Print 5 labels**: a PDF opens in a new tab with 5 labels (name, option, price, bars, digits, SKU).
4. Click outside the copy boxes and scan one of the option's labels: its copies go up by 1. Scan an unknown code: a beep and "No option of … has the code …".
5. Checkout: scan a printed label → that option is added.
6. Copies totalling more than 2,000 → the Print button is disabled with "At most 2000 labels per print".
7. Settings → Labels → **Add layout** "Shelf 40 × 30": page 40 × 30, label 40 × 30, 1 × 1, fields Name, Price, Barcode bars → saved; **Test page** prints it. A label wider than the page → "the labels need … mm across but the page is … mm wide".
8. Products → a product with sizes → **Create variants** → tick **Give each new variant an in-store barcode** → the new options have codes starting with 2.
9. Roles: Admin has "Print barcode labels"; a Biller has not (no **Labels** button).
10. Settings → untick **Barcode labels** → the Labels button, the Labels settings tab and the matrix tick box disappear; barcodes already made still scan at checkout.

Known problems:
- Labels print Latin text only well: names in Devanagari or other complex scripts show unjoined letters (same as receipts). Long names are cut with "…".
- Printing from a received purchase (copies = quantity received) arrives with item 8, and from a stock count with item 7.
- A scan while a copies box has the focus types into the box; click outside it first.

## Industry P1 – Item 6: Import templates

Set the shop to **Clothing**. Have a user with the Admin role and one Biller.

How to check:
1. Products → **Import** → Template shows **Clothing** (the shop's type). **Excel template** → open it in Excel: a Products sheet with one example row (TSH-01) and a Help sheet listing each column and the allowed values (Men, Women…; sizes XS–3XL). **CSV template** gives the same columns.
2. Choose **General** → the CSV template has the same columns as before (name, sku, price…).
3. In the Excel template replace the example with 3 rows: TSH-01 Crew tee size M, TSH-01 Crew tee size L (both Blue, opening qty 5 and 3), and BAG-9 Tote bag with no size. Type a 13-digit barcode in one row: it stays as typed (the column is text). Save as .xlsx.
4. Choose the file → **Check file**: "2 products (3 options) would be created". Click Crew tee → rows M / Blue and L / Blue with SKU, price and "5 at Main Store". Nothing is saved yet (Products list unchanged).
5. **Import** → "Imported 2 products (3 options)". Crew tee has options M / Blue and L / Blue, Department Men, stock 8.
6. Errors: in a CSV give the two rows of one article different departments, a size XXXL, and a barcode Excel turned into 8.90123E+12 → "Nothing was imported" with each row and column listed. **Download the error report** → an Excel file with Row, Column, Problem and the original row.
7. Two rows of one article with the same size and colour (M Blue, m blue) → "the same size and colour as row 2".
8. Leave the dialog with the error list open for more than an hour → **Download the error report** says the report has expired (reports are kept 1 hour, one per user, and only the user who ran the import can download it).
9. A Biller has no Import button (and gets 403 on the template and import).
10. A **Retail** shop: Import works as before with the General template; a CSV with only the old columns imports exactly as before.

Known problems:
- Import creates products only; it does not update existing ones or add options to an existing article (an existing SKU or barcode is a row error).
- Only the first sheet of a workbook is read; formulas are read as their last saved value.
- An .xls (Excel 97–2003) file is refused: save it as .xlsx or CSV UTF-8.

## Industry P1 – Item 7: Stock counts

Set the shop to **Clothing** (Stock counts is on by default). Have a product with 3 options (5 in stock each) and another product in a brand.

How to check:
1. Sidebar → Inventory → **Stock counts** → **New count** → Everything in the store → **Start counting**: a count CNT-000001, Draft; "Not counted yet" lists all items.
2. Scan option A twice (or type its barcode/SKU in the box and press Enter): A shows Counted 2, In stock now 5, Difference −3. Click outside any box and scan again: 3.
3. Type 1 in B's Counted box and press Enter: Difference −4.
4. **Upload a count sheet**: a CSV `sku,counted_qty` with `C,9` → C counted 9. A sheet with an unknown SKU → "Nothing was imported" and the row listed; nothing changes.
5. Sell one C at checkout, then reload the count: C shows "Stock changed by −1 since counted" and a warning at the top.
6. **New count** → One brand → that brand: scanning an option of the first product says it is not part of this count. Another whole-store count cannot take A ("already in draft count CNT-000001").
7. **Labels** (Barcode labels on): copies = the counted quantities; print.
8. **Review and complete**: the dialog says how many items change and by how many units → **Complete and update stock**: Completed; Stock shows A 3, B 1, C 9; the branded product unchanged. Adjustments & history → Stock changes lists "Stock count" lines.
9. A completed or cancelled count has no boxes or buttons. **Cancel count** on a draft keeps it as Cancelled with no stock change.
10. Roles: Admin has the three "stock count" permissions; a Biller has no Stock counts menu. A role with View + Count but not Complete has no **Review and complete** button.
11. Settings → untick **Stock counts** → the menu item disappears and the pages say no access; stock changes made by counts stay in the history.

Known problems:
- With Stock counts turned off, the count pages are hidden (not read-only); their stock changes stay in Adjustments & history.
- A scan while a Counted box has the focus types into the box; click outside it first.
- "Not counted yet" lists up to 200 items (those with most stock); the total is always shown.
- Two people can scan into the same count at once; a typed number replaces whatever was there.

## Industry P1 – Item 8: Receive purchases in parts

Have a purchase with status Ordered and two lines (for example 100 shirts and 5 caps), and a USB barcode scanner if possible.

How to check:
1. Open the purchase → **Receive goods**. Each line shows Ordered and Still to come.
2. Type 60 for the shirts (or scan a shirt's barcode or SKU: each scan adds one) → **Receive 60 units**. A GRN number appears; the status is **Partially received**; the Received column shows 60 / 0; stock went up by 60.
3. If labels are on, the label window opens with 60 shirt labels; print or close it. Each entry under **Goods received** also has **Print labels**.
4. Open **Receive goods** again: the shirts show 40 still to come. Type 41 → refused ("At most 40 still to receive"). Scan a code that is not on the purchase → beep and a message.
5. **Cancel** is gone for this purchase; payments and the note still work.
6. **Everything arrived** → Receive: the status becomes **Received**, with the date. Goods received lists both GRNs.
7. Another purchase: **Receive everything** (the old one-click way) still works and makes one GRN.
8. Purchases list → status filter **Partially received**; Reports → Purchases shows each line's received quantity; the Dashboard counts partly received purchases as open.
9. As a user without "approve purchases": no Receive buttons and no Goods received list.

Known problems:
- A partly received purchase that will never be completed cannot be closed; correct stock with a stock adjustment (as designed).

## Industry P1 – Item 9: Returns policy, exchanges, store credit

Have a named customer (not Walk-in), a few products in stock and two users: the owner and a cashier without "override return policy".

How to check:
1. Settings → **Returns**: Refunds is on, the rest off, no time limit — today's behaviour. A return made now works exactly as before.
2. Turn on **Store credit** and **Exchanges**, set a time limit of 7 days and **Ask the condition** (restock: Resellable). Save.
3. Sell 2 items to the customer. Returns → New → pick the sale, give each line a condition: one **Resellable**, one **Damaged**. Choose **Store credit** → Save. The return shows "Given back as Store credit", the damaged line says "not restocked", stock went up by 1 only.
4. Customer account → **Store credit** shows the balance and the entry. As owner, **Adjust** (+50, reason) → balance goes up; −10000 is refused.
5. Checkout for the same customer → payment **Store credit**: up to the balance; the receipt shows "Store credit left". For Walk-in there is no Store credit option.
6. Returns → New → **Exchange**: pick the sale, a line to return, then scan or pick the new goods. The amounts update (customer pays / refund / to store credit). Pay the difference and save: you get a return and a new sale; the sale shows an "Exchange" payment. That sale cannot be voided.
7. Return against a sale older than 7 days → refused with "Return window closed". As owner, type a reason (10+ characters) and save → it goes through; the audit log has "sale_return.policy_override". As the cashier the override box is not offered.
8. Dashboard: collected today does not count exchange or store credit payments.

## Industry P1 – Item 10: Thermal receipt and HSN on lines

Have a product with an HSN code (Products → edit → HSN, e.g. 0902) and one without.

How to check:
1. Settings → Shop → Receipt text → **Receipt paper** shows A4 (as before). Choose **80 mm thermal roll** → Save.
2. Sell both products → the receipt opens; its paper box shows 80 mm (on a computer that never picked a size). **Print**: the preview is a narrow roll, text black, long names wrap, each line shows "SKU …" and, for the first product, "HSN 0902".
3. Pick **58 mm** in the receipt's paper box and print: narrower still. Reopen a receipt: this computer keeps 58 mm; another computer uses the shop's 80 mm.
4. **Download PDF** / email: still A4, the first line shows "· HSN 0902".
5. Change the product's HSN to 6109, reopen the old receipt: it still shows 0902 (the code at sale time). Receipts of sales made before this update show no HSN, as before.
6. On the counter printer, print one receipt at 80 mm and check margins (hardware checklist §0.1); if the edges are cut, set the printer driver's margins to none.

Known problems:
- Browser print margins depend on the printer driver; some drivers add their own margin on thermal rolls.
- Names in Devanagari or other complex scripts print well on screen and thermal (browser), but not in the PDF (as before).

## Industry P3 – Item 1: Batches and expiry

Use a shop with two stores. Have product A with stock in both stores (e.g. 10 and 4) and product B with stock in one.

How to check:
1. Settings → Business type → **Pharmacy** (or tick **Batches and expiry**) → Save. Sidebar → Inventory shows **Batches & expiry**; the list is empty ("Switch on … on a product to start"). Existing products are unchanged and sell as before.
2. Products → A → the **Batches and expiry** card says the product does not track batches. Switch **Track batches and expiry** on: the toast says current stock is listed as "No batch"; the card shows No batch 14, Sellable 14, Expired 0.
3. Batches & expiry: one row for A, batch "No batch", In stock 14. Open it: stock 10 / 4 per store; movements "Tracking switched on" +10 and +4.
4. Sell 2 of A in the first store and add 3 with an adjustment: the batch shows 15; its movements list "Sale #…" −2 and "Adjustment" +3. The Stock page and the batch total always agree.
5. Add a new product C with opening stock 7: its card shows tracking on and No batch 7 (new products track by default).
6. Switch A off: its card says it does not track batches; Batches & expiry no longer lists it (tick **Show batches with no stock** to see its batch with 0). Sell 1 of A: works as before. Switch it on again: No batch = the stock then on hand.
7. Settings → untick **Batches and expiry** → Save: the menu item disappears, the batch page says no access, products sell as before. Tick it again: A and C show their current stock under No batch.
8. Filters: Status **Expired** / **No batch**, Expiry **in 30 days**, Store. (Dated batches arrive with receiving, item 2.)
9. **Correct details** (Manage batches; dated batches only, not "No batch"): a reason under 5 characters is refused; MM/YYYY becomes the month's last day; a date more than 10 years ahead asks to tick "This expiry date is right"; a duplicate batch number is refused. The audit log shows "batch.edit".
10. Roles: Admin has "View batches" and "Manage batches"; a Biller of a new shop has View only (sees the page, no switch, no Correct details).

Known problems:
- Until item 2 (receiving) and item 6 (counts by batch), all stock of a tracked product sits in "No batch"; dated batches cannot be created from the screen yet, so step 9 needs item 2.
- The product card totals cover every store; the batch page can be filtered by store.
- Switching tracking off is refused while units are in quarantine (quarantine arrives with item 4).

## Industry P3 – Item 2: Receiving with batch and expiry

Use a pharmacy shop (Batches and expiry on) with a supplier, product A that tracks batches and product B that does not (switch it off on its page).

How to check:
1. Purchases → new purchase: 100 of A and 5 of B, status Ordered → open it → **Receive goods**. A shows batch rows (Batch no., Expiry, MRP, Qty) and **Another batch**; B shows the usual box.
2. Enter A as batch B1 expiring 03/2030 × 60 and B2 expiring in 20 days × 10; B 5 → **Receive**. A yellow box lists B2 ("expires … (20 days)") → **Receive them anyway**. The receipt lists "B1 exp 2030-03-31 × 60; B2 … × 10".
3. Batches & expiry: B1 and B2 with their stock; B2 shows "20 days left" in amber. The audit log entry "purchase.receive" lists B2 as accepted.
4. Receive again with batch B1 but expiry 02/2030: refused ("already recorded … another expiry date"). With 03/2030: accepted, B1 grows.
5. An expiry in the past: "This batch has expired". A date more than 10 years ahead: refused until **The expiry dates are right** is ticked.
6. Batch quantities that do not add up are impossible on screen (the line is their sum); leave the batch number empty: refused.
7. **Status → Received** on a purchase with A still outstanding: refused ("Receive the goods line by line…"). A purchase of B only can still be created as Received.
8. Scan A's barcode in the receive window: the last batch row's quantity goes up by one.
9. After the receipt, **Print labels**: one row per batch ("Batch B1 · Exp 2030-03-31"); the PDF shows "B B1 EXP 03/2030" on each label.
10. Settings → untick Batches and expiry: Receive goods shows plain boxes again for A and receives without batches.

Known problems:
- Batch number and expiry are typed; reading them from 2D codes on packs is later (Q17).
- The near-expiry window (90 days) can be changed once item 7 adds Settings → Batches.
- A label shows the batch only on layouts that print the option or price line (all three built-in layouts do).

## Industry P3 – Item 3: Selling by batch

Use the pharmacy shop from item 2: product A with batches EARLY (expires in 100 days) × 3, MID (200 days) × 4, and 2 units with no batch.

How to check:
1. Checkout → add 5 of A: under the line "Batch EARLY · Exp … × 3" and "Batch MID · Exp … × 2" appear before payment. Pay: the receipt shows the same batches with MRP.
2. **Change batch** on a line: the list shows only sellable batches with their stock here; put 1 on MID → **Use these batches**: the line shows MID "(chosen)" and the rest earliest first. Any cashier can do this.
3. Batches & expiry → MID → **Correct details** (rename it). Reopen the earlier receipt: it still shows the old number.
4. Sell until only the "No batch" units are left: the line says "No batch recorded: check the pack" in amber; the sale goes through.
5. Ask for more than the sellable batches hold: a red message under the total ("Not enough sellable stock … can be sold") and **PAY** is disabled.
6. Make a batch expired (correct its expiry to yesterday, ticking the confirmation if asked): it is no longer offered or sold. An expiry of today still sells.
7. Orders → open a sale with A → **Edit**: refused ("Return or void the sale instead"). **Void** it: Batches & expiry shows the units back in the same batches.
8. A product that does not track batches sells exactly as before.

Known problems:
- "Minimum days to sell" (default 0) is set in Settings → Batches with item 7.
- The batch shown before payment can change if another till sells the same batch first; the sale then uses what is left, earliest first.

## Industry P3 – Item 4: Returns per batch and quarantine

Use the pharmacy shop from item 3: product A with batches EARLY and MID, and a sale of 5 (3 EARLY, 2 MID).

How to check:
1. Returns → New return → choose the sale: product A shows a **Condition** column even if the return policy has conditions off, and "Back to batch EARLY, MID". Saving without a condition is refused.
2. Return 3 as **Resellable**: the return detail shows "Batch MID × 2 · back on sale" and "Batch EARLY × 1 · back on sale". Batches & expiry shows the units back.
3. Make EARLY expired (correct its expiry to yesterday). Return the last 2 as Resellable: the detail shows "Batch EARLY × 2 · quarantine" in amber; product stock does not go up; the batch shows 2 under Quarantined.
4. Sell 1 more and return it as **Damaged**: it goes to MID's quarantine.
5. Return without a sale (needs the price override permission): a product that tracks batches shows Condition and a **Batch** list; "Not known (quarantine)" puts the units in quarantine under "No batch".
6. Batches & expiry → MID → **Quarantine** at a store: quantity and reason (5+ characters); stock goes down, Quarantined goes up, and the movement list shows "Quarantined". Checkout cannot sell those units.
7. **Release** puts them back. On an expired or recalled batch the Release button is not shown, and the server refuses it.
8. Exchanges: the returned goods follow the same rules; a product that does not track batches returns exactly as before.

Known problems:
- Units in quarantine keep the batch module and the product's tracking on until they are released or written off (write-off comes with item 5).

## Industry P3 – Item 5: Write-offs and recalls

Use the pharmacy shop from item 4.

How to check:
1. Batches & expiry → a batch → **Write off** on a store row: choose From (Stock or Quarantine, with the counts), quantity, reason (Expired is chosen for an expired batch, Recalled for a recalled one). Save: the counts go down and the movement list shows "Written off".
2. Ask for more than is there: refused, nothing changes. Reason **Other** needs a note.
3. Sidebar → **Write-offs**: the new `WO-000001` is listed; click it to see the lines.
4. On a batch, **Recall** with a reason: a red banner shows "Recalled … It cannot be sold". Checkout refuses the product if only that batch has stock. **Release** is no longer offered for its quarantine.
5. A return of a recalled batch goes to quarantine (item 4).
6. **Where it went** on a batch lists every sale (with the customer's name and phone when not walk-in), returns and write-offs, with totals.
7. **Lift recall** with a reason: the batch sells again.
8. A biller sees batches and write-offs but has no Write off, Recall or Lift recall buttons.

Known problems:
- A write-off made from a batch has one line; several batches in one write-off are possible through the API only.
- "Returned to supplier" is a write-off reason only; a supplier return with a debit note comes later.

## Industry P3 – Item 6: Stock counts per batch

Use a pharmacy product with 10 units of "No batch" stock and batch B1 × 3.

How to check:
1. Stock counts → New count → scan or type the product: the line has a **Count a batch** link.
2. **Count a batch** → B1 → 2. Then **Count a batch** → "Found on the shelf: new batch…" → number SHELF1, expiry 03/2028 → 5; again SHELF2 with last month's expiry → 4. Each line shows its batch; SHELF2 shows "expired" in red.
3. An amber note lists "No batch (10)": it will be set to 0 on completion because the product is now counted by batch. Totals include it.
4. Typing B1 again with another expiry is refused ("on record with another expiry date").
5. Complete: Batches & expiry shows B1 2, SHELF1 5, SHELF2 4, no "No batch" stock; the product's stock is 11.
6. On the completed count an amber note offers **Write off expired**: it creates one write-off (reason Expired) for SHELF2's 4 units.
7. A count that only scans the product (no batch line) works as before; a decrease is taken from the earliest expiry first.
8. Products that do not track batches count exactly as before.

Known problems:
- Scanning adds to the line with no batch; for a product counted by batch that line counts its "No batch" stock.

## Industry P3 – Item 7: Expiry report, dashboard, settings, digest

Use a pharmacy shop with an expired batch, one expiring within 90 days, one quarantined unit and one recalled batch.

How to check:
1. Reports → **Expiry** tab (shown only with Batches & expiry on): four boxes (Expired on hand, Expiring within 90 days, Quarantined, Recalled with stock) with units, batches, cost and MRP value. The table lists recalled first, then expired, expiring and quarantined.
2. Select a box: only that section shows; select it again to show all. Change the store and the window (30/60/90/180 days).
3. Select a batch number: the batch dialog opens (write off, recall, quarantine as allowed). After a write-off the report refreshes.
4. Excel and PDF download the same rows.
5. Dashboard: cards for expired, expiring soon, recalled and quarantined units (and "No batch" units); each opens the report filtered to that section ("No batch" opens Batches filtered to the unknown batch).
6. Settings → **Batches**: near-expiry days (1–730), minimum days left to sell (0–365), daily expiry email (off by default). Out-of-range values show an error under the field.
7. With the daily email on, the owner gets one email a day after 08:00 shop time, only when something is expired, expiring, quarantined or recalled.
8. A retail shop (module off) shows no Expiry tab, no batch cards and no Batches settings tab.

Known problems:
- Exports are Excel and PDF like the other reports; the design said CSV and Excel.

## Industry P3 – Item 8: Pharmacy import template

How to check:
1. Products → Import: the Template list has **Pharmacy** only when Batches & expiry is on. Download the Excel template: a Products sheet with the batch columns and a Help sheet.
2. Fill three rows for one SKU (batches P1 and P2 in the main store, P1 again in a second store using its slug), one row with an expiry last month, and one with an expiry next month. Use `MM/YYYY` for some expiries.
3. **Check file**: one product per SKU; open it to see each batch row with its store and quantity. The expired row is struck through and marked "expired, not imported". An amber box lists the expired row and the near-expiry row.
4. Change one row's price or name for the same SKU, or give a batch two expiries: Check file lists the row errors and nothing is saved.
5. **Import**: Batches & expiry shows the batches with their expiry, MRP and stock per store; the product's stock in each store is the sum of its batches; the expired batch is not there.
6. In a pharmacy shop the General template still works; its products track batches and the opening stock shows as "No batch".
7. In a retail shop (module off) the Pharmacy template is not offered and imports work as before.

