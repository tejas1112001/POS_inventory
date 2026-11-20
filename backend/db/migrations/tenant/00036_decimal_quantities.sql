-- ARCHITECTURE_REVIEW H3, step 1: quantities may have three decimals
-- (0.25 kg, 1.5 m, 3 tablets of a strip). Every quantity column becomes
-- numeric(14,3). This is the expand step: an app still on the previous
-- version keeps working (whole numbers read and write the same), and no
-- fraction is written until a product allows it (allow_fraction, below;
-- the app refuses fractions otherwise).
--
-- Changing the type rewrites each table once, under its lock; shops are
-- small enough today (review: "decide it before you have many shops").

-- +goose Up
ALTER TABLE inventories
  ALTER COLUMN qty TYPE numeric(14,3),
  ALTER COLUMN quantity_alert TYPE numeric(14,3);
ALTER TABLE stock_movements
  ALTER COLUMN delta TYPE numeric(14,3),
  ALTER COLUMN qty_after TYPE numeric(14,3);
ALTER TABLE order_items
  ALTER COLUMN quantity TYPE numeric(14,3);
ALTER TABLE return_items
  ALTER COLUMN quantity TYPE numeric(14,3);
ALTER TABLE purchase_items
  ALTER COLUMN quantity TYPE numeric(14,3),
  ALTER COLUMN received_qty TYPE numeric(14,3);
ALTER TABLE stock_adjustments
  ALTER COLUMN quantity TYPE numeric(14,3),
  ALTER COLUMN qty_after TYPE numeric(14,3);
ALTER TABLE stock_count_lines
  ALTER COLUMN counted_qty TYPE numeric(14,3),
  ALTER COLUMN expected_at_completion TYPE numeric(14,3),
  ALTER COLUMN delta TYPE numeric(14,3);
ALTER TABLE purchase_receipt_items
  ALTER COLUMN quantity TYPE numeric(14,3);
ALTER TABLE batch_stock
  ALTER COLUMN qty TYPE numeric(14,3),
  ALTER COLUMN quarantined_qty TYPE numeric(14,3);
ALTER TABLE batch_movements
  ALTER COLUMN delta TYPE numeric(14,3),
  ALTER COLUMN qty_after TYPE numeric(14,3);
ALTER TABLE purchase_receipt_batches
  ALTER COLUMN quantity TYPE numeric(14,3);
ALTER TABLE order_item_batches
  ALTER COLUMN quantity TYPE numeric(14,3);
ALTER TABLE return_item_batches
  ALTER COLUMN quantity TYPE numeric(14,3);
ALTER TABLE write_off_lines
  ALTER COLUMN quantity TYPE numeric(14,3);
ALTER TABLE ledger_checkpoints
  ALTER COLUMN qty TYPE numeric(14,3);

-- A product sold by weight or length allows fractional quantities; the
-- rest stay whole (the server refuses 1.5 of a shirt).
ALTER TABLE products ADD COLUMN allow_fraction boolean NOT NULL DEFAULT false;

-- +goose Down
-- Refuses ("invalid input syntax for type integer: fraction ...") when a
-- fraction has been stored since. Not 1/0: Postgres folds a constant
-- division at plan time, even in a branch that never runs.
ALTER TABLE products DROP COLUMN allow_fraction;
ALTER TABLE ledger_checkpoints
  ALTER COLUMN qty TYPE bigint USING CASE WHEN qty = trunc(qty) THEN qty::bigint ELSE ('fraction ' || qty::text)::bigint END;
ALTER TABLE write_off_lines
  ALTER COLUMN quantity TYPE integer USING CASE WHEN quantity = trunc(quantity) THEN quantity::integer ELSE ('fraction ' || quantity::text)::integer END;
ALTER TABLE return_item_batches
  ALTER COLUMN quantity TYPE integer USING CASE WHEN quantity = trunc(quantity) THEN quantity::integer ELSE ('fraction ' || quantity::text)::integer END;
ALTER TABLE order_item_batches
  ALTER COLUMN quantity TYPE integer USING CASE WHEN quantity = trunc(quantity) THEN quantity::integer ELSE ('fraction ' || quantity::text)::integer END;
