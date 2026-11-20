-- Step 9: receipts (EMAIL_AND_RECEIPTS §5). One row per sale version; an
-- edit or a void adds a new version, old PDFs are kept.

-- +goose Up
CREATE TABLE receipts (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  sale_id bigint NOT NULL REFERENCES sales(id),
  sale_version int NOT NULL CHECK (sale_version >= 1),
  status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','rendered','failed')),
  file_key text CHECK (length(file_key) <= 300),
  sha256 bytea CHECK (sha256 IS NULL OR length(sha256) = 32),
  size_bytes int,
  render_attempts int NOT NULL DEFAULT 0,
  last_error text CHECK (length(last_error) <= 300),
  rendered_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (sale_id, sale_version),
  CHECK (status <> 'rendered' OR (file_key IS NOT NULL AND sha256 IS NOT NULL))
);
CREATE INDEX receipts_pending_idx ON receipts (created_at) WHERE status = 'pending';

CREATE INDEX email_outbox_resend_idx ON email_outbox (to_addr, created_at) WHERE kind = 'receipt_resend';

-- +goose Down
DROP INDEX email_outbox_resend_idx;
DROP TABLE receipts;
