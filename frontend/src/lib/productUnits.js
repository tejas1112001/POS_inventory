// A product's other units (ARCHITECTURE_REVIEW H3): besides its base unit
// (products.unitId, the unit stock is kept in) a product can be sold or
// bought in other units, each holding `factor` base units (a box of 12
// pieces: factor 12; 500 g of a product kept in kg: factor 0.5). The
// server checks the same rules; checking here gives the message at once.

import { parseQty } from './qty.js';

// MAX_UNITS matches the server (products.MaxUnits).
export const MAX_UNITS = 10;

// emptyUnitRow is a new row of the editor.
export function emptyUnitRow() {
  return { unitId: '', factor: '', forSale: true, forPurchase: true };
}

// unitRowsFromProduct turns a loaded product's units into editor rows.
export function unitRowsFromProduct(product) {
  return (product?.units || []).map((u) => ({
    unitId: String(u.unitId),
    factor: String(u.factor),
    forSale: u.forSale !== false,
    forPurchase: u.forPurchase !== false,
  }));
}

// isBlankRow: a row the user added and left empty (ignored on save).
function isBlankRow(r) {
  return !r.unitId && String(r.factor ?? '').trim() === '';
}

// unitsProblem is the first problem with the rows, or '' when they can be
// saved. baseUnitId is the product's unit; fractional is allowFraction.
export function unitsProblem(rows, baseUnitId, fractional) {
  const used = rows.filter((r) => !isBlankRow(r));
  if (used.length === 0) return '';
  if (!baseUnitId) return "Choose the product's unit first: other units are counted in it.";
  if (used.length > MAX_UNITS) return `A product can have at most ${MAX_UNITS} other units.`;
  const seen = new Set();
  for (let i = 0; i < used.length; i++) {
    const r = used[i];
    const n = `Other unit ${i + 1}`;
    if (!r.unitId) return `${n}: choose a unit.`;
    if (String(r.unitId) === String(baseUnitId)) return `${n}: this is already the product's unit.`;
    if (seen.has(String(r.unitId))) return `${n}: this unit is listed twice.`;
    seen.add(String(r.unitId));
    const f = parseQty(r.factor, { fractional: true });
    if (f === null || f <= 0) return `${n}: enter how many base units it holds (above 0, at most 3 decimals).`;
    if (!fractional && !Number.isInteger(f)) return `${n}: a product sold in whole units needs a whole number here.`;
    if (!r.forSale && !r.forPurchase) return `${n}: use it for sale, purchase or both.`;
  }
  return '';
}

// unitsPayload is the `units` of a product body; blank rows are left out.
export function unitsPayload(rows) {
  return rows
    .filter((r) => !isBlankRow(r))
    .map((r) => ({
      unitId: Number(r.unitId),
      factor: parseQty(r.factor, { fractional: true }),
      forSale: !!r.forSale,
      forPurchase: !!r.forPurchase,
    }));
}
