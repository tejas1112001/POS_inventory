const inr2 = new Intl.NumberFormat('en-IN', {
  style: 'currency',
  currency: 'INR',
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
});

const inr0 = new Intl.NumberFormat('en-IN', {
  style: 'currency',
  currency: 'INR',
  maximumFractionDigits: 0,
});

/** ₹1,23,456.00 */
export const money = (n) => inr2.format(n);

/** ₹1,23,456 */
export const money0 = (n) => inr0.format(n);

/** "Tejas Shop" -> "tejas-shop" (the shop's subdomain). */
export const slugify = (s) =>
  (s || '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
