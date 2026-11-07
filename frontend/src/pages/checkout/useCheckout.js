import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { api } from '@/services/api';
import { priceChanges } from '@/lib/checkout';
import { variantLabel, variantStock, variantsOf } from '@/components/pos/stock';

const RENEW_MS = 60_000; // locks last 2 minutes; renew well before
const QUOTE_DELAY = 300; // wait for the cart to settle before asking the server

export const lineName = (l) => `${l.name}${l.label ? ` (${l.label})` : ''}`;
export const parseAmount = (v) => {
  const n = parseFloat(String(v ?? '').replace(/[^0-9.]/g, ''));
  return Number.isFinite(n) ? n : 0;
};

// cartBody is what both the price check and the sale send.
export function cartBody(lines, { storeId, customerId, discount }) {
  return {
    orderItems: lines.map((l) => ({ productId: l.productId, variantId: l.variantId, quantity: l.quantity, unitPrice: l.unitPrice })),
    storeId: Number(storeId),
    ...(customerId ? { customerId: Number(customerId) } : {}),
    discount,
  };
}

function lock(id, storeId, on, opts = {}) {
  return api.put(`/api/products/${id}/lock`, { lock: on, storeId }, { quiet: true, ...opts });
}

// useCheckout: everything the new checkout keeps between renders.
//  - stores, customers, the chosen store and customer
//  - the store's products (refetched per store)
//  - cart lines, one per variant: { key, productId, variantId, name, label, image, price, unitPrice, quantity, maxQty }
//  - product locks: taken when a product enters the cart, renewed every
//    minute, released when its last line leaves, on store change and when
//    the tab closes
//  - the server's price check for the cart (POST /api/sales/quote)
export default function useCheckout({ defaultCustomerId }) {
  const [stores, setStores] = useState([]);
  const [storeId, setStoreId] = useState('');
  const [customers, setCustomers] = useState([]);
  const [customerId, setCustomerId] = useState(null);
  const [products, setProducts] = useState([]);
  const [productsStore, setProductsStore] = useState('');
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [lines, setLines] = useState([]);
  const [discountText, setDiscountText] = useState('');
  const [problem, setProblem] = useState(null);
  const [quote, setQuote] = useState({ data: null, key: '', loading: false, error: null });
  const [changedPrices, setChangedPrices] = useState(null);
  const locked = useRef(new Set());
  const wanted = useRef('');
  const recheckFor = useRef(null);
  const storeRef = useRef('');
  storeRef.current = storeId;

  const discount = parseAmount(discountText);
  const walkIn = useMemo(() => {
    const byDefault = customers.find((c) => String(c.id) === String(defaultCustomerId));
    return byDefault || customers.find((c) => c.name === 'Walk-in Customer') || customers[0] || null;
  }, [customers, defaultCustomerId]);

  // First load: customers and stores.
  useEffect(() => {
    let active = true;
    (async () => {
      try {
        const [cs, ss] = await Promise.all([api.get('/api/customers', { quiet: true }), api.get('/api/stores', { quiet: true })]);
        if (!active) return;
        setCustomers(Array.isArray(cs) ? cs : cs?.data || []);
        const list = Array.isArray(ss) ? ss : ss?.data || [];
        setStores(list);
        if (list.length) setStoreId(String(list[0].id));
        else setLoadError('No active store. Add or turn on a store in Settings → Stores.');
      } catch (err) {
        if (active) setLoadError(err.message || 'The checkout could not load.');
      } finally {
        if (active) setLoading(false);
      }
    })();
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    if (customerId == null && walkIn) setCustomerId(walkIn.id);
  }, [walkIn, customerId]);

  const fetchProducts = useCallback(async (id) => {
    if (!id) return;
    wanted.current = id;
    try {
      const data = await api.get(`/api/products/pos?storeId=${id}`, { quiet: true });
      if (wanted.current !== id) return; // a newer store was chosen meanwhile
      setProducts(Array.isArray(data) ? data : []);
      setProductsStore(id);
    } catch (err) {
      setProblem(err.message || 'The products could not be loaded.');
    }
  }, []);

  useEffect(() => {
    if (storeId) fetchProducts(storeId);
  }, [storeId, fetchProducts]);

  const replaceProduct = useCallback((p) => p && setProducts((prev) => prev.map((x) => (x.id === p.id ? p : x))), []);

  // Release a product's lock when its last line leaves the cart.
  useEffect(() => {
    const inCart = new Set(lines.map((l) => l.productId));
    for (const id of [...locked.current]) {
      if (inCart.has(id)) continue;
      locked.current.delete(id);
      lock(id, storeId, false).then(replaceProduct).catch(() => {});
    }
  }, [lines, storeId, replaceProduct]);

  // Changing the store (or leaving the page) releases every lock.
  useEffect(() => {
    const held = locked.current;
    return () => {
      for (const id of held) lock(id, storeId, false).catch(() => {});
      held.clear();
    };
  }, [storeId]);

  // Closing the tab: release with keepalive so the request outlives the page.
  useEffect(() => {
    const onHide = () => {
      for (const id of locked.current) lock(id, storeRef.current, false, { keepalive: true }).catch(() => {});
    };
    window.addEventListener('pagehide', onHide);
    return () => window.removeEventListener('pagehide', onHide);
  }, []);

  // Renew the locks of products in the cart.
  useEffect(() => {
    const t = setInterval(() => {
      for (const id of locked.current) {
        lock(id, storeRef.current, true)
          .then(replaceProduct)
          .catch(() => setProblem('Another till took over an item in this cart. It can still be sold; check the stock.'));
      }
    }, RENEW_MS);
    return () => clearInterval(t);
  }, [replaceProduct]);

  const findLine = (key) => lines.find((l) => l.key === key);

  const changeQuantity = useCallback(
    (key, delta) => {
      const line = lines.find((l) => l.key === key);
      if (!line) return;
      const quantity = line.quantity + delta;
      if (delta > 0 && quantity > line.maxQty) {
        setProblem(`Only ${line.maxQty} of "${lineName(line)}" in stock here.`);
        return;
      }
      setProblem(null);
      setLines((prev) => prev.map((l) => (l.key === key ? { ...l, quantity } : l)).filter((l) => l.quantity > 0));
    },
    [lines]
  );

  const setQuantity = useCallback(
    (key, qty) => {
      const line = lines.find((l) => l.key === key);
      if (!line) return;
      const n = Math.floor(Number(qty));
      if (!Number.isFinite(n) || n < 1) return;
      if (n > line.maxQty) {
        setProblem(`Only ${line.maxQty} of "${lineName(line)}" in stock here.`);
        setLines((prev) => prev.map((l) => (l.key === key ? { ...l, quantity: Math.max(1, line.maxQty) } : l)));
        return;
      }
      setProblem(null);
      setLines((prev) => prev.map((l) => (l.key === key ? { ...l, quantity: n } : l)));
    },
    [lines]
  );

  const removeLine = useCallback((key) => setLines((prev) => prev.filter((l) => l.key !== key)), []);

  // addVariant puts one unit in the cart (or one more). Returns the line key.
  const addVariant = useCallback(
    async (product, variant) => {
      setProblem(null);
      const existing = lines.find((l) => l.variantId === variant.id);
      if (existing) {
        changeQuantity(existing.key, 1);
        return existing.key;
      }
      try {
        const fresh = await lock(product.id, storeId, true);
        locked.current.add(product.id);
        replaceProduct(fresh);
        const v = variantsOf(fresh).find((x) => x.id === variant.id) || variant;
        const available = variantStock(v);
        const many = variantsOf(fresh).length > 1;
        if (available <= 0) {
          setProblem(`"${product.name}${many ? ` (${variantLabel(v)})` : ''}" is out of stock here.`);
          return null;
        }
        setLines((prev) => [
          ...prev,
          {
            key: v.id,
            productId: product.id,
            variantId: v.id,
            name: product.name,
            label: many ? variantLabel(v) : '',
            image: fresh.image,
            price: Number(v.price),
            unitPrice: v.price,
            quantity: 1,
            maxQty: available,
          },
        ]);
        return v.id;
      } catch (err) {
        setProblem(err.code === 'PRODUCT_LOCKED' ? `"${product.name}" is in use at another till.` : err.message || `Could not add "${product.name}".`);
        return null;
      }
    },
    [lines, storeId, changeQuantity, replaceProduct]
  );

  // restoreCart loads a held bill (S10 item 7): the customer, the discount
  // and the lines, locking each product at this till again. Nothing was
  // reserved while it was held, so stock is read fresh; the quote then
  // re-prices the cart (a changed price opens "Prices have changed").
  const restoreCart = useCallback(
    async (held) => {
      setProblem(null);
      const notes = [];
      const next = [];
      for (const pid of [...new Set(held.lines.map((l) => l.productId))]) {
        let fresh = null;
        try {
          fresh = await lock(pid, storeId, true);
          locked.current.add(pid);
          replaceProduct(fresh);
        } catch (err) {
          const name = held.lines.find((l) => l.productId === pid)?.name || 'An item';
          notes.push(err.code === 'PRODUCT_LOCKED' ? `"${name}" is in use at another till.` : `"${name}" could not be added.`);
          continue;
        }
        const many = variantsOf(fresh).length > 1;
        for (const l of held.lines.filter((x) => x.productId === pid)) {
          const v = variantsOf(fresh).find((x) => x.id === l.variantId);
          const stock = v ? Math.max(0, variantStock(v)) : 0;
          if (!v || stock <= 0) {
            notes.push(`"${l.name || fresh.name}" is out of stock here.`);
            continue;
          }
          if (l.quantity > stock) notes.push(`Only ${stock} of "${l.name || fresh.name}" left here.`);
          next.push({
            key: v.id,
            productId: pid,
            variantId: v.id,
            name: fresh.name,
            label: many ? variantLabel(v) : '',
            image: fresh.image,
            price: Number(l.unitPrice ?? v.price),
            unitPrice: l.unitPrice ?? v.price,
            quantity: Math.min(l.quantity, stock),
            maxQty: stock,
          });
        }
      }
      setLines(next);
      setDiscountText(Number(held.discount) ? String(held.discount) : '');
      if (held.customerId) setCustomerId(held.customerId);
      if (notes.length) setProblem(notes.join(' '));
    },
    [storeId, replaceProduct]
  );

  const clearCart = useCallback(() => {
    setLines([]);
    setDiscountText('');
    setProblem(null);
    setChangedPrices(null);
  }, []);

  // Changing the store keeps the cart: once the new store's products are
  // in, lines get its stock limits, products are locked there, and lines
  // without enough stock are named.
  const changeStore = useCallback(
    (value) => {
      if (value === storeId) return;
      recheckFor.current = lines.length ? value : null;
      setStoreId(value);
    },
    [storeId, lines.length]
  );

  useEffect(() => {
    const store = recheckFor.current;
    if (!store || productsStore !== store) return;
    recheckFor.current = null;
    const byVariant = new Map();
    for (const p of products) for (const v of variantsOf(p)) byVariant.set(v.id, v);
    const missing = [];
    const short = [];
    const next = lines.map((l) => {
      const v = byVariant.get(l.variantId);
      const stock = v ? Math.max(0, variantStock(v)) : 0;
      if (stock <= 0) missing.push(lineName(l));
      else if (l.quantity > stock) short.push(`${lineName(l)} (${stock} here)`);
      return { ...l, maxQty: stock };
    });
    setLines(next);
    const name = stores.find((s) => String(s.id) === String(store))?.name || 'this store';
    const notes = [];
    if (missing.length) notes.push(`Not in stock at ${name}: ${missing.join(', ')}.`);
    if (short.length) notes.push(`Not enough at ${name}: ${short.join(', ')}.`);
    if (notes.length) notes.push('Change the quantities or remove these items before paying.');
    setProblem(notes.length ? notes.join(' ') : null);
    for (const id of new Set(next.map((l) => l.productId))) {
      lock(id, store, true)
        .then((p) => {
          locked.current.add(id);
          replaceProduct(p);
        })
        .catch(() => setProblem((prev) => `${prev ? `${prev} ` : ''}"${next.find((l) => l.productId === id)?.name}" is in use at another till here.`));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- once per store change, when its products arrive
  }, [products, productsStore]);

  // The server's price check. The key is the request body: a quote is used
  // only while the cart still matches it.
  const body = useMemo(() => cartBody(lines, { storeId, customerId, discount }), [lines, storeId, customerId, discount]);
  const bodyKey = useMemo(() => JSON.stringify(body), [body]);
  const asked = useRef('');
  const [quoteTick, setQuoteTick] = useState(0);

  useEffect(() => {
    if (!lines.length || !storeId) {
      asked.current = '';
      setQuote({ data: null, key: '', loading: false, error: null });
      return;
    }
    if (asked.current === bodyKey) {
      // Back to a cart that was already checked (or is being checked).
      setQuote((q) => (q.loading && q.key === bodyKey ? { ...q, loading: false } : q));
      return;
    }
    setQuote((q) => ({ ...q, loading: true, error: null }));
    const t = setTimeout(() => {
      asked.current = bodyKey;
      const sent = bodyKey;
      api
        .post('/api/sales/quote', JSON.parse(sent), { quiet: true })
        .then((data) => {
          if (asked.current !== sent) return;
          setQuote({ data, key: sent, loading: false, error: null });
          // Fresh stock from the server: keep each line's limit current.
          const avail = new Map((data?.lines || []).map((l) => [l.variantId, Number(l.available)]));
          setLines((prev) => prev.map((l) => (avail.has(l.variantId) && avail.get(l.variantId) !== l.maxQty ? { ...l, maxQty: Math.max(0, avail.get(l.variantId)) } : l)));
        })
        .catch((err) => {
          if (asked.current !== sent) return;
          const changes = priceChanges(err);
          if (changes) setChangedPrices(changes);
          setQuote({ data: null, key: sent, loading: false, error: err });
        });
    }, QUOTE_DELAY);
    return () => clearTimeout(t);
  }, [bodyKey, lines.length, storeId, quoteTick]);

  const retryQuote = () => {
    asked.current = '';
    setQuoteTick((n) => n + 1);
  };

  const acceptNewPrices = () => {
    const byVariant = new Map((changedPrices || []).map((c) => [c.variantId, c.unitPrice]));
    setLines((prev) => prev.map((l) => (byVariant.has(l.variantId) ? { ...l, unitPrice: byVariant.get(l.variantId), price: Number(byVariant.get(l.variantId)) } : l)));
    setChangedPrices(null);
  };

  const quoteReady = !!quote.data && quote.key === bodyKey && !quote.loading;

  return {
    stores,
    storeId,
    changeStore,
    customers,
    setCustomers,
    customerId,
    setCustomerId,
    walkIn,
    products,
    loading,
    loadError,
    lines,
    findLine,
    addVariant,
    changeQuantity,
    setQuantity,
    removeLine,
    clearCart,
    restoreCart,
    discountText,
    setDiscountText,
    discount,
    problem,
    setProblem,
    body,
    bodyKey,
    quote,
    quoteReady,
    retryQuote,
    changedPrices,
    setChangedPrices,
    acceptNewPrices,
  };
}
