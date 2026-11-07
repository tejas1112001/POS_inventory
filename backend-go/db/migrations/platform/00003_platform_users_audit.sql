-- Super Admin identity, sessions and audit (PHASE1_DESIGN §7, TENANCY §2.1).

-- +goose Up
CREATE TABLE platform.platform_users (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  email extensions.citext NOT NULL UNIQUE CHECK (length(email) <= 254),
  name text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
  role text NOT NULL CHECK (role IN ('super_admin','billing_admin','support')),
  status text NOT NULL DEFAULT 'Active' CHECK (status IN ('Active','Disabled')),
  password_hash text NOT NULL,
  must_change_password boolean NOT NULL DEFAULT true,
  mfa_enrolled boolean NOT NULL DEFAULT false,
  totp_ciphertext bytea, totp_dek bytea, totp_key_id text,
  totp_last_step bigint NOT NULL DEFAULT 0,
  pending_totp_ciphertext bytea, pending_totp_dek bytea, pending_totp_key_id text,
  failed_logins int NOT NULL DEFAULT 0,
  locked_at timestamptz,
  last_login_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (NOT mfa_enrolled OR totp_ciphertext IS NOT NULL)
);

CREATE TABLE platform.platform_recovery_codes (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  user_id bigint NOT NULL REFERENCES platform.platform_users(id) ON DELETE CASCADE,
  code_hash bytea NOT NULL UNIQUE,
  used_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX platform_recovery_codes_user_idx ON platform.platform_recovery_codes (user_id) WHERE used_at IS NULL;

-- One row per signed-in session (refresh-token family).
CREATE TABLE platform.platform_sessions (
  id uuid PRIMARY KEY,
  user_id bigint NOT NULL REFERENCES platform.platform_users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  absolute_expires_at timestamptz NOT NULL,
  stepup_until timestamptz,
  revoked_at timestamptz, revoke_reason text,
  ip inet, user_agent text CHECK (length(user_agent) <= 400)
);
CREATE INDEX platform_sessions_user_idx ON platform.platform_sessions (user_id) WHERE revoked_at IS NULL;

CREATE TABLE platform.platform_refresh_tokens (
  id uuid PRIMARY KEY,
  session_id uuid NOT NULL REFERENCES platform.platform_sessions(id),
  token_hash bytea NOT NULL UNIQUE,
  issued_at timestamptz NOT NULL DEFAULT now(),
  rotated_at timestamptz,
  revoked_at timestamptz
);
CREATE INDEX platform_refresh_tokens_session_idx ON platform.platform_refresh_tokens (session_id);

-- Short-lived steps between password and session: forced password change,
-- MFA enrolment, MFA verification.
CREATE TABLE platform.platform_login_challenges (
  id uuid PRIMARY KEY,
  user_id bigint NOT NULL REFERENCES platform.platform_users(id),
  purpose text NOT NULL CHECK (purpose IN ('password_change','enroll','mfa')),
  attempts int NOT NULL DEFAULT 0,
  expires_at timestamptz NOT NULL,
  used_at timestamptz,
  ip inet,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE platform.platform_audit_log (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  at timestamptz NOT NULL DEFAULT now(),
  actor_id bigint REFERENCES platform.platform_users(id),
  actor_email extensions.citext,
  action text NOT NULL,
  target_tenant text,
  target text,
  before jsonb, after jsonb,          -- redacted: never secrets
  ip inet, user_agent text, request_id text
);
CREATE INDEX platform_audit_log_at_idx ON platform.platform_audit_log (at DESC);
CREATE INDEX platform_audit_log_actor_idx ON platform.platform_audit_log (actor_id, at DESC);
CREATE INDEX platform_audit_log_tenant_idx ON platform.platform_audit_log (target_tenant, at DESC);

CREATE TABLE platform.tenant_slug_history (
  old_slug   extensions.citext PRIMARY KEY,
  tenant_id  text NOT NULL REFERENCES platform.tenants(id),
  changed_at timestamptz NOT NULL DEFAULT now(),
  changed_by bigint NOT NULL REFERENCES platform.platform_users(id),
  redirect_until timestamptz NOT NULL
);

ALTER TABLE platform.tenants ADD COLUMN notes text CHECK (length(notes) <= 2000);

-- Append-only for the application (default privileges granted DML).
REVOKE UPDATE, DELETE, TRUNCATE ON platform.platform_audit_log FROM pos_platform;

-- +goose Down
GRANT UPDATE, DELETE ON platform.platform_audit_log TO pos_platform;
ALTER TABLE platform.tenants DROP COLUMN notes;
DROP TABLE platform.tenant_slug_history;
DROP TABLE platform.platform_audit_log;
DROP TABLE platform.platform_login_challenges;
DROP TABLE platform.platform_refresh_tokens;
DROP TABLE platform.platform_sessions;
DROP TABLE platform.platform_recovery_codes;
DROP TABLE platform.platform_users;
