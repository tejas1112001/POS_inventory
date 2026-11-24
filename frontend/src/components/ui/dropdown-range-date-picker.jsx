import { useState } from 'react';
import { format, isValid, parseISO } from 'date-fns';
import { Calendar as CalendarIcon } from 'lucide-react';
import { Calendar } from '@/components/ui/calendar';
import { Button } from '@/components/ui/button';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { cn } from '@/lib/utils';

const toDate = (s) => {
  if (!s) return undefined;
  const d = parseISO(s);
  return isValid(d) ? d : undefined;
};
const toText = (d) => (d ? format(d, 'yyyy-MM-dd') : '');
const MONTHS = Array.from({ length: 12 }, (_, i) => format(new Date(2000, i, 1), 'MMMM'));

function rangeLabel(from, to) {
  if (!to || toText(to) === toText(from)) return format(from, 'd MMM yyyy');
  if (from.getFullYear() === to.getFullYear()) return `${format(from, 'd MMM')} – ${format(to, 'd MMM yyyy')}`;
  return `${format(from, 'd MMM yyyy')} – ${format(to, 'd MMM yyyy')}`;
}

/*
 * DropdownRangeDatePicker: one button that opens a year and month picker
 * over a range calendar, with Clear and Apply.
 * value: { from, to } as 'YYYY-MM-DD' ('' or missing = not set).
 * onChange({ from, to }): on Apply (a single day gives from = to) and on Clear.
 * min / max: 'YYYY-MM-DD' bounds. clearable: false hides Clear (a range is
 * required). placeholder: the button text with no range.
 */
function DropdownRangeDatePicker({ value, onChange, placeholder = 'Pick a date range', label = 'Date range', min, max, clearable = true, align = 'start', className }) {
  const from = toDate(value?.from);
  const to = toDate(value?.to);
  const minDate = toDate(min);
  const maxDate = toDate(max);
  const today = new Date();
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState(undefined);
  const [month, setMonth] = useState(today);

  const firstYear = minDate ? minDate.getFullYear() : today.getFullYear() - 10;
  const lastYear = maxDate ? maxDate.getFullYear() : today.getFullYear() + 1;
  const years = Array.from({ length: lastYear - firstYear + 1 }, (_, i) => lastYear - i);

  const onOpenChange = (next) => {
    if (next) {
      // Start from the applied range, shown at its first month.
      setDraft(from ? { from, to } : undefined);
      setMonth(from ?? (maxDate && maxDate < today ? maxDate : today));
    }
    setOpen(next);
  };
  const apply = () => {
    if (!draft?.from) return;
    onChange({ from: toText(draft.from), to: toText(draft.to ?? draft.from) });
    setOpen(false);
  };
  const clear = () => {
    setDraft(undefined);
    onChange({ from: '', to: '' });
    setOpen(false);
  };

  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          aria-label={from ? `${label}: ${rangeLabel(from, to)}` : label}
          className={cn('h-11 justify-start gap-2 px-3 font-medium sm:h-10', !from && 'text-muted-foreground', className)}
        >
          <CalendarIcon className="size-4 shrink-0" aria-hidden="true" />
          <span className="truncate">{from ? rangeLabel(from, to) : placeholder}</span>
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-auto max-w-[calc(100vw-1.5rem)] p-3" align={align}>
        <div className="space-y-3">
          <div className="flex gap-2">
            <Select value={String(month.getFullYear())} onValueChange={(y) => setMonth(new Date(Number(y), month.getMonth(), 1))}>
              <SelectTrigger className="h-9 w-[104px]" aria-label="Year">
                <SelectValue placeholder="Year" />
              </SelectTrigger>
              <SelectContent className="max-h-64">
                {years.map((y) => (
                  <SelectItem key={y} value={String(y)}>
                    {y}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={String(month.getMonth())} onValueChange={(m) => setMonth(new Date(month.getFullYear(), Number(m), 1))}>
              <SelectTrigger className="h-9 flex-1" aria-label="Month">
                <SelectValue placeholder="Month" />
              </SelectTrigger>
              <SelectContent className="max-h-64">
                {MONTHS.map((name, i) => (
                  <SelectItem key={name} value={String(i)}>
                    {name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <Calendar
            mode="range"
            selected={draft}
            onSelect={setDraft}
            month={month}
            onMonthChange={setMonth}
            startMonth={new Date(firstYear, 0, 1)}
            endMonth={new Date(lastYear, 11, 1)}
            disabled={[minDate && { before: minDate }, maxDate && { after: maxDate }].filter(Boolean)}
            className="rounded-lg border bg-card"
          />

          <div className="flex items-center justify-between gap-2">
            {clearable ? (
              <Button type="button" size="sm" variant="ghost" onClick={clear} disabled={!from && !draft}>
                Clear
              </Button>
            ) : (
              <span />
            )}
            <Button type="button" size="sm" onClick={apply} disabled={!draft?.from}>
              Apply
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}

export { DropdownRangeDatePicker };
