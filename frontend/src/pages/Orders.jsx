import { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { Ban, Eye, FileText, Plus, Undo2 } from 'lucide-react';
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
import DateBox from '@/components/kit/DateBox';
import Receipt from '@/components/Receipt';
import VoidSaleDialog from '@/components/orders/VoidSaleDialog';
import { formatCurrency, formatDateTime } from '@/lib/format';

const STATUS = [
  { value: 'Completed', label: 'Completed' },
  { value: 'Pending', label: 'Pending' },
  { value: 'Void', label: 'Voided' },
];
const PAYMENT = ['Paid', 'Partial', 'Unpaid', 'Overdue'].map((v) => ({ value: v, label: v }));

// Orders (/sales): every sale, newest first, from the server in pages.
// Search by number or customer; filter by status, payment, store and dates.
// Open an order for its page; receipt, return and void from the row menu.
export default function Orders() {
  const { hasPermission } = usePermission();
  const navigate = useNavigate();
  const list = usePagedList('/api/sales', { filters: ['status', 'paymentStatus', 'storeId', 'startDate', 'endDate'] });
  const [stores, setStores] = useState([]);
  const [receiptFor, setReceiptFor] = useState(null);
  const [voiding, setVoiding] = useState(null);

  useEffect(() => {
    if (!hasPermission('stores', 'view')) return;
    api
      .get('/api/stores', { quiet: true })
      .then((s) => setStores(Array.isArray(s) ? s : s?.data || []))
      .catch(() => {});
  }, [hasPermission]);

  const columns = [
    {
      id: 'ref',
      header: 'Order',
      card: 'title',
      cell: (r) => (
        <Link to={`/sales/${r.id}`} className="font-medium text-primary hover:underline">
          {r.reference}
        </Link>
      ),
    },
    { id: 'date', header: 'Date', card: 'subtitle', cell: (r) => formatDateTime(r.date) },
    { id: 'customer', header: 'Customer', card: 'meta', cell: (r) => r.customer?.name || '—' },
    { id: 'store', header: 'Store', card: 'hidden', className: 'hidden lg:table-cell', cell: (r) => r.Store?.name || '—' },
    { id: 'cashier', header: 'Cashier', card: 'hidden', className: 'hidden xl:table-cell', cell: (r) => r.user?.name || '—' },
    { id: 'total', header: 'Total', card: 'meta', align: 'right', cell: (r) => <span className="tabular-nums">{formatCurrency(r.total)}</span> },
    { id: 'pay', header: 'Payment', card: 'meta', cell: (r) => <StatusBadge value={r.payment_status} /> },
    { id: 'status', header: 'Status', card: 'meta', cell: (r) => <StatusBadge value={r.status} /> },
  ];

  const filters = [
    { key: 'status', label: 'Status', allLabel: 'Not voided', options: STATUS },
    { key: 'paymentStatus', label: 'Payment', allLabel: 'Any payment', options: PAYMENT },
    ...(stores.length > 1 ? [{ key: 'storeId', label: 'Store', allLabel: 'All stores', options: stores.map((s) => ({ value: String(s.id), label: s.name })) }] : []),
  ];
  const dates = (
    <>
      <DateBox label="From" value={list.values.startDate} max={list.values.endDate || undefined} onChange={(v) => list.setFilter('startDate', v)} />
      <DateBox label="To" value={list.values.endDate} min={list.values.startDate || undefined} onChange={(v) => list.setFilter('endDate', v)} />
    </>
  );
  const filtered = list.search || Object.values(list.values).some(Boolean);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        description="Every sale, newest first. Open an order for its items, payments, returns and receipt."
        actions={
          hasPermission('sales', 'create') && (
            <Button type="button" className="max-sm:h-11" onClick={() => navigate('/add-sale')}>
              <Plus className="size-4" aria-hidden="true" /> New order
            </Button>
          )
        }
      />
      <FilterBar
        search={list.search}
        onSearch={list.setSearch}
        placeholder="Search order number or customer…"
        filters={filters}
        values={list.values}
        onFilter={list.setFilter}
        onClear={list.clearFilters}
        extra={dates}
        extraActive={(list.values.startDate ? 1 : 0) + (list.values.endDate ? 1 : 0)}
      />
      <DataTable
        label="Orders"
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
            <EmptyState title="No orders match" action={<Button variant="outline" onClick={list.clearFilters}>Clear filters</Button>}>
              Try another search, status or dates.
            </EmptyState>
          ) : (
            <EmptyState title="No orders yet" action={hasPermission('pos', 'view') && <Button onClick={() => navigate('/pos')}>Open Checkout</Button>} />
          )
        }
        actions={(row) => (
          <RowActions
            label={row.reference}
            actions={[
              { label: 'Open', icon: Eye, onSelect: () => navigate(`/sales/${row.id}`) },
              { label: 'Receipt', icon: FileText, onSelect: () => setReceiptFor(row) },
              row.status !== 'Void' && hasPermission('sales_returns', 'create') && { label: 'Return items', icon: Undo2, onSelect: () => navigate(`/add-sales-return?saleId=${row.id}`) },
              row.status !== 'Void' && hasPermission('sales', 'delete') && { label: 'Void…', icon: Ban, destructive: true, onSelect: () => setVoiding(row) },
            ]}
          />
        )}
      />
      {receiptFor && <Receipt saleId={receiptFor.id} onClose={() => setReceiptFor(null)} />}
      <VoidSaleDialog sale={voiding} onClose={() => setVoiding(null)} onVoided={list.reload} />
    </div>
  );
}
