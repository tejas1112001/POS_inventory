import { cn } from '@/lib/utils';

// QuickButton: a small secondary tile button (quick cash amounts, keypad).
export default function QuickButton({ className, ...rest }) {
  return (
    <button
      type="button"
      className={cn(
        'min-h-10 rounded-lg border bg-card text-sm font-semibold tabular-nums transition-colors hover:border-primary/40 hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring',
        className
      )}
      {...rest}
    />
  );
}
