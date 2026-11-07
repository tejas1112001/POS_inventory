import { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { api, query } from '../api';
import { useSession } from '../session';
import { Button, Notice, Table } from '../components/ui';
import { formatDateTime } from '../lib/format';

const PAGE = 100;

// changes shows before/after values as plain text, one "key: value" line
// each (values from tenants are never rendered as HTML).
function changes(before, after) {
  const keys = [...new Set([...Object.keys(before || {}), ...Object.keys(after || {})])];
  if (!keys.length) return '';
  const show = (v) => (v === undefined ? '–' : typeof v === 'string' ? v : JSON.stringify(v));
  return keys
    .map((k) => {
      const b = before?.[k];
      const a = after?.[k];
      if (before && after && k in before && k in after) return `${k}: ${show(b)} → ${show(a)}`;
      return `${k}: ${show(after ? a : b)}`;
    })
    .join('\n');
}

// AuditLog lists platform audit entries, newest first, 100 at a time.
// Without audit:view_all the server returns only your own entries.
export function AuditLog({ tenantId, showTenant = true }) {
  const [rows, setRows] = useState([]);
  const [loading, setLoading] = useState(true);
  const [more, setMore] = useState(false);
  const [error, setError] = useState('');

  const load = useCallback(
    (before) => {
      setLoading(true);
      api
        .get(`/audit${query({ tenant: tenantId, before, limit: PAGE })}`)
        .then((r) => {
          const list = r || [];
          setRows((prev) => (before ? [...prev, ...list] : list));
          setMore(list.length === PAGE);
          setError('');
        })
        .catch((err) => setError(err.message))
        .finally(() => setLoading(false));
    },
    [tenantId]
  );
  useEffect(() => load(), [load]);

  return (
    <div className="stack">
      <Notice kind="error">{error}</Notice>
      <Table
        loading={loading && rows.length === 0}
        rows={rows}
        empty="No entries."
        columns={[
          { key: 'at', label: 'When', render: (r) => formatDateTime(r.at), className: 'nowrap' },
          { key: 'actorEmail', label: 'Who', render: (r) => r.actorEmail || 'system' },
          { key: 'action', label: 'Action', render: (r) => <span className="mono small">{r.action}</span> },
          ...(showTenant
            ? [{ key: 'targetTenant', label: 'Tenant', render: (r) => (r.targetTenant ? <Link to={`/tenants/${encodeURIComponent(r.targetTenant)}`} className="mono small">{r.targetTenant}</Link> : '') }]
            : []),
          { key: 'changes', label: 'Details', render: (r) => <pre className="json">{[r.target && `target: ${r.target}`, changes(r.before, r.after)].filter(Boolean).join('\n')}</pre> },
          { key: 'ip', label: 'IP', render: (r) => <span className="mono small">{r.ip || ''}</span> },
        ]}
      />
      {more && (
        <div>
          <Button onClick={() => load(rows[rows.length - 1]?.id)} busy={loading}>
            Load older entries
          </Button>
        </div>
      )}
    </div>
  );
}

export default function Audit() {
  const { can } = useSession();
  const [tenant, setTenant] = useState('');
  const [applied, setApplied] = useState('');
  return (
    <div className="stack">
      <h1>Audit log</h1>
      {!can('audit:view_all') && <Notice kind="info">Your role shows only your own actions.</Notice>}
      <form
        className="row"
        onSubmit={(e) => {
          e.preventDefault();
          setApplied(tenant.trim());
        }}
      >
        <input className="input narrow-input" placeholder="Tenant ID (optional)" aria-label="Tenant ID" value={tenant} onChange={(e) => setTenant(e.target.value)} />
        <Button type="submit">Filter</Button>
      </form>
      <AuditLog key={applied} tenantId={applied || undefined} />
    </div>
  );
}

export function TenantAudit({ tenant }) {
  return <AuditLog tenantId={tenant.id} showTenant={false} />;
}
