import { Container, Eyebrow } from '@/components/ui/Heading';

/** Dark title band at the top of every inner page. */
export function PageHero({ eyebrow, title, lead }) {
  return (
    <section className="bg-night surface-glow text-slate-200">
      <Container className="flex flex-wrap items-end justify-between gap-8 pt-14 pb-16 sm:pt-16 sm:pb-[72px]">
        <div className="min-w-0 flex-[1_1_280px]">
          <Eyebrow dark>{eyebrow}</Eyebrow>
        </div>
        <div className="min-w-0 flex-[1_1_520px]">
          <h1 className="text-[34px] leading-[1.08] font-extrabold tracking-[-0.03em] text-white sm:text-[46px]">
            {title}
          </h1>
          <p className="mt-4 text-lg leading-relaxed text-slate-300">{lead}</p>
        </div>
      </Container>
    </section>
  );
}
