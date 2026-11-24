import { usePermission } from '@/context/PermissionContext';
import { useModules } from '@/hooks/useModules';
import TabBar from '@/components/kit/TabBar';

// ReportTabs: one Reports page with a tab per report. The Purchases tab
// needs the purchases view permission as well (the server requires it).
export default function ReportTabs() {
  const { hasPermission } = usePermission();
  const { hasModule } = useModules();
  const tabs = [
    { to: '/sales-report', label: 'Sales' },
    hasPermission('purchases', 'view') && { to: '/purchase-report', label: 'Purchases' },
    { to: '/customer-report', label: 'Customers' },
    { to: '/customer-due-report', label: 'Customer dues' },
    hasModule('batch_expiry') && { to: '/reports/expiry', label: 'Expiry' },
    // GST returns cover the whole shop (the server needs sales:view_all).
    hasPermission('sales', 'view_all') && { to: '/reports/gst', label: 'GST returns' },
  ].filter(Boolean);
  return <TabBar label="Reports" tabs={tabs} className="mb-3" />;
}
