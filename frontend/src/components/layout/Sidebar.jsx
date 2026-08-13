import { useState } from 'react';
import { NavLink, useNavigate } from 'react-router-dom';
import {
  LayoutDashboard,
  ShoppingCart,
  Receipt,
  RotateCcw,
  FileText,
  Package,
  Boxes,
  Truck,
  Tag,
  Award,
  Ruler,
  Users,
  UserCog,
  BarChart3,
  ChevronsLeft,
  ChevronsRight,
  LogOut,
  X,
  Plus,
  Settings,
} from 'lucide-react';
import { usePos } from '@/hooks/usePos';
import { cn } from '@/lib/utils';
import { Button } from '@/components/ui/button';
import { Avatar, AvatarFallback } from '@/components/ui/avatar';
import { initials } from '@/lib/format';

const navSections = [
  {
    title: 'Overview',
    items: [{ label: 'Dashboard', path: '/', icon: LayoutDashboard }],
  },
  {
    title: 'Sales',
    items: [
      { label: 'POS', path: '/pos', icon: ShoppingCart },
      { label: 'Sales', path: '/sales', icon: Receipt },
      { label: 'Sales Returns', path: '/sales-return', icon: RotateCcw },
      { label: 'Invoices', path: '/invoices', icon: FileText },
    ],
  },
  {
    title: 'Inventory',
    items: [
      { label: 'Products', path: '/products', icon: Package },
      { label: 'Inventory', path: '/inventory', icon: Boxes },
      { label: 'Purchases', path: '/purchases', icon: Truck },
    ],
  },
  {
    title: 'Master Data',
    items: [
      { label: 'Categories', path: '/categories', icon: Tag },
      { label: 'Brands', path: '/brands', icon: Award },
      { label: 'Units', path: '/units', icon: Ruler },
      { label: 'Suppliers', path: '/suppliers', icon: Truck },
    ],
  },
  {
    title: 'People',
    items: [
      { label: 'Customers', path: '/customers', icon: Users },
    ],
  },
  {
    title: 'Reports',
    items: [
      { label: 'Sales Report', path: '/sales-report', icon: BarChart3 },
      { label: 'Purchase Report', path: '/purchase-report', icon: BarChart3 },
      { label: 'Customer Report', path: '/customer-report', icon: BarChart3 },
      { label: 'Customer Due', path: '/customer-due-report', icon: BarChart3 },
    ],
  },
  {
    title: 'System',
    items: [{ label: 'Settings', path: '/settings', icon: Settings }],
  },
];

export default function Sidebar({ mobileOpen, setMobileOpen, collapsed: controlledCollapsed, onCollapsedChange }) {
  const [internalCollapsed, setInternalCollapsed] = useState(false);
  const collapsed = controlledCollapsed ?? internalCollapsed;
  const setCollapsed = onCollapsedChange ?? setInternalCollapsed;
  const { currentUser, logout } = usePos();
  const navigate = useNavigate();

  const handleSignOut = () => {
    logout();
    navigate('/signin');
  };

  const adminSection =
    currentUser?.role === 'admin'
      ? {
          title: 'Admin',
          items: [{ label: 'Billers', path: '/billers', icon: UserCog }],
        }
      : null;

  const sections = adminSection ? [...navSections, adminSection] : navSections;

  return (
    <>
      {mobileOpen && (
        <div
          className="fixed inset-0 z-30 bg-black/40 lg:hidden"
          onClick={() => setMobileOpen(false)}
          aria-hidden
        />
      )}

      <aside
        className={cn(
          'fixed inset-y-0 left-0 z-40 flex h-screen flex-col overflow-hidden border-r bg-sidebar text-sidebar-foreground transition-[width,transform] duration-200',
          collapsed ? 'w-[68px]' : 'w-64',
          mobileOpen ? 'translate-x-0' : '-translate-x-full lg:translate-x-0'
        )}
      >
        <div className="flex h-16 items-center justify-between border-b px-3 shrink-0">
          <div className="flex items-center gap-2 overflow-hidden">
            <div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary text-primary-foreground">
              <ShoppingCart className="size-5" />
            </div>
            {!collapsed && (
              <div className="flex flex-col leading-tight">
                <span className="text-sm font-bold text-foreground">ReactronPOS</span>
                <span className="text-[10px] text-muted-foreground">Retail Suite</span>
              </div>
            )}
          </div>
          <div className="flex items-center gap-1">
            <Button
              variant="ghost"
              size="icon"
              className="hidden lg:flex size-7"
              onClick={() => setCollapsed((v) => !v)}
              aria-label="Toggle sidebar"
            >
              {collapsed ? <ChevronsRight className="size-4" /> : <ChevronsLeft className="size-4" />}
            </Button>
            <Button
              variant="ghost"
              size="icon"
              className="lg:hidden size-7"
              onClick={() => setMobileOpen(false)}
              aria-label="Close sidebar"
            >
              <X className="size-4" />
            </Button>
          </div>
        </div>

        <nav className="min-h-0 flex-1 overflow-y-auto no-scrollbar px-2 py-3">
          <div className="space-y-4">
            {sections.map((section) => (
              <div key={section.title}>
                {!collapsed && (
                  <h3 className="mb-1.5 px-2 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                    {section.title}
                  </h3>
                )}
                <ul className="space-y-0.5">
                  {section.items.map((item) => {
                    const Icon = item.icon;
                    return (
                      <li key={item.path}>
                        <NavLink
                          to={item.path}
                          end={item.path === '/'}
                          onClick={() => setMobileOpen(false)}
                          className={({ isActive }) =>
                            cn(
                              'flex w-full items-center gap-3 rounded-md px-2.5 py-2 text-sm font-medium transition-colors',
                              isActive
                                ? 'bg-primary text-primary-foreground'
                                : 'text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground',
                              collapsed && 'justify-center px-0'
                            )
                          }
                          title={collapsed ? item.label : undefined}
                        >
                          <Icon className="size-4 shrink-0" />
                          {!collapsed && <span className="truncate">{item.label}</span>}
                        </NavLink>
                      </li>
                    );
                  })}
                </ul>
              </div>
            ))}
          </div>
        </nav>

        <div className="border-t p-2 shrink-0">
          <div className={cn('flex items-center gap-2 rounded-md p-2', collapsed && 'justify-center')}>
            <Avatar className="size-8 shrink-0">
              <AvatarFallback className="bg-primary/10 text-primary text-xs font-semibold">
                {currentUser ? initials(currentUser.name) : 'U'}
              </AvatarFallback>
            </Avatar>
            {!collapsed && (
              <div className="flex flex-1 items-center justify-between overflow-hidden">
                <div className="overflow-hidden leading-tight">
                  <p className="truncate text-sm font-medium">{currentUser?.name}</p>
                  <p className="truncate text-xs text-muted-foreground capitalize">{currentUser?.role}</p>
                </div>
                <Button
                  variant="ghost"
                  size="icon"
                  className="size-7 text-muted-foreground hover:text-destructive"
                  onClick={handleSignOut}
                  aria-label="Sign out"
                >
                  <LogOut className="size-4" />
                </Button>
              </div>
            )}
          </div>
        </div>
      </aside>
    </>
  );
}
