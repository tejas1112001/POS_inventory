# End-to-End Flow Test Report

**Date:** 2026-08-18  
**Environment:** Local development (`http://localhost:3000` backend, `http://localhost:5173` frontend)  
**Database:** PostgreSQL `pos` on `localhost:5432`  
**Test user:** `admin@pos.com` (admin role)  
**Result:** ✅ **ALL CHECKS PASSED**

---

## Test Objective

Verify the complete inventory lifecycle:

1. Create required master data and one product
2. Create a purchase and confirm stock increases
3. Sell exactly **1 quantity** through POS and confirm stock decreases by **1**
4. Validate purchase totals, sale totals, and inventory records

---

## Master Data Created

| Entity | ID | Name / Details |
|--------|----|----------------|
| Category | 1 | FLOW-TEST Category |
| Sub-Category | 1 | FLOW-TEST SubCategory (under Category 1) |
| Brand | 1 | FLOW-TEST Brand |
| Unit | 1 | Pieces (`pcs`) |
| Supplier | 1 | FLOW-TEST Supplier — `flowtest-supplier@example.com`, phone `9999999999` |
| Store | 2 | FLOW-TEST Store — 456 Test Avenue |
| Customer | 1 | FLOW-TEST Customer — `flowtest-customer@example.com` |

> **Note:** Store ID 2 was used. Store ID 1 (`islamabad-main`) already existed from seed data.

---

## Product Created

| Field | Value |
|-------|-------|
| Product ID | 1 |
| Name | FLOW-TEST Widget |
| SKU | FLOW-TEST-SKU-001 |
| Variant ID | 1 |
| Type | Single product |
| Selling type | POS |
| Store | FLOW-TEST Store (ID 2) |
| Unit price | ₹100.00 |
| Cost | ₹60.00 |
| Tax | GST 5% (Exclusive) |
| Low-stock alert | 2 units |

**Initial stock:** **1 unit**

> The API requires `quantity >= 1` when creating a single product, so initial stock was set to 1 (not 0).

---

## Purchase Created

| Field | Value |
|-------|-------|
| Purchase ID | 1 |
| Reference | FLOW-TEST-PO-001 |
| Supplier | FLOW-TEST Supplier |
| Status | Received |
| Payment status | Paid |
| Quantity purchased | **10 units** |
| Unit price | ₹60.00 |
| **Total** | **₹600.00** |
| Paid | ₹600.00 |
| Due | ₹0.00 |

### Stock after purchase

| Stage | Stock |
|-------|-------|
| After product create | 1 |
| After purchase (+10) | **11** |

**Verification:** 1 + 10 = **11** ✅

---

## POS Sale Created

| Field | Value |
|-------|-------|
| Sale ID | 1 |
| Reference | FLOW-TEST-POS-001 |
| Customer | FLOW-TEST Customer |
| Store | FLOW-TEST Store (ID 2) |
| Payment method | Cash |
| Status | Completed |
| Payment status | Paid |
| Quantity sold | **1 unit** |
| Unit price | ₹100.00 |
| **Subtotal / Total** | **₹100.00** |
| Paid | ₹100.00 |
| Due | ₹0.00 |

### Stock after sale

| Stage | Stock |
|-------|-------|
| After purchase | 11 |
| After POS sale (−1) | **10** |

**Verification:** 11 − 1 = **10** ✅

---

## Inventory Summary

| Checkpoint | Expected | Actual | Result |
|------------|----------|--------|--------|
| After product create | 1 | 1 | ✅ Pass |
| After purchase | 11 | 11 | ✅ Pass |
| After POS sale | 10 | 10 | ✅ Pass |

---

## Totals Verification

| Record | Expected Total | Actual Total | Result |
|--------|---------------|--------------|--------|
| Purchase (10 × ₹60) | ₹600.00 | ₹600.00 | ✅ Pass |
| Sale (1 × ₹100) | ₹100.00 | ₹100.00 | ✅ Pass |

---

## API Verification Checks

| Check | Result |
|-------|--------|
| Initial stock after product create = 1 | ✅ Pass |
| Stock increased after purchase to 11 | ✅ Pass |
| Stock decreased by 1 after POS sale to 10 | ✅ Pass |
| Purchase total = ₹600 | ✅ Pass |
| Sale total = ₹100 | ✅ Pass |
| Purchase retrievable via `GET /api/purchases` | ✅ Pass |
| Sale retrievable via `GET /api/sales` | ✅ Pass |
| Sale order item quantity = 1 | ✅ Pass |

---

## Final Result

**✅ END-TO-END FLOW TEST PASSED**

All master data, product, purchase, POS sale, inventory quantities, and financial totals behaved correctly. No application code changes were required.

---

## How to Re-run

A reproducible script is available at `scripts/e2e-flow-test.mjs`:

```bash
# Ensure backend is running, then:
node scripts/e2e-flow-test.mjs
```

> Re-running on a database that already contains FLOW-TEST records will fail on duplicate names/SKUs. Use a fresh database or update the `PREFIX` constant in the script.
