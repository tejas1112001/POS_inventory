-- Super Admin console (S10 item 1, D11): platform users are added by an
-- invite link (7 days). Only a hash of each token is stored; a user may
-- hold two open tokens for one invite (the emailed one and the one the
-- adding admin copies). Re-inviting, deactivating or accepting revokes or
-- uses them all.

-- +goose Up
CREATE TABLE platform.platform_invites (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id bigint NOT NULL REFERENCES platform.platform_users(id),
  token_hash bytea NOT NULL UNIQUE,
  via text NOT NULL CHECK (via IN ('copy','email')),
  expires_at timestamptz NOT NULL,
  used_at timestamptz,
  revoked_at timestamptz,
  created_by bigint REFERENCES platform.platform_users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (expires_at > created_at)
);
CREATE INDEX platform_invites_open_idx ON platform.platform_invites (user_id) WHERE used_at IS NULL AND revoked_at IS NULL;

-- +goose Down
DROP TABLE platform.platform_invites;
