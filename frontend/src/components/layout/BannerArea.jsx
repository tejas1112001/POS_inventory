import { useCallback, useEffect, useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { AlertTriangle, Info, ShieldAlert, X } from 'lucide-react';
import { api } from '@/services/api';
import { usePos } from '@/hooks/usePos';
import { cn } from '@/lib/utils';

const STYLE = {
  info: 'border-transparent bg-info-bg text-info-fg',
  warning: 'border-amber-brand/40 bg-warning-bg text-warning-fg',
  critical: 'border-danger/30 bg-danger-bg text-danger-fg',
};
const ICON = { info: Info, warning: AlertTriangle, critical: ShieldAlert };
const DISMISSED_KEY = 'dismissedBanners';

function dismissedIds() {
  try {
    return JSON.parse(sessionStorage.getItem(DISMISSED_KEY) || '[]');
  } catch {
    return [];
  }
}

// Only same-app paths are followed from a banner.
const safePath = (p) => (typeof p === 'string' && /^\/[a-z0-9/-]*$/i.test(p) ? p : null);

// BannerArea shows the billing banners (GET /api/notifications/banners),
// a notice while a platform support person is signed in to the shop, and
// one for a read-only shop. Dismissed banners stay hidden in this tab.
export default function BannerArea() {
  const { impersonation, tenant } = usePos();
  const [banners, setBanners] = useState([]);
  const [dismissed, setDismissed] = useState(dismissedIds);
  const location = useLocation();

  const load = useCallback(() => {
    api
      .get('/api/notifications/banners', { quiet: true })
      .then((list) => setBanners(Array.isArray(list) ? list : []))
      .catch(() => {});
  }, []);

  useEffect(() => {
    load();
    const t = setInterval(load, 5 * 60 * 1000);
    return () => clearInterval(t);
  }, [load]);

  // A payment changes the banners: reload when leaving the billing page.
  useEffect(() => {
    if (location.pathname !== '/billing') load();
  }, [location.pathname, load]);

  const dismiss = (id) => {
    const next = [...dismissed, id];
    setDismissed(next);
    try {
      sessionStorage.setItem(DISMISSED_KEY, JSON.stringify(next));
    } catch {
      /* the banner just comes back on reload */
    }
  };

  const items = [];
  if (impersonation) {
    items.push({ id: 'impersonation', level: 'warning', message: 'Platform support is signed in to this shop. Every action is recorded.' });
  }
  if (tenant?.status === 'read_only' && !banners.some((b) => b.level === 'critical')) {
    items.push({ id: 'read-only', level: 'critical', message: 'This shop is read-only: you can look things up but not sell or change anything.' });
  }
  items.push(...banners.filter((b) => !(b.dismissible && dismissed.includes(b.id))));
  if (items.length === 0) return null;

  return (
    <div className="space-y-2 px-4 pt-3 sm:px-6 lg:px-8">
      {items.map((b) => {
        const Icon = ICON[b.level] || Info;
        const path = safePath(b.action?.path);
        return (
          <div key={b.id} role={b.level === 'critical' ? 'alert' : 'status'} className={cn('flex items-center gap-2 rounded-xl border px-3 py-1.5 text-xs sm:gap-3 sm:py-2 sm:text-sm', STYLE[b.level] || STYLE.info)}>
            <Icon className="size-4 shrink-0" />
            <span className="min-w-0 flex-1 line-clamp-2 sm:line-clamp-none">{b.message}</span>
            {path && b.action?.label && location.pathname !== path && (
              <Link to={path} className="shrink-0 rounded-md bg-background/80 px-2.5 py-1 text-xs font-semibold text-foreground shadow-xs hover:bg-background">
                {b.action.label}
              </Link>
            )}
            {b.dismissible && (
              <button type="button" onClick={() => dismiss(b.id)} className="shrink-0 rounded p-0.5 opacity-70 hover:opacity-100" aria-label="Dismiss">
                <X className="size-4" />
              </button>
            )}
          </div>
        );
      })}
    </div>
  );
}
