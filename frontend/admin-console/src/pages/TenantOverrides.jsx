import { toast } from '../../../shared/toast/store';
import { useCallback, useEffect, useState } from 'react';
import { api } from '../api';
import { useSession } from '../session';
import ActionDialog from '../components/ActionDialog';
import { Button, Notice, Table } from '../components/ui';
import { formatDateTime } from '../lib/format';

const valueText = (o) => (o.flag ? (o.value === true || String(o.value) === 'true' ? 'On' : 'Off') : String(o.value));

function state(o) {
  if (o.revokedAt) return o.revokeReason === 'replaced' ? 'Replaced' : 'Revoked';
  if (!o.active) return 'Expired';
  return o.expiringSoon ? 'Expiring soon' : 'Active';
}

// TenantOverrides: per-shop limit overrides and feature flags
// (docs/S9_SECURITY_NOTE.md). Everyone with tenants:view reads the
// history; only super_admin (tenants:override) changes it, with step-up
// MFA and a reason. Overrides end on their date; they are never edited.
export default function TenantOverrides({ tenant, reload }) {
  const { can } = useSession();
  const base = `/tenants/${encodeURIComponent(tenant.id)}/overrides`;
  const [rows, setRows] = useState(null);
  const [keys, setKeys] = useState([]);
  const [error, setError] = useState('');
  const [adding, setAdding] = useState(false);
  const [revoking, setRevoking] = useState(null);
  const edit = can('tenants:override');

  const load = useCallback(() => {
    api
      .get(base)
      .then((r) => {
        setRows(r || []);
        setError('');
      })
      .catch((err) => setError(err.message));
  }, [base]);
  useEffect(load, [load]);
  useEffect(() => {
    api
      .get('/override-keys')
      .then((k) => setKeys(k || []))
      .catch(() => {});
  }, []);

  const changed = () => {
    load();
    reload?.();
  };
  const soon = (rows || []).filter((o) => o.active && o.expiringSoon);

  const keyOptions = keys.map((k) => ({ value: k.key, label: k.flag ? `${k.label} (on/off)` : `${k.label} (limit, at most ${k.cap})` }));

  return (
    <div className="stack">
      <div className="row-between">
        <p className="muted">Changes to this shop's plan limits and features. Each ends on its date; changing one starts a new one.</p>
        {edit && (
          <Button variant="primary" onClick={() => setAdding(true)}>
            Add override
          </Button>
        )}
      </div>
      {soon.length > 0 && (
        <Notice kind="warn">
          Ending within 7 days: {soon.map((o) => `${o.label} (${formatDateTime(o.expiresAt)})`).join(', ')}. Add a new override to keep it.
        </Notice>
      )}
      <Notice kind="error">{error}</Notice>
      <Table
        loading={rows === null}
        rows={rows || []}
        empty="No overrides: this shop uses its plan as it is."
        columns={[
          { key: 'label', label: 'Setting', render: (o) => <strong>{o.label}</strong> },
          { key: 'value', label: 'Value', render: valueText },
          { key: 'state', label: 'State', render: (o) => <span className={o.expiringSoon ? 'badge badge-warn' : 'badge badge-muted'}>{state(o)}</span> },
          { key: 'expiresAt', label: 'Ends', render: (o) => formatDateTime(o.expiresAt), className: 'nowrap' },
          { key: 'reason', label: 'Reason', render: (o) => o.reason },
          { key: 'createdBy', label: 'Set by', render: (o) => `${o.createdBy} · ${formatDateTime(o.createdAt)}` },
          {
            key: 'revoked',
            label: 'Revoked',
            render: (o) => (o.revokedAt ? `${o.revokedBy || ''} · ${formatDateTime(o.revokedAt)}${o.revokeReason && o.revokeReason !== 'replaced' ? ` · ${o.revokeReason}` : ''}` : '-'),
          },
          ...(edit
            ? [{ key: 'actions', label: 'Actions', render: (o) => (o.active ? <Button variant="danger" onClick={() => setRevoking(o)}>Revoke</Button> : null) }]
            : []),
        ]}
      />
      {adding && (
        <ActionDialog
          title="Add override"
          intro="Needs a fresh MFA code. The platform inbox gets a notice and the audit log records the change."
          fields={[
            { key: 'key', label: 'Setting', type: 'select', options: keyOptions, initial: keyOptions[0]?.value, required: true },
            { key: 'value', label: 'Value', hint: 'A whole number for limits; "on" or "off" for features.', required: true },
            { key: 'days', label: 'Ends after (days)', type: 'number', initial: '30', hint: '1 to 366 days.', required: true },
            { key: 'reason', label: 'Reason', type: 'textarea', minLength: 10, required: true, hint: 'At least 10 characters. Shown in the history.' },
          ]}
          submitLabel="Add override"
          onSubmit={(v) => {
            const k = keys.find((x) => x.key === v.key);
            const raw = String(v.value).trim().toLowerCase();
            const value = k?.flag ? raw === 'on' || raw === 'true' || raw === 'yes' : Number(raw);
            if (k?.flag && !['on', 'off', 'true', 'false', 'yes', 'no'].includes(raw)) {
              return Promise.reject(Object.assign(new Error('Please correct the marked fields.'), { fields: { value: 'Type on or off.' } }));
            }
            return api.post(base, { key: v.key, value, days: Number(v.days), reason: v.reason.trim() });
          }}
          onClose={() => setAdding(false)}
          onDone={() => {
            toast.success('Override added.');
            setAdding(false);
            changed();
          }}
        />
      )}
      {revoking && (
        <ActionDialog
          title={`Revoke ${revoking.label}?`}
          intro="The shop goes back to its plan value at once. Needs a fresh MFA code."
          fields={[{ key: 'reason', label: 'Reason', type: 'textarea', minLength: 10, required: true }]}
          submitLabel="Revoke"
          danger
          onSubmit={(v) => api.post(`${base}/${revoking.id}/revoke`, { reason: v.reason.trim() })}
          onClose={() => setRevoking(null)}
          onDone={() => {
            toast.success('Override revoked.');
            setRevoking(null);
            changed();
          }}
        />
      )}
    </div>
  );
}
