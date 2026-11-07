import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { api, query } from '../api';
import { Notice, StatusBadge, Table, Tabs } from '../components/ui';
import { formatDate, formatDateTime, money } from '../lib/format';

const TABS = [
  { key: 'expiring', label: 'Expiring' },
  { key: 'past-due', label: 'Past due' },
  { key: 'invoices', label: 'Invoices' },
  { key: 'drift', label: 'Drift' },
];

const FIELD = { status: 'Provider status', plan: 'Plan', period_end: 'Period end', fetch: 'Subscription at provider' };

// Billing: trials and subscriptions ending soon, tenants that have not
// paid, recent invoices, and the hourly reconcile's differences with the
// payment provider (billing:view). A row opens the tenant.
export default function Billing() {
  const navigate = useNavigate();
  const [tab, setTab] = useState('expiring');
  const [days, setDays] = useState('7');
  const [rows, setRows] = useState(null);
  const [error, setError] = useState('');

  useEffect(() => {
    let active = true;
    setRows(null);
    setError('');
    const path = {
      expiring: `/billing/expiring${query({ days })}`,
      'past-due': '/billing/past-due',
      invoices: '/billing/invoices',
      drift: '/billing/drift',
    }[tab];
    api
      .get(path)
      .then((r) => active && setRows(r || (tab === 'drift' ? { open: [], resolved: [] } : [])))
      .catch((err) => active && setError(err.message));
    return () => {
      active = false;
    };
  }, [tab, days]);

  const open = (r) => navigate(`/tenants/${encodeURIComponent(r.tenantId)}`);
  const tenantCols = [
    { key: 'name', label: 'Tenant', render: (r) => <><strong>{r.name}</strong> <span className="mono muted small">{r.slug}</span></> },
    { key: 'tenantStatus', label: 'Tenant status', render: (r) => <StatusBadge status={r.tenantStatus} /> },
    { key: 'status', label: 'Subscription', render: (r) => <StatusBadge status={r.status} /> },
    { key: 'planCode', label: 'Plan' },
  ];

  return (
    <div className="stack">
      <h1>Billing</h1>
      <Tabs tabs={TABS} value={tab} onChange={setTab} />
      {tab === 'expiring' && (
        <label className="row">
          <span>Ending within</span>
          <select className="select narrow" value={days} onChange={(e) => setDays(e.target.value)}>
            {['3', '7', '14', '30', '60', '90'].map((d) => (
              <option key={d} value={d}>
                {d} days
              </option>
            ))}
          </select>
        </label>
      )}
      <Notice kind="error">{error}</Notice>
      {tab === 'expiring' && (rows === null || Array.isArray(rows)) && (
        <Table
          loading={!rows && !error}
          rows={rows || []}
          rowKey={(r) => r.tenantId}
          onRowClick={open}
          empty="Nothing ends in this period."
          columns={[
            ...tenantCols,
            { key: 'end', label: 'Ends', render: (r) => formatDate(r.status === 'trialing' ? r.trialEnd : r.currentPeriodEnd), className: 'nowrap' },
            { key: 'cancel', label: 'Cancelling', render: (r) => (r.cancelAtPeriodEnd || r.status === 'cancelled' ? 'Yes' : '') },
          ]}
        />
      )}
      {tab === 'past-due' && (rows === null || Array.isArray(rows)) && (
        <Table
          loading={!rows && !error}
          rows={rows || []}
          rowKey={(r) => r.tenantId}
          onRowClick={open}
          empty="No tenant is behind with payment."
          columns={[
            ...tenantCols,
            { key: 'graceEnd', label: 'Pay by', render: (r) => formatDate(r.graceEnd), className: 'nowrap' },
            { key: 'source', label: 'Suspended by', render: (r) => (r.tenantStatus === 'suspended' ? r.suspensionSource || '' : '') },
          ]}
        />
      )}
      {tab === 'drift' && rows === null && !error && <p className="muted">Loading…</p>}
      {tab === 'drift' && rows && !Array.isArray(rows) && <Drift report={rows} open={open} />}
      {tab === 'invoices' && (rows === null || Array.isArray(rows)) && (
        <Table
          loading={!rows && !error}
          rows={rows || []}
          rowKey={(r) => `${r.tenantId}-${r.providerInvoiceId}`}
          onRowClick={open}
          empty="No invoices yet."
          columns={[
            { key: 'issuedAt', label: 'Issued', render: (r) => formatDate(r.issuedAt), className: 'nowrap' },
            { key: 'tenantId', label: 'Tenant', render: (r) => <span className="mono small">{r.tenantId}</span> },
            { key: 'amount', label: 'Amount', render: (r) => money(r.amountMinor, r.currency) },
            { key: 'paid', label: 'Paid', render: (r) => money(r.amountPaidMinor, r.currency) },
            { key: 'status', label: 'Status', render: (r) => <StatusBadge status={r.status} /> },
            { key: 'paidAt', label: 'Paid on', render: (r) => formatDate(r.paidAt) },
          ]}
        />
      )}
    </div>
  );
}

// Drift lists where our subscription and the provider's differ. Report
// only: fix with the tenant's billing actions.
function Drift({ report, open }) {
  const cols = [
    { key: 'slug', label: 'Tenant', render: (r) => <span className="mono">{r.slug}</span> },
    { key: 'field', label: 'Field', render: (r) => FIELD[r.field] || r.field },
    { key: 'ours', label: 'Ours', render: (r) => <span className="mono small">{r.ours || '—'}</span> },
    { key: 'theirs', label: 'Provider', render: (r) => <span className="mono small">{r.theirs || '—'}</span> },
    { key: 'firstSeen', label: 'First seen', render: (r) => formatDateTime(r.firstSeen), className: 'nowrap' },
  ];
  return (
    <div className="stack">
      <p className="muted">
        Checked every hour against the payment provider. Nothing is fixed automatically. Last webhook received:{' '}
        {report.lastWebhookAt ? formatDateTime(report.lastWebhookAt) : 'never'} (alert after {report.quietAfterHours} hours without one while subscriptions are live).
      </p>
      <h2>Open</h2>
      <Table
        rows={report.open || []}
        rowKey={(r) => r.id}
        onRowClick={open}
        empty="No differences. Our records match the provider."
        columns={[...cols, { key: 'lastSeen', label: 'Last seen', render: (r) => formatDateTime(r.lastSeen), className: 'nowrap' }, { key: 'alertedAt', label: 'Alerted', render: (r) => (r.alertedAt ? 'Yes' : '') }]}
      />
      <h2>Resolved in the last 7 days</h2>
      <Table
        rows={report.resolved || []}
        rowKey={(r) => r.id}
        onRowClick={open}
        empty="None."
        columns={[...cols, { key: 'resolvedAt', label: 'Resolved', render: (r) => formatDateTime(r.resolvedAt), className: 'nowrap' }]}
      />
    </div>
  );
}
