import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Globe, Pencil, Plus, Power, PowerOff, Star } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { usePermission } from '@/context/PermissionContext';
import { useConfirm } from '@/components/feedback/ConfirmDialog';
import { api } from '@/services/api';
import { toast } from '../../shared/toast/store';
import usePagedList from '@/hooks/usePagedList';
import DataTable from '@/components/kit/DataTable';
import FilterBar from '@/components/kit/FilterBar';
import RowActions from '@/components/kit/RowActions';
import StatusBadge from '@/components/kit/StatusBadge';
import { EmptyState } from '@/components/kit/States';
import { FormSheet, SelectField, STATUS_OPTIONS, TextAreaField, TextField } from '@/components/kit/Form';
import { GstinField, StateField } from '@/components/forms/GstFields';
import { fmtQty } from '@/lib/qty';

const trim = (v) => (typeof v === 'string' ? v.trim() : v);
const units = (n) => `${fmtQty(n || 0)} ${Number(n) === 1 ? 'unit' : 'units'}`;

const columns = [
  {
    id: 'name',
    header: 'Store',
    card: 'title',
    cell: (r) => (
      <span className="flex flex-wrap items-center gap-2">
        <span className="font-medium">{r.name}</span>
        {r.isDefault && (
          <span className="inline-flex items-center gap-1 rounded bg-accent px-1.5 py-0.5 text-[11px] font-medium text-accent-foreground">
            <Star className="size-3" aria-hidden="true" /> Online shop default
          </span>
        )}
      </span>
    ),
  },
  { id: 'address', header: 'Address', card: 'subtitle', className: 'max-w-64 truncate', cell: (r) => r.address || '—' },
  { id: 'phone', header: 'Phone', card: 'meta', className: 'hidden md:table-cell', cell: (r) => r.phone || '—' },
  { id: 'stock', header: 'In stock', card: 'meta', align: 'right', cell: (r) => <span className="tabular-nums">{units(r.stockUnits)}</span> },
  { id: 'online', header: 'Online shop', card: 'meta', className: 'hidden lg:table-cell', cell: (r) => (r.isPublicEnabled ? 'On' : 'Off') },
  { id: 'status', header: 'Status', card: 'meta', cell: (r) => <StatusBadge value={r.status} /> },
];

