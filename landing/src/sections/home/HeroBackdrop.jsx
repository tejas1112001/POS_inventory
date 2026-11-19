import { lazy, Suspense } from 'react';
import { useIdle } from '@/lib/useIdle';

const HeroParticles = lazy(() => import('./HeroParticles'));

/**
 * Animated background for the home hero: drifting aurora light (pure CSS),
 * a glow that trails the cursor, and a particle network. The particles load
 * after the page is idle so they never slow the first paint.
 */
export function HeroBackdrop() {
  const idle = useIdle();
  return (
    <div aria-hidden="true" className="pointer-events-none absolute inset-0 -z-10 overflow-hidden">
      <div className="aurora aurora-1" />
      <div className="aurora aurora-2" />
      <div className="aurora aurora-3" />
      <div className="aurora-cursor" />
      {idle && (
        <Suspense fallback={null}>
          <HeroParticles />
        </Suspense>
      )}
    </div>
  );
}
