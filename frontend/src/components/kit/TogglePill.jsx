import { cn } from '@/lib/utils';

const TONES = {
  teal: 'bg-teal-soft text-primary hover:bg-teal-soft/70 dark:text-teal',
  amber: 'bg-amber-soft text-[#7a4a05] hover:bg-amber-soft/70 dark:text-amber-brand',
};

// TogglePill: a small on/off switch shown as a pill (Published / Hidden,
// Featured / Standard). aria-pressed tells screen readers its state.
export default function TogglePill({ on, onClick, onLabel, offLabel, icon: Icon, offIcon: OffIcon, tone = 'teal', label }) {
  const Shown = on ? Icon : OffIcon || Icon;
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={on}
      aria-label={label}
      className={cn(
        'inline-flex min-h-8 items-center gap-1.5 rounded-full px-3 text-xs font-semibold transition-colors',
        on ? TONES[tone] : 'bg-muted text-muted-foreground hover:bg-muted/70'
      )}
    >
      {Shown && <Shown className="size-3.5" aria-hidden="true" />}
      {on ? onLabel : offLabel}
    </button>
  );
}
