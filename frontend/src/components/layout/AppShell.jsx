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
    <div className={cn('mx-auto max-w-[1600px] space-y-4', className)}>
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
            fullBleed ? 'p-0' : 'px-3 pb-3 pt-2 sm:px-4 sm:pt-3'
          )}
        >
          {children}
        </main>
      </div>
    </div>
  );
}
