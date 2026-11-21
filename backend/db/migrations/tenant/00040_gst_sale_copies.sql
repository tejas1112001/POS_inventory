-- M3 step 2 (docs/M3_GST_DESIGN.md DB5, DB6): each sale and credit note
-- keeps a copy of the tax identity it was made under: the seller (the
-- store's GSTIN and state, or the shop's), the buyer (a registered
-- customer's GSTIN, name, address and state), the place of supply, the
-- split used and whether it is a B2B or B2C invoice. Receipts, reports
-- and credit notes read these copies, never today's settings.
--
-- tax_split cgst_utgst keeps the UTGST amounts in the sgst columns.
-- All nullable: sales made before this step have no copy.

-- +goose Up
ALTER TABLE sales
  ADD COLUMN seller_gstin text,
  ADD COLUMN seller_state text CHECK (seller_state ~ '^[0-9]{2}$'),
  ADD COLUMN buyer_gstin text,
  ADD COLUMN buyer_name text CHECK (length(buyer_name) <= 200),
  ADD COLUMN buyer_address text CHECK (length(buyer_address) <= 700),
  ADD COLUMN buyer_state text CHECK (buyer_state ~ '^[0-9]{2}$'),
  ADD COLUMN place_of_supply text CHECK (place_of_supply ~ '^[0-9]{2}$'),
  ADD COLUMN tax_split text CHECK (tax_split ~ '^[a-z_]{1,20}$'),
  ADD COLUMN invoice_kind text CHECK (invoice_kind IN ('b2b', 'b2c'));

ALTER TABLE sale_returns
  ADD COLUMN seller_gstin text,
  ADD COLUMN seller_state text CHECK (seller_state ~ '^[0-9]{2}$'),
  ADD COLUMN buyer_gstin text,
  ADD COLUMN buyer_name text CHECK (length(buyer_name) <= 200),
  ADD COLUMN buyer_address text CHECK (length(buyer_address) <= 700),
  ADD COLUMN buyer_state text CHECK (buyer_state ~ '^[0-9]{2}$'),
  ADD COLUMN place_of_supply text CHECK (place_of_supply ~ '^[0-9]{2}$'),
  ADD COLUMN tax_split text CHECK (tax_split ~ '^[a-z_]{1,20}$'),
  ADD COLUMN invoice_kind text CHECK (invoice_kind IN ('b2b', 'b2c'));

-- +goose Down
ALTER TABLE sale_returns DROP COLUMN invoice_kind, DROP COLUMN tax_split, DROP COLUMN place_of_supply, DROP COLUMN buyer_state,
  DROP COLUMN buyer_address, DROP COLUMN buyer_name, DROP COLUMN buyer_gstin, DROP COLUMN seller_state, DROP COLUMN seller_gstin;
ALTER TABLE sales DROP COLUMN invoice_kind, DROP COLUMN tax_split, DROP COLUMN place_of_supply, DROP COLUMN buyer_state,
  DROP COLUMN buyer_address, DROP COLUMN buyer_name, DROP COLUMN buyer_gstin, DROP COLUMN seller_state, DROP COLUMN seller_gstin;
