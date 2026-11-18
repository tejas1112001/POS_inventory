import { useCallback, useEffect, useState } from 'react';
import { CalendarClock, Loader2 } from 'lucide-react';
import { SectionCard } from '@/components/shared/section-card';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { CheckField, SelectField } from '@/components/settings/FormKit';
import BatchDialog from '@/components/batches/BatchDialog';
import BatchExpiry from '@/components/batches/BatchExpiry';
import { usePermission } from '@/context/PermissionContext';
import { api } from '@/services/api';

const STATUSES = [
  { value: '', label: 'Any' },
  { value: 'available', label: 'Sellable' },
  { value: 'expired', label: 'Expired' },
  { value: 'recalled', label: 'Recalled' },
  { value: 'unknown', label: 'No batch' },
];
const WITHIN = [
  { value: '', label: 'Any expiry' },
  { value: '30', label: 'Expiring in 30 days' },
  { value: '60', label: 'Expiring in 60 days' },
  { value: '90', label: 'Expiring in 90 days' },
];

// Batches lists batches with stock, earliest expiry first (industry P3
// item 1, A23), with filters for store, status and nearness of expiry.
export default function Batches() {
  const { hasPermission } = usePermission();
  const [stores, setStores] = useState([]);
  const [storeId, setStoreId] = useState('');
  const [status, setStatus] = useState('');
  const [within, setWithin] = useState('');
  const [includeEmpty, setIncludeEmpty] = useState(false);
  const [rows, setRows] = useState([]);
  const [limit, setLimit] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [open, setOpen] = useState(null);

  useEffect(() => {
    api
      .get('/api/stores', { quiet: true })
      .then((s) => setStores(s || []))
      .catch(() => {});
  }, []);

  const load = useCallback(() => {
    setLoading(true);
    const q = new URLSearchParams();
    if (storeId) q.set('storeId', storeId);
    if (status) q.set('status', status);
    if (within) q.set('expiringWithinDays', within);
    if (includeEmpty) q.set('includeEmpty', 'true');
    return api
      .get(`/api/batches?${q}`, { quiet: true })
      .then((r) => {
        setRows(r?.items || []);
        setLimit(r?.limit || 0);
        setError(null);
      })
      .catch(setError)
      .finally(() => setLoading(false));
  }, [storeId, status, within, includeEmpty]);

  useEffect(() => {
    load();
  }, [load]);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end gap-3">
        <div className="w-48">
          <SelectField
            id="batch-store"
            label="Store"
            value={storeId}
            onChange={setStoreId}
            options={[{ value: '', label: 'All stores' }, ...stores.map((s) => ({ value: String(s.id), label: s.name }))]}
          />
        </div>
        <div className="w-40">
          <SelectField id="batch-status" label="Status" value={status} onChange={setStatus} options={STATUSES} />
        </div>
        <div className="w-52">
          <SelectField id="batch-within" label="Expiry" value={within} onChange={setWithin} options={WITHIN} />
        </div>
        <CheckField id="batch-empty" label="Show batches with no stock" checked={includeEmpty} onChange={setIncludeEmpty} />
      </div>

      <p className="text-sm text-muted-foreground">
        Stock of products that track batches, earliest expiry first. Sales take the earliest expiry first; stock with no batch is sold last.
      </p>

      {error && <InlineAlert>{error.message}</InlineAlert>}

      <SectionCard>
        <div className="overflow-x-auto pt-2">
          <table className="w-full text-sm">
            <thead className="text-left text-xs uppercase tracking-wide text-muted-foreground">
              <tr>
                <th scope="col" className="px-3 py-2 font-medium">
                  Product
                </th>
                <th scope="col" className="px-3 py-2 font-medium">
                  Batch
                </th>
                <th scope="col" className="px-3 py-2 font-medium">
                  Expiry
                </th>
                <th scope="col" className="px-3 py-2 text-right font-medium">
                  MRP
                </th>
                <th scope="col" className="px-3 py-2 text-right font-medium">
                  In stock
                </th>
                <th scope="col" className="px-3 py-2 text-right font-medium">
                  Quarantined
                </th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {loading && (
                <tr>
                  <td colSpan={6} className="px-3 py-6 text-center text-muted-foreground">
                    <Loader2 className="mx-auto size-5 animate-spin" aria-label="Loading" />
                  </td>
                </tr>
              )}
              {!loading && rows.length === 0 && (
                <tr>
                  <td colSpan={6} className="px-3 py-8 text-center text-muted-foreground">
                    <CalendarClock className="mx-auto mb-2 size-6" aria-hidden="true" />
                    No batches match. Switch on &quot;Track batches and expiry&quot; on a product to start.
                  </td>
                </tr>
              )}
              {!loading &&
                rows.map((b) => (
                  <tr key={b.id} className="hover:bg-accent/40">
                    <td className="px-3 py-2">
                      <button type="button" className="text-left font-medium text-primary underline-offset-2 hover:underline" onClick={() => setOpen(b.id)}>
                        {b.productName}
                      </button>
                      {b.sku && <div className="text-xs text-muted-foreground">{b.sku}</div>}
                    </td>
                    <td className="px-3 py-2">{b.unknown ? <span className="text-muted-foreground">No batch</span> : b.batchNo}</td>
                    <td className="px-3 py-2">
                      <BatchExpiry batch={b} />
                    </td>
                    <td className="px-3 py-2 text-right tabular-nums">{b.mrp ?? '—'}</td>
                    <td className="px-3 py-2 text-right tabular-nums">{b.totalQty}</td>
                    <td className="px-3 py-2 text-right tabular-nums">{b.totalQuarantined || '—'}</td>
                  </tr>
                ))}
            </tbody>
          </table>
          {!loading && limit > 0 && rows.length >= limit && (
            <p className="px-3 py-2 text-xs text-muted-foreground">Showing the first {limit} batches. Narrow the filters to see the rest.</p>
          )}
        </div>
      </SectionCard>

      <BatchDialog
        batchId={open}
        canManage={hasPermission('batches', 'manage')}
        canWriteOff={hasPermission('batches', 'write_off')}
        canRecall={hasPermission('batches', 'recall')}
        onClose={() => setOpen(null)}
        onSaved={load}
      />
    </div>
  );
}
