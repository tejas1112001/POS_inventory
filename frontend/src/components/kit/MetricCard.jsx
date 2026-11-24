import { Link } from 'react-router-dom';
import { cn } from '@/lib/utils';
import IconTile from './IconTile';

// MetricCard: a smaller figure card (icon, label, value, hint); a link
// when `to` is given.
export default function MetricCard({ icon, tone = 'muted', label, value, hint, to }) {
  const body = (
    <div className={cn('flex h-full items-center gap-3 rounded-2xl border bg-card p-4 shadow-xs transition-colors', to && 'hover:border-primary/40')}>
      {icon && <IconTile icon={icon} tone={tone} size="md" />}
      <div className="min-w-0">
        <p className="truncate text-xs font-medium text-muted-foreground">{label}</p>
        <p className="truncate text-lg font-bold text-navy tabular-nums">{value}</p>
        {hint && <p className="truncate text-xs text-muted-foreground">{hint}</p>}
      </div>
    </div>
  );
  return to ? (
    <Link to={to} className="block rounded-2xl focus-visible:outline-2 focus-visible:outline-ring">
      {body}
    </Link>
  ) : (
    body
  );
}
