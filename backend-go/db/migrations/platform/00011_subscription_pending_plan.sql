-- Plan changes (S10 item 2, D1): a downgrade is scheduled for the end of
-- the paid period. The provider applies it then; the next subscription
-- event that carries the new plan clears these columns.

-- +goose Up
ALTER TABLE platform.subscriptions
  ADD COLUMN pending_plan_id bigint REFERENCES platform.plans(id),
  ADD COLUMN pending_change_at timestamptz,
  ADD CONSTRAINT subscriptions_pending_pair CHECK ((pending_plan_id IS NULL) = (pending_change_at IS NULL));

-- +goose Down
ALTER TABLE platform.subscriptions
  DROP CONSTRAINT subscriptions_pending_pair,
  DROP COLUMN pending_change_at,
  DROP COLUMN pending_plan_id;
