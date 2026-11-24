import { Search } from 'lucide-react';
import { cn } from '@/lib/utils';

// SearchBox: the one search field of list pages and the checkout. Same
// height, radius and icon everywhere; 44 px tall on phones (touch), 40 px
// from 640 px. onChange gets the text. Other props go to the <input>.
export default function SearchBox({ value, onChange, placeholder = 'Search…', label, className, ref, ...rest }) {
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
        className="h-11 w-full rounded-lg border border-input bg-card pr-3 pl-9 text-base shadow-xs outline-none transition-[border-color,box-shadow] placeholder:text-muted-foreground focus-visible:border-primary focus-visible:ring-[3px] focus-visible:ring-ring/25 sm:h-10 sm:text-sm"
        {...rest}
      />
    </div>
  );
}
