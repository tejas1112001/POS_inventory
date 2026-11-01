import { useEffect, useId, useState } from 'react';
import { FormProvider, useForm, useFormContext } from 'react-hook-form';
import { ImagePlus, Loader2, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { InlineAlert, FieldError } from '@/components/feedback/InlineAlert';
import { useImageSrc } from '@/components/shared/auth-image';
import { cn } from '@/lib/utils';

const inputCls =
  'h-11 w-full rounded-md border border-input bg-background px-3 text-base outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 aria-[invalid=true]:border-danger sm:h-9 sm:text-sm';

// FormSheet: a form in a dialog (a bottom sheet on phones). onSubmit gets
// the values and returns a promise; server field errors ({ fields }) are
// shown under the matching fields, other errors at the top.
export function FormSheet({ open, onOpenChange, title, description, defaultValues, onSubmit, submitLabel = 'Save', children }) {
  const form = useForm({ defaultValues, mode: 'onTouched' });
  const [error, setError] = useState('');

  useEffect(() => {
    if (open) {
      form.reset(defaultValues);
      setError('');
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- reset only when the sheet opens
  }, [open]);

  const submit = form.handleSubmit(async (values) => {
    setError('');
    try {
      await onSubmit(values);
      onOpenChange(false);
    } catch (err) {
      const fields = err?.fields || {};
      const known = Object.keys(fields).filter((k) => k in (defaultValues || {}));
      known.forEach((k, i) => form.setError(k, { message: fields[k] }, { shouldFocus: i === 0 }));
      setError(known.length ? 'Please correct the highlighted fields.' : err?.message || 'Could not save. Try again.');
    }
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent sheet className="max-h-[90dvh] overflow-y-auto pb-0 sm:max-w-lg">
        <FormProvider {...form}>
          <form onSubmit={submit} noValidate className="grid gap-4">
            <DialogHeader>
              <DialogTitle>{title}</DialogTitle>
              {description && <DialogDescription>{description}</DialogDescription>}
            </DialogHeader>
            {error && <InlineAlert>{error}</InlineAlert>}
            <div className="grid gap-3 sm:grid-cols-2">{children}</div>
            {/* Actions stay visible while long forms scroll (phones). */}
            <DialogFooter className="sticky bottom-0 z-10 -mx-6 gap-2 border-t bg-background px-6 py-3 pb-[max(12px,env(safe-area-inset-bottom))]">
              <Button type="button" variant="outline" className="max-sm:h-11" onClick={() => onOpenChange(false)}>
                Cancel
              </Button>
              <Button type="submit" className="max-sm:h-11" disabled={form.formState.isSubmitting}>
                {form.formState.isSubmitting && <Loader2 className="size-4 animate-spin" aria-hidden="true" />}
                {submitLabel}
              </Button>
            </DialogFooter>
          </form>
        </FormProvider>
      </DialogContent>
    </Dialog>
  );
}

// Field: label linked to its control, hint, error under it.
function Field({ name, label, required, hint, wide, children }) {
  const { formState } = useFormContext();
  const uid = useId();
  const id = `f-${name}-${uid}`;
  const message = formState.errors[name]?.message;
  const described = [hint && `${id}-hint`, message && `${id}-err`].filter(Boolean).join(' ') || undefined;
  return (
    <div className={cn('grid content-start gap-1.5', wide && 'sm:col-span-2')}>
      <label htmlFor={id} className="text-sm font-medium">
        {label}
        {required && <span className="text-danger"> *</span>}
      </label>
      {children({ id, invalid: !!message, describedBy: described })}
      {hint && (
        <p id={`${id}-hint`} className="text-xs text-muted-foreground">
          {hint}
        </p>
      )}
      <FieldError id={`${id}-err`}>{message}</FieldError>
    </div>
  );
}

export function TextField({ name, label, required, hint, wide, type = 'text', rules = {}, ...rest }) {
  const { register } = useFormContext();
  return (
    <Field name={name} label={label} required={required} hint={hint} wide={wide}>
      {({ id, invalid, describedBy }) => (
        <input
          id={id}
          type={type}
          className={inputCls}
          aria-invalid={invalid}
          aria-describedby={describedBy}
          {...register(name, { required: required ? `${label} is required.` : false, ...rules })}
          {...rest}
        />
      )}
    </Field>
  );
}

export function SelectField({ name, label, required, hint, wide, options }) {
  const { register } = useFormContext();
  return (
    <Field name={name} label={label} required={required} hint={hint} wide={wide}>
      {({ id, invalid, describedBy }) => (
        <select id={id} className={inputCls} aria-invalid={invalid} aria-describedby={describedBy} {...register(name, { required: required ? `${label} is required.` : false })}>
          {options.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
      )}
    </Field>
  );
}

export function TextAreaField({ name, label, hint, rows = 3, wide = true }) {
  const { register } = useFormContext();
  return (
    <Field name={name} label={label} hint={hint} wide={wide}>
      {({ id, invalid, describedBy }) => (
        <textarea id={id} rows={rows} className={cn(inputCls, 'h-auto py-2')} aria-invalid={invalid} aria-describedby={describedBy} {...register(name)} />
      )}
    </Field>
  );
}

const MAX_IMAGE = 4 * 1024 * 1024;

// ImageField: the value is undefined (keep), '' (remove) or a data URL (new).
export function ImageField({ name, label, current }) {
  const { setValue, watch, setError, clearErrors } = useFormContext();
  const value = watch(name);
  const shownCurrent = useImageSrc(value === undefined ? current : null);
  const preview = typeof value === 'string' && value ? value : value === undefined ? shownCurrent : null;
  const pick = (file) => {
    if (!file) return;
    if (!/^image\/(png|jpeg|webp)$/.test(file.type)) return setError(name, { message: 'Use a PNG, JPEG or WebP picture.' });
    if (file.size > MAX_IMAGE) return setError(name, { message: 'The picture can be at most 4 MB.' });
    clearErrors(name);
    const reader = new FileReader();
    reader.onload = () => setValue(name, reader.result, { shouldDirty: true });
    reader.readAsDataURL(file);
  };
  return (
    <Field name={name} label={label} wide>
      {({ id, describedBy }) => (
        <div className="flex items-center gap-3">
          <div className="flex size-14 shrink-0 items-center justify-center overflow-hidden rounded-md border bg-muted">
            {preview ? <img src={preview} alt="" className="size-full object-cover" /> : <ImagePlus className="size-5 text-muted-foreground" aria-hidden="true" />}
          </div>
          <label htmlFor={id} className="inline-flex min-h-11 cursor-pointer items-center rounded-md border px-3 text-sm font-medium hover:bg-accent sm:min-h-9">
            {preview ? 'Change picture' : 'Choose picture'}
            <input id={id} type="file" accept="image/png,image/jpeg,image/webp" className="sr-only" aria-describedby={describedBy} onChange={(e) => pick(e.target.files?.[0])} />
          </label>
          {preview && (
            <Button type="button" variant="ghost" className="min-h-11 sm:min-h-9" onClick={() => setValue(name, '', { shouldDirty: true })}>
              <X className="size-4" aria-hidden="true" /> Remove
            </Button>
          )}
        </div>
      )}
    </Field>
  );
}

export const STATUS_OPTIONS = [
  { value: 'Active', label: 'Active' },
  { value: 'Inactive', label: 'Inactive' },
];
