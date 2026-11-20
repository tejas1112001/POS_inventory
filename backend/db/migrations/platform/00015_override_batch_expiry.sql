-- Industry Phase 3 item 1 (docs/INDUSTRY_PHASE3_PHARMACY_DESIGN.md, DB37):
-- the console can turn the batch_expiry module off for one shop, like the
-- Phase 1 modules (E2). Plans do not name the module, so every plan and the
-- trial include it (E3).

-- +goose Up
ALTER TABLE platform.tenant_overrides DROP CONSTRAINT tenant_overrides_key_check;
ALTER TABLE platform.tenant_overrides ADD CONSTRAINT tenant_overrides_key_check CHECK (key IN (
  'max_stores','max_users','max_products','max_emails_per_day','max_storage_mb',
  'public_shop','csv_import','new_checkout',
  'product_attributes','barcode_labels','stock_counts','batch_expiry'));

-- +goose Down
DELETE FROM platform.tenant_overrides WHERE key = 'batch_expiry';
ALTER TABLE platform.tenant_overrides DROP CONSTRAINT tenant_overrides_key_check;
ALTER TABLE platform.tenant_overrides ADD CONSTRAINT tenant_overrides_key_check CHECK (key IN (
  'max_stores','max_users','max_products','max_emails_per_day','max_storage_mb',
  'public_shop','csv_import','new_checkout',
  'product_attributes','barcode_labels','stock_counts'));
