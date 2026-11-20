-- Step 18: per-tenant export and restore (OPERATIONS §6.2–6.4, TENANCY §2).

-- +goose Up
-- One row per export. The archive lives in object storage, encrypted with
-- age; sha256 is the hash of the stored (encrypted) object, manifest_sha256
-- the hash of the signed manifest inside it.
CREATE TABLE platform.tenant_exports (
  id uuid PRIMARY KEY,
  tenant_id text NOT NULL REFERENCES platform.tenants(id),
  kind text NOT NULL DEFAULT 'manual' CHECK (kind IN ('manual','nightly','final')),
  status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','running','succeeded','failed','expired')),
  include_files boolean NOT NULL DEFAULT false,
  requested_by text NOT NULL CHECK (length(requested_by) BETWEEN 1 AND 120),
  object_key text CHECK (length(object_key) <= 300),
  size_bytes bigint CHECK (size_bytes >= 0),
  sha256 text CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  manifest_sha256 text CHECK (manifest_sha256 ~ '^[0-9a-f]{64}$'),
  schema_version bigint,
  table_count int, row_count bigint, file_count int,
  error text CHECK (length(error) <= 500),
  created_at timestamptz NOT NULL DEFAULT now(),
  started_at timestamptz, finished_at timestamptz, expired_at timestamptz,
  CHECK (status <> 'succeeded' OR (object_key IS NOT NULL AND sha256 IS NOT NULL AND manifest_sha256 IS NOT NULL))
);
-- One export at a time per tenant.
CREATE UNIQUE INDEX tenant_exports_one_active ON platform.tenant_exports (tenant_id) WHERE status IN ('queued','running');
CREATE INDEX tenant_exports_tenant_idx ON platform.tenant_exports (tenant_id, created_at DESC);

-- One row per restore: a restore test (into scratch, compared, dropped), a
-- kept scratch restore, or a replace of the live schema.
CREATE TABLE platform.tenant_restores (
  id uuid PRIMARY KEY,
  export_id uuid NOT NULL REFERENCES platform.tenant_exports(id),
  tenant_id text NOT NULL REFERENCES platform.tenants(id),
  mode text NOT NULL CHECK (mode IN ('test','scratch','replace')),
  status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','running','succeeded','failed')),
  requested_by text NOT NULL CHECK (length(requested_by) BETWEEN 1 AND 120),
  scratch_schema text CHECK (scratch_schema ~ '^r_[a-z0-9]{6,20}_[0-9]{12}$'),
  old_schema text CHECK (old_schema ~ '^t_[a-z0-9]{6,20}_old_[0-9]{12}$'),
  result jsonb CHECK (result IS NULL OR jsonb_typeof(result) = 'object'),
  error text CHECK (length(error) <= 500),
  created_at timestamptz NOT NULL DEFAULT now(),
  started_at timestamptz, finished_at timestamptz,
  drop_after timestamptz, dropped_at timestamptz
);
CREATE UNIQUE INDEX tenant_restores_one_active ON platform.tenant_restores (tenant_id) WHERE status IN ('queued','running');
CREATE INDEX tenant_restores_tenant_idx ON platform.tenant_restores (tenant_id, created_at DESC);
CREATE INDEX tenant_restores_cleanup_idx ON platform.tenant_restores (drop_after) WHERE dropped_at IS NULL;

-- Day 0 of the deletion clock (BILLING_DESIGN §3): when billing first made
-- the tenant read-only or suspended it. Cleared when it pays.
ALTER TABLE platform.tenants ADD COLUMN restricted_at timestamptz;
UPDATE platform.tenants t SET restricted_at = coalesce(t.suspended_at, t.updated_at)
  WHERE t.status = 'read_only' OR (t.status = 'suspended' AND t.suspension_source = 'billing');

-- +goose Down
ALTER TABLE platform.tenants DROP COLUMN restricted_at;
DROP TABLE platform.tenant_restores;
DROP TABLE platform.tenant_exports;
