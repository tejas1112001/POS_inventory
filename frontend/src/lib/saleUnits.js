// Selling in a product's other units (ARCHITECTURE_REVIEW H3). A cart line
// may be in one of the product's sale units (a box of 12): its quantity is
// then in that unit (2 boxes), while stock, the price per unit and the
// server's line quantity stay in base units (24 pieces at the piece price).

import { fmtQty, roundQty } from './qty.js';

// saleUnitsOf lists the units a product is sold in besides its base unit:
// [{ unitId, name, factor }]. POS products carry only sale units; the full
// product record (the lock endpoint) carries all, with forSale.
export function saleUnitsOf(product) {
  return (product?.units || [])
    .filter((u) => u.forSale !== false)
    .map((u) => ({ unitId: u.unitId, name: u.name, factor: Number(u.factor) }));
}

// lineBase is a cart line's quantity in base units.
export function lineBase(line) {
  return roundQty(Number(line.quantity) * (line.factor || 1));
}

// maxInUnit is how many of a unit of `factor` base units fit in `maxQty`
// base units: whole units for a whole-unit product, else to 3 decimals.
export function maxInUnit(maxQty, factor = 1, fractional = false) {
  const n = Number(maxQty) / (factor || 1);
  if (!Number.isFinite(n) || n <= 0) return 0;
  return fractional ? Math.floor(roundQty(n) * 1000) / 1000 : Math.floor(roundQty(n));
}

// unitOptionLabel is the picker label: "Box (12)".
export function unitOptionLabel(u) {
  return `${u.name} (${fmtQty(u.factor)})`;
}

// lineQtyText is a line's quantity for text: "2 Box (24)", or "3".
export function lineQtyText(line) {
  if (!line.unitId) return fmtQty(line.quantity);
  return `${fmtQty(line.quantity)} ${line.unitName} (${fmtQty(lineBase(line))})`;
}