// Stores (Settings → Stores): every store, active or not. Add and edit in
// a sheet; turn a store off or on (the server keeps one store active).
// The web address, logo and online-shop details stay on the Storefront page.
export default function Stores() {
  const { hasPermission } = usePermission();
  const confirm = useConfirm();
  const navigate = useNavigate();
  const list = usePagedList('/api/stores?includeInactive=true', { filters: ['status'] });
  const [editing, setEditing] = useState(null); // null: closed, {}: new, row: edit
  const can = (a) => hasPermission('stores', a);

  const save = async (values) => {
    const isNew = !editing?.id;
    // M3: the store's own GST registration ("" clears; with a GSTIN the state is its first two digits).
    const gstin = (trim(values.gstin) || '').toUpperCase();
    const body = {
      name: trim(values.name), phone: trim(values.phone), address: trim(values.address), status: values.status,
      gstin, stateCode: gstin ? gstin.slice(0, 2) : values.stateCode || '', invoiceCode: (trim(values.invoiceCode) || '').toUpperCase(),
    };
    try {
      if (isNew) await api.post('/api/stores', body, { quiet: true });
      else await api.put(`/api/stores/${editing.id}`, body, { quiet: true });
    } catch (err) {
      if (err.code === 'PLAN_LIMIT_REACHED') err.message = `${err.message} You can upgrade on Plan & billing.`;
      throw err;
    }
    toast.success(isNew ? 'Store added.' : 'Store updated.');
    list.reload();
  };

  const setStatus = async (row, status) => {
    const off = status === 'Inactive';
    const notes = [];
    if (off) {
      notes.push('The store disappears from the checkout, stock forms and pickers. Its sales and stock history stay.');
      if (Number(row.stockUnits) > 0) notes.push(`It still has ${units(row.stockUnits)} in stock; they cannot be sold until the store is active again.`);
      if (row.isDefault) notes.push('It is the store your online shop opens: the shop will open another active store instead.');
    }
    const ok = await confirm({
      title: off ? `Turn off "${row.name}"?` : `Turn on "${row.name}"?`,
      body: off ? notes.join(' ') : 'The store comes back in the checkout and pickers.',
      confirmLabel: off ? 'Turn off' : 'Turn on',
      destructive: off,
    });
    if (!ok) return;
    try {
      await api.put(`/api/stores/${row.id}`, { status }, { quiet: true });
      toast.success(off ? 'Store turned off.' : 'Store turned on.');
      list.reload();
    } catch (err) {
      toast.error(err.message || 'The store could not be changed.');
    }
  };

  const filtered = list.search || list.values.status;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <FilterBar
        actions={
          can('create') && (
            <Button type="button" className="max-sm:h-11" onClick={() => setEditing({})}>
              <Plus className="size-4" aria-hidden="true" /> Add store
            </Button>
          )
        }
        search={list.search}
        onSearch={list.setSearch}
        placeholder="Search name, address or phone…"
        filters={[{ key: 'status', label: 'Status', allLabel: 'All statuses', options: STATUS_OPTIONS }]}
        values={list.values}
        onFilter={list.setFilter}
        onClear={list.clearFilters}
      />
      <DataTable
        label="Stores"
        columns={columns}
        rows={list.rows}
        loading={list.loading}
        error={list.error}
        onRetry={list.reload}
        total={list.total}
        page={list.page}
        size={list.size}
        pageSizes={list.pageSizes}
        onPage={list.setPage}
        onSize={list.setSize}
        empty={
          filtered ? (
            <EmptyState title="No stores match" action={<Button variant="outline" onClick={list.clearFilters}>Clear filters</Button>}>
              Try another search or status.
            </EmptyState>
          ) : (
            <EmptyState title="No stores yet" />
          )
        }
        actions={(row) => (
          <RowActions
            label={row.name}
            actions={[
              can('edit') && { label: 'Edit', icon: Pencil, onSelect: () => setEditing(row) },
              can('edit') && row.status === 'Active' && { label: 'Turn off', icon: PowerOff, destructive: true, onSelect: () => setStatus(row, 'Inactive') },
              can('edit') && row.status !== 'Active' && { label: 'Turn on', icon: Power, onSelect: () => setStatus(row, 'Active') },
              can('edit') && { label: 'Online shop settings', icon: Globe, onSelect: () => navigate('/showcase-settings') },
            ]}
          />
        )}
      />
      <FormSheet
        open={!!editing}
        onOpenChange={(o) => !o && setEditing(null)}
        title={editing?.id ? 'Edit store' : 'Add store'}
        description={editing?.id ? `Web address: /${editing.slug} (change it on the Storefront page).` : 'The web address is made from the name; you can change it on the Storefront page.'}
        defaultValues={{
          name: editing?.name || '', phone: editing?.phone || '', address: editing?.address || '', status: editing?.status || 'Active',
          gstin: editing?.gstin || '', stateCode: editing?.stateCode || '', invoiceCode: editing?.invoiceCode || '',
        }}
        onSubmit={save}
        submitLabel={editing?.id ? 'Save changes' : 'Add store'}
      >
        <TextField name="name" label="Store name" required maxLength={120} autoFocus />
        <TextField name="phone" label="Phone" type="tel" maxLength={40} autoComplete="off" />
        <SelectField name="status" label="Status" options={STATUS_OPTIONS} />
        <TextAreaField name="address" label="Address" rows={2} />
        <GstinField hint="Optional. The store's own GSTIN when it is registered in another state; the shop's is used otherwise." />
        <StateField hint="Where the store is. Set from the GSTIN." />
        <TextField name="invoiceCode" label="Invoice code" maxLength={5} autoComplete="off" style={{ textTransform: 'uppercase' }}
          hint="Optional. 2 to 5 letters or digits, such as MUM, for invoice numbers per store."
          rules={{ pattern: { value: /^\s*[A-Za-z0-9]{2,5}\s*$/, message: 'Use 2 to 5 letters or digits.' } }} />
      </FormSheet>
    </div>
  );
}
