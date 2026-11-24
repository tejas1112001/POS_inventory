import { useEffect, useMemo, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { api } from '@/services/api';
import { formatCurrency } from '@/lib/format';
import { toast } from '../../../shared/toast/store';

const key = (variantId, storeId) => `${variantId}:${storeId}`;

// pricesOf is the store prices of a product's variants as typed values.
function pricesOf(product) {
  const out = {};
  for (const v of product?.ProductVariants || []) {
    for (const sp of v.storePrices || []) out[key(v.id, sp.storeId)] = String(Number(sp.price));
  }
  return out;
}

// StorePricesCard sets a variant's own price in a store (tenant 00042):
// empty means the variant price. Cost and MRP stay per variant. Shown when
// the shop has more than one store.
export default function StorePricesCard({ product, stores, canEdit, onSaved }) {
  const saved = useMemo(() => pricesOf(product), [product]);
  const [values, setValues] = useState(saved);
  const [busy, setBusy] = useState(false);
  useEffect(() => setValues(saved), [saved]);
  const variants = product?.ProductVariants || [];
  if (stores.length < 2 || variants.length === 0) return null;

  const changes = [];
  for (const v of variants) {
    for (const s of stores) {
      const k = key(v.id, s.id);
      const now = (values[k] ?? '').trim();
      if (now === (saved[k] ?? '')) continue;
      changes.push({ variantId: v.id, storeId: s.id, price: now === '' ? null : Number(now) });
    }
  }
  const bad = changes.some((c) => c.price !== null && !(Number.isFinite(c.price) && c.price >= 0));

  const save = async () => {
    setBusy(true);
    try {
      await api.put(`/api/products/${product.id}/store-prices`, { prices: changes }, { quiet: true });
      toast.success('Store prices saved.');
      onSaved?.();
    } catch (err) {
      toast.error(err.message || 'The store prices could not be saved.');
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="overflow-hidden rounded-2xl border bg-card shadow-xs" aria-labelledby="sp-h">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b px-4 py-3">
        <div>
          <h2 id="sp-h" className="text-base font-semibold">
            Prices by store
          </h2>
          <p className="text-xs text-muted-foreground">Empty: the option&apos;s own price. Cost and MRP are the same in every store.</p>
        </div>
        {canEdit && (
          <Button size="sm" onClick={save} disabled={busy || changes.length === 0 || bad}>
            {busy ? 'Saving…' : 'Save prices'}
          </Button>
        )}
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead className="bg-muted text-left text-xs uppercase tracking-wide text-slate-600 dark:text-slate-300">
            <tr>
              <th scope="col" className="px-4 py-2 font-medium">Option</th>
              <th scope="col" className="px-3 py-2 text-right font-medium">Price</th>
              {stores.map((s) => (
                <th key={s.id} scope="col" className="px-3 py-2 text-right font-medium">
                  {s.name}
                </th>
              ))}
            </tr>
          </thead>
          <tbody className="divide-y">
            {variants.map((v) => (
              <tr key={v.id}>
                <td className="px-4 py-2">{v.label || v.sku}</td>
                <td className="px-3 py-2 text-right tabular-nums">{formatCurrency(v.price)}</td>
                {stores.map((s) => {
                  const k = key(v.id, s.id);
                  return (
                    <td key={s.id} className="px-3 py-1.5 text-right">
                      {canEdit ? (
                        <Input
                          type="number"
                          min="0"
                          step="0.01"
                          inputMode="decimal"
                          className="ml-auto h-8 w-28 text-right tabular-nums"
                          placeholder={String(Number(v.price))}
                          value={values[k] ?? ''}
                          onChange={(e) => setValues((x) => ({ ...x, [k]: e.target.value }))}
                          aria-label={`Price of ${v.label || v.sku} at ${s.name}`}
                        />
                      ) : (
                        <span className="tabular-nums">{saved[k] ? formatCurrency(saved[k]) : '—'}</span>
                      )}
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}
