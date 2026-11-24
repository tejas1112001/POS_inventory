import { FileSpreadsheet, ReceiptText, Store } from 'lucide-react';
import { steps } from '@/content/site';
import { Container, Eyebrow, SectionTitle } from '@/components/ui/Heading';

const icons = [Store, FileSpreadsheet, ReceiptText];
const borders = ['border-brand', 'border-blue-300', 'border-blue-200'];

export function HowItWorks() {
  return (
    <Container as="section" aria-labelledby="how-title" className="pt-[72px] pb-24">
      <Eyebrow>How it works</Eyebrow>
      <SectionTitle className="mt-3 mb-10">
        <span id="how-title">Start billing in three steps</span>
      </SectionTitle>
      <ol className="grid gap-8 md:grid-cols-3 md:gap-5">
        {steps.map((s, i) => {
          const Icon = icons[i];
          return (
            <li key={s.title} className={`group relative border-t-[3px] px-1 pt-6 ${borders[i]}`}>
              {/* On hover a brand line runs across the top border. */}
              <span
                aria-hidden="true"
                className="absolute -top-[3px] left-0 h-[3px] w-0 bg-brand transition-[width] duration-500 ease-out group-hover:w-full"
              />
              <div className="flex items-center gap-3">
                <span className="text-[15px] font-extrabold text-brand">0{i + 1}</span>
                <Icon
                  className="size-5 text-brand transition-transform duration-300 ease-out group-hover:scale-125 group-hover:-rotate-12"
                  strokeWidth={1.8}
                  aria-hidden="true"
                />
              </div>
              <h3 className="mt-2.5 mb-2 text-[21px] font-bold text-ink">{s.title}</h3>
              <p className="text-[15px] leading-relaxed text-body">{s.body}</p>
            </li>
          );
        })}
      </ol>
    </Container>
  );
}
