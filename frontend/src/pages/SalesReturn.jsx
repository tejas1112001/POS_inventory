import { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { Eye, Plus, ReceiptText } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { usePermission } from '@/context/PermissionContext';
import { api } from '@/services/api';
import usePagedList from '@/hooks/usePagedList';
import DataTable from '@/components/kit/DataTable';
import FilterBar from '@/components/kit/FilterBar';
import RowActions from '@/components/kit/RowActions';
import StatusBadge from '@/components/kit/StatusBadge';
import { EmptyState } from '@/components/kit/States';
import { formatCurrency, formatDate } from '@/lib/format';
import { DropdownRangeDatePicker } from '@/components/ui/dropdown-range-date-picker';

// Returns (/sales-return): items brought back, newest first. Search a
// return number, an order number or a customer; filter by store and dates.
export default function SalesReturn() {
  const { hasPermission } = usePermission();
  const navigate = useNavigate();
  const list = usePagedList('/api/sales/returns', { filters: ['storeId', 'startDate', 'endDate'] });
  const [stores, setStores] = useState([]);

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
      header: 'Return',
      card: 'title',
      cell: (r) => (
        <Link to={`/sales-return/${r.id}`} className="font-medium text-primary hover:underline">
          {r.reference}
        </Link>
      ),
    },
    { id: 'date', header: 'Date', card: 'subtitle', cell: (r) => formatDate(r.date) },
    {
      id: 'order',
      header: 'Order',
      card: 'meta',
      cell: (r) =>
        r.sale_id ? (
          <Link to={`/sales/${r.sale_id}`} className="text-primary hover:underline">
            {r.saleReference}
          </Link>
        ) : (
          <span className="text-muted-foreground">Without order</span>
        ),
    },
    { id: 'customer', header: 'Customer', card: 'meta', cell: (r) => r.customerName || '—' },
    { id: 'store', header: 'Store', card: 'hidden', className: 'hidden lg:table-cell', cell: (r) => r.storeName || '—' },
    { id: 'total', header: 'Refund', card: 'meta', align: 'right', cell: (r) => <span className="tabular-nums">{formatCurrency(r.total)}</span> },
    { id: 'pay', header: 'Paid back', card: 'meta', cell: (r) => <StatusBadge value={r.payment_status} /> },
  ];
  const filters = stores.length > 1 ? [{ key: 'storeId', label: 'Store', allLabel: 'All stores', options: stores.map((s) => ({ value: String(s.id), label: s.name })) }] : [];
  const dates = (
    <DropdownRangeDatePicker
      placeholder="All dates"
      className="h-10 text-[13px] sm:h-9"
      value={{ from: list.values.startDate, to: list.values.endDate }}
      onChange={({ from, to }) => list.setFilters({ startDate: from, endDate: to })}
    />
  );
  const filtered = list.search || Object.values(list.values).some(Boolean);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <FilterBar
        actions={
          hasPermission('sales_returns', 'create') && (
            <Button type="button" className="max-sm:h-11" onClick={() => navigate('/add-sales-return')}>
              <Plus className="size-4" aria-hidden="true" /> New return
            </Button>
          )
        }
        search={list.search}
        onSearch={list.setSearch}
        placeholder="Search return, order or customer…"
        filters={filters}
        values={list.values}
        onFilter={list.setFilter}
        onClear={list.clearFilters}
        extra={dates}
        extraActive={(list.values.startDate ? 1 : 0) + (list.values.endDate ? 1 : 0)}
      />
      <DataTable
        label="Returns"
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
            <EmptyState title="No returns match" action={<Button variant="outline" onClick={list.clearFilters}>Clear filters</Button>}>
              Try another search or dates.
            </EmptyState>
          ) : (
            <EmptyState title="No returns yet">Returns are started from an order (Return items) or with New return.</EmptyState>
          )
        }
        actions={(row) => (
          <RowActions
            label={row.reference}
            actions={[
              { label: 'Open', icon: Eye, onSelect: () => navigate(`/sales-return/${row.id}`) },
              row.sale_id && hasPermission('sales', 'view') && { label: 'Open the order', icon: ReceiptText, onSelect: () => navigate(`/sales/${row.sale_id}`) },
            ]}
          />
        )}
      />
    </div>
  );
}
