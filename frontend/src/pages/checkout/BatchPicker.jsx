import { useEffect, useState } from 'react';
import { Loader2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import BatchExpiry from '@/components/batches/BatchExpiry';
import { api } from '@/services/api';
import { addQty, fmtQty, qtyStep } from '@/lib/qty';

// BatchPicker (industry P3 item 3, Q12): the cashier picks which batches a
// line is sold from. Any cashier may; the sale line records the choice.
// Expired and recalled batches are not offered. Units not picked are filled
// earliest expiry first.
export default function BatchPicker({ line, storeId, onClose, onPick }) {
  const [rows, setRows] = useState(null);
  const [error, setError] = useState(null);
  const [qty, setQty] = useState(() => Object.fromEntries((line.batches || []).map((b) => [b.batchId, String(b.quantity)])));

  useEffect(() => {
    api
      .get(`/api/batches?variantId=${line.variantId}&storeId=${storeId}&status=available`, { quiet: true })
      .then((r) => setRows(r?.items || []))
      .catch(setError);
  }, [line.variantId, storeId]);

  const total = addQty(...Object.values(qty).map((v) => Number(v) || 0));
  const stockHere = (b) => b.stock.find((s) => String(s.storeId) === String(storeId))?.qty ?? 0;

  const save = () => {
    const picks = Object.entries(qty)
      .filter(([, v]) => Number(v) > 0)
      .map(([id, v]) => ({ batchId: Number(id), quantity: Number(v) }));
    onPick(picks);
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Choose batches: {line.name}</DialogTitle>
          <DialogDescription>
            {fmtQty(line.quantity)} in the cart. Units you do not assign here are taken earliest expiry first. Expired and recalled batches are not shown.
          </DialogDescription>
        </DialogHeader>
        {error && <InlineAlert>{error.message}</InlineAlert>}
        {!rows && !error && <Loader2 className="mx-auto size-5 animate-spin" aria-label="Loading" />}
        {rows && rows.length === 0 && <p className="text-sm text-muted-foreground">No sellable batches at this store.</p>}
        {rows && rows.length > 0 && (
          <table className="w-full text-sm">
            <thead className="text-left text-xs uppercase text-muted-foreground">
              <tr>
                <th scope="col" className="py-1 font-medium">Batch</th>
                <th scope="col" className="py-1 font-medium">Expiry</th>
                <th scope="col" className="py-1 text-right font-medium">Here</th>
                <th scope="col" className="py-1 text-right font-medium">Sell</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {rows.map((b) => (
                <tr key={b.id}>
                  <td className="py-1">{b.unknown ? 'No batch' : b.batchNo}</td>
                  <td className="py-1">
                    <BatchExpiry batch={b} />
                  </td>
                  <td className="py-1 text-right tabular-nums">{fmtQty(stockHere(b))}</td>
                  <td className="py-1 text-right">
                    <input
                      type="number"
                      min="0"
                      max={stockHere(b)}
                      step={qtyStep(line.allowFraction)}
                      inputMode={line.allowFraction ? 'decimal' : 'numeric'}
                      aria-label={`Units from batch ${b.batchNo}`}
                      className="h-11 w-20 rounded-md border border-input bg-background px-2 text-right tabular-nums sm:h-8"
                      value={qty[b.id] ?? ''}
                      onChange={(e) => setQty((cur) => ({ ...cur, [b.id]: e.target.value }))}
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {total > line.quantity && <InlineAlert>The batches add up to {fmtQty(total)}; the line has {fmtQty(line.quantity)}.</InlineAlert>}
        <DialogFooter className="gap-2">
          <Button type="button" variant="outline" onClick={() => onPick([])}>
            Earliest expiry first
          </Button>
          <Button type="button" onClick={save} disabled={total > line.quantity}>
            Use these batches
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
