import { useState } from 'react';
import { useLocation } from 'react-router-dom';
import Sidebar from './Sidebar';
import Topbar from './Topbar';
import { MainLayout } from './AppShell';
import { getRouteTitle } from './routeTitles';

export default function AppLayout({ children, fullBleed = false }) {
  const [mobileOpen, setMobileOpen] = useState(false);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const location = useLocation();
  const title = getRouteTitle(location.pathname);

  return (
    <MainLayout
      fullBleed={fullBleed}
      sidebarCollapsed={sidebarCollapsed}
      sidebar={
        <Sidebar
          mobileOpen={mobileOpen}
          setMobileOpen={setMobileOpen}
          collapsed={sidebarCollapsed}
          onCollapsedChange={setSidebarCollapsed}
        />
      }
      topbar={<Topbar title={title} onOpenMobileMenu={() => setMobileOpen(true)} />}
    >
      {children}
    </MainLayout>
  );
}
