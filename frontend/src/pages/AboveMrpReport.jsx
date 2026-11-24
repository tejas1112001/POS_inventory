import { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { EmptyState, ErrorState, LoadingRows } from '@/components/kit/States';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { formatCurrency } from '@/lib/format';
import { mrpText } from '@/lib/mrp';
import { api } from '@/services/api';

const KIND = {
  price: 'Price',
  store_price: 'Store price',
  batch: 'Batch MRP below the price',
  no_mrp: 'No MRP (pharmacy)',
};

// AboveMrpReport (pharmacy plan §6.2): every price the strict MRP rule
// refuses, saved before the rule existed. Each row links to its product
// to fix the price or the MRP; the till refuses these lines until then.
export default function AboveMrpReport() {
  const [data, setData] = useState(null);
  const [error, setError] = useState(null);

  const load = useCallback(() => {
    setError(null);
    api
      .get('/api/products/above-mrp', { quiet: true })
      .then(setData)
      .catch((e) => setError(e?.message || 'The report could not be loaded.'));
  }, []);
  useEffect(load, [load]);

  const rows = data?.rows || [];
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      {error && <ErrorState message={error} onRetry={load} />}
      {!data && !error && <LoadingRows rows={4} />}
      {data && rows.length === 0 && <EmptyState title="Every price is at or below its MRP">Nothing to fix.</EmptyState>}
      {data?.truncated && <InlineAlert className="text-xs">Only the first {rows.length} rows are shown. Fix these, then reload.</InlineAlert>}
      {rows.length > 0 && (
        <div className="overflow-x-auto rounded-md border">
          <table className="w-full text-sm">
            <caption className="sr-only">Prices above MRP</caption>
            <thead className="bg-muted/50 text-left text-xs text-muted-foreground">
              <tr>
                <th scope="col" className="px-3 py-2 font-medium">Product</th>
                <th scope="col" className="px-3 py-2 font-medium">Problem</th>
                <th scope="col" className="px-3 py-2 font-medium">Store / batch</th>
                <th scope="col" className="px-3 py-2 text-right font-medium">Price with tax</th>
                <th scope="col" className="px-3 py-2 text-right font-medium">MRP</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {rows.map((r, i) => (
                <tr key={`${r.kind}-${r.variantId}-${r.store || ''}-${r.batchNo || ''}-${i}`}>
                  <td className="px-3 py-2">
                    <Link to={`/products/${r.productId}`} className="font-medium underline-offset-2 hover:underline">
                      {r.productName}
                    </Link>
                    <span className="block text-xs text-muted-foreground">SKU {r.sku}</span>
                  </td>
                  <td className="px-3 py-2">{KIND[r.kind] || r.kind}</td>
                  <td className="px-3 py-2">{[r.store, r.batchNo && `Batch ${r.batchNo}`].filter(Boolean).join(' · ') || '—'}</td>
                  <td className="px-3 py-2 text-right tabular-nums">{formatCurrency(r.priceWithTax)} a unit</td>
                  <td className="px-3 py-2 text-right tabular-nums">{r.mrp != null ? mrpText(r.mrp, r.mrpPack, formatCurrency) : '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
