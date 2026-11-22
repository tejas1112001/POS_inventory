import { ArrowLeft } from 'lucide-react';
import { usePageMeta } from '@/lib/usePageMeta';
import { Button } from '@/components/ui/Button';
import { Container } from '@/components/ui/Heading';

export default function NotFound() {
  usePageMeta('Page not found | BilMitra');
  return (
    <section className="bg-night surface-glow">
      <Container className="py-28 text-center">
        <p className="text-[13px] font-bold tracking-[0.1em] text-brand-bright uppercase">404</p>
        <h1 className="mt-3 text-[40px] font-extrabold tracking-[-0.03em] text-white">We could not find that page</h1>
        <p className="mt-3 text-lg text-slate-300">The link may be old or mistyped.</p>
        <Button to="/" size="lg" className="mt-8">
          <ArrowLeft className="size-[18px]" aria-hidden="true" />
          Back to home
        </Button>
      </Container>
    </section>
  );
}
