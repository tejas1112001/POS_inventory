import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Loader2, Minus, Plus } from 'lucide-react';
import { SectionCard } from '@/components/shared/section-card';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Field, Notice, SelectField, TextArea, TextField } from '@/components/settings/FormKit';
import { fieldCls } from '@/components/settings/formUtils';
import { usePermission } from '@/context/PermissionContext';
import { api } from '@/services/api';
import { formatDateTime } from '@/lib/format';
import { newIdempotencyKey } from '@/lib/checkout';
import { cn } from '@/lib/utils';

const PAGE = 50;
const REASONS = ['Stock count correction', 'Damaged', 'Expired', 'Lost or stolen', 'Returned to supplier', 'Found', 'Other'];

// list reads a paginated list: {data, pagination} or a bare array.
async function fetchPage(path, page) {
  const sep = path.includes('?') ? '&' : '?';
  const res = await api.get(`${path}${sep}page=${page}&limit=${PAGE}`, { quiet: true });
  if (Array.isArray(res)) return { rows: res, total: res.length };
  return { rows: res?.data || [], total: res?.pagination?.total ?? 0 };
}

const variantName = (v) => {
  const attrs = v?.attributes && typeof v.attributes === 'object' ? Object.values(v.attributes).filter(Boolean).join(' / ') : '';
  return attrs || v?.sku || '';
};

// StockAdjustments records stock corrections (POST /api/stock-adjustments:
// one store, one variant, add or remove, with a reason) and shows them,
// and the stock ledger of every change (GET /api/stock-movements).
export default function StockAdjustments() {
  const { hasPermission } = usePermission();
  const canCreate = hasPermission('stock_adjustments', 'create');
  const canHistory = hasPermission('inventory', 'view');
  const [tab, setTab] = useState('adjustments');
  const [stores, setStores] = useState([]);
  const [storeId, setStoreId] = useState('');
  const [open, setOpen] = useState(false);
  const [reload, setReload] = useState(0);

  useEffect(() => {
    api.get('/api/stores', { quiet: true }).then((s) => setStores(s || [])).catch(() => {});
  }, []);

  const storeName = useCallback((id) => stores.find((s) => s.id === id)?.name || '', [stores]);

  return (
    <div className="flex flex-col gap-3 p-3 sm:p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div role="tablist" className="flex gap-1 border-b">
          <Tab active={tab === 'adjustments'} onClick={() => setTab('adjustments')}>
            Adjustments
          </Tab>
          {canHistory && (
            <Tab active={tab === 'history'} onClick={() => setTab('history')}>
              Stock history
            </Tab>
          )}
        </div>
        <div className="flex items-center gap-2">
          <select className={cn(fieldCls, 'w-48')} value={storeId} onChange={(e) => setStoreId(e.target.value)} aria-label="Store">
            <option value="">All stores</option>
            {stores.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </select>
          {canCreate && (
            <Button onClick={() => setOpen(true)}>
              <Plus className="size-4" /> New adjustment
            </Button>
          )}
        </div>
      </div>

      {tab === 'adjustments' ? (
        <AdjustmentList storeId={storeId} storeName={storeName} reload={reload} />
      ) : (
        <HistoryList storeId={storeId} storeName={storeName} reload={reload} />
      )}

      {open && (
        <AdjustmentDialog
          stores={stores}
          defaultStore={storeId || (stores.length === 1 ? String(stores[0].id) : '')}
          onClose={() => setOpen(false)}
          onSaved={() => {
            setOpen(false);
            setReload((n) => n + 1);
          }}
        />
      )}
    </div>
  );
}

function Tab({ active, onClick, children }) {
  return (
    <button
      role="tab"
      type="button"
      aria-selected={active}
      onClick={onClick}
      className={cn('-mb-px border-b-2 px-4 py-2 text-sm font-medium', active ? 'border-primary text-primary' : 'border-transparent text-muted-foreground hover:text-foreground')}
    >
      {children}
    </button>
  );
}

function usePaged(path, deps) {
  const [page, setPage] = useState(1);
  const [state, setState] = useState({ rows: [], total: 0, loading: true, error: '' });
  useEffect(() => setPage(1), deps); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    let active = true;
    setState((s) => ({ ...s, loading: true }));
    fetchPage(path, page)
      .then((r) => active && setState({ ...r, loading: false, error: '' }))
      .catch((err) => active && setState({ rows: [], total: 0, loading: false, error: err.message }));
    return () => {
      active = false;
    };
  }, [path, page, ...deps]); // eslint-disable-line react-hooks/exhaustive-deps
  return { ...state, page, setPage, pages: Math.max(1, Math.ceil(state.total / PAGE)) };
}

function Pager({ page, pages, setPage }) {
  if (pages <= 1) return null;
  return (
    <div className="mt-3 flex items-center justify-end gap-2 text-sm">
      <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>
        Previous
      </Button>
      <span>
        Page {page} of {pages}
      </span>
      <Button variant="outline" size="sm" disabled={page >= pages} onClick={() => setPage(page + 1)}>
        Next
      </Button>
    </div>
  );
}

