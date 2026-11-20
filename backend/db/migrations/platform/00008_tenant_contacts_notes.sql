-- Super Admin console (UI S9): people to contact at a tenant, and a notes
-- timeline. Contacts can be edited; notes are append-only (the app role
-- may not change or delete them), so the timeline stays trustworthy.

-- +goose Up
CREATE TABLE platform.tenant_contacts (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id text NOT NULL REFERENCES platform.tenants(id),
  name text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
  role text NOT NULL DEFAULT '' CHECK (length(role) <= 60),          -- "Owner", "Accountant"…
  email extensions.citext CHECK (length(email) <= 254),
  phone text CHECK (length(phone) <= 40),
  is_primary boolean NOT NULL DEFAULT false,
  created_by bigint REFERENCES platform.platform_users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (email IS NOT NULL OR phone IS NOT NULL)
);
CREATE INDEX tenant_contacts_tenant_idx ON platform.tenant_contacts (tenant_id, id);
CREATE UNIQUE INDEX tenant_contacts_one_primary ON platform.tenant_contacts (tenant_id) WHERE is_primary;

CREATE TABLE platform.tenant_notes (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id text NOT NULL REFERENCES platform.tenants(id),
  author_id bigint NOT NULL REFERENCES platform.platform_users(id),
  body text NOT NULL CHECK (length(body) BETWEEN 1 AND 2000),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX tenant_notes_tenant_idx ON platform.tenant_notes (tenant_id, created_at DESC);

-- Default privileges give pos_platform full DML on new tables; notes are
-- insert and read only.
REVOKE UPDATE, DELETE ON platform.tenant_notes FROM pos_platform;

-- +goose Down
DROP TABLE platform.tenant_notes;
DROP TABLE platform.tenant_contacts;
