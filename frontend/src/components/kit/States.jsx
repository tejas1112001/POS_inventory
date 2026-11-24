import { AlertCircle, Inbox, RefreshCw } from 'lucide-react';
import { Button } from '@/components/ui/button';
import IconTile from './IconTile';

export function EmptyState({ title = 'Nothing here yet', children, action }) {
  return (
    <div className="flex flex-col items-center px-4 py-12 text-center">
      <IconTile icon={Inbox} size="lg" className="mb-3" />
      <p className="font-semibold text-navy">{title}</p>
      {children && <p className="mt-1 max-w-sm text-sm text-muted-foreground">{children}</p>}
      {action && <div className="mt-4">{action}</div>}
    </div>
  );
}

export function ErrorState({ message, onRetry }) {
  return (
    <div role="alert" className="flex flex-col items-center px-4 py-12 text-center">
      <IconTile icon={AlertCircle} tone="danger" size="lg" className="mb-3" />
      <p className="font-semibold text-navy">The list could not be loaded</p>
      <p className="mt-1 max-w-sm text-sm text-muted-foreground">{message}</p>
      {onRetry && (
        <Button type="button" variant="outline" className="mt-4" onClick={onRetry}>
          <RefreshCw className="size-4" aria-hidden="true" /> Try again
        </Button>
      )}
    </div>
  );
}

// LoadingRows: skeleton rows while a table loads (announced once).
export function LoadingRows({ rows = 5 }) {
  return (
    <div className="divide-y" aria-busy="true" aria-label="Loading">
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="flex items-center gap-3 px-4 py-3">
          <div className="size-8 animate-pulse rounded-md bg-muted" />
          <div className="h-3 flex-1 animate-pulse rounded bg-muted" />
          <div className="hidden h-3 w-24 animate-pulse rounded bg-muted sm:block" />
        </div>
      ))}
    </div>
  );
}
