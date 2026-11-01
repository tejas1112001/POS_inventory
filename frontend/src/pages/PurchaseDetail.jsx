import { useCallback, useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ArrowLeft, Ban, PackageCheck, Wallet } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { api } from '@/services/api';
import StatusBadge from '@/components/kit/StatusBadge';
import { ErrorState } from '@/components/kit/States';
import usePurchaseActions from '@/components/purchases/usePurchaseActions';
import { formatCurrency, formatDate, formatDateTime } from '@/lib/format';

const n = (v) => Number(v) || 0;

function Row({ label, value, strong }) {
  return (
    <div className={`flex justify-between gap-3 py-0.5 ${strong ? 'text-base font-bold' : 'text-sm'}`}>
      <span>{label}</span>
      <span className="tabular-nums">{value}</span>
    </div>
  );
}

// PurchaseDetail (/purchases/:id): the purchase, its lines (with the store
// each one stocks), totals and payment, and Receive / Record payment /
// Cancel.
export default function PurchaseDetail() {
  const { id } = useParams();
  const [p, setP] = useState(null);
  const [error, setError] = useState('');

  const load = useCallback(() => {
    setError('');
    api
      .get(`/api/purchases/${id}`, { quiet: true })
      .then(setP)
      .catch((err) => setError(err.status === 404 ? 'This purchase does not exist.' : err.message));
  }, [id]);
  useEffect(load, [load]);
  const actions = usePurchaseActions(load);

  if (error) {
    return (
      <div className="space-y-3">
        <Link to="/purchases" className="inline-flex items-center gap-1 text-sm text-primary hover:underline">
          <ArrowLeft className="size-4" aria-hidden="true" /> Purchases
        </Link>
        <ErrorState message={error} onRetry={load} />
      </div>
    );
  }
  if (!p) return <div className="h-64 animate-pulse rounded-lg bg-muted" aria-busy="true" aria-label="Loading the purchase" />;

  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
        <div>
          <Link to="/purchases" className="inline-flex items-center gap-1 text-sm text-primary hover:underline">
            <ArrowLeft className="size-4" aria-hidden="true" /> Purchases
          </Link>
          <h1 className="mt-1 flex flex-wrap items-center gap-2 text-xl font-semibold">
            Purchase {p.reference} <StatusBadge value={p.status} /> <StatusBadge value={p.payment_status} />
          </h1>
          <p className="text-sm text-muted-foreground">
            {formatDate(p.date)} · {p.supplier?.name}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          {actions.canReceive(p) && (
            <Button type="button" className="max-sm:h-11" onClick={() => actions.receive(p)}>
              <PackageCheck className="size-4" aria-hidden="true" /> Receive into stock
            </Button>
          )}
          {actions.canPay(p) && (
            <Button type="button" variant="outline" className="max-sm:h-11" onClick={() => actions.pay(p)}>
              <Wallet className="size-4" aria-hidden="true" /> Record payment
            </Button>
          )}
          {actions.canCancel(p) && (
            <Button type="button" variant="outline" className="text-danger hover:text-danger max-sm:h-11" onClick={() => actions.cancel(p)}>
              <Ban className="size-4" aria-hidden="true" /> Cancel
            </Button>
          )}
        </div>
      </div>

      {p.status === 'Received' && (
        <p role="status" className="rounded-lg bg-success-bg px-4 py-3 text-sm text-success-fg">
          Received{p.received_at ? ` on ${formatDateTime(p.received_at)}` : ''}: the items are in stock. Corrections go through stock adjustments.
        </p>
      )}
      {(p.status === 'Ordered' || p.status === 'Pending') && !actions.canReceive(p) && (
        <p className="rounded-lg bg-info-bg px-4 py-3 text-sm text-info-fg">Not received yet. Receiving needs the "approve purchases" permission.</p>
      )}

      <div className="grid gap-4 xl:grid-cols-3">
        <section className="overflow-hidden rounded-lg border bg-card shadow-xs xl:col-span-2" aria-labelledby="pitems-h">
          <h2 id="pitems-h" className="border-b px-4 py-3 text-base font-semibold">
            Items
          </h2>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="bg-muted text-left text-xs uppercase tracking-wide text-muted-foreground">
                <tr>
                  <th scope="col" className="px-4 py-2 font-medium">Item</th>
                  <th scope="col" className="hidden px-3 py-2 font-medium md:table-cell">Store</th>
                  <th scope="col" className="px-3 py-2 text-right font-medium">Qty</th>
                  <th scope="col" className="px-3 py-2 text-right font-medium">Unit cost</th>
                  <th scope="col" className="px-4 py-2 text-right font-medium">Total</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {(p.items || []).map((i) => (
                  <tr key={i.id}>
                    <td className="px-4 py-2">
                      <span className="font-medium">{i.product_name}</span>
                      <span className="block text-xs text-muted-foreground">
                        {i.sku ? `SKU ${i.sku}` : ''}
                        <span className="md:hidden">{i.store?.name ? ` · ${i.store.name}` : ''}</span>
                      </span>
                    </td>
                    <td className="hidden px-3 py-2 md:table-cell">{i.store?.name || '—'}</td>
                    <td className="px-3 py-2 text-right tabular-nums">{i.quantity}</td>
                    <td className="px-3 py-2 text-right tabular-nums">{formatCurrency(i.unit_price)}</td>
                    <td className="px-4 py-2 text-right font-medium tabular-nums">{formatCurrency(i.subtotal)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
        <section className="rounded-lg border bg-card p-4 shadow-xs" aria-labelledby="ptot-h">
          <h2 id="ptot-h" className="mb-2 text-base font-semibold">
            Totals
          </h2>
          <Row label="Subtotal" value={formatCurrency(p.subtotal)} />
          {n(p.discount) > 0 && <Row label="Discount" value={`-${formatCurrency(p.discount)}`} />}
          <div className="my-1 border-t" />
          <Row strong label="Total" value={formatCurrency(p.total)} />
          <Row label="Paid" value={formatCurrency(p.paid)} />
          {n(p.due) > 0 && <Row strong label="Due" value={formatCurrency(p.due)} />}
          {p.note && <p className="mt-3 border-t pt-2 text-sm text-muted-foreground">{p.note}</p>}
        </section>
      </div>
      {actions.sheet}
    </div>
  );
}
