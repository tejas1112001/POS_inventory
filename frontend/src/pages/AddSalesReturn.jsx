import { toast } from '../../shared/toast/store';
import { beep, findByCode } from '@/lib/scan';
import { useScanner } from '@/hooks/useScanner';
import { useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useNavigate } from 'react-router-dom';
import { PlusCircleIcon, XMarkIcon } from '@heroicons/react/24/outline';
import PageHeader from '../components/forms/PageHeader';
import FormFooter from '../components/forms/FormFooter';
import FormPageLayout, { FormCard, FormBody } from '../components/forms/FormPageLayout';
import FormStepper from '../components/forms/FormStepper';
import { useFormWizard } from '../hooks/useFormWizard';
import { usePermission } from '../context/PermissionContext';
import { api } from '../services/api';
import { currencySymbol, formatCurrency, formatDate, todayInShop } from '@/lib/format';
import { newIdempotencyKey } from '@/lib/checkout';
import { COLLECT_METHODS, methodLabel } from '@/lib/payments';
import { CONDITIONS, DEFAULT_POLICY } from '@/lib/returns';
import { addQty, fmtQty, roundQty } from '@/lib/qty';
import { maxInUnit, saleUnitsOf, unitOptionLabel, unitQtyText } from '@/lib/saleUnits';
import BatchSelect from '@/components/batches/BatchSelect';

// Refusals the return policy gives (industry P1 item 9); a user with
// sales_returns:override_policy can go ahead with a written reason.
const POLICY_CODES = ['RETURN_WINDOW_CLOSED', 'RETURN_NOT_ALLOWED', 'SALE_REQUIRED'];
let xseq = 0;

const STEPS = [
  { id: 'sale', title: 'Sale', description: 'What is returned' },
  { id: 'items', title: 'Items', description: 'Quantities and reasons' },
  { id: 'refund', title: 'Settle', description: 'Refund, credit or exchange' },
];
const inputCls = 'w-full rounded-lg border border-gray-300 px-3 py-2 focus:border-blue-500 focus:ring-blue-500';
const today = () => todayInShop();
let seq = 0;
const freeLine = () => ({ key: ++seq, productId: '', variantId: '', quantity: 1, unitPrice: '', reason: '', condition: '', batchId: '', unitId: '' });

