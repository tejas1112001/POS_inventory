import { NavLink } from 'react-router-dom';
import { usePermission } from '@/context/PermissionContext';
import { cn } from '@/lib/utils';

// ReportTabs: one Reports page with a tab per report. The Purchases tab
// needs the purchases view permission as well (the server requires it).
export default function ReportTabs() {
  const { hasPermission } = usePermission();
  const tabs = [
    { to: '/sales-report', label: 'Sales' },
    hasPermission('purchases', 'view') && { to: '/purchase-report', label: 'Purchases' },
    { to: '/customer-report', label: 'Customers' },
    { to: '/customer-due-report', label: 'Customer dues' },
  ].filter(Boolean);
  return (
    <nav aria-label="Reports" className="-mx-4 mb-4 overflow-x-auto border-b px-4 sm:mx-0 sm:px-0">
      <ul className="flex min-w-max gap-1">
        {tabs.map((t) => (
          <li key={t.to}>
            <NavLink
              to={t.to}
              className={({ isActive }) =>
                cn(
                  '-mb-px inline-flex min-h-11 items-center border-b-2 px-4 text-sm font-medium',
                  isActive ? 'border-primary text-primary' : 'border-transparent text-muted-foreground hover:text-foreground'
                )
              }
            >
              {t.label}
            </NavLink>
          </li>
        ))}
      </ul>
    </nav>
  );
}
