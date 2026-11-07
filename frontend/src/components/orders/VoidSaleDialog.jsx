import { useEffect, useState } from 'react';
import { AlertTriangle, Loader2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { api } from '@/services/api';
import { formatCurrency } from '@/lib/format';
import { toast } from '../../../shared/toast/store';

// VoidSaleDialog: void an order with a reason (DELETE /api/sales/{id}?reason=).
// The stock goes back; the receipt is marked VOID. Cannot be undone.
export default function VoidSaleDialog({ sale, onClose, onVoided }) {
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (sale) {
      setReason('');
      setError('');
    }
  }, [sale]);

  const submit = async (e) => {
    e.preventDefault();
    const r = reason.trim();
    if (r.length < 3) {
      setError('Write the reason (at least 3 characters). It is kept with the order and shown on the receipt.');
      return;
    }
    setBusy(true);
    setError('');
    try {
      await api.delete(`/api/sales/${sale.id}?reason=${encodeURIComponent(r)}`, { quiet: true });
      toast.success(`Order ${sale.reference} voided. Its stock is back.`);
      onVoided?.();
      onClose();
    } catch (err) {
      setError(err.message || 'The order could not be voided.');
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={!!sale} onOpenChange={(o) => !o && !busy && onClose()}>
      {sale && (
        <DialogContent sheet className="sm:max-w-md">
          <form onSubmit={submit} noValidate className="contents">
            <DialogHeader>
              <DialogTitle className="flex items-center gap-2">
                <AlertTriangle className="size-5 text-danger" aria-hidden="true" /> Void order {sale.reference}?
              </DialogTitle>
              <DialogDescription>
                {formatCurrency(sale.total)} · the items go back into stock and the receipt is marked VOID. This cannot be undone.
              </DialogDescription>
            </DialogHeader>
            {error && <InlineAlert>{error}</InlineAlert>}
            <div className="grid gap-1.5">
              <label htmlFor="void-reason" className="text-sm font-medium">
                Reason <span className="text-danger">*</span>
              </label>
              <textarea
                id="void-reason"
                rows={3}
                maxLength={500}
                autoFocus
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                className="w-full rounded-md border border-input bg-card px-3 py-2 text-sm"
                placeholder="For example: entered twice, customer cancelled"
              />
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" className="h-11" onClick={onClose} disabled={busy}>
                Keep order
              </Button>
              <Button type="submit" variant="destructive" className="h-11" disabled={busy}>
                {busy && <Loader2 className="size-4 animate-spin" aria-hidden="true" />} Void order
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      )}
    </Dialog>
  );
}
