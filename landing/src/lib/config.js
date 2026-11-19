// Set at build time (see docs/OPERATIONS.md: nothing hard-coded).
export const PLATFORM_NAME = import.meta.env.VITE_PLATFORM_NAME || 'BillPiko';
export const BASE_DOMAIN = import.meta.env.VITE_BASE_DOMAIN || 'billpiko.com';

/** https://<slug>.billpiko.com/login */
export const shopLoginUrl = (slug) => `https://${slug || 'yourshop'}.${BASE_DOMAIN}/login`;
