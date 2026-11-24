import { useId } from 'react';

// BrandMark: the MintoBill "M" (two teal arches, moodboard Nov 2026).
// Pure SVG attributes (no style attribute), so it is safe under the
// console's strict CSP. Decorative: the brand name is always next to it.
export default function BrandMark({ size = 32, className }) {
  const id = useId().replace(/:/g, '');
  return (
    <svg width={size} height={size} viewBox="0 0 40 40" fill="none" aria-hidden="true" focusable="false" className={className}>
      <defs>
        <linearGradient id={`bm-a-${id}`} x1="4" y1="34" x2="22" y2="6" gradientUnits="userSpaceOnUse">
          <stop offset="0" stopColor="#123b45" />
          <stop offset="1" stopColor="#17807a" />
        </linearGradient>
        <linearGradient id={`bm-b-${id}`} x1="18" y1="6" x2="36" y2="34" gradientUnits="userSpaceOnUse">
          <stop offset="0" stopColor="#17807a" />
          <stop offset="1" stopColor="#2fd0bb" />
        </linearGradient>
      </defs>
      <path d="M8 33V15.5a6.5 6.5 0 0 1 13 0V27" stroke={`url(#bm-a-${id})`} strokeWidth="6.5" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M21 15.5a6.5 6.5 0 0 1 13 0V33" stroke={`url(#bm-b-${id})`} strokeWidth="6.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

// Wordmark: "MintoBill" with the second half in fresh teal.
export function Wordmark({ className, accentClassName }) {
  return (
    <span className={className}>
      Minto<span className={accentClassName}>Bill</span>
    </span>
  );
}
