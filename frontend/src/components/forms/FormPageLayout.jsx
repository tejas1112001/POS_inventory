import React from 'react';
import { cn } from '@/lib/utils';

export default function FormPageLayout({ children, className }) {
  return (
    <div className={cn('form-page flex h-full flex-col gap-2 p-3 sm:p-4', className)}>
      {children}
    </div>
  );
}

export function FormCard({ children, className }) {
  return (
    <div
      className={cn(
        'flex min-h-0 flex-1 flex-col rounded-lg border bg-card shadow-sm',
        className
      )}
    >
      {children}
    </div>
  );
}

export function FormBody({ children, className }) {
  return (
    <div className={cn('flex-1 min-h-0 overflow-y-auto p-3 sm:p-4 scrollbar-thin', className)}>
      {children}
    </div>
  );
}
