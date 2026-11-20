-- Step 12: stock adjustments (PHASE1_DESIGN §4.3, PRD §3.9, P0-4). A record
-- of each manual addition or subtraction, made atomically with the stock
-- change and its ledger row. Append-only for the tenant role
-- (db.AppendOnlyTables): corrections are new adjustments.

-- +goose Up
CREATE TABLE stock_adjustments (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  type text NOT NULL CHECK (type IN ('Addition','Subtraction')),
  quantity integer NOT NULL CHECK (quantity > 0),
  reason text NOT NULL CHECK (length(reason) BETWEEN 1 AND 200),
  note text CHECK (length(note) <= 1000),
  product_id bigint NOT NULL REFERENCES products(id),
  variant_id bigint NOT NULL REFERENCES product_variants(id),
  store_id bigint NOT NULL REFERENCES stores(id),
  qty_after integer NOT NULL CHECK (qty_after >= 0),               -- stock right after this adjustment
  created_by bigint NOT NULL REFERENCES users(id),
  impersonation_id uuid,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX stock_adjustments_created_idx ON stock_adjustments (created_at DESC);
CREATE INDEX stock_adjustments_store_variant_idx ON stock_adjustments (store_id, variant_id);

-- +goose Down
DROP TABLE stock_adjustments;
