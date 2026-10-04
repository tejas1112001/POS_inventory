import React, { useState, useMemo } from 'react';
import { usePos } from '../../hooks/usePos';
import { usePermission } from '../../hooks/usePermission';
import { useUI } from "../ListComponents/useUI";
import { selectColumn, indexColumn, imageColumn, statusColumn, actionsColumn } from '../ListComponents/columnHelpers';
import { api } from '../../services/api';
import EditBrandForm from '../forms/EditBrandForm';

// Reusable components
import ListPageLayout from '../ListComponents/ListPageLayout';
import ListControlButtons from '../ListComponents/ListControlButtons';
import SearchInput from '../ListComponents/SearchInput';
import SelectFilters from '../ListComponents/SelectFilters';

export default function BrandList({ brands = [], setShowForm, onRefresh }) {
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState('All');
  const [rowSelection, setRowSelection] = useState({});
  const { hasPermission } = usePermission();
  
  // Status options
  const statusOptions = useMemo(() => ['All', 'Active', 'Inactive'], []);

  // Columns configuration using helpers
  const columns = [
    selectColumn(),
    indexColumn(),
    imageColumn('name', 'Brand', 'image'),
    {
      id: 'created_by',
      accessorFn: row => row.createdBy ?? row.created_by ?? 'Unknown',
      header: 'Created By',
      size: 140,
    },
    {
      accessorKey: 'createdAt',
      header: 'Created Date',
      size: 120,
    },
    statusColumn('status', 'Status'),
    actionsColumn(['edit', 'delete'], 120, {
      isActionDisabled: (action) => !hasPermission('brands', action),
    })
  ];

  // Filtered data
  const filteredData = useMemo(() => {
    return brands.filter(brand => 
      (statusFilter === 'All' || brand.status === statusFilter) &&
      brand.name.toLowerCase().includes(search.toLowerCase())
    );
  }, [brands, search, statusFilter]);

  const handleDelete = async (brand) => {
    try {
      await api.delete(`/api/brands/${brand.id}`);
      if (typeof onRefresh === 'function') {
        onRefresh();
      }
    } catch (error) {
      console.error('Error deleting brand:', error);
    }
  };

  const actionRenderers = {
    edit: ({ data, onClose }) => (
      <EditBrandForm brand={data} onSaved={onRefresh} onCancel={onClose} />
    ),
  };

  // Use UI hook
  const {
    table,
    controlButtons,
    primaryButtons,
    emptyState
  } = useUI({
    moduleName: 'brands',
    filteredData,
    columns,
    rowSelection,
    setRowSelection,
    onAddItem: () => setShowForm(true),
    isAddDisabled: !hasPermission('brands', 'create'),
    onRefresh,
    resetFilters: () => {
      setSearch('');
      setStatusFilter('All');
      setRowSelection({});
    }
  });

  return (
    <ListPageLayout
      title="Brands"
      description="Manage your brands"
      controlButtons={<ListControlButtons buttons={controlButtons} />}
      primaryButtons={primaryButtons.map((btn, i) => (
        <React.Fragment key={i}>{btn.element}</React.Fragment>
      ))}
      filters={
        <>
          <SearchInput search={search} setSearch={setSearch} placeholder="Search brands..." />
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
