import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { api, query } from '../api';
import { Notice, StatusBadge, Table, Tabs } from '../components/ui';
import { formatDate, money } from '../lib/format';

const TABS = [
  { key: 'expiring', label: 'Expiring' },
  { key: 'past-due', label: 'Past due' },
  { key: 'invoices', label: 'Invoices' },
];

// Billing: trials and subscriptions ending soon, tenants that have not
// paid, and recent invoices (billing:view). A row opens the tenant.
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
    const path = tab === 'expiring' ? `/billing/expiring${query({ days })}` : tab === 'past-due' ? '/billing/past-due' : '/billing/invoices';
    api
      .get(path)
      .then((r) => active && setRows(r || []))
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
      {tab === 'expiring' && (
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
      {tab === 'past-due' && (
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
      {tab === 'invoices' && (
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
