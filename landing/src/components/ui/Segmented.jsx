import { cn } from '@/lib/cn';

/**
 * Two-to-four option toggle (aria-pressed buttons in a labelled group).
 * options: [{ value, label, extra? }]
 */
export function Segmented({ label, options, value, onChange, size = 'md', className }) {
  return (
    <div role="group" aria-label={label} className={cn('inline-flex rounded-xl bg-slate-100 p-1', className)}>
      {options.map((o) => {
        const on = o.value === value;
        return (
          <button
            key={o.value}
            type="button"
            aria-pressed={on}
            onClick={() => onChange(o.value)}
            className={cn(
              'rounded-[9px] font-semibold transition-colors duration-150',
              size === 'lg' ? 'min-h-11 px-[22px] text-[15px]' : 'min-h-10 px-3.5 text-sm',
              on ? 'bg-white font-bold text-ink shadow-sm' : 'text-body hover:text-ink',
            )}
          >
            {o.label}
            {o.extra}
          </button>
        );
      })}
    </div>
  );
}
