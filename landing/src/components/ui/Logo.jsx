import { useId } from 'react';
import { Link } from 'react-router-dom';
import { cn } from '@/lib/cn';

// The MintoBill "M" (two teal arches, as in the shop app) on an ivory tile,
// so it reads on the dark header and footer alike.
export function LogoMark({ size = 34 }) {
  const id = useId().replace(/:/g, '');
  return (
    <svg width={size} height={size} viewBox="0 0 40 40" fill="none" aria-hidden="true" className="shrink-0">
      <defs>
        <linearGradient id={`lm-a-${id}`} x1="4" y1="34" x2="22" y2="6" gradientUnits="userSpaceOnUse">
          <stop offset="0" stopColor="#123b45" />
          <stop offset="1" stopColor="#17807a" />
        </linearGradient>
        <linearGradient id={`lm-b-${id}`} x1="18" y1="6" x2="36" y2="34" gradientUnits="userSpaceOnUse">
          <stop offset="0" stopColor="#17807a" />
          <stop offset="1" stopColor="#20b6a4" />
        </linearGradient>
      </defs>
      <rect width="40" height="40" rx="11" fill="#f4f2e9" />
      <g transform="translate(5.5 5.2) scale(0.72)">
        <path d="M8 33V15.5a6.5 6.5 0 0 1 13 0V27" stroke={`url(#lm-a-${id})`} strokeWidth="6.5" strokeLinecap="round" strokeLinejoin="round" />
        <path d="M21 15.5a6.5 6.5 0 0 1 13 0V33" stroke={`url(#lm-b-${id})`} strokeWidth="6.5" strokeLinecap="round" strokeLinejoin="round" />
      </g>
    </svg>
  );
}

export function Wordmark({ className }) {
  return (
    <span className={cn('font-extrabold tracking-tight text-white', className)}>
      Minto<span className="text-brand-bright">Bill</span>
    </span>
  );
}

export function Logo({ className }) {
  return (
    <Link to="/" aria-label="MintoBill home" className={cn('flex items-center gap-2.5 rounded-lg p-1', className)}>
      <LogoMark />
      <Wordmark className="text-[21px]" />
    </Link>
  );
}
