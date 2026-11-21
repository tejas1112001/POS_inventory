-- Scale barcodes (REMAINING "also decided"): labels printed by a weighing
-- scale carry an in-store EAN-13 (prefix 20-29) with the item's scale code
-- (PLU) and its weight or price. Scales are set up per shop, so the
-- format is a shop setting: settings.scale_barcodes is a list of rules
-- {prefix, itemDigits, value (weight or price), decimals}; empty: off.
-- product_variants.scale_code is the item code the scale prints.

-- +goose Up
ALTER TABLE settings
  ADD COLUMN scale_barcodes jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(scale_barcodes) = 'array');
ALTER TABLE product_variants
  ADD COLUMN scale_code text CHECK (scale_code ~ '^[0-9]{4,6}$');
CREATE UNIQUE INDEX product_variants_scale_code_uq ON product_variants (scale_code) WHERE deleted_at IS NULL AND scale_code IS NOT NULL;

-- +goose Down
DROP INDEX product_variants_scale_code_uq;
ALTER TABLE product_variants DROP COLUMN scale_code;
ALTER TABLE settings DROP COLUMN scale_barcodes;
