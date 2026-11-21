# M3: GST per store and B2B invoices (design note)

Status: **approved 2026-11-21** (all of §11 as recommended; Q5: purchase tax and input tax credit are a future module, M3 stores the supplier's GSTIN and state only).
Source: ARCHITECTURE_REVIEW M3 (decided: multi-state chains and B2B are in scope) and REMAINING.md.

> This note describes software behaviour. The GST rules it follows (place of supply, invoice contents, numbering) should be checked by the shop's chartered accountant before shops rely on it. Questions that need that check are marked **(CA)**.

## 1. Today

- **One tax identity per shop.** `settings.tax_registration_no` is the shop's GSTIN, and it prints on every receipt as "GSTIN: …".
- **One split per shop.** `settings.tax_split` (`none`, `cgst_sgst` or `igst`) decides how every sale's tax is shown. `internal/pricing` splits each line's tax that way, and the sale line keeps `cgst`, `sgst` and `igst`.
- **No state anywhere.** `stores`, `customers` and `suppliers` have no state and no GSTIN. A shop with stores in two states can't issue correct invoices, and a B2B invoice can't show the buyer's GSTIN.
- **Shop-wide numbering.** Sale numbers come from one shop-wide counter (`doc_counters 'sale'`, `INV-000042`), and returns from another (`RET-000010`). Neither is per store or per financial year.
- **Purchases have no tax.** Purchase lines carry a price and a discount, but no tax.
- **Only POS sales.** The public shop is a catalogue only, so every sale is made at a till (or the back-office Add Sale form).

## 2. Goals

1. Each store sells under the GSTIN and state it is registered in. A store without its own GSTIN uses the shop's.
2. Each sale gets **CGST+SGST** (or **CGST+UTGST**) when the place of supply is in the store's state, and **IGST** when it is in another state. The split used is copied onto the sale, so later setting changes never rewrite an old invoice.
3. **B2B invoices** show the buyer's legal name, GSTIN, address and state, and the place of supply.
4. Optionally, **invoice numbers per store and financial year**, with credit notes numbered the same way.
5. **Shops that set nothing behave exactly as today**, with the same split, numbers and receipts.

Not in this item: e-invoicing (IRN/QR, for turnover above ₹5 crore), e-way bills, GSTR-1 / GSTR-3B files, and tax on purchases (input tax credit). See Q4 and Q5.

## 3. Data (tenant migration, one step)

| ID | Change |
|---|---|
| DB1 | `settings.state_code char(2)`: the shop's home state (GST state code: `27` = Maharashtra, `29` = Karnataka, …). Nullable. |
| DB2 | `stores.gstin text`, `stores.state_code char(2)`, `stores.invoice_code text` (a short code for invoice numbers, e.g. `MUM`). All nullable. A store without them uses the shop's. |
| DB3 | `customers.gstin`, `customers.state_code`, `customers.legal_name` (the registered name, if different), `customers.billing_address`. All nullable. |
| DB4 | `suppliers.gstin`, `suppliers.state_code`. Nullable (stored and shown now; purchase tax is later, Q5). |
| DB5 | Copies on `sales`: `seller_gstin`, `seller_state`, `buyer_gstin`, `buyer_name`, `buyer_address`, `buyer_state`, `place_of_supply` (state code), `tax_split` (the split used: `none`, `cgst_sgst`, `cgst_utgst` or `igst`), and `invoice_kind` (`b2c` or `b2b`). All nullable for old sales. |
| DB6 | Copies on `sale_returns` (credit notes): the same seller/buyer/place fields, taken from the sale (a return without a sale takes them as for a new sale). Line amounts are already pro rata copies of the sale's `cgst`/`sgst`/`igst`. |
| DB7 | `invoice_series (store_id, kind, fy, next_value)`, with kind `sale` or `credit_note` and fy like `2026-27`, for numbering per store and financial year (§6). |

- State codes are checked against a fixed list in code (the GST state code table, including `97` Other Territory), in line with M1's rule of keeping lists in code, not in the schema. The list marks the union territories that use UTGST.
- A GSTIN is checked for format (15 characters: state code, PAN, entity number, `Z`, check digit) and for its check digit. Its first two digits must equal the state it is entered for (Q6).

## 4. Choosing the split for a sale

Done on the server, in `internal/pricing`'s caller (checkout and quote), once per sale:

1. **Seller:** the store's GSTIN and state, falling back to the shop's.
2. **Seller not registered** (no GSTIN anywhere): split `none`, as today for a shop without a GSTIN. The receipt says "Receipt", not "Tax Invoice".
3. **No state known** (the shop and the store have no state code): **the shop's `tax_split` setting, as today.** This keeps every existing shop unchanged until it sets a state.
4. **Place of supply:** by default **the store's state**, since a counter sale is delivered at the store. The cashier can change it for a sale delivered to another state (Q1). For a B2B customer from another state, checkout shows a hint ("Buyer is registered in Karnataka. Delivering there? Change the place of supply.") but does not change it on its own.
5. **Split:** place of supply = seller state → `cgst_sgst` (or `cgst_utgst` for a UT without a legislature); otherwise → `igst`. Amounts split as today (half and half, rounding the odd paisa to CGST).
6. The sale keeps every value it used (DB5). Receipts, reports and credit notes read the copies, never the current settings.

`pricing.Compute` keeps its inputs. The caller passes the derived split instead of the shop setting, so the engine and its tests barely change. `POST /api/sales/quote` returns the split and the place of supply, so checkout shows them before payment.

## 5. B2B invoices

- A sale is **B2B** when its customer has a GSTIN; it then copies the buyer's legal name (or name), GSTIN, billing address and state (DB5).
- The receipt (PDF, screen and print) of a B2B sale gets a **"Bill to"** block (name, GSTIN, address, state) and "Place of supply: Karnataka (29)". Its title is "Tax Invoice" either way. A B2C sale above ₹50,000 to another state needs the buyer's address and state (Q7).
- The seller block prints **the store's** GSTIN (or the shop's).
- HSN codes: a B2B line without an HSN code gets a warning at checkout (it can still be sold). HSN is already copied per line (industry P1 item 10).
- Credit notes (returns) of a B2B sale print the same blocks and name the original invoice number and date.

