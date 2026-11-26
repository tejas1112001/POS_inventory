import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { UsersRound, Wallet } from 'lucide-react';
import { useUI } from '../ListComponents/useUI';
import { indexColumn } from '../ListComponents/columnHelpers';
import ReportToolbar, { ReportActions } from '../reports/ReportToolbar';
import ReportTable from '../reports/ReportTable';
import StatCard from '../kit/StatCard';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { useServerReport } from '@/hooks/useServerReport';
import { formatCurrency, formatDate } from '@/lib/format';
import { addQty, fmtQty } from '@/lib/qty';

const INDEX_W = 48;

// byCustomer: one row per customer from the sold lines (returns netted),
// with the last receipt in the period.
function byCustomer(lines) {
  const out = {};
  for (const item of lines) {
    const name = item.customerName || item.customer?.name || 'Walk-in customer';
    const key = item.customer?.id ?? name;
    const c = (out[key] ||= { id: String(key), customerId: item.customer?.id, name, sales: new Set(), items: 0, amount: 0, lastDate: item.date, reference: item.reference });
    c.sales.add(item.saleId);
    c.items = addQty(c.items, Number(item.soldQty) || 0);
    c.amount += Number(item.netAmount ?? item.soldAmount) || 0;
    if (new Date(item.date) > new Date(c.lastDate)) {
      c.lastDate = item.date;
      c.reference = item.reference;
    }
  }
  return Object.values(out)
    .map((c) => ({ ...c, totalOrders: c.sales.size, sales: undefined }))
    .sort((a, b) => b.amount - a.amount);
}

// CustomerReportList: sales per customer in a period, from the sales
// report (GET /api/sales/report, own sales only without sales:view_all).
export default function CustomerReportList() {
  const [search, setSearch] = useState('');
  const [fromDate, setFromDate] = useState('');
  const [toDate, setToDate] = useState('');
  const { rows: lines, loading, error } = useServerReport('/api/sales/report', { startDate: fromDate, endDate: toDate });
  const reports = useMemo(() => byCustomer(lines), [lines]);

  const columns = useMemo(
    () => [
      { ...indexColumn(INDEX_W), meta: { align: 'center', sticky: 0 } },
      {
        accessorKey: 'name',
        header: 'Customer',
        size: 200,
        meta: { sticky: INDEX_W },
        cell: ({ row }) =>
          row.original.customerId ? (
            <Link to={`/customers/${row.original.customerId}`} className="font-medium text-primary hover:underline">
              {row.original.name}
            </Link>
          ) : (
            <span className="font-medium">{row.original.name}</span>
          ),
      },
      { accessorKey: 'totalOrders', header: 'Sales', size: 80, meta: { align: 'right' } },
      { accessorKey: 'items', header: 'Items', size: 80, meta: { align: 'right' }, cell: (c) => fmtQty(c.getValue()) },
      { accessorKey: 'amount', header: 'Net amount', size: 120, meta: { align: 'right' }, cell: ({ getValue }) => formatCurrency(getValue()) },
      { accessorKey: 'reference', header: 'Last receipt', size: 120 },
      { accessorKey: 'lastDate', header: 'Last sale', size: 110, cell: ({ getValue }) => formatDate(getValue()) },
    ],
    []
  );

  const filteredData = useMemo(() => {
    const q = search.trim().toLowerCase();
    return reports.filter((r) => !q || `${r.name} ${r.reference}`.toLowerCase().includes(q));
  }, [reports, search]);
  const total = useMemo(() => filteredData.reduce((s, r) => s + r.amount, 0), [filteredData]);
  const exportRows = useMemo(
    () => filteredData.map((r) => ({ Customer: r.name, Sales: r.totalOrders, Items: r.items, 'Net amount': r.amount, 'Last receipt': r.reference, 'Last sale': formatDate(r.lastDate) })),
    [filteredData]
  );

  const clear = () => {
    setSearch('');
    setFromDate('');
    setToDate('');
  };
  const { table, emptyState } = useUI({
    moduleName: 'customers',
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
        placeholder="Search customer or receipt…"
        onClear={clear}
        dates={{
          from: fromDate,
          to: toDate,
          onChange: ({ from, to }) => {
            setFromDate(from);
            setToDate(to);
          },
        }}
        actions={<ReportActions rows={exportRows} name="customer-report" />}
      />
      {error && <InlineAlert>{error}</InlineAlert>}
      <div className="mb-3 grid shrink-0 grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard icon={UsersRound} tone="teal" label="Customers" value={loading ? '…' : filteredData.length} />
        <StatCard icon={Wallet} tone="teal" label="Net sales" value={loading ? '…' : formatCurrency(total)} />
      </div>
      <ReportTable table={table} loading={loading} emptyState={emptyState} dataLength={filteredData.length} />
    </>
  );
}
