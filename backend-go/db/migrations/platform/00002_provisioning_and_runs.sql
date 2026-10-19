-- Provisioning, tenant migration runs, subscriptions, platform mail and the
-- permission catalog (TENANCY §2.1, §6, §8; BILLING_DESIGN §2; EMAIL §4).

-- +goose Up
CREATE TABLE platform.provisioning_runs (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id   text NOT NULL REFERENCES platform.tenants(id),
  step        int NOT NULL DEFAULT 0 CHECK (step BETWEEN 0 AND 10),
  status      text NOT NULL DEFAULT 'running' CHECK (status IN ('running','succeeded','failed','discarded')),
  attempts    int NOT NULL DEFAULT 1,
  last_error  text CHECK (length(last_error) <= 500),
  -- Non-secret request options replayed on retry (owner name, currency, ...).
  request     jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(request) = 'object'),
  started_at  timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz
);
CREATE INDEX provisioning_runs_tenant_idx ON platform.provisioning_runs (tenant_id, started_at DESC);
CREATE INDEX provisioning_runs_stuck_idx ON platform.provisioning_runs (started_at) WHERE status = 'running';

CREATE TABLE platform.migration_runs (
  id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  target_version bigint NOT NULL,
  status         text NOT NULL DEFAULT 'running' CHECK (status IN ('running','succeeded','partial','failed')),
  dry_run        boolean NOT NULL DEFAULT false,
  parallelism    int NOT NULL CHECK (parallelism BETWEEN 1 AND 64),
  started_at     timestamptz NOT NULL DEFAULT now(),
  finished_at    timestamptz
);

CREATE TABLE platform.migration_run_items (
  run_id       bigint NOT NULL REFERENCES platform.migration_runs(id),
  tenant_id    text NOT NULL REFERENCES platform.tenants(id),
  status       text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','succeeded','failed','skipped')),
  from_version bigint,
  to_version   bigint,
  duration_ms  bigint,
  error        text CHECK (length(error) <= 500),
  PRIMARY KEY (run_id, tenant_id)
);

CREATE TABLE platform.subscriptions (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id text NOT NULL UNIQUE REFERENCES platform.tenants(id),
  plan_id bigint NOT NULL REFERENCES platform.plans(id),
  provider text NOT NULL DEFAULT 'razorpay',
  provider_customer_id text, provider_subscription_id text UNIQUE,
  status text NOT NULL CHECK (status IN ('trialing','active','past_due','grace','suspended',
                                         'cancelled','expired','read_only')),
  provider_status text,
  trial_end timestamptz, current_period_start timestamptz, current_period_end timestamptz,
  grace_end timestamptz, cancel_at_period_end boolean NOT NULL DEFAULT false, cancelled_at timestamptz,
  last_event_at timestamptz,
  checkout_short_url text,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX subscriptions_period_idx ON platform.subscriptions (status, current_period_end);
CREATE INDEX subscriptions_trial_idx ON platform.subscriptions (status, trial_end);

CREATE TABLE platform.email_outbox (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  idempotency_key text NOT NULL UNIQUE,
  kind text NOT NULL CHECK (kind IN ('receipt','receipt_resend','test','customer','platform_notice')),
  to_addr extensions.citext NOT NULL, to_name text,
  template text NOT NULL, template_data jsonb NOT NULL CHECK (jsonb_typeof(template_data) = 'object'),
  attachment_receipt_id bigint,
  sender_policy text NOT NULL DEFAULT 'platform',
  status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','sending','sent','failed','dead','cancelled')),
  attempts int NOT NULL DEFAULT 0, max_attempts int NOT NULL DEFAULT 8,
  next_retry_at timestamptz NOT NULL DEFAULT now(),
  lease_until timestamptz, leased_by text,
  last_error_class text, last_error text CHECK (length(last_error) <= 500),
  sent_via text CHECK (sent_via IN ('tenant','platform')), provider_message_id text,
  created_at timestamptz NOT NULL DEFAULT now(), sent_at timestamptz
);
CREATE INDEX email_outbox_due ON platform.email_outbox (next_retry_at) WHERE status IN ('pending','failed');

CREATE TABLE platform.mail_dispatch (
  tenant_id       text PRIMARY KEY REFERENCES platform.tenants(id),
  pending_since   timestamptz NOT NULL DEFAULT now(),
  next_attempt_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE platform.permission_catalog (
  module text NOT NULL CHECK (module ~ '^[a-z_]{2,40}$'),
  action text NOT NULL CHECK (action ~ '^[a-z_]{2,40}$'),
  label  text NOT NULL,
  PRIMARY KEY (module, action)
);

-- New tables get pos_platform DML through the default privileges from 00001.

-- +goose Down
DROP TABLE platform.permission_catalog;
DROP TABLE platform.mail_dispatch;
DROP TABLE platform.email_outbox;
DROP TABLE platform.subscriptions;
DROP TABLE platform.migration_run_items;
DROP TABLE platform.migration_runs;
DROP TABLE platform.provisioning_runs;
