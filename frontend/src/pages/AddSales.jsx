import { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { PlusCircleIcon, XMarkIcon } from '@heroicons/react/24/outline';
import PageHeader from '../components/forms/PageHeader';
import FormFooter from '../components/forms/FormFooter';
import FormPageLayout, { FormCard, FormBody } from '../components/forms/FormPageLayout';
import FormStepper from '../components/forms/FormStepper';
import PriceChangedDialog from '../components/PriceChangedDialog';
import { useFormWizard, FORM_STEPS } from '../hooks/useFormWizard';
import { usePermission } from '../context/PermissionContext';
import { api } from '../services/api';
import { currencySymbol, formatCurrency } from '@/lib/format';
import { isRetryable, newIdempotencyKey, priceChanges } from '@/lib/checkout';

const inputCls = 'w-full rounded-lg border border-gray-300 px-3 py-2 focus:border-blue-500 focus:ring-blue-500';
const today = () => new Date().toLocaleDateString('en-CA'); // YYYY-MM-DD, read in the shop's zone by the server

let lineSeq = 0;
const newLine = () => ({ key: ++lineSeq, productId: '', variantId: '', quantity: 1, unitPrice: '' });

// AddSales records a back-office sale. All three steps are one controlled
// form, so nothing is lost when a step is not on screen. The server prices
// the sale (C-04): unit prices are the catalog prices unless the user may
// override them, and tax is worked out from each product.
export default function AddSales() {
  const navigate = useNavigate();
  const { hasPermission } = usePermission();
  const canOverride = hasPermission('sales', 'price_override');
  const { currentStep, goNext, goBack, goToStep } = useFormWizard(3);

  const [customers, setCustomers] = useState([]);
  const [stores, setStores] = useState([]);
  const [products, setProducts] = useState([]);
  const [info, setInfo] = useState({ date: today(), customerId: '', storeId: '', status: 'Completed', note: '' });
  const [lines, setLines] = useState([newLine()]);
  const [pay, setPay] = useState({ payment_method: 'Cash', discount: '', shipping: '', paid: '' });
  const [error, setError] = useState(null);
  const [saving, setSaving] = useState(false);
  const [changed, setChanged] = useState(null);
  const idemKey = useRef(null);

  useEffect(() => {
    Promise.all([api.get('/api/customers'), api.get('/api/stores'), api.get('/api/products')])
      .then(([c, s, p]) => {
        setCustomers(c || []);
        setStores(s || []);
        setProducts(p || []);
        setInfo((prev) => ({
          ...prev,
          storeId: prev.storeId || (s?.length === 1 ? String(s[0].id) : ''),
          customerId: prev.customerId || String((c || []).find((x) => x.name === 'Walk-in Customer')?.id || ''),
        }));
      })
      .catch(() => setError({ message: 'Could not load customers, stores and products.' }));
  }, []);

  // A changed form is a different request: it needs a new key.
  useEffect(() => {
    idemKey.current = null;
  }, [info, lines, pay]);

  const variantsOf = (productId) => products.find((p) => String(p.id) === String(productId))?.ProductVariants || [];
  const variantLabel = (v) => {
    const attrs = v.attributes && typeof v.attributes === 'object' ? Object.values(v.attributes).filter(Boolean).join(' / ') : '';
    return [attrs, v.sku].filter(Boolean).join(' · ') || `Variant ${v.id}`;
  };

  const setLine = (key, patch) => setLines((prev) => prev.map((l) => (l.key === key ? { ...l, ...patch } : l)));
  const pickProduct = (key, productId) => {
    const vs = variantsOf(productId);
    const v = vs.length === 1 ? vs[0] : null;
    setLine(key, { productId, variantId: v ? String(v.id) : '', unitPrice: v ? String(v.price) : '' });
  };
  const pickVariant = (key, productId, variantId) => {
    const v = variantsOf(productId).find((x) => String(x.id) === String(variantId));
    setLine(key, { variantId, unitPrice: v ? String(v.price) : '' });
  };

  const subtotal = useMemo(() => lines.reduce((sum, l) => sum + (Number(l.quantity) || 0) * (Number(l.unitPrice) || 0), 0), [lines]);

  const validate = () => {
    if (!info.storeId) return { message: 'Choose the store the goods leave from.', step: 0 };
    if (!info.customerId) return { message: 'Choose a customer.', step: 0 };
    const usable = lines.filter((l) => l.variantId);
    if (usable.length === 0) return { message: 'Add at least one product.', step: 1 };
    if (usable.some((l) => !(Number(l.quantity) >= 1))) return { message: 'Every quantity must be at least 1.', step: 1 };
    return null;
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
    const num = (v) => (v === '' || v === null ? undefined : Number(v));
    const body = {
      date: info.date || undefined,
      customerId: Number(info.customerId),
      storeId: Number(info.storeId),
      status: info.status,
      note: info.note || undefined,
      payment_method: pay.payment_method || undefined,
      discount: num(pay.discount),
      shipping: num(pay.shipping),
      paid: num(pay.paid),
      orderItems: lines
        .filter((l) => l.variantId)
        .map((l) => ({
          productId: Number(l.productId),
          variantId: Number(l.variantId),
          quantity: Number(l.quantity),
          unitPrice: Number(l.unitPrice),
        })),
    };
    try {
      await api.post('/api/sales', body, { headers: { 'Idempotency-Key': idemKey.current }, quiet: true });
      idemKey.current = null;
      navigate('/sales');
    } catch (err) {
      if (!isRetryable(err)) idemKey.current = null;
      const changes = priceChanges(err);
      if (changes) setChanged(changes);
      else setError({ message: err.message, fields: err.fields });
    } finally {
      setSaving(false);
    }
  };

  const acceptPrices = () => {
    const byVariant = new Map(changed.map((c) => [String(c.variantId), String(c.unitPrice)]));
    setLines((prev) => prev.map((l) => (byVariant.has(String(l.variantId)) ? { ...l, unitPrice: byVariant.get(String(l.variantId)) } : l)));
    setChanged(null);
  };

  return (
    <FormPageLayout>
      <PageHeader />
      <form className="flex min-h-0 flex-1 flex-col" onSubmit={submit} noValidate>
        <FormCard>
          <FormBody>
            <FormStepper steps={FORM_STEPS.sale} currentStep={currentStep} onStepClick={goToStep} />

            {error && (
              <div role="alert" className="mb-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
                {error.message}
                {error.fields && (
                  <ul className="mt-1 list-disc pl-5">
                    {Object.entries(error.fields).map(([k, v]) => (
                      <li key={k}>{v}</li>
                    ))}
                  </ul>
                )}
              </div>
            )}

            {currentStep === 0 && (
              <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
                <Field label="Store" required>
                  <select className={inputCls} value={info.storeId} onChange={(e) => setInfo({ ...info, storeId: e.target.value })}>
                    <option value="">Select store</option>
                    {stores.map((s) => (
                      <option key={s.id} value={s.id}>
                        {s.name}
                      </option>
                    ))}
                  </select>
                </Field>
                <Field label="Customer" required>
                  <select className={inputCls} value={info.customerId} onChange={(e) => setInfo({ ...info, customerId: e.target.value })}>
                    <option value="">Select customer</option>
                    {customers.map((c) => (
                      <option key={c.id} value={c.id}>
                        {c.name}
                      </option>
                    ))}
                  </select>
                </Field>
                <Field label="Date" required>
                  <input type="date" className={inputCls} value={info.date} onChange={(e) => setInfo({ ...info, date: e.target.value })} />
                </Field>
                <Field label="Status" required>
                  <select className={inputCls} value={info.status} onChange={(e) => setInfo({ ...info, status: e.target.value })}>
                    <option value="Completed">Completed</option>
                    <option value="Pending">Pending</option>
                  </select>
                </Field>
                <div className="md:col-span-2">
                  <Field label="Note">
                    <textarea
                      className={`${inputCls} min-h-[90px]`}
                      value={info.note}
                      onChange={(e) => setInfo({ ...info, note: e.target.value })}
                      placeholder="Optional note"
                    />
                  </Field>
                </div>
                <p className="text-xs text-muted-foreground md:col-span-2">The reference number is given by the system when the sale is saved.</p>
              </div>
            )}

            {currentStep === 1 && (
              <div>
                <div className="pos-table-wrap">
                  <table className="pos-table">
                    <thead>
                      <tr>
                        <th>Product</th>
                        <th>Variant</th>
                        <th>Quantity</th>
                        <th>Unit price</th>
                        <th>Amount</th>
                        <th />
                      </tr>
                    </thead>
                    <tbody>
                      {lines.map((l) => {
                        const vs = variantsOf(l.productId);
                        return (
                          <tr key={l.key}>
                            <td className="px-2 py-2">
                              <select className={inputCls} value={l.productId} onChange={(e) => pickProduct(l.key, e.target.value)} aria-label="Product">
                                <option value="">Select product</option>
                                {products.map((p) => (
                                  <option key={p.id} value={p.id}>
                                    {p.name}
                                  </option>
                                ))}
                              </select>
                            </td>
                            <td className="px-2 py-2">
                              <select
                                className={inputCls}
                                value={l.variantId}
                                onChange={(e) => pickVariant(l.key, l.productId, e.target.value)}
                                disabled={vs.length <= 1}
                                aria-label="Variant"
                              >
                                <option value="">{vs.length ? 'Select variant' : '—'}</option>
                                {vs.map((v) => (
                                  <option key={v.id} value={v.id}>
                                    {variantLabel(v)}
                                  </option>
                                ))}
                              </select>
                            </td>
                            <td className="px-2 py-2">
                              <input
                                type="number"
                                min="1"
                                step="1"
                                className={inputCls}
                                value={l.quantity}
                                onChange={(e) => setLine(l.key, { quantity: e.target.value })}
                                aria-label="Quantity"
                              />
                            </td>
                            <td className="px-2 py-2">
                              <input
                                type="number"
                                min="0"
                                step="0.01"
                                className={`${inputCls} ${canOverride ? '' : 'bg-gray-100'}`}
                                value={l.unitPrice}
                                readOnly={!canOverride}
                                title={canOverride ? '' : 'Catalog price'}
                                onChange={(e) => setLine(l.key, { unitPrice: e.target.value })}
                                aria-label="Unit price"
                              />
                            </td>
                            <td className="whitespace-nowrap px-2 py-2 text-right">
                              {formatCurrency((Number(l.quantity) || 0) * (Number(l.unitPrice) || 0))}
                            </td>
                            <td className="px-2 py-2">
                              <button
                                type="button"
                                className="rounded p-1 text-red-600 hover:bg-red-50"
                                onClick={() => setLines((prev) => (prev.length > 1 ? prev.filter((x) => x.key !== l.key) : [newLine()]))}
                                aria-label="Remove line"
                              >
                                <XMarkIcon className="size-5" />
                              </button>
                            </td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                </div>
                <div className="mt-3 flex items-center justify-between">
                  <button
                    type="button"
                    className="flex items-center rounded-lg bg-gray-100 p-2 text-sm hover:bg-gray-200"
                    onClick={() => setLines((prev) => [...prev, newLine()])}
                  >
                    <PlusCircleIcon className="mr-1 size-4" /> Add item
                  </button>
                  <div className="text-sm">
                    Subtotal before tax: <span className="font-semibold">{formatCurrency(subtotal)}</span>
                  </div>
                </div>
              </div>
            )}

            {currentStep === 2 && (
              <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
                <Field label="Payment method">
                  <select className={inputCls} value={pay.payment_method} onChange={(e) => setPay({ ...pay, payment_method: e.target.value })}>
                    <option value="Cash">Cash</option>
                    <option value="Card">Card</option>
                    <option value="Bank Transfer">Bank transfer</option>
                    <option value="UPI">UPI</option>
                    <option value="Credit">On credit</option>
                  </select>
                </Field>
                <Field label={`Paid amount (${currencySymbol()})`} hint="Leave empty if the customer paid in full.">
                  <input type="number" min="0" step="0.01" className={inputCls} value={pay.paid} onChange={(e) => setPay({ ...pay, paid: e.target.value })} />
                </Field>
                <Field label={`Discount (${currencySymbol()})`}>
                  <input type="number" min="0" step="0.01" className={inputCls} value={pay.discount} onChange={(e) => setPay({ ...pay, discount: e.target.value })} />
                </Field>
                <Field label={`Shipping (${currencySymbol()})`}>
                  <input type="number" min="0" step="0.01" className={inputCls} value={pay.shipping} onChange={(e) => setPay({ ...pay, shipping: e.target.value })} />
                </Field>
                <p className="text-xs text-muted-foreground md:col-span-2">
                  Tax and the total are worked out when the sale is saved, from each product&apos;s tax rate.
                </p>
              </div>
            )}
          </FormBody>

          <FormFooter
            cancelPath="/sales"
            submitLabel={saving ? 'Saving...' : 'Create Sale'}
            disabled={saving}
            showStepNav
            currentStep={currentStep}
            totalSteps={3}
            onBack={goBack}
            onNext={goNext}
          />
        </FormCard>
      </form>
      <PriceChangedDialog
        changes={changed}
        nameOf={(c) => products.find((p) => (p.ProductVariants || []).some((v) => v.id === c.variantId))?.name}
        onAccept={acceptPrices}
        onClose={() => setChanged(null)}
      />
    </FormPageLayout>
  );
}

function Field({ label, required, hint, children }) {
  return (
    <label className="block">
      <span className="mb-1 block text-sm font-medium text-gray-700">
        {label} {required && <span className="text-red-500">*</span>}
      </span>
      {children}
      {hint && <span className="mt-1 block text-xs text-gray-500">{hint}</span>}
    </label>
  );
}
