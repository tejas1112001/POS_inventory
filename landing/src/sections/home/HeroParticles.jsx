import { useEffect, useRef } from 'react';
import { useMedia } from '@/lib/useMedia';

const LINK = 120; // particles closer than this are joined by a line
const REACH = 200; // cursor pull radius
const PERSONAL = 46; // particles keep this far from the cursor
const LIGHT = 260; // lines light up within this distance of the cursor
const SHOCK = 260; // click push radius
const RING_LIFE = 900;

/**
 * Floating particles for the home hero backdrop. They join into a network
 * around the cursor and scatter on click, and the component also moves the
 * backdrop's cursor glow (--hx / --hy / data-hover on its parent). Loaded
 * lazily once the page is idle (see HeroBackdrop). Mouse effects need a fine
 * pointer; with reduced motion the particles are drawn once and stay still.
 */
export default function HeroParticles() {
  const canvasRef = useRef(null);
  const fine = useMedia('(hover: hover) and (pointer: fine)');
  const calm = useMedia('(prefers-reduced-motion: reduce)');

  useEffect(() => {
    const canvas = canvasRef.current;
    const root = canvas?.parentElement;
    const ctx = canvas?.getContext('2d');
    if (!root || !ctx) return undefined;

    let w = 0;
    let h = 0;
    let parts = [];
    let rings = [];
    let mx = 0;
    let my = 0;
    let inside = false;
    let visible = true;
    let frame = 0;
    let last = 0;

    const seed = () => {
      const n = Math.min(120, Math.round((w * h) / 12000));
      parts = Array.from({ length: n }, () => {
        const vx = (Math.random() - 0.5) * 0.3;
        const vy = (Math.random() - 0.5) * 0.3 - 0.06;
        return { x: Math.random() * w, y: Math.random() * h, vx, vy, bvx: vx, bvy: vy, r: 0.6 + Math.random() * 1.4 };
      });
    };

    const step = (dt) => {
      for (const p of parts) {
        if (inside) {
          const dx = mx - p.x;
          const dy = my - p.y;
          const d = Math.hypot(dx, dy);
          if (d > 1 && d < REACH) {
            const f = d < PERSONAL ? -0.05 * (1 - d / PERSONAL) : 0.012 * (1 - d / REACH);
            p.vx += (dx / d) * f * dt;
            p.vy += (dy / d) * f * dt;
          }
        }
        // Ease back to the particle's own slow drift.
        p.vx += (p.bvx - p.vx) * 0.02 * dt;
        p.vy += (p.bvy - p.vy) * 0.02 * dt;
        p.x += p.vx * dt;
        p.y += p.vy * dt;
        if (p.x < -10) p.x = w + 10;
        else if (p.x > w + 10) p.x = -10;
        if (p.y < -10) p.y = h + 10;
        else if (p.y > h + 10) p.y = -10;
      }
    };

    const draw = (now) => {
      ctx.clearRect(0, 0, w, h);
      ctx.lineWidth = 1;

      for (let i = 0; i < parts.length; i++) {
        const a = parts[i];
        for (let j = i + 1; j < parts.length; j++) {
          const b = parts[j];
          const dx = a.x - b.x;
          const dy = a.y - b.y;
          if (dx > LINK || dx < -LINK || dy > LINK || dy < -LINK) continue;
          const d = Math.hypot(dx, dy);
          if (d >= LINK) continue;
          const close = 1 - d / LINK;
          let alpha = close * 0.08;
          if (inside) {
            const near = 1 - Math.hypot((a.x + b.x) / 2 - mx, (a.y + b.y) / 2 - my) / LIGHT;
            if (near > 0) alpha += near * close * 0.55;
          }
          ctx.strokeStyle = `rgba(63, 208, 189, ${alpha.toFixed(3)})`;
          ctx.beginPath();
          ctx.moveTo(a.x, a.y);
          ctx.lineTo(b.x, b.y);
          ctx.stroke();
        }
      }

      if (inside) {
        for (const p of parts) {
          const d = Math.hypot(p.x - mx, p.y - my);
          if (d >= REACH) continue;
          ctx.strokeStyle = `rgba(127, 214, 200, ${((1 - d / REACH) * 0.35).toFixed(3)})`;
          ctx.beginPath();
          ctx.moveTo(p.x, p.y);
          ctx.lineTo(mx, my);
          ctx.stroke();
        }
      }

      for (const p of parts) {
        const near = inside ? Math.max(0, 1 - Math.hypot(p.x - mx, p.y - my) / LIGHT) : 0;
        ctx.fillStyle = `rgba(216, 242, 237, ${(0.35 + near * 0.6).toFixed(3)})`;
        ctx.beginPath();
        ctx.arc(p.x, p.y, p.r + near * 1.6, 0, Math.PI * 2);
        ctx.fill();
      }

      rings = rings.filter((r) => now - r.t < RING_LIFE);
      for (const r of rings) {
        const k = (now - r.t) / RING_LIFE;
        ctx.strokeStyle = `rgba(127, 214, 200, ${(0.6 * (1 - k)).toFixed(3)})`;
        ctx.lineWidth = 2;
        ctx.beginPath();
        ctx.arc(r.x, r.y, 20 + k * SHOCK, 0, Math.PI * 2);
        ctx.stroke();
      }
    };

    const tick = (now) => {
      const dt = Math.min(3, (now - (last || now)) / 16.67);
      last = now;
      step(dt);
      draw(now);
      frame = requestAnimationFrame(tick);
    };

    const start = () => {
      if (calm || frame || !visible || document.hidden) return;
      last = 0;
      frame = requestAnimationFrame(tick);
    };
    const stop = () => {
      cancelAnimationFrame(frame);
      frame = 0;
    };

    const resize = () => {
      const dpr = Math.min(window.devicePixelRatio || 1, 2);
      w = root.clientWidth;
      h = root.clientHeight;
      canvas.width = Math.round(w * dpr);
      canvas.height = Math.round(h * dpr);
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      seed();
      draw(performance.now());
    };

    const onMove = (e) => {
      const r = root.getBoundingClientRect();
      mx = e.clientX - r.left;
      my = e.clientY - r.top;
      inside = mx >= 0 && my >= 0 && mx <= r.width && my <= r.height;
      root.dataset.hover = String(inside);
      if (inside) {
        root.style.setProperty('--hx', `${mx}px`);
        root.style.setProperty('--hy', `${my}px`);
      }
    };

    const onDown = (e) => {
      const r = root.getBoundingClientRect();
      const x = e.clientX - r.left;
      const y = e.clientY - r.top;
      if (x < 0 || y < 0 || x > r.width || y > r.height) return;
      rings.push({ x, y, t: performance.now() });
      for (const p of parts) {
        const dx = p.x - x;
        const dy = p.y - y;
        const d = Math.hypot(dx, dy);
        if (d > 1 && d < SHOCK) {
          const push = (1 - d / SHOCK) * 7;
          p.vx += (dx / d) * push;
          p.vy += (dy / d) * push;
        }
      }
    };

    const onOut = (e) => {
      if (e.relatedTarget) return;
      inside = false;
      root.dataset.hover = 'false';
    };

    const onVisibility = () => (document.hidden ? stop() : start());

    const sizeObserver = new ResizeObserver(resize);
    sizeObserver.observe(root);
    const viewObserver = new IntersectionObserver(([entry]) => {
      visible = entry.isIntersecting;
      if (visible) start();
      else stop();
    });
    viewObserver.observe(root);
    document.addEventListener('visibilitychange', onVisibility);
    if (fine && !calm) {
      window.addEventListener('pointermove', onMove, { passive: true });
      window.addEventListener('pointerdown', onDown, { passive: true });
      window.addEventListener('mouseout', onOut);
    }
    resize();
    start();

    return () => {
      stop();
      sizeObserver.disconnect();
      viewObserver.disconnect();
      document.removeEventListener('visibilitychange', onVisibility);
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerdown', onDown);
      window.removeEventListener('mouseout', onOut);
    };
  }, [fine, calm]);

  return <canvas ref={canvasRef} className="absolute inset-0 size-full" />;
}
