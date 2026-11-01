# Records created in demo-shop by the UI audit

Every record carries the prefix **UI-AUDIT**. Nothing was deleted. All of these were created through the screens unless the "How" column says otherwise.

| # | Kind | Name / reference | How | Notes |
|---|---|---|---|---|
| 1 | Category | UI-AUDIT Category | Add Category page | |
| 2 | Unit | UI-AUDIT Unit (short name uia) | Add Unit page | |
| 3 | Role | UI-AUDIT Role (3 permissions) | Roles → Create Role | |
| 4 | User | UI-AUDIT Cashier (ui-audit-cashier@demo.test), role Biller | Users → Add User, with a password | Password kept out of the repository |
| 5 | Brand | UI-AUDIT Brand | Add Brand page, with a 1-pixel picture | Without a picture the page cannot save |
| 6 | Products | UI-AUDIT Lifecycle Tea (UIA-TEA-1), UI-AUDIT Inclusive Soap (UIA-SOAP-1), UI-AUDIT T-Shirt (UIA-TS-M, UIA-TS-L) | Products → Import, store Main Store | Opening stock 0, 5, 4, 4 |
| 7 | Supplier | UI-AUDIT Supplier | Add Purchase → Add New | |
| 8 | Purchase | UI-AUDIT-PUR-1, Received, 10 × Lifecycle Tea at ₹80, paid ₹500 | Add Purchase | Total ₹800, Partial |
| 9 | Customer | UI-AUDIT Customer (phone 9000000003) | POS → Add New | |
| 10 | Sale | POS-INV-000031: 2 × Lifecycle Tea, ₹284.38, cash ₹500, emailed to ui-audit-walkin@example.test | POS (owner) | Receipt resent to ui-audit-resend@example.test |
| 11 | Sale | POS-INV-000032: 1 × Inclusive Soap, ₹118.00 | POS (owner) | Screen showed ₹136.00 |
| 12 | Sale | POS-INV-000033: 1 × Lifecycle Tea + ₹40 shipping, ₹182.19, paid ₹100 (Partial), UI-AUDIT Customer | Sales → Add Sale | |
| 13 | Sale | POS-INV-000034: 1 × Lifecycle Tea, discount ₹12.05, ₹127.97 | POS (UI-AUDIT Cashier) | A ₹12.06 discount was refused first |
| 14 | Return | RET-000008: 1 × Lifecycle Tea from POS-INV-000031, ₹142.19 | Sales Returns → Add (from a sale) | |
| 15 | Return | RET-000009: 1 × Inclusive Soap, no sale, ₹50.00, UI-AUDIT Customer | Sales Returns → Add (without a sale) | |
| 16 | Stock adjustment | Lifecycle Tea, Main Store, remove 1, Damaged, note "UI-AUDIT" | Stock Adjustments → New adjustment | |
| 17 | Inventory edit | UI-AUDIT T-Shirt L (UIA-TS-L), Main Store, set 4 → 6 | Inventory → Edit | |
| 18 | Showcase | UI-AUDIT Lifecycle Tea published on Main Store | Product Showcase | Visible on /shop/main |
| 19 | Purchase | UI-AUDIT-PUR-3, **Pending**, 3 × Inclusive Soap at ₹70 | Add Purchase | Cannot be received from any screen (only a View button) |
| 20 | Email settings | SMTP server "smtp.ui-audit.invalid", from name UI-AUDIT, **Use this server: off** | Settings → Email | Saved turned off, so receipt emails still go through the platform. Test failed as expected |

## Changed and put back

| What | Change | Restored |
|---|---|---|
| Main Store web address (slug) | Typing "Bad Slug!" in Product Showcase was silently turned into "badslug" and saved | Set back to **main** through the same screen about a minute later |

## Attempts that were refused (nothing saved)

- Brand without a picture, Customer page, Supplier page: "Validation failed" (picture field).
- Purchase UI-AUDIT-PUR-2 (Pending, T-Shirt with sizes): "Choose a variant for this product."
- Category duplicate, discount 12.06 as cashier, return of 3 when 1 was left, removing 999 units, deleting a product with stock, Settings with discount 150 or an empty business name, Email settings with no server.
- Edit Product price 118 → 120 on UI-AUDIT Inclusive Soap: blocked silently by the browser (Brand required, product has none).

## Records created by the automated tests (step 2)

Named `E2E-<slice> <kind> w<width> <run id>`; each run makes its own.

| Slice | Records |
|---|---|
| S1 | 5 customers, 5 suppliers, 5 brands (two earlier runs stopped after these), 3 products (opening stock 3, ₹99.50, 18 %, Main Store; description edited by the test), 3 **Pending** purchases of 2 × UI-AUDIT T-Shirt at ₹250 |
