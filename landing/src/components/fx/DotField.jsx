import { useEffect, useRef } from 'react';

const GAP = 28; // dot spacing in px
const COLUMN = 600; // half the content width; dots are only drawn outside it
const REACH = 160; // cursor influence radius
const WAVE_SPEED = 0.55; // click wave, px per ms
const WAVE_BAND = 36;
const WAVE_LIFE = 1800;
const BIT_LIFE = 1400;
const BITS = ['₹', '₹', '%', '+'];

/**
 * Fixed canvas of dots behind the page, shown only in the empty side gutters
 * (a CSS mask hides the content column). Dots near the cursor light up and
 * drift away; a click sends a wave through them, and a click in a gutter
 * also throws a few ₹ symbols. With `calm` (reduced motion) the dots only
 * light up, without movement, waves or symbols.
 */
export function DotField({ calm }) {
  const ref = useRef(null);

  useEffect(() => {
    const canvas = ref.current;
    const ctx = canvas?.getContext('2d');
    if (!ctx) return undefined;

    let w = 0;
    let h = 0;
    let mx = -1e4;
    let my = -1e4;
    let frame = 0;
    let waves = [];
    let bits = [];

    const draw = (now) => {
      frame = 0;
      ctx.clearRect(0, 0, w, h);
      const left = w / 2 - COLUMN;
      const right = w / 2 + COLUMN;
      if (left <= 0) return; // no gutters at this width

      waves = waves.filter((v) => now - v.t < WAVE_LIFE);
      bits = bits.filter((b) => now - b.t < BIT_LIFE);

      // Dots drift a little slower than the page when scrolling.
      const shift = (window.scrollY * 0.3) % GAP;
      const lit = [];
      ctx.fillStyle = 'rgba(140, 165, 165, 0.45)';
      for (let x = GAP / 2; x < w; x += GAP) {
        if (x > left && x < right) continue;
        for (let y = GAP / 2 - shift; y < h + GAP; y += GAP) {
          let s = 0;
          let ox = 0;
          let oy = 0;
          const dx = x - mx;
          const dy = y - my;
          const d = Math.hypot(dx, dy);
          if (d < REACH) {
            s = 1 - d / REACH;
            if (!calm && d > 0) {
              const push = s * s * 12;
              ox = (dx / d) * push;
              oy = (dy / d) * push;
            }
          }
          for (const v of waves) {
            const age = now - v.t;
            const off = Math.abs(Math.hypot(x - v.x, y - v.y) - age * WAVE_SPEED);
            if (off < WAVE_BAND) s = Math.max(s, (1 - off / WAVE_BAND) * (1 - age / WAVE_LIFE));
          }
          if (s < 0.03) ctx.fillRect(x - 0.75, y - 0.75, 1.5, 1.5);
          else lit.push(x + ox, y + oy, s);
        }
      }

      for (let i = 0; i < lit.length; i += 3) {
        const s = lit[i + 2];
        ctx.fillStyle = `rgba(32, 182, 164, ${(0.3 + s * 0.7).toFixed(3)})`;
        ctx.beginPath();
        ctx.arc(lit[i], lit[i + 1], 0.9 + s * 2.2, 0, Math.PI * 2);
        ctx.fill();
      }

      ctx.font = '600 15px "Manrope Variable", system-ui, sans-serif';
      ctx.textAlign = 'center';
      for (const b of bits) {
        const t = (now - b.t) / 1000;
        ctx.fillStyle = `rgba(23, 128, 122, ${(1 - (now - b.t) / BIT_LIFE).toFixed(3)})`;
        ctx.fillText(b.ch, b.x + b.vx * t, b.y + b.vy * t + 260 * t * t);
      }

      if (waves.length || bits.length) frame = requestAnimationFrame(draw);
    };

    const schedule = () => {
      if (!frame) frame = requestAnimationFrame(draw);
    };

    const resize = () => {
      const dpr = Math.min(window.devicePixelRatio || 1, 2);
      w = document.documentElement.clientWidth;
      h = window.innerHeight;
      canvas.width = Math.round(w * dpr);
      canvas.height = Math.round(h * dpr);
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      schedule();
    };

    const onMove = (e) => {
      mx = e.clientX;
      my = e.clientY;
      schedule();
    };

    const onOut = (e) => {
      if (e.relatedTarget) return;
      mx = -1e4;
      my = -1e4;
      schedule();
    };

    const onDown = (e) => {
      if (calm) return;
      const now = performance.now();
      waves.push({ x: e.clientX, y: e.clientY, t: now });
      if (waves.length > 4) waves.shift();
      if (Math.abs(e.clientX - w / 2) > COLUMN) {
        for (let i = 0; i < 9; i++) {
          const a = -Math.PI / 2 + (Math.random() - 0.5) * 1.6;
          const sp = 140 + Math.random() * 160;
          bits.push({ x: e.clientX, y: e.clientY, vx: Math.cos(a) * sp, vy: Math.sin(a) * sp, t: now, ch: BITS[i % BITS.length] });
        }
      }
      schedule();
    };

    resize();
    window.addEventListener('resize', resize);
    window.addEventListener('scroll', schedule, { passive: true });
    window.addEventListener('pointermove', onMove, { passive: true });
    window.addEventListener('pointerdown', onDown, { passive: true });
    window.addEventListener('mouseout', onOut);
    return () => {
      cancelAnimationFrame(frame);
      window.removeEventListener('resize', resize);
      window.removeEventListener('scroll', schedule);
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerdown', onDown);
      window.removeEventListener('mouseout', onOut);
    };
  }, [calm]);

  return <canvas ref={ref} className="dot-field" aria-hidden="true" />;
}
