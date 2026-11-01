import { useEffect, useState } from 'react';
import { Boxes } from 'lucide-react';
import { api } from '@/services/api';
import { usePermission } from '@/context/PermissionContext';
import { toast } from '../../shared/toast/store';
import usePagedList from '@/hooks/usePagedList';
import DataTable from '@/components/kit/DataTable';
import FilterBar from '@/components/kit/FilterBar';
import PageHeader from '@/components/kit/PageHeader';
import RowActions from '@/components/kit/RowActions';
import StatusBadge from '@/components/kit/StatusBadge';
import { EmptyState } from '@/components/kit/States';
import { FormSheet, TextField } from '@/components/kit/Form';
import { ProductImage } from '@/components/shared/product-image';
import { formatCurrency } from '@/lib/format';
import { cn } from '@/lib/utils';

const STOCK = ['In Stock', 'Low Stock', 'Out of Stock'];

const variantText = (attrs) =>
  attrs && typeof attrs === 'object'
    ? Object.entries(attrs)
        .map(([k, v]) => `${k}: ${v}`)
        .join(', ')
    : '';

// Counters: rows per stock status for the chosen store (one small request each).
function useCounts(storeId, tick) {
  const [counts, setCounts] = useState({});
  useEffect(() => {
    let live = true;
    const base = storeId ? `&storeId=${storeId}` : '';
    Promise.all(
      ['', ...STOCK].map((s) =>
        api
          .get(`/api/inventory?page=1&limit=1${base}${s ? `&stockStatus=${encodeURIComponent(s)}` : ''}`, { quiet: true })
          .then((r) => r?.pagination?.total ?? 0)
          .catch(() => null)
      )
    ).then(([all, inStock, low, out]) => live && setCounts({ all, inStock, low, out }));
    return () => {
      live = false;
    };
  }, [storeId, tick]);
  return counts;
}

// Stock: one row per variant per store, paged and filtered on the server.
export default function Inventory() {
  const { hasPermission } = usePermission();
  const list = usePagedList('/api/inventory', { filters: ['storeId', 'stockStatus'] });
  const [stores, setStores] = useState([]);
  const [editing, setEditing] = useState(null);
  const [tick, setTick] = useState(0);
  const counts = useCounts(list.values.storeId, tick);

  useEffect(() => {
    api.get('/api/stores', { quiet: true }).then((s) => setStores(Array.isArray(s) ? s : [])).catch(() => {});
  }, []);

  const save = async (v) => {
    await api.post(
      '/api/inventory',
      { variantId: editing.variantId, storeId: editing.storeId, qty: Number(v.qty), quantityAlert: Number(v.quantityAlert) },
      { quiet: true }
    );
    toast.success(`Stock of ${editing.productName} set to ${Number(v.qty)}.`);
    list.reload();
    setTick((t) => t + 1);
  };

  const columns = [
    {
      id: 'product',
      header: 'Product',
      card: 'title',
      cell: (r) => (
        <span className="flex items-center gap-2">
          <ProductImage name={r.productName} src={r.productImage} size={32} />
          <span className="min-w-0">
            <span className="block truncate">{r.productName}</span>
            <span className="block truncate text-xs text-muted-foreground">
              {[r.sku, variantText(r.attributes)].filter(Boolean).join(' · ')}
            </span>
          </span>
        </span>
      ),
    },
    { id: 'store', header: 'Store', card: 'subtitle', cell: (r) => r.storeName },
    { id: 'qty', header: 'In stock', card: 'meta', align: 'right', cell: (r) => <span className={cn('font-semibold', r.qty <= 0 && 'text-danger')}>{r.qty}</span> },
    { id: 'alert', header: 'Alert at', card: 'meta', align: 'right', className: 'hidden md:table-cell', cell: (r) => r.quantityAlert },
    { id: 'price', header: 'Price', card: 'hidden', align: 'right', className: 'hidden lg:table-cell', cell: (r) => (r.price != null ? formatCurrency(r.price) : '—') },
    { id: 'status', header: 'Status', card: 'meta', cell: (r) => <StatusBadge value={r.status} /> },
  ];

  const tiles = [
    { label: 'All', value: counts.all, filter: '' },
    { label: 'In stock', value: counts.inStock, filter: 'In Stock' },
    { label: 'Low stock', value: counts.low, filter: 'Low Stock' },
    { label: 'Out of stock', value: counts.out, filter: 'Out of Stock' },
  ];

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader description="How many of each product are in each store. Use Adjustments to add or remove with a reason." />
      <div className="mb-3 grid grid-cols-2 gap-2 sm:grid-cols-4">
        {tiles.map((t) => (
          <button
            key={t.label}
            type="button"
            onClick={() => list.setFilter('stockStatus', t.filter)}
            aria-pressed={(list.values.stockStatus || '') === t.filter}
            className={cn(
              'min-h-14 rounded-lg border bg-card px-3 py-2 text-left',
              (list.values.stockStatus || '') === t.filter && 'border-primary ring-1 ring-primary'
            )}
          >
            <span className="block text-xs text-muted-foreground">{t.label}</span>
            <span className="text-lg font-semibold tabular-nums">{t.value ?? '…'}</span>
          </button>
        ))}
      </div>
      <FilterBar
        search={list.search}
        onSearch={list.setSearch}
        placeholder="Search product, SKU or store…"
        filters={[
          { key: 'storeId', label: 'Store', allLabel: 'All stores', options: stores.map((s) => ({ value: String(s.id), label: s.name })) },
          { key: 'stockStatus', label: 'Stock status', allLabel: 'Any stock status', options: STOCK.map((s) => ({ value: s, label: s })) },
        ]}
        values={list.values}
        onFilter={list.setFilter}
        onClear={list.clearFilters}
      />
      <DataTable
        label="Stock"
        columns={columns}
        rows={list.rows}
        loading={list.loading}
        error={list.error}
        onRetry={list.reload}
        total={list.total}
        page={list.page}
        size={list.size}
        pageSizes={list.pageSizes}
        onPage={list.setPage}
        onSize={list.setSize}
        rowKey={(r) => `${r.storeId}-${r.variantId}`}
        empty={<EmptyState title="No stock matches">Try another search, store or status.</EmptyState>}
        actions={(r) => (
          <RowActions
            label={`${r.productName} in ${r.storeName}`}
            actions={[hasPermission('inventory', 'edit') && { label: 'Set stock', icon: Boxes, onSelect: () => setEditing(r) }]}
          />
        )}
      />
      <FormSheet
        open={!!editing}
        onOpenChange={(o) => !o && setEditing(null)}
        title="Set stock"
        description={editing ? `${editing.productName} · ${editing.storeName}. Now ${editing.qty}. The number you enter replaces it.` : ''}
        defaultValues={{ qty: String(editing?.qty ?? 0), quantityAlert: String(editing?.quantityAlert ?? 0) }}
        onSubmit={save}
        submitLabel="Save stock"
      >
        <TextField name="qty" label="Quantity" type="number" inputMode="numeric" min="0" step="1" required rules={{ min: { value: 0, message: 'Must be 0 or more.' } }} />
        <TextField name="quantityAlert" label="Low-stock alert at" type="number" inputMode="numeric" min="0" step="1" hint="Below or at this number the product shows as low stock." />
      </FormSheet>
    </div>
  );
}
