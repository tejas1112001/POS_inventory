import React from 'react';
import { Label } from '@/components/ui/label';
import { cn } from '@/lib/utils';

export default function FormField({
  label,
  required,
  htmlFor,
  hint,
  action,
  children,
  className,
}) {
  return (
    <div className={cn('space-y-1.5', className)}>
      {(label || action) && (
        <div className="flex items-center justify-between gap-2">
          {label && (
            <Label htmlFor={htmlFor} className="text-sm font-medium">
              {label}
              {required && <span className="ml-0.5 text-destructive">*</span>}
            </Label>
          )}
          {action}
        </div>
      )}
      {children}
      {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
    </div>
  );
}

export const formInputClass =
  'flex h-10 w-full rounded-lg border border-input bg-background px-3 py-2 text-sm shadow-xs transition-colors placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:border-primary disabled:cursor-not-allowed disabled:opacity-50';

export const formSelectClass = formInputClass;

export const formTextareaClass =
  'flex min-h-[80px] w-full rounded-lg border border-input bg-background px-3 py-2 text-sm shadow-xs transition-colors placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50 focus-visible:border-primary disabled:cursor-not-allowed disabled:opacity-50';

export const formGridClass = 'grid grid-cols-1 gap-5 md:grid-cols-2';

export function FormAddLink({ onClick, children = 'Add New' }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="text-xs font-medium text-primary hover:underline"
    >
      + {children}
    </button>
  );
}
