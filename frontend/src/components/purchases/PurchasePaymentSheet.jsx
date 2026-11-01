import { useState } from 'react';
import { Loader2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { api } from '@/services/api';
import { formatCurrency } from '@/lib/format';
import { toast } from '../../../shared/toast/store';

const n = (v) => Number(v) || 0;

// PaymentSheet records money paid to the supplier: the new paid total is
// sent (PUT /api/purchases/{id} {paid}); the server sets due and status.
export default function PurchasePaymentSheet({ purchase, onClose, onDone }) {
  const [amount, setAmount] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const due = n(purchase?.due);

  const submit = async (e) => {
    e.preventDefault();
    const a = parseFloat(amount);
    if (!Number.isFinite(a) || a <= 0) return setError('Enter an amount above 0.');
    if (a > due + 1e-9) return setError(`At most ${formatCurrency(due)} is due.`);
    setBusy(true);
    setError('');
    try {
      const out = await api.put(`/api/purchases/${purchase.id}`, { paid: Number((n(purchase.paid) + a).toFixed(4)) }, { quiet: true });
      toast.success(`${formatCurrency(a)} recorded for ${purchase.reference}.`);
      onDone?.(out);
      onClose();
    } catch (err) {
      setError(err.message || 'The payment could not be recorded.');
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={!!purchase} onOpenChange={(o) => !o && !busy && onClose()}>
      {purchase && (
        <DialogContent sheet className="sm:max-w-md">
          <form onSubmit={submit} noValidate className="contents">
            <DialogHeader>
              <DialogTitle>Record payment</DialogTitle>
              <DialogDescription>
                {purchase.reference} · total {formatCurrency(purchase.total)} · paid {formatCurrency(purchase.paid)} · due {formatCurrency(due)}
              </DialogDescription>
            </DialogHeader>
            {error && <InlineAlert>{error}</InlineAlert>}
            <div className="grid gap-1.5">
              <label htmlFor="pur-pay" className="text-sm font-medium">
                Amount paid now
              </label>
              <Input id="pur-pay" autoFocus type="text" inputMode="decimal" placeholder="0.00" value={amount} onChange={(e) => setAmount(e.target.value.replace(/[^0-9.]/g, ''))} className="h-11 text-right tabular-nums" />
              <button type="button" className="justify-self-start text-sm font-medium text-primary hover:underline" onClick={() => setAmount(due.toFixed(2))}>
                Pay the full {formatCurrency(due)}
              </button>
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" className="h-11" onClick={onClose} disabled={busy}>
                Cancel
              </Button>
              <Button type="submit" className="h-11" disabled={busy}>
                {busy && <Loader2 className="size-4 animate-spin" aria-hidden="true" />} Record payment
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      )}
    </Dialog>
  );
}
