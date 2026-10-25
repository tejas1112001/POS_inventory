import { NavLink } from 'react-router-dom';
import { useSession } from '../session';
import styles from './Layout.module.css';

const NAV = [
  { to: '/tenants', label: 'Tenants', perm: 'tenants:view' },
  { to: '/plans', label: 'Plans', perm: ['plans:manage', 'billing:view'] },
  { to: '/billing', label: 'Billing', perm: 'billing:view' },
  { to: '/audit', label: 'Audit log', perm: ['audit:view_all', 'audit:view_own'] },
];

// Layout: navigation by permission, the signed-in user, sign-out.
export default function Layout({ children }) {
  const { user, can, signOut } = useSession();
  const allowed = (perm) => (Array.isArray(perm) ? perm.some(can) : can(perm));
  return (
    <div className={styles.shell}>
      <header className={styles.top}>
        <span className={styles.brand}>Platform console</span>
        <nav className={styles.nav} aria-label="Main">
          {NAV.filter((n) => allowed(n.perm)).map((n) => (
            <NavLink key={n.to} to={n.to} className={({ isActive }) => `${styles.link} ${isActive ? styles.active : ''}`}>
              {n.label}
            </NavLink>
          ))}
        </nav>
        <div className={styles.user}>
          <span className="muted small">
            {user?.email} · {String(user?.role || '').replace('_', ' ')}
          </span>
          <button type="button" className="btn" onClick={signOut}>
            Sign out
          </button>
        </div>
      </header>
      <main className={styles.main}>{children}</main>
    </div>
  );
}
