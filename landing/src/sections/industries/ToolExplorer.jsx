import { Check, CircleAlert } from 'lucide-react';
import { toolOrder, tools } from '@/content/site';
import { cn } from '@/lib/cn';
import { tabKeyHandler } from '@/lib/tabKeys';
import { Container, Eyebrow } from '@/components/ui/Heading';
import { toolMocks } from './mockMap';

/** Tabs for the switch-on tools, each with its problem, fix and a live preview. */
export function ToolExplorer({ tool, onSelect }) {
  const cur = tools[tool];
  const Mock = toolMocks[tool];

  return (
    <Container as="section" id="tools" tabIndex={-1} aria-labelledby="tools-title" className="scroll-mt-6 pt-12 pb-[72px] outline-none">
      <Eyebrow>Tools you can switch on</Eyebrow>
      <h2 id="tools-title" className="mt-2.5 mb-6 text-[28px] leading-[1.1] font-extrabold tracking-[-0.03em] text-ink sm:text-[34px]">
        Every shop gets the basics. Add what your shop needs.
      </h2>

      <div role="tablist" aria-label="Shop tools" onKeyDown={tabKeyHandler(toolOrder, tool, onSelect)} className="flex flex-wrap gap-2.5">
        {toolOrder.map((id) => {
          const t = tools[id];
          const on = id === tool;
          return (
            <button
              key={id}
              type="button"
              role="tab"
              id={`ttab-${id}`}
              data-tab={id}
              aria-selected={on}
              aria-controls="tpanel"
              tabIndex={on ? 0 : -1}
              onClick={() => onSelect(id)}
              className={cn(
                'inline-flex min-h-12 items-center gap-2 rounded-full border px-[22px] text-base',
                on ? 'border-ink bg-ink font-bold text-white' : 'card-lift border-slate-300 bg-white font-semibold text-ink',
              )}
            >
              <t.icon className={cn('size-[18px]', on ? 'text-blue-300' : 'text-brand')} aria-hidden="true" />
              {t.label}
            </button>
          );
        })}
      </div>

      <div role="tabpanel" id="tpanel" aria-labelledby={`ttab-${tool}`} className="mt-7 flex flex-wrap items-start gap-10">
        <div className="min-w-0 flex-[1_1_380px]">
          <span
            className={cn(
              'inline-block rounded-full px-3 py-1 text-[13px] font-bold',
              cur.ready ? 'bg-ok-soft text-ok-strong' : 'bg-warn-soft text-warn-strong',
            )}
          >
            {cur.ready ? 'Available now' : 'Rolling out'}
          </span>
          <h3 className="mt-3.5 mb-2.5 text-[30px] leading-[1.1] font-extrabold tracking-[-0.03em] text-ink sm:text-[38px]">{cur.label}</h3>
          <p className="text-[17px] leading-relaxed text-body">{cur.lead}</p>

          <div className="mt-6 rounded-[18px] border border-orange-200 bg-warn-soft p-5">
            <h4 className="flex items-center gap-2 text-[13px] font-extrabold tracking-[0.08em] text-warn-strong uppercase">
              <CircleAlert className="size-4" aria-hidden="true" />
              The problem
            </h4>
            <ul className="mt-3 flex list-disc flex-col gap-2.5 pl-5 marker:text-orange-400">
              {cur.pains.map((p) => (
                <li key={p} className="text-[15px] leading-normal text-orange-900">{p}</li>
              ))}
            </ul>
          </div>

          <div className="mt-3.5 rounded-[18px] border border-blue-200 bg-brand-soft p-5">
            <h4 className="flex items-center gap-2 text-[13px] font-extrabold tracking-[0.08em] text-brand-strong uppercase">
              <Check className="size-4" strokeWidth={2.6} aria-hidden="true" />
              How MintoBill helps
            </h4>
            <ul className="mt-3 flex flex-col gap-2.5">
              {cur.wins.map((w) => (
                <li key={w} className="flex gap-2.5 text-[15px] leading-normal text-ink">
                  <Check className="mt-0.5 size-[18px] shrink-0 text-brand" strokeWidth={2.4} aria-hidden="true" />
                  {w}
                </li>
              ))}
            </ul>
          </div>

          <h4 className="mt-[18px] text-[13px] font-bold tracking-[0.08em] text-muted uppercase">Useful for</h4>
          <ul className="mt-2.5 flex flex-wrap gap-2">
            {cur.usedBy.map((u) => (
              <li key={u} className="rounded-full bg-slate-100 px-3 py-1.5 text-sm font-semibold text-ink">{u}</li>
            ))}
          </ul>
          {cur.note && <p className="mt-3.5 text-sm leading-relaxed text-muted">{cur.note}</p>}
        </div>

        <div className="relative min-w-0 flex-[1.3_1_480px]">
          <div aria-hidden="true" className="absolute inset-[30px] rounded-[40px] bg-brand opacity-15 blur-[80px]" />
          <div className="relative rounded-[22px] border border-line bg-white p-4 shadow-[0_24px_60px_rgba(18,59,69,.12)] sm:p-6">
            <Mock />
          </div>
        </div>
      </div>
    </Container>
  );
}
