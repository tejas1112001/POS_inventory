import { useState } from 'react';
import { NavLink, useLocation, useNavigate } from 'react-router-dom';
import { isItemActive, visibleNav } from './navigation';
import { ChevronsLeft, ChevronsRight, LogOut, X } from 'lucide-react';
import { usePos } from '@/hooks/usePos';
import { usePermission } from '@/hooks/usePermission';
import { useModules } from '@/hooks/useModules';
import { cn } from '@/lib/utils';
import { Button } from '@/components/ui/button';
import { Avatar, AvatarFallback } from '@/components/ui/avatar';
import { initials } from '@/lib/format';
import BrandMark, { Wordmark } from '../../../shared/brand/BrandMark';

// Sidebar (Teal & Cream theme): white rail in light mode, deep teal in
// dark mode; the open page is a soft teal pill (solid teal in dark).
export default function Sidebar({ mobileOpen, setMobileOpen, collapsed: controlledCollapsed, onCollapsedChange }) {
  const [internalCollapsed, setInternalCollapsed] = useState(false);
  const collapsed = controlledCollapsed ?? internalCollapsed;
  const setCollapsed = onCollapsedChange ?? setInternalCollapsed;
  const { currentUser, logout, tenant } = usePos();
  const { hasPermission } = usePermission();
  const { hasModule } = useModules();
  const navigate = useNavigate();

  const handleSignOut = async () => {
    await logout();
    navigate('/login', { replace: true });
  };

  const { pathname } = useLocation();
  const visibleSections = visibleNav(hasPermission, hasModule);

  return (
    <>
      {mobileOpen && (
        <div
          className="fixed inset-0 z-40 bg-[#0b2a32]/50 lg:hidden"
          onClick={() => setMobileOpen(false)}
          aria-hidden
        />
      )}

      <aside
        className={cn(
          'fixed inset-y-0 left-0 z-50 flex h-dvh flex-col overflow-hidden border-r border-sidebar-border bg-sidebar text-sidebar-foreground transition-[width,transform] duration-200',
          collapsed ? 'w-[72px]' : 'w-64',
          mobileOpen ? 'translate-x-0 shadow-lg' : '-translate-x-full lg:translate-x-0'
        )}
      >
        <div className={cn('flex h-16 shrink-0 items-center', collapsed ? 'justify-center px-2' : 'justify-between pr-2 pl-4')}>
          {collapsed ? (
            <button
              type="button"
              className="flex size-11 items-center justify-center rounded-xl transition-colors hover:bg-sidebar-accent focus-visible:outline-2 focus-visible:outline-ring"
              onClick={() => setCollapsed(false)}
              title="Expand sidebar"
              aria-label="Expand sidebar"
            >
              <BrandMark size={28} />
            </button>
          ) : (
            <>
              <div className="flex min-w-0 items-center gap-2.5">
                <BrandMark size={32} className="shrink-0" />
                <div className="flex min-w-0 flex-col leading-tight">
                  <Wordmark className="text-[17px] font-extrabold tracking-tight text-navy" accentClassName="text-[#17807a] dark:text-teal" />
                  <span className="max-w-[150px] truncate text-[11px] font-medium text-sidebar-muted" title={tenant?.name || ''}>
                    {tenant?.name || 'Point of sale'}
                  </span>
                </div>
              </div>
              <div className="flex items-center">
                <Button
                  variant="ghost"
                  size="icon"
                  className="hidden size-8 text-sidebar-muted hover:bg-sidebar-accent hover:text-sidebar-accent-foreground lg:flex"
                  onClick={() => setCollapsed(true)}
                  aria-label="Collapse sidebar"
                  title="Collapse sidebar"
                >
                  <ChevronsLeft className="size-4" />
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  className="size-10 text-sidebar-muted hover:bg-sidebar-accent hover:text-sidebar-accent-foreground lg:hidden"
                  onClick={() => setMobileOpen(false)}
                  aria-label="Close sidebar"
                >
                  <X className="size-4" />
                </Button>
              </div>
            </>
          )}
        </div>

        <nav className="min-h-0 flex-1 overflow-y-auto px-3 pt-2 pb-3 scrollbar-thin" aria-label="Main">
          <div className="space-y-5">
            {visibleSections.map((section) => (
              <div key={section.title}>
                {!collapsed ? (
                  <h3 className="mb-1.5 px-3 text-[10.5px] font-semibold tracking-[0.08em] text-sidebar-muted uppercase">{section.title}</h3>
                ) : (
                  <div className="mx-auto mb-2 h-px w-8 bg-sidebar-border" aria-hidden="true" />
                )}
                <ul className="space-y-0.5">
                  {section.items.map((item) => {
                    const Icon = item.icon;
                    const active = isItemActive(item, pathname);
                    return (
                      <li key={item.path}>
                        <NavLink
                          to={item.path}
                          onClick={() => setMobileOpen(false)}
                          aria-current={active ? 'page' : undefined}
                          className={cn(
                            'group flex items-center rounded-xl text-[13.5px] transition-colors duration-150 focus-visible:outline-2 focus-visible:outline-ring',
                            collapsed ? 'mx-auto size-11 justify-center' : 'min-h-10 w-full gap-3 px-3 py-2',
                            active
                              ? 'bg-[var(--sidebar-active)] font-semibold text-[var(--sidebar-active-foreground)] shadow-xs'
                              : 'font-medium text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground'
                          )}
                          title={collapsed ? item.label : undefined}
                        >
                          <Icon
                            className={cn('size-[18px] shrink-0', !active && 'text-sidebar-muted group-hover:text-sidebar-accent-foreground')}
                            strokeWidth={active ? 2.2 : 1.9}
                            aria-hidden="true"
                          />
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

        <div className="shrink-0 border-t border-sidebar-border p-3">
          {collapsed ? (
            <div className="flex flex-col items-center gap-1">
              <button
                type="button"
                onClick={() => setCollapsed(false)}
                className="hidden size-10 items-center justify-center rounded-xl text-sidebar-muted hover:bg-sidebar-accent hover:text-sidebar-accent-foreground lg:flex"
                aria-label="Expand sidebar"
                title="Expand sidebar"
              >
                <ChevronsRight className="size-4" />
              </button>
              <button
                type="button"
                onClick={handleSignOut}
                title={`Sign out (${currentUser?.name || 'User'})`}
                aria-label={`Sign out (${currentUser?.name || 'User'})`}
                className="group flex size-11 items-center justify-center rounded-xl transition-colors hover:bg-sidebar-accent"
              >
                <Avatar className="size-8 shrink-0 ring-2 ring-teal/30 group-hover:ring-teal/60">
                  <AvatarFallback className="bg-brand-gradient text-xs font-bold text-white">
                    {currentUser ? initials(currentUser.name) : 'U'}
                  </AvatarFallback>
                </Avatar>
              </button>
            </div>
          ) : (
            <div className="flex items-center gap-2.5 rounded-2xl border border-sidebar-border bg-card-gradient p-2.5">
              <Avatar className="size-9 shrink-0">
                <AvatarFallback className="bg-brand-gradient text-xs font-bold text-white">
                  {currentUser ? initials(currentUser.name) : 'U'}
                </AvatarFallback>
              </Avatar>
              <div className="min-w-0 flex-1 leading-tight">
                <p className="truncate text-sm font-semibold text-navy">{currentUser?.name}</p>
                <p className="truncate text-xs text-sidebar-muted capitalize">{currentUser?.role}</p>
              </div>
              <Button
                variant="ghost"
                size="icon"
                className="size-9 shrink-0 text-sidebar-muted hover:bg-danger-bg hover:text-danger-fg"
                onClick={handleSignOut}
                aria-label="Sign out"
                title="Sign out"
              >
                <LogOut className="size-4" />
              </Button>
            </div>
          )}
        </div>
      </aside>
    </>
  );
}
