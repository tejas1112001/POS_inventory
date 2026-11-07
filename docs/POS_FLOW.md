# How the POS works today

A guide for the shop owner. It describes what the system does today, section by section, in the same order as the menu on the left of the screen.

| | |
|---|---|
| Written | 29 October 2026 |
| Based on | the screens, the system's rules and its automatic tests, checked by trying things in a test copy of the shop where the screens alone were not clear |
| Not covered | the public shop pages that customers see, the Super Admin console, and the old version of the system |

## How to read this guide

Every behaviour carries one of these marks:

| Mark | Meaning |
|---|---|
| **IMPLEMENTED** | Works today, on the screen and in the system's rules |
| **PARTLY IMPLEMENTED** | Works, but something is missing, or the screen and the rules do not fully agree |
| **NOT BUILT** | There is no way to do this today (sometimes the system could do it, but no screen offers it) |
| **UNSURE** | Could not be confirmed |

Money examples use Indian rupees (₹), the default currency, with 2 decimal places.

Messages in quotes are copied exactly as the screens show them. A few of them use the system's internal words (for example "supplierId is required."); they are quoted as they appear.

## Words used in this guide

| Word | Meaning |
|---|---|
| **Sale** | A customer buying from the shop, at the POS or entered in the back office |
| **Purchase** | The shop buying stock from a supplier |
| **Sales return** | A customer bringing goods back |
| **Stock adjustment** | Changing stock by hand, for example after a count or for damaged goods |
| **Store** | A place that sells and holds stock |
| **Warehouse** | A place that holds stock but does not sell |
| **Variant** | One version of a product, for example "T-shirt, size M". Simple products have exactly one variant. Stock and prices belong to variants |
| **SKU** | The short code that identifies a variant |
| **Biller** | The staff member who made a sale. Also the name of the built-in cashier role |
| **Walk-in Customer** | The customer used when nobody is named. Every new shop gets one |
| **Receipt number** | The number printed on a receipt, for example POS-INV-000001 |

---

## 1. Who can do what (permissions, in short)

**IMPLEMENTED.** Every staff member has one **role**, and the role decides what they can see and do. The full list of permissions is in section 15. Three roles come with every shop:

| Role | What it can do |
|---|---|
| **Owner** | Everything |
| **Admin** | Everything except Billing |
| **Biller** (cashier) | Dashboard, POS, see **their own** sales, give discounts up to the shop's limit, print and resend receipts, see invoices, see products, inventory and stores, see and add customers, see reports, see settings |

A biller **cannot**: edit or cancel sales, change prices at the till, give more than the discount limit, handle returns, see other staff members' sales, buy stock, adjust stock, or change settings. The owner can change what the Biller role includes, and can create new roles.

How permissions show on screen:

- Menu entries you are not allowed to use are hidden.
- If you open a page you are not allowed to see (for example by typing its address), you are sent to the Dashboard. If you may not see the Dashboard either, it says **"You do not have access to this page."**
- Inside a page, some buttons are hidden when you lack the permission. Where a button is not hidden, the system refuses when you press it, with a message such as **"You do not have permission for this action"**.
- Role changes take effect within about a minute, without signing out.

**Own sales only.** Staff without **View all sales** see only the sales they made themselves, everywhere: the Sales list, the Dashboard figures, Invoices, receipts and returns. Someone else's sale behaves as if it did not exist.

### When the shop's account is not active

| Account state | What happens |
|---|---|
| **Read-only** (subscription not renewed) | Everything can be looked at, but nothing can be saved: no sales, no changes. Saving shows **"This account is read-only until the subscription is renewed."** The owner can still sign in and pay. **IMPLEMENTED** |
| **Suspended** | Staff are signed out and cannot sign in. Sign-in shows **"This shop is suspended. The owner has been emailed about it."** **IMPLEMENTED** |

### Connection problems

**IMPLEMENTED.** If the system cannot be reached, screens show **"Cannot reach the server. Check your connection and try again."** (the POS has its own message, see section 4). Many errors also appear as a red box in the top-right corner for 5 seconds.

---

## 2. The full journey: from signing in to the reports

This is the path of one ordinary sale through the system. Each step points to the section with the details.

1. **Sign in** (section 2.1) with email and password. You land on the **Dashboard** (section 3).
2. **Open the POS** (section 4). The Walk-in Customer and the first store are already chosen. Tap products to add them to the cart. The system checks there is stock.
3. **Press PAY**, enter the cash received, choose the payment type, optionally type the customer's email, and press **Process Payment**.
4. The system works out the real prices, discount and tax, checks the stock again, gives the sale the next **receipt number**, lowers the stock, and saves the sale. If the customer has an email address, the **receipt email** is sent in the background (section 17).
5. The **receipt** opens on screen. Print it, download it as a PDF, or email it again (section 17). Closing it empties the cart for the next customer.
6. The sale now appears in **Sales** (section 5) and **Invoices** (section 7), counts in the **Dashboard** figures, and appears line by line in the **Sales Report** (section 13).
7. **Edit or cancel ("void")**: there is no button for this on any screen today. The system has the rules for it (section 5), but they can only be used outside the screens. **NOT BUILT** (on screen)
8. **Return**: the customer brings something back. A manager opens **Sales Returns → Add**, picks the sale, enters how many come back, and saves. The money to refund is worked out from what the customer paid, and the goods go back into stock at once (section 6). Once a sale has a return, it can no longer be edited or voided.
9. **Reports**: the Sales Report lists every sold line with what came back and the net amount, and the Customer Report and Customer Due report show sales and money owed per customer (section 13). The Dashboard does not take returns off its figures. **PARTLY IMPLEMENTED**

### 2.1 Signing in

**IMPLEMENTED.**

| Field | Notes |
|---|---|
| Email | Required |
| Password | Required. An eye button shows or hides it |
| Remember me | Keeps you signed in on this device (at most 90 days) and remembers the email |

Button: **Sign In** (shows "Signing in..." while working). After signing in you go to the Dashboard, or to the page you were trying to open.

| Situation | Message |
|---|---|
| Wrong email or password | "The email or password is not correct." |
| Too many wrong attempts | "Too many attempts. Try again in N minutes." From the 5th wrong password in a row the account is locked for 1 minute, then 2, 4, 8 … up to 1 hour |
| Shop suspended | "This shop is suspended. The owner has been emailed about it." |
| No connection | "Cannot reach the server. Check your connection and try again." |

**Forgot password?** opens "Reset your password". Enter the email and press the button. The screen always says: "If <email> has an account here, a reset link is on its way. The link works once and expires soon." (it does not reveal whether the account exists). The link works for 1 hour. Too many requests: "Too many requests. Try again in a few minutes."

**Setting a new password** (from the reset email or an invitation email): enter it twice. Too short: "Use at least 12 characters." Different: "The two passwords are not the same." A used or expired link: "This reset link is not valid any more. Ask for a new one." or "This invitation link is not valid any more. Ask the shop owner to send a new one." Invitation links last 7 days. After a reset, every device signed in to that account is signed out.

**Change password** (from your account menu): current password, new password (at least 12 characters), repeat. Messages: "The current password is not correct.", "Use at least 12 characters.", "The two passwords are not the same.", and on success "Password changed. You were signed out on your other devices."

---

## 3. Dashboard

### Purpose and who can see it

A quick overview of how the shop is doing. Needs **View dashboard** (all three built-in roles have it). **IMPLEMENTED**

Staff without **View all sales** see figures for **their own sales only**. Owners and admins see the whole shop.

### What the screen shows

A greeting, two switch buttons, eight figure cards and two charts.

**Greeting:** "Welcome back, Admin. Overview of your store performance." It always says "Admin", whoever is signed in, because the system does not send the person's name to this screen. **PARTLY IMPLEMENTED**

**Filters:** none on screen. The figures always cover **all time** and **all stores**. (The system can filter by dates, store and staff member, but the screen offers no way to choose them.) **NOT BUILT** (on screen)

**Switch buttons "revenue" / "orders":** change the bar chart between money and number of sales. **IMPLEMENTED**

First row of cards:

| Card | What the number means |
|---|---|
| **Total Revenue** | The sum of the totals of all sales that are not voided, including tax and delivery charges. Returns are **not** taken off |
| **Total Orders** | The number of sales that are not voided |
| **Total Customers** | The number of customers on file, including the Walk-in Customer. Not limited to your own sales |
| **Total Products** | The number of products on file, active and inactive. Not limited to your own sales |

Second row of cards:

| Card | What the number means |
|---|---|
| **Total Amount** | The same figure as Total Revenue |
| **Total Paid** | The money received on those sales |
| **Total Unpaid** | The money still owed on sales that are Unpaid or Partly paid |
| **Total Overdue** | Always ₹0.00. Nothing in the system ever marks a sale as overdue. **PARTLY IMPLEMENTED** |

Charts:

| Chart | What it shows |
|---|---|
| **Revenue Overview** (bar chart) | One bar per calendar month for the last 12 months, including the current month, in the shop's time zone. Months without sales show 0. "revenue" shows the money, "orders" the number of sales |
| **Sales Distribution** (pie chart) | Despite the name, the **top 5 products** by money taken (line totals including tax), one slice each |

**Worked example.** The shop has made three sales: ₹284.38 paid in full, ₹142.19 with nothing paid, and ₹142.19 with ₹50.00 paid. A fourth sale of ₹100.00 was voided.
- Total Revenue = Total Amount = 284.38 + 142.19 + 142.19 = **₹568.76** (the voided sale is left out)
- Total Orders = **3**
- Total Paid = 284.38 + 0 + 50.00 = **₹334.38**
- Total Unpaid = 0 + 142.19 + 92.19 = **₹234.38**
- Total Overdue = **₹0.00**

The system also works out "total returns" and "revenue after returns", but the screen does not show them. **NOT BUILT** (on screen)

### Other outcomes

| Situation | What you see |
|---|---|
| Loading | "Loading dashboard..." |
| Any failure | A red box with the reason, for example "Cannot reach the server. Check your connection and try again." |
| No permission | The Dashboard is hidden from the menu; opening it shows "You do not have access to this page." |
| Shop read-only | The Dashboard works normally (it only shows information) |

### How it connects

Every sale raises Total Revenue, Total Orders and the current month's bar. Voiding a sale removes it from all figures. Returns change nothing here. Purchases, stock adjustments and stock levels are not shown.

---

## 4. POS (selling at the till)

### 4.1 Purpose and who can use it

The till screen for selling to customers. **IMPLEMENTED**

| To do this | You need |
|---|---|
| Open the POS | **Open POS** |
| Put products in the cart | **Sell at POS** |
| Complete the sale (PAY) | **Create sales** |
| Give any discount | **Give discounts** |
| Give a discount above the shop's limit (default 10 %) | **Give discounts** and **Override prices** |
| See the customer list / add a customer | **View customers** / **Create customers** |
| See the store list | **View stores and warehouses** |

The built-in Biller role has all of these except **Override prices**.

If a role can open the POS but lacks **Create sales**, the cashier can fill the cart but PAY fails with "You do not have permission for this action". If it lacks **View customers** or **View stores and warehouses**, the POS shows "Failed to load initial data." **PARTLY IMPLEMENTED** (the screen does not warn in advance)

### 4.2 What the screen shows

Left side, top bar:
- **Store** list: only active stores. The first store is chosen automatically.
- **Customer** list: name and phone. The "Walk-in Customer" is chosen automatically (if no customer has exactly that name, the first customer is chosen).
- **Add New** button: opens "Add New Customer" (section 4.3).
- **Search** box "Search by product name...": filters the products by name. Pressing **Enter** adds the first matching product that is not locked. Searching by SKU does **not** work (the screen looks for a code the products do not carry). **PARTLY IMPLEMENTED**
- **Category** buttons: "All" plus one button per category of the products on sale in this store.

