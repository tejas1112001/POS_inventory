import { useEffect, useState } from 'react';
import { Loader2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { SelectField, TextField } from '@/components/settings/FormKit';
import { api } from '@/services/api';

// CountBatchDialog adds a batch line to a stock count (industry P3 item 6,
// A32): an existing batch of the product, or a batch found on the shelf
// (number and expiry; the batch is created). A product counted by batch
// has every batch set to its count on completion; batches with no line go
// to 0.
export default function CountBatchDialog({ line, storeId, onAdd, onClose }) {
  const [rows, setRows] = useState(null);
  const [batchId, setBatchId] = useState('new');
  const [batchNo, setBatchNo] = useState('');
  const [expiry, setExpiry] = useState('');
  const [qty, setQty] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);

  useEffect(() => {
    api
      .get(`/api/batches?variantId=${line.variantId}&storeId=${storeId}&includeEmpty=true`, { quiet: true })
      .then((r) => {
        const items = (r?.items || []).filter((b) => !b.unknown);
        setRows(items);
        if (items.length) setBatchId(String(items[0].id));
      })
      .catch(() => setRows([]));
  }, [line.variantId, storeId]);

  const submit = async (e) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    const l = { variantId: line.variantId, qty: Number(qty) };
    if (batchId === 'new') Object.assign(l, { batchNo, expiry });
    else l.batchId = Number(batchId);
    try {
      await onAdd(l);
      onClose();
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  };

  const f = Object.fromEntries(Object.entries(error?.fields || {}).map(([k, v]) => [k.replace(/^lines\[0\]\.?/, ''), v]));
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Count a batch: {line.productName}</DialogTitle>
          <DialogDescription>
            Once a product has a batch line it is counted by batch: on completion each of its batches here becomes the counted number, and batches with no line
            go to 0.
          </DialogDescription>
        </DialogHeader>
        {error && !error.fields && <InlineAlert>{error.message}</InlineAlert>}
        {!rows && <Loader2 className="mx-auto size-5 animate-spin" aria-label="Loading" />}
        {rows && (
          <form onSubmit={submit} className="grid gap-3">
            <SelectField
              id="cb-batch"
              label="Batch"
              value={batchId}
              onChange={setBatchId}
              error={f.batchId}
              options={[
                ...rows.map((b) => ({
                  value: String(b.id),
                  label: `${b.batchNo}${b.expiry ? ` · Exp ${b.expiry}` : ''}${b.expired ? ' (expired)' : ''}`,
                })),
                { value: 'new', label: 'Found on the shelf: new batch…' },
              ]}
            />
            {batchId === 'new' && (
              <>
                <TextField id="cb-no" label="Batch number" value={batchNo} onChange={setBatchNo} error={f.batchNo} maxLength={40} required />
                <TextField id="cb-exp" label="Expiry" hint="YYYY-MM-DD or MM/YYYY" value={expiry} onChange={setExpiry} error={f.expiry} required />
              </>
            )}
            <TextField id="cb-qty" label="Counted" inputMode="numeric" value={qty} onChange={setQty} error={f.qty || f['']} required />
            <DialogFooter>
              <Button type="button" variant="outline" className="max-sm:h-11" onClick={onClose} disabled={busy}>
                Cancel
              </Button>
              <Button type="submit" className="max-sm:h-11" disabled={busy || qty === ''}>
                {busy && <Loader2 className="size-4 animate-spin" aria-hidden="true" />} Add
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
