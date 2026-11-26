import { useEffect, useMemo, useState } from 'react';
import { ShoppingBag } from 'lucide-react';
import { useUI } from '../ListComponents/useUI';
import { indexColumn } from '../ListComponents/columnHelpers';
import ReportToolbar, { ReportActions } from '../reports/ReportToolbar';
import ReportTable from '../reports/ReportTable';
import StatCard from '../kit/StatCard';
import { InlineAlert } from '@/components/feedback/InlineAlert';
import { useServerReport } from '@/hooks/useServerReport';
import { api } from '@/services/api';
import { formatDate, formatCurrency } from '@/lib/format';
import { unitQtyText } from '@/lib/saleUnits';
import { fmtQty } from '@/lib/qty';

const n = (v) => Number(v) || 0;
const INDEX_W = 48;
const EMPTY = { supplierId: '', storeId: '', product: '' };

// PurchaseReportList: one row per purchased line (GET /api/purchase/report).
// Dates, supplier and store are filtered by the server; product and text
// search filter the rows here.
export default function PurchaseReportList() {
  const [search, setSearch] = useState('');
  const [fromDate, setFromDate] = useState('');
  const [toDate, setToDate] = useState('');
  const [values, setValues] = useState(EMPTY);
  const [suppliers, setSuppliers] = useState([]);
  const [stores, setStores] = useState([]);

  useEffect(() => {
    api.get('/api/suppliers', { quiet: true }).then((s) => setSuppliers(s || [])).catch(() => {});
    api.get('/api/stores', { quiet: true }).then((s) => setStores(s || [])).catch(() => {});
  }, []);
  const { rows, loading, error, qs } = useServerReport('/api/purchase/report', {
    startDate: fromDate,
    endDate: toDate,
    supplierId: values.supplierId,
    storeId: values.storeId,
  });
  const manyStores = stores.length > 1;

  const filters = useMemo(
    () => [
      { key: 'supplierId', label: 'Supplier', options: suppliers.map((s) => ({ value: String(s.id), label: s.name })) },
      ...(manyStores ? [{ key: 'storeId', label: 'Store', options: stores.map((s) => ({ value: String(s.id), label: s.name })) }] : []),
      { key: 'product', label: 'Product', options: [...new Set(rows.map((r) => r.product?.name).filter(Boolean))].sort().map((p) => ({ value: p, label: p })) },
    ],
    [suppliers, manyStores, stores, rows]
  );

  const columns = useMemo(
    () => [
      { ...indexColumn(INDEX_W), meta: { align: 'center', sticky: 0 } },
      { accessorKey: 'reference', header: 'Reference', size: 120, meta: { sticky: INDEX_W }, cell: ({ getValue }) => <span className="font-medium">{getValue()}</span> },
      { accessorKey: 'date', header: 'Date', size: 104, cell: ({ getValue }) => formatDate(getValue(), '—') },
      {
        id: 'product',
        accessorFn: (r) => r.product?.name || '',
        header: 'Product',
        size: 200,
        meta: { wrap: true },
        cell: ({ row }) => (
          <div className="max-w-[13rem] min-w-[9rem]">
            <div className="line-clamp-2">{row.original.product?.name}</div>
            {row.original.itemSku && <div className="truncate text-xs text-muted-foreground">{row.original.itemSku}</div>}
          </div>
        ),
      },
      { accessorKey: 'category', header: 'Category', size: 110 },
      { id: 'supplier', accessorFn: (r) => r.supplierName || r.supplier?.name || '', header: 'Supplier', size: 140 },
      ...(manyStores ? [{ id: 'store', accessorFn: (r) => r.store?.name || '', header: 'Store', size: 120 }] : []),
      { accessorKey: 'status', header: 'Status', size: 100 },
      // H3: "2 Carton (48)" for a line bought in another unit.
      { accessorKey: 'purchaseQty', header: 'Qty', size: 90, meta: { align: 'right' }, cell: ({ row }) => unitQtyText(row.original, row.original.purchaseQty) },
      { accessorKey: 'purchaseAmount', header: 'Amount', size: 110, meta: { align: 'right' }, cell: ({ getValue }) => formatCurrency(getValue()) },
      { accessorKey: 'stockQty', header: 'In stock', size: 80, meta: { align: 'right' }, cell: (c) => fmtQty(c.getValue()) },
    ],
    [manyStores]
  );

  const filteredData = useMemo(() => {
    const q = search.trim().toLowerCase();
    return rows.filter((r) => {
      const passesProduct = !values.product || r.product?.name === values.product;
      const text = `${r.reference} ${r.itemSku || ''} ${r.product?.name} ${r.category} ${r.supplierName}`.toLowerCase();
      return passesProduct && (!q || text.includes(q));
    });
  }, [rows, search, values.product]);

  const total = useMemo(() => filteredData.reduce((t, r) => t + n(r.purchaseAmount), 0), [filteredData]);

  const clear = () => {
    setSearch('');
    setValues(EMPTY);
    setFromDate('');
    setToDate('');
  };
  const { table, emptyState } = useUI({
    moduleName: 'purchases',
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
        placeholder="Search reference or product…"
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
        actions={<ReportActions server={{ path: '/api/purchase/report/export', qs, name: 'purchase-report' }} />}
      />
      {error && <InlineAlert>{error}</InlineAlert>}
      <div className="mb-3 grid shrink-0 grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard icon={ShoppingBag} tone="teal" label="Purchased" value={loading ? '…' : formatCurrency(total)} hint="Cancelled left out" />
      </div>
      <ReportTable table={table} loading={loading} emptyState={emptyState} dataLength={filteredData.length} />
    </>
  );
}
