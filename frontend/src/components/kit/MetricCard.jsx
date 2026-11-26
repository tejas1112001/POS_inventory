import { Link } from 'react-router-dom';
import { cn } from '@/lib/utils';
import IconTile from './IconTile';

// MetricCard: a figure card (icon, label, value, hint) the same size as
// StatCard, so rows of cards line up; a link when `to` is given.
export default function MetricCard({ icon, tone = 'muted', label, value, hint, to }) {
  const body = (
    <div className={cn('flex h-full items-center gap-3.5 rounded-2xl border bg-card p-4 shadow-xs transition-colors', to && 'hover:border-primary/40')}>
      {icon && <IconTile icon={icon} tone={tone} size="lg" />}
      <div className="min-w-0 flex-1 space-y-1">
        <p className="truncate text-xs font-medium text-muted-foreground sm:text-sm">{label}</p>
        <p className="truncate text-xl font-extrabold tracking-tight text-navy tabular-nums sm:text-2xl">{value}</p>
        {hint && <p className="truncate text-xs text-muted-foreground">{hint}</p>}
      </div>
    </div>
  );
  return to ? (
    <Link to={to} className="block h-full rounded-2xl focus-visible:outline-2 focus-visible:outline-ring">
      {body}
    </Link>
  ) : (
    body
  );
}
