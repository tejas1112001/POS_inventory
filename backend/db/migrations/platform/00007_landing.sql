-- Landing page on the main address (TENANCY §7.1): contact and demo
-- requests, and the lookup behind "Find my shop".

-- +goose Up
-- One row per contact or demo request sent from the landing page. The
-- notice to the platform inbox carries only the row ID; the text stays here.
CREATE TABLE platform.contact_requests (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  topic text NOT NULL CHECK (topic IN ('demo','question')),
  name text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
  email extensions.citext NOT NULL CHECK (length(email) BETWEEN 3 AND 254),
  business text CHECK (length(business) <= 120),
  phone text CHECK (length(phone) <= 40),
  message text NOT NULL DEFAULT '' CHECK (length(message) <= 2000),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX contact_requests_created_idx ON platform.contact_requests (created_at);

-- "Find my shop" looks tenants up by owner or billing email.
CREATE INDEX tenants_owner_email_idx ON platform.tenants (owner_email);
CREATE INDEX tenants_billing_email_idx ON platform.tenants (billing_email) WHERE billing_email IS NOT NULL;

-- +goose Down
DROP INDEX platform.tenants_billing_email_idx;
DROP INDEX platform.tenants_owner_email_idx;
DROP TABLE platform.contact_requests;
