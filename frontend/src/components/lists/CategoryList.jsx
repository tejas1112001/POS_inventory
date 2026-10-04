import React, { useState, useMemo } from 'react';
import { useUI } from "../ListComponents/useUI";
import { selectColumn, indexColumn, statusColumn, actionsColumn } from '../ListComponents/columnHelpers';
import { usePos } from '../../hooks/usePos';
import { usePermission } from '../../hooks/usePermission';
import { api } from '../../services/api';
import EditCategoryForm from '../forms/EditCategoryForm';

// Reusable components
import ListPageLayout from '../ListComponents/ListPageLayout';
import ListControlButtons from '../ListComponents/ListControlButtons';
import SearchInput from '../ListComponents/SearchInput';
import SelectFilters from '../ListComponents/SelectFilters';

export default function CategoryList({ categories = [], setShowForm, onRefresh }) {
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
    {
      accessorKey: 'name',
      header: 'Category',
      size: 150,
    },
    {
      id: 'image',
      accessorFn: row => row.image || row.icon || '',
      header: 'Image',
      size: 90,
      cell: ({ getValue }) => {
        const src = getValue();
        return src ? (
          <img src={src} alt="cat" className="w-10 h-10 object-cover rounded-md" />
        ) : (
          <span className="text-xs text-gray-400">--</span>
        );
      }
    },
    statusColumn('status', 'Status'),
    {
      id: 'created_by',
      accessorFn: row => row.createdBy ?? row.created_by ?? row.created_by_name ?? row.creatorName ?? null,
      header: 'Created By',
      size: 140,
      cell: ({ getValue }) => getValue() || '-'
    },
    actionsColumn(['edit', 'delete'], 120, {
      isActionDisabled: (action) => !hasPermission('categories', action),
    })
  ];

  // Filtered data
  const filteredData = useMemo(() => {
    return categories.filter(cat => 
      (statusFilter === 'All' || (cat.status ?? '').toString() === statusFilter) &&
      `${cat.name ?? ''}`.toLowerCase().includes(search.toLowerCase())
    );
  }, [categories, search, statusFilter]);

  const handleDelete = async (category) => {
    try {
      await api.delete(`/api/categories/${category.id}`);
      if (typeof onRefresh === 'function') {
        onRefresh();
      }
    } catch (error) {
      console.error('Error deleting category:', error);
    }
  };

  const actionRenderers = {
    edit: ({ data, onClose }) => (
      <EditCategoryForm category={data} onSaved={onRefresh} onCancel={onClose} />
    ),
  };

  const {
    table,
    controlButtons,
    primaryButtons,
    emptyState
  } = useUI({
    moduleName: 'categories',
    filteredData,
    columns,
    rowSelection,
    setRowSelection,
    onAddItem: () => setShowForm(true),
    isAddDisabled: !hasPermission('categories', 'create'),
    onRefresh,
    resetFilters: () => {
      setSearch('');
      setStatusFilter('All');
      setRowSelection({});
    }
  });

  return (
    <ListPageLayout
      title="Categories"
      description="Manage your categories"
      controlButtons={<ListControlButtons buttons={controlButtons} />}
      primaryButtons={primaryButtons.map((btn, i) => (
        <React.Fragment key={i}>{btn.element}</React.Fragment>
      ))}
      filters={
        <>
          <SearchInput search={search} setSearch={setSearch} placeholder="Search categories..." />
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
