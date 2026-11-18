import { useEffect, useState } from 'react';
import { Loader2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { CheckField, TextField } from '@/components/settings/FormKit';
import { formatDate, formatDateTime } from '@/lib/format';
import { api } from '@/services/api';
import { toast } from '../../../shared/toast/store';
import BatchExpiry from './BatchExpiry';

const REASONS = {
  opening: 'Opening stock',
  sale: 'Sale',
  sale_edit: 'Sale edited',
  sale_void: 'Sale voided',
  return: 'Return',
  purchase_receive: 'Received',
  adjustment: 'Adjustment',
  manual_set: 'Stock set',
  product_edit: 'Product edit',
  import: 'Import',
  stock_count: 'Stock count',
  tracking_on: 'Tracking switched on',
  tracking_off: 'Tracking switched off',
};

// BatchDialog shows one batch (industry P3 item 1, A23): its stock per
// store, its movements, and, for batches:manage, a form to correct its
// details with a reason (A24).
export default function BatchDialog({ batchId, canManage, onClose, onSaved }) {
  const [b, setB] = useState(null);
  const [error, setError] = useState(null);
  const [form, setForm] = useState(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!batchId) return;
    setB(null);
    setForm(null);
    setError(null);
    api.get(`/api/batches/${batchId}`, { quiet: true }).then(setB).catch(setError);
  }, [batchId]);

  const startEdit = () =>
    setForm({
      batchNo: b.batchNo,
      expiry: b.expiry || '',
      mfgDate: b.mfgDate || '',
      mrp: b.mrp ?? '',
      cost: b.cost ?? '',
      reason: '',
    });

  const save = async (e) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    const body = { reason: form.reason, confirmFarExpiry: !!form.confirmFar };
    if (form.batchNo !== b.batchNo) body.batchNo = form.batchNo;
    if (form.expiry !== (b.expiry || '')) body.expiry = form.expiry;
    if (form.mfgDate !== (b.mfgDate || '')) body.mfgDate = form.mfgDate;
    if (String(form.mrp) !== String(b.mrp ?? '')) body.mrp = form.mrp === '' ? null : form.mrp;
    if (String(form.cost) !== String(b.cost ?? '')) body.cost = form.cost === '' ? null : form.cost;
    try {
      const out = await api.put(`/api/batches/${b.id}`, body, { quiet: true });
      setB(out);
      setForm(null);
      toast.success('Batch corrected.');
      onSaved?.();
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  };

  const f = error?.fields || {};
  const set = (k) => (v) => setForm((cur) => ({ ...cur, [k]: v }));
  return (
    <Dialog open={!!batchId} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{b ? `${b.productName} · ${b.unknown ? 'No batch' : `Batch ${b.batchNo}`}` : 'Batch'}</DialogTitle>
          <DialogDescription>
            {b?.unknown
              ? 'Stock from before batches were tracked, or received without a batch. It is sold after every dated batch.'
              : 'Stock of this batch in each store, and every change to it.'}
          </DialogDescription>
        </DialogHeader>
        {error && !error.fields && <InlineAlert>{error.message}</InlineAlert>}
        {!b && !error && <Loader2 className="mx-auto size-5 animate-spin" aria-label="Loading" />}
        {b && !form && (
          <div className="space-y-4 text-sm">
            <dl className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              <div>
                <dt className="text-xs text-muted-foreground">Expiry</dt>
                <dd>
                  <BatchExpiry batch={b} />
                </dd>
              </div>
              <div>
                <dt className="text-xs text-muted-foreground">Manufactured</dt>
                <dd>{b.mfgDate ? formatDate(b.mfgDate) : '—'}</dd>
              </div>
              <div>
                <dt className="text-xs text-muted-foreground">MRP</dt>
                <dd>{b.mrp ?? '—'}</dd>
              </div>
              <div>
                <dt className="text-xs text-muted-foreground">Cost</dt>
                <dd>{b.cost ?? '—'}</dd>
              </div>
            </dl>
            <table className="w-full">
              <caption className="sr-only">Stock per store</caption>
              <thead className="text-left text-xs uppercase text-muted-foreground">
                <tr>
                  <th scope="col" className="py-1 font-medium">
                    Store
                  </th>
                  <th scope="col" className="py-1 text-right font-medium">
                    In stock
                  </th>
                  <th scope="col" className="py-1 text-right font-medium">
                    Quarantined
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {b.stock.map((s) => (
                  <tr key={s.storeId}>
                    <td className="py-1">{s.storeName}</td>
                    <td className="py-1 text-right tabular-nums">{s.qty}</td>
                    <td className="py-1 text-right tabular-nums">{s.quarantined}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            <div>
              <h3 className="mb-1 text-sm font-semibold">Movements</h3>
              <ul className="divide-y rounded-md border">
                {b.movements.length === 0 && <li className="p-2 text-muted-foreground">No movements.</li>}
                {b.movements.map((m) => (
                  <li key={m.id} className="flex flex-wrap items-baseline justify-between gap-2 p-2">
                    <span>
                      {REASONS[m.reason] || m.reason}
                      {m.refType && m.refId ? ` #${m.refId}` : ''} · {m.storeName}
                      {m.bucket === 'quarantine' ? ' · quarantine' : ''}
                    </span>
                    <span className="tabular-nums">
                      <span className={m.delta < 0 ? 'text-destructive' : 'text-emerald-700 dark:text-emerald-400'}>
                        {m.delta > 0 ? `+${m.delta}` : m.delta}
                      </span>{' '}
                      → {m.qtyAfter}
                    </span>
                    <span className="w-full text-xs text-muted-foreground">
                      {formatDateTime(m.createdAt)}
                      {m.userName ? ` · ${m.userName}` : ''}
                    </span>
                  </li>
                ))}
              </ul>
            </div>
            {canManage && !b.unknown && (
              <DialogFooter>
                <Button type="button" variant="outline" className="max-sm:h-11" onClick={startEdit}>
                  Correct details
                </Button>
              </DialogFooter>
            )}
          </div>
        )}
        {b && form && (
          <form onSubmit={save} className="grid gap-3 sm:grid-cols-2">
            <TextField id="b-no" label="Batch number" value={form.batchNo} onChange={set('batchNo')} error={f.batchNo} maxLength={40} />
            <div className="space-y-2">
              <TextField id="b-exp" label="Expiry" hint="YYYY-MM-DD or MM/YYYY" value={form.expiry} onChange={set('expiry')} error={f.expiry} />
              {(form.confirmFar || /confirm/.test(f.expiry || '')) && (
                <CheckField id="b-exp-confirm" label="This expiry date is right" checked={!!form.confirmFar} onChange={set('confirmFar')} />
              )}
            </div>
            <TextField id="b-mfg" label="Manufactured" hint="YYYY-MM-DD, optional" value={form.mfgDate} onChange={set('mfgDate')} error={f.mfgDate} />
            <TextField id="b-mrp" label="MRP" inputMode="decimal" value={form.mrp} onChange={set('mrp')} error={f.mrp} />
            <TextField id="b-cost" label="Cost" inputMode="decimal" value={form.cost} onChange={set('cost')} error={f.cost} />
            <TextField id="b-reason" label="Reason" value={form.reason} onChange={set('reason')} error={f.reason} maxLength={500} required />
            <p className="text-xs text-muted-foreground sm:col-span-2">Receipts of sales already made keep the details they were sold with.</p>
            <DialogFooter className="sm:col-span-2">
              <Button type="button" variant="outline" className="max-sm:h-11" onClick={() => setForm(null)} disabled={busy}>
                Cancel
              </Button>
              <Button type="submit" className="max-sm:h-11" disabled={busy}>
                {busy && <Loader2 className="size-4 animate-spin" aria-hidden="true" />} Save
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
