import { lazy, Suspense, useEffect, useRef } from 'react';
import { Outlet, useLocation } from 'react-router-dom';
import { useIdle } from '@/lib/useIdle';
import { CtaBand } from './CtaBand';
import { Footer } from './Footer';
import { Header } from './Header';

// Mouse effects are decoration: load them only once the page is idle.
const InteractiveLayer = lazy(() =>
  import('@/components/fx/InteractiveLayer').then((m) => ({ default: m.InteractiveLayer })),
);

/**
 * Page frame: skip link, header, routed page, closing CTA and footer.
 * On navigation it scrolls to the top (or to the #hash target) and moves
 * keyboard focus to <main> so screen readers announce the new page.
 */
export function Layout() {
  const { pathname, hash } = useLocation();
  const mainRef = useRef(null);
  const firstRender = useRef(true);
  const idle = useIdle();

  useEffect(() => {
    if (firstRender.current) {
      firstRender.current = false;
      if (!hash) return;
    }
    if (hash) {
      // A lazy page may still be loading: look for the target each frame,
      // for up to about 3 seconds.
      let frame;
      let tries = 0;
      const find = () => {
        const el = document.getElementById(decodeURIComponent(hash.slice(1)));
        if (el) {
          el.scrollIntoView({ block: 'start' });
          el.focus?.({ preventScroll: true });
        } else if (tries++ < 180) {
          frame = requestAnimationFrame(find);
        }
      };
      frame = requestAnimationFrame(find);
      return () => cancelAnimationFrame(frame);
    }
    window.scrollTo(0, 0);
    mainRef.current?.focus({ preventScroll: true });
  }, [pathname, hash]);

  return (
    <div className="flex min-h-dvh flex-col">
      {idle && (
        <Suspense fallback={null}>
          <InteractiveLayer />
        </Suspense>
      )}
      <a
        href="#main"
        className="sr-only z-50 rounded-lg bg-white px-4 py-3 font-semibold text-ink focus:not-sr-only focus:fixed focus:top-3 focus:left-3"
      >
        Skip to main content
      </a>
      <Header />
      <main id="main" ref={mainRef} tabIndex={-1} className="flex-1 outline-none">
        <Suspense fallback={<div className="bg-night min-h-[60vh]" aria-busy="true" />}>
          <Outlet />
        </Suspense>
        {pathname !== '/contact' && <CtaBand />}
      </main>
      <Footer />
    </div>
  );
}
