import { Check, X } from 'lucide-react';
import { cn } from '@/lib/cn';

const tones = {
  light: { icon: 'text-brand', text: 'text-slate-700' },
  dark: { icon: 'text-ok', text: 'text-slate-200' },
  darkBrand: { icon: 'text-brand-bright', text: 'text-slate-200' },
  muted: { icon: 'text-slate-400', text: 'text-body' },
};

/** A list with a check (or cross) icon before each item. */
export function CheckList({ items, tone = 'light', cross = false, className, itemClassName }) {
  const t = tones[tone];
  const Icon = cross ? X : Check;
  return (
    <ul className={cn('flex flex-col gap-3', className)}>
      {items.map((it) => (
        <li key={it} className={cn('flex gap-2.5 text-[15px] leading-normal', t.text, itemClassName)}>
          <Icon className={cn('mt-0.5 size-[18px] shrink-0', t.icon)} strokeWidth={2.4} aria-hidden="true" />
          <span>{it}</span>
        </li>
      ))}
    </ul>
  );
}
