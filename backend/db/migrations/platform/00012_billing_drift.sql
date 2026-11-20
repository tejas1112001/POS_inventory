-- Razorpay reconcile (S10 item 3, D2): the hourly job compares each live
-- subscription with the provider and records differences here. Report
-- only: nothing is fixed automatically. One open row per tenant and
-- field; it is resolved when the values agree again. alerted_at marks the
-- one inbox email sent when a difference is seen in two runs.

-- +goose Up
CREATE TABLE platform.billing_drift (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id text NOT NULL REFERENCES platform.tenants(id),
  field text NOT NULL CHECK (field IN ('status','plan','period_end','fetch')),
  ours text CHECK (length(ours) <= 200),
  theirs text CHECK (length(theirs) <= 200),
  first_seen timestamptz NOT NULL DEFAULT now(),
  last_seen timestamptz NOT NULL DEFAULT now(),
  alerted_at timestamptz,
  resolved_at timestamptz,
  CHECK (last_seen >= first_seen)
);
CREATE UNIQUE INDEX billing_drift_open ON platform.billing_drift (tenant_id, field) WHERE resolved_at IS NULL;
CREATE INDEX billing_drift_recent ON platform.billing_drift (last_seen DESC);

-- +goose Down
DROP TABLE platform.billing_drift;
