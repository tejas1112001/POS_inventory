import React, { useEffect, useMemo, useState } from 'react';
import { PrinterIcon, CurrencyDollarIcon, ArrowUturnLeftIcon, BanknotesIcon, CubeIcon } from '@heroicons/react/24/outline';
import { useUI } from '../ListComponents/useUI';
import { indexColumn } from '../ListComponents/columnHelpers';
import ListPageLayout from '../ListComponents/ListPageLayout';
import ListControlButtons from '../ListComponents/ListControlButtons';
import DateRangePicker from '../ListComponents/DateRangePicker';
import SelectField from '../ListComponents/SelectField';
import SearchInput from '../ListComponents/SearchInput';
import ReportExportButtons from '../ListComponents/ReportExportButtons';
import { useServerReport } from '@/hooks/useServerReport';
import { api } from '@/services/api';
import { formatCurrency, formatDate } from '@/lib/format';

const n = (v) => Number(v) || 0;

// SalesReportList: one row per sold line (GET /api/sales/report). Dates
// and store are filtered by the server, which also limits users without
// sales:view_all to their own sales; product and text search filter the
// rows here. Totals are worked out from the rows shown.
export default function SalesReportList() {
  const [search, setSearch] = useState('');
  const [fromDate, setFromDate] = useState('');
  const [toDate, setToDate] = useState('');
  const [storeName, setStoreName] = useState('All');
  const [productFilter, setProductFilter] = useState('All');
  const [stores, setStores] = useState([]);

  useEffect(() => {
    api.get('/api/stores', { quiet: true }).then((s) => setStores(s || [])).catch(() => {});
  }, []);
  const storeId = stores.find((s) => s.name === storeName)?.id;
  const { rows, loading, error, qs } = useServerReport('/api/sales/report', { startDate: fromDate, endDate: toDate, storeId });

  const storeOptions = useMemo(() => ['All', ...stores.map((s) => s.name)], [stores]);
  const productOptions = useMemo(() => ['All', ...new Set(rows.map((r) => r.product?.name).filter(Boolean))], [rows]);

  const columns = useMemo(
    () => [
      indexColumn(),
      { accessorKey: 'date', header: 'Date', size: 110, cell: ({ getValue }) => formatDate(getValue()) },
      { accessorKey: 'reference', header: 'Receipt', size: 120 },
      { accessorKey: 'sku', header: 'SKU', size: 90 },
      {
        id: 'product',
        accessorFn: (r) => r.product?.name || '',
        header: 'Product',
        size: 200,
        cell: ({ row }) => (
          <div>
            <div>{row.original.product?.name}</div>
            {row.original.product?.brand && <div className="text-xs text-muted-foreground">{row.original.product.brand}</div>}
          </div>
        ),
      },
      { accessorKey: 'category', header: 'Category', size: 110 },
      { id: 'customer', accessorFn: (r) => r.customerName || r.customer?.name || '', header: 'Customer', size: 140 },
      { id: 'store', accessorFn: (r) => r.store?.name || '', header: 'Store', size: 120 },
      { accessorKey: 'soldQty', header: 'Sold', size: 70, meta: { align: 'center' } },
      { accessorKey: 'returnedQty', header: 'Returned', size: 80, meta: { align: 'center' } },
      { accessorKey: 'soldAmount', header: 'Sold amount', size: 120, cell: ({ getValue }) => formatCurrency(getValue()) },
      { accessorKey: 'netAmount', header: 'Net', size: 120, cell: ({ getValue }) => formatCurrency(getValue()) },
      { accessorKey: 'stockQty', header: 'In stock', size: 80, meta: { align: 'center' } },
    ],
    []
  );

  const filteredData = useMemo(() => {
    const q = search.trim().toLowerCase();
    return rows.filter((r) => {
      const passesProduct = productFilter === 'All' || r.product?.name === productFilter;
      const text = `${r.reference} ${r.sku} ${r.product?.name} ${r.product?.brand} ${r.category} ${r.customerName}`.toLowerCase();
      return passesProduct && (!q || text.includes(q));
    });
  }, [rows, search, productFilter]);

  const totals = useMemo(
    () =>
      filteredData.reduce(
        (t, r) => ({ sold: t.sold + n(r.soldAmount), returned: t.returned + n(r.returnedAmount), net: t.net + n(r.netAmount), qty: t.qty + n(r.soldQty) }),
        { sold: 0, returned: 0, net: 0, qty: 0 }
      ),
    [filteredData]
  );
  const summary = [
    { title: 'Sold', value: formatCurrency(totals.sold), icon: CurrencyDollarIcon, color: 'bg-green-500' },
    { title: 'Returned', value: formatCurrency(totals.returned), icon: ArrowUturnLeftIcon, color: 'bg-orange-500' },
    { title: 'Net sales', value: formatCurrency(totals.net), icon: BanknotesIcon, color: 'bg-blue-500' },
    { title: 'Items sold', value: totals.qty, icon: CubeIcon, color: 'bg-purple-500' },
  ];

  const { table, controlButtons, emptyState } = useUI({
    moduleName: 'sales reports',
    filteredData,
    columns,
    rowSelection: {},
    setRowSelection: () => {},
    onAddItem: false,
    resetFilters: () => {
      setSearch('');
      setStoreName('All');
      setProductFilter('All');
      setFromDate('');
      setToDate('');
    },
  });

  return (
    <ListPageLayout
      title="Sales Report"
      description="Sold lines with returns netted. Voided sales are left out."
      controlButtons={<ListControlButtons buttons={[...controlButtons, { icon: <PrinterIcon className="h-5 w-5" />, onClick: () => window.print(), title: 'Print' }]} />}
      extraContent={
        <>
        {error && <div role="alert" className="mb-2 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">{error}</div>}
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {summary.map((item) => (
            <div key={item.title} className="rounded-xl border border-gray-200 bg-white p-4 shadow-sm dark:border-border dark:bg-card">
              <div className="flex items-center">
                <span className={`${item.color} mr-4 rounded-full p-3`}>
                  <item.icon className="h-6 w-6 text-white" />
                </span>
                <div>
                  <p className="text-sm font-medium text-gray-600 dark:text-muted-foreground">{item.title}</p>
                  <h3 className="text-xl font-bold">{loading ? '…' : item.value}</h3>
                </div>
              </div>
            </div>
          ))}
        </div>
        </>
      }
      filterRow={
        <div className="grid grid-cols-1 gap-4 md:grid-cols-12">
          <DateRangePicker fromDate={fromDate} toDate={toDate} setFromDate={setFromDate} setToDate={setToDate} />
          <div className="md:col-span-2">
            <SelectField name="store" label="Store" value={storeName} onChange={setStoreName} options={storeOptions} placeholder="All" colClass="" />
          </div>
          <div className="md:col-span-3">
            <SelectField name="product" label="Product" value={productFilter} onChange={setProductFilter} options={productOptions} placeholder="All" colClass="" />
          </div>
          <ReportExportButtons path="/api/sales/report/export" qs={qs} name="sales-report" />
        </div>
      }
      filters={<SearchInput search={search} setSearch={setSearch} placeholder="Search receipt, product, customer..." />}
      table={table}
      isLoading={loading}
      emptyState={emptyState}
      dataLength={filteredData.length}
    />
  );
}
