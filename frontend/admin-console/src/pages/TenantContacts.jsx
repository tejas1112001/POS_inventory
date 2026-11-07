import { toast } from '../../../shared/toast/store';
import { useCallback, useEffect, useState } from 'react';
import { api } from '../api';
import { useSession } from '../session';
import { Button, Check, Modal, Notice, Table, TextInput } from '../components/ui';
import ActionDialog from '../components/ActionDialog';
import { formatDateTime } from '../lib/format';

const EMPTY = { name: '', role: '', email: '', phone: '', isPrimary: false };

// TenantContacts: people to reach at the shop (tenants:view to read,
// tenants:edit to change). One contact can be the primary one.
export default function TenantContacts({ tenant }) {
  const { can } = useSession();
  const base = `/tenants/${encodeURIComponent(tenant.id)}/contacts`;
  const [rows, setRows] = useState(null);
  const [error, setError] = useState('');
  const [editing, setEditing] = useState(null);
  const [removing, setRemoving] = useState(null);
  const edit = can('tenants:edit');

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

  return (
    <div className="stack">
      <div className="row-between">
        <p className="muted">People to reach at this shop. Only platform staff see them.</p>
        {edit && (
          <Button variant="primary" onClick={() => setEditing(EMPTY)}>
            Add contact
          </Button>
        )}
      </div>
      <Notice kind="error">{error}</Notice>
      <Table
        loading={rows === null}
        rows={rows || []}
        empty="No contacts yet."
        columns={[
          { key: 'name', label: 'Name', render: (c) => <strong>{c.name}{c.isPrimary ? ' (primary)' : ''}</strong> },
          { key: 'role', label: 'Role', render: (c) => c.role || '-' },
          // Plain, selectable text: tenant data never becomes a link (console rule).
          { key: 'email', label: 'Email', render: (c) => <span className="select-all">{c.email || '-'}</span> },
          { key: 'phone', label: 'Phone', render: (c) => <span className="select-all">{c.phone || '-'}</span> },
          { key: 'updatedAt', label: 'Updated', render: (c) => formatDateTime(c.updatedAt), className: 'nowrap' },
          ...(edit
            ? [
                {
                  key: 'actions',
                  label: 'Actions',
                  render: (c) => (
                    <span className="row">
                      <Button onClick={() => setEditing(c)}>Edit</Button>
                      <Button variant="danger" onClick={() => setRemoving(c)}>
                        Remove
                      </Button>
                    </span>
                  ),
                },
              ]
            : []),
        ]}
      />
      {removing && (
        <ActionDialog
          title={`Remove ${removing.name}?`}
          intro="The contact is removed from this shop. The audit log keeps the change."
          submitLabel="Remove contact"
          danger
          onSubmit={() => api.del(`${base}/${removing.id}`)}
          onClose={() => setRemoving(null)}
          onDone={() => {
            toast.success('Contact removed.');
            setRemoving(null);
            load();
          }}
        />
      )}
      {editing && (
        <ContactForm
          contact={editing}
          onClose={() => setEditing(null)}
          onSave={async (v) => {
            if (editing.id) await api.put(`${base}/${editing.id}`, v);
            else await api.post(base, v);
            toast.success(editing.id ? 'Contact updated.' : 'Contact added.');
            setEditing(null);
            load();
          }}
        />
      )}
    </div>
  );
}

function ContactForm({ contact, onClose, onSave }) {
  const [form, setForm] = useState({ ...EMPTY, ...contact });
  const [fields, setFields] = useState({});
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const set = (k) => (v) => setForm((f) => ({ ...f, [k]: v }));

  const submit = async (e) => {
    e.preventDefault();
    const f = {};
    if (!form.name.trim()) f.name = 'Enter a name.';
    if (!form.email.trim() && !form.phone.trim()) f.email = 'Give an email or a phone.';
    setFields(f);
    if (Object.keys(f).length) return;
    setBusy(true);
    setError('');
    try {
      await onSave({ name: form.name.trim(), role: form.role.trim(), email: form.email.trim(), phone: form.phone.trim(), isPrimary: form.isPrimary });
    } catch (err) {
      setFields(err.fields || {});
      setError(err.fields ? 'Please correct the marked fields.' : err.message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal title={contact.id ? 'Edit contact' : 'Add contact'} onClose={onClose}>
      <form onSubmit={submit} className="stack" noValidate>
        <Notice kind="error">{error}</Notice>
        <TextInput label="Name" value={form.name} onChange={set('name')} error={fields.name} maxLength={120} />
        <TextInput label="Role" hint="For example Owner, Accountant" value={form.role} onChange={set('role')} error={fields.role} maxLength={60} />
        <TextInput label="Email" type="email" value={form.email} onChange={set('email')} error={fields.email} maxLength={254} />
        <TextInput label="Phone" type="tel" value={form.phone} onChange={set('phone')} error={fields.phone} maxLength={40} />
        <Check label="Primary contact" checked={form.isPrimary} onChange={set('isPrimary')} />
        <div className="row right">
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="primary" type="submit" busy={busy}>
            Save
          </Button>
        </div>
      </form>
    </Modal>
  );
}
