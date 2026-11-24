import { Link } from 'react-router-dom';
import { ArrowRight } from 'lucide-react';
import { cn } from '@/lib/utils';

// Panel: a titled card (moodboard dashboard panels). id labels the section.
export default function Panel({ title, id, action, description, className, children }) {
  return (
    <section className={cn('flex min-w-0 flex-col rounded-2xl border bg-card p-4 shadow-xs', className)} aria-labelledby={id}>
      <div className="mb-3 flex min-h-9 items-center justify-between gap-2">
        <div className="min-w-0">
          <h2 id={id} className="truncate text-base font-bold">
            {title}
          </h2>
          {description && <p className="truncate text-xs text-muted-foreground">{description}</p>}
        </div>
        {action}
      </div>
      {children}
    </section>
  );
}

// ViewAllLink: the "View all" link in a panel header.
export function ViewAllLink({ to, label, children = 'View all' }) {
  return (
    <Link to={to} className="inline-flex min-h-9 shrink-0 items-center gap-1 rounded-lg px-2 text-sm font-semibold text-primary transition-colors hover:bg-accent" aria-label={label}>
      {children} <ArrowRight className="size-3.5" aria-hidden="true" />
    </Link>
  );
}
