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

/**
 * Page container with the standard side gutter: 16 px on phones, 24 px on
 * tablets, 40-48 px on laptops; content up to 1344 px wide (1440 minus the
 * gutters). `width` replaces the max width for narrow sections.
 */
export function Container({ children, className, width = 'max-w-[1440px]', as: Tag = 'div', ...rest }) {
  return (
    <Tag className={cn('mx-auto w-full px-4 sm:px-6 lg:px-10 xl:px-12', width, className)} {...rest}>
      {children}
    </Tag>
  );
}
