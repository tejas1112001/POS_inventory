-- Industry Phase 1 item 2 (docs/INDUSTRY_PHASE1_DESIGN.md, DB4, D3): every
-- payment the cashier records is "recorded", never verified; there is no
-- payment provider for shop sales in this release. A later provider
-- release widens the CHECK with 'verified'. Existing rows get 'recorded'
-- from the default (no backfill job).

-- +goose Up
ALTER TABLE sale_payments ADD COLUMN verification text NOT NULL DEFAULT 'recorded'
  CONSTRAINT sale_payments_verification_check CHECK (verification IN ('recorded'));
ALTER TABLE customer_payments ADD COLUMN verification text NOT NULL DEFAULT 'recorded'
  CONSTRAINT customer_payments_verification_check CHECK (verification IN ('recorded'));

-- +goose Down
ALTER TABLE customer_payments DROP COLUMN verification;
ALTER TABLE sale_payments DROP COLUMN verification;
