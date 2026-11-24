import { Link } from 'react-router-dom';
import { Plus } from 'lucide-react';
import { shopTypes, tools } from '@/content/site';
import { cn } from '@/lib/cn';
import { Container } from '@/components/ui/Heading';

/** Grid of shop types; picking one shows the tool that helps it most. */
export function ShopTypeGrid({ shop, onPick }) {
  return (
    <Container as="section" aria-labelledby="types-title" className="pt-16 pb-6">
      <div className="mb-7 flex flex-wrap items-end justify-between gap-4">
        <h2 id="types-title" className="text-[28px] leading-[1.1] font-extrabold tracking-[-0.03em] text-ink sm:text-[34px]">
          Shops that use a POS like MintoBill
        </h2>
        <p className="max-w-[420px] text-[15px] text-body">Tap your shop type to see the tool that helps it most.</p>
      </div>
      <ul className="grid grid-cols-[repeat(auto-fit,minmax(220px,1fr))] gap-3.5">
        {shopTypes.map(({ slug, name, icon: Icon, tint, tool }) => {
          const on = shop === slug;
          return (
            <li key={slug}>
              <button
                type="button"
                aria-pressed={on}
                onClick={() => onPick(slug)}
                className={cn(
                  'flex min-h-[84px] w-full items-center gap-3.5 rounded-[18px] border p-[18px] text-left',
                  on ? 'border-ink bg-ink shadow-[0_12px_30px_rgba(18,59,69,.18)]' : 'card-lift border-line bg-white',
                )}
              >
                <span className={cn('flex size-11 shrink-0 items-center justify-center rounded-xl', tint)}>
                  <Icon className="size-[22px]" strokeWidth={1.8} aria-hidden="true" />
                </span>
                <span className="flex flex-col gap-[3px]">
                  <span className={cn('font-bold', on ? 'text-white' : 'text-ink')}>{name}</span>
                  <span className={cn('text-[13px]', on ? 'text-blue-300' : 'text-muted')}>{tools[tool].label}</span>
                </span>
              </button>
            </li>
          );
        })}
        <li>
          <Link
            to="/contact#demo"
            className="card-lift flex min-h-[84px] w-full items-center gap-3.5 rounded-[18px] border border-dashed border-blue-300 bg-brand-soft p-[18px]"
          >
            <span className="flex size-11 shrink-0 items-center justify-center rounded-xl bg-brand text-white">
              <Plus className="size-[22px]" aria-hidden="true" />
            </span>
            <span className="flex flex-col gap-[3px]">
              <span className="font-bold text-ink">Something else?</span>
              <span className="text-[13px] text-brand-strong">Tell us what you sell</span>
            </span>
          </Link>
        </li>
      </ul>
    </Container>
  );
}
