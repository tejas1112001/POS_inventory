import { cn } from '@/lib/utils';

// ListPageShell: the page column under the top bar. scroll: the whole page
// scrolls (report tabs with cards above the table), so the table always
// keeps a usable height instead of being squeezed to a row or two.
export default function ListPageShell({ children, scroll = false }) {
  return (
    <div className={cn('flex min-h-0 flex-1 flex-col', scroll ? 'overflow-y-auto overflow-x-hidden pb-4 scrollbar-thin' : 'overflow-hidden')}>
      {children}
    </div>
  );
}
