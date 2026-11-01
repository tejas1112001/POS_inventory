import { useCallback, useEffect, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { ArrowLeft, History, Pencil, Trash2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { usePermission } from '@/context/PermissionContext';
import { useConfirm } from '@/components/feedback/ConfirmDialog';
import { api } from '@/services/api';
import { toast } from '../../shared/toast/store';
import StatusBadge from '@/components/kit/StatusBadge';
import { ErrorState } from '@/components/kit/States';
import { ProductImage } from '@/components/shared/product-image';
import { variantLabel } from '@/components/pos/stock';
import EditProductSheet from '@/components/products/EditProductSheet';
import { formatCurrency, formatNumber } from '@/lib/format';
import { cn } from '@/lib/utils';

function Field({ label, children }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="text-sm font-medium break-words">{children || '—'}</dd>
    </div>
  );
}

// ProductDetail (/products/:id): the product, its options with price, cost
// and stock per store (low stock marked), and Edit / Delete.
export default function ProductDetail() {
  const { id } = useParams();
  const navigate = useNavigate();
  const confirm = useConfirm();
  const { hasPermission } = usePermission();
  const [product, setProduct] = useState(null);
  const [stores, setStores] = useState([]);
  const [error, setError] = useState('');
  const [editing, setEditing] = useState(false);

  const load = useCallback(() => {
    setError('');
    api
      .get(`/api/products/${id}`, { quiet: true })
      .then(setProduct)
      .catch((err) => setError(err.status === 404 ? 'This product does not exist or was deleted.' : err.message));
  }, [id]);

  useEffect(load, [load]);
  useEffect(() => {
    if (!hasPermission('stores', 'view')) return;
    api
      .get('/api/stores?includeInactive=true&limit=100', { quiet: true })
      .then((s) => setStores(Array.isArray(s) ? s : s?.data || []))
      .catch(() => {});
  }, [hasPermission]);

  const remove = async () => {
    const ok = await confirm({
      title: `Delete "${product.name}"?`,
      body: 'It disappears from the checkout, stock and the online shop. Past orders keep its name.',
      confirmLabel: 'Delete',
      destructive: true,
    });
    if (!ok) return;
    try {
      await api.delete(`/api/products/${product.id}`, { quiet: true });
      toast.success('Product deleted.');
      navigate('/products');
    } catch (err) {
      toast.error(err.message || 'The product could not be deleted.');
    }
  };

  if (error) {
    return (
      <div className="space-y-3">
        <Link to="/products" className="inline-flex items-center gap-1 text-sm text-primary hover:underline">
          <ArrowLeft className="size-4" aria-hidden="true" /> Products
        </Link>
        <ErrorState message={error} onRetry={load} />
      </div>
    );
  }
  if (!product) return <div className="h-64 animate-pulse rounded-lg bg-muted" aria-busy="true" aria-label="Loading the product" />;

  const variants = product.ProductVariants || [];
  // Stores that hold a stock row for this product, in store order.
  const storeIds = [...new Set(variants.flatMap((v) => (v.Inventories || []).map((i) => i.store_id)))];
  const storeName = (sid) => stores.find((s) => s.id === sid)?.name || `Store ${sid}`;
  const inv = (v, sid) => (v.Inventories || []).find((i) => i.store_id === sid);
  const total = (v) => (v.Inventories || []).reduce((n, i) => n + (Number(i.qty) || 0), 0);
  const many = variants.length > 1;

  return (
    <div className="space-y-4">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
        <div className="flex min-w-0 items-start gap-3">
          <ProductImage name={product.name} src={product.image} size={64} rounded="rounded-lg" />
          <div className="min-w-0">
            <Link to="/products" className="inline-flex items-center gap-1 text-sm text-primary hover:underline">
              <ArrowLeft className="size-4" aria-hidden="true" /> Products
            </Link>
            <h2 className="mt-1 flex flex-wrap items-center gap-2 text-xl font-semibold">
              {product.name} <StatusBadge value={product.status} />
            </h2>
            <p className="text-sm text-muted-foreground">
              {[product.category, product.brand, product.unit].filter(Boolean).join(' · ') || 'No category'}
            </p>
          </div>
        </div>
        <div className="flex flex-wrap gap-2">
          {hasPermission('inventory', 'view') && (
            <Button type="button" variant="outline" className="max-sm:h-11" onClick={() => navigate(`/inventory?q=${encodeURIComponent(variants[0]?.sku || product.name)}`)}>
              <History className="size-4" aria-hidden="true" /> Stock
            </Button>
          )}
          {hasPermission('products', 'edit') && (
            <Button type="button" variant="outline" className="max-sm:h-11" onClick={() => setEditing(true)}>
              <Pencil className="size-4" aria-hidden="true" /> Edit
            </Button>
          )}
          {hasPermission('products', 'delete') && (
            <Button type="button" variant="outline" className="text-danger hover:text-danger max-sm:h-11" onClick={remove}>
              <Trash2 className="size-4" aria-hidden="true" /> Delete
            </Button>
          )}
        </div>
      </div>

      <section className="overflow-hidden rounded-lg border bg-card shadow-xs" aria-labelledby="opts-h">
        <h2 id="opts-h" className="border-b px-4 py-3 text-base font-semibold">
          {many ? `${variants.length} options` : 'Price and stock'}
        </h2>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="bg-muted text-left text-xs uppercase tracking-wide text-slate-600 dark:text-slate-300">
              <tr>
                {many && <th scope="col" className="px-4 py-2 font-medium">Option</th>}
                <th scope="col" className="px-4 py-2 font-medium">SKU</th>
                <th scope="col" className="px-3 py-2 text-right font-medium">Price</th>
                {hasPermission('products', 'edit') && <th scope="col" className="hidden px-3 py-2 text-right font-medium md:table-cell">Cost</th>}
                {storeIds.map((sid) => (
                  <th key={sid} scope="col" className="px-3 py-2 text-right font-medium">
                    {storeName(sid)}
                  </th>
                ))}
                {storeIds.length > 1 && <th scope="col" className="px-4 py-2 text-right font-medium">Total</th>}
              </tr>
            </thead>
            <tbody className="divide-y">
              {variants.map((v) => (
                <tr key={v.id}>
                  {many && <td className="px-4 py-2 font-medium">{variantLabel(v)}</td>}
                  <td className="px-4 py-2 text-muted-foreground">{v.sku}</td>
                  <td className="px-3 py-2 text-right tabular-nums">{formatCurrency(v.price)}</td>
                  {hasPermission('products', 'edit') && <td className="hidden px-3 py-2 text-right tabular-nums md:table-cell">{formatCurrency(v.cost)}</td>}
                  {storeIds.map((sid) => {
                    const i = inv(v, sid);
                    const qty = Number(i?.qty) || 0;
                    const low = i && qty > 0 && qty <= Number(i.quantityAlert);
                    return (
                      <td key={sid} className={cn('px-3 py-2 text-right tabular-nums', qty <= 0 && 'text-danger-fg', low && 'text-warning-fg')}>
                        {i ? formatNumber(qty, 0) : '—'}
                        {low && <span className="sr-only"> (low)</span>}
                      </td>
                    );
                  })}
                  {storeIds.length > 1 && <td className="px-4 py-2 text-right font-medium tabular-nums">{formatNumber(total(v), 0)}</td>}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {!storeIds.length && <p className="px-4 py-3 text-sm text-muted-foreground">No stock recorded in any store yet.</p>}
      </section>

      <section className="rounded-lg border bg-card p-4 shadow-xs" aria-labelledby="details-h">
        <h2 id="details-h" className="mb-3 text-base font-semibold">
          Details
        </h2>
        <dl className="grid grid-cols-2 gap-3 md:grid-cols-4">
          <Field label="Tax">{product.tax != null ? `${Number(product.tax)}% ${product.taxType ? `(${product.taxType.toLowerCase()})` : ''}` : 'Shop default'}</Field>
          <Field label="HSN code">{product.hsnCode}</Field>
          <Field label="Sold at">{product.sellingType}</Field>
          <Field label="Online shop">{product.isPublished ? 'Published' : 'Not published'}</Field>
          <Field label="Added by">{product.createdBy}</Field>
          {product.description && (
            <div className="col-span-2 md:col-span-4">
              <Field label="Description">{product.description}</Field>
            </div>
          )}
        </dl>
      </section>

      <EditProductSheet product={editing ? product : null} onClose={() => setEditing(false)} onSaved={load} />
    </div>
  );
}
