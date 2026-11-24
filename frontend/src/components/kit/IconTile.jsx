import { cn } from '@/lib/utils';

// Icon tile colours (Teal & Cream moodboard).
const TILE_TONES = {
  teal: 'bg-teal-soft text-primary dark:text-teal',
  amber: 'bg-amber-soft text-[#b46f00] dark:text-amber-brand',
  deep: 'bg-[#123b45] text-white dark:bg-[#8fd9cd] dark:text-[#0b2a32]',
  muted: 'bg-muted text-primary dark:text-teal',
  warn: 'bg-warning-bg text-warning-fg',
  danger: 'bg-danger-bg text-danger-fg',
  brand: 'bg-brand-gradient text-white',
};

const SIZES = {
  sm: ['size-9 rounded-xl', 'size-[18px]'],
  md: ['size-11 rounded-xl', 'size-5'],
  lg: ['size-12 rounded-2xl sm:size-14', 'size-6 sm:size-7'],
};

// IconTile: a decorative icon in a rounded, tinted square.
export default function IconTile({ icon: Icon, tone = 'teal', size = 'md', className }) {
  const [box, glyph] = SIZES[size] || SIZES.md;
  return (
    <span className={cn('flex shrink-0 items-center justify-center', box, TILE_TONES[tone] || TILE_TONES.teal, className)}>
      <Icon className={glyph} strokeWidth={1.9} aria-hidden="true" />
    </span>
  );
}
