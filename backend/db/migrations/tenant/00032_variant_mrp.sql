-- MRP (maximum retail price) for every product, not only batches.
-- A variant's MRP is the price printed on the pack, tax included. A
-- tracked product's batch MRP (00025) wins over it. Selling above MRP is
-- only warned about, never blocked (NEXT_STEPS: MRP control waits on a
-- legal review). Sale lines keep the MRP at sale time so receipts do not
-- change when the product does. Receipts show MRP and the saving only
-- when the shop turns it on.

-- +goose Up
ALTER TABLE product_variants
  ADD COLUMN mrp numeric(14,2) CONSTRAINT product_variants_mrp_check CHECK (mrp >= 0);

-- mrp: one unit's MRP (NULL when unknown, or when the line's batches carry
-- different MRPs). mrp_amount: the MRP of the whole line, summed over its
-- batches; NULL when any unit has no MRP.
ALTER TABLE order_items
  ADD COLUMN mrp numeric(14,2),
  ADD COLUMN mrp_amount numeric(18,2);

ALTER TABLE settings ADD COLUMN show_mrp_on_receipt boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE settings DROP COLUMN show_mrp_on_receipt;
ALTER TABLE order_items DROP COLUMN mrp_amount, DROP COLUMN mrp;
ALTER TABLE product_variants DROP COLUMN mrp;
