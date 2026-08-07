# ReactronPOS — User Guide

A complete walkthrough of every flow in the application, from first login to reports.

---

## Table of Contents

1. [Application Overview](#1-application-overview)
2. [Getting Started](#2-getting-started)
3. [Authentication](#3-authentication)
4. [Dashboard](#4-dashboard)
5. [Master Data Setup (Do This First)](#5-master-data-setup-do-this-first)
   - 5.1 [Categories & Sub-Categories](#51-categories--sub-categories)
   - 5.2 [Brands](#52-brands)
   - 5.3 [Units](#53-units)
   - 5.4 [Suppliers](#54-suppliers)
   - 5.5 [Stores](#55-stores)
   - 5.6 [Warehouses](#56-warehouses)
   - 5.7 [Customers](#57-customers)
   - 5.8 [Billers (Users)](#58-billers-users)
6. [Product Management](#6-product-management)
   - 6.1 [Create a Single Product](#61-create-a-single-product)
   - 6.2 [Create a Variable Product](#62-create-a-variable-product)
   - 6.3 [Edit or Delete a Product](#63-edit-or-delete-a-product)
7. [Inventory Management](#7-inventory-management)
8. [Purchasing (Stock In)](#8-purchasing-stock-in)
9. [POS — Point of Sale Checkout](#9-pos--point-of-sale-checkout)
10. [Sales Management](#10-sales-management)
    - 10.1 [View Sales](#101-view-sales)
    - 10.2 [Manual Sale Entry](#102-manual-sale-entry)
    - 10.3 [Invoices](#103-invoices)
11. [Sales Returns](#11-sales-returns)
12. [Reports](#12-reports)
    - 12.1 [Sales Report](#121-sales-report)
    - 12.2 [Purchase Report](#122-purchase-report)
    - 12.3 [Customer Report](#123-customer-report)
    - 12.4 [Customer Due Report](#124-customer-due-report)
13. [Role Permissions Summary](#13-role-permissions-summary)
14. [Data Flow Diagram](#14-data-flow-diagram)
15. [API Reference](#15-api-reference)

---

## 1. Application Overview

ReactronPOS is a full-stack point-of-sale system for retail management. It is built with:

- **Frontend**: React 19, Vite, TailwindCSS, Headless UI, React Router (HashRouter)
- **Backend**: Node.js, Express, Sequelize ORM, PostgreSQL
- **Desktop wrapper**: Electron (optional)

Key capabilities:

| Feature | Description |
|---|---|
| POS Checkout | Cart-based checkout with product locking, tax, discount, receipt |
| Inventory | Per-store, per-variant stock tracking with low-stock alerts |
| Multi-store | Independent inventory per store location |
| Product Variants | Single or variable products (Color/Size/etc.) |
| Purchases | Stock-in from suppliers with automatic inventory increment |
| Sales Returns | Return items back to inventory |
| Reports | Sales, purchases, customers, and dues with PDF/Excel export |
| Role-based access | Admin (full access) vs Biller (own sales only) |

---

## 2. Getting Started

### Prerequisites

- Node.js 18+
- PostgreSQL running locally or remotely

### Installation

```bash
# Install all dependencies (root, backend, frontend)
npm install

# Configure environment variables
cp backend/.env.example backend/.env
cp frontend/.env.example frontend/.env
```

Edit `backend/.env`:

```env
DB_HOST=localhost
DB_PORT=5432
DB_NAME=reactronpos
DB_USER=postgres
DB_PASSWORD=yourpassword
JWT_SECRET=your-secret-key
CORS_ORIGIN=http://localhost:5173
PORT=3000
```

Edit `frontend/.env`:

```env
VITE_API_URL=http://localhost:3000
```

### Running the App

```bash
# Start both backend and frontend together
npm run dev

# Or start them individually
cd backend && npm run dev
cd frontend && npm run dev
```

### Seed Sample Data (optional)

```bash
npm run seed --workspace backend
```

### Electron Desktop Mode

```bash
npm run start:electron   # development
npm run dist             # build + package as desktop app
```

---

## 3. Authentication

### Sign Up

1. Navigate to `/signup` (or click **Sign Up** on the login page).
2. Enter your **name**, **email**, and **password**.
3. The first user registered automatically becomes an **admin**. Subsequent users are created as **billers** unless the admin promotes them.
4. On success you are redirected to the sign-in page.

### Sign In

1. Navigate to `/signin`.
2. Enter your **email** and **password**.
3. The app stores a JWT token and user data in `localStorage`.
4. You are redirected to the **Dashboard**.

### Session Handling

- The JWT token expires after **1 hour**.
- If a request returns `401` or `403`, the app automatically logs you out and shows a session-expired message.
- Clicking **Logout** in the navbar clears the token and user data from `localStorage`.

---

## 4. Dashboard

The dashboard (`/`) gives a real-time overview of the business.

| Card | What it shows |
|---|---|
| Total Revenue | Sum of all completed sale totals |
| Total Orders | Count of all sales |
| Total Customers | Count of registered customers |
| Total Products | Count of active products |

Below the KPI cards you will find:

- **Revenue trend chart** — daily/weekly revenue line graph
- **Sales distribution pie chart** — breakdown by category or store
- **Recent activity** — latest sales or purchases

> Only admins see all data. Billers see only their own sales.

---

## 5. Master Data Setup (Do This First)

Before creating products or processing sales, set up the supporting data. All master data management is **admin-only** for write operations.

### 5.1 Categories & Sub-Categories

**Path**: `/categories` → **Add Category** button

Categories classify your products at the top level.

1. Click **Add Category**.
2. Enter a **Name**, upload an optional **image**, set **Status** (Active/Inactive).
3. Save.

Sub-categories live under a parent category:

1. Navigate to `/categories` and click into a category, or use the **Sub-categories** link.
2. Click **Add Sub-category**.
3. Select the **parent category**, enter a **name**, upload an image, set status.

> You can also create sub-categories inline while adding a product by clicking **Add New** next to the Sub-Category field.

---

### 5.2 Brands

**Path**: `/brands` → **Add Brand** button

1. Click **Add Brand**.
2. Enter a **Name**, optional image, set status.
3. Save.

---

### 5.3 Units

**Path**: `/units` → **Add Unit** button

Units define how a product is measured (e.g., pcs, kg, litre).

1. Click **Add Unit**.
2. Enter a **Name** (e.g., "Kilogram") and **Short Name** (e.g., "kg").
3. Set status and save.

---

### 5.4 Suppliers

**Path**: `/suppliers` → **Add Supplier** button

Suppliers are linked to products and purchase orders.

1. Click **Add Supplier**.
2. Fill in **Name**, **Email**, **Phone**, **Address**, optional image.
3. Save.

> You can also create a supplier inline while adding a product or purchase by clicking **Add New** next to the Supplier field.

---

### 5.5 Stores

Stores represent physical locations. Inventory is tracked **per store**.

Stores can be created:
- Via the **Stores** section (if accessible from the nav).
- Inline on the Add Product form by clicking **Add New** next to the Store field.

Fields: **Name**, **Address**, **Status**.

---

### 5.6 Warehouses

Warehouses are available as an entity for future use. Currently they can be created via the API (`POST /api/warehouses`) with a **Name**, **Address**, and **Status**. They are not yet tied to the main inventory flow.

---

### 5.7 Customers

**Path**: `/customers` → **Add Customer** button

1. Fill in **Name**, **Email**, **Phone**, **Address**, **City**, **Country**, optional image.
2. Set **Status** (Active/Inactive).
3. Save.

> A **Walk-in Customer** is used by default in the POS when no specific customer is selected.

You can also create a customer directly from the POS checkout page without leaving the checkout flow.

---

### 5.8 Billers (Users)

**Path**: `/billers`

Billers are staff users who operate the POS. Only admins can manage billers.

1. Click **Add Biller**.
2. Enter **Name**, **Email**, **Password**, assign **Role** (admin or biller).
3. Save.

Biller limitations:
- Can only view their **own sales** on the Sales list.
- Cannot create purchases, manage master data, or view all reports.

---

## 6. Product Management

**Path**: `/products`

Products are the core of the inventory. Each product must have at least one **variant** (even single products have one variant internally).

### 6.1 Create a Single Product

A single product has one SKU, one price, and one inventory per store.

1. Go to `/products` → click **Add Product**.
2. **Product Information** section:
   - Select **Store** (required — determines which store's inventory this product belongs to).
   - Select **Supplier** (optional).
   - Enter **Product Name** (required).
   - Enter or generate a **SKU** (click **Generate** for a random `SK######` code).
   - Set **Selling Type**: `POS` (for checkout) or `Online`.
   - Select **Category**, **Sub-Category**, **Brand**, **Unit**.
   - Select **Barcode Symbology** (Code 128 / Code 39 / UPC-A) and enter **Item Barcode** (optional).
   - Add a **Description** (max 60 words).
3. **Pricing & Stocks** section:
   - Choose **Product Type** = `Single Product`.
   - Enter **Quantity** (initial stock at the selected store).
   - Enter **Price**.
   - Select **Tax Type** (Exclusive/Inclusive) and **Tax** rate (IGST 8%, GST 5%, SGST 4%).
   - Set **Discount Type** (Percentage/Fixed) and **Discount Value**.
   - Set **Quantity Alert** — triggers a low-stock indicator when stock falls to this number.
4. **Images** section: upload one or more product images (converted to Base64 for storage).
5. Click **Save**. The app creates: `Product → ProductVariant → Inventory` all in one transaction.

---

### 6.2 Create a Variable Product

A variable product has multiple variants, each with its own SKU, price, and stock.

1. Follow steps 1–2 from above (Product Information is the same).
2. In **Pricing & Stocks**, select **Variable Product**.
3. In the variants table, each row represents one variant:
   - **Variation** — the attribute name (e.g., `Color`).
   - **Variant Value** — the value (e.g., `Red`).
   - **SKU** — unique identifier (e.g., `FD001-RED`).
   - **Quantity** — initial stock for this variant.
   - **Price** — selling price for this variant.
4. Add more rows as needed (e.g., `Color / Blue / FD001-BLUE`).
5. Click **Save**.

---

### 6.3 Edit or Delete a Product

From the `/products` list:
- Click the **edit icon** on a row to open the edit form. All fields are editable.
- Click the **delete icon** to remove a product. This also removes its variants and inventory records.

> If a product is currently in an active POS cart (locked), it cannot be edited until the lock expires (2 minutes) or the item is removed from the cart.

---

## 7. Inventory Management

Inventory is automatically managed through purchases and sales. However, you can also adjust it directly via the API (`POST /api/inventory`) if needed.

**Inventory record structure:**
- `variant_id` — which product variant
- `store_id` — which store
- `qty` — current quantity
- `quantity_alert` — low-stock threshold

**When does inventory change?**

| Event | Effect |
|---|---|
| Product created | `qty` set to initial quantity entered |
| Purchase received | `qty` incremented by purchased quantity |
| Sale completed | `qty` decremented by quantity sold |
| Sale deleted | `qty` restored |
| Sale return processed | `qty` incremented by returned quantity |

**Product locking in POS:**
When a product is added to a POS cart, it is **locked** (`PUT /api/products/:id/lock`) to prevent concurrent edits while a transaction is in progress. The lock releases when:
- The item is removed from the cart.
- The checkout completes.
- The POS page is navigated away from (unmount cleanup).
- 2 minutes pass (server-side timeout).

---

## 8. Purchasing (Stock In)

**Path**: `/purchases` → **Add Purchase**

Use purchases to record stock received from suppliers. This increments inventory.

1. **Purchase Information** section:
   - Enter a **Reference** number (e.g., `PO-2026-001`).
   - Set the **Date**.
   - Select the **Supplier** (create inline if needed).
   - Set **Status**: `Received` / `Pending` / `Ordered`.
   - Enter **Paid Amount**. The app auto-calculates **Due Amount** (Total − Paid).
   - Set **Payment Status**: `Paid` / `Unpaid` / `Overdue`.

2. **Purchase Items** section:
   - Click **Add Item**.
   - For each row, select a **Product**, then **Variant**, then **Store**.
   - Enter **Quantity** and **Unit Price**.
   - The row total auto-calculates. The grand total updates automatically.
   - Remove rows with the ✕ button.

3. Click **Add Purchase** to save.

**What happens on the backend:**
- A `Purchase` record is created.
- `PurchaseItem` rows are created.
- For each item, the corresponding `Inventory` record at the specified store is **incremented** by the purchase quantity (upserted if it doesn't exist).

> Only **admins** can create purchases.

---

## 9. POS — Point of Sale Checkout

**Path**: `/pos`

This is the main checkout interface for serving customers.

### Step-by-step checkout

**1. Select Store**
- The first thing to do is select which store you are serving from.
- Products displayed are filtered to only show items in stock at that store.

**2. Select or Create Customer**
- Choose a customer from the dropdown (defaults to **Walk-in Customer**).
- To add a new customer on the fly, click **Add Customer** to open the quick-create form without leaving the POS.

**3. Browse & Search Products**
- Products appear as cards with image, name, price, and stock status.
- Use the **search bar** to find by name.
- Use **category tabs** to filter by category (all + each category).
- Products with no stock are greyed out.

**4. Add to Cart**
- Click **Add** on a product card to add it to the cart (right panel).
- The product is **locked** on the server to prevent concurrent modification.
- Use **+/-** buttons in the cart to adjust quantity.
- Click the **trash icon** to remove an item (auto-unlocks the product).

**5. Apply Discount**
- Enter a discount amount in the **Discount** field at the bottom of the cart.

**6. Review Totals**

| Field | Calculation |
|---|---|
| Subtotal | Sum of (price × qty) for all cart items |
| Tax | Subtotal × 8% |
| Discount | Manual input |
| **Total** | Subtotal + Tax − Discount |

**7. Checkout (Pay)**
- Click **Pay** to open the payment form.
- Select **Payment Method** (Cash, Card, etc.).
- Enter **Amount Paid**.
- The form calculates **Change Due** automatically.
- Click **Confirm Payment**.

**What happens on the backend (POST /api/sales):**
1. Validates inventory — checks each cart item has sufficient stock.
2. Creates a `Sale` record (linked to the customer, user, store).
3. Creates `OrderItem` rows for each cart item.
4. Decrements `Inventory.qty` for each variant at the selected store.
5. All of the above run inside a **database transaction** (all-or-nothing).

**8. Receipt**
- A receipt modal appears automatically after payment.
- The receipt shows store info, itemised list, subtotal, tax, discount, total, and payment method.
- Click **Print** to print the receipt.
- Cart is cleared and the POS resets for the next transaction.

---

## 10. Sales Management

### 10.1 View Sales

**Path**: `/sales`

Displays a table of all sales. Each row shows:
- Date, reference/ID
- Customer name
- Biller (user who processed the sale)
- Store
- Total, Paid, Due
- Payment status (Paid / Unpaid / Overdue)
- Sale status (Completed / Pending)

**Filtering**: Use date range pickers, search, and status filters to narrow the list.

**Export**: Export to PDF or Excel using the toolbar buttons.

> Billers see only their own sales. Admins see all sales.

---

### 10.2 Manual Sale Entry

**Path**: `/add-sale`

Use this to record a sale that wasn't processed through the POS (e.g., a phone order).

Fill in customer, items, pricing, and payment details the same way you would in the POS form, then submit.

---

### 10.3 Invoices

**Path**: `/invoices`

Lists all invoices generated from completed sales. You can view, download, or print individual invoices from here.

---

## 11. Sales Returns

**Path**: `/sales-return`

Use this when a customer returns items.

1. Select the **Customer**.
2. Select the **original Sale** (the system links the return to the original transaction).
3. Select the **Store** where the items are being returned.
4. Add **Return Items**:
   - Select the **Product** (and variant if applicable).
   - Enter the **Quantity** returned.
   - Enter the **Unit Price**.
   - Add a **Reason** (optional).
5. The system calculates the **Total**, **Paid**, and **Due** amounts.
6. Set **Payment Status** and **Status** (Pending / Completed).
7. Click **Save**.

**What happens on the backend (POST /api/sales-returns):**
1. Creates a `SaleReturn` record (linked to customer, sale, store).
2. Creates `ReturnItem` rows.
3. For each returned item, **increments** the `Inventory.qty` at the specified store.
4. All operations run inside a **database transaction**.

> Only **admins** can create sales returns.

---

## 12. Reports

All reports support filtering by date range and can be exported to **PDF** or **Excel**.

### 12.1 Sales Report

**Path**: `/sales-report`

Shows aggregated sales data. Filters include:
- **Date range**
- **Product** (filter by specific product)

Columns: Date, Invoice #, Customer, Items, Total, Paid, Due, Payment Status.

---

### 12.2 Purchase Report

**Path**: `/purchase-report`

Shows all purchase orders with line items. Filters include:
- Date range
- Supplier

Columns: Date, Reference, Supplier, Items, Total, Paid, Due, Payment Status.

---

### 12.3 Customer Report

**Path**: `/customer-report`

Aggregates sales per customer. Shows total purchases, amount spent, and frequency per customer. Useful for identifying your best customers.

---

### 12.4 Customer Due Report

**Path**: `/customer-due-report`

Shows customers with outstanding balances (sales where `due > 0` and payment status is Unpaid or Overdue). Useful for following up on receivables.

---

## 13. Role Permissions Summary

| Action | Admin | Biller |
|---|---|---|
| Sign In / Sign Up | ✅ | ✅ |
| View Dashboard | ✅ (all data) | ✅ (own data) |
| View Products | ✅ | ✅ |
| Create / Edit / Delete Product | ✅ | ❌ |
| Create / Manage Categories | ✅ | ❌ |
| Create / Manage Brands | ✅ | ❌ |
| Create / Manage Units | ✅ | ❌ |
| Create / Manage Suppliers | ✅ | ❌ |
| Create / Manage Customers | ✅ | ✅ |
| Manage Billers | ✅ | ❌ |
| Operate POS | ✅ | ✅ |
| View Sales | ✅ (all) | ✅ (own only) |
| Create Manual Sale | ✅ | ✅ |
| Edit / Delete Sale | ✅ | ❌ |
| Create Purchases | ✅ | ❌ |
| Process Sales Returns | ✅ | ❌ |
| View Reports | ✅ | ❌ |
| Export PDF / Excel | ✅ | ❌ |
| Manage Stores / Warehouses | ✅ | ❌ |

---

## 14. Data Flow Diagram

```
                          ┌──────────────┐
                          │   SUPPLIER   │
                          └──────┬───────┘
                                 │ supplies
                                 ▼
ADMIN ──────────────────► PURCHASE ORDER
                                 │ increments
                                 ▼
CATEGORY ──┐                 INVENTORY
BRAND      │                (per variant,    ◄──── SALE RETURN
UNIT       ├──► PRODUCT ──► per store)             (increments)
SUPPLIER   │    (+ Variants)     │
STORE  ────┘                     │ decrements
                                 ▼
CUSTOMER ──────────────────► SALE (POS or Manual)
                                 │
                          ┌──────┴──────┐
                          │             │
                     ORDER ITEMS    INVOICE
                          │
                     SALE RETURN
                          │
                    RETURN ITEMS ──► INVENTORY (increments)
                          │
                       REPORTS
```

### Full Lifecycle of a Product

```
1. Setup      → Create Category, Brand, Unit, Supplier, Store
2. Create     → Add Product (single or variable) with initial stock
3. Stock In   → Create Purchase → Inventory auto-increments
4. Sell       → POS Checkout → Inventory auto-decrements
5. Return     → Sales Return → Inventory auto-increments
6. Report     → View Sales/Purchase reports, customer dues
```

---

## 15. API Reference

All endpoints (except auth) require: `Authorization: Bearer <token>` header.

### Authentication

| Method | Endpoint | Description | Auth |
|---|---|---|---|
| POST | `/api/auth/login` | Login, returns JWT token | None |
| POST | `/api/auth/signup` | Register new user | None |

### Products

| Method | Endpoint | Description | Role |
|---|---|---|---|
| GET | `/api/products` | List all products | Any |
| GET | `/api/products/pos?storeId=X` | POS products (active, in-stock) | Any |
| POST | `/api/products` | Create product + variants + inventory | Admin |
| PUT | `/api/products/:id` | Update product | Admin |
| DELETE | `/api/products/:id` | Delete product | Admin |
| PUT | `/api/products/:id/lock` | Lock/unlock product for editing | Any |

### Sales

| Method | Endpoint | Description | Role |
|---|---|---|---|
| GET | `/api/sales` | List sales (biller sees own only) | Any |
| POST | `/api/sales` | Create sale (validates + decrements stock) | Any |
| PUT | `/api/sales/:id` | Update sale | Admin |
| DELETE | `/api/sales/:id` | Delete sale (restores inventory) | Admin |

### Purchases

| Method | Endpoint | Description | Role |
|---|---|---|---|
| GET | `/api/purchases` | List purchases | Any |
| POST | `/api/purchases` | Create purchase (increments inventory) | Admin |

### Sales Returns

| Method | Endpoint | Description | Role |
|---|---|---|---|
| GET | `/api/sales-returns` | List all returns | Any |
| POST | `/api/sales-returns` | Create return (increments inventory) | Admin |

### Inventory

| Method | Endpoint | Description | Role |
|---|---|---|---|
| POST | `/api/inventory` | Upsert inventory record | Admin |

### Reports & Dashboard

| Method | Endpoint | Description | Role |
|---|---|---|---|
| GET | `/api/dashboard` | KPIs + chart data | Any |
| GET | `/api/sales/report` | Sales report (filterable) | Any |
| GET | `/api/purchase/report` | Purchase report | Any |

### Master Data (Categories, Brands, Units, etc.)

All follow the same pattern:

| Method | Endpoint | Role |
|---|---|---|
| GET | `/api/{resource}` | Any |
| POST | `/api/{resource}` | Admin |
| PUT | `/api/{resource}/:id` | Admin |
| DELETE | `/api/{resource}/:id` | Admin |

Resources: `categories`, `brands`, `units`, `customers`, `suppliers`, `stores`, `sub-categories`, `billers`, `warehouses`, `product-variants`, `invoices`.

---

> **Tip**: Always complete master data setup (Categories → Brands → Units → Suppliers → Stores) before adding products. Without these, the product creation form dropdowns will be empty.
