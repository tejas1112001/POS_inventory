-- Held bills (S10 item 7, D9): "Hold" parks a checkout cart; any cashier of
-- the same store can recall it. No stock is reserved (the product locks
-- are released when the cart is parked) and prices and stock are checked
-- again when it is recalled. A held cart expires after 7 days; a user may
-- hold at most 20 at a time. Recall removes the row (once only).

-- +goose Up
CREATE TABLE held_carts (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  store_id bigint NOT NULL REFERENCES stores(id),
  user_id bigint NOT NULL REFERENCES users(id),
  customer_id bigint REFERENCES customers(id),
  label text CHECK (length(label) <= 60),
  lines jsonb NOT NULL CHECK (jsonb_typeof(lines) = 'array' AND jsonb_array_length(lines) BETWEEN 1 AND 500),
  discount numeric(18,4) NOT NULL DEFAULT 0 CHECK (discount >= 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL DEFAULT now() + interval '7 days',
  CHECK (expires_at > created_at)
);
CREATE INDEX held_carts_store_idx ON held_carts (store_id, created_at DESC);
CREATE INDEX held_carts_user_idx ON held_carts (user_id);

-- +goose Down
DROP TABLE held_carts;
