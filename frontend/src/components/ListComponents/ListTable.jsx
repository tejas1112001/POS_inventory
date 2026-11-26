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
    'sticky top-0 z-10 border-b border-border bg-muted px-3 py-2.5 text-left text-xs font-semibold text-muted-foreground';
  // meta.sticky: the column stays at this left offset (px) while the table
  // scrolls sideways (# and the receipt number of the reports). Pinned cells
  // need a solid background, so they mix the row colours into the card.
  const pin = (column) => {
    const left = column.columnDef.meta?.sticky;
    return left === undefined ? undefined : { left };
  };
  const pinnedCell = (rowIndex) =>
    cn(
      'sticky z-[1]',
      rowIndex % 2 === 1 ? 'bg-[color-mix(in_oklab,var(--muted)_30%,var(--card))]' : 'bg-card',
      'group-hover:bg-[color-mix(in_oklab,var(--accent)_40%,var(--card))]'
    );

  return (
    <>
      <div className={cn('flex min-h-0 flex-col overflow-hidden rounded-2xl border bg-card shadow-sm', className)}>
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
                        // ! beats the .pos-table th rule (text-left).
                        header.column.columnDef.meta?.align === 'center' && '!text-center',
                        header.column.columnDef.meta?.align === 'right' && '!text-right',
                        pin(header.column) && 'z-20'
                      )}
                      style={{ width: header.getSize(), ...pin(header.column) }}
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
                    'group transition-colors duration-150 hover:bg-accent/40',
                    rowIndex % 2 === 1 && 'bg-muted/30'
                  )}
                >
                  {row.getVisibleCells().map(cell => (
                    <td
                      key={cell.id}
                      className={cn(
                        'border-b border-border/60 px-3 py-2.5 text-sm text-foreground',
                        cell.column.id === 'select' && 'px-0',
                        cell.column.columnDef.meta?.align === 'center' ? 'text-center' : '',
                        cell.column.columnDef.meta?.align === 'right' && 'text-right tabular-nums',
                        cell.column.id !== 'select' && !cell.column.columnDef.meta?.wrap && 'whitespace-nowrap',
                        pin(cell.column) && pinnedCell(rowIndex)
                      )}
                      style={pin(cell.column)}
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
