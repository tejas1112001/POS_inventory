import { ArrowDownRight, ArrowUpRight } from 'lucide-react';
import { cn } from '@/lib/utils';

// Delta: a change such as +12.4% (green, arrow up) or -2.1% (red, arrow
// down) with an optional hint. The arrow and the sign carry the meaning,
// not only the colour.
export default function Delta({ delta, hint }) {
  if (!delta && !hint) return null;
  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs">
      {delta && (
        <span className={cn('inline-flex items-center gap-0.5 font-semibold tabular-nums', delta.up ? 'text-success' : 'text-danger')}>
          {delta.up ? <ArrowUpRight className="size-3.5" aria-hidden="true" /> : <ArrowDownRight className="size-3.5" aria-hidden="true" />}
          {delta.text}
        </span>
      )}
      {hint && <span className="text-muted-foreground">{hint}</span>}
    </div>
  );
}
