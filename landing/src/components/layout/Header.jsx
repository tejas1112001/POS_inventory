import { useEffect, useState } from 'react';
import { NavLink, useLocation } from 'react-router-dom';
import { Menu, X } from 'lucide-react';
import { nav } from '@/content/site';
import { cn } from '@/lib/cn';
import { Button } from '@/components/ui/Button';
import { Container } from '@/components/ui/Heading';
import { Logo } from '@/components/ui/Logo';

const pill = ({ isActive }) =>
  cn(
    'inline-flex min-h-11 items-center rounded-full border px-4 text-[15px] transition-colors duration-150',
    isActive
      ? 'border-blue-400/35 bg-blue-600/20 font-semibold text-white'
      : 'border-transparent font-medium text-slate-300 hover:text-white',
  );

export function Header() {
  const [open, setOpen] = useState(false);
  const { pathname } = useLocation();

  // Close the mobile menu on navigation and on Escape.
  useEffect(() => setOpen(false), [pathname]);
  useEffect(() => {
    if (!open) return;
    const onKey = (e) => e.key === 'Escape' && setOpen(false);
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open]);

  return (
    <header className="bg-night bg-grid border-b border-slate-400/15 text-slate-200">
      <Container className="flex items-center justify-between gap-4 py-4">
        <Logo />

        <nav aria-label="Main" className="hidden lg:block">
          <ul className="flex gap-1">
            {nav.map((n) => (
              <li key={n.to}>
                <NavLink to={n.to} end={n.to === '/'} className={pill}>
                  {n.label}
                </NavLink>
              </li>
            ))}
          </ul>
        </nav>

        <div className="hidden items-center gap-2.5 sm:flex">
          <Button to="/contact#find-shop" variant="ghost">
            Find my shop
          </Button>
          <Button to="/contact#demo">Book a demo</Button>
        </div>

        <button
          type="button"
          className="inline-flex size-11 items-center justify-center rounded-xl border border-slate-400/35 text-white lg:hidden"
          aria-expanded={open}
          aria-controls="mobile-menu"
          aria-label={open ? 'Close menu' : 'Open menu'}
          onClick={() => setOpen((o) => !o)}
        >
          {open ? <X className="size-5" aria-hidden="true" /> : <Menu className="size-5" aria-hidden="true" />}
        </button>
      </Container>

      {open && (
        <div id="mobile-menu" className="border-t border-slate-400/15 lg:hidden">
          <Container as="nav" aria-label="Main" className="flex flex-col gap-1 py-4">
            {nav.map((n) => (
              <NavLink key={n.to} to={n.to} end={n.to === '/'} className={pill}>
                {n.label}
              </NavLink>
            ))}
            <div className="mt-3 flex flex-wrap gap-2.5 sm:hidden">
              <Button to="/contact#find-shop" variant="ghost" className="flex-1">
                Find my shop
              </Button>
              <Button to="/contact#demo" className="flex-1">
                Book a demo
              </Button>
            </div>
          </Container>
        </div>
      )}
    </header>
  );
}
