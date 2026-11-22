import { trustStrip } from '@/content/site';
import { Container } from '@/components/ui/Heading';

export function TrustStrip() {
  return (
    <section aria-label="Why shops choose BilMitra" className="border-b border-line bg-surface">
      <Container as="ul" className="flex flex-wrap justify-between gap-x-10 gap-y-6 py-7">
        {trustStrip.map(({ icon: Icon, label }) => (
          <li key={label} className="group flex flex-[1_1_220px] items-center gap-3">
            <Icon
              className="size-[22px] shrink-0 text-brand transition-transform duration-300 ease-out group-hover:-translate-y-0.5 group-hover:scale-125"
              strokeWidth={1.8}
              aria-hidden="true"
            />
            <span className="text-[15px] font-semibold text-ink">{label}</span>
          </li>
        ))}
      </Container>
    </section>
  );
}
