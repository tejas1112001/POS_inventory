import { Blocks, FileSpreadsheet, Undo2 } from 'lucide-react';
import { Container } from '@/components/ui/Heading';
import { IconTile } from '@/components/ui/IconTile';

const items = [
  { icon: FileSpreadsheet, t: 'Import your catalogue', d: 'Use our spreadsheet template for your kind of shop. Fill it once and import everything.' },
  { icon: Undo2, t: 'Your own return rules', d: 'Choose exchange, store credit or refund, and how many days customers have.' },
  { icon: Blocks, t: 'Grows with your shop', d: 'New tools are added as modules. Switch them on when your shop needs them.' },
];

export function Extras() {
  return (
    <Container as="section" aria-label="Also included" className="pb-20">
      <ul className="grid grid-cols-[repeat(auto-fit,minmax(260px,1fr))] gap-4">
        {items.map((it) => (
          <li key={it.t} className="rounded-[20px] border border-line bg-surface p-6">
            <IconTile icon={it.icon} size="sm" className="mb-3.5" />
            <h3 className="mb-1.5 text-lg font-bold text-ink">{it.t}</h3>
            <p className="text-[15px] leading-relaxed text-body">{it.d}</p>
          </li>
        ))}
      </ul>
    </Container>
  );
}
