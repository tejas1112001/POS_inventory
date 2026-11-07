-- Onboarding checklist (S10 item 8, D10): when the owner chose "Hide
-- checklist" (for good). The ticks themselves come from real data.

-- +goose Up
ALTER TABLE settings ADD COLUMN onboarding_hidden_at timestamptz;

-- +goose Down
ALTER TABLE settings DROP COLUMN onboarding_hidden_at;
