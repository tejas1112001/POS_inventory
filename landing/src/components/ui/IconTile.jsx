import { cn } from '@/lib/cn';

const tones = {
  brand: 'bg-brand-soft text-brand',
  dark: 'bg-blue-600/25 text-blue-300',
  ok: 'bg-ok-soft text-ok-strong',
  warn: 'bg-warn-soft text-warn-strong',
  solid: 'bg-brand text-white',
};

const sizes = {
  sm: 'size-9 rounded-[10px] [&>svg]:size-[18px]',
  md: 'size-11 rounded-xl [&>svg]:size-[22px]',
  lg: 'size-12 rounded-xl [&>svg]:size-6',
};

/** Rounded square holding a decorative icon. */
export function IconTile({ icon: Icon, tone = 'brand', size = 'md', className }) {
  return (
    <span className={cn('flex shrink-0 items-center justify-center', tones[tone], sizes[size], className)}>
      <Icon strokeWidth={1.8} aria-hidden="true" />
    </span>
  );
}
