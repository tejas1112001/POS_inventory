import { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { PlusCircleIcon, XMarkIcon } from '@heroicons/react/24/outline';
import PageHeader from '../components/forms/PageHeader';
import FormFooter from '../components/forms/FormFooter';
import FormPageLayout, { FormCard, FormBody } from '../components/forms/FormPageLayout';
import FormStepper from '../components/forms/FormStepper';
import { useFormWizard } from '../hooks/useFormWizard';
import { usePermission } from '../context/PermissionContext';
import { api } from '../services/api';
import { currencySymbol, formatCurrency, formatDate } from '@/lib/format';
import { newIdempotencyKey } from '@/lib/checkout';

const STEPS = [
  { id: 'sale', title: 'Sale', description: 'What is returned' },
  { id: 'items', title: 'Items', description: 'Quantities and reasons' },
  { id: 'refund', title: 'Refund', description: 'Money back' },
];
const inputCls = 'w-full rounded-lg border border-gray-300 px-3 py-2 focus:border-blue-500 focus:ring-blue-500';
const today = () => new Date().toLocaleDateString('en-CA');
let seq = 0;
const freeLine = () => ({ key: ++seq, productId: '', variantId: '', quantity: 1, unitPrice: '', reason: '' });

// AddSalesReturn records a return (C-13). A return linked to a sale lists
// that sale's lines with what is left to return; the server refunds the
// price the customer paid and refuses more than was sold. A return
// without a sale needs sales:price_override.
export default function AddSalesReturn() {
  const navigate = useNavigate();
  const { hasPermission } = usePermission();
  const canUnlinked = hasPermission('sales', 'price_override');
  const { currentStep, goNext, goBack, goToStep } = useFormWizard(3);

  const [mode, setMode] = useState('linked');
  const [search, setSearch] = useState('');
  const [found, setFound] = useState([]);
  const [sale, setSale] = useState(null);
  const [qty, setQty] = useState({}); // order item id -> {quantity, reason}
  const [info, setInfo] = useState({ date: today(), status: 'Completed', note: '', customerId: '', storeId: '' });
  const [lines, setLines] = useState([freeLine()]);
  const [paid, setPaid] = useState('');
  const [lookups, setLookups] = useState({ customers: [], stores: [], products: [] });
  const [error, setError] = useState(null);
  const [saving, setSaving] = useState(false);
  const idemKey = useRef(null);

  // Recent sales, or those matching the search (reference or customer).
  useEffect(() => {
    const t = setTimeout(() => {
      const q = search.trim() ? `&search=${encodeURIComponent(search.trim())}` : '';
      api
        .get(`/api/sales?page=1&limit=20${q}`, { quiet: true })
        .then((res) => setFound(Array.isArray(res) ? res : res?.data || []))
        .catch(() => setFound([]));
    }, 250);
    return () => clearTimeout(t);
  }, [search]);

  useEffect(() => {
    if (mode !== 'unlinked' || lookups.products.length) return;
    Promise.all([api.get('/api/customers'), api.get('/api/stores'), api.get('/api/products')])
      .then(([customers, stores, products]) => setLookups({ customers: customers || [], stores: stores || [], products: products || [] }))
      .catch(() => {});
  }, [mode, lookups.products.length]);

  useEffect(() => {
    idemKey.current = null;
  }, [mode, sale, qty, info, lines, paid]);

  const pickSale = async (id, keepError = false) => {
    if (!keepError) setError(null);
    try {
      const s = await api.get(`/api/sales/${id}`, { quiet: true });
      setSale(s);
      setQty({});
    } catch (err) {
      setError({ message: err.message });
    }
  };

  const left = (item) => Math.max(0, (Number(item.quantity) || 0) - (Number(item.returnedQty) || 0));
  const linkedTotal = useMemo(
    () => (sale?.items || []).reduce((sum, i) => sum + (Number(qty[i.id]?.quantity) || 0) * (Number(i.unit_price) || 0), 0),
    [sale, qty]
  );
  const freeTotal = useMemo(() => lines.reduce((s, l) => s + (Number(l.quantity) || 0) * (Number(l.unitPrice) || 0), 0), [lines]);
  const estimate = mode === 'linked' ? linkedTotal : freeTotal;

  const variantsOf = (pid) => lookups.products.find((p) => String(p.id) === String(pid))?.ProductVariants || [];
  const setLine = (key, patch) => setLines((prev) => prev.map((l) => (l.key === key ? { ...l, ...patch } : l)));

  const validate = () => {
    if (mode === 'linked') {
      if (!sale) return { message: 'Choose the sale the goods come from.', step: 0 };
      const chosen = (sale.items || []).filter((i) => Number(qty[i.id]?.quantity) > 0);
      if (!chosen.length) return { message: 'Enter how many of at least one item come back.', step: 1 };
      const over = chosen.find((i) => Number(qty[i.id].quantity) > left(i));
      if (over) return { message: `Only ${left(over)} of ${over.product_name} can still be returned.`, step: 1 };
      return null;
    }
    if (!info.customerId || !info.storeId) return { message: 'Choose the customer and the store.', step: 0 };
    if (!lines.some((l) => l.variantId && Number(l.quantity) > 0)) return { message: 'Add at least one product.', step: 1 };
    return null;
  };

  const body = () => {
    const common = { date: info.date || undefined, status: info.status, note: info.note || undefined, paid: paid === '' ? undefined : Number(paid) };
    if (mode === 'linked') {
      return {
        ...common,
        saleId: sale.id,
        customerId: sale.customer_id,
        storeId: sale.store_id,
        // Linked lines name the sale line; the server uses its price.
        returnItems: (sale.items || [])
          .filter((i) => Number(qty[i.id]?.quantity) > 0)
          .map((i) => ({ orderItemId: i.id, quantity: Number(qty[i.id].quantity), reason: qty[i.id].reason || undefined })),
      };
    }
    return {
      ...common,
      customerId: Number(info.customerId),
      storeId: Number(info.storeId),
      returnItems: lines
        .filter((l) => l.variantId && Number(l.quantity) > 0)
        .map((l) => ({ productId: Number(l.productId), variantId: Number(l.variantId), quantity: Number(l.quantity), unitPrice: Number(l.unitPrice) || 0, reason: l.reason || undefined })),
    };
  };

  const submit = async (e) => {
    e.preventDefault();
    const problem = validate();
    if (problem) {
      setError(problem);
      goToStep(problem.step);
      return;
    }
    setSaving(true);
    setError(null);
    if (!idemKey.current) idemKey.current = newIdempotencyKey();
    try {
      await api.post('/api/sales/returns', body(), { headers: { 'Idempotency-Key': idemKey.current }, quiet: true });
      navigate('/sales-return');
    } catch (err) {
      if (err.code === 'RETURN_EXCEEDS_SOLD') {
        // Someone returned items of this sale meanwhile: show the server's
        // explanation and reload what is left.
        setError({ message: `${err.message} The quantities that can still come back are updated below.` });
        if (sale) pickSale(sale.id, true);
        goToStep(1);
      } else {
        setError({ message: err.message, list: err.fields ? Object.values(err.fields) : null });
      }
    } finally {
      setSaving(false);
    }
  };

  return (
    <FormPageLayout>
      <PageHeader />
      <form className="flex min-h-0 flex-1 flex-col" onSubmit={submit} noValidate>
        <FormCard>
          <FormBody>
            <FormStepper steps={STEPS} currentStep={currentStep} onStepClick={goToStep} />

            {error && (
              <div role="alert" className="mb-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
                {error.message}
                {error.list?.length > 0 && (
                  <ul className="mt-1 list-disc pl-5">
                    {error.list.map((x) => (
                      <li key={x}>{x}</li>
                    ))}
                  </ul>
                )}
              </div>
            )}

            {currentStep === 0 && (
              <div className="space-y-4">
                {canUnlinked && (
                  <div className="flex gap-4 text-sm">
                    <label className="flex items-center gap-2">
                      <input type="radio" checked={mode === 'linked'} onChange={() => setMode('linked')} /> From a sale
                    </label>
                    <label className="flex items-center gap-2">
                      <input type="radio" checked={mode === 'unlinked'} onChange={() => setMode('unlinked')} /> Without a sale
                    </label>
                  </div>
                )}

                {mode === 'linked' ? (
                  <div className="space-y-2">
                    <input
                      className={inputCls}
                      placeholder="Search by receipt number or customer"
                      value={search}
                      onChange={(e) => setSearch(e.target.value)}
                      aria-label="Search sales"
                    />
                    <div className="max-h-72 divide-y overflow-y-auto rounded-lg border">
                      {found.filter((s) => s.status !== 'Void').map((s) => (
                        <button
                          type="button"
                          key={s.id}
                          onClick={() => pickSale(s.id)}
                          className={`flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-sm hover:bg-accent ${sale?.id === s.id ? 'bg-primary/10' : ''}`}
                        >
                          <span>
                            <span className="font-medium">{s.reference}</span> · {s.customer?.name || 'Walk-in Customer'}
                          </span>
                          <span className="whitespace-nowrap text-muted-foreground">
                            {formatDate(s.date)} · {formatCurrency(s.total)}
                          </span>
                        </button>
                      ))}
                      {found.length === 0 && <p className="px-3 py-4 text-sm text-muted-foreground">No sales found.</p>}
                    </div>
                    {sale && (
                      <p className="text-sm">
                        Selected: <strong>{sale.reference}</strong>, {sale.customer?.name || 'Walk-in Customer'}, {sale.Store?.name}
                      </p>
                    )}
                  </div>
                ) : (
                  <div className="grid gap-3 md:grid-cols-2">
                    <Field label="Customer">
                      <select className={inputCls} value={info.customerId} onChange={(e) => setInfo({ ...info, customerId: e.target.value })}>
                        <option value="">Select customer</option>
                        {lookups.customers.map((c) => (
                          <option key={c.id} value={c.id}>
                            {c.name}
                          </option>
                        ))}
                      </select>
                    </Field>
                    <Field label="Store">
                      <select className={inputCls} value={info.storeId} onChange={(e) => setInfo({ ...info, storeId: e.target.value })}>
                        <option value="">Select store</option>
                        {lookups.stores.map((s) => (
                          <option key={s.id} value={s.id}>
                            {s.name}
                          </option>
                        ))}
                      </select>
                    </Field>
                  </div>
                )}

                <div className="grid gap-3 md:grid-cols-2">
                  <Field label="Date">
                    <input type="date" className={inputCls} value={info.date} onChange={(e) => setInfo({ ...info, date: e.target.value })} />
                  </Field>
                  <Field label="Status">
                    <select className={inputCls} value={info.status} onChange={(e) => setInfo({ ...info, status: e.target.value })}>
                      <option value="Completed">Completed</option>
                      <option value="Pending">Pending</option>
                    </select>
                  </Field>
                </div>
                <Field label="Note">
                  <textarea className={`${inputCls} min-h-[70px]`} value={info.note} onChange={(e) => setInfo({ ...info, note: e.target.value })} />
                </Field>
              </div>
            )}

            {currentStep === 1 && mode === 'linked' && (
              <div>
                {!sale ? (
                  <p className="py-4 text-sm text-muted-foreground">Choose a sale first.</p>
                ) : (
                  <div className="pos-table-wrap">
                    <table className="pos-table">
                      <thead>
                        <tr>
                          <th>Item</th>
                          <th>Price paid</th>
                          <th>Sold</th>
                          <th>Returned before</th>
                          <th>Return now</th>
                          <th>Reason</th>
                        </tr>
                      </thead>
                      <tbody>
                        {(sale.items || []).map((i) => (
                          <tr key={i.id}>
                            <td className="px-2 py-2">
                              {i.product_name}
                              <div className="text-xs text-muted-foreground">{i.sku}</div>
                            </td>
                            <td className="px-2 py-2">{formatCurrency(i.unit_price)}</td>
                            <td className="px-2 py-2">{i.quantity}</td>
                            <td className="px-2 py-2">{i.returnedQty || 0}</td>
                            <td className="px-2 py-2">
                              {left(i) > 0 ? (
                                <input
                                  type="number"
                                  min="0"
                                  max={left(i)}
                                  step="1"
                                  className={inputCls}
                                  value={qty[i.id]?.quantity ?? ''}
                                  placeholder={`0–${left(i)}`}
                                  onChange={(e) => setQty({ ...qty, [i.id]: { ...qty[i.id], quantity: e.target.value } })}
                                  aria-label={`Return quantity for ${i.product_name}`}
                                />
                              ) : (
                                <span className="text-xs text-muted-foreground">All returned</span>
                              )}
                            </td>
                            <td className="px-2 py-2">
                              <input
                                className={inputCls}
                                maxLength={200}
                                value={qty[i.id]?.reason ?? ''}
                                onChange={(e) => setQty({ ...qty, [i.id]: { ...qty[i.id], reason: e.target.value } })}
                                placeholder="Optional"
                                aria-label={`Reason for ${i.product_name}`}
                              />
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                )}
              </div>
            )}

            {currentStep === 1 && mode === 'unlinked' && (
              <div>
                <div className="pos-table-wrap">
                  <table className="pos-table">
                    <thead>
                      <tr>
                        <th>Product</th>
                        <th>Variant</th>
                        <th>Quantity</th>
                        <th>Refund per unit</th>
                        <th>Reason</th>
                        <th />
                      </tr>
                    </thead>
                    <tbody>
                      {lines.map((l) => {
                        const vs = variantsOf(l.productId);
                        return (
                          <tr key={l.key}>
                            <td className="px-2 py-2">
                              <select
                                className={inputCls}
                                value={l.productId}
                                aria-label="Product"
                                onChange={(e) => {
                                  const v = variantsOf(e.target.value);
                                  setLine(l.key, { productId: e.target.value, variantId: v.length === 1 ? String(v[0].id) : '', unitPrice: v.length === 1 ? String(v[0].price) : '' });
                                }}
                              >
                                <option value="">Select product</option>
                                {lookups.products.map((p) => (
                                  <option key={p.id} value={p.id}>
                                    {p.name}
                                  </option>
                                ))}
                              </select>
                            </td>
                            <td className="px-2 py-2">
                              <select className={inputCls} value={l.variantId} disabled={vs.length <= 1} aria-label="Variant" onChange={(e) => setLine(l.key, { variantId: e.target.value })}>
                                <option value="">{vs.length ? 'Select variant' : '—'}</option>
                                {vs.map((v) => (
                                  <option key={v.id} value={v.id}>
                                    {v.sku}
                                  </option>
                                ))}
                              </select>
                            </td>
                            <td className="px-2 py-2">
                              <input type="number" min="1" className={inputCls} value={l.quantity} aria-label="Quantity" onChange={(e) => setLine(l.key, { quantity: e.target.value })} />
                            </td>
                            <td className="px-2 py-2">
                              <input type="number" min="0" step="0.01" className={inputCls} value={l.unitPrice} aria-label="Refund per unit" onChange={(e) => setLine(l.key, { unitPrice: e.target.value })} />
                            </td>
                            <td className="px-2 py-2">
                              <input className={inputCls} maxLength={200} value={l.reason} aria-label="Reason" onChange={(e) => setLine(l.key, { reason: e.target.value })} />
                            </td>
                            <td className="px-2 py-2">
                              <button type="button" className="rounded p-1 text-red-600 hover:bg-red-50" aria-label="Remove line"
                                onClick={() => setLines((prev) => (prev.length > 1 ? prev.filter((x) => x.key !== l.key) : [freeLine()]))}>
                                <XMarkIcon className="size-5" />
                              </button>
                            </td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                </div>
                <button type="button" className="mt-3 flex items-center rounded-lg bg-gray-100 p-2 text-sm hover:bg-gray-200" onClick={() => setLines((p) => [...p, freeLine()])}>
                  <PlusCircleIcon className="mr-1 size-4" /> Add item
                </button>
              </div>
            )}

            {currentStep === 2 && (
              <div className="grid gap-3 md:grid-cols-2">
                <div className="rounded-lg border p-3 text-sm md:col-span-2">
                  Items back, before tax: <strong>{formatCurrency(estimate)}</strong>
                  <p className="mt-1 text-xs text-muted-foreground">
                    The refund, including tax, is worked out when the return is saved
                    {mode === 'linked' ? ', from the prices on the original sale' : ''}.
                  </p>
                </div>
                <Field label={`Refunded now (${currencySymbol()})`} hint="Leave empty if the full refund is paid out now.">
                  <input type="number" min="0" step="0.01" className={inputCls} value={paid} onChange={(e) => setPaid(e.target.value)} />
                </Field>
              </div>
            )}
          </FormBody>

          <FormFooter
            cancelPath="/sales-return"
            submitLabel={saving ? 'Saving...' : 'Create Return'}
            disabled={saving}
            showStepNav
            currentStep={currentStep}
            totalSteps={3}
            onBack={goBack}
            onNext={goNext}
          />
        </FormCard>
      </form>
    </FormPageLayout>
  );
}

function Field({ label, hint, children }) {
  return (
    <label className="block">
      <span className="mb-1 block text-sm font-medium text-gray-700">{label}</span>
      {children}
      {hint && <span className="mt-1 block text-xs text-gray-500">{hint}</span>}
    </label>
  );
}