ALTER TABLE purchase_receipt_batches
  ALTER COLUMN quantity TYPE integer USING CASE WHEN quantity = trunc(quantity) THEN quantity::integer ELSE ('fraction ' || quantity::text)::integer END;
ALTER TABLE batch_movements
  ALTER COLUMN delta TYPE integer USING CASE WHEN delta = trunc(delta) THEN delta::integer ELSE ('fraction ' || delta::text)::integer END,
  ALTER COLUMN qty_after TYPE integer USING CASE WHEN qty_after = trunc(qty_after) THEN qty_after::integer ELSE ('fraction ' || qty_after::text)::integer END;
ALTER TABLE batch_stock
  ALTER COLUMN qty TYPE integer USING CASE WHEN qty = trunc(qty) THEN qty::integer ELSE ('fraction ' || qty::text)::integer END,
  ALTER COLUMN quarantined_qty TYPE integer USING CASE WHEN quarantined_qty = trunc(quarantined_qty) THEN quarantined_qty::integer ELSE ('fraction ' || quarantined_qty::text)::integer END;
ALTER TABLE purchase_receipt_items
  ALTER COLUMN quantity TYPE integer USING CASE WHEN quantity = trunc(quantity) THEN quantity::integer ELSE ('fraction ' || quantity::text)::integer END;
ALTER TABLE stock_count_lines
  ALTER COLUMN counted_qty TYPE integer USING CASE WHEN counted_qty = trunc(counted_qty) THEN counted_qty::integer ELSE ('fraction ' || counted_qty::text)::integer END,
  ALTER COLUMN expected_at_completion TYPE integer USING CASE WHEN expected_at_completion = trunc(expected_at_completion) THEN expected_at_completion::integer ELSE ('fraction ' || expected_at_completion::text)::integer END,
  ALTER COLUMN delta TYPE integer USING CASE WHEN delta = trunc(delta) THEN delta::integer ELSE ('fraction ' || delta::text)::integer END;
ALTER TABLE stock_adjustments
  ALTER COLUMN quantity TYPE integer USING CASE WHEN quantity = trunc(quantity) THEN quantity::integer ELSE ('fraction ' || quantity::text)::integer END,
  ALTER COLUMN qty_after TYPE integer USING CASE WHEN qty_after = trunc(qty_after) THEN qty_after::integer ELSE ('fraction ' || qty_after::text)::integer END;
ALTER TABLE purchase_items
  ALTER COLUMN quantity TYPE integer USING CASE WHEN quantity = trunc(quantity) THEN quantity::integer ELSE ('fraction ' || quantity::text)::integer END,
  ALTER COLUMN received_qty TYPE integer USING CASE WHEN received_qty = trunc(received_qty) THEN received_qty::integer ELSE ('fraction ' || received_qty::text)::integer END;
ALTER TABLE return_items
  ALTER COLUMN quantity TYPE integer USING CASE WHEN quantity = trunc(quantity) THEN quantity::integer ELSE ('fraction ' || quantity::text)::integer END;
ALTER TABLE order_items
  ALTER COLUMN quantity TYPE integer USING CASE WHEN quantity = trunc(quantity) THEN quantity::integer ELSE ('fraction ' || quantity::text)::integer END;
ALTER TABLE stock_movements
  ALTER COLUMN delta TYPE integer USING CASE WHEN delta = trunc(delta) THEN delta::integer ELSE ('fraction ' || delta::text)::integer END,
  ALTER COLUMN qty_after TYPE integer USING CASE WHEN qty_after = trunc(qty_after) THEN qty_after::integer ELSE ('fraction ' || qty_after::text)::integer END;
ALTER TABLE inventories
  ALTER COLUMN qty TYPE integer USING CASE WHEN qty = trunc(qty) THEN qty::integer ELSE ('fraction ' || qty::text)::integer END,
  ALTER COLUMN quantity_alert TYPE integer USING CASE WHEN quantity_alert = trunc(quantity_alert) THEN quantity_alert::integer ELSE ('fraction ' || quantity_alert::text)::integer END;
