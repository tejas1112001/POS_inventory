import TenantBilling from './TenantBilling';
import TenantExports from './TenantExports';
import { TenantAudit } from './Audit';
import TenantContacts from './TenantContacts';
import TenantNotes from './TenantNotes';

// Tabs on the tenant page after Overview, each with the permissions that
// show it (any of them).
export const TENANT_TABS = [
  { key: 'contacts', label: 'Contacts', perm: ['tenants:view'], Component: TenantContacts },
  { key: 'notes', label: 'Notes', perm: ['tenants:view'], Component: TenantNotes },
  { key: 'billing', label: 'Billing', perm: ['billing:view'], Component: TenantBilling },
  { key: 'exports', label: 'Exports', perm: ['tenants:export'], Component: TenantExports },
  { key: 'audit', label: 'Audit', perm: ['audit:view_all', 'audit:view_own'], Component: TenantAudit },
];
