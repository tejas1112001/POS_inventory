# GST reports (GSTR-1 export): design note

Status: **approved 2026-11-21** (all of §7 as recommended). Build order in §6.
Source: M3 design §11 Q4 ("GST reports as a separate item after M3; M3 stores everything they need").

> This note describes software behaviour. The return layout and thresholds below follow the GST rules as known on 2026-11-21 and should be checked by the shop's chartered accountant before shops file from them. Points that need that check are marked **(CA)**.

## 1. Goal

A shop (or its accountant) downloads, for one GSTIN and one month (or quarter), a workbook with the figures for **GSTR-1** (outward supplies), laid out like the sections of the GST offline tool, plus a **GSTR-3B summary** of the same period. The app does **not** file anything: it prepares the figures.

Not in this item: filing or uploading to the GST portal (the portal's JSON upload format), e-invoicing (IRN), GSTR-2B matching and purchase tax (the future purchases module).

## 2. What goes in

- **Sales and credit notes made under a GST state** (with the M3 copy: seller GSTIN, place of supply, split), dated in the period in the shop's time zone, for the chosen **seller GSTIN** (each GSTIN files its own return: a store registered in Karnataka is its own report).
- **Voided sales** are left out of the figures and counted as cancelled in the document summary.
- **Sales from before M3** (no copy) are left out, and the report says how many there are in the period (Q3).
- Amounts come from the saved lines: taxable value = line total − tax; CGST, SGST (or UTGST), IGST as split on the line. Shipping is not taxed today and is left out of taxable values.

## 3. Sheets (GSTR-1)

| Sheet | What | Rows |
|---|---|---|
| **b2b** | Invoices to registered buyers (B2B) | One row per invoice and tax rate: buyer GSTIN, buyer name, invoice number, date, invoice value, place of supply, reverse charge (N), invoice type (Regular), rate, taxable value, IGST, CGST, SGST/UTGST, cess (0) |
| **b2cl** | B2C invoices to **another state** above **₹1,00,000** (Q1, CA) | One row per invoice and rate: invoice number, date, value, place of supply, rate, taxable value, IGST |
| **b2cs** | All other B2C supplies | Summed by place of supply and rate: type (OE), place of supply, rate, taxable value, IGST or CGST + SGST |
| **cdnr** | Credit notes to registered buyers | One row per credit note and rate: buyer GSTIN, note number, date, original invoice number and date, note value, place of supply, rate, taxable value, taxes |
| **cdnur** | Credit notes to unregistered buyers **of b2cl invoices** | As cdnr, without GSTIN (type B2CL). Credit notes of b2cs sales reduce b2cs instead |
| **hsn** | HSN summary | By HSN code and UQC: description, unit (UQC), total quantity, total value, taxable value, IGST, CGST, SGST (Q2). Lines without an HSN code are listed apart, so they can be fixed |
| **nil** | Nil-rated supplies (0 % lines) | Taxable value of 0 % lines, inter-state and intra-state, B2B and B2C |
| **docs** | Documents issued | Per number series (invoices; credit notes): first and last number, total issued, cancelled (voided) |

And **gstr3b**: the period's totals for GSTR-3B table 3.1(a) (taxable outward supplies: taxable value, IGST, CGST, SGST) and 3.1(c) (nil-rated), and table 3.2 (inter-state supplies to unregistered persons by place of supply).

## 4. Screens and API

- **Reports → GST returns** (permission `reports:view`; download needs `reports:export`): choose the GSTIN (the shop's and each store's own), the period (a month, or a quarter), see summary tiles (taxable value, tax by kind, invoices, credit notes, warnings) and **Download GSTR-1 workbook** (Excel).
- `GET /api/reports/gst?gstin=&period=2026-11` (or `period=2026-Q3`): the summary and warnings as JSON.
- `GET /api/reports/gst/export?gstin=&period=…&format=xlsx`: the workbook (one sheet per section above). Audited like other exports.
- **Warnings** on screen and in the workbook's first sheet: sales before M3 in the period (left out); B2B lines without an HSN code; HSN codes shorter than the required length (Q4); units with no UQC.

## 5. Data changes

- **DB1** `units.uqc` (text, optional): the GST Unique Quantity Code of a unit (`NOS`, `KGS`, `LTR`, `BOX`, `MTR`, …), set on the Units page from a fixed list in code (Q2). A unit without one is reported as `OTH` with a warning.
- No other schema change: everything else is in the M3 copies.

## 6. Build order (after approval)

1. The GST period figures (one query layer, tested on a two-store, two-state shop with B2B, large and small B2C, credit notes, a voided sale, 0 % lines, a UT store).
2. The workbook export and the JSON summary.
3. The Reports screen; the UQC field on units.

**Built 2026-11-21** (steps 1–3): `internal/reports/gst.go` (figures), `gst_http.go` (`/api/reports/gst/gstins`, `/api/reports/gst`, `/api/reports/gst/export`), tenant 00045 (`units.uqc`), `GET /api/gst/uqcs`; Reports → GST returns; a GST unit code on the Units page. Tests: `TestGSTReturns`, `TestParseGSTPeriod`, `TestUQCs`. The HSN summary is net of credit notes **(CA)**.

## 7. Questions for the owner

| Q | Question | Recommendation |
|---|---|---|
| Q1 | **B2CL threshold**: inter-state B2C invoices above ₹1,00,000 are reported invoice-wise (the limit was ₹2,50,000 before August 2024). Use ₹1,00,000, as a setting? **(CA)** | ₹1,00,000, kept in one place in code so it can change |
| Q2 | **UQC per unit** (the HSN summary needs the GST unit code): add a UQC choice on each unit, default `NOS` for pieces? | Yes; list in code; unset units show as `OTH` with a warning |
| Q3 | **Sales made before M3** (no tax copy): leave them out (with a count in the warnings), or report them from today's shop settings? | Leave them out and warn: their place of supply was never recorded |
| Q4 | **HSN length**: 4 digits for turnover up to ₹5 crore, 6 above (B2B). Warn about codes shorter than a setting (4 by default)? **(CA)** | Yes, warn only |
| Q5 | **Periods**: monthly and quarterly (QRMP) choices? | Yes, both |
| Q6 | **Format**: Excel workbook now, portal JSON later? | Yes |
