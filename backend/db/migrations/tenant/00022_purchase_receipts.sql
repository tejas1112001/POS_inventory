-- Industry Phase 1 item 8: partial purchase receiving
-- (docs/INDUSTRY_PHASE1_DESIGN.md §8, DB13, DB14, DB15). A purchase is
-- received in one or more receipts; each receipt line puts stock in through
-- the ledger. Existing received purchases get one receipt for everything
-- (the backfill, run on a copy first with a per-shop report).

-- +goose Up
-- DB13: goods receipts and their counter (GRN-000001).
ALTER TABLE doc_counters DROP CONSTRAINT doc_counters_name_check;
ALTER TABLE doc_counters ADD CONSTRAINT doc_counters_name_check
  CHECK (name IN ('sale','sale_return','purchase','barcode','stock_count','goods_receipt'));
INSERT INTO doc_counters (name) VALUES ('goods_receipt') ON CONFLICT (name) DO NOTHING;

CREATE TABLE purchase_receipts (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  purchase_id bigint NOT NULL REFERENCES purchases(id),
  reference text NOT NULL UNIQUE CHECK (length(reference) BETWEEN 1 AND 40),
  received_at timestamptz NOT NULL DEFAULT now(),
  received_by bigint REFERENCES users(id),
  note text CHECK (length(note) <= 1000),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX purchase_receipts_purchase_idx ON purchase_receipts (purchase_id);

CREATE TABLE purchase_receipt_items (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  receipt_id bigint NOT NULL REFERENCES purchase_receipts(id),
  purchase_item_id bigint NOT NULL REFERENCES purchase_items(id),
  quantity integer NOT NULL CHECK (quantity > 0),
  UNIQUE (receipt_id, purchase_item_id)
);
CREATE INDEX purchase_receipt_items_item_idx ON purchase_receipt_items (purchase_item_id);

-- DB14: how much of each line has arrived.
ALTER TABLE purchase_items ADD COLUMN received_qty integer NOT NULL DEFAULT 0
  CONSTRAINT purchase_items_received_qty_check CHECK (received_qty BETWEEN 0 AND quantity);

-- DB15: a purchase that has arrived in part.
ALTER TABLE purchases DROP CONSTRAINT purchases_status_check;
ALTER TABLE purchases ADD CONSTRAINT purchases_status_check
  CHECK (status IN ('Ordered','Pending','Partially received','Received','Cancelled'));

-- Backfill: every received purchase is fully received, in one receipt
-- dated when it was received (oldest first, so GRN numbers follow time).
UPDATE purchase_items i SET received_qty = i.quantity
  FROM purchases p WHERE p.id = i.purchase_id AND p.status = 'Received';
INSERT INTO purchase_receipts (purchase_id, reference, received_at, received_by, created_at)
SELECT p.id, 'GRN-' || lpad((row_number() OVER (ORDER BY p.received_at, p.id))::text, 6, '0'), p.received_at, p.received_by, p.received_at
  FROM purchases p WHERE p.status = 'Received' ORDER BY p.received_at, p.id;
INSERT INTO purchase_receipt_items (receipt_id, purchase_item_id, quantity)
SELECT r.id, i.id, i.quantity FROM purchase_receipts r JOIN purchase_items i ON i.purchase_id = r.purchase_id ORDER BY r.id, i.id;
UPDATE doc_counters SET next_value = (SELECT count(*) + 1 FROM purchase_receipts) WHERE name = 'goods_receipt';

-- +goose Down
ALTER TABLE purchases DROP CONSTRAINT purchases_status_check;
ALTER TABLE purchases ADD CONSTRAINT purchases_status_check CHECK (status IN ('Ordered','Pending','Received','Cancelled'));
ALTER TABLE purchase_items DROP COLUMN received_qty;
DROP TABLE purchase_receipt_items;
DROP TABLE purchase_receipts;
DELETE FROM doc_counters WHERE name = 'goods_receipt';
ALTER TABLE doc_counters DROP CONSTRAINT doc_counters_name_check;
ALTER TABLE doc_counters ADD CONSTRAINT doc_counters_name_check CHECK (name IN ('sale','sale_return','purchase','barcode','stock_count'));
