import { cn } from '@/lib/utils';

// SegmentedControl: a row of toggle buttons with one pressed (date ranges,
// chart metric). options: [{ value, label }].
export default function SegmentedControl({ options, value, onChange, label, className }) {
  return (
    <div className={cn('no-scrollbar flex max-w-full gap-1 overflow-x-auto rounded-xl border bg-muted p-1', className)} role="group" aria-label={label}>
      {options.map((o) => {
        const on = o.value === value;
        return (
          <button
            key={o.value}
            type="button"
            aria-pressed={on}
            onClick={() => onChange(o.value)}
            className={cn(
              'min-h-9 shrink-0 rounded-lg px-3 text-sm font-semibold transition-colors duration-150 focus-visible:outline-2 focus-visible:outline-ring',
              on ? 'bg-card text-primary shadow-xs dark:bg-primary dark:text-white' : 'text-muted-foreground hover:text-foreground'
            )}
          >
            {o.label}
          </button>
        );
      })}
    </div>
  );
}
