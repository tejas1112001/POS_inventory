-- Low-stock alerts (S10 item 6, D8): the owner gets a daily email (08:00
-- shop time) listing the items at or below their alert level, once a day
-- and only when there are any. On by default; the owner can turn it off.

-- +goose Up
ALTER TABLE settings ADD COLUMN low_stock_email boolean NOT NULL DEFAULT true;
ALTER TABLE email_outbox DROP CONSTRAINT email_outbox_kind_check;
ALTER TABLE email_outbox ADD CONSTRAINT email_outbox_kind_check
  CHECK (kind IN ('receipt','receipt_resend','test','customer','platform_notice','low_stock'));

-- +goose Down
DELETE FROM email_outbox WHERE kind = 'low_stock';
ALTER TABLE email_outbox DROP CONSTRAINT email_outbox_kind_check;
ALTER TABLE email_outbox ADD CONSTRAINT email_outbox_kind_check
  CHECK (kind IN ('receipt','receipt_resend','test','customer','platform_notice'));
ALTER TABLE settings DROP COLUMN low_stock_email;
