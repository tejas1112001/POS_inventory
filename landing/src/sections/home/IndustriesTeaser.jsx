import { Link } from 'react-router-dom';
import { ArrowRight } from 'lucide-react';
import { shopTypes } from '@/content/site';
import { Container, SectionTitle } from '@/components/ui/Heading';

export function IndustriesTeaser() {
  return (
    <section aria-labelledby="ind-title" className="border-y border-line bg-surface">
      <Container className="py-[88px]">
        <div className="mb-9 flex flex-wrap items-end justify-between gap-6">
          <div className="max-w-[640px]">
            <SectionTitle>
              <span id="ind-title">Works for clothing, pharmacy, grocery and more</span>
            </SectionTitle>
            <p className="mt-3 text-[17px] leading-relaxed text-body">
              If you sell from a counter, MintoBill fits. Turn on the tools your shop needs and leave the rest off.
            </p>
          </div>
          <Link
            to="/industries"
            className="group inline-flex min-h-11 items-center gap-2 font-bold text-brand-strong hover:text-brand"
          >
            Find the tools for your shop
            <ArrowRight className="size-[18px] transition-transform duration-150 group-hover:translate-x-0.5" aria-hidden="true" />
          </Link>
        </div>

        <ul className="grid grid-cols-2 gap-2.5 sm:flex sm:flex-wrap sm:gap-3">
          {shopTypes.map(({ slug, name, icon: Icon, tint }) => (
            <li key={slug}>
              <Link
                to={`/industries?shop=${slug}#tools`}
                className="card-lift flex min-h-12 items-center gap-2.5 rounded-2xl border border-line bg-white py-2.5 pr-3 pl-2.5 text-sm leading-snug font-semibold text-ink sm:rounded-full sm:pr-[18px] sm:text-[15px]"
              >
                <span className={`flex size-[30px] items-center justify-center rounded-full ${tint}`}>
                  <Icon className="size-4" strokeWidth={2} aria-hidden="true" />
                </span>
                {name}
              </Link>
            </li>
          ))}
        </ul>
      </Container>
    </section>
  );
}
