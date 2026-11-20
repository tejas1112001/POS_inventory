-- Industry Phase 3 item 6: stock counts per batch
-- (docs/INDUSTRY_PHASE3_PHARMACY_DESIGN.md §6, DB35). A count line may
-- name a batch; no batch counts as one value in the unique key. Existing
-- lines keep batch_id NULL and mean what they meant.

-- +goose Up
ALTER TABLE stock_count_lines ADD COLUMN batch_id bigint REFERENCES batches(id);
ALTER TABLE stock_count_lines DROP CONSTRAINT stock_count_lines_count_id_variant_id_key;
CREATE UNIQUE INDEX stock_count_lines_uq ON stock_count_lines (count_id, variant_id, (coalesce(batch_id, 0)));
CREATE INDEX stock_count_lines_batch_idx ON stock_count_lines (batch_id) WHERE batch_id IS NOT NULL;

-- +goose Down
-- Batch lines cannot be kept under the old key (one line per variant).
DELETE FROM stock_count_lines WHERE batch_id IS NOT NULL;
DROP INDEX stock_count_lines_batch_idx;
DROP INDEX stock_count_lines_uq;
ALTER TABLE stock_count_lines ADD CONSTRAINT stock_count_lines_count_id_variant_id_key UNIQUE (count_id, variant_id);
ALTER TABLE stock_count_lines DROP COLUMN batch_id;
