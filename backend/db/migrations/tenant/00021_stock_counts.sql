-- Industry Phase 1 item 7: stock counts (docs/INDUSTRY_PHASE1_DESIGN.md
-- §7, DB11, DB12). Nothing changes stock until a count is completed; then
-- each counted line posts one ledger movement with reason 'stock_count'.

-- +goose Up
-- DB11: the counter behind count references (CNT-000001).
ALTER TABLE doc_counters DROP CONSTRAINT doc_counters_name_check;
ALTER TABLE doc_counters ADD CONSTRAINT doc_counters_name_check CHECK (name IN ('sale','sale_return','purchase','barcode','stock_count'));
INSERT INTO doc_counters (name) VALUES ('stock_count') ON CONFLICT (name) DO NOTHING;

CREATE TABLE stock_counts (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  reference text NOT NULL UNIQUE CHECK (length(reference) BETWEEN 1 AND 40),
  store_id bigint NOT NULL REFERENCES stores(id),
  -- {} = the whole store; {"categoryId": n} and/or {"brandId": n} = part of it.
  scope jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(scope) = 'object'),
  status text NOT NULL DEFAULT 'Draft' CHECK (status IN ('Draft', 'Completed', 'Cancelled')),
  note text CHECK (length(note) <= 1000),
  created_by bigint REFERENCES users(id),
  completed_by bigint REFERENCES users(id),
  completed_at timestamptz,
  version integer NOT NULL DEFAULT 1 CHECK (version >= 1),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT stock_counts_completed_check CHECK ((status = 'Completed') = (completed_at IS NOT NULL))
);
CREATE INDEX stock_counts_store_idx ON stock_counts (store_id, status, id DESC);

CREATE TABLE stock_count_lines (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  count_id bigint NOT NULL REFERENCES stock_counts(id),
  variant_id bigint NOT NULL REFERENCES product_variants(id),
  counted_qty integer NOT NULL CHECK (counted_qty BETWEEN 0 AND 1000000),
  counted_at timestamptz NOT NULL DEFAULT now(),
  counted_by bigint REFERENCES users(id),
  -- Set when the count is completed: the quantity it replaced and the
  -- movement it posted (counted - expected).
  expected_at_completion integer CHECK (expected_at_completion >= 0),
  delta integer,
  UNIQUE (count_id, variant_id)
);
CREATE INDEX stock_count_lines_variant_idx ON stock_count_lines (variant_id);

-- DB12: the ledger reason for completed counts.
ALTER TABLE stock_movements DROP CONSTRAINT stock_movements_reason_check;
ALTER TABLE stock_movements ADD CONSTRAINT stock_movements_reason_check CHECK (reason IN ('opening','sale','sale_edit','sale_void','return',
  'purchase_receive','adjustment','manual_set','product_edit','import','stock_count'));

-- +goose Down
ALTER TABLE stock_movements DROP CONSTRAINT stock_movements_reason_check;
ALTER TABLE stock_movements ADD CONSTRAINT stock_movements_reason_check CHECK (reason IN ('opening','sale','sale_edit','sale_void','return',
  'purchase_receive','adjustment','manual_set','product_edit','import'));
DROP TABLE stock_count_lines;
DROP TABLE stock_counts;
DELETE FROM doc_counters WHERE name = 'stock_count';
ALTER TABLE doc_counters DROP CONSTRAINT doc_counters_name_check;
ALTER TABLE doc_counters ADD CONSTRAINT doc_counters_name_check CHECK (name IN ('sale','sale_return','purchase','barcode'));
