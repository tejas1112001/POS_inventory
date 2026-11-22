import { Link } from 'react-router-dom';
import { cn } from '@/lib/cn';

export function LogoMark({ size = 34 }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" aria-hidden="true" className="shrink-0">
      <rect width="32" height="32" rx="9" fill="#2563EB" />
      <path d="M10 7.5h12v17l-2-1.4-2 1.4-2-1.4-2 1.4-2-1.4-2 1.4z" fill="#FFFFFF" />
      <path d="M13 12.5h6M13 16.5h4" stroke="#2563EB" strokeWidth="1.8" strokeLinecap="round" />
    </svg>
  );
}

export function Wordmark({ className }) {
  return (
    <span className={cn('font-extrabold tracking-tight text-white', className)}>
      Bill<span className="text-brand-bright">Piko</span>
    </span>
  );
}

export function Logo({ className }) {
  return (
    <Link to="/" aria-label="BilMitra home" className={cn('flex items-center gap-2.5 rounded-lg p-1', className)}>
      <LogoMark />
      <Wordmark className="text-[21px]" />
    </Link>
  );
}