Left side, product tiles: picture, name, price, category. A product that someone is selling right now shows **Locked** and cannot be tapped (section 4.6).

Which products appear: active products that have a stock record in the chosen store, even if the stock there is 0. Inactive and deleted products never appear.

There is no barcode scanning on this screen. **NOT BUILT** (the system can look a product up by barcode or SKU, but the POS does not use it)

Right side, "Current Sale":
- The number of lines, and **Clear** (asks "Clear cart?" / "All items will be removed from the current sale." with **Cancel** and **Clear Cart**).
- "Customer: <name>".
- Each line: picture, name, "₹… each", **−** / quantity / **+**, the line amount, and a bin button. An empty cart shows "Cart is empty" / "Click products to add them to the sale."
- **Subtotal**, **Discount** (a number box, ₹), **Tax** (named after the shop's tax label, for example "GST"), **Total**.
- The big **PAY ₹…** button (greyed out while the cart is empty).

While loading: "Loading products...".

### 4.3 Adding a customer from the POS

**Add New** opens "Add New Customer" with two fields: **Customer Name** and **Phone**, buttons **Cancel** and **Submit**. **PARTLY IMPLEMENTED**

- Both are required: "Customer Name is required", "Phone is required".
- The new customer is added to the list and chosen at once.
- Email, address, city and country cannot be entered here, so a new customer added at the till has no email for the receipt (type it in the payment window instead, section 4.5). They can be added later in Customers.
- Other problems show the system's reason in red at the top of the form.

### 4.4 Building the cart

**Tapping a product** **IMPLEMENTED**
1. If it is already in the cart: **'"<name>" is already in the cart. Use the "+" button to increase quantity.'**
2. Otherwise the system **locks** the product for you in this store (section 4.6). If another cashier has it: **"Product is currently locked by another user."**
3. One unit is added at today's price.
4. If there is no stock in this store: **"Insufficient stock for <name>"**, and nothing is added.

A product with several variants (for example sizes) is always sold as its **first** variant on this screen; there is no way to pick another one. **PARTLY IMPLEMENTED** (the back-office sale form, section 5.4, lets you choose)

**Changing the cart** **IMPLEMENTED**
- **+** adds one. More than the stock seen when the product was added: **'Cannot add more than N units of "<name>" (only N in stock).'**
- **−** takes one away; at zero the line disappears. The bin removes the line.
- The price cannot be changed at the till. **NOT BUILT**
- Changing the **store** keeps the cart. The stock limits shown are still those of the old store, and the sale will be made from the new store. **PARTLY IMPLEMENTED**

**Discount.** Type a money amount for the whole sale in the Discount box. There are no discounts per line and no percentage discounts. **IMPLEMENTED**

### 4.5 Paying

Press **PAY**. The "Finalize Sale" window opens. **IMPLEMENTED**

| Field | Required? | Default | Notes |
|---|---|---|---|
| Total Amount | shown only | the cart total | |
| **Received** | Yes (0 is accepted) | empty | The cash handed over |
| Paying | shown only | the cart total | |
| Change | shown only | 0.00 | Received minus Paying, never below 0 when typed |
| **Payment Type** | Yes | Cash | Credit Card, Cash, Cheque, Deposit, Points |
| Quick Cash | | | Only for Cash: ₹10, 50, 100, 500, 1000, 5000. Tapping one fills Received. (The change shown can then be negative if the note is smaller than the total) |
| Payment Note | No | empty | Up to 1000 characters |
| Sale Note | No | empty | Up to 1000 characters |
| Email the receipt to (optional) | No | the customer's email, if they have one | Must look like an email address |

Buttons: **Cancel** and **Process Payment** (shows a spinner and cannot be pressed twice while working). The window closes when the system answers, whether the sale worked or not.

**Payment always counts as "paid in full".** The POS does not tell the system how much was paid, so every POS sale is saved as **Paid** with nothing owed, even if Received is less than the total. Part payment at the till is **NOT BUILT** (it is possible in the back-office sale form, section 5.4).

The amount received is stored, and the change is worked out by the system as received minus total (never below 0). For card and other payments where Received is 0, no received amount or change is stored.

### 4.6 Product locks

**PARTLY IMPLEMENTED.** A lock stops two cashiers from adding the same product at the same moment.

- Adding a product to the cart locks it **in that store** for **2 minutes**. Other cashiers see **Locked** on its tile.
- The lock is released when the line leaves the cart, the cart is cleared, the store is changed, you leave the POS, or you close the receipt after a sale.
- The POS does not renew the lock while the product sits in the cart. After 2 minutes another cashier can lock and sell it.
- The lock is only a guide for the screens. What really prevents selling stock twice is the stock check when the sale is saved (section 4.8).
- The tiles show the locks as they were when the product list was loaded; the list does not refresh by itself.

### 4.7 How the system works out the price, discount and tax

**IMPLEMENTED.** The cart on screen shows an **estimate**. When you press Process Payment, the system works out the real figures. The receipt always shows the real figures.

1. **Price.** Each line uses the product's current price. (For what happens if the price changed while the product was in the cart, see "Price changed" in section 4.9.)
2. **Line amount** = price × quantity, rounded to the paisa.
3. **Discount.** The sale discount is shared between the lines in proportion to their amounts, before tax. The shares always add up exactly to the discount.
4. **Tax** is worked out per line on the amount after its share of the discount:
   - Product priced **without tax**: tax = amount × tax rate, added on top.
   - Product priced **with tax included**: the tax inside the price is shown, nothing is added.
   - A product with no tax type follows the shop setting "Prices include tax" (off by default).
   - With the GST setting "CGST + SGST" (the default), the tax is split into two halves; with "IGST" it is all IGST.
5. **Total** = lines after discount + tax added on top + delivery charge (there is none at the POS). Delivery is not taxed.
6. Everything is rounded half-up to the paisa.

**Discount rules**
- A discount needs **Give discounts**: else **"You do not have permission to give discounts."**
- Without **Override prices**, the discount may be at most the shop's limit (default **10 %** of the subtotal): else **"Discounts above 10% need the price override permission."** Exactly 10 % is allowed.
- The discount may not be larger than the subtotal: **"Discount cannot be larger than the subtotal."** 100 % is allowed.
- A negative discount: **"Must not be negative."**

**Worked example.** One shirt at ₹120.50, GST 18 %, priced without tax. The cashier gives ₹12.05 discount (exactly 10 %).

| Step | Amount |
|---|---|
| Subtotal | ₹120.50 |
| Discount | − ₹12.05 |
| Amount after discount | ₹108.45 |
| GST 18 % of 108.45 = 19.521 | ₹19.52 (CGST ₹9.76 + SGST ₹9.76) |
| **Total saved on the sale** | **₹127.97** |

Without a discount: 2 shirts = ₹241.00 + GST ₹43.38 (CGST ₹21.69 + SGST ₹21.69) = **₹284.38**.

**The estimate on screen can differ from the real total.** **PARTLY IMPLEMENTED**
- The screen takes the discount off **after** tax. In the example above the screen shows 120.50 + 21.69 − 12.05 = **₹130.14**, while the sale is saved at **₹127.97**.
- For products priced **with tax included**, the screen adds the tax on top again, so it shows more than the real total. Example: one item at ₹118.00 including 18 % GST. The screen shows ₹136.00 (118.00 + 18.00); the real total is ₹118.00.
- The PAY button, the payment window and its change figure use the screen estimate. The receipt uses the real figures.

### 4.8 What happens when you press Process Payment

**IMPLEMENTED.** All of the following happens together. If any step fails, **nothing** is saved: no sale, no stock change, no receipt number used.

1. The system checks the sale (customer, store, products, quantities, prices, discount).
2. It checks there is enough stock **in the chosen store** for every line, and holds those stock figures so two tills cannot sell the same last item.
3. It gives the sale the next **receipt number**: the shop's prefix plus 6 digits, for example **POS-INV-000001**. Numbers have no gaps.
4. It saves the sale as **Completed**, payment status **Paid**, with you as the biller, today's date and time, the customer, the store, the payment type and notes.
5. Each line keeps a copy of the product name, SKU, price, tax and amounts, so later product changes do not change old sales.
6. It **lowers the stock** of each variant in that store and records the movement as "sale" in the stock history.
7. It creates the receipt (version 1).
8. If there is an email address (typed, or the customer's), it queues the receipt email (section 17.5).
9. The receipt opens on screen (section 17.2).
10. In the background, the PDF of the receipt is prepared.

Pressing Process Payment twice, or again after a lost connection, **never makes two sales** for the same cart. **IMPLEMENTED**

### 4.9 Every other outcome at the till

When saving fails, the payment window closes, the cart stays as it was, and the reason appears in a **red bar above the products** (close it with ×).

| Situation | What the cashier sees |
|---|---|
| No answer from the system (connection lost) | "No answer from the server. The sale may not be saved. Press PAY again: it will not be charged twice." Pressing PAY again with the same cart is safe |
| System briefly unavailable | the same message |
| **Price changed** since the product was added, and the cashier cannot override prices | A window **"Prices have changed"**: "Nothing was charged. These prices changed since the items were added. Use the current prices and check the total with the customer before taking payment again." Each line shows the old price crossed out and the new price. **Use current prices** updates the cart; **Cancel** leaves it. Then press PAY again |
| Price changed, and the cashier **can** override prices (owner, admin) | No window. The sale is made at the **old** price and recorded as a price override |
| **Out of stock** (someone sold it meanwhile, or the store was changed) | "Insufficient stock for <SKU>. Available: N, requested: M." |
| Discount without permission | "You do not have permission to give discounts." |
| Discount above the limit | "Discounts above 10% need the price override permission." (with the shop's limit) |
| Discount larger than the subtotal | "Discount cannot be larger than the subtotal." |
| Product made inactive meanwhile | "<name> is inactive and cannot be sold." |
| Store made inactive or deleted meanwhile | "Store not found or inactive." |
| Customer deleted meanwhile | "Customer not found." |
| Invalid receipt email | "Enter a valid email address." (the sale is not saved) |
| Note longer than 1000 characters | "At most 1000 characters." |
| No store or customer chosen | "Select a store and a customer first." |
| Several problems at once | "Validation failed" (the till does not list them) |
| No permission to complete sales | "You do not have permission for this action" |
| Shop read-only | "This account is read-only until the subscription is renewed." Products cannot be added either (the lock is refused) |
| Shop suspended | The cashier is signed out |
| Signed-in session expired | The system renews it silently and carries on; if it cannot, the cashier is signed out |
| Anything unexpected | "Internal server error" |

Other limits: at most 500 lines per sale, at most 1,000,000 units per line, whole units only.

### 4.10 How the POS connects to other sections

- **Stock**: every sale lowers the stock of that store at once (Inventory, section 9).
- **Sales**: the sale appears in the Sales list (section 5) and Invoices (section 7).
- **Dashboard and reports**: the sale counts in the Dashboard (section 3), the Sales Report, Customer Report and Customer Due report (section 13).
- **Customers**: the sale is linked to the chosen customer; the Walk-in Customer is used when no one is named.
- **Settings**: receipt prefix, tax label, tax split, "prices include tax", discount limit, currency and time zone all come from Settings (section 16).
- **Receipt and email**: section 17.

---

## 5. Sales

### 5.1 Purpose and who can use it

The list of all sales (the page is titled **"POS Orders"**), and a back-office form to record a sale without the till, for example a phone order or a sale entered after the fact.

| To do this | You need |
|---|---|
| See the list | **View own sales** (only your own) or also **View all sales** (everyone's) |
| Record a sale (Add Sale) | **Create sales** |
| Change the price of a line | **Override prices** |
| Discounts | as at the POS (section 4.7) |
| Edit a sale | **Edit sales**, but there is no screen (section 5.6) |
| Void a sale | **Void sales**, but there is no screen (section 5.7) |
| Print, download or email a receipt | **View receipts** / **Resend receipts** |

### 5.2 The list ("POS Orders")

**IMPLEMENTED**, with the limits noted.

Columns: tick box, row number, **Customer**, **Reference** (receipt number), **Date**, **Status**, **Grand Total**, **Paid**, **Due**, **Payment Status**, **Biller**, and a **receipt** button that opens the receipt (section 17.2).

Above the list: a search box (matches receipt number and customer name), and three filters: **Status** (All, Completed, Pending), **Payment Status** (All, Paid, Unpaid, Overdue) and **Customer**. Rows per page: 5, 10, 20, 30, 40 or 50. Buttons: **Export to PDF**, **Export to Excel**, and **Add Sale**.

Limits:
- Newest first. The whole list is loaded at once, up to the system's limit for one page. If the shop has more sales than that, the oldest are not shown and the screen does not say so. **PARTLY IMPLEMENTED**
- **Voided sales never appear** in this list. **PARTLY IMPLEMENTED**
- The "Overdue" filter never finds anything (no sale is ever overdue), and there is no "Partial" filter: partly paid sales are only visible under "All". **PARTLY IMPLEMENTED**
- Export to PDF and Excel save the rows currently filtered, but with the system's internal column names, and details such as the customer and the lines do not come out as readable text. **PARTLY IMPLEMENTED**
- The **Add Sale** button is shown even to staff who may not create sales; pressing it then sends them to the Dashboard.
- There are no buttons to edit, void or return a sale from this list. **NOT BUILT**
- Problems loading show "Failed to fetch sales". While loading: "Loading sales...".

### 5.3 Payment status and sale status

**IMPLEMENTED**

| Payment status | When |
|---|---|
| **Paid** | Paid amount is the total or more (anything above the total is not kept: the paid amount is capped at the total). Also a sale with a total of 0 |
| **Partial** | Something was paid, but less than the total |
| **Unpaid** | Nothing was paid |
| Overdue | Shown in filters, but never set by the system. **NOT BUILT** |

**Due** = total − paid.

| Sale status | Meaning |
|---|---|
| **Completed** | The normal state |
| **Pending** | Only a label. Stock is taken and the sale counts in the figures exactly as for Completed. **PARTLY IMPLEMENTED** (nothing in the system treats Pending differently) |
| **Void** | Cancelled (section 5.7). Kept for the records, stock returned, left out of lists and figures |

### 5.4 Recording a sale in the back office (Add Sale)

**IMPLEMENTED.** A form in three steps. Moving between steps keeps everything typed. The bottom bar has **Back**, **Next**, **Cancel** (back to the list) and **Create Sale**.

**Step 1, "Sale Info"**

| Field | Required? | Default | Notes |
|---|---|---|---|
| **Store** | Yes | the only store, if there is just one | Where the goods leave from |
| **Customer** | Yes | Walk-in Customer | |
| **Date** | Yes | today | A past date is allowed (back-dating). Today means "now". A date more than a day ahead is refused |
| **Status** | Yes | Completed | Completed or Pending |
| Note | No | | Up to 1000 characters |

"The reference number is given by the system when the sale is saved."

**Step 2, "Order Items"**: one row per product.

| Column | Notes |
|---|---|
| **Product** | Any product (the list is not limited to the chosen store or to products in stock) |
| **Variant** | Only for products with several variants; must be chosen |
| **Quantity** | Whole number, at least 1 |
| **Unit price** | Filled with the current price. Can be changed only with **Override prices**; otherwise it is greyed out ("Catalog price") |
| Amount | price × quantity |

**Add item** adds a row; × removes one. Below: "Subtotal before tax".

**Step 3, "Payment"**

| Field | Required? | Default | Notes |
|---|---|---|---|
| Payment method | No | Cash | Cash, Card, Bank transfer, UPI, On credit |
| Paid amount (₹) | No | empty | "Leave empty if the customer paid in full." 0 means nothing paid |
| Discount (₹) | No | empty | Same rules and limit as at the POS |
| Shipping (₹) | No | empty | Added to the total, not taxed |

"Tax and the total are worked out when the sale is saved, from each product's tax rate."

The form never shows the total or the tax before saving. **PARTLY IMPLEMENTED**

**Checks before sending** (the form jumps to the step with the problem):
- "Choose the store the goods leave from."
- "Choose a customer."
- "Add at least one product."
- "Every quantity must be at least 1."

**When you press Create Sale**, the system does exactly what it does at the POS (section 4.8): checks, stock check in that store, next receipt number, stock lowered, receipt created, receipt email queued if the customer has an email address. There is no field for a receipt email on this form, so only customers with an email on file get one. Then the list opens. **IMPLEMENTED**

The sale is **not** opened afterwards; to print the receipt, find it in the list and press its receipt button.

**Worked example.** 1 shirt at ₹120.50 (18 % GST on top), shipping ₹40.00, paid ₹100.00:
- Line ₹120.50 + GST ₹21.69 = ₹142.19
- Total = 142.19 + 40.00 = **₹182.19**
- Paid ₹100.00 → **Partial**, Due **₹82.19**

**Changing a price** (with Override prices): the sale uses your price, keeps the normal price alongside it, and records the change in the shop's activity log. Example: shirt sold at ₹100.00 instead of ₹120.50 → ₹100.00 + 18 % = **₹118.00**.

**Other outcomes on this form**: errors appear in a red box at the top, with a list of every problem when there are several. "Prices have changed" opens the same window as at the POS (section 4.9). Out of stock, discount, inactive product, inactive store, future date ("Date cannot be in the future."), no permission, read-only shop and connection problems give the same messages as at the POS. A connection problem keeps the form filled; pressing Create Sale again does not make two sales.

### 5.5 Back-office sale compared with the POS

| | POS | Add Sale |
|---|---|---|
| Choose a variant | No (first variant) | Yes |
| Change a price | No | With Override prices |
| Part payment | No (always Paid) | Yes |
| Shipping | No | Yes |
| Back-dating | No | Yes |
| Status Pending | No | Yes |
| Type a receipt email | Yes | No |
| Receipt opens after saving | Yes | No |
| Product locks | Yes | No |
| Stock check when saving | Yes | Yes |

### 5.6 Editing a sale

**NOT BUILT on screen.** No screen offers it. The system has these rules, tested automatically, for when a screen is added:

- Needs **Edit sales**. Without **View all sales**, only your own sales.
- A voided sale cannot be edited: "A voided sale cannot be edited."
- A sale with any return cannot be edited: "This sale has returns and cannot be edited."
- All lines are replaced. Stock of the old lines goes back to the old store; stock for the new lines comes out of the (possibly new) store. Not enough stock: refused.
- Keeping a line at the price it already had is allowed even if the product's price changed since.
- The receipt number and the **original biller** stay. The person who edited is recorded separately.
- A new version of the receipt is made. **No new email is sent.**
- The change is recorded in the shop's activity log (old and new total, paid amount, store, number of lines).
- If the paid amount is not given again, the old paid amount is kept (but never more than the new total).
- If the customer is not given again, the sale is moved to the **Walk-in Customer**, unlike every other detail, which is kept. **UNSURE** whether this is intended

### 5.7 Voiding (cancelling) a sale

**NOT BUILT on screen.** No screen offers it. The system's rules:

- Needs **Void sales**. Without **View all sales**, only your own sales.
- An optional reason, up to 500 characters ("Reason must be at most 500 characters.").
- A sale with any return cannot be voided: "This sale has returns and cannot be voided."
- Voiding twice does nothing the second time.
- All the sold goods go back into the sale's store, recorded as "sale void" in the stock history.
- The sale is **kept** with status **Void**, the time, who voided it and the reason. It is never deleted.
- A new version of the receipt is made, marked **VOID** (section 17.4). **No email is sent.**
- Recorded in the shop's activity log.
- Afterwards the sale disappears from the Sales list, Invoices, the Dashboard and the Sales Report, and cannot be edited or returned. Opening its receipt shows "VOIDED SALE".

### 5.8 How Sales connects

- Each sale lowers stock (section 9); a void puts it back.
- Sales feed the Dashboard (3), Invoices (7) and Reports (13).
- Returns (section 6) are made against a sale and then block editing and voiding.

---

## 6. Sales Returns

### 6.1 Purpose and who can use it

Recording goods a customer brings back, and the money refunded.

| To do this | You need |
|---|---|
| See the list | **View returns** |
| Record a return | **Create returns** |
| Return goods against someone else's sale | also **View all sales** |
| Return goods **without** a sale | also **Override prices** |

The built-in Biller role has none of these. **IMPLEMENTED**

Unlike sales, the returns list is **not** limited to your own: anyone who can see returns sees all of them.

### 6.2 The list ("Sales Return", "Manage your returns")

**PARTLY IMPLEMENTED.**

Columns: tick box, row number, **Product** (picture and name of the first returned item), **Date**, **Customer**, **Status**, **Total**, **Paid**, **Due**, **Payment Status**, and a **View** button.

Search "Search sales returns..." (product and customer name), filters **Customer**, **Status** (All, Received, Pending) and **Payment Status** (All, Paid, Unpaid, Overdue). Buttons: **Export to PDF**, **Export to Excel**, **Add Sales Return**.

Limits:
- The **Date** is shown in the system's raw format (for example 2026-10-29T10:15:00Z) instead of a normal date.
- The Status filter offers "Received", which no return ever has. Returns are either **Completed** or **Pending**, so completed returns cannot be filtered. "Overdue" also never matches, and there is no "Partial".
- **View** opens a box "View Item" listing the first 12 stored details with their internal names. There is no proper return detail page and no printout for a return. **NOT BUILT**
- Returns cannot be edited or deleted. **NOT BUILT**
- Problems loading show "Failed to fetch sales returns".

### 6.3 Return linked to a sale (the normal case)

**IMPLEMENTED.** "Add Sales Return" opens a form in three steps: **Sale**, **Items**, **Refund**, with **Back**, **Next**, **Cancel** and **Create Return**.

**Step 1, "Sale" ("What is returned")**
- "From a sale" / "Without a sale" (the second choice is only shown with **Override prices**).
- Search box "Search by receipt number or customer". It shows the 20 most recent sales, or those whose receipt number matches. Each shows receipt number, customer, date and total. Voided sales are not shown. "No sales found." when nothing matches.
- Pick one: "Selected: <receipt number>, <customer>, <store>".
- **Date** (default today), **Status** (default Completed; or Pending), **Note** (up to 1000 characters).

**Step 2, "Items" ("Quantities and reasons")**: one row per line of the sale: Item and SKU, **Price paid**, **Sold**, **Returned before**, **Return now** (0 up to what is left), **Reason** (optional, up to 200 characters). Lines already fully returned show "All returned".

**Step 3, "Refund" ("Money back")**
- "Items back, before tax: ₹…" (an estimate: quantity × price paid).
- "The refund, including tax, is worked out when the return is saved, from the prices on the original sale."
- **Refunded now (₹)**: "Leave empty if the full refund is paid out now."

**Checks before sending**
- "Choose the sale the goods come from."
- "Enter how many of at least one item come back."
- "Only N of <name> can still be returned."

**When you press Create Return**
1. The system checks the sale still exists, is yours (or you can see all sales), and is not voided.
2. It checks that, counting every earlier return, no more comes back than was sold. If someone returned items of the same sale meanwhile: "Cannot return N of <SKU>: X sold, Y already returned." The form adds "The quantities that can still come back are updated below.", reloads the sale and goes back to step 2.
3. It works out the refund from **what the customer actually paid** on each line, ignoring today's prices: each unit's share of the line's discount, tax and total. If a line is returned in parts, the parts always add up exactly to what was charged.
4. Store and customer are always those of the sale.
5. The goods go back into **the sale's store** at once, whatever the status, recorded as "return" in the stock history.
6. The return gets the next return number: **RET-000001**, RET-000002 … (no gaps).
7. Refund status: **Paid** if "Refunded now" is empty or covers the refund, **Partial** if less, **Unpaid** if 0.
8. Recorded in the shop's activity log. The list opens.
9. From now on, the sale can no longer be edited or voided.

No receipt, credit note or email is produced for a return. **NOT BUILT**

**Worked example.** A sale of 3 units at ₹120.50 each, 18 % GST, with ₹30.00 discount on the sale.
- Line: 3 × 120.50 = ₹361.50, less ₹30.00 = ₹331.50, GST ₹59.67, line total **₹391.17**.
- The customer returns **1** unit: refund = one third of the line, rounded: discount share ₹10.00, GST ₹19.89, **refund ₹130.39**.
- Later returns the other **2**: refund = ₹391.17 − ₹130.39 = **₹260.78**. Together they refund exactly ₹391.17.
- Stock goes up by 1, then by 2.

### 6.4 Return without a sale

**IMPLEMENTED** (with **Override prices** only). For goods bought before the system was in use, or when the sale cannot be found.

- Step 1: **Customer** and **Store** (both required on the form: "Choose the customer and the store."), date, status, note.
- Step 2: one row per product: **Product**, **Variant** (when there are several), **Quantity** (at least 1), **Refund per unit**, **Reason**. **Add item** / ×. "Add at least one product." if none.
- The refund is **refund per unit × quantity**. **No tax** is worked out or shown separately.
- Without the permission: "A return without a sale needs the price override permission."
- Missing refund per unit (only possible outside this form): "unitPrice is required for a return without a sale."

### 6.5 Other outcomes

| Situation | Message |
|---|---|
| Sale not found, or someone else's sale | "Sale not found." |
| Sale voided | "A voided sale cannot be returned." |
| More than what is left | "Cannot return N of <SKU>: X sold, Y already returned." |
| Product not on the sale | "This product is not on the sale." |
| Quantity not a whole number above 0 | "Quantity must be a whole number above 0." |
| Reason longer than 500 characters | "At most 500 characters." (the form stops at 200) |
| No permission, read-only shop, connection problem | as elsewhere (section 1) |

After a connection problem, pressing Create Return again does not record the return twice.

### 6.6 How returns connect

- **Stock** goes up at once in the sale's store (Inventory, section 9).
- **Sales**: the sale shows how much was returned per line; it can no longer be edited or voided.
- **Sales Report**: "Returned" quantity and amount, and "Net" per line (section 13.1).
- **Dashboard**: returns are **not** taken off Total Revenue. **PARTLY IMPLEMENTED**
- **Customer Due**: refunds are not taken off what a customer owes. **UNSURE** (see section 13.4)
- **Receipt**: the original receipt does not change after a return.

---

## 7. Invoices

### 7.1 Purpose and who can see it

A money-focused list of the same sales: what was charged, paid and still owed. Needs **View invoices** (the Biller role has it). Without **View all sales**, only your own sales. **PARTLY IMPLEMENTED**

### 7.2 What the screen shows

Columns: tick box, row number, **Invoice No** (the receipt number), **Customer** (with picture), **Due Date**, **Amount**, **Paid**, **Amount Due**, **Status** (the payment status).

Search "Search invoices..." (customer and invoice number), filters **Customer** and **Status** (All, Paid, Unpaid, Overdue). Export to PDF and Excel. No Add button: invoices are not created here, every sale is an invoice.

Limits:
- **Due Date** is the **sale date**. There are no payment terms or real due dates. **NOT BUILT**
- The invoice number looks like a link but opens nothing. **NOT BUILT**
- Voided sales are left out.
- "Overdue" never matches; there is no "Partial" filter.
- A customer without a picture shows an empty picture frame.
- There is no way to record a later payment against an invoice. **NOT BUILT**
- Loading problems: "Failed to fetch invoices". While loading: "Loading invoices...".

### 7.3 How it connects

Every sale appears here at once. The Customer Due report (section 13.4) reads the same figures.

---

## 8. Products

### 8.1 Purpose and who can use it

The catalogue of what the shop sells: names, prices, tax, variants and opening stock.

| To do this | You need |
|---|---|
| See the list | **View products** (the Biller role has it) |
| Add a product | **Create products** |
| Edit a product | **Edit products** |
| Delete a product | **Delete products** |
| Import products from a spreadsheet file | **Import products** |

The **View**, **Edit** and **Delete** buttons on each row are greyed out without the matching permission. The number of products may be limited by the shop's plan.

### 8.2 The list ("Product List")

**IMPLEMENTED.** Columns: tick box, row number, **Product Name** (with picture), **Category** ("Unknown" if none), **Brand**, **Price** (of the first variant), **Unit**, **Qty** (stock of all variants in all stores together), **Created By**, and View / Edit / Delete.

Search matches the product name and the SKU of the first variant. Filters: **Category** and **Brand**. Buttons: Export to PDF, Export to Excel, **Import** (only with Import products), **Add Product**.

Deleted products are not listed. Inactive products are listed.

### 8.3 Adding a product (Add Product)

**PARTLY IMPLEMENTED — saving does not work today.**

The form has four steps: **Product Info**, **Pricing & Stock**, **Images**, **Custom Fields**, with **Back**, **Next**, **Cancel** and **Add Product** (on the last step only).

Each step only keeps its own boxes on the page. When **Add Product** is pressed on the last step, only the last step's boxes are sent, so the name, store, type, SKU and price typed on earlier steps never reach the system. The system answers **"Product name is required."** (shown in the red box in the top-right corner) and nothing is saved. This was confirmed by sending the system exactly what the form sends. Until this is fixed, new products can only be added with **Import** (section 8.5).

What the form asks for, as built:

| Step | Field | Marked required | Notes |
|---|---|---|---|
| Product Info | Store (with **+** to create a store) | Yes | Where the opening stock goes |
| | Supplier (with **+**) | | |
| | Product Name | Yes | |
| | Slug | | Used by the public shop address |
| | SKU, with **Generate** | Yes | Generate makes "SK" plus a random number |
| | Selling Type | Yes | Online or POS. Stored, but the POS shows products of both types |
| | Category (with **Add New**, which leaves the form) | Yes | |
| | Sub Category (with **Add New**) | Yes | |
| | Brand, Unit | shown with a red star, but not enforced | |
| | Barcode Symbology | | Code 128, Code 39, UPC-A. Stored only; no barcode labels are printed |
| | Item Barcode, with **Generate** | | |
| | Description | | |
| Pricing & Stock | Product Type | Yes | **Single Product** or **Variable Product** |
| | Quantity, Price | Yes (single) | Opening stock in the chosen store, and the price |
| | Tax Type | | Exclusive (tax on top) or Inclusive (tax in the price) |
| | Tax | | Only **IGST (8%)**, **GST (5%)**, **SGST (4%)**. Common rates such as 12 %, 18 % and 28 % cannot be chosen here (they can in Edit and Import) |
| | Discount Type, Discount | | Percentage or Fixed. **Stored but never used**: sales do not apply a product's own discount |
| | Quantity Alert | | The low-stock level |
| | Variant table (variable only) | | Two example rows are filled in ("Color / Red / FD001-RED / 10 / 7.99" and a second one); there is no working way to add or remove rows |
| Images | Upload Product Images | | "PNG, JPG, GIF up to 10MB". Only the first picture is used |
| Custom Fields | Warranties, Manufacturer, Expiry tick boxes; Warranty, Manufacturer, Manufactured Date, Expiry Date | | |

**System rules for a new product** (they apply to Import too):
- "Product name is required." Name up to 200 characters.
- "SKU is required." SKU up to 64 characters, and unique in the shop: "A product with this SKU already exists."
- Barcode: letters, digits and . _ / + − only; unique: "A product with this barcode already exists."
- Price required, 0 or more.
- Tax between 0 and 100.
- A single product has exactly one variant; a variable product at least one and at most 200.
- The opening stock is recorded as "opening" in the stock history.

### 8.4 Editing a product

**IMPLEMENTED** (with limits). **Edit** opens "Edit Product" in a window, button **Save Product**.

Fields: **Product Name** (required), **Status** (Active / Inactive, required), **Category**, **Brand**, **Unit** (all three marked required), Selling Type, Slug, Description, **Tax Type**, **Tax** (any number from 0 to 100), Discount Type, Discount Value, Barcode Symbology, Product Image.

For **single** products also: SKU, Item Barcode, **Price**, **Store** (required), **Quantity**, **Quantity Alert**.

For **variable** products: "This product uses multiple variants. Only core product details can be edited here." Variant prices, SKUs and stock cannot be changed on any screen. **PARTLY IMPLEMENTED**

What saving does:
- A **price change** is recorded in the shop's activity log. Sales already made keep their old price.
- **Quantity** is **set** to the number typed for that store (not added), recorded as "product edit" in the stock history. For example, stock 7 and you type 10: stock becomes 10.
- The form sends back the stock of every store as it was when the form opened. If a sale happens while the edit window is open, saving can put the stock back to the earlier figure. **UNSURE** (follows from the rules; not tried)
- Making a product **Inactive** removes it from the POS at once. It stays in old sales and reports.

### 8.5 Importing products from a file

**IMPLEMENTED.** **Import** opens a window:
- "One row per product (or per variant: rows with the same name become variants). Required columns: name, sku, price. Categories, brands, units and stores must already exist. Existing SKUs or barcodes are not changed."
- "All rows are imported or none: any error stops the whole file. At most 5,000 rows."
- **Download the sample file** gives a file with the columns: name, sku, price, cost, barcode, category, sub_category, brand, unit, tax_type, tax_rate, hsn_code, store, opening_qty, quantity_alert, status.
- **Choose a CSV file** (at most 5 MB: "The file can be at most 5 MB."; none chosen: "Choose a CSV file first.").
- **Check file** tests without saving: "The file is fine: N products (M variants) would be created. Nothing was saved yet."
- **Import** saves: "Imported N products (M variants)." Then **Close**.
- On any problem: "Nothing was imported. Fix these rows and try again:" followed by "Row N, column: problem" for each (at most 200 listed). Examples: "name is required (max 200 characters)", "a product with this SKU already exists", "sku repeats row 3", "must be a number >= 0", "must be Exclusive or Inclusive", "must be between 0 and 100", "unknown category", "unknown store (use the store slug)", "store is required with opening_qty or quantity_alert".
- Too many products for the plan: "Your plan does not allow this many products."

### 8.6 Viewing and deleting

- **View** shows "Product Details": picture, name, status, brand, unit, selling type, SKU, price, quantity, created by, description. **IMPLEMENTED**
- **Delete** asks "Are you sure you want to delete …?" with **Cancel** / **Delete**. **IMPLEMENTED**
  - A product that still has stock anywhere cannot be deleted: "A product that still has stock cannot be deleted. Set its stock to 0 first."
  - Deleting hides the product everywhere, removes it from the public shop and releases any POS locks. Past sales keep their copy of the name and price.

### 8.7 How products connect

- **POS** shows active products that have a stock record in the chosen store (section 4).
- **Price, tax type and tax rate** decide sale prices (section 4.7). The product's own discount does not.
- **Stock** lives in Inventory (section 9) and changes with sales, returns, purchases and adjustments.
- **Product Showcase** decides which products the public shop shows (section 14).

---

## 9. Inventory

### 9.1 Purpose and who can use it

How many units of each variant are in each store, and the low-stock level.

| To do this | You need |
|---|---|
| See stock | **View inventory** (the Biller role has it) |
| Set a stock figure by hand | **Edit inventory** (the Edit button is greyed out without it) |

**IMPLEMENTED**

### 9.2 What the screen shows ("Inventory")

One row per variant per store. Columns: tick box, row number, **Product** (with picture), **SKU**, **Variant** (for example "Size: M"), **Store**, **Qty**, **Alert At**, **Price**, **Stock Status**, and **Edit**.

Above the list: search "Search product, SKU or store…", filters **Store** and **Stock Status** (All, In Stock, Low Stock, Out of Stock), and counters: **Total SKUs**, **In Stock**, **Low Stock**, **Out of Stock**. Export to PDF and Excel. Most recently changed first.

| Stock Status | When |
|---|---|
| **Out of Stock** | 0 units (or fewer) |
| **Low Stock** | more than 0 but at or below the "Alert At" level |
| **In Stock** | above the "Alert At" level |

Example: Alert At 5. Qty 0 → Out of Stock; Qty 3 → Low Stock; Qty 5 → Low Stock; Qty 6 → In Stock.

There are no low-stock notifications or emails; the status is only shown here. **NOT BUILT**

### 9.3 Setting stock by hand (Edit)

**IMPLEMENTED.** **Edit** shows the product, SKU, store and variant, and two fields:

| Field | Required? | Notes |
|---|---|---|
| **Quantity** | Yes | 0 or more. This **replaces** the current figure |
| **Low-Stock Alert Threshold** | No | 0 or more |

Buttons **Cancel** and **Save** ("Saving…" while working).

What saving does: the stock becomes exactly the number typed; the difference is written to the stock history as a manual change, and the old and new figures are recorded in the shop's activity log. Example: stock 7, you type 10 → stock 10, history shows +3.

Messages: a quantity below 0 or above 1,000,000,000 is refused ("qty must be between 0 and 1000000000"); a product or store deleted meanwhile: "Variant or store not found."; other problems show the system's reason, or "Failed to update inventory".

For a recorded reason, use Stock Adjustments (section 10) instead: it adds or removes an amount with a reason.

### 9.4 Other outcomes

Loading: "Loading inventory…". Failure: "Failed to load inventory". Read-only shop: Save is refused with the read-only message.

### 9.5 How stock changes (all sections)

| What happens | Effect on stock | Shown in Stock history as |
|---|---|---|
| Product created (form or import) with an opening quantity | sets the starting figure | Opening stock / Import |
| Sale at the POS or in the back office | goes **down** in the sale's store | Sale |
| Sale edited (no screen) | old lines back, new lines out | Sale edited |
| Sale voided (no screen) | goes back **up** | Sale voided |
| Sales return | goes **up** in the sale's store | Return |
| Purchase **received** | goes **up** in the purchase's store | Purchase received |
| Stock adjustment | up or down by the amount | Adjustment |
| Edit on this screen | set to the figure typed | manual_set (shown in the system's own words) |
| Quantity changed in Edit Product | set to the figure typed | product_edit (shown in the system's own words) |

Stock can never go below 0: sales and adjustments that would do so are refused.

---

## 10. Stock Adjustments

### 10.1 Purpose and who can use it

Adding or removing stock with a reason (damage, count corrections, theft …), and the full stock history.

| To do this | You need |
|---|---|
| See the adjustments list | **View stock adjustments** |
| Make an adjustment | **Create stock adjustments** (the **New adjustment** button is hidden without it) |
| See the **Stock history** tab | **View inventory** |

The Biller role has none of the adjustment permissions. **IMPLEMENTED**

### 10.2 What the screen shows

A store filter ("All stores" or one store), **New adjustment**, and two tabs:

- **Adjustments**: columns Date, Product, Store, Reason, By, Change (+ green / − red), Stock after. 50 per page with page buttons. "Nothing here yet." when empty.
- **Stock history**: "Every change to stock: sales, returns, purchases, adjustments and imports." Columns Date, Product, Store, Reason, Change, Stock after. Reasons are shown as Sale, Sale voided, Sale edited, Return, Purchase received, Adjustment, Import, Opening stock; hand-set figures appear as "manual_set" and "product_edit". **PARTLY IMPLEMENTED**

### 10.3 Making an adjustment ("New stock adjustment")

| Field | Required? | Default | Notes |
|---|---|---|---|
| **Store** | Yes | the filtered store, or the only store | |
| **Product** | Yes | | |
| **Variant** | Yes, for products with several | | |
| **Change** | Yes | **Remove** | Remove or Add, and a quantity (whole number, at least 1, default 1) |
| | | | The window shows "In stock now: N. After this: M", and "(not possible)" if M would be below 0 |
| **Reason** | Yes | Stock count correction | Stock count correction, Damaged, Expired, Lost or stolen, Returned to supplier, Found, Other |
| Other reason | Yes when Reason is Other | | Up to 200 characters |
| Note (optional) | No | | Up to 500 characters |

Buttons **Cancel** and **Save adjustment** (greyed out while the result would be below 0).

Messages:
- Something missing: "Choose a store and a product, a quantity of at least 1 and a reason."
- Not enough stock to remove (for example someone sold it meanwhile): "There is not that much in stock to remove."
- "Store not found.", "Product variant not found.", "Quantity must be a whole number above 0."

What saving does: the stock changes by the amount, the adjustment is listed with who made it, the stock history shows "Adjustment", and the shop's activity log records it. Adjustments cannot be edited or deleted afterwards; to undo one, make an opposite one. **IMPLEMENTED**

**Example.** 12 units in stock, 2 are damaged: Remove, 2, Damaged → "In stock now: 12. After this: 10". After saving, stock is 10.

### 10.4 How it connects

Changes Inventory (section 9) and the "In stock" column of the Sales Report. "Returned to supplier" here only removes stock; it does not touch Purchases or the supplier.

---

## 11. Purchases (the shop buying stock)

### 11.1 Purpose and who can use it

Recording stock bought from suppliers. A purchase **raises stock only when it is "Received"**.

| To do this | You need |
|---|---|
| See the list | **View purchases** |
| Record a purchase | **Create purchases** |
| Record it as **Received** (stock goes in) | also **Approve and receive purchases** |
| Change status or payment later | **Edit purchases**, but there is no screen for it (section 11.5) |

The Biller role has none of these. **IMPLEMENTED**

### 11.2 The list ("Purchases", "Manage your purchases")

**PARTLY IMPLEMENTED.** Columns: tick box, row number, **Supplier Name**, **Reference**, **Date**, **Status**, **Total**, **Paid**, **Due**, **Payment Status**, and **View**. Search "Search purchases...", filter **Payment Status** (All, Paid, Unpaid, Overdue; no "Partial"). Export to PDF and Excel, **Add Purchase**.

**View** shows the raw stored details ("View Item"), not a proper purchase page. There is no edit, delete, receive or cancel button. **NOT BUILT**

### 11.3 Recording a purchase (Add Purchase)

**IMPLEMENTED.** Two steps: **Purchase Info** and **Purchase Items**, with **Back**, **Next**, **Cancel** and **Add Purchase** (greyed out without Create purchases).

**Step 1, "Purchase Info"**

| Field | Required? | Notes |
|---|---|---|
| **Reference** | Yes on the form | The supplier's invoice number. Up to 60 characters. Must be unique per supplier: "This supplier already has a purchase with reference X." (If left empty outside this form, the system numbers it PUR-000001, PUR-000002 …) |
| **Date** | Yes | Today or earlier; more than a day ahead is refused |
| **Supplier** (with **Add New**) | Yes | |
| **Status** | Yes | **Pending**, **Ordered**, and **Received** (Received only appears with Approve and receive purchases) |
| Total Amount | shown only | Worked out from the lines |
| **Paid Amount** | No | 0 if empty. More than the total is refused: "Paid cannot be more than the total." |
| Due Amount | shown only | Total − Paid |
| **Payment Status** | Yes | Unpaid, Paid, Overdue. See the rule below |

**Step 2, "Purchase Items"**: one row per line: **Product**, **Variant** ("No variants available for this product" if none), **Store** (where the stock goes; each line can go to a different store), **Quantity**, **Unit Price** (the cost price you paid), line **Total**, and a remove button; plus a button to add a line.

**How the system works out the figures**
- Line total = quantity × unit price (rounded), minus a line discount (the form has no discount box, so this is always 0 from the screen).
- Purchase total = sum of the lines. **No tax** is worked out on purchases. **NOT BUILT**
- Payment status is decided by the amounts, not by the choice in the form:
  - Paid amount = total → **Paid**
  - Paid amount between 0 and the total → **Partial**
  - 0 → **Unpaid**
  - "Overdue" is the only choice the system keeps (when not fully paid).
  - So choosing **Paid** without typing the paid amount saves the purchase as **Unpaid**. **PARTLY IMPLEMENTED**

**Example.** 10 shirts at ₹80.00 cost into the Main store, 5 caps at ₹40.00 into the same store. Total = 800.00 + 200.00 = **₹1,000.00**. Paid ₹600.00 → **Partial**, due **₹400.00**. If the status is **Received**, shirt stock rises by 10 and cap stock by 5 at once.

**When you press Add Purchase**
1. The system checks the supplier, products, variants and stores (active stores only).
2. Saves the purchase with the totals above.
3. If the status is **Received**: raises the stock of each line in its store, shown as "Purchase received" in the stock history, and records who received it and when.
4. Records it in the shop's activity log, then opens the Purchases list.

Pressing the button twice cannot record the same purchase twice, because the reference must be unique per supplier: the second attempt is refused with "This supplier already has a purchase with reference X." **IMPLEMENTED** (the general double-press protection used by sales is not switched on by this form)

### 11.4 Messages and other outcomes

| Situation | Message |
|---|---|
| No supplier | "supplierId is required." |
| Supplier deleted | "Supplier not found." |
| No lines | "At least one purchase item is required." |
| Quantity not a whole number above 0 | "Quantity must be a whole number above 0." |
| No unit price | "unitPrice is required." |
| Store inactive or deleted | "Store not found or inactive." |
| Product with several variants but none chosen | "Choose a variant for this product." |
| Received without the approve permission | "Receiving a purchase needs the approve permission." |
| Paid more than the total | "Paid cannot be more than the total." |
| Several problems | "Validation failed" |

Errors appear in the red box in the top-right corner; the form stays filled. No permission, read-only shop and connection problems as in section 1.

### 11.5 Receiving, cancelling and paying later

**NOT BUILT on screen.** The system has these rules, but no screen uses them:
- Change the status, the paid amount, the payment status and the note.
- Moving to **Received** raises stock at that moment (needs Approve and receive purchases).
- A received purchase cannot change status again: "A received purchase cannot change status. Correct stock with a stock adjustment."
- A cancelled purchase cannot be reopened: "A cancelled purchase cannot be reopened."

So today, a purchase saved as **Pending** or **Ordered** can **never** be received from the screens, and its stock never arrives. To get stock in, record the purchase as **Received** straight away.

There are no purchase returns to suppliers (use a Stock Adjustment "Returned to supplier", which only removes stock). **NOT BUILT**

### 11.6 How purchases connect

- **Inventory**: a received purchase raises stock (section 9).
- **Suppliers**: each purchase belongs to one supplier (section 12.4).
- **Purchase Report**: lists purchases (section 13.2).
- The product's sale price does not change with the purchase price; the cost price on the product is not updated either.

---

## 12. Master data

Lists that other sections pick from. They all work the same way unless noted:

- A list with search, an **Active / Inactive** status filter, Export to PDF and Excel, and an **Add** button.
- **Edit** opens a small window with a **Save** button; problems show in red inside it ("Something went wrong. Please try again." if there is no reason).
- **Delete** asks "Are you sure you want to delete …?". Deleting **hides** the record; it is never removed from past documents, which keep showing its name.
- Problems while adding appear only in the red box in the top-right corner; the form stays open.
- Names are trimmed. Two categories, two brands or two units cannot have the same name (for example "Category with this name already exists."). Suppliers and customers may share names.
- **Status (Active / Inactive)** is stored and shown, but for categories, brands, units, suppliers and customers nothing else changes: inactive ones can still be chosen on every screen. **PARTLY IMPLEMENTED**

### 12.1 Categories

**IMPLEMENTED.** Permissions: View / Create / Edit / Delete categories (the Biller role has none; the POS builds its category buttons from the products, so cashiers do not need them).

- List: Category, Image, Status, Created By, Edit, Delete. "Search categories...".
- **Add Category** (a separate page): **Category Name** (required; "Category name is required.", up to 120 characters), **Status** (Active by default), **Image** (optional). Button **Add Category**, then back to the list.
- Edit: Category Name, Status, Category Image; **Save Category**.
- **Sub-categories** can only be created from the Add Product form (**Add New** next to Sub Category): name ("Sub-category name is required.") and its category ("Category is required."). They cannot be listed, edited or deleted anywhere. **PARTLY IMPLEMENTED**
- Deleting a category that products use is allowed; those products keep showing the old category name. **IMPLEMENTED** (no warning)
- Connects to: Products, POS category buttons, Sales Report "Category" column, public shop.

### 12.2 Brands

**IMPLEMENTED.** Permissions: View / Create / Edit / Delete brands.

- List: Brand (with picture), Status, Created By, Created Date, Edit, Delete. "Search brands...".
- **Add Brand**: **Brand Name** (required, up to 120 characters), Status, Image. Edit: Brand Name, Status, Brand Image; **Save Brand**.
- Connects to: Products, Sales Report (shown under the product name; "No Brand" when empty).

### 12.3 Units

**IMPLEMENTED.** Permissions: View / Create / Edit / Delete units.

- List: Unit, Short Name, Status, Created By, Created Date, Edit, Delete. "Search units...".
- **Add Unit**: **Unit name** ("Unit name is required.", up to 60 characters), **Short name**, Status. Edit: Unit Name, Short Name (both required), Status; **Save Unit**.
- Units are labels only: quantities are always whole numbers, so selling by weight or length (for example 1.5 kg) is not possible. **NOT BUILT**

### 12.4 Suppliers

Permissions: View / Create / Edit / Delete suppliers.

- List: Image, Name, Email, Phone, Address, Status, View / Edit / Delete (greyed out without the permission). "Search suppliers...". **IMPLEMENTED**
- **Add Supplier**: two steps, **Supplier Info** (Supplier Name, Email, Phone, Address, Status, all marked required) and **Profile Image**, button **Add Supplier**. **PARTLY IMPLEMENTED — saving does not work today**: as with Add Product, only the last step's boxes are sent. Tried against the system: with no picture it answers "Validation failed"; with a picture, the supplier name is missing. Nothing is saved.
- Suppliers can still be created with **Add New** on the Add Purchase form or **+** on the Add Product form.
- Edit: Supplier Name (required), Email, Phone, Address, Status, Profile Image; **Save Supplier**. **IMPLEMENTED**
- System rules: name required ("Supplier name is required.", up to 120 characters); email optional but must be valid; phone up to 40 characters.
- Connects to: Purchases (each purchase has one supplier), Products (an optional supplier). There is no supplier balance or statement. **NOT BUILT**

### 12.5 Customers

Permissions: View / Create / Edit / Delete customers (the Biller role can view and create).

- List: Image, Name, Email, Phone, City, Country, Status, View / Edit / Delete. "Search customers...". The buttons are not greyed out without permission; the system refuses instead. **IMPLEMENTED**
- **Add Customer** (from Customers): two steps, **Customer Info** (Customer Name, Email, Phone, Address, City, Country, Status, all marked required) and **Profile Image**, button **Add Customer**. **PARTLY IMPLEMENTED — saving does not work today**, for the same reason as Add Supplier (tried: "Validation failed" without a picture, "Customer name is required." with one).
- Customers **can** be added from the POS (**Add New**: name and phone only, section 4.3). This works.
- Edit: Customer Name (required), Email, Phone, Address, City, Country, Status, Profile Image; **Save Customer**. **IMPLEMENTED**
- System rules: name required ("Customer name is required.", up to 120 characters); email optional but must be valid; phone up to 40 characters.
- The **Walk-in Customer** cannot be deleted: "The walk-in customer cannot be deleted."
- A customer's **email** is used for receipt emails (section 17.5).
- Connects to: POS and sales, returns, Invoices, Customer Report, Customer Due, Dashboard "Total Customers". There is no loyalty, credit limit or customer statement. **NOT BUILT**

### 12.6 Stores

**PARTLY IMPLEMENTED.** Permissions: **View / Create / Edit / Delete stores and warehouses**.

- There is **no Stores page** in the menu. A store can only be **created** from the Add Product form (**+** next to Store): "Create New Store" with **Store Name** (required), Address, Status (Active by default), and **Create Store**. (Since saving a new product does not work today, the new store is still kept: it is saved by its own window.)
- The store's public details (phone, description, logo, banner, opening hours) are edited in **Product Showcase** (section 14).
- A store cannot be renamed, deactivated or deleted from any screen. The system's rules exist: name required ("Store name is required."), and "The last store cannot be deleted."
- The number of stores may be limited by the plan.
- Only **active** stores appear in the POS, sale, purchase and adjustment forms.
- Each new shop starts with one store, called "Main Store".
- Connects to: every sale, return, purchase line and stock figure belongs to one store.

### 12.7 Warehouses

**NOT BUILT on screen.** The system can keep a list of warehouses (name, address, status), but no screen shows or creates them, and warehouses cannot hold stock: all stock belongs to stores. There are no transfers between stores or warehouses. **NOT BUILT**

---

## 13. Reports

All four reports need **View reports** (the Biller role has it). Staff without **View all sales** see only their own sales in the sales-based reports. The **Excel** and **PDF** buttons on the Sales and Purchase reports need **Export reports** (hidden without it) and a plan that includes report exports ("Report export is not in your plan."). These files are made by the system and include every row that matches the dates and store chosen. The other export buttons (the PDF and Excel icons, "Generate PDF") save what is on screen, with the system's internal column names. **PARTLY IMPLEMENTED**

### 13.1 Sales Report

**IMPLEMENTED.** "Sold lines with returns netted. Voided sales are left out."

- One row per sold line: **Date**, **Receipt**, **SKU**, **Product** (with brand), **Category**, **Customer**, **Store**, **Sold** (quantity), **Returned** (quantity), **Sold amount**, **Net**, **In stock**.
- Cards above (worked out from the rows on screen): **Sold**, **Returned**, **Net sales**, **Items sold**.
- Filters: **Choose Date** (from / to, whole days in the shop's time zone), **Store**, **Product**; search "Search receipt, product, customer...". Buttons: Excel, PDF, Print, and the PDF / Excel icons.
- Sold amount = the line total the customer was charged, after its share of the discount and **including tax**. Returned = what the returns of that line refunded. Net = Sold amount − Returned.
- **In stock** is today's stock of that variant in that store, not the stock on the sale date.
- Pending sales are included like Completed ones.

**Example.** The 3 shirts from section 6.3: Sold 3, Returned 1, Sold amount ₹391.17, Net ₹260.78.

### 13.2 Purchase Report

**IMPLEMENTED.** "Purchased lines. Total shown: ₹…. Cancelled purchases are left out."

- Also needs **View purchases**; without it: "The purchase report needs the purchases view permission." (The Biller role sees the report in the menu but gets this message.) **PARTLY IMPLEMENTED**
- One row per purchase line: Date, Reference, SKU, Product, Category, Supplier, Store, Status, Qty, Amount, In stock.
- Filters: Choose Date, Supplier, Store, Product, search. Excel, PDF, and the icon exports.
- **Pending and Ordered purchases are included** even though their stock never arrived; use the Status column to tell them apart.

### 13.3 Customer Report

**PARTLY IMPLEMENTED.** Built on the screen from the sales lines.

- One row per customer: **Last receipt**, **Customer**, **Sales** (number of sales), **Net amount** (sales after returns, including tax), **Payment Method**, **Status**.
- **Payment Method** always shows "-" and **Status** always "Completed". The Payment Method filter (Cash, Paypal, Stripe, Credit Card) therefore hides every row when used; the Payment Status filter only matches "Completed".
- The date filter keeps or drops a customer by the date of their **last** sale; the totals are always for all time.
- Buttons: Generate PDF, Print, and the icon exports.

### 13.4 Customer Due

**PARTLY IMPLEMENTED.** Built on the screen from the Invoices list.

- One row per **sale** (not per customer) that still has money owed: **Reference**, **Customer**, **Total Amount**, **Paid**, **Due**, **Status**.
- POS sales never appear (they are always fully paid). Only back-office sales with a part or no payment appear.
- Refunds from returns are **not** taken off the amount due. **UNSURE** whether intended.
- The **Payment Method** filter hides every row when anything other than "All" is chosen. The date filter has no effect unless a future date is chosen (the rows carry no date). The status filter offers "Completed" and "Overdue", which never match.
- There is no way to record that a customer paid what they owed. **NOT BUILT**

### 13.5 How reports connect

| Report | Fed by |
|---|---|
| Sales Report | Sales (POS and back office), returns, current stock |
| Purchase Report | Purchases, current stock |
| Customer Report | Sales Report rows |
| Customer Due | Invoices (sales with money owed) |

There is no profit, tax (GST) summary, stock valuation, cashier end-of-day or cash drawer report. **NOT BUILT**

---

## 14. Product Showcase

### 14.1 Purpose and who can use it

Controls the shop's **public web page**: a catalogue customers can browse (products, prices, in stock or not, shop details). Customers cannot order or pay online; the page invites them to visit the store. **IMPLEMENTED** (online ordering **NOT BUILT**)

Needs **View stores and warehouses** to open; **Edit stores and warehouses** to change anything (the system refuses saves without it).

### 14.2 What the screen shows

"Product Showcase Settings" / "Manage your public shop profile and control which products are visible to physical store customers."

- **Store** choice (when there is more than one store), **Copy Public Link** ("Copied Link"), **Preview Public Shop**.
- A banner: **Public Showcase Active** or **Catalog Offline**, with the public address.
- **Enable Public Showcase** switch.
- **Set as default store** (or "Default store" when it already is). Greyed out until the showcase is on and saved: "Turn the public showcase on and save first." The default store is the one the shop's main address shows. If the shop's main address still shows the staff sign-in: "The main address shows the staff sign-in. To show this shop there, choose Online shop in Settings, Home page."

**Shop Profile Details** (button **Save Profile Changes**):

| Field | Notes |
|---|---|
| Public URL Slug | "Lowercase letters, digits and dashes." Up to 60. "Your store link will be: …/shop/<slug>". Taken: "This shop URL slug is already taken." Invalid: "Shop URL may use lowercase letters, digits and single hyphens (max 60)." |
| Contact Phone | Shown on the public page and on receipts |
| Physical Shop Address | Shown on the public page and on receipts |
| Opening Hours | e.g. "Mon - Sat: 10:00 AM - 9:00 PM, Sun: Closed" |
| Shop Description / Welcome Note | Welcome text |

The store's logo and banner pictures are kept by the system but cannot be changed on this screen. **NOT BUILT**

Success and failure appear in a browser pop-up: "Shop Showcase settings saved successfully!" or the reason ("Failed to save settings").

**Product Publishing Control**: search "Search products to publish...", filter All Products / Published Only / Unpublished Only / Featured Only, **Publish All** / **Unpublish All**, and a table: Product Name, Category, **POS Price**, **Public Status** (switch), **Featured Badge** (switch). Switches save at once.

### 14.3 Rules

- Only **published** products of an **active** store with the showcase **on** appear publicly. Featured products are highlighted.
- The public price is the product's normal (POS) price.
- The public page shows whether a product is in stock in that store, not how many.
- Deleting a product unpublishes it everywhere.
- Only an active store with its showcase on can be the default: "Only an active store with its public showcase turned on can be the default store."

### 14.4 How it connects

Uses Products (section 8), store stock (section 9) and the Home page setting (section 16). The store phone and address set here are printed on receipts (section 17).

---

## 15. Users and roles

### 15.1 Purpose and who can use it

Staff accounts and what each role may do.

| To do this | You need |
|---|---|
| See users / roles | **View users** / **View roles** |
| Invite, edit, delete users | **Create / Edit / Delete users** |
| Create, edit, delete roles | **Create / Edit / Delete roles** |

Owners and admins have these; billers do not.

### 15.2 Users ("Users", "Manage application users and assign custom roles")

**IMPLEMENTED.** List: Name, Email, Role, Status, View / Edit / Delete; search and an Active / Inactive filter; **Add User**. The buttons are not greyed out without permission; the system refuses instead. **View** shows the raw stored details.

**Add New User**

| Field | Required? | Notes |
|---|---|---|
| **Full Name** | Yes | Up to 120 characters |
| **Email Address** | Yes | Must be valid and not used by another user: "Email is already in use." |
| Password | No | "Leave empty to email an invitation". If typed, at least 12 characters |
| **Assigned Role** | Yes | Any role except Owner |
| Status | | Active (default) or Inactive |

Messages: "Name and email are required.", "A password needs at least 12 characters, or leave it empty to send an invitation." Button **Create User** ("Creating...").

What happens: with a password, the user can sign in at once. Without one, an **invitation email** is sent; the link works for 7 days and lets them choose a password (section 2.1). The number of users may be limited by the plan.

**Edit User**: Full Name (required: "Name is required."), Email Address, Assigned Role, Status; **Save User**. Making a user **Inactive** signs them out everywhere and stops them signing in.

Rules (the system refuses with these messages):
- "You cannot change your own role or status." / "You cannot delete yourself"
- "You cannot change a user with more permissions than you"
- "The owner role can only be assigned through an ownership transfer" (there is no ownership transfer screen: **NOT BUILT**)
- "The last owner cannot be removed"

Deleting a user keeps their name on the sales they made.

### 15.3 Roles ("Roles & Permissions")

**IMPLEMENTED.** One card per role with its name, description ("Built-in system role with default permissions." or "Custom user role.") and its permissions ("No permissions assigned" if none). Search "Search roles...". Buttons: create (with Create roles), edit (with Edit roles), delete (with Delete roles; not on built-in roles).

**Create Custom Role / Edit Role**
- **Role name** (required: "Role name is required."; unique: "Role with this name already exists."), e.g. "Inventory Manager, Store Accountant".
- Description.
- **Module Permissions Matrix**: tick boxes for every permission, grouped by section, with **Select All** / **Deselect Module** per section.
- Button **Create Role** or **Update Role**.

Rules:
- Owner and Admin cannot be changed: "The owner and admin roles cannot be changed". The Biller role can be changed but not renamed: "System roles cannot be renamed".
- You cannot give permissions you do not have yourself: "You cannot grant permissions you do not have".
- "You cannot change your own role".
- Delete asks 'Are you sure you want to delete the role "<name>"?'. Built-in roles: "System roles cannot be deleted." A role still given to users: "Cannot delete a role assigned to N user(s). Reassign them first."
- Changes reach signed-in staff within about a minute.

### 15.4 Full list of permissions

As shown in the matrix, grouped by section:

| Section | Permissions |
|---|---|
| Dashboard | View dashboard |
| POS | Open POS, Sell at POS |
| Products | View, Create, Edit, Delete, Import products |
| Categories, Brands, Units, Suppliers, Customers | View, Create, Edit, Delete (each) |
| Inventory | View inventory, Edit inventory |
| Stock adjustments | View, Create stock adjustments |
| Sales | View own sales, View all sales, Create sales, Edit sales, Void sales, Override prices, Give discounts |
| Sales returns | View returns, Create returns |
| Purchases | View, Create, Edit purchases, Approve and receive purchases |
| Invoices | View invoices |
| Reports | View reports, Export reports |
| Users, Roles | View, Create, Edit, Delete (each) |
| Stores | View, Create, Edit, Delete stores and warehouses |
| Settings | View settings, Edit settings |
| Email settings | View email settings, Edit email settings |
| Receipts | View receipts, Resend receipts |
| Audit | View audit log (there is no audit log screen: **NOT BUILT**) |
| Billing | View billing, Manage billing |

---

## 16. Settings that affect sales and receipts

### 16.1 Purpose and who can use it

**Settings** has three tabs: **Shop** (needs **View settings** to see, **Edit settings** to change), **Email** (needs **View / Edit email settings**), and **Appearance** (light or dark look, for everyone). Without edit permission: "You can view these settings but not change them." The Biller role can view Shop settings. **IMPLEMENTED**

Changes apply to new sales from the moment they are saved. Old sales and their receipts keep the figures they were saved with (but see section 17.3 for the receipt's shop details).

### 16.2 Shop tab

One page, one save button. Messages: "Settings saved.", "Nothing to save.", or the problem next to the field.

**Business profile**

| Field | Notes | On the receipt? |
|---|---|---|
| **Business name** | Required: "Business name is required (at most 120 characters)." | Yes, at the top |
| Legal name | "If different, shown on invoices." | Yes |
| Address, Phone | | Used when the store has none |
| Email | Must be valid: "Enter a valid email address." When emails go out through the platform, customer replies go here | Yes |
| Logo | PNG, JPEG or WebP, at most 2 MB ("Use a PNG, JPEG or WebP image.", "The logo can be at most 2 MB."). "Save to use the new logo." | Yes, on the PDF and in emails |

**Money and time**

| Field | Default | Notes |
|---|---|---|
| Currency | INR | Decides the symbol and the number of decimals (0 for JPY, 3 for KWD …). Changing it does not convert old sales |
| Number format | India (1,23,456.78) | How amounts are written |
| Time zone | Asia/Kolkata | Decides "today", receipt dates, report days and the Dashboard months |

**Tax and invoices**

| Field | Default | Effect |
|---|---|---|
| Tax name | GST | The word shown for tax at the POS and on receipts. "Tax label is required (at most 20 characters)." |
| Tax registration number | empty | "GSTIN or VAT number. Receipts become tax invoices." Letters, digits, / and − only, up to 30 |
| Tax lines on receipts | CGST + SGST (within the state) | Also "One tax line" and "IGST (between states)". Decides how tax is split on each sale line (section 4.7) |
| Invoice number prefix | POS-INV | "Letters, digits and - only." Up to 20. New receipt numbers use the new prefix; the counting continues (POS-INV-000041 can be followed by SHOP-000042) |
| **Largest discount without approval (%)** | 10 | The discount limit for staff without Override prices (section 4.7). 0 to 100: "Use a percentage between 0 and 100." |
| Prices include tax | off | "Used for products without their own setting." |

Example of the discount limit: limit 10 %, subtotal ₹500.00. A biller may give up to ₹50.00. ₹50.01 is refused with "Discounts above 10% need the price override permission."

**Receipt text**: **Header** ("Shown above the shop name.") and **Footer** ("For example a return policy or a thank-you note."), up to 500 characters each.

**Home page**: what the shop's main address shows: **Staff login** (default) or **Online shop**, and the **Default store** whose public shop is shown there ("Automatic (the "main" store, else the first public store)").

The default (walk-in) customer cannot be changed on this screen. **NOT BUILT**

### 16.3 Email tab ("Email (SMTP)")

**IMPLEMENTED.** Lets the shop send receipts and other emails from its own mailbox. Without it, emails go out from the platform as "<Business name> via <platform>", if the plan allows.

| Field | Notes |
|---|---|
| SMTP server | e.g. smtp.example.com |
| Port | 587 (STARTTLS) or 465 (TLS) |
| Emails per day at most | the shop mailbox's daily limit |
| Username | |
| Password | "Often an app password from your mail provider." Once saved: "A password is saved. Leave empty to keep it." |
| From name | e.g. "Your shop" |
| From address | "Usually the same as the username." |
| Use this server | on / off |
| If it fails, send through the platform | on / off |

Status shown: **Not tested yet**, **Working**, **Failing**, **Turned off**, with Last test, Last email sent and Last error.

Buttons and messages:
- Save: "Saved. Send a test email to check that it works."
- Send test (to a typed address or your own): "Test email sent. Check the inbox." or "The test failed: <reason>. Check the server, port, username and password." Too many: "Too many tests. Wait a few minutes."
- Remove: asks "Remove the SMTP settings? Emails then go out through the platform, if allowed." then "SMTP settings removed."

How sending is chosen is in section 17.6.

### 16.4 Billing (short note)

**Billing** (owners only by default) shows the plan, usage against its limits (products, stores, users, file storage, and features such as report export, the public shop page and the shop's own email server), plan choices and past invoices. When the subscription is not paid, the shop becomes **read-only** (section 1). This guide does not cover billing further.

---

## 17. Receipts

### 17.1 When a receipt is created

**IMPLEMENTED.**

- Every sale gets a receipt the moment it is saved (POS or back office). Its number is the sale's receipt number, for example **POS-INV-000042**.
- Right after the sale, the system prepares the **PDF** in the background, so downloads and emails are quick.
- If the sale is later **edited** or **voided** (no screens today, section 5.6 and 5.7), a **new version** of the receipt is made. Older versions are kept by the system but cannot be opened; downloading always gives the latest version.
- Returns do not change the receipt and do not produce one.

### 17.2 The receipt on screen

Opens automatically after a POS sale, and from the receipt button in the Sales list ("Loading receipt..." first). **IMPLEMENTED**

Contents, top to bottom:
1. Receipt header text (Settings), business name, legal name (if different), store name, store address (or the business address), "Phone: …" (store phone, or the business phone), "<Tax name> No.: <registration number>" (if set).
2. A title: **TAX INVOICE** if the shop has a tax registration number, otherwise **RECEIPT**; **VOIDED SALE** for a voided sale.
3. Receipt no., Date (date and time), Customer ("Walk-in Customer" if none), Cashier.
4. Lines: Item (with SKU), Price, Qty, Amount (price × quantity minus its share of the discount, **before tax**, so the lines add up to the subtotal less discount).
5. Subtotal; Discount (if any, in red); **CGST** and **SGST** rows, or **IGST**, or a single tax row named after the tax name; Shipping (if any); **Total**; "Paid (<payment type>)"; Received and Change (if cash was entered); **Due** (if anything is owed).
6. Footer text (Settings), or "Thank you for shopping with us!".

Buttons:
- **Print**: opens the browser's print window with only the receipt on the page. There is no direct connection to receipt printers or cash drawers, no automatic printing, and no narrow-paper layout. **PARTLY IMPLEMENTED**
- **Download PDF** (needs **View receipts**): see 17.3.
- **Email** box with "Customer email (optional)" (needs **Resend receipts**; hidden for voided sales): see 17.6.
- Close (× or clicking outside). At the POS this also empties the cart.

The on-screen receipt takes the shop details from the **current** Settings. If the business name or address is changed later, old receipts opened on screen show the new details. **PARTLY IMPLEMENTED**

**Worked example.** Two shirts at ₹120.50, 18 % GST on top, paid in cash with ₹500.00:

| | |
|---|---|
| Shirt ×2 | ₹241.00 |
| Subtotal | ₹241.00 |
| CGST | ₹21.69 |
| SGST | ₹21.69 |
| **Total** | **₹284.38** |
| Paid (cash) | ₹284.38 |
| Received | ₹500.00 |
| Change | ₹215.62 |

### 17.3 The PDF receipt

**IMPLEMENTED.** **Download PDF** saves a file named after the receipt number, for example POS-INV-000042.pdf.

- Title **"Tax Invoice"** when the shop has a tax registration number (shown as "GSTIN: …" when the tax name is GST, otherwise "<tax name> No: …"), otherwise **"Receipt"**.
- Shop logo, business and legal name, address, phone, email; store name, address and phone; receipt header and footer.
- Receipt number, date and time in the shop's time zone, cashier, customer and the receipt email address.
- Each line with variant details (for example "Size: M") and SKU, quantity, price, discount, tax rate, tax and total; a tax summary per rate; totals, paid, received and change (if any), amount due (if any), payment type and payment status.
- The PDF is made once per version and kept, so later changes to Settings do not change a PDF already made.
- If the PDF is not ready within 5 seconds: "The PDF is still being prepared. Try again in a moment."
- Someone else's sale without View all sales: not found.

### 17.4 The void version

**IMPLEMENTED in the system; no screen can void (section 5.7).** When a sale is voided:
- A new receipt version is made. The PDF shows a large red **VOID**, with "on <date and time>" and the reason, if given.
- The on-screen receipt shows **VOIDED SALE** and hides the Email box.
- No email is sent when a sale is voided. If an earlier receipt email was still waiting to be sent, it goes out with the void version: subject "Voided receipt from <business>" and the text "This sale was voided. The attached receipt is marked VOID."

### 17.5 Emailing the receipt with the sale

**IMPLEMENTED.**

Who gets it:
1. The address typed in the payment window at the POS ("Email the receipt to (optional)"), or
2. if nothing was typed, the customer's email from Customers, if valid, or
3. nobody.

| Case | Email sent? |
|---|---|
| Walk-in Customer, nothing typed | No |
| Address typed at the POS | Yes, to that address (it is also saved on the sale) |
| Customer has an email, box left as filled in | Yes |
| Customer has an email, cashier **clears** the box | **Still yes**, to the customer's email. **PARTLY IMPLEMENTED** (there is no way to say "no email") |
| Back-office sale (no email box) | Yes if the customer has an email |
| Invalid typed address | The sale is refused: "Enter a valid email address." |

The email is queued together with the sale: it is sent only if the sale is saved, and a failure to send never cancels the sale. Each sale sends at most one email this way.

The email: subject **"Your receipt from <business> (<receipt number>)"**, the business name and logo, receipt header, "Hello <customer>, Thank you for your purchase.", receipt number, date, total, store name, address and phone, footer, and "Your receipt is attached as a PDF." with the PDF attached. If the PDF cannot be made after 3 tries, the email goes without it and says "Your receipt could not be attached. Please ask the store for a copy."

The email always shows the sale as it is **when sent**. If the sale changes before sending, the newer version goes out.

### 17.6 Sending it again (resend)

**IMPLEMENTED.** In the receipt window, type an address (or leave it empty) and press **Email**.

| Situation | What you see |
|---|---|
| Address typed | "The receipt will be emailed to <address>." The new address is saved on the sale and the change is recorded in the activity log |
| Nothing typed, an address is known (saved on the sale, or the customer's) | "The receipt will be emailed to the customer's address." |
| No address anywhere | "Enter the customer's email address." |
| Invalid address | "Enter a valid email address." |
| The same address already got 5 resent receipts in the last 24 hours (from any sales) | "This address already got 5 receipts today. Try again tomorrow." |

**How emails are sent**
- From the shop's own mailbox (Settings → Email) when it is turned on, working and under its daily limit.
- Otherwise from the platform as "<Business name> via <platform>", with replies going to the shop's email. The plan may limit how many platform emails a day are allowed; above it, emails wait.
- If the shop chose not to fall back to the platform and its own mailbox is not usable, emails wait and are retried.
- Failed sends are retried with growing waits (from 30 seconds up to 6 hours).
- There is no screen showing whether a particular receipt email was delivered. **NOT BUILT**

---

## 18. How the sections connect (summary)

| Section | Takes from | Changes | Shows up in |
|---|---|---|---|
| **Dashboard** | Sales, customers, products | nothing | |
| **POS** | Products, stock, customers, stores, Settings | Stock **down**, new sale, receipt, receipt email | Sales, Invoices, Dashboard, Sales / Customer reports, Stock history |
| **Sales** (Add Sale) | same as POS | same as POS | same as POS |
| Sale edit / void (no screen) | the sale | Stock back (void) or re-taken (edit), new receipt version | Sale leaves lists and figures when voided |
| **Sales Returns** | a sale (or nothing, with Override prices) | Stock **up**, refund recorded, sale locked against edit and void | Sales Report (Returned, Net), Stock history. Not the Dashboard, not Customer Due |
| **Invoices** | Sales | nothing | Customer Due |
| **Products** | Categories, brands, units, suppliers, stores | Catalogue, opening stock, price | POS, Inventory, Showcase, reports |
| **Inventory** | Products, stores | Stock **set** to a figure | POS stock check, reports "In stock" |
| **Stock Adjustments** | Products, stores | Stock **up or down** with a reason | Inventory, Stock history |
| **Purchases** | Suppliers, products, stores | Stock **up** only when Received | Purchase Report, Stock history |
| **Categories / Brands / Units** | | labels | Products, POS buttons, reports |
| **Suppliers** | | | Purchases, Products |
| **Customers** | | | POS, sales, returns, invoices, receipt emails, customer reports |
| **Stores** | | where stock lives | Everything with stock or sales |
| **Reports** | Sales, returns, purchases, stock | nothing | |
| **Product Showcase** | Products, store stock | public page, store phone and address | Public shop, receipts (store details) |
| **Users and roles** | | who can do what | every screen |
| **Settings** | | tax, discount limit, prefix, currency, time zone, receipt text, email | POS, sales, receipts, emails, reports |
| **Receipts** | the sale, Settings, store details | PDF, emails | |

Stock in one line: **purchases received, returns, voids and adjustments (Add) raise it; sales and adjustments (Remove) lower it; Inventory edits, product edits and imports set it.**

---

## 19. Gaps and open questions

How this guide was checked: the screens, the system's rules and its tests were read. Where the screens were unclear, the running test shop was asked directly, sending exactly what each screen sends; this is how items 1 and 2 were confirmed. Clicking through the screens in a browser was not possible on this computer (no usable browser, no internet access to install one), so screen behaviour comes from reading the screens.

### Does not work today

1. **Add Product cannot save.** Only the last step's boxes are sent; the system answers "Product name is required." New products can only be added by Import. (Confirmed against the test shop.)
2. **Add Customer and Add Supplier (from their own pages) cannot save**, for the same reason ("Validation failed" without a picture, "Customer name is required." with one). Customers can still be added at the POS (name and phone only); suppliers from Add Purchase or Add Product. (Confirmed.)
3. **Purchases saved as Pending or Ordered can never be received**, so their stock never arrives. Only purchases saved as Received raise stock.

### Missing (NOT BUILT)

4. No screen to **edit** or **void** a sale, although the system has full rules for both.
5. No **Stores** page (stores can only be created from Add Product; no rename, deactivate or delete), and **warehouses** do not exist on any screen and cannot hold stock. No stock transfers.
6. Products with several variants: variant prices, SKUs and stock cannot be edited; the POS always sells the first variant; Add Product's variant table is a fixed example.
7. At the till: no barcode scanning, no price change, no part payment, no way to say "do not email the receipt".
8. No way to record a later payment from a customer or to a supplier; no due dates; "Overdue" is never set.
9. No return receipt or credit note, no return detail page, returns cannot be edited.
10. No receive, cancel or pay-later actions for purchases; no tax on purchases; no returns to suppliers.
11. No low-stock alerts; the status only shows on the Inventory page.
12. No screens for: the activity (audit) log, ownership transfer, the default customer, sub-categories, the store logo and banner, or email delivery status.
13. No profit, GST summary, stock valuation, cash drawer or end-of-day reports. No receipt-printer support.

### Screens and rules that disagree (PARTLY IMPLEMENTED)

14. **The POS shows a different total** from the one saved: it takes the discount off after tax (₹130.14 shown, ₹127.97 saved in the section 4.7 example) and adds tax again on tax-inclusive prices (₹136.00 shown, ₹118.00 saved). The payment window's change uses the screen figure.
15. **POS sales are always "Paid in full"**, whatever was received.
16. **Clearing the email box** at the POS still emails customers who have an email on file.
17. Add Product offers only 8 %, 5 % and 4 % tax (no 12 %, 18 %, 28 %). The product **discount** and **selling type** are stored but never used.
18. Filters that never match: "Overdue" (Sales, Invoices, Returns, Purchases), "Received" (Returns), Customer Report's payment method and status, Customer Due's payment method and dates. No "Partial" filter anywhere.
19. Dashboard: greeting always says "Admin"; no date, store or staff filters; Total Overdue is always 0; returns are not taken off revenue; "Sales Distribution" is really the top 5 products.
20. Sales list: voided sales never appear; only the newest sales up to a limit are loaded, without warning.
21. Sale status "Pending" changes nothing.
22. Inactive categories, brands, units, suppliers and customers can still be chosen everywhere.
23. Customer and supplier forms mark every field as required, but the system requires only the name.
24. Purchases: choosing "Paid" without a paid amount saves "Unpaid". The Purchase Report includes Pending and Ordered purchases. Billers see the Purchase Report in the menu but are refused.
25. Returns list shows dates in a raw technical format; "View" buttons (returns, purchases, users) show raw stored details; list exports use internal column names.
26. Stock history shows "manual_set" and "product_edit" instead of plain words.
27. Some buttons show without permission (Add Sale; Customers and Users edit and delete); the system refuses afterwards.
28. A role with "Open POS" but without "Create sales" can fill a cart but not complete it.
29. The on-screen receipt uses today's shop details for old sales; the PDF keeps the details from when it was made.

### Open questions (UNSURE)

30. Owners and admins never see "Prices have changed": a stale cart price is sold as a price override. Intended?
31. Editing a sale without sending the customer moves it to the Walk-in Customer. Intended? (No screen uses edit today.)
32. Saving **Edit Product** sends back the stock figures from when the window opened; a sale made meanwhile could be undone in the stock count. Follows from the rules; not tried.
33. Refunds are not taken off **Customer Due**. Intended?
34. POS product locks last 2 minutes and are not renewed; changing the store keeps the cart. Acceptable?
35. The discount limit is measured against the subtotal including tax for tax-inclusive prices. Intended?
36. Returns are visible to everyone with "View returns", unlike sales, which are limited to your own. Intended?
37. A comment in the system says a receipt email without the PDF carries a download link; the email actually says "Please ask the store for a copy." The void email says "The attached receipt is marked VOID" even when nothing is attached.
38. Units are labels only; quantities are always whole numbers. Is selling by weight needed?
