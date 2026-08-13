import React, { useState, useMemo } from 'react';
import { useUI } from "../ListComponents/useUI";
import { selectColumn, statusColumn, actionsColumn } from '../ListComponents/columnHelpers';
import { api } from '../../services/api';
import EditCustomerForm from '../forms/EditCustomerForm';

// Reusable components
import ListPageLayout from '../ListComponents/ListPageLayout';
import ListControlButtons from '../ListComponents/ListControlButtons';
import SearchInput from '../ListComponents/SearchInput';
import SelectFilters from '../ListComponents/SelectFilters';

export default function CustomerList({ customers = [], setShowForm, onRefresh }) {
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState('All');
  const [rowSelection, setRowSelection] = useState({});

  // Status options
  const statusOptions = useMemo(() => ['All', 'Active', 'Inactive'], []);

  // Columns configuration using helpers
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
          <img src={src} alt="customer" className="w-10 h-10 object-cover rounded-md" />
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
      accessorKey: 'city',
      header: 'City',
      size: 120,
    },
    {
      accessorKey: 'country',
      header: 'Country',
      size: 120,
    },
    statusColumn('status', 'Status'),
    actionsColumn(['view', 'edit', 'delete'])
  ];

  // Filtered data
  const filteredData = useMemo(() => {
    return customers.filter(customer => 
      (statusFilter === 'All' || customer.status === statusFilter) &&
      `${customer.name || ''} ${customer.email || ''} ${customer.phone || ''} ${customer.city || ''} ${customer.country || ''}`
        .toLowerCase()
        .includes(search.toLowerCase())
    );
  }, [customers, search, statusFilter]);

  const handleDelete = async (customer) => {
    try {
      await api.delete(`/api/customers/${customer.id}`);
      if (typeof onRefresh === 'function') {
        onRefresh();
      }
    } catch (error) {
      console.error('Error deleting customer:', error);
    }
  };

  const actionRenderers = {
    edit: ({ data, onClose }) => (
      <EditCustomerForm customer={data} onSaved={onRefresh} onCancel={onClose} />
    ),
  };

  // Use UI hook
  const {
    table,
    controlButtons,
    primaryButtons,
    emptyState
  } = useUI({
    moduleName: 'customers',
    filteredData,
    columns,
    rowSelection,
    setRowSelection,
    onAddItem: () => setShowForm(true),
    onRefresh,
    resetFilters: () => {
      setSearch('');
      setStatusFilter('All');
      setRowSelection({});
    }
  });

  return (
    <ListPageLayout
      title="Customers"
      description="Manage your customers"
      controlButtons={<ListControlButtons buttons={controlButtons} />}
      primaryButtons={primaryButtons.map((btn, i) => (
        <React.Fragment key={i}>{btn.element}</React.Fragment>
      ))}
      filters={
        <>
          <SearchInput search={search} setSearch={setSearch} placeholder="Search customers..." />
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
