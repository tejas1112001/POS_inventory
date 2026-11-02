import { toast } from '../../../shared/toast/store';
import { useCallback, useEffect, useState } from 'react';
import { api } from '../api';
import { useSession } from '../session';
import ActionDialog from '../components/ActionDialog';
import { Button, Modal, Notice, StatusBadge, Table } from '../components/ui';
import { formatDateTime } from '../lib/format';

const ROLES = [
  { value: 'super_admin', label: 'Super admin' },
  { value: 'billing_admin', label: 'Billing admin' },
  { value: 'support', label: 'Support' },
];
const roleLabel = (r) => ROLES.find((x) => x.value === r)?.label || r;
const REASON = { key: 'reason', label: 'Reason', type: 'textarea', required: true, minLength: 10, hint: 'At least 10 characters. Kept in the audit log and sent to the user.' };

// PlatformUsers (S10 item 1): console accounts. Added by invite link (7
// days; emailed, and shown once here to copy); MFA is set up at the first
// sign-in. Every change asks for a fresh MFA code and a reason, is audited
// and emailed to the user and the platform inbox.
export default function PlatformUsers() {
  const { user } = useSession();
  const [rows, setRows] = useState(null);
  const [error, setError] = useState('');
  const [dialog, setDialog] = useState(null); // { kind, user? }
  const [invite, setInvite] = useState(null); // { email, url, expiresAt }

  const load = useCallback(() => {
    api
      .get('/platform-users')
      .then((r) => {
        setRows(r || []);
        setError('');
      })
      .catch((err) => setError(err.message));
  }, []);
  useEffect(load, [load]);

  const done = (msg) => () => {
    toast.success(msg);
    setDialog(null);
    load();
  };
  const showInvite = (res) => {
    setDialog(null);
    setInvite({ email: res.user.email, url: res.inviteUrl, expiresAt: res.expiresAt });
    load();
  };

  const dialogs = {
    add: {
      title: 'Add a platform user',
      intro: 'They get an invite email with a link valid for 7 days. You also see the link once, to copy. They set up MFA at their first sign-in.',
      fields: [
        { key: 'name', label: 'Name', required: true, maxLength: 120 },
        { key: 'email', label: 'Email', required: true, maxLength: 254 },
        { key: 'role', label: 'Role', type: 'select', options: ROLES, initial: 'support', required: true },
        REASON,
      ],
      submitLabel: 'Send invite',
      onSubmit: (v) => api.post('/platform-users', { name: v.name.trim(), email: v.email.trim(), role: v.role, reason: v.reason.trim() }),
      onDone: showInvite,
    },
    reinvite: (u) => ({
      title: `New invite for ${u.email}`,
      intro: 'Earlier links stop working. A new email is sent and you see the new link once.',
      fields: [REASON],
      submitLabel: 'Send new invite',
      onSubmit: (v) => api.post(`/platform-users/${u.id}/invite`, { reason: v.reason.trim() }),
      onDone: showInvite,
    }),
    role: (u) => ({
      title: `Change the role of ${u.email}`,
      intro: 'They are signed out and see their new menu at the next sign-in.',
      fields: [{ key: 'role', label: 'New role', type: 'select', options: ROLES.filter((r) => r.value !== u.role), initial: ROLES.find((r) => r.value !== u.role).value, required: true }, REASON],
      submitLabel: 'Change role',
      onSubmit: (v) => api.post(`/platform-users/${u.id}/role`, { role: v.role, reason: v.reason.trim() }),
      onDone: done('Role changed.'),
    }),
    deactivate: (u) => ({
      title: `Deactivate ${u.email}?`,
      intro: 'They are signed out everywhere and cannot sign in until reactivated. Open invites stop working.',
      fields: [REASON],
      submitLabel: 'Deactivate',
      danger: true,
      onSubmit: (v) => api.post(`/platform-users/${u.id}/deactivate`, { reason: v.reason.trim() }),
      onDone: done('User deactivated.'),
    }),
    reactivate: (u) => ({
      title: `Reactivate ${u.email}?`,
      fields: [REASON],
      submitLabel: 'Reactivate',
      onSubmit: (v) => api.post(`/platform-users/${u.id}/reactivate`, { reason: v.reason.trim() }),
      onDone: done('User reactivated.'),
    }),
    mfa: (u) => ({
      title: `Reset MFA for ${u.email}?`,
      intro: 'Their authenticator and recovery codes stop working and they are signed out. At the next sign-in they set up a new authenticator. Only do this after checking who is asking.',
      fields: [REASON],
      submitLabel: 'Reset MFA',
      danger: true,
      onSubmit: (v) => api.post(`/platform-users/${u.id}/reset-mfa`, { reason: v.reason.trim() }),
      onDone: done('MFA reset. The user enrols again at the next sign-in.'),
    }),
  };

  const actions = (u) => {
    if (u.id === user?.id) return <span className="muted small">You</span>;
    const list = [];
    if (u.status === 'Active' && !u.mfaEnrolled) list.push(['reinvite', 'New invite']);
    list.push(['role', 'Role']);
    if (u.status === 'Active') list.push(['deactivate', 'Deactivate']);
    else list.push(['reactivate', 'Reactivate']);
    if (u.mfaEnrolled) list.push(['mfa', 'Reset MFA']);
    return (
      <span className="row">
        {list.map(([k, label]) => (
          <Button key={k} variant={k === 'deactivate' || k === 'mfa' ? 'danger' : undefined} onClick={() => setDialog({ kind: k, user: u })}>
            {label}
          </Button>
        ))}
      </span>
    );
  };

  const current = dialog && (dialog.kind === 'add' ? dialogs.add : dialogs[dialog.kind](dialog.user));

  return (
    <div className="stack">
      <div className="row-between">
        <h1>Platform users</h1>
        <Button variant="primary" onClick={() => setDialog({ kind: 'add' })}>
          Add user
        </Button>
      </div>
      <Notice kind="error">{error}</Notice>
      <Table
        loading={rows === null}
        rows={rows || []}
        empty="No platform users."
        columns={[
          { key: 'email', label: 'Email', render: (u) => <strong>{u.email}</strong> },
          { key: 'name', label: 'Name' },
          { key: 'role', label: 'Role', render: (u) => roleLabel(u.role) },
          { key: 'status', label: 'Status', render: (u) => <StatusBadge status={u.status === 'Active' ? 'active' : 'suspended'} /> },
          {
            key: 'mfa',
            label: 'Sign-in',
            render: (u) =>
              u.invitePending ? `Invite sent (until ${formatDateTime(u.inviteExpiresAt)})` : u.mfaEnrolled ? 'MFA set up' : 'MFA not set up',
          },
          { key: 'lastLoginAt', label: 'Last sign-in', render: (u) => formatDateTime(u.lastLoginAt, 'Never'), className: 'nowrap' },
          { key: 'actions', label: 'Actions', render: actions },
        ]}
      />
      {current && <ActionDialog {...current} onClose={() => setDialog(null)} />}
      {invite && <InviteLink invite={invite} onClose={() => setInvite(null)} />}
    </div>
  );
}

// InviteLink shows a new invite link once, to copy.
function InviteLink({ invite, onClose }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(invite.url);
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };
  return (
    <Modal title="Invite sent" onClose={onClose}>
      <div className="stack">
        <p>
          An email is on its way to <strong>{invite.email}</strong>. If it is slow, give them this link yourself. It works once, until{' '}
          {formatDateTime(invite.expiresAt)}, and is <strong>not shown again</strong>.
        </p>
        <p className="mono small select-all">{invite.url}</p>
        <div className="row right">
          <Button onClick={copy}>{copied ? 'Copied' : 'Copy link'}</Button>
          <Button variant="primary" onClick={onClose}>
            Done
          </Button>
        </div>
      </div>
    </Modal>
  );
}
