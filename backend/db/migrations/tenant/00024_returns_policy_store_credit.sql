-- Industry Phase 1 item 9: configurable returns, exchanges and store credit
-- (docs/INDUSTRY_PHASE1_DESIGN.md §9, DB16-DB22). Every default keeps
-- today's behaviour: refunds only, no time limit, returns without a sale
-- allowed, everything back to stock.

-- +goose Up
-- DB16: the shop's return policy.
ALTER TABLE settings ADD COLUMN return_policy jsonb NOT NULL DEFAULT
  '{"refund": true, "storeCredit": false, "exchange": false, "timeLimitDays": null, "requireLinkedSale": false, "conditions": false, "restockConditions": ["resellable"]}'::jsonb
  CONSTRAINT settings_return_policy_check CHECK (jsonb_typeof(return_policy) = 'object');

-- DB17: the condition of a returned line, and whether it went back to stock.
ALTER TABLE return_items ADD COLUMN condition text
  CONSTRAINT return_items_condition_check CHECK (condition IN ('resellable', 'damaged', 'defective'));
ALTER TABLE return_items ADD COLUMN restocked boolean NOT NULL DEFAULT true;

-- DB18: how a return's value was settled. Existing rows: refund, zeros.
ALTER TABLE sale_returns ADD COLUMN settlement text NOT NULL DEFAULT 'refund'
  CONSTRAINT sale_returns_settlement_check CHECK (settlement IN ('refund', 'store_credit', 'exchange'));
ALTER TABLE sale_returns ADD COLUMN to_store_credit numeric(18,4) NOT NULL DEFAULT 0
  CONSTRAINT sale_returns_to_store_credit_check CHECK (to_store_credit >= 0);
ALTER TABLE sale_returns ADD COLUMN to_exchange numeric(18,4) NOT NULL DEFAULT 0
  CONSTRAINT sale_returns_to_exchange_check CHECK (to_exchange >= 0);
ALTER TABLE sale_returns DROP CONSTRAINT sale_returns_amounts_check;
ALTER TABLE sale_returns ADD CONSTRAINT sale_returns_amounts_check
  CHECK (paid + due + applied_to_due + to_store_credit + to_exchange = total);

-- DB19: payment methods exchange (linked to the return) and store_credit.
ALTER TABLE sale_payments DROP CONSTRAINT sale_payments_method_check;
ALTER TABLE sale_payments ADD CONSTRAINT sale_payments_method_check
  CHECK (method IN ('cash','upi','card','credit','cheque','deposit','points','exchange','store_credit'));
ALTER TABLE sale_payments ADD COLUMN sale_return_id bigint REFERENCES sale_returns(id);
ALTER TABLE sale_payments ADD CONSTRAINT sale_payments_exchange_check
  CHECK ((method = 'exchange') = (sale_return_id IS NOT NULL));

-- DB20: the sale made by an exchange, linked to its return. A plain link,
-- not a foreign key: sale_returns already references sales, and tenant
-- export/restore needs the foreign keys to have no cycle. The exchange
-- payment row (sale_payments.sale_return_id) holds the real foreign key.
ALTER TABLE sales ADD COLUMN exchange_return_id bigint UNIQUE;

-- DB21: the store credit ledger, append-only for the tenant role
-- (db.AppendOnlyTables). The balance is the sum; the code keeps it >= 0
-- under the customer's row lock.
CREATE TABLE customer_credit_entries (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  customer_id bigint NOT NULL REFERENCES customers(id),
  amount numeric(18,4) NOT NULL CHECK (amount <> 0),
  kind text NOT NULL CHECK (kind IN ('return', 'sale', 'adjust', 'reversal')),
  sale_return_id bigint REFERENCES sale_returns(id),
  sale_id bigint REFERENCES sales(id),
  reason text CHECK (length(reason) <= 500),
  currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
  user_id bigint NOT NULL REFERENCES users(id),
  impersonation_id uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (kind <> 'return' OR (amount > 0 AND sale_return_id IS NOT NULL)),
  CHECK (kind <> 'sale' OR (amount < 0 AND sale_id IS NOT NULL)),
  CHECK (kind NOT IN ('adjust', 'reversal') OR length(reason) >= 5)
);
CREATE INDEX customer_credit_entries_customer_idx ON customer_credit_entries (customer_id, created_at);
CREATE INDEX customer_credit_entries_return_idx ON customer_credit_entries (sale_return_id) WHERE sale_return_id IS NOT NULL;
CREATE INDEX customer_credit_entries_sale_idx ON customer_credit_entries (sale_id) WHERE sale_id IS NOT NULL;

-- +goose Down
DROP TABLE customer_credit_entries;
ALTER TABLE sales DROP COLUMN exchange_return_id;
DELETE FROM sale_payments WHERE method IN ('exchange', 'store_credit');
ALTER TABLE sale_payments DROP CONSTRAINT sale_payments_exchange_check;
ALTER TABLE sale_payments DROP COLUMN sale_return_id;
ALTER TABLE sale_payments DROP CONSTRAINT sale_payments_method_check;
ALTER TABLE sale_payments ADD CONSTRAINT sale_payments_method_check
  CHECK (method IN ('cash','upi','card','credit','cheque','deposit','points'));
ALTER TABLE sale_returns DROP CONSTRAINT sale_returns_amounts_check;
ALTER TABLE sale_returns ADD CONSTRAINT sale_returns_amounts_check CHECK (paid + due + applied_to_due = total);
ALTER TABLE sale_returns DROP COLUMN to_exchange;
ALTER TABLE sale_returns DROP COLUMN to_store_credit;
ALTER TABLE sale_returns DROP COLUMN settlement;
ALTER TABLE return_items DROP COLUMN restocked;
ALTER TABLE return_items DROP COLUMN condition;
ALTER TABLE settings DROP COLUMN return_policy;
