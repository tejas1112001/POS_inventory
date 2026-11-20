-- Checkpointed stock reconcile (ARCHITECTURE_REVIEW M2). The nightly
-- check used to add up a shop's whole stock and batch ledgers every night,
-- so its cost grew with the shop's history. A full check now runs about
-- once a week and records, per ledger row, the balance up to a movement
-- ID; the nights in between add only the movements after it. The ledgers
-- are append-only, so a recorded balance never goes stale.
--
-- Derived data: never exported (a restored shop rebuilds it on its next
-- full check). No foreign keys: stores, variants and batches are never
-- deleted, and a dropped row only means one more key to re-check.

-- +goose Up
CREATE TABLE ledger_checkpoints (
  ledger text NOT NULL CHECK (ledger ~ '^[a-z_]{1,40}$'), -- stock, batch_stock, batch_quarantine
  store_id bigint NOT NULL,
  item_id bigint NOT NULL,                                 -- the variant (stock) or the batch
  qty bigint NOT NULL,
  PRIMARY KEY (ledger, store_id, item_id)
);

CREATE TABLE ledger_checkpoint_marks (
  ledger text PRIMARY KEY CHECK (ledger ~ '^[a-z_]{1,40}$'), -- stock (stock_movements), batch (batch_movements)
  last_movement_id bigint NOT NULL CHECK (last_movement_id >= 0),
  taken_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE ledger_checkpoint_marks;
DROP TABLE ledger_checkpoints;
