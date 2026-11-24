import { Minus, Plus } from 'lucide-react';
import { Button } from '@/components/ui/button';

// QtyStepper: [ minus | quantity | plus ] (moodboard cart). children
// replaces the middle value (for example a box to type the quantity).
export default function QtyStepper({ value, onDecrease, onIncrease, increaseDisabled, decreaseLabel = 'One less', increaseLabel = 'One more', children }) {
  return (
    <div className="flex items-center rounded-lg border bg-card shadow-xs">
      <Button variant="ghost" size="icon" className="size-9 rounded-r-none" aria-label={decreaseLabel} onClick={onDecrease}>
        <Minus className="size-3.5" aria-hidden="true" />
      </Button>
      {children ?? (
        <span className="w-9 border-x text-center text-sm leading-9 font-bold tabular-nums" aria-label="Quantity">
          {value}
        </span>
      )}
      <Button variant="ghost" size="icon" className="size-9 rounded-l-none" aria-label={increaseLabel} disabled={increaseDisabled} onClick={onIncrease}>
        <Plus className="size-3.5" aria-hidden="true" />
      </Button>
    </div>
  );
}
