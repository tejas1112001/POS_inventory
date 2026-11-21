-- ARCHITECTURE_REVIEW H3, step 3: returns in units.
--
-- order_items.unit_id records which of the product's units a line was sold
-- in (00037 kept only the name, factor and quantity). A return of "1 box"
-- against a sale takes the lines sold in that unit, at the box size they
-- were sold at, even after the product's units change.
--
-- return_items gets the same unit copy as sale and purchase lines (name,
-- factor, quantity in that unit). Quantities stay in base units.

-- +goose Up
ALTER TABLE order_items
  ADD COLUMN unit_id bigint REFERENCES units(id),
  ADD CONSTRAINT order_items_unit_id_named CHECK (unit_id IS NULL OR unit_name IS NOT NULL);
CREATE INDEX order_items_unit_idx ON order_items (unit_id) WHERE unit_id IS NOT NULL;

-- Lines sold in a unit before this step: the product's unit of that name.
UPDATE order_items i SET unit_id = u.id
  FROM product_units pu JOIN units u ON u.id = pu.unit_id
  WHERE i.unit_name IS NOT NULL AND i.unit_id IS NULL AND pu.product_id = i.product_id AND u.name = i.unit_name;

ALTER TABLE return_items
  ADD COLUMN unit_name text CHECK (length(unit_name) BETWEEN 1 AND 60),
  ADD COLUMN unit_factor numeric(14,3) CHECK (unit_factor > 0),
  ADD COLUMN unit_qty numeric(14,3) CHECK (unit_qty > 0),
  ADD CONSTRAINT return_items_unit_all CHECK ((unit_name IS NULL) = (unit_factor IS NULL) AND (unit_factor IS NULL) = (unit_qty IS NULL));

-- +goose Down
ALTER TABLE return_items DROP COLUMN unit_qty, DROP COLUMN unit_factor, DROP COLUMN unit_name;
DROP INDEX order_items_unit_idx;
ALTER TABLE order_items DROP COLUMN unit_id;
