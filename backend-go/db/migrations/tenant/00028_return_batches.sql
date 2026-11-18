-- Industry Phase 3 item 4: returns per batch and quarantine
-- (docs/INDUSTRY_PHASE3_PHARMACY_DESIGN.md §4, DB32, DB33).
-- New table, empty; two ledger reasons added. Nothing changes for shops
-- without tracked products.

-- +goose Up
-- DB32: where each returned unit of a tracked product went: back to a
-- batch's sellable stock, or to its quarantine (counted, never sold).
CREATE TABLE return_item_batches (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  return_item_id bigint NOT NULL REFERENCES return_items(id),
  batch_id bigint NOT NULL REFERENCES batches(id),
  quantity integer NOT NULL CHECK (quantity > 0),
  bucket text NOT NULL CHECK (bucket IN ('stock', 'quarantine')),
  UNIQUE (return_item_id, batch_id, bucket)
);
CREATE INDEX return_item_batches_batch_idx ON return_item_batches (batch_id);

-- DB33: moving units between sellable stock and quarantine changes
-- inventories.qty, so both ledgers name it.
ALTER TABLE stock_movements DROP CONSTRAINT stock_movements_reason_check;
ALTER TABLE stock_movements ADD CONSTRAINT stock_movements_reason_check CHECK (reason IN ('opening','sale','sale_edit','sale_void','return',
  'purchase_receive','adjustment','manual_set','product_edit','import','stock_count','quarantine','quarantine_release'));
ALTER TABLE batch_movements DROP CONSTRAINT batch_movements_reason_check;
ALTER TABLE batch_movements ADD CONSTRAINT batch_movements_reason_check CHECK (reason IN ('opening','sale','sale_edit','sale_void','return',
  'purchase_receive','adjustment','manual_set','product_edit','import','stock_count','tracking_on','tracking_off','quarantine','quarantine_release'));

-- +goose Down
ALTER TABLE batch_movements DROP CONSTRAINT batch_movements_reason_check;
ALTER TABLE batch_movements ADD CONSTRAINT batch_movements_reason_check CHECK (reason IN ('opening','sale','sale_edit','sale_void','return',
  'purchase_receive','adjustment','manual_set','product_edit','import','stock_count','tracking_on','tracking_off'));
ALTER TABLE stock_movements DROP CONSTRAINT stock_movements_reason_check;
ALTER TABLE stock_movements ADD CONSTRAINT stock_movements_reason_check CHECK (reason IN ('opening','sale','sale_edit','sale_void','return',
  'purchase_receive','adjustment','manual_set','product_edit','import','stock_count'));
DROP TABLE return_item_batches;
