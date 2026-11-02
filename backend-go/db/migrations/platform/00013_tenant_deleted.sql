-- Final deletion step (S10 item 4, D4). The deletion date is the existing
-- purge_after column (set by the deletion clock, or 30 days after a
-- console delete); warnings go out 14, 7 and 1 day(s) before it. A Super
-- Admin finalises: status deleted, deleted_at and deleted_by. The schema
-- and files are not dropped (an operator does that later, by hand).

-- +goose Up
ALTER TABLE platform.tenants
  ADD COLUMN deleted_at timestamptz,
  ADD COLUMN deleted_by bigint REFERENCES platform.platform_users(id),
  ADD CONSTRAINT tenants_deleted_pair CHECK ((status = 'deleted') = (deleted_at IS NOT NULL) OR deleted_at IS NULL);
UPDATE platform.tenants SET purge_after = coalesce(deletion_requested_at, now()) + interval '30 days'
  WHERE status = 'pending_deletion' AND purge_after IS NULL;

-- +goose Down
ALTER TABLE platform.tenants
  DROP CONSTRAINT tenants_deleted_pair,
  DROP COLUMN deleted_by,
  DROP COLUMN deleted_at;
