-- Step 16: billing (BILLING_DESIGN §2, §4, §6). platform.subscriptions and
-- platform.plans already exist (steps 1 and 3).

-- +goose Up
CREATE TABLE platform.invoices (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id text NOT NULL REFERENCES platform.tenants(id),
  subscription_id bigint REFERENCES platform.subscriptions(id),
  provider text NOT NULL DEFAULT 'razorpay' CHECK (provider IN ('razorpay','fake')),
  provider_invoice_id text NOT NULL CHECK (length(provider_invoice_id) BETWEEN 1 AND 100),
  amount_minor bigint NOT NULL CHECK (amount_minor >= 0),          -- paise; never floats
  amount_paid_minor bigint NOT NULL DEFAULT 0 CHECK (amount_paid_minor >= 0),
  currency char(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
  status text NOT NULL CHECK (status IN ('issued','partially_paid','paid','cancelled','expired')),
  issued_at timestamptz, paid_at timestamptz,
  short_url text CHECK (length(short_url) <= 500),
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (provider, provider_invoice_id)
);
CREATE INDEX invoices_tenant_idx ON platform.invoices (tenant_id, created_at DESC);

-- Every webhook delivery. Only events with a valid signature keep their
-- payload and take part in dedupe (a forged request must not be able to
-- reserve a real event ID).
CREATE TABLE platform.billing_events (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  provider text NOT NULL DEFAULT 'razorpay' CHECK (provider IN ('razorpay','fake')),
  event_id text NOT NULL CHECK (length(event_id) <= 100),
  event_type text NOT NULL CHECK (length(event_type) <= 100),
  provider_created_at timestamptz,
  signature_valid boolean NOT NULL,
  payload jsonb,
  tenant_id text REFERENCES platform.tenants(id),
  received_at timestamptz NOT NULL DEFAULT now(),
  processed_at timestamptz,
  outcome text CHECK (outcome IN ('applied','stale','stale_ignored','ignored','unknown_subscription','rejected')),
  error text CHECK (length(error) <= 500),
  CHECK (signature_valid OR payload IS NULL)
);
CREATE UNIQUE INDEX billing_events_dedupe_uq ON platform.billing_events (provider, event_id) WHERE signature_valid;
CREATE INDEX billing_events_unprocessed_idx ON platform.billing_events (received_at) WHERE signature_valid AND processed_at IS NULL;
CREATE INDEX billing_events_tenant_idx ON platform.billing_events (tenant_id, received_at DESC);

-- One row per notice sent; the unique key makes re-runs and parallel
-- workers unable to send a notice twice (§6).
CREATE TABLE platform.notification_log (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id text NOT NULL REFERENCES platform.tenants(id),
  kind text NOT NULL CHECK (kind ~ '^[a-z0-9_]{1,60}$'),
  period_key text NOT NULL CHECK (length(period_key) BETWEEN 1 AND 100),
  outbox_id bigint REFERENCES platform.email_outbox(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, kind, period_key)
);

-- Who suspended a tenant: a Super Admin's suspension is never lifted by a
-- payment; a billing suspension is.
ALTER TABLE platform.tenants ADD COLUMN suspension_source text CHECK (suspension_source IN ('admin','billing'));
UPDATE platform.tenants SET suspension_source = 'admin' WHERE status = 'suspended';

-- +goose Down
ALTER TABLE platform.tenants DROP COLUMN suspension_source;
DROP TABLE platform.notification_log;
DROP TABLE platform.billing_events;
DROP TABLE platform.invoices;
