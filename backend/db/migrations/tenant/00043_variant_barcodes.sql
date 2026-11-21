-- Several barcodes per variant (ARCHITECTURE_REVIEW L6, REMAINING "also
-- decided"). product_variants.item_barcode stays the main code (labels
-- print it); variant_barcodes adds others: an old EAN after a pack change,
-- a case code. A code may name one of the product's units (H3): scanning
-- a case code adds one case.
--
-- A code is unique among the extra codes (index) and must not be any
-- variant's main code: the app checks both directions.

-- +goose Up
CREATE TABLE variant_barcodes (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  variant_id bigint NOT NULL REFERENCES product_variants(id),
  code text NOT NULL CHECK (code ~ '^[A-Za-z0-9._/+-]{1,64}$'),
  unit_id bigint REFERENCES units(id),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX variant_barcodes_code_uq ON variant_barcodes (code);
CREATE INDEX variant_barcodes_variant_idx ON variant_barcodes (variant_id);

-- +goose Down
DROP TABLE variant_barcodes;
