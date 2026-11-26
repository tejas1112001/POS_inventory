import { useEffect, useMemo, useState } from 'react';
import { CurrencyDollarIcon, ArrowUturnLeftIcon, BanknotesIcon, CubeIcon } from '@heroicons/react/24/outline';
import StatCard from '../kit/StatCard';
import { useUI } from '../ListComponents/useUI';
import { indexColumn } from '../ListComponents/columnHelpers';
import ReportToolbar, { ReportActions } from '../reports/ReportToolbar';
import ReportTable from '../reports/ReportTable';
import PaymentMethodsReport from './PaymentMethodsReport';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { useServerReport } from '@/hooks/useServerReport';
import { api } from '@/services/api';
import { formatCurrency, formatDate, todayInShop } from '@/lib/format';
import { addDays } from '@/lib/payments';
import { unitQtyText } from '@/lib/saleUnits';
import { addQty, fmtQty } from '@/lib/qty';

const n = (v) => Number(v) || 0;
const INDEX_W = 48;

// SalesReportList: one row per sold line (GET /api/sales/report). One date
// range and store for the whole tab: the cards, the payment-method summary
// and the table all show the same period. Dates and store are filtered by
// the server, which also limits users without sales:view_all to their own
// sales; product and text search filter the rows here. Totals are worked
// out from the rows shown.
export default function SalesReportList() {
  const [search, setSearch] = useState('');
  const [fromDate, setFromDate] = useState(() => addDays(todayInShop(), -29));
  const [toDate, setToDate] = useState(() => todayInShop());
  const [values, setValues] = useState({ storeId: '', product: '' });
  const [stores, setStores] = useState([]);

  useEffect(() => {
    api.get('/api/stores', { quiet: true }).then((s) => setStores(s || [])).catch(() => {});
  }, []);
  const { rows, loading, error, qs } = useServerReport('/api/sales/report', { startDate: fromDate, endDate: toDate, storeId: values.storeId });
  const manyStores = stores.length > 1;

  const filters = useMemo(
    () => [
      ...(manyStores ? [{ key: 'storeId', label: 'Store', options: stores.map((s) => ({ value: String(s.id), label: s.name })) }] : []),
      { key: 'product', label: 'Product', options: [...new Set(rows.map((r) => r.product?.name).filter(Boolean))].sort().map((p) => ({ value: p, label: p })) },
    ],
    [manyStores, stores, rows]
  );

  const columns = useMemo(
    () => [
      { ...indexColumn(INDEX_W), meta: { align: 'center', sticky: 0 } },
      { accessorKey: 'reference', header: 'Receipt', size: 120, meta: { sticky: INDEX_W }, cell: ({ getValue }) => <span className="font-medium">{getValue()}</span> },
      { accessorKey: 'date', header: 'Date', size: 104, cell: ({ getValue }) => formatDate(getValue()) },
      {
        id: 'product',
        accessorFn: (r) => r.product?.name || '',
        header: 'Product',
        size: 200,
        meta: { wrap: true },
        cell: ({ row }) => {
          const r = row.original;
          const sub = [r.product?.brand, r.itemSku].filter(Boolean).join(' · ');
          return (
            <div className="max-w-[13rem] min-w-[9rem]">
              <div className="line-clamp-2">{r.product?.name}</div>
              {sub && <div className="truncate text-xs text-muted-foreground">{sub}</div>}
            </div>
          );
        },
      },
      { accessorKey: 'category', header: 'Category', size: 110 },
      { id: 'customer', accessorFn: (r) => r.customerName || r.customer?.name || '', header: 'Customer', size: 140 },
      ...(manyStores ? [{ id: 'store', accessorFn: (r) => r.store?.name || '', header: 'Store', size: 120 }] : []),
      // H3: "2 Box (24)" for a line sold in another unit.
      { accessorKey: 'soldQty', header: 'Sold', size: 80, meta: { align: 'right' }, cell: ({ row }) => unitQtyText(row.original, row.original.soldQty) },
      { accessorKey: 'returnedQty', header: 'Returned', size: 80, meta: { align: 'right' }, cell: (c) => fmtQty(c.getValue()) },
      { accessorKey: 'soldAmount', header: 'Sold amount', size: 110, meta: { align: 'right' }, cell: ({ getValue }) => formatCurrency(getValue()) },
      { accessorKey: 'netAmount', header: 'Net', size: 110, meta: { align: 'right' }, cell: ({ getValue }) => formatCurrency(getValue()) },
      { accessorKey: 'stockQty', header: 'In stock', size: 80, meta: { align: 'right' }, cell: (c) => fmtQty(c.getValue()) },
    ],
    [manyStores]
  );

  const filteredData = useMemo(() => {
    const q = search.trim().toLowerCase();
    return rows.filter((r) => {
      const passesProduct = !values.product || r.product?.name === values.product;
      const text = `${r.reference} ${r.itemSku || ''} ${r.product?.name} ${r.product?.brand} ${r.category} ${r.customerName}`.toLowerCase();
      return passesProduct && (!q || text.includes(q));
    });
  }, [rows, search, values.product]);

  const totals = useMemo(
    () =>
      filteredData.reduce(
        (t, r) => ({ sold: t.sold + n(r.soldAmount), returned: t.returned + n(r.returnedAmount), net: t.net + n(r.netAmount), qty: addQty(t.qty, n(r.soldQty)) }),
        { sold: 0, returned: 0, net: 0, qty: 0 }
      ),
    [filteredData]
  );
  const summary = [
    { title: 'Sold', value: formatCurrency(totals.sold), icon: CurrencyDollarIcon, tone: 'teal' },
    { title: 'Returned', value: formatCurrency(totals.returned), icon: ArrowUturnLeftIcon, tone: 'warn' },
    { title: 'Net sales', value: formatCurrency(totals.net), icon: BanknotesIcon, tone: 'teal' },
    { title: 'Items sold', value: fmtQty(totals.qty), icon: CubeIcon, tone: 'amber' },
  ];

  const clear = () => {
    setSearch('');
    setValues({ storeId: '', product: '' });
    setFromDate('');
    setToDate('');
  };
  const { table, emptyState } = useUI({
    moduleName: 'sales',
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
        placeholder="Search receipt or product…"
        filters={filters}
        values={values}
        onFilter={(k, v) => setValues((s) => ({ ...s, [k]: v }))}
        onClear={clear}
        dates={{
          from: fromDate,
          to: toDate,
          onChange: ({ from, to }) => {
            setFromDate(from);
            setToDate(to);
          },
        }}
        actions={<ReportActions server={{ path: '/api/sales/report/export', qs, name: 'sales-report' }} />}
      />
      {error && <InlineAlert>{error}</InlineAlert>}
      <div className="mb-3 grid shrink-0 grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
        {summary.map((item) => (
          <StatCard key={item.title} icon={item.icon} tone={item.tone} label={item.title} value={loading ? '…' : item.value} />
        ))}
      </div>
      <PaymentMethodsReport qs={qs} />
      <ReportTable table={table} loading={loading} emptyState={emptyState} dataLength={filteredData.length} />
    </>
  );
}
