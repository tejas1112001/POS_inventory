import { NotebookPen, Sparkles } from 'lucide-react';
import { versus } from '@/content/site';
import { CheckList } from '@/components/ui/CheckList';
import { Container } from '@/components/ui/Heading';

export function Versus() {
  return (
    <Container as="section" aria-labelledby="vs-title" className="pt-10 pb-[72px]">
      <h2 id="vs-title" className="mb-7 text-[30px] leading-[1.1] font-extrabold tracking-[-0.03em] text-ink sm:text-[38px]">
        From notebook to MintoBill
      </h2>
      <div className="flex flex-wrap gap-5">
        <div className="flex-[1_1_360px] rounded-[22px] border border-line bg-white p-7">
          <h3 className="flex items-center gap-2 text-sm font-bold tracking-[0.08em] text-muted uppercase">
            <NotebookPen className="size-[18px]" aria-hidden="true" />
            Notebook and calculator
          </h3>
          <CheckList items={versus.map((v) => v.old)} tone="muted" cross className="mt-[18px] gap-3.5" />
        </div>
        <div className="flex-[1_1_360px] rounded-[22px] bg-ink p-7">
          <h3 className="flex items-center gap-2 text-sm font-bold tracking-[0.08em] text-brand-bright uppercase">
            <Sparkles className="size-[18px]" aria-hidden="true" />
            With MintoBill
          </h3>
          <CheckList items={versus.map((v) => v.now)} tone="dark" className="mt-[18px] gap-3.5" />
        </div>
      </div>
    </Container>
  );
}
