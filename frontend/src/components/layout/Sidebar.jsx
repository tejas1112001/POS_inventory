import { useState } from 'react';
import { NavLink, useNavigate } from 'react-router-dom';
import {
  LayoutDashboard,
  ShoppingCart,
  ReceiptText,
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
  ShieldCheck,
  Globe,
} from 'lucide-react';
import { usePos } from '@/hooks/usePos';
import { usePermission } from '@/hooks/usePermission';
import { cn } from '@/lib/utils';
import { Button } from '@/components/ui/button';
import { Avatar, AvatarFallback } from '@/components/ui/avatar';
import { initials } from '@/lib/format';

const navSections = [
  {
    title: 'Overview',
    items: [{ label: 'Dashboard', path: '/', icon: LayoutDashboard, menuKey: 'dashboard' }],
  },
  {
    title: 'Sales',
    items: [
      { label: 'POS', path: '/pos', icon: ShoppingCart, menuKey: 'pos' },
      { label: 'Sales', path: '/sales', icon: ReceiptText, menuKey: 'sales' },
      { label: 'Sales Returns', path: '/sales-return', icon: RotateCcw, menuKey: 'sales_returns' },
      { label: 'Invoices', path: '/invoices', icon: FileText, menuKey: 'invoices' },
    ],
  },
  {
    title: 'Inventory',
    items: [
      { label: 'Products', path: '/products', icon: Package, menuKey: 'products' },
      { label: 'Inventory', path: '/inventory', icon: Boxes, menuKey: 'inventory' },
      { label: 'Purchases', path: '/purchases', icon: Truck, menuKey: 'purchases' },
    ],
  },
  {
    title: 'Master Data',
    items: [
      { label: 'Categories', path: '/categories', icon: Tag, menuKey: 'categories' },
      { label: 'Brands', path: '/brands', icon: Award, menuKey: 'brands' },
      { label: 'Units', path: '/units', icon: Ruler, menuKey: 'units' },
      { label: 'Suppliers', path: '/suppliers', icon: Truck, menuKey: 'suppliers' },
    ],
  },
  {
    title: 'People',
    items: [
      { label: 'Customers', path: '/customers', icon: Users, menuKey: 'customers' },
    ],
  },
  {
    title: 'Reports',
    items: [
      { label: 'Sales Report', path: '/sales-report', icon: BarChart3, menuKey: 'reports' },
      { label: 'Purchase Report', path: '/purchase-report', icon: BarChart3, menuKey: 'reports' },
      { label: 'Customer Report', path: '/customer-report', icon: BarChart3, menuKey: 'reports' },
      { label: 'Customer Due', path: '/customer-due-report', icon: BarChart3, menuKey: 'reports' },
    ],
  },
  {
    title: 'System',
    items: [
      { label: 'Product Showcase', path: '/showcase-settings', icon: Globe, menuKey: 'stores' },
      { label: 'Users', path: '/users', icon: UserCog, menuKey: 'users' },
      { label: 'Roles & Permissions', path: '/roles', icon: ShieldCheck, menuKey: 'roles' },
      { label: 'Settings', path: '/settings', icon: Settings, menuKey: 'stores' },
    ],
  },
];

