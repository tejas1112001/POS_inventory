-- M3 step 1 (docs/M3_GST_DESIGN.md DB1-DB4): who is registered where.
-- The shop's home state; each store's own GSTIN, state and invoice code
-- (falling back to the shop's); customers' and suppliers' GSTIN and state.
-- All nullable: a shop that sets nothing works as before. The state list
-- and the GSTIN check character live in code (internal/gst, as M1); the
-- schema checks the format only.

-- +goose Up
ALTER TABLE settings
  ADD COLUMN state_code text CHECK (state_code ~ '^[0-9]{2}$');

ALTER TABLE stores
  ADD COLUMN gstin text CHECK (gstin ~ '^[0-9]{2}[A-Z0-9]{13}$'),
  ADD COLUMN state_code text CHECK (state_code ~ '^[0-9]{2}$'),
  ADD COLUMN invoice_code text CHECK (invoice_code ~ '^[A-Z0-9]{2,5}$'),
  ADD CONSTRAINT stores_gstin_state CHECK (gstin IS NULL OR state_code = left(gstin, 2));
CREATE UNIQUE INDEX stores_invoice_code_uq ON stores (invoice_code) WHERE deleted_at IS NULL AND invoice_code IS NOT NULL;

ALTER TABLE customers
  ADD COLUMN gstin text CHECK (gstin ~ '^[0-9]{2}[A-Z0-9]{13}$'),
  ADD COLUMN state_code text CHECK (state_code ~ '^[0-9]{2}$'),
  ADD COLUMN legal_name text CHECK (length(legal_name) BETWEEN 1 AND 200),
  ADD COLUMN billing_address text CHECK (length(billing_address) BETWEEN 1 AND 500),
  ADD CONSTRAINT customers_gstin_state CHECK (gstin IS NULL OR state_code = left(gstin, 2));

ALTER TABLE suppliers
  ADD COLUMN gstin text CHECK (gstin ~ '^[0-9]{2}[A-Z0-9]{13}$'),
  ADD COLUMN state_code text CHECK (state_code ~ '^[0-9]{2}$'),
  ADD CONSTRAINT suppliers_gstin_state CHECK (gstin IS NULL OR state_code = left(gstin, 2));

-- +goose Down
ALTER TABLE suppliers DROP COLUMN state_code, DROP COLUMN gstin;
ALTER TABLE customers DROP COLUMN billing_address, DROP COLUMN legal_name, DROP COLUMN state_code, DROP COLUMN gstin;
DROP INDEX stores_invoice_code_uq;
ALTER TABLE stores DROP COLUMN invoice_code, DROP COLUMN state_code, DROP COLUMN gstin;
ALTER TABLE settings DROP COLUMN state_code;
