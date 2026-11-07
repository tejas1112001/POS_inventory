import { ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight } from 'lucide-react';
import { cn } from '@/lib/utils';

// pageList: page numbers to show, with null for a gap (1 … 4 5 6 … 12).
function pageList(page, pages) {
  if (pages <= 7) return Array.from({ length: pages }, (_, i) => i + 1);
  const set = new Set([1, pages, page - 1, page, page + 1].filter((p) => p >= 1 && p <= pages));
  const sorted = [...set].sort((a, b) => a - b);
  const out = [];
  sorted.forEach((p, i) => {
    if (i && p - sorted[i - 1] > 1) out.push(null);
    out.push(p);
  });
  return out;
}

const btn =
  'inline-flex size-11 items-center justify-center rounded-md border bg-card text-sm font-medium tabular-nums hover:bg-muted disabled:pointer-events-none disabled:opacity-40 sm:size-8';

// Pagination: "1–25 of N", rows per page, first / previous / page numbers /
// next / last. One component for every list (server pages or client pages).
export default function Pagination({ label = 'List', total, page, size, pageSizes = [10, 25, 50], onPage, onSize, className }) {
  const pages = Math.max(1, Math.ceil(total / size));
  const first = total ? (page - 1) * size + 1 : 0;
  const last = Math.min(page * size, total);
  return (
    <nav aria-label={`${label} pages`} className={cn('flex shrink-0 flex-wrap items-center justify-between gap-2 border-t bg-card px-3 py-2 text-sm', className)}>
      <div className="flex items-center gap-3">
        <p className="tabular-nums text-muted-foreground" aria-live="polite">
          {first}–{last} of {total}
        </p>
        {onSize && (
          <label className="hidden items-center gap-2 text-muted-foreground sm:flex">
            Rows
            <select className="h-8 rounded-md border bg-card px-2 text-foreground" value={size} onChange={(e) => onSize(Number(e.target.value))}>
              {pageSizes.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
          </label>
        )}
      </div>
      <div className="flex items-center gap-1">
        <button type="button" className={cn(btn, 'hidden sm:inline-flex')} disabled={page <= 1} onClick={() => onPage(1)} aria-label="First page">
          <ChevronsLeft className="size-4" aria-hidden="true" />
        </button>
        <button type="button" className={btn} disabled={page <= 1} onClick={() => onPage(page - 1)} aria-label="Previous page">
          <ChevronLeft className="size-4" aria-hidden="true" />
        </button>
        <span className="px-2 tabular-nums sm:hidden">
          {page} / {pages}
        </span>
        <span className="hidden items-center gap-1 sm:flex">
          {pageList(page, pages).map((p, i) =>
            p === null ? (
              <span key={`gap-${i}`} className="px-1 text-muted-foreground" aria-hidden="true">
                …
              </span>
            ) : (
              <button
                key={p}
                type="button"
                className={cn(btn, p === page && 'border-primary bg-primary text-primary-foreground hover:bg-primary')}
                aria-current={p === page ? 'page' : undefined}
                aria-label={`Page ${p}`}
                onClick={() => onPage(p)}
              >
                {p}
              </button>
            )
          )}
        </span>
        <button type="button" className={btn} disabled={page >= pages} onClick={() => onPage(page + 1)} aria-label="Next page">
          <ChevronRight className="size-4" aria-hidden="true" />
        </button>
        <button type="button" className={cn(btn, 'hidden sm:inline-flex')} disabled={page >= pages} onClick={() => onPage(pages)} aria-label="Last page">
          <ChevronsRight className="size-4" aria-hidden="true" />
        </button>
      </div>
    </nav>
  );
}
