-- Industry Phase 1 item 5: in-store barcodes and label layouts
-- (docs/INDUSTRY_PHASE1_DESIGN.md §5, DB9, DB10).

-- +goose Up
-- DB9: the counter behind generated EAN-13 codes (2 + 11 digits + check).
ALTER TABLE doc_counters DROP CONSTRAINT doc_counters_name_check;
ALTER TABLE doc_counters ADD CONSTRAINT doc_counters_name_check CHECK (name IN ('sale','sale_return','purchase','barcode'));
INSERT INTO doc_counters (name) VALUES ('barcode') ON CONFLICT (name) DO NOTHING;

-- DB10: the shop's own label layouts (the built-in presets live in code).
ALTER TABLE settings ADD COLUMN label_layouts jsonb NOT NULL DEFAULT '[]'
  CONSTRAINT settings_label_layouts_array CHECK (jsonb_typeof(label_layouts) = 'array' AND jsonb_array_length(label_layouts) <= 20);

-- +goose Down
ALTER TABLE settings DROP COLUMN label_layouts;
DELETE FROM doc_counters WHERE name = 'barcode';
ALTER TABLE doc_counters DROP CONSTRAINT doc_counters_name_check;
ALTER TABLE doc_counters ADD CONSTRAINT doc_counters_name_check CHECK (name IN ('sale','sale_return','purchase'));
