import { cn } from '@/lib/utils';

// ChoiceButton: one option of a radio group drawn as a tile (payment
// method and similar). The chosen one is filled with the primary gradient.
export default function ChoiceButton({ selected, icon: Icon, children, className, ...rest }) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      className={cn(
        'flex min-h-12 flex-col items-center justify-center gap-0.5 rounded-xl border text-sm font-semibold transition-colors duration-150 focus-visible:outline-2 focus-visible:outline-ring',
        selected ? 'border-transparent bg-primary bg-primary-gradient text-primary-foreground shadow-xs' : 'bg-card hover:border-primary/40 hover:bg-accent',
        className
      )}
      {...rest}
    >
      {Icon && <Icon className="size-4" aria-hidden="true" />}
      {children}
    </button>
  );
}
