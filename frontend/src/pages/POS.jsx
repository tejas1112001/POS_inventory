import { useState, useEffect, useRef, useCallback } from 'react';
import {
  Search, Minus, Plus, Trash2, ShoppingCart, X, UserPlus, Banknote, CheckCircle2,
} from 'lucide-react';
import CustomerForm from '../components/CustomerForm';
import PayForm from '../components/PayForm';
import Receipt from '../components/Receipt';
import { api } from '../services/api';
import { usePos } from '../hooks/usePos';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { ProductImage } from '@/components/shared/product-image';
import { formatCurrency } from '@/lib/format';
import { cn } from '@/lib/utils';

const usePrevious = (value) => {
  const ref = useRef();
  useEffect(() => {
    ref.current = value;
  }, [value]);
  return ref.current;
};

function CategoryPill({ active, onClick, children }) {
  return (
    <button
      onClick={onClick}
      className={cn(
        'shrink-0 rounded-full px-4 py-1.5 text-sm font-medium transition-colors',
        active ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground hover:bg-accent hover:text-foreground'
      )}
    >
      {children}
    </button>
  );
}

export default function POS() {
  const { currentUser } = usePos();
  const [products, setProducts] = useState([]);
  const [customers, setCustomers] = useState([]);
  const [stores, setStores] = useState([]);
  const [selectedStore, setSelectedStore] = useState('');
  const [selectedCustomer, setSelectedCustomer] = useState(null);
  const [searchTerm, setSearchTerm] = useState('');
  const [activeCategory, setActiveCategory] = useState('all');
  const [orderItems, setOrderItems] = useState([]);
  const [showCustomerForm, setShowCustomerForm] = useState(false);
  const [categories, setCategories] = useState([]);
  const [showPayForm, setShowPayForm] = useState(false);
  const [showReceipt, setShowReceipt] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [clearConfirm, setClearConfirm] = useState(false);

  const subtotal = orderItems.reduce((sum, item) => sum + (item.price * item.quantity), 0);
  const [discount, setDiscount] = useState(0.0);
  const tax = subtotal * 0.08;
  const total = subtotal + tax - discount;

  const prevOrderItems = usePrevious(orderItems);

  const fetchProducts = useCallback(async (storeId) => {
    if (!storeId) return;
    try {
      setLoading(true);
      const data = await api.get(`/api/products/pos?storeId=${storeId}`);
      setProducts(data);
    } catch (err) {
      setError('Failed to fetch products for the selected store');
      console.error(err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    const itemsToUnlock = (prevOrderItems || []).filter(
      (prevItem) => !orderItems.some((item) => item.id === prevItem.id)
    );

    itemsToUnlock.forEach((item) => {
      api.put(`/api/products/${item.id}/lock`, { lock: false, storeId: selectedStore })
        .then((unlockedProduct) => {
          setProducts((prev) => prev.map((p) => (p.id === unlockedProduct.id ? unlockedProduct : p)));
        })
        .catch((err) => console.error(`Failed to unlock product ${item.id}`, err));
    });
  }, [orderItems, prevOrderItems, selectedStore]);

  useEffect(() => {
    return () => {
      setOrderItems((currentOrderItems) => {
        currentOrderItems.forEach((item) => {
          api.put(`/api/products/${item.id}/lock`, { lock: false, storeId: selectedStore })
            .catch((err) => console.error(`Failed to unlock product ${item.id} on unmount`, err));
        });
        return currentOrderItems;
      });
    };
  }, [selectedStore]);

  useEffect(() => {
    const fetchInitialData = async () => {
      try {
        setLoading(true);
        const fetchedCustomers = await api.get('/api/customers');
        setCustomers(fetchedCustomers);

        if (fetchedCustomers) {
          const walkInCustomer = fetchedCustomers.find((c) => c.name === 'Walk-in Customer');
          setSelectedCustomer(walkInCustomer ? walkInCustomer.id : (fetchedCustomers[0]?.id || null));
        }

        const fetchedCategories = await api.get('/api/categories');
        setCategories(fetchedCategories);

        const fetchedStores = await api.get('/api/stores');
        setStores(fetchedStores);
        if (fetchedStores && fetchedStores.length > 0) {
          setSelectedStore(fetchedStores[0].id);
        } else {
          setError('No stores found. Please add a store.');
        }
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
    if (selectedStore) {
      fetchProducts(selectedStore);
    }
  }, [selectedStore, fetchProducts]);

  const filteredProducts = products
    .filter((product) => activeCategory === 'all' || product.category === activeCategory)
    .filter((product) => product.name.toLowerCase().includes(searchTerm.toLowerCase()));

  const addToOrder = useCallback(async (product) => {
    if (orderItems.some((item) => item.id === product.id)) {
      setError(`"${product.name}" is already in the cart. Use the "+" button to increase quantity.`);
      return;
    }

    try {
      const lockedProduct = await api.put(`/api/products/${product.id}/lock`, {
        lock: true,
        storeId: selectedStore,
      });

      const variant = lockedProduct.ProductVariants?.[0];
      if (!variant) {
        throw new Error(`No variants available for ${lockedProduct.name}.`);
      }

      if (variant.qty <= 0) {
        await api.put(`/api/products/${lockedProduct.id}/lock`, { lock: false, storeId: selectedStore });
        throw new Error(`Insufficient stock for ${lockedProduct.name}`);
      }

      setProducts((prevProducts) => prevProducts.map((p) => (p.id === lockedProduct.id ? lockedProduct : p)));

      setOrderItems((prev) => [...prev, {
        ...lockedProduct,
        quantity: 1,
        variantId: variant.id,
        unitPrice: variant.price,
        price: variant.price,
      }]);
    } catch (err) {
      const errorMessage = err.response?.data?.message || err.message || `Error adding "${product.name}"`;
      setError(errorMessage);
    }
  }, [orderItems, selectedStore]);

  function addQuantity(item) {
    setOrderItems((prev) => prev.map((i) => (i.id === item.id ? { ...i, quantity: i.quantity + 1 } : i)));
  }

  function removeQuantity(item) {
    setOrderItems(orderItems.map((newItem) =>
      newItem.id === item.id ? { ...newItem, quantity: newItem.quantity - 1 } : newItem
    ).filter((newItem) => newItem.quantity !== 0));
  }

  function clearAll() {
    setOrderItems([]);
    setError(null);
    setDiscount(0.0);
  }

  const processOrder = async (paymentData) => {
    try {
      if (!selectedStore || !selectedCustomer) {
        throw new Error('Store and Customer must be selected');
      }

      const orderData = {
        ...paymentData,
        orderItems: orderItems.map((item) => ({
          productId: item.id,
          variantId: item.variantId,
          quantity: item.quantity,
          unitPrice: item.unitPrice,
          total: item.price * item.quantity,
        })),
        customerId: selectedCustomer,
        storeId: selectedStore,
        subtotal,
        discount,
        tax,
        total,
      };

      const result = await api.post('/api/sales', orderData);
      setShowReceipt(true);
      return result;
    } catch (err) {
      setError(err.response?.data?.error || err.message || 'Failed to process order');
      throw err;
    }
  };

  const isProductLocked = (product) => {
    const twoMinutes = 2 * 60 * 1000;
    return product.isLocked && (new Date() - new Date(product.lockedAt)) < twoMinutes;
  };

  const selectedCustomerObj = customers.find((c) => c.id === selectedCustomer);

  if (loading && products.length === 0) {
    return <div className="flex h-full items-center justify-center text-muted-foreground">Loading products...</div>;
  }

  return (
    <div className="flex h-[calc(100dvh-4rem)] flex-col overflow-hidden lg:flex-row">
      {/* Products panel */}
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
        <div className="space-y-3 border-b bg-card p-3">
          {error && (
            <div className="flex items-center justify-between rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              <span>{error}</span>
              <button onClick={() => setError(null)} aria-label="Dismiss error">
                <X className="size-4" />
              </button>
            </div>
          )}

          <div className="flex flex-wrap items-center gap-2">
            <Select value={selectedStore} onValueChange={setSelectedStore} disabled={stores.length === 0}>
              <SelectTrigger className="h-10 w-[170px]">
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

            <Select value={String(selectedCustomer || '')} onValueChange={setSelectedCustomer}>
              <SelectTrigger className="h-10 min-w-[200px] flex-1">
                <SelectValue placeholder="Select customer" />
              </SelectTrigger>
              <SelectContent>
                {customers.map((customer) => (
                  <SelectItem key={customer.id} value={String(customer.id)}>
                    {customer.name} {customer.phone ? `(${customer.phone})` : ''}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            <Button variant="outline" className="h-10" onClick={() => setShowCustomerForm(true)}>
              <UserPlus className="size-4" /> Add New
            </Button>

            <div className="relative min-w-[200px] flex-1">
              <Search className="absolute left-3 top-1/2 size-5 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={searchTerm}
                onChange={(e) => setSearchTerm(e.target.value)}
                placeholder="Search by product name..."
                className="h-10 pl-10"
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && filteredProducts.length > 0) {
                    const first = filteredProducts.find((p) => !isProductLocked(p));
                    if (first) addToOrder(first);
                    setSearchTerm('');
                  }
                }}
              />
            </div>
          </div>

          <div className="no-scrollbar flex gap-1.5 overflow-x-auto pb-1">
            <CategoryPill active={activeCategory === 'all'} onClick={() => setActiveCategory('all')}>
              All
            </CategoryPill>
            {categories.map((category) => (
              <CategoryPill
                key={category.id}
                active={activeCategory === category.name}
                onClick={() => setActiveCategory(category.name)}
              >
                {category.name}
              </CategoryPill>
            ))}
          </div>
        </div>

        <div className="scrollbar-thin flex-1 overflow-y-auto p-3">
          {filteredProducts.length === 0 ? (
            <div className="flex h-full flex-col items-center justify-center text-center text-muted-foreground">
              <Search className="mb-2 size-8 opacity-40" />
              <p className="text-sm font-medium">No products found</p>
              <p className="mt-1 text-xs">Try another search or category.</p>
            </div>
          ) : (
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-5">
              {filteredProducts.map((product) => {
                const locked = isProductLocked(product);
                return (
                  <button
                    key={product.id}
                    onClick={() => !locked && addToOrder(product)}
                    disabled={locked}
                    className={cn(
                      'group flex flex-col overflow-hidden rounded-lg border bg-card text-left shadow-sm transition-all hover:-translate-y-0.5 hover:shadow-md disabled:cursor-not-allowed disabled:opacity-60',
                      locked && 'grayscale'
                    )}
                  >
                    <div className="relative aspect-square w-full">
                      <ProductImage name={product.name} src={product.image} className="!h-full !w-full rounded-none" size={0} />
                      {locked && (
                        <div className="absolute inset-0 flex items-center justify-center bg-background/60">
                          <span className="rounded-md bg-muted px-2 py-1 text-xs font-semibold">Locked</span>
                        </div>
                      )}
                    </div>
                    <div className="flex flex-1 flex-col p-2.5">
                      <p className="line-clamp-2 text-sm font-medium leading-tight">{product.name}</p>
                      <div className="mt-auto flex items-center justify-between pt-2">
                        <span className="text-base font-bold text-primary">{formatCurrency(product.price)}</span>
                        {product.category && (
                          <Badge variant="secondary" className="text-[10px]">{product.category}</Badge>
                        )}
                      </div>
                    </div>
                  </button>
                );
              })}
            </div>
          )}
        </div>
      </div>

      {/* Cart panel */}
      <aside className="flex max-h-[42dvh] w-full shrink-0 flex-col border-t bg-card sm:max-h-[45dvh] lg:max-h-none lg:w-96 lg:border-l lg:border-t-0">
        <div className="flex items-center justify-between border-b p-4">
          <div className="flex items-center gap-2">
            <ShoppingCart className="size-5 text-primary" />
            <h2 className="text-base font-semibold">Current Sale</h2>
            {orderItems.length > 0 && <Badge variant="secondary">{orderItems.length}</Badge>}
          </div>
          <Button
            variant="ghost"
            size="sm"
            className="text-destructive hover:text-destructive"
            disabled={orderItems.length === 0}
            onClick={() => setClearConfirm(true)}
          >
            <X className="size-4" /> Clear
          </Button>
        </div>

        <div className="border-b bg-muted/30 px-4 py-2 text-xs">
          <span className="text-muted-foreground">Customer: </span>
          <span className="font-medium">{selectedCustomerObj?.name || 'Walk-in Customer'}</span>
        </div>

        <div className="scrollbar-thin flex-1 overflow-y-auto">
          {orderItems.length === 0 ? (
            <div className="flex h-full flex-col items-center justify-center p-6 text-center">
              <div className="mb-3 flex size-16 items-center justify-center rounded-full bg-muted text-muted-foreground">
                <ShoppingCart className="size-7" />
              </div>
              <p className="text-sm font-medium">Cart is empty</p>
              <p className="mt-1 text-xs text-muted-foreground">Click products to add them to the sale.</p>
            </div>
          ) : (
            <ul className="divide-y">
              {orderItems.map((item) => (
                <li key={item.id} className="flex gap-3 p-3">
                  <ProductImage name={item.name} src={item.image} size={44} />
                  <div className="flex min-w-0 flex-1 flex-col">
                    <div className="flex items-start justify-between gap-2">
                      <div className="min-w-0">
                        <p className="truncate text-sm font-medium">{item.name}</p>
                        <p className="text-[10px] text-muted-foreground">{formatCurrency(item.price)} each</p>
                      </div>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="size-6 shrink-0 text-muted-foreground hover:text-destructive"
                        onClick={() => setOrderItems((prev) => prev.filter((i) => i.id !== item.id))}
                      >
                        <Trash2 className="size-3.5" />
                      </Button>
                    </div>
                    <div className="mt-2 flex items-center justify-between">
                      <div className="flex items-center gap-1">
                        <Button variant="outline" size="icon" className="size-7" onClick={() => removeQuantity(item)}>
                          <Minus className="size-3" />
                        </Button>
                        <span className="w-8 text-center text-sm font-semibold">{item.quantity}</span>
                        <Button variant="outline" size="icon" className="size-7" onClick={() => addQuantity(item)}>
                          <Plus className="size-3" />
                        </Button>
                      </div>
                      <p className="text-sm font-semibold">{formatCurrency(item.price * item.quantity)}</p>
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
            <span className="font-medium">{formatCurrency(subtotal)}</span>
          </div>
          <div className="flex items-center justify-between text-sm">
            <span className="text-muted-foreground">Discount</span>
            <Input
              type="number"
              className="h-8 w-24 text-right"
              value={discount.toFixed(2)}
              onChange={(e) => setDiscount(parseFloat(e.target.value) || 0)}
              onFocus={(e) => e.target.select()}
              step="0.01"
              min="0"
            />
          </div>
          <div className="flex items-center justify-between text-sm">
            <span className="text-muted-foreground">Tax (8%)</span>
            <span className="font-medium">{formatCurrency(tax)}</span>
          </div>
          <div className="flex items-center justify-between border-t pt-2">
            <span className="text-sm font-semibold">Total</span>
            <span className="text-xl font-bold text-primary">{formatCurrency(total)}</span>
          </div>
          <Button
            className="h-14 w-full text-base font-bold"
            disabled={orderItems.length === 0}
            onClick={() => {
              if (orderItems.length === 0) {
                setError('Please add items to the order first.');
                return;
              }
              setShowPayForm(true);
            }}
          >
            <Banknote className="size-5" /> PAY {formatCurrency(total)}
          </Button>
        </div>
      </aside>

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
        />
      )}

      {showReceipt && (
        <Receipt
          isOpen={showReceipt}
          onClose={() => {
            setShowReceipt(false);
            clearAll();
          }}
          customerName={selectedCustomerObj?.name || 'Walk-in Customer'}
          customerId={selectedCustomer}
          orderItems={orderItems}
          subtotal={subtotal}
          discount={discount}
          tax={tax}
          total={total}
          biller={currentUser?.name || 'System'}
        />
      )}

      {clearConfirm && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={() => setClearConfirm(false)}>
          <div className="w-full max-w-sm rounded-lg bg-card p-5 shadow-xl" onClick={(e) => e.stopPropagation()}>
            <div className="mb-3 flex items-center gap-2">
              <CheckCircle2 className="size-5 text-amber-600" />
              <h3 className="text-base font-semibold">Clear cart?</h3>
            </div>
            <p className="text-sm text-muted-foreground">All items will be removed from the current sale.</p>
            <div className="mt-4 flex justify-end gap-2">
              <Button variant="outline" onClick={() => setClearConfirm(false)}>Cancel</Button>
              <Button variant="destructive" onClick={() => { clearAll(); setClearConfirm(false); }}>Clear Cart</Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
