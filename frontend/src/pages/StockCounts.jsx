import { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { ClipboardCheck, Loader2, Plus } from 'lucide-react';
import Pagination from '@/components/kit/Pagination';
import { SectionCard } from '@/components/shared/section-card';
import { Button } from '@/components/ui/button';
import CountStatus from '@/components/stock/CountStatus';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { SelectField, TextArea } from '@/components/settings/FormKit';
import { usePermission } from '@/context/PermissionContext';
import { useModules } from '@/hooks/useModules';
import { formatDateTime } from '@/lib/format';
import { api } from '@/services/api';

const PAGE = 25;
const STATUSES = ['', 'Draft', 'Completed', 'Cancelled'];

// StockCounts lists stock counts (industry P1 item 7, A15) and starts a
// new one for a store, or for one category or brand in it.
export default function StockCounts() {
  const { hasPermission } = usePermission();
  const { hasModule } = useModules();
  // With the module off, past counts stay readable; no new ones.
  const moduleOn = hasModule('stock_counts');
  const canCreate = moduleOn && hasPermission('stock_counts', 'create');
  const [stores, setStores] = useState([]);
  const [storeId, setStoreId] = useState('');
  const [status, setStatus] = useState('');
  const [page, setPage] = useState(1);
  const [rows, setRows] = useState([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [open, setOpen] = useState(false);

  useEffect(() => {
    api.get('/api/stores', { quiet: true }).then((s) => setStores(s || [])).catch(() => {});
  }, []);

  useEffect(() => {
    let live = true;
    setLoading(true);
    const q = new URLSearchParams({ page: String(page), limit: String(PAGE) });
    if (storeId) q.set('storeId', storeId);
    if (status) q.set('status', status);
    api
      .get(`/api/stock-counts?${q}`, { quiet: true })
      .then((r) => {
        if (!live) return;
        setRows(r?.data || []);
        setTotal(r?.pagination?.total ?? 0);
        setError(null);
      })
      .catch((err) => live && setError(err))
      .finally(() => live && setLoading(false));
    return () => {
      live = false;
    };
  }, [page, storeId, status]);

  return (
    <div className="space-y-4">
      {!moduleOn && (
        <InlineAlert kind="info">
          Stock counts are switched off for this shop. Past counts are shown read-only; turn the module on in Settings to count again.
        </InlineAlert>
      )}
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex flex-wrap gap-3">
          <div className="w-48">
            <SelectField
              id="count-store"
              label="Store"
              value={storeId}
              onChange={(v) => {
                setStoreId(v);
                setPage(1);
              }}
              options={[{ value: '', label: 'All stores' }, ...stores.map((s) => ({ value: String(s.id), label: s.name }))]}
            />
          </div>
          <div className="w-40">
            <SelectField
              id="count-status"
              label="Status"
              value={status}
              onChange={(v) => {
                setStatus(v);
                setPage(1);
              }}
              options={STATUSES.map((s) => ({ value: s, label: s || 'Any' }))}
            />
          </div>
        </div>
        {canCreate && (
          <Button type="button" className="max-sm:h-11" onClick={() => setOpen(true)}>
            <Plus className="size-4" aria-hidden="true" /> New count
          </Button>
        )}
      </div>

      <p className="text-sm text-muted-foreground">
        Count what is on the shelves by scanning, typing or uploading a sheet. Nothing changes until the count is completed; then each counted item&apos;s
        stock becomes the counted quantity. Items you did not count are left as they are.
      </p>

      {error && <InlineAlert>{error.message}</InlineAlert>}

      <SectionCard>
        <div className="overflow-x-auto pt-2">
          <table className="w-full text-sm">
            <thead className="text-left text-xs uppercase tracking-wide text-muted-foreground">
              <tr>
                <th scope="col" className="px-3 py-2 font-medium">Count</th>
                <th scope="col" className="px-3 py-2 font-medium">Store</th>
                <th scope="col" className="px-3 py-2 font-medium">What</th>
                <th scope="col" className="px-3 py-2 text-right font-medium">Items</th>
                <th scope="col" className="px-3 py-2 font-medium">Status</th>
                <th scope="col" className="px-3 py-2 font-medium">Started</th>
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
                    <ClipboardCheck className="mx-auto mb-2 size-6" aria-hidden="true" />
                    No stock counts yet.
                  </td>
                </tr>
              )}
              {!loading &&
                rows.map((c) => (
                  <tr key={c.id} className="hover:bg-accent/40">
                    <td className="px-3 py-2">
                      <Link to={`/stock-counts/${c.id}`} className="font-medium text-primary underline-offset-2 hover:underline">
                        {c.reference}
                      </Link>
                      {c.note && <div className="max-w-xs truncate text-xs text-muted-foreground">{c.note}</div>}
                    </td>
                    <td className="px-3 py-2">{c.Store?.name}</td>
                    <td className="px-3 py-2">{c.scopeText}</td>
                    <td className="px-3 py-2 text-right tabular-nums">{c.lineCount}</td>
                    <td className="px-3 py-2"><CountStatus status={c.status} /></td>
                    <td className="px-3 py-2 whitespace-nowrap">
                      {formatDateTime(c.createdAt)}
                      {c.createdBy && <div className="text-xs text-muted-foreground">{c.createdBy.name}</div>}
                    </td>
                  </tr>
                ))}
            </tbody>
          </table>
        </div>
        <Pagination label="Stock counts" total={total} page={page} size={PAGE} onPage={setPage} className="mt-3 rounded-lg border" />
      </SectionCard>

      {open && <NewCountDialog stores={stores} defaultStore={storeId} onClose={() => setOpen(false)} />}
    </div>
  );
}

function NewCountDialog({ stores, defaultStore, onClose }) {
  const navigate = useNavigate();
  const [storeId, setStoreId] = useState(defaultStore || (stores[0] ? String(stores[0].id) : ''));
  const [part, setPart] = useState('');
  const [categories, setCategories] = useState([]);
  const [brands, setBrands] = useState([]);
  const [categoryId, setCategoryId] = useState('');
  const [brandId, setBrandId] = useState('');
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);

  useEffect(() => {
    api.get('/api/categories', { quiet: true }).then((r) => setCategories(Array.isArray(r) ? r : r?.data || [])).catch(() => {});
    api.get('/api/brands', { quiet: true }).then((r) => setBrands(Array.isArray(r) ? r : r?.data || [])).catch(() => {});
  }, []);

  const start = async () => {
    setBusy(true);
    setError(null);
    const scope = {};
    if (part === 'category' && categoryId) scope.categoryId = Number(categoryId);
    if (part === 'brand' && brandId) scope.brandId = Number(brandId);
    try {
      const c = await api.post('/api/stock-counts', { storeId: Number(storeId), scope, note }, { quiet: true });
      navigate(`/stock-counts/${c.id}`);
    } catch (err) {
      setError(err);
      setBusy(false);
    }
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent sheet className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>New stock count</DialogTitle>
          <DialogDescription>Count the whole store, or one category or brand.</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <SelectField id="new-count-store" label="Store" value={storeId} onChange={setStoreId} options={stores.map((s) => ({ value: String(s.id), label: s.name }))} />
          <SelectField
            id="new-count-part"
            label="What to count"
            value={part}
            onChange={setPart}
            options={[
              { value: '', label: 'Everything in the store' },
              { value: 'category', label: 'One category' },
              { value: 'brand', label: 'One brand' },
            ]}
          />
          {part === 'category' && (
            <SelectField
              id="new-count-category"
              label="Category"
              value={categoryId}
              onChange={setCategoryId}
              options={[{ value: '', label: 'Choose…' }, ...categories.map((c) => ({ value: String(c.id), label: c.name }))]}
            />
          )}
          {part === 'brand' && (
            <SelectField
              id="new-count-brand"
              label="Brand"
              value={brandId}
              onChange={setBrandId}
              options={[{ value: '', label: 'Choose…' }, ...brands.map((b) => ({ value: String(b.id), label: b.name }))]}
            />
          )}
          <TextArea id="new-count-note" label="Note (optional)" value={note} onChange={setNote} maxLength={1000} rows={2} />
          {error && <InlineAlert>{error.message}</InlineAlert>}
        </div>
        <DialogFooter className="gap-2">
          <Button type="button" variant="outline" className="max-sm:h-11" onClick={onClose}>
            Cancel
          </Button>
          <Button
            type="button"
            className="max-sm:h-11"
            disabled={busy || !storeId || (part === 'category' && !categoryId) || (part === 'brand' && !brandId)}
            onClick={start}
          >
            {busy && <Loader2 className="size-4 animate-spin" aria-hidden="true" />} Start counting
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
