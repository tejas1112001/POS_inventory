import { useCallback, useEffect, useState } from 'react';
import { Loader2, PauseCircle, Trash2, Undo2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { api } from '@/services/api';
import { formatCurrency, formatDateTime } from '@/lib/format';

// HoldDialog parks the current cart with an optional label (S10 item 7).
// Nothing is reserved; the cart is emptied and its products released.
export function HoldDialog({ open, onClose, onHold }) {
  const [label, setLabel] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  useEffect(() => {
    if (open) {
      setLabel('');
      setError('');
    }
  }, [open]);
  const submit = async (e) => {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      await onHold(label.trim());
    } catch (err) {
      setError(err.message || 'The bill could not be held.');
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open={open} onOpenChange={(o) => !o && !busy && onClose()}>
      <DialogContent className="sm:max-w-sm">
        <form onSubmit={submit} noValidate className="contents">
          <DialogHeader>
            <DialogTitle>Hold this bill</DialogTitle>
            <DialogDescription>Any cashier of this store can recall it within 7 days. Stock is not reserved; prices are checked again on recall.</DialogDescription>
          </DialogHeader>
          {error && <InlineAlert>{error}</InlineAlert>}
          <label className="block space-y-1 text-sm font-medium">
            Label <span className="font-normal text-muted-foreground">(optional, e.g. a name or table)</span>
            <Input autoFocus value={label} maxLength={60} onChange={(e) => setLabel(e.target.value)} />
          </label>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose} disabled={busy}>
              Cancel
            </Button>
            <Button type="submit" disabled={busy}>
              {busy ? <Loader2 className="size-4 animate-spin" aria-hidden="true" /> : <PauseCircle className="size-4" aria-hidden="true" />} Hold bill
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// RecallDialog lists the store's held bills, newest first, with who held
// them; Recall loads one into the cart (once only), Discard removes it.
export function RecallDialog({ open, storeId, cartBusy, onClose, onRecall }) {
  const [rows, setRows] = useState(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(null);

  const load = useCallback(() => {
    setRows(null);
    setError('');
    api
      .get(`/api/held-carts?storeId=${storeId}`, { quiet: true })
      .then((r) => setRows(r || []))
      .catch((err) => setError(err.message));
  }, [storeId]);
  useEffect(() => {
    if (open && storeId) load();
  }, [open, storeId, load]);

  const act = async (row, discard) => {
    setBusy(row.id);
    setError('');
    try {
      if (discard) {
        await api.delete(`/api/held-carts/${row.id}`, { quiet: true });
        load();
      } else {
        const held = await api.post(`/api/held-carts/${row.id}/recall`, {}, { quiet: true });
        await onRecall(held);
      }
    } catch (err) {
      setError(err.status === 404 ? 'Someone else recalled or discarded that bill.' : err.message);
      load();
    } finally {
      setBusy(null);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Held bills</DialogTitle>
          <DialogDescription>Bills held at this store in the last 7 days.</DialogDescription>
        </DialogHeader>
        {error && <InlineAlert>{error}</InlineAlert>}
        {cartBusy && <InlineAlert kind="info">The cart is not empty. Hold or clear it before recalling another bill.</InlineAlert>}
        {rows === null && !error && <p className="text-sm text-muted-foreground">Loading…</p>}
        {rows?.length === 0 && <p className="text-sm text-muted-foreground">No held bills.</p>}
        {rows?.length > 0 && (
          <ul className="max-h-[60vh] divide-y overflow-y-auto rounded-lg border">
            {rows.map((r) => (
              <li key={r.id} className="flex items-center justify-between gap-2 px-3 py-2 text-sm">
                <div className="min-w-0">
                  <p className="truncate font-medium">
                    {r.label || `${r.items} item${r.items === 1 ? '' : 's'}`}
                    {r.customerName ? <span className="font-normal text-muted-foreground"> · {r.customerName}</span> : null}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    {r.items} item{r.items === 1 ? '' : 's'} · about {formatCurrency(r.estimate)} · held by {r.userName || 'someone'} · {formatDateTime(r.createdAt)}
                  </p>
                </div>
                <div className="flex shrink-0 gap-1">
                  <Button size="sm" disabled={cartBusy || busy != null} onClick={() => act(r, false)}>
                    {busy === r.id ? <Loader2 className="size-4 animate-spin" aria-hidden="true" /> : <Undo2 className="size-4" aria-hidden="true" />} Recall
                  </Button>
                  <Button size="sm" variant="ghost" aria-label={`Discard ${r.label || 'this bill'}`} disabled={busy != null} onClick={() => act(r, true)}>
                    <Trash2 className="size-4" aria-hidden="true" />
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        )}
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose}>
            Close
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
