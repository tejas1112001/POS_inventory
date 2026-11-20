-- Step 8: sales, order lines, gapless document numbers and idempotency
-- keys (S2). PHASE1_DESIGN §4.3, §4.4, §5; API_CONTRACT C-04, C-10, C-11.

-- +goose Up
-- Gapless per-tenant counters (GST invoices need consecutive numbers). The
-- row lock is held until commit, so a rolled-back sale leaves no gap.
CREATE TABLE doc_counters (
  name text PRIMARY KEY CHECK (name IN ('sale')),
  next_value bigint NOT NULL DEFAULT 1 CHECK (next_value >= 1)
);
INSERT INTO doc_counters (name) VALUES ('sale');

CREATE TABLE sales (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  reference text NOT NULL UNIQUE CHECK (length(reference) BETWEEN 1 AND 60),
  date timestamptz NOT NULL DEFAULT now(),
  status text NOT NULL DEFAULT 'Completed' CHECK (status IN ('Completed','Pending','Void')),
  payment_status text NOT NULL CHECK (payment_status IN ('Paid','Partial','Unpaid','Overdue')),
  payment_method text CHECK (length(payment_method) <= 40),
  payment_note text CHECK (length(payment_note) <= 1000),
  note text CHECK (length(note) <= 1000),
  subtotal numeric(18,4) NOT NULL CHECK (subtotal >= 0),
  discount numeric(18,4) NOT NULL DEFAULT 0 CHECK (discount >= 0),
  tax numeric(18,4) NOT NULL DEFAULT 0 CHECK (tax >= 0),
  shipping numeric(18,4) NOT NULL DEFAULT 0 CHECK (shipping >= 0),
  total numeric(18,4) NOT NULL CHECK (total >= 0),
  paid numeric(18,4) NOT NULL DEFAULT 0 CHECK (paid >= 0),
  due numeric(18,4) NOT NULL DEFAULT 0 CHECK (due >= 0),
  tendered numeric(18,4) CHECK (tendered >= 0),
  change_due numeric(18,4) CHECK (change_due >= 0),
  currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
  customer_id bigint NOT NULL REFERENCES customers(id),
  store_id bigint NOT NULL REFERENCES stores(id),
  user_id bigint NOT NULL REFERENCES users(id),            -- biller; never changed by an edit (Q-10)
  updated_by bigint REFERENCES users(id),
  impersonation_id uuid,
  receipt_email citext CHECK (length(receipt_email) <= 254),
  voided_at timestamptz, voided_by bigint REFERENCES users(id),
  void_reason text CHECK (length(void_reason) <= 500),
  version int NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (paid <= total),
  CHECK ((status = 'Void') = (voided_at IS NOT NULL))
);
CREATE INDEX sales_date_idx ON sales (date DESC) WHERE status <> 'Void';
CREATE INDEX sales_user_date_idx ON sales (user_id, date DESC);
CREATE INDEX sales_store_date_idx ON sales (store_id, date DESC);
CREATE INDEX sales_customer_idx ON sales (customer_id);
CREATE INDEX sales_ref_trgm ON sales USING gin (reference gin_trgm_ops);

CREATE TABLE order_items (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  sale_id bigint NOT NULL REFERENCES sales(id),
  product_id bigint REFERENCES products(id),
  variant_id bigint REFERENCES product_variants(id),
  product_name text NOT NULL,                              -- snapshots for receipts and history
  sku text,
  quantity integer NOT NULL CHECK (quantity > 0),
  unit_price numeric(18,4) NOT NULL CHECK (unit_price >= 0),
  catalog_price numeric(18,4) NOT NULL CHECK (catalog_price >= 0), -- differs only on a price override (Q-3)
  discount numeric(18,4) NOT NULL DEFAULT 0 CHECK (discount >= 0),
  tax_type text CHECK (tax_type IN ('Exclusive','Inclusive')),
  tax_rate numeric(6,3) NOT NULL DEFAULT 0 CHECK (tax_rate BETWEEN 0 AND 100),
  tax_amount numeric(18,4) NOT NULL DEFAULT 0 CHECK (tax_amount >= 0),
  cgst numeric(18,4), sgst numeric(18,4), igst numeric(18,4),
  subtotal numeric(18,4) NOT NULL CHECK (subtotal >= 0),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX order_items_sale_idx ON order_items (sale_id);
CREATE INDEX order_items_product_idx ON order_items (product_id);
CREATE INDEX order_items_variant_idx ON order_items (variant_id);

-- S2: a retried POST with the same key returns the stored response.
CREATE TABLE idempotency_keys (
  user_id bigint NOT NULL,
  key text NOT NULL CHECK (length(key) BETWEEN 1 AND 100),
  request_hash bytea NOT NULL,
  status int,
  response jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, key)
);
CREATE INDEX idempotency_keys_created_idx ON idempotency_keys (created_at);

-- +goose Down
DROP TABLE idempotency_keys;
DROP TABLE order_items;
DROP TABLE sales;
DROP TABLE doc_counters;
