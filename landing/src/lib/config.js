// Set at build time (see docs/OPERATIONS.md: nothing hard-coded).
export const PLATFORM_NAME = import.meta.env.VITE_PLATFORM_NAME || 'BilMitra';
export const BASE_DOMAIN = import.meta.env.VITE_BASE_DOMAIN || 'bilmitra.com';

/** https://<slug>.bilmitra.com/login */
export const shopLoginUrl = (slug) => `https://${slug || 'yourshop'}.${BASE_DOMAIN}/login`;
