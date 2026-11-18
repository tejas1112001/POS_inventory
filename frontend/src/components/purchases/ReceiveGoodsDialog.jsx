import { useMemo, useState } from 'react';
import { Loader2, PackageCheck, ScanLine } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { useScanner } from '@/hooks/useScanner';
import { beep } from '@/lib/scan';
import { api } from '@/services/api';
import { toast } from '../../../shared/toast/store';

const inputCls =
  'h-11 w-24 rounded-md border border-input bg-background px-2 text-right text-base tabular-nums outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 sm:h-9 sm:text-sm';

const outstanding = (i) => Math.max(0, (Number(i.quantity) || 0) - (Number(i.received_qty) || 0));

// ReceiveGoodsDialog (industry P1 item 8, A16): what arrived now, per
// purchase line, up to what is still outstanding. A scan (SKU or barcode)
// adds one piece to the matching line. Saving stocks it in one goods
// receipt; onDone gets {receipt, purchase}.
export default function ReceiveGoodsDialog({ purchase, onClose, onDone }) {
  const open = useMemo(() => (purchase.items || []).filter((i) => outstanding(i) > 0), [purchase]);
  const [qty, setQty] = useState(() => Object.fromEntries(open.map((i) => [i.id, ''])));
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  const [key] = useState(() => (crypto.randomUUID ? crypto.randomUUID() : `grn-${Date.now()}-${Math.random()}`));

  const num = (id) => Number(qty[id]) || 0;
  const units = open.reduce((s, i) => s + num(i.id), 0);

  useScanner(
    (code) => {
      const c = code.trim().toLowerCase();
      const hit = open.find((i) => (i.barcode && i.barcode.toLowerCase() === c) || (i.sku && i.sku.toLowerCase() === c));
      if (!hit) {
        beep();
        toast.error(`Nothing still to receive on this purchase has the code ${code}.`);
        return;
      }
      if (num(hit.id) >= outstanding(hit)) {
        beep();
        toast.error(`${hit.product_name}: all ${outstanding(hit)} still to receive are already counted.`);
        return;
      }
      setQty((q) => ({ ...q, [hit.id]: String((Number(q[hit.id]) || 0) + 1) }));
    },
    { enabled: !busy }
  );

  const fillAll = () => setQty(Object.fromEntries(open.map((i) => [i.id, String(outstanding(i))])));

  const save = async () => {
    setError(null);
    const lines = open.filter((i) => num(i.id) > 0).map((i) => ({ purchaseItemId: i.id, quantity: num(i.id) }));
    if (!lines.length) {
      setError({ message: 'Enter how many arrived for at least one line.' });
      return;
    }
    setBusy(true);
    try {
      const out = await api.post(`/api/purchases/${purchase.id}/receipts`, { lines, note }, { quiet: true, headers: { 'Idempotency-Key': key } });
      toast.success(`${out.receipt.reference}: ${units} units added to stock.`);
      onDone?.(out);
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  };

  const fieldErr = (k) => error?.fields && Object.entries(error.fields).find(([f]) => f === k)?.[1];

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
                const idx = open.filter((x) => num(x.id) > 0).findIndex((x) => x.id === i.id);
                const err = idx >= 0 ? fieldErr(`lines[${idx}].quantity`) : null;
                return (
                  <tr key={i.id}>
                    <td className="py-2 pr-2">
                      <span className="font-medium">{i.product_name}</span>
                      <span className="block text-xs text-muted-foreground">
                        {[i.sku && `SKU ${i.sku}`, i.store?.name].filter(Boolean).join(' · ')}
                      </span>
                      {err && <span className="block text-xs text-danger">{err}</span>}
                    </td>
                    <td className="px-2 py-2 text-right tabular-nums">{i.quantity}</td>
                    <td className="px-2 py-2 text-right tabular-nums">{outstanding(i)}</td>
                    <td className="py-2 pl-2 text-right">
                      <input
                        type="number"
                        min="0"
                        max={outstanding(i)}
                        step="1"
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
        <DialogFooter className="gap-2 sm:justify-between">
          <Button type="button" variant="outline" onClick={fillAll} disabled={busy}>
            Everything arrived
          </Button>
          <div className="flex gap-2">
            <Button type="button" variant="outline" onClick={onClose} disabled={busy}>
              Close
            </Button>
            <Button type="button" onClick={save} disabled={busy || units === 0}>
              {busy ? <Loader2 className="size-4 animate-spin" aria-hidden="true" /> : <PackageCheck className="size-4" aria-hidden="true" />}
              Receive {units > 0 ? `${units} units` : ''}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
