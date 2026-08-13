import React from 'react';
import { cn } from '@/lib/utils';

export default function ListHeader({ title, description, controlButtons, primaryButtons, extraElement, className }) {
  return (
    <div className={cn('rounded-xl border bg-card p-4 shadow-sm', className)}>
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-foreground">{title}</h1>
          {description && <p className="mt-1 text-sm text-muted-foreground">{description}</p>}
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {controlButtons && (
            <div className="flex items-center gap-2">{controlButtons}</div>
          )}
          {primaryButtons && (
            <div className="flex items-center gap-2">{primaryButtons}</div>
          )}
          {extraElement}
        </div>
      </div>
    </div>
  );
}
