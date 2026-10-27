import React, { useEffect, useMemo, useState } from 'react';
import { useUI } from '../ListComponents/useUI';
import { indexColumn } from '../ListComponents/columnHelpers';
import ListPageLayout from '../ListComponents/ListPageLayout';
import DateRangePicker from '../ListComponents/DateRangePicker';
import SelectField from '../ListComponents/SelectField';
import ListControlButtons from '../ListComponents/ListControlButtons';
import SearchInput from '../ListComponents/SearchInput';
import ReportExportButtons from '../ListComponents/ReportExportButtons';
import { useServerReport } from '@/hooks/useServerReport';
import { api } from '@/services/api';
import { formatDate, formatCurrency } from '@/lib/format';

const n = (v) => Number(v) || 0;

// PurchaseReportList: one row per purchased line (GET /api/purchase/report).
// Dates, supplier and store are filtered by the server; product and text
// search filter the rows here.
export default function PurchaseReportList() {
  const [search, setSearch] = useState('');
  const [fromDate, setFromDate] = useState('');
  const [toDate, setToDate] = useState('');
  const [supplierName, setSupplierName] = useState('All');
  const [storeName, setStoreName] = useState('All');
  const [productFilter, setProductFilter] = useState('All');
  const [suppliers, setSuppliers] = useState([]);
  const [stores, setStores] = useState([]);

  useEffect(() => {
    api.get('/api/suppliers', { quiet: true }).then((s) => setSuppliers(s || [])).catch(() => {});
    api.get('/api/stores', { quiet: true }).then((s) => setStores(s || [])).catch(() => {});
  }, []);
  const supplierId = suppliers.find((s) => s.name === supplierName)?.id;
  const storeId = stores.find((s) => s.name === storeName)?.id;
  const { rows, loading, error, qs } = useServerReport('/api/purchase/report', { startDate: fromDate, endDate: toDate, supplierId, storeId });

  const productOptions = useMemo(() => ['All', ...new Set(rows.map((r) => r.product?.name).filter(Boolean))], [rows]);

  const columns = useMemo(
    () => [
      indexColumn(),
      { accessorKey: 'date', header: 'Date', size: 110, cell: ({ getValue }) => formatDate(getValue(), 'N/A') },
      { accessorKey: 'reference', header: 'Reference', size: 110 },
      { accessorKey: 'sku', header: 'SKU', size: 90 },
      { id: 'product', accessorFn: (r) => r.product?.name || '', header: 'Product', size: 200 },
      { accessorKey: 'category', header: 'Category', size: 110 },
      { id: 'supplier', accessorFn: (r) => r.supplierName || r.supplier?.name || '', header: 'Supplier', size: 140 },
      { id: 'store', accessorFn: (r) => r.store?.name || '', header: 'Store', size: 120 },
      { accessorKey: 'status', header: 'Status', size: 100 },
      { accessorKey: 'purchaseQty', header: 'Qty', size: 70, meta: { align: 'center' } },
      { accessorKey: 'purchaseAmount', header: 'Amount', size: 120, cell: ({ getValue }) => formatCurrency(getValue()) },
      { accessorKey: 'stockQty', header: 'In stock', size: 80, meta: { align: 'center' } },
    ],
    []
  );

  const filteredData = useMemo(() => {
    const q = search.trim().toLowerCase();
    return rows.filter((r) => {
      const passesProduct = productFilter === 'All' || r.product?.name === productFilter;
      const text = `${r.reference} ${r.sku} ${r.product?.name} ${r.category} ${r.supplierName}`.toLowerCase();
      return passesProduct && (!q || text.includes(q));
    });
  }, [rows, search, productFilter]);

  const total = useMemo(() => filteredData.reduce((t, r) => t + n(r.purchaseAmount), 0), [filteredData]);

  const { table, controlButtons, emptyState } = useUI({
    moduleName: 'purchase reports',
    filteredData,
    columns,
    rowSelection: {},
    setRowSelection: () => {},
    onAddItem: false,
    resetFilters: () => {
      setSearch('');
      setSupplierName('All');
      setStoreName('All');
      setProductFilter('All');
      setFromDate('');
      setToDate('');
    },
  });

  return (
    <ListPageLayout
      title="Purchase Report"
      description={`Purchased lines. Total shown: ${formatCurrency(total)}. Cancelled purchases are left out.`}
      controlButtons={<ListControlButtons buttons={controlButtons} />}
      filters={<SearchInput search={search} setSearch={setSearch} placeholder="Search purchase reports..." />}
      filterRow={
        <div className="grid grid-cols-1 gap-4 md:grid-cols-12">
          <DateRangePicker fromDate={fromDate} toDate={toDate} setFromDate={setFromDate} setToDate={setToDate} />
          <div className="md:col-span-2">
            <SelectField name="supplier" label="Supplier" value={supplierName} onChange={setSupplierName} options={['All', ...suppliers.map((s) => s.name)]} placeholder="All" colClass="" />
          </div>
          <div className="md:col-span-2">
            <SelectField name="store" label="Store" value={storeName} onChange={setStoreName} options={['All', ...stores.map((s) => s.name)]} placeholder="All" colClass="" />
          </div>
          <div className="md:col-span-2">
            <SelectField name="product" label="Product" value={productFilter} onChange={setProductFilter} options={productOptions} placeholder="All" colClass="" />
          </div>
          <ReportExportButtons path="/api/purchase/report/export" qs={qs} name="purchase-report" />
        </div>
      }
      extraContent={error && <div role="alert" className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">{error}</div>}
      table={table}
      isLoading={loading}
      emptyState={emptyState}
      dataLength={filteredData.length}
    />
  );
}
