import { useEffect, useState } from 'react';
import { Search, SlidersHorizontal, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';

// FilterBar: search plus select filters. Filters: [{ key, label, options:
// [{ value, label }] }]. On phones the filters move into a sheet behind a
// "Filters" button; active filters show as removable chips. extra: more
// controls (for example date boxes), shown next to the selects and in the
// phone sheet; extraActive counts them as active filters. actions: the
// page's buttons (New order, Add product…), on the same line at the right;
// on phones they take their own row under the search.
export default function FilterBar({ search, onSearch, placeholder = 'Search…', filters = [], values = {}, onFilter, onClear, extra, extraActive = 0, actions }) {
  const [text, setText] = useState(search || '');
  const [open, setOpen] = useState(false);
  useEffect(() => setText(search || ''), [search]);

  const active = filters.filter((f) => values[f.key]);
  const activeCount = active.length + extraActive;
  const selects = (stack) =>
    filters.map((f) => (
      <label key={f.key} className={stack ? 'grid gap-1.5 text-sm font-medium' : undefined}>
        {stack && f.label}
        <select
          aria-label={f.label}
          className="h-11 rounded-lg border border-input bg-card px-3 text-sm shadow-xs sm:h-10"
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
      <div className="flex flex-wrap items-center gap-2">
        <div className="relative min-w-0 flex-1 sm:min-w-[14rem]">
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
            className="h-11 w-full rounded-lg border border-input bg-card pr-3 pl-9 text-base shadow-xs outline-none transition-[border-color,box-shadow] focus-visible:border-primary focus-visible:ring-[3px] focus-visible:ring-ring/25 sm:h-10 sm:text-sm"
          />
        </div>
        {(filters.length > 0 || extra) && (
          <>
            <div className="hidden flex-wrap gap-2 sm:flex">
              {selects(false)}
              {extra}
            </div>
            <Button type="button" variant="outline" className="h-11 sm:hidden" onClick={() => setOpen(true)}>
              <SlidersHorizontal className="size-4" aria-hidden="true" /> Filters{activeCount ? ` (${activeCount})` : ''}
            </Button>
          </>
        )}
        {actions && <div className="flex flex-wrap items-center gap-2 max-sm:w-full max-sm:[&>*]:flex-1 sm:ml-auto">{actions}</div>}
      </div>
      {(activeCount > 0 || search) && (
        <div className="flex flex-wrap items-center gap-2 text-sm">
          {active.map((f) => (
            <button
              key={f.key}
              type="button"
              onClick={() => onFilter(f.key, '')}
              className="inline-flex min-h-8 items-center gap-1 rounded-full bg-accent px-3 font-medium text-accent-foreground"
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
          <div className="grid gap-3">
            {selects(true)}
            {extra && <div className="grid gap-3">{extra}</div>}
          </div>
          <Button type="button" className="h-11" onClick={() => setOpen(false)}>
            Show results
          </Button>
        </DialogContent>
      </Dialog>
    </div>
  );
}
