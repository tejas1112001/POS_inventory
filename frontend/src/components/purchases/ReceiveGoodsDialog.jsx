import { useMemo, useState } from 'react';
import { Loader2, PackageCheck, Plus, ScanLine, Trash2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { useScanner } from '@/hooks/useScanner';
import { beep } from '@/lib/scan';
import { unitQtyText } from '@/lib/saleUnits';
import { addQty, fmtQty, roundQty } from '@/lib/qty';
import { api } from '@/services/api';
import { toast } from '../../../shared/toast/store';

const inputCls =
  'h-11 w-24 rounded-md border border-input bg-background px-2 text-right text-base tabular-nums outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 sm:h-9 sm:text-sm';

const smallCls =
  'h-11 w-full min-w-0 rounded-md border border-input bg-background px-2 text-base outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 sm:h-8 sm:text-sm';

const outstanding = (i) => Math.max(0, roundQty((Number(i.quantity) || 0) - (Number(i.received_qty) || 0)));
const emptyBatch = () => ({ batchNo: '', expiry: '', mrp: '', quantity: '' });

// ReceiveGoodsDialog (industry P1 item 8, A16): what arrived now, per
// purchase line, up to what is still outstanding. A scan (SKU or barcode)
// adds one piece to the matching line. Saving stocks it in one goods
// receipt; onDone gets {receipt, purchase}.
//
// Industry P3 item 2 (A26): a line of a product that tracks batches is
// entered per batch (number, expiry, MRP, quantity); its quantity is the
// sum. Batches close to expiry need a confirmation (409 NEAR_EXPIRY).
export default function ReceiveGoodsDialog({ purchase, onClose, onDone }) {
  const open = useMemo(() => (purchase.items || []).filter((i) => outstanding(i) > 0), [purchase]);
  const [qty, setQty] = useState(() => Object.fromEntries(open.map((i) => [i.id, ''])));
  const [batches, setBatches] = useState(() => Object.fromEntries(open.filter((i) => i.tracksBatches).map((i) => [i.id, [emptyBatch()]])));
  const [near, setNear] = useState(null);
  const [confirmFar, setConfirmFar] = useState(false);
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  const [key] = useState(() => (crypto.randomUUID ? crypto.randomUUID() : `grn-${Date.now()}-${Math.random()}`));

  const tracked = (id) => Array.isArray(batches[id]);
  const num = (id) => (tracked(id) ? addQty(...batches[id].map((b) => Number(b.quantity) || 0)) : roundQty(Number(qty[id]) || 0));
  const setBatch = (id, k, field, v) =>
    setBatches((cur) => ({ ...cur, [id]: cur[id].map((b, j) => (j === k ? { ...b, [field]: v } : b)) }));
  const addBatch = (id) => setBatches((cur) => ({ ...cur, [id]: [...cur[id], emptyBatch()] }));
  const dropBatch = (id, k) => setBatches((cur) => ({ ...cur, [id]: cur[id].length > 1 ? cur[id].filter((_, j) => j !== k) : [emptyBatch()] }));
  const units = addQty(...open.map((i) => num(i.id)));

  useScanner(
    (code) => {
      const c = code.trim().toLowerCase();
      // The main barcode or SKU count one; another code of the variant counts
      // its unit's size (a case code, several barcodes).
      let step = 1;
      let hit = open.find((i) => (i.barcode && i.barcode.toLowerCase() === c) || (i.sku && i.sku.toLowerCase() === c));
      if (!hit) {
        hit = open.find((i) => (i.barcodes || []).some((b) => b.code.toLowerCase() === c));
        if (hit) step = Number(hit.barcodes.find((b) => b.code.toLowerCase() === c).factor) || 1;
      }
      if (!hit) {
        beep();
        toast.error(`Nothing still to receive on this purchase has the code ${code}.`);
        return;
      }
      if (addQty(num(hit.id), step) > outstanding(hit)) {
        beep();
        toast.error(`${hit.product_name}: only ${fmtQty(roundQty(outstanding(hit) - num(hit.id)))} more still to receive.`);
        return;
      }
      if (tracked(hit.id)) {
        // A scan adds one to the line's last batch; batch and expiry are typed (Q17).
        setBatches((cur) => {
          const rows = cur[hit.id];
          const last = rows.length - 1;
          return { ...cur, [hit.id]: rows.map((b, j) => (j === last ? { ...b, quantity: String(addQty(b.quantity, step)) } : b)) };
        });
        return;
      }
      setQty((q) => ({ ...q, [hit.id]: String(addQty(q[hit.id], step)) }));
    },
    { enabled: !busy }
  );

  const fillAll = () => {
    setQty(Object.fromEntries(open.map((i) => [i.id, String(outstanding(i))])));
    setBatches((cur) =>
      Object.fromEntries(
        Object.entries(cur).map(([id, rows]) => {
          const i = open.find((x) => String(x.id) === id);
          return [id, rows.length === 1 ? [{ ...rows[0], quantity: String(outstanding(i)) }] : rows];
        })
      )
    );
  };

  const save = async (acceptNearExpiry = false) => {
    setError(null);
    const lines = open
      .filter((i) => num(i.id) > 0)
      .map((i) =>
        tracked(i.id)
          ? {
              purchaseItemId: i.id,
              quantity: num(i.id),
              batches: batches[i.id]
                .filter((b) => Number(b.quantity) > 0)
                .map((b) => ({ batchNo: b.batchNo, expiry: b.expiry, mrp: b.mrp === '' ? null : b.mrp, quantity: Number(b.quantity) })),
            }
          : { purchaseItemId: i.id, quantity: num(i.id) }
      );
    if (!lines.length) {
      setError({ message: 'Enter how many arrived for at least one line.' });
      return;
    }
    setBusy(true);
    try {
      const out = await api.post(
        `/api/purchases/${purchase.id}/receipts`,
        { lines, note, acceptNearExpiry, confirmFarExpiry: confirmFar },
        { quiet: true, headers: { 'Idempotency-Key': key } }
      );
      setNear(null);
      toast.success(`${out.receipt.reference}: ${units} units added to stock.`);
      onDone?.(out);
    } catch (err) {
      if (err?.code === 'NEAR_EXPIRY') {
        setNear(err.details || { batches: [] });
      } else {
        setError(err);
      }
    } finally {
      setBusy(false);
    }
  };

  const fieldErr = (k) => error?.fields && Object.entries(error.fields).find(([f]) => f === k)?.[1];
  const lineIdx = (id) => open.filter((x) => num(x.id) > 0).findIndex((x) => x.id === id);
  const batchErrs = (id) => {
    const idx = lineIdx(id);
    if (idx < 0 || !error?.fields) return [];
    return Object.entries(error.fields)
      .filter(([f]) => f === `lines[${idx}].batches` || f.startsWith(`lines[${idx}].batches[`))
      .map(([f, m]) => {
        const k = /batches\[(\d+)\]/.exec(f);
        return k ? `Batch ${Number(k[1]) + 1}: ${m}` : m;
      });
  };
  const farAsked = Object.values(error?.fields || {}).some((m) => /confirm/.test(m));

  return (
    <Dialog open onOpenChange={(o) => !o && !busy && onClose()}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Receive goods: {purchase.reference}</DialogTitle>
          <DialogDescription>
            Enter what arrived now, or scan each piece. Stock goes in when you save; the rest stays outstanding.
          </DialogDescription>
        </DialogHeader>
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <ScanLine className="size-4" aria-hidden="true" /> Scanner ready: each scan adds one.
        </p>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="text-left text-xs uppercase tracking-wide text-muted-foreground">
              <tr>
                <th scope="col" className="py-2 pr-2 font-medium">Item</th>
                <th scope="col" className="px-2 py-2 text-right font-medium">Ordered</th>
                <th scope="col" className="px-2 py-2 text-right font-medium">Still to come</th>
                <th scope="col" className="py-2 pl-2 text-right font-medium">Arrived now</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {open.map((i, k) => {
                const idx = lineIdx(i.id);
                const err = idx >= 0 ? fieldErr(`lines[${idx}].quantity`) : null;
                if (tracked(i.id)) {
                  return (
                    <tr key={i.id}>
                      <td colSpan={4} className="py-2">
                        <div className="flex flex-wrap items-baseline justify-between gap-2">
                          <span>
                            <span className="font-medium">{i.product_name}</span>
                            <span className="block text-xs text-muted-foreground">
                              {[i.sku && `SKU ${i.sku}`, i.store?.name, `ordered ${unitQtyText(i)}`, `still to come ${fmtQty(outstanding(i))}`]
                                .filter(Boolean)
                                .join(' · ')}
                            </span>
                          </span>
                          <span className="text-sm tabular-nums">Arrived now: {num(i.id)}</span>
                        </div>
                        {err && <span className="block text-xs text-danger">{err}</span>}
                        {batchErrs(i.id).map((m) => (
                          <span key={m} className="block text-xs text-danger">
                            {m}
                          </span>
                        ))}
                        <div className="mt-2 space-y-2">
                          {batches[i.id].map((b, j) => (
                            <div key={j} className="grid grid-cols-2 gap-2 sm:grid-cols-[1fr_8rem_6rem_5rem_auto]">
                              <input
                                className={smallCls}
                                aria-label={`Batch number ${j + 1}: ${i.product_name}`}
                                placeholder="Batch no."
                                maxLength={40}
                                value={b.batchNo}
                                autoFocus={k === 0 && j === 0}
                                onChange={(e) => setBatch(i.id, j, 'batchNo', e.target.value)}
                              />
                              <input
                                className={smallCls}
                                aria-label={`Expiry ${j + 1}: ${i.product_name}`}
                                placeholder="Expiry MM/YYYY"
                                value={b.expiry}
                                onChange={(e) => setBatch(i.id, j, 'expiry', e.target.value)}
                              />
                              <input
                                className={smallCls}
                                aria-label={`MRP ${j + 1}: ${i.product_name}`}
                                placeholder="MRP"
                                inputMode="decimal"
                                value={b.mrp}
                                onChange={(e) => setBatch(i.id, j, 'mrp', e.target.value)}
                              />
                              <input
                                type="number"
                                min="0"
                                step="any"
                                inputMode="numeric"
                                className={`${smallCls} text-right tabular-nums`}
                                aria-label={`Quantity ${j + 1}: ${i.product_name}`}
                                placeholder="Qty"
                                value={b.quantity}
                                onChange={(e) => setBatch(i.id, j, 'quantity', e.target.value)}
                              />
                              <Button type="button" variant="ghost" size="icon" aria-label="Remove this batch" onClick={() => dropBatch(i.id, j)}>
                                <Trash2 className="size-4" aria-hidden="true" />
                              </Button>
                            </div>
                          ))}
                          <Button type="button" variant="outline" size="sm" onClick={() => addBatch(i.id)}>
                            <Plus className="size-4" aria-hidden="true" /> Another batch
                          </Button>
                        </div>
                      </td>
                    </tr>
                  );
                }
                return (
                  <tr key={i.id}>
                    <td className="py-2 pr-2">
                      <span className="font-medium">{i.product_name}</span>
                      <span className="block text-xs text-muted-foreground">
                        {[i.sku && `SKU ${i.sku}`, i.store?.name].filter(Boolean).join(' · ')}
                      </span>
                      {err && <span className="block text-xs text-danger">{err}</span>}
                    </td>
                    <td className="px-2 py-2 text-right tabular-nums">{fmtQty(i.quantity)}</td>
                    <td className="px-2 py-2 text-right tabular-nums">{outstanding(i)}</td>
                    <td className="py-2 pl-2 text-right">
                      <input
                        type="number"
                        min="0"
                        max={outstanding(i)}
                        step="any"
                        inputMode="numeric"
                        aria-label={`Arrived now: ${i.product_name}`}
                        className={inputCls}
                        value={qty[i.id] ?? ''}
                        autoFocus={k === 0}
                        onChange={(e) => setQty((q) => ({ ...q, [i.id]: e.target.value }))}
                      />
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
        <label className="block text-sm">
          <span className="mb-1 block font-medium">Note (optional)</span>
          <input
            className="h-11 w-full rounded-md border border-input bg-background px-3 text-base outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 sm:h-9 sm:text-sm"
            value={note}
            maxLength={1000}
            placeholder="For example the delivery note number"
            onChange={(e) => setNote(e.target.value)}
          />
        </label>
        {error && <InlineAlert kind="error">{error.message}</InlineAlert>}
        {farAsked && (
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={confirmFar} onChange={(e) => setConfirmFar(e.target.checked)} /> The expiry dates are right
          </label>
        )}
        {near && (
          <div role="alertdialog" aria-labelledby="near-h" className="rounded-md border border-amber-300 bg-amber-50 p-3 text-sm dark:border-amber-700 dark:bg-amber-950">
            <p id="near-h" className="font-medium">
              These batches expire within {near.nearExpiryDays} days:
            </p>
            <ul className="my-2 list-disc pl-5">
              {(near.batches || []).map((b) => (
                <li key={`${b.line}-${b.batchNo}`}>
                  {b.batchNo}: expires {b.expiry} ({b.daysLeft} days), {b.quantity} units
                </li>
              ))}
            </ul>
            <div className="flex gap-2">
              <Button type="button" variant="outline" onClick={() => setNear(null)} disabled={busy}>
                Go back
              </Button>
              <Button type="button" onClick={() => save(true)} disabled={busy}>
                Receive them anyway
              </Button>
            </div>
          </div>
        )}
        <DialogFooter className="gap-2 sm:justify-between">
          <Button type="button" variant="outline" onClick={fillAll} disabled={busy}>
            Everything arrived
          </Button>
          <div className="flex gap-2">
            <Button type="button" variant="outline" onClick={onClose} disabled={busy}>
              Close
            </Button>
            <Button type="button" onClick={() => save(false)} disabled={busy || units === 0}>
              {busy ? <Loader2 className="size-4 animate-spin" aria-hidden="true" /> : <PackageCheck className="size-4" aria-hidden="true" />}
              Receive {units > 0 ? `${units} units` : ''}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
