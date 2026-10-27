// Checkout helpers shared by the POS and the back-office sale form.

// newIdempotencyKey names one checkout attempt (Idempotency-Key header,
// API_CONTRACT C-04): a retry of the same request with the same key can
// never create a second sale.
export function newIdempotencyKey() {
  if (typeof crypto !== 'undefined' && crypto.randomUUID) return crypto.randomUUID();
  const b = new Uint8Array(16);
  crypto.getRandomValues(b);
  return Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');
}

// priceChanges reads a 409 PRICE_CHANGED error: the lines whose price is
// not the catalog price any more, as {variantId, sku, sent, unitPrice}.
export function priceChanges(err) {
  if (err?.code !== 'PRICE_CHANGED') return null;
  const items = err.details?.items;
  return Array.isArray(items)
    ? items.map((i) => ({ index: i.index, variantId: i.variantId, sku: i.sku, sent: Number(i.sent), unitPrice: Number(i.unitPrice) }))
    : [];
}

// isRetryable tells whether the same request may be sent again with the
// same key (no answer from the server, or a temporary failure).
export function isRetryable(err) {
  return err?.code === 'NETWORK' || err?.status === 503;
}
