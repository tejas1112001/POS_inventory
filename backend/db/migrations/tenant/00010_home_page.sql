-- What the tenant's main address (https://<slug>.<domain>/) shows: the
-- staff sign-in (default) or the public shop of one store. shop_store_id
-- has no foreign key, like default_customer_id, so restores can load
-- tables in any order; the shop checks the store when it reads it.
-- NULL means the store with slug 'main', or else the oldest active store.

-- +goose Up
ALTER TABLE settings
  ADD COLUMN home_page text NOT NULL DEFAULT 'login' CHECK (home_page IN ('login','shop')),
  ADD COLUMN shop_store_id bigint;

-- +goose Down
ALTER TABLE settings DROP COLUMN shop_store_id, DROP COLUMN home_page;
