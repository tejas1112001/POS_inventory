-- +goose Up
-- Industry Phase 3 item 7 (design §7): the owner's daily expiry email goes
-- through the mail outbox. The settings columns (DB36) came with 00026.
ALTER TABLE email_outbox DROP CONSTRAINT email_outbox_kind_check;
ALTER TABLE email_outbox ADD CONSTRAINT email_outbox_kind_check
  CHECK (kind IN ('receipt','receipt_resend','test','customer','platform_notice','low_stock','expiry_digest'));

-- +goose Down
DELETE FROM email_outbox WHERE kind = 'expiry_digest';
ALTER TABLE email_outbox DROP CONSTRAINT email_outbox_kind_check;
ALTER TABLE email_outbox ADD CONSTRAINT email_outbox_kind_check
  CHECK (kind IN ('receipt','receipt_resend','test','customer','platform_notice','low_stock'));
