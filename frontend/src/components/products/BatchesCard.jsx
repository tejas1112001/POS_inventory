import { useCallback, useEffect, useState } from 'react';
import { Loader2 } from 'lucide-react';
import { Switch } from '@/components/ui/switch';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import BatchDialog from '@/components/batches/BatchDialog';
import BatchExpiry from '@/components/batches/BatchExpiry';
import { formatDate } from '@/lib/format';
import { productUpdatePayload } from '@/lib/productPayload';
import { api } from '@/services/api';
import { toast } from '../../../shared/toast/store';

// BatchesCard (industry P3 item 1, A25): whether the product tracks
// batches, the switch for batches:manage, and its batches with stock.
// Shown when the batch_expiry module is on.
export default function BatchesCard({ product, canManage, onSaved }) {
  const [rows, setRows] = useState(null);
  const [error, setError] = useState(null);
  const [busy, setBusy] = useState(false);
  const [open, setOpen] = useState(null);
  const tracks = !!product.tracksBatches;
  const s = product.batchStock;

  const load = useCallback(() => {
    if (!tracks) {
      setRows([]);
      return;
    }
    api
      .get(`/api/batches?productId=${product.id}`, { quiet: true })
      .then((r) => setRows(r?.items || []))
      .catch(setError);
  }, [product.id, tracks]);

  useEffect(() => {
    load();
  }, [load]);

  const toggle = async (on) => {
    setBusy(true);
    setError(null);
    try {
      await api.put(`/api/products/${product.id}`, { ...productUpdatePayload(product), tracksBatches: on }, { quiet: true });
      toast.success(on ? 'Batch tracking switched on. Current stock is listed as "No batch".' : 'Batch tracking switched off.');
      onSaved();
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="rounded-lg border bg-card p-4 shadow-xs" aria-labelledby="batches-h">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <h2 id="batches-h" className="text-base font-semibold">
          Batches and expiry
        </h2>
        {canManage && (
          <div className="flex items-center gap-2">
            {busy && <Loader2 className="size-4 animate-spin" aria-hidden="true" />}
            <Switch id="tracks-batches" label="Track batches and expiry" checked={tracks} onCheckedChange={toggle} disabled={busy} />
          </div>
        )}
      </div>
      {error && <InlineAlert>{error.message}</InlineAlert>}
      {!tracks && (
        <p className="text-sm text-muted-foreground">
          This product does not track batches. {canManage ? 'Switch it on to keep stock per batch with its expiry date.' : ''}
        </p>
      )}
      {tracks && s && (
        <dl className="mb-3 grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
          <div>
            <dt className="text-xs text-muted-foreground">Nearest expiry</dt>
            <dd>{s.nearestExpiry ? formatDate(s.nearestExpiry) : '—'}</dd>
          </div>
          <div>
            <dt className="text-xs text-muted-foreground">No batch</dt>
            <dd className="tabular-nums">{s.unbatched}</dd>
          </div>
          <div>
            <dt className="text-xs text-muted-foreground">Expired</dt>
            <dd className={s.expired ? 'font-medium text-destructive tabular-nums' : 'tabular-nums'}>{s.expired}</dd>
          </div>
          <div>
            <dt className="text-xs text-muted-foreground">Quarantined</dt>
            <dd className="tabular-nums">{s.quarantined}</dd>
          </div>
        </dl>
      )}
      {tracks && rows && rows.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="text-left text-xs uppercase text-muted-foreground">
              <tr>
                <th scope="col" className="py-1 font-medium">
                  Batch
                </th>
                <th scope="col" className="py-1 font-medium">
                  Expiry
                </th>
                <th scope="col" className="py-1 text-right font-medium">
                  In stock
                </th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {rows.map((b) => (
                <tr key={b.id}>
                  <td className="py-1">
                    <button type="button" className="text-primary underline-offset-2 hover:underline" onClick={() => setOpen(b.id)}>
                      {b.unknown ? 'No batch' : b.batchNo}
                    </button>
                  </td>
                  <td className="py-1">
                    <BatchExpiry batch={b} />
                  </td>
                  <td className="py-1 text-right tabular-nums">{b.totalQty}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <BatchDialog batchId={open} canManage={canManage} onClose={() => setOpen(null)} onSaved={load} />
    </section>
  );
}
