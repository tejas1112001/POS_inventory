import { ChevronLeft, ChevronRight } from 'lucide-react';
import { DayPicker } from 'react-day-picker';
import { cn } from '@/lib/utils';

const navButton =
  'inline-flex size-8 items-center justify-center rounded-md border border-input bg-card text-foreground transition-colors hover:bg-accent hover:text-accent-foreground disabled:pointer-events-none disabled:opacity-40';

// Calendar: react-day-picker with the app theme (teal range, cream
// surface; light and dark). Takes every DayPicker prop.
function Calendar({ className, classNames, showOutsideDays = true, ...props }) {
  return (
    <DayPicker
      showOutsideDays={showOutsideDays}
      weekStartsOn={1}
      className={cn('p-3', className)}
      classNames={{
        months: 'relative flex flex-col gap-4 sm:flex-row',
        month: 'space-y-3',
        month_caption: 'flex h-8 items-center justify-center',
        caption_label: 'text-sm font-semibold',
        nav: 'absolute inset-x-0 top-0 z-10 flex h-8 items-center justify-between',
        button_previous: navButton,
        button_next: navButton,
        month_grid: 'w-full border-collapse',
        weekdays: 'flex',
        weekday: 'w-9 text-center text-[0.75rem] font-medium text-muted-foreground',
        week: 'mt-1 flex w-full',
        day: 'relative size-9 p-0 text-center text-sm',
        day_button:
          'inline-flex size-9 items-center justify-center rounded-md font-medium tabular-nums transition-colors outline-none hover:bg-accent hover:text-accent-foreground focus-visible:ring-2 focus-visible:ring-ring',
        selected: '[&>button]:bg-primary [&>button]:text-primary-foreground [&>button]:hover:bg-primary [&>button]:hover:text-primary-foreground',
        range_start: 'rounded-l-md bg-accent',
        range_end: 'rounded-r-md bg-accent',
        range_middle: 'rounded-none bg-accent [&>button]:!bg-transparent [&>button]:!text-accent-foreground [&>button]:rounded-none',
        today: '[&>button]:ring-1 [&>button]:ring-primary/50',
        outside: '[&>button]:text-muted-foreground/60',
        disabled: '[&>button]:pointer-events-none [&>button]:opacity-35',
        hidden: 'invisible',
        ...classNames,
      }}
      components={{
        Chevron: ({ orientation, className: c }) =>
          orientation === 'left' ? <ChevronLeft className={cn('size-4', c)} aria-hidden="true" /> : <ChevronRight className={cn('size-4', c)} aria-hidden="true" />,
      }}
      {...props}
    />
  );
}

export { Calendar };
