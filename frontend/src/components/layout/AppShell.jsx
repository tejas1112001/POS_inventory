import { cn } from '@/lib/utils';

export function AppShell({ children }) {
  return (
    <div className="flex min-h-dvh bg-background">
      {children}
    </div>
  );
}

export function PageWrapper({ children, className }) {
  return (
    <div className={cn('w-full space-y-3 sm:space-y-4', className)}>
      {children}
    </div>
  );
}

export function MainLayout({
  sidebar,
  topbar,
  banner,
  children,
  fullBleed = false,
  sidebarCollapsed = false,
  bottomNav = null,
}) {
  return (
    <div className="flex h-dvh w-full overflow-hidden bg-background bg-page-gradient">
      {sidebar}
      <div
        className={cn(
          'flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden transition-[padding-left] duration-200',
          sidebarCollapsed ? 'lg:pl-[72px]' : 'lg:pl-64'
        )}
      >
        <div className="shrink-0">{topbar}</div>
        {banner && <div className="shrink-0">{banner}</div>}
        <main
          className={cn(
            'flex min-h-0 flex-1 flex-col overflow-x-hidden overflow-y-auto no-scrollbar',
            fullBleed ? 'p-0' : 'p-3 sm:p-4 lg:px-5',
            bottomNav && 'max-md:pb-24'
          )}
        >
          {children}
        </main>
        {bottomNav}
      </div>
    </div>
  );
}
