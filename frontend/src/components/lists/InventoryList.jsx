import React, { useState, useMemo } from 'react';
import { usePos } from '../../hooks/usePos';
import { useUI } from '../ListComponents/useUI';
import {
  selectColumn,
  indexColumn,
  imageColumn,
  actionsColumn,
  StatusBadge,
} from '../ListComponents/columnHelpers';
import { api } from '../../services/api';
import ListPageLayout from '../ListComponents/ListPageLayout';
import ListControlButtons from '../ListComponents/ListControlButtons';
import SearchInput from '../ListComponents/SearchInput';
import SelectFilters from '../ListComponents/SelectFilters';

// Inline edit form for adjusting qty / alert threshold
function EditInventoryForm({ data, onSaved, onCancel }) {
  const [qty, setQty] = useState(data.qty ?? 0);
  const [quantityAlert, setQuantityAlert] = useState(data.quantityAlert ?? 0);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);

  const handleSubmit = async (e) => {
    e.preventDefault();
    setSaving(true);
    setError(null);
    try {
      await api.post('/api/inventory', {
        variantId: data.variantId,
        storeId: data.storeId,
        qty: Number(qty),
        quantityAlert: Number(quantityAlert),
      });
      onSaved();
      onCancel();
    } catch (err) {
      setError(err.message || 'Failed to update inventory');
    } finally {
      setSaving(false);
    }
  };

  return (
    <form onSubmit={handleSubmit} className="space-y-4 p-2">
      <div className="grid grid-cols-2 gap-4 text-sm text-gray-600 mb-2">
        <div>
          <span className="font-medium text-gray-800">Product:</span> {data.productName}
        </div>
        <div>
          <span className="font-medium text-gray-800">SKU:</span> {data.sku}
        </div>
        <div>
          <span className="font-medium text-gray-800">Store:</span> {data.storeName}
        </div>
        <div>
          <span className="font-medium text-gray-800">Variant:</span>{' '}
          {data.attributes ? JSON.stringify(data.attributes) : '—'}
        </div>
      </div>

      <div className="grid grid-cols-2 gap-4">
        <div>
          <label className="block text-sm font-medium text-gray-700 mb-1">
            Quantity
          </label>
          <input
            type="number"
            min="0"
            className="w-full px-3 py-2 border-2 border-gray-300 rounded-lg focus:ring-blue-500 focus:border-blue-500"
            value={qty}
            onChange={(e) => setQty(e.target.value)}
            required
          />
        </div>
        <div>
          <label className="block text-sm font-medium text-gray-700 mb-1">
            Low-Stock Alert Threshold
          </label>
          <input
            type="number"
            min="0"
            className="w-full px-3 py-2 border-2 border-gray-300 rounded-lg focus:ring-blue-500 focus:border-blue-500"
            value={quantityAlert}
            onChange={(e) => setQuantityAlert(e.target.value)}
          />
        </div>
      </div>

      {error && <p className="text-red-500 text-sm">{error}</p>}

      <div className="flex justify-end gap-3 pt-2">
        <button
          type="button"
          onClick={onCancel}
          className="px-4 py-2 border border-gray-300 rounded-lg text-gray-700 hover:bg-gray-50"
        >
          Cancel
        </button>
        <button
          type="submit"
          disabled={saving}
          className="px-4 py-2 bg-black text-white rounded-lg hover:bg-gray-800 disabled:opacity-50"
        >
          {saving ? 'Saving…' : 'Save'}
        </button>
      </div>
    </form>
  );
}

// Status badge handled by shared StatusBadge component

