-- Part payment, split payment and customer dues (S10 item 5,
-- docs/S10_DESIGN.md §5; D5, D6, D7, D12 and the refund rule §5.3 A).
--
-- sale_payments: how a sale was paid, one row per method; credit = put on
-- the customer's account. sales.paid stays the stored total of the
-- non-credit rows and the code keeps both in sync. A collection against
-- dues (customer_payments) writes a sale_payments row per sale it pays.
-- customer_payments and customer_payment_allocations are append-only for
-- the tenant role (db.AppendOnlyTables): a wrong collection is reversed by
-- a new negative entry with a reason.
-- sale_returns.applied_to_due: the part of a return that reduced what the
-- customer still owed on the sale (option A); only the rest is refunded.

-- +goose Up
ALTER TABLE customers ADD COLUMN credit_limit numeric(18,4) CHECK (credit_limit >= 0);  -- NULL: no credit
ALTER TABLE sales ADD COLUMN due_date date;
CREATE INDEX sales_customer_due_idx ON sales (customer_id) WHERE due > 0 AND status <> 'Void';

CREATE TABLE customer_payments (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  customer_id bigint NOT NULL REFERENCES customers(id),
  amount numeric(18,4) NOT NULL CHECK (amount <> 0),
  method text NOT NULL CHECK (method IN ('cash','upi','card','cheque','deposit')),
  reference text CHECK (length(reference) <= 100),
  note text CHECK (length(note) <= 500),
  currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
  reversal_of bigint UNIQUE REFERENCES customer_payments(id),
  reason text CHECK (length(reason) <= 500),
  store_id bigint REFERENCES stores(id),
  user_id bigint NOT NULL REFERENCES users(id),
  impersonation_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK ((amount < 0) = (reversal_of IS NOT NULL)),
  CHECK (reversal_of IS NULL OR length(reason) >= 5)
);
CREATE INDEX customer_payments_customer_idx ON customer_payments (customer_id, created_at);

CREATE TABLE customer_payment_allocations (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  customer_payment_id bigint NOT NULL REFERENCES customer_payments(id),
  sale_id bigint NOT NULL REFERENCES sales(id),
  amount numeric(18,4) NOT NULL CHECK (amount <> 0),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX customer_payment_allocations_payment_idx ON customer_payment_allocations (customer_payment_id);
CREATE INDEX customer_payment_allocations_sale_idx ON customer_payment_allocations (sale_id);

CREATE TABLE sale_payments (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  sale_id bigint NOT NULL REFERENCES sales(id),
  method text NOT NULL CHECK (method IN ('cash','upi','card','credit','cheque','deposit','points')),
  amount numeric(18,4) NOT NULL CHECK (amount <> 0),
  tendered numeric(18,4) CHECK (tendered >= 0),
  change_due numeric(18,4) CHECK (change_due >= 0),
  reference text CHECK (length(reference) <= 100),
  customer_payment_id bigint REFERENCES customer_payments(id),  -- set: from a collection against dues
  user_id bigint NOT NULL REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (amount > 0 OR customer_payment_id IS NOT NULL),        -- negative only when a collection is reversed
  CHECK (method <> 'credit' OR customer_payment_id IS NULL),
  CHECK (tendered IS NULL OR method = 'cash')
);
CREATE INDEX sale_payments_sale_idx ON sale_payments (sale_id);
CREATE INDEX sale_payments_date_idx ON sale_payments (created_at);

ALTER TABLE sale_returns ADD COLUMN applied_to_due numeric(18,4) NOT NULL DEFAULT 0 CHECK (applied_to_due >= 0);
-- +goose StatementBegin
DO $$
DECLARE c text;
BEGIN
  SELECT conname INTO c FROM pg_constraint
   WHERE conrelid = 'sale_returns'::regclass AND contype = 'c' AND pg_get_constraintdef(oid) LIKE '%(paid + due) = total%';
  IF c IS NOT NULL THEN
    EXECUTE format('ALTER TABLE sale_returns DROP CONSTRAINT %I', c);
  END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE sale_returns ADD CONSTRAINT sale_returns_amounts_check CHECK (paid + due + applied_to_due = total);

-- D5 backfill: every existing sale gets one payment row for its current
-- paid amount, with its current method mapped to the new list (cash when
-- none or unknown); no row when nothing was paid. The sum per sale equals
-- sales.paid exactly.
INSERT INTO sale_payments (sale_id, method, amount, tendered, change_due, user_id, created_at)
SELECT s.id,
       CASE WHEN lower(coalesce(s.payment_method, '')) LIKE '%upi%' THEN 'upi'
            WHEN lower(coalesce(s.payment_method, '')) LIKE '%card%' THEN 'card'
            WHEN lower(coalesce(s.payment_method, '')) ~ '(cheque|check)' THEN 'cheque'
            WHEN lower(coalesce(s.payment_method, '')) ~ '(bank|deposit|transfer|neft|imps|rtgs)' THEN 'deposit'
            WHEN lower(coalesce(s.payment_method, '')) LIKE '%point%' THEN 'points'
            ELSE 'cash' END,
       s.paid,
       CASE WHEN s.tendered IS NOT NULL AND (s.payment_method IS NULL OR lower(s.payment_method) LIKE '%cash%') THEN s.tendered END,
       CASE WHEN s.tendered IS NOT NULL AND (s.payment_method IS NULL OR lower(s.payment_method) LIKE '%cash%') THEN s.change_due END,
       s.user_id, s.date
FROM sales s WHERE s.paid > 0 ORDER BY s.id;

-- +goose Down
DELETE FROM sale_payments;
DROP TABLE sale_payments;
ALTER TABLE sale_returns DROP CONSTRAINT sale_returns_amounts_check;
ALTER TABLE sale_returns DROP COLUMN applied_to_due;
ALTER TABLE sale_returns ADD CONSTRAINT sale_returns_check CHECK (paid + due = total);
DROP TABLE customer_payment_allocations;
DROP TABLE customer_payments;
DROP INDEX sales_customer_due_idx;
ALTER TABLE sales DROP COLUMN due_date;
ALTER TABLE customers DROP COLUMN credit_limit;