export default function Sidebar({ mobileOpen, setMobileOpen, collapsed: controlledCollapsed, onCollapsedChange }) {
  const [internalCollapsed, setInternalCollapsed] = useState(false);
  const collapsed = controlledCollapsed ?? internalCollapsed;
  const setCollapsed = onCollapsedChange ?? setInternalCollapsed;
  const { currentUser, logout, tenant } = usePos();
  const { canAccessMenu } = usePermission();
  const navigate = useNavigate();

  const handleSignOut = async () => {
    await logout();
    navigate('/signin', { replace: true });
  };

  const visibleSections = navSections
    .map((section) => ({
      ...section,
      items: section.items.filter((item) => canAccessMenu(item.menuKey)),
    }))
    .filter((section) => section.items.length > 0);

  return (
    <>
      {mobileOpen && (
        <div
          className="fixed inset-0 z-40 bg-black/50 backdrop-blur-xs lg:hidden"
          onClick={() => setMobileOpen(false)}
          aria-hidden
        />
      )}

      <aside
        className={cn(
          'fixed inset-y-0 left-0 z-50 flex h-screen flex-col overflow-hidden border-r bg-sidebar text-sidebar-foreground transition-[width,transform] duration-200',
          collapsed ? 'w-[68px]' : 'w-64',
          mobileOpen ? 'translate-x-0' : '-translate-x-full lg:translate-x-0'
        )}
      >
        <div className={cn('flex h-16 items-center border-b shrink-0', collapsed ? 'justify-center px-2' : 'justify-between px-3')}>
          {collapsed ? (
            <Button
              variant="ghost"
              size="icon"
              className="size-10 rounded-xl hover:bg-sidebar-accent"
              onClick={() => setCollapsed(false)}
              title="Expand sidebar"
            >
              <div className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary text-primary-foreground shadow-xs">
                <ShoppingCart className="size-4" />
              </div>
            </Button>
          ) : (
            <>
              <div className="flex items-center gap-2.5 overflow-hidden">
                <div className="flex size-9 shrink-0 items-center justify-center rounded-xl bg-primary text-primary-foreground shadow-xs">
                  <ShoppingCart className="size-5" />
                </div>
                <div className="flex flex-col leading-tight">
                  <span className="max-w-[150px] truncate text-sm font-bold tracking-tight text-foreground" title={tenant?.name || ''}>
                    {tenant?.name || 'Point of Sale'}
                  </span>
                  <span className="text-[10px] font-medium text-muted-foreground">Point of sale</span>
                </div>
              </div>
              <div className="flex items-center gap-1">
                <Button
                  variant="ghost"
                  size="icon"
                  className="hidden lg:flex size-7 text-muted-foreground hover:text-foreground"
                  onClick={() => setCollapsed(true)}
                  aria-label="Collapse sidebar"
                  title="Collapse sidebar"
                >
                  <ChevronsLeft className="size-4" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  className="lg:hidden size-7 text-muted-foreground hover:text-foreground"
                  onClick={() => setMobileOpen(false)}
                  aria-label="Close sidebar"
                >
                  <X className="size-4" />
                </Button>
              </div>
            </>
          )}
        </div>

        <nav className="min-h-0 flex-1 overflow-y-auto no-scrollbar px-2 py-3">
          <div className="space-y-4">
            {visibleSections.map((section) => (
              <div key={section.title}>
                {!collapsed && (
                  <h3 className="mb-1.5 px-2 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                    {section.title}
                  </h3>
                )}
                <ul className="space-y-1">
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
                              'flex items-center rounded-xl text-sm transition-all',
                              collapsed ? 'size-10 justify-center mx-auto' : 'w-full gap-3 px-3 py-2.5',
                              isActive
                                ? 'bg-primary text-primary-foreground shadow-xs font-semibold'
                                : 'text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground font-medium'
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
          {collapsed ? (
            <button
              onClick={handleSignOut}
              title={`Sign out (${currentUser?.name || 'User'})`}
              className="flex size-10 mx-auto items-center justify-center rounded-xl hover:bg-destructive/10 text-sidebar-foreground hover:text-destructive transition-colors group"
            >
              <Avatar className="size-8 shrink-0 ring-2 ring-primary/20 group-hover:ring-destructive/30">
                <AvatarFallback className="bg-primary/10 text-primary text-xs font-bold group-hover:bg-destructive/10 group-hover:text-destructive">
                  {currentUser ? initials(currentUser.name) : 'U'}
                </AvatarFallback>
              </Avatar>
            </button>
          ) : (
            <div className="flex items-center gap-2 rounded-xl p-2 bg-sidebar-accent/50">
              <Avatar className="size-8 shrink-0">
                <AvatarFallback className="bg-primary/10 text-primary text-xs font-semibold">
                  {currentUser ? initials(currentUser.name) : 'U'}
                </AvatarFallback>
              </Avatar>
              <div className="flex flex-1 items-center justify-between overflow-hidden">
                <div className="overflow-hidden leading-tight">
                  <p className="truncate text-sm font-semibold text-foreground">{currentUser?.name}</p>
                  <p className="truncate text-xs text-muted-foreground capitalize">{currentUser?.role}</p>
                </div>
                <Button
                  variant="ghost"
                  size="icon"
                  className="size-7 text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                  onClick={handleSignOut}
                  aria-label="Sign out"
                  title="Sign out"
                >
                  <LogOut className="size-4" />
                </Button>
              </div>
            </div>
          )}
        </div>
      </aside>
    </>
  );
}
