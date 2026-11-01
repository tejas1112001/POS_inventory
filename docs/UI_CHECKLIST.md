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
