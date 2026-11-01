import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Banknote, HelpCircle, Loader2, Minus, Plus, RefreshCw, Search, ShoppingCart, Star, Trash2, User, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { useConfirm } from '@/components/feedback/ConfirmDialog';
import { ProductImage } from '@/components/shared/product-image';
import { CategoryPill, ProductTile, VariantSheet } from '@/components/pos/catalog';
import { lockedByOther, productStock, variantsOf } from '@/components/pos/stock';
import PriceChangedDialog from '@/components/PriceChangedDialog';
import Receipt from '@/components/Receipt';
import { usePos } from '@/hooks/usePos';
import { useShopSettings } from '@/hooks/useShopSettings';
import { isRetryable, newIdempotencyKey, priceChanges } from '@/lib/checkout';
import { formatCurrency } from '@/lib/format';
import { cn } from '@/lib/utils';
import { api } from '@/services/api';
import useCheckout, { lineName } from './useCheckout';
import PaymentSheet from './PaymentSheet';
import SaleDone from './SaleDone';
import CustomerPicker from './CustomerPicker';
import ShortcutsHelp from './ShortcutsHelp';

const FAV_KEY = 'pos.favourites';

// Favourites are kept per browser (each till keeps its own list).
function useFavourites() {
  const [favs, setFavs] = useState(() => {
    try {
      return new Set(JSON.parse(localStorage.getItem(FAV_KEY) || '[]'));
    } catch {
      return new Set();
    }
  });
  const toggle = useCallback((id) => {
    setFavs((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      try {
        localStorage.setItem(FAV_KEY, JSON.stringify([...next]));
      } catch {
        /* private window: not kept */
      }
      return next;
    });
  }, []);
  return [favs, toggle];
}

const finePointer = () => typeof window !== 'undefined' && window.matchMedia?.('(pointer: fine)').matches;

