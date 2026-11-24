import { useLocation } from 'react-router-dom';
import Sidebar from './Sidebar';
import Topbar from './Topbar';
import BannerArea from './BannerArea';
import { MainLayout } from './AppShell';
import { pageTitle } from './navigation';
import BottomNav from './BottomNav';
import useShortcuts from './useShortcuts';
import ErrorBoundary from '../errors/ErrorBoundary';
import { PageError } from '../errors/ErrorScreens';
import { useAppDispatch, useAppSelector } from '@/store/hooks';
import { selectMobileNavOpen, selectSidebarCollapsed, setMobileNavOpen, setSidebarCollapsed } from '@/store/uiSlice';

// AppLayout: sidebar, top bar, banners and the page. The sidebar state
// (collapsed on desktop, open on phones) is in the Redux store.
export default function AppLayout({ children, fullBleed = false }) {
  const dispatch = useAppDispatch();
  const mobileOpen = useAppSelector(selectMobileNavOpen);
  const sidebarCollapsed = useAppSelector(selectSidebarCollapsed);
  const location = useLocation();
  const title = pageTitle(location.pathname);
  useShortcuts();
  const showTabs = !fullBleed;
  const setMobileOpen = (open) => dispatch(setMobileNavOpen(open));

  return (
    <MainLayout
      fullBleed={fullBleed}
      sidebarCollapsed={sidebarCollapsed}
      sidebar={
        <Sidebar
          mobileOpen={mobileOpen}
          setMobileOpen={setMobileOpen}
          collapsed={sidebarCollapsed}
          onCollapsedChange={(c) => dispatch(setSidebarCollapsed(c))}
        />
      }
      topbar={<Topbar title={title} onOpenMobileMenu={() => setMobileOpen(true)} />}
      banner={<BannerArea />}
      bottomNav={showTabs ? <BottomNav onMore={() => setMobileOpen(true)} /> : null}
    >
      <ErrorBoundary resetKey={location.pathname} fallback={(p) => <PageError {...p} />}>
        {children}
      </ErrorBoundary>
    </MainLayout>
  );
}
