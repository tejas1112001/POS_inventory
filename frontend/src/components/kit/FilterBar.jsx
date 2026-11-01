import { useEffect, useState } from 'react';
import { Search, SlidersHorizontal, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';

// FilterBar: search plus select filters. Filters: [{ key, label, options:
// [{ value, label }] }]. On phones the filters move into a sheet behind a
// "Filters" button; active filters show as removable chips.
export default function FilterBar({ search, onSearch, placeholder = 'Search…', filters = [], values = {}, onFilter, onClear }) {
  const [text, setText] = useState(search || '');
  const [open, setOpen] = useState(false);
  useEffect(() => setText(search || ''), [search]);

  const active = filters.filter((f) => values[f.key]);
  const selects = (stack) =>
    filters.map((f) => (
      <label key={f.key} className={stack ? 'grid gap-1.5 text-sm font-medium' : undefined}>
        {stack && f.label}
        <select
          aria-label={f.label}
          className="h-11 rounded-md border bg-background px-3 text-sm sm:h-9"
          value={values[f.key] || ''}
          onChange={(e) => onFilter(f.key, e.target.value)}
        >
          <option value="">{f.allLabel || `All ${f.label.toLowerCase()}`}</option>
          {f.options.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
      </label>
    ));

  return (
    <div className="mb-3 space-y-2">
      <div className="flex gap-2">
        <div className="relative min-w-0 flex-1">
          <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
          <input
            type="search"
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              onSearch(e.target.value);
            }}
            placeholder={placeholder}
            aria-label={placeholder.replace(/…$/, '')}
            className="h-11 w-full rounded-md border bg-background pr-3 pl-9 text-base sm:h-9 sm:text-sm"
          />
        </div>
        {filters.length > 0 && (
          <>
            <div className="hidden gap-2 sm:flex">{selects(false)}</div>
            <Button type="button" variant="outline" className="h-11 sm:hidden" onClick={() => setOpen(true)}>
              <SlidersHorizontal className="size-4" aria-hidden="true" /> Filters{active.length ? ` (${active.length})` : ''}
            </Button>
          </>
        )}
      </div>
      {(active.length > 0 || search) && (
        <div className="flex flex-wrap items-center gap-2 text-sm">
          {active.map((f) => (
            <button
              key={f.key}
              type="button"
              onClick={() => onFilter(f.key, '')}
              className="inline-flex min-h-8 items-center gap-1 rounded-full bg-accent px-3 text-accent-foreground"
              aria-label={`Remove filter ${f.label}: ${f.options.find((o) => o.value === values[f.key])?.label}`}
            >
              {f.options.find((o) => o.value === values[f.key])?.label}
              <X className="size-3.5" aria-hidden="true" />
            </button>
          ))}
          <button type="button" onClick={onClear} className="min-h-8 px-1 text-primary underline-offset-2 hover:underline">
            Clear filters
          </button>
        </div>
      )}
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent sheet>
          <DialogHeader>
            <DialogTitle>Filters</DialogTitle>
          </DialogHeader>
          <div className="grid gap-3">{selects(true)}</div>
          <Button type="button" className="h-11" onClick={() => setOpen(false)}>
            Show results
          </Button>
        </DialogContent>
      </Dialog>
    </div>
  );
}
