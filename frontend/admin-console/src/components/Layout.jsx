import { useEffect } from 'react';
import { NavLink, useLocation } from 'react-router-dom';
import { useDispatch, useSelector } from 'react-redux';
import { Building2, CreditCard, Layers, LogOut, Menu, ScrollText, ShieldCheck, X } from 'lucide-react';
import ErrorBoundary from './ErrorBoundary';
import { ThemeToggle } from './ui';
import { useSession } from '../session';
import { selectMenuOpen, setMenuOpen } from '../store';
import BrandMark, { Wordmark } from '../../../shared/brand/BrandMark';
import styles from './Layout.module.css';

const NAV = [
  { to: '/tenants', label: 'Tenants', icon: Building2, perm: 'tenants:view' },
  { to: '/plans', label: 'Plans', icon: Layers, perm: ['plans:manage', 'billing:view'] },
  { to: '/billing', label: 'Billing', icon: CreditCard, perm: 'billing:view' },
  { to: '/audit', label: 'Audit log', icon: ScrollText, perm: ['audit:view_all', 'audit:view_own'] },
  { to: '/platform-users', label: 'Platform users', icon: ShieldCheck, perm: 'platform_users:manage' },
];

function initials(email = '') {
  const name = String(email).split('@')[0].replace(/[._-]+/g, ' ').trim();
  const parts = name.split(' ').filter(Boolean);
  return ((parts[0]?.[0] || 'S') + (parts[1]?.[0] || '')).toUpperCase();
}

// Layout (Teal & Cream): sidebar navigation by permission, a top bar with
// the theme switch and the signed-in user, then the page. The phone menu
// state is in the Redux store.
export default function Layout({ children }) {
  const { user, can, signOut } = useSession();
  const { pathname } = useLocation();
  const dispatch = useDispatch();
  const menu = useSelector(selectMenuOpen);
  const allowed = (perm) => (Array.isArray(perm) ? perm.some(can) : can(perm));
  const role = String(user?.role || '').replace(/_/g, ' ');

  useEffect(() => {
    dispatch(setMenuOpen(false));
  }, [pathname, dispatch]);

  return (
    <div className={styles.shell}>
      {menu && <div className={styles.scrim} onClick={() => dispatch(setMenuOpen(false))} aria-hidden="true" />}
      <aside id="console-nav" className={`${styles.side} ${menu ? styles.sideOpen : ''}`}>
        <div className={styles.brand}>
          <BrandMark size={32} />
          <div className={styles.brandText}>
            <Wordmark className={styles.wordmark} accentClassName={styles.wordmarkAccent} />
            <span className={styles.brandSub}>Platform console</span>
          </div>
          <button type="button" className={`${styles.iconBtn} ${styles.closeBtn}`} onClick={() => dispatch(setMenuOpen(false))} aria-label="Close menu">
            <X size={18} aria-hidden="true" />
          </button>
        </div>
        <nav className={styles.nav} aria-label="Main">
          <p className={styles.section}>Manage</p>
          {NAV.filter((n) => allowed(n.perm)).map(({ to, label, icon: Icon }) => (
            <NavLink key={to} to={to} className={({ isActive }) => `${styles.link} ${isActive ? styles.active : ''}`}>
              <Icon size={18} aria-hidden="true" />
              <span>{label}</span>
            </NavLink>
          ))}
        </nav>
        <div className={styles.userCard}>
          <span className={styles.avatar} aria-hidden="true">
            {initials(user?.email)}
          </span>
          <div className={styles.userText}>
            <span className={styles.userEmail} title={user?.email}>
              {user?.email}
            </span>
            <span className={styles.userRole}>{role}</span>
          </div>
          <button type="button" className={`${styles.iconBtn} ${styles.signOut}`} onClick={signOut} aria-label="Sign out" title="Sign out">
            <LogOut size={17} aria-hidden="true" />
          </button>
        </div>
      </aside>

      <div className={styles.body}>
        <header className={styles.top}>
          <button type="button" className={`${styles.iconBtn} ${styles.menuButton}`} aria-expanded={menu} aria-controls="console-nav" aria-label="Open menu" onClick={() => dispatch(setMenuOpen(true))}>
            <Menu size={20} aria-hidden="true" />
          </button>
          <span className={styles.crumb}>
            <span className="badge badge-brand">Super Admin</span>
          </span>
          <div className={styles.topRight}>
            <ThemeToggle />
            <span className={styles.chip}>
              <span className={`${styles.avatar} ${styles.avatarSm}`} aria-hidden="true">
                {initials(user?.email)}
              </span>
              <span className={styles.chipText}>{user?.email}</span>
            </span>
          </div>
        </header>
        <main className={styles.main}>
          <ErrorBoundary resetKey={pathname}>{children}</ErrorBoundary>
        </main>
      </div>
    </div>
  );
}
