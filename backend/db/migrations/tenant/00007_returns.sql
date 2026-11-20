-- Step 10: sales returns (PHASE1_DESIGN §4.3, API_CONTRACT C-13). A return
-- puts the goods back at once. Lines linked to a sold line are checked
-- cumulatively under the sale's row lock. return_items is append-only for
-- the tenant role (db.GrantTenantPrivileges), so a return cannot be removed
-- to make room for another.

-- +goose Up
ALTER TABLE doc_counters DROP CONSTRAINT doc_counters_name_check;
ALTER TABLE doc_counters ADD CONSTRAINT doc_counters_name_check CHECK (name IN ('sale','sale_return'));
INSERT INTO doc_counters (name) VALUES ('sale_return');

CREATE TABLE sale_returns (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  reference text NOT NULL UNIQUE CHECK (length(reference) BETWEEN 1 AND 60),
  date timestamptz NOT NULL DEFAULT now(),
  status text NOT NULL DEFAULT 'Pending' CHECK (status IN ('Pending','Completed')),
  payment_status text NOT NULL CHECK (payment_status IN ('Paid','Partial','Unpaid')),
  total numeric(18,4) NOT NULL CHECK (total >= 0),
  tax numeric(18,4) NOT NULL DEFAULT 0 CHECK (tax >= 0),
  paid numeric(18,4) NOT NULL DEFAULT 0 CHECK (paid >= 0),         -- refunded to the customer
  due numeric(18,4) NOT NULL DEFAULT 0 CHECK (due >= 0),           -- still owed to the customer
  currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
  note text CHECK (length(note) <= 1000),
  sale_id bigint REFERENCES sales(id),                             -- NULL: not linked to a sale
  customer_id bigint NOT NULL REFERENCES customers(id),
  store_id bigint NOT NULL REFERENCES stores(id),
  created_by bigint NOT NULL REFERENCES users(id),
  impersonation_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (paid + due = total)
);
CREATE INDEX sale_returns_date_idx ON sale_returns (date DESC);
CREATE INDEX sale_returns_sale_idx ON sale_returns (sale_id);

CREATE TABLE return_items (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  sale_return_id bigint NOT NULL REFERENCES sale_returns(id),
  order_item_id bigint REFERENCES order_items(id),                 -- the sold line; NULL for unlinked returns
  product_id bigint REFERENCES products(id),
  variant_id bigint NOT NULL REFERENCES product_variants(id),
  product_name text NOT NULL,                                      -- snapshots, as on order_items
  sku text,
  quantity integer NOT NULL CHECK (quantity > 0),
  unit_price numeric(18,4) NOT NULL CHECK (unit_price >= 0),       -- the sold unit price
  discount numeric(18,4) NOT NULL DEFAULT 0 CHECK (discount >= 0), -- share of the sale discount
  tax_amount numeric(18,4) NOT NULL DEFAULT 0 CHECK (tax_amount >= 0),
  cgst numeric(18,4), sgst numeric(18,4), igst numeric(18,4),
  subtotal numeric(18,4) NOT NULL CHECK (subtotal >= 0),           -- refund for this line, tax included
  reason text CHECK (length(reason) <= 500),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX return_items_return_idx ON return_items (sale_return_id);
CREATE INDEX return_items_order_item_idx ON return_items (order_item_id);

-- +goose Down
DROP TABLE return_items;
DROP TABLE sale_returns;
DELETE FROM doc_counters WHERE name = 'sale_return';
ALTER TABLE doc_counters DROP CONSTRAINT doc_counters_name_check;
ALTER TABLE doc_counters ADD CONSTRAINT doc_counters_name_check CHECK (name IN ('sale'));
