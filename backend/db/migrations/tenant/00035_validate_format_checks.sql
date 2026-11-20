-- ARCHITECTURE_REVIEW M1, second step of 00034: validate the format checks
-- added NOT VALID. VALIDATE scans the table under SHARE UPDATE EXCLUSIVE,
-- which does not block reads or writes. Every existing row already passed
-- the stricter list it replaces.

-- +goose Up
ALTER TABLE email_outbox VALIDATE CONSTRAINT email_outbox_kind_check;
ALTER TABLE stock_movements VALIDATE CONSTRAINT stock_movements_reason_check;
ALTER TABLE batch_movements VALIDATE CONSTRAINT batch_movements_reason_check;

-- +goose Down
-- Nothing to undo: 00034's Down replaces the constraints.
SELECT 1;
