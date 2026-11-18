import { useEffect, useState } from 'react';
import { Loader2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { CheckField, SelectField, TextField } from '@/components/settings/FormKit';
import { formatDate, formatDateTime } from '@/lib/format';
import { api } from '@/services/api';
import { toast } from '../../../shared/toast/store';
import BatchExpiry from './BatchExpiry';
import { WRITE_OFF_REASONS } from '@/lib/writeOffs';

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
  quarantine: 'Quarantined',
  quarantine_release: 'Released from quarantine',
  write_off: 'Written off',
};

// BatchDialog shows one batch (industry P3 item 1, A23): its stock per
// store, its movements, and, for batches:manage, a form to correct its
// details with a reason (A24).
export default function BatchDialog({ batchId, canManage, canWriteOff, canRecall, onClose, onSaved }) {
  const [b, setB] = useState(null);
  const [error, setError] = useState(null);
  const [form, setForm] = useState(null);
  const [busy, setBusy] = useState(false);
  const [move, setMove] = useState(null); // {kind: quarantine | release, storeId, storeName, max, quantity, reason}
  // Industry P3 item 5: write off from a store, recall or lift it, see where it went.
  const [wo, setWo] = useState(null); // {storeId, storeName, qty, quarantined, from, quantity, reason, note}
  const [rc, setRc] = useState(null); // {lift, reason}
  const [trace, setTrace] = useState(null);

  useEffect(() => {
    if (!batchId) return;
    setB(null);
    setForm(null);
    setMove(null);
    setWo(null);
    setRc(null);
    setTrace(null);
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

  // doMove is A29: quarantine units found damaged, or release them back to sale.
  const doMove = async (e) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const out = await api.post(
        `/api/batches/${b.id}/${move.kind}`,
        { storeId: move.storeId, quantity: Number(move.quantity), reason: move.reason },
        { quiet: true },
      );
      setB(out);
      setMove(null);
      toast.success(move.kind === 'quarantine' ? 'Moved to quarantine.' : 'Released back to sale.');
      onSaved?.();
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  };

  const run = async (fn, done) => {
    setBusy(true);
    setError(null);
    try {
      await fn();
      toast.success(done);
      onSaved?.();
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  };

  // A30: one line, posted at once.
  const doWriteOff = (e) => {
    e.preventDefault();
    run(async () => {
      await api.post(
        '/api/write-offs',
        { storeId: wo.storeId, reason: wo.reason, note: wo.note, lines: [{ batchId: b.id, quantity: Number(wo.quantity), from: wo.from }] },
        { quiet: true },
      );
      setB(await api.get(`/api/batches/${b.id}`, { quiet: true }));
      setWo(null);
    }, 'Written off.');
  };

  // A31: recall, or lift a recall, with a reason.
  const doRecall = (e) => {
    e.preventDefault();
    run(async () => {
      setB(await api.post(`/api/batches/${b.id}/${rc.lift ? 'lift-recall' : 'recall'}`, { reason: rc.reason }, { quiet: true }));
      setRc(null);
    }, rc.lift ? 'Recall lifted.' : 'Batch recalled: it can no longer be sold.');
  };

  const showTrace = () => {
    setError(null);
    api.get(`/api/batches/${b.id}/trace`, { quiet: true }).then(setTrace).catch(setError);
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
        {b?.status === 'recalled' && !form && (
          <InlineAlert>
            Recalled{b.recalledAt ? ` on ${formatDateTime(b.recalledAt)}` : ''}: {b.recallReason}. It cannot be sold; write it off or keep it in quarantine.
          </InlineAlert>
        )}
        {b && !form && !move && !wo && !rc && (
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
                  {(canManage || canWriteOff) && (
                    <th scope="col" className="py-1">
                      <span className="sr-only">Actions</span>
                    </th>
                  )}
                </tr>
              </thead>
              <tbody className="divide-y">
                {b.stock.map((s) => (
                  <tr key={s.storeId}>
                    <td className="py-1">{s.storeName}</td>
                    <td className="py-1 text-right tabular-nums">{s.qty}</td>
                    <td className="py-1 text-right tabular-nums">{s.quarantined}</td>
                    {(canManage || canWriteOff) && (
                      <td className="py-1 text-right">
                        {canWriteOff && (s.qty > 0 || s.quarantined > 0) && (
                          <Button type="button" variant="ghost" size="sm" className="max-sm:h-11"
                            onClick={() => setWo({ storeId: s.storeId, storeName: s.storeName, qty: s.qty, quarantined: s.quarantined,
                              from: s.qty > 0 ? 'stock' : 'quarantine', quantity: '1',
                              reason: b.status === 'recalled' ? 'recalled' : b.expired ? 'expired' : 'damaged', note: '' })}>
                            Write off
                          </Button>
                        )}
                        {canManage && s.qty > 0 && (
                          <Button type="button" variant="ghost" size="sm" className="max-sm:h-11"
                            onClick={() => setMove({ kind: 'quarantine', storeId: s.storeId, storeName: s.storeName, max: s.qty, quantity: '1', reason: '' })}>
                            Quarantine
                          </Button>
                        )}
                        {canManage && s.quarantined > 0 && !b.expired && b.status !== 'recalled' && (
                          <Button type="button" variant="ghost" size="sm" className="max-sm:h-11"
                            onClick={() => setMove({ kind: 'release', storeId: s.storeId, storeName: s.storeName, max: s.quarantined, quantity: '1', reason: '' })}>
                            Release
                          </Button>
                        )}
                      </td>
                    )}
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
            {trace && (
              <div>
                <h3 className="mb-1 text-sm font-semibold">Where it went</h3>
                <p className="mb-1 text-xs text-muted-foreground">
                  Sold {trace.sold} · returned {trace.returned} · written off {trace.writtenOff}. Walk-in customers are not named.
                </p>
                <ul className="divide-y rounded-md border text-xs">
                  {trace.sales.length + trace.returns.length + trace.writeOffs.length === 0 && <li className="p-2 text-muted-foreground">Not sold, returned or written off.</li>}
                  {trace.sales.map((s) => (
                    <li key={`s${s.saleId}`} className="p-2">
                      Sale {s.reference} · {formatDateTime(s.date)} · {s.storeName} · × {s.quantity}
                      {s.customerName ? ` · ${s.customerName}${s.customerPhone ? ` (${s.customerPhone})` : ''}` : ''}
                      {s.status === 'Void' ? ' · voided' : ''}
                    </li>
                  ))}
                  {trace.returns.map((x) => (
                    <li key={`r${x.returnId}-${x.bucket}`} className="p-2">
                      Return {x.reference} · {formatDateTime(x.date)} · {x.storeName} · × {x.quantity} · {x.bucket === 'quarantine' ? 'to quarantine' : 'back on sale'}
                    </li>
                  ))}
                  {trace.writeOffs.map((x) => (
                    <li key={`w${x.writeOffId}-${x.from}`} className="p-2">
                      Write-off {x.reference} · {formatDateTime(x.date)} · {x.storeName} · × {x.quantity} · {x.reason}
                    </li>
                  ))}
                </ul>
              </div>
            )}
            <DialogFooter className="flex-wrap gap-2">
              {!b.unknown && (
                <Button type="button" variant="outline" className="max-sm:h-11" onClick={showTrace}>
                  Where it went
                </Button>
              )}
              {canRecall && !b.unknown && (
                <Button type="button" variant={b.status === 'recalled' ? 'outline' : 'destructive'} className="max-sm:h-11"
                  onClick={() => setRc({ lift: b.status === 'recalled', reason: '' })}>
                  {b.status === 'recalled' ? 'Lift recall' : 'Recall'}
                </Button>
              )}
              {canManage && !b.unknown && (
                <Button type="button" variant="outline" className="max-sm:h-11" onClick={startEdit}>
                  Correct details
                </Button>
              )}
            </DialogFooter>
          </div>
        )}
        {b && wo && (
          <form onSubmit={doWriteOff} className="grid gap-3 sm:grid-cols-2">
            <p className="text-sm sm:col-span-2">Take units out of {wo.storeName} for good. This cannot be undone.</p>
            <SelectField id="b-wo-from" label="From" value={wo.from} onChange={(v) => setWo((cur) => ({ ...cur, from: v }))}
              options={[
                ...(wo.qty > 0 ? [{ value: 'stock', label: `Stock (${wo.qty})` }] : []),
                ...(wo.quarantined > 0 ? [{ value: 'quarantine', label: `Quarantine (${wo.quarantined})` }] : []),
              ]} />
            <TextField id="b-wo-qty" label="Quantity" inputMode="numeric" value={wo.quantity}
              onChange={(v) => setWo((cur) => ({ ...cur, quantity: v }))} error={f['lines[0].quantity']} />
            <SelectField id="b-wo-reason" label="Reason" value={wo.reason} onChange={(v) => setWo((cur) => ({ ...cur, reason: v }))} options={WRITE_OFF_REASONS} />
            <TextField id="b-wo-note" label={wo.reason === 'other' ? 'Note' : 'Note (optional)'} value={wo.note}
              onChange={(v) => setWo((cur) => ({ ...cur, note: v }))} error={f.note} maxLength={500} />
            <DialogFooter className="sm:col-span-2">
              <Button type="button" variant="outline" className="max-sm:h-11" onClick={() => setWo(null)} disabled={busy}>
                Cancel
              </Button>
              <Button type="submit" variant="destructive" className="max-sm:h-11" disabled={busy}>
                {busy && <Loader2 className="size-4 animate-spin" aria-hidden="true" />} Write off
              </Button>
            </DialogFooter>
          </form>
        )}
        {b && rc && (
          <form onSubmit={doRecall} className="grid gap-3">
            <p className="text-sm">
              {rc.lift
                ? 'The batch can be sold again. Its quarantined units stay in quarantine until released.'
                : 'Checkout stops selling this batch at once, in every store. Its stock can then only be written off or kept in quarantine.'}
            </p>
            <TextField id="b-rc-reason" label="Reason" value={rc.reason} onChange={(v) => setRc((cur) => ({ ...cur, reason: v }))}
              error={f.reason} maxLength={500} required />
            <DialogFooter>
              <Button type="button" variant="outline" className="max-sm:h-11" onClick={() => setRc(null)} disabled={busy}>
                Cancel
              </Button>
              <Button type="submit" variant={rc.lift ? 'default' : 'destructive'} className="max-sm:h-11" disabled={busy}>
                {busy && <Loader2 className="size-4 animate-spin" aria-hidden="true" />} {rc.lift ? 'Lift recall' : 'Recall batch'}
              </Button>
            </DialogFooter>
          </form>
        )}
        {b && move && (
          <form onSubmit={doMove} className="grid gap-3 sm:grid-cols-2">
            <p className="text-sm sm:col-span-2">
              {move.kind === 'quarantine'
                ? `Take units off sale at ${move.storeName}. They stay counted in quarantine and are never sold.`
                : `Put quarantined units at ${move.storeName} back on sale.`}
            </p>
            <TextField id="b-mv-qty" label={`Quantity (up to ${move.max})`} inputMode="numeric" value={move.quantity}
              onChange={(v) => setMove((cur) => ({ ...cur, quantity: v }))} error={f.quantity} />
            <TextField id="b-mv-reason" label="Reason" value={move.reason} onChange={(v) => setMove((cur) => ({ ...cur, reason: v }))}
              error={f.reason} maxLength={500} required />
            <DialogFooter className="sm:col-span-2">
              <Button type="button" variant="outline" className="max-sm:h-11" onClick={() => setMove(null)} disabled={busy}>
                Cancel
              </Button>
              <Button type="submit" className="max-sm:h-11" disabled={busy}>
                {busy && <Loader2 className="size-4 animate-spin" aria-hidden="true" />} {move.kind === 'quarantine' ? 'Quarantine' : 'Release'}
              </Button>
            </DialogFooter>
          </form>
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
