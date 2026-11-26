import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { ProductImage } from '@/components/shared/product-image';
import { useUI } from '../ListComponents/useUI';
import { selectColumn, indexColumn, statusColumn } from '../ListComponents/columnHelpers';
import ReportToolbar, { ReportActions } from '../reports/ReportToolbar';
import ReportTable from '../reports/ReportTable';
import { formatCurrency } from '@/lib/format';

const EMPTY = { customer: '', status: '' };

// InvoiceList: every non-void sale as an invoice (GET /api/invoices), with
// the same filter line as the other lists (search, Add filter).
export default function InvoiceList({ invoices }) {
  const [search, setSearch] = useState('');
  const [values, setValues] = useState(EMPTY);
  const [rowSelection, setRowSelection] = useState({});

  const filters = useMemo(
    () => [
      { key: 'customer', label: 'Customer', options: [...new Set(invoices.map((i) => i.customer?.name).filter(Boolean))].sort().map((c) => ({ value: c, label: c })) },
      { key: 'status', label: 'Status', options: ['Paid', 'Unpaid', 'Overdue'].map((s) => ({ value: s, label: s })) },
    ],
    [invoices]
  );

  const columns = useMemo(
    () => [
      selectColumn(),
      indexColumn(),
      {
        accessorKey: 'invoiceNo',
        header: 'Invoice no.',
        size: 120,
        cell: ({ row, getValue }) => (
          <Link to={`/sales/${row.original.id}`} className="font-medium text-primary hover:underline">
            {getValue()}
          </Link>
        ),
      },
      {
        id: 'customer',
        accessorFn: (r) => r.customer?.name || '',
        header: 'Customer',
        size: 200,
        cell: ({ row }) => (
          <div className="flex items-center gap-3">
            <ProductImage name={row.original.customer?.name} src={row.original.customer?.avatar} size={32} rounded="rounded-full" />
            <span>{row.original.customer?.name}</span>
          </div>
        ),
      },
      { accessorKey: 'dueDate', header: 'Due date', size: 120 },
      { accessorKey: 'amount', header: 'Amount', size: 110, meta: { align: 'right' }, cell: ({ getValue }) => formatCurrency(getValue()) },
      { accessorKey: 'paid', header: 'Paid', size: 110, meta: { align: 'right' }, cell: ({ getValue }) => formatCurrency(getValue()) },
      { accessorKey: 'amountDue', header: 'Amount due', size: 120, meta: { align: 'right' }, cell: ({ getValue }) => formatCurrency(getValue()) },
      statusColumn('status', 'Status'),
    ],
    []
  );

  const filteredData = useMemo(() => {
    const q = search.trim().toLowerCase();
    return invoices.filter(
      (i) =>
        (!values.customer || i.customer?.name === values.customer) &&
        (!values.status || i.status === values.status) &&
        (!q || `${i.customer?.name} ${i.invoiceNo}`.toLowerCase().includes(q))
    );
  }, [invoices, search, values]);
  const exportRows = useMemo(
    () => filteredData.map((i) => ({ 'Invoice no.': i.invoiceNo, Customer: i.customer?.name, 'Due date': i.dueDate, Amount: i.amount, Paid: i.paid, 'Amount due': i.amountDue, Status: i.status })),
    [filteredData]
  );

  const clear = () => {
    setSearch('');
    setValues(EMPTY);
  };
  const { table, emptyState } = useUI({
    moduleName: 'invoices',
    filteredData,
    columns,
    rowSelection,
    setRowSelection,
    onAddItem: false,
    resetFilters: clear,
  });

  return (
    <>
      <ReportToolbar
        search={search}
        onSearch={setSearch}
        placeholder="Search invoice or customer…"
        filters={filters}
        values={values}
        onFilter={(k, v) => setValues((s) => ({ ...s, [k]: v }))}
        onClear={clear}
        actions={<ReportActions rows={exportRows} name="invoices" print={false} gated={false} />}
      />
      <ReportTable table={table} emptyState={emptyState} dataLength={filteredData.length} />
    </>
  );
}
