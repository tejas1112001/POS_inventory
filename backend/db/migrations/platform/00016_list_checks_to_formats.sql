-- ARCHITECTURE_REVIEW M1: the console's copy of the business type and the
-- override keys were fixed lists in a CHECK. They are validated in the
-- code now (internal/modules, internal/platform/overrides.go); the
-- database keeps a format check under the same name.

-- +goose Up
ALTER TABLE platform.tenants DROP CONSTRAINT tenants_business_type_check,
  ADD CONSTRAINT tenants_business_type_check CHECK (business_type ~ '^[a-z][a-z0-9_]{0,39}$');
ALTER TABLE platform.tenant_overrides DROP CONSTRAINT tenant_overrides_key_check,
  ADD CONSTRAINT tenant_overrides_key_check CHECK (key ~ '^[a-z][a-z0-9_]{0,39}$');

-- +goose Down
-- Fails if a value outside the old lists has been written since.
ALTER TABLE platform.tenant_overrides DROP CONSTRAINT tenant_overrides_key_check,
  ADD CONSTRAINT tenant_overrides_key_check CHECK (key IN (
  'max_stores','max_users','max_products','max_emails_per_day','max_storage_mb',
  'public_shop','csv_import','new_checkout',
  'product_attributes','barcode_labels','stock_counts','batch_expiry'));
ALTER TABLE platform.tenants DROP CONSTRAINT tenants_business_type_check,
  ADD CONSTRAINT tenants_business_type_check CHECK (business_type IN ('retail','clothing','footwear','pharmacy'));
