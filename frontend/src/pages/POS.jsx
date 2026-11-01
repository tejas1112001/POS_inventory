import { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import { Search, Minus, Plus, Trash2, ShoppingCart, UserPlus, Banknote, X, Lock } from 'lucide-react';
import CustomerForm from '../components/CustomerForm';
import { InlineAlert } from '../components/feedback/InlineAlert';
import { useConfirm } from '../components/feedback/ConfirmDialog';
import PayForm from '../components/PayForm';
import Receipt from '../components/Receipt';
import PriceChangedDialog from '../components/PriceChangedDialog';
import { useShopSettings } from '../hooks/useShopSettings';
import { usePos } from '../hooks/usePos';
import { isRetryable, newIdempotencyKey, priceChanges } from '../lib/checkout';
import { api } from '../services/api';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/ui/dialog';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { ProductImage } from '@/components/shared/product-image';
import { formatCurrency } from '@/lib/format';
import { cn } from '@/lib/utils';

const LOCK_MS = 2 * 60 * 1000;
const LOW = 5; // "3 left" from this number down

// Stock of a variant in the chosen store (the POS list only carries that store's row).
const variantStock = (v) => Number(v?.Inventories?.[0]?.qty ?? 0);
const variantsOf = (p) => p?.ProductVariants || [];
const productStock = (p) => variantsOf(p).reduce((n, v) => n + Math.max(0, variantStock(v)), 0);

// variantLabel: "Size: M, Colour: Red", or the SKU when a variant has no attributes.
function variantLabel(v) {
  const attrs = v?.attributes && typeof v.attributes === 'object' ? Object.entries(v.attributes).filter(([, x]) => x !== '' && x != null) : [];
  return attrs.length ? attrs.map(([k, x]) => `${k}: ${x}`).join(', ') : v?.sku || `Option ${v?.id}`;
}

function StockBadge({ qty }) {
  if (qty <= 0) return <span className="rounded bg-danger-bg px-1.5 py-0.5 text-[11px] font-medium text-danger-fg">Out of stock</span>;
  if (qty <= LOW) return <span className="rounded bg-warning-bg px-1.5 py-0.5 text-[11px] font-medium text-warning-fg">{qty} left</span>;
  return null;
}

function CategoryPill({ active, onClick, children }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={cn(
        'min-h-9 shrink-0 rounded-full px-4 text-sm font-medium transition-colors',
        active ? 'bg-primary text-primary-foreground' : 'bg-card text-foreground ring-1 ring-border hover:bg-muted'
      )}
    >
      {children}
    </button>
  );
}

function ProductTile({ product, lockedByOther, onPick }) {
  const variants = variantsOf(product);
  const stock = productStock(product);
  const out = stock <= 0;
  const disabled = lockedByOther || out;
  return (
    <button
      type="button"
      onClick={() => !disabled && onPick(product)}
      disabled={disabled}
      aria-label={`${product.name}, ${formatCurrency(product.price)}${variants.length > 1 ? `, ${variants.length} options` : ''}${out ? ', out of stock' : ''}${lockedByOther ? ', in use at another till' : ''}`}
      className="group flex flex-col overflow-hidden rounded-lg border bg-card text-left shadow-xs transition hover:border-primary/40 hover:shadow-sm focus-visible:outline-2 focus-visible:outline-ring disabled:cursor-not-allowed disabled:opacity-55"
    >
      <div className="relative aspect-[4/3] w-full bg-muted">
        <ProductImage name={product.name} src={product.image} className="!h-full !w-full rounded-none text-lg" size={0} />
        {variants.length > 1 && (
          <span className="absolute top-1.5 left-1.5 rounded bg-white/95 px-1.5 py-0.5 text-[11px] font-medium text-slate-700 shadow-xs">
            {variants.length} options
          </span>
        )}
        {lockedByOther && (
          <span className="absolute inset-0 flex items-center justify-center bg-white/70">
            <span className="inline-flex items-center gap-1 rounded bg-slate-800 px-2 py-1 text-xs font-medium text-white">
              <Lock className="size-3" aria-hidden="true" /> In use
            </span>
          </span>
        )}
      </div>
      <div className="flex flex-1 flex-col gap-1 p-2">
        <p className="line-clamp-2 min-h-[2.5em] text-sm leading-tight font-medium text-foreground">{product.name}</p>
        {product.category && <p className="truncate text-xs text-muted-foreground">{product.category}</p>}
        <div className="mt-auto flex items-center justify-between gap-1 pt-1">
          <span className="font-semibold tabular-nums text-foreground">{formatCurrency(product.price)}</span>
          <StockBadge qty={stock} />
        </div>
      </div>
    </button>
  );
}

