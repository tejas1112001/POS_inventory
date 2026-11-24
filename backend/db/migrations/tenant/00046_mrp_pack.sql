-- Strict MRP (pharmacy compliance plan step 1, §6.2, DB13). An MRP is
-- printed per pack (a strip of 10 or 15 tablets, a bottle); mrp_pack is the
-- number of base units it was printed for, so "₹25.00 per 15 tablets" is
-- kept exactly instead of a rounded per-tablet price. Default 1: today's
-- MRPs keep their meaning (one base unit). The sale copies keep the pack
-- the line's MRP was printed for (order_items.mrp and
-- order_item_batches.mrp are per mrp_pack base units).

-- +goose Up
ALTER TABLE product_variants
  ADD COLUMN mrp_pack numeric(14,3) NOT NULL DEFAULT 1 CONSTRAINT product_variants_mrp_pack_check CHECK (mrp_pack > 0);
ALTER TABLE batches
  ADD COLUMN mrp_pack numeric(14,3) NOT NULL DEFAULT 1 CONSTRAINT batches_mrp_pack_check CHECK (mrp_pack > 0);
ALTER TABLE order_items
  ADD COLUMN mrp_pack numeric(14,3) CONSTRAINT order_items_mrp_pack_check CHECK (mrp_pack > 0);
ALTER TABLE order_item_batches
  ADD COLUMN mrp_pack numeric(14,3) CONSTRAINT order_item_batches_mrp_pack_check CHECK (mrp_pack > 0);

-- +goose Down
ALTER TABLE order_item_batches DROP COLUMN mrp_pack;
ALTER TABLE order_items DROP COLUMN mrp_pack;
ALTER TABLE batches DROP COLUMN mrp_pack;
ALTER TABLE product_variants DROP COLUMN mrp_pack;
