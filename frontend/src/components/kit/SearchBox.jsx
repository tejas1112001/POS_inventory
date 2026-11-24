import { Search } from 'lucide-react';
import { cn } from '@/lib/utils';

// SearchBox: the one search field of list pages and the checkout. Same
// radius and icon everywhere. size 'sm' (filter lines): 40 px on phones,
// 36 px from 640 px; 'md' (checkout): 44 px / 40 px. onChange gets the
// text. Other props go to the <input>.
const HEIGHT = { sm: 'h-10 sm:h-9 sm:text-[13px]', md: 'h-11 sm:h-10 sm:text-sm' };

export default function SearchBox({ value, onChange, placeholder = 'Search…', label, size = 'md', className, ref, ...rest }) {
  return (
    <div className={cn('relative min-w-0', className)}>
      <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
      <input
        ref={ref}
        type="search"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        aria-label={label || placeholder.replace(/…$/, '')}
        className={cn('w-full rounded-lg border border-input bg-card pr-3 pl-9 text-base shadow-xs outline-none transition-[border-color,box-shadow] placeholder:text-muted-foreground focus-visible:border-primary focus-visible:ring-[3px] focus-visible:ring-ring/25', HEIGHT[size])}
        {...rest}
      />
    </div>
  );
}