// Checkout: the new till screen (UI S6), used when the shop turns on
// "Use the new checkout" in Settings. Totals shown before PAY are the
// server's (POST /api/sales/quote); the sale itself is unchanged.
export default function Checkout() {
  const { currentUser } = usePos();
  const { settings } = useShopSettings();
  const confirm = useConfirm();
  const c = useCheckout({ defaultCustomerId: settings?.defaultCustomerId });
  const { lines } = c;
  const [search, setSearch] = useState('');
  const [category, setCategory] = useState('all'); // 'all', 'fav' or a category name
  const [favs, toggleFav] = useFavourites();
  const [choosing, setChoosing] = useState(null);
  const [selected, setSelected] = useState(null);
  const [flash, setFlash] = useState(null);
  const [editingQty, setEditingQty] = useState(null);
  const [payOpen, setPayOpen] = useState(false);
  const [cartOpen, setCartOpen] = useState(false);
  const [pickCustomer, setPickCustomer] = useState(false);
  const [help, setHelp] = useState(false);
  const [done, setDone] = useState(null);
  const [receiptFor, setReceiptFor] = useState(null); // { sale, print }
  const searchRef = useRef(null);
  const discountRef = useRef(null);
  const idemKey = useRef(null);

  // A changed cart is a different sale: it needs a new key.
  useEffect(() => {
    idemKey.current = null;
  }, [c.bodyKey]);

  const focusSearch = useCallback(() => {
    if (finePointer()) searchRef.current?.focus();
  }, []);

  // Keep a line selected (for + / − / Delete): the last one if the selected left.
  useEffect(() => {
    if (!lines.length) setSelected(null);
    else if (!lines.some((l) => l.key === selected)) setSelected(lines[lines.length - 1].key);
  }, [lines, selected]);

  const categories = useMemo(() => [...new Set(c.products.map((p) => p.category).filter(Boolean))].sort((a, b) => a.localeCompare(b)), [c.products]);
  const inCart = useMemo(() => {
    const m = new Map();
    for (const l of lines) m.set(l.productId, (m.get(l.productId) || 0) + l.quantity);
    return m;
  }, [lines]);

  const shown = useMemo(() => {
    const q = search.trim().toLowerCase();
    return c.products.filter((p) => {
      if (category === 'fav' && !favs.has(p.id)) return false;
      if (category !== 'all' && category !== 'fav' && p.category !== category) return false;
      if (!q) return true;
      return p.name.toLowerCase().includes(q) || variantsOf(p).some((v) => v.sku?.toLowerCase().includes(q));
    });
  }, [c.products, category, favs, search]);

  const isLocked = useCallback((p) => lockedByOther(p, currentUser?.id), [currentUser]);

  const added = (key) => {
    if (!key) return;
    setSelected(key);
    setFlash(key);
    setTimeout(() => setFlash((f) => (f === key ? null : f)), 700);
    focusSearch();
  };

  const add = async (product, variant) => added(await c.addVariant(product, variant));

  const pick = (product) => {
    const variants = variantsOf(product);
    if (variants.length > 1) setChoosing(product);
    else if (variants.length === 1) add(product, variants[0]);
    else c.setProblem(`"${product.name}" has no options to sell.`);
  };

  const onSearchEnter = () => {
    const q = search.trim().toLowerCase();
    if (!q) return;
    for (const p of shown) {
      const v = variantsOf(p).find((x) => x.sku?.toLowerCase() === q);
      if (v && !isLocked(p)) {
        add(p, v);
        setSearch('');
        return;
      }
    }
    const usable = shown.filter((p) => !isLocked(p) && productStock(p) > 0);
    if (usable.length === 1) {
      pick(usable[0]);
      setSearch('');
    } else if (!usable.length) c.setProblem(`Nothing matches "${search.trim()}".`);
  };

  const openPay = useCallback(() => {
    if (lines.length && c.quoteReady) setPayOpen(true);
  }, [lines.length, c.quoteReady]);

  const askClear = async () => {
    const ok = await confirm({ title: 'Clear the cart?', body: 'All items are removed from the current sale.', confirmLabel: 'Clear cart', destructive: true });
    if (ok) c.clearCart();
  };

  // pay sends the sale. The body is exactly the one the server just priced.
  const pay = async (payment) => {
    if (!c.quoteReady) throw new Error('The total is being checked. Try again in a moment.');
    if (!idemKey.current) idemKey.current = newIdempotencyKey();
    const { receiptEmail, ...rest } = payment;
    try {
      const sale = await api.post('/api/sales', { ...c.body, ...rest, ...(receiptEmail ? { receiptEmail } : {}) }, { headers: { 'Idempotency-Key': idemKey.current }, quiet: true });
      idemKey.current = null;
      setPayOpen(false);
      setCartOpen(false);
      setDone(sale);
    } catch (err) {
      if (!isRetryable(err)) idemKey.current = null;
      const changes = priceChanges(err);
      if (changes) {
        c.setChangedPrices(changes);
        setPayOpen(false);
        return;
      }
      throw new Error(
        isRetryable(err)
          ? 'No answer from the server. The sale may already be saved. Press "Complete sale" again: it will not be charged twice.'
          : err.message || 'The sale could not be saved.'
      );
    }
  };

  const newSale = useCallback(() => {
    c.clearCart();
    if (c.walkIn) c.setCustomerId(c.walkIn.id);
    setDone(null);
    setReceiptFor(null);
    setSelected(null);
    setTimeout(focusSearch, 0);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- c's functions are stable enough; walkIn is read fresh
  }, [c.walkIn, focusSearch]);

  // Keyboard shortcuts (while no window is open).
  useEffect(() => {
    const onKey = (e) => {
      if (document.querySelector('[role="dialog"], [role="alertdialog"]')) return;
      const inSearch = e.target === searchRef.current;
      const typing = /^(INPUT|TEXTAREA|SELECT)$/.test(e.target.tagName) && !inSearch;
      if (e.key === 'F2') {
        e.preventDefault();
        searchRef.current?.focus();
        return;
      }
      if (e.key === 'F4') {
        e.preventDefault();
        setPickCustomer(true);
        return;
      }
      if (e.key === 'F8') {
        e.preventDefault();
        discountRef.current?.focus();
        discountRef.current?.select();
        return;
      }
      if (e.key === 'F10' || (e.key === 'Enter' && (e.ctrlKey || e.metaKey))) {
        e.preventDefault();
        openPay();
        return;
      }
      if (typing || (inSearch && search)) return;
      if (e.key === '?') {
        e.preventDefault();
        setHelp(true);
        return;
      }
      if (!lines.length) return;
      const i = lines.findIndex((l) => l.key === selected);
      if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault();
        const next = e.key === 'ArrowDown' ? Math.min(lines.length - 1, i + 1) : Math.max(0, i - 1);
        setSelected(lines[next].key);
      } else if ((e.key === '+' || e.key === '=') && selected != null) {
        e.preventDefault();
        c.changeQuantity(selected, 1);
      } else if (e.key === '-' && selected != null) {
        e.preventDefault();
        c.changeQuantity(selected, -1);
      } else if (e.key === 'Delete' && selected != null) {
        e.preventDefault();
        c.removeLine(selected);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [lines, selected, search, openPay, c]);

  const customer = c.customers.find((x) => String(x.id) === String(c.customerId));
  const store = c.stores.find((s) => String(s.id) === String(c.storeId));
  const q = c.quoteReady ? c.quote.data : null;
  const estimate = lines.reduce((n, l) => n + l.price * l.quantity, 0);
  const total = q ? Number(q.total) : Math.max(0, estimate - c.discount);
  const itemCount = lines.reduce((n, l) => n + l.quantity, 0);
  const quoteProblem = c.quote.error && !priceChanges(c.quote.error) ? c.quote.error : null;

  if (c.loading) {
    return (
      <div className="flex h-full items-center justify-center gap-2 text-muted-foreground">
        <Loader2 className="size-5 animate-spin" aria-hidden="true" /> Loading the checkout…
      </div>
    );
  }
  if (c.loadError) {
    return (
      <div className="p-4">
        <InlineAlert>{c.loadError}</InlineAlert>
      </div>
    );
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
      <button type="button" onClick={() => setPickCustomer(true)} className="flex min-h-11 items-center justify-between gap-2 border-b bg-muted/40 px-4 py-2 text-left text-sm hover:bg-muted">
        <span className="flex min-w-0 items-center gap-2">
          <User className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
          <span className="truncate font-medium">{customer?.name || 'Walk-in Customer'}</span>
        </span>
        <span className="shrink-0 text-xs text-muted-foreground">Change · F4</span>
      </button>
      <div className="min-h-0 flex-1 overflow-y-auto scrollbar-thin">
        {lines.length === 0 ? (
          <div className="flex h-full flex-col items-center justify-center p-6 text-center">
            <div className="mb-3 flex size-14 items-center justify-center rounded-full bg-muted text-muted-foreground">
              <ShoppingCart className="size-6" aria-hidden="true" />
            </div>
            <p className="text-sm font-medium">Cart is empty</p>
            <p className="mt-1 text-xs text-muted-foreground">Search or tap products to add them.</p>
          </div>
        ) : (
          <ul className="divide-y" aria-label="Cart">
            {lines.map((l) => {
              const ql = q?.lines?.find((x) => x.variantId === l.variantId);
              return (
                <li
                  key={l.key}
                  onClick={() => setSelected(l.key)}
                  aria-current={selected === l.key ? 'true' : undefined}
                  className={cn('flex gap-3 p-3 transition-colors', selected === l.key && 'bg-accent/60 md:shadow-[inset_3px_0_0_var(--primary)]', flash === l.key && 'bg-accent')}
                >
                  <ProductImage name={l.name} src={l.image} size={44} />
                  <div className="flex min-w-0 flex-1 flex-col">
                    <div className="flex items-start justify-between gap-2">
                      <div className="min-w-0">
                        <p className="truncate text-sm font-medium">{l.name}</p>
                        {l.label && <p className="truncate text-xs text-muted-foreground">{l.label}</p>}
                        <p className="text-xs text-muted-foreground tabular-nums">
                          {formatCurrency(l.price)} each{ql && Number(ql.taxRate) > 0 ? ` · tax ${Number(ql.taxRate)}%` : ''}
                        </p>
                        {l.maxQty <= 0 ? (
                          <p className="mt-0.5 text-xs font-medium text-danger-fg">Not in stock at this store</p>
                        ) : (
                          l.quantity > l.maxQty && <p className="mt-0.5 text-xs font-medium text-warning-fg">Only {l.maxQty} at this store</p>
                        )}
                      </div>
                      <Button variant="ghost" size="icon" className="size-9 shrink-0 text-muted-foreground hover:text-danger" aria-label={`Remove ${lineName(l)}`} onClick={() => c.removeLine(l.key)}>
                        <Trash2 className="size-4" aria-hidden="true" />
                      </Button>
                    </div>
                    <div className="mt-1 flex items-center justify-between">
                      <div className="flex items-center gap-1">
                        <Button variant="outline" size="icon" className="size-9" aria-label={`One less ${lineName(l)}`} onClick={() => c.changeQuantity(l.key, -1)}>
                          <Minus className="size-3.5" aria-hidden="true" />
                        </Button>
                        {editingQty === l.key ? (
                          <Input
                            autoFocus
                            type="text"
                            inputMode="numeric"
                            defaultValue={l.quantity}
                            aria-label={`Quantity of ${lineName(l)}`}
                            className="h-9 w-14 text-center tabular-nums"
                            onFocus={(e) => e.target.select()}
                            onBlur={(e) => {
                              c.setQuantity(l.key, e.target.value);
                              setEditingQty(null);
                            }}
                            onKeyDown={(e) => {
                              if (e.key === 'Enter') e.currentTarget.blur();
                              if (e.key === 'Escape') setEditingQty(null);
                            }}
                          />
                        ) : (
                          <button type="button" onClick={() => setEditingQty(l.key)} className="h-9 min-w-10 rounded-md px-1 text-center text-sm font-semibold tabular-nums hover:bg-muted" aria-label={`Quantity ${l.quantity}. Tap to type a quantity`}>
                            {l.quantity}
                          </button>
                        )}
                        <Button variant="outline" size="icon" className="size-9" aria-label={`One more ${lineName(l)}`} disabled={l.quantity >= l.maxQty} onClick={() => c.changeQuantity(l.key, 1)}>
                          <Plus className="size-3.5" aria-hidden="true" />
                        </Button>
                      </div>
                      <p className="text-sm font-semibold tabular-nums">{formatCurrency(ql ? ql.total : l.price * l.quantity)}</p>
                    </div>
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </div>
      <div className="space-y-2 border-t bg-card p-4">
        <div className="flex items-center justify-between text-sm">
          <span className="text-muted-foreground">Subtotal</span>
          <span className="font-medium tabular-nums">{formatCurrency(q ? q.subtotal : estimate)}</span>
        </div>
        <div className="flex items-center justify-between text-sm">
          <label htmlFor="co-discount" className="text-muted-foreground">
            Discount <span className="text-xs">(F8)</span>
          </label>
          <Input
            ref={discountRef}
            id="co-discount"
            type="text"
            inputMode="decimal"
            className="h-9 w-28 text-right tabular-nums"
            value={c.discountText}
            placeholder="0.00"
            onChange={(e) => c.setDiscountText(e.target.value.replace(/[^0-9.]/g, ''))}
            onKeyDown={(e) => e.key === 'Enter' && searchRef.current?.focus()}
          />
        </div>
        <div className="flex items-center justify-between text-sm">
          <span className="text-muted-foreground">{settings?.taxLabel || 'Tax'}</span>
          <span className="font-medium tabular-nums">{q ? formatCurrency(q.tax) : '—'}</span>
        </div>
        <div className="flex items-center justify-between border-t pt-2">
          <span className="flex items-center gap-2 text-sm font-semibold">
            Total
            {c.quote.loading && (
              <span className="inline-flex items-center gap-1 text-xs font-normal text-muted-foreground" role="status">
                <Loader2 className="size-3 animate-spin" aria-hidden="true" /> checking
              </span>
            )}
          </span>
          <span className={cn('text-xl font-bold tabular-nums text-navy', !q && 'text-muted-foreground')}>{formatCurrency(total)}</span>
        </div>
        {quoteProblem && (
          <InlineAlert className="text-xs">
            <span>{quoteProblem.code === 'NETWORK' ? 'The total could not be checked (no connection).' : quoteProblem.message}</span>{' '}
            <button type="button" className="inline-flex items-center gap-1 font-medium underline" onClick={c.retryQuote}>
              <RefreshCw className="size-3" aria-hidden="true" /> Check again
            </button>
          </InlineAlert>
        )}
        <Button className="h-12 w-full text-base font-bold" disabled={!lines.length || !c.quoteReady} onClick={openPay}>
          <Banknote className="size-5" aria-hidden="true" /> PAY {formatCurrency(total)}
          <span className="hidden text-xs font-normal opacity-80 lg:inline">F10</span>
        </Button>
      </div>
    </div>
  );

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden md:flex-row">
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
        <div className="space-y-2 border-b bg-card p-3">
          {c.problem && <InlineAlert onClose={() => c.setProblem(null)}>{c.problem}</InlineAlert>}
          <div className="flex flex-wrap items-center gap-2">
            <Select value={c.storeId} onValueChange={c.changeStore} disabled={c.stores.length < 2}>
              <SelectTrigger className="h-10 w-full sm:w-[190px]" aria-label="Store">
                <SelectValue placeholder="Store" />
              </SelectTrigger>
              <SelectContent>
                {c.stores.map((s) => (
                  <SelectItem key={s.id} value={String(s.id)}>
                    {s.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <span className="hidden text-sm text-muted-foreground lg:inline">Cashier: {currentUser?.name}</span>
            <Button type="button" variant="ghost" size="icon" className="ml-auto size-10" aria-label="Keyboard shortcuts" onClick={() => setHelp(true)}>
              <HelpCircle className="size-5" aria-hidden="true" />
            </Button>
          </div>
          <div className="relative">
            <Search className="pointer-events-none absolute top-1/2 left-3 size-5 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
            <Input
              ref={searchRef}
              autoFocus={finePointer()}
              type="search"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search by name or SKU · F2"
              aria-label="Search products by name or SKU"
              className="h-11 pl-10 text-base"
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.ctrlKey && !e.metaKey) {
                  e.preventDefault();
                  onSearchEnter();
                } else if (e.key === 'Escape') setSearch('');
              }}
            />
          </div>
          <div className="no-scrollbar -mx-3 flex gap-1.5 overflow-x-auto px-3 pb-1">
            <CategoryPill active={category === 'all'} onClick={() => setCategory('all')}>
              All
            </CategoryPill>
            {favs.size > 0 && (
              <CategoryPill active={category === 'fav'} onClick={() => setCategory('fav')}>
                <Star className="size-3.5" aria-hidden="true" /> Favourites
              </CategoryPill>
            )}
            {categories.map((cat) => (
              <CategoryPill key={cat} active={category === cat} onClick={() => setCategory(cat)}>
                {cat}
              </CategoryPill>
            ))}
          </div>
        </div>

        <div className={cn('min-h-0 flex-1 overflow-y-auto p-3 scrollbar-thin', lines.length && 'max-md:pb-24')}>
          {shown.length === 0 ? (
            <div className="flex h-full flex-col items-center justify-center text-center text-muted-foreground">
              <Search className="mb-2 size-8 opacity-40" aria-hidden="true" />
              <p className="text-sm font-medium">{category === 'fav' ? 'No favourites match' : 'No products found'}</p>
              <p className="mt-1 text-xs">{category === 'fav' ? 'Tap the star on a product to add it.' : 'Try another search or category.'}</p>
            </div>
          ) : (
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-6">
              {shown.map((p) => (
                <ProductTile
                  key={p.id}
                  product={p}
                  lockedByOther={isLocked(p)}
                  onPick={pick}
                  inCart={inCart.get(p.id) || 0}
                  favourite={favs.has(p.id)}
                  onToggleFavourite={toggleFav}
                />
              ))}
            </div>
          )}
        </div>

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

      <aside className="hidden min-h-0 w-[360px] shrink-0 flex-col border-l bg-card md:flex lg:w-[420px]" aria-label="Current sale">
        {cart}
      </aside>

      <Dialog open={cartOpen} onOpenChange={setCartOpen}>
        <DialogContent sheet className="flex h-[calc(100dvh-2rem)] flex-col gap-0 overflow-hidden p-0 md:hidden">
          <DialogTitle className="sr-only">Current sale</DialogTitle>
          <DialogDescription className="sr-only">Items, totals and payment</DialogDescription>
          {cart}
        </DialogContent>
      </Dialog>

      <VariantSheet product={choosing} lines={lines} onPick={add} onClose={() => setChoosing(null)} />
      <CustomerPicker
        open={pickCustomer}
        onClose={() => {
          setPickCustomer(false);
          setTimeout(focusSearch, 0);
        }}
        customers={c.customers}
        value={c.customerId}
        onPick={c.setCustomerId}
        onAdded={(cu) => c.setCustomers((prev) => [...prev, cu])}
      />
      <ShortcutsHelp open={help} onClose={() => setHelp(false)} />
      <PaymentSheet open={payOpen} onClose={() => setPayOpen(false)} total={total} customerEmail={customer?.email || ''} onPay={pay} />
      <SaleDone
        sale={receiptFor ? null : done}
        onPrint={() => setReceiptFor({ sale: done, print: true })}
        onReceipt={() => setReceiptFor({ sale: done, print: false })}
        onNewSale={newSale}
      />
      {receiptFor && <Receipt isOpen sale={receiptFor.sale} store={store} autoPrint={receiptFor.print} onClose={() => setReceiptFor(null)} />}
      <PriceChangedDialog
        changes={c.changedPrices}
        nameOf={(ch) => {
          const l = lines.find((i) => i.variantId === ch.variantId);
          return l ? lineName(l) : undefined;
        }}
        onAccept={c.acceptNewPrices}
        onClose={() => c.setChangedPrices(null)}
      />
    </div>
  );
}
