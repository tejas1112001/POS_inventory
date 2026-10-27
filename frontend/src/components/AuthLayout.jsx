import { ShoppingCart } from 'lucide-react';
import { useTenantInfo } from '@/hooks/useTenantInfo';

// AuthLayout frames the sign-in, forgot-password and set-password pages
// with the shop's name.
export default function AuthLayout({ title, subtitle, children }) {
  const info = useTenantInfo();
  return (
    <div className="flex min-h-screen items-center justify-center bg-gradient-to-br from-sky-50 via-background to-blue-50 p-4">
      <div className="w-full max-w-md">
        <div className="mb-6 flex flex-col items-center text-center">
          {info?.logoUrl ? (
            <img src={info.logoUrl} alt="" className="mb-3 size-14 rounded-xl object-contain shadow-lg" />
          ) : (
            <div className="mb-3 flex size-14 items-center justify-center rounded-xl bg-primary text-primary-foreground shadow-lg shadow-primary/20">
              <ShoppingCart className="size-7" />
            </div>
          )}
          <h1 className="text-2xl font-bold tracking-tight">{info?.name || ' '}</h1>
          <p className="text-sm text-muted-foreground">Point of sale and inventory</p>
        </div>
        <div className="rounded-xl border bg-card p-6 shadow-sm">
          <h2 className="text-lg font-semibold">{title}</h2>
          {subtitle && <p className="mt-1 text-sm text-muted-foreground">{subtitle}</p>}
          {children}
        </div>
      </div>
    </div>
  );
}

export function FormMessage({ kind = 'error', children }) {
  if (!children) return null;
  const cls =
    kind === 'error'
      ? 'border-destructive/30 bg-destructive/10 text-destructive'
      : 'border-emerald-300 bg-emerald-50 text-emerald-800 dark:border-emerald-800 dark:bg-emerald-950 dark:text-emerald-200';
  return (
    <div role={kind === 'error' ? 'alert' : 'status'} className={`mt-4 rounded-md border px-3 py-2 text-sm ${cls}`}>
      {children}
    </div>
  );
}