// AddSalesReturn records a return (C-13). A return linked to a sale lists
// that sale's lines with what is left to return; the server refunds the
// price the customer paid and refuses more than was sold. A return
// without a sale needs sales:price_override.
//
// Industry P1 item 9: the shop's return policy decides whether the value is
// refunded, given as store credit, or used for an exchange (new goods sold
// in the same step, A20), whether each line needs a condition, and the
// time limit. A refusal can be overridden with a reason by a user allowed to.
export default function AddSalesReturn() {
  const navigate = useNavigate();
  const { hasPermission } = usePermission();
  const canUnlinked = hasPermission('sales', 'price_override');
  const canOverride = hasPermission('sales_returns', 'override_policy');
  const canSell = hasPermission('sales', 'create');
  const [policy, setPolicy] = useState(DEFAULT_POLICY);
  const [settlement, setSettlement] = useState('refund'); // refund | store_credit | exchange
  const [override, setOverride] = useState(null); // {message} when the policy refused
  const [overrideReason, setOverrideReason] = useState('');
  const [newLines, setNewLines] = useState([]); // exchange: {key, productId, variantId, name, quantity, unitPrice}
  const [pick, setPick] = useState('');
  const [quote, setQuote] = useState(null); // exchange dry run: amounts from the server
  const [xPay, setXPay] = useState({ method: 'cash', reference: '' });
  const [xRest, setXRest] = useState('refund'); // the part the new goods do not use
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
    api
      .get('/api/settings/returns', { quiet: true })
      .then((p) => setPolicy({ ...DEFAULT_POLICY, ...p }))
      .catch(() => {});
  }, []);

  useEffect(() => {
    if ((mode !== 'unlinked' && settlement !== 'exchange') || lookups.products.length) return;
    Promise.all([api.get('/api/customers'), api.get('/api/stores'), api.get('/api/products')])
      .then(([customers, stores, products]) => setLookups({ customers: customers || [], stores: stores || [], products: products || [] }))
      .catch(() => {});
  }, [mode, settlement, lookups.products.length]);

  useEffect(() => {
    idemKey.current = null;
    setQuote(null);
  }, [mode, sale, qty, info, lines, paid, settlement, newLines, xRest]);

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
  // /add-sales-return?saleId=… (from an order) opens with that sale.
  const [params] = useSearchParams();
  const preset = params.get('saleId');
  useEffect(() => {
    if (preset) pickSale(preset);
  }, [preset]);


  const left = (item) => Math.max(0, roundQty((Number(item.quantity) || 0) - (Number(item.returnedQty) || 0)));
  // H3: a line sold in another unit (a box of 12) can come back in that
  // unit (the default) or in base units. left() stays in base units.
  const soldInUnit = (i) => !!(i.unitId && i.unitName && Number(i.unitFactor) > 0);
  const inUnit = (i, q = qty) => soldInUnit(i) && (q[i.id]?.inUnit ?? true);
  const returnBase = (i, q = qty) => roundQty((Number(q[i.id]?.quantity) || 0) * (inUnit(i, q) ? Number(i.unitFactor) : 1));
  const leftIn = (i, q = qty) => (inUnit(i, q) ? maxInUnit(left(i), Number(i.unitFactor)) : left(i));
  const linkedTotal = useMemo(
    () => (sale?.items || []).reduce((sum, i) => sum + returnBase(i) * (Number(i.unit_price) || 0), 0),
    // eslint-disable-next-line react-hooks/exhaustive-deps -- returnBase reads only sale and qty
    [sale, qty]
  );
  // H3: a line without a sale, or a new exchange line, may be in one of the
  // product's sale units as they are now; prices stay per base unit.
  const unitsOfProduct = (pid) => saleUnitsOf(lookups.products.find((p) => String(p.id) === String(pid)));
  const factorOf = (pid, unitId) => (unitId ? unitsOfProduct(pid).find((u) => String(u.unitId) === String(unitId))?.factor || 1 : 1);
  const baseOf = (l) => roundQty((Number(l.quantity) || 0) * factorOf(l.productId, l.unitId));
  const unitChoice = (l, onChange, name) => {
    const us = unitsOfProduct(l.productId);
    if (!us.length) return null;
    return (
      <>
        <select className="mt-1 w-full rounded-lg border border-gray-300 px-2 py-1 text-xs" aria-label={`Unit of ${name}`} value={l.unitId || ''} onChange={(e) => onChange(e.target.value)}>
          <option value="">Single</option>
          {us.map((u) => (
            <option key={u.unitId} value={u.unitId}>{unitOptionLabel(u)}</option>
          ))}
        </select>
        {l.unitId && Number(l.quantity) > 0 && <p className="mt-1 text-xs text-muted-foreground tabular-nums">= {fmtQty(baseOf(l))}</p>}
      </>
    );
  };
  const freeTotal = useMemo(
    () => lines.reduce((s, l) => s + baseOf(l) * (Number(l.unitPrice) || 0), 0),
    // eslint-disable-next-line react-hooks/exhaustive-deps -- baseOf reads only lines and the products
    [lines, lookups.products]
  );
  const estimate = mode === 'linked' ? linkedTotal : freeTotal;

  const variantsOf = (pid) => lookups.products.find((p) => String(p.id) === String(pid))?.ProductVariants || [];
  // Industry P3 item 4: products that track batches always need a condition
  // (unsafe units go to quarantine), whatever the return policy says.
  const soldTracked = (i) => (i.batches || []).length > 0;
  const lineTracked = (l) => !!lookups.products.find((p) => String(p.id) === String(l.productId))?.tracksBatches;
  const linkedCond = policy.conditions || (sale?.items || []).some(soldTracked);
  const unlinkedCond = policy.conditions || lines.some(lineTracked);
  const setLine = (key, patch) => setLines((prev) => prev.map((l) => (l.key === key ? { ...l, ...patch } : l)));

  // Barcode scanning on the Items step (industry P1 item 3): one more of
  // the scanned item per scan. With a sale, the item must be on it (by SKU,
  // or by barcode through the server lookup); without one, any product.
  const noMatch = (code, where) => {
    beep();
    toast.error(`No ${where} with barcode ${code}`);
  };
  const onScan = async (code) => {
    if (mode === 'linked' && sale) {
      const open = (sale.items || []).filter((i) => left(i) > 0);
      let item = open.find((i) => (i.sku || '').toLowerCase() === code.toLowerCase());
      if (!item) {
        try {
          const p = await api.get(`/api/products/barcode/${encodeURIComponent(code)}`, { quiet: true });
          item = open.find((i) => p.variantId != null && String(i.variant_id) === String(p.variantId));
        } catch {
          // unknown code: handled below
        }
      }
      if (!item) return noMatch(code, 'item still returnable on this sale');
      setQty((q) => {
        const cur = Number(q[item.id]?.quantity) || 0;
        return { ...q, [item.id]: { ...q[item.id], quantity: String(Math.min(leftIn(item, q), addQty(cur, 1))) } };
      });
      return undefined;
    }
    if (mode === 'unlinked') {
      const hit = findByCode(lookups.products, code);
      if (!hit) return noMatch(code, 'product');
      setLines((prev) => {
        const same = prev.find((l) => String(l.variantId) === String(hit.variant.id));
        if (same) return prev.map((l) => (l === same ? { ...l, quantity: addQty(l.quantity, 1) } : l));
        const line = { productId: String(hit.product.id), variantId: String(hit.variant.id), quantity: 1, unitPrice: String(hit.variant.price ?? '') };
        const empty = prev.find((l) => !l.productId);
        return empty ? prev.map((l) => (l === empty ? { ...l, ...line } : l)) : [...prev, { ...freeLine(), ...line }];
      });
    }
    return undefined;
  };
  useScanner(onScan, { enabled: currentStep === 1 });

  // Exchange: the new goods. A scan on the Settle step adds one.
  const addNew = (product, variant) => {
    setNewLines((prev) => {
      const same = prev.find((l) => String(l.variantId) === String(variant.id));
      if (same) return prev.map((l) => (l === same ? { ...l, quantity: addQty(l.quantity, 1) } : l));
      const label = (product.ProductVariants || []).length > 1 ? `${product.name} (${variant.sku})` : product.name;
      return [...prev, { key: ++xseq, productId: product.id, variantId: variant.id, name: label, quantity: 1, unitPrice: variant.price }];
    });
  };
  useScanner(
    (code) => {
      const hit = findByCode(lookups.products, code);
      if (!hit) return noMatch(code, 'product');
      addNew(hit.product, hit.variant);
      return undefined;
    },
    { enabled: currentStep === 2 && settlement === 'exchange' }
  );
  const pickOptions = useMemo(
    () =>
      lookups.products.flatMap((p) =>
        (p.ProductVariants || []).map((v) => ({
          key: `${p.id}:${v.id}`,
          p,
          v,
          label: (p.ProductVariants || []).length > 1 ? `${p.name} (${v.sku})` : `${p.name}${v.sku ? ` (${v.sku})` : ''}`,
        }))
      ),
    [lookups.products]
  );
  const walkIn = mode === 'linked' ? !sale?.customer?.name || /walk-?in/i.test(sale?.customer?.name || '') : false;

  const validate = () => {
    if (mode === 'linked') {
      if (!sale) return { message: 'Choose the sale the goods come from.', step: 0 };
      const chosen = (sale.items || []).filter((i) => Number(qty[i.id]?.quantity) > 0);
      if (!chosen.length) return { message: 'Enter how many of at least one item come back.', step: 1 };
      const over = chosen.find((i) => returnBase(i) > left(i));
      if (over) return { message: `Only ${fmtQty(left(over))} of ${over.product_name} can still be returned${inUnit(over) ? ` (${fmtQty(leftIn(over))} ${over.unitName})` : ''}.`, step: 1 };
      if (chosen.some((i) => (policy.conditions || soldTracked(i)) && !qty[i.id].condition)) return { message: 'Choose the condition of each returned item.', step: 1 };
      if (settlement === 'exchange' && !newLines.some((l) => Number(l.quantity) > 0)) return { message: 'Add the new goods the customer takes.', step: 2 };
      return null;
    }
    if (!info.customerId || !info.storeId) return { message: 'Choose the customer and the store.', step: 0 };
    if (!lines.some((l) => l.variantId && Number(l.quantity) > 0)) return { message: 'Add at least one product.', step: 1 };
    if (lines.some((l) => l.variantId && Number(l.quantity) > 0 && (policy.conditions || lineTracked(l)) && !l.condition)) {
      return { message: 'Choose the condition of each returned item.', step: 1 };
    }
    return null;
  };

  const body = () => {
    const common = {
      date: info.date || undefined,
      status: info.status,
      note: info.note || undefined,
      paid: settlement === 'refund' && paid !== '' ? Number(paid) : undefined,
      settlement: settlement === 'exchange' ? undefined : settlement,
      policyOverrideReason: override && overrideReason.trim() ? overrideReason.trim() : undefined,
    };
    if (mode === 'linked') {
      return {
        ...common,
        saleId: sale.id,
        customerId: sale.customer_id,
        storeId: sale.store_id,
        // Linked lines name the sale line; the server uses its price.
        returnItems: (sale.items || [])
          .filter((i) => Number(qty[i.id]?.quantity) > 0)
          .map((i) => ({
            orderItemId: i.id,
            quantity: Number(qty[i.id].quantity),
            ...(inUnit(i) ? { unitId: i.unitId } : {}), // H3: the quantity is in the unit it was sold in
            reason: qty[i.id].reason || undefined,
            condition: qty[i.id].condition || undefined,
          })),
      };
    }
    return {
      ...common,
      customerId: Number(info.customerId),
      storeId: Number(info.storeId),
      returnItems: lines
        .filter((l) => l.variantId && Number(l.quantity) > 0)
        .map((l) => ({
          productId: Number(l.productId),
          variantId: Number(l.variantId),
          quantity: Number(l.quantity),
          unitPrice: Number(l.unitPrice) || 0, // per base unit
          ...(l.unitId ? { unitId: Number(l.unitId) } : {}),
          reason: l.reason || undefined,
          condition: l.condition || undefined,
          batchId: lineTracked(l) && l.batchId ? Number(l.batchId) : undefined,
        })),
    };
  };

  // exchangeBody is A20: the return lines, the new goods, how the difference
  // is paid, and where any rest goes.
  const exchangeBody = (q) => {
    const b = body();
    const owe = q ? Number(q.customerPays) || 0 : 0;
    return {
      saleId: sale.id,
      date: b.date,
      note: b.note,
      policyOverrideReason: b.policyOverrideReason,
      returnLines: b.returnItems,
      newLines: newLines
        .filter((l) => Number(l.quantity) > 0)
        .map((l) => ({
          productId: l.productId,
          variantId: l.variantId,
          quantity: Number(l.quantity),
          unitPrice: Number(l.unitPrice), // per base unit
          ...(l.unitId ? { unitId: Number(l.unitId) } : {}),
        })),
      payments: owe > 0 ? [{ method: xPay.method, amount: owe, ...(xPay.reference.trim() ? { reference: xPay.reference.trim() } : {}) }] : [],
      settlement: xRest,
    };
  };

  const policyRefusal = (err) => {
    if (!POLICY_CODES.includes(err.code)) return false;
    setOverride({ message: err.message });
    setError({ message: canOverride ? `${err.message} You can go ahead with a reason below.` : `${err.message} Ask the owner or an admin to override it.` });
    goToStep(2);
    return true;
  };

  const checkExchange = async () => {
    const problem = validate();
    if (problem) {
      setError(problem);
      goToStep(problem.step);
      return;
    }
    setError(null);
    try {
      setQuote(await api.post('/api/sales/exchanges?dryRun=true', exchangeBody(null), { quiet: true }));
    } catch (err) {
      if (!policyRefusal(err)) setError({ message: err.message, list: err.fields ? Object.values(err.fields) : null });
    }
  };

  const submit = async (e) => {
    e.preventDefault();
    const problem = validate();
    if (problem) {
      setError(problem);
      goToStep(problem.step);
      return;
    }
    if (override && canOverride && overrideReason.trim().length < 10) {
      setError({ message: 'Write why the policy is overridden (at least 10 characters).' });
      goToStep(2);
      return;
    }
    if (settlement === 'exchange' && !quote) {
      await checkExchange();
      return;
    }
    setSaving(true);
    setError(null);
    if (!idemKey.current) idemKey.current = newIdempotencyKey();
    try {
      if (settlement === 'exchange') {
        const out = await api.post('/api/sales/exchanges', exchangeBody(quote), { headers: { 'Idempotency-Key': idemKey.current }, quiet: true });
        toast.success(`Exchange saved: return ${out.return?.reference}, sale ${out.sale?.reference}.`);
        navigate(out.sale?.id ? `/orders/${out.sale.id}` : '/sales-return');
        return;
      }
      await api.post('/api/sales/returns', body(), { headers: { 'Idempotency-Key': idemKey.current }, quiet: true });
      toast.success(settlement === 'store_credit' ? "Return saved. The value is on the customer's store credit." : 'Return saved.');
      navigate('/sales-return');
    } catch (err) {
      if (policyRefusal(err)) {
        // explained above the form
      } else if (err.code === 'RETURN_EXCEEDS_SOLD') {
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
                          {linkedCond && <th>Condition</th>}
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
                            <td className="px-2 py-2">{unitQtyText(i)}</td>
                            <td className="px-2 py-2">{fmtQty(i.returnedQty || 0)}</td>
                            <td className="px-2 py-2">
                              {left(i) > 0 ? (
                                <>
                                  <input
                                    type="number"
                                    min="0"
                                    max={leftIn(i)}
                                    step="any"
                                    className={inputCls}
                                    value={qty[i.id]?.quantity ?? ''}
                                    placeholder={`0–${fmtQty(leftIn(i))}`}
                                    onChange={(e) => setQty({ ...qty, [i.id]: { ...qty[i.id], quantity: e.target.value } })}
                                    aria-label={`Return quantity for ${i.product_name}`}
                                  />
                                  {soldInUnit(i) && (
                                    <select
                                      className="mt-1 w-full rounded-lg border border-gray-300 px-2 py-1 text-xs"
                                      aria-label={`Unit of the return for ${i.product_name}`}
                                      value={inUnit(i) ? 'unit' : 'base'}
                                      onChange={(e) => setQty({ ...qty, [i.id]: { ...qty[i.id], inUnit: e.target.value === 'unit', quantity: '' } })}
                                    >
                                      <option value="unit">{`${i.unitName} (${fmtQty(i.unitFactor)})`}</option>
                                      <option value="base">Single</option>
                                    </select>
                                  )}
                                  {inUnit(i) && Number(qty[i.id]?.quantity) > 0 && (
                                    <p className="mt-1 text-xs text-muted-foreground tabular-nums">= {fmtQty(returnBase(i))}</p>
                                  )}
                                </>
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
                            {linkedCond && (
                              <td className="px-2 py-2">
                                {(policy.conditions || soldTracked(i)) && (
                                  <ConditionSelect
                                    value={qty[i.id]?.condition ?? ''}
                                    onChange={(v) => setQty({ ...qty, [i.id]: { ...qty[i.id], condition: v } })}
                                    label={`Condition of ${i.product_name}`}
                                    restock={policy.conditions ? policy.restockConditions : ['resellable']}
                                  />
                                )}
                                {soldTracked(i) && (
                                  <p className="mt-1 text-xs text-muted-foreground">
                                    Back to batch {i.batches.map((b) => (b.unknown ? 'No batch' : b.batchNo)).join(', ')}; expired or recalled batches go to quarantine.
                                  </p>
                                )}
                              </td>
                            )}
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
                        {unlinkedCond && <th>Condition</th>}
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
                                  setLine(l.key, { productId: e.target.value, variantId: v.length === 1 ? String(v[0].id) : '', unitPrice: v.length === 1 ? String(v[0].price) : '', unitId: '' });
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
                              <input type="number" min="0" step="any" className={inputCls} value={l.quantity} aria-label="Quantity" onChange={(e) => setLine(l.key, { quantity: e.target.value })} />
                              {unitChoice(l, (v) => setLine(l.key, { unitId: v }), 'the returned item')}
                            </td>
                            <td className="px-2 py-2">
                              <input type="number" min="0" step="0.01" className={inputCls} value={l.unitPrice} aria-label="Refund per unit" onChange={(e) => setLine(l.key, { unitPrice: e.target.value })} />
                              {l.unitId && l.unitPrice !== '' && (
                                <p className="mt-1 text-xs text-muted-foreground tabular-nums">
                                  per single unit · {formatCurrency((Number(l.unitPrice) || 0) * factorOf(l.productId, l.unitId))} per{' '}
                                  {unitsOfProduct(l.productId).find((u) => String(u.unitId) === String(l.unitId))?.name}
                                </p>
                              )}
                            </td>
                            <td className="px-2 py-2">
                              <input className={inputCls} maxLength={200} value={l.reason} aria-label="Reason" onChange={(e) => setLine(l.key, { reason: e.target.value })} />
                            </td>
                            {unlinkedCond && (
                              <td className="px-2 py-2">
                                {(policy.conditions || lineTracked(l)) && (
                                  <ConditionSelect value={l.condition} onChange={(v) => setLine(l.key, { condition: v })} label="Condition"
                                    restock={policy.conditions ? policy.restockConditions : ['resellable']} />
                                )}
                                {lineTracked(l) && l.variantId && (
                                  <BatchSelect className={`${inputCls} mt-1`} variantId={l.variantId} value={l.batchId}
                                    onChange={(v) => setLine(l.key, { batchId: v })} label="Batch" />
                                )}
                              </td>
                            )}
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
              <div className="space-y-4">
                <fieldset className="flex flex-wrap gap-4 text-sm">
                  <legend className="mb-1 w-full text-sm font-medium">Give the value back as</legend>
                  {(policy.refund || canOverride) && (
                    <label className="flex items-center gap-2">
                      <input type="radio" checked={settlement === 'refund'} onChange={() => setSettlement('refund')} /> Refund
                      {!policy.refund && ' (needs override)'}
                    </label>
                  )}
                  {(policy.storeCredit || canOverride) && !walkIn && (
                    <label className="flex items-center gap-2">
                      <input type="radio" checked={settlement === 'store_credit'} onChange={() => setSettlement('store_credit')} /> Store credit
                      {!policy.storeCredit && ' (needs override)'}
                    </label>
                  )}
                  {(policy.exchange || canOverride) && mode === 'linked' && canSell && (
                    <label className="flex items-center gap-2">
                      <input type="radio" checked={settlement === 'exchange'} onChange={() => setSettlement('exchange')} /> Exchange for other goods
                      {!policy.exchange && ' (needs override)'}
                    </label>
                  )}
                </fieldset>
                <div className="rounded-lg border p-3 text-sm">
                  Items back, before tax: <strong>{formatCurrency(estimate)}</strong>
                  <p className="mt-1 text-xs text-muted-foreground">
                    The value, including tax, is worked out when the return is saved
                    {mode === 'linked' ? ', from the prices on the original sale' : ''}. It first reduces what the customer still owes on the sale.
                  </p>
                </div>
                {settlement === 'refund' && (
                  <div className="grid gap-3 md:grid-cols-2">
                    <Field label={`Refunded now (${currencySymbol()})`} hint="Leave empty if the full refund is paid out now.">
                      <input type="number" min="0" step="0.01" className={inputCls} value={paid} onChange={(e) => setPaid(e.target.value)} />
                    </Field>
                  </div>
                )}
                {settlement === 'store_credit' && (
                  <p className="text-sm">
                    The value goes to {(mode === 'linked' && sale?.customer?.name) || 'the customer'}&apos;s store credit. They can spend it at checkout; it does not expire.
                  </p>
                )}
                {settlement === 'exchange' && (
                  <div className="space-y-3">
                    <Field label="New goods the customer takes" hint="Pick from the list or scan; each scan adds one.">
                      <select
                        className={inputCls}
                        value={pick}
                        onChange={(e) => {
                          const o = pickOptions.find((x) => x.key === e.target.value);
                          if (o) addNew(o.p, o.v);
                          setPick('');
                        }}
                        aria-label="Add a product"
                      >
                        <option value="">Add a product…</option>
                        {pickOptions.map((o) => (
                          <option key={o.key} value={o.key}>
                            {o.label}
                          </option>
                        ))}
                      </select>
                    </Field>
                    {newLines.length > 0 && (
                      <div className="pos-table-wrap">
                        <table className="pos-table">
                          <thead>
                            <tr>
                              <th>Item</th>
                              <th>Quantity</th>
                              <th>Price</th>
                              <th />
                            </tr>
                          </thead>
                          <tbody>
                            {newLines.map((l) => (
                              <tr key={l.key}>
                                <td className="px-2 py-2">{l.name}</td>
                                <td className="px-2 py-2">
                                  <input
                                    type="number"
                                    min="0"
                                    step="any"
                                    className={inputCls}
                                    value={l.quantity}
                                    aria-label={`Quantity of ${l.name}`}
                                    onChange={(e) => setNewLines((p) => p.map((x) => (x.key === l.key ? { ...x, quantity: e.target.value } : x)))}
                                  />
                                  {unitChoice(l, (v) => setNewLines((p) => p.map((x) => (x.key === l.key ? { ...x, unitId: v } : x))), l.name)}
                                </td>
                                <td className="px-2 py-2">
                                  {formatCurrency(l.unitPrice)}
                                  {l.unitId && <span className="block text-xs text-muted-foreground">each single unit</span>}
                                </td>
                                <td className="px-2 py-2">
                                  <button
                                    type="button"
                                    className="rounded p-1 text-red-600 hover:bg-red-50"
                                    aria-label={`Remove ${l.name}`}
                                    onClick={() => setNewLines((p) => p.filter((x) => x.key !== l.key))}
                                  >
                                    <XMarkIcon className="size-5" />
                                  </button>
                                </td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      </div>
                    )}
                    <Field label="If the new goods cost less, the rest is">
                      <select className={inputCls} value={xRest} onChange={(e) => setXRest(e.target.value)}>
                        {(policy.refund || canOverride) && <option value="refund">Refunded</option>}
                        {(policy.storeCredit || canOverride) && !walkIn && <option value="store_credit">Added to store credit</option>}
                      </select>
                    </Field>
                    {quote && (
                      <div className="space-y-2 rounded-lg border bg-muted/40 p-3 text-sm">
                        <p>
                          Return value <strong>{formatCurrency(quote.returnValue)}</strong>
                          {Number(quote.appliedToDue) > 0 && <> ({formatCurrency(quote.appliedToDue)} reduces what was still owed)</>}
                          {' · '}new goods <strong>{formatCurrency(quote.newTotal)}</strong>
                        </p>
                        {Number(quote.customerPays) > 0 && (
                          <div className="grid gap-3 md:grid-cols-2">
                            <p className="font-medium md:col-span-2">The customer pays {formatCurrency(quote.customerPays)}.</p>
                            <Field label="Paid by">
                              <select className={inputCls} value={xPay.method} onChange={(e) => setXPay({ ...xPay, method: e.target.value })}>
                                {COLLECT_METHODS.map((m) => (
                                  <option key={m} value={m}>
                                    {methodLabel(m)}
                                  </option>
                                ))}
                              </select>
                            </Field>
                            {xPay.method !== 'cash' && (
                              <Field label="Reference (optional)">
                                <input className={inputCls} maxLength={100} value={xPay.reference} onChange={(e) => setXPay({ ...xPay, reference: e.target.value })} />
                              </Field>
                            )}
                          </div>
                        )}
                        {Number(quote.refund) > 0 && <p className="font-medium">Refund to the customer: {formatCurrency(quote.refund)}.</p>}
                        {Number(quote.toStoreCredit) > 0 && <p className="font-medium">Added to store credit: {formatCurrency(quote.toStoreCredit)}.</p>}
                        {Number(quote.customerPays) <= 0 && Number(quote.refund) <= 0 && Number(quote.toStoreCredit) <= 0 && (
                          <p className="font-medium">Even exchange: nothing to pay.</p>
                        )}
                      </div>
                    )}
                    {!quote && (
                      <button type="button" className="rounded-lg bg-gray-100 px-3 py-2 text-sm font-medium hover:bg-gray-200" onClick={checkExchange}>
                        Work out the difference
                      </button>
                    )}
                  </div>
                )}
                {override && canOverride && (
                  <Field label="Why the policy is overridden" hint="At least 10 characters; kept in the audit log.">
                    <textarea className={`${inputCls} min-h-[70px]`} value={overrideReason} maxLength={500} onChange={(e) => setOverrideReason(e.target.value)} />
                  </Field>
                )}
              </div>
            )}
          </FormBody>

          <FormFooter
            cancelPath="/sales-return"
            submitLabel={saving ? 'Saving...' : settlement === 'exchange' ? (quote ? 'Save exchange' : 'Work out the difference') : 'Create Return'}
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

function ConditionSelect({ value, onChange, label, restock }) {
  return (
    <select className={inputCls} value={value} onChange={(e) => onChange(e.target.value)} aria-label={label}>
      <option value="">Choose…</option>
      {CONDITIONS.map((c) => (
        <option key={c.value} value={c.value}>
          {c.label}
          {restock.includes(c.value) ? ' (back to stock)' : ' (not restocked)'}
        </option>
      ))}
    </select>
  );
}
