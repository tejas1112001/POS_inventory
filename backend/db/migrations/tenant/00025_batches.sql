-- Industry Phase 3 item 1: batches and expiry (docs/INDUSTRY_PHASE3_PHARMACY_DESIGN.md
-- §1, DB25–DB29). Nothing changes for a shop until it turns the
-- batch_expiry module on and a product tracks batches: every product starts
-- untracked and the new tables start empty, so no backfill runs here.
--
-- The rule kept by internal/inventory (batch.go): for a tracked product in
-- a store, inventories.qty = sum(batch_stock.qty). Quarantined units are
-- outside both.

-- +goose Up
-- DB25: the module.
ALTER TABLE settings DROP CONSTRAINT settings_modules_check;
ALTER TABLE settings ADD CONSTRAINT settings_modules_check CHECK (
  modules <@ ARRAY['product_attributes','barcode_labels','stock_counts','batch_expiry']::text[]
  AND array_position(modules, NULL) IS NULL);

-- DB26: which products keep stock per batch.
ALTER TABLE products ADD COLUMN tracks_batches boolean NOT NULL DEFAULT false;

-- DB27: a batch (lot) of one variant. expiry_date is NULL only for the
-- unknown batch, which holds stock that had no batch when tracking began.
-- "Expired" is worked out from the date in the shop's time zone, never
-- stored, so no nightly job can run late.
CREATE TABLE batches (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  variant_id bigint NOT NULL REFERENCES product_variants(id),
  batch_no citext NOT NULL CHECK (length(batch_no) BETWEEN 1 AND 40),
  expiry_date date,
  mfg_date date,
  mrp numeric(14,2) CHECK (mrp >= 0),
  cost numeric(14,2) CHECK (cost >= 0),
  is_unknown boolean NOT NULL DEFAULT false,
  status text NOT NULL DEFAULT 'available' CHECK (status IN ('available', 'recalled')),
  recalled_at timestamptz,
  recall_reason text CHECK (length(recall_reason) <= 500),
  created_by bigint REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (variant_id, batch_no),
  CHECK (is_unknown OR expiry_date IS NOT NULL),
  CHECK (NOT is_unknown OR (expiry_date IS NULL AND batch_no = 'UNBATCHED')),
  CHECK (mfg_date IS NULL OR expiry_date IS NULL OR mfg_date <= expiry_date),
  CHECK ((status = 'recalled') = (recalled_at IS NOT NULL))
);
CREATE UNIQUE INDEX batches_unknown_uq ON batches (variant_id) WHERE is_unknown;
CREATE INDEX batches_expiry_idx ON batches (expiry_date) WHERE expiry_date IS NOT NULL;

-- DB28: stock of a batch in a store.
CREATE TABLE batch_stock (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  store_id bigint NOT NULL REFERENCES stores(id),
  batch_id bigint NOT NULL REFERENCES batches(id),
  qty integer NOT NULL DEFAULT 0 CHECK (qty >= 0),
  quarantined_qty integer NOT NULL DEFAULT 0 CHECK (quarantined_qty >= 0),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (store_id, batch_id)
);
CREATE INDEX batch_stock_batch_idx ON batch_stock (batch_id);

-- DB29: append-only ledger of batch quantities (db.AppendOnlyTables), one
-- row per change, like stock_movements.
CREATE TABLE batch_movements (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  store_id bigint NOT NULL REFERENCES stores(id),
  batch_id bigint NOT NULL REFERENCES batches(id),
  bucket text NOT NULL DEFAULT 'stock' CHECK (bucket IN ('stock', 'quarantine')),
  delta integer NOT NULL CHECK (delta <> 0),
  qty_after integer NOT NULL CHECK (qty_after >= 0),
  reason text NOT NULL CHECK (reason IN ('opening','sale','sale_edit','sale_void','return','purchase_receive','adjustment',
    'manual_set','product_edit','import','stock_count','tracking_on','tracking_off')),
  ref_type text, ref_id bigint,
  user_id bigint REFERENCES users(id),
  impersonation_id uuid,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX batch_movements_batch_idx ON batch_movements (batch_id, created_at);
CREATE INDEX batch_movements_ref_idx ON batch_movements (ref_type, ref_id) WHERE ref_id IS NOT NULL;

-- +goose Down
DROP TABLE batch_movements;
DROP TABLE batch_stock;
DROP TABLE batches;
ALTER TABLE products DROP COLUMN tracks_batches;
UPDATE settings SET modules = array_remove(modules, 'batch_expiry');
ALTER TABLE settings DROP CONSTRAINT settings_modules_check;
ALTER TABLE settings ADD CONSTRAINT settings_modules_check CHECK (
  modules <@ ARRAY['product_attributes','barcode_labels','stock_counts']::text[]
  AND array_position(modules, NULL) IS NULL);
