-- Industry Phase 1 item 1 (docs/INDUSTRY_PHASE1_DESIGN.md, DB1, DB2): the
-- shop's business type (a preset, never a block) and its switched-on
-- modules. Defaults reproduce today's behaviour: retail, no modules.

-- +goose Up
ALTER TABLE settings
  ADD COLUMN business_type text NOT NULL DEFAULT 'retail'
    CONSTRAINT settings_business_type_check CHECK (business_type IN ('retail','clothing','footwear','pharmacy')),
  ADD COLUMN modules text[] NOT NULL DEFAULT '{}'
    -- Later phases widen this list.
    CONSTRAINT settings_modules_check CHECK (
      modules <@ ARRAY['product_attributes','barcode_labels','stock_counts']::text[]
      AND array_position(modules, NULL) IS NULL);

-- +goose Down
ALTER TABLE settings DROP COLUMN modules, DROP COLUMN business_type;
