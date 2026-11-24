import { useState } from 'react';
import { BadgeCheck, CalendarRange, Check, ChevronDown, Clock, Minus, Rocket } from 'lucide-react';
import { compareRows, faqs, plans, yearlySaving } from '@/content/site';
import { cn } from '@/lib/cn';
import { usePageMeta } from '@/lib/usePageMeta';
import { PageHero } from '@/components/layout/PageHero';
import { Button } from '@/components/ui/Button';
import { CheckList } from '@/components/ui/CheckList';
import { Container } from '@/components/ui/Heading';
import { Segmented } from '@/components/ui/Segmented';

const timeline = [
  { when: 'Day 1', icon: Rocket, t: 'We set up your shop', d: 'Your shop address, logins and products are ready.', text: 'text-brand-bright', border: 'border-brand-bright' },
  { when: 'Day 1 to 14', icon: CalendarRange, t: 'Use everything free', d: 'All features, with your real products and staff.', text: 'text-brand-bright', border: 'border-brand-bright' },
  { when: 'Day 14', icon: BadgeCheck, t: 'Pick a plan', d: 'Choose the plan that fits and keep billing.', text: 'text-ok', border: 'border-ok' },
  { when: 'Need more time?', icon: Clock, t: '7 extra days', d: 'After that the shop turns read-only. You can still see and export your data.', text: 'text-amber-400', border: 'border-amber-400' },
];

const h2 = 'text-[28px] font-extrabold tracking-[-0.03em] text-ink sm:text-[32px]';

function PlanCard({ plan, period }) {
  const f = plan.featured;
  return (
    <article
      className={cn(
        'flex flex-[1_1_300px] flex-col gap-4 rounded-3xl border p-8',
        f ? 'bg-night surface-glow border-brand shadow-[0_24px_60px_rgba(23,128,122,.25)]' : 'card-lift border-line bg-white',
      )}
    >
      <div className="flex items-center justify-between gap-2.5">
        <h2 className={cn('text-[22px] font-bold', f ? 'text-white' : 'text-ink')}>{plan.name}</h2>
        {f && <span className="rounded-full bg-brand px-3 py-1 text-xs font-bold text-white">Best for growing shops</span>}
      </div>
      <p className={cn('text-[15px]', f ? 'text-slate-300' : 'text-body')}>{plan.forWho}</p>
      <p className="flex items-baseline gap-1.5">
        <span className={cn('text-[42px] font-extrabold tracking-[-0.03em]', f ? 'text-white' : 'text-ink')}>₹[PRICE]</span>
        <span className={cn('text-[15px]', f ? 'text-slate-300' : 'text-body')}>{period}</span>
      </p>
      <CheckList items={plan.items} tone={f ? 'darkBrand' : 'light'} className="gap-2.5" />
      <Button to="/contact#demo" variant={f ? 'primary' : 'outline'} size="lg" className="mt-auto min-h-12 text-[15px] font-bold">
        Start 14-day free trial
      </Button>
    </article>
  );
}

