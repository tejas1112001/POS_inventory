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
- **Returns, exchanges, held carts and the quote in units** (2026-11-21, tenant migration 00038). Sale lines record which unit they were sold in (`order_items.unit_id`, returned as `unitId`). A return line can name `unitId`: against a sale it takes only the lines sold in that unit, at the size they were sold at (a box of 12 stays 12 after the product's box becomes 10); without a sale it uses the product's sale unit as it is now. Return lines keep the unit copy (`unitName`, `unitFactor`, `unitQty`; on a return split over several sold lines, `unitQty` is each row's share to three decimals). Exchanges take the same return lines. The quote returns `unitId`/`unitName`/`unitFactor`/`unitQty` per line. Held lines keep `unitId` with the quantity in that unit (as the checkout takes it), plus the unit's name and size when held; the cart's `items` and `estimate` are in base units. Test: `TestReturnAndHeldUnits`.
- **Receipts and reports show the unit** (server side, 2026-11-21). The PDF receipt prints a line sold in another unit as "2 Box (24)" (the Qty column widens only on receipts that have such a line; the price stays per base unit). The sales and purchase reports return `unitName`/`unitFactor`/`unitQty` per row, and their Excel/PDF exports have a "Sold as" / "Bought as" column ("2 Box"); Qty stays in base units so its total adds up. The receipt email lists totals only, so it is unchanged. The screen and print receipts are drawn by the frontend (below).
- **Frontend units** (2026-11-21, four slices):
  - Product forms: an "Other units" editor (`ProductUnitsEditor`, `lib/productUnits.js`).
  - Checkout: a unit picker per cart line ("Piece / Box (12)"); stock limits, totals, batches and store changes on the base quantity; held bills keep the unit (`lib/saleUnits.js`).
  - Add Purchase: a Unit column; the price stays per base unit, with the price per unit shown.
  - Returns against a sale: a line sold in a unit comes back in it (default) or in single units; the sold line's `unitId` is sent.
  - "2 Box (24)" on the order, purchase and return pages, the Receive goods dialog and the sales and purchase report screens. The screen and print receipt keep Qty in base units (so "24 × price" reads right on thermal paper) and tag the line "2 Box (24)".
  - Returns without a sale and the new goods of an exchange: a unit choice per line (the product's sale units now); the refund and the price stay per base unit.
  - Decided 2026-11-21: the old POS screen stays in base units. Selling in units needs the new checkout (Settings: new checkout, off by default; all dev shops have it off).
- **Decimal quantities on the remaining screens** (2026-11-21). Stock counts kept a counted 2.5 as 2 (`Math.floor`); they now keep 3 decimals. The old Add Sale, Add Purchase and return/exchange quantity inputs take decimals (`step="any"`, above 0). Quantities worked out in the browser (still to receive, still returnable, report totals, scan +1) use `addQty`/`roundQty`/`fmtQty`, so no float noise (1.2000000000000002). The server already sends quantities in their shortest form and checks `allowFraction` per product.
- **Imports with decimal quantities** (2026-11-21). Every product template has an optional `allow_fraction` column (yes/no, empty = no). A product marked yes is sold in part units and takes `opening_qty` / `quantity_alert` with up to 3 decimals; the others stay whole. Every row of a product (sizes, colours, pharmacy batches) must say the same. The dry-run preview shows `allowFraction`. Test: `TestImportFractions`.

Left:
(nothing left in H3)

Design notes to keep:
- Prices on sale **and purchase** lines stay **per base unit**: a unit's price is the base price times its factor, and `unit_price × quantity` still equals the line amount.
- Turning `allowFraction` off is refused while any stock, low-stock level or unit factor of the product is in part units.
- A return against a sale converts its units at the size they were sold at (the sold line's copy), never the product's current size.

## M3: GST per store and B2B (decided: multi-state and B2B)

Design note `docs/M3_GST_DESIGN.md`, approved 2026-11-21 (§11 as recommended). Build order in §10. Step 1 done 2026-11-21 (state list, GSTIN checks, shop state, store/customer/supplier GSTIN and state, forms; tenant 00039). Step 2 done 2026-11-21 (the split per sale from the seller's state and the place of supply, B2B buyers, copies on sales and credit notes, the over-₹50,000 rule, the checkout's place of supply; tenant 00040). Step 3 done 2026-11-21 (PDF, screen and print receipts from the sale's copy; the return page shows the credit note's GST). Step 4 done 2026-11-21 (opt-in numbering per store and financial year: MUM/2627/00042, credit notes MUM/2627/C0003; tenant 00041). **M3 is built.** Printable credit note done 2026-11-21 (GET /api/sales/returns/{id}/credit-note: the return's seller, buyer, place of supply and tax split, "Against invoice …"; a Credit note button on the return page). Left: GST reports (GSTR-1 style) as a separate item; purchase tax and input tax credit as a future module; the CA check of Q1 and Q7. Purchase tax and input tax credit: a future module (owner).
- Add GSTIN and state on `stores`, falling back to the shop's.
- Add GSTIN and state on customers and suppliers.
- For each sale, use CGST+SGST or IGST depending on the store's state and the place of supply, and keep a copy on the sale. Today the split is one shop setting (`settings.tax_split`), used in `internal/pricing`.
- The invoice series per store and financial year from the review is part of this item.

## Also decided, not started

- **Price per store:** done 2026-11-21 (tenant 00042 `store_prices`; `PUT /api/products/{id}/store-prices`, null removes; checkout, quote, sale edits, exchanges, the POS list, the product lock, the inventory list, the storefront picker and the online shop of that store use it; a "Prices by store" card on the product page when the shop has more than one store; `TestStorePrices`). Cost and MRP stay per variant. Barcode labels still print the variant price.
- **Several barcodes per variant:** a table of barcodes; scanning and the stock-count code lookup (`qByCode`) search it.
- **Scale barcodes:** weight or price embedded in the EAN-13 (prefix 2x). This needs `allowFraction` products (done) and the barcode table above.

## Housekeeping

- `golangci-lint` reports a pre-existing `ineffassign` at `backend/test/isolation/batch_import_test.go:139` (`store2`).
- `TestBillingReconcile` failed once on a sandbox provider answer (`provider answered 400 BAD_REQUEST_ERROR`). It is unrelated to this work; check it if it repeats.
- Platform MFA is still switched off (`PLATFORM_MFA_DISABLED=true`) and must be turned back on before production.
