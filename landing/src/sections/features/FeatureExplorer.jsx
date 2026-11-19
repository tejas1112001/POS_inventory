import { Check } from 'lucide-react';
import { useSearchParams } from 'react-router-dom';
import { featureTabs } from '@/content/site';
import { cn } from '@/lib/cn';
import { tabKeyHandler } from '@/lib/tabKeys';
import { Container, Eyebrow } from '@/components/ui/Heading';
import { mocks } from './mockMap';

const ids = featureTabs.map((f) => f.id);

/** Vertical tab list of feature areas with a working preview of each. */
export function FeatureExplorer() {
  // ?tab=gst links straight to a tab.
  const [params, setParams] = useSearchParams();
  const active = ids.includes(params.get('tab')) ? params.get('tab') : ids[0];
  const select = (id) => setParams({ tab: id }, { replace: true, preventScrollReset: true });
  const cur = featureTabs.find((f) => f.id === active);
  const Mock = mocks[active];

  return (
    <Container as="section" aria-label="Feature areas" className="pt-[72px] pb-14">
      <div className="flex flex-wrap items-start gap-7">
        <div
          role="tablist"
          aria-label="Feature areas"
          aria-orientation="vertical"
          onKeyDown={tabKeyHandler(ids, active, select)}
          className="flex min-w-0 flex-[1_1_280px] flex-col gap-2.5"
        >
          {featureTabs.map((f, i) => {
            const on = f.id === active;
            return (
              <button
                key={f.id}
                type="button"
                role="tab"
                id={`ftab-${f.id}`}
                data-tab={f.id}
                aria-selected={on}
                aria-controls="fpanel"
                tabIndex={on ? 0 : -1}
                onClick={() => select(f.id)}
                className={cn(
                  'flex min-h-16 items-center gap-3.5 rounded-2xl border px-[18px] py-4 text-left transition-colors duration-150',
                  on
                    ? 'border-ink bg-ink text-white shadow-[0_12px_30px_rgba(15,23,42,.18)]'
                    : 'card-lift border-line bg-white text-ink',
                )}
              >
                <span className={cn('text-[13px] font-extrabold', on ? 'text-brand-bright' : 'text-brand')}>0{i + 1}</span>
                <f.icon className={cn('size-5 shrink-0', on ? 'text-blue-300' : 'text-slate-400')} strokeWidth={1.8} aria-hidden="true" />
                <span className="flex flex-col gap-0.5">
                  <span className="font-bold">{f.title}</span>
                  <span className={cn('text-[13px]', on ? 'text-slate-300' : 'text-muted')}>{f.sub}</span>
                </span>
              </button>
            );
          })}
        </div>

        <div
          role="tabpanel"
          id="fpanel"
          aria-labelledby={`ftab-${active}`}
          className="flex min-w-0 flex-[3_1_620px] flex-col gap-[22px] rounded-[26px] border border-line bg-surface p-5 sm:p-8"
        >
          <div>
            <Eyebrow>{cur.kicker}</Eyebrow>
            <h2 className="mt-2 mb-2.5 text-[26px] leading-[1.12] font-extrabold tracking-[-0.03em] text-ink sm:text-[32px]">{cur.h}</h2>
            <p className="max-w-[620px] leading-relaxed text-body">{cur.body}</p>
            <ul className="mt-4 flex flex-wrap gap-2">
              {cur.points.map((p) => (
                <li key={p} className="inline-flex items-center gap-1.5 rounded-full border border-line bg-white px-3 py-[7px] text-sm font-semibold text-ink">
                  <Check className="size-[15px] text-brand" strokeWidth={2.4} aria-hidden="true" />
                  {p}
                </li>
              ))}
            </ul>
          </div>
          <div className="rounded-[18px] border border-line bg-white p-4 shadow-[0_20px_50px_rgba(15,23,42,.08)] sm:p-[22px]">
            <Mock />
          </div>
        </div>
      </div>
    </Container>
  );
}
