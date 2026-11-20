-- Step 11: purchases (PHASE1_DESIGN §4.3, API_CONTRACT C-09, Q-16). Each
-- line names the variant and store it stocks. Received is terminal: stock
-- goes in once, when the purchase becomes Received.

-- +goose Up
ALTER TABLE doc_counters DROP CONSTRAINT doc_counters_name_check;
ALTER TABLE doc_counters ADD CONSTRAINT doc_counters_name_check CHECK (name IN ('sale','sale_return','purchase'));
INSERT INTO doc_counters (name) VALUES ('purchase');

CREATE TABLE purchases (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  reference text NOT NULL CHECK (length(reference) BETWEEN 1 AND 60), -- the supplier's invoice number, or PUR-000001
  date timestamptz NOT NULL DEFAULT now(),
  status text NOT NULL CHECK (status IN ('Ordered','Pending','Received','Cancelled')),
  payment_status text NOT NULL DEFAULT 'Unpaid' CHECK (payment_status IN ('Paid','Unpaid','Partial','Overdue')),
  subtotal numeric(18,4) NOT NULL CHECK (subtotal >= 0),
  discount numeric(18,4) NOT NULL DEFAULT 0 CHECK (discount >= 0),
  total numeric(18,4) NOT NULL CHECK (total >= 0),
  paid numeric(18,4) NOT NULL DEFAULT 0 CHECK (paid >= 0),
  due numeric(18,4) NOT NULL DEFAULT 0 CHECK (due >= 0),
  currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
  note text CHECK (length(note) <= 1000),
  supplier_id bigint NOT NULL REFERENCES suppliers(id),
  received_at timestamptz,
  received_by bigint REFERENCES users(id),
  created_by bigint NOT NULL REFERENCES users(id),
  updated_by bigint REFERENCES users(id),
  impersonation_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (paid + due = total),
  CHECK ((status = 'Received') = (received_at IS NOT NULL)),
  CONSTRAINT purchases_supplier_reference_key UNIQUE (supplier_id, reference)
);
CREATE INDEX purchases_date_idx ON purchases (date DESC);

CREATE TABLE purchase_items (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  purchase_id bigint NOT NULL REFERENCES purchases(id),
  product_id bigint NOT NULL REFERENCES products(id),
  variant_id bigint NOT NULL REFERENCES product_variants(id),
  store_id bigint NOT NULL REFERENCES stores(id),
  product_name text NOT NULL,                                       -- snapshots
  sku text,
  quantity integer NOT NULL CHECK (quantity > 0),
  unit_price numeric(18,4) NOT NULL CHECK (unit_price >= 0),
  discount numeric(18,4) NOT NULL DEFAULT 0 CHECK (discount >= 0),
  subtotal numeric(18,4) NOT NULL CHECK (subtotal >= 0),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX purchase_items_purchase_idx ON purchase_items (purchase_id);
CREATE INDEX purchase_items_variant_idx ON purchase_items (variant_id);

-- +goose Down
DROP TABLE purchase_items;
DROP TABLE purchases;
DELETE FROM doc_counters WHERE name = 'purchase';
ALTER TABLE doc_counters DROP CONSTRAINT doc_counters_name_check;
ALTER TABLE doc_counters ADD CONSTRAINT doc_counters_name_check CHECK (name IN ('sale','sale_return'));
