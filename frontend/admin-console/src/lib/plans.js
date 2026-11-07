// Plan limits and features (BILLING_DESIGN §2). A limit of 0 means none.
export const LIMITS = [
  ['max_stores', 'Stores'],
  ['max_users', 'Users'],
  ['max_products', 'Products'],
  ['max_storage_mb', 'File storage (MB)'],
  ['max_emails_per_day', 'Emails per day'],
];

export const FEATURES = [
  ['reports_export', 'Report export'],
  ['public_shop', 'Public shop'],
  ['tenant_smtp', 'Own email server'],
  ['multi_store', 'Several stores'],
  ['custom_domain', 'Custom domain'],
];
