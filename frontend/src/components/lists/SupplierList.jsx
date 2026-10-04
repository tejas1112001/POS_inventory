import React, { useState, useMemo } from 'react';
import { usePos } from '../../hooks/usePos';
import { usePermission } from '../../hooks/usePermission';
import { useUI } from "../ListComponents/useUI";
import {
  selectColumn,
  statusColumn,
  actionsColumn
} from '../ListComponents/columnHelpers';
import { api } from '../../services/api';
import EditSupplierForm from '../forms/EditSupplierForm';

// Reusable components
import ListPageLayout from '../ListComponents/ListPageLayout';
import ListControlButtons from '../ListComponents/ListControlButtons';
import SearchInput from '../ListComponents/SearchInput';
import SelectFilters from '../ListComponents/SelectFilters';

export default function SupplierList({ suppliers = [], setShowForm, onRefresh }) {
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState('All');
  const [rowSelection, setRowSelection] = useState({});
  const { hasPermission } = usePermission();

  // Status options
  const statusOptions = useMemo(() => ['All', 'Active', 'Inactive'], []);

  // Columns using helpers
  const columns = [
    selectColumn(),
    {
      id: 'image',
      accessorFn: row => row.image || row.icon || '',
      header: 'Image',
      size: 90,
      cell: ({ getValue }) => {
        const src = getValue();
        return src ? (
          <img src={src} alt="supplier" className="w-10 h-10 object-cover rounded-md" />
        ) : (
          <span className="text-xs text-gray-400">--</span>
        );
      }
    },
    {
      accessorKey: 'name',
      header: 'Name',
      size: 150,
    },
    {
      accessorKey: 'email',
      header: 'Email',
      size: 200,
    },
    {
      accessorKey: 'phone',
      header: 'Phone',
      size: 120,
    },
    {
      accessorKey: 'address',
      header: 'Address',
      size: 200,
    },
    statusColumn('status', 'Status'),
    actionsColumn(['view', 'edit', 'delete'], 120, {
      isActionDisabled: (action) => !hasPermission('suppliers', action),
    })
  ];

  // Filtered data
  const filteredData = useMemo(() => {
    return suppliers.filter(supplier => 
      (statusFilter === 'All' || supplier.status === statusFilter) &&
      `${supplier.name || ''} ${supplier.email || ''} ${supplier.phone || ''} ${supplier.address || ''}`
        .toLowerCase()
        .includes(search.toLowerCase())
    );
  }, [suppliers, search, statusFilter]);

  const handleDelete = async (supplier) => {
    try {
      await api.delete(`/api/suppliers/${supplier.id}`);
      if (typeof onRefresh === 'function') {
        onRefresh();
      }
    } catch (error) {
      console.error('Error deleting supplier:', error);
    }
  };

  const actionRenderers = {
    edit: ({ data, onClose }) => (
      <EditSupplierForm supplier={data} onSaved={onRefresh} onCancel={onClose} />
    ),
  };

  // Use UI hook
  const {
    table,
    controlButtons,
    primaryButtons,
    emptyState
  } = useUI({
    moduleName: 'suppliers',
    filteredData,
    columns,
    rowSelection,
    setRowSelection,
    onAddItem: () => setShowForm(true),
    isAddDisabled: !hasPermission('suppliers', 'create'),
    onRefresh,
    resetFilters: () => {
      setSearch('');
      setStatusFilter('All');
      setRowSelection({});
    }
  });

  return (
    <ListPageLayout
      title="Suppliers"
      description="Manage your suppliers"
      controlButtons={<ListControlButtons buttons={controlButtons} />}
      primaryButtons={primaryButtons.map((btn, i) => (
        <React.Fragment key={i}>{btn.element}</React.Fragment>
      ))}
      filters={
        <>
          <SearchInput search={search} setSearch={setSearch} placeholder="Search suppliers..." />
          <SelectFilters
            statusFilter={statusFilter}
            setStatusFilter={setStatusFilter}
            statusOptions={statusOptions}
          />
        </>
      }
      table={table}
      emptyState={emptyState}
      dataLength={filteredData.length}
      tableProps={{ handleDelete, actionRenderers }}
    />
  );
}
