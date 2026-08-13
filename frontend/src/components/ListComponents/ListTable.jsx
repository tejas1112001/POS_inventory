import React, { useState } from 'react';
import { flexRender } from '@tanstack/react-table';
import { cn } from '@/lib/utils';
import ActionModal from './ActionModal';

export default function ListTable({
  table,
  isLoading,
  emptyState,
  maxHeight,
  className,
  handleDelete,
  handleEdit,
  actionRenderers,
  actionTitles,
}) {
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [modalAction, setModalAction] = useState(null);
  const [modalData, setModalData] = useState(null);

  const openModal = (action, data) => {
    setModalAction(action);
    setModalData(data);
    setIsModalOpen(true);
  };

  const closeModal = () => {
    setIsModalOpen(false);
    setModalAction(null);
    setModalData(null);
  };

  if (isLoading) {
    return (
      <div className={cn('p-4 text-sm text-muted-foreground', className)}>
        Loading...
      </div>
    );
  }

  const headerCellClass =
    'sticky top-0 z-10 border-b-2 border-border bg-muted/80 px-3 py-2 text-left text-[11px] font-semibold uppercase tracking-wide text-muted-foreground backdrop-blur-sm';

  return (
    <>
      <div className={cn('flex min-h-0 flex-col overflow-hidden rounded-lg border bg-card shadow-sm', className)}>
        <div
          className={cn(
            'relative isolate min-h-0 flex-1 overflow-auto scrollbar-thin',
            maxHeight
          )}
        >
          <table className="pos-table min-w-full">
            <thead>
              {table.getHeaderGroups().map(headerGroup => (
                <tr key={headerGroup.id}>
                  {headerGroup.headers.map(header => (
                    <th
                      key={header.id}
                      className={cn(
                        headerCellClass,
                        header.column.id === 'select' && 'px-0',
                        header.column.columnDef.meta?.align === 'center' && 'text-center'
                      )}
                      style={{ width: header.getSize() }}
                    >
                      {!header.isPlaceholder && flexRender(
                        header.column.columnDef.header,
                        header.getContext()
                      )}
                    </th>
                  ))}
                </tr>
              ))}
            </thead>
            <tbody>
              {table.getRowModel().rows.map((row, rowIndex) => (
                <tr
                  key={row.id}
                  className={cn(
                    'transition-colors hover:bg-primary/[0.04]',
                    rowIndex % 2 === 1 && 'bg-muted/25'
                  )}
                >
                  {row.getVisibleCells().map(cell => (
                    <td
                      key={cell.id}
                      className={cn(
                        'border-b border-border/50 px-3 py-2 text-sm text-foreground',
                        cell.column.id === 'select' && 'px-0',
                        cell.column.columnDef.meta?.align === 'center' ? 'text-center' : '',
                        cell.column.id !== 'select' && 'whitespace-nowrap'
                      )}
                    >
                      {flexRender(cell.column.columnDef.cell, { ...cell.getContext(), openModal })}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        {table.getRowModel().rows.length === 0 && emptyState}
      </div>
      <ActionModal
        isOpen={isModalOpen}
        onClose={closeModal}
        action={modalAction}
        data={modalData}
        handleDelete={handleDelete}
        handleEdit={handleEdit}
        renderers={actionRenderers}
        titles={actionTitles}
      />
    </>
  );
}
