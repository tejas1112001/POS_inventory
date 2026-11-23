import { ArrowRight } from 'lucide-react';
import { Button } from '@/components/ui/Button';
import { Container } from '@/components/ui/Heading';

export function CtaBand() {
  return (
    <Container as="section" aria-labelledby="cta-title" className="pb-24">
      <div className="bg-night surface-glow flex flex-wrap items-center justify-between gap-7 rounded-[28px] px-6 py-12 sm:px-12 sm:py-14">
        <div className="max-w-[600px]">
          <h2 id="cta-title" className="text-[30px] leading-[1.1] font-extrabold tracking-[-0.03em] text-white sm:text-[38px]">
            Ready to bill faster?
          </h2>
          <p className="mt-3 text-[17px] leading-relaxed text-slate-300">
            Try MintoBill free for 14 days. We help you set up your shop.
          </p>
        </div>
        <div className="flex flex-wrap gap-3">
          <Button to="/contact#demo" size="lg">
            Book a free demo
            <ArrowRight className="size-[18px]" aria-hidden="true" />
          </Button>
          <Button to="/pricing" variant="ghost" size="lg">
            See pricing
          </Button>
        </div>
      </div>
    </Container>
  );
}
