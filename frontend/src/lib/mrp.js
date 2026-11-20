// MRP (maximum retail price) is the most one unit may cost the customer,
// tax included. The server checks the same rule (pricing.AboveMRP); these
// helpers only warn early, while a form is being filled in.

const num = (v) => (v === '' || v == null ? null : Number(v));

// taxInclusivePrice is one unit's price with its tax. taxType is the
// product's ('Inclusive' / 'Exclusive'); without one the shop default
// (settings.pricesIncludeTax) decides.
export function taxInclusivePrice(price, taxRate, taxType, pricesIncludeTax) {
  const p = Number(price) || 0;
  const inclusive = taxType ? taxType === 'Inclusive' : Boolean(pricesIncludeTax);
  return inclusive ? p : p + (p * (Number(taxRate) || 0)) / 100;
}

// aboveMrp reports whether the price with tax is above the MRP. No MRP or
// no price: never above.
export function aboveMrp({ price, mrp, taxRate, taxType, pricesIncludeTax }) {
  const m = num(mrp);
  if (m == null || Number.isNaN(m) || num(price) == null) return false;
  return Math.round(taxInclusivePrice(price, taxRate, taxType, pricesIncludeTax) * 100) > Math.round(m * 100);
}

// mrpWarning is the text shown under a price above its MRP.
export function mrpWarning(args, format = (v) => v.toFixed(2)) {
  if (!aboveMrp(args)) return '';
  const withTax = taxInclusivePrice(args.price, args.taxRate, args.taxType, args.pricesIncludeTax);
  return `The price with tax (${format(withTax)}) is above the MRP (${format(Number(args.mrp))}). You can still save and sell it.`;
}

// mrpBody is the mrp value a product request sends: a number, or null to
// clear it.
export function mrpBody(v) {
  const m = num(v);
  return m == null || Number.isNaN(m) ? null : m;
}
