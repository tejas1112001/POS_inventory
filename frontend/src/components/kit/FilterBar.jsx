import { useEffect, useMemo, useState } from 'react';
import SearchBox from './SearchBox';
import { FilterToolbar } from '@/components/ui/filter-toolbar';

// FilterBar: one line for every list page. A short search on the left, then
// "Add filter" (a menu: pick a field, then a value) with the applied filters
// as removable chips, then the page's buttons on the right.
// filters: [{ key, label, options: [{ value, label }] }]; values: { key: value }.
// extra: more controls (the date range button) next to Add filter;
// extraActive counts them as applied. actions: the page's buttons (New order, Add product…).
export default function FilterBar({ search, onSearch, placeholder = 'Search…', filters = [], values = {}, onFilter, onClear, extra, extraActive = 0, actions }) {
  const [text, setText] = useState(search || '');
  useEffect(() => setText(search || ''), [search]);

  const fieldKey = filters.map((f) => `${f.key}:${f.options.length}`).join('|');
  const chipKey = filters.map((f) => `${f.key}=${values[f.key] || ''}`).join('|');
  /* eslint-disable react-hooks/exhaustive-deps -- keyed by content: pages pass new arrays on every render */
  const fields = useMemo(
    () => filters.map((f) => ({ id: f.key, label: f.label, options: f.options.map((o) => ({ value: String(o.value), label: o.label })) })),
    [fieldKey]
  );
  const chips = useMemo(
    () =>
      filters
        .filter((f) => values[f.key])
        .map((f) => ({ id: f.key, label: f.label, value: f.options.find((o) => String(o.value) === String(values[f.key]))?.label ?? String(values[f.key]) })),
    [chipKey, fieldKey]
  );
  /* eslint-enable react-hooks/exhaustive-deps */

  return (
    <div className="mb-3 flex flex-wrap items-center gap-2">
      <SearchBox
        size="sm"
        className="w-full sm:w-56 lg:w-64"
        value={text}
        onChange={(v) => {
          setText(v);
          onSearch(v);
        }}
        placeholder={placeholder}
      />
      {(fields.length > 0 || extra) && (
        <FilterToolbar
          filters={chips}
          onRemove={(id) => onFilter(id, '')}
          onClearAll={onClear}
          addFilter={fields.length ? { fields, onAdd: (chip) => onFilter(chip.id, chip.option), align: 'start' } : undefined}
        >
          {extra}
          {extraActive > 0 && chips.length === 0 && (
            <button type="button" onClick={onClear} className="h-8 rounded-md px-1.5 text-[13px] font-semibold text-primary hover:underline">
              Clear all
            </button>
          )}
        </FilterToolbar>
      )}
      {actions && <div className="flex flex-wrap items-center gap-2 max-sm:w-full max-sm:[&>*]:flex-1 sm:ml-auto sm:[&>*]:h-9 sm:[&>*]:text-[13px]">{actions}</div>}
    </div>
  );
}