export default function POS() {
  const { currentUser } = usePos();
  const confirm = useConfirm();
  const [products, setProducts] = useState([]);
  const [customers, setCustomers] = useState([]);
  const [stores, setStores] = useState([]);
  const [selectedStore, setSelectedStore] = useState('');
  const [selectedCustomer, setSelectedCustomer] = useState(null);
  const [searchTerm, setSearchTerm] = useState('');
  const [activeCategory, setActiveCategory] = useState('all');
  const [lines, setLines] = useState([]); // one line per variant: { key, productId, variantId, name, label, price, unitPrice, quantity, maxQty, tax, taxType, image }
  const [showCustomerForm, setShowCustomerForm] = useState(false);
  const [showPayForm, setShowPayForm] = useState(false);
  const [showReceipt, setShowReceipt] = useState(false);
  const [cartOpen, setCartOpen] = useState(false); // phones: the cart sheet
  const [choosing, setChoosing] = useState(null); // product whose option is being chosen
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [completedSale, setCompletedSale] = useState(null);
  const [changedPrices, setChangedPrices] = useState(null);
  const [discount, setDiscount] = useState(0.0);
  const { settings } = useShopSettings();
  // One key per checkout attempt; a retry after a lost answer reuses it.
  const idemKey = useRef(null);
  const lockedProducts = useRef(new Set());

  // Screen estimate only: the server prices the sale (see processOrder).
  const subtotal = lines.reduce((sum, l) => sum + l.price * l.quantity, 0);
  const tax = lines.reduce((sum, l) => {
    const lineTotal = l.price * l.quantity;
    const match = l.tax ? String(l.tax).match(/(\d+(\.\d+)?)/) : null;
    const rate = match ? parseFloat(match[0]) / 100 : 0;
    if (rate <= 0) return sum;
    return sum + (l.taxType === 'Inclusive' ? lineTotal - lineTotal / (1 + rate) : lineTotal * rate);
  }, 0);
  const total = Math.max(0, subtotal + tax - discount);
  const itemCount = lines.reduce((n, l) => n + l.quantity, 0);

  const fetchProducts = useCallback(async (storeId) => {
    if (!storeId) return;
    try {
      setLoading(true);
      const data = await api.get(`/api/products/pos?storeId=${storeId}`);
      setProducts(Array.isArray(data) ? data : []);
    } catch (err) {
      setError('Failed to fetch products for the selected store');
      console.error(err);
    } finally {
      setLoading(false);
    }
  }, []);

  // Locks are per product: release a product when its last line leaves the cart.
  useEffect(() => {
    const inCart = new Set(lines.map((l) => l.productId));
    for (const id of [...lockedProducts.current]) {
      if (inCart.has(id)) continue;
      lockedProducts.current.delete(id);
      api
        .put(`/api/products/${id}/lock`, { lock: false, storeId: selectedStore }, { quiet: true })
        .then((p) => p && setProducts((prev) => prev.map((x) => (x.id === p.id ? p : x))))
        .catch((err) => console.error(`Failed to unlock product ${id}`, err));
    }
  }, [lines, selectedStore]);

  // Leaving the page (or changing store) releases every lock.
  useEffect(() => {
    const held = lockedProducts.current;
    return () => {
      for (const id of held) {
        api.put(`/api/products/${id}/lock`, { lock: false, storeId: selectedStore }, { quiet: true }).catch(() => {});
      }
      held.clear();
    };
  }, [selectedStore]);

  useEffect(() => {
    const fetchInitialData = async () => {
      try {
        setLoading(true);
        const fetchedCustomers = await api.get('/api/customers');
        setCustomers(fetchedCustomers);
        if (fetchedCustomers) {
          const walkIn = fetchedCustomers.find((c) => c.name === 'Walk-in Customer');
          setSelectedCustomer(walkIn ? walkIn.id : fetchedCustomers[0]?.id || null);
        }
        const fetchedStores = await api.get('/api/stores');
        setStores(fetchedStores);
        if (fetchedStores && fetchedStores.length > 0) setSelectedStore(String(fetchedStores[0].id));
        else setError('No stores found. Please add a store.');
      } catch (err) {
        setError('Failed to load initial data.');
        console.error(err);
      } finally {
        setLoading(false);
      }
    };
    fetchInitialData();
  }, []);

  useEffect(() => {
    if (selectedStore) fetchProducts(selectedStore);
  }, [selectedStore, fetchProducts]);

  // Changing the store empties the cart: its stock limits belong to the old store.
  const changeStore = async (value) => {
    if (value === selectedStore) return;
    if (lines.length) {
      const ok = await confirm({
        title: 'Change the store?',
        body: 'The cart is emptied: stock and prices are checked again for the new store.',
        confirmLabel: 'Change store',
      });
      if (!ok) return;
      setLines([]);
    }
    setSelectedStore(value);
  };

  // Category pills come from the products on sale here, so cashiers need
  // no categories:view permission (the biller role does not have it).
  const categories = useMemo(() => [...new Set(products.map((p) => p.category).filter(Boolean))].sort((a, b) => a.localeCompare(b)), [products]);

  const filteredProducts = useMemo(() => {
    const q = searchTerm.trim().toLowerCase();
    return products.filter((p) => {
      if (activeCategory !== 'all' && p.category !== activeCategory) return false;
      if (!q) return true;
      return p.name.toLowerCase().includes(q) || variantsOf(p).some((v) => v.sku?.toLowerCase().includes(q));
    });
  }, [products, activeCategory, searchTerm]);

  const lockedByOther = useCallback(
    (p) => p.isLocked && p.locked_by != null && String(p.locked_by) !== String(currentUser?.id) && Date.now() - new Date(p.lockedAt) < LOCK_MS,
    [currentUser]
  );

  const changeQuantity = (key, delta) => {
    setError(null);
    const line = lines.find((l) => l.key === key);
    if (!line) return;
    const quantity = line.quantity + delta;
    if (quantity > line.maxQty) {
      setError(`Only ${line.maxQty} of "${line.name}${line.label ? ` (${line.label})` : ''}" in stock.`);
      return;
    }
    setLines((prev) => prev.map((l) => (l.key === key ? { ...l, quantity } : l)).filter((l) => l.quantity > 0));
  };

  // addVariant puts one unit of a variant in the cart (or one more).
  const addVariant = useCallback(
    async (product, variant) => {
      setError(null);
      const existing = lines.find((l) => l.variantId === variant.id);
      if (existing) {
        changeQuantity(existing.key, 1);
        return;
      }
      try {
        // Lock the product for this till (refreshes our own lock).
        const locked = await api.put(`/api/products/${product.id}/lock`, { lock: true, storeId: selectedStore });
        lockedProducts.current.add(product.id);
        setProducts((prev) => prev.map((p) => (p.id === locked.id ? locked : p)));
        const fresh = variantsOf(locked).find((v) => v.id === variant.id) || variant;
        const available = variantStock(fresh);
        if (available <= 0) {
          setError(`"${product.name}${variantsOf(locked).length > 1 ? ` (${variantLabel(fresh)})` : ''}" is out of stock.`);
          return;
        }
        setLines((prev) => [
          ...prev,
          {
            key: fresh.id,
            productId: product.id,
            variantId: fresh.id,
            name: product.name,
            label: variantsOf(locked).length > 1 ? variantLabel(fresh) : '',
            price: Number(fresh.price),
            unitPrice: fresh.price,
            quantity: 1,
            maxQty: available,
            tax: locked.tax,
            taxType: locked.taxType,
            image: locked.image,
          },
        ]);
      } catch (err) {
        setError(err.message || `Could not add "${product.name}".`);
      }
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps -- changeQuantity reads the same lines
    [lines, selectedStore]
  );

  const pickProduct = (product) => {
    const variants = variantsOf(product);
    if (variants.length > 1) setChoosing(product);
    else if (variants.length === 1) addVariant(product, variants[0]);
    else setError(`"${product.name}" has no options to sell.`);
  };

  function clearAll() {
    setLines([]);
    setError(null);
    setDiscount(0.0);
  }

  const askClear = async () => {
    const ok = await confirm({ title: 'Clear the cart?', body: 'All items are removed from the current sale.', confirmLabel: 'Clear cart', destructive: true });
    if (ok) clearAll();
  };

  // A changed cart is a different request: it needs a new key.
  useEffect(() => {
    idemKey.current = null;
  }, [lines, discount, selectedCustomer, selectedStore]);

  // processOrder sends the sale. The server prices it (C-04): the cart's
  // own subtotal, tax and total are only an estimate and are not sent.
  const processOrder = async (paymentData) => {
    if (!selectedStore || !selectedCustomer) {
      setError('Select a store and a customer first.');
      return;
    }
    if (!idemKey.current) idemKey.current = newIdempotencyKey();
    const { receiptEmail, ...payment } = paymentData;
    const orderData = {
      ...payment,
      orderItems: lines.map((l) => ({ productId: l.productId, variantId: l.variantId, quantity: l.quantity, unitPrice: l.unitPrice })),
      customerId: Number(selectedCustomer),
      storeId: Number(selectedStore),
      discount,
      ...(receiptEmail ? { receiptEmail } : {}),
    };
    try {
      const result = await api.post('/api/sales', orderData, { headers: { 'Idempotency-Key': idemKey.current }, quiet: true });
      idemKey.current = null;
      setCartOpen(false);
      setCompletedSale(result);
      setShowReceipt(true);
    } catch (err) {
      if (!isRetryable(err)) idemKey.current = null;
      const changes = priceChanges(err);
      if (changes) {
        setChangedPrices(changes);
        return;
      }
      setError(
        isRetryable(err)
          ? 'No answer from the server. The sale may not be saved. Press PAY again: it will not be charged twice.'
          : err.message || 'The sale could not be saved.'
      );
    }
  };

  const acceptNewPrices = () => {
    const byVariant = new Map((changedPrices || []).map((c) => [c.variantId, c.unitPrice]));
    setLines((prev) => prev.map((l) => (byVariant.has(l.variantId) ? { ...l, unitPrice: byVariant.get(l.variantId), price: Number(byVariant.get(l.variantId)) } : l)));
    setChangedPrices(null);
  };

  const selectedCustomerObj = customers.find((c) => String(c.id) === String(selectedCustomer));

  if (loading && products.length === 0) {
    return <div className="flex h-full items-center justify-center text-muted-foreground">Loading products...</div>;
  }

  const cart = (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center justify-between border-b px-4 py-3 max-md:pr-14">
        <div className="flex items-center gap-2">
          <ShoppingCart className="size-5 text-primary" aria-hidden="true" />
          <h2 className="text-base font-semibold">Current sale</h2>
          {itemCount > 0 && <span className="rounded-full bg-muted px-2 text-xs font-medium tabular-nums">{itemCount}</span>}
        </div>
        <Button variant="ghost" size="sm" className="text-danger hover:text-danger" disabled={!lines.length} onClick={askClear}>
          <X className="size-4" aria-hidden="true" /> Clear
        </Button>
      </div>
      <div className="border-b bg-muted/40 px-4 py-2 text-xs">
        <span className="text-muted-foreground">Customer: </span>
        <span className="font-medium">{selectedCustomerObj?.name || 'Walk-in Customer'}</span>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto scrollbar-thin">
        {lines.length === 0 ? (
          <div className="flex h-full flex-col items-center justify-center p-6 text-center">
            <div className="mb-3 flex size-14 items-center justify-center rounded-full bg-muted text-muted-foreground">
              <ShoppingCart className="size-6" aria-hidden="true" />
            </div>
            <p className="text-sm font-medium">Cart is empty</p>
            <p className="mt-1 text-xs text-muted-foreground">Tap products to add them to the sale.</p>
          </div>
        ) : (
          <ul className="divide-y" aria-label="Cart">
            {lines.map((l) => (
              <li key={l.key} className="flex gap-3 p-3">
                <ProductImage name={l.name} src={l.image} size={44} />
                <div className="flex min-w-0 flex-1 flex-col">
                  <div className="flex items-start justify-between gap-2">
                    <div className="min-w-0">
                      <p className="truncate text-sm font-medium">{l.name}</p>
                      {l.label && <p className="truncate text-xs text-muted-foreground">{l.label}</p>}
                      <p className="text-xs text-muted-foreground tabular-nums">{formatCurrency(l.price)} each</p>
                    </div>
                    <Button variant="ghost" size="icon" className="size-9 shrink-0 text-muted-foreground hover:text-danger" aria-label={`Remove ${l.name}${l.label ? ` ${l.label}` : ''}`} onClick={() => setLines((prev) => prev.filter((x) => x.key !== l.key))}>
                      <Trash2 className="size-4" aria-hidden="true" />
                    </Button>
                  </div>
                  <div className="mt-1 flex items-center justify-between">
                    <div className="flex items-center gap-1">
                      <Button variant="outline" size="icon" className="size-9" aria-label="One less" onClick={() => changeQuantity(l.key, -1)}>
                        <Minus className="size-3.5" aria-hidden="true" />
                      </Button>
                      <span className="w-8 text-center text-sm font-semibold tabular-nums" aria-label="Quantity">{l.quantity}</span>
                      <Button variant="outline" size="icon" className="size-9" aria-label="One more" disabled={l.quantity >= l.maxQty} onClick={() => changeQuantity(l.key, 1)}>
                        <Plus className="size-3.5" aria-hidden="true" />
                      </Button>
                    </div>
                    <p className="text-sm font-semibold tabular-nums">{formatCurrency(l.price * l.quantity)}</p>
                  </div>
                </div>
              </li>
            ))}
          </ul>
        )}
      </div>
      <div className="space-y-2 border-t bg-card p-4">
        <div className="flex items-center justify-between text-sm">
          <span className="text-muted-foreground">Subtotal</span>
          <span className="font-medium tabular-nums">{formatCurrency(subtotal)}</span>
        </div>
        <div className="flex items-center justify-between text-sm">
          <label htmlFor="pos-discount" className="text-muted-foreground">Discount</label>
          <Input
            id="pos-discount"
            type="text"
            inputMode="decimal"
            className="h-9 w-28 text-right tabular-nums"
            value={discount ? String(discount) : ''}
            placeholder="0.00"
            onChange={(e) => setDiscount(parseFloat(e.target.value.replace(/[^0-9.]/g, '')) || 0)}
          />
        </div>
        <div className="flex items-center justify-between text-sm">
          <span className="text-muted-foreground">{settings?.taxLabel || 'Tax'}</span>
          <span className="font-medium tabular-nums">{formatCurrency(tax)}</span>
        </div>
        <div className="flex items-center justify-between border-t pt-2">
          <span className="text-sm font-semibold">Total</span>
          <span className="text-xl font-bold tabular-nums text-navy">{formatCurrency(total)}</span>
        </div>
        <Button className="h-12 w-full text-base font-bold" disabled={!lines.length} onClick={() => setShowPayForm(true)}>
          <Banknote className="size-5" aria-hidden="true" /> PAY {formatCurrency(total)}
        </Button>
      </div>
    </div>
  );

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden md:flex-row">
      {/* Products */}
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
        <div className="space-y-2 border-b bg-card p-3">
          {error && <InlineAlert onClose={() => setError(null)}>{error}</InlineAlert>}
          <div className="flex flex-wrap items-center gap-2">
            <Select value={selectedStore} onValueChange={changeStore} disabled={stores.length === 0}>
              <SelectTrigger className="h-10 w-full sm:w-[170px]" aria-label="Store">
                <SelectValue placeholder="Select store" />
              </SelectTrigger>
              <SelectContent>
                {stores.map((store) => (
                  <SelectItem key={store.id} value={String(store.id)}>
                    {store.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <div className="flex min-w-0 flex-1 gap-2">
              <Select value={String(selectedCustomer || '')} onValueChange={setSelectedCustomer}>
                <SelectTrigger className="h-10 min-w-0 flex-1" aria-label="Customer">
                  <SelectValue placeholder="Select customer" />
                </SelectTrigger>
                <SelectContent>
                  {customers.map((c) => (
                    <SelectItem key={c.id} value={String(c.id)}>
                      {c.name} {c.phone ? `(${c.phone})` : ''}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button variant="outline" className="h-10 shrink-0" onClick={() => setShowCustomerForm(true)}>
                <UserPlus className="size-4" aria-hidden="true" /> <span className="hidden sm:inline">Add New</span>
                <span className="sr-only sm:hidden">Add customer</span>
              </Button>
            </div>
          </div>
          <div className="relative">
            <Search className="pointer-events-none absolute top-1/2 left-3 size-5 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
            <Input
              type="search"
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              placeholder="Search by name or SKU…"
              aria-label="Search products by name or SKU"
              className="h-11 pl-10 text-base"
              onKeyDown={(e) => {
                if (e.key !== 'Enter') return;
                const q = searchTerm.trim().toLowerCase();
                // Exact SKU first, then the only match.
                for (const p of filteredProducts) {
                  const v = variantsOf(p).find((x) => x.sku?.toLowerCase() === q);
                  if (v && !lockedByOther(p)) {
                    addVariant(p, v);
                    setSearchTerm('');
                    return;
                  }
                }
                const usable = filteredProducts.filter((p) => !lockedByOther(p) && productStock(p) > 0);
                if (usable.length === 1) {
                  pickProduct(usable[0]);
                  setSearchTerm('');
                }
              }}
            />
          </div>
          <div className="no-scrollbar -mx-3 flex gap-1.5 overflow-x-auto px-3 pb-1">
            <CategoryPill active={activeCategory === 'all'} onClick={() => setActiveCategory('all')}>
              All
            </CategoryPill>
            {categories.map((c) => (
              <CategoryPill key={c} active={activeCategory === c} onClick={() => setActiveCategory(c)}>
                {c}
              </CategoryPill>
            ))}
          </div>
        </div>

        <div className={cn('min-h-0 flex-1 overflow-y-auto p-3 scrollbar-thin', lines.length && 'max-md:pb-24')}>
          {filteredProducts.length === 0 ? (
            <div className="flex h-full flex-col items-center justify-center text-center text-muted-foreground">
              <Search className="mb-2 size-8 opacity-40" aria-hidden="true" />
              <p className="text-sm font-medium">No products found</p>
              <p className="mt-1 text-xs">Try another search or category.</p>
            </div>
          ) : (
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-6">
              {filteredProducts.map((p) => (
                <ProductTile key={p.id} product={p} lockedByOther={lockedByOther(p)} onPick={pickProduct} />
              ))}
            </div>
          )}
        </div>

        {/* Phones: a cart bar instead of the side panel. */}
        {lines.length > 0 && (
          <div className="fixed inset-x-0 bottom-0 z-30 border-t bg-card p-3 pb-[max(12px,env(safe-area-inset-bottom))] md:hidden">
            <Button className="h-12 w-full justify-between text-base" onClick={() => setCartOpen(true)}>
              <span>
                {itemCount} {itemCount === 1 ? 'item' : 'items'} · <span className="tabular-nums">{formatCurrency(total)}</span>
              </span>
              <span>Review</span>
            </Button>
          </div>
        )}
      </div>

      {/* Cart panel: tablets and desktops */}
      <aside className="hidden min-h-0 w-[340px] shrink-0 flex-col border-l bg-card md:flex lg:w-96" aria-label="Current sale">
        {cart}
      </aside>

      {/* Cart sheet: phones */}
      <Dialog open={cartOpen} onOpenChange={setCartOpen}>
        <DialogContent sheet showCloseButton className="flex h-[calc(100dvh-2rem)] flex-col gap-0 overflow-hidden p-0 md:hidden">
          <DialogTitle className="sr-only">Current sale</DialogTitle>
          <DialogDescription className="sr-only">Items, totals and payment</DialogDescription>
          {cart}
        </DialogContent>
      </Dialog>

      {/* Choose option (products with several variants) */}
      <Dialog open={!!choosing} onOpenChange={(o) => !o && setChoosing(null)}>
        {choosing && (
          <DialogContent sheet className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>Choose option</DialogTitle>
              <DialogDescription>{choosing.name}</DialogDescription>
            </DialogHeader>
            <ul className="grid gap-2" aria-label="Options">
              {variantsOf(choosing).map((v) => {
                const stock = variantStock(v);
                const inCart = lines.find((l) => l.variantId === v.id)?.quantity || 0;
                const full = stock <= 0 || inCart >= stock;
                return (
                  <li key={v.id}>
                    <button
                      type="button"
                      disabled={full}
                      onClick={() => {
                        addVariant(choosing, v);
                        setChoosing(null);
                      }}
                      className="flex min-h-14 w-full items-center justify-between gap-3 rounded-lg border bg-card px-3 py-2 text-left hover:border-primary hover:bg-accent disabled:cursor-not-allowed disabled:opacity-55"
                    >
                      <span className="min-w-0">
                        <span className="block truncate font-medium">{variantLabel(v)}</span>
                        <span className="block truncate text-xs text-muted-foreground">
                          {v.sku}
                          {inCart ? ` · ${inCart} in cart` : ''}
                        </span>
                      </span>
                      <span className="flex shrink-0 flex-col items-end gap-1">
                        <span className="font-semibold tabular-nums">{formatCurrency(v.price)}</span>
                        {stock <= 0 ? <StockBadge qty={0} /> : <span className="text-xs text-muted-foreground tabular-nums">{stock} in stock</span>}
                      </span>
                    </button>
                  </li>
                );
              })}
            </ul>
          </DialogContent>
        )}
      </Dialog>

      {showCustomerForm && (
        <CustomerForm
          isOpen={showCustomerForm}
          onClose={() => setShowCustomerForm(false)}
          onAddCustomer={(customer) => {
            setCustomers((prev) => [...prev, customer]);
            setSelectedCustomer(customer.id);
          }}
        />
      )}

      {showPayForm && (
        <PayForm
          isOpen={showPayForm}
          onClose={() => setShowPayForm(false)}
          total={total}
          onPaySubmit={processOrder}
          customerEmail={selectedCustomerObj?.email || ''}
        />
      )}

      {showReceipt && (
        <Receipt
          isOpen={showReceipt}
          onClose={() => {
            setShowReceipt(false);
            setCompletedSale(null);
            clearAll();
          }}
          sale={completedSale}
          store={stores.find((st) => String(st.id) === String(selectedStore))}
        />
      )}

      <PriceChangedDialog
        changes={changedPrices}
        nameOf={(c) => {
          const l = lines.find((i) => i.variantId === c.variantId);
          return l ? `${l.name}${l.label ? ` (${l.label})` : ''}` : undefined;
        }}
        onAccept={acceptNewPrices}
        onClose={() => setChangedPrices(null)}
      />
    </div>
  );
}
