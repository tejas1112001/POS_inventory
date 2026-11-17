import { bento } from '@/content/site';
import { cn } from '@/lib/cn';
import { Container, SectionTitle } from '@/components/ui/Heading';
import { IconTile } from '@/components/ui/IconTile';

const card = 'card-lift min-w-0 rounded-[22px] border';

function SmallCard({ icon, title, body }) {
  return (
    <article className={cn(card, 'flex flex-[1_1_280px] flex-col gap-3.5 border-line bg-white p-7')}>
      <IconTile icon={icon} />
      <h3 className="text-xl font-bold text-ink">{title}</h3>
      <p className="text-[15px] leading-relaxed text-body">{body}</p>
    </article>
  );
}

// Decorative bar chart for the reports card.
const BARS = [38, 55, 46, 72, 64, 92];

export function Bento() {
  const { checkout, reports, small, more } = bento;
  return (
    <Container as="section" aria-labelledby="bento-title" className="pt-24 pb-10">
      <div className="mb-10 flex flex-wrap items-end justify-between gap-6">
        <SectionTitle className="max-w-[560px]">
          <span id="bento-title">Everything your counter needs. Nothing it does not.</span>
        </SectionTitle>
        <p className="max-w-[420px] text-[17px] leading-relaxed text-body">
          From the first bill of the morning to the stock check at night, BillPiko keeps every number in one place.
        </p>
      </div>

      <div className="flex flex-wrap gap-5">
        <article className={cn(card, 'flex flex-[2_1_560px] flex-col gap-[18px] border-ink bg-ink p-8 text-slate-200')}>
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

        {small.map((c) => (
          <SmallCard key={c.title} {...c} />
        ))}

        <article className={cn(card, 'flex flex-[2_1_560px] flex-wrap items-end gap-7 border-line bg-surface p-8')}>
          <div className="flex flex-[1_1_240px] flex-col gap-3.5">
            <IconTile icon={reports.icon} />
            <h3 className="text-2xl font-bold text-ink">{reports.title}</h3>
            <p className="leading-relaxed text-body">{reports.body}</p>
          </div>
          <div aria-hidden="true" className="flex h-[150px] flex-[1_1_220px] items-end gap-2.5 rounded-[14px] border border-line bg-white p-4">
            {BARS.map((h, i) => (
              <div
                key={i}
                className={cn('flex-1 rounded-t-md rounded-b-xs', i === BARS.length - 1 ? 'bg-brand' : i > 2 ? 'bg-blue-300' : 'bg-blue-200')}
                style={{ height: `${h}%` }}
              />
            ))}
          </div>
        </article>

        {more.map((c) => (
          <SmallCard key={c.title} {...c} />
        ))}
      </div>
    </Container>
  );
}
