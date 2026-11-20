-- ARCHITECTURE_REVIEW M1: business types, modules, stock-movement reasons,
-- document counters and email kinds were fixed lists in a CHECK in every
-- shop schema, so a new vertical, module or reason needed a migration
-- across every shop. The lists now live in the code (internal/modules and
-- the packages that write each value); the database keeps a format check
-- under the same name, which still refuses junk.
--
-- The ledgers can be large: their new checks are added NOT VALID here (a
-- brief lock, no scan) and validated in 00035, which holds only a lock
-- that lets sales go on.

-- +goose Up
ALTER TABLE settings DROP CONSTRAINT settings_business_type_check,
  ADD CONSTRAINT settings_business_type_check CHECK (business_type ~ '^[a-z][a-z0-9_]{0,39}$');
ALTER TABLE settings DROP CONSTRAINT settings_modules_check,
  ADD CONSTRAINT settings_modules_check CHECK (
    cardinality(modules) <= 64 AND array_position(modules, NULL) IS NULL
    AND array_to_string(modules, ',') ~ '^([a-z][a-z0-9_]{0,39}(,[a-z][a-z0-9_]{0,39})*)?$');
ALTER TABLE doc_counters DROP CONSTRAINT doc_counters_name_check,
  ADD CONSTRAINT doc_counters_name_check CHECK (name ~ '^[a-z][a-z0-9_]{0,39}$');
ALTER TABLE email_outbox DROP CONSTRAINT email_outbox_kind_check,
  ADD CONSTRAINT email_outbox_kind_check CHECK (kind ~ '^[a-z][a-z0-9_]{0,39}$') NOT VALID;
ALTER TABLE stock_movements DROP CONSTRAINT stock_movements_reason_check,
  ADD CONSTRAINT stock_movements_reason_check CHECK (reason ~ '^[a-z][a-z0-9_]{0,39}$') NOT VALID;
ALTER TABLE batch_movements DROP CONSTRAINT batch_movements_reason_check,
  ADD CONSTRAINT batch_movements_reason_check CHECK (reason ~ '^[a-z][a-z0-9_]{0,39}$') NOT VALID;

-- +goose Down
-- Fails if a value outside the old lists has been written since.
ALTER TABLE batch_movements DROP CONSTRAINT batch_movements_reason_check,
  ADD CONSTRAINT batch_movements_reason_check CHECK (reason IN ('opening','sale','sale_edit','sale_void','return',
  'purchase_receive','adjustment','manual_set','product_edit','import','stock_count','tracking_on','tracking_off','quarantine','quarantine_release',
  'write_off'));
ALTER TABLE stock_movements DROP CONSTRAINT stock_movements_reason_check,
  ADD CONSTRAINT stock_movements_reason_check CHECK (reason IN ('opening','sale','sale_edit','sale_void','return',
  'purchase_receive','adjustment','manual_set','product_edit','import','stock_count','quarantine','quarantine_release','write_off'));
ALTER TABLE email_outbox DROP CONSTRAINT email_outbox_kind_check,
  ADD CONSTRAINT email_outbox_kind_check
  CHECK (kind IN ('receipt','receipt_resend','test','customer','platform_notice','low_stock','expiry_digest'));
ALTER TABLE doc_counters DROP CONSTRAINT doc_counters_name_check,
  ADD CONSTRAINT doc_counters_name_check CHECK (name IN ('sale','sale_return','purchase','barcode','stock_count','goods_receipt','write_off'));
ALTER TABLE settings DROP CONSTRAINT settings_modules_check,
  ADD CONSTRAINT settings_modules_check CHECK (
    modules <@ ARRAY['product_attributes','barcode_labels','stock_counts','batch_expiry']::text[]
    AND array_position(modules, NULL) IS NULL);
ALTER TABLE settings DROP CONSTRAINT settings_business_type_check,
  ADD CONSTRAINT settings_business_type_check CHECK (business_type IN ('retail','clothing','footwear','pharmacy'));
