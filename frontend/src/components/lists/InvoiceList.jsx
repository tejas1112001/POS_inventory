import React, { useState, useMemo } from 'react';
import { useUI } from "../ListComponents/useUI";
import { selectColumn, indexColumn, statusColumn } from '../ListComponents/columnHelpers';

// Reusable components
import ListPageLayout from '../ListComponents/ListPageLayout';
import SearchInput from '../ListComponents/SearchInput';
import SelectField from '../ListComponents/SelectField';
import ListControlButtons from '../ListComponents/ListControlButtons';

export default function InvoiceList({ invoices }) {
  const [search, setSearch] = useState('');
  const [customerFilter, setCustomerFilter] = useState('All');
  const [statusFilter, setStatusFilter] = useState('All');
  const [rowSelection, setRowSelection] = useState({});
  
  // Customer options
  const customerOptions = useMemo(() => {
    const customers = [...new Set(invoices.map(invoice => invoice.customer.name))];
    return ['All', ...customers];
  }, [invoices]);
  
  // Status options
  const statusOptions = ['All', 'Paid', 'Unpaid', 'Overdue'];

  // Columns configuration
  const columns = [
    selectColumn(),
    indexColumn(),
    {
      accessorKey: 'invoiceNo',
      header: 'Invoice No',
      cell: ({ getValue }) => (
        <a href="#" className="text-blue-600 hover:text-blue-800">
          {getValue()}
        </a>
      ),
      size: 120,
    },
    {
      accessorKey: 'customer',
      header: 'Customer',
      cell: ({ row }) => (
        <div className="flex items-center">
          <div className="bg-gray-100 rounded-full p-1 mr-3">
            <img 
              src={row.original.customer.avatar} 
              alt={row.original.customer.name} 
              className="w-8 h-8 rounded-full object-cover"
            />
          </div>
          <span>{row.original.customer.name}</span>
        </div>
      ),
      size: 200,
    },
    {
      accessorKey: 'dueDate',
      header: 'Due Date',
      size: 120,
    },
    {
      accessorKey: 'amount',
      header: 'Amount',
      cell: ({ getValue }) => <span>${getValue()}</span>,
      size: 100,
    },
    {
      accessorKey: 'paid',
      header: 'Paid',
      cell: ({ getValue }) => <span>${getValue()}</span>,
      size: 100,
    },
    {
      accessorKey: 'amountDue',
      header: 'Amount Due',
      cell: ({ getValue }) => <span>${getValue()}</span>,
      size: 120,
    },
    statusColumn('status', 'Status'),
  ];

  // Filtered data
  const filteredData = useMemo(() => {
    return invoices.filter(invoice => 
      (customerFilter === 'All' || invoice.customer.name === customerFilter) &&
      (statusFilter === 'All' || invoice.status === statusFilter) &&
      `${invoice.customer.name} ${invoice.invoiceNo}`.toLowerCase().includes(search.toLowerCase())
    );
  }, [invoices, search, customerFilter, statusFilter]);

  // Use UI hook
  const {
    table,
    controlButtons,
    emptyState
  } = useUI({
    moduleName: 'invoices',
    filteredData,
    columns,
    rowSelection,
    setRowSelection,
    onAddItem: false,
    resetFilters: () => {
      setSearch('');
      setCustomerFilter('All');
      setStatusFilter('All');
    }
  });

  return (
    <ListPageLayout
      title="Invoices"
      description="Manage your stock invoices"
      controlButtons={<ListControlButtons buttons={controlButtons} />}
      filters={
        <>
          <SearchInput search={search} setSearch={setSearch} placeholder="Search invoices..." />
          <SelectField
            name="customer"
            label="Customer"
            value={customerFilter}
            onChange={setCustomerFilter}
            options={customerOptions}
            placeholder="All Customers"
          />
          <SelectField
            name="status"
            label="Status"
            value={statusFilter}
            onChange={setStatusFilter}
            options={statusOptions}
            placeholder="All Statuses"
          />
        </>
      }
      table={table}
      emptyState={emptyState}
      dataLength={filteredData.length}
    />
  );
}