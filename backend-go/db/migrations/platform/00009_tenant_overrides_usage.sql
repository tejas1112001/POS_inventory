-- Super Admin console (UI S9, docs/S9_SECURITY_NOTE.md, approved):
-- per-tenant limit overrides and feature flags, and a usage snapshot the
-- worker writes so the console never reads tenant schemas.

-- +goose Up
-- One row per override. Rows are never edited or deleted: a change is a new
-- row (the old one is revoked as "replaced"), and revoking only fills the
-- revoke columns, once. Expired rows stop counting by themselves.
CREATE TABLE platform.tenant_overrides (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id text NOT NULL REFERENCES platform.tenants(id),
  key text NOT NULL CHECK (key IN ('max_stores','max_users','max_products','max_emails_per_day','max_storage_mb',
                                   'public_shop','csv_import','new_checkout')),
  value jsonb NOT NULL CHECK (jsonb_typeof(value) IN ('number','boolean')),
  reason text NOT NULL CHECK (length(reason) BETWEEN 10 AND 500),
  expires_at timestamptz NOT NULL,
  created_by bigint NOT NULL REFERENCES platform.platform_users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  revoked_at timestamptz,
  revoked_by bigint REFERENCES platform.platform_users(id),
  revoke_reason text CHECK (length(revoke_reason) <= 500),
  CHECK (expires_at > created_at AND expires_at <= created_at + interval '366 days'),
  CHECK ((revoked_at IS NULL) = (revoked_by IS NULL))
);
CREATE UNIQUE INDEX tenant_overrides_one_open ON platform.tenant_overrides (tenant_id, key) WHERE revoked_at IS NULL;
CREATE INDEX tenant_overrides_tenant_idx ON platform.tenant_overrides (tenant_id, created_at DESC);

REVOKE UPDATE, DELETE ON platform.tenant_overrides FROM pos_platform;
GRANT UPDATE (revoked_at, revoked_by, revoke_reason) ON platform.tenant_overrides TO pos_platform;

-- A revoked override stays revoked: the revoke columns are written once.
-- +goose StatementBegin
CREATE FUNCTION platform.tenant_overrides_revoke_once() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.revoked_at IS NOT NULL THEN
    RAISE EXCEPTION 'tenant override % is already revoked', OLD.id USING ERRCODE = 'check_violation';
  END IF;
  IF NEW.revoked_at IS NULL THEN
    RAISE EXCEPTION 'an override can only be revoked' USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER tenant_overrides_revoke_once BEFORE UPDATE ON platform.tenant_overrides
  FOR EACH ROW EXECUTE FUNCTION platform.tenant_overrides_revoke_once();

-- Counts only (no names, emails or amounts), written by the worker.
CREATE TABLE platform.tenant_usage (
  tenant_id text PRIMARY KEY REFERENCES platform.tenants(id),
  users int NOT NULL DEFAULT 0,
  stores int NOT NULL DEFAULT 0,
  products int NOT NULL DEFAULT 0,
  receipts_sent_7d int NOT NULL DEFAULT 0,
  receipts_failed_7d int NOT NULL DEFAULT 0,
  receipts_waiting int NOT NULL DEFAULT 0,
  taken_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE platform.tenant_usage;
DROP TRIGGER tenant_overrides_revoke_once ON platform.tenant_overrides;
DROP FUNCTION platform.tenant_overrides_revoke_once();
DROP TABLE platform.tenant_overrides;
