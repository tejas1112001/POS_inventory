import { useEffect, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { Pencil, Plus, Trash2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { usePermission } from '@/context/PermissionContext';
import { useConfirm } from '@/components/feedback/ConfirmDialog';
import { api } from '@/services/api';
import { toast } from '../../../shared/toast/store';
import usePagedList from '@/hooks/usePagedList';
import DataTable from './DataTable';
import FilterBar from './FilterBar';
import PageHeader from './PageHeader';
import RowActions from './RowActions';
import { EmptyState } from './States';
import { FormSheet, STATUS_OPTIONS } from './Form';

// RecordsPage: a server-paged list with search, a status filter, add/edit
// in a sheet and delete with confirmation. Used by categories, brands,
// units, customers and suppliers.
//   config: { endpoint, module, noun, nounPlural, description, searchHint,
//             columns, fields(row), defaults(row), payload(values, row) }
export default function RecordsPage({ config, startNew = false }) {
  const { endpoint, module, noun, nounPlural, description, searchHint, columns, fields, defaults, payload } = config;
  const { hasPermission } = usePermission();
  const confirm = useConfirm();
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const list = usePagedList(endpoint, { filters: ['status'] });
  const [editing, setEditing] = useState(startNew ? {} : null); // null: closed, {}: new, row: edit
  const can = (a) => hasPermission(module, a);

  // /add-… addresses open the "new" sheet on the list.
  useEffect(() => {
    if (startNew) setEditing({});
  }, [startNew]);

  const close = (open) => {
    if (open) return;
    setEditing(null);
    if (startNew) navigate(pathname.replace(/^\/add-(.*)$/, (_, n) => `/${n.endsWith('y') ? `${n.slice(0, -1)}ies` : `${n}s`}`), { replace: true });
  };

  const save = async (values) => {
    const isNew = !editing?.id;
    const body = payload(values, editing);
    if (isNew) await api.post(endpoint, body, { quiet: true });
    else await api.put(`${endpoint}/${editing.id}`, body, { quiet: true });
    toast.success(isNew ? `${noun} added.` : `${noun} updated.`);
    list.reload();
  };

  const remove = async (row) => {
    const ok = await confirm({
      title: `Delete ${noun.toLowerCase()} "${row.name}"?`,
      body: 'It is hidden from lists and forms. Past documents keep its name.',
      confirmLabel: 'Delete',
      destructive: true,
    });
    if (!ok) return;
    try {
      await api.delete(`${endpoint}/${row.id}`, { quiet: true });
      toast.success(`${noun} deleted.`);
      list.reload();
    } catch (err) {
      toast.error(err.message || `The ${noun.toLowerCase()} could not be deleted.`);
    }
  };

  const filtered = list.search || list.values.status;

  return (
    <div>
      <PageHeader
        description={description}
        actions={
          can('create') && (
            <Button type="button" className="max-sm:h-11" onClick={() => setEditing({})}>
              <Plus className="size-4" aria-hidden="true" /> Add {noun.toLowerCase()}
            </Button>
          )
        }
      />
      <FilterBar
        search={list.search}
        onSearch={list.setSearch}
        placeholder={searchHint || `Search ${nounPlural.toLowerCase()}…`}
        filters={[{ key: 'status', label: 'Status', allLabel: 'All statuses', options: STATUS_OPTIONS }]}
        values={list.values}
        onFilter={list.setFilter}
        onClear={list.clearFilters}
      />
      <DataTable
        label={nounPlural}
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
            <EmptyState title={`No ${nounPlural.toLowerCase()} match`} action={<Button variant="outline" onClick={list.clearFilters}>Clear filters</Button>}>
              Try another search or status.
            </EmptyState>
          ) : (
            <EmptyState
              title={`No ${nounPlural.toLowerCase()} yet`}
              action={can('create') && <Button onClick={() => setEditing({})}>Add {noun.toLowerCase()}</Button>}
            />
          )
        }
        actions={(row) => (
          <RowActions
            label={row.name}
            actions={[
              can('edit') && { label: 'Edit', icon: Pencil, onSelect: () => setEditing(row) },
              can('delete') && { label: 'Delete', icon: Trash2, destructive: true, onSelect: () => remove(row) },
            ]}
          />
        )}
      />
      <FormSheet
        open={!!editing}
        onOpenChange={close}
        title={editing?.id ? `Edit ${noun.toLowerCase()}` : `Add ${noun.toLowerCase()}`}
        defaultValues={defaults(editing || {})}
        onSubmit={save}
        submitLabel={editing?.id ? 'Save changes' : `Add ${noun.toLowerCase()}`}
      >
        {fields(editing || {})}
      </FormSheet>
    </div>
  );
}
