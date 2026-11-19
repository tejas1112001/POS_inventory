import { cn } from '@/lib/cn';

export function Eyebrow({ children, dark = false, className }) {
  return (
    <p
      className={cn(
        'text-[13px] font-bold tracking-[0.1em] uppercase',
        dark ? 'text-brand-bright' : 'text-brand',
        className,
      )}
    >
      {children}
    </p>
  );
}

/** Section title (h2) at the site's standard size. */
export function SectionTitle({ children, className, as: Tag = 'h2' }) {
  return (
    <Tag
      className={cn(
        'text-[30px] leading-[1.1] font-extrabold tracking-[-0.03em] text-ink sm:text-[40px]',
        className,
      )}
    >
      {children}
    </Tag>
  );
}

/** Max-width page container with the standard side gutter. */
export function Container({ children, className, as: Tag = 'div', ...rest }) {
  return (
    <Tag className={cn('mx-auto w-full max-w-[1200px] px-4 sm:px-6', className)} {...rest}>
      {children}
    </Tag>
  );
}