## 6. Invoice numbering per store and financial year (opt-in)

- A new shop setting, `invoiceNumbering`:
  - `shop` (the default, as today): `INV-000042`, `RET-000010`.
  - `store_year`: separate numbers per store and financial year, restarting on 1 April, formatted `{prefix}/{store code}/{yy-yy}/{n}`, for example `INV/MUM/26-27/000042` (at most 16 characters, as GST allows; the format is checked to fit). Credit notes are numbered the same way with their own prefix (`CN/MUM/26-27/000003`).
- The financial year comes from the sale's date in the shop's time zone.
- Numbers stay gapless per series, using the same locked counter as today (`invoice_series` row per store, kind and year).
- Switching to `store_year` changes only new documents. Old sales keep their numbers. Switching back is allowed, but the setting warns that numbers then mix.
- A store needs an `invoice_code` before `store_year` can be turned on.

## 7. API (summary)

| ID | Change |
|---|---|
| A1 | `GET/PUT /api/settings`: `stateCode`, `invoiceNumbering`, `creditNotePrefix`. |
| A2 | Stores: `gstin`, `stateCode`, `invoiceCode` (validated; GSTIN state = store state). |
| A3 | Customers: `gstin`, `stateCode`, `legalName`, `billingAddress`. Suppliers: `gstin`, `stateCode`. |
| A4 | `POST /api/sales` and `/quote`: optional `placeOfSupply` (state code). The quote returns `taxSplit`, `placeOfSupply`, `sellerGstin`, `invoiceKind`, and warnings (B2B line without HSN). |
| A5 | Sale and return JSON: the DB5/DB6 copies. |
| A6 | `GET /api/gst/states`: the state code list for forms. |
| A7 | Exchanges: the new sale takes the place of supply of the returned sale unless given. |

## 8. Screens

- **Settings → Business:** the shop's state; invoice numbering (and credit note prefix).
- **Settings → Stores:** GSTIN, state, invoice code per store.
- **Customers and suppliers:** GSTIN, state, legal name, billing address (customers).
- **Checkout (new):** under the customer, "B2B · GSTIN …" for a registered buyer; a "Place of supply" line (the store's state by default, changeable); the quote's split shown in the totals ("CGST 9% / SGST 9%" or "IGST 18%").
- **Receipts:** seller GSTIN of the store, Bill to, place of supply, and CGST/SGST/UTGST/IGST rows from the sale's copy (PDF, screen, print).
- The old POS screen: sales there use the store's state as place of supply (no override), following the old-POS decision for units.

## 9. Rollout and compatibility

- The migration only adds nullable columns and a table, and needs no backfill. Old sales keep `tax_split` NULL and are read with today's logic (the shop setting when they were made is not recorded, so the receipt keeps the line amounts it already has).
- A shop that sets no state works exactly as today: rule 4.3.
- Tenant export/restore picks up the new columns and table (FK order: `invoice_series` → `stores`).

## 10. Build order (after approval)

1. DB1–DB4, A1–A3, A6, the state list and GSTIN checks, and the forms (Settings, Stores, Customers, Suppliers). **Done 2026-11-21** (tenant 00039, `internal/gst`, `TestGSTParties`).
2. §4 split per sale, DB5/DB6 copies, A4, A5, A7, and the checkout display.
3. Receipts: PDF, screen and print (§5).
4. Numbering per store and financial year (§6), with DB7 (moved here from step 1: the table comes with the numbering).

Each step ships with integration tests (two stores in two states; a B2B buyer in another state; a UT store; a shop with no state behaving as today; numbering across 31 March / 1 April).

## 11. Questions for the owner (answered 2026-11-21: as recommended)

| Q | Question | Recommendation (approved) |
|---|---|---|
| Q1 | **Place of supply** for a counter sale: always the store's state, with the cashier able to change it for delivery to another state? Or default to the buyer's registered state for B2B? **(CA)** | Store's state by default, changeable per sale; a hint when a B2B buyer is from another state. |
| Q2 | **Invoice numbering per store and financial year**: build it now as an opt-in setting, in the `INV/MUM/26-27/000042` format? | Yes, opt-in; today's shop-wide numbering stays the default. |
| Q3 | **Credit notes** (returns) numbered per store and year too, with prefix `CN`? | Yes, with the same setting. |
| Q4 | **GST reports** (a GSTR-1 style export: B2B invoices, B2C summary, HSN summary, credit notes): in M3, or a separate item after it? | A separate item after M3; M3 stores everything they need. |
| Q5 | **Tax on purchases** (input tax credit): out of scope for M3, storing only the supplier's GSTIN and state now? | Yes, out of scope; purchases have no tax lines today and need their own design. **Owner: a future module (purchase tax and ITC); M3 stores the supplier's GSTIN and state now.** |
| Q6 | A GSTIN whose first two digits don't match the state it is entered for: refuse it, or warn? | Refuse (it can't be right). |
| Q7 | B2C sales above ₹50,000 to another state need the buyer's name, address and state on the invoice. Enforce it at checkout (refuse until entered), or warn? **(CA)** | Refuse at checkout when the place of supply is another state and the total is above ₹50,000 without a buyer address. |
| Q8 | Store **invoice code** format: 2–5 capital letters or digits? | Yes. |
