import { ChevronLeft, ChevronRight } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import { EmptyState, ErrorState, LoadingRows } from './States';

// DataTable: one page of server data. Columns:
//   { id, header, cell: (row) => node, card: 'title' | 'subtitle' | 'meta' | 'hidden', align: 'right', className }
// From 640 px it is a table; on phones every row is a card (title,
// subtitle, then the "meta" columns as label/value pairs).
export default function DataTable({
  label,
  columns,
  rows,
  rowKey = (r) => r.id,
  actions,
  loading,
  error,
  onRetry,
  empty,
  total = 0,
  page = 1,
  size = 25,
  pageSizes = [10, 25, 50],
  onPage,
  onSize,
}) {
  const body = () => {
    if (error) return <ErrorState message={error} onRetry={onRetry} />;
    if (loading && !rows.length) return <LoadingRows />;
    if (!rows.length) return empty || <EmptyState />;
    return (
      <>
        {/* Table: 640 px and up */}
        <div className="hidden overflow-x-auto sm:block">
          <table className="w-full text-sm" aria-label={label} aria-busy={loading || undefined}>
            <thead className="sticky top-0 z-[1] bg-muted/60 text-left text-xs font-medium uppercase tracking-wide text-muted-foreground">
              <tr>
                {columns.map((c) => (
                  <th key={c.id} scope="col" className={cn('px-4 py-2.5 font-medium', c.align === 'right' && 'text-right', c.className)}>
                    {c.header}
                  </th>
                ))}
                {actions && (
                  <th scope="col" className="w-12 px-2">
                    <span className="sr-only">Actions</span>
                  </th>
                )}
              </tr>
            </thead>
            <tbody className="divide-y">
              {rows.map((r) => (
                <tr key={rowKey(r)} className="hover:bg-muted/40">
                  {columns.map((c) => (
                    <td key={c.id} className={cn('px-4 py-2.5 align-middle', c.align === 'right' && 'text-right tabular-nums', c.className)}>
                      {c.cell(r)}
                    </td>
                  ))}
                  {actions && <td className="px-2 text-right">{actions(r)}</td>}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {/* Cards: phones */}
        <ul className="divide-y sm:hidden" aria-label={label}>
          {rows.map((r) => {
            const title = columns.find((c) => c.card === 'title');
            const subtitle = columns.find((c) => c.card === 'subtitle');
            const meta = columns.filter((c) => c.card === 'meta');
            return (
              <li key={rowKey(r)} className="flex gap-2 px-4 py-3">
                <div className="min-w-0 flex-1">
                  {title && <div className="font-medium text-foreground">{title.cell(r)}</div>}
                  {subtitle && <div className="text-sm text-muted-foreground">{subtitle.cell(r)}</div>}
                  {meta.length > 0 && (
                    <dl className="mt-1.5 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
                      {meta.map((c) => (
                        <div key={c.id} className="contents">
                          <dt className="text-muted-foreground">{c.header}</dt>
                          <dd className="min-w-0 truncate">{c.cell(r)}</dd>
                        </div>
                      ))}
                    </dl>
                  )}
                </div>
                {actions && <div className="-mr-2 shrink-0">{actions(r)}</div>}
              </li>
            );
          })}
        </ul>
      </>
    );
  };

  const first = total ? (page - 1) * size + 1 : 0;
  const last = Math.min(page * size, total);
  const pages = Math.max(1, Math.ceil(total / size));

  return (
    <section className="overflow-hidden rounded-lg border bg-card">
      {body()}
      {!error && total > 0 && (
        <nav aria-label={`${label} pages`} className="flex flex-wrap items-center justify-between gap-2 border-t px-4 py-2.5 text-sm">
          <p className="text-muted-foreground" aria-live="polite">
            {first}–{last} of {total}
          </p>
          <div className="flex items-center gap-2">
            <label className="hidden items-center gap-2 text-muted-foreground sm:flex">
              Rows
              <select
                className="h-8 rounded-md border bg-background px-2 text-foreground"
                value={size}
                onChange={(e) => onSize?.(Number(e.target.value))}
              >
                {pageSizes.map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
              </select>
            </label>
            <Button type="button" variant="outline" size="icon" className="size-11 sm:size-8" disabled={page <= 1} onClick={() => onPage?.(page - 1)} aria-label="Previous page">
              <ChevronLeft className="size-4" aria-hidden="true" />
            </Button>
            <span className="tabular-nums">
              {page} / {pages}
            </span>
            <Button type="button" variant="outline" size="icon" className="size-11 sm:size-8" disabled={page >= pages} onClick={() => onPage?.(page + 1)} aria-label="Next page">
              <ChevronRight className="size-4" aria-hidden="true" />
            </Button>
          </div>
        </nav>
      )}
    </section>
  );
}
