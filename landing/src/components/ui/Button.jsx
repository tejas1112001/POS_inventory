import { Link } from 'react-router-dom';
import { cn } from '@/lib/cn';

const base =
  'inline-flex items-center justify-center gap-2 rounded-xl font-semibold whitespace-nowrap ' +
  'transition-[background-color,border-color,color,transform] duration-150 ease-out disabled:opacity-60';

const variants = {
  primary: 'bg-brand text-white hover:bg-brand-strong',
  // On dark backgrounds.
  ghost: 'border border-slate-400/40 text-white hover:bg-white/10',
  // On light backgrounds.
  outline: 'border border-slate-300 bg-white text-ink hover:border-slate-400 hover:bg-slate-50',
  link: 'px-0! text-brand-strong hover:text-brand underline-offset-4 hover:underline',
};

const sizes = {
  sm: 'min-h-10 px-4 text-sm',
  md: 'min-h-11 px-[18px] text-[15px]',
  lg: 'min-h-13 px-6 text-base',
};

/**
 * One button style for the whole site.
 * `to` renders a router Link, `href` an anchor, otherwise a <button>.
 * Large buttons lean toward the cursor (data-magnetic, see InteractiveLayer).
 */
export function Button({ to, href, variant = 'primary', size = 'md', className, children, ...rest }) {
  const cls = cn(base, variants[variant], sizes[size], className);
  if (size === 'lg' && variant !== 'link') rest = { 'data-magnetic': '', ...rest };
  if (to) {
    return (
      <Link to={to} className={cls} {...rest}>
        {children}
      </Link>
    );
  }
  if (href) {
    return (
      <a href={href} className={cls} {...rest}>
        {children}
      </a>
    );
  }
  return (
    <button type="button" className={cls} {...rest}>
      {children}
    </button>
  );
}
