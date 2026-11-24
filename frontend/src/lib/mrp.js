// MRP (maximum retail price) is the most the customer may pay, tax
// included. Selling above it is refused (pharmacy compliance plan §6.2).
// An MRP is printed for a pack of base units (a strip of 10 tablets:
// mrpPack 10); mrpPack 1 is one base unit. The server checks the same rule
// (pricing.AboveMRP) when a price is saved and on every sale line; these
// helpers only say so early, while a form is being filled in.

const num = (v) => (v === '' || v == null ? null : Number(v));
const cents = (v) => Math.round(v * 100);

// packOf is a pack size, 1 when unset or not a positive number.
export function packOf(mrpPack) {
  const p = num(mrpPack);
  return p == null || Number.isNaN(p) || p <= 0 ? 1 : p;
}

// taxInclusivePrice is one unit's price with its tax. taxType is the
// product's ('Inclusive' / 'Exclusive'); without one the shop default
// (settings.pricesIncludeTax) decides.
export function taxInclusivePrice(price, taxRate, taxType, pricesIncludeTax) {
  const p = Number(price) || 0;
  const inclusive = taxType ? taxType === 'Inclusive' : Boolean(pricesIncludeTax);
  return inclusive ? p : p + (p * (Number(taxRate) || 0)) / 100;
}

// unitShare is one base unit's share of an MRP printed for pack units,
// rounded down to the paisa (₹25.00 for 15 tablets: ₹1.66).
export function unitShare(mrp, mrpPack) {
  return Math.floor((Number(mrp) * 100) / packOf(mrpPack) + 1e-9) / 100;
}

// aboveMrp reports whether a price per base unit, with tax, breaks the MRP:
// the whole pack costs more than the MRP, or one unit sold loose costs more
// than its share. No MRP or no price: never above.
export function aboveMrp({ price, mrp, mrpPack, taxRate, taxType, pricesIncludeTax }) {
  const m = num(mrp);
  if (m == null || Number.isNaN(m) || num(price) == null) return false;
  const withTax = taxInclusivePrice(price, taxRate, taxType, pricesIncludeTax);
  const pack = packOf(mrpPack);
  return cents(withTax * pack) > cents(m) || cents(withTax) > cents(unitShare(m, pack));
}

// mrpText is an MRP as printed: "35.00", or "35.00 for 10" for a pack.
export function mrpText(mrp, mrpPack, format = (v) => Number(v).toFixed(2)) {
  if (mrp == null || mrp === '') return '';
  const pack = packOf(mrpPack);
  return pack === 1 ? format(Number(mrp)) : `${format(Number(mrp))} for ${pack}`;
}

// mrpWarning is the text shown under a price above its MRP.
export function mrpWarning(args, format = (v) => v.toFixed(2)) {
  if (!aboveMrp(args)) return '';
  const withTax = taxInclusivePrice(args.price, args.taxRate, args.taxType, args.pricesIncludeTax);
  return `The price with tax (${format(withTax)} a unit) is above the MRP (${mrpText(args.mrp, args.mrpPack, format)}). Lower the price or correct the MRP: it cannot be saved or sold above MRP.`;
}

// mrpBody is the mrp value a product request sends: a number, or null to
// clear it.
export function mrpBody(v) {
  const m = num(v);
  return m == null || Number.isNaN(m) ? null : m;
}

// mrpPackBody is the mrpPack a request sends: a positive number, or
// undefined to leave it out (the server keeps it; new variants: 1).
export function mrpPackBody(v) {
  const p = num(v);
  return p == null || Number.isNaN(p) || p <= 0 ? undefined : p;
}
