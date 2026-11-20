-- ARCHITECTURE_REVIEW H3, step 2: units. A product's unit (products.unit_id)
-- is its base unit: stock, the ledgers and every line quantity stay in it.
-- product_units adds other units it is sold or bought in, each with how
-- many base units it holds (a box of 12 pieces: factor 12; 500 g of a
-- product kept in kg: factor 0.5).
--
-- A sale or purchase line in another unit keeps a copy of the unit as it
-- was (name, factor, quantity in that unit), so a later change to the
-- product's units never rewrites an old bill.

-- +goose Up
CREATE TABLE product_units (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  product_id bigint NOT NULL REFERENCES products(id),
  unit_id bigint NOT NULL REFERENCES units(id),
  factor numeric(14,3) NOT NULL CHECK (factor > 0),
  for_sale boolean NOT NULL DEFAULT true,
  for_purchase boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (product_id, unit_id),
  CHECK (for_sale OR for_purchase)
);
CREATE INDEX product_units_unit_idx ON product_units (unit_id);

ALTER TABLE order_items
  ADD COLUMN unit_name text CHECK (length(unit_name) BETWEEN 1 AND 60),
  ADD COLUMN unit_factor numeric(14,3) CHECK (unit_factor > 0),
  ADD COLUMN unit_qty numeric(14,3) CHECK (unit_qty > 0),
  ADD CONSTRAINT order_items_unit_all CHECK ((unit_name IS NULL) = (unit_factor IS NULL) AND (unit_factor IS NULL) = (unit_qty IS NULL));
ALTER TABLE purchase_items
  ADD COLUMN unit_name text CHECK (length(unit_name) BETWEEN 1 AND 60),
  ADD COLUMN unit_factor numeric(14,3) CHECK (unit_factor > 0),
  ADD COLUMN unit_qty numeric(14,3) CHECK (unit_qty > 0),
  ADD CONSTRAINT purchase_items_unit_all CHECK ((unit_name IS NULL) = (unit_factor IS NULL) AND (unit_factor IS NULL) = (unit_qty IS NULL));

-- +goose Down
ALTER TABLE purchase_items DROP COLUMN unit_qty, DROP COLUMN unit_factor, DROP COLUMN unit_name;
ALTER TABLE order_items DROP COLUMN unit_qty, DROP COLUMN unit_factor, DROP COLUMN unit_name;
DROP TABLE product_units;
