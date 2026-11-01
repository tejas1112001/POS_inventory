import React, { useState } from 'react';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';

const defaultTitles = {
  view: 'View Item',
  edit: 'Edit Item',
  delete: 'Delete Item',
};

const toTitleCase = (value) => {
  if (!value) return '';
  return value
    .replace(/_/g, ' ')
    .replace(/([a-z])([A-Z])/g, '$1 $2')
    .replace(/^./, (char) => char.toUpperCase());
};

const formatValue = (value) => {
  if (value === null || value === undefined || value === '') return '—';
  if (Array.isArray(value)) return `${value.length} items`;
  if (typeof value === 'object') return 'Object';
  return String(value);
};

const getItemLabel = (data) => {
  if (!data || typeof data !== 'object') return 'this item';
  const candidates = [
    data.name,
    data.code,
    data.email,
    data.sku,
    data.id,
  ];
  const label = candidates.find((item) => item && String(item).trim());
  return label ? String(label) : 'this item';
};

export default function ActionModal({
  isOpen,
  onClose,
  action,
  data,
  handleDelete,
  handleEdit,
  renderers = {},
  titles = {},
}) {
  const [isWorking, setIsWorking] = useState(false);

  if (!isOpen || !action) return null;

  const title = titles[action] || defaultTitles[action] || 'Action';

  const handleDeleteConfirm = async () => {
    if (!handleDelete) return;
    try {
      setIsWorking(true);
      await handleDelete(data);
      onClose();
    } finally {
      setIsWorking(false);
    }
  };

  const handleEditConfirm = async (updatedData) => {
    if (!handleEdit) return;
    await handleEdit(updatedData);
    onClose();
  };

  const defaultRenderer = () => {
    if (action === 'view') {
      if (!data || typeof data !== 'object') {
        return <div className="text-sm text-gray-600">No details available.</div>;
      }

      const rows = Object.entries(data)
        .filter(([key]) => !['_id', '__v'].includes(key))
        .slice(0, 12);

      return (
        <div className="space-y-2 text-sm">
          {rows.map(([key, value]) => (
            <div key={key} className="flex justify-between border-b border-gray-100 pb-1">
              <span className="text-gray-500">{toTitleCase(key)}</span>
              <span className="text-gray-900">{formatValue(value)}</span>
            </div>
          ))}
        </div>
      );
    }

    if (action === 'edit') {
      return (
        <div className="text-sm text-gray-600">
          No editor configured for this item.
        </div>
      );
    }

    if (action === 'delete') {
      const label = getItemLabel(data);
      return (
        <div>
          <p>
            Are you sure you want to delete{' '}
            <span className="font-medium text-gray-900">{label}</span>? This action cannot be undone.
          </p>
          <div className="mt-4 flex justify-end space-x-2">
            <button
              type="button"
              className="inline-flex justify-center rounded-md border border-transparent bg-gray-100 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-gray-500 focus-visible:ring-offset-2"
              onClick={onClose}
              disabled={isWorking}
            >
              Cancel
            </button>
            <button
              type="button"
              className="inline-flex justify-center rounded-md border border-transparent bg-red-100 px-4 py-2 text-sm font-medium text-red-900 hover:bg-red-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-red-500 focus-visible:ring-offset-2 disabled:opacity-60"
              onClick={handleDeleteConfirm}
              disabled={isWorking}
            >
              {isWorking ? 'Deleting...' : 'Delete'}
            </button>
          </div>
        </div>
      );
    }

    return null;
  };

  const renderer = renderers[action];
  const content = renderer
    ? renderer({
        data,
        onClose,
        onDelete: handleDeleteConfirm,
        onEdit: handleEditConfirm,
      })
    : defaultRenderer();

  // Radix dialog (focus trap, Esc, bottom sheet on phones).
  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && onClose()}>
      <DialogContent sheet className="max-h-[90dvh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        <div>{content}</div>
      </DialogContent>
    </Dialog>
  );
}
