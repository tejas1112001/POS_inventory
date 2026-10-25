import TenantBilling from './TenantBilling';

// Tabs on the tenant page after Overview, each with the permissions that
// show it (any of them).
export const TENANT_TABS = [{ key: 'billing', label: 'Billing', perm: ['billing:view'], Component: TenantBilling }];
