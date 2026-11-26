import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { HandCoins, ReceiptText } from 'lucide-react';
import { ProductImage } from '@/components/shared/product-image';
import { useUI } from '../ListComponents/useUI';
import { indexColumn, StatusBadge } from '../ListComponents/columnHelpers';
import ReportToolbar, { ReportActions } from '../reports/ReportToolbar';
import ReportTable from '../reports/ReportTable';
import StatCard from '../kit/StatCard';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { formatCurrency } from '@/lib/format';

const INDEX_W = 48;
// day turns the server's "02 Jan 2026" into 2026-01-02 (local calendar day).
const pad = (n) => String(n).padStart(2, '0');
const day = (s) => {
  const d = new Date(s);
  return Number.isNaN(d.getTime()) ? null : `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
};

// CustomerDueReportList: every sale with money still due (from
// GET /api/invoices), filtered here by due date, status and text.
export default function CustomerDueReportList({ reports, loading, error }) {
  const [search, setSearch] = useState('');
  const [fromDate, setFromDate] = useState('');
  const [toDate, setToDate] = useState('');
  const [values, setValues] = useState({ status: '' });

  const filters = useMemo(
    () => [{ key: 'status', label: 'Status', options: [...new Set(reports.map((r) => r.status).filter(Boolean))].sort().map((s) => ({ value: s, label: s })) }],
    [reports]
  );

  const columns = useMemo(
    () => [
      { ...indexColumn(INDEX_W), meta: { align: 'center', sticky: 0 } },
      { accessorKey: 'reference', header: 'Receipt', size: 120, meta: { sticky: INDEX_W }, cell: ({ getValue }) => <span className="font-medium">{getValue()}</span> },
      {
        id: 'customer',
        accessorFn: (r) => r.customer?.name || '',
        header: 'Customer',
        size: 200,
        cell: ({ row }) => (
          <div className="flex items-center gap-3">
            <ProductImage name={row.original.customer?.name} src={row.original.customer?.avatar} size={32} rounded="rounded-full" />
            {row.original.customer?.id ? (
              <Link to={`/customers/${row.original.customer.id}`} className="text-primary hover:underline">
                {row.original.customer.name}
              </Link>
            ) : (
              <span>{row.original.customer?.name}</span>
            )}
          </div>
        ),
      },
      { accessorKey: 'dueDate', header: 'Due date', size: 110 },
      { accessorKey: 'totalAmount', header: 'Total', size: 110, meta: { align: 'right' }, cell: ({ getValue }) => formatCurrency(getValue()) },
      { accessorKey: 'paid', header: 'Paid', size: 110, meta: { align: 'right' }, cell: ({ getValue }) => formatCurrency(getValue()) },
      { accessorKey: 'due', header: 'Due', size: 110, meta: { align: 'right' }, cell: ({ getValue }) => <span className="font-semibold">{formatCurrency(getValue())}</span> },
      { accessorKey: 'status', header: 'Status', size: 110, cell: ({ getValue }) => <StatusBadge value={getValue()} /> },
    ],
    []
  );

  const filteredData = useMemo(() => {
    const q = search.trim().toLowerCase();
    return reports.filter((r) => {
      const d = day(r.dueDate);
      if (fromDate && (!d || d < fromDate)) return false;
      if (toDate && (!d || d > toDate)) return false;
      if (values.status && r.status !== values.status) return false;
      return !q || `${r.reference} ${r.customer?.name} ${r.status}`.toLowerCase().includes(q);
    });
  }, [reports, search, values.status, fromDate, toDate]);
  const totalDue = useMemo(() => filteredData.reduce((s, r) => s + (Number(r.due) || 0), 0), [filteredData]);
  const exportRows = useMemo(
    () => filteredData.map((r) => ({ Receipt: r.reference, Customer: r.customer?.name, 'Due date': r.dueDate, Total: r.totalAmount, Paid: r.paid, Due: r.due, Status: r.status })),
    [filteredData]
  );

  const clear = () => {
    setSearch('');
    setValues({ status: '' });
    setFromDate('');
    setToDate('');
  };
  const { table, emptyState } = useUI({
    moduleName: 'dues',
    filteredData,
    columns,
    rowSelection: {},
    setRowSelection: () => {},
    onAddItem: false,
    resetFilters: clear,
  });

  return (
    <>
      <ReportToolbar
        search={search}
        onSearch={setSearch}
        placeholder="Search receipt or customer…"
        filters={filters}
        values={values}
        onFilter={(k, v) => setValues((s) => ({ ...s, [k]: v }))}
        onClear={clear}
        dates={{
          from: fromDate,
          to: toDate,
          placeholder: 'Any due date',
          onChange: ({ from, to }) => {
            setFromDate(from);
            setToDate(to);
          },
        }}
        actions={<ReportActions rows={exportRows} name="customer-dues" />}
      />
      {error && <InlineAlert>{error}</InlineAlert>}
      <div className="mb-3 grid shrink-0 grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard icon={HandCoins} tone="warn" label="Total due" value={loading ? '…' : formatCurrency(totalDue)} />
        <StatCard icon={ReceiptText} tone="teal" label="Unpaid receipts" value={loading ? '…' : filteredData.length} />
      </div>
      <ReportTable table={table} loading={loading} emptyState={emptyState} dataLength={filteredData.length} />
    </>
  );
}
