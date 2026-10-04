import * as React from 'react';
import { cn } from '@/lib/utils';

const Switch = React.forwardRef(
  (
    {
      className,
      checked = false,
      onCheckedChange,
      disabled = false,
      label,
      description,
      id,
      ...props
    },
    ref
  ) => {
    const handleToggle = () => {
      if (disabled) return;
      if (typeof onCheckedChange === 'function') {
        onCheckedChange(!checked);
      }
    };

    const generatedId = React.useId();
    const switchId = id || generatedId;

    return (
      <div className="inline-flex items-center gap-3 select-none">
        <button
          type="button"
          role="switch"
          id={switchId}
          aria-checked={checked}
          disabled={disabled}
          onClick={handleToggle}
          ref={ref}
          className={cn(
            'peer relative inline-flex h-6 w-11 shrink-0 cursor-pointer items-center rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50',
            checked ? 'bg-primary' : 'bg-muted-foreground/30 dark:bg-muted-foreground/40',
            className
          )}
          {...props}
        >
          <span
            className={cn(
              'pointer-events-none block size-5 rounded-full bg-background shadow-md ring-0 transition-transform duration-200 ease-in-out',
              checked ? 'translate-x-5' : 'translate-x-0'
            )}
          />
        </button>
        {(label || description) && (
          <label htmlFor={switchId} className="cursor-pointer text-sm">
            {label && <span className="font-medium text-foreground block">{label}</span>}
            {description && <span className="text-xs text-muted-foreground">{description}</span>}
          </label>
        )}
      </div>
    );
  }
);

Switch.displayName = 'Switch';

export { Switch };
