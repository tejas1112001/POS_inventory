import React, { useState, useMemo } from 'react';
import { useUI } from "../ListComponents/useUI";
import { selectColumn, statusColumn, actionsColumn } from '../ListComponents/columnHelpers';
import { api } from '../../services/api';
import EditBillerForm from '../forms/EditBillerForm';
import AddBillerForm from '../forms/AddBillerForm';

// Reusable components
import ListPageLayout from '../ListComponents/ListPageLayout';
import ListControlButtons from '../ListComponents/ListControlButtons';
import SearchInput from '../ListComponents/SearchInput';
import SelectFilters from '../ListComponents/SelectFilters';

export default function BillerList({ billers = [], showForm, setShowForm, onRefresh }) {
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState('All');
  const [rowSelection, setRowSelection] = useState({});

  // Status options
  const statusOptions = useMemo(() => ['All', 'Active', 'Inactive'], []);

  // Columns aligned with User model fields.
  const columns = [
    selectColumn(),
    {
      id: 'name',
      accessorKey: 'name',
      header: 'Name',
      size: 150,
    },
    {
      id: 'email',
      accessorKey: 'email',
      header: 'Email',
      size: 200,
    },
    {
      id: 'role',
      accessorFn: (row) => row.Role?.name || row.role,
      header: 'Role',
      size: 120,
      cell: ({ getValue }) => <span className="capitalize font-medium">{getValue()}</span>,
    },
    statusColumn('status', 'Status'),
    actionsColumn(['view', 'edit', 'delete'])
  ];

  // Filtered data - updated to match User model fields
  const filteredData = useMemo(() => {
    return billers.filter(biller => 
      (statusFilter === 'All' || biller.status === statusFilter) &&
      `${biller.name || ''} ${biller.email || ''} ${biller.Role?.name || biller.role || ''}`
        .toLowerCase()
        .includes(search.toLowerCase())
    );
  }, [billers, search, statusFilter]);

  const handleDelete = async (biller) => {
    try {
      await api.delete(`/api/billers/${biller.id}`);
      if (typeof onRefresh === 'function') {
        onRefresh();
      }
    } catch (error) {
      console.error('Error deleting user:', error);
    }
  };

  const actionRenderers = {
    edit: ({ data, onClose }) => (
      <EditBillerForm biller={data} onSaved={onRefresh} onCancel={onClose} />
    ),
  };

  // Use UI hook
  const {
    table,
    controlButtons,
    primaryButtons,
    emptyState
  } = useUI({
    moduleName: 'users',
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
    <>
      <ListPageLayout
        title="Users"
        description="Manage application users and assign custom roles"
        controlButtons={<ListControlButtons buttons={controlButtons} />}
        primaryButtons={primaryButtons.map((btn, i) => (
          <React.Fragment key={i}>{btn.element}</React.Fragment>
        ))}
        filters={
          <>
            <SearchInput search={search} setSearch={setSearch} placeholder="Search users..." />
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

      {showForm && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
          <div className="w-full max-w-md rounded-xl bg-white p-6 shadow-xl dark:bg-card">
            <AddBillerForm onSaved={onRefresh} onCancel={() => setShowForm(false)} />
          </div>
        </div>
      )}
    </>
  );
}
