import { cn } from '@/lib/utils';

// One colour map for every status in the app.
const TONE = {
  Active: 'success', Inactive: 'muted',
  Paid: 'success', Partial: 'warning', Unpaid: 'danger', Overdue: 'danger',
  Completed: 'success', Pending: 'warning', Void: 'muted', Received: 'success', 'Partially received': 'warning', Ordered: 'info', Cancelled: 'muted',
  'In Stock': 'success', 'Low Stock': 'warning', 'Out of Stock': 'danger',
};
const CLS = {
  success: 'bg-success-bg text-success-fg',
  warning: 'bg-warning-bg text-warning-fg',
  danger: 'bg-danger-bg text-danger-fg',
  info: 'bg-info-bg text-info-fg',
  muted: 'bg-muted text-muted-foreground',
};

export default function StatusBadge({ value }) {
  if (!value) return null;
  return (
    <span className={cn('inline-flex items-center whitespace-nowrap rounded-md px-2.5 py-1 text-xs leading-none font-semibold', CLS[TONE[value] || 'muted'])}>
      {value}
    </span>
  );
}
