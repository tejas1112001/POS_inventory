import { NavLink, useLocation } from 'react-router-dom';
import { LayoutDashboard, Menu, ReceiptText, ShoppingCart, Users } from 'lucide-react';
import { usePermission } from '@/context/PermissionContext';
import { cn } from '@/lib/utils';

// BottomNav: the phone tab bar (< 768 px). Owners and admins get
// Dashboard, Checkout, Orders; cashiers Checkout, Orders, Customers.
// "More" opens the full menu.
export default function BottomNav({ onMore }) {
  const { hasPermission } = usePermission();
  const { pathname } = useLocation();
  const manager = hasPermission('settings', 'edit');
  const candidates = manager
    ? [
        { to: '/dashboard', label: 'Dashboard', icon: LayoutDashboard, ok: hasPermission('dashboard', 'view') },
        { to: '/pos', label: 'Checkout', icon: ShoppingCart, ok: hasPermission('pos', 'view') },
        { to: '/sales', label: 'Orders', icon: ReceiptText, ok: hasPermission('sales', 'view') },
      ]
    : [
        { to: '/pos', label: 'Checkout', icon: ShoppingCart, ok: hasPermission('pos', 'view') },
        { to: '/sales', label: 'Orders', icon: ReceiptText, ok: hasPermission('sales', 'view') },
        { to: '/customers', label: 'Customers', icon: Users, ok: hasPermission('customers', 'view') },
      ];
  const tabs = candidates.filter((t) => t.ok);
  const item = 'flex min-h-14 flex-1 flex-col items-center justify-center gap-0.5 text-[11px] font-medium';

  return (
    <nav aria-label="Quick menu" className="fixed inset-x-0 bottom-0 z-30 flex border-t bg-background/95 pb-[env(safe-area-inset-bottom)] backdrop-blur md:hidden">
      {tabs.map(({ to, label, icon: Icon }) => (
        <NavLink
          key={to}
          to={to}
          className={({ isActive }) => cn(item, isActive || pathname.startsWith(`${to}/`) ? 'text-primary' : 'text-muted-foreground')}
        >
          <Icon className="size-5" aria-hidden="true" />
          {label}
        </NavLink>
      ))}
      <button type="button" onClick={onMore} className={cn(item, 'text-muted-foreground')} aria-label="More: open the full menu">
        <Menu className="size-5" aria-hidden="true" />
        More
      </button>
    </nav>
  );
}
