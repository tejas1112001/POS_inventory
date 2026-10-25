import { useCallback, useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { api } from '../api';
import { useSession } from '../session';
import { useStepUp } from '../stepup';
import ActionDialog from '../components/ActionDialog';
import { Button, Facts, Modal, Notice, StatusBadge, Tabs } from '../components/ui';
import { bytes, formatDate, formatDateTime } from '../lib/format';

const LIVE = ['trialing', 'active', 'past_due', 'grace', 'read_only', 'cancelled'];

// TenantDetail: one tenant's facts and the console actions on it. Each
// action needs its permission; the destructive ones also need step-up
// (asked for when the server requires it).
export default function TenantDetail({ extraTabs = [] }) {
  const { id } = useParams();
  const { can } = useSession();
  const [tenant, setTenant] = useState(null);
  const [error, setError] = useState('');
  const [tab, setTab] = useState('overview');

  const load = useCallback(() => {
    api
      .get(`/tenants/${encodeURIComponent(id)}`)
      .then((t) => {
        setTenant(t);
        setError('');
      })
      .catch((err) => setError(err.status === 404 ? 'No such tenant.' : err.message));
  }, [id]);
  useEffect(load, [load]);

  if (!tenant) {
    return (
      <div className="stack">
        <Link to="/tenants">← Tenants</Link>
        {error ? <Notice kind="error">{error}</Notice> : <p className="muted">Loading…</p>}
      </div>
    );
  }

  const tabs = [{ key: 'overview', label: 'Overview' }, ...extraTabs.filter((t) => !t.perm || t.perm.some(can))];
  const Extra = extraTabs.find((t) => t.key === tab)?.Component;

  return (
    <div className="stack">
      <Link to="/tenants">← Tenants</Link>
      <div className="row-between">
        <div className="row">
          <h1>{tenant.name}</h1>
          <StatusBadge status={tenant.status} />
        </div>
        <span className="mono muted">{tenant.slug}</span>
      </div>
      <Tabs tabs={tabs} value={tab} onChange={setTab} />
      {tab === 'overview' ? <Overview tenant={tenant} reload={load} /> : Extra ? <Extra tenant={tenant} reload={load} /> : null}
    </div>
  );
}

function Overview({ tenant: t, reload }) {
  const { can } = useSession();
  const withStepUp = useStepUp();
  const [dialog, setDialog] = useState(null);
  const [invite, setInvite] = useState(null);
  const [notice, setNotice] = useState(null);
  const path = `/tenants/${encodeURIComponent(t.id)}`;
  const done = (text) => () => {
    setDialog(null);
    setNotice({ kind: 'ok', text });
    reload();
  };

  const dialogs = {
    edit: {
      title: 'Edit details',
      fields: [
        { key: 'name', label: 'Business name', required: true, initial: t.name, maxLength: 120 },
        { key: 'billingEmail', label: 'Billing email', initial: t.billingEmail || '', hint: 'Billing emails go here instead of the owner.' },
        { key: 'notes', label: 'Internal notes', type: 'textarea', initial: t.notes || '', maxLength: 2000 },
      ],
      submitLabel: 'Save',
      onSubmit: (v) => api.patch(path, { name: v.name.trim(), billingEmail: v.billingEmail.trim(), notes: v.notes }),
      onDone: done('Details saved.'),
    },
    slug: {
      title: 'Change subdomain',
      intro: 'The old address keeps redirecting for a while, and stays reserved. Staff must use the new address to sign in.',
      fields: [{ key: 'slug', label: 'New subdomain', required: true, initial: t.slug, maxLength: 30 }],
      submitLabel: 'Change subdomain',
      onSubmit: (v) => api.post(`${path}/slug`, { slug: v.slug.trim().toLowerCase() }),
      onDone: done('Subdomain changed.'),
    },
    suspend: {
      title: 'Suspend tenant',
      intro: 'Nobody can sign in while the tenant is suspended. Data is kept.',
      fields: [{ key: 'reason', label: 'Reason', type: 'textarea', required: true, minLength: 5, maxLength: 500 }],
      submitLabel: 'Suspend',
      danger: true,
      onSubmit: (v) => api.post(`${path}/suspend`, { reason: v.reason.trim() }),
      onDone: done('Tenant suspended.'),
    },
    reactivate: {
      title: 'Reactivate tenant',
      intro: 'Staff can sign in again. A billing suspension comes back if the subscription is still unpaid.',
      submitLabel: 'Reactivate',
      onSubmit: () => api.post(`${path}/reactivate`),
      onDone: done('Tenant reactivated.'),
    },
    delete: {
      title: 'Delete tenant',
      intro: 'The tenant moves to pending deletion: nobody can sign in and a final export is taken. Nothing is dropped now; it can be restored until it is deleted for good.',
      fields: [{ key: 'reason', label: 'Reason (optional)', type: 'textarea', maxLength: 500 }],
      confirmWord: t.slug,
      submitLabel: 'Delete',
      danger: true,
      onSubmit: (v) => api.del(path, v.reason.trim() ? { reason: v.reason.trim() } : undefined),
      onDone: done('Tenant moved to pending deletion.'),
    },
    restore: {
      title: 'Restore tenant',
      intro: 'The pending deletion is cancelled. The tenant comes back suspended; reactivate it when it should open again.',
      submitLabel: 'Restore',
      onSubmit: () => api.post(`${path}/restore`),
      onDone: done('Tenant restored (suspended).'),
    },
  };

  const retry = async () => {
    try {
      await api.post(`${path}/provisioning/retry`);
      setNotice({ kind: 'ok', text: 'Provisioning started again.' });
      reload();
    } catch (err) {
      setNotice({ kind: 'error', text: err.message });
    }
  };

  const ownerInvite = async () => {
    try {
      setInvite(await withStepUp(() => api.post(`${path}/owner-invite`)));
    } catch (err) {
      if (err.code !== 'CANCELLED') setNotice({ kind: 'error', text: err.message });
    }
  };

  const live = LIVE.includes(t.status);
  const actions = [
    can('tenants:edit') && ['edit', 'Edit details'],
    can('tenants:edit') && live && ['slug', 'Change subdomain'],
    can('tenants:suspend') && live && ['suspend', 'Suspend'],
    can('tenants:suspend') && t.status === 'suspended' && ['reactivate', 'Reactivate'],
    can('tenants:delete') && (live || t.status === 'suspended') && ['delete', 'Delete'],
    can('tenants:delete') && t.status === 'pending_deletion' && ['restore', 'Restore'],
  ].filter(Boolean);

  return (
    <div className="stack">
      <Notice kind={notice?.kind}>{notice?.text}</Notice>
      <div className="grid-2">
        <div className="card stack">
          <h2>Tenant</h2>
          <Facts
            items={[
              ['ID', <span key="id" className="mono">{t.id}</span>],
              ['Owner', t.ownerEmail],
              ['Billing email', t.billingEmail],
              ['Plan', t.plan],
              ['Created', formatDate(t.createdAt)],
              ['Last activity', t.lastActivityAt ? formatDateTime(t.lastActivityAt) : 'None yet'],
              ['Schema version', t.schemaVersion],
              ['Storage', bytes(t.storageBytes)],
              ['Suspended', t.suspendedAt ? `${formatDateTime(t.suspendedAt)}${t.suspendReason ? ` (${t.suspendReason})` : ''}` : null],
            ]}
          />
          {t.notes && (
            <>
              <h3>Notes</h3>
              <p className="pre-line">{t.notes}</p>
            </>
          )}
        </div>
        <div className="stack">
          {t.subscription && (
            <div className="card stack">
              <h2>Subscription</h2>
              <Facts
                items={[
                  ['Status', <StatusBadge key="s" status={t.subscription.status} />],
                  ['Trial ends', t.subscription.trialEnd && formatDate(t.subscription.trialEnd)],
                  ['Period ends', t.subscription.currentPeriodEnd && formatDate(t.subscription.currentPeriodEnd)],
                ]}
              />
            </div>
          )}
          {t.provisioning && t.provisioning.status !== 'succeeded' && (
            <div className="card stack">
              <h2>Provisioning</h2>
              <Facts
                items={[
                  ['Status', <StatusBadge key="p" status={t.provisioning.status} />],
                  ['Step', t.provisioning.step],
                  ['Attempts', t.provisioning.attempts],
                  ['Last error', t.provisioning.lastError],
                ]}
              />
              {can('tenants:create') && t.status === 'failed' && (
                <div>
                  <Button onClick={retry}>Retry provisioning</Button>
                </div>
              )}
            </div>
          )}
          <div className="card stack">
            <h2>Actions</h2>
            <div className="row">
              {actions.map(([key, label]) => (
                <Button key={key} variant={['suspend', 'delete'].includes(key) ? 'danger' : undefined} onClick={() => setDialog(key)}>
                  {label}
                </Button>
              ))}
              {can('tenants:edit') && live && <Button onClick={ownerInvite}>Owner sign-in link</Button>}
            </div>
            {actions.length === 0 && !can('tenants:edit') && <p className="muted">Your role has no actions on tenants.</p>}
          </div>
        </div>
      </div>
      {dialog && <ActionDialog {...dialogs[dialog]} onClose={() => setDialog(null)} />}
      {invite && (
        <Modal title="Owner sign-in link" onClose={() => setInvite(null)}>
          <p>Send this link to the owner privately. It works once and expires {formatDateTime(invite.expiresAt)}. It replaces earlier links.</p>
          <p className="mono small select-all">{invite.inviteUrl}</p>
          <div className="row right">
            <Button onClick={() => navigator.clipboard?.writeText(invite.inviteUrl)}>Copy</Button>
            <Button variant="primary" onClick={() => setInvite(null)}>
              Done
            </Button>
          </div>
        </Modal>
      )}
    </div>
  );
}
