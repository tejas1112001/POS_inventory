import { Loader2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import { fieldCls } from './formUtils';

// Small form pieces shared by the settings sections.

export function Field({ label, htmlFor, hint, error, children, className }) {
  return (
    <div className={cn('space-y-1.5', className)}>
      <label htmlFor={htmlFor} className="block text-sm font-medium">
        {label}
      </label>
      {children}
      {error ? (
        <p className="text-xs text-destructive">{error}</p>
      ) : (
        hint && <p className="text-xs text-muted-foreground">{hint}</p>
      )}
    </div>
  );
}

export function TextField({ id, label, hint, error, value, onChange, disabled, className, ...rest }) {
  return (
    <Field label={label} htmlFor={id} hint={hint} error={error} className={className}>
      <input
        id={id}
        className={fieldCls}
        value={value ?? ''}
        onChange={(e) => onChange(e.target.value)}
        disabled={disabled}
        aria-invalid={!!error}
        {...rest}
      />
    </Field>
  );
}

export function TextArea({ id, label, hint, error, value, onChange, disabled, rows = 3, maxLength }) {
  return (
    <Field label={label} htmlFor={id} hint={hint} error={error}>
      <textarea
        id={id}
        rows={rows}
        maxLength={maxLength}
        className={cn(fieldCls, 'h-auto min-h-[70px] py-2')}
        value={value ?? ''}
        onChange={(e) => onChange(e.target.value)}
        disabled={disabled}
        aria-invalid={!!error}
      />
    </Field>
  );
}

export function SelectField({ id, label, hint, error, value, onChange, options, disabled }) {
  return (
    <Field label={label} htmlFor={id} hint={hint} error={error}>
      <select id={id} className={fieldCls} value={value ?? ''} onChange={(e) => onChange(e.target.value)} disabled={disabled}>
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
    </Field>
  );
}

export function CheckField({ id, label, hint, checked, onChange, disabled }) {
  return (
    <div className="flex items-start gap-2">
      <input
        id={id}
        type="checkbox"
        className="mt-0.5 size-4 accent-[var(--primary)]"
        checked={!!checked}
        onChange={(e) => onChange(e.target.checked)}
        disabled={disabled}
      />
      <div>
        <label htmlFor={id} className="text-sm font-medium">
          {label}
        </label>
        {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
      </div>
    </div>
  );
}

// Notice shows the result of the last action.
export function Notice({ notice }) {
  if (!notice) return null;
  return (
    <div
      role={notice.kind === 'error' ? 'alert' : 'status'}
      className={cn(
        'rounded-md border px-3 py-2 text-sm',
        notice.kind === 'error'
          ? 'border-destructive/30 bg-destructive/10 text-destructive'
          : 'border-emerald-300 bg-emerald-50 text-emerald-800 dark:border-emerald-800 dark:bg-emerald-950 dark:text-emerald-200'
      )}
    >
      {notice.text}
    </div>
  );
}

export function SaveButton({ busy, disabled, children = 'Save changes' }) {
  return (
    <Button type="submit" disabled={busy || disabled}>
      {busy && <Loader2 className="size-4 animate-spin" />}
      {children}
    </Button>
  );
}
