import { Fragment, useCallback, useEffect, useState } from 'react';
import { Loader2, Trash2 } from 'lucide-react';
import { SectionCard } from '@/components/shared/section-card';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { SelectField } from '@/components/settings/FormKit';
import { formatDateTime } from '@/lib/format';
import { api } from '@/services/api';
import { WRITE_OFF_REASONS } from '@/lib/writeOffs';
import { fmtQty } from '@/lib/qty';

const reasonLabel = (v) => WRITE_OFF_REASONS.find((r) => r.value === v)?.label || v;

// WriteOffs lists write-offs, newest first (industry P3 item 5, A30). They
// are made from a batch (Batches & expiry → a batch → Write off).
export default function WriteOffs() {
  const [stores, setStores] = useState([]);
  const [storeId, setStoreId] = useState('');
  const [rows, setRows] = useState(null);
  const [total, setTotal] = useState(0);
  const [error, setError] = useState(null);
  const [open, setOpen] = useState(null);

  useEffect(() => {
    api
      .get('/api/stores', { quiet: true })
      .then((s) => setStores(s || []))
      .catch(() => {});
  }, []);

  const load = useCallback(() => {
    const q = new URLSearchParams({ limit: '100' });
    if (storeId) q.set('storeId', storeId);
    api
      .get(`/api/write-offs?${q}`, { quiet: true })
      .then((r) => {
        setRows(r?.items || []);
        setTotal(r?.total || 0);
        setError(null);
      })
      .catch(setError);
  }, [storeId]);
  useEffect(() => {
    load();
  }, [load]);

  return (
    <div className="space-y-3">
      <SectionCard
        icon={Trash2}
        title="Write-offs"
        description="Units of batches that left the shop for good: expired, damaged, recalled or returned to the supplier. To write off, open a batch in Batches & expiry."
      >
        <div className="mb-3 max-w-xs">
          <SelectField id="wo-store" label="Store" value={storeId} onChange={setStoreId}
            options={[{ value: '', label: 'All stores' }, ...stores.map((s) => ({ value: String(s.id), label: s.name }))]} />
        </div>
        {error && <InlineAlert>{error.message}</InlineAlert>}
        {!rows && !error && <Loader2 className="mx-auto size-5 animate-spin" aria-label="Loading" />}
        {rows && rows.length === 0 && <p className="text-sm text-muted-foreground">No write-offs yet.</p>}
        {rows && rows.length > 0 && (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="text-left text-xs uppercase text-muted-foreground">
                <tr>
                  <th scope="col" className="py-2 font-medium">Number</th>
                  <th scope="col" className="py-2 font-medium">Date</th>
                  <th scope="col" className="py-2 font-medium">Store</th>
                  <th scope="col" className="py-2 font-medium">Reason</th>
                  <th scope="col" className="py-2 text-right font-medium">Units</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {rows.map((w) => (
                  <Fragment key={w.id}>
                    <tr className="cursor-pointer hover:bg-muted/50" onClick={() => setOpen(open === w.id ? null : w.id)}>
                      <td className="py-2">
                        <button type="button" className="font-medium text-primary underline-offset-2 hover:underline" aria-expanded={open === w.id}>
                          {w.reference}
                        </button>
                      </td>
                      <td className="py-2">{formatDateTime(w.createdAt)}</td>
                      <td className="py-2">{w.storeName}</td>
                      <td className="py-2">{reasonLabel(w.reason)}</td>
                      <td className="py-2 text-right tabular-nums">{w.units}</td>
                    </tr>
                    {open === w.id && (
                      <tr>
                        <td colSpan={5} className="bg-muted/30 px-3 py-2 text-xs">
                          {w.note && <p className="mb-1">{w.note}</p>}
                          {w.lines.map((l) => (
                            <p key={`${l.batchId}-${l.from}`}>
                              {l.productName} · {l.unknown ? 'No batch' : `Batch ${l.batchNo}`}
                              {l.expiry ? ` · Exp ${l.expiry}` : ''} × {fmtQty(l.quantity)} · from {l.from === 'quarantine' ? 'quarantine' : 'stock'}
                            </p>
                          ))}
                          <p className="mt-1 text-muted-foreground">{w.createdByName ? `By ${w.createdByName}` : ''}</p>
                        </td>
                      </tr>
                    )}
                  </Fragment>
                ))}
              </tbody>
            </table>
            {total > rows.length && <p className="mt-2 text-xs text-muted-foreground">Showing the latest {rows.length} of {total}.</p>}
          </div>
        )}
      </SectionCard>
    </div>
  );
}
