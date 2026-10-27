import { useCallback, useEffect, useState } from 'react';
import { api } from '../api';
import { useSession } from '../session';
import ActionDialog from '../components/ActionDialog';
import { Button, Facts, Notice, StatusBadge, Table } from '../components/ui';
import { formatDate, formatDateTime, money } from '../lib/format';

// TenantBilling: the tenant's subscription, invoices and provider events,
// and the console's subscription changes (billing:manage; cancelling also
// needs step-up). Every change needs a reason and is audited.
export default function TenantBilling({ tenant, reload }) {
  const { can } = useSession();
  const [data, setData] = useState(null);
  const [plans, setPlans] = useState([]);
  const [error, setError] = useState('');
  const [dialog, setDialog] = useState(null);
  const [notice, setNotice] = useState('');
  const base = `/tenants/${encodeURIComponent(tenant.id)}`;

  const load = useCallback(() => {
    api
      .get(`${base}/billing`)
      .then(setData)
      .catch((err) => setError(err.message));
  }, [base]);
  useEffect(load, [load]);
  useEffect(() => {
    if (can('billing:manage')) api.get('/plans').then((p) => setPlans((p || []).filter((x) => x.isActive))).catch(() => {});
  }, [can]);

  if (!data) return error ? <Notice kind="error">{error}</Notice> : <p className="muted">Loading…</p>;
  const s = data.subscription;
  const reason = { key: 'reason', label: 'Reason', type: 'textarea', required: true, minLength: 5, maxLength: 500, hint: 'Kept in the audit log.' };
  const done = (text) => () => {
    setDialog(null);
    setNotice(text);
    load();
    reload();
  };

  const dialogs = {
    plan: {
      title: 'Change plan',
      intro: 'Limits change at once. Not possible while the payment provider bills the tenant.',
      fields: [{ key: 'planCode', label: 'Plan', type: 'select', required: true, initial: s.planCode, options: plans.map((p) => ({ value: p.code, label: `${p.name} (${p.code})` })) }, reason],
      submitLabel: 'Change plan',
      onSubmit: (v) => api.post(`${base}/subscription`, { planCode: v.planCode, reason: v.reason.trim() }),
      onDone: done('Plan changed.'),
    },
    trial: {
      title: 'Extend trial',
      intro: 'Also reopens a shop that became read-only when its trial ended.',
      fields: [{ key: 'days', label: 'Extra days (1–90)', type: 'number', required: true, min: 1, max: 90, initial: '7' }, reason],
      submitLabel: 'Extend trial',
      onSubmit: (v) => api.post(`${base}/subscription/extend-trial`, { days: Number(v.days), reason: v.reason.trim() }),
      onDone: done('Trial extended.'),
    },
    grace: {
      title: 'Give more time to pay',
      intro: 'Sets a grace period; a billing suspension is lifted while it runs.',
      fields: [{ key: 'days', label: 'Days (1–60)', type: 'number', required: true, min: 1, max: 60, initial: '7' }, reason],
      submitLabel: 'Apply grace',
      onSubmit: (v) => api.post(`${base}/subscription/grace`, { days: Number(v.days), reason: v.reason.trim() }),
      onDone: done('Grace period applied.'),
    },
    cancel: {
      title: 'Cancel subscription',
      intro: 'Cancels at the payment provider first. The shop becomes read-only when the paid period ends.',
      fields: [{ key: 'atPeriodEnd', label: 'At the end of the paid period (otherwise now)', type: 'check', initial: true }, reason],
      submitLabel: 'Cancel subscription',
      danger: true,
      onSubmit: (v) => api.post(`${base}/subscription/cancel`, { atPeriodEnd: v.atPeriodEnd, reason: v.reason.trim() }),
      onDone: done('Subscription cancelled.'),
    },
  };

  return (
    <div className="stack">
      <Notice kind="ok">{notice}</Notice>
      <div className="grid-2">
        <div className="card stack">
          <h2>Subscription</h2>
          <Facts
            items={[
              ['Status', <StatusBadge key="s" status={s.status} />],
              ['Plan', s.planCode],
              ['Provider', s.provider],
              ['Provider subscription', s.providerSubscriptionId && <span key="p" className="mono small">{s.providerSubscriptionId}</span>],
              ['Provider status', s.providerStatus],
              ['Trial ends', s.trialEnd && formatDate(s.trialEnd)],
              ['Period ends', s.currentPeriodEnd && formatDate(s.currentPeriodEnd)],
              ['Pay by (grace)', s.graceEnd && formatDate(s.graceEnd)],
              ['Cancels at period end', s.cancelAtPeriodEnd ? 'Yes' : null],
              ['Last provider event', s.lastEventAt && formatDateTime(s.lastEventAt)],
            ]}
          />
        </div>
        {can('billing:manage') && (
          <div className="card stack">
            <h2>Change</h2>
            <div className="row">
              <Button onClick={() => setDialog('plan')}>Change plan</Button>
              {/* Offered only where the server accepts them. */}
              {['trialing', 'read_only'].includes(s.status) && <Button onClick={() => setDialog('trial')}>Extend trial</Button>}
              {['past_due', 'grace', 'suspended'].includes(s.status) && <Button onClick={() => setDialog('grace')}>Give more time to pay</Button>}
              {['active', 'trialing', 'past_due', 'grace'].includes(s.status) && (
                <Button variant="danger" onClick={() => setDialog('cancel')}>
                  Cancel subscription
                </Button>
              )}
            </div>
          </div>
        )}
      </div>
      <h2>Invoices</h2>
      <Table
        rows={data.invoices || []}
        rowKey={(r) => r.providerInvoiceId}
        empty="No invoices."
        columns={[
          { key: 'issuedAt', label: 'Issued', render: (r) => formatDate(r.issuedAt) },
          { key: 'amount', label: 'Amount', render: (r) => money(r.amountMinor, r.currency) },
          { key: 'paid', label: 'Paid', render: (r) => money(r.amountPaidMinor, r.currency) },
          { key: 'status', label: 'Status', render: (r) => <StatusBadge status={r.status} /> },
          { key: 'providerInvoiceId', label: 'Provider invoice', render: (r) => <span className="mono small">{r.providerInvoiceId}</span> },
        ]}
      />
      <h2>Provider events</h2>
      <Table
        rows={data.events || []}
        rowKey={(r) => r.eventId}
        empty="No events."
        columns={[
          { key: 'receivedAt', label: 'Received', render: (r) => formatDateTime(r.receivedAt), className: 'nowrap' },
          { key: 'eventType', label: 'Event', render: (r) => <span className="mono small">{r.eventType}</span> },
          { key: 'outcome', label: 'Outcome' },
          { key: 'eventId', label: 'Event ID', render: (r) => <span className="mono small">{r.eventId}</span> },
        ]}
      />
      {dialog && <ActionDialog {...dialogs[dialog]} onClose={() => setDialog(null)} />}
    </div>
  );
}
