-- Step 9: tenant SMTP settings (EMAIL_AND_RECEIPTS §3). The password is
-- envelope-encrypted and bound to this tenant's ID; it is never returned.

-- +goose Up
CREATE TABLE tenant_email_settings (
  id smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  enabled boolean NOT NULL DEFAULT false,
  host text NOT NULL CHECK (host ~ '^[A-Za-z0-9.-]{1,253}$'),
  port int NOT NULL CHECK (port IN (465, 587)),
  tls_mode text NOT NULL CHECK (tls_mode IN ('implicit','starttls')),
  username text NOT NULL CHECK (length(username) BETWEEN 1 AND 254),
  password_ciphertext bytea NOT NULL,
  password_dek_wrapped bytea NOT NULL,
  key_id text NOT NULL,
  from_name text CHECK (length(from_name) <= 78),
  from_email citext NOT NULL CHECK (length(from_email) <= 254),
  fallback_to_platform boolean NOT NULL DEFAULT true,
  daily_cap int NOT NULL DEFAULT 300 CHECK (daily_cap BETWEEN 1 AND 100000),
  status text NOT NULL DEFAULT 'unverified' CHECK (status IN ('unverified','ok','failing','disabled')),
  last_test_at timestamptz, last_success_at timestamptz,
  last_error_code text CHECK (length(last_error_code) <= 60),
  consecutive_failures int NOT NULL DEFAULT 0,
  updated_by bigint REFERENCES users(id),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK ((port = 465 AND tls_mode = 'implicit') OR (port = 587 AND tls_mode = 'starttls'))
);

-- Settings text shown on receipts stays short.
ALTER TABLE settings
  ADD CONSTRAINT settings_receipt_text_len CHECK (length(receipt_header) <= 500 AND length(receipt_footer) <= 500),
  ADD CONSTRAINT settings_invoice_prefix_chk CHECK (invoice_prefix ~ '^[A-Za-z0-9-]{0,20}$');

-- +goose Down
ALTER TABLE settings DROP CONSTRAINT settings_invoice_prefix_chk, DROP CONSTRAINT settings_receipt_text_len;
DROP TABLE tenant_email_settings;
