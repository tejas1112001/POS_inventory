-- Industry Phase 3 item 3: selling by batch (docs/INDUSTRY_PHASE3_PHARMACY_DESIGN.md
-- §3, DB31). Each line of a tracked product records the batches it was
-- sold from, with the batch details copied at sale time (the receipt keeps
-- them even if the batch is corrected later, A24). Older sales and
-- untracked products have no rows.

-- +goose Up
CREATE TABLE order_item_batches (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  order_item_id bigint NOT NULL REFERENCES order_items(id),
  batch_id bigint NOT NULL REFERENCES batches(id),
  quantity integer NOT NULL CHECK (quantity > 0),
  chosen boolean NOT NULL DEFAULT false,
  is_unknown boolean NOT NULL DEFAULT false,
  batch_no citext NOT NULL,
  expiry_date date,
  mrp numeric(14,2),
  UNIQUE (order_item_id, batch_id)
);
CREATE INDEX order_item_batches_batch_idx ON order_item_batches(batch_id);

-- +goose Down
DROP TABLE order_item_batches;