export default function InventoryList({ inventory = [], onRefresh }) {
  const [search, setSearch] = useState('');
  const [storeFilter, setStoreFilter] = useState('All');
  const [statusFilter, setStatusFilter] = useState('All');
  const [rowSelection, setRowSelection] = useState({});
  const { currentUser } = usePos();
  const isAdmin = currentUser?.role === 'admin';

  // Derive unique store names for filter dropdown
  const storeOptions = useMemo(() => {
    const names = inventory.map((r) => r.storeName).filter(Boolean);
    return ['All', ...new Set(names)];
  }, [inventory]);

  const statusOptions = ['All', 'In Stock', 'Low Stock', 'Out of Stock'];

  const columns = [
    selectColumn(),
    indexColumn(),
    imageColumn('productName', 'Product', 'productImage'),
    {
      accessorKey: 'sku',
      header: 'SKU',
      size: 120,
    },
    {
      accessorKey: 'attributes',
      header: 'Variant',
      size: 120,
      cell: ({ getValue }) => {
        const val = getValue();
        if (!val) return <span className="text-gray-400">—</span>;
        if (typeof val === 'object') {
          return Object.entries(val)
            .map(([k, v]) => `${k}: ${v}`)
            .join(', ');
        }
        return val;
      },
    },
    {
      accessorKey: 'storeName',
      header: 'Store',
      size: 120,
    },
    {
      accessorKey: 'qty',
      header: 'Qty',
      size: 70,
      cell: ({ getValue, row }) => {
        const qty = getValue();
        const alert = row.original.quantityAlert;
        const isLow = qty > 0 && qty <= alert;
        return (
          <span className={`font-semibold ${isLow ? 'text-yellow-600' : qty === 0 ? 'text-red-600' : 'text-gray-800'}`}>
            {qty}
          </span>
        );
      },
    },
    {
      accessorKey: 'quantityAlert',
      header: 'Alert At',
      size: 80,
      cell: ({ getValue }) => (
        <span className="text-gray-500">{getValue() ?? 0}</span>
      ),
    },
    {
      accessorKey: 'price',
      header: 'Price',
      size: 90,
      cell: ({ getValue }) =>
        getValue() != null ? `PKR ${Number(getValue()).toLocaleString()}` : '—',
    },
    {
      accessorKey: 'status',
      header: 'Stock Status',
      size: 120,
      cell: ({ getValue }) => <StatusBadge value={getValue()} />,
    },
    actionsColumn(['edit'], 70, {
      isActionDisabled: () => !isAdmin,
    }),
  ];

  const filteredData = useMemo(() => {
    const q = search.toLowerCase();
    return inventory.filter((r) => {
      const matchesSearch =
        (r.productName ?? '').toLowerCase().includes(q) ||
        (r.sku ?? '').toLowerCase().includes(q) ||
        (r.storeName ?? '').toLowerCase().includes(q);
      const matchesStore = storeFilter === 'All' || r.storeName === storeFilter;
      const matchesStatus = statusFilter === 'All' || r.status === statusFilter;
      return matchesSearch && matchesStore && matchesStatus;
    });
  }, [inventory, search, storeFilter, statusFilter]);

  const actionRenderers = {
    edit: ({ data, onClose }) => (
      <EditInventoryForm data={data} onSaved={onRefresh} onCancel={onClose} />
    ),
  };

  const actionTitles = {
    edit: 'Adjust Inventory',
  };

  const { table, controlButtons, primaryButtons, emptyState } = useUI({
    moduleName: 'inventory',
    filteredData,
    columns,
    rowSelection,
    setRowSelection,
    onRefresh,
    resetFilters: () => {
      setSearch('');
      setStoreFilter('All');
      setStatusFilter('All');
      setRowSelection({});
    },
  });

  return (
    <ListPageLayout
      title="Inventory"
      description="Real-time stock levels across all stores"
      controlButtons={<ListControlButtons buttons={controlButtons} />}
      primaryButtons={primaryButtons.map((btn, i) => (
        <React.Fragment key={i}>{btn.element}</React.Fragment>
      ))}
      filters={
        <>
          <SearchInput
            search={search}
            setSearch={setSearch}
            placeholder="Search product, SKU or store…"
          />
          <SelectFilters
            statusFilter={storeFilter}
            setStatusFilter={setStoreFilter}
            statusOptions={storeOptions}
            placeholder="Store"
          />
          <SelectFilters
            statusFilter={statusFilter}
            setStatusFilter={setStatusFilter}
            statusOptions={statusOptions}
            placeholder="Stock Status"
          />
        </>
      }
      extraContent={
        <div className="flex flex-wrap gap-2">
          {[
            {
              label: 'Total SKUs',
              value: inventory.length,
              color: 'bg-primary/5 text-primary border-primary/20',
            },
            {
              label: 'In Stock',
              value: inventory.filter((r) => r.status === 'In Stock').length,
              color: 'bg-emerald-500/10 text-emerald-700 border-emerald-500/20',
            },
            {
              label: 'Low Stock',
              value: inventory.filter((r) => r.status === 'Low Stock').length,
              color: 'bg-amber-500/10 text-amber-700 border-amber-500/20',
            },
            {
              label: 'Out of Stock',
              value: inventory.filter((r) => r.status === 'Out of Stock').length,
              color: 'bg-red-500/10 text-red-700 border-red-500/20',
            },
          ].map(({ label, value, color }) => (
            <div
              key={label}
              className={`flex items-center gap-1.5 rounded-md border px-2.5 py-1 text-xs font-medium ${color}`}
            >
              <span>{label}:</span>
              <span className="text-sm font-bold tabular-nums">{value}</span>
            </div>
          ))}
        </div>
      }
      table={table}
      emptyState={emptyState}
      dataLength={filteredData.length}
      tableProps={{ actionRenderers, actionTitles }}
    />
  );
}
