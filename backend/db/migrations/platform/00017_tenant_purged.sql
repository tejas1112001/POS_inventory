-- ARCHITECTURE_REVIEW M4: a deleted shop's schema, role and files were
-- kept for ever (00013: "an operator does that later, by hand"). The
-- purge job now drops them once the retention window after the final
-- deletion has passed (PURGE_AFTER_DAYS) and the final export's restore
-- test has passed. The tenant row stays as the tombstone: its ID, slug,
-- dates and audit history, with purged_at set.

-- +goose Up
ALTER TABLE platform.tenants
  ADD COLUMN purged_at timestamptz,
  ADD CONSTRAINT tenants_purged_deleted CHECK (purged_at IS NULL OR status = 'deleted');

-- +goose Down
ALTER TABLE platform.tenants DROP CONSTRAINT tenants_purged_deleted, DROP COLUMN purged_at;
