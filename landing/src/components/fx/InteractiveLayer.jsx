import { useEffect, useRef } from 'react';
import { useMedia } from '@/lib/useMedia';
import { DotField } from './DotField';

// Surfaces whose hover glow follows the cursor (see surface-glow and card-lift in index.css).
const SPOT = '.surface-glow, .card-lift';
const CLICKABLE = 'a, button, [role="button"], summary, label, select';
const TYPING = 'input, textarea, [contenteditable="true"]';

/**
 * Site-wide mouse effects, mounted once in Layout. Mouse and trackpad only:
 * - cursor glow on .surface-glow and .card-lift surfaces (sets --mx / --my)
 * - a ring that trails the cursor and grows over clickable things
 * - a ripple on every click
 * - [data-tilt="deg"] elements tilt toward the cursor
 * - [data-magnetic] elements lean toward the cursor
 * - the dot field in the side gutters
 * Movement is skipped when the user prefers reduced motion.
 */
export function InteractiveLayer() {
  const fine = useMedia('(hover: hover) and (pointer: fine)');
  const calm = useMedia('(prefers-reduced-motion: reduce)');
  const ringRef = useRef(null);

  useEffect(() => {
    if (!fine) return undefined;
    const ring = ringRef.current;
    let x = -100;
    let y = -100;
    let rx = x;
    let ry = y;
    let frame = 0;
    // Hovered tilt / magnetic element and its rect, measured before it moves.
    let tilt = null;
    let tiltRect = null;
    let mag = null;
    let magRect = null;

    const follow = () => {
      rx += (x - rx) * 0.22;
      ry += (y - ry) * 0.22;
      if (ring) ring.style.transform = `translate3d(${rx.toFixed(1)}px, ${ry.toFixed(1)}px, 0)`;
      frame = Math.abs(x - rx) + Math.abs(y - ry) > 0.2 ? requestAnimationFrame(follow) : 0;
    };

    const release = (el) => {
      if (el) el.style.transform = '';
    };

    const onMove = (e) => {
      x = e.clientX;
      y = e.clientY;
      const t = e.target instanceof Element ? e.target : null;

      for (let el = t?.closest(SPOT); el; el = el.parentElement?.closest(SPOT)) {
        const r = el.getBoundingClientRect();
        el.style.setProperty('--mx', `${x - r.left}px`);
        el.style.setProperty('--my', `${y - r.top}px`);
      }

      if (ring) {
        ring.dataset.visible = 'true';
        ring.dataset.state = t?.closest(TYPING) ? 'text' : t?.closest(CLICKABLE) ? 'link' : '';
      }

      if (!calm) {
        const nextTilt = t?.closest('[data-tilt]') ?? null;
        if (nextTilt !== tilt) {
          release(tilt);
          tilt = nextTilt;
          tiltRect = tilt?.getBoundingClientRect() ?? null;
        }
        if (tilt && tiltRect) {
          const max = Number(tilt.dataset.tilt) || 6;
          const px = (x - tiltRect.left) / tiltRect.width - 0.5;
          const py = (y - tiltRect.top) / tiltRect.height - 0.5;
          tilt.style.transform = `perspective(1400px) rotateX(${(-py * max).toFixed(2)}deg) rotateY(${(px * max).toFixed(2)}deg)`;
        }

        const nextMag = t?.closest('[data-magnetic]') ?? null;
        if (nextMag !== mag) {
          release(mag);
          mag = nextMag;
          magRect = mag?.getBoundingClientRect() ?? null;
        }
        if (mag && magRect) {
          const dx = x - (magRect.left + magRect.width / 2);
          const dy = y - (magRect.top + magRect.height / 2);
          mag.style.transform = `translate(${(dx * 0.18).toFixed(1)}px, ${(dy * 0.3).toFixed(1)}px)`;
        }
      }

      if (!frame) frame = requestAnimationFrame(follow);
    };

    const onScroll = () => {
      // Rects go stale when the page scrolls under a still cursor.
      tiltRect = tilt?.getBoundingClientRect() ?? null;
      magRect = mag?.getBoundingClientRect() ?? null;
    };

    const onOut = (e) => {
      if (e.relatedTarget) return;
      if (ring) ring.dataset.visible = 'false';
      release(tilt);
      release(mag);
      tilt = mag = tiltRect = magRect = null;
    };

    const onDown = (e) => {
      if (ring) ring.dataset.down = 'true';
      if (calm) return;
      const dot = document.createElement('span');
      dot.className = 'click-ripple';
      dot.setAttribute('aria-hidden', 'true');
      dot.style.left = `${e.clientX}px`;
      dot.style.top = `${e.clientY}px`;
      dot.addEventListener('animationend', () => dot.remove(), { once: true });
      document.body.appendChild(dot);
    };

    const onUp = () => {
      if (ring) ring.dataset.down = 'false';
    };

    window.addEventListener('pointermove', onMove, { passive: true });
    window.addEventListener('pointerdown', onDown, { passive: true });
    window.addEventListener('pointerup', onUp, { passive: true });
    window.addEventListener('scroll', onScroll, { passive: true });
    window.addEventListener('mouseout', onOut);
    return () => {
      cancelAnimationFrame(frame);
      release(tilt);
      release(mag);
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerdown', onDown);
      window.removeEventListener('pointerup', onUp);
      window.removeEventListener('scroll', onScroll);
      window.removeEventListener('mouseout', onOut);
    };
  }, [fine, calm]);

  if (!fine) return null;
  return (
    <>
      <DotField calm={calm} />
      {!calm && <div ref={ringRef} className="cursor-ring" aria-hidden="true" />}
    </>
  );
}