function Table({ head, children, loading, error, empty }) {
  if (error) return <Notice notice={{ kind: 'error', text: error }} />;
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b text-left text-xs text-muted-foreground">
            {head.map((h, i) => (
              <th key={h || i} className={cn('px-2 py-2 font-medium', i >= head.length - 3 && 'text-right')}>
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y">{children}</tbody>
      </table>
      {loading && <p className="py-6 text-center text-sm text-muted-foreground">Loading...</p>}
      {!loading && empty && <p className="py-6 text-center text-sm text-muted-foreground">Nothing here yet.</p>}
    </div>
  );
}

function AdjustmentList({ storeId, storeName, reload }) {
  const path = storeId ? `/api/stock-adjustments?storeId=${storeId}` : '/api/stock-adjustments';
  const { rows, loading, error, page, pages, setPage } = usePaged(path, [storeId, reload]);
  return (
    <SectionCard>
      <div className="pt-4">
        <Table head={['Date', 'Product', 'Store', 'Reason', 'By', 'Change', 'Stock after', '']} loading={loading} error={error} empty={rows.length === 0}>
          {rows.map((a) => (
            <tr key={a.id}>
              <td className="whitespace-nowrap px-2 py-2">{formatDateTime(a.createdAt)}</td>
              <td className="px-2 py-2">
                <div className="font-medium">{a.Variant?.Product?.name}</div>
                <div className="text-xs text-muted-foreground">{[variantName(a.Variant), a.Variant?.sku].filter(Boolean).filter((x, i, l) => l.indexOf(x) === i).join(' · ')}</div>
              </td>
              <td className="px-2 py-2">{a.Store?.name || storeName(a.store_id)}</td>
              <td className="px-2 py-2">
                {a.reason}
                {a.note && <div className="text-xs text-muted-foreground">{a.note}</div>}
              </td>
              <td className="px-2 py-2">{a.creator?.name}</td>
              <td className={cn('px-2 py-2 text-right font-semibold', a.type === 'Addition' ? 'text-emerald-700' : 'text-red-700')}>
                {a.type === 'Addition' ? '+' : '−'}
                {a.quantity}
              </td>
              <td className="px-2 py-2 text-right">{a.qtyAfter}</td>
              <td />
            </tr>
          ))}
        </Table>
        <Pager page={page} pages={pages} setPage={setPage} />
      </div>
    </SectionCard>
  );
}

// Ledger reasons in plain words; anything else is shown as stored.
const REASON = {
  sale: 'Sale',
  sale_void: 'Sale voided',
  sale_edit: 'Sale edited',
  return: 'Return',
  purchase_receive: 'Purchase received',
  adjustment: 'Adjustment',
  import: 'Import',
  opening: 'Opening stock',
};

function HistoryList({ storeId, storeName, reload }) {
  const path = storeId ? `/api/stock-movements?storeId=${storeId}` : '/api/stock-movements';
  const { rows, loading, error, page, pages, setPage } = usePaged(path, [storeId, reload]);
  return (
    <SectionCard>
      <div className="pt-4">
        <p className="mb-2 text-xs text-muted-foreground">Every change to stock: sales, returns, purchases, adjustments and imports.</p>
        <Table head={['Date', 'Product', 'Store', 'Reason', '', 'Change', 'Stock after', '']} loading={loading} error={error} empty={rows.length === 0}>
          {rows.map((m) => (
            <tr key={m.id}>
              <td className="whitespace-nowrap px-2 py-2">{formatDateTime(m.createdAt)}</td>
              <td className="px-2 py-2">
                <div className="font-medium">{m.productName}</div>
                <div className="text-xs text-muted-foreground">{m.sku}</div>
              </td>
              <td className="px-2 py-2">{storeName(m.storeId)}</td>
              <td className="px-2 py-2">{REASON[String(m.reason).toLowerCase()] || m.reason}</td>
              <td />
              <td className={cn('px-2 py-2 text-right font-semibold', m.delta >= 0 ? 'text-emerald-700' : 'text-red-700')}>
                {m.delta > 0 ? `+${m.delta}` : m.delta < 0 ? `−${-m.delta}` : '0'}
              </td>
              <td className="px-2 py-2 text-right">{m.qtyAfter}</td>
              <td />
            </tr>
          ))}
        </Table>
        <Pager page={page} pages={pages} setPage={setPage} />
      </div>
    </SectionCard>
  );
}

function AdjustmentDialog({ stores, defaultStore, onClose, onSaved }) {
  const [products, setProducts] = useState([]);
  const [form, setForm] = useState({ storeId: defaultStore, productId: '', variantId: '', type: 'Subtraction', quantity: '1', reason: REASONS[0], other: '', note: '' });
  const [fields, setFields] = useState({});
  const [notice, setNotice] = useState(null);
  const [busy, setBusy] = useState(false);
  const key = useRef(newIdempotencyKey());

  useEffect(() => {
    api.get('/api/products', { quiet: true }).then((p) => setProducts(p || [])).catch((err) => setNotice({ kind: 'error', text: err.message }));
  }, []);

  const set = (k) => (v) => {
    key.current = newIdempotencyKey(); // a changed request is a new one
    setForm((f) => ({ ...f, [k]: v }));
  };
  const product = products.find((p) => String(p.id) === String(form.productId));
  const variants = useMemo(() => product?.ProductVariants || [], [product]);
  const variant = variants.find((v) => String(v.id) === String(form.variantId));
  const onHand = variant ? (variant.Inventories || []).find((i) => String(i.store_id) === String(form.storeId))?.qty ?? 0 : null;
  const qty = Number(form.quantity) || 0;
  const after = onHand === null ? null : form.type === 'Addition' ? onHand + qty : onHand - qty;

  const pickProduct = (id) => {
    const vs = products.find((p) => String(p.id) === String(id))?.ProductVariants || [];
    setForm((f) => ({ ...f, productId: id, variantId: vs.length === 1 ? String(vs[0].id) : '' }));
  };

  const save = async (e) => {
    e.preventDefault();
    setNotice(null);
    setFields({});
    const reason = form.reason === 'Other' ? form.other.trim() : form.reason;
    if (!form.storeId || !form.variantId || !(qty >= 1) || !reason) {
      setNotice({ kind: 'error', text: 'Choose a store and a product, a quantity of at least 1 and a reason.' });
      return;
    }
    setBusy(true);
    try {
      await api.post(
        '/api/stock-adjustments',
        { storeId: Number(form.storeId), productId: Number(form.productId), variantId: Number(form.variantId), type: form.type, quantity: qty, reason, note: form.note || undefined },
        { headers: { 'Idempotency-Key': key.current }, quiet: true }
      );
      onSaved();
    } catch (err) {
      setFields(err.fields || {});
      setNotice({ kind: 'error', text: err.code === 'INSUFFICIENT_STOCK' ? 'There is not that much in stock to remove.' : err.message });
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>New stock adjustment</DialogTitle>
        </DialogHeader>
        <form onSubmit={save} className="space-y-4" noValidate>
          <div className="grid gap-4 sm:grid-cols-2">
            <SelectField id="adj-store" label="Store" value={form.storeId} onChange={set('storeId')} error={fields.storeId}
              options={[{ value: '', label: 'Select store' }, ...stores.map((s) => ({ value: String(s.id), label: s.name }))]} />
            <SelectField id="adj-product" label="Product" value={form.productId} onChange={pickProduct} error={fields.productId}
              options={[{ value: '', label: 'Select product' }, ...products.map((p) => ({ value: String(p.id), label: p.name }))]} />
            {variants.length > 1 && (
              <SelectField id="adj-variant" label="Variant" value={form.variantId} onChange={set('variantId')} error={fields.variantId}
                options={[{ value: '', label: 'Select variant' }, ...variants.map((v) => ({ value: String(v.id), label: [variantName(v), v.sku].filter(Boolean).join(' · ') }))]} />
            )}
          </div>

          <Field label="Change">
            <div className="flex gap-2">
              <Button type="button" variant={form.type === 'Subtraction' ? 'default' : 'outline'} onClick={() => set('type')('Subtraction')}>
                <Minus className="size-4" /> Remove
              </Button>
              <Button type="button" variant={form.type === 'Addition' ? 'default' : 'outline'} onClick={() => set('type')('Addition')}>
                <Plus className="size-4" /> Add
              </Button>
              <input type="number" min="1" step="1" className={cn(fieldCls, 'w-28')} value={form.quantity} onChange={(e) => set('quantity')(e.target.value)} aria-label="Quantity" />
            </div>
            {fields.quantity && <p className="text-xs text-destructive">{fields.quantity}</p>}
          </Field>
          {onHand !== null && form.storeId && (
            <p className={cn('text-sm', after < 0 && 'text-destructive')}>
              In stock now: <strong>{onHand}</strong>. After this: <strong>{after}</strong>
              {after < 0 && ' (not possible)'}
            </p>
          )}

          <div className="grid gap-4 sm:grid-cols-2">
            <SelectField id="adj-reason" label="Reason" value={form.reason} onChange={set('reason')} error={fields.reason} options={REASONS.map((r) => ({ value: r, label: r }))} />
            {form.reason === 'Other' && <TextField id="adj-other" label="Other reason" value={form.other} onChange={set('other')} maxLength={200} />}
          </div>
          <TextArea id="adj-note" label="Note (optional)" value={form.note} onChange={set('note')} error={fields.note} rows={2} maxLength={500} />

          <Notice notice={notice} />
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={busy || after < 0}>
              {busy && <Loader2 className="size-4 animate-spin" />} Save adjustment
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
