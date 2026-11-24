import { BarChart3, Boxes, ShieldCheck, ShoppingCart } from 'lucide-react';
import { useTenantInfo } from '@/hooks/useTenantInfo';
import BrandMark, { Wordmark } from '../../shared/brand/BrandMark';

const POINTS = [
  { icon: ShoppingCart, text: 'Fast checkout with UPI, cash and card' },
  { icon: Boxes, text: 'Stock across every store, always in step' },
  { icon: BarChart3, text: 'Sales, GST and dues reports in one place' },
];

// AuthLayout frames the sign-in, forgot-password and set-password pages:
// a brand panel (Teal & Cream moodboard) next to the form, with the shop's
// name and logo above the form.
export default function AuthLayout({ title, subtitle, children }) {
  const info = useTenantInfo();
  return (
    <div className="flex min-h-dvh bg-background bg-page-gradient">
      <aside className="relative hidden w-[44%] max-w-[640px] shrink-0 overflow-hidden bg-brand-gradient p-10 text-white lg:flex lg:flex-col xl:p-14" aria-hidden="true">
        {/* Soft waves (decorative, static SVG) */}
        <svg className="pointer-events-none absolute inset-x-0 bottom-0 h-1/2 w-full" viewBox="0 0 600 300" preserveAspectRatio="none" fill="none">
          <path d="M0 180C120 120 220 240 340 200C460 160 520 80 600 110V300H0Z" fill="white" fillOpacity="0.07" />
          <path d="M0 230C140 190 240 280 380 250C480 228 540 170 600 180V300H0Z" fill="white" fillOpacity="0.08" />
          <path d="M0 120C160 60 260 200 400 150C500 115 560 40 600 60" stroke="white" strokeOpacity="0.18" strokeWidth="1.5" />
        </svg>
        <div className="pointer-events-none absolute -top-24 -right-24 size-72 rounded-full bg-[#f2b544]/15" />

        <div className="relative flex items-center gap-3">
          <span className="flex size-12 items-center justify-center rounded-2xl bg-white shadow-lg">
            <BrandMark size={30} />
          </span>
          <Wordmark className="text-2xl font-extrabold tracking-tight" accentClassName="text-[#9ff0e2]" />
        </div>

        <div className="relative mt-auto mb-auto max-w-md space-y-6 pt-16">
          <p className="inline-flex items-center gap-2 rounded-full bg-white/12 px-3 py-1 text-xs font-semibold tracking-wide text-white/90 ring-1 ring-white/20">
            <ShieldCheck className="size-3.5" /> Minimal &amp; premium retail
          </p>
          <h2 className="text-4xl leading-tight font-extrabold tracking-tight text-white">Billing and inventory, beautifully simple.</h2>
          <p className="text-base text-white/80">A fast, modern point of sale for multi-store shops and everyday retail work.</p>
          <ul className="space-y-3">
            {POINTS.map(({ icon: Icon, text }) => (
              <li key={text} className="flex items-center gap-3 text-sm font-medium text-white/90">
                <span className="flex size-9 items-center justify-center rounded-xl bg-white/12 ring-1 ring-white/15">
                  <Icon className="size-4" />
                </span>
                {text}
              </li>
            ))}
          </ul>
        </div>
        <p className="relative text-xs text-white/60">© {new Date().getFullYear()} MintoBill</p>
      </aside>

      <main className="flex flex-1 items-center justify-center p-4 sm:p-8">
        <div className="w-full max-w-md">
          <div className="mb-6 flex flex-col items-center text-center">
            {info?.logoUrl ? (
              <img src={info.logoUrl} alt="" className="mb-3 size-14 rounded-2xl bg-white object-contain p-1 shadow-sm ring-1 ring-border" />
            ) : (
              <span className="mb-3 flex size-14 items-center justify-center rounded-2xl bg-card shadow-sm ring-1 ring-border">
                <BrandMark size={34} />
              </span>
            )}
            <h1 className="text-2xl font-extrabold tracking-tight">{info?.name || ' '}</h1>
            <p className="text-sm text-muted-foreground">Point of sale and inventory</p>
          </div>
          <div className="rounded-3xl border bg-card p-6 shadow-sm sm:p-8">
            <h2 className="text-xl font-bold">{title}</h2>
            {subtitle && <p className="mt-1 text-sm text-muted-foreground">{subtitle}</p>}
            {children}
          </div>
        </div>
      </main>
    </div>
  );
}

export function FormMessage({ kind = 'error', children }) {
  if (!children) return null;
  const cls = kind === 'error' ? 'border-destructive/30 bg-danger-bg text-danger-fg' : 'border-success/30 bg-success-bg text-success-fg';
  return (
    <div role={kind === 'error' ? 'alert' : 'status'} className={`mt-4 rounded-xl border px-3 py-2 text-sm ${cls}`}>
      {children}
    </div>
  );
}
