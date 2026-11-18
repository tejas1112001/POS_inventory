import { formatDate } from '@/lib/format';

// BatchExpiry shows a batch's expiry with how near it is: expired in red,
// within 90 days in amber (industry P3 item 1). The unknown batch has none.
export default function BatchExpiry({ batch }) {
  if (batch.unknown || !batch.expiry) return <span className="text-muted-foreground">No batch</span>;
  const d = batch.daysToExpiry;
  let note = null;
  let cls = '';
  if (batch.expired) {
    note = 'expired';
    cls = 'text-destructive font-medium';
  } else if (d != null && d <= 90) {
    note = d === 0 ? 'expires today' : `${d} day${d === 1 ? '' : 's'} left`;
    cls = 'text-amber-700 dark:text-amber-400';
  }
  return (
    <span className={cls}>
      {formatDate(batch.expiry)}
      {note && <span className="ml-1 text-xs">({note})</span>}
      {batch.status === 'recalled' && <span className="ml-1 text-xs text-destructive">· recalled</span>}
    </span>
  );
}
