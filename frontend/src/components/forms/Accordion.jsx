import React from 'react';
import { ChevronDown } from 'lucide-react';
import { cn } from '@/lib/utils';

const Accordion = ({
  title,
  isOpen,
  onToggle,
  children,
  icon: Icon,
  description,
}) => (
  <div className="mb-4 overflow-hidden rounded-xl border bg-card shadow-sm last:mb-0">
    <button
      type="button"
      className={cn(
        'flex w-full items-center justify-between gap-3 px-4 py-3.5 text-left transition-colors hover:bg-muted/50 sm:px-5',
        isOpen && 'border-b bg-muted/30'
      )}
      onClick={onToggle}
      aria-expanded={isOpen}
    >
      <div className="flex min-w-0 items-center gap-3">
        {Icon && (
          <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
            <Icon className="size-4" />
          </span>
        )}
        <div className="min-w-0">
          <span className="block text-sm font-semibold text-foreground">{title}</span>
          {description && (
            <span className="block truncate text-xs text-muted-foreground">{description}</span>
          )}
        </div>
      </div>
      <ChevronDown
        className={cn(
          'size-4 shrink-0 text-muted-foreground transition-transform duration-200',
          isOpen && 'rotate-180'
        )}
      />
    </button>

    {isOpen && (
      <div className="p-4 sm:p-5">
        {children}
      </div>
    )}
  </div>
);

export default Accordion;
