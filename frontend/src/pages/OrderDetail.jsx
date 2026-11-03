import { formatDay, methodLabel } from '@/lib/payments';
import { useCallback, useEffect, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { ArrowLeft, Ban, FileText, Printer, Undo2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { usePermission } from '@/context/PermissionContext';
import { api } from '@/services/api';
import StatusBadge from '@/components/kit/StatusBadge';
import { ErrorState } from '@/components/kit/States';
import Receipt from '@/components/Receipt';
import VoidSaleDialog from '@/components/orders/VoidSaleDialog';
import { formatCurrency, formatDateTime } from '@/lib/format';

const n = (v) => Number(v) || 0;

function attrText(a) {
  if (!a || typeof a !== 'object') return '';
  return Object.keys(a)
    .sort()
    .filter((k) => a[k] !== '' && a[k] != null)
    .map((k) => `${k}: ${a[k]}`)
    .join(', ');
}

function Field({ label, children }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="truncate text-sm font-medium">{children || '—'}</dd>
    </div>
  );
}

function Row({ label, value, strong, muted }) {
  return (
    <div className={`flex justify-between gap-3 py-0.5 ${strong ? 'text-base font-bold' : 'text-sm'} ${muted ? 'text-muted-foreground' : ''}`}>
      <span>{label}</span>
      <span className="tabular-nums">{value}</span>
    </div>
  );
}

// OrderDetail (/sales/:id): one order with its lines (and what was
// returned), totals and payment, its returns, and the actions: receipt,
// print, return items, void with a reason.
export default function OrderDetail() {
  const { id } = useParams();
  const navigate = useNavigate();
  const { hasPermission } = usePermission();
  const [sale, setSale] = useState(null);
  const [returns, setReturns] = useState([]);
  const [error, setError] = useState('');
  const [receipt, setReceipt] = useState(null); // { print }
  const [voiding, setVoiding] = useState(false);

  const load = useCallback(() => {
    setError('');
    api
      .get(`/api/sales/${id}`, { quiet: true })
      .then(setSale)
      .catch((err) => setError(err.status === 404 ? 'This order does not exist (or belongs to another cashier).' : err.message));
    if (hasPermission('sales_returns', 'view')) {
      api
        .get(`/api/sales/returns?saleId=${id}&limit=50`, { quiet: true })
        .then((r) => setReturns(Array.isArray(r) ? r : r?.data || []))
        .catch(() => setReturns([]));
    }
  }, [id, hasPermission]);

  useEffect(load, [load]);

  if (error) {
    return (
      <div className="space-y-3">
        <Link to="/sales" className="inline-flex items-center gap-1 text-sm text-primary hover:underline">
          <ArrowLeft className="size-4" aria-hidden="true" /> Orders
        </Link>
        <ErrorState message={error} onRetry={load} />
      </div>
    );
  }
  if (!sale) return <div className="h-64 animate-pulse rounded-lg bg-muted" aria-busy="true" aria-label="Loading the order" />;

  const voided = sale.status === 'Void';
  const items = sale.items || [];
  const returnable = !voided && items.some((i) => n(i.quantity) > n(i.returnedQty));

  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
        <div className="min-w-0">
          <Link to="/sales" className="inline-flex items-center gap-1 text-sm text-primary hover:underline">
            <ArrowLeft className="size-4" aria-hidden="true" /> Orders
          </Link>
          <h2 className="mt-1 flex flex-wrap items-center gap-2 text-xl font-semibold">
            Order {sale.reference}
            <StatusBadge value={sale.status} />
            <StatusBadge value={sale.payment_status} />
          </h2>
          <p className="text-sm text-muted-foreground">{formatDateTime(sale.date)}</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button type="button" variant="outline" className="max-sm:h-11" onClick={() => setReceipt({ print: false })}>
            <FileText className="size-4" aria-hidden="true" /> Receipt
          </Button>
          <Button type="button" variant="outline" className="max-sm:h-11" onClick={() => setReceipt({ print: true })}>
            <Printer className="size-4" aria-hidden="true" /> Print
          </Button>
          {returnable && hasPermission('sales_returns', 'create') && (
            <Button type="button" variant="outline" className="max-sm:h-11" onClick={() => navigate(`/add-sales-return?saleId=${sale.id}`)}>
              <Undo2 className="size-4" aria-hidden="true" /> Return items
            </Button>
          )}
          {!voided && hasPermission('sales', 'delete') && (
            <Button type="button" variant="outline" className="text-danger hover:text-danger max-sm:h-11" onClick={() => setVoiding(true)}>
              <Ban className="size-4" aria-hidden="true" /> Void…
            </Button>
          )}
        </div>
      </div>

      {voided && (
        <div role="status" className="rounded-lg border border-danger/30 bg-danger-bg px-4 py-3 text-sm text-danger-fg">
          <strong>Voided</strong>
          {sale.voided_at ? ` on ${formatDateTime(sale.voided_at)}` : ''}
          {sale.void_reason ? `: ${sale.void_reason}` : ''}. The stock went back.
        </div>
      )}

      <section className="rounded-lg border bg-card p-4 shadow-xs" aria-label="Order details">
        <dl className="grid grid-cols-2 gap-3 md:grid-cols-4">
          <Field label="Customer">{sale.customer?.name}</Field>
          <Field label="Store">{sale.Store?.name}</Field>
          <Field label="Cashier">{sale.user?.name}</Field>
          <Field label="Payment">
            {sale.payments?.length
              ? `${[...new Set(sale.payments.filter((p) => p.amount > 0).map((p) => methodLabel(p.method)))].join(' + ')} (${sale.payment_status})`
              : sale.payment_method
                ? `${sale.payment_method} (${sale.payment_status})`
                : sale.payment_status}
          </Field>
          {sale.dueDate && n(sale.due) > 0 && <Field label="Due by">{formatDay(sale.dueDate)}</Field>}
          {sale.receipt_email && <Field label="Receipt emailed to">{sale.receipt_email}</Field>}
          {sale.note && <Field label="Sale note">{sale.note}</Field>}
          {sale.payment_note && <Field label="Payment note">{sale.payment_note}</Field>}
        </dl>
      </section>

      <div className="grid gap-4 xl:grid-cols-3">
        <section className="overflow-hidden rounded-lg border bg-card shadow-xs xl:col-span-2" aria-labelledby="items-h">
          <h2 id="items-h" className="border-b px-4 py-3 text-base font-semibold">
            Items
          </h2>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="bg-muted text-left text-xs uppercase tracking-wide text-slate-600 dark:text-slate-300">
                <tr>
                  <th scope="col" className="px-4 py-2 font-medium">Item</th>
                  <th scope="col" className="px-3 py-2 text-right font-medium">Qty</th>
                  <th scope="col" className="px-3 py-2 text-right font-medium">Price</th>
                  <th scope="col" className="hidden px-3 py-2 text-right font-medium sm:table-cell">Disc.</th>
                  <th scope="col" className="hidden px-3 py-2 text-right font-medium sm:table-cell">Tax</th>
                  <th scope="col" className="px-4 py-2 text-right font-medium">Total</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {items.map((i) => {
                  const attrs = attrText(i.variant?.attributes);
                  return (
                    <tr key={i.id}>
                      <td className="px-4 py-2">
                        <span className="font-medium">{i.product_name || i.name}</span>
                        {attrs && <span className="text-muted-foreground"> ({attrs})</span>}
                        <span className="block text-xs text-muted-foreground">
                          {i.sku ? `SKU ${i.sku}` : ''}
                          {n(i.returnedQty) > 0 && <span className="ml-1 font-medium text-warning-fg">· {i.returnedQty} returned</span>}
                        </span>
                      </td>
                      <td className="px-3 py-2 text-right tabular-nums">{i.quantity}</td>
                      <td className="px-3 py-2 text-right tabular-nums">{formatCurrency(i.unit_price)}</td>
                      <td className="hidden px-3 py-2 text-right tabular-nums sm:table-cell">{n(i.discount) ? formatCurrency(i.discount) : '—'}</td>
                      <td className="hidden px-3 py-2 text-right tabular-nums sm:table-cell">{n(i.tax_rate) ? `${n(i.tax_rate)}%` : '—'}</td>
                      <td className="px-4 py-2 text-right font-medium tabular-nums">{formatCurrency(i.subtotal)}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </section>

        <section className="rounded-lg border bg-card p-4 shadow-xs" aria-labelledby="totals-h">
          <h2 id="totals-h" className="mb-2 text-base font-semibold">
            Totals
          </h2>
          <Row label="Subtotal" value={formatCurrency(sale.subtotal)} />
          {n(sale.discount) > 0 && <Row label="Discount" value={`-${formatCurrency(sale.discount)}`} />}
          <Row label="Tax" value={formatCurrency(sale.tax)} />
          {n(sale.shipping) > 0 && <Row label="Shipping" value={formatCurrency(sale.shipping)} />}
          <div className="my-1 border-t" />
          <Row strong label="Total" value={formatCurrency(sale.total)} />
          {(sale.payments || [])
            .filter((p) => p.method !== 'credit')
            .map((p) => (
              <Row
                key={p.id}
                muted
                label={`${p.customerPaymentId ? (n(p.amount) < 0 ? 'Reversed' : 'Collected') + ' · ' : ''}${methodLabel(p.method)}${p.reference ? ` (${p.reference})` : ''}`}
                value={formatCurrency(p.amount)}
              />
            ))}
          <Row label="Paid" value={formatCurrency(sale.paid)} />
          {n(sale.tendered) > 0 && <Row muted label="Received" value={formatCurrency(sale.tendered)} />}
          {n(sale.tendered) > 0 && <Row muted label="Change" value={formatCurrency(sale.change)} />}
          {n(sale.due) > 0 && <Row strong label={sale.overdue ? 'Due (overdue)' : 'Balance due'} value={formatCurrency(sale.due)} />}
        </section>
      </div>

      {hasPermission('sales_returns', 'view') && (
        <section className="rounded-lg border bg-card shadow-xs" aria-labelledby="returns-h">
          <h2 id="returns-h" className="border-b px-4 py-3 text-base font-semibold">
            Returns
          </h2>
          {returns.length === 0 ? (
            <p className="px-4 py-4 text-sm text-muted-foreground">Nothing returned from this order.</p>
          ) : (
            <ul className="divide-y">
              {returns.map((r) => (
                <li key={r.id} className="flex flex-wrap items-center justify-between gap-2 px-4 py-2 text-sm">
                  <span className="flex items-center gap-2">
                    <Link to={`/sales-return/${r.id}`} className="font-medium text-primary hover:underline">
                      {r.reference}
                    </Link>
                    <span className="text-muted-foreground">{formatDateTime(r.date)}</span>
                  </span>
                  <span className="flex items-center gap-2">
                    <span className="font-medium tabular-nums">{formatCurrency(r.total)}</span>
                    <StatusBadge value={r.payment_status} />
                  </span>
                </li>
              ))}
            </ul>
          )}
        </section>
      )}

      {receipt && <Receipt sale={sale} autoPrint={receipt.print} onClose={() => setReceipt(null)} />}
      <VoidSaleDialog sale={voiding ? sale : null} onClose={() => setVoiding(false)} onVoided={load} />
    </div>
  );
}
