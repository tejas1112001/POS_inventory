import React from 'react';
import { EyeIcon, PencilIcon, TrashIcon } from '@heroicons/react/24/outline';
import { Checkbox } from '@/components/ui/checkbox';
import { cn } from '@/lib/utils';

const statusStyles = {
  Received: { dot: 'bg-emerald-500', badge: 'bg-emerald-500/10 text-emerald-700 border-emerald-500/20' },
  Pending: { dot: 'bg-sky-500', badge: 'bg-sky-500/10 text-sky-700 border-sky-500/20' },
  Ordered: { dot: 'bg-amber-500', badge: 'bg-amber-500/10 text-amber-700 border-amber-500/20' },
  Active: { dot: 'bg-emerald-500', badge: 'bg-emerald-500/10 text-emerald-700 border-emerald-500/20' },
  Inactive: { dot: 'bg-muted-foreground', badge: 'bg-muted text-muted-foreground border-border' },
  Paid: { dot: 'bg-emerald-500', badge: 'bg-emerald-500/10 text-emerald-700 border-emerald-500/20' },
  Unpaid: { dot: 'bg-red-500', badge: 'bg-red-500/10 text-red-700 border-red-500/20' },
  Overdue: { dot: 'bg-amber-500', badge: 'bg-amber-500/10 text-amber-700 border-amber-500/20' },
  'In Stock': { dot: 'bg-emerald-500', badge: 'bg-emerald-500/10 text-emerald-700 border-emerald-500/20' },
  'Low Stock': { dot: 'bg-amber-500', badge: 'bg-amber-500/10 text-amber-700 border-amber-500/20' },
  'Out of Stock': { dot: 'bg-red-500', badge: 'bg-red-500/10 text-red-700 border-red-500/20' },
  default: { dot: 'bg-muted-foreground', badge: 'bg-muted text-muted-foreground border-border' },
};

export function StatusBadge({ value, className }) {
  const style = statusStyles[value] || statusStyles.default;
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-md border px-2 py-0.5 text-xs font-medium',
        style.badge,
        className
      )}
    >
      <span className={cn('size-1.5 shrink-0 rounded-full', style.dot)} />
      {value}
    </span>
  );
}

// Reusable Select Column
export const selectColumn = (size = 48) => ({
  id: 'select',
  header: ({ table }) => (
    <div className="flex items-center justify-center">
      <Checkbox
        aria-label="Select all rows"
        checked={
          table.getIsAllPageRowsSelected()
            ? true
            : table.getIsSomePageRowsSelected()
              ? 'indeterminate'
              : false
        }
        onCheckedChange={(value) => table.toggleAllPageRowsSelected(!!value)}
      />
    </div>
  ),
  cell: ({ row }) => (
    <div className="flex items-center justify-center">
      <Checkbox
        aria-label="Select row"
        checked={row.getIsSelected()}
        onCheckedChange={(value) => row.toggleSelected(!!value)}
      />
    </div>
  ),
  size,
  meta: { align: 'center' },
});

// Reusable Index Column
export const indexColumn = (size = 50) => ({
  id: 'index',
  header: '#',
  cell: ({ row, table }) => {
    const page = table.getState().pagination.pageIndex;
    const pageSize = table.getState().pagination.pageSize;
    return (
      <span className="text-muted-foreground tabular-nums">
        {page * pageSize + row.index + 1}
      </span>
    );
  },
  size,
  meta: { align: 'center' }
});

// Reusable Status Column
export const statusColumn = (accessor, header, size = 100) => ({
  accessorKey: accessor,
  header,
  cell: ({ getValue }) => <StatusBadge value={getValue()} />,
  size
});

// Reusable Actions Column
const actionConfig = {
  view: {
    label: 'View',
    className: 'text-primary hover:bg-primary/10',
    icon: EyeIcon,
  },
  edit: {
    label: 'Edit',
    className: 'text-emerald-600 hover:bg-emerald-500/10',
    icon: PencilIcon,
  },
  delete: {
    label: 'Delete',
    className: 'text-destructive hover:bg-destructive/10',
    icon: TrashIcon,
  },
};

export const actionsColumn = (
  actions = ['view', 'edit', 'delete'],
  size = 120,
  options = {}
) => ({
  id: 'actions',
  header: '',
  cell: ({ row, openModal }) => (
    <div className="flex justify-center gap-1">
      {actions.map((action) => {
        const config = actionConfig[action] || {};
        const Icon = config.icon;
        const isDisabled = typeof options.isActionDisabled === 'function'
          ? options.isActionDisabled(action, row.original)
          : false;
        const label = typeof options.getActionLabel === 'function'
          ? options.getActionLabel(action, row.original)
          : config.label || action;

        return (
          <button
            key={action}
            type="button"
            className={`p-1 rounded-md transition-colors ${config.className || ''} ${
              isDisabled ? 'opacity-40 cursor-not-allowed' : ''
            }`}
            onClick={() => !isDisabled && openModal(action, row.original)}
            disabled={isDisabled}
            aria-label={label}
            title={label}
          >
            {Icon ? <Icon className="w-4 h-4" /> : null}
          </button>
        );
      })}
    </div>
  ),
  size
});

// Reusable Image Column - shows image + name (if nameAccessor is provided)
export const imageColumn = (nameAccessor, header, imageAccessor, size = 'md') => ({
  accessorKey: imageAccessor,
  header,
  cell: ({ row }) => {
    const imageData = row.getValue(imageAccessor);
    const name = row.original[nameAccessor] || 'Unknown';
    
    return (
      <div className="flex items-center gap-2">
        {imageData ? (
          <img 
            src={imageData} 
            alt={name}
            className="size-8 rounded-md object-cover ring-1 ring-border"
            onError={(e) => {
              e.target.style.display = 'none';
              // Show fallback when image fails to load
              const fallback = e.target.nextSibling;
              if (fallback) fallback.style.display = 'flex';
            }}
          />
        ) : null}
        {/* Fallback displayed if no image or image fails to load */}
        <div 
          className={`size-8 bg-muted rounded-md flex items-center justify-center ring-1 ring-border ${imageData ? 'hidden' : 'flex'}`}
        >
          <span className="text-xs font-medium text-muted-foreground">
            {name.charAt(0).toUpperCase()}
          </span>
        </div>
        {nameAccessor && <span className="font-medium">{name}</span>}
      </div>
    );
  },
  size: nameAccessor ? 200 : 70
});
