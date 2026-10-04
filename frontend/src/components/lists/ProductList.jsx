import React, { useState, useMemo } from 'react';
import { useNavigate } from 'react-router-dom';

// Import reusable components
import ListPageLayout from '../ListComponents/ListPageLayout';
import ListControlButtons from '../ListComponents/ListControlButtons';
import SearchInput from '../ListComponents/SearchInput';
import SelectFilters from '../ListComponents/SelectFilters';
import { useUI } from '../ListComponents/useUI';
import { 
  selectColumn, 
  indexColumn, 
  imageColumn,
  actionsColumn,
  statusColumn
} from '../ListComponents/columnHelpers';

import { usePos } from '../../hooks/usePos';
import { usePermission } from '../../hooks/usePermission';
import { api } from '../../services/api';
import { formatCurrency } from '@/lib/format';
import EditProductForm from '../forms/EditProductForm';
import ProductDetails from './ProductDetails';

export default function ProductList({ products = [], setShowForm, onRefresh }) {
  const [search, setSearch] = useState('');
  const [categoryFilter, setCategoryFilter] = useState('All');
  const [brandFilter, setBrandFilter] = useState('All');
  const [rowSelection, setRowSelection] = useState({});
  const navigate = useNavigate();
  const { hasPermission } = usePermission();

  // Derive unique categories & brands
  const categories = useMemo(() => {
    const values = products.map(p => p.category).filter(Boolean);
    return ['All', ...new Set(values)];
  }, [products]);
  const brands = useMemo(() => {
    const values = products.map(p => p.brand).filter(Boolean);
    return ['All', ...new Set(values)];
  }, [products]);

  // Columns configuration
const columns = [
  selectColumn(),
  indexColumn(),
  imageColumn('name', 'Product Name', 'image'),
  {
    accessorKey: 'category',
    header: 'Category',
    size: 100,
  },
  {
    accessorKey: 'brand',
    header: 'Brand',
    size: 100,
  },
  {
    accessorKey: 'price',
    header: 'Price',
    cell: ({ getValue }) => formatCurrency(getValue()),
    size: 90,
  },
  {
    accessorKey: 'unit',
    header: 'Unit',
    size: 60,
  },
  {
    accessorKey: 'qty',
    header: 'Qty',
    size: 60,
  },
  statusColumn('status', 'Status'),
  {
    accessorKey: 'createdBy',
    header: 'Created By',
    size: 140,
    cell: ({ getValue }) => getValue() || 'Unknown'
  },
  // imageColumn('createdBy', 'Created By', 'createdByAvatar', 140),
  actionsColumn(['view', 'edit', 'delete'], 120, {
    isActionDisabled: (action) => !hasPermission('products', action),
  })
];

  // Filtered data
  const filteredData = useMemo(() => {
    return products.filter(p => {
      const sku = p.ProductVariants?.[0]?.sku || '';
      const searchValue = `${p.code ?? ''} ${p.name ?? ''} ${sku}`.toLowerCase();
      return (
        (categoryFilter === 'All' || p.category === categoryFilter) &&
        (brandFilter === 'All' || p.brand === brandFilter) &&
        searchValue.includes(search.toLowerCase())
      );
    });
  }, [products, search, categoryFilter, brandFilter]);

  const handleDelete = async (product) => {
    try {
      await api.delete(`/api/products/${product.id}`);
      if (typeof onRefresh === 'function') {
        onRefresh();
      }
    } catch (error) {
      console.error('Error deleting product:', error);
    }
  };

  const actionRenderers = {
    view: ({ data }) => <ProductDetails product={data} />,
    edit: ({ data, onClose }) => (
      <EditProductForm product={data} onSaved={onRefresh} onCancel={onClose} />
    ),
  };

  const actionTitles = {
    view: 'Product Details',
    edit: 'Edit Product',
    delete: 'Delete Product',
  };

  const {
  table,
  controlButtons,
  primaryButtons,
  emptyState,
} = useUI({
  moduleName: 'products',
  filteredData,
  columns,
  rowSelection,
  setRowSelection,
  onImportItem: hasPermission('products', 'create') ? () => setShowForm(true) : null,
  onAddItem: () => navigate('/add-product'),
  isAddDisabled: !hasPermission('products', 'create'),
  onRefresh,
  resetFilters: () => {
          setSearch('');
          setCategoryFilter('All');
          setBrandFilter('All');
          setRowSelection({});
        }
});

  return (
    <ListPageLayout
      title="Product List"
      description="Manage your products inventory"
      controlButtons={<ListControlButtons buttons={controlButtons} />}
      primaryButtons={primaryButtons.map((btn, i) => (
        <React.Fragment key={i}>{btn.element}</React.Fragment>
      ))}
      filters={
        <>
          <SearchInput search={search} setSearch={setSearch} />
          <SelectFilters
            statusFilter={categoryFilter}
            setStatusFilter={setCategoryFilter}
            statusOptions={categories}
            placeholder="Category"
          />
          <SelectFilters
            statusFilter={brandFilter}
            setStatusFilter={setBrandFilter}
            statusOptions={brands}
            placeholder="Brand"
          />
        </>
      }
      table={table}
      emptyState={emptyState}
      dataLength={filteredData.length}
      tableProps={{
        handleDelete,
        actionRenderers,
        actionTitles,
      }}
    />
  );
}
