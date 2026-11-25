import { Link } from 'react-router-dom';
import { BASE_DOMAIN } from '@/lib/config';
import { Container } from '@/components/ui/Heading';
import { LogoMark, Wordmark } from '@/components/ui/Logo';

const columns = [
  {
    title: 'Product',
    links: [
      { to: '/features', label: 'Features' },
      { to: '/industries', label: 'Industries' },
      { to: '/pricing', label: 'Pricing' },
    ],
  },
  {
    title: 'Company',
    links: [
      { to: '/contact#demo', label: 'Book a demo' },
      { to: '/contact#find-shop', label: 'Find my shop' },
    ],
  },
  {
    title: 'Legal',
    links: [
      { href: '/terms', label: 'Terms of service' },
      { href: '/privacy', label: 'Privacy policy' },
    ],
  },
];

const linkCls = 'inline-flex min-h-10 items-center text-[15px] text-slate-400 transition-colors hover:text-white';

export function Footer() {
  return (
    <footer className="bg-night text-slate-400">
      <Container className="flex flex-wrap justify-between gap-10 pt-14 pb-7">
        <div className="max-w-[360px] flex-[1_1_280px]">
          <div className="flex items-center gap-2.5">
            <LogoMark size={30} />
            <Wordmark className="text-[19px]" />
          </div>
          <p className="mt-3.5 text-[15px] leading-relaxed">Billing, stock and GST for Indian shops. Made in Pune.</p>
        </div>

        <nav aria-label="Footer" className="grid w-full grid-cols-3 gap-4 sm:flex sm:w-auto sm:flex-wrap sm:gap-12">
          {columns.map((c) => (
            <div key={c.title}>
              <p className="mb-1.5 text-[13px] font-bold tracking-[0.08em] text-slate-200 uppercase">{c.title}</p>
              <ul>
                {c.links.map((l) => (
                  <li key={l.label}>
                    {l.to ? (
                      <Link to={l.to} className={linkCls}>
                        {l.label}
                      </Link>
                    ) : (
                      // Terms and privacy are served by the platform (backend/internal/landing).
                      <a href={l.href} className={linkCls}>
                        {l.label}
                      </a>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </nav>
      </Container>
      {/* The year is baked in at build time; the browser may be in a newer one. */}
      <Container className="border-t border-slate-400/15 pt-5 pb-8 text-sm" suppressHydrationWarning>
        © {new Date().getFullYear()} MintoBill · {BASE_DOMAIN}
      </Container>
    </footer>
  );
}
