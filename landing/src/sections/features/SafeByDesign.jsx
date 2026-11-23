import { BadgeIndianRupee, DatabaseBackup, History, ShieldCheck } from 'lucide-react';
import { Container } from '@/components/ui/Heading';
import { IconTile } from '@/components/ui/IconTile';

const items = [
  { icon: ShieldCheck, t: 'Your data stays yours', d: 'Each shop has its own separate data space. No other shop can see it.' },
  { icon: BadgeIndianRupee, t: 'Prices checked every time', d: 'The server checks every price. A cashier cannot change one without permission.' },
  { icon: History, t: 'Every change recorded', d: 'Stock changes, voids and returns are all written down with who did them.' },
  { icon: DatabaseBackup, t: 'Nothing deleted by surprise', d: 'If a plan ends, your shop becomes read-only first. Your data waits for you.' },
];

export function SafeByDesign() {
  return (
    <Container as="section" aria-labelledby="safe-title" className="pb-20">
      <div className="rounded-[26px] border border-line bg-surface p-6 sm:p-10">
        <h2 id="safe-title" className="text-[28px] font-extrabold tracking-[-0.03em] text-ink sm:text-[32px]">
          Safe by design
        </h2>
        <p className="mt-2 mb-7 text-body">Your shop&apos;s numbers are your business. We built MintoBill to protect them.</p>
        <ul className="grid grid-cols-[repeat(auto-fit,minmax(230px,1fr))] gap-4">
          {items.map((it) => (
            <li key={it.t} className="rounded-[18px] border border-line bg-white p-[22px]">
              <IconTile icon={it.icon} size="sm" className="size-10 rounded-xl" />
              <h3 className="mt-3.5 mb-1.5 text-[17px] font-bold text-ink">{it.t}</h3>
              <p className="text-sm leading-relaxed text-body">{it.d}</p>
            </li>
          ))}
        </ul>
      </div>
    </Container>
  );
}
