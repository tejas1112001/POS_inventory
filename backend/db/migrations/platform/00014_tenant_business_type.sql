-- Industry Phase 1 item 1 (docs/INDUSTRY_PHASE1_DESIGN.md, DB3): a copy of
-- each shop's business type for console filtering (kept in step by the
-- tenant settings write and by provisioning), and override keys that turn a
-- Phase 1 module off for one shop (E2).

-- +goose Up
ALTER TABLE platform.tenants ADD COLUMN business_type text NOT NULL DEFAULT 'retail'
  CONSTRAINT tenants_business_type_check CHECK (business_type IN ('retail','clothing','footwear','pharmacy'));

ALTER TABLE platform.tenant_overrides DROP CONSTRAINT tenant_overrides_key_check;
ALTER TABLE platform.tenant_overrides ADD CONSTRAINT tenant_overrides_key_check CHECK (key IN (
  'max_stores','max_users','max_products','max_emails_per_day','max_storage_mb',
  'public_shop','csv_import','new_checkout',
  'product_attributes','barcode_labels','stock_counts'));

-- +goose Down
ALTER TABLE platform.tenant_overrides DROP CONSTRAINT tenant_overrides_key_check;
ALTER TABLE platform.tenant_overrides ADD CONSTRAINT tenant_overrides_key_check CHECK (key IN (
  'max_stores','max_users','max_products','max_emails_per_day','max_storage_mb',
  'public_shop','csv_import','new_checkout'));
ALTER TABLE platform.tenants DROP COLUMN business_type;
