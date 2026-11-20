-- Industry Phase 3 item 5: write-offs and recalls
-- (docs/INDUSTRY_PHASE3_PHARMACY_DESIGN.md §5, DB34). New tables, empty;
-- one counter and one ledger reason added. Recalls use the batches.status
-- column from 00025.

-- +goose Up
-- DB34: a write-off takes units of batches out of the shop for good:
-- expired, damaged, recalled, returned to the supplier, or other. Posted
-- when created; never edited (db.AppendOnlyTables).
CREATE TABLE write_offs (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  reference text NOT NULL UNIQUE,
  store_id bigint NOT NULL REFERENCES stores(id),
  reason text NOT NULL CHECK (reason IN ('expired', 'damaged', 'recalled', 'returned_to_supplier', 'other')),
  note text CHECK (length(note) <= 500),
  created_by bigint REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX write_offs_store_idx ON write_offs (store_id, created_at);

CREATE TABLE write_off_lines (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  write_off_id bigint NOT NULL REFERENCES write_offs(id),
  batch_id bigint NOT NULL REFERENCES batches(id),
  variant_id bigint NOT NULL REFERENCES product_variants(id),
  bucket text NOT NULL CHECK (bucket IN ('stock', 'quarantine')),
  quantity integer NOT NULL CHECK (quantity > 0),
  UNIQUE (write_off_id, batch_id, bucket)
);
CREATE INDEX write_off_lines_batch_idx ON write_off_lines (batch_id);

ALTER TABLE doc_counters DROP CONSTRAINT doc_counters_name_check;
ALTER TABLE doc_counters ADD CONSTRAINT doc_counters_name_check CHECK (name IN ('sale','sale_return','purchase','barcode','stock_count','goods_receipt','write_off'));

ALTER TABLE stock_movements DROP CONSTRAINT stock_movements_reason_check;
ALTER TABLE stock_movements ADD CONSTRAINT stock_movements_reason_check CHECK (reason IN ('opening','sale','sale_edit','sale_void','return',
  'purchase_receive','adjustment','manual_set','product_edit','import','stock_count','quarantine','quarantine_release','write_off'));
ALTER TABLE batch_movements DROP CONSTRAINT batch_movements_reason_check;
ALTER TABLE batch_movements ADD CONSTRAINT batch_movements_reason_check CHECK (reason IN ('opening','sale','sale_edit','sale_void','return',
  'purchase_receive','adjustment','manual_set','product_edit','import','stock_count','tracking_on','tracking_off','quarantine','quarantine_release',
  'write_off'));

-- +goose Down
ALTER TABLE batch_movements DROP CONSTRAINT batch_movements_reason_check;
ALTER TABLE batch_movements ADD CONSTRAINT batch_movements_reason_check CHECK (reason IN ('opening','sale','sale_edit','sale_void','return',
  'purchase_receive','adjustment','manual_set','product_edit','import','stock_count','tracking_on','tracking_off','quarantine','quarantine_release'));
ALTER TABLE stock_movements DROP CONSTRAINT stock_movements_reason_check;
ALTER TABLE stock_movements ADD CONSTRAINT stock_movements_reason_check CHECK (reason IN ('opening','sale','sale_edit','sale_void','return',
  'purchase_receive','adjustment','manual_set','product_edit','import','stock_count','quarantine','quarantine_release'));
DELETE FROM doc_counters WHERE name = 'write_off';
ALTER TABLE doc_counters DROP CONSTRAINT doc_counters_name_check;
ALTER TABLE doc_counters ADD CONSTRAINT doc_counters_name_check CHECK (name IN ('sale','sale_return','purchase','barcode','stock_count','goods_receipt'));
DROP TABLE write_off_lines;
DROP TABLE write_offs;
