-- Which checkout the shop uses (UI S6): false keeps the old POS screen,
-- true switches every till of the shop to the new checkout. Off by default,
-- so nothing changes until the owner turns it on in Settings.

-- +goose Up
ALTER TABLE settings ADD COLUMN new_checkout boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE settings DROP COLUMN new_checkout;
