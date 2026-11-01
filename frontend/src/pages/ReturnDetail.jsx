import { useCallback, useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ArrowLeft } from 'lucide-react';
import { api } from '@/services/api';
import StatusBadge from '@/components/kit/StatusBadge';
import { ErrorState } from '@/components/kit/States';
import { formatCurrency, formatDateTime } from '@/lib/format';

const n = (v) => Number(v) || 0;

function Field({ label, children }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="truncate text-sm font-medium">{children || '—'}</dd>
    </div>
  );
}

// ReturnDetail (/sales-return/:id): one return with its lines, reasons
// and refund.
export default function ReturnDetail() {
  const { id } = useParams();
  const [r, setR] = useState(null);
  const [error, setError] = useState('');

  const load = useCallback(() => {
    setError('');
    api
      .get(`/api/sales/returns/${id}`, { quiet: true })
      .then(setR)
      .catch((err) => setError(err.status === 404 ? 'This return does not exist.' : err.message));
  }, [id]);
  useEffect(load, [load]);

  if (error) {
    return (
      <div className="space-y-3">
        <Link to="/sales-return" className="inline-flex items-center gap-1 text-sm text-primary hover:underline">
          <ArrowLeft className="size-4" aria-hidden="true" /> Returns
        </Link>
        <ErrorState message={error} onRetry={load} />
      </div>
    );
  }
  if (!r) return <div className="h-64 animate-pulse rounded-lg bg-muted" aria-busy="true" aria-label="Loading the return" />;

  return (
    <div className="space-y-4">
      <div>
        <Link to="/sales-return" className="inline-flex items-center gap-1 text-sm text-primary hover:underline">
          <ArrowLeft className="size-4" aria-hidden="true" /> Returns
        </Link>
        <h2 className="mt-1 flex flex-wrap items-center gap-2 text-xl font-semibold">
          Return {r.reference} <StatusBadge value={r.status} /> <StatusBadge value={r.payment_status} />
        </h2>
        <p className="text-sm text-muted-foreground">{formatDateTime(r.date)}</p>
      </div>

      <section className="rounded-lg border bg-card p-4 shadow-xs" aria-label="Return details">
        <dl className="grid grid-cols-2 gap-3 md:grid-cols-4">
          <Field label="Order">
            {r.sale_id ? (
              <Link to={`/sales/${r.sale_id}`} className="text-primary hover:underline">
                {r.saleReference}
              </Link>
            ) : (
              'Without order'
            )}
          </Field>
          <Field label="Customer">{r.customerName}</Field>
          <Field label="Store">{r.storeName}</Field>
          {r.note && <Field label="Note">{r.note}</Field>}
        </dl>
      </section>

      <div className="grid gap-4 xl:grid-cols-3">
        <section className="overflow-hidden rounded-lg border bg-card shadow-xs xl:col-span-2" aria-labelledby="ritems-h">
          <h2 id="ritems-h" className="border-b px-4 py-3 text-base font-semibold">
            Items returned
          </h2>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="bg-muted text-left text-xs uppercase tracking-wide text-slate-600 dark:text-slate-300">
                <tr>
                  <th scope="col" className="px-4 py-2 font-medium">Item</th>
                  <th scope="col" className="hidden px-3 py-2 font-medium md:table-cell">Reason</th>
                  <th scope="col" className="px-3 py-2 text-right font-medium">Qty</th>
                  <th scope="col" className="px-3 py-2 text-right font-medium">Price</th>
                  <th scope="col" className="px-4 py-2 text-right font-medium">Total</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {(r.items || []).map((i) => (
                  <tr key={i.id}>
                    <td className="px-4 py-2">
                      <span className="font-medium">{i.product_name}</span>
                      <span className="block text-xs text-muted-foreground">
                        {i.sku ? `SKU ${i.sku}` : ''}
                        <span className="md:hidden">{i.reason ? ` · ${i.reason}` : ''}</span>
                      </span>
                    </td>
                    <td className="hidden px-3 py-2 md:table-cell">{i.reason || '—'}</td>
                    <td className="px-3 py-2 text-right tabular-nums">{i.quantity}</td>
                    <td className="px-3 py-2 text-right tabular-nums">{formatCurrency(i.unit_price)}</td>
                    <td className="px-4 py-2 text-right font-medium tabular-nums">{formatCurrency(i.subtotal)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
        <section className="rounded-lg border bg-card p-4 shadow-xs" aria-labelledby="rtot-h">
          <h2 id="rtot-h" className="mb-2 text-base font-semibold">
            Refund
          </h2>
          <div className="flex justify-between py-0.5 text-sm">
            <span>Tax included</span>
            <span className="tabular-nums">{formatCurrency(r.tax)}</span>
          </div>
          <div className="flex justify-between border-t py-1 text-base font-bold">
            <span>Total</span>
            <span className="tabular-nums">{formatCurrency(r.total)}</span>
          </div>
          <div className="flex justify-between py-0.5 text-sm">
            <span>Paid back</span>
            <span className="tabular-nums">{formatCurrency(r.paid)}</span>
          </div>
          {n(r.due) > 0 && (
            <div className="flex justify-between py-0.5 text-sm font-semibold">
              <span>Still to pay back</span>
              <span className="tabular-nums">{formatCurrency(r.due)}</span>
            </div>
          )}
        </section>
      </div>
    </div>
  );
}
