import { useCallback, useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ArrowLeft, Ban, PackageCheck, Tag, Truck, Wallet } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { api } from '@/services/api';
import StatusBadge from '@/components/kit/StatusBadge';
import { ErrorState } from '@/components/kit/States';
import usePurchaseActions from '@/components/purchases/usePurchaseActions';
import ReceiveGoodsDialog from '@/components/purchases/ReceiveGoodsDialog';
import PrintLabelsDialog from '@/components/products/PrintLabelsDialog';
import { usePermission } from '@/context/PermissionContext';
import { useModules } from '@/hooks/useModules';
import { formatCurrency, formatDate, formatDateTime } from '@/lib/format';
import { unitQtyText } from '@/lib/saleUnits';
import { fmtQty } from '@/lib/qty';

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
// each one stocks, and how much has arrived), totals and payment, its goods
// receipts, and Receive goods / Record payment / Cancel. Goods can arrive
// in parts (industry P1 item 8); labels print for what arrived.
export default function PurchaseDetail() {
  const { id } = useParams();
  const [p, setP] = useState(null);
  const [error, setError] = useState('');
  const [receipts, setReceipts] = useState([]);
  const [receiving, setReceiving] = useState(false);
  const [labels, setLabels] = useState(null); // a receipt
  const { hasPermission } = usePermission();
  const canApprove = hasPermission('purchases', 'approve');
  const { hasModule } = useModules();
  const canLabels = hasModule('barcode_labels') && hasPermission('labels', 'print');

  const load = useCallback(() => {
    setError('');
    api
      .get(`/api/purchases/${id}`, { quiet: true })
      .then(setP)
      .catch((err) => setError(err.status === 404 ? 'This purchase does not exist.' : err.message));
    if (canApprove) {
      api
        .get(`/api/purchases/${id}/receipts`, { quiet: true })
        .then(setReceipts)
        .catch(() => setReceipts([]));
    }
  }, [id, canApprove]);
  useEffect(load, [load]);
  const actions = usePurchaseActions(load);
  const partial = p?.status === 'Partially received';
  const barcodeOf = (itemId) => (p?.items || []).find((i) => i.id === itemId)?.barcode || null;

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
          <h2 className="mt-1 flex flex-wrap items-center gap-2 text-xl font-semibold">
            Purchase {p.reference} <StatusBadge value={p.status} /> <StatusBadge value={p.payment_status} />
          </h2>
          <p className="text-sm text-muted-foreground">
            {formatDate(p.date)} · {p.supplier?.name}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          {actions.canReceive(p) && (
            <>
              <Button type="button" className="max-sm:h-11" onClick={() => setReceiving(true)}>
                <Truck className="size-4" aria-hidden="true" /> Receive goods
              </Button>
              <Button type="button" variant="outline" className="max-sm:h-11" onClick={() => actions.receive(p)}>
                <PackageCheck className="size-4" aria-hidden="true" /> Receive everything
              </Button>
            </>
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
      {partial && (
        <p role="status" className="rounded-lg bg-warning-bg px-4 py-3 text-sm text-warning-fg">
          Part of this purchase has arrived. The rest is still to come; it cannot be cancelled now (correct stock with a stock adjustment).
        </p>
      )}
      {(p.status === 'Ordered' || p.status === 'Pending' || partial) && !actions.canReceive(p) && (
        <p className="rounded-lg bg-info-bg px-4 py-3 text-sm text-info-fg">Not received yet. Receiving needs the "approve purchases" permission.</p>
      )}

      <div className="grid gap-4 xl:grid-cols-3">
        <section className="overflow-hidden rounded-2xl border bg-card shadow-xs xl:col-span-2" aria-labelledby="pitems-h">
          <h2 id="pitems-h" className="border-b px-4 py-3 text-base font-semibold">
            Items
          </h2>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="bg-muted text-left text-xs uppercase tracking-wide text-slate-600 dark:text-slate-300">
                <tr>
                  <th scope="col" className="px-4 py-2 font-medium">Item</th>
                  <th scope="col" className="hidden px-3 py-2 font-medium md:table-cell">Store</th>
                  <th scope="col" className="px-3 py-2 text-right font-medium">Qty</th>
                  <th scope="col" className="px-3 py-2 text-right font-medium">Received</th>
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
                    <td className="px-3 py-2 text-right tabular-nums">{unitQtyText(i)}</td>
                    <td className={`px-3 py-2 text-right tabular-nums ${n(i.received_qty) < n(i.quantity) && p.status !== 'Cancelled' ? 'text-warning-fg' : ''}`}>
                      {n(i.received_qty)}
                    </td>
                    <td className="px-3 py-2 text-right tabular-nums">{formatCurrency(i.unit_price)}</td>
                    <td className="px-4 py-2 text-right font-medium tabular-nums">{formatCurrency(i.subtotal)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
        <section className="rounded-2xl border bg-card p-4 shadow-xs" aria-labelledby="ptot-h">
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
      {receipts.length > 0 && (
        <section className="overflow-hidden rounded-2xl border bg-card shadow-xs" aria-labelledby="prc-h">
          <h2 id="prc-h" className="border-b px-4 py-3 text-base font-semibold">
            Goods received
          </h2>
          <ul className="divide-y">
            {receipts.map((r) => (
              <li key={r.id} className="flex flex-col gap-2 px-4 py-3 sm:flex-row sm:items-start sm:justify-between">
                <div className="text-sm">
                  <p className="font-medium">
                    {r.reference} · {formatDateTime(r.receivedAt)}
                    {r.receivedBy?.name ? ` · ${r.receivedBy.name}` : ''}
                  </p>
                  <p className="text-muted-foreground">
                    {r.lines
                      .map(
                        (l) =>
                          `${fmtQty(l.quantity)} × ${l.productName}${l.sku ? ` (${l.sku})` : ''}` +
                          ((l.batches || []).length ? ` [${l.batches.map((b) => `${b.batchNo} exp ${b.expiry} × ${b.quantity}`).join('; ')}]` : '')
                      )
                      .join(', ')}
                  </p>
                  {r.note && <p className="text-muted-foreground">{r.note}</p>}
                </div>
                {canLabels && (
                  <Button type="button" variant="outline" size="sm" className="max-sm:h-11" onClick={() => setLabels(r)}>
                    <Tag className="size-4" aria-hidden="true" /> Print labels
                  </Button>
                )}
              </li>
            ))}
          </ul>
        </section>
      )}
      {receiving && (
        <ReceiveGoodsDialog
          purchase={p}
          onClose={() => setReceiving(false)}
          onDone={(out) => {
            setReceiving(false);
            setP(out.purchase);
            load();
            if (canLabels) setLabels(out.receipt);
          }}
        />
      )}
      {labels && (
        <PrintLabelsDialog
          title={`${p.reference} · ${labels.reference}`}
          rows={labels.lines.flatMap((l) =>
            (l.batches || []).length
              ? l.batches.map((b) => ({
                  id: l.variantId,
                  key: `${l.variantId}-${b.batchId}`,
                  batchId: b.batchId,
                  batchLabel: `Batch ${b.batchNo} · Exp ${b.expiry}`,
                  name: l.productName,
                  sku: l.sku,
                  itemBarcode: barcodeOf(l.purchaseItemId),
                  copies: b.quantity,
                }))
              : [{ id: l.variantId, name: l.productName, sku: l.sku, itemBarcode: barcodeOf(l.purchaseItemId), copies: l.quantity }]
          )}
          canGenerate={hasPermission('products', 'edit')}
          onClose={() => setLabels(null)}
          onChanged={load}
        />
      )}
      {actions.sheet}
    </div>
  );
}
