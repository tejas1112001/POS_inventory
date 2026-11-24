import { Link } from 'react-router-dom';
import { ArrowRight } from 'lucide-react';
import { formatNumber } from '@/lib/format';
import IconTile from './IconTile';

const TONE = { info: 'teal', warn: 'warn', danger: 'danger' };

// ActionCard: something that needs attention, linking to where it is fixed.
export default function ActionCard({ icon, tone = 'info', title, value, text, to }) {
  return (
    <Link to={to} className="group flex items-center gap-3 rounded-2xl border bg-card p-3.5 shadow-xs transition-colors hover:border-primary/40 focus-visible:outline-2 focus-visible:outline-ring">
      <IconTile icon={icon} tone={TONE[tone] || 'teal'} />
      <span className="min-w-0 flex-1">
        <span className="block text-sm font-semibold text-navy">{title}</span>
        <span className="block truncate text-xs text-muted-foreground">
          {value != null && <span className="font-semibold text-foreground tabular-nums">{formatNumber(value, 0)} </span>}
          {text}
        </span>
      </span>
      <ArrowRight className="size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5" aria-hidden="true" />
    </Link>
  );
}
