// What a receipt shows of a sale's GST (docs/M3_GST_DESIGN.md §5), as the
// PDF does (internal/receipts): a sale made under a GST state prints from
// its own copy (sale.gst); before M3, or in a shop with no GST state, from
// the shop's settings.

const stateName = (states, code) => {
  const s = (states || []).find((x) => x.code === code);
  return s ? `${s.name} (${code})` : code;
};

// receiptGst returns { title, taxId, placeOfSupply, billTo: [], secondHalf }.
export function receiptGst(sale, settings, taxLabel, states) {
  const g = sale?.gst;
  const out = { title: 'Receipt', taxId: '', placeOfSupply: '', billTo: [], secondHalf: 'SGST' };
  if (g?.sellerState) {
    if (g.sellerGstin) {
      out.title = 'Tax Invoice';
      out.taxId = `GSTIN: ${g.sellerGstin}`;
    }
    if (g.placeOfSupply) out.placeOfSupply = stateName(states, g.placeOfSupply);
    if (g.taxSplit === 'cgst_utgst') out.secondHalf = 'UTGST';
    if (g.invoiceKind === 'b2b') {
      out.billTo = [
        g.buyerName,
        g.buyerGstin && `GSTIN: ${g.buyerGstin}`,
        g.buyerAddress,
        g.buyerState && `State: ${stateName(states, g.buyerState)}`,
      ].filter(Boolean);
    }
    return out;
  }
  const reg = settings?.taxRegistrationNo;
  if (reg) {
    out.title = 'Tax Invoice';
    out.taxId = taxLabel === 'GST' ? `GSTIN: ${reg}` : `${taxLabel} No: ${reg}`;
  }
  return out;
}
