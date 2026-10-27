import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { api, query } from '../api';
import { useSession } from '../session';
import { Button, Modal, Notice, Pager, Select, StatusBadge, Table, TextInput } from '../components/ui';
import { formatDate } from '../lib/format';

const STATUSES = ['', 'trialing', 'active', 'past_due', 'grace', 'cancelled', 'read_only', 'suspended', 'pending_deletion', 'provisioning', 'failed', 'deleted'];
const SLUG = /^[a-z0-9](?:[a-z0-9-]{1,28}[a-z0-9])$/;

// Tenants lists the shops (search by name, slug or email; status filter)
// and creates new ones.
export default function Tenants() {
  const { can } = useSession();
  const navigate = useNavigate();
  const [filters, setFilters] = useState({ q: '', status: '', page: 1 });
  const [search, setSearch] = useState('');
  const [data, setData] = useState({ rows: [], total: 0, totalPages: 0, loading: true, error: '' });
  const [creating, setCreating] = useState(false);

  useEffect(() => {
    const t = setTimeout(() => setFilters((f) => (f.q === search.trim() ? f : { ...f, q: search.trim(), page: 1 })), 300);
    return () => clearTimeout(t);
  }, [search]);

  useEffect(() => {
    let active = true;
    setData((d) => ({ ...d, loading: true }));
    api
      .get(`/tenants${query({ q: filters.q, status: filters.status, page: filters.page, limit: 50 })}`)
      .then((res) => active && setData({ rows: res.data || [], total: res.pagination?.total || 0, totalPages: res.pagination?.totalPages || 0, loading: false, error: '' }))
      .catch((err) => active && setData({ rows: [], total: 0, totalPages: 0, loading: false, error: err.message }));
    return () => {
      active = false;
    };
  }, [filters]);

  return (
    <div className="stack">
      <div className="row-between">
        <h1>Tenants</h1>
        {can('tenants:create') && (
          <Button variant="primary" onClick={() => setCreating(true)}>
            New tenant
          </Button>
        )}
      </div>
      <div className="row">
        <input className="input" type="search" placeholder="Search name, subdomain or owner email" aria-label="Search tenants" value={search} onChange={(e) => setSearch(e.target.value)} />
        <select className="select" aria-label="Status" value={filters.status} onChange={(e) => setFilters((f) => ({ ...f, status: e.target.value, page: 1 }))}>
          {STATUSES.map((s) => (
            <option key={s} value={s}>
              {s ? s.replace(/_/g, ' ') : 'All statuses'}
            </option>
          ))}
        </select>
        <span className="muted small">{data.total} tenants</span>
      </div>
      <Notice kind="error">{data.error}</Notice>
      <Table
        loading={data.loading}
        rows={data.rows}
        onRowClick={(r) => navigate(`/tenants/${r.id}`)}
        empty="No tenants match."
        columns={[
          { key: 'name', label: 'Name', render: (r) => <strong>{r.name}</strong> },
          { key: 'slug', label: 'Subdomain', render: (r) => <span className="mono">{r.slug}</span> },
          { key: 'status', label: 'Status', render: (r) => <StatusBadge status={r.status} /> },
          { key: 'plan', label: 'Plan' },
          { key: 'ownerEmail', label: 'Owner' },
          { key: 'createdAt', label: 'Created', render: (r) => formatDate(r.createdAt), className: 'nowrap' },
        ]}
      />
      <Pager page={filters.page} totalPages={data.totalPages} onPage={(p) => setFilters((f) => ({ ...f, page: p }))} />
      {creating && <CreateTenant onClose={() => setCreating(false)} onCreated={(id) => navigate(`/tenants/${id}`)} />}
    </div>
  );
}

function CreateTenant({ onClose, onCreated }) {
  const { can } = useSession();
  const [form, setForm] = useState({ name: '', slug: '', ownerEmail: '', ownerName: '', planCode: '', currency: 'INR', locale: 'en-IN', timezone: 'Asia/Kolkata' });
  const [plans, setPlans] = useState([]);
  const [fields, setFields] = useState({});
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const set = (k) => (v) => setForm((f) => ({ ...f, [k]: v }));

  useEffect(() => {
    if (can('plans:manage') || can('billing:view')) api.get('/plans').then((p) => setPlans((p || []).filter((x) => x.isActive))).catch(() => {});
  }, [can]);

  const submit = async (e) => {
    e.preventDefault();
    const f = {};
    if (!form.name.trim()) f.name = 'Enter the business name.';
    if (!SLUG.test(form.slug) || form.slug.includes('--')) f.slug = '3–30 lowercase letters, digits and single hyphens.';
    if (!form.ownerEmail.includes('@')) f.ownerEmail = 'Enter the owner’s email.';
    setFields(f);
    if (Object.keys(f).length) return;
    setBusy(true);
    setError('');
    try {
      const res = await api.post('/tenants', { ...form, name: form.name.trim(), ownerEmail: form.ownerEmail.trim(), ownerName: form.ownerName.trim() });
      onCreated(res.id);
    } catch (err) {
      setFields(err.fields || {});
      setError(err.code === 'CONFLICT' ? 'That subdomain is taken or reserved.' : err.fields ? 'Please correct the marked fields.' : err.message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal title="New tenant" onClose={onClose} wide>
      <form onSubmit={submit} className="stack" noValidate>
        <div className="grid-2">
          <TextInput label="Business name" value={form.name} onChange={set('name')} error={fields.name} maxLength={120} />
          <TextInput label="Subdomain" value={form.slug} onChange={(v) => set('slug')(v.toLowerCase())} error={fields.slug} hint="The shop's address: subdomain.your-domain" maxLength={30} />
          <TextInput label="Owner email" type="email" value={form.ownerEmail} onChange={set('ownerEmail')} error={fields.ownerEmail} />
          <TextInput label="Owner name" value={form.ownerName} onChange={set('ownerName')} error={fields.ownerName} />
          <Select
            label="Plan"
            value={form.planCode}
            onChange={set('planCode')}
            error={fields.planCode}
            options={[{ value: '', label: 'Default (trial)' }, ...plans.map((p) => ({ value: p.code, label: `${p.name} (${p.code})` }))]}
          />
          <TextInput label="Currency" value={form.currency} onChange={(v) => set('currency')(v.toUpperCase())} error={fields.currency} maxLength={3} />
          <TextInput label="Number format" value={form.locale} onChange={set('locale')} error={fields.locale} hint="en-IN, en-US, …" />
          <TextInput label="Time zone" value={form.timezone} onChange={set('timezone')} error={fields.timezone} />
        </div>
        <p className="small muted">The owner gets a welcome email with a link to set a password.</p>
        <Notice kind="error">{error}</Notice>
        <div className="row right">
          <Button onClick={onClose}>Cancel</Button>
          <Button type="submit" variant="primary" busy={busy}>
            Create tenant
          </Button>
        </div>
      </form>
    </Modal>
  );
}
