-- Industry Phase 3 item 2: receiving with batch and expiry
-- (docs/INDUSTRY_PHASE3_PHARMACY_DESIGN.md §2, DB30). Also DB36 (§7): the
-- shop's batch settings, because receiving already needs the near-expiry
-- window and selling the minimum days to sell. Defaults keep every shop as
-- it is; the settings only matter with the batch_expiry module on.

-- +goose Up
-- DB30: which batches a goods receipt line put stock into.
CREATE TABLE purchase_receipt_batches (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  receipt_item_id bigint NOT NULL REFERENCES purchase_receipt_items(id),
  batch_id bigint NOT NULL REFERENCES batches(id),
  quantity integer NOT NULL CHECK (quantity > 0),
  near_expiry_accepted boolean NOT NULL DEFAULT false,
  UNIQUE (receipt_item_id, batch_id)
);
CREATE INDEX purchase_receipt_batches_batch_idx ON purchase_receipt_batches(batch_id);

-- DB36: batch settings (Q11, Q16).
ALTER TABLE settings
  ADD COLUMN near_expiry_days integer NOT NULL DEFAULT 90 CHECK (near_expiry_days BETWEEN 1 AND 730),
  ADD COLUMN min_days_to_sell integer NOT NULL DEFAULT 0 CHECK (min_days_to_sell BETWEEN 0 AND 365),
  ADD COLUMN expiry_digest boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE settings DROP COLUMN expiry_digest, DROP COLUMN min_days_to_sell, DROP COLUMN near_expiry_days;
DROP TABLE purchase_receipt_batches;
