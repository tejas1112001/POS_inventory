-- Price per store (REMAINING "also decided"): a variant may sell at its
-- own price in a store; without a row it sells at product_variants.price.
-- Only the selling price varies: cost and MRP stay per variant. Prices
-- are per base unit, like the variant price (H3).

-- +goose Up
CREATE TABLE store_prices (
  store_id bigint NOT NULL REFERENCES stores(id),
  variant_id bigint NOT NULL REFERENCES product_variants(id),
  price numeric(18,4) NOT NULL CHECK (price >= 0),
  updated_by bigint REFERENCES users(id),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (store_id, variant_id)
);
CREATE INDEX store_prices_variant_idx ON store_prices (variant_id);

-- +goose Down
DROP TABLE store_prices;
