// Stock and lock helpers shared by the checkout screens.

export const LOCK_MS = 2 * 60 * 1000;

// Stock of a variant in the chosen store (the POS list only carries that store's row).
export const variantStock = (v) => Number(v?.Inventories?.[0]?.qty ?? 0);
export const variantsOf = (p) => p?.ProductVariants || [];
export const productStock = (p) => variantsOf(p).reduce((n, v) => n + Math.max(0, variantStock(v)), 0);

// variantLabel: "Size: M, Colour: Red", or the SKU when a variant has no attributes.
export function variantLabel(v) {
  const attrs = v?.attributes && typeof v.attributes === 'object' ? Object.entries(v.attributes).filter(([, x]) => x !== '' && x != null) : [];
  return attrs.length ? attrs.map(([k, x]) => `${k}: ${x}`).join(', ') : v?.sku || `Option ${v?.id}`;
}

// lockedByOther: another till holds this product (locks last two minutes).
export function lockedByOther(p, userId) {
  return !!(p.isLocked && p.locked_by != null && String(p.locked_by) !== String(userId) && Date.now() - new Date(p.lockedAt) < LOCK_MS);
}
