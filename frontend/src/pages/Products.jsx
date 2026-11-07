import { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { Eye, Pencil, Plus, Trash2, Upload } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { usePermission } from '@/context/PermissionContext';
import { useConfirm } from '@/components/feedback/ConfirmDialog';
import { api } from '@/services/api';
import { toast } from '../../shared/toast/store';
import usePagedList from '@/hooks/usePagedList';
import DataTable from '@/components/kit/DataTable';
import FilterBar from '@/components/kit/FilterBar';
import PageHeader from '@/components/kit/PageHeader';
import RowActions from '@/components/kit/RowActions';
import StatusBadge from '@/components/kit/StatusBadge';
import { EmptyState } from '@/components/kit/States';
import { STATUS_OPTIONS } from '@/components/kit/Form';
import { ProductImage } from '@/components/shared/product-image';
import ImportProduct from '@/components/ImportProduct';
import EditProductSheet from '@/components/products/EditProductSheet';
import { formatCurrency, formatNumber } from '@/lib/format';

const variantsOf = (p) => p?.ProductVariants || [];
const stockOf = (p) => variantsOf(p).reduce((n, v) => n + (v.Inventories || []).reduce((m, i) => m + (Number(i.qty) || 0), 0), 0);

function priceText(p) {
  const prices = variantsOf(p).map((v) => Number(v.price));
  if (!prices.length) return '—';
  const lo = Math.min(...prices);
  const hi = Math.max(...prices);
  return lo === hi ? formatCurrency(lo) : `${formatCurrency(lo)} – ${formatCurrency(hi)}`;
}

const lookup = async (path) => {
  try {
    const r = await api.get(`${path}?limit=500`, { quiet: true });
    return (Array.isArray(r) ? r : r?.data || []).map((x) => ({ value: String(x.id), label: x.name }));
  } catch {
    return []; // no permission for this list: the filter is left out
  }
};

// Products (/products): server pages, search by name or SKU, filters for
// category, brand and status. Open a product for its options and stock.
export default function Products() {
  const { hasPermission } = usePermission();
  const confirm = useConfirm();
  const navigate = useNavigate();
  const list = usePagedList('/api/products', { filters: ['categoryId', 'brandId', 'status'] });
  const [cats, setCats] = useState([]);
  const [brands, setBrands] = useState([]);
  const [editing, setEditing] = useState(null);
  const [importing, setImporting] = useState(false);

  useEffect(() => {
    lookup('/api/categories').then(setCats);
    lookup('/api/brands').then(setBrands);
  }, []);

  const remove = async (row) => {
    const ok = await confirm({
      title: `Delete "${row.name}"?`,
      body: 'It disappears from the checkout, stock and the online shop. Past orders keep its name.',
      confirmLabel: 'Delete',
      destructive: true,
    });
    if (!ok) return;
    try {
      await api.delete(`/api/products/${row.id}`, { quiet: true });
      toast.success('Product deleted.');
      list.reload();
    } catch (err) {
      toast.error(err.message || 'The product could not be deleted.');
    }
  };

  const columns = [
    {
      id: 'name',
      header: 'Product',
      card: 'title',
      cell: (r) => (
        <Link to={`/products/${r.id}`} className="flex min-w-0 items-center gap-2 hover:underline">
          <ProductImage name={r.name} src={r.image} size={32} />
          <span className="truncate font-medium text-foreground">{r.name}</span>
        </Link>
      ),
    },
    {
      id: 'sku',
      header: 'SKU',
      card: 'subtitle',
      cell: (r) => {
        const vs = variantsOf(r);
        return (
          <span className="text-muted-foreground">
            {vs[0]?.sku || '—'}
            {vs.length > 1 && <span className="ml-1 rounded bg-muted px-1.5 py-0.5 text-[11px] font-medium text-foreground">{vs.length} options</span>}
          </span>
        );
      },
    },
    { id: 'category', header: 'Category', card: 'meta', className: 'hidden md:table-cell', cell: (r) => r.category || '—' },
    { id: 'brand', header: 'Brand', card: 'hidden', className: 'hidden xl:table-cell', cell: (r) => r.brand || '—' },
    { id: 'price', header: 'Price', card: 'meta', align: 'right', cell: (r) => <span className="tabular-nums">{priceText(r)}</span> },
    { id: 'stock', header: 'In stock', card: 'meta', align: 'right', cell: (r) => <span className="tabular-nums">{formatNumber(stockOf(r), 0)}</span> },
    { id: 'status', header: 'Status', card: 'meta', cell: (r) => <StatusBadge value={r.status} /> },
  ];

  const filters = [
    ...(cats.length ? [{ key: 'categoryId', label: 'Category', allLabel: 'All categories', options: cats }] : []),
    ...(brands.length ? [{ key: 'brandId', label: 'Brand', allLabel: 'All brands', options: brands }] : []),
    { key: 'status', label: 'Status', allLabel: 'All statuses', options: STATUS_OPTIONS },
  ];
  const filtered = list.search || Object.values(list.values).some(Boolean);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        description="What you sell. Each product has one or more options (size, colour…) with their own SKU, price and stock."
        actions={
          <div className="flex flex-wrap gap-2">
            {hasPermission('products', 'import') && (
              <Button type="button" variant="outline" className="max-sm:h-11" onClick={() => setImporting(true)}>
                <Upload className="size-4" aria-hidden="true" /> Import CSV
              </Button>
            )}
            {hasPermission('products', 'create') && (
              <Button type="button" className="max-sm:h-11" onClick={() => navigate('/add-product')}>
                <Plus className="size-4" aria-hidden="true" /> Add product
              </Button>
            )}
          </div>
        }
      />
      <FilterBar search={list.search} onSearch={list.setSearch} placeholder="Search name or SKU…" filters={filters} values={list.values} onFilter={list.setFilter} onClear={list.clearFilters} />
      <DataTable
        label="Products"
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
        empty={
          filtered ? (
            <EmptyState title="No products match" action={<Button variant="outline" onClick={list.clearFilters}>Clear filters</Button>}>
              Try another search or filter.
            </EmptyState>
          ) : (
            <EmptyState title="No products yet" action={hasPermission('products', 'create') && <Button onClick={() => navigate('/add-product')}>Add product</Button>}>
              Add them one by one or import a CSV file.
            </EmptyState>
          )
        }
        actions={(row) => (
          <RowActions
            label={row.name}
            actions={[
              { label: 'Open', icon: Eye, onSelect: () => navigate(`/products/${row.id}`) },
              hasPermission('products', 'edit') && { label: 'Edit', icon: Pencil, onSelect: () => setEditing(row) },
              hasPermission('products', 'delete') && { label: 'Delete', icon: Trash2, destructive: true, onSelect: () => remove(row) },
            ]}
          />
        )}
      />
      <EditProductSheet product={editing} onClose={() => setEditing(null)} onSaved={list.reload} />
      {importing && <ImportProduct showForm={importing} setShowForm={setImporting} onImportComplete={list.reload} />}
    </div>
  );
}
