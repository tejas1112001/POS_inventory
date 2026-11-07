-- Tenant schema, first part (PHASE1_DESIGN §5): identity, settings, stores,
-- customers, audit log and the email outbox. Later steps add their tables
-- as new migrations (expand/contract, TENANCY §8).
-- Runs with search_path = "t_<id>", extensions and role pos_owner (startup
-- parameters), so names are unqualified and citext/pg_trgm resolve.

-- +goose Up
CREATE TABLE roles (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name citext NOT NULL CHECK (length(name) BETWEEN 1 AND 60),
  description text CHECK (length(description) <= 500),
  color text NOT NULL DEFAULT '#6366f1' CHECK (color ~ '^#[0-9A-Fa-f]{6}$'),
  is_system boolean NOT NULL DEFAULT false,
  system_key text UNIQUE CHECK (system_key IN ('owner','admin','biller')),
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE UNIQUE INDEX roles_name_uq ON roles (name) WHERE deleted_at IS NULL;

CREATE TABLE permissions (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  module text NOT NULL, action text NOT NULL, label text,
  UNIQUE (module, action)
);

CREATE TABLE role_permissions (
  role_id bigint NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
  permission_id bigint NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
  PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE users (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
  email citext NOT NULL CHECK (length(email) <= 254),
  password_hash text,
  status text NOT NULL DEFAULT 'Active' CHECK (status IN ('Active','Inactive')),
  role_id bigint NOT NULL REFERENCES roles(id),
  token_version int NOT NULL DEFAULT 1,
  failed_logins int NOT NULL DEFAULT 0, locked_until timestamptz,
  password_changed_at timestamptz, last_login_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE UNIQUE INDEX users_email_uq ON users (email) WHERE deleted_at IS NULL;
CREATE INDEX users_role_idx ON users (role_id);

CREATE TABLE refresh_tokens (
  id uuid PRIMARY KEY, user_id bigint NOT NULL REFERENCES users(id),
  family_id uuid NOT NULL, token_hash bytea NOT NULL UNIQUE,
  remember boolean NOT NULL, issued_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL, absolute_expires_at timestamptz NOT NULL,
  rotated_at timestamptz, revoked_at timestamptz, revoke_reason text,
  ip inet, user_agent text CHECK (length(user_agent) <= 400)
);
CREATE INDEX refresh_tokens_user_idx ON refresh_tokens (user_id) WHERE revoked_at IS NULL;
CREATE INDEX refresh_tokens_family_idx ON refresh_tokens (family_id);

CREATE TABLE auth_tokens (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id bigint NOT NULL REFERENCES users(id),
  purpose text NOT NULL CHECK (purpose IN ('invite','reset')),
  token_hash bytea NOT NULL UNIQUE, expires_at timestamptz NOT NULL, used_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX auth_tokens_user_idx ON auth_tokens (user_id, purpose) WHERE used_at IS NULL;

CREATE TABLE settings (
  id smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  business_name text NOT NULL CHECK (length(business_name) BETWEEN 1 AND 120),
  legal_name text, address text, phone text, email citext,
  logo_file_id uuid,
  currency char(3) NOT NULL DEFAULT 'INR' CHECK (currency ~ '^[A-Z]{3}$'),
  locale text NOT NULL DEFAULT 'en-IN' CHECK (locale ~ '^en-[A-Z]{2}$'),
  timezone text NOT NULL DEFAULT 'Asia/Kolkata',
  tax_label text NOT NULL DEFAULT 'GST', tax_registration_no text,
  tax_split text NOT NULL DEFAULT 'cgst_sgst' CHECK (tax_split IN ('none','cgst_sgst','igst')),
  prices_include_tax_default boolean NOT NULL DEFAULT false,
  discount_mode text NOT NULL DEFAULT 'prorata_pre_tax',
  max_discount_pct_without_override numeric(5,2) NOT NULL DEFAULT 10 CHECK (max_discount_pct_without_override BETWEEN 0 AND 100),
  invoice_prefix text NOT NULL DEFAULT 'POS-INV',
  receipt_header text, receipt_footer text,
  default_customer_id bigint,
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE stores (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
  -- Compared as text: a regex on citext would be case-insensitive.
  slug citext NOT NULL CHECK (slug::text ~ '^[a-z0-9](?:[a-z0-9-]{0,58}[a-z0-9])?$'),
  address text, phone text,
  status text NOT NULL DEFAULT 'Active' CHECK (status IN ('Active','Inactive')),
  logo_file_id uuid, banner_file_id uuid, description text, opening_hours text,
  is_public_enabled boolean NOT NULL DEFAULT true,
  created_by bigint REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE UNIQUE INDEX stores_slug_uq ON stores (slug) WHERE deleted_at IS NULL;

CREATE TABLE customers (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
  email citext, phone text, address text, city text, country text,
  image_file_id uuid,
  status text NOT NULL DEFAULT 'Active' CHECK (status IN ('Active','Inactive')),
  marketing_consent boolean NOT NULL DEFAULT false, marketing_consent_at timestamptz,
  created_by bigint REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  deleted_at timestamptz
);
CREATE INDEX customers_name_trgm ON customers USING gin (name gin_trgm_ops);
CREATE INDEX customers_phone_idx ON customers (phone);
CREATE INDEX customers_email_idx ON customers (email);

CREATE TABLE audit_log (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  at timestamptz NOT NULL DEFAULT now(),
  actor_type text NOT NULL CHECK (actor_type IN ('user','platform_impersonation','system')),
  actor_id bigint, impersonation_id uuid,
  action text NOT NULL, entity text NOT NULL, entity_id text,
  before jsonb, after jsonb,
  ip inet, user_agent text, request_id text
);
CREATE INDEX audit_log_at_idx ON audit_log (at DESC);
CREATE INDEX audit_log_entity_idx ON audit_log (entity, entity_id);

CREATE TABLE email_outbox (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  idempotency_key text NOT NULL UNIQUE,
  kind text NOT NULL CHECK (kind IN ('receipt','receipt_resend','test','customer','platform_notice')),
  to_addr citext NOT NULL, to_name text,
  template text NOT NULL, template_data jsonb NOT NULL CHECK (jsonb_typeof(template_data) = 'object'),
  attachment_receipt_id bigint,
  sender_policy text NOT NULL DEFAULT 'tenant_then_platform',
  status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','sending','sent','failed','dead','cancelled')),
  attempts int NOT NULL DEFAULT 0, max_attempts int NOT NULL DEFAULT 8,
  next_retry_at timestamptz NOT NULL DEFAULT now(),
  lease_until timestamptz, leased_by text,
  last_error_class text, last_error text CHECK (length(last_error) <= 500),
  sent_via text CHECK (sent_via IN ('tenant','platform')), provider_message_id text,
  created_at timestamptz NOT NULL DEFAULT now(), sent_at timestamptz
);
CREATE INDEX email_outbox_due ON email_outbox (next_retry_at) WHERE status IN ('pending','failed');
-- The mark_outbox_pending() trigger is created by the runner's post-step,
-- because it carries this tenant's ID as a literal (EMAIL §4.2).

-- +goose Down
DROP TABLE email_outbox;
DROP TABLE audit_log;
DROP TABLE customers;
DROP TABLE stores;
DROP TABLE settings;
DROP TABLE auth_tokens;
DROP TABLE refresh_tokens;
DROP TABLE users;
DROP TABLE role_permissions;
DROP TABLE permissions;
DROP TABLE roles;