function CompareTable() {
  return (
    <Container as="section" aria-labelledby="compare-title" className="py-10">
      <h2 id="compare-title" className={cn(h2, 'mb-5')}>Compare plans</h2>
      <div tabIndex={0} role="region" aria-label="Compare plans table" className="overflow-x-auto rounded-[20px] border border-line">
        <table className="w-full min-w-[640px] border-collapse text-[15px]">
          <thead>
            <tr className="bg-surface text-left">
              <th scope="col" className="px-5 py-4 text-ink">Feature</th>
              {plans.map((p) => (
                <th key={p.name} scope="col" className={cn('px-5 py-4', p.featured ? 'text-brand-strong' : 'text-ink')}>{p.name}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {compareRows.map((r) => (
              <tr key={r.f} className="border-t border-line">
                <th scope="row" className="px-5 py-3.5 text-left font-semibold text-ink">{r.f}</th>
                {r.v.map((v, i) => (
                  <td key={i} className="px-5 py-3.5 text-slate-700">
                    {v === true ? (
                      <Check className="size-5 text-brand" strokeWidth={2.4} aria-label="Included" />
                    ) : v === false ? (
                      <Minus className="size-5 text-slate-400" aria-label="Not included" />
                    ) : (
                      v
                    )}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Container>
  );
}

function Faq() {
  const [open, setOpen] = useState(0);
  return (
    <Container as="section" aria-labelledby="faq-title" width="max-w-[860px]" className="pt-12 pb-24">
      <h2 id="faq-title" className={cn(h2, 'mb-6')}>Questions</h2>
      <div className="flex flex-col gap-3">
        {faqs.map(([q, a], i) => {
          const isOpen = open === i;
          return (
            <div key={q} className="rounded-2xl border border-line bg-white">
              <h3>
                <button
                  type="button"
                  id={`faq-q-${i}`}
                  aria-expanded={isOpen}
                  aria-controls={`faq-a-${i}`}
                  onClick={() => setOpen(isOpen ? -1 : i)}
                  className="flex min-h-14 w-full items-center justify-between gap-4 px-[22px] py-5 text-left text-[17px] font-semibold text-ink"
                >
                  {q}
                  <ChevronDown
                    className={cn('size-5 shrink-0 text-brand transition-transform duration-200', isOpen && 'rotate-180')}
                    aria-hidden="true"
                  />
                </button>
              </h3>
              <div id={`faq-a-${i}`} role="region" aria-labelledby={`faq-q-${i}`} hidden={!isOpen}>
                <p className="px-[22px] pb-5 text-[15px] leading-[1.65] text-body">{a}</p>
              </div>
            </div>
          );
        })}
      </div>
    </Container>
  );
}

export default function Pricing() {
  const [yearly, setYearly] = useState(false);
  usePageMeta('Pricing | MintoBill', 'Simple plans with a 14-day free trial. Every plan has GST bills, stock and reports.');

  return (
    <>
      <PageHero
        eyebrow="Pricing"
        title="Simple plans. 14 days free."
        lead="Every plan has GST bills, stock and reports. Pick one when your trial ends."
      />

      <Container as="section" aria-label="Plans" className="pt-16 pb-10">
        <div className="flex justify-center">
          <Segmented
            size="lg"
            label="Billing period"
            value={yearly ? 'y' : 'm'}
            onChange={(v) => setYearly(v === 'y')}
            className="rounded-[14px] p-[5px]"
            options={[
              { value: 'm', label: 'Monthly' },
              { value: 'y', label: 'Yearly', extra: <span className="font-bold text-ok-strong"> · save {yearlySaving}</span> },
            ]}
          />
        </div>
        <div className="mt-9 flex flex-wrap items-stretch gap-5">
          {plans.map((p) => (
            <PlanCard key={p.name} plan={p} period={yearly ? '/ year' : '/ month'} />
          ))}
        </div>
      </Container>

      <CompareTable />

      <Container as="section" aria-labelledby="trial-title" className="py-10">
        <div className="rounded-[26px] bg-ink p-6 sm:p-10">
          <h2 id="trial-title" className="text-[26px] font-extrabold tracking-[-0.03em] text-white sm:text-[30px]">
            How the free trial works
          </h2>
          <p className="mt-2 mb-7 text-slate-300">No card needed to start. Your data is never deleted without warning.</p>
          <ol className="grid grid-cols-[repeat(auto-fit,minmax(210px,1fr))] gap-4">
            {timeline.map((s) => (
              <li key={s.when} className={cn('border-t-[3px] pt-4', s.border)}>
                <p className={cn('flex items-center gap-2 text-[13px] font-extrabold tracking-[0.08em] uppercase', s.text)}>
                  <s.icon className="size-4" aria-hidden="true" />
                  {s.when}
                </p>
                <h3 className="mt-2 mb-1.5 text-lg font-bold text-white">{s.t}</h3>
                <p className="text-sm leading-relaxed text-slate-300">{s.d}</p>
              </li>
            ))}
          </ol>
        </div>
      </Container>

      <Faq />
    </>
  );
}
