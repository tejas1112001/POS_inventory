import { useEffect, useMemo, useState } from 'react';
import { Pencil, Plus, Trash2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { usePermission } from '@/context/PermissionContext';
import { useConfirm } from '@/components/feedback/ConfirmDialog';
import { usePos } from '@/hooks/usePos';
import { api } from '@/services/api';
import { toast } from '../../shared/toast/store';
import usePagedList from '@/hooks/usePagedList';
import DataTable from '@/components/kit/DataTable';
import FilterBar from '@/components/kit/FilterBar';
import PageHeader from '@/components/kit/PageHeader';
import RowActions from '@/components/kit/RowActions';
import StatusBadge from '@/components/kit/StatusBadge';
import { EmptyState } from '@/components/kit/States';
import { FormSheet, SelectField, STATUS_OPTIONS, TextField } from '@/components/kit/Form';
import { formatDate } from '@/lib/format';

const EMAIL = { pattern: { value: /^[^\s@]+@[^\s@]+\.[^\s@]+$/, message: 'Enter a valid email address.' } };
const PASSWORD = { validate: (v) => !v || v.length >= 12 || 'At least 12 characters, or leave it empty to send an invitation.' };

// Users (/users): the people who sign in to this shop. The server returns
// the whole list (it is short); search and pages are done here, the status
// filter by the server. Adding without a password emails an invitation.
export default function Users() {
  const { hasPermission } = usePermission();
  const { currentUser } = usePos();
  const confirm = useConfirm();
  const list = usePagedList('/api/users', { filters: ['status'] });
  const [roles, setRoles] = useState([]);
  const [editing, setEditing] = useState(null);
  const can = (a) => hasPermission('users', a);

  useEffect(() => {
    api
      .get('/api/roles', { quiet: true })
      .then((r) => setRoles((Array.isArray(r) ? r : r?.data || []).map((x) => ({ value: String(x.id), label: x.name }))))
      .catch(() => {});
  }, []);

  // The server answers with every user: search and page here.
  const all = useMemo(() => {
    const q = list.search.trim().toLowerCase();
    return q ? list.rows.filter((u) => `${u.name} ${u.email}`.toLowerCase().includes(q)) : list.rows;
  }, [list.rows, list.search]);
  const rows = all.slice((list.page - 1) * list.size, list.page * list.size);
  const roleName = (u) => u.Role?.name || roles.find((r) => r.value === String(u.role_id))?.label || u.role;

  const save = async (v) => {
    const isNew = !editing?.id;
    const body = { name: v.name.trim(), email: v.email.trim(), role_id: v.role_id ? Number(v.role_id) : undefined, status: v.status };
    if (isNew) {
      await api.post('/api/users', { ...body, ...(v.password ? { password: v.password } : {}) }, { quiet: true });
      toast.success(v.password ? 'User added. They can sign in now.' : `User added. An invitation was emailed to ${body.email}.`);
    } else {
      await api.put(`/api/users/${editing.id}`, body, { quiet: true });
      toast.success('User updated.');
    }
    list.reload();
  };

  const remove = async (u) => {
    const ok = await confirm({
      title: `Remove ${u.name}?`,
      body: 'They can no longer sign in. Their past sales keep their name.',
      confirmLabel: 'Remove user',
      destructive: true,
    });
    if (!ok) return;
    try {
      await api.delete(`/api/users/${u.id}`, { quiet: true });
      toast.success('User removed.');
      list.reload();
    } catch (err) {
      toast.error(err.message || 'The user could not be removed.');
    }
  };

  const me = (u) => String(u.id) === String(currentUser?.id);
  const columns = [
    {
      id: 'name',
      header: 'Name',
      card: 'title',
      cell: (u) => (
        <span className="flex items-center gap-2">
          <span className="font-medium">{u.name}</span>
          {me(u) && <span className="rounded bg-accent px-1.5 py-0.5 text-[11px] font-medium text-accent-foreground">You</span>}
        </span>
      ),
    },
    { id: 'email', header: 'Email', card: 'subtitle', cell: (u) => u.email },
    { id: 'role', header: 'Role', card: 'meta', cell: (u) => roleName(u) },
    { id: 'since', header: 'Added', card: 'hidden', className: 'hidden lg:table-cell', cell: (u) => formatDate(u.createdAt) },
    { id: 'status', header: 'Status', card: 'meta', cell: (u) => <StatusBadge value={u.status} /> },
  ];

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        description="People who sign in to this shop. Their role decides what they can see and do."
        actions={
          can('create') && (
            <Button type="button" className="max-sm:h-11" onClick={() => setEditing({})}>
              <Plus className="size-4" aria-hidden="true" /> Add user
            </Button>
          )
        }
      />
      <FilterBar
        search={list.search}
        onSearch={list.setSearch}
        placeholder="Search name or email…"
        filters={[{ key: 'status', label: 'Status', allLabel: 'All statuses', options: STATUS_OPTIONS }]}
        values={list.values}
        onFilter={list.setFilter}
        onClear={list.clearFilters}
      />
      <DataTable
        label="Users"
        columns={columns}
        rows={rows}
        loading={list.loading}
        error={list.error}
        onRetry={list.reload}
        total={all.length}
        page={list.page}
        size={list.size}
        pageSizes={list.pageSizes}
        onPage={list.setPage}
        onSize={list.setSize}
        empty={<EmptyState title={list.search || list.values.status ? 'No users match' : 'No users yet'} />}
        actions={(u) => (
          <RowActions
            label={u.name}
            actions={[
              can('edit') && { label: 'Edit', icon: Pencil, onSelect: () => setEditing(u) },
              can('delete') && !me(u) && { label: 'Remove', icon: Trash2, destructive: true, onSelect: () => remove(u) },
            ]}
          />
        )}
      />
      <FormSheet
        open={!!editing}
        onOpenChange={(o) => !o && setEditing(null)}
        title={editing?.id ? 'Edit user' : 'Add user'}
        description={editing?.id ? undefined : 'Leave the password empty to email an invitation; they choose their own password.'}
        defaultValues={{ name: editing?.name || '', email: editing?.email || '', password: '', role_id: editing?.role_id ? String(editing.role_id) : roles[roles.length - 1]?.value || '', status: editing?.status || 'Active' }}
        onSubmit={save}
        submitLabel={editing?.id ? 'Save changes' : 'Add user'}
      >
        <TextField name="name" label="Name" required maxLength={120} autoFocus />
        <TextField name="email" label="Email" type="email" required maxLength={254} autoComplete="off" rules={EMAIL} />
        {!editing?.id && <TextField name="password" label="Password (optional)" type="password" autoComplete="new-password" rules={PASSWORD} hint="At least 12 characters." />}
        <SelectField name="role_id" label="Role" options={roles} />
        <SelectField name="status" label="Status" options={STATUS_OPTIONS} />
      </FormSheet>
    </div>
  );
}
