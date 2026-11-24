import { useState } from 'react';
import { bento } from '@/content/site';
import { cn } from '@/lib/cn';
import { money0 } from '@/lib/format';
import { Container, SectionTitle } from '@/components/ui/Heading';
import { IconTile } from '@/components/ui/IconTile';

const card = 'card-lift min-w-0 rounded-[22px] border';

// Grid placement. Desktop (6 columns): checkout 4 + GST 2, stock 3 +
// receipts 3, reports 4 + dues 2, catalogue 3 + staff 3. Tablet (2
// columns): customer dues moves up next to receipts and the reports card
// goes after it, so the small cards always come in pairs.
const SMALL_SPANS = ['sm:order-2 lg:order-none lg:col-span-2', 'sm:order-3 lg:order-none lg:col-span-3', 'sm:order-4 lg:order-none lg:col-span-3'];
const MORE_SPANS = ['sm:order-5 lg:order-none lg:col-span-2', 'sm:order-7 lg:order-none lg:col-span-3', 'sm:order-8 lg:order-none lg:col-span-3'];

function SmallCard({ icon, title, body, className }) {
  return (
    <article className={cn(card, 'flex flex-col gap-3.5 border-line bg-white p-6 sm:p-7', className)}>
      <IconTile icon={icon} />
      <h3 className="text-xl font-bold text-ink">{title}</h3>
      <p className="text-[15px] leading-relaxed text-body">{body}</p>
    </article>
  );
}

// Sample weeks for the reports card chart; click the chart to step through them.
const WEEKS = [
  { label: 'This week', bars: [38, 55, 46, 72, 64, 92] },
  { label: 'Last week', bars: [52, 40, 61, 58, 77, 70] },
  { label: 'Festival week', bars: [64, 78, 85, 90, 96, 100] },
];
const DAYS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
const RUPEES_PER_POINT = 420;

function SalesChart() {
  const [w, setW] = useState(0);
  const { label, bars } = WEEKS[w];
  const top = Math.max(...bars);
  return (
    <button
      type="button"
      onClick={() => setW((n) => (n + 1) % WEEKS.length)}
      aria-label={`Sample sales chart: ${label}. Show the next week.`}
      className="group/chart flex h-[170px] min-w-0 flex-[1_1_220px] flex-col gap-2 rounded-[14px] border border-line bg-white p-4 text-left transition-colors duration-150 hover:border-blue-300"
    >
      <span aria-hidden="true" className="flex items-center justify-between text-xs font-semibold text-muted">
        {label}
        <span className="text-brand opacity-0 transition-opacity duration-150 group-hover/chart:opacity-100">Next week →</span>
      </span>
      <span aria-hidden="true" className="flex flex-1 items-end gap-2.5">
        {bars.map((h, i) => (
          <span
            key={DAYS[i]}
            className={cn(
              'group/bar relative flex-1 rounded-t-md rounded-b-xs transition-[height,background-color] duration-500 ease-out',
              h === top ? 'bg-brand' : 'bg-blue-200 hover:bg-blue-400',
            )}
            style={{ height: `${h}%` }}
          >
            <span className="pointer-events-none absolute bottom-full left-1/2 mb-1.5 -translate-x-1/2 rounded-md bg-ink px-1.5 py-0.5 text-[11px] font-semibold whitespace-nowrap text-white opacity-0 transition-opacity duration-150 group-hover/bar:opacity-100">
              {DAYS[i]} · {money0(h * RUPEES_PER_POINT)}
            </span>
          </span>
        ))}
      </span>
    </button>
  );
}

export function Bento() {
  const { checkout, reports, small, more } = bento;
  return (
    <Container as="section" aria-labelledby="bento-title" className="pt-24 pb-10">
      <div className="mb-10 flex flex-wrap items-end justify-between gap-6">
        <SectionTitle className="max-w-[560px]">
          <span id="bento-title">Everything your counter needs. Nothing it does not.</span>
        </SectionTitle>
        <p className="max-w-[420px] text-[17px] leading-relaxed text-body">
          From the first bill of the morning to the stock check at night, MintoBill keeps every number in one place.
        </p>
      </div>

      <div className="grid gap-4 sm:grid-cols-2 sm:gap-5 lg:grid-cols-6">
        <article className={cn(card, 'flex flex-col gap-[18px] border-ink bg-ink p-6 text-slate-200 sm:order-1 sm:col-span-2 sm:p-8 lg:order-none lg:col-span-4')}>
          <IconTile icon={checkout.icon} tone="dark" />
          <h3 className="text-2xl font-bold text-white">{checkout.title}</h3>
          <p className="max-w-[520px] leading-relaxed text-slate-300">{checkout.body}</p>
          <ul className="mt-auto flex flex-wrap gap-2.5">
            {checkout.tags.map((t) => (
              <li key={t} className="rounded-full border border-slate-400/30 px-3.5 py-2 text-sm">
                {t}
              </li>
            ))}
          </ul>
        </article>

        {small.map((c, i) => (
          <SmallCard key={c.title} {...c} className={SMALL_SPANS[i]} />
        ))}

        <article className={cn(card, 'flex flex-wrap items-end gap-7 border-line bg-surface p-6 sm:order-6 sm:col-span-2 sm:p-8 lg:order-none lg:col-span-4')}>
          <div className="flex flex-[1_1_240px] flex-col gap-3.5">
            <IconTile icon={reports.icon} />
            <h3 className="text-2xl font-bold text-ink">{reports.title}</h3>
            <p className="leading-relaxed text-body">{reports.body}</p>
          </div>
          <SalesChart />
        </article>

        {more.map((c, i) => (
          <SmallCard key={c.title} {...c} className={MORE_SPANS[i]} />
        ))}
      </div>
    </Container>
  );
}
