-- Platform registry core (TENANCY §3, BILLING_DESIGN §2).
-- Runs as pos_migrator with role=pos_owner and search_path=platform, extensions
-- set as connection startup parameters. Names are schema-qualified anyway.

-- +goose Up
CREATE TABLE platform.plans (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  code text NOT NULL UNIQUE CHECK (code ~ '^[a-z0-9_]{2,40}$'),
  name text NOT NULL,
  price_minor bigint NOT NULL CHECK (price_minor >= 0),
  currency char(3) NOT NULL DEFAULT 'INR',
  billing_interval text NOT NULL CHECK (billing_interval IN ('monthly','yearly')),
  razorpay_plan_id text UNIQUE,
  trial_days int NOT NULL DEFAULT 14 CHECK (trial_days BETWEEN 0 AND 365),
  limits jsonb NOT NULL CHECK (jsonb_typeof(limits) = 'object'),
  features jsonb NOT NULL CHECK (jsonb_typeof(features) = 'object'),
  is_public boolean NOT NULL DEFAULT true,
  is_active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE platform.tenants (
  id                 text PRIMARY KEY CHECK (id ~ '^[a-z0-9]{10}$'),
  schema_name        text NOT NULL UNIQUE CHECK (schema_name ~ '^t_[a-z0-9]{6,20}$'),
  db_role            text NOT NULL UNIQUE CHECK (db_role ~ '^t_[a-z0-9]{6,20}_rw$'),
  slug               extensions.citext NOT NULL UNIQUE
                     -- Compare as text: on citext the regex would be case-insensitive.
                     CHECK (slug::text ~ '^[a-z0-9](?:[a-z0-9-]{1,28}[a-z0-9])$' AND slug::text !~ '--'),
  name               text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
  status             text NOT NULL CHECK (status IN
                       ('provisioning','failed','trialing','active','past_due','grace',
                        'suspended','read_only','cancelled','pending_deletion','deleted')),
  plan_id            bigint REFERENCES platform.plans(id),
  owner_email        extensions.citext NOT NULL CHECK (length(owner_email) <= 254),
  billing_email      extensions.citext CHECK (length(billing_email) <= 254),
  db_cluster         text NOT NULL DEFAULT 'default' CHECK (db_cluster ~ '^[a-z0-9_-]{1,40}$'),
  schema_version     bigint NOT NULL DEFAULT 0,
  last_activity_at   timestamptz,
  storage_bytes      bigint NOT NULL DEFAULT 0,
  suspended_at       timestamptz,
  suspend_reason     text,
  deletion_requested_at timestamptz,
  purge_after        timestamptz,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now(),
  CHECK (schema_name = 't_' || id),
  CHECK (db_role = 't_' || id || '_rw')
);
CREATE INDEX tenants_status_idx ON platform.tenants (status);

CREATE TABLE platform.reserved_slugs (
  slug       extensions.citext PRIMARY KEY,
  reason     text,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- Hidden internal trial plan (O-3). The owner replaces it with real plans.
INSERT INTO platform.plans (code, name, price_minor, billing_interval, trial_days, limits, features, is_public)
VALUES ('trial', 'Trial', 0, 'monthly', 14,
        '{"max_stores":3,"max_users":10,"max_products":5000,"max_emails_per_day":300,"max_storage_mb":1000}',
        '{"reports_export":true,"public_shop":true,"tenant_smtp":true,"custom_domain":false,"multi_store":true}',
        false);

-- Tell running processes to drop cached tenants when a row changes.
-- +goose StatementBegin
CREATE FUNCTION platform.notify_tenant_changed() RETURNS trigger
  LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
  PERFORM pg_notify('tenant_changed', COALESCE(NEW.id, OLD.id));
  RETURN NULL;
END $$;
-- +goose StatementEnd
CREATE TRIGGER tenants_notify AFTER INSERT OR UPDATE OR DELETE ON platform.tenants
  FOR EACH ROW EXECUTE FUNCTION platform.notify_tenant_changed();

-- pos_platform: DML on platform tables only (TENANCY §5.3).
GRANT USAGE ON SCHEMA platform TO pos_platform;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA platform TO pos_platform;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA platform TO pos_platform;
ALTER DEFAULT PRIVILEGES FOR ROLE pos_owner IN SCHEMA platform
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO pos_platform;
ALTER DEFAULT PRIVILEGES FOR ROLE pos_owner IN SCHEMA platform
  GRANT USAGE, SELECT ON SEQUENCES TO pos_platform;
REVOKE ALL ON FUNCTION platform.notify_tenant_changed() FROM PUBLIC;
-- The migration version table is read-only for the app (/readyz checks it).
REVOKE ALL ON platform.goose_db_version FROM pos_platform;
GRANT SELECT ON platform.goose_db_version TO pos_platform;
-- +goose StatementBegin
DO $$
DECLARE s text := pg_get_serial_sequence('platform.goose_db_version', 'id');
BEGIN
  IF s IS NOT NULL THEN
    EXECUTE format('REVOKE ALL ON SEQUENCE %s FROM pos_platform', s);
  END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE platform.reserved_slugs;
DROP TABLE platform.tenants;
DROP FUNCTION platform.notify_tenant_changed();
DROP TABLE platform.plans;
