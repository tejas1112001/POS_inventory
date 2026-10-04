import { cn } from '@/lib/utils';

export function AppShell({ children }) {
  return (
    <div className="flex min-h-screen bg-background">
      {children}
    </div>
  );
}

export function PageWrapper({ children, className }) {
  return (
    <div className={cn('w-full space-y-4 sm:space-y-6', className)}>
      {children}
    </div>
  );
}

export function MainLayout({
  sidebar,
  topbar,
  children,
  fullBleed = false,
  sidebarCollapsed = false,
}) {
  return (
    <div className="flex h-screen w-full overflow-hidden bg-background">
      {sidebar}
      <div
        className={cn(
          'flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden transition-[padding-left] duration-200',
          sidebarCollapsed ? 'lg:pl-[68px]' : 'lg:pl-64'
        )}
      >
        <div className="shrink-0">{topbar}</div>
        <main
          className={cn(
            'flex min-h-0 flex-1 flex-col overflow-x-hidden overflow-y-auto no-scrollbar',
            fullBleed ? 'p-0' : 'p-4 sm:p-6'
          )}
        >
          {children}
        </main>
      </div>
    </div>
  );
}
