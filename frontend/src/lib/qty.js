// Quantities (ARCHITECTURE_REVIEW H3). The server keeps three decimals;
// only a product with allowFraction (sold by weight, length or volume)
// may use them, the rest stay whole numbers.

export const QTY_DECIMALS = 3;
const SCALE = 10 ** QTY_DECIMALS;

// roundQty drops float noise (0.1 + 0.2) to three decimals.
export function roundQty(n) {
  const x = Number(n);
  if (!Number.isFinite(x)) return 0;
  return Math.round(x * SCALE) / SCALE;
}

// addQty adds quantities without float noise.
export function addQty(...qs) {
  return roundQty(qs.reduce((s, q) => s + Math.round(Number(q || 0) * SCALE), 0) / SCALE);
}

// parseQty reads a typed quantity: a number of at least 0 with at most
// three decimals, and whole unless fractional. Returns null otherwise.
export function parseQty(value, { fractional = false } = {}) {
  const s = String(value ?? '').trim();
  if (s === '' || !/^-?\d*(\.\d*)?$/.test(s) || s === '.' || s === '-') return null;
  const x = Number(s);
  if (!Number.isFinite(x) || x < 0) return null;
  const dot = s.indexOf('.');
  if (dot >= 0 && s.length - dot - 1 > QTY_DECIMALS && Number(s.slice(dot + 1 + QTY_DECIMALS)) !== 0) return null;
  if (!fractional && !Number.isInteger(x)) return null;
  return roundQty(x);
}

// qtyStep is the input step for a product's quantities.
export function qtyStep(fractional) {
  return fractional ? '0.001' : '1';
}

// qtyHint is the message for a quantity parseQty refused.
export function qtyHint(fractional) {
  return fractional ? 'Enter a number with at most 3 decimals.' : 'This product is sold in whole units only.';
}

const fmt = new Intl.NumberFormat('en-IN', { maximumFractionDigits: QTY_DECIMALS });

// fmtQty shows a quantity: 2, 1.25, 0.005.
export function fmtQty(n) {
  return fmt.format(roundQty(n));
}
