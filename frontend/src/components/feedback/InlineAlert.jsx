import { AlertCircle, AlertTriangle, CheckCircle2, Info, X } from 'lucide-react';
import { cn } from '@/lib/utils';

const KINDS = {
  error: { icon: AlertCircle, cls: 'border-danger/40 bg-danger-bg text-danger-fg', role: 'alert' },
  warning: { icon: AlertTriangle, cls: 'border-warning/40 bg-warning-bg text-warning-fg', role: 'alert' },
  success: { icon: CheckCircle2, cls: 'border-success/40 bg-success-bg text-success-fg', role: 'status' },
  info: { icon: Info, cls: 'border-info/40 bg-info-bg text-info-fg', role: 'status' },
};

// InlineAlert: a message that stays on the page (a form that cannot save,
// a page-level state). Field problems go under the field (FieldError).
export function InlineAlert({ kind = 'error', title, children, onClose, className }) {
  const k = KINDS[kind] || KINDS.error;
  const Icon = k.icon;
  return (
    <div role={k.role} className={cn('flex items-start gap-2 rounded-md border px-3 py-2 text-sm', k.cls, className)}>
      <Icon className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
      <div className="min-w-0 flex-1">
        {title && <p className="font-semibold">{title}</p>}
        {children && <div className={title ? 'mt-0.5' : undefined}>{children}</div>}
      </div>
      {onClose && (
        <button type="button" onClick={onClose} aria-label="Dismiss message" className="-m-2 flex size-11 shrink-0 items-center justify-center rounded-md opacity-70 hover:opacity-100 sm:size-8">
          <X className="size-4" aria-hidden="true" />
        </button>
      )}
    </div>
  );
}

// FieldError: the problem with one field, linked to it by id
// (aria-describedby on the input, aria-invalid set by the caller).
export function FieldError({ id, children }) {
  if (!children) return null;
  return (
    <p id={id} className="mt-1 text-xs font-medium text-danger">
      {children}
    </p>
  );
}
