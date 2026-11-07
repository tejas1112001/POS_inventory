import { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { Ban, Eye, PackageCheck, Plus, Wallet } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { usePermission } from '@/context/PermissionContext';
import { api } from '@/services/api';
import usePagedList from '@/hooks/usePagedList';
import DataTable from '@/components/kit/DataTable';
import FilterBar from '@/components/kit/FilterBar';
import PageHeader from '@/components/kit/PageHeader';
import RowActions from '@/components/kit/RowActions';
import StatusBadge from '@/components/kit/StatusBadge';
import { EmptyState } from '@/components/kit/States';
import usePurchaseActions from '@/components/purchases/usePurchaseActions';
import { formatCurrency, formatDate } from '@/lib/format';

const STATUS = ['Ordered', 'Pending', 'Received', 'Cancelled'].map((v) => ({ value: v, label: v }));
const PAYMENT = ['Paid', 'Partial', 'Unpaid', 'Overdue'].map((v) => ({ value: v, label: v }));

// Purchases (/purchases): stock bought from suppliers. Receive puts the
// stock in (once); cancel closes an order; payments record what was paid.
export default function Purchases() {
  const { hasPermission } = usePermission();
  const navigate = useNavigate();
  const list = usePagedList('/api/purchases', { filters: ['status', 'paymentStatus', 'supplierId'] });
  const [suppliers, setSuppliers] = useState([]);
  const actions = usePurchaseActions(list.reload);

  useEffect(() => {
    if (!hasPermission('suppliers', 'view')) return;
    api
      .get('/api/suppliers?limit=500', { quiet: true })
      .then((r) => setSuppliers((Array.isArray(r) ? r : r?.data || []).map((s) => ({ value: String(s.id), label: s.name }))))
      .catch(() => {});
  }, [hasPermission]);

  const columns = [
    {
      id: 'ref',
      header: 'Purchase',
      card: 'title',
      cell: (r) => (
        <Link to={`/purchases/${r.id}`} className="font-medium text-primary hover:underline">
          {r.reference}
        </Link>
      ),
    },
    { id: 'date', header: 'Date', card: 'subtitle', cell: (r) => formatDate(r.date) },
    { id: 'supplier', header: 'Supplier', card: 'meta', cell: (r) => r.supplier?.name || '—' },
    { id: 'total', header: 'Total', card: 'meta', align: 'right', cell: (r) => <span className="tabular-nums">{formatCurrency(r.total)}</span> },
    { id: 'due', header: 'Due', card: 'meta', align: 'right', className: 'hidden md:table-cell', cell: (r) => <span className="tabular-nums">{Number(r.due) ? formatCurrency(r.due) : '—'}</span> },
    { id: 'status', header: 'Status', card: 'meta', cell: (r) => <StatusBadge value={r.status} /> },
    { id: 'pay', header: 'Payment', card: 'meta', cell: (r) => <StatusBadge value={r.payment_status} /> },
  ];
  const filters = [
    { key: 'status', label: 'Status', allLabel: 'All statuses', options: STATUS },
    { key: 'paymentStatus', label: 'Payment', allLabel: 'Any payment', options: PAYMENT },
    ...(suppliers.length ? [{ key: 'supplierId', label: 'Supplier', allLabel: 'All suppliers', options: suppliers }] : []),
  ];
  const filtered = list.search || Object.values(list.values).some(Boolean);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        description="Stock bought from suppliers. Receiving a purchase adds its items to stock, once."
        actions={
          hasPermission('purchases', 'create') && (
            <Button type="button" className="max-sm:h-11" onClick={() => navigate('/add-purchase')}>
              <Plus className="size-4" aria-hidden="true" /> New purchase
            </Button>
          )
        }
      />
      <FilterBar search={list.search} onSearch={list.setSearch} placeholder="Search number or supplier…" filters={filters} values={list.values} onFilter={list.setFilter} onClear={list.clearFilters} />
      <DataTable
        label="Purchases"
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
            <EmptyState title="No purchases match" action={<Button variant="outline" onClick={list.clearFilters}>Clear filters</Button>}>
              Try another search or filter.
            </EmptyState>
          ) : (
            <EmptyState title="No purchases yet" action={hasPermission('purchases', 'create') && <Button onClick={() => navigate('/add-purchase')}>New purchase</Button>} />
          )
        }
        actions={(row) => (
          <RowActions
            label={row.reference}
            actions={[
              { label: 'Open', icon: Eye, onSelect: () => navigate(`/purchases/${row.id}`) },
              actions.canReceive(row) && { label: 'Receive into stock', icon: PackageCheck, onSelect: () => actions.receive(row) },
              actions.canPay(row) && { label: 'Record payment', icon: Wallet, onSelect: () => actions.pay(row) },
              actions.canCancel(row) && { label: 'Cancel purchase', icon: Ban, destructive: true, onSelect: () => actions.cancel(row) },
            ]}
          />
        )}
      />
      {actions.sheet}
    </div>
  );
}
