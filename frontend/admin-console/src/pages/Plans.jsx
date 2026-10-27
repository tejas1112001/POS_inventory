import { useCallback, useEffect, useState } from 'react';
import { api } from '../api';
import { useSession } from '../session';
import { Button, Check, Modal, Notice, Select, Table, TextInput } from '../components/ui';
import { money } from '../lib/format';
import { FEATURES, LIMITS } from '../lib/plans';

// Plans lists the subscription plans; plans:manage creates and edits them
// (BILLING_DESIGN §2, §7). A plan linked to the payment provider keeps its
// price and interval: create a new plan to change them.
export default function Plans() {
  const { can } = useSession();
  const [plans, setPlans] = useState(null);
  const [error, setError] = useState('');
  const [editing, setEditing] = useState(null); // plan, or {} for a new one

  const load = useCallback(() => {
    api
      .get('/plans')
      .then((p) => setPlans(p || []))
      .catch((err) => setError(err.message));
  }, []);
  useEffect(load, [load]);

  return (
    <div className="stack">
      <div className="row-between">
        <h1>Plans</h1>
        {can('plans:manage') && (
          <Button variant="primary" onClick={() => setEditing({})}>
            New plan
          </Button>
        )}
      </div>
      <Notice kind="error">{error}</Notice>
      <Table
        loading={!plans && !error}
        rows={plans || []}
        onRowClick={can('plans:manage') ? setEditing : undefined}
        columns={[
          { key: 'name', label: 'Plan', render: (p) => <><strong>{p.name}</strong> <span className="mono muted small">{p.code}</span></> },
          { key: 'price', label: 'Price', render: (p) => `${money(p.priceMinor, p.currency)} / ${p.interval === 'yearly' ? 'year' : 'month'}`, className: 'nowrap' },
          { key: 'trialDays', label: 'Trial days' },
          { key: 'provider', label: 'Provider plan', render: (p) => <span className="mono small">{p.razorpayPlanId || '-'}</span> },
          { key: 'limits', label: 'Limits', render: (p) => LIMITS.map(([k, l]) => `${l}: ${p.limits?.[k] || '∞'}`).join(' · '), className: 'small' },
          { key: 'flags', label: 'Shown', render: (p) => [p.isActive ? 'active' : 'inactive', p.isPublic ? 'public' : 'hidden'].join(', ') },
          { key: 'tenants', label: 'Tenants', className: 'right' },
        ]}
      />
      {editing && (
        <PlanForm
          plan={editing}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null);
            load();
          }}
        />
      )}
    </div>
  );
}

function PlanForm({ plan, onClose, onSaved }) {
  const isNew = !plan.id;
  const linked = !!plan.razorpayPlanId;
  const [v, setV] = useState(() => ({
    code: plan.code || '',
    name: plan.name || '',
    price: plan.priceMinor != null ? String(plan.priceMinor / 100) : '',
    interval: plan.interval || 'monthly',
    razorpayPlanId: plan.razorpayPlanId || '',
    trialDays: plan.trialDays != null ? String(plan.trialDays) : '14',
    limits: Object.fromEntries(LIMITS.map(([k]) => [k, String(plan.limits?.[k] ?? 0)])),
    features: Object.fromEntries(FEATURES.map(([k]) => [k, !!plan.features?.[k]])),
    isPublic: plan.isPublic ?? false,
    isActive: plan.isActive ?? true,
  }));
  const [fields, setFields] = useState({});
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const set = (k) => (x) => setV((s) => ({ ...s, [k]: x }));

  const submit = async (e) => {
    e.preventDefault();
    setBusy(true);
    setError('');
    setFields({});
    const body = {
      name: v.name.trim(),
      trialDays: Number(v.trialDays),
      limits: Object.fromEntries(Object.entries(v.limits).map(([k, x]) => [k, Number(x) || 0])),
      features: v.features,
      isPublic: v.isPublic,
      isActive: v.isActive,
    };
    if (isNew || !linked) {
      body.priceMinor = Math.round(Number(v.price) * 100);
      body.interval = v.interval;
    }
    if (isNew) {
      body.code = v.code.trim();
      body.currency = 'INR';
    }
    if (!linked && v.razorpayPlanId.trim()) body.razorpayPlanId = v.razorpayPlanId.trim();
    try {
      if (isNew) await api.post('/plans', body);
      else await api.patch(`/plans/${plan.id}`, body);
      onSaved();
    } catch (err) {
      setFields(err.fields || {});
      setError(err.fields ? 'Please correct the marked fields.' : err.message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal title={isNew ? 'New plan' : `Edit plan ${plan.code}`} onClose={onClose} wide>
      <form onSubmit={submit} className="stack" noValidate>
        <div className="grid-2">
          <TextInput label="Code" value={v.code} onChange={set('code')} error={fields.code} disabled={!isNew} hint="Lowercase letters, digits and _. Cannot change later." maxLength={40} />
          <TextInput label="Name" value={v.name} onChange={set('name')} error={fields.name} maxLength={120} />
          <TextInput label="Price (₹)" type="number" min="0" step="0.01" value={v.price} onChange={set('price')} error={fields.priceMinor} disabled={linked} />
          <Select label="Billed" value={v.interval} onChange={set('interval')} error={fields.interval} disabled={linked} options={[{ value: 'monthly', label: 'Monthly' }, { value: 'yearly', label: 'Yearly' }]} />
          <TextInput label="Trial days" type="number" min="0" max="365" value={v.trialDays} onChange={set('trialDays')} error={fields.trialDays} />
          <TextInput label="Provider plan ID" value={v.razorpayPlanId} onChange={set('razorpayPlanId')} error={fields.razorpayPlanId} disabled={linked} hint={linked ? 'Linked to the provider: price and billing period are fixed.' : 'plan_… from Razorpay; set once.'} />
        </div>
        <h3>Limits (0 = no limit)</h3>
        <div className="grid-2">
          {LIMITS.map(([k, label]) => (
            <TextInput key={k} label={label} type="number" min="0" value={v.limits[k]} onChange={(x) => setV((s) => ({ ...s, limits: { ...s.limits, [k]: x } }))} />
          ))}
        </div>
        {fields.limits && <p className="error-text">{fields.limits}</p>}
        <h3>Features</h3>
        <div className="row">
          {FEATURES.map(([k, label]) => (
            <Check key={k} label={label} checked={v.features[k]} onChange={(x) => setV((s) => ({ ...s, features: { ...s.features, [k]: x } }))} />
          ))}
        </div>
        {fields.features && <p className="error-text">{fields.features}</p>}
        <div className="row">
          <Check label="Active (can be assigned)" checked={v.isActive} onChange={set('isActive')} />
          <Check label="Public (shops can buy it)" checked={v.isPublic} onChange={set('isPublic')} />
        </div>
        <Notice kind="error">{error}</Notice>
        <div className="row right">
          <Button onClick={onClose}>Cancel</Button>
          <Button type="submit" variant="primary" busy={busy}>
            {isNew ? 'Create plan' : 'Save plan'}
          </Button>
        </div>
      </form>
    </Modal>
  );
}
