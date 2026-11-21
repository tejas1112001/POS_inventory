# Remaining work (architecture review follow-up)

Last updated 2026-11-21. Progress per item is in `status.md`; this file lists what is **not done yet**, in the order it should be picked up.

## H3: decimal quantities and units

Done:
- Every quantity column is `numeric(14,3)` (tenant migration 00036).
- Each product has an `allowFraction` flag, and the Go `qty.Q` type carries quantities through every flow.
- Part units are refused for whole-unit products.
- Products have extra units (`product_units`, migration 00037). A sale line can be sold in one of them (`unitId`); the line keeps a copy of the unit name, its factor and the quantity in that unit.
- Frontend: checkout, the old POS, the batch picker and the product add/edit forms take part units.
- **Purchases in units** (2026-11-21): a purchase line can name `unitId` (one of the product's `forPurchase` units). The quantity is in that unit; the unit price stays **per base unit**; the line keeps the unit name, factor and quantity in that unit (`unitName`, `unitFactor`, `unitQty`). Stock, `received_qty` and goods receipts stay in base units. Code: `internal/purchases/units.go`; test: `TestPurchaseUnits`.

Left:
1. **Returns, held carts and the quote in units.** Today these work in base units only: a return of "1 box" is entered as 12 pieces. Held carts and `POST /api/sales/quote` lines should carry `unitId` and the unit copy.
2. **Receipts and reports show the unit.** The receipt (HTML, PDF and print) and the sales report should show "2 Box (24)" when `unitName` is set. `internal/receipts/view.go` only reads the quantity in base units so far.
3. **Frontend units.**
   - A units editor on the product form (`units: [{unitId, factor, forSale, forPurchase}]`).
   - A unit picker on checkout lines (POS products carry `units`).
   - A unit choice on purchase lines (the API takes `unitId` since 2026-11-21; the price field stays per base unit).
   - Show `unitName`/`unitQty` on sale, order and purchase lines.
4. **Frontend decimal display, remaining screens.** Stock counts, AddSales/AddSalesReturn, the purchase receive dialog and the reports still show raw numbers. Use `fmtQty` from `frontend/src/lib/qty.js`. The inputs already accept decimals (`step="any"`), and the server checks each product's `allowFraction`.
5. **Imports.** Product CSV import (`opening_qty`, `quantity_alert`) still takes whole numbers only, because imported products start as whole-unit. Add an `allow_fraction` column if shops need it.

Design notes to keep:
- Prices on sale **and purchase** lines stay **per base unit**: a unit's price is the base price times its factor, and `unit_price × quantity` still equals the line amount.
- Turning `allowFraction` off is refused while any stock, low-stock level or unit factor of the product is in part units.

## M3: GST per store and B2B (decided: multi-state and B2B)

Not started.
- Add GSTIN and state on `stores`, falling back to the shop's.
- Add GSTIN and state on customers and suppliers.
- For each sale, use CGST+SGST or IGST depending on the store's state and the place of supply, and keep a copy on the sale. Today the split is one shop setting (`settings.tax_split`), used in `internal/pricing`.
- The invoice series per store and financial year from the review is part of this item.

## Also decided, not started

- **Price per store:** a variant price for each store, falling back to the variant price.
- **Several barcodes per variant:** a table of barcodes; scanning and the stock-count code lookup (`qByCode`) search it.
- **Scale barcodes:** weight or price embedded in the EAN-13 (prefix 2x). This needs `allowFraction` products (done) and the barcode table above.

## Housekeeping

- `golangci-lint` reports a pre-existing `ineffassign` at `backend/test/isolation/batch_import_test.go:139` (`store2`).
- `TestBillingReconcile` failed once on a sandbox provider answer (`provider answered 400 BAD_REQUEST_ERROR`). It is unrelated to this work; check it if it repeats.
- Platform MFA is still switched off (`PLATFORM_MFA_DISABLED=true`) and must be turned back on before production.
