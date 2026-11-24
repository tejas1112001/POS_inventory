import { toast } from '../../../shared/toast/store';
import { HoldDialog, RecallDialog } from './HeldBills';
import { usePermission } from '@/context/PermissionContext';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Banknote, HelpCircle, ListRestart, Loader2, PauseCircle, RefreshCw, Search, ShoppingCart, Star, Trash2, User, X } from 'lucide-react';
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
import { mrpText } from '@/lib/mrp';
import { beep, findByCode } from '@/lib/scan';
import { useScanner } from '@/hooks/useScanner';
import { cn } from '@/lib/utils';
import SearchBox from '@/components/kit/SearchBox';
import CartHeader from '@/components/kit/CartHeader';
import QtyStepper from '@/components/kit/QtyStepper';
import IconTile from '@/components/kit/IconTile';
import { api } from '@/services/api';
import useCheckout, { lineName } from './useCheckout';
import PaymentSheet from './PaymentSheet';
import SaleDone from './SaleDone';
import CustomerPicker from './CustomerPicker';
import ShortcutsHelp from './ShortcutsHelp';
import BatchPicker from './BatchPicker';
import { addQty, fmtQty } from '@/lib/qty';
import { lineBase, unitOptionLabel } from '@/lib/saleUnits';
import { stateLabel, useGstStates } from '@/lib/gst';

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
  const [holding, setHolding] = useState(false);
  const [recalling, setRecalling] = useState(false);
  const { hasPermission } = usePermission();
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
  const [picking, setPicking] = useState(null); // a cart line (industry P3 item 3)
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
    for (const l of lines) m.set(l.productId, addQty(m.get(l.productId) || 0, lineBase(l)));
    return m;
  }, [lines]);

  const shown = useMemo(() => {
    const q = search.trim().toLowerCase();
    return c.products.filter((p) => {
      if (category === 'fav' && !favs.has(p.id)) return false;
      if (category !== 'all' && category !== 'fav' && p.category !== category) return false;
      if (!q) return true;
      return p.name.toLowerCase().includes(q) || variantsOf(p).some((v) => v.sku?.toLowerCase().includes(q) || v.itemBarcode?.toLowerCase() === q);
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

  const add = async (product, variant, unitId = null, scaleQty = null) => added(await c.addVariant(product, variant, unitId, scaleQty));

  const pick = (product) => {
    const variants = variantsOf(product);
    if (variants.length > 1) setChoosing(product);
    else if (variants.length === 1) add(product, variants[0]);
    else c.setProblem(`"${product.name}" has no options to sell.`);
  };

  // addByCode adds the variant of an exact barcode or SKU (industry P1
  // item 3): from the loaded list first, then the server (A5) for a
  // product the list does not carry. Returns true when something matched.
  const addByCode = async (code) => {
    const hit = findByCode(c.products, code);
    if (hit) {
      if (isLocked(hit.product)) c.setProblem(`"${hit.product.name}" is in use at another till.`);
      else await add(hit.product, hit.variant, hit.unitId);
      return true;
    }
    try {
      const p = await api.get(`/api/products/barcode/${encodeURIComponent(code)}?storeId=${encodeURIComponent(c.storeId)}`, { quiet: true });
      const v = variantsOf(p).find((x) => x.id === p.variantId) || (variantsOf(p).length === 1 ? variantsOf(p)[0] : null);
      if (v) {
        // A weighing scale's label carries the quantity (by weight or price).
        await add(p, v, p.unitId ?? null, p.quantity != null ? Number(p.quantity) : null);
        return true;
      }
    } catch (err) {
      if (err.status !== 404) c.setProblem(err.message || 'Could not look up the barcode.');
    }
    return false;
  };

  // A scanner's code: added once per scan; no match beeps and keeps the
  // code in the search box so it can be fixed.
  const onScan = async (code) => {
    if (await addByCode(code)) {
      setSearch('');
      return;
    }
    beep();
    toast.error(`No product with barcode ${code}`);
    setSearch(code);
    focusSearch();
  };
  useScanner(onScan, { enabled: !payOpen, accept: (t) => t === searchRef.current });

  const onSearchEnter = async () => {
    const q = search.trim().toLowerCase();
    if (!q) return;
    if (findByCode(c.products, search.trim())) {
      await addByCode(search.trim());
      setSearch('');
      return;
    }
    const usable = shown.filter((p) => !isLocked(p) && productStock(p) > 0);
    if (usable.length === 1) {
      pick(usable[0]);
      setSearch('');
    } else if (!usable.length) c.setProblem(`Nothing matches "${search.trim()}".`);
  };

  // Strict MRP (pharmacy plan §6.2): a line above its MRP, or a pharmacy
  // medicine with no MRP, cannot be paid for; the server refuses it too.
  const mrpBlocked = (c.quote?.data?.lines || []).some((x) => x.aboveMrp || x.mrpRequired);
  const openPay = useCallback(() => {
    if (lines.length && c.quoteReady && !mrpBlocked) setPayOpen(true);
  }, [lines.length, c.quoteReady, mrpBlocked]);

  // Held bills (S10 item 7): park the cart, or load a parked one.
  const holdBill = async (label) => {
    await api.post(
      '/api/held-carts',
      {
        storeId: Number(c.storeId),
        ...(c.customerId && String(c.customerId) !== String(c.walkIn?.id) ? { customerId: Number(c.customerId) } : {}),
        label,
        discount: c.discount || 0,
        lines: lines.map((l) => ({
          productId: l.productId,
          variantId: l.variantId,
          quantity: l.quantity,
          unitPrice: l.unitPrice,
          name: l.name,
          label: l.label || '',
          ...(l.unitId ? { unitId: l.unitId } : {}), // H3: quantity in this unit
        })),
      },
      { quiet: true }
    );
    setHolding(false);
    c.clearCart();
    if (c.walkIn) c.setCustomerId(c.walkIn.id);
    toast.success(label ? `Bill "${label}" held.` : 'Bill held.');
  };
  const recallBill = async (held) => {
    setRecalling(false);
    await c.restoreCart(held);
    toast.success(held.label ? `Bill "${held.label}" recalled.` : 'Bill recalled.');
  };

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
      if (e.key === 'F6') {
        e.preventDefault();
        if (lines.length) setHolding(true);
        return;
      }
      if (e.key === 'F7') {
        e.preventDefault();
        setRecalling(true);
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
  // M3: the quote's GST (split, place of supply) and its tax by kind.
  const gst = q?.gst;
  const taxParts = useMemo(() => {
    const sum = (k) => (q?.lines || []).reduce((n, l) => n + (Number(l[k]) || 0), 0);
    switch (gst?.taxSplit) {
      case 'cgst_sgst':
        return [['CGST', sum('cgst')], ['SGST', sum('sgst')]];
      case 'cgst_utgst':
        return [['CGST', sum('cgst')], ['UTGST', sum('sgst')]];
      case 'igst':
        return [['IGST', sum('igst')]];
      default:
        return [];
    }
  }, [q, gst?.taxSplit]);
  const canCredit = hasPermission('sales', 'credit');
  const store = c.stores.find((s) => String(s.id) === String(c.storeId));
  const q = c.quoteReady ? c.quote.data : null;
  const estimate = lines.reduce((n, l) => n + l.price * lineBase(l), 0);
  const total = q ? Number(q.total) : Math.max(0, estimate - c.discount);
  const itemCount = lines.reduce((n, l) => addQty(n, lineBase(l)), 0); // base units
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
      <div className="flex items-center justify-between gap-2 border-b px-4 py-2.5 max-md:pr-14">
        <CartHeader count={itemCount}>
          <button
            type="button"
            onClick={() => setPickCustomer(true)}
            title="Change customer (F4)"
            className="-mx-1 inline-flex min-h-7 min-w-0 items-center gap-1.5 rounded-md px-1 font-medium text-foreground hover:bg-accent"
          >
            <User className="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />
            <span className="truncate">{customer?.name || 'Walk-in Customer'}</span>
            {customer?.gstin && (
              <span className="shrink-0 rounded bg-accent px-1.5 py-0.5 font-mono text-[11px] text-accent-foreground" title="Registered buyer: a B2B invoice">
                B2B · {customer.gstin}
              </span>
            )}
            <span className="sr-only">Change customer</span>
          </button>
        </CartHeader>
        <div className="flex items-center gap-1">
          <Button variant="ghost" size="sm" disabled={!lines.length} onClick={() => setHolding(true)} title="Park this bill (F6)">
            <PauseCircle className="size-4" aria-hidden="true" /> Hold
          </Button>
          <Button variant="ghost" size="sm" onClick={() => setRecalling(true)} title="Held bills (F7)">
            <ListRestart className="size-4" aria-hidden="true" /> Recall
          </Button>
          <Button variant="ghost" size="sm" className="text-danger hover:bg-danger-bg hover:text-danger" disabled={!lines.length} onClick={askClear}>
            <X className="size-4" aria-hidden="true" /> Clear
          </Button>
        </div>
      </div>
      <PlaceOfSupply c={c} gst={gst} customer={customer} />
      <div className="min-h-0 flex-1 overflow-y-auto scrollbar-thin">
        {lines.length === 0 ? (
          <div className="flex h-full flex-col items-center justify-center p-6 text-center">
            <IconTile icon={ShoppingCart} size="lg" className="mb-3" />
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
                        <p className="truncate text-sm font-semibold text-navy">{l.name}</p>
                        {l.label && <p className="truncate text-xs text-slate-600 dark:text-slate-300">{l.label}</p>}
                        <p className="text-xs text-slate-600 tabular-nums dark:text-slate-300">
                          {l.unitId ? `${formatCurrency(l.price * l.factor)} per ${l.unitName}` : `${formatCurrency(l.price)} each`}
                          {ql && Number(ql.taxRate) > 0 ? ` · tax ${Number(ql.taxRate)}%` : ''}
                          {ql?.mrp != null ? ` · MRP ${mrpText(ql.mrp, ql.mrpPack, formatCurrency)}` : ''}
                        </p>
                        {ql?.aboveMrp && (
                          <p className="mt-0.5 text-xs font-medium text-danger">
                            Above MRP: {formatCurrency(ql.total)} for MRP {formatCurrency(ql.mrpAmount)}. Lower the price or add a discount to sell it.
                          </p>
                        )}
                        {ql?.mrpRequired && (
                          <p className="mt-0.5 text-xs font-medium text-danger">No MRP recorded. Add the MRP to the product or its batch to sell it.</p>
                        )}
                        {ql?.batches?.length > 0 && (
                          <div className="mt-0.5 text-xs text-slate-600 dark:text-slate-300">
                            {ql.batches.map((b) => (
                              <p key={b.batchId} className={b.unknown ? 'font-medium text-warning-fg' : ''}>
                                {b.unknown ? 'No batch recorded: check the pack' : `Batch ${b.batchNo} · Exp ${b.expiry}`}
                                {ql.batches.length > 1 ? ` × ${fmtQty(b.quantity)}` : ''}
                                {b.chosen ? ' (chosen)' : ''}
                              </p>
                            ))}
                            <button
                              type="button"
                              className="font-medium text-primary underline-offset-2 hover:underline"
                              onClick={(e) => {
                                e.stopPropagation();
                                setPicking(l);
                              }}
                            >
                              Change batch
                            </button>
                          </div>
                        )}
                        {l.units?.length > 0 && (
                          // H3: sell this line by the piece or in another of the product's units.
                          <div className="mt-1" onClick={(e) => e.stopPropagation()}>
                            <Select value={l.unitId ? String(l.unitId) : 'base'} onValueChange={(v) => c.setLineUnit(l.key, v === 'base' ? null : v)}>
                              <SelectTrigger className="h-8 w-auto min-w-[8rem] text-xs" aria-label={`Unit of ${lineName(l)}`}>
                                <SelectValue />
                              </SelectTrigger>
                              <SelectContent>
                                <SelectItem value="base">{l.baseName || 'Single'}</SelectItem>
                                {l.units.map((u) => (
                                  <SelectItem key={u.unitId} value={String(u.unitId)}>
                                    {unitOptionLabel(u)}
                                  </SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                            {l.unitId && <span className="ml-2 text-xs text-slate-600 tabular-nums dark:text-slate-300">= {fmtQty(lineBase(l))} {l.baseName}</span>}
                          </div>
                        )}
                        {l.maxQty <= 0 ? (
                          <p className="mt-0.5 text-xs font-medium text-danger-fg">Not in stock at this store</p>
                        ) : (
                          lineBase(l) > l.maxQty && <p className="mt-0.5 text-xs font-medium text-warning-fg">Only {fmtQty(l.maxQty)} {l.baseName} at this store</p>
                        )}
                      </div>
                      <Button variant="ghost" size="icon" className="size-9 shrink-0 text-muted-foreground hover:text-danger" aria-label={`Remove ${lineName(l)}`} onClick={() => c.removeLine(l.key)}>
                        <Trash2 className="size-4" aria-hidden="true" />
                      </Button>
                    </div>
                    <div className="mt-1 flex items-center justify-between">
                      <QtyStepper
                        decreaseLabel={`One less ${lineName(l)}`}
                        increaseLabel={`One more ${lineName(l)}`}
                        onDecrease={() => c.changeQuantity(l.key, -1)}
                        onIncrease={() => c.changeQuantity(l.key, 1)}
                        increaseDisabled={lineBase({ ...l, quantity: addQty(l.quantity, 1) }) > l.maxQty}
                      >
                          {editingQty === l.key ? (
                            <Input
                              autoFocus
                              type="text"
                              inputMode={l.allowFraction ? 'decimal' : 'numeric'}
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
                            <button type="button" onClick={() => setEditingQty(l.key)} className="h-9 min-w-10 border-x px-1 text-center text-sm font-bold tabular-nums hover:bg-muted" aria-label={`Quantity ${fmtQty(l.quantity)}${l.unitId ? ` ${l.unitName}` : ''}. Tap to type a quantity`}>
                              {fmtQty(l.quantity)}
                            </button>
                          )}
                      </QtyStepper>
                      <p className="text-sm font-bold text-navy tabular-nums">{formatCurrency(ql ? ql.total : l.price * lineBase(l))}</p>
                    </div>
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </div>
      <div className="space-y-2 border-t bg-muted/40 p-4">
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
        {taxParts.length > 0 && (
          <div className="-mt-1 flex justify-end gap-3 text-xs text-slate-600 tabular-nums dark:text-slate-300" aria-label="Tax by kind">
            {taxParts.map(([k, v]) => (
              <span key={k}>
                {k} {formatCurrency(v)}
              </span>
            ))}
          </div>
        )}
        {(q?.gstWarnings || []).map((w) => (
          <p key={w} className="text-xs font-medium text-warning-fg">
            {w}
          </p>
        ))}
        <div className="flex items-center justify-between border-t pt-2">
          <span className="flex items-center gap-2 text-sm font-semibold">
            Total
            {c.quote.loading && (
              <span className="inline-flex items-center gap-1 text-xs font-normal text-muted-foreground" role="status">
                <Loader2 className="size-3 animate-spin" aria-hidden="true" /> checking
              </span>
            )}
          </span>
          <span className={cn('text-2xl font-extrabold tabular-nums text-navy', !q && 'text-muted-foreground')}>{formatCurrency(total)}</span>
        </div>
        {q?.batchProblem && (
          <InlineAlert className="text-xs">
            {q.batchProblem.message}
          </InlineAlert>
        )}
        {quoteProblem && (
          <InlineAlert className="text-xs">
            <span>{quoteProblem.code === 'NETWORK' ? 'The total could not be checked (no connection).' : quoteProblem.message}</span>{' '}
            <button type="button" className="inline-flex items-center gap-1 font-medium underline" onClick={c.retryQuote}>
              <RefreshCw className="size-3" aria-hidden="true" /> Check again
            </button>
          </InlineAlert>
        )}
        {mrpBlocked && (
          <InlineAlert className="text-xs">A line is above its MRP or has no MRP. Fix it to take payment.</InlineAlert>
        )}
        <Button className="h-12 w-full rounded-xl text-base font-bold" disabled={!lines.length || !c.quoteReady || !!q?.batchProblem || mrpBlocked} onClick={openPay}>
          <Banknote className="size-5" aria-hidden="true" /> Pay {formatCurrency(total)}
          <span className="hidden text-xs font-normal lg:inline">F10</span>
        </Button>
      </div>
    </div>
  );

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden md:flex-row">
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
        <div className="space-y-2 px-3 pt-3 sm:px-4 sm:pt-4 lg:px-5">
          {c.problem && <InlineAlert onClose={() => c.setProblem(null)}>{c.problem}</InlineAlert>}
          <div className="flex flex-wrap items-center gap-2">
            <SearchBox
              ref={searchRef}
              autoFocus={finePointer()}
              className="w-full lg:w-auto lg:min-w-[14rem] lg:flex-1"
              value={search}
              onChange={setSearch}
              placeholder="Scan, or search by name, SKU or barcode · F2"
              label="Search products by name, SKU or barcode"
              onKeyDown={(e) => {
                if (e.key === 'Enter' && !e.ctrlKey && !e.metaKey) {
                  e.preventDefault();
                  onSearchEnter();
                } else if (e.key === 'Escape') setSearch('');
              }}
            />
            <Select value={c.storeId} onValueChange={c.changeStore} disabled={c.stores.length < 2}>
              <SelectTrigger className="h-10 min-w-0 flex-1 sm:w-[190px] sm:flex-none" aria-label="Store">
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
            <Button type="button" variant="ghost" size="icon" className="size-10 shrink-0" aria-label="Keyboard shortcuts" onClick={() => setHelp(true)}>
              <HelpCircle className="size-5" aria-hidden="true" />
            </Button>
          </div>
          <div className="no-scrollbar -mx-3 flex gap-1.5 overflow-x-auto px-3 pb-1 sm:-mx-4 sm:px-4 lg:-mx-5 lg:px-5">
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

        <div className={cn('min-h-0 flex-1 overflow-y-auto px-3 pt-2 pb-3 scrollbar-thin sm:px-4 sm:pb-4 lg:px-5', lines.length && 'max-md:pb-24')}>
          {shown.length === 0 ? (
            <div className="flex h-full flex-col items-center justify-center text-center text-muted-foreground">
              <Search className="mb-2 size-8 opacity-40" aria-hidden="true" />
              <p className="text-sm font-medium">{category === 'fav' ? 'No favourites match' : 'No products found'}</p>
              <p className="mt-1 text-xs">{category === 'fav' ? 'Tap the star on a product to add it.' : 'Try another search or category.'}</p>
            </div>
          ) : (
            <div className="grid grid-cols-2 gap-2.5 sm:grid-cols-3 sm:gap-3 xl:grid-cols-4 2xl:grid-cols-6">
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
            <Button className="h-12 w-full justify-between rounded-xl text-base" onClick={() => setCartOpen(true)}>
              <span>
                {itemCount} {itemCount === 1 ? 'item' : 'items'} · <span className="tabular-nums">{formatCurrency(total)}</span>
              </span>
              <span>Review</span>
            </Button>
          </div>
        )}
      </div>

      <aside className="hidden min-h-0 w-[360px] shrink-0 flex-col overflow-hidden rounded-2xl border bg-card shadow-xs md:my-4 md:mr-4 md:flex lg:mr-5 lg:w-[420px]" aria-label="Current sale">
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
      <HoldDialog open={holding} onClose={() => setHolding(false)} onHold={holdBill} />
      <RecallDialog open={recalling} storeId={c.storeId} cartBusy={lines.length > 0} onClose={() => setRecalling(false)} onRecall={recallBill} />
      <PaymentSheet
        open={payOpen}
        onClose={() => setPayOpen(false)}
        total={total}
        customerEmail={customer?.email || ''}
        onPay={pay}
        creditCustomer={customer && String(customer.id) !== String(c.walkIn?.id) ? customer.id : null}
        onAccount={
          canCredit && customer && String(customer.id) !== String(c.walkIn?.id)
            ? { name: customer.name, owes: Number(customer.due) || 0, limit: customer.creditLimit == null ? null : Number(customer.creditLimit) }
            : null
        }
      />
      <SaleDone
        sale={receiptFor ? null : done}
        onPrint={() => setReceiptFor({ sale: done, print: true })}
        onReceipt={() => setReceiptFor({ sale: done, print: false })}
        onNewSale={newSale}
      />
      {receiptFor && <Receipt isOpen sale={receiptFor.sale} store={store} autoPrint={receiptFor.print} onClose={() => setReceiptFor(null)} />}
      {picking && (
        <BatchPicker
          line={{ ...picking, quantity: lineBase(picking) }}
          storeId={c.storeId}
          onClose={() => setPicking(null)}
          onPick={(picks) => {
            c.setLineBatches(picking.key, picks);
            setPicking(null);
          }}
        />
      )}
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

// PlaceOfSupply (M3 §8): where the goods are supplied, the store's state
// unless the cashier changes it (delivery to another state: IGST). Shown
// once the shop has a GST state (the quote then names a place of supply).
function PlaceOfSupply({ c, gst, customer }) {
  const states = useGstStates();
  if (!gst?.placeOfSupply && !c.placeOfSupply) return null;
  const place = c.placeOfSupply || gst?.placeOfSupply || '';
  const other = customer?.gstin && customer.stateCode && customer.stateCode !== place;
  return (
    <div className="border-b px-4 py-1.5 text-xs">
      <label className="flex items-center justify-between gap-2">
        <span className="text-muted-foreground">Place of supply</span>
        <select
          className="h-7 max-w-[60%] rounded-lg border bg-card px-1 text-xs"
          value={place}
          onChange={(e) => c.setPlaceOfSupply(e.target.value === gst?.sellerState ? '' : e.target.value)}
          aria-label="Place of supply"
        >
          {(states || []).map((s) => (
            <option key={s.code} value={s.code}>
              {s.name} ({s.code})
            </option>
          ))}
          {!states && <option value={place}>{place}</option>}
        </select>
      </label>
      {other && (
        <p className="mt-1 text-warning-fg">
          The buyer is registered in {stateLabel(states, customer.stateCode)}. Delivering there? Change the place of supply.
        </p>
      )}
    </div>
  );
}
