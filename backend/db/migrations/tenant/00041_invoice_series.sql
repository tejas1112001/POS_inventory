-- M3 step 4 (docs/M3_GST_DESIGN.md §6, DB7): invoice and credit note
-- numbers per store and financial year, opt-in. settings.invoice_numbering
-- 'shop' (the default) keeps the shop-wide counters (INV-000042,
-- RET-000010); 'store_year' numbers each store's documents per financial
-- year (1 April to 31 March): MUM/2627/00042 and MUM/2627/C0003.
--
-- invoice_series holds the next number of each store, kind and year. A
-- document takes it under the row lock until commit, so numbers stay
-- gapless, as doc_counters does for the shop-wide series.

-- +goose Up
ALTER TABLE settings
  ADD COLUMN invoice_numbering text NOT NULL DEFAULT 'shop' CHECK (invoice_numbering ~ '^[a-z_]{1,20}$');

CREATE TABLE invoice_series (
  store_id bigint NOT NULL REFERENCES stores(id),
  kind text NOT NULL CHECK (kind ~ '^[a-z_]{1,20}$'),
  fy text NOT NULL CHECK (fy ~ '^[0-9]{4}$'),
  next_value bigint NOT NULL CHECK (next_value > 0),
  PRIMARY KEY (store_id, kind, fy)
);

-- +goose Down
DROP TABLE invoice_series;
ALTER TABLE settings DROP COLUMN invoice_numbering;
